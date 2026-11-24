package cmd

// The chain of commits a capture carries (Epic 390): the commits leading back
// from the run's commit, read from the checkout the command runs in and sent
// with the capture as raw git commit objects, which the server checks by
// hashing and keeps beside the run.
//
// The walk lists commits with `git rev-list --topo-order`, which follows every
// parent and lists no commit before its children, and reads each object as git
// stores it with `git cat-file --batch`. It stops once every stop commit of the
// Release is listed, at the commit limit, or before the objects would pass the
// byte limit; with no stop commit it goes on to the first commits of the
// history. Only an end at a limit or at a shallow checkout's edge leaves the
// chain cut, as the capture records it.

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// The limits of one capture's chain (assumed defaults 597 and 599 of the plan).
var (
	modernizationChainMaxCommits = 2000
	modernizationChainMaxBytes   = 8_000_000
)

// modernizationChainEntry is one commit of the chain: its id and the commit
// object as git stores it, base64-encoded.
type modernizationChainEntry struct {
	SHA    string `json:"sha"`
	Object string `json:"object"`
}

// modernizationChain is what the walk returns: the entries in walk order, how
// the walk ended, and whether that end leaves the chain cut.
type modernizationChain struct {
	Entries []modernizationChainEntry
	Stopped string
	Cut     bool
}

// summary is the one line the command prints about the walk.
func (c modernizationChain) summary() string {
	noun := "commits"
	if len(c.Entries) == 1 {
		noun = "commit"
	}
	return fmt.Sprintf("commit chain: %d %s sent, %s", len(c.Entries), noun, c.Stopped)
}

// payload is the chain as the capture carries it: a list of {sha, object}.
func (c modernizationChain) payload() []map[string]any {
	entries := make([]map[string]any, 0, len(c.Entries))
	for _, entry := range c.Entries {
		entries = append(entries, map[string]any{"sha": entry.SHA, "object": entry.Object})
	}
	return entries
}

// modernizationCheckout says whether the folder is a git checkout that holds
// the run's commit: when it does, the commit's full id in lower case; when it
// does not, why the run carries no chain.
type modernizationCheckout struct {
	Held   bool
	FullID string
	Reason string
}

// modernizationCheckoutCommit resolves commit in dir. In a checkout that holds
// the commit it refuses a commit that is not given as its full 40- or
// 64-character id (assumed default 601) and, when the commit is the checkout's
// HEAD, uncommitted changes to tracked files (assumed default 598). Elsewhere
// the run carries no chain and the commit is sent as written (assumed default
// 600).
func modernizationCheckoutCommit(dir, commit string) (modernizationCheckout, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return modernizationCheckout{Reason: "git is not installed"}, nil
	}
	notHeld := modernizationCheckout{Reason: fmt.Sprintf("this folder is not a git checkout that holds commit %s", commit)}
	if strings.HasPrefix(commit, "-") {
		// git would read it as an option.
		return notHeld, nil
	}
	resolved, err := chainGit(dir, nil, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil {
		if strings.Contains(err.Error(), "ambiguous") {
			return modernizationCheckout{}, fmt.Errorf("this checkout holds more than one object that starts with %s: give the full commit id, because the capture carries its commit chain", commit)
		}
		return notHeld, nil
	}
	fullID := strings.ToLower(strings.TrimSpace(string(resolved)))
	if !fullGitSHA.MatchString(commit) {
		return modernizationCheckout{}, fmt.Errorf("this checkout holds commit %s as %s: give the full commit id, because the capture carries its commit chain", commit, fullID)
	}
	if fullID != strings.ToLower(commit) {
		// A full id of an object that is not a commit, such as a tag, peels
		// to another commit: the run's commit itself is not in this checkout.
		return notHeld, nil
	}
	head, err := chainGit(dir, nil, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if err == nil && strings.EqualFold(strings.TrimSpace(string(head)), fullID) {
		status, err := chainGit(dir, nil, "status", "--porcelain", "--untracked-files=no")
		if err == nil && len(bytes.TrimSpace(status)) > 0 {
			return modernizationCheckout{}, fmt.Errorf("commit %s is this checkout's HEAD and tracked files have uncommitted changes: commit or stash them and run the tests again", fullID)
		}
	}
	return modernizationCheckout{Held: true, FullID: fullID}, nil
}

// modernizationChainStops reads the Release's chain_stops, the commits at
// which the walk may stop: in place the reference revision once one is set,
// and the merge commits the Proof tab Epic adds. A Release read without a
// modernization object gives none.
func modernizationChainStops(env *cliEnv, releaseID int) ([]string, error) {
	status, body, err := env.get(fmt.Sprintf("/api/compliance/releases/%d?system_id=%d", releaseID, env.SystemID))
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("Release read refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Release response is missing its data object")
	}
	modernization, _ := data["modernization"].(map[string]any)
	return stringSlice(modernization["chain_stops"]), nil
}

