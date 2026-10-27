package cmd

// Epic 325 (A2), test case 440e8a25: the command line reads the planning
// contexts. `roadmap show` prints each Release's planning context under its
// name and each Initiative's under its title, as the Roadmap read route stores
// them (no reference titles), and --json carries planning_context on each
// (criterion 172c7fb8). `epics get` accepts --fields planning_contexts and
// returns planning_contexts with --full, as the Epic read computes it, with
// reference titles (criterion cfe4ca92).
//
// The tests drive the root command and use no new symbol, so they build on a
// binary without the field and fail on their assertions.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Roadmap read with a planning context on the Release, on the Initiative
// under it and on the Initiative with no Release; the second Release's is the
// column default {}, an empty planning context.
const roadmapPlanningContextReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":7,"name":"Spring","status":"active",
  "planning_context":{"version":1,"accepted_context_revision":0,
   "decisions":[{"statement":"Card data stays with the payment provider","rationale":"PCI scope"}],
   "assumptions":[{"statement":"The provider's sandbox is available"}],
   "open_questions":[{"question":"Do refunds go through the same flow?","status":"open"}],
   "references":[{"kind":"document","target_ref":"document:sd:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"}]},
  "initiatives":[
   {"id":"11111111-1111-4111-8111-111111111111","text":"Card wallet","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[41],
    "planning_context":{"version":1,"accepted_context_revision":0,
     "decisions":[{"statement":"One wallet per customer"}],
     "assumptions":[],"open_questions":[],
     "references":[{"kind":"file","target_ref":"file:v2:repo:3:system:1:path:docs/wallet.md"}]}}]},
 {"id":8,"name":"Summer","status":"planned","planning_context":{},"initiatives":[]}],
