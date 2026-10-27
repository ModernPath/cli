package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEpicRetireUsesScopedSoftDelete(t *testing.T) {
	for _, tc := range []struct {
		name, detail, response, wantError string
		status, deletes                   int
	}{
		{"success", `{"id":42,"system_id":1}`, `{"data":{"deleted":true}}`, "", 200, 1},
		{"foreign System", `{"id":42,"system_id":2}`, `{"data":{"deleted":true}}`, "bound System", 200, 0},
		{"owned requirement", `{"id":42,"system_id":1}`, `{"error":"Complete the Epic's approved requirement change before retiring it"}`, "approved requirement change", 409, 1},
		{"missing receipt", `{"id":42,"system_id":1}`, `{"data":{}}`, "confirm retirement", 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deletes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/systems" {
					healthyProbe(w, r)
					return
				}
				if r.URL.Path != "/api/work/epics/42" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if r.Method == http.MethodGet {
					_, _ = w.Write([]byte(`{"data":` + tc.detail + `}`))
					return
				}
				if r.Method != http.MethodDelete {
					t.Errorf("expected DELETE, got %s", r.Method)
				}
				deletes++
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			cobraWorkspace(t, server)
			out, err := runRoot(t, "epics", "retire", "42", "--json")
			if tc.wantError == "" {
				if err != nil || !strings.Contains(out, `"operation": "retired"`) {
					t.Fatalf("retire = %q, %v", out, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("expected %q, got %v", tc.wantError, err)
			}
			if deletes != tc.deletes {
				t.Fatalf("DELETE count = %d, want %d", deletes, tc.deletes)
			}
		})
	}
}

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

// Epic 327 (A4), test case e7453cba (criterion 39d59354): `epics create`
// names an Initiative with `line_id`. It forwards `line_id` and `release_id`
// from the file, adds only `system_id`, refuses a `line_id` that is not a
// string before sending, prints the Release and the Initiative with its
// context in the receipt, and stops with an error naming the created Epic
// when the server answered without the Initiative (a server without this
// change ignores `line_id`). `epics get --fields covered_by` prints the
// Initiative that covers an Epic.

const a4LineID = "6f1c2a52-3d4e-4f60-8a7b-9c0d1e2f3a4b"

// The route's accepted list: system_id, line_id and the client fields of
// `epics update` (Core.Planning.EpicRecord).
var a4AcceptedEpicKeys = map[string]bool{
	"system_id": true, "line_id": true,
	"title": true, "description": true, "scope": true, "non_goals": true, "outcome_source": true,
	"impact_assessment": true, "shared_context": true, "start_date": true, "target_date": true,
	"severity": true, "category": true, "target_profile_id": true, "assignee_id": true, "release_id": true,
}

// a4CreateServer answers POST /api/work/epics with `answer` and records each body.
func a4CreateServer(t *testing.T, answer string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var received []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/work/epics" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		received = append(received, body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(server.Close)
	return server, &received
}

func a4EpicFile(t *testing.T, body string) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "epic.json")
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestA4EpicsCreateForwardsTheInitiativeAndPrintsItInTheReceipt(t *testing.T) {
	server, received := a4CreateServer(t, `{"data":{"id":42,"target_architecture_id":1,"title":"Wallet checkout","process_status":"PROPOSED","release_id":12,"initiative":{"line_id":"`+a4LineID+`","text":"Pay in one step","context":"PAY","context_name":"Payments"}}}`)
	cobraWorkspace(t, server)
	file := a4EpicFile(t, `{"title":"Wallet checkout","line_id":"`+a4LineID+`","release_id":12}`)

	out, err := runRoot(t, "epics", "create", "--file", file, "--json")
	if err != nil {
		t.Fatalf("create: %s %v", out, err)
	}
	if len(*received) != 1 {
		t.Fatalf("expected one request, got %d", len(*received))
	}
	body := (*received)[0]
	if body["line_id"] != a4LineID || body["release_id"] != float64(12) || body["system_id"] != float64(1) || body["title"] != "Wallet checkout" || len(body) != 4 {
		t.Fatalf("epics create must forward line_id and release_id and add only system_id: %#v", body)
	}

	var receipt map[string]any
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		t.Fatalf("receipt is not JSON: %q", out)
	}
	if receipt["operation"] != "created" || receipt["id"] != float64(42) || receipt["title"] != "Wallet checkout" {
		t.Fatalf("receipt lost the Epic: %#v", receipt)
	}
	if receipt["release_id"] != float64(12) {
		t.Fatalf("receipt must carry the Release: %#v", receipt)
	}
	initiative, ok := receipt["initiative"].(map[string]any)
	if !ok {
		t.Fatalf("receipt must carry the Initiative: %#v", receipt)
	}
	if initiative["line_id"] != a4LineID || initiative["text"] != "Pay in one step" || initiative["context"] != "PAY" || initiative["context_name"] != "Payments" {
		t.Fatalf("receipt's Initiative must carry line_id, text, context and context_name: %#v", initiative)
	}

	out, err = runRoot(t, "epics", "create", "--file", file)
	if err != nil {
		t.Fatalf("text create: %s %v", out, err)
	}
	for _, want := range []string{"release_id: 12", "Pay in one step", "PAY", "Payments"} {
		if !strings.Contains(out, want) {
			t.Errorf("text receipt does not name %q: %q", want, out)
		}
	}
}

