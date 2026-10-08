package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// SR-RDD-ONBOARD-038: a tracked file of a clean repository is identified by
// its committed content, so the same commit gives the same identity on every
// operating system. Line-ending conversion on checkout is simulated with
// core.autocrlf=true, as Git for Windows sets it by default.

// committedFiles is the inventory entry of each file as committed, in path order.
func committedFiles(files map[string]string) []reverseFile {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	out := make([]reverseFile, len(paths))
	for i, path := range paths {
		out[i] = reverseFile{path, reverseDigest([]byte(files[path])), int64(len(files[path]))}
	}
	return out
}

// committedUnder keeps the entries under one folder.
func committedUnder(files []reverseFile, folder string) []reverseFile {
	kept := []reverseFile{}
	for _, file := range files {
		if strings.HasPrefix(file.Path, folder+"/") {
			kept = append(kept, file)
		}
	}
	return kept
}

// committedFixture pushes the files, committed with LF line endings, to a bare
// remote and clones it with core.autocrlf=true. It returns the remote, the
// converting checkout and the files as committed.
func committedFixture(t *testing.T, files map[string]string) (string, string, []reverseFile) {
	t.Helper()
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	author := filepath.Join(t.TempDir(), "author")
	gitRun(t, t.TempDir(), "clone", remote, author)
	scopeWrite(t, author, files)
	scopeCommit(t, author, "committed with LF")
	gitRun(t, author, "push", "origin", "main")
	root := filepath.Join(t.TempDir(), "converting")
	gitRun(t, t.TempDir(), "clone", "-c", "core.autocrlf=true", remote, root)
	converted := false
	for path, body := range files {
		disk, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		converted = converted || (strings.Contains(body, "\n") && bytes.Contains(disk, []byte("\r\n")))
	}
	if !converted {
		t.Fatal("fixture: the checkout did not convert line endings")
	}
	if status := gitRun(t, root, "status", "--porcelain", "--untracked-files=normal"); status != "" {
		t.Fatalf("fixture: the converting checkout is not clean: %s", status)
	}
	return remote, root, committedFiles(files)
}

// committedCaptureServer serves one run's authorization and checks a capture
// the way the server does: exactly the authorized files, each with the
// authorized size and sha256 of its decoded bytes, under the authorized
// snapshot digest.
type committedCaptureServer struct {
	repository reverseRepository
	captures   int
	uploaded   map[string]string
}

func (s *committedCaptureServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/sync/contract":
			w.WriteHeader(404)
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/reverse-engineering/runs/recorded-run"):
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": "recorded-run", "authorization": map[string]any{"repositories": []reverseRepository{s.repository}},
			}})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/reverse-engineering/runs/recorded-run/sources"):
			var payload struct {
				Files []struct {
					Path    string `json:"path"`
					Content string `json:"content_base64"`
				} `json:"files"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			expected := map[string]reverseFile{}
			for _, file := range s.repository.Files {
				expected[file.Path] = file
			}
			uploaded := map[string]string{}
			refuse := len(payload.Files) != len(s.repository.Files) || reverseSnapshot(s.repository.Files) != s.repository.SnapshotDigest
			for _, file := range payload.Files {
				content, err := base64.StdEncoding.DecodeString(file.Content)
				want, known := expected[file.Path]
				_, repeated := uploaded[file.Path]
				if err != nil || !known || repeated || int64(len(content)) != want.Size || reverseDigest(content) != want.SHA256 {
					refuse = true
				}
				uploaded[file.Path] = string(content)
			}
			if refuse {
				w.WriteHeader(422)
				_, _ = w.Write([]byte(`{"error":"source_inventory_mismatch"}`))
				return
			}
			s.captures++
			s.uploaded = uploaded
			_, _ = w.Write([]byte(`{"data":{"id":"capture-1","state":"queued"}}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// AC1: a clean checkout with line-ending conversion and one without give the
// same inventory, and its hashes and sizes are those of the committed content.
func TestSRRDDONBOARD038CleanInventoryHashesCommittedContent(t *testing.T) {
	files := map[string]string{"pkg/a.txt": "alpha\nbeta\n", "pkg/sub/b.txt": "gamma\n", "top.txt": "top\n"}
	remote, root, committed := committedFixture(t, files)
	converting, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	repo := converting.Repositories[0]
	if repo.Dirty || repo.Revision != gitRun(t, root, "rev-parse", "HEAD") {
		t.Fatalf("the converting checkout must be inventoried clean at its commit: dirty=%v revision=%s", repo.Dirty, repo.Revision)
	}
	var size int64
	for _, file := range committed {
		size += file.Size
	}
	if !reflect.DeepEqual(repo.Files, committed) || repo.SnapshotDigest != reverseSnapshot(committed) || converting.Bytes != size {
		t.Fatalf("a clean checkout with line-ending conversion must hash the committed content:\n got %+v (%d bytes)\nwant %+v (%d bytes)", repo.Files, converting.Bytes, committed, size)
	}

	plainRoot := filepath.Join(t.TempDir(), "plain")
	gitRun(t, t.TempDir(), "clone", remote, plainRoot)
	plain, out, err := scopeInventory(t, "--repository", "repo="+plainRoot)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	if !reflect.DeepEqual(converting, plain) {
		t.Fatalf("checkouts of the same commit with and without conversion differ:\n got %+v\nwant %+v", converting, plain)
	}

	scoped, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("scoped inventory refused: %v: %s", err, out)
	}
	if want := committedUnder(committed, "pkg"); !reflect.DeepEqual(scoped.Repositories[0].Files, want) {
		t.Fatalf("a named path must hash the committed content too:\n got %+v\nwant %+v", scoped.Repositories[0].Files, want)
	}
}

