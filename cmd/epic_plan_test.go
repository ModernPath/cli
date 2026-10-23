package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// planServer binds a workspace to a test server that records each Epic plan
// request body and answers with an Epic in the given state.
func planServer(t *testing.T, state string) *[]map[string]any {
	t.Helper()
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"process_status":"` + state + `"}}`))
	}))
	t.Cleanup(server.Close)
	cobraWorkspace(t, server)
	return &bodies
}

// presentationBundle stands in for the completion bundle that epics gates
// presentation returns; presentationFile saves it as the human saw it.
const presentationBundle = `{"review":{"plan":{"body":"Reviewed plan"}},"reviews":[{"phase":"implementation"}],"readiness":{"ready":true,"blockers":[]}}`

func presentationFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "presentation.json")
	if err := os.WriteFile(file, []byte(presentationBundle), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

// SR-1638.1: the return posts the human's reason and the completion bundle read
// from --file, and reports the operation it performed.
func TestSR1638_1ReturnPostsTheReasonAndTheBundle(t *testing.T) {
	var method, path string
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		method, path = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":1,"process_status":"IN_PROGRESS"}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "plan", "return-to-implementation", "42", "--file", presentationFile(t), "--reason", "USER:2026-10-20: not yet")
	if err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/api/work/epics/42/return-to-implementation" {
		t.Fatalf("return sent %s %s, want POST /api/work/epics/42/return-to-implementation", method, path)
	}
	var want map[string]any
	if err := json.Unmarshal([]byte(presentationBundle), &want); err != nil {
		t.Fatal(err)
	}
	presentation, _ := body["presentation"].(map[string]any)
	if body["reason"] != "USER:2026-10-20: not yet" || !reflect.DeepEqual(presentation, want) {
		t.Fatalf("return must send the reason and the file's bundle as presentation, got %#v", body)
	}
	for _, want := range []string{"operation: implementation-returned", "id: 42", "process_status: IN_PROGRESS"} {
		if !strings.Contains(out, want) {
			t.Errorf("return output lacks %q: %q", want, out)
		}
	}
}

// SR-1638.5: without --file the return fails before any request and names the
// completion bundle it needs.
func TestSR1638_5ReturnWithoutFileFailsBeforeAnyRequest(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	_, err := runRoot(t, "epics", "plan", "return-to-implementation", "42", "--reason", "USER:2026-10-20: not yet")
	if err == nil || !strings.Contains(err.Error(), "completion bundle") {
		t.Fatalf("return-to-implementation without --file must fail naming the completion bundle, got %v", err)
	}
	if len(*bodies) != 0 {
		t.Fatalf("return-to-implementation without --file sent %d requests, want none", len(*bodies))
	}
}

// SR-1638.2: a non-positive Epic ID is refused before any request.
func TestSR1638_2ReturnRefusesANonPositiveEpicID(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	_, err := runRoot(t, "epics", "plan", "return-to-implementation", "0", "--file", presentationFile(t), "--reason", "r")
	if err == nil || !strings.Contains(err.Error(), "epic ID must be a positive integer") {
		t.Fatalf("return-to-implementation 0 must fail with the Epic ID message, got %v", err)
	}
	if len(*bodies) != 0 {
		t.Fatalf("return-to-implementation 0 sent %d requests, want none", len(*bodies))
	}
}

// SR-1641.1: --agent <slug> sends the agent as the payload actor beside the reason.
func TestSR1641_1AgentFlagSendsTheActor(t *testing.T) {
	bodies := planServer(t, "IN_REVIEW")
	if _, err := runRoot(t, "epics", "plan", "submit-review", "42", "--agent", "claude-code", "--reason", "Ready"); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("submit-review made %d calls, want 1", len(*bodies))
	}
	body := (*bodies)[0]
	actor, _ := body["actor"].(map[string]any)
	if body["reason"] != "Ready" || actor["kind"] != "agent" || actor["agent_slug"] != "claude-code" || len(actor) != 2 {
		t.Fatalf("--agent claude-code must send actor {kind: agent, agent_slug: claude-code} beside the reason, got %#v", body)
	}
}

// SR-1641.2: every plan write lists --agent in its help; show, a read, does not.
func TestSR1641_2EveryPlanWriteListsAgentInItsHelp(t *testing.T) {
	for _, verb := range []string{"save", "approve-implementation", "submit-review", "return-to-implementation", "approve-completion", "show"} {
		if c, _, err := rootCmd.Find([]string{"epics", "plan", verb}); err != nil || c.Name() != verb {
			t.Errorf("epics plan %s is not a command", verb)
			continue
		}
		help, err := runRoot(t, "epics", "plan", verb, "--help")
		if err != nil || !strings.Contains(help, "--json") {
			t.Fatalf("epics plan %s --help printed no flags (err %v): %q", verb, err, help)
		}
		if listed, write := strings.Contains(help, "--agent string"), verb != "show"; listed != write {
			t.Errorf("epics plan %s --help lists --agent: %v, want %v", verb, listed, write)
		}
	}
}

// Retained behaviour (Epic 292): without --agent the payload carries no actor,
// so the server attributes the write to the signed-in person.
func TestEpicPlanWriteWithoutAgentSendsNoActor(t *testing.T) {
	bodies := planServer(t, "IN_REVIEW")
	if _, err := runRoot(t, "epics", "plan", "submit-review", "42", "--reason", "Ready"); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("submit-review made %d calls, want 1", len(*bodies))
	}
	if _, has := (*bodies)[0]["actor"]; has || (*bodies)[0]["reason"] != "Ready" {
		t.Fatalf("without --agent the payload must carry the reason and no actor key: %#v", (*bodies)[0])
	}
}

func TestEpicReviewedPlanPreservesNativeBigintEndpoints(t *testing.T) {
	const input = `{"body":"Reviewed plan","requirements":[],"traces":[{"target_type":"epic","target_id":9223372036854775807}]}`
	plan, err := readEpicPlan(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"target_id":9223372036854775807`) {
		t.Fatalf("reviewed native endpoint changed in transport: %s", encoded)
	}
}

func TestEpicPlanShowExposesSourceFilePaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":42,"source_files":[{"id":"file-id","repository_key":"new-service","path":"src/api.ex"}]}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	command.SetArgs([]string{"show", "42", "--fields", "source_files", "--json"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var read struct {
		SourceFiles []struct {
			ID         string `json:"id"`
			Repository string `json:"repository_key"`
			Path       string `json:"path"`
		} `json:"source_files"`
	}
	if err := json.Unmarshal(output.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	if len(read.SourceFiles) != 1 || read.SourceFiles[0].ID != "file-id" || read.SourceFiles[0].Repository != "new-service" || read.SourceFiles[0].Path != "src/api.ex" {
		t.Fatalf("missing source-file identity: %s", output.String())
	}
}

func TestEpicPlanSaveForwardsEmptyDraftAndKeepsSuccessfulJSON(t *testing.T) {
	input := `{"body":"Draft before requirements", "requirements":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/work/epics/42/plan" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		plan, ok := request["plan"].(map[string]any)
		if !ok || plan["body"] != "Draft before requirements" || len(plan["requirements"].([]any)) != 0 {
			t.Errorf("forwarded plan = %#v", request["plan"])
		}
		_, _ = w.Write([]byte(`{"data":{"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	cmd := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	cmd.SetArgs([]string{"save", "42", "--json"})
	cmd.SetIn(strings.NewReader(input))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
		t.Fatalf("successful response is not JSON: %s; %v", output.String(), err)
	}
	if receipt["operation"] != "saved" || receipt["process_status"] != "PROPOSED" {
		t.Fatalf("successful response JSON = %s", output.String())
	}
}

func TestEpicReturnToPlanningRetainsHumanDecisionAndExactPlan(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/return-to-planning" ||
			input["reason"] != "USER: correct verification scope" || input["plan"].(map[string]any)["body"] != "Exact approved scope" {
			t.Errorf("changed return decision: %s %s %#v", r.Method, r.URL.Path, input)
		}
		_, _ = w.Write([]byte(`{"data":{"id":287,"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"return-to-planning", "287", "--file", "-", "--reason", "USER: correct verification scope"})
	command.SetIn(strings.NewReader(`{"body":"Exact approved scope"}`))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err != nil || requests != 1 || !strings.Contains(output.String(), "PROPOSED") {
		t.Fatalf("return to planning: err=%v requests=%d output=%s", err, requests, output.String())
	}
}

func TestEpicReturnToPlanningRetainsAgentAttribution(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/return-to-planning" ||
			input["actor"].(map[string]any)["agent_slug"] != "planning-builder" || input["reason"] != "USER: correct verification scope" || input["plan"].(map[string]any)["body"] != "Exact approved scope" {
			t.Errorf("changed return decision: %s %s %#v", r.Method, r.URL.Path, input)
		}
		_, _ = w.Write([]byte(`{"data":{"id":287,"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"return-to-planning", "287", "--file", "-", "--reason", "USER: correct verification scope", "--agent", "planning-builder"})
	command.SetIn(strings.NewReader(`{"body":"Exact approved scope"}`))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err != nil || requests != 1 || !strings.Contains(output.String(), "PROPOSED") {
		t.Fatalf("return to planning: err=%v requests=%d output=%s", err, requests, output.String())
	}
}

func TestEpicPlanCoverageRefusalIsReadableAndNonzero(t *testing.T) {
	const message = "A selected requirement is already covered by another initiative."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"already_on_roadmap","ids":["00e93572-d3ec-454e-b07e-2be7f29232f7"],"message":"` + message + `"}`))
	}))
	defer server.Close()
	cmd := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	cmd.SetArgs([]string{"save", "42"})
	cmd.SetIn(strings.NewReader(`{"body":"Draft","requirements":[]}`))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("coverage refusal = %v", err)
	}
}

func TestEpicApprovalInputIsOneCompletePlanObject(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{"body":"One"} {"body":"Another"}`, `{"body":"Incomplete"`, `{"body":"One"} trailing`} {
		t.Run(input, func(t *testing.T) {
			if _, err := readEpicPlan(strings.NewReader(input)); err == nil {
				t.Fatal("accepted an incomplete or ambiguous reviewed plan")
			}
		})
	}
}

func TestEpicCompletionApprovalRequiresAnExplicitPresentationFile(t *testing.T) {
	loads := 0
	command := newEpicPlanCommand(func() (*cliEnv, error) {
		loads++
		return nil, errors.New("unexpected server access")
	})
	command.SetArgs([]string{"approve-completion", "42"})
	command.SetIn(strings.NewReader("stdin must not be read"))

	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "--file") {
		t.Fatalf("missing presentation file error = %v", err)
	}
	if loads != 0 {
		t.Fatalf("loaded CLI environment %d times before validating required input", loads)
	}
}

// SR-1602: transport only the decision bundle shown to the human; never refresh it.
func TestEpicImplementationApprovalRetainsDecisionPresentation(t *testing.T) {
	const bundle = `{"plan":{"body":"A","requirements":[]},"decisions":[{"actor_id":9223372036854775807,"content":{"answer":"Choose A","source":"USER: A"}}]}`
	file := filepath.Join(t.TempDir(), "reviewed.json")
	if err := os.WriteFile(file, []byte(bundle), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/approve-implementation" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var input map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		var got, want any
		decoder := json.NewDecoder(bytes.NewReader(input["presentation"]))
		decoder.UseNumber()
		if err := decoder.Decode(&got); err != nil {
			t.Errorf("missing presentation: %v", err)
		}
		decoder = json.NewDecoder(strings.NewReader(bundle))
		decoder.UseNumber()
		_ = decoder.Decode(&want)
		actual, _ := json.Marshal(got)
		expected, _ := json.Marshal(want)
		if !bytes.Equal(actual, expected) {
			t.Errorf("changed decision bundle: %s", actual)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"process_status": "IN_PROGRESS"}})
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"approve-implementation", "287", "--file", "-", "--presentation-file", file})
	command.SetIn(strings.NewReader(`{"body":"A","requirements":[]}`))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("approval fetched unseen content: %d calls", calls)
	}
}

