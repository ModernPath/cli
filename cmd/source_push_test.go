// REQ-SYS-211 (EPIC-ANALYSIS-009): `modernpath source push` re-packs the
// working directory with the import filters and uploads it to the bound
// system's upload repository, carrying the customer's git revision as
// metadata. The server computes the revision and reports the result.
package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

// Response fixture used to make assertions about server-reported revisions.
var parityFixture = map[string]string{
	"README.md":            "# App\n",
	"docs/nested/ärger.md": "ü\n",
	"lib/app.ex":           "defmodule App do\n  def hello, do: :v1\nend\n",
}

const parityDigest = "1927f502c38d5f207b565d5e7594c37725a8505d4b3aa3f6ac9f3aa73f92c2bc"

func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, contents := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

type pushStub struct {
	server     *httptest.Server
	repository map[string]interface{}
	system     map[string]interface{}
	response   map[string]interface{}
	status     int
	posts      []*http.Request
	zipEntries []string
	fields     map[string]string
	jsonBody   map[string]interface{}
}

func newPushStub(t *testing.T) *pushStub {
	t.Helper()
	stub := &pushStub{status: http.StatusOK, fields: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/systems/42", func(w http.ResponseWriter, r *http.Request) {
		// The system read is not wrapped in `data`; the repository read is.
		_ = json.NewEncoder(w).Encode(stub.system)
	})
	mux.HandleFunc("/api/systems/42/repositories/7", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"data": stub.repository})
	})
	mux.HandleFunc("/api/systems/42/repositories/7/source-archive", func(w http.ResponseWriter, r *http.Request) {
		stub.posts = append(stub.posts, r)
		ct := r.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(ct, "multipart/form-data"):
			if err := r.ParseMultipartForm(32 << 20); err != nil {
				t.Fatalf("multipart: %v", err)
			}
			for key, values := range r.MultipartForm.Value {
				stub.fields[key] = values[0]
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatalf("file part: %v", err)
			}
			data, _ := io.ReadAll(file)
			reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("zip: %v", err)
			}
			stub.zipEntries = nil
			for _, entry := range reader.File {
				stub.zipEntries = append(stub.zipEntries, entry.Name)
			}
		default:
			_ = json.NewDecoder(r.Body).Decode(&stub.jsonBody)
		}
		w.WriteHeader(stub.status)
		_ = json.NewEncoder(w).Encode(stub.response)
	})
	stub.server = httptest.NewServer(mux)
	t.Cleanup(stub.server.Close)
	return stub
}

func (s *pushStub) config(repositoryID int) *config.Config {
	return &config.Config{APIURL: s.server.URL, SystemID: 42, RepositoryID: repositoryID}
}

func uploadRepository(revision, trigger, refreshStatus string) map[string]interface{} {
	repo := map[string]interface{}{
		"id":              7,
		"name":            "Pushed Project",
		"provider":        "local",
		"repository_url":  nil,
		"source_revision": revision,
		"source_metadata": map[string]interface{}{"trigger": trigger},
	}
	if refreshStatus != "" {
		repo["source_refresh"] = map[string]interface{}{"status": refreshStatus, "session_id": "push:1"}
	} else {
		repo["source_refresh"] = nil
	}
	return repo
}

