package cmd

// Epic 330 (B1), test case c2328739 (TC-B1-11), criterion f0008435: `roadmap
// show` prints, for a Modernization Release, its type, the legacy system or in
// place, the reference revision or that none is set, and each context in the
// scope with its undecided count out of its total. A Delivery Release prints
// none of these.
//
// The test drives the root command and uses no new symbol, so it builds on a
// binary whose printer does not know the new fields of the Roadmap read and
// fails on its assertions.

import (
	"net/http"
	"strings"
	"testing"
)

// The Roadmap read of System 1 with a Modernization Release onto a new system
// (legacy System 41, scope narrowed to two contexts, a reference revision), a
// Delivery Release, and a Modernization Release in place (the bound System,
// every context, no reference revision). The Delivery Release sits between the
// two, so a line printed under the wrong heading lands in its block.
const roadmapModernizationReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INTG","name":"Integrations"},{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL","INTG"],"legacy_reference_revision":"a1b2c3d4",
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":4,"undecided":3},
   {"code":"INTG","name":"Integrations","in_scope":3,"undecided":3}]},
  "initiatives":[
   {"id":"11111111-1111-4111-8111-111111111111","text":"Invoice runs","horizon":"now","context":"BILL","context_name":"Billing","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[51]}]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,
  "initiatives":[
   {"id":"22222222-2222-4222-8222-222222222222","text":"Gift cards","horizon":"next","context":"PAY","context_name":"Payments","new_context":false,"work_kind":"new_work","release_id":22,"epic_ids":[]}]},
 {"id":23,"name":"Renewal","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":1,"legacy_system_name":"T","in_place":true,
  "legacy_scope":null,"legacy_reference_revision":null,
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":5,"undecided":5},
   {"code":"INTG","name":"Integrations","in_scope":2,"undecided":2},
   {"code":"PAY","name":"Payments","in_scope":6,"undecided":6},
   {"code":"—","name":"Unclassified","in_scope":1,"undecided":1}]},
  "initiatives":[]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

func TestB1C2328739RoadmapShowPrintsAModernizationReleasesLegacyFacts(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, roadmapModernizationReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")

	// Each Release's block runs from its heading to the next heading.
	heads := []string{"Release Cutover", "Release Autumn", "Release Renewal", "\nNo release"}
	at := make([]int, len(heads))
	for i, head := range heads {
		at[i] = strings.Index(out, head)
		if at[i] < 0 || (i > 0 && at[i] < at[i-1]) {
			t.Fatalf("output lacks %q in the served order of the Releases:\n%s", head, out)
		}
	}
	cutover, autumn, renewal := out[at[0]:at[1]], out[at[1]:at[2]], out[at[2]:at[3]]

	for _, want := range []struct{ release, block, phrase string }{
		{"Cutover", cutover, "Modernization · replacing Legacy billing"},
		{"Cutover", cutover, "Reference revision: a1b2c3d4"},
		{"Cutover", cutover, "Billing (BILL): 3 of 4 undecided"},
		{"Cutover", cutover, "Integrations (INTG): 3 of 3 undecided"},
		{"Renewal", renewal, "Modernization · in place"},
		{"Renewal", renewal, "Reference revision: not set"},
	} {
		if !strings.Contains(out, want.phrase) {
			t.Errorf("output lacks %q:\n%s", want.phrase, out)
		} else if !strings.Contains(want.block, want.phrase) {
			t.Errorf("%q is not printed under the Release %s:\n%s", want.phrase, want.release, out)
		}
	}
	if strings.Contains(renewal, "replacing") {
		t.Errorf("the in-place Release Renewal says it is replacing a legacy system:\n%s", out)
	}
	for _, absent := range []string{"Modernization", "Reference revision", "undecided"} {
		if strings.Contains(autumn, absent) {
			t.Errorf("the Delivery Release Autumn prints %q:\n%s", absent, out)
		}
	}
}
