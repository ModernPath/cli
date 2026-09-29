package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func TestPrepareInputsDoesNotUseSharedConfigTimestampForLinkedWorktreeExport(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	main := filepath.Join(t.TempDir(), "main")
	worktree := filepath.Join(t.TempDir(), "sibling")
	if err := os.MkdirAll(filepath.Join(main, ".modernpath", "demo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(main, ".modernpath", "demo", "current.md"), []byte("main export"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init")
	gitRun(t, main, "add", ".modernpath/demo/current.md")
	gitRun(t, main, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	gitRun(t, main, "worktree", "add", "-b", "sibling", worktree, "HEAD")

	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	if err := os.Chdir(main); err != nil {
		t.Fatal(err)
	}
	const sharedSync = "2026-09-20T12:00:00Z"
	if err := config.WriteConfig(&config.Config{APIURL: srv.URL, SystemID: 7, SystemName: "Demo", LastSyncAt: sharedSync}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAuth(&config.Auth{Token: initGoodToken, Actor: "jane@example.com"}); err != nil {
		t.Fatal(err)
	}
	const mainSync = "2026-09-18T08:00:00Z"
	if err := os.WriteFile(filepath.Join(main, ".modernpath", "demo", "docs_push_manifest.json"), []byte(`{"version":1,"generated_at":"`+mainSync+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(worktree); err != nil {
		t.Fatal(err)
	}

	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs: %v\n%s", err, out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	if got := report["local_documents_last_synced_at"]; got != nil {
		t.Fatalf("reported shared config timestamp %v for linked worktree export without local provenance; want unknown", got)
	}
	if !strings.Contains(strings.ToLower(report["local_documents_remedy"].(string)), "modernpath docs sync") {
		t.Fatalf("missing local manifest timestamp omitted sync guidance: %#v", report["local_documents_remedy"])
	}

	if err := os.Chdir(main); err != nil {
		t.Fatal(err)
	}
	out, err = runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs in main checkout: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode main checkout preparation report: %v\n%s", err, out)
	}
	if got := report["local_documents_last_synced_at"]; got != mainSync {
		t.Fatalf("main export timestamp = %v, want its own manifest time %s instead of newer shared config timestamp %s", got, mainSync, sharedSync)
	}
}
