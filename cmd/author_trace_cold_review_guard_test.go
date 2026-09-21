package cmd

// REQ-CROSS-364 (EPIC-CLI-015): creating a cold-review trace whose review context
// is unresolved (nil/empty) is refused at creation with a clear message, instead
// of silently persisting a gate_class trace, purpose cold-review row with a nil
// review_context_id that can never satisfy cold_review and — because traces are
// immutable — can never be removed. This is the CLI half (fail fast before the
// post); the server enforces the same backstop. Scoped to cold-review: a
// non-cold-review trace never carries a context and must still post.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestREQCROSS364TraceRefusesColdReviewWithoutContext(t *testing.T) {
	posted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		posted = true
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "CR-NOCTX", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// The reviewed scope was never pulled --for-review, so no review context is
	// stamped for REQ-CROSS-358.
	err := authorTrace(env, "CR-NOCTX", map[string]any{
		"purpose":     "cold-review",
		"exact_scope": []string{"REQ-CROSS-358"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	})
	if err == nil {
		t.Fatalf("author trace must refuse a cold-review verdict with no resolved review context")
	}
	if posted {
		t.Fatalf("the CLI must refuse before posting — a nil-context cold-review trace is immutable and can never satisfy cold_review")
	}
}

func TestREQCROSS364TraceAllowsColdReviewWithResolvedContext(t *testing.T) {
	posted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		posted = true
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "CR-CTX", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// The reviewer pulled the scope --for-review, stamping a review context.
	dir := filepath.Join(env.Root, workingSetDir, "REQ-CROSS-358")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := "# working-set context\n\nmode: review\ncontext_id: review-3a\nscope: single_sr:REQ-CROSS-358\n"
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := authorTrace(env, "CR-CTX", map[string]any{
		"purpose":     "cold-review",
		"exact_scope": []string{"REQ-CROSS-358"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	}); err != nil {
		t.Fatalf("a cold-review trace with a resolved context must post, got: %v", err)
	}
	if !posted {
		t.Fatalf("a cold-review trace with a resolved context must reach the server")
	}
}

func TestREQCROSS364TraceAllowsNonColdReviewWithoutContext(t *testing.T) {
	posted := false
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		posted = true
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "TR-PLAN", "state": "pass", "fingerprint": "agg"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	// A non-cold-review trace never carries a review context — the guard must not
	// touch it.
	if err := authorTrace(env, "TR-PLAN", map[string]any{
		"purpose":     "lower-verify",
		"exact_scope": []string{"REQ-CROSS-358"},
		"verdict":     "PASS",
		"fingerprint": "agg",
	}); err != nil {
		t.Fatalf("a non-cold-review trace with no context must post, got: %v", err)
	}
	if !posted {
		t.Fatalf("a non-cold-review trace must reach the server")
	}
}
