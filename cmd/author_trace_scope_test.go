package cmd

// REQ-CROSS-365 (EPIC-CLI-015): `author trace --scope` accepts both the bare
// external id (`REQ-CROSS-358`) and the `kind:ext` form
// (`single_sr:REQ-CROSS-358`), resolving both to the same bare scope. The server
// stores and matches bare ids (phase_facts.ex), and reviewContextForScope
// resolves the review context off the working-set dir name (the bare id), so a
// literal `single_sr:REQ-CROSS-358` matches nothing and breaks context
// resolution. `process findings add` already parses the kind:ext form via
// splitScope; `author trace` did not. CLI-only normalization; no server change.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestREQCROSS365TraceScopeNormalizesKindExtToBareId(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "CR-TRACE-X", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// The caller names the scope in the kind:ext form `findings add` requires,
	// mixed with a bare id. Both must reach the server as bare ids. Normalization
	// is purpose-independent, so a free purpose is used here to isolate it from the
	// cold-review review-context requirement (REQ-CROSS-364).
	if err := authorTrace(env, "TR-SCOPE-X", map[string]any{
		"purpose":     "lower-verify",
		"exact_scope": []string{"single_sr:REQ-CROSS-358", "REQ-CROSS-359"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	}); err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	raw, _ := record["exact_scope"].([]any)
	var ids []string
	for _, s := range raw {
		ids = append(ids, s.(string))
	}
	want := []string{"REQ-CROSS-358", "REQ-CROSS-359"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("author trace --scope must resolve kind:ext and bare ids to bare ids; want %v got %v", want, ids)
	}
}

func TestREQCROSS365TraceScopeKindExtResolvesReviewContext(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "CR-TRACE-Y", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// The reviewer pulled the scope --for-review, which stamps a review context in
	// the scope directory named by the BARE id.
	dir := filepath.Join(env.Root, workingSetDir, "REQ-CROSS-358")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := "# working-set context\n\nmode: review\ncontext_id: review-7c\nscope: single_sr:REQ-CROSS-358\n"
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}

	// The caller names the scope as kind:ext. Context resolution keys on the bare
	// dir name, so normalization must happen before reviewContextForScope runs.
	if err := authorTrace(env, "CR-TRACE-Y", map[string]any{
		"purpose":     "cold-review",
		"exact_scope": []string{"single_sr:REQ-CROSS-358"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	}); err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	if record["review_context_id"] != "review-7c" {
		t.Fatalf("a kind:ext scope must still resolve the review context off the bare dir name; got %v", record["review_context_id"])
	}
}
