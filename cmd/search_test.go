package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSearchUsesCompactDefaultAndExplicitDetails(t *testing.T) {
	var got url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"docs":  []map[string]any{{"id": "doc-1", "title": "Token guard", "tier": "module", "angle": "security", "summary": "Checks bearer tokens"}},
			"files": []map[string]any{{"id": 9, "relative_path": "auth/guard.go", "language": "go", "purpose": "Validates a bearer token"}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	stdout, err := runRoot(t, "search", "token", "guard", "--json")
	if err != nil {
		t.Fatal(err)
	}
	if got.Get("q") != "token guard" || got.Get("limit") != "10" || got.Get("type") != "all" {
		t.Fatalf("search params: %v", got)
	}
	var response map[string][]map[string]any
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("JSON output: %v\n%s", err, stdout)
	}
	if len(response["docs"]) != 1 || response["docs"][0]["id"] != "doc-1" || response["docs"][0]["title"] != "Token guard" {
		t.Fatalf("doc identity missing: %v", response)
	}
	if _, exists := response["docs"][0]["summary"]; exists {
		t.Fatalf("default JSON includes detail: %v", response)
	}
	if _, exists := response["files"][0]["purpose"]; exists {
		t.Fatalf("default JSON includes purpose: %v", response)
	}

	stdout, err = runRoot(t, "search", "token", "guard", "--json", "--fields", "summary,purpose")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		t.Fatalf("projected JSON: %v\n%s", err, stdout)
	}
	if response["docs"][0]["summary"] != "Checks bearer tokens" || response["files"][0]["purpose"] != "Validates a bearer token" {
		t.Fatalf("explicit detail missing: %v", response)
	}
}

func TestSearchRejectsInvalidSelectionBeforeRequest(t *testing.T) {
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search", func(http.ResponseWriter, *http.Request) { requests++ })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "search", "query", "--fields", "unknown")
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("invalid fields error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("search calls = %d, want 0", requests)
	}
}
