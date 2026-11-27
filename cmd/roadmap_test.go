package cmd

// Epic 324 (A1), test case 98066857: `modernpath roadmap show` reads the
// Roadmap of the bound System and `modernpath roadmap apply --file <plan.json>
// [--request-id <id>] [--agent <name>] [--dry-run]` applies a Roadmap plan
// from a file, on the routes under the System that need no Chat session. The
// request id is the file's request_id, else --request-id, else derived from
// the file's operations; a --request-id that differs from the file's is
// refused before any request. A replay says nothing was written and that a new
// request id applies the file again; --dry-run sends dry_run and prints each
// operation's summary and that nothing was written. Both refuse without a
// bound System.
//
// The tests drive the root command and use no new symbol, so on a binary
// without the roadmap group they build, the command is unknown, and the
// assertions on the requests and the output fail.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

type roadmapRequest struct {
	method, path, query string
	body                map[string]any
}

// roadmapServer answers the reachability probe and records every other
// request, replying with status and body.
func roadmapServer(t *testing.T, status int, body string) (*httptest.Server, *[]roadmapRequest) {
	t.Helper()
	seen := []roadmapRequest{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		var decoded map[string]any
		_ = json.NewDecoder(r.Body).Decode(&decoded)
		seen = append(seen, roadmapRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, body: decoded})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func writeRoadmapPlan(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

const roadmapReadReply = `{"data":{"system_id":1,"plan_state":"changed_since_agreement",
"contexts":[{"code":"PAY","name":"Payments"}],
"releases":[{"id":7,"name":"Spring","status":"active","initiatives":[
  {"id":"11111111-1111-4111-8111-111111111111","text":"Card wallet","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[41,42]}]}],
"no_release":{"now":[],"next":[
  {"id":"22222222-2222-4222-8222-222222222222","text":"Points for every order","horizon":"next","context":"Loyalty","new_context":true,"work_kind":"new_work","release_id":null,"epic_ids":[]}],
  "later":[]}}}`

const roadmapPlanFile = `{"operations":[{"op":"create_line","horizon":"next","text":"Points for every order","context":"Loyalty"}]}`

const roadmapReceipt = `{"data":{"replayed":false,"results":[{"op":"create_line","index":1,"line_id":"33333333-3333-4333-8333-333333333333","text":"Points for every order"}],
"new_contexts":["Loyalty"],"actor":{"id":5,"name":"Pia Planner"},"agent":"claude-code"}}`

func requireOneRoadmapRequest(t *testing.T, seen *[]roadmapRequest, method, path string) roadmapRequest {
	t.Helper()
	if len(*seen) != 1 {
		t.Fatalf("requests = %#v; want one %s %s", *seen, method, path)
	}
	got := (*seen)[0]
	if got.method != method || got.path != path {
		t.Fatalf("request = %s %s; want %s %s", got.method, got.path, method, path)
	}
	return got
}

func TestRoadmapShowPrintsReleasesThenInitiativesWithNoReleaseByHorizon(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapReadReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")

	for _, want := range []string{"Spring", "Card wallet", "41", "42", "Points for every order", "Loyalty", "new context"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Payments") && !strings.Contains(out, "PAY") {
		t.Errorf("output lacks the context of Card wallet:\n%s", out)
	}
	if !regexp.MustCompile(`(?i)new[ _]work`).MatchString(out) {
		t.Errorf("output lacks the kind of work:\n%s", out)
	}
	if !regexp.MustCompile(`(?i)\bnext\b`).MatchString(out) {
		t.Errorf("output lacks the horizon of the Initiative with no Release:\n%s", out)
	}
	if !regexp.MustCompile(`(?i)changed[ _]since[ _]agree`).MatchString(out) {
		t.Errorf("output does not say whether the Now plan is agreed:\n%s", out)
	}
	if strings.Index(out, "Card wallet") > strings.Index(out, "Points for every order") {
		t.Errorf("the Releases and their Initiatives come before the Initiatives with no Release:\n%s", out)
	}
}

func TestRoadmapApplySendsTheFileWithADerivedRequestIDAndTheAgent(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapReceipt)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", roadmapPlanFile)

	out, err := runRoot(t, "roadmap", "apply", "--file", plan, "--agent", "claude-code")
	if err != nil {
		t.Fatalf("roadmap apply: %v\n%s", err, out)
	}
	got := requireOneRoadmapRequest(t, seen, http.MethodPost, "/api/v1/systems/1/roadmap/plan")

	var file map[string]any
	if err := json.Unmarshal([]byte(roadmapPlanFile), &file); err != nil {
		t.Fatal(err)
	}
	gotOps, _ := json.Marshal(got.body["operations"])
	wantOps, _ := json.Marshal(file["operations"])
	if string(gotOps) != string(wantOps) {
		t.Fatalf("operations = %s; want %s", gotOps, wantOps)
	}
	if got.body["agent"] != "claude-code" {
		t.Fatalf("agent = %#v", got.body["agent"])
	}
	if dry, present := got.body["dry_run"]; present && dry != false {
		t.Fatalf("dry_run = %#v on a real apply", dry)
	}
	id, _ := got.body["request_id"].(string)
	if id == "" || len(id) > 251 || strings.HasPrefix(id, "chat:") {
		t.Fatalf("request_id = %q; want a derived id of 1 to 251 bytes", id)
	}
	for _, want := range []string{"Pia Planner", "claude-code", "Loyalty", "new context"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRoadmapApplyDerivesTheSameRequestIDFromTheSameOperations(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapReceipt)
	cobraWorkspace(t, server, 1)

	apply := func(content string) string {
		t.Helper()
		plan := writeRoadmapPlan(t, "plan.json", content)
		if out, err := runRoot(t, "roadmap", "apply", "--file", plan); err != nil {
			t.Fatalf("roadmap apply: %v\n%s", err, out)
		}
		if len(*seen) == 0 {
			t.Fatal("roadmap apply sent no request")
		}
		id, _ := (*seen)[len(*seen)-1].body["request_id"].(string)
		return id
	}

	first := apply(roadmapPlanFile)
	again := apply(roadmapPlanFile)
	reformatted := apply("{\n  \"operations\": [\n    {\"text\": \"Points for every order\", \"op\": \"create_line\",\n     \"context\": \"Loyalty\", \"horizon\": \"next\"}\n  ]\n}\n")
	changed := apply(strings.Replace(roadmapPlanFile, "Points for every order", "Points for each order", 1))

	if first == "" || first != again || first != reformatted {
		t.Fatalf("the same operations derived %q, %q and %q", first, again, reformatted)
	}
	if changed == first {
		t.Fatalf("changed operations derived the same request id %q", changed)
	}
}

func TestRoadmapApplyTakesTheRequestIDFromTheFileElseTheFlag(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapReceipt)
	cobraWorkspace(t, server, 1)

	withID := writeRoadmapPlan(t, "with-id.json", `{"request_id":"batch-7","operations":[{"op":"create_line","horizon":"now","text":"Card wallet"}]}`)
	if out, err := runRoot(t, "roadmap", "apply", "--file", withID); err != nil {
		t.Fatalf("roadmap apply: %v\n%s", err, out)
	}
	if out, err := runRoot(t, "roadmap", "apply", "--file", withID, "--request-id", "batch-7"); err != nil {
		t.Fatalf("roadmap apply with the same --request-id: %v\n%s", err, out)
	}
	without := writeRoadmapPlan(t, "without-id.json", roadmapPlanFile)
	if out, err := runRoot(t, "roadmap", "apply", "--file", without, "--request-id", "batch-9"); err != nil {
		t.Fatalf("roadmap apply --request-id: %v\n%s", err, out)
	}
	if len(*seen) != 3 {
		t.Fatalf("requests = %#v", *seen)
	}
	for i, want := range []string{"batch-7", "batch-7", "batch-9"} {
		if got := (*seen)[i].body["request_id"]; got != want {
			t.Errorf("request %d request_id = %#v; want %q", i+1, got, want)
		}
	}
}

func TestRoadmapApplyRefusesARequestIDThatDiffersFromTheFile(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapReceipt)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", `{"request_id":"batch-7","operations":[{"op":"create_line","horizon":"now","text":"Card wallet"}]}`)

	out, err := runRoot(t, "roadmap", "apply", "--file", plan, "--request-id", "batch-8")
	if err == nil {
		t.Fatalf("a differing --request-id was accepted:\n%s", out)
	}
	if strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("roadmap apply is unknown: %v", err)
	}
	if !strings.Contains(err.Error(), "batch-7") || !strings.Contains(err.Error(), "batch-8") {
		t.Errorf("the refusal does not name both ids: %v", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("the refused apply reached the server: %#v", *seen)
	}
}

