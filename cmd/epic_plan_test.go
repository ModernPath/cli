package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestExistingBehaviorPlanDeclarationReachesServer(t *testing.T) {
	bodies := planServer(t, "PROPOSED")
	file := filepath.Join(t.TempDir(), "plan.json")
	input := `{"body":"Verify implemented behavior","verification":{"mode":"existing_behavior","existing_behavior_reason":"Already implemented"}}`
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "epics", "plan", "save", "42", "--file", file); err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("expected one save request: %#v", *bodies)
	}
	plan := (*bodies)[0]["plan"].(map[string]any)
	verification := plan["verification"].(map[string]any)
	if verification["mode"] != "existing_behavior" || verification["existing_behavior_reason"] != "Already implemented" {
		t.Fatalf("declaration was changed in transport: %#v", verification)
	}
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
func TestReturnPostsOnlyTheHumanDecision(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	out, err := runRoot(t, "epics", "plan", "return-to-implementation", "42", "--reason", "USER: continue")
	if err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 1 || len((*bodies)[0]) != 1 || (*bodies)[0]["reason"] != "USER: continue" {
		t.Fatalf("decision=%#v", *bodies)
	}
	if !strings.Contains(out, "implementation-returned") {
		t.Fatal(out)
	}
}

func TestReturnRejectsRemovedBundleFlagBeforeAnyRequest(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	_, err := runRoot(t, "epics", "plan", "return-to-implementation", "42", "--file", "obsolete.json", "--reason", "USER: continue")
	if err == nil || !strings.Contains(err.Error(), "unknown flag") || len(*bodies) != 0 {
		t.Fatalf("err=%v requests=%#v", err, *bodies)
	}
}

