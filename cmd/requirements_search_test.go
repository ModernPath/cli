package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// REQ-PLN-192 (EPIC-REQ-SEARCH): requirements search reads the requirement
// search route for the bound system and prints each hit's id, name, status
// and how it matched. A row with no external id is shown by its display
// reference.
func TestRequirementsSearchCallsTheRouteAndRenders(t *testing.T) {
	var got url.Values
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "unauthorized search request", http.StatusUnauthorized)
			return
		}
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"query": got.Get("q"),
			"items": []map[string]any{
				{"kind": "requirement", "requirement_kind": "system", "id": "a1", "external_id": "REQ-CROSS-970",
					"display_id": "SR-12", "name": "Signing keys rotate", "description": "Keys rotate.",
					"work_status": "TODO", "candidate": false, "matched": []string{"exact", "semantic"}, "similarity": 0.91},
				{"kind": "requirement", "requirement_kind": "user", "id": "b2", "external_id": nil,
					"display_id": "UR-5", "name": "Credentials are replaced", "description": nil,
					"work_status": "DERIVED", "candidate": true, "matched": []string{"exact"}, "similarity": nil},
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	out, err := runRoot(t, "requirements", "search", "signing keys", "--limit", "5")
	if err != nil {
		t.Fatalf("requirements search: %v", err)
	}
	if got.Get("system_id") != "1" || got.Get("q") != "signing keys" || got.Get("limit") != "5" {
		t.Fatalf("query = %v, want system_id=1 q=\"signing keys\" limit=5", got)
	}
	for _, want := range []string{
		"REQ-CROSS-970", "Signing keys rotate", "TODO", "exact, semantic",
		"UR-5", "Credentials are replaced", "DERIVED", "candidate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output omitted %q:\n%s", want, out)
		}
	}

	out, err = runRoot(t, "requirements", "search", "signing keys", "--json")
	if err != nil {
		t.Fatalf("requirements search --json: %v", err)
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if len(decoded.Items) != 2 || decoded.Items[0]["external_id"] != "REQ-CROSS-970" || decoded.Items[1]["display_id"] != "UR-5" {
		t.Fatalf("--json items = %v", decoded.Items)
	}
}

// The 404 body tells an unknown system apart from a server without the route.
func TestRequirementsSearchTellsUnknownSystemFromMissingRoute(t *testing.T) {
	unknownSystem := true
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/search", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		if unknownSystem {
			_, _ = w.Write([]byte(`{"error":{"message":"system not found"}}`))
		} else {
			_, _ = w.Write([]byte(`{"errors":{"detail":"Not Found"}}`))
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "requirements", "search", "keys")
	if err == nil || !strings.Contains(err.Error(), "system 1 is not known") {
		t.Fatalf("unknown system error = %v, want it to name the system", err)
	}
	unknownSystem = false
	_, err = runRoot(t, "requirements", "search", "keys")
	if err == nil || !strings.Contains(err.Error(), "does not support requirement search") {
		t.Fatalf("missing route error = %v, want an update-the-server message", err)
	}
}

func TestRequirementsSearchRejectsBadArgumentsBeforeRequest(t *testing.T) {
	requests := 0
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/requirements/search", func(http.ResponseWriter, *http.Request) { requests++ })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	if _, err := runRoot(t, "requirements", "search", "  "); err == nil {
		t.Error("a blank query was accepted")
	}
	if _, err := runRoot(t, "requirements", "search", "keys", "--limit", "26"); err == nil || !strings.Contains(err.Error(), "--limit") {
		t.Errorf("--limit 26 error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("search requests = %d, want none", requests)
	}
}
