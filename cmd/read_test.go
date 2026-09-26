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
		{"tier", []string{"read-doc", "--list", "--tier", "subsystem"}, url.Values{"system_id": {"1"}, "tier": {"subsystem"}}},
		{"angle", []string{"read-doc", "--list", "--angle", "architecture"}, url.Values{"system_id": {"1"}, "angle": {"architecture"}}},
		{"tier and angle", []string{"read-doc", "--list", "--tier", "subsystem", "--angle", "architecture"}, url.Values{"system_id": {"1"}, "tier": {"subsystem"}, "angle": {"architecture"}}},
		{"unfiltered", []string{"read-doc", "--list"}, url.Values{"system_id": {"1"}, "list": {"true"}}},
		{"id before title and list", []string{"read-doc", "Other title", "--id", "doc-1", "--list", "--tier", "module"}, url.Values{"system_id": {"1"}, "doc_id": {"doc-1"}}},
		{"list before title", []string{"read-doc", "Other title", "--list"}, url.Values{"system_id": {"1"}, "list": {"true"}}},
		{"title before filters", []string{"read-doc", "Other title", "--tier", "module"}, url.Values{"system_id": {"1"}, "title": {"Other title"}}},
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
					"documents": []map[string]string{{"id": "doc-1", "title": "Subsystem doc", "tier": "subsystem"}},
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
