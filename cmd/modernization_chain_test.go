package cmd

// Epic 390 (D5), test case TC-D5-10: `modernpath modernization record` walks
// the commit chain of a temporary git repository to the Release's stop commits
// or the root, 2,000 commits or 8,000,000 bytes, reports a cut chain, refuses a
// short commit id and, at HEAD, uncommitted changes in a checkout that holds
// the commit, sends none elsewhere, and its help, the CLI guide and the CLI
// reference describe the chain (SR-D5-2, criteria 0 and 1).
//
// The test builds its repositories under a git configuration pinned here, so
// the commit objects and their ids are the same on every machine: HOME in a
// temporary folder, no system or global configuration, fixed author and
// committer names, emails and dates, a fixed default branch, no signing, no
// hooks and no line-ending conversion. The command runs git in the process's
// folder, so each case changes into its repository with t.Chdir and nothing
// here runs in parallel. A test server answers the Release read and the
// capture.
//
// The fixture cmd/testdata/commit_chain_capture.json holds the sha and the
// commit_chain the command sends for the merge repository, walked to the root.
// It is written by writeCommitChainFixture from the test's own reading of
// `git rev-list` and `git cat-file`, never from the command under test, when
// MODERNPATH_WRITE_COMMIT_CHAIN_FIXTURE=1; the capture route's test posts the
// same fixture, so the command's payload meets the route's checks.
//
// On slice 1 the walk entry point returns no chain, so every case that reads a
// sent chain, a refusal before sending, the lower-cased commit, the no-chain
// notice or a described text fails on its assertion. The guards hold: the
// limits' defaults, the fixture's commit ids, a commit other than HEAD sent
// despite uncommitted changes, a file from outside git sent as written without
// the commit_chain key, and the printed invalid_commit_chain refusal.

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const commitChainFixturePath = "testdata/commit_chain_capture.json"

// The test case and criterion every capture file of these tests names.
const (
	chainTestCaseID  = "22222222-2222-4222-8222-222222222222"
	chainCriterionID = "11111111-1111-4111-8111-111111111111"
)

// pinGit makes git deterministic for the test and for the command, which
// inherits the process environment: no setting of the machine applies, every
// commit has the same author, committer and dates, the default branch is
// fixed, and nothing signs, runs a hook or converts line endings. Variables
// that point git at another repository are removed.
func pinGit(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	global := filepath.Join(home, "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(home, "hooks")
	if err := os.MkdirAll(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{
		"HOME":                home,
		"XDG_CONFIG_HOME":     filepath.Join(home, "xdg"),
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_CONFIG_GLOBAL":   global,
		"GIT_AUTHOR_NAME":     "Fixture",
		"GIT_AUTHOR_EMAIL":    "fixture@example.com",
		"GIT_AUTHOR_DATE":     "2026-01-02T03:04:05Z",
		"GIT_COMMITTER_NAME":  "Fixture",
		"GIT_COMMITTER_EMAIL": "fixture@example.com",
		"GIT_COMMITTER_DATE":  "2026-01-02T03:04:05Z",
		"GIT_CONFIG_COUNT":    "5",
		"GIT_CONFIG_KEY_0":    "init.defaultBranch",
		"GIT_CONFIG_VALUE_0":  "main",
		"GIT_CONFIG_KEY_1":    "commit.gpgsign",
		"GIT_CONFIG_VALUE_1":  "false",
		"GIT_CONFIG_KEY_2":    "core.hooksPath",
		"GIT_CONFIG_VALUE_2":  hooks,
		"GIT_CONFIG_KEY_3":    "core.autocrlf",
		"GIT_CONFIG_VALUE_3":  "false",
		"GIT_CONFIG_KEY_4":    "log.showSignature",
		"GIT_CONFIG_VALUE_4":  "false",
	} {
		t.Setenv(name, value)
	}
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES"} {
		unsetenv(t, name)
	}
}

// unsetenv removes an environment variable for the test and restores it after.
func unsetenv(t *testing.T, name string) {
	t.Helper()
	value, present := os.LookupEnv(name)
	if !present {
		return
	}
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Setenv(name, value) })
}

