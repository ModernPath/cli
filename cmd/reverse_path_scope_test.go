package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// scopeRepo commits the given files into a fresh Git repository and returns its root.
func scopeRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "--initial-branch=main")
	scopeWrite(t, root, files)
	scopeCommit(t, root, "fixture")
	return root
}

func scopeWrite(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for path, body := range files {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func scopeCommit(t *testing.T, root, message string) {
	t.Helper()
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-qm", message)
}

// scopeInventory runs the inventory command. On an error the output holds
// cobra's usage text, so callers assert on the JSON marker, not on emptiness.
func scopeInventory(t *testing.T, args ...string) (reverseInventory, string, error) {
	t.Helper()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	out, err := reCommand(t, server, "", append([]string{"inventory"}, args...)...)
	var inventory reverseInventory
	if err == nil {
		if decodeErr := json.Unmarshal([]byte(out), &inventory); decodeErr != nil {
			t.Fatalf("inventory output is not JSON: %v: %s", decodeErr, out)
		}
	}
	return inventory, out, err
}

func scopePaths(files []reverseFile) []string {
	paths := make([]string, len(files))
	for i, file := range files {
		paths[i] = file.Path
	}
	return paths
}

func scopeExcluded(exclusions []reverseExclusion, path, kind, reason string) bool {
	for _, exclusion := range exclusions {
		if exclusion.Path == path && exclusion.Kind == kind && strings.Contains(exclusion.Reason, reason) {
			return true
		}
	}
	return false
}

func TestSRRDDONBOARD011TrackedSymlinkIsExcludedNotFatal(t *testing.T) {
	root := scopeRepo(t, map[string]string{"a.txt": "alpha"})
	if err := os.Symlink("a.txt", filepath.Join(root, "link.txt")); err != nil {
		t.Fatal(err)
	}
	scopeCommit(t, root, "tracked link")
	inventory, err := buildReverseInventory([]string{"repo=" + root})
	if err != nil {
		t.Fatalf("a tracked symbolic link stopped the inventory: %v", err)
	}
	repo := inventory.Repositories[0]
	if !reflect.DeepEqual(scopePaths(repo.Files), []string{"a.txt"}) {
		t.Fatalf("the link entered the file list: %+v", repo.Files)
	}
	if !scopeExcluded(repo.Exclusions, "link.txt", "file", "symbolic link") {
		t.Fatalf("the link is not disclosed as an exclusion: %+v", repo.Exclusions)
	}
}

func TestSRRDDONBOARD011SymlinkTargetIsNeverRead(t *testing.T) {
	secret := []byte("bytes that live outside the repository")
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, secret, 0600); err != nil {
		t.Fatal(err)
	}
	root := scopeRepo(t, map[string]string{"a.txt": "alpha"})
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err != nil {
		t.Fatal(err)
	}
	scopeCommit(t, root, "tracked link to an outside file")
	inventory, err := buildReverseInventory([]string{"repo=" + root})
	if err != nil {
		t.Fatalf("a tracked symbolic link stopped the inventory: %v", err)
	}
	for _, file := range inventory.Repositories[0].Files {
		if file.Path == "escape.txt" || file.SHA256 == reverseDigest(secret) {
			t.Fatalf("the link target was read into the inventory: %+v", file)
		}
	}
	if inventory.Bytes != int64(len("alpha")) {
		t.Fatalf("byte count includes more than the regular file: %d", inventory.Bytes)
	}
}

// Regression guard: a capture that reads the working tree, the capture of a
// run recorded dirty, keeps refusing an authorized path that has become a
// link. A run recorded clean is captured from its revision through Git and
// never opens the working tree (SR-RDD-ONBOARD-038).
func TestSRRDDONBOARD011CaptureStillRefusesSymlink(t *testing.T) {
	root := scopeRepo(t, map[string]string{"a.txt": "alpha", "b.txt": "beta"})
	scopeWrite(t, root, map[string]string{"untracked.txt": "makes the repository dirty"})
	inventory, err := buildReverseInventory([]string{"repo=" + root})
	if err != nil {
		t.Fatal(err)
	}
	if !inventory.Repositories[0].Dirty {
		t.Fatal("fixture: the repository must be recorded dirty")
	}
	if err := os.Remove(filepath.Join(root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(root, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := reverseSourceBundle(inventory.Repositories[0], root); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("capture accepted an authorized path that is now a link: %v", err)
	}
}

func TestSRRDDONBOARD010PathScopeKeepsGitIdentity(t *testing.T) {
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "pkg/sub/b.txt": "beta", "other/c.txt": "gamma"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	if !reflect.DeepEqual(scopePaths(repo.Files), []string{"pkg/a.txt", "pkg/sub/b.txt"}) {
		t.Fatalf("scope does not hold exactly the files under the named path: %+v", repo.Files)
	}
	if repo.Revision != gitRun(t, root, "rev-parse", "HEAD") || len(repo.Revision) != 40 || repo.Dirty {
		t.Fatalf("scoped inventory lost the repository's Git identity: %s dirty=%v", repo.Revision, repo.Dirty)
	}
	scopeWrite(t, root, map[string]string{"other/uncommitted.txt": "new"})
	inventory, out, err = scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	repo = inventory.Repositories[0]
	if !repo.Dirty || len(repo.Files) != 2 {
		t.Fatalf("dirty state must be the whole repository's: dirty=%v files=%+v", repo.Dirty, repo.Files)
	}
}

func TestSRRDDONBOARD010ScopedDigestIsCaptureCompatible(t *testing.T) {
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "pkg/sub/b.txt": "beta", "other/c.txt": "gamma"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	want := reverseSnapshot([]reverseFile{
		{"pkg/a.txt", reverseDigest([]byte("alpha")), 5},
		{"pkg/sub/b.txt", reverseDigest([]byte("beta")), 4},
	})
	if repo.SnapshotDigest != want {
		t.Fatalf("snapshot digest does not cover exactly the included files: %s != %s", repo.SnapshotDigest, want)
	}
	files, err := reverseSourceBundle(repo, root)
	if err != nil || len(files) != 2 {
		t.Fatalf("capture does not accept the scoped authorization: %+v %v", files, err)
	}
	if inventory.Files != 2 || inventory.Bytes != 9 {
		t.Fatalf("counts include out-of-scope files: %d files %d bytes", inventory.Files, inventory.Bytes)
	}
}

func TestSRRDDONBOARD010OutOfScopeIsDisclosed(t *testing.T) {
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "pkg/sub/b.txt": "beta", "other/c.txt": "gamma", "other/deep/d.txt": "delta", "top.txt": "top"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	exclusions := inventory.Repositories[0].Exclusions
	if !scopeExcluded(exclusions, "other", "subtree", "outside the authorized scope") || !scopeExcluded(exclusions, "top.txt", "file", "outside the authorized scope") {
		t.Fatalf("left-out paths are not disclosed: %+v", exclusions)
	}
	if scopeExcluded(exclusions, "other/c.txt", "file", "") || scopeExcluded(exclusions, "pkg", "subtree", "") {
		t.Fatalf("exclusions are not collapsed to the top-most left-out directory: %+v", exclusions)
	}
	inventory, out, err = scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg/sub")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	exclusions = inventory.Repositories[0].Exclusions
	if !scopeExcluded(exclusions, "pkg/a.txt", "file", "outside the authorized scope") || !scopeExcluded(exclusions, "other", "subtree", "outside the authorized scope") {
		t.Fatalf("a left-out file beside included ones is not disclosed: %+v", exclusions)
	}
}

func TestSRRDDONBOARD010BadPathsRefuse(t *testing.T) {
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "other/c.txt": "gamma"})
	plain := t.TempDir()
	scopeWrite(t, plain, map[string]string{"sub/x.txt": "x"})
	for _, tc := range []struct{ name, path, named string }{
		{"absolute", "repo=/abs", "/abs"},
		{"parent", "repo=../x", "../x"},
		{"inner parent", "repo=pkg/../other", "pkg/../other"},
		{"trailing slash", "repo=pkg/", "pkg/"},
		{"no match", "repo=missing", "missing"},
		{"dash option", "repo=--full-name", "--full-name"},
		{"short dash option", "repo=-z", "-z"},
		{"literal star", "repo=*", "*"},
		{"undeclared key", "nope=pkg", "nope=pkg"},
		{"non-Git root", "plain=sub", "plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, out, err := scopeInventory(t, "--repository", "repo="+root, "--repository", "plain="+plain, "--path", tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.named) {
				t.Fatalf("refusal does not name %q: %v", tc.named, err)
			}
			if strings.Contains(out, `"repositories"`) {
				t.Fatalf("an inventory was printed with the refusal: %s", out)
			}
		})
	}
}

