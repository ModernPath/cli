package cmd

// REQ-CROSS-315 (SR-CLI-0086), EPIC-CLI-008: a cold-review verdict recorded
// through `author trace` must carry the review-context id the reviewed scope was
// pulled under, so PhaseFacts.cold_review can judge independence. Without it a
// CLI-recorded verdict is never independent, cold_review is never satisfied, and
// the entry route never opens (#8). `process findings add` already stamps it;
// `author trace` did not.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestREQCROSS315ColdReviewTraceCarriesTheReviewContext(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "TRACE-COLD-EPIC-CLI-008", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// The reviewer pulled the scope `--for-review`, which stamps a review context
	// in the scope directory.
	dir := filepath.Join(env.Root, workingSetDir, "EPIC-CLI-008")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := "# working-set context\n\nmode: review\ncontext_id: review-9f\nscope: epic:EPIC-CLI-008\n"
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}

	err := authorTrace(env, "TRACE-COLD-EPIC-CLI-008", map[string]any{
		"purpose":     "cold-review",
		"exact_scope": []string{"EPIC-CLI-008"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	})
	if err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	if record["review_context_id"] != "review-9f" {
		t.Fatalf("a cold-review trace must carry the review-context id read from the reviewed scope; got %v", record["review_context_id"])
	}
}

// A non-cold-review trace, or one with no pulled review context, carries no
// review_context_id — the stamping is scoped to the independence check's need.
func TestREQCROSS315NonColdReviewTraceCarriesNoReviewContext(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "TRACE-PLAN", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	if err := authorTrace(env, "TRACE-PLAN", map[string]any{
		"purpose":     "plan",
		"exact_scope": []string{"EPIC-CLI-008"},
		"verdict":     "PASS",
	}); err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	if _, present := record["review_context_id"]; present {
		t.Fatalf("a non-cold-review trace must not stamp a review context: %v", record["review_context_id"])
	}
}