"no_release":{"now":[],"next":[
  {"id":"22222222-2222-4222-8222-222222222222","text":"Points for every order","horizon":"next","context":"Loyalty","new_context":true,"work_kind":"new_work","release_id":null,"epic_ids":[],
   "planning_context":{"version":1,"accepted_context_revision":0,"decisions":[],
    "assumptions":[{"statement":"Points never expire"}],
    "open_questions":[{"question":"Who funds the points?","status":"resolved","resolution":"Marketing"}],
    "references":[]}}],
  "later":[]}}}`

func TestRoadmapShowPrintsEachPlanningContextUnderItsReleaseAndInitiative(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapPlanningContextReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")

	at := func(text string) int {
		t.Helper()
		i := strings.Index(out, text)
		if i < 0 {
			t.Errorf("output lacks %q:\n%s", text, out)
		}
		return i
	}
	spring, wallet, summer, noRelease, points :=
		at("Spring"), at("Card wallet"), at("Summer"), at("No release"), at("Points for every order")

	// The Release's planning context, under its name and before its Initiatives.
	for _, entry := range []string{
		"Card data stays with the payment provider",
		"PCI scope",
		"The provider's sandbox is available",
		"Do refunds go through the same flow?",
		"document:sd:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
	} {
		if i := at(entry); i >= 0 && !(spring < i && i < wallet) {
			t.Errorf("%q is not printed under the Release Spring, before its Initiatives:\n%s", entry, out)
		}
	}
	// The Initiative's planning context, under its title and before the next Release.
	for _, entry := range []string{"One wallet per customer", "file:v2:repo:3:system:1:path:docs/wallet.md"} {
		if i := at(entry); i >= 0 && !(wallet < i && i < summer) {
			t.Errorf("%q is not printed under the Initiative Card wallet:\n%s", entry, out)
		}
	}
	// The planning context of an Initiative with no Release, under its title.
	for _, entry := range []string{"Points never expire", "Who funds the points?", "Marketing"} {
		if i := at(entry); i >= 0 && !(noRelease < i && points < i) {
			t.Errorf("%q is not printed under the Initiative Points for every order:\n%s", entry, out)
		}
	}
}

func TestRoadmapShowJSONCarriesEachPlanningContextAsServed(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, roadmapPlanningContextReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, out)
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(out), &data); err != nil {
		t.Fatalf("roadmap show --json is not JSON: %v\n%s", err, out)
	}
	releases, _ := data["releases"].([]any)
	if len(releases) != 2 {
		t.Fatalf("releases = %#v", data["releases"])
	}
	spring := releases[0].(map[string]any)
	context, ok := spring["planning_context"].(map[string]any)
	if !ok || context["version"] != float64(1) || len(context["decisions"].([]any)) != 1 {
		t.Fatalf("Spring's planning_context = %#v", spring["planning_context"])
	}
	references := context["references"].([]any)
	if ref := references[0].(map[string]any); ref["target_ref"] != "document:sd:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" || ref["title"] != nil {
		t.Fatalf("the Roadmap read carries references as stored, without titles: %#v", ref)
	}
	wallet := spring["initiatives"].([]any)[0].(map[string]any)
	if _, ok := wallet["planning_context"].(map[string]any); !ok {
		t.Fatalf("Card wallet's planning_context = %#v", wallet["planning_context"])
	}
	if summer := releases[1].(map[string]any); summer["planning_context"] == nil {
		t.Fatalf("Summer's empty planning_context is missing: %#v", summer)
	}
	next := data["no_release"].(map[string]any)["next"].([]any)
	if points := next[0].(map[string]any); points["planning_context"] == nil {
		t.Fatalf("Points for every order's planning_context is missing: %#v", points)
	}
}

// The Epic read with the planning contexts of its Release and Initiative, each
// reference with its title and each with who last changed it and when.
const epicPlanningContextsReply = `{"data":{"id":42,"system_id":1,"title":"Card checkout","process_status":"PROPOSED",
"planning_contexts":{
 "release":{"id":7,"name":"Spring",
  "planning_context":{"version":1,"accepted_context_revision":0,
   "decisions":[{"statement":"Card data stays with the payment provider"}],
   "assumptions":[],"open_questions":[],
   "references":[{"kind":"document","target_ref":"document:sd:aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","title":"Payments architecture"}]},
  "changed_by":{"id":5,"name":"Pia Planner"},"changed_at":"2026-10-25T10:00:00Z"},
 "initiative":{"id":"11111111-1111-4111-8111-111111111111","title":"Card wallet",
  "planning_context":{"version":1,"accepted_context_revision":0,
   "decisions":[],"assumptions":[{"statement":"One wallet per customer"}],"open_questions":[],"references":[]},
  "changed_by":{"id":6,"name":"Ola Owner"},"changed_at":"2026-10-25T11:00:00Z"}}}}`

func epicPlanningContextsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/work/epics/42" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(epicPlanningContextsReply))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestEpicGetAcceptsFieldsPlanningContexts(t *testing.T) {
	cobraWorkspace(t, epicPlanningContextsServer(t))

	out, err := runRoot(t, "epics", "get", "42", "--fields", "planning_contexts", "--json")
	if err != nil {
		t.Fatalf("epics get --fields planning_contexts: %v\n%s", err, out)
	}
	var selected map[string]any
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatalf("epics get --json is not JSON: %v\n%s", err, out)
	}
	contexts, ok := selected["planning_contexts"].(map[string]any)
	if len(selected) != 1 || !ok {
		t.Fatalf("--fields planning_contexts must return exactly that field: %#v", selected)
	}
	release, _ := contexts["release"].(map[string]any)
	initiative, _ := contexts["initiative"].(map[string]any)
	if release["name"] != "Spring" || initiative["title"] != "Card wallet" {
		t.Fatalf("planning_contexts = %#v", contexts)
	}
	refs := release["planning_context"].(map[string]any)["references"].([]any)
	if ref := refs[0].(map[string]any); ref["title"] != "Payments architecture" {
		t.Fatalf("the Epic read's reference carries its title: %#v", ref)
	}
	if release["changed_at"] != "2026-10-25T10:00:00Z" || release["changed_by"] == nil {
		t.Fatalf("the Release's planning context carries who changed it and when: %#v", release)
	}

	out, err = runRoot(t, "epics", "get", "42", "--fields", "planning_contexts")
	if err != nil || !strings.Contains(out, "planning_contexts: {") || !strings.Contains(out, "One wallet per customer") {
		t.Fatalf("text output must print planning_contexts as a JSON object field: %q err=%v", out, err)
	}
}

func TestEpicGetFullIncludesThePlanningContexts(t *testing.T) {
	cobraWorkspace(t, epicPlanningContextsServer(t))

	out, err := runRoot(t, "epics", "get", "42", "--full", "--json")
	if err != nil {
		t.Fatalf("epics get --full: %v\n%s", err, out)
	}
	var full map[string]any
	if err := json.Unmarshal([]byte(out), &full); err != nil {
		t.Fatalf("epics get --full --json is not JSON: %v\n%s", err, out)
	}
	if _, ok := full["planning_contexts"].(map[string]any); !ok {
		t.Fatalf("--full must include planning_contexts: %#v", full)
	}
}
