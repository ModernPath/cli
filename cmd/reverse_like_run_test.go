package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// SR-RDD-ONBOARD-027: reverse-engineer inventory --like-run repeats the area
// an earlier run covered, derived from its authorized files and out-of-scope
// exclusions, and names on the error stream every file that differs.

// likeRunServer serves earlier runs by id and answers any other id as the
// server does.
type likeRunServer struct {
	runs map[string][]reverseRepository
	gets int
}

func (s *likeRunServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	prefix := "/api/v1/systems/4/reverse-engineering/runs/"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, prefix) {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
			return
		}
		s.gets++
		id := strings.TrimPrefix(r.URL.Path, prefix)
		repositories, ok := s.runs[id]
		if !ok {
			w.WriteHeader(404)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"id": id, "key": "earlier", "mode": "baseline",
			"authorization": map[string]any{"key": "earlier", "mode": "baseline", "repositories": repositories, "documents": []any{}, "document_snapshots": []any{}},
			"groups":        []any{},
		}})
	}))
	t.Cleanup(server.Close)
	return server
}

// likeRunInventory runs inventory with the given arguments and returns the
// output stream and everything written to the error stream, warnings included.
func likeRunInventory(t *testing.T, server *httptest.Server, args ...string) (string, string, error) {
	t.Helper()
	var warnings bytes.Buffer
	savedErr, savedNoColor := color.Error, color.NoColor
	color.Error, color.NoColor = &warnings, true
	defer func() { color.Error, color.NoColor = savedErr, savedNoColor }()
	out, errOut, err := reCommandStreams(t, server, "", append([]string{"inventory"}, args...)...)
	return out, warnings.String() + errOut, err
}

// likeRunStrict decodes an inventory the way authorize --inventory does: an
// unknown key is refused.
func likeRunStrict(t *testing.T, out string) reverseInventory {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(out))
	decoder.DisallowUnknownFields()
	var inventory reverseInventory
	if err := decoder.Decode(&inventory); err != nil {
		t.Fatalf("the output stream is not exactly an inventory: %v: %s", err, out)
	}
	return inventory
}

// likeRunAuthorizes runs authorize with the printed inventory saved unchanged.
func likeRunAuthorizes(t *testing.T, out string) {
	t.Helper()
	state := &authorizeServer{}
	server := state.start(t)
	inventory := authorizeWrite(t, "inventory.json", out)
	preflight := authorizeWrite(t, "preflight.json", authorizePreflight)
	result, err := reCommand(t, server, "", "authorize", "--inventory", inventory, "--preflight", preflight,
		"--mode", "baseline", "--source", "USER:2026-10-07:the area again", "--key", "area-again", "--documents", "none")
	if err != nil || state.requests != 1 {
		t.Fatalf("authorize did not accept the inventory: %v requests=%d: %s", err, state.requests, result)
	}
	raw, _ := json.Marshal(state.body["repositories"])
	var sent []reverseRepository
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	if printed := likeRunStrict(t, out).Repositories; !reflect.DeepEqual(sent, printed) {
		t.Fatalf("authorize sent other repositories than the inventory printed:\n got %+v\nwant %+v", sent, printed)
	}
}

func likeRunFixture(t *testing.T, extra map[string]string) string {
	t.Helper()
	files := map[string]string{
		"pkg/a.txt": "alpha", "pkg/sub/b.txt": "beta", "pkg/sub/deep/c.txt": "gamma",
		"docs/guide.md": "guide", "other/d.txt": "delta", "other/deep/e.txt": "epsilon",
		"top.txt": "top", "readme.txt": "readme",
	}
	for path, body := range extra {
		files[path] = body
	}
	return scopeRepo(t, files)
}

// likeRunEarlier is the repository entry an earlier run authorized: the
// inventory of the named paths, as authorize sends it.
func likeRunEarlier(t *testing.T, root string, paths ...string) reverseRepository {
	t.Helper()
	args := []string{"--repository", "repo=" + root}
	for _, path := range paths {
		args = append(args, "--path", "repo="+path)
	}
	inventory, out, err := scopeInventory(t, args...)
	if err != nil {
		t.Fatalf("the earlier run's inventory refused: %v: %s", err, out)
	}
	return inventory.Repositories[0]
}

func likeRunContains(t *testing.T, text string, parts ...string) {
	t.Helper()
	for _, part := range parts {
		if !strings.Contains(text, part) {
			t.Fatalf("the error stream must say %q:\n%s", part, text)
		}
	}
}