func TestSourcePushUploadsAChangedTreeWithGitMetadata(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	gitCommitFixture(t, dir)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository("0000000000000000000000000000000000000000000000000000000000000000", "import", "")
	stub.response = map[string]interface{}{
		"result": "superseded", "revision": parityDigest,
		"previous_revision": "0000000000000000000000000000000000000000000000000000000000000000",
		"refresh_job_id":    5,
	}

	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100}, &out); err != nil {
		t.Fatalf("push: %v\n%s", err, out.String())
	}

	if len(stub.posts) != 1 {
		t.Fatalf("expected one upload, got %d", len(stub.posts))
	}
	want := []string{"README.md", "docs/nested/ärger.md", "lib/app.ex"}
	if strings.Join(sortedStrings(stub.zipEntries), ",") != strings.Join(want, ",") {
		t.Fatalf("zip entries %v, want %v", stub.zipEntries, want)
	}
	if len(stub.fields["git_head"]) != 40 {
		t.Fatalf("git_head must be the checkout's HEAD, got %q", stub.fields["git_head"])
	}
	if stub.fields["git_branch"] != "main" {
		t.Fatalf("git_branch %q, want main", stub.fields["git_branch"])
	}
	if stub.fields["git_dirty"] != "false" {
		t.Fatalf("git_dirty %q, want false", stub.fields["git_dirty"])
	}
	if stub.fields["captured_at"] == "" {
		t.Fatal("captured_at must be sent")
	}
	if !strings.Contains(out.String(), "pushed "+parityDigest[:12]) || !strings.Contains(out.String(), "job 5") {
		t.Fatalf("output must name the revision and the refresh job:\n%s", out.String())
	}
}

func TestSourcePushOmitsGitMetadataOutsideACheckout(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository("old", "push", "completed")
	stub.response = map[string]interface{}{"result": "superseded", "revision": parityDigest, "previous_revision": "old", "refresh_job_id": 6}

	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	for _, key := range []string{"git_head", "git_branch", "git_dirty"} {
		if _, sent := stub.fields[key]; sent {
			t.Fatalf("%s must be omitted outside a git checkout", key)
		}
	}
}

func TestSourcePushAlwaysUploadsAndLetsTheServerDetermineWhetherSourceChanged(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository(parityDigest, "push", "completed")
	stub.response = map[string]interface{}{"result": "unchanged", "revision": parityDigest}

	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(stub.posts) != 1 {
		t.Fatalf("the selected archive must be uploaded for server-side comparison, got %d posts", len(stub.posts))
	}
	if len(stub.zipEntries) != len(parityFixture) {
		t.Fatalf("upload must contain selected source files, got %v", stub.zipEntries)
	}
	if !strings.Contains(out.String(), "unchanged at "+parityDigest[:12]) {
		t.Fatalf("output must say unchanged:\n%s", out.String())
	}

}

func TestSourcePushJSONWritesOnlyTheServerResponseToStdout(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository("old", "push", "completed")
	stub.response = map[string]interface{}{"result": "superseded", "revision": "server-revision", "refresh_job_id": 13}

	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100, JSON: true}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("stdout must contain JSON only: %q (%v)", out.String(), err)
	}
	if response["revision"] != "server-revision" || response["result"] != "superseded" {
		t.Fatalf("expected the server response, got %v", response)
	}
	if len(stub.posts) != 1 || len(stub.zipEntries) != len(parityFixture) {
		t.Fatalf("JSON mode must still upload selected files, posts=%d entries=%v", len(stub.posts), stub.zipEntries)
	}
}

func TestSourcePushUploadsEvenWhenTheServerNeedsToRetryAnUnfinishedRefresh(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository(parityDigest, "push", "stopped")
	stub.response = map[string]interface{}{"result": "superseded", "revision": parityDigest, "refresh_job_id": 9}

	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(stub.posts) != 1 {
		t.Fatalf("expected one retry request, got %d", len(stub.posts))
	}
	if len(stub.zipEntries) != len(parityFixture) {
		t.Fatalf("the archive must be uploaded, got entries %v", stub.zipEntries)
	}
	if !strings.Contains(out.String(), "pushed "+parityDigest[:12]+" (previous none); refresh queued (job 9)") {
		t.Fatalf("output must say the refresh was re-queued:\n%s", out.String())
	}
}

func TestSourcePushRefusalExitsNonZeroWithTheServersCodeAndSince(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.repository = uploadRepository("old", "push", "completed")
	stub.status = http.StatusConflict
	stub.response = map[string]interface{}{
		"error":      "source_active",
		"message":    "The repository is being analysed",
		"busy_since": "2026-09-26T09:00:00Z",
	}

	var out bytes.Buffer
	err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100}, &out)
	if err == nil {
		t.Fatal("a server refusal must fail the command")
	}
	for _, want := range []string{"source_active", "The repository is being analysed", "busy since 2026-09-26T09:00:00Z"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must contain %q", err.Error(), want)
		}
	}
}

