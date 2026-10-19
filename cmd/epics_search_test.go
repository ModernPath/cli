package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

// REQ-PLN-192 (EPIC-REQ-SEARCH): epics search reads the epic search route for
// the bound system and prints each hit's native ID, name, status and match.
func TestEpicsSearchCallsTheRouteAndRenders(t *testing.T) {
	var got url.Values
	mux := http.NewServeMux()
	addDiscoveryTestSystems(mux)
	mux.HandleFunc("/api/import/capabilities", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Error("capabilities read omitted authorization")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"upload_limit_bytes": 90000000, "app_url": "https://app.example.test/"})
	})
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
				{"id": 42, "name": "Teams push", "description": "Work moments",
					"process_status": "TODO", "matched": []string{"keyword", "semantic"}, "similarity": 0.8},
				{"id": 43, "name": "Chat notifications", "description": nil,
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
		"#42 [TODO] Teams push", "TODO",
		"#43", "Chat notifications", "PROPOSED",
		"https://app.example.test/epics/42", "https://app.example.test/epics/43",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output omitted %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "keyword") || strings.Contains(out, "Work moments") {
		t.Fatalf("default output should omit match detail and description: %s", out)
	}
	out, err = runRoot(t, "epics", "search", "teams", "--fields", "description,matched")
	if err != nil || !strings.Contains(out, "keyword, semantic") || !strings.Contains(out, "Work moments") {
		t.Fatalf("explicit detail projection: %v %s", err, out)
	}

	out, err = runRoot(t, "epics", "search", "teams", "--fields", "title,url", "--json")
	if err != nil || !strings.Contains(out, `"title":`) {
		t.Fatalf("search must accept the title field used by get/list: %v %s", err, out)
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
	if len(decoded.Items) != 2 || decoded.Items[0]["id"] != float64(42) || decoded.Items[0]["url"] != "https://app.example.test/epics/42" {
		t.Fatalf("--json items = %v", decoded.Items)
	}
	if err := config.WriteAuth(&config.Auth{Token: "t", WorkspaceID: "org-1"}); err != nil {
		t.Fatal(err)
	}
	out, err = runRoot(t, "epics", "search", "teams", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://app.example.test/workspaces/org-1/systems/1/epics/42") {
		t.Fatalf("missing scoped epic link: %s", out)
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