func TestSRRDDONBOARD010LimitsCountIncludedFilesOnly(t *testing.T) {
	big := strings.Repeat("x", 17_000_000)
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha", "other/big1.bin": big, "other/big2.bin": big})
	// The refusal without named paths keeps its delivered wording.
	if _, _, err := scopeInventory(t, "--repository", "repo="+root); err == nil || !strings.Contains(err.Error(), "exceeds the 32000000-byte source limit; narrow and disclose the scope") {
		t.Fatalf("control: the whole repository must exceed the source limit with the delivered message: %v", err)
	}
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("named paths under the limit were refused: %v: %s", err, out)
	}
	if inventory.Files != 1 || inventory.Bytes != 5 {
		t.Fatalf("limits counted out-of-scope files: %d files %d bytes", inventory.Files, inventory.Bytes)
	}
	// Named paths that are themselves over the limit say so.
	if _, _, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=other"); err == nil || !strings.Contains(err.Error(), "within the named paths; name fewer or smaller paths") {
		t.Fatalf("named paths over the limit must refuse and say so: %v", err)
	}
}

// SR-RDD-ONBOARD-025: the size refusal states the total it counted, the limit,
// the largest top-level folders with the repository root as one entry, and
// that tracked and untracked unignored files both count — with and without
// named paths, and without printing an inventory.
func TestSRRDDONBOARD025SizeRefusalStatesWhatItCounted(t *testing.T) {
	root := scopeRepo(t, map[string]string{
		"big/a.bin":    strings.Repeat("x", 20_000_000),
		"docs/c.txt":   strings.Repeat("d", 1_000),
		"f1/one.txt":   strings.Repeat("1", 300),
		"f2/two.txt":   strings.Repeat("2", 200),
		"f3/three.txt": strings.Repeat("3", 100),
		"root.txt":     strings.Repeat("r", 500),
	})
	// An untracked, unignored file counts like a tracked one.
	scopeWrite(t, root, map[string]string{"media/b.bin": strings.Repeat("m", 12_000_000)})
	ordered := func(t *testing.T, text string, parts ...string) {
		t.Helper()
		at := 0
		for _, part := range parts {
			i := strings.Index(text[at:], part)
			if i < 0 {
				t.Fatalf("the refusal must hold %q after position %d, in order:\n%s", part, at, text)
			}
			at += i + len(part)
		}
	}

	_, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err == nil {
		t.Fatal("the repository over the limit was accepted")
	}
	if strings.Contains(out, `"repositories"`) {
		t.Fatalf("an inventory was printed with the refusal: %s", out)
	}
	ordered(t, err.Error(),
		"exceeds the 32000000-byte source limit; narrow and disclose the scope",
		"total 32002100 bytes", "the limit is 32000000 bytes",
		"Tracked files and untracked files that are not ignored both count",
		"big (20000000 bytes)", "media (12000000 bytes)", "docs (1000 bytes)",
		"files in the repository root (500 bytes)", "f1 (300 bytes).")
	for _, unlisted := range []string{"f2 (", "f3 ("} {
		if strings.Contains(err.Error(), unlisted) {
			t.Fatalf("only the five largest entries are listed, got %q:\n%v", unlisted, err)
		}
	}

	_, out, err = scopeInventory(t, "--repository", "repo="+root, "--path", "repo=big", "--path", "repo=media", "--path", "repo=f1")
	if err == nil {
		t.Fatal("named paths over the limit were accepted")
	}
	if strings.Contains(out, `"repositories"`) {
		t.Fatalf("an inventory was printed with the refusal: %s", out)
	}
	ordered(t, err.Error(),
		"within the named paths; name fewer or smaller paths",
		"total 32000300 bytes", "the limit is 32000000 bytes",
		"Tracked files and untracked files that are not ignored both count",
		"big (20000000 bytes)", "media (12000000 bytes)", "f1 (300 bytes).")
	for _, unnamed := range []string{"docs (", "repository root (", "f2 ("} {
		if strings.Contains(err.Error(), unnamed) {
			t.Fatalf("a folder outside the named paths was counted (%q):\n%v", unnamed, err)
		}
	}
}