func TestSourcePushResolvesTheSingleUploadRepositoryWhenTheConfigPredatesIt(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.system = map[string]interface{}{
		"id": 42,
		"repositories": []map[string]interface{}{
			{"id": 3, "name": "linked", "provider": "github", "repository_url": "https://github.com/x/y.git"},
			{"id": 7, "name": "upload", "provider": "local", "repository_url": nil},
		},
	}
	stub.repository = uploadRepository("old", "push", "completed")
	stub.response = map[string]interface{}{"result": "superseded", "revision": parityDigest, "previous_revision": "old", "refresh_job_id": 11}

	var out bytes.Buffer
	if err := sourcePush(stub.config(0), sourcePushOptions{MaxSizeMB: 100}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(stub.posts) != 1 {
		t.Fatalf("expected one upload to repository 7, got %d", len(stub.posts))
	}
}

func TestSourcePushRefusesBeforePackingWithoutAnUploadRepository(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, parityFixture)
	t.Chdir(dir)

	stub := newPushStub(t)
	stub.system = map[string]interface{}{
		"id": 42,
		"repositories": []map[string]interface{}{
			{"id": 3, "name": "linked", "provider": "github", "repository_url": "https://github.com/x/y.git"},
		},
	}

	var out bytes.Buffer
	err := sourcePush(stub.config(0), sourcePushOptions{MaxSizeMB: 100}, &out)
	if err == nil {
		t.Fatal("a system without an upload repository must be refused")
	}
	if !strings.Contains(err.Error(), "git provider") {
		t.Fatalf("the refusal names the provider refresh path, got %q", err.Error())
	}
	if len(stub.posts) != 0 {
		t.Fatal("nothing may be posted")
	}
}

// AC6: `import --local` records the created repository id by merging into
// <cwd>/.modernpath/config.json, preserving the file's other fields.
func TestImportSavesTheRepositoryIdIntoTheWorkingDirectoryConfig(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"api_url":"http://api.test","auto_sync":true,"current_release":"v1"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "config.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := handleImportSuccess("http://api.test", 42, "Pushed Project", "pushed-project", 7); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]interface{}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["repository_id"] != float64(7) || saved["system_id"] != float64(42) {
		t.Fatalf("repository and system ids must be saved, got %v", saved)
	}
	if saved["auto_sync"] != true || saved["current_release"] != "v1" {
		t.Fatalf("the file's other fields must be preserved, got %v", saved)
	}
}

// M1 (PR #669 review): the push packs the bound checkout, never merely the
// working directory — a push from a subdirectory would otherwise replace the
// repository's whole source with the subtree.
func TestSourcePushRootIsTheBindingDirectoryNotTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	writeTree(t, dir, parityFixture)
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "config.json"), []byte(`{"api_url":"http://api.test","system_id":42,"repository_id":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(filepath.Join(dir, "docs", "nested"))

	root, err := pushRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != dir {
		t.Fatalf("push root %q, want the binding's directory %q", root, dir)
	}

	// And the pack honours it: every fixture path, not the subtree.
	stub := newPushStub(t)
	stub.repository = uploadRepository("old", "push", "completed")
	stub.response = map[string]interface{}{"result": "superseded", "revision": parityDigest, "previous_revision": "old", "refresh_job_id": 12}
	var out bytes.Buffer
	if err := sourcePush(stub.config(7), sourcePushOptions{MaxSizeMB: 100, Root: root}, &out); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(stub.zipEntries) != len(parityFixture) {
		t.Fatalf("zip entries %v, want the whole checkout", stub.zipEntries)
	}
}

func gitCommitFixture(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "fixture@example.com"},
		{"config", "user.name", "Fixture"},
		{"add", "."},
		{"commit", "-q", "-m", "fixture"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

var _ = multipart.NewWriter