// gitRun runs git in dir for the test and returns its trimmed output.
func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, stderr.String())
	}
	return strings.TrimSpace(string(out))
}

// chainRepo is a temporary repository and its commits in the order made.
type chainRepo struct {
	dir     string
	commits []string
}

func newChainRepo(t *testing.T) *chainRepo {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "--quiet", "--object-format=sha1")
	return &chainRepo{dir: dir}
}

// commit writes content to file and commits it with message.
func (r *chainRepo) commit(t *testing.T, file, content, message string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, file), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, r.dir, "add", "--", file)
	gitRun(t, r.dir, "commit", "--quiet", "-m", message)
	id := gitRun(t, r.dir, "rev-parse", "HEAD")
	r.commits = append(r.commits, id)
	return id
}

// buildMergeRepo builds A <- B <- D <- M <- E with B <- C <- M, M the merge
// of topic (C) into main (D). HEAD is E.
func buildMergeRepo(t *testing.T) *chainRepo {
	t.Helper()
	r := newChainRepo(t)
	r.commit(t, "f", "a\n", "A")
	r.commit(t, "f", "b\n", "B")
	gitRun(t, r.dir, "checkout", "--quiet", "-b", "topic")
	r.commit(t, "g", "c\n", "C")
	gitRun(t, r.dir, "checkout", "--quiet", "main")
	r.commit(t, "f", "d\n", "D")
	gitRun(t, r.dir, "merge", "--quiet", "--no-ff", "-m", "M", "topic")
	r.commits = append(r.commits, gitRun(t, r.dir, "rev-parse", "HEAD"))
	r.commit(t, "f", "e\n", "E")
	return r
}

// buildStraightRepo builds n commits on one line of history.
func buildStraightRepo(t *testing.T, n int) *chainRepo {
	t.Helper()
	r := newChainRepo(t)
	for i := 0; i < n; i++ {
		r.commit(t, "f", fmt.Sprintf("%d\n", i), fmt.Sprintf("commit %d", i))
	}
	return r
}

// cloneShallow clones source over file:// with the given depth, since a plain
// local clone ignores --depth.
func cloneShallow(t *testing.T, source string, depth int) string {
	t.Helper()
	parent := t.TempDir()
	dir := filepath.Join(parent, "shallow")
	gitRun(t, parent, "clone", "--quiet", "--depth", strconv.Itoa(depth), "file://"+source, dir)
	return dir
}

// gitObjects reads the commit objects of ids as git stores them, through
// `git cat-file --batch`, in the order given.
func gitObjects(t *testing.T, dir string, ids []string) [][]byte {
	t.Helper()
	command := exec.Command("git", "cat-file", "--batch")
	command.Dir = dir
	command.Stdin = strings.NewReader(strings.Join(ids, "\n") + "\n")
	out, err := command.Output()
	if err != nil {
		t.Fatalf("git cat-file --batch in %s: %v", dir, err)
	}
	reader := bufio.NewReader(bytes.NewReader(out))
	objects := make([][]byte, 0, len(ids))
	for _, id := range ids {
		header, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the header of %s: %v", id, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != id || fields[1] != "commit" {
			t.Fatalf("git cat-file --batch header for %s = %q", id, header)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil {
			t.Fatal(err)
		}
		object := make([]byte, size)
		if _, err := io.ReadFull(reader, object); err != nil {
			t.Fatalf("reading the object of %s: %v", id, err)
		}
		if _, err := reader.ReadByte(); err != nil {
			t.Fatalf("reading the newline after %s: %v", id, err)
		}
		objects = append(objects, object)
	}
	return objects
}

// commitID is the SHA-1 id git gives a commit object: the hash of "commit ",
// the object's size, a zero byte and the object.
func commitID(object []byte) string {
	sum := sha1.Sum(append([]byte(fmt.Sprintf("commit %d\x00", len(object))), object...))
	return hex.EncodeToString(sum[:])
}

// chainEntries are the entries the command should send for ids, in order.
func chainEntries(t *testing.T, dir string, ids []string) []any {
	t.Helper()
	entries := make([]any, 0, len(ids))
	for i, object := range gitObjects(t, dir, ids) {
		if commitID(object) != ids[i] {
			t.Fatalf("the object git gives for %s hashes to %s", ids[i], commitID(object))
		}
		entries = append(entries, map[string]any{"sha": ids[i], "object": base64.StdEncoding.EncodeToString(object)})
	}
	return entries
}

