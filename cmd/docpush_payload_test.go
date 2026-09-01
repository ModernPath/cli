// REQ-CROSS-207 (EPIC-CLI-001 T3, RUN:2026-08-18): `docs push` sent
// {"system_id": N, "docs": [...]} while the server's only import_docs clause
// pattern-matches "architecture_id" — so a sync→edit→push roundtrip always
// answered 400 "Missing required 'architecture_id' and 'docs' array".
// USER:2026-08-18: the server's current shape is canonical; the CLI adapts.
// The mux below dispatches exactly like the server clause does.
package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func docsImportTestServer(t *testing.T) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	captured := map[string]interface{}{}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/docs/import", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		captured = body

		docs, docsOK := body["docs"].([]interface{})
		_, archOK := body["architecture_id"]
		if !archOK || !docsOK {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"Missing required 'architecture_id' and 'docs' array"}`))
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"data":    map[string]int{"imported": len(docs), "updated": len(docs), "created": 0},
		})
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, &captured
}

func TestPushDocsSendsTheKeyTheServerRequires(t *testing.T) {
	server, captured := docsImportTestServer(t)

	dir := t.TempDir()
	docDir := filepath.Join(dir, ".modernpath", "sandbox")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := "# Pushed Doc\n\nA body paragraph long enough to become the summary.\n"
	if err := os.WriteFile(filepath.Join(docDir, "arch.md"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	cfg := &config.Config{APIURL: server.URL, SystemID: 42, SystemSlug: "sandbox"}

	count, err := pushDocs(cfg)
	if err != nil {
		t.Fatalf("push against the server's real dispatch failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 imported doc, got %d", count)
	}
	if _, ok := (*captured)["architecture_id"]; !ok {
		t.Fatalf("payload must carry architecture_id, got keys: %v", *captured)
	}
}
