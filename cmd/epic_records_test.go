package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEpicListUsesBoundedServerPageAndCompactOutput(t *testing.T) {
	var gotQuery url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"data":{"items":[{"id":17,"title":"Shopping cart"}],"next_cursor":"next-page"}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "list", "--status", "in_progress", "--query", "cart", "--order", "title")
	if err != nil {
		t.Fatal(err)
	}
	if gotQuery.Get("limit") != "20" || gotQuery.Get("order") != "title" || gotQuery.Get("query") != "cart" || gotQuery.Get("status") != "in_progress" {
		t.Fatalf("page contract not sent to server: %#v", gotQuery)
	}
	if !strings.Contains(out, "17  Shopping cart\n") || !strings.Contains(out, "next_cursor: next-page") {
		t.Fatalf("unexpected compact list output: %q", out)
	}
}

func TestEpicGetDefaultsToSummaryAndSupportsExactProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"title":"Explicit","process_status":"IN_PROGRESS","scope":"bounded","description":"long body","tenant_id":99}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "get", "42", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(out), &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary) != 3 || summary["id"] != float64(42) || summary["title"] != "Explicit" || summary["process_status"] != "IN_PROGRESS" {
		t.Fatalf("unexpected default summary: %#v", summary)
	}

	out, err = runRoot(t, "epics", "get", "42", "--fields", "title,scope", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var selected map[string]any
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected["title"] != "Explicit" || selected["scope"] != "bounded" {
		t.Fatalf("fields must return exactly the requested projection: %#v", selected)
	}
}

func TestEpicSpecificationContentIsExplicit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems":
			healthyProbe(w, r)
		case "/api/work/epics/42":
			_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1}}`))
		case "/api/work/epics/42/specs":
			_, _ = w.Write([]byte(`{"data":{"specs":[{"id":8,"filename":"overview.md","content":"Long specification body"}]}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "specs", "list", "42", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]any
	if err := json.Unmarshal([]byte(out), &summary); err != nil || len(summary) != 2 || summary["count"] != float64(1) || summary["epic_id"] != float64(42) {
		t.Fatalf("specification summary = %s; %v", out, err)
	}
	for _, flag := range [][]string{{"--fields", "specs"}, {"--full"}} {
		args := append([]string{"epics", "specs", "list", "42", "--json"}, flag...)
		out, err := runRoot(t, args...)
		if err != nil || !json.Valid([]byte(out)) || !strings.Contains(out, "Long specification body") {
			t.Fatalf("explicit specification content = %s; %v", out, err)
		}
	}
}

func TestEpicSpecificationStatusIncludesCountsByState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems":
			healthyProbe(w, r)
		case "/api/work/epics/42":
			_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1}}`))
		case "/api/work/epics/42/spec-status":
			_, _ = w.Write([]byte(`{"data":{"epic_id":42,"artifact_count":3,"artifacts":[{"id":1,"status":"draft"},{"id":2,"status":"ready"},{"id":3,"status":"ready"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	cobraWorkspace(t, server)
	stdout, _, err := runAskStreams(t, "epics", "specs", "status", "42", "--json")
	var summary struct {
		ArtifactCount int            `json:"artifact_count"`
		StatusCounts  map[string]int `json:"status_counts"`
		Artifacts     any            `json:"artifacts"`
	}
	if err != nil || json.Unmarshal([]byte(stdout), &summary) != nil || summary.ArtifactCount != 3 || summary.StatusCounts["ready"] != 2 || summary.StatusCounts["draft"] != 1 || summary.Artifacts != nil {
		t.Fatalf("status must summarize actual artifact states: %s err=%v", stdout, err)
	}
}

func TestSystemAppLinkReturnsOnlyAURL(t *testing.T) {
	for _, app := range []string{"", "http://app.example.test"} {
		link := systemAppLink(app, "http://api.example.test/", 42, "overview")
		parsed, err := url.ParseRequestURI(link)
		if err != nil || parsed.Host == "" || strings.ContainsAny(link, " \t\n") {
			t.Fatalf("link must be a usable URL: %q err=%v", link, err)
		}
	}
}
