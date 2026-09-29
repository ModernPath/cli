package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// REQ-PLN-192 (EPIC-REQ-SEARCH): epics search reads the epic search route for
// the bound system and prints each hit's code, name, status and how it matched.
func TestEpicsSearchCallsTheRouteAndRenders(t *testing.T) {
	var got url.Values
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/epics/search", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer t" {
			http.Error(w, "unauthorized search request", http.StatusUnauthorized)
			return
		}
		got = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"query": got.Get("q"),
			"items": []map[string]any{
				{"id": 42, "code": "EPIC-INT-009", "name": "Teams push", "description": "Work moments",
					"process_status": "TODO", "matched": []string{"keyword", "semantic"}, "similarity": 0.8},
				{"id": 43, "code": nil, "name": "Chat notifications", "description": nil,
					"process_status": "PROPOSED", "matched": []string{"semantic"}, "similarity": 0.7},
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	out, err := runRoot(t, "epics", "search", "teams", "--limit", "3")
	if err != nil {
		t.Fatalf("epics search: %v", err)
	}
	if got.Get("system_id") != "1" || got.Get("q") != "teams" || got.Get("limit") != "3" {
		t.Fatalf("query = %v, want system_id=1 q=teams limit=3", got)
	}
	for _, want := range []string{
		"EPIC-INT-009", "Teams push", "TODO", "keyword, semantic",
		"#43", "Chat notifications", "PROPOSED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output omitted %q:\n%s", want, out)
		}
	}

	out, err = runRoot(t, "epics", "search", "teams", "--json")
	if err != nil {
		t.Fatalf("epics search --json: %v", err)
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
	if len(decoded.Items) != 2 || decoded.Items[0]["code"] != "EPIC-INT-009" {
		t.Fatalf("--json items = %v", decoded.Items)
	}
}

// The 404 body tells an unknown system apart from a server without the route.
func TestEpicsSearchTellsUnknownSystemFromMissingRoute(t *testing.T) {
	unknownSystem := true
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/v1/sync/epics/search", func(w http.ResponseWriter, _ *http.Request) {
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

	_, err := runRoot(t, "epics", "search", "teams")
	if err == nil || !strings.Contains(err.Error(), "system 1 is not known") {
		t.Fatalf("unknown system error = %v, want it to name the system", err)
	}
	unknownSystem = false
	_, err = runRoot(t, "epics", "search", "teams")
	if err == nil || !strings.Contains(err.Error(), "does not support epic search") {
		t.Fatalf("missing route error = %v, want an update-the-server message", err)
	}
}
