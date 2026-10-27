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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

type roadmapRequest struct {
	method, path string
	body         map[string]any
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
		seen = append(seen, roadmapRequest{method: r.Method, path: r.URL.Path, body: decoded})
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