func TestSR1638_2ReturnRefusesANonPositiveEpicID(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	_, err := runRoot(t, "epics", "plan", "return-to-implementation", "0", "--reason", "r")
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
			input["reason"] != "USER: correct verification scope" || input["plan"] != nil || input["presentation"] != nil {
			t.Errorf("changed return decision: %s %s %#v", r.Method, r.URL.Path, input)
		}
		_, _ = w.Write([]byte(`{"data":{"id":287,"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"return-to-planning", "287", "--reason", "USER: correct verification scope", "--json"})
	command.SetIn(strings.NewReader(`{"body":"Exact approved scope"}`))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&bytes.Buffer{})
	if err := command.Execute(); err != nil || requests != 1 || !strings.Contains(output.String(), "PROPOSED") {
		t.Fatalf("return to planning: err=%v requests=%d output=%s", err, requests, output.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil || receipt["operation"] != "returned-to-planning" {
		t.Fatalf("return receipt operation = %v; err=%v; output=%s", receipt["operation"], err, output.String())
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
			input["actor"].(map[string]any)["agent_slug"] != "planning-builder" || input["reason"] != "USER: correct verification scope" || input["plan"] != nil || input["presentation"] != nil {
			t.Errorf("changed return decision: %s %s %#v", r.Method, r.URL.Path, input)
		}
		_, _ = w.Write([]byte(`{"data":{"id":287,"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"return-to-planning", "287", "--reason", "USER: correct verification scope", "--agent", "planning-builder"})
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

func TestEpicCompletionRejectsRemovedPresentationFlag(t *testing.T) {
	loads := 0
	command := newEpicPlanCommand(func() (*cliEnv, error) { loads++; return nil, errors.New("unexpected access") })
	command.SetArgs([]string{"approve-completion", "42", "--presentation-file", "obsolete.json"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "unknown flag") || loads != 0 {
		t.Fatalf("err=%v loads=%d", err, loads)
	}
}

func TestEpicImplementationApprovalSendsOnlyDecision(t *testing.T) {
	bodies := planServer(t, "IN_PROGRESS")
	_, err := runRoot(t, "epics", "plan", "approve-implementation", "42", "--reason", "USER: implement")
	if err != nil {
		t.Fatal(err)
	}
	if len(*bodies) != 1 || len((*bodies)[0]) != 1 || (*bodies)[0]["reason"] != "USER: implement" {
		t.Fatalf("decision=%#v", *bodies)
	}
}

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
	if saved["saved_plan"] != nil || saved["delta"] != nil {
		t.Fatalf("unexpected copied plan summary: %#v", saved)
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

// Epic 342: the plan help and both copies of the CLI guide describe
// remove_traces, that a trace left out of a plan is kept, and that a check keeps
// its id by name.
func TestEpicsPlanHelpAndCLIGuideDescribeRemoveTracesKeptTracesAndCheckIDsKeptByName(t *testing.T) {
	help, err := runRoot(t, "epics", "plan", "--help")
	if err != nil {
		t.Fatal(err)
	}
	docs := map[string]string{"epics plan --help": help}
	for _, path := range []string{"../../../docs/cli.md", "../internal/kit/assets/cli-guide/cli.md"} {
		body, err := os.ReadFile(path)
		// docs/cli.md lies outside this module; the core image tests the module
		// alone. Its identical kit copy is always checked.
		if os.IsNotExist(err) && path == "../../../docs/cli.md" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		docs[path] = string(body)
	}
	for name, text := range docs {
		normalized := strings.ToLower(strings.Join(strings.Fields(text), " "))
		for _, want := range []string{"remove_traces", "left out", "is kept", "planning gate", "keeps its id by name"} {
			if !strings.Contains(normalized, want) {
				t.Errorf("%s does not say %q", name, want)
			}
		}
		for _, obsolete := range []string{
			"traces left out that existed at approval are removed at completion",
			"a new check id is assigned on save",
		} {
			if strings.Contains(normalized, obsolete) {
				t.Errorf("%s still says %q", name, obsolete)
			}
		}
	}
}

// TestCase (Epic 344): "return-to-planning sends repeated --carry-decision ids
// as carry_decisions".
func TestReturnToPlanningSendsRepeatedCarryDecisionIdsAsCarryDecisions(t *testing.T) {
	const first = "6f1c2b8e-1d2a-4c3b-9e4f-5a6b7c8d9e01"
	const second = "0a9b8c7d-6e5f-4a3b-8c2d-1e0f9a8b7c62"
	run := func(t *testing.T, args ...string) ([]map[string]any, error) {
		t.Helper()
		var bodies []map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input map[string]any
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.URL.Path != "/api/work/epics/287/return-to-planning" {
				t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			}
			bodies = append(bodies, input)
			_, _ = w.Write([]byte(`{"data":{"id":287,"process_status":"PROPOSED"}}`))
		}))
		defer server.Close()
		command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
		command.SetArgs(append([]string{"return-to-planning", "287", "--reason", "USER: keep the decisions"}, args...))
		command.SetIn(strings.NewReader(""))
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		err := command.Execute()
		return bodies, err
	}

	t.Run("repeated flag sends the ids in flag order", func(t *testing.T) {
		bodies, err := run(t, "--carry-decision", first, "--carry-decision", second)
		if err != nil {
			t.Fatalf("return with --carry-decision: %v", err)
		}
		if len(bodies) != 1 {
			t.Fatalf("requests = %d, want 1", len(bodies))
		}
		got, ok := bodies[0]["carry_decisions"].([]any)
		if !ok || len(got) != 2 || got[0] != first || got[1] != second {
			t.Fatalf("carry_decisions = %#v, want [%s %s]", bodies[0]["carry_decisions"], first, second)
		}
		if bodies[0]["reason"] != "USER: keep the decisions" {
			t.Fatalf("reason = %#v", bodies[0]["reason"])
		}
	})

	t.Run("without the flag the body has no carry_decisions key", func(t *testing.T) {
		bodies, err := run(t)
		if err != nil || len(bodies) != 1 {
			t.Fatalf("return without --carry-decision: err=%v requests=%d", err, len(bodies))
		}
		if _, present := bodies[0]["carry_decisions"]; present {
			t.Fatalf("body without --carry-decision has carry_decisions: %#v", bodies[0])
		}
	})

	t.Run("a value that is not a UUID fails before any request", func(t *testing.T) {
		bodies, err := run(t, "--carry-decision", "not-a-uuid")
		if err == nil {
			t.Fatal("a non-UUID --carry-decision was accepted")
		}
		if strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("--carry-decision is not a flag of return-to-planning: %v", err)
		}
		if len(bodies) != 0 {
			t.Fatalf("requests = %d, want 0 before a valid --carry-decision", len(bodies))
		}
	})
}

// Epic 380, SR-4653-0 criterion d3c8893a: the save help shows the plan's
// retire key with its {kind, id, reason} shape, and the approve-completion
// help says a retired requirement becomes an OBSOLETE version.
func TestEpicPlanSaveHelpShowsRetireAndCompletionHelpNamesTheObsoleteVersion(t *testing.T) {
	normalize := func(text string) string { return strings.Join(strings.Fields(text), " ") }
	shape := `"retire"?: [{"kind": "user_requirement" | "system_requirement", "id": "<uuid of a published requirement`
	for _, args := range [][]string{{"epics", "plan", "--help"}, {"epics", "plan", "save", "--help"}} {
		help, err := runRoot(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		text := normalize(help)
		for _, want := range []string{`"retire"`, `"kind"`, `"id"`, `"reason"`} {
			if !strings.Contains(text, want) {
				t.Errorf("%s does not show %s", strings.Join(args, " "), want)
			}
		}
		if !strings.Contains(text, "retire") || !strings.Contains(text, "remove_traces") {
			t.Errorf("%s does not name retire and remove_traces", strings.Join(args, " "))
		}
	}
	if !strings.Contains(normalize(epicPlanHelp), shape) {
		t.Errorf("the plan help does not show the retire shape %q", shape)
	}
	if !strings.Contains(normalize(epicPlanOperationHelp["save"]), `"retire": [{"kind", "id", "reason"}]`) {
		t.Errorf("the save help does not show retire as {kind, id, reason}: %q", epicPlanOperationHelp["save"])
	}
	completion, err := runRoot(t, "epics", "plan", "approve-completion", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(normalize(completion), "Each requirement the plan retires becomes a new OBSOLETE version") {
		t.Errorf("approve-completion help does not say retired requirements become OBSOLETE versions:\n%s", completion)
	}
}
