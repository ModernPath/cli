package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReadDocListFiltersAndRequestPrecedence(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want url.Values
	}{
		{"tier", []string{"read-doc", "--list", "--tier", "subsystem"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "tier": {"subsystem"}}},
		{"angle", []string{"read-doc", "--list", "--angle", "architecture"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "angle": {"architecture"}}},
		{"tier and angle", []string{"read-doc", "--list", "--tier", "subsystem", "--angle", "architecture"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "tier": {"subsystem"}, "angle": {"architecture"}}},
		{"unfiltered", []string{"read-doc", "--list"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}}},
		{"unfiltered cursor", []string{"read-doc", "--cursor", "next-page"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "cursor": {"next-page"}}},
		{"unfiltered order", []string{"read-doc", "--order", "title"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "order": {"title"}}},
		{"id before title and list", []string{"read-doc", "Other title", "--id", "doc-1", "--list", "--tier", "module"}, url.Values{"system_id": {"1"}, "doc_id": {"doc-1"}}},
		{"list before title", []string{"read-doc", "Other title", "--list"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}}},
		{"title before filters", []string{"read-doc", "Other title", "--tier", "module"}, url.Values{"system_id": {"1"}, "paged": {"true"}, "limit": {"20"}, "q": {"Other title"}, "tier": {"module"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/docs/read" {
					t.Errorf("request path = %q", r.URL.Path)
				}
				got = r.URL.Query()
				if got.Get("doc_id") != "" {
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{
						"id": "doc-1", "title": "Subsystem doc", "content": "Document content", "tier": "subsystem",
					}})
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
					"documents": []map[string]string{{"id": "doc-1", "title": "Subsystem doc", "tier": "subsystem"}}, "count": 1,
				}})
			}))
			t.Cleanup(srv.Close)
			cobraWorkspace(t, srv)

			out, err := runRoot(t, tt.args...)
			if err != nil {
				t.Fatalf("read-doc failed: %v\n%s", err, out)
			}
			if got.Encode() != tt.want.Encode() {
				t.Errorf("query = %q, want %q", got.Encode(), tt.want.Encode())
			}
			if !strings.Contains(out, "Subsystem doc") {
				t.Errorf("document was not rendered:\n%s", out)
			}
		})
	}
}

func TestReadDocUnfilteredValidatesLimit(t *testing.T) {
	for _, limit := range []string{"0", "201"} {
		t.Run(limit, func(t *testing.T) {
			requested := false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested = true
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"documents": []any{}}})
			}))
			t.Cleanup(srv.Close)
			cobraWorkspace(t, srv)
			_, err := runRoot(t, "read-doc", "--limit", limit)
			if err == nil || !strings.Contains(err.Error(), "--limit must be between 1 and 200") {
				t.Errorf("invalid limit error = %v", err)
			}
			if requested {
				t.Error("invalid limit sent an HTTP request")
			}
		})
	}
}

func TestReadDocListIsOneLineAndContinuationUsesStderr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"documents": []map[string]string{{"id": "doc-1", "title": "Checkout\nreference", "tier": "module"}}, "next_cursor": "next-page",
		}})
	}))
	defer srv.Close()
	cobraWorkspace(t, srv)
	stdout, stderr, err := runAskStreams(t, "read-doc", "--list")
	if err != nil || stdout != "doc-1  Checkout reference\n" || !strings.Contains(stderr, "next-page") {
		t.Fatalf("list contract: stdout=%q stderr=%q err=%v", stdout, stderr, err)
	}
}

func TestReadDocSelectedContentHasNoSearchEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"id": "doc-1", "title": "Checkout", "content": "Text"}})
	}))
	defer srv.Close()
	cobraWorkspace(t, srv)
	stdout, _, err := runAskStreams(t, "read-doc", "--id", "doc-1", "--fields", "content", "--json")
	var row map[string]any
	if err != nil || json.Unmarshal([]byte(stdout), &row) != nil || len(row) != 3 || row["content"] != "Text" {
		t.Fatalf("selected detail contract: %s err=%v", stdout, err)
	}
}