// writeCommitChainFixture writes the shared fixture from the test's own
// reading of the merge repository: HEAD and the chain from it to the root.
func writeCommitChainFixture(t *testing.T, repo *chainRepo, path string) {
	t.Helper()
	ids := strings.Fields(gitRun(t, repo.dir, "rev-list", "--topo-order", "HEAD"))
	body, err := json.MarshalIndent(map[string]any{"sha": ids[0], "commit_chain": chainEntries(t, repo.dir, ids)}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readCommitChainFixture decodes the fixture as the server would decode the
// command's payload: strings, lists and objects.
func readCommitChainFixture(t *testing.T, path string) map[string]any {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the shared fixture is missing (write it with MODERNPATH_WRITE_COMMIT_CHAIN_FIXTURE=1): %v", err)
	}
	var fixture map[string]any
	if err := json.Unmarshal(body, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func fixtureSHAs(fixture map[string]any) []string {
	entries, _ := fixture["commit_chain"].([]any)
	ids := make([]string, 0, len(entries))
	for _, raw := range entries {
		entry, _ := raw.(map[string]any)
		ids = append(ids, fmt.Sprint(entry["sha"]))
	}
	return ids
}

// chainServer answers the Release read with its stop commits and the capture
// with a receipt, or with a bare refusal code, and keeps what it received.
type chainServer struct {
	t        *testing.T
	server   *httptest.Server
	stops    []string
	refusal  string
	mu       sync.Mutex
	requests []string
	received []map[string]any
}

func newChainServer(t *testing.T, stops []string) *chainServer {
	t.Helper()
	s := &chainServer{t: t, stops: stops}
	s.server = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.server.Close)
	return s
}

func (s *chainServer) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/compliance/releases/17":
		if r.URL.Query().Get("system_id") != "4" {
			s.t.Errorf("the Release read is not scoped to the bound System: %s", r.URL)
		}
		stops := s.stops
		if stops == nil {
			stops = []string{}
		}
		encoded, _ := json.Marshal(stops)
		fmt.Fprintf(w, `{"data":{"id":17,"name":"Cutover","status":"active","release_type":"modernization","legacy_system_id":4,"modernization":{"legacy_system":{"id":4,"name":"T","deleted":false},"scope":null,"contexts":[],"reference_revision":null,"passing_revisions":[],"chain_stops":%s,"results_recorded":false,"current_proof":{"recorded":0,"passed":0,"not_passed":[]},"can_change_reference_revision":false}}}`, encoded)
	case r.Method == http.MethodPost && r.URL.Path == "/api/compliance/releases/17/modernization/test-runs":
		if r.URL.Query().Get("system_id") != "4" {
			s.t.Errorf("the capture is not scoped to the bound System: %s", r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.t.Errorf("the capture body is not JSON: %v", err)
		}
		s.received = append(s.received, body)
		if s.refusal != "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprintf(w, `{"error":%q}`, s.refusal)
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"data":{"run":{"id":"run-1","sha":%q,"system_id":4},"results":[{"id":"result-1","test_case_id":%q,"result":"pass","criterion_ids":[%q]}]}}`, fmt.Sprint(body["sha"]), chainTestCaseID, chainCriterionID)
	default:
		s.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
	}
}

func (s *chainServer) paths() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// lastCapture is the body of the last capture the server received, or nil.
func (s *chainServer) lastCapture() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.received) == 0 {
		return nil
	}
	return s.received[len(s.received)-1]
}

// runRecord runs `modernization record --release-id 17 --file <capture> --json`
// from dir against server, with a capture file whose commit is sha and whose
// one entry passes, and returns what the command wrote to stderr and its error.
func runRecord(t *testing.T, server *chainServer, dir, sha string) (string, error) {
	t.Helper()
	env := nativeCLIEnv(t, server.server, 4)
	file := filepath.Join(t.TempDir(), "capture.json")
	content := fmt.Sprintf(`{"sha":%q,"tests":[{"test_case_id":%q,"result":"pass","criterion_ids":[%q]}]}`, sha, chainTestCaseID, chainCriterionID)
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	command := newModernizationCommand(func() (*cliEnv, error) { return env, nil })
	command.SetArgs([]string{"record", "--release-id", "17", "--file", file, "--json"})
	var out, errOut bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&errOut)
	err := command.Execute()
	return errOut.String(), err
}

// sentChain is the commit_chain the server received, as the ids it lists.
func sentChain(t *testing.T, received map[string]any) ([]string, []any) {
	t.Helper()
	if received == nil {
		t.Fatal("the server received no capture")
	}
	entries, ok := received["commit_chain"].([]any)
	if !ok {
		t.Fatalf("the capture carries no commit_chain list: %v", received)
	}
	ids := make([]string, 0, len(entries))
	for i, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("commit_chain[%d] is not an object: %v", i, raw)
		}
		ids = append(ids, fmt.Sprint(entry["sha"]))
	}
	return ids, entries
}

func TestD5ModernizationRecordWalksTheCommitChain(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is needed to build the test repositories: %v", err)
	}
	fixturePath, err := filepath.Abs(commitChainFixturePath)
	if err != nil {
		t.Fatal(err)
	}
	// Texts are read before any case changes the folder.
	guide := modernizationGuideSection(t)
	pinGit(t)

	merge := buildMergeRepo(t)
	straight := buildStraightRepo(t, 5)
	long := buildStraightRepo(t, 7)
	dirty := buildStraightRepo(t, 5)
	if err := os.WriteFile(filepath.Join(dirty.dir, "f"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	shallow := cloneShallow(t, merge.dir, 2)

	if os.Getenv("MODERNPATH_WRITE_COMMIT_CHAIN_FIXTURE") == "1" {
		writeCommitChainFixture(t, merge, fixturePath)
	}
	fixture := readCommitChainFixture(t, fixturePath)

	t.Run("the limits default to 2,000 commits and 8,000,000 bytes (guard)", func(t *testing.T) {
		if modernizationChainMaxCommits != 2000 || modernizationChainMaxBytes != 8_000_000 {
			t.Errorf("limits = %d commits, %d bytes; want 2000 and 8000000", modernizationChainMaxCommits, modernizationChainMaxBytes)
		}
	})

	t.Run("the merge repository has the fixture's commits (guard)", func(t *testing.T) {
		// A git setting the test failed to pin fails here, not on the chain.
		ids := strings.Fields(gitRun(t, merge.dir, "rev-list", "--topo-order", "HEAD"))
		if !reflect.DeepEqual(ids, fixtureSHAs(fixture)) {
			t.Fatalf("git rev-list --topo-order HEAD = %v; the fixture holds %v", ids, fixtureSHAs(fixture))
		}
		if fixture["sha"] != merge.commits[len(merge.commits)-1] {
			t.Fatalf("fixture sha = %v; HEAD is %s", fixture["sha"], merge.commits[len(merge.commits)-1])
		}
		if len(ids) != 6 {
			t.Fatalf("the merge repository has %d commits, want 6", len(ids))
		}
	})

	t.Run("with no stop commit the walk reaches the root and sends the fixture's chain", func(t *testing.T) {
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, merge.dir, merge.commits[len(merge.commits)-1])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		received := server.lastCapture()
		if received == nil {
			t.Fatal("no capture was sent")
		}
		if received["sha"] != fixture["sha"] {
			t.Errorf("sent sha = %v; want the fixture's %v", received["sha"], fixture["sha"])
		}
		if !reflect.DeepEqual(received["commit_chain"], fixture["commit_chain"]) {
			t.Errorf("sent commit_chain differs from the fixture:\n%v\nwant\n%v", received["commit_chain"], fixture["commit_chain"])
		}
		if want := []string{"GET /api/compliance/releases/17", "POST /api/compliance/releases/17/modernization/test-runs"}; !reflect.DeepEqual(server.paths(), want) {
			t.Errorf("requests = %v; want the Release read, then the capture", server.paths())
		}
		for _, want := range []string{"6 commits sent", "reached the start of the history"} {
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr lacks %q:\n%s", want, stderr)
			}
		}
		if strings.Contains(stderr, "cut") {
			t.Errorf("a walk that reached the root says the chain is cut:\n%s", stderr)
		}
	})

	t.Run("each entry is the commit object git stores, named by its hash, with the keys sha and object", func(t *testing.T) {
		server := newChainServer(t, nil)
		if _, err := runRecord(t, server, merge.dir, merge.commits[len(merge.commits)-1]); err != nil {
			t.Fatal(err)
		}
		ids, entries := sentChain(t, server.lastCapture())
		objects := gitObjects(t, merge.dir, ids)
		for i, raw := range entries {
			entry := raw.(map[string]any)
			if len(entry) != 2 {
				t.Errorf("commit_chain[%d] has the keys %v; want sha and object only", i, entry)
			}
			object, err := base64.StdEncoding.DecodeString(fmt.Sprint(entry["object"]))
			if err != nil {
				t.Fatalf("commit_chain[%d].object is not base64: %v", i, err)
			}
			if !bytes.Equal(object, objects[i]) {
				t.Errorf("commit_chain[%d].object is not the object git cat-file commit %s prints", i, ids[i])
			}
			if commitID(object) != ids[i] {
				t.Errorf("commit_chain[%d].sha = %s; its object hashes to %s", i, ids[i], commitID(object))
			}
		}
	})

	t.Run("the walk ends once every stop commit is listed, past the newer stop to the older", func(t *testing.T) {
		// Commits 0..4; the stops are commit 2 (newer) and commit 1 (older),
		// the older given in upper case: the walk lists 4, 3, 2 and 1.
		server := newChainServer(t, []string{straight.commits[2], strings.ToUpper(straight.commits[1])})
		stderr, err := runRecord(t, server, straight.dir, straight.commits[4])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		ids, _ := sentChain(t, server.lastCapture())
		want := []string{straight.commits[4], straight.commits[3], straight.commits[2], straight.commits[1]}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("sent chain = %v; want %v", ids, want)
		}
		for _, phrase := range []string{"4 commits sent", "ended once every stop commit was listed"} {
			if !strings.Contains(stderr, phrase) {
				t.Errorf("stderr lacks %q:\n%s", phrase, stderr)
			}
		}
		if strings.Contains(stderr, "cut") {
			t.Errorf("a walk that ended at its stop commits says the chain is cut:\n%s", stderr)
		}
	})

	t.Run("a stop commit the history does not hold does not end the walk", func(t *testing.T) {
		server := newChainServer(t, []string{"0123456789abcdef0123456789abcdef01234567"})
		stderr, err := runRecord(t, server, straight.dir, straight.commits[4])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		ids, _ := sentChain(t, server.lastCapture())
		if len(ids) != 5 {
			t.Errorf("sent chain = %v; want all 5 commits", ids)
		}
		if !strings.Contains(stderr, "reached the start of the history") {
			t.Errorf("stderr lacks the end at the start of the history:\n%s", stderr)
		}
	})

	t.Run("the commit limit cuts the walk", func(t *testing.T) {
		saved := modernizationChainMaxCommits
		modernizationChainMaxCommits = 5
		t.Cleanup(func() { modernizationChainMaxCommits = saved })
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, long.dir, long.commits[6])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		ids, _ := sentChain(t, server.lastCapture())
		want := []string{long.commits[6], long.commits[5], long.commits[4], long.commits[3], long.commits[2]}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("sent chain = %v; want the 5 newest commits %v", ids, want)
		}
		for _, phrase := range []string{"5 commits sent", "cut at the limit of 5 commits"} {
			if !strings.Contains(stderr, phrase) {
				t.Errorf("stderr lacks %q:\n%s", phrase, stderr)
			}
		}
	})

	t.Run("the byte limit cuts the walk before the objects pass it", func(t *testing.T) {
		objects := gitObjects(t, long.dir, []string{long.commits[6], long.commits[5], long.commits[4], long.commits[3]})
		// The three newest objects fit exactly; the fourth would pass the limit.
		limit := len(objects[0]) + len(objects[1]) + len(objects[2])
		saved := modernizationChainMaxBytes
		modernizationChainMaxBytes = limit
		t.Cleanup(func() { modernizationChainMaxBytes = saved })
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, long.dir, long.commits[6])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		ids, _ := sentChain(t, server.lastCapture())
		want := []string{long.commits[6], long.commits[5], long.commits[4]}
		if !reflect.DeepEqual(ids, want) {
			t.Errorf("sent chain = %v; want the 3 commits within %d bytes %v", ids, limit, want)
		}
		for _, phrase := range []string{"3 commits sent", fmt.Sprintf("cut before the limit of %d bytes", limit)} {
			if !strings.Contains(stderr, phrase) {
				t.Errorf("stderr lacks %q:\n%s", phrase, stderr)
			}
		}
	})

	t.Run("a shallow checkout cuts the walk at its edge", func(t *testing.T) {
		if gitRun(t, shallow, "rev-parse", "--is-shallow-repository") != "true" {
			t.Fatal("the clone is not shallow")
		}
		head := gitRun(t, shallow, "rev-parse", "HEAD")
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, shallow, head)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		ids, _ := sentChain(t, server.lastCapture())
		if want := []string{merge.commits[5], merge.commits[4]}; !reflect.DeepEqual(ids, want) {
			t.Errorf("sent chain = %v; want the two commits the clone holds %v", ids, want)
		}
		for _, phrase := range []string{"2 commits sent", "cut at the edge of the shallow checkout"} {
			if !strings.Contains(stderr, phrase) {
				t.Errorf("stderr lacks %q:\n%s", phrase, stderr)
			}
		}
	})

	t.Run("a short commit id is refused before anything is sent", func(t *testing.T) {
		server := newChainServer(t, nil)
		head := merge.commits[len(merge.commits)-1]
		_, err := runRecord(t, server, merge.dir, head[:12])
		if err == nil || !strings.Contains(err.Error(), "full commit id") {
			t.Errorf("error = %v; want a refusal that says to give the full commit id", err)
		}
		if len(server.paths()) != 0 {
			t.Errorf("requests = %v; want none", server.paths())
		}
	})

	t.Run("uncommitted changes to tracked files are refused when the commit is HEAD", func(t *testing.T) {
		if gitRun(t, dirty.dir, "status", "--porcelain", "--untracked-files=no") == "" {
			t.Fatal("the dirty repository has no uncommitted change")
		}
		server := newChainServer(t, nil)
		_, err := runRecord(t, server, dirty.dir, dirty.commits[4])
		if err == nil || !strings.Contains(err.Error(), "commit or stash") {
			t.Errorf("error = %v; want a refusal that says to commit or stash the changes and run the tests again", err)
		}
		if len(server.paths()) != 0 {
			t.Errorf("requests = %v; want none", server.paths())
		}
	})

	t.Run("a commit other than HEAD is sent despite uncommitted changes (guard)", func(t *testing.T) {
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, dirty.dir, dirty.commits[3])
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		if received := server.lastCapture(); received == nil || received["sha"] != dirty.commits[3] {
			t.Errorf("sent capture = %v; want sha %s", received, dirty.commits[3])
		}
	})

	t.Run("an upper-case commit id is sent in lower case", func(t *testing.T) {
		server := newChainServer(t, nil)
		head := merge.commits[len(merge.commits)-1]
		stderr, err := runRecord(t, server, merge.dir, strings.ToUpper(head))
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		received := server.lastCapture()
		if received == nil || received["sha"] != head {
			t.Errorf("sent sha = %v; want %s in lower case", received["sha"], head)
		}
		if ids, _ := sentChain(t, received); len(ids) == 0 || ids[0] != head {
			t.Errorf("sent chain starts with %v; want %s", ids, head)
		}
	})

	t.Run("outside a git checkout the file is sent as written without a chain (guard)", func(t *testing.T) {
		plain := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
		server := newChainServer(t, nil)
		if _, err := runRecord(t, server, plain, "abc123"); err != nil {
			t.Fatalf("modernization record: %v", err)
		}
		received := server.lastCapture()
		if received == nil || received["sha"] != "abc123" {
			t.Errorf("sent capture = %v; want sha abc123 as written", received)
		}
		if _, present := received["commit_chain"]; present {
			t.Errorf("a capture from outside git carries commit_chain: %v", received["commit_chain"])
		}
		if want := []string{"POST /api/compliance/releases/17/modernization/test-runs"}; !reflect.DeepEqual(server.paths(), want) {
			t.Errorf("requests = %v; want the capture alone", server.paths())
		}
	})

	t.Run("outside a git checkout the command says the run carries no chain", func(t *testing.T) {
		plain := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(plain))
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, plain, "abc123")
		if err != nil {
			t.Fatalf("modernization record: %v", err)
		}
		if !strings.Contains(stderr, "carries no commit chain") {
			t.Errorf("stderr does not say the run carries no commit chain:\n%s", stderr)
		}
	})

	t.Run("a checkout that does not hold the commit sends the file without a chain and says so", func(t *testing.T) {
		const elsewhere = "0123456789abcdef0123456789abcdef01234567"
		server := newChainServer(t, nil)
		stderr, err := runRecord(t, server, merge.dir, elsewhere)
		if err != nil {
			t.Fatalf("modernization record: %v\n%s", err, stderr)
		}
		received := server.lastCapture()
		if received == nil || received["sha"] != elsewhere {
			t.Errorf("sent capture = %v; want sha %s as written", received, elsewhere)
		}
		if _, present := received["commit_chain"]; present {
			t.Errorf("a capture of a commit the checkout does not hold carries commit_chain: %v", received["commit_chain"])
		}
		if !strings.Contains(stderr, "carries no commit chain") {
			t.Errorf("stderr does not say the run carries no commit chain:\n%s", stderr)
		}
	})

	t.Run("the server's invalid_commit_chain refusal is printed (guard)", func(t *testing.T) {
		server := newChainServer(t, nil)
		server.refusal = "invalid_commit_chain"
		_, err := runRecord(t, server, merge.dir, merge.commits[len(merge.commits)-1])
		if err == nil || !strings.Contains(err.Error(), "invalid_commit_chain") {
			t.Errorf("error = %v; want the server's invalid_commit_chain", err)
		}
	})

	t.Run("the help describes the chain, its limits and its refusals", func(t *testing.T) {
		record, _, err := newModernizationCommand(nil).Find([]string{"record"})
		if err != nil {
			t.Fatal(err)
		}
		help := plainText(record.Long)
		for _, phrase := range []string{
			"commit chain",
			"git rev-list --topo-order",
			"2,000 commits",
			"8,000,000 bytes",
			"full 40- or 64-character",
			"lower case",
			"uncommitted changes to tracked files",
			"commit or stash them and run the tests again",
			"carries no commit chain",
			"invalid_commit_chain",
			"cut",
		} {
			if !strings.Contains(help, phrase) {
				t.Errorf("modernization record --help lacks %q", phrase)
			}
		}
		if usage := record.Flags().Lookup("release-id").Usage; !strings.Contains(plainText(usage), "stop commits") {
			t.Errorf("--release-id help does not say the Release names the walk's stop commits: %q", usage)
		}
	})

	t.Run("the CLI guide's section on modernization test history describes the chain", func(t *testing.T) {
		for _, phrase := range []string{
			"commit chain",
			"commit_chain",
			"git rev-list --topo-order",
			"2,000 commits",
			"8,000,000 bytes",
			"full 40- or 64-character",
			"lower case",
			"uncommitted changes to tracked files",
			"full-depth checkout",
			"carries no commit chain",
			"invalid_commit_chain",
			"cut",
		} {
			if !strings.Contains(guide, phrase) {
				t.Errorf("the CLI guide's section on modernization test history lacks %q", phrase)
			}
		}
	})

	t.Run("the CLI reference has the chain's refusal rows", func(t *testing.T) {
		for code, phrases := range map[string][]string{
			"invalid_commit_chain":    {"modernization record", "nothing was written"},
			"give the full commit id": {"modernization record", "nothing was sent"},
			"commit or stash them":    {"modernization record", "HEAD", "nothing was sent"},
		} {
			row := referenceRow(code)
			if row == "" {
				t.Errorf("reference-loop.md has no refusal row for `%s`", code)
				continue
			}
			for _, phrase := range phrases {
				if !strings.Contains(row, phrase) {
					t.Errorf("the `%s` row lacks %q: %s", code, phrase, row)
				}
			}
		}
	})
}