func TestRoadmapApplySaysAReplayWroteNothing(t *testing.T) {
	replay := strings.Replace(roadmapReceipt, `"replayed":false`, `"replayed":true`, 1)
	server, seen := roadmapServer(t, http.StatusOK, replay)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", roadmapPlanFile)

	out, err := runRoot(t, "roadmap", "apply", "--file", plan, "--agent", "claude-code")
	if err != nil {
		t.Fatalf("roadmap apply: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodPost, "/api/v1/systems/1/roadmap/plan")
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "nothing was written") || !strings.Contains(lower, "new request id") {
		t.Fatalf("a replay must say nothing was written and that a new request id applies the file again:\n%s", out)
	}
}

func TestRoadmapApplyNamesTheRequestIDOfAConflict(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusConflict, `{"error":"idempotency_conflict","request_id":"batch-7"}`)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", `{"request_id":"batch-7","operations":[{"op":"create_line","horizon":"now","text":"Card wallet, changed"}]}`)

	_, err := runRoot(t, "roadmap", "apply", "--file", plan)
	if err == nil || !strings.Contains(err.Error(), "batch-7") {
		t.Fatalf("error = %v; want a conflict that names batch-7", err)
	}
	requireOneRoadmapRequest(t, seen, http.MethodPost, "/api/v1/systems/1/roadmap/plan")
}