// AC2: the capture of a run recorded clean sends the content committed at the
// recorded revision, whatever the working tree holds and wherever HEAD is,
// and the server's per-file and snapshot checks pass.
func TestSRRDDONBOARD038CaptureSendsTheRecordedRevision(t *testing.T) {
	files := map[string]string{"pkg/a.txt": "alpha\nbeta\n", "pkg/sub/b.txt": "gamma\n", "other/c.txt": "delta\n"}
	_, root, committed := committedFixture(t, files)
	recorded := reverseRepository{Key: "repo", Revision: gitRun(t, root, "rev-parse", "HEAD"), Dirty: false, Files: committed, SnapshotDigest: reverseSnapshot(committed)}
	state := &committedCaptureServer{repository: recorded}
	server := state.start(t)
	captures := func(step string) {
		t.Helper()
		out, err := reCommand(t, server, "", "capture-source", "--run", "recorded-run", "--repository", "repo", "--root", root)
		if err != nil {
			t.Fatalf("%s: the capture of a run recorded clean refused: %v: %s", step, err, out)
		}
		if !reflect.DeepEqual(state.uploaded, files) {
			t.Fatalf("%s: the capture did not send the content committed at the recorded revision:\n got %q\nwant %q", step, state.uploaded, files)
		}
	}

	scopeWrite(t, root, map[string]string{"pkg/new.txt": "untracked\n"})
	captures("an untracked file was added after the inventory")

	scopeWrite(t, root, map[string]string{"pkg/a.txt": "edited and committed\n", "other/c.txt": "moved on\n"})
	scopeCommit(t, root, "HEAD moves past the recorded revision")
	scopeWrite(t, root, map[string]string{"pkg/sub/b.txt": "edited, not committed\n"})
	captures("tracked files changed and HEAD moved after the inventory")

	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("bytes that live outside the repository"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "other", "c.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "other", "c.txt")); err != nil {
		t.Fatal(err)
	}
	captures("an authorized path became a link in the working tree")
	if state.captures != 3 {
		t.Fatalf("every capture must pass the server's checks: %d of 3", state.captures)
	}
}

// AC2: capture reads the recorded revision through Git, so a checkout that
// does not hold that commit refuses and names it, even when its files on disk
// match.
func TestSRRDDONBOARD038CaptureRefusesWhenTheRecordedRevisionIsMissing(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	inventoried := filepath.Join(t.TempDir(), "inventoried")
	gitRun(t, t.TempDir(), "clone", remote, inventoried)
	scopeWrite(t, inventoried, map[string]string{"pkg/a.txt": "alpha\n"})
	scopeCommit(t, inventoried, "pushed")
	gitRun(t, inventoried, "push", "origin", "main")
	scopeWrite(t, inventoried, map[string]string{"notes.txt": "local only\n"})
	scopeCommit(t, inventoried, "not pushed")
	inventory, out, err := scopeInventory(t, "--repository", "repo="+inventoried, "--path", "repo=pkg")
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	recorded := inventory.Repositories[0]
	other := filepath.Join(t.TempDir(), "other")
	gitRun(t, t.TempDir(), "clone", remote, other)
	state := &committedCaptureServer{repository: recorded}
	_, err = reCommand(t, state.start(t), "", "capture-source", "--run", "recorded-run", "--repository", "repo", "--root", other)
	if err == nil || !strings.Contains(err.Error(), recorded.Revision) {
		t.Fatalf("a checkout without the recorded revision must refuse and name it: %v", err)
	}
	if state.captures != 0 {
		t.Fatal("a capture was sent without the recorded revision")
	}
}

