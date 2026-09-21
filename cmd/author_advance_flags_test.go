package cmd

// PR #452 review round 2, finding 1: `author advance --to` must reach the
// server. The --from/--to flags added for trace and gate (REQ-CROSS-377)
// rebound the advance command's --to as well, so every human-gated apply
// posted "to": "". Driven through cobra, the way an operator runs it, against
// a capture server — a direct call to authorAdvance cannot see a flag binding.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// cobraWorkspace binds a temp workspace to srv so rootCmd.Execute resolves
// the environment from disk exactly as the installed binary does.
func cobraWorkspace(t *testing.T, srv *httptest.Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"api_url":"` + srv.URL + `","system_id":1,"system_name":"T","system_slug":"t","epic_id":1}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "auth.json"), []byte(`{"token":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
}

func TestAuthorAdvancePostsTheToFlagThroughCobra(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-ADV-1", "work_status": "DONE"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	resetTreeFlags(rootCmd)
	rootCmd.SetArgs([]string{"author", "advance", "REQ-ADV-1", "--kind", "requirement", "--to", "DONE", "--expected", "IN_REVIEW",
		"--gate", "COMPLETE-X", "--gate-answer", "approve", "--gate-fingerprint", "gfp"})
	defer rootCmd.SetArgs(nil)
	captureOut(t, func() { _ = rootCmd.Execute() })

	if got["to"] != "DONE" || got["expected"] != "IN_REVIEW" {
		t.Fatalf("author advance must post the --to and --expected it was given, posted to=%v expected=%v (body %v)", got["to"], got["expected"], got)
	}
	if got["gate_ref"] != "COMPLETE-X" || got["gate_answer"] != "approve" {
		t.Fatalf("the gate reference and answer ride the post, got %v", got)
	}
}