// SR-5099, criterion 1e4cb806: a 409 idempotency_conflict names the request
// id and says how to apply the plan under a new one.
func TestRoadmapApplyConflictOfARequestIDSaysToChangeIt(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusConflict, `{"error":"idempotency_conflict","request_id":"batch-7",
"message":"request id batch-7 was already used for a different plan; use a new request id to apply this one"}`)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", `{"request_id":"batch-7","operations":[{"op":"create_line","horizon":"now","text":"Card wallet, changed"}]}`)

	_, err := runRoot(t, "roadmap", "apply", "--file", plan)
	if err == nil {
		t.Fatal("apply succeeded; want the conflict")
	}
	for _, want := range []string{"batch-7", "already used", "request_id in the file", "--request-id"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error lacks %q: %v", want, err)
		}
	}
}

// SR-5099, criterion 5b02070e: any other 409, here the approved-Epic lock a
// link_epic meets, prints the server's code and the reason and message of
// each returned error, and never says the request id was already used. The
// first body is the plan check's (validation_error), the second the write's.
func TestRoadmapApplyPrintsTheServersReasonOfAnotherConflict(t *testing.T) {
	message := "Card wallet's approved planning basis would change. Return the Epic to planning with an explicit human decision before moving it."
	epic := `"epic":{"id":42,"title":"Card wallet"},"source_initiative_id":"11111111-1111-4111-8111-111111111111","target_initiative_id":"22222222-2222-4222-8222-222222222222"`
	bodies := map[string]string{
		"plan check": `{"error":"approved_epic_placement_locked","index":1,"reason":"planning_context_changed",` + epic + `,"message":"` + message + `",
"errors":[{"index":1,"op":"link_epic","reason":"approved_epic_placement_locked","code":"approved_epic_placement_locked","placement_reason":"planning_context_changed",` + epic + `,"message":"` + message + `"}]}`,
		"write": `{"error":"approved_epic_placement_locked","index":1,"reason":"planning_context_changed",` + epic + `,"message":"` + message + `"}`,
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			server, seen := roadmapServer(t, http.StatusConflict, body)
			cobraWorkspace(t, server, 1)
			plan := writeRoadmapPlan(t, "plan.json", `{"request_id":"move-wallet","operations":[{"op":"link_epic","line_id":"22222222-2222-4222-8222-222222222222","epic_id":42}]}`)

			_, err := runRoot(t, "roadmap", "apply", "--file", plan)
			if err == nil {
				t.Fatal("apply succeeded; want the refusal")
			}
			requireOneRoadmapRequest(t, seen, http.MethodPost, "/api/v1/systems/1/roadmap/plan")
			for _, want := range []string{"approved_epic_placement_locked", "operation 1", "planning_context_changed", message} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error lacks %q: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "already used") || strings.Contains(err.Error(), "--request-id") {
				t.Errorf("a conflict other than idempotency_conflict must not blame the request id: %v", err)
			}
		})
	}
}

// SR-5100, criterion da0b3dba: the help lists link_epic with its fields and
// says it can move an Epic from another Initiative.
func TestRoadmapApplyHelpListsLinkEpicAndItsMove(t *testing.T) {
	out, err := runRoot(t, "roadmap", "apply", "--help")
	if err != nil {
		t.Fatalf("roadmap apply --help: %v\n%s", err, out)
	}
	flat := strings.Join(strings.Fields(out), " ")
	for _, want := range []string{"link_epic", `"line_id"`, `"epic_id"`, "from another Initiative"} {
		if !strings.Contains(flat, want) {
			t.Errorf("help lacks %q:\n%s", want, out)
		}
	}
}

