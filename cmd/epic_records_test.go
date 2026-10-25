package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestEpicReadsSendNoRequestBody(t *testing.T) {
	for _, args := range [][]string{
		{"epics", "get", "42", "--full", "--json"},
		{"epics", "list", "--json"},
		{"epics", "plan", "show", "42", "--full", "--json"},
		{"epics", "specs", "list", "42", "--json"},
		{"epics", "specs", "status", "42", "--json"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/systems" {
					healthyProbe(w, r)
					return
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodGet || len(body) != 0 {
					t.Errorf("Epic read must send GET without a body: method=%s body=%q", r.Method, body)
					http.Error(w, "request body not allowed for GET", http.StatusBadRequest)
					return
				}
				if strings.HasPrefix(r.URL.Path, "/api/work/epics/42") {
					_, _ = w.Write([]byte(`{"data":{"id":42,"epic_id":42,"system_id":1,"description":"Full Epic details","specs":[],"artifact_count":0,"artifacts":[]}}`))
				} else {
					_, _ = w.Write([]byte(`{"data":{"items":[],"next_cursor":null}}`))
				}
			}))
			defer server.Close()
			cobraWorkspace(t, server)
			out, err := runRoot(t, args...)
			if err != nil || !json.Valid([]byte(out)) {
				t.Fatalf("Epic read failed: output=%q err=%v", out, err)
			}
			if args[1] == "get" && !strings.Contains(out, "Full Epic details") {
				t.Fatalf("full Epic content missing: %s", out)
			}
		})
	}
}

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

// Epic 298 (SR-3812, TC-1397): `backlog` is a public Epic field, so the records
// routed to or closed by the Epic are served by --full and --fields backlog and
// printed as the other array fields are in text.
func TestEpicGetServesTheBacklogRecordsRoutedToTheEpic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"title":"Explicit","process_status":"IN_PROGRESS","backlog":[{"id":"11111111-1111-4111-8111-111111111111","kind":"backlog","title":"Routed discovery","disposition":"ROUTED to 42","disposition_ref":"USER:2026-10-22:plan-save","role":"routed_to","raised_at":"2026-10-21T10:00:00Z","raised_by":"raiser@example.com"}]}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "get", "42", "--fields", "backlog", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var selected map[string]any
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatal(err)
	}
	backlog, ok := selected["backlog"].([]any)
	if len(selected) != 1 || !ok || len(backlog) != 1 {
		t.Fatalf("--fields backlog must pass the list through: %#v", selected)
	}
	if row := backlog[0].(map[string]any); row["id"] != "11111111-1111-4111-8111-111111111111" || row["role"] != "routed_to" || row["disposition"] != "ROUTED to 42" {
		t.Fatalf("unexpected backlog row: %#v", row)
	}

	out, err = runRoot(t, "epics", "get", "42", "--full", "--json")
	if err != nil || !strings.Contains(out, "Routed discovery") {
		t.Fatalf("--full must include the backlog records: %q err=%v", out, err)
	}

	out, err = runRoot(t, "epics", "get", "42", "--fields", "backlog")
	if err != nil || !strings.Contains(out, "backlog: [{") || !strings.Contains(out, `"title":"Routed discovery"`) {
		t.Fatalf("text output must print backlog as a JSON array field: %q err=%v", out, err)
	}
}

func TestEpicUpdateHelpNamesSharedContextEntryShapes(t *testing.T) {
	out, err := runRoot(t, "epics", "update", "--help")
	if err != nil {
		t.Fatal(err)
	}
	ref := referenceSection(t, renderCLIReference(rootCmd), "modernpath epics update")
	for name, text := range map[string]string{"help": out, "reference": ref} {
		flat := strings.Join(strings.Fields(text), " ")
		for _, want := range []string{
			`"decisions":[{"statement":`,
			`"assumptions":[{"statement":`,
			`"open_questions":[{"question":`,
			"each decision needs a non-empty statement",
		} {
			if !strings.Contains(flat, want) {
				t.Errorf("epics update %s does not name %q", name, want)
			}
		}
	}
}
