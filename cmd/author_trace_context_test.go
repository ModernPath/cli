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
	"strings"
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

	aggregate := strings.Repeat("a", 64)
	reviewDir := writeReviewConsumerSnapshot(t, env.Root, env.APIURL, env.SystemID, "review-9f", "epic", "EPIC-CLI-008", aggregate)

	err := authorTrace(env, "TRACE-COLD-EPIC-CLI-008", map[string]any{
		"purpose":           "cold-review",
		"exact_scope":       []string{"EPIC-CLI-008"},
		"verdict":           "PASS",
		"fingerprint":       aggregate,
		"review_context_id": "review-9f",
	})
	if err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	if record["review_context_id"] != "review-9f" {
		t.Fatalf("a cold-review trace must carry the review-context id read from the reviewed scope; got %v", record["review_context_id"])
	}
	if record["fingerprint"] != aggregate || !strings.Contains(record["body_md"].(string), reviewManifestDigestFromDisk(t, reviewDir)) {
		t.Fatalf("cold-review trace must persist the selected snapshot pin and digest, got %v", record)
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