func TestA4EpicsCreateRefusesALineIDThatIsNotAStringBeforeSending(t *testing.T) {
	server, received := a4CreateServer(t, `{"data":{"id":42,"target_architecture_id":1,"title":"Wallet checkout"}}`)
	cobraWorkspace(t, server)

	for _, value := range []string{`17`, `null`, `{"id":"x"}`, `["` + a4LineID + `"]`} {
		file := a4EpicFile(t, `{"title":"Wallet checkout","line_id":`+value+`}`)
		out, err := runRoot(t, "epics", "create", "--file", file, "--json")
		if err == nil || !strings.Contains(err.Error(), "line_id") {
			t.Errorf("line_id %s must be refused naming line_id: out=%q err=%v", value, out, err)
		}
	}
	if len(*received) != 0 {
		t.Fatalf("a refused line_id must send nothing, the server received %d requests: %#v", len(*received), *received)
	}
}

func TestA4EpicsCreateStopsWhenTheServerDidNotLinkTheInitiative(t *testing.T) {
	server, received := a4CreateServer(t, `{"data":{"id":42,"target_architecture_id":1,"title":"Wallet checkout","process_status":"PROPOSED","release_id":12}}`)
	cobraWorkspace(t, server)
	file := a4EpicFile(t, `{"title":"Wallet checkout","line_id":"`+a4LineID+`"}`)

	out, err := runRoot(t, "epics", "create", "--file", file, "--json")
	if len(*received) != 1 {
		t.Fatalf("expected one request, got %d", len(*received))
	}
	if err == nil {
		t.Fatalf("epics create must fail when the answer names no Initiative, it printed %q", out)
	}
	message := err.Error()
	for _, want := range []string{"42", "line_id"} {
		if !strings.Contains(message, want) {
			t.Errorf("the error must name %q: %q", want, message)
		}
	}
	if !strings.Contains(strings.ToLower(message), "server") {
		t.Errorf("the error must say that line_id needs a server with this change: %q", message)
	}
	if strings.Contains(out, `"operation"`) || strings.Contains(out, "operation:") {
		t.Fatalf("no receipt may be printed for an Epic the server did not link: %q", out)
	}
}

func TestA4EpicsCreateSendsNoKeyOutsideTheAcceptedList(t *testing.T) {
	server, received := a4CreateServer(t, `{"data":{"id":42,"target_architecture_id":1,"title":"Account access","process_status":"PROPOSED"}}`)
	cobraWorkspace(t, server)
	file := a4EpicFile(t, `{"title":"Account access","description":"Allow users to sign in","scope":"Sign-in flow","non_goals":"Account recovery","release_id":12}`)

	if out, err := runRoot(t, "epics", "create", "--file", file, "--json"); err != nil {
		t.Fatalf("create: %s %v", out, err)
	}
	if len(*received) != 1 {
		t.Fatalf("expected one request, got %d", len(*received))
	}
	if _, wrapped := (*received)[0]["epic"]; wrapped {
		t.Fatalf("the Epic object is sent unwrapped: %#v", (*received)[0])
	}
	for key := range (*received)[0] {
		if !a4AcceptedEpicKeys[key] {
			t.Errorf("epics create sent %q, a key POST /api/work/epics refuses", key)
		}
	}
}

func TestA4EpicsGetPrintsTheInitiativeThatCoversTheEpic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"title":"Wallet checkout","process_status":"PROPOSED","covered_by":{"line_id":"` + a4LineID + `","text":"Pay in one step"}}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "get", "42", "--fields", "covered_by", "--json")
	if err != nil {
		t.Fatalf("epics get --fields covered_by: %q %v", out, err)
	}
	var selected map[string]any
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatal(err)
	}
	covered, ok := selected["covered_by"].(map[string]any)
	if len(selected) != 1 || !ok || covered["line_id"] != a4LineID || covered["text"] != "Pay in one step" {
		t.Fatalf("--fields covered_by must print the covering Initiative: %#v", selected)
	}
}