func TestRoadmapApplyDryRunSendsDryRunAndPrintsEachSummary(t *testing.T) {
	summary := `Add initiative \"Points for every order\" to Next, context Loyalty (new context)`
	server, seen := roadmapServer(t, http.StatusOK, `{"data":{"operations":[{"index":1,"op":"create_line","summary":"`+summary+`","release":null}],"errors":[]}}`)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "plan.json", roadmapPlanFile)

	out, err := runRoot(t, "roadmap", "apply", "--file", plan, "--dry-run")
	if err != nil {
		t.Fatalf("roadmap apply --dry-run: %v\n%s", err, out)
	}
	got := requireOneRoadmapRequest(t, seen, http.MethodPost, "/api/v1/systems/1/roadmap/plan")
	if got.body["dry_run"] != true {
		t.Fatalf("dry_run = %#v; want true", got.body["dry_run"])
	}
	if !strings.Contains(out, `Add initiative "Points for every order" to Next, context Loyalty (new context)`) {
		t.Errorf("output lacks the operation's summary:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "nothing was written") {
		t.Errorf("output does not say nothing was written:\n%s", out)
	}
}

func TestRoadmapCommandsRefuseWithoutABoundSystem(t *testing.T) {
	for _, args := range [][]string{
		{"roadmap", "show"},
		{"roadmap", "apply", "--file", "plan.json"},
	} {
		t.Run(strings.Join(args[:2], " "), func(t *testing.T) {
			server, seen := roadmapServer(t, http.StatusOK, roadmapReceipt)
			cobraWorkspace(t, server, 0)
			writeRoadmapPlan(t, "plan.json", roadmapPlanFile)

			_, err := runRoot(t, args...)
			if err == nil || !strings.Contains(err.Error(), "bound System") {
				t.Fatalf("error = %v; want a refusal that names the bound System", err)
			}
			if len(*seen) != 0 {
				t.Fatalf("an unbound workspace reached the server: %#v", *seen)
			}
		})
	}
}

// Epic 326 (A3), test case a0258698: `modernpath roadmap show` says, for each
// Initiative, Done, No work yet for one with no linked work that counts, or
// Not done with n of m Epics, user requirements and system requirements
// complete, naming only the kinds with an item that counts; under an
// Initiative that is not done it prints one line per item of waiting_for with
// its title, its step and whose turn it is. --json carries done, done_when and
// waiting_for as served. The words are the Initiative page's: the steps
// Blocked, Not shaped into work yet, Being built, Plan waiting for approval,
// Completion waiting for approval and Deferred, and the turns The team and A
// person on the team.
const roadmapDoneReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"PAY","name":"Payments"}],
"releases":[{"id":7,"name":"Spring","status":"active","deadline":"2026-12-15","initiatives":[
  {"id":"11111111-1111-4111-8111-111111111111","text":"Card wallet","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[41,42],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":1,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"epic","id":42,"display_ref":null,"system_id":1,"title":"Wallet screens","step":"completion_approval","turn":"person","member_epic":null}]},
  {"id":"33333333-3333-4333-8333-333333333333","text":"Gift cards","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[43,44],
   "done":true,
   "done_when":[{"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[]},
  {"id":"44444444-4444-4444-8444-444444444444","text":"Receipts by email","horizon":"now","context":null,"new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":1}}}],
   "waiting_for":[{"kind":"system_requirement","id":"55555555-5555-4555-8555-555555555555","display_ref":"SR-12","system_id":1,"title":"Send each receipt as a message","step":"not_shaped","turn":"team","member_epic":null}]},
  {"id":"66666666-6666-4666-8666-666666666666","text":"Tax report","horizon":"now","context":null,"new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[],
   "done":true,
   "done_when":[{"key":"work_complete","met":true,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":1,"counting":1}}}],
   "waiting_for":[]},
  {"id":"77777777-7777-4777-8777-777777777777","text":"Loyalty tiers","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[52],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":1},"user_requirement":{"done":0,"counting":1},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[
     {"kind":"epic","id":52,"display_ref":null,"system_id":1,"title":"Tier screens","step":"blocked","turn":"team","member_epic":null},
     {"kind":"user_requirement","id":"88888888-8888-4888-8888-888888888888","display_ref":"UR-3","system_id":1,"title":"Customers see their tier","step":"plan_approval","turn":"person","member_epic":{"id":51,"title":"Tier engine"}}]}]}],