// AC1, AC3: with the tree unchanged, the derived area holds the same files as
// the named paths of the earlier run, and the printed inventory is the one the
// derived paths give as --path: same shape, limits and exclusions.
func TestSRRDDONBOARD027LikeRunDerivesTheNamedArea(t *testing.T) {
	for _, tc := range []struct {
		name    string
		named   []string
		derived []string
	}{
		{"nested folders", []string{"pkg/sub", "docs"}, []string{"docs", "pkg/sub"}},
		{"root file and a folder", []string{"top.txt", "pkg"}, []string{"pkg", "top.txt"}},
		{"one file of a folder", []string{"pkg/sub/b.txt"}, []string{"pkg/sub/b.txt"}},
		{"the only file of a folder derives the folder", []string{"docs/guide.md"}, []string{"docs"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := likeRunFixture(t, nil)
			earlier := likeRunEarlier(t, root, tc.named...)
			server := (&likeRunServer{runs: map[string][]reverseRepository{"run-1": {earlier}}}).start(t)
			out, stderr, err := likeRunInventory(t, server, "--repository", "repo="+root, "--like-run", "run-1")
			if err != nil {
				t.Fatalf("inventory --like-run refused: %v: %s", err, stderr)
			}
			inventory := likeRunStrict(t, out)
			if !reflect.DeepEqual(inventory.Repositories[0].Files, earlier.Files) {
				t.Fatalf("the derived area does not hold the earlier run's files:\n got %v\nwant %v", scopePaths(inventory.Repositories[0].Files), scopePaths(earlier.Files))
			}
			want, _, err := scopeInventory(t, append([]string{"--repository", "repo=" + root}, likeRunPathArgs(tc.derived)...)...)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(inventory, want) {
				t.Fatalf("the inventory is not the one the derived paths give as --path:\n got %+v\nwant %+v", inventory, want)
			}
			likeRunContains(t, stderr, "repository repo", strings.Join(tc.derived, ", "), "run-1", "no file differs")
		})
	}
	root := likeRunFixture(t, nil)
	server := (&likeRunServer{runs: map[string][]reverseRepository{"run-1": {likeRunEarlier(t, root, "pkg/sub", "docs")}}}).start(t)
	out, stderr, err := likeRunInventory(t, server, "--repository", "repo="+root, "--like-run", "run-1")
	if err != nil {
		t.Fatalf("inventory --like-run refused: %v: %s", err, stderr)
	}
	likeRunAuthorizes(t, out)
}

func likeRunPathArgs(paths []string) []string {
	args := []string{}
	for _, path := range paths {
		args = append(args, "--path", "repo="+path)
	}
	return args
}

// AC2, AC3 and the journey of an area whose files changed: every changed,
// missing and new file of the area is named with its state and counted on the
// error stream; a file outside the area is not; the output stream holds only
// the inventory, which authorize accepts.
func TestSRRDDONBOARD027LikeRunNamesChangedMissingAndNewFiles(t *testing.T) {
	root := likeRunFixture(t, map[string]string{"pkg/gone.txt": "to be deleted"})
	earlier := likeRunEarlier(t, root, "pkg", "top.txt")
	scopeWrite(t, root, map[string]string{
		"pkg/a.txt":         "alpha changed",
		"pkg/sub/b.txt":     "beta changed",
		"pkg/new.txt":       "new in the area",
		"other/outside.txt": "new outside the area",
	})
	// The deletions are staged: a tracked file deleted only in the working
	// tree is still listed by Git and refuses a dirty inventory.
	gitRun(t, root, "rm", "-q", "pkg/gone.txt", "top.txt")
	server := (&likeRunServer{runs: map[string][]reverseRepository{"run-1": {earlier}}}).start(t)
	out, stderr, err := likeRunInventory(t, server, "--repository", "repo="+root, "--like-run", "run-1")
	if err != nil {
		t.Fatalf("inventory --like-run refused: %v: %s", err, stderr)
	}
	inventory := likeRunStrict(t, out)
	if got := scopePaths(inventory.Repositories[0].Files); !reflect.DeepEqual(got, []string{"pkg/a.txt", "pkg/new.txt", "pkg/sub/b.txt", "pkg/sub/deep/c.txt"}) {
		t.Fatalf("the inventory does not hold the area as it is now: %v", got)
	}
	likeRunContains(t, stderr,
		"5 files differ from run run-1: 2 changed, 2 missing, 1 new",
		"changed pkg/a.txt", "changed pkg/sub/b.txt", "missing pkg/gone.txt", "missing top.txt", "new pkg/new.txt")
	for _, unlisted := range []string{"other/outside.txt", "pkg/sub/deep/c.txt"} {
		if strings.Contains(stderr, unlisted) {
			t.Fatalf("%s is outside the area or unchanged and must not be named:\n%s", unlisted, stderr)
		}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, []string{"byte_count", "file_count", "repositories"}) {
		t.Fatalf("the inventory JSON gained a key: %v", names)
	}
	likeRunAuthorizes(t, out)
}