// Epic 290 (SR6): `epics plan history` summarizes each stored revision by
// default and prints the served items unchanged with --full.
func TestEpicPlanHistorySummarizesRevisionsAndForwardsPaging(t *testing.T) {
	var gotQuery string
	status := 200
	plan := map[string]any{
		"body": "Plan v2 body",
		"requirements": []any{
			map[string]any{"kind": "user_requirement", "id": "u1", "content": map[string]any{}, "criteria": []any{map[string]any{"id": "c1"}, map[string]any{"id": "c2"}}},
			map[string]any{"kind": "system_requirement", "id": "s1", "content": map[string]any{}, "criteria": []any{map[string]any{"id": "c3"}}},
		},
		"traces": []any{map[string]any{"id": "t1"}},
	}
	previous := map[string]any{
		"body":         "Plan v1",
		"requirements": []any{map[string]any{"kind": "user_requirement", "id": "u1", "content": map[string]any{}, "criteria": []any{map[string]any{"id": "c1"}}}},
		"traces":       []any{},
	}
	items := []any{
		map[string]any{"id": "e2", "action": "updated", "inserted_at": "2026-10-21T10:00:00Z", "user_id": 7, "user_name": "Editor",
			"details": map[string]any{"description": "Epic content updated.", "fields": []any{"title"}, "previous": map[string]any{"title": "Old"}, "changes": map[string]any{"title": "New"}}},
		map[string]any{"id": "e1", "action": "plan_saved", "inserted_at": "2026-10-21T09:00:00Z", "user_id": 7, "user_name": "Editor",
			"details": map[string]any{"description": "Epic plan saved.", "comment": "second draft", "plan": plan, "previous_plan": previous}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.URL.Path != "/api/work/epics/42/plan/history" || r.Method != http.MethodGet {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		gotQuery = r.URL.RawQuery
		w.WriteHeader(status)
		if status != 200 {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "Epic not found"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items, "total_count": 2, "has_more": false, "limit": 5, "offset": 10}})
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "plan", "history", "42", "--json", "--limit", "5", "--offset", "10")
	if err != nil {
		t.Fatalf("history: %s %v", out, err)
	}
	if gotQuery != "limit=5&offset=10" {
		t.Fatalf("paging not forwarded: %q", gotQuery)
	}
	var page struct {
		Items      []map[string]any `json:"items"`
		TotalCount int              `json:"total_count"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil || page.TotalCount != 2 || len(page.Items) != 2 {
		t.Fatalf("history output: %s %v", out, err)
	}
	edit, saved := page.Items[0], page.Items[1]
	if edit["action"] != "updated" || edit["at"] != "2026-10-21T10:00:00Z" || edit["user_name"] != "Editor" {
		t.Fatalf("updated item: %#v", edit)
	}
	if fields, _ := json.Marshal(edit["fields"]); string(fields) != `["title"]` || edit["saved_plan"] != nil {
		t.Fatalf("updated item prints fields, not a plan summary: %#v", edit)
	}
	counts := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if saved["comment"] != "second draft" || saved["id"] != "e1" {
		t.Fatalf("plan_saved item: %#v", saved)
	}
	if got := counts(saved["saved_plan"]); got != `{"body_chars":12,"criteria":3,"requirements":2,"traces":1}` {
		t.Fatalf("saved_plan counts: %s", got)
	}
	if got := counts(saved["delta"]); got != `{"body_chars":5,"criteria":2,"requirements":1,"traces":1}` {
		t.Fatalf("delta counts: %s", got)
	}
	if _, has := saved["revision"]; has || strings.Contains(out, "Plan v2 body") {
		t.Fatalf("summary leaked the body or named a revision: %s", out)
	}

	out, err = runRoot(t, "epics", "plan", "history", "42", "--json", "--full")
	if err != nil {
		t.Fatalf("history --full: %s %v", out, err)
	}
	var full struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &full); err != nil || len(full.Items) != 2 {
		t.Fatalf("full output: %s %v", out, err)
	}
	details, _ := full.Items[1]["details"].(map[string]any)
	if counts(details["plan"]) != counts(plan) || counts(details["previous_plan"]) != counts(previous) {
		t.Fatalf("--full changed the served details: %#v", details)
	}
	if counts(full.Items[0]["details"].(map[string]any)["fields"]) != `["title"]` {
		t.Fatalf("--full dropped the updated event's fields: %#v", full.Items[0])
	}

	status = 404
	if _, err := runRoot(t, "epics", "plan", "history", "42"); err == nil || !strings.Contains(err.Error(), "epic plan history (server 404)") {
		t.Fatalf("404 is not a named server refusal: %v", err)
	}
}

func TestPlanningReturnHistoryDoesNotClaimPlanContentChanged(t *testing.T) {
	summary := planHistorySummary(map[string]any{
		"id": "return-event", "action": "returned_to_planning",
		"details": map[string]any{"plan": map[string]any{"body": "Retained approved plan"}},
	})
	if summary["action"] != "returned_to_planning" || summary["delta"] != nil {
		t.Fatalf("planning return must retain its action without a content delta: %#v", summary)
	}
}