"no_release":{"now":[],"next":[
  {"id":"22222222-2222-4222-8222-222222222222","text":"Points for every order","horizon":"next","context":"Loyalty","new_context":true,"work_kind":"new_work","release_id":null,"epic_ids":[],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"initiative","id":"22222222-2222-4222-8222-222222222222","display_ref":null,"system_id":1,"title":"Points for every order","step":"not_shaped","turn":"team","member_epic":null}]}],
  "later":[]}}}`

var roadmapDoneTitles = []string{"Card wallet", "Gift cards", "Receipts by email", "Tax report", "Loyalty tiers", "Points for every order"}

// roadmapBlocks splits the text of roadmap show into one block per
// Initiative: its own line and every line under it, up to the next
// Initiative, Release or horizon heading.
func roadmapBlocks(out string, titles []string) map[string]string {
	blocks := map[string]string{}
	current := ""
	var lines []string
	flush := func() {
		if current != "" {
			blocks[current] = strings.Join(lines, "\n")
		}
	}
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		started := ""
		for _, title := range titles {
			if strings.HasPrefix(trimmed, "- "+title) {
				started = title
			}
		}
		heading := strings.HasPrefix(trimmed, "Release ") || trimmed == "No release" ||
			trimmed == "Now" || trimmed == "Next" || trimmed == "Later"
		if started != "" || heading {
			flush()
			current, lines = started, nil
		}
		if current != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return blocks
}

// roadmapLineWith is the one line of block that holds text, or "".
func roadmapLineWith(block, text string) string {
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

func requireRoadmapMatch(t *testing.T, what, text, pattern string) {
	t.Helper()
	if !regexp.MustCompile(pattern).MatchString(text) {
		t.Errorf("%s does not match %s:\n%s", what, pattern, text)
	}
}

func requireNoRoadmapMatch(t *testing.T, what, text, pattern string) {
	t.Helper()
	if regexp.MustCompile(pattern).MatchString(text) {
		t.Errorf("%s matches %s:\n%s", what, pattern, text)
	}
}

func TestRoadmapShowSaysWhetherEachInitiativeIsDoneAndWhatItWaitsFor(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapDoneReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")
	blocks := roadmapBlocks(out, roadmapDoneTitles)
	for _, title := range roadmapDoneTitles {
		if _, ok := blocks[title]; !ok {
			t.Fatalf("output has no Initiative %q:\n%s", title, out)
		}
	}

	// Two Epics, one DONE: not done, the Epics counted alone, the one in review waiting.
	wallet := blocks["Card wallet"]
	requireRoadmapMatch(t, "Card wallet", wallet, `(?i)\bnot done\b`)
	requireRoadmapMatch(t, "Card wallet", wallet, `(?i)\b1 of 2 epics\b`)
	requireNoRoadmapMatch(t, "Card wallet", wallet, `(?i)of \d+ (user|system) requirements`)
	screens := roadmapLineWith(wallet, "Wallet screens")
	requireRoadmapMatch(t, "the line of Wallet screens", screens, `(?i)completion waiting for approval`)
	requireRoadmapMatch(t, "the line of Wallet screens", screens, `(?i)a person on the team`)

	// Done: no waiting line.
	for _, title := range []string{"Gift cards", "Tax report"} {
		block := blocks[title]
		requireRoadmapMatch(t, title, block, `(?i)\bdone\b`)
		requireNoRoadmapMatch(t, title, block, `(?i)\bnot done\b|waiting for approval|not shaped|being built|the team`)
	}

	// Its own link a PROPOSED system requirement: 0 of 1 system requirements, and the requirement waits.
	receipts := blocks["Receipts by email"]
	requireRoadmapMatch(t, "Receipts by email", receipts, `(?i)\bnot done\b`)
	requireRoadmapMatch(t, "Receipts by email", receipts, `(?i)\b0 of 1 system requirements\b`)
	requireNoRoadmapMatch(t, "Receipts by email", receipts, `(?i)of \d+ (epics|user requirements)`)
	message := roadmapLineWith(receipts, "Send each receipt as a message")
	requireRoadmapMatch(t, "the line of Send each receipt as a message", message, `(?i)not shaped into work yet`)
	requireRoadmapMatch(t, "the line of Send each receipt as a message", message, `(?i)\bthe team\b`)

	// A blocked Epic first, then the requirement whose member Epic waits for its plan's approval.
	tiers := blocks["Loyalty tiers"]
	requireRoadmapMatch(t, "Loyalty tiers", tiers, `(?i)\bnot done\b`)
	requireRoadmapMatch(t, "Loyalty tiers", tiers, `(?i)\b0 of 1 user requirements\b`)
	blocked := roadmapLineWith(tiers, "Tier screens")
	requireRoadmapMatch(t, "the line of Tier screens", blocked, `(?i)\bblocked\b`)
	requireRoadmapMatch(t, "the line of Tier screens", blocked, `(?i)\bthe team\b`)
	tier := roadmapLineWith(tiers, "Customers see their tier")
	requireRoadmapMatch(t, "the line of Customers see their tier", tier, `(?i)plan waiting for approval`)
	requireRoadmapMatch(t, "the line of Customers see their tier", tier, `(?i)a person on the team`)
	if blocked != "" && tier != "" && strings.Index(tiers, blocked) > strings.Index(tiers, tier) {
		t.Errorf("the lines under Loyalty tiers are not in the served order:\n%s", tiers)
	}

	// No linked work that counts: No work yet, and one line saying it waits for the team.
	points := blocks["Points for every order"]
	requireRoadmapMatch(t, "Points for every order", points, `(?i)\bno work yet\b`)
	requireRoadmapMatch(t, "Points for every order", points, `(?i)waits for the team to shape it into work`)
}

func TestRoadmapShowJSONCarriesDoneDoneWhenAndWaitingFor(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, roadmapDoneReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, out)
	}
	var data, served map[string]any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("roadmap show --json is not JSON: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(roadmapDoneReply), &served); err != nil {
		t.Fatal(err)
	}
	want := served["data"].(map[string]any)["releases"].([]any)[0].(map[string]any)["initiatives"].([]any)
	releases, _ := data["releases"].([]any)
	if len(releases) != 1 {
		t.Fatalf("releases = %#v", data["releases"])
	}
	got, _ := releases[0].(map[string]any)["initiatives"].([]any)
	if len(got) != len(want) {
		t.Fatalf("initiatives = %#v", got)
	}
	for i := range want {
		for _, key := range []string{"done", "done_when", "waiting_for"} {
			gotJSON, _ := json.Marshal(got[i].(map[string]any)[key])
			wantJSON, _ := json.Marshal(want[i].(map[string]any)[key])
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("initiative %d %s = %s; want %s", i+1, key, gotJSON, wantJSON)
			}
		}
	}
}

// 38568a46-9be6-451a-b398-b48f910cc69a
func TestRoadmapStatusDiagnostics(t *testing.T) {
	const path = "/api/v1/systems/1/roadmap/status/diagnostics"
	const id = "11111111-1111-4111-8111-111111111111"
	const reply = `{"data":{"id":"11111111-1111-4111-8111-111111111111","status":"failed","recorded_status":"drafting","inserted_at":"2026-11-18T10:00:00Z","updated_at":"2026-11-18T10:00:00Z","failure":{"category":"draft_stale","summary":"The status draft did not finish in time."},"job":{"state":"discarded","attempt":3,"max_attempts":3}}}`
	t.Run("selected ID text", func(t *testing.T) {
		server, seen := roadmapServer(t, http.StatusOK, reply)
		cobraWorkspace(t, server, 1)
		out, err := runRoot(t, "roadmap", "status-diagnostics", id)
		if err != nil {
			t.Fatal(err)
		}
		req := requireOneRoadmapRequest(t, seen, http.MethodGet, path)
		if req.query != "update_id="+id {
			t.Fatalf("query = %s", req.query)
		}
		for _, want := range []string{id, "failed", "drafting", "2026-11-18T10:00:00Z", "did not finish in time", "3/3"} {
			if !strings.Contains(out, want) {
				t.Errorf("output lacks %q: %s", want, out)
			}
		}
	})
	t.Run("latest JSON", func(t *testing.T) {
		server, seen := roadmapServer(t, http.StatusOK, reply)
		cobraWorkspace(t, server, 1)
		out, err := runRoot(t, "roadmap", "status-diagnostics", "--json")
		if err != nil {
			t.Fatal(err)
		}
		req := requireOneRoadmapRequest(t, seen, http.MethodGet, path)
		if req.query != "" {
			t.Fatalf("query = %s", req.query)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(out), &got); err != nil || got["status"] != "failed" {
			t.Fatalf("invalid diagnostic JSON: %s, %v", out, err)
		}
	})
	t.Run("refusal and invalid ID", func(t *testing.T) {
		server, seen := roadmapServer(t, http.StatusNotFound, `{"error":"not_found"}`)
		cobraWorkspace(t, server, 1)
		if _, err := runRoot(t, "roadmap", "status-diagnostics", "invalid"); err == nil || !strings.Contains(err.Error(), "UUID") {
			t.Fatalf("expected invalid UUID error, got %v", err)
		}
		if len(*seen) != 0 {
			t.Fatalf("unexpected request: %#v", *seen)
		}
		if _, err := runRoot(t, "roadmap", "status-diagnostics", id); err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("expected a 404 refusal, got %v", err)
		}
	})
}

// Epic 458 (H1a), SR-1 criterion 2 (b7c6e1ee): `roadmap show` prints the
// continuation link under each Initiative and `--json` carries it; `roadmap
// apply`, with and without --dry-run, prints an initiative_continued refusal
// with the server's message; `releases activate` refused with
// incumbent_has_unfinished_work prints, besides the code, only named fields
// of each release and its undelivered decisions, never the body.
//
// 6e29820b-3471-4f7f-9a6c-825a1e126b6f
func TestRoadmapContinuationOutputAndTheActivationRefusal(t *testing.T) {
	const first = `{"id":"44444444-4444-4444-8444-444444444444","text":"Replace invoicing","horizon":"now","context":"PAY","work_kind":"replaces_legacy","release_id":7,"epic_ids":[41],
"continues":null,"continued_by":{"id":"55555555-5555-4555-8555-555555555555","display_number":13,"text":"Replace invoicing (continued)","archived_at":null,"release_id":8,"release_name":"Summer"}}`
	const part = `{"id":"55555555-5555-4555-8555-555555555555","text":"Replace invoicing (continued)","horizon":"next","context":"PAY","work_kind":"replaces_legacy","release_id":8,"epic_ids":[42],
"continues":{"id":"44444444-4444-4444-8444-444444444444","display_number":12,"text":"Replace invoicing","archived_at":null,"release_id":7,"release_name":"Spring"},"continued_by":null}`
	read := `{"data":{"system_id":1,"plan_state":"agreed","contexts":[{"code":"PAY","name":"Payments"}],
"releases":[{"id":7,"name":"Spring","status":"released","initiatives":[` + first + `]},{"id":8,"name":"Summer","status":"planned","initiatives":[` + part + `]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

	server, _ := roadmapServer(t, http.StatusOK, read)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	for _, want := range []string{
		"continued in Replace invoicing (continued) (INIT-13)",
		"continues: Replace invoicing (INIT-12)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("roadmap show lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "continued in") > strings.Index(out, "Replace invoicing (continued)  [") {
		t.Errorf("the link prints under its Initiative:\n%s", out)
	}

	out, err = runRoot(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, out)
	}
	if !strings.Contains(out, `"continued_by"`) || !strings.Contains(out, `"continues"`) {
		t.Errorf("roadmap show --json lacks the link:\n%s", out)
	}

	message := "Replace invoicing is continued by Replace invoicing (continued) in Summer, so it cannot be archived or deleted; it stays as part of the chain."
	refused := `{"index":1,"op":"archive_line","reason":"initiative_continued","message":"` + message + `","continuation":{"id":"55555555-5555-4555-8555-555555555555","display_number":13,"text":"Replace invoicing (continued)","release_id":8,"release_name":"Summer"}}`
	const archive = `{"operations":[{"op":"archive_line","line_id":"44444444-4444-4444-8444-444444444444"}]}`

	server, _ = roadmapServer(t, http.StatusOK, `{"data":{"operations":[{"index":1,"op":"archive_line","summary":"Archive initiative","release":null}],"errors":[`+refused+`],"dry_run":true}}`)
	cobraWorkspace(t, server, 1)
	plan := writeRoadmapPlan(t, "archive.json", archive)
	out, err = runRoot(t, "roadmap", "apply", "--file", plan, "--dry-run")
	if err == nil || !strings.Contains(out, message) {
		t.Errorf("roadmap apply --dry-run prints the server's message (err %v):\n%s", err, out)
	}

	server, _ = roadmapServer(t, http.StatusUnprocessableEntity, `{"error":"initiative_continued","index":1,"errors":[`+refused+`]}`)
	cobraWorkspace(t, server, 1)
	plan = writeRoadmapPlan(t, "archive.json", archive)
	out, err = runRoot(t, "roadmap", "apply", "--file", plan)
	if err == nil || !strings.Contains(err.Error()+out, message) {
		t.Errorf("roadmap apply prints the server's message (err %v):\n%s", err, out)
	}

	// releases activate: the named fields only.
	activation := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":{"items":[{"id":1,"system_id":4,"slug":"current","status":"active"},{"id":2,"system_id":4,"slug":"next","status":"planned"}]}}`)
			return
		}
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"incumbent_has_unfinished_work","detail":"body text 7731","open_releases":[{"id":1,"name":"Spring","slug":"current","status":"active","system_id":4,"note":"body text 7732",
"undelivered_decisions":[{"id":"44444444-4444-4444-8444-444444444444","display_number":12,"title":"Replace invoicing","decisions":2}]}]}`)
	}))
	defer activation.Close()
	cmd := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, activation, 4), nil })
	cmd.SetArgs([]string{"activate", "next", "--close-current", "--pin-stdin"})
	cmd.SetIn(strings.NewReader("482913\n"))
	var printed bytes.Buffer
	cmd.SetOut(&printed)
	cmd.SetErr(&printed)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err = cmd.Execute()
	if err == nil {
		t.Fatal("the refused activation must fail the command")
	}
	text := err.Error() + printed.String()
	for _, want := range []string{
		"incumbent_has_unfinished_work",
		"Spring (current)",
		"still has unfinished work or decisions that go on; close it on its close page first",
		"Replace invoicing (INIT-12): 2 decisions go on",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("releases activate lacks %q:\n%s", want, text)
		}
	}
	for _, never := range []string{"body text 7731", "body text 7732", "482913"} {
		if strings.Contains(text, never) {
			t.Errorf("releases activate printed %q:\n%s", never, text)
		}
	}
}

func TestRoadmapVerify(t *testing.T) {
	const violations = `[{"epic_id":4,"problems":["missing_initiative"],"initiative_ids":[]},{"epic_id":8,"problems":["multiple_initiatives","release_mismatch"],"initiative_ids":["11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222"]}]`
	for _, tt := range []struct {
		name, body, output string
		status             int
		wantError          bool
	}{
		{"clean", `{"data":{"violations":[]}}`, `"violations":[]`, 200, false},
		{"mixed", `{"data":{"violations":` + violations + `}}`, "release_mismatch", 200, true},
		{"unknown outer field", `{"data":{"violations":[]},"error":"verification_failed"}`, "", 200, true},
		{"unknown inner field", `{"data":{"violations":[],"error":"verification_failed"}}`, "", 200, true},
		{"missing", `{"data":{}}`, "", 200, true},
		{"null", `{"data":{"violations":null}}`, "", 200, true},
		{"invalid", `{"data":{"violations":[{"epic_id":4,"problems":["unknown"],"initiative_ids":[]}]}}`, "", 200, true},
		{"refused", `{"error":"not_found"}`, "", 404, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server, seen := roadmapServer(t, tt.status, tt.body)
			cobraWorkspace(t, server, 1)
			out, err := runRootStdout(t, "roadmap", "verify", "--json")
			if (err != nil) != tt.wantError {
				t.Fatalf("error = %v, want error = %v, output: %q", err, tt.wantError, out)
			}
			requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap/verify")
			if tt.output != "" {
				var data map[string]json.RawMessage
				if err := json.Unmarshal([]byte(out), &data); err != nil || len(data) != 1 || data["violations"] == nil {
					t.Fatalf("want only the violations object, got %q (%v)", out, err)
				}
				compact := strings.Join(strings.Fields(out), "")
				if !strings.Contains(compact, tt.output) {
					t.Fatalf("output = %q, want %q", out, tt.output)
				}
			}
		})
	}
}

// Epic 460 (H3), TestCase TC-H3-S10, SR-5 criterion 1: "roadmap show prints an
// Initiative's Epics as Epic 2 (1 Epic) or Epics 2, 10 (2 Epics), in number
// order." Before the Epic the line reads "Epics 2" for one Epic and sorts the
// ids as text ("Epics 10, 2"), so the assertions fail. An Initiative with no
// Epic names none (guard).
const h3RoadmapEpicsReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"PAY","name":"Payments"}],
"releases":[{"id":7,"name":"Spring","status":"active","initiatives":[
  {"id":"11111111-1111-4111-8111-111111111111","text":"Card wallet","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[10,2]},
  {"id":"33333333-3333-4333-8333-333333333333","text":"Gift cards","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[2]}]}],
"no_release":{"now":[],"next":[
  {"id":"22222222-2222-4222-8222-222222222222","text":"Points for every order","horizon":"next","context":"PAY","new_context":false,"work_kind":"new_work","release_id":null,"epic_ids":[]}],
  "later":[]}}}`

func TestH3_RoadmapShowCountsAnInitiativesEpicsInWords(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, h3RoadmapEpicsReply)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	line := func(text string) string {
		for _, l := range strings.Split(out, "\n") {
			if strings.Contains(l, text) {
				return l
			}
		}
		t.Fatalf("roadmap show prints no line for %s:\n%s", text, out)
		return ""
	}

	t.Run("two Epics in number order, with their count", func(t *testing.T) {
		if got := line("Card wallet"); !strings.Contains(got, ", Epics 2, 10 (2 Epics)]") {
			t.Errorf("the line of Card wallet = %q; want Epics 2, 10 (2 Epics)", got)
		}
	})
	t.Run("one Epic in the singular, with its count", func(t *testing.T) {
		if got := line("Gift cards"); !strings.Contains(got, ", Epic 2 (1 Epic)]") {
			t.Errorf("the line of Gift cards = %q; want Epic 2 (1 Epic)", got)
		}
	})
	t.Run("an Initiative with no Epic names none (guard)", func(t *testing.T) {
		if got := line("Points for every order"); strings.Contains(got, "Epic") {
			t.Errorf("the line of Points for every order = %q; want no Epic", got)
		}
	})
}