// AC5: a run without out-of-scope exclusions derives the whole repository,
// and the comparison covers every file; the earlier run's dirty flag is not
// compared.
func TestSRRDDONBOARD027LikeRunOfWholeRepositoryCoversEveryFile(t *testing.T) {
	root := likeRunFixture(t, nil)
	scopeWrite(t, root, map[string]string{"scratch.txt": "untracked when the run was made"})
	earlier := likeRunEarlier(t, root)
	if !earlier.Dirty {
		t.Fatal("fixture: the earlier run must be recorded dirty")
	}
	scopeWrite(t, root, map[string]string{"readme.txt": "readme changed", "other/deep/e.txt": "epsilon changed", "added.txt": "added"})
	scopeCommit(t, root, "the repository moves on")
	server := (&likeRunServer{runs: map[string][]reverseRepository{"run-1": {earlier}}}).start(t)
	out, stderr, err := likeRunInventory(t, server, "--repository", "repo="+root, "--like-run", "run-1")
	if err != nil {
		t.Fatalf("inventory --like-run refused: %v: %s", err, stderr)
	}
	want, _, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatal(err)
	}
	if inventory := likeRunStrict(t, out); !reflect.DeepEqual(inventory, want) || inventory.Repositories[0].Dirty {
		t.Fatalf("a whole-repository run must derive the whole repository:\n got %+v\nwant %+v", inventory, want)
	}
	likeRunContains(t, stderr, "the whole repository",
		"3 files differ from run run-1: 2 changed, 0 missing, 1 new",
		"changed readme.txt", "changed other/deep/e.txt", "new added.txt")
	if strings.Contains(stderr, "scratch.txt") || strings.Contains(stderr, "dirty") {
		t.Fatalf("the earlier run's dirty state must not be compared:\n%s", stderr)
	}
}

// AC4: --like-run refuses with a message naming the problem, and prints no
// inventory, for --path, an unknown run, a run repository that is not
// declared, a declared repository the run did not cover, a directory that is
// not a Git repository, and a missing --repository.
func TestSRRDDONBOARD027LikeRunRefusals(t *testing.T) {
	root := likeRunFixture(t, nil)
	earlier := likeRunEarlier(t, root, "pkg")
	earlier.Key = "catalog"
	plain := t.TempDir()
	scopeWrite(t, plain, map[string]string{"pkg/a.txt": "alpha"})
	second := likeRunFixture(t, nil)
	for _, tc := range []struct {
		name  string
		args  []string
		gets  int
		named []string
	}{
		{"with --path", []string{"--repository", "catalog=" + root, "--path", "catalog=pkg", "--like-run", "run-1"}, 0, []string{"--like-run", "--path"}},
		{"unknown run", []string{"--repository", "catalog=" + root, "--like-run", "nope"}, 1, []string{"could not read run nope"}},
		{"run repository not declared", []string{"--repository", "billing=" + root, "--like-run", "run-1"}, 1, []string{`"catalog"`, "--repository catalog="}},
		{"declared repository not in the run", []string{"--repository", "catalog=" + root, "--repository", "billing=" + second, "--like-run", "run-1"}, 1, []string{`"billing"`, "run-1"}},
		{"not a Git repository", []string{"--repository", "catalog=" + plain, "--like-run", "run-1"}, 1, []string{"catalog", "not a Git repository"}},
		{"missing --repository", []string{"--like-run", "run-1"}, 0, []string{`"repository"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &likeRunServer{runs: map[string][]reverseRepository{"run-1": {earlier}}}
			out, stderr, err := likeRunInventory(t, state.start(t), tc.args...)
			if err == nil {
				t.Fatalf("inventory --like-run was accepted: %s", out)
			}
			for _, named := range tc.named {
				if !strings.Contains(err.Error(), named) {
					t.Fatalf("the refusal must say %q: %v", named, err)
				}
			}
			if strings.Contains(out, `"repositories"`) {
				t.Fatalf("an inventory was printed with the refusal: %s", out)
			}
			if state.gets != tc.gets {
				t.Fatalf("the run was read %d times, want %d: %s", state.gets, tc.gets, stderr)
			}
		})
	}
}

// With the identity of a committed file, an area inventoried in a checkout
// that converts line endings and again in one that does not differs in no
// file.
func TestSRRDDONBOARD027LikeRunAcrossLineEndingConversionFindsNoDifference(t *testing.T) {
	files := map[string]string{"pkg/a.txt": "alpha\nbeta\n", "pkg/sub/b.txt": "gamma\n", "other/c.txt": "delta\n"}
	remote, converting, _ := committedFixture(t, files)
	earlier := likeRunEarlier(t, converting, "pkg")
	plain := filepath.Join(t.TempDir(), "plain")
	gitRun(t, t.TempDir(), "clone", remote, plain)
	server := (&likeRunServer{runs: map[string][]reverseRepository{"run-1": {earlier}}}).start(t)
	out, stderr, err := likeRunInventory(t, server, "--repository", "repo="+plain, "--like-run", "run-1")
	if err != nil {
		t.Fatalf("inventory --like-run refused: %v: %s", err, stderr)
	}
	if !reflect.DeepEqual(likeRunStrict(t, out).Repositories[0], earlier) {
		t.Fatalf("the same commit must give the same inventory on both checkouts:\n got %+v\nwant %+v", likeRunStrict(t, out).Repositories[0], earlier)
	}
	likeRunContains(t, stderr, "repository repo: no file differs from run run-1")
}
