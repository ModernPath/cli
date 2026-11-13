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
// and how it matched. Human output uses the server's display reference while
// JSON retains the immutable UUID.
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
				{"kind": "requirement", "requirement_kind": "system_requirement", "id": "8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c",
					"display_ref": "SR-12-1", "name": "Signing keys rotate", "description": "Keys rotate.",
					"work_status": "TODO", "candidate": false, "matched": []string{"exact", "semantic"}, "similarity": 0.91},
				{"kind": "requirement", "requirement_kind": "test_case", "id": "a132ce3d-8b3b-4bd2-b80f-983d42b25da4",
					"display_ref": "TC-3-1", "name": "Replacement is covered", "description": nil,
					"candidate": true, "matched": []string{"exact"}},
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
		"SR-12-1", "Signing keys rotate", "TODO",
		"TC-3-1", "Replacement is covered", "test_case", "candidate",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("human output omitted %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "exact") || strings.Contains(out, "Keys rotate.") {
		t.Fatalf("default output should omit match detail and description: %s", out)
	}
	out, err = runRoot(t, "requirements", "search", "signing keys", "--fields", "description,matched", "--full")
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatal("--fields and --full must be mutually exclusive")
	}
	out, err = runRoot(t, "requirements", "search", "signing keys", "--fields", "description,matched")
	if err != nil || !strings.Contains(out, "exact, semantic") || !strings.Contains(out, "Keys rotate.") {
		t.Fatalf("explicit detail projection: %v %s", err, out)
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
	if len(decoded.Items) != 2 || decoded.Items[0]["id"] != "8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c" || decoded.Items[1]["display_ref"] != "TC-3-1" || decoded.Items[1]["candidate"] != true {
		t.Fatalf("--json items = %v", decoded.Items)
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

// SR-4873-0: a 404 on either search reaches the operator as the server's
// refusal with the binding hint.
func TestSearch404ReportsTheServerRefusal(t *testing.T) {
	mux := http.NewServeMux()
	for _, route := range []string{"/api/v1/sync/requirements/search", "/api/v1/sync/epics/search"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"system_not_found","message":"system not found"}}`))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	for _, args := range [][]string{{"requirements", "search", "keys"}, {"epics", "search", "teams"}} {
		_, err := runRoot(t, args...)
		if err == nil {
			t.Fatalf("%v: a 404 must fail the command", args)
		}
		if want := "server 404: system not found\nRun 'modernpath status' to check the System binding."; err.Error() != want {
			t.Fatalf("%v: error =\n  %q\nwant\n  %q", args, err.Error(), want)
		}
	}
}
