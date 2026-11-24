package cmd

// Epic 471 (H5b), TestCase TC-H5B-S4, SR-3 criterion 0 (3efdb90d): roadmap show
// prints under an Initiative each item of planning_waits before its
// waiting_for lines: "Waits for <n> legacy requirements undecided: Decide them
// on the Selection tab, The team" (in the singular for one), and "Waits for the
// agreement of Now: Not agreed yet" or "Changed since the agreement", then
// "<name> (business owner)", "The business owner" when the person has no name,
// or "A workspace admin" when the item names none. --json carries
// planning_waits. Before the build writeRoadmapWaits read only waiting_for, so
// no planning line prints and the assertions on them fail.

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// h5bPlanningRoadmap holds under the planned Modernization Release Billing
// modernization four replacing Initiatives in Now with no Epic: one with six
// undecided rows and never agreed (Bea Owner agrees), one with one undecided
// row changed since the agreement whose owner has no name, one changed since
// the agreement while no business owner is named, and one with no planning step.
const h5bPlanningRoadmap = `{"data":{"system_id":1,"plan_state":"changed_since_agreement",
"contexts":[],
"releases":[{"id":41,"name":"Billing modernization","status":"planned","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy billing",
 "legacy_reference_revision":null,"modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
 {"id":"b5b00001-0000-4000-8000-000000000001","text":"Invoice run","horizon":"now","context":"BILL","new_context":false,"work_kind":"replaces_legacy","release_id":41,"epic_ids":[],
  "progress":"planned","accepted":false,"done":false,
  "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
  "waiting_for":[{"kind":"initiative","id":"b5b00001-0000-4000-8000-000000000001","display_ref":null,"system_id":null,"title":"Invoice run","step":"not_shaped","turn":"team","member_epic":null}],
  "planning_waits":[
   {"kind":"selection","id":"b5b00001-0000-4000-8000-000000000001","step":"selection_undecided","turn":"team","count":6},
   {"kind":"agreement","id":"b5b00001-0000-4000-8000-000000000001","step":"agreement_not_agreed","turn":"person","person":{"id":7,"name":"Bea Owner"}}]},
 {"id":"b5b00002-0000-4000-8000-000000000002","text":"Price lists","horizon":"now","context":"BILL","new_context":false,"work_kind":"replaces_legacy","release_id":41,"epic_ids":[],
  "progress":"planned","accepted":false,"done":false,
  "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
  "waiting_for":[{"kind":"initiative","id":"b5b00002-0000-4000-8000-000000000002","display_ref":null,"system_id":null,"title":"Price lists","step":"not_shaped","turn":"team","member_epic":null}],
  "planning_waits":[
   {"kind":"selection","id":"b5b00002-0000-4000-8000-000000000002","step":"selection_undecided","turn":"team","count":1},
   {"kind":"agreement","id":"b5b00002-0000-4000-8000-000000000002","step":"agreement_changed","turn":"person","person":{"id":8,"name":null}}]},
 {"id":"b5b00003-0000-4000-8000-000000000003","text":"Credit notes","horizon":"now","context":"BILL","new_context":false,"work_kind":"replaces_legacy","release_id":41,"epic_ids":[],
  "progress":"planned","accepted":false,"done":false,
  "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
  "waiting_for":[{"kind":"initiative","id":"b5b00003-0000-4000-8000-000000000003","display_ref":null,"system_id":null,"title":"Credit notes","step":"not_shaped","turn":"team","member_epic":null}],
  "planning_waits":[
   {"kind":"agreement","id":"b5b00003-0000-4000-8000-000000000003","step":"agreement_changed","turn":"person","person":null}]},
 {"id":"b5b00004-0000-4000-8000-000000000004","text":"Statements","horizon":"now","context":"BILL","new_context":false,"work_kind":"replaces_legacy","release_id":41,"epic_ids":[],
  "progress":"planned","accepted":false,"done":false,
  "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
  "waiting_for":[{"kind":"initiative","id":"b5b00004-0000-4000-8000-000000000004","display_ref":null,"system_id":null,"title":"Statements","step":"not_shaped","turn":"team","member_epic":null}],
  "planning_waits":[]}]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

// h5bWaitLines is each "Waits for" line of an Initiative's block, trimmed.
func h5bWaitLines(block string) []string {
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "Waits for") {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// h5bBlock is the text of the Initiative titled `title`, up to the next one.
func h5bBlock(out, title string, titles []string) string {
	i := strings.Index(out, title)
	if i < 0 {
		return ""
	}
	rest := out[i+len(title):]
	end := len(rest)
	for _, other := range titles {
		if other == title {
			continue
		}
		if j := strings.Index(rest, other); j >= 0 && j < end {
			end = j
		}
	}
	return rest[:end]
}

// 7b7ad6a6-3736-425d-a5cd-233cd936e02b
func TestH5B_RoadmapShowPrintsThePlanningStepsBeforeTheOtherWaits(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, h5bPlanningRoadmap)
	cobraWorkspace(t, server, 1)
	out, err := runRootStdout(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}

	titles := []string{"Invoice run", "Price lists", "Credit notes", "Statements"}
	cases := []struct {
		title string
		want  []string
	}{
		{"Invoice run", []string{
			"Waits for 6 legacy requirements undecided: Decide them on the Selection tab, The team",
			"Waits for the agreement of Now: Not agreed yet, Bea Owner (business owner)",
			"Waits for the team to shape it into work",
		}},
		{"Price lists", []string{
			"Waits for 1 legacy requirement undecided: Decide it on the Selection tab, The team",
			"Waits for the agreement of Now: Changed since the agreement, The business owner",
			"Waits for the team to shape it into work",
		}},
		{"Credit notes", []string{
			"Waits for the agreement of Now: Changed since the agreement, A workspace admin",
			"Waits for the team to shape it into work",
		}},
		{"Statements", []string{
			"Waits for the team to shape it into work",
		}},
	}
	for _, c := range cases {
		block := h5bBlock(out, c.title, titles)
		if block == "" {
			t.Fatalf("roadmap show does not print %q:\n%s", c.title, out)
		}
		if got := h5bWaitLines(block); !slices.Equal(got, c.want) {
			t.Errorf("%s waits for\n %q\nwant\n %q\n%s", c.title, got, c.want, block)
		}
	}
	for _, code := range []string{"selection_undecided", "agreement_not_agreed", "agreement_changed"} {
		if strings.Contains(out, code) {
			t.Errorf("roadmap show prints the code %q:\n%s", code, out)
		}
	}

	// --json carries planning_waits as served.
	jsonOut, err := runRootStdout(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, jsonOut)
	}
	var printed, served map[string]any
	if err := json.Unmarshal([]byte(jsonOut), &printed); err != nil {
		t.Fatalf("roadmap show --json is not JSON: %v\n%s", err, jsonOut)
	}
	if err := json.Unmarshal([]byte(h5bPlanningRoadmap), &served); err != nil {
		t.Fatal(err)
	}
	waitsOf := func(read map[string]any) []any {
		if data, ok := read["data"].(map[string]any); ok {
			read = data
		}
		releases, _ := read["releases"].([]any)
		release, _ := releases[0].(map[string]any)
		initiatives, _ := release["initiatives"].([]any)
		first, _ := initiatives[0].(map[string]any)
		waits, _ := first["planning_waits"].([]any)
		return waits
	}
	got, want := waitsOf(printed), waitsOf(served)
	if len(want) != 2 || !jsonEqual(got, want) {
		t.Errorf("roadmap show --json carries planning_waits %v; want %v", got, want)
	}
}

func jsonEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