// SR-RDD-ONBOARD-024: in a Git root, tracked and unignored files with a .claude
// path component are left out of the file list, each disclosed as a file
// exclusion with the agent-metadata reason; the managed instruction files and
// editor folders stay in; the digest and the counts cover the included files.
func TestSRRDDONBOARD024GitInventoryLeavesClaudeOut(t *testing.T) {
	root := scopeRepo(t, map[string]string{
		"app.go":                          "package app",
		"AGENTS.md":                       "managed",
		"CLAUDE.md":                       "managed",
		".github/copilot-instructions.md": "managed",
		".vscode/settings.json":           "{}",
		".claude/settings.json":           "{\"hooks\":{}}",
		"pkg/lib.go":                      "package pkg",
		"pkg/.claude/agents/review.md":    "agent",
	})
	scopeWrite(t, root, map[string]string{".claude/skills/rdd/SKILL.md": "untracked skill"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	files := []reverseFile{
		{".github/copilot-instructions.md", reverseDigest([]byte("managed")), 7},
		{".vscode/settings.json", reverseDigest([]byte("{}")), 2},
		{"AGENTS.md", reverseDigest([]byte("managed")), 7},
		{"CLAUDE.md", reverseDigest([]byte("managed")), 7},
		{"app.go", reverseDigest([]byte("package app")), 11},
		{"pkg/lib.go", reverseDigest([]byte("package pkg")), 11},
	}
	if !reflect.DeepEqual(repo.Files, files) {
		t.Fatalf("the file list must leave .claude out and keep the rest:\n got %+v\nwant %+v", repo.Files, files)
	}
	for _, path := range []string{".claude/settings.json", ".claude/skills/rdd/SKILL.md", "pkg/.claude/agents/review.md"} {
		if !scopeExcluded(repo.Exclusions, path, "file", "agent workspace metadata") {
			t.Fatalf("%s is not disclosed as a file exclusion with the agent-metadata reason: %+v", path, repo.Exclusions)
		}
	}
	if repo.SnapshotDigest != reverseSnapshot(files) || inventory.Files != 6 || inventory.Bytes != 45 {
		t.Fatalf("digest and counts must cover the included files only: %s %d files %d bytes", repo.SnapshotDigest, inventory.Files, inventory.Bytes)
	}

	inventory, out, err = scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	repo = inventory.Repositories[0]
	if !reflect.DeepEqual(scopePaths(repo.Files), []string{"pkg/lib.go"}) || !scopeExcluded(repo.Exclusions, "pkg/.claude/agents/review.md", "file", "agent workspace metadata") {
		t.Fatalf("a .claude folder inside a named path must be left out and disclosed: %+v %+v", repo.Files, repo.Exclusions)
	}
}

// SR-RDD-ONBOARD-024: a named path that is or lies under a .claude folder
// refuses, names the path, says whether it is the folder or lies in one, and
// prints no inventory.
func TestSRRDDONBOARD024NamedClaudePathRefuses(t *testing.T) {
	root := scopeRepo(t, map[string]string{
		"app.go":                       "package app",
		".claude/settings.json":        "{}",
		".claude/skills/rdd/SKILL.md":  "skill",
		"pkg/.claude/agents/review.md": "agent",
	})
	for named, where := range map[string]string{
		".claude":               "is",
		"pkg/.claude":           "is",
		".claude/skills":        "is in",
		".claude/settings.json": "is in",
	} {
		t.Run(named, func(t *testing.T) {
			_, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo="+named)
			want := `path "` + named + `" ` + where + ` a .claude folder, which holds agent workspace metadata and is left out of every inventory`
			if err == nil || err.Error() != want {
				t.Fatalf("a named path under .claude must refuse and name the path:\n got %v\nwant %s", err, want)
			}
			if strings.Contains(out, `"repositories"`) {
				t.Fatalf("an inventory was printed with the refusal: %s", out)
			}
		})
	}
}

// A path scope takes what Git would track: untracked files are included, ignored
// files are not.
func TestSRRDDONBOARD010PathScopeTakesUntrackedAndSkipsIgnoredFiles(t *testing.T) {
	root := scopeRepo(t, map[string]string{".gitignore": "*.log\n", "pkg/a.txt": "alpha", "other/c.txt": "gamma"})
	scopeWrite(t, root, map[string]string{"pkg/new.txt": "untracked", "pkg/x.log": "ignored"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	if !reflect.DeepEqual(scopePaths(repo.Files), []string{"pkg/a.txt", "pkg/new.txt"}) || !repo.Dirty {
		t.Fatalf("the scope must hold its tracked and untracked files and no ignored one: %+v dirty=%v", repo.Files, repo.Dirty)
	}
	if !scopeExcluded(repo.Exclusions, "pkg/x.log", "file", "Git ignore policy") {
		t.Fatalf("an ignored file inside the named path is not disclosed: %+v", repo.Exclusions)
	}
}

// Ignored files under a directory the scope already leaves out are covered by
// that directory's exclusion. Listing each one could pass the server's limit
// on exclusions for a repository with many build outputs.
func TestSRRDDONBOARD010IgnoredFilesUnderLeftOutDirectoriesAreNotListed(t *testing.T) {
	root := scopeRepo(t, map[string]string{".gitignore": "*.o\n", "pkg/a.txt": "alpha", "other/c.txt": "gamma", "other/deep/d.txt": "delta"})
	scopeWrite(t, root, map[string]string{"pkg/a.o": "object", "other/c.o": "object", "other/deep/d.o": "object"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	exclusions := inventory.Repositories[0].Exclusions
	if !scopeExcluded(exclusions, "other", "subtree", "outside the authorized scope") || !scopeExcluded(exclusions, "pkg/a.o", "file", "Git ignore policy") {
		t.Fatalf("the left-out directory and the scope's ignored file must be disclosed: %+v", exclusions)
	}
	for _, exclusion := range exclusions {
		if strings.HasPrefix(exclusion.Path, "other/") {
			t.Fatalf("a path under an already left-out directory is listed again: %+v", exclusion)
		}
	}
}

// Regression guard for an inventory without named paths: the complete output
// of a fixed fixture, so a change to the listing or the read loop shows here.
func TestSRRDDONBOARD010WholeRepositoryInventoryIsUnchanged(t *testing.T) {
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "--initial-branch=main")
	scopeWrite(t, root, map[string]string{
		".gitignore": "ignored.log\nbuild/\n",
		".env":       "SECRET=1",
		"a.txt":      "alpha",
		"dir/b.txt":  "beta",
	})
	scopeCommit(t, root, "fixture")
	scopeWrite(t, root, map[string]string{"ignored.log": "noise", "build/out.bin": "artifact"})
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	files := []reverseFile{
		{".gitignore", reverseDigest([]byte("ignored.log\nbuild/\n")), 19},
		{"a.txt", reverseDigest([]byte("alpha")), 5},
		{"dir/b.txt", reverseDigest([]byte("beta")), 4},
	}
	want := reverseInventory{Files: 3, Bytes: 28, Repositories: []reverseRepository{{
		Key:            "repo",
		Revision:       gitRun(t, root, "rev-parse", "HEAD"),
		Dirty:          false,
		SnapshotDigest: reverseSnapshot(files),
		Files:          files,
		Exclusions: []reverseExclusion{
			{"build", "Git ignore policy", "subtree"},
			{"ignored.log", "Git ignore policy", "file"},
			{".env", "private or workspace metadata", "file"},
		},
	}}}
	if !reflect.DeepEqual(inventory, want) {
		t.Fatalf("whole-repository inventory changed:\n got %+v\nwant %+v", inventory, want)
	}
}
