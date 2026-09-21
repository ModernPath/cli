package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func reCommand(t *testing.T, server *httptest.Server, input string, args ...string) (string, error) {
	t.Helper()
	command := newReverseEngineerCommand(func() (*factoryEnv, error) { return wsEnv(t, server), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetIn(strings.NewReader(input))
	command.SetArgs(args)
	err := command.Execute()
	return out.String(), err
}

func TestSRRDDONBOARD008StructuredPublication(t *testing.T) {
	var received map[string]any
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		if r.Method != "PUT" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/groups/catalog" {
			t.Errorf("wrong target: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("actor token missing")
		}
		posts++
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&received)
		_, _ = w.Write([]byte(`{"data":{"id":"receipt","result":{"created_requirements":1}}}`))
	}))
	defer server.Close()
	input := `{"requirements":[{"kind":"system","external_id":"SR-NEW","source_citations":[{"kind":"document","document_id":9007199254740993}],"criteria":[{"statement":"preserve nested fields"}]}]}`
	out, err := reCommand(t, server, input, "publish", "--run", "run-id", "--group", "catalog", "--file", "-")
	if err != nil || posts != 1 || !strings.Contains(out, "receipt") {
		t.Fatalf("publication: %s %v posts=%d", out, err, posts)
	}
	encoded, _ := json.Marshal(received)
	if !strings.Contains(string(encoded), "9007199254740993") || strings.Contains(string(encoded), "release_id") {
		t.Fatalf("payload altered: %s", encoded)
	}
}

func TestSRRDDONBOARD002ReadCapturedDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/documents/doc-id" {
			t.Errorf("wrong captured document target: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"content":"original document","version":1}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "read-document", "--run", "run-id", "--document", "doc-id")
	if err != nil || !strings.Contains(out, "original document") {
		t.Fatalf("document readback: %s %v", out, err)
	}
}

func TestSRRDDONBOARD007StoredCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/systems/4/reverse-engineering/runs/run-id/coverage" {
			t.Errorf("wrong coverage route: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"inventory":{"included_files":2},"coverage":{"governed_files":1,"candidate_files":0}}}`))
	}))
	defer server.Close()
	out, err := reCommand(t, server, "", "coverage", "--run", "run-id")
	if err != nil || !strings.Contains(out, "governed_files") {
		t.Fatalf("coverage: %s %v", out, err)
	}
}

func TestSRRDDONBOARD008RefusalsDoNotRetryOrWidenScope(t *testing.T) {
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(404)
			return
		}
		writes++
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"error":"stale_corpus"}`))
	}))
	defer server.Close()
	_, err := reCommand(t, server, `{"requirements":[]}`, "publish", "--run", "r", "--group", "g", "--file", "-")
	if err == nil || !strings.Contains(err.Error(), "stale_corpus") || writes != 1 {
		t.Fatalf("refusal: %v writes=%d", err, writes)
	}
	for _, input := range []string{`{"system_id":99}`, `{"tenant_id":99}`, `{"mode":"baseline"} {}`, `[]`} {
		_, err = reCommand(t, server, input, "authorize", "--file", "-")
		if err == nil || writes != 1 {
			t.Fatalf("invalid intent reached server: %s %v", input, err)
		}
	}
}

func TestSRRDDONBOARD008CommandsAndStableCapabilities(t *testing.T) {
	for _, name := range []string{"preflight", "inventory", "authorize", "capture-source", "source-status", "publish", "status", "candidates", "preview", "decide", "read-source"} {
		command, _, err := rootCmd.Find([]string{"reverse-engineer", name})
		if err != nil || command.Name() != name {
			t.Errorf("missing command %s", name)
		}
	}
	for path, want := range map[string]string{
		"/api/v1/systems/54/reverse-engineering/runs":            "reverse_engineering.authorize",
		"/api/v1/systems/54/reverse-engineering/runs/r/groups/g": "reverse_engineering.publish",
		"/api/v1/systems/54/reverse-engineering/runs/r/sources":  "reverse_engineering.capture",
		"/api/v1/systems/54/candidate-sets/decisions":            "candidate_set.decide",
	} {
		if got := writeName(path, nil); got != want {
			t.Errorf("%s: %s != %s", path, got, want)
		}
	}
}

func TestSRRDDONBOARD007ExplicitMultiRepositoryInventoryAndImmutableBundle(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"catalog", "mobile"} {
		root := filepath.Join(parent, name)
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
		for _, filename := range []string{"view.jsp", "model.xml", "transform.xsl", "transform.xslt", ".env"} {
			if err := os.WriteFile(filepath.Join(root, filename), []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	manifest, err := buildReverseInventory([]string{"catalog=" + filepath.Join(parent, "catalog"), "mobile=" + filepath.Join(parent, "mobile")})
	if err != nil || len(manifest.Repositories) != 2 {
		t.Fatalf("inventory: %+v %v", manifest, err)
	}
	for _, repo := range manifest.Repositories {
		if len(repo.Files) != 4 || len(repo.Exclusions) != 1 || repo.Revision != "unversioned" || !repo.Dirty {
			t.Fatalf("incomplete inventory: %+v", repo)
		}
	}
	repo := manifest.Repositories[0]
	files, err := reverseSourceBundle(repo, filepath.Join(parent, repo.Key))
	if err != nil || len(files) != 4 {
		t.Fatalf("bundle: %+v %v", files, err)
	}
	if err := os.WriteFile(filepath.Join(parent, repo.Key, "view.jsp"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := reverseSourceBundle(repo, filepath.Join(parent, repo.Key)); err == nil {
		t.Fatal("changed authorized bytes accepted")
	}
	if err := os.Symlink(filepath.Join(parent, "mobile", "model.xml"), filepath.Join(parent, "catalog", "escape.xml")); err != nil {
		t.Fatal(err)
	}
	if _, err := buildReverseInventory([]string{"catalog=" + filepath.Join(parent, "catalog")}); err == nil {
		t.Fatal("symlink entered inventory")
	}
}

func TestSRRDDONBOARD007GitWorktreeAndIgnoredFiles(t *testing.T) {
	root := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		c := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git: %s %v", out, err)
		}
	}
	git("init", "-q")
	git("-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-qm", "fixture")
	worktree := filepath.Join(t.TempDir(), "checkout")
	git("worktree", "add", "--detach", worktree)
	if err := os.WriteFile(filepath.Join(worktree, ".gitignore"), []byte("ignored.xml\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"visible.jsp", "ignored.xml"} {
		if err := os.WriteFile(filepath.Join(worktree, path), []byte("bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := buildReverseInventory([]string{"worktree=" + worktree})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(manifest.Repositories[0].Files)
	if !strings.Contains(string(encoded), "visible.jsp") || strings.Contains(string(encoded), "ignored.xml") || len(manifest.Repositories[0].Revision) != 40 {
		t.Fatalf("wrong worktree inventory: %+v", manifest)
	}
}