// AC2: the delivery observation of a clean checkout at the tested revision
// hashes the committed content, with and without a named run.
func TestSRRDDONBOARD038DeliveryObservationHashesCommittedContent(t *testing.T) {
	files := map[string]string{"pkg/a.txt": "alpha\nbeta\n", "other/c.txt": "delta\n"}
	_, root, committed := committedFixture(t, files)
	head := gitRun(t, root, "rev-parse", "HEAD")
	input := asBuiltDeliveryInput{Key: "observation", RepositoryKey: "catalog", Root: root, TestedRevision: head, SnapshotDigest: reverseSnapshot(committed)}
	report, err := collectAsBuiltDelivery(input)
	if err != nil {
		t.Fatalf("the whole-repository observation of a converting checkout refused: %v", err)
	}
	if report["snapshot_digest"] != input.SnapshotDigest || report["integrated_revision"] != head {
		t.Fatalf("the observation does not carry the committed snapshot: %v", report)
	}

	scope := committedUnder(committed, "pkg")
	authorized := reverseRepository{Key: "catalog", Revision: head, Files: scope, SnapshotDigest: reverseSnapshot(scope)}
	state := &scopeRunServer{repositories: []reverseRepository{authorized}}
	scoped := input
	scoped.SnapshotDigest = authorized.SnapshotDigest
	out, err := scopeDeliveryProof(t, state.start(t), scoped)
	if err != nil {
		t.Fatalf("the observation of a named run in a converting checkout refused: %v: %s", err, out)
	}
	if state.posts != 1 || state.report["snapshot_digest"] != authorized.SnapshotDigest {
		t.Fatalf("the scoped observation does not carry the committed snapshot: posts=%d report=%v", state.posts, state.report)
	}
}

// AC4: a file in scope with a content filter attribute, Git LFS included,
// refuses the inventory with a message naming the file and the attribute; a
// named path that leaves such files out is inventoried.
func TestSRRDDONBOARD038ContentFilterRefusesInventory(t *testing.T) {
	pointer := "version https://git-lfs.github.com/spec/v1\noid sha256:4d7a214614ab2935c943f9e0ff69d22eadbb8f32b1258daaa5e2ca24d17e2393\nsize 12345\n"
	root := scopeRepo(t, map[string]string{
		".gitattributes":   "*.bin filter=lfs diff=lfs merge=lfs -text\nvault/* filter=crypt\n",
		"assets/model.bin": pointer,
		"vault/token.txt":  "ciphertext",
		"src/app.go":       "package app\n",
	})
	for _, tc := range []struct {
		name  string
		path  string
		named []string
	}{
		{"whole repository", "", []string{"assets/model.bin", "filter=lfs", "vault/token.txt", "filter=crypt"}},
		{"named path with an LFS file", "repo=assets", []string{"assets/model.bin", "filter=lfs"}},
		{"named path with another content filter", "repo=vault", []string{"vault/token.txt", "filter=crypt"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{"--repository", "repo=" + root}
			if tc.path != "" {
				args = append(args, "--path", tc.path)
			}
			_, out, err := scopeInventory(t, args...)
			if err == nil {
				t.Fatal("a file with a content filter attribute was inventoried")
			}
			for _, named := range tc.named {
				if !strings.Contains(err.Error(), named) {
					t.Fatalf("the refusal must name %q: %v", named, err)
				}
			}
			if strings.Contains(out, `"repositories"`) {
				t.Fatalf("an inventory was printed with the refusal: %s", out)
			}
		})
	}
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root, "--path", "repo=src")
	if err != nil || !reflect.DeepEqual(scopePaths(inventory.Repositories[0].Files), []string{"src/app.go"}) {
		t.Fatalf("a named path without filtered files must be inventoried: %v: %s", err, out)
	}
}

// AC3: a repository inventoried dirty is hashed from disk, line endings as
// checked out, and so is its capture; in a run recorded clean, a path not
// tracked at the recorded revision is captured from disk.
func TestSRRDDONBOARD038DirtyRepositoryHashesDiskBytes(t *testing.T) {
	files := map[string]string{"pkg/a.txt": "alpha\nbeta\n", "top.txt": "top\n"}
	_, root, committed := committedFixture(t, files)
	scopeWrite(t, root, map[string]string{"pkg/new.txt": "untracked\n"})
	disk := map[string]string{}
	for _, path := range []string{"pkg/a.txt", "pkg/new.txt", "top.txt"} {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		disk[path] = string(content)
	}
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	repo := inventory.Repositories[0]
	if !repo.Dirty || !reflect.DeepEqual(repo.Files, committedFiles(disk)) {
		t.Fatalf("a dirty repository must be hashed from disk:\n got dirty=%v %+v\nwant %+v", repo.Dirty, repo.Files, committedFiles(disk))
	}
	state := &committedCaptureServer{repository: repo}
	if out, err := reCommand(t, state.start(t), "", "capture-source", "--run", "recorded-run", "--repository", "repo", "--root", root); err != nil || !reflect.DeepEqual(state.uploaded, disk) {
		t.Fatalf("the capture of a run recorded dirty must send the disk bytes: %v: %s\n got %q", err, out, state.uploaded)
	}

	untracked := append(append([]reverseFile{}, committed...), reverseFile{"pkg/new.txt", reverseDigest([]byte(disk["pkg/new.txt"])), int64(len(disk["pkg/new.txt"]))})
	clean := reverseRepository{Key: "repo", Revision: gitRun(t, root, "rev-parse", "HEAD"), Files: untracked, SnapshotDigest: reverseSnapshot(untracked)}
	state = &committedCaptureServer{repository: clean}
	want := map[string]string{"pkg/a.txt": files["pkg/a.txt"], "top.txt": files["top.txt"], "pkg/new.txt": disk["pkg/new.txt"]}
	if out, err := reCommand(t, state.start(t), "", "capture-source", "--run", "recorded-run", "--repository", "repo", "--root", root); err != nil || !reflect.DeepEqual(state.uploaded, want) {
		t.Fatalf("a path not tracked at the recorded revision must be captured from disk: %v: %s\n got %q\nwant %q", err, out, state.uploaded, want)
	}
}