// modernizationCommitChain walks the history of dir back from commit, a full
// id the checkout holds, until it has listed every commit in stops (compared
// in lower case), or reached the first commits of the history, or hit a limit.
// While a stop commit is still unlisted, the parents of the stop commits
// already listed are listed too.
func modernizationCommitChain(dir, commit string, stops []string) (modernizationChain, error) {
	unlisted := map[string]bool{}
	for _, stop := range stops {
		if stop = strings.ToLower(strings.TrimSpace(stop)); stop != "" {
			unlisted[stop] = true
		}
	}
	hasStops := len(unlisted) > 0
	maxCommits := modernizationChainMaxCommits
	// One commit past the limit tells a list the limit cut from one that ended.
	out, err := chainGit(dir, nil, "rev-list", "--topo-order", "--max-count="+strconv.Itoa(maxCommits+1), commit, "--")
	if err != nil {
		return modernizationChain{}, fmt.Errorf("listing the commit chain: %w", err)
	}
	chain := modernizationChain{}
	var ids []string
	for i, id := range strings.Fields(string(out)) {
		if i == maxCommits {
			chain.Stopped, chain.Cut = fmt.Sprintf("cut at the limit of %d commits", maxCommits), true
			break
		}
		id = strings.ToLower(id)
		ids = append(ids, id)
		delete(unlisted, id)
		if hasStops && len(unlisted) == 0 {
			chain.Stopped = "ended once every stop commit was listed"
			break
		}
	}
	sizes, err := chainObjectSizes(dir, ids)
	if err != nil {
		return modernizationChain{}, err
	}
	kept, total := len(ids), 0
	for i, size := range sizes {
		if total+size > modernizationChainMaxBytes {
			chain.Stopped, chain.Cut = fmt.Sprintf("cut before the limit of %d bytes of commit objects", modernizationChainMaxBytes), true
			kept = i
			break
		}
		total += size
	}
	ids = ids[:kept]
	objects, err := chainObjects(dir, ids)
	if err != nil {
		return modernizationChain{}, err
	}
	listed := map[string]bool{}
	for _, id := range ids {
		listed[id] = true
	}
	outside := false
	for i, object := range objects {
		chain.Entries = append(chain.Entries, modernizationChainEntry{SHA: ids[i], Object: base64.StdEncoding.EncodeToString(object)})
		for _, parent := range commitParents(object) {
			if !listed[parent] {
				outside = true
			}
		}
	}
	if chain.Stopped == "" {
		// The list ended: at the first commits of the history, or at the edge
		// of a shallow checkout, whose last commits still name parents the
		// checkout does not hold.
		if outside {
			chain.Stopped, chain.Cut = "cut at the edge of the shallow checkout", true
		} else {
			chain.Stopped = "reached the start of the history"
		}
	}
	return chain, nil
}

// chainGit runs git in dir with GIT_NO_REPLACE_OBJECTS set, so replace refs
// cannot change what the command reads, and returns its output.
func chainGit(dir string, stdin io.Reader, args ...string) ([]byte, error) {
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1")
	command.Stdin = stdin
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// chainObjectSizes reads the size of each commit object through
// `git cat-file --batch-check`, so the byte limit is applied before any object
// is read.
func chainObjectSizes(dir string, ids []string) ([]int, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := chainGit(dir, strings.NewReader(strings.Join(ids, "\n")+"\n"), "cat-file", "--batch-check")
	if err != nil {
		return nil, fmt.Errorf("reading the commit sizes: %w", err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != len(ids) {
		return nil, fmt.Errorf("git cat-file --batch-check answered %d lines for %d commits", len(lines), len(ids))
	}
	sizes := make([]int, len(ids))
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 || !strings.EqualFold(fields[0], ids[i]) || fields[1] != "commit" {
			return nil, fmt.Errorf("commit %s could not be read: %s", ids[i], strings.TrimSpace(line))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			return nil, fmt.Errorf("commit %s has no readable size: %s", ids[i], strings.TrimSpace(line))
		}
		sizes[i] = size
	}
	return sizes, nil
}

// chainObjects reads the commit objects of ids, exactly as git stores them,
// through `git cat-file --batch`, which answers each id with
// "<id> commit <size>\n", the object and a newline.
func chainObjects(dir string, ids []string) ([][]byte, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	out, err := chainGit(dir, strings.NewReader(strings.Join(ids, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return nil, fmt.Errorf("reading the commit objects: %w", err)
	}
	reader := bufio.NewReader(bytes.NewReader(out))
	objects := make([][]byte, 0, len(ids))
	for _, id := range ids {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("reading commit %s: %w", id, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || !strings.EqualFold(fields[0], id) || fields[1] != "commit" {
			return nil, fmt.Errorf("commit %s could not be read: %s", id, strings.TrimSpace(header))
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 {
			return nil, fmt.Errorf("commit %s has no readable size: %s", id, strings.TrimSpace(header))
		}
		object := make([]byte, size)
		if _, err := io.ReadFull(reader, object); err != nil {
			return nil, fmt.Errorf("reading commit %s: %w", id, err)
		}
		if _, err := reader.ReadByte(); err != nil && err != io.EOF {
			return nil, fmt.Errorf("reading commit %s: %w", id, err)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

// commitParents reads the parent lines of a commit object's header, which is
// ASCII up to the author line, so an author or a message in any encoding is
// left alone.
func commitParents(object []byte) []string {
	header := object
	if at := bytes.Index(object, []byte("\nauthor ")); at >= 0 {
		header = object[:at]
	} else if at := bytes.Index(object, []byte("\n\n")); at >= 0 {
		header = object[:at]
	}
	var parents []string
	for _, line := range bytes.Split(header, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("parent ")) {
			parents = append(parents, strings.ToLower(strings.TrimSpace(string(line[len("parent "):]))))
		}
	}
	return parents
}
