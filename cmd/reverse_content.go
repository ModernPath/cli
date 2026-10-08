package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// reverseContent is the one reader of source bytes for the inventory,
// capture-source and the delivery observation. Given a revision, a path
// tracked at that revision is read from Git as committed, so one commit gives
// the same bytes on every operating system whatever line-ending conversion the
// checkout applies. Every other path, and every path without a revision, is
// read from disk by reverseRead, except in a reader opened for a proof, which
// refuses a path the revision does not hold.
type reverseContent struct {
	root     string
	revision string
	// committedOnly refuses a path that the revision does not hold instead of
	// reading it from disk.
	committedOnly bool
	tracked       map[string]reverseTreeEntry
	batch         *exec.Cmd
	in            io.WriteCloser
	out           *bufio.Reader
}

type reverseTreeEntry struct {
	mode, kind, object string
	size               int64
}

var reverseObjectPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// reverseObjectID reports whether a recorded revision is a Git commit id.
func reverseObjectID(revision string) bool {
	return reverseObjectPattern.MatchString(revision)
}

// openReverseContent prepares reads of paths under root. With an empty
// revision every read is from disk. Otherwise the tree of the revision is
// listed once, keeping the entries of the given paths.
func openReverseContent(root, revision string, paths []string) (*reverseContent, error) {
	content := &reverseContent{root: root, revision: revision, tracked: map[string]reverseTreeEntry{}}
	if revision == "" {
		return content, nil
	}
	if !reverseObjectID(revision) {
		return nil, fmt.Errorf("revision %q is not a Git commit id", revision)
	}
	wanted := make(map[string]bool, len(paths))
	for _, path := range paths {
		wanted[path] = true
	}
	command := exec.Command("git", "-C", root, "ls-tree", "-r", "-z", "-l", "--full-tree", revision)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	scanner.Split(reverseSplitNUL)
	for scanner.Scan() {
		meta, path, ok := strings.Cut(scanner.Text(), "\t")
		if !ok || !wanted[path] {
			continue
		}
		fields := strings.Fields(meta)
		if len(fields) != 4 {
			continue
		}
		// A size is "-" for an entry that is not a blob.
		size, _ := strconv.ParseInt(fields[3], 10, 64)
		content.tracked[path] = reverseTreeEntry{mode: fields[0], kind: fields[1], object: fields[2], size: size}
	}
	scanErr := scanner.Err()
	_, _ = io.Copy(io.Discard, stdout)
	if err := command.Wait(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("revision %s cannot be read in %s: %s", revision, root, detail)
	}
	if scanErr != nil {
		return nil, fmt.Errorf("revision %s cannot be listed in %s: %w", revision, root, scanErr)
	}
	return content, nil
}

// openCommittedReverseContent prepares the reads of a proof of a tested
// commit: a path that the revision does not hold as a file is refused, even
// when a copy on disk is identical, because the commit is what was delivered.
// The inventory and capture keep reading such a path from disk. Without a
// revision, a dirty checkout that the proof refuses, every read is from disk.
func openCommittedReverseContent(root, revision string, paths []string) (*reverseContent, error) {
	content, err := openReverseContent(root, revision, paths)
	if err != nil {
		return nil, err
	}
	content.committedOnly = revision != ""
	return content, nil
}

func reverseSplitNUL(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, 0); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

// link reports whether the path is a symbolic link at the revision.
func (c *reverseContent) link(path string) bool {
	return c.tracked[path].mode == "120000"
}

// size is the committed size of a path tracked at the revision.
func (c *reverseContent) size(path string) (int64, bool) {
	entry, ok := c.tracked[path]
	return entry.size, ok && entry.kind == "blob"
}

// read returns the committed bytes of a path tracked at the revision and the
// disk bytes of any other path, with the refusals of reverseRead. A reader
// opened for a proof refuses any other path.
func (c *reverseContent) read(path string) ([]byte, error) {
	if !reverseSafePath(path) {
		return nil, fmt.Errorf("unsafe source path %q", path)
	}
	entry, ok := c.tracked[path]
	if !ok {
		if c.committedOnly {
			return nil, fmt.Errorf("%s is not in commit %s; the proof reads each file as committed there, so a copy on disk does not count", path, c.revision)
		}
		return reverseRead(c.root, path)
	}
	if entry.mode == "120000" {
		return nil, fmt.Errorf("symlink source refused: %s", path)
	}
	if entry.kind != "blob" || entry.size > 32_000_000 {
		return nil, fmt.Errorf("non-regular or oversized source: %s", path)
	}
	if c.batch == nil {
		// The reader is kept only once it has started, so a failed start
		// leaves nothing for a later read or close to use.
		batch := exec.Command("git", "-C", c.root, "cat-file", "--batch")
		in, err := batch.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := batch.StdoutPipe()
		if err != nil {
			_ = in.Close()
			return nil, err
		}
		if err := batch.Start(); err != nil {
			return nil, err
		}
		c.batch, c.in, c.out = batch, in, bufio.NewReader(out)
	}
	if _, err := io.WriteString(c.in, entry.object+"\n"); err != nil {
		return nil, fmt.Errorf("committed content of %s cannot be read: %w", path, err)
	}
	header, err := c.out.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("committed content of %s cannot be read: %w", path, err)
	}
	if fields := strings.Fields(header); len(fields) != 3 || fields[0] != entry.object || fields[1] != "blob" || fields[2] != strconv.FormatInt(entry.size, 10) {
		return nil, fmt.Errorf("committed content of %s cannot be read: git answered %q", path, strings.TrimSpace(header))
	}
	content := make([]byte, entry.size+1)
	if _, err := io.ReadFull(c.out, content); err != nil {
		return nil, fmt.Errorf("committed content of %s cannot be read: %w", path, err)
	}
	if content[entry.size] != '\n' {
		return nil, fmt.Errorf("committed content of %s cannot be read: git answered a malformed object", path)
	}
	return content[:entry.size], nil
}

// close ends the Git process that read committed content, if one started.
func (c *reverseContent) close() {
	if c.batch == nil {
		return
	}
	_ = c.in.Close()
	_, _ = io.Copy(io.Discard, c.out)
	_ = c.batch.Wait()
	c.batch = nil
}

// reverseFiltered names, with its value, every path whose Git attributes
// select a content filter, Git LFS included: the content Git stores for such
// a file is not the file. A filter attribute that is set, unset or
// unspecified selects no filter and is not named; text, eol and diff
// attributes are not read.
func reverseFiltered(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	command := exec.Command("git", "-C", root, "check-attr", "-z", "--stdin", "filter")
	command.Stdin = strings.NewReader(strings.Join(paths, "\x00") + "\x00")
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("the Git attributes of the files cannot be read: %w", err)
	}
	fields := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	filtered := []string{}
	for i := 0; i+2 < len(fields); i += 3 {
		switch value := fields[i+2]; value {
		case "unspecified", "unset", "set":
		default:
			filtered = append(filtered, fmt.Sprintf("%s (filter=%s)", fields[i], value))
		}
	}
	return filtered, nil
}