// AC4: text, end-of-line and diff attributes, and an unset filter, do not
// refuse the inventory, and the files are hashed as committed.
func TestSRRDDONBOARD038TextEolAndDiffAttributesDoNotRefuse(t *testing.T) {
	files := map[string]string{
		".gitattributes": "*.md diff=markdown\n*.txt text eol=lf\n*.sh text eol=crlf\nplain/* -filter\n",
		"docs/guide.md":  "# Guide\n\nBody\n",
		"notes.txt":      "notes\n",
		"run.sh":         "echo run\n",
		"plain/data.csv": "a,b\n",
	}
	_, root, committed := committedFixture(t, files)
	inventory, out, err := scopeInventory(t, "--repository", "repo="+root)
	if err != nil {
		t.Fatalf("text, eol, diff or an unset filter refused the inventory: %v: %s", err, out)
	}
	if !reflect.DeepEqual(inventory.Repositories[0].Files, committed) {
		t.Fatalf("files with these attributes must be hashed as committed:\n got %+v\nwant %+v", inventory.Repositories[0].Files, committed)
	}
}

// A link committed to the repository is left out as a symbolic link also in a
// checkout that writes links as plain files, so both checkouts of the commit
// give the same inventory.
func TestSRRDDONBOARD038CommittedLinkIsLeftOutInACheckoutWithoutLinks(t *testing.T) {
	remote := filepath.Join(t.TempDir(), "origin.git")
	gitRun(t, t.TempDir(), "init", "--bare", "--initial-branch=main", remote)
	author := filepath.Join(t.TempDir(), "author")
	gitRun(t, t.TempDir(), "clone", remote, author)
	scopeWrite(t, author, map[string]string{"a.txt": "alpha\n"})
	if err := os.Symlink("a.txt", filepath.Join(author, "link.txt")); err != nil {
		t.Fatal(err)
	}
	scopeCommit(t, author, "a file and a link")
	gitRun(t, author, "push", "origin", "main")
	plain := filepath.Join(t.TempDir(), "plain")
	gitRun(t, t.TempDir(), "clone", "-c", "core.symlinks=false", remote, plain)
	if info, err := os.Lstat(filepath.Join(plain, "link.txt")); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("fixture: the checkout must write the link as a plain file: %v", err)
	}
	withLinks, out, err := scopeInventory(t, "--repository", "repo="+author)
	if err != nil {
		t.Fatalf("inventory refused: %v: %s", err, out)
	}
	withoutLinks, out, err := scopeInventory(t, "--repository", "repo="+plain)
	if err != nil {
		t.Fatalf("inventory of a checkout without links refused: %v: %s", err, out)
	}
	if !reflect.DeepEqual(withoutLinks, withLinks) || !scopeExcluded(withoutLinks.Repositories[0].Exclusions, "link.txt", "file", "symbolic link") {
		t.Fatalf("a committed link must be left out in both checkouts:\n got %+v\nwant %+v", withoutLinks, withLinks)
	}
}

// A Git reader that cannot start fails the read with its error; a later read
// does not use a half-started reader, and closing the content does not panic.
func TestSRRDDONBOARD038ReaderThatCannotStartFailsTheReadWithoutPanic(t *testing.T) {
	root := scopeRepo(t, map[string]string{"pkg/a.txt": "alpha\n"})
	head := gitRun(t, root, "rev-parse", "HEAD")
	content, err := openReverseContent(root, head, []string{"pkg/a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	// With no git on PATH the reader process cannot start.
	t.Setenv("PATH", t.TempDir())
	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := content.read("pkg/a.txt"); err == nil {
			t.Fatalf("read %d: a reader that cannot start must fail the read", attempt)
		}
	}
	content.close()
}
