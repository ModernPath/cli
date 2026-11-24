package cmd

// Epic 459 (H2), SR "One set of status and proof words in the command line",
// criterion 0 (86b19856): roadmap show prints under an Initiative that is
// built, accepted or done the web app's stage phrase (Built · waits for
// acceptance, Accepted · and what remains, Done or Done · accepted), and under
// any other Not done or No work yet as before; under one that is not done it
// prints each proof item of waiting_for with its step words and turn as the
// web app shows them, with its count or the refusal's sentence. --json carries
// progress, accepted, done_when and waiting_for. initiative get and initiative
// list print the same phrase on the Initiative's line, where they printed the
// progress code; a work item's line keeps its progress code (plan review 3).
//
// The served reads are the core's after this Epic (contract §3 and §5). At the
// Epic's base roadmap show prints "Not done: 2 of 2 Epics complete" where
// "Built · waits for acceptance" is expected and the proof items' step codes,
// and initiative list prints the code built, so the assertions on the printed
// lines fail. The tests use no new Go symbol.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const h2StageNotModernization = "The initiative's release does not replace a legacy system."

// h2StageRoadmap holds, under the open Modernization Release Orders
// modernization, a built Initiative, an accepted one with rows below the agreed
// level, rows not proved yet and an open difference, a done one and an
// accepted one whose rows below were accepted by name; under Returns
// modernization, with no reference revision, an accepted one whose proof waits
// for it; under Winter cutover, closed since D3, an accepted and a built one
// without the proof item; under the Delivery Release Spring new work done and
// accepted, new work done, new work in progress and a replacing Initiative
// whose proof cannot be read; under no Release a built one whose proof cannot
// be read and an idea.
const h2StageRoadmap = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"ORD","name":"Orders"}],
"releases":[
 {"id":31,"name":"Orders modernization","status":"active","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy orders",
  "legacy_reference_revision":"abc1234","modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
  {"id":"c2000001-0000-4000-8000-000000000001","text":"Order history","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":31,"epic_ids":[71,72],
   "progress":"built","accepted":false,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_not_started","release":{"id":31,"name":"Orders modernization"},"report_id":null,"counts":{"passed":0,"excluded":0,"failed":0,"not_tested":2,"total":2}},
     {"key":"differences_resolved","met":true,"open":0},
     {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":3,"as_agreed":2,"below":1,"below_accepted":0,"not_yet":0}}],
   "waiting_for":[
     {"kind":"acceptance","id":null,"display_ref":null,"system_id":1,"title":"Orders modernization","step":"acceptance_not_started","turn":"person","member_epic":null},
     {"kind":"proof","id":"c2000001-0000-4000-8000-000000000001","display_ref":null,"title":null,"system_id":1,"step":"proof_below_agreed_level","turn":"person","member_epic":null,"count":1}]},
  {"id":"c2000002-0000-4000-8000-000000000002","text":"Order export","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":31,"epic_ids":[73],
   "progress":"accepted","accepted":true,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":31,"name":"Orders modernization"},"report_id":"c2e00001-0000-4000-8000-000000000001","counts":{"passed":3,"excluded":0,"failed":0,"not_tested":0,"total":3}},
     {"key":"differences_resolved","met":false,"open":1},
     {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":2,"below":2,"below_accepted":0,"not_yet":1}}],
   "waiting_for":[
     {"kind":"difference","id":"c2d00001-0000-4000-8000-000000000001","display_ref":"Gap 4","system_id":1,"title":"Totals differ","step":"difference_open","turn":"team","member_epic":null},
     {"kind":"proof","id":"c2000002-0000-4000-8000-000000000002","display_ref":null,"title":null,"system_id":1,"step":"proof_below_agreed_level","turn":"person","member_epic":null,"count":2},
     {"kind":"proof","id":"c2000002-0000-4000-8000-000000000002","display_ref":null,"title":null,"system_id":1,"step":"proof_not_yet","turn":"team","member_epic":null,"count":1}]},
  {"id":"c2000003-0000-4000-8000-000000000003","text":"Order search","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":31,"epic_ids":[74],
   "progress":"done","accepted":true,"done":true,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":31,"name":"Orders modernization"},"report_id":"c2e00001-0000-4000-8000-000000000001","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":true,"open":0},
     {"key":"proof_as_agreed","met":true,"waits_for_reference_revision":false,"counts":{"kept_or_changed":3,"as_agreed":3,"below":0,"below_accepted":0,"not_yet":0}}],
   "waiting_for":[]},
  {"id":"c2000004-0000-4000-8000-000000000004","text":"Order archive","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":31,"epic_ids":[75],
   "progress":"accepted","accepted":true,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":31,"name":"Orders modernization"},"report_id":"c2e00001-0000-4000-8000-000000000001","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":false,"open":2},
     {"key":"proof_as_agreed","met":true,"waits_for_reference_revision":false,"counts":{"kept_or_changed":2,"as_agreed":1,"below":0,"below_accepted":1,"not_yet":0}}],
   "waiting_for":[
     {"kind":"difference","id":"c2d00002-0000-4000-8000-000000000002","display_ref":"Gap 5","system_id":1,"title":"Dates in UTC","step":"difference_open","turn":"team","member_epic":null},
     {"kind":"difference","id":"c2d00003-0000-4000-8000-000000000003","display_ref":"Gap 6","system_id":1,"title":"Archive sorted by id","step":"difference_open","turn":"team","member_epic":null}]}]},
 {"id":32,"name":"Returns modernization","status":"active","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy orders",
  "legacy_reference_revision":null,"modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
  {"id":"c2000005-0000-4000-8000-000000000005","text":"Return labels","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":32,"epic_ids":[76],
   "progress":"accepted","accepted":true,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":32,"name":"Returns modernization"},"report_id":"c2e00002-0000-4000-8000-000000000002","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":true,"open":0},
     {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":true,"counts":{"kept_or_changed":2,"as_agreed":0,"below":0,"below_accepted":0,"not_yet":2}}],
   "waiting_for":[
     {"kind":"proof","id":"c2000005-0000-4000-8000-000000000005","display_ref":null,"title":null,"system_id":1,"step":"proof_waits_for_reference_revision","turn":"person","member_epic":null}]}]},
 {"id":33,"name":"Winter cutover","status":"released","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy orders",
  "legacy_reference_revision":"abc0000","modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
  {"id":"c2000006-0000-4000-8000-000000000006","text":"Old invoices","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":33,"epic_ids":[61],
   "progress":"accepted","accepted":true,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":33,"name":"Winter cutover"},"report_id":"c2e00003-0000-4000-8000-000000000003","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":false,"open":1}],
   "waiting_for":[
     {"kind":"difference","id":"c2d00004-0000-4000-8000-000000000004","display_ref":"Gap 2","system_id":1,"title":"Rounding","step":"difference_open","turn":"team","member_epic":null}]},
  {"id":"c2000007-0000-4000-8000-000000000007","text":"Old statements","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":33,"epic_ids":[62],
   "progress":"built","accepted":false,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_in_progress","release":{"id":33,"name":"Winter cutover"},"report_id":"c2e00003-0000-4000-8000-000000000003","counts":{"passed":0,"excluded":0,"failed":0,"not_tested":1,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[
     {"kind":"acceptance","id":"c2e00003-0000-4000-8000-000000000003","display_ref":null,"system_id":1,"title":"Winter cutover","step":"acceptance_in_progress","turn":"person","member_epic":null}]}]},
 {"id":7,"name":"Spring","status":"active","deadline":"2026-12-15","initiatives":[
  {"id":"c2000008-0000-4000-8000-000000000008","text":"Gift cards","horizon":"now","context":"ORD","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[44,45],
   "progress":"done","accepted":true,"done":true,
   "done_when":[{"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[]},
  {"id":"c2000009-0000-4000-8000-000000000009","text":"Tax report","horizon":"now","context":null,"new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[46],
   "progress":"done","accepted":false,"done":true,
   "done_when":[{"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[]},
  {"id":"c2000010-0000-4000-8000-000000000010","text":"Card wallet","horizon":"now","context":"ORD","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[41,42],
   "progress":"in_progress","accepted":false,"done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":1,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"epic","id":42,"display_ref":null,"system_id":1,"title":"Wallet screens","step":"building","turn":"team","member_epic":null}]},
  {"id":"c2000011-0000-4000-8000-000000000011","text":"Loyalty export","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":7,"epic_ids":[47],
   "progress":"accepted","accepted":true,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":7,"name":"Spring"},"report_id":"c2e00004-0000-4000-8000-000000000004","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":true,"open":0},
     {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"reason":{"code":"release_not_modernization","message":"` + h2StageNotModernization + `"}}],
   "waiting_for":[
     {"kind":"proof","id":"c2000011-0000-4000-8000-000000000011","display_ref":null,"title":null,"system_id":1,"step":"proof_unreadable","turn":"person","member_epic":null,
      "reason":{"code":"release_not_modernization","message":"` + h2StageNotModernization + `"}}]}]}],
"no_release":{"now":[
  {"id":"c2000012-0000-4000-8000-000000000012","text":"Ledger export","horizon":"now","context":"ORD","new_context":false,"work_kind":"replaces_legacy","release_id":null,"epic_ids":[67],
   "progress":"built","accepted":false,"done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_not_started","release":null,"report_id":null,"counts":{"passed":0,"excluded":0,"failed":0,"not_tested":1,"total":1}},
     {"key":"differences_resolved","met":true,"open":0},
     {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"reason":{"code":"release_not_modernization","message":"` + h2StageNotModernization + `"}}],
   "waiting_for":[
     {"kind":"acceptance","id":null,"display_ref":null,"system_id":1,"title":null,"step":"acceptance_not_started","turn":"person","member_epic":null},
     {"kind":"proof","id":"c2000012-0000-4000-8000-000000000012","display_ref":null,"title":null,"system_id":1,"step":"proof_unreadable","turn":"person","member_epic":null,
      "reason":{"code":"release_not_modernization","message":"` + h2StageNotModernization + `"}}]}],
 "next":[
  {"id":"c2000013-0000-4000-8000-000000000013","text":"Points for every order","horizon":"next","context":"Loyalty","new_context":true,"work_kind":"new_work","release_id":null,"epic_ids":[],
   "progress":"idea","accepted":false,"done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"initiative","id":"c2000013-0000-4000-8000-000000000013","display_ref":null,"system_id":1,"title":"Points for every order","step":"not_shaped","turn":"team","member_epic":null}]}],
 "later":[]}}}`

var h2StageTitles = []string{"Order history", "Order export", "Order search", "Order archive", "Return labels", "Old invoices", "Old statements",
	"Gift cards", "Tax report", "Card wallet", "Loyalty export", "Ledger export", "Points for every order"}

// The wait lines of the proof items, in the Overview's words (contract §5).
const (
	h2WaitBelowOne   = "Waits for 1 proved below the agreed level: Prove it at the agreed level or accept the lower level by name, A person on the team"
	h2WaitBelowTwo   = "Waits for 2 proved below the agreed level: Prove it at the agreed level or accept the lower level by name, A person on the team"
	h2WaitNotYetOne  = "Waits for 1 not proved yet: Waits for a run that proves it, The team"
	h2WaitReference  = "Waits for Reference revision: Set the Release's reference revision, A person on the team"
	h2WaitUnreadable = "Waits for Proof cannot be read: The initiative's release does not replace a legacy system, A person on the team"
)

// h2Trimmed is each line with its indent removed.
func h2Trimmed(lines []string) []string {
	trimmed := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed = append(trimmed, strings.TrimSpace(line))
	}
	return trimmed
}

// 2c82edf3-4506-41de-bb4f-a7f16d830b38
func TestRoadmapShowPrintsTheStagePhraseAndTheProofWaits(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, h2StageRoadmap)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")
	blocks := roadmapBlocks(out, h2StageTitles)
	for _, title := range h2StageTitles {
		if _, ok := blocks[title]; !ok {
			t.Fatalf("output has no Initiative %q:\n%s", title, out)
		}
	}

	for _, c := range []struct {
		title, phrase string
		waits         []string
	}{
		{"Order history", "Built · waits for acceptance", []string{
			"Waits for Acceptance for Orders modernization: Acceptance test not started, A person on the team", h2WaitBelowOne}},
		{"Order export", "Accepted · 2 proved below the agreed level · 1 not proved yet · 1 open difference", []string{
			"Waits for Gap 4 Totals differ: Open difference: fix it or accept it by name, The team", h2WaitBelowTwo, h2WaitNotYetOne}},
		{"Order search", "Done", []string{}},
		// The proof item is met (its row below was accepted by name): only the open differences remain.
		{"Order archive", "Accepted · 2 open differences", []string{
			"Waits for Gap 5 Dates in UTC: Open difference: fix it or accept it by name, The team",
			"Waits for Gap 6 Archive sorted by id: Open difference: fix it or accept it by name, The team"}},
		// While the proof waits for the reference revision, neither count shows.
		{"Return labels", "Accepted · proof waits for the reference revision", []string{h2WaitReference}},
		// Under a Release closed since D3: no proof item, the words change only.
		{"Old invoices", "Accepted · 1 open difference", []string{
			"Waits for Gap 2 Rounding: Open difference: fix it or accept it by name, The team"}},
		{"Old statements", "Built · waits for acceptance", []string{
			"Waits for Acceptance for Winter cutover: Acceptance test in progress, A person on the team"}},
		// New work: Done · accepted only for a done_when with no acceptance item.
		{"Gift cards", "Done · accepted", []string{}},
		{"Tax report", "Done", []string{}},
		// The proof read refuses: the stage phrase and the wait say why.
		{"Loyalty export", "Accepted · proof cannot be read", []string{h2WaitUnreadable}},
		{"Ledger export", "Built · waits for acceptance", []string{
			"Waits for Acceptance: Acceptance test not started, A person on the team", h2WaitUnreadable}},
	} {
		t.Run(c.title, func(t *testing.T) {
			block := blocks[c.title]
			requireRoadmapMatch(t, c.title, block, `(?m)^\s+`+regexp.QuoteMeta(c.phrase)+`$`)
			requireNoRoadmapMatch(t, c.title, block, `(?i)\bnot done\b|No work yet`)
			if waits := h2Trimmed(d3WaitLines(block)); !slices.Equal(waits, c.waits) {
				t.Errorf("%s waits for %q; want %q\n%s", c.title, waits, c.waits, block)
			}
		})
	}

	t.Run("any other Initiative prints Not done or No work yet as before", func(t *testing.T) {
		wallet := blocks["Card wallet"]
		requireRoadmapMatch(t, "Card wallet", wallet, `(?m)^\s+Not done: 1 of 2 Epics complete$`)
		requireRoadmapMatch(t, "Card wallet", roadmapLineWith(wallet, "Wallet screens"), `Waits for Wallet screens: Being built, The team$`)
		points := blocks["Points for every order"]
		requireRoadmapMatch(t, "Points for every order", points, `(?m)^\s+No work yet$`)
		requireRoadmapMatch(t, "Points for every order", points, `Waits for the team to shape it into work`)
	})

	t.Run("no line says Completed or a step code", func(t *testing.T) {
		for _, word := range []string{"Completed", "proof_below_agreed_level", "proof_not_yet", "proof_waits_for_reference_revision", "proof_unreadable",
			"release_not_modernization", "less well", "as well as expected", "less than expected", "should reach"} {
			if strings.Contains(out, word) {
				t.Errorf("roadmap show prints %q:\n%s", word, out)
			}
		}
	})
}

// 2c82edf3-4506-41de-bb4f-a7f16d830b38
func TestRoadmapShowJSONCarriesProgressAcceptedDoneWhenAndWaitingFor(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, h2StageRoadmap)
	cobraWorkspace(t, server, 1)
	out, err := runRootStdout(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, out)
	}
	var printed, served map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("roadmap show --json is not JSON: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(h2StageRoadmap), &served); err != nil {
		t.Fatal(err)
	}
	initiatives := func(data map[string]any) map[string]map[string]any {
		all := map[string]map[string]any{}
		add := func(list any) {
			items, _ := list.([]any)
			for _, item := range items {
				if m, ok := item.(map[string]any); ok {
					all[str(m, "text")] = m
				}
			}
		}
		releases, _ := data["releases"].([]any)
		for _, r := range releases {
			release, _ := r.(map[string]any)
			add(release["initiatives"])
		}
		noRelease, _ := data["no_release"].(map[string]any)
		for _, horizon := range []string{"now", "next", "later"} {
			add(noRelease[horizon])
		}
		return all
	}
	want := initiatives(served["data"].(map[string]any))
	got := initiatives(printed)
	if len(want) != len(h2StageTitles) {
		t.Fatalf("the fixture holds %d Initiatives; want %d", len(want), len(h2StageTitles))
	}
	for _, title := range h2StageTitles {
		for _, key := range []string{"progress", "accepted", "done_when", "waiting_for"} {
			if _, present := got[title][key]; !present {
				t.Errorf("%s: --json carries no %s", title, key)
				continue
			}
			gotJSON, _ := json.Marshal(got[title][key])
			wantJSON, _ := json.Marshal(want[title][key])
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("%s: %s = %s; want %s", title, key, gotJSON, wantJSON)
			}
		}
	}
}

// h2StageLines is the bound System's list of Initiatives as GET
// /api/roadmap/lines serves it: each line with its progress, accepted and
// done_when, and its work, whose own progress code stays as served.
const h2StageLines = `{"data":[
 {"id":"c2000001-0000-4000-8000-000000000001","text":"Order history","release_id":31,"horizon":"now","progress":"built","accepted":false,
  "done_when":[{"key":"work_complete","met":true},{"key":"acceptance","met":false},{"key":"differences_resolved","met":true,"open":0},
   {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":3,"as_agreed":2,"below":1,"below_accepted":0,"not_yet":0}}],
  "work":[{"kind":"epic","id":71,"title":"History screens","status":"DONE","own":true,"progress":"done"}]},
 {"id":"c2000002-0000-4000-8000-000000000002","text":"Order export","release_id":31,"horizon":"now","progress":"accepted","accepted":true,
  "done_when":[{"key":"work_complete","met":true},{"key":"acceptance","met":true},{"key":"differences_resolved","met":false,"open":1},
   {"key":"proof_as_agreed","met":false,"waits_for_reference_revision":false,"counts":{"kept_or_changed":5,"as_agreed":2,"below":2,"below_accepted":0,"not_yet":1}}],
  "work":[{"kind":"epic","id":73,"title":"Export engine","status":"DONE","own":true,"progress":"done"}]},
 {"id":"c2000008-0000-4000-8000-000000000008","text":"Gift cards","release_id":7,"horizon":"now","progress":"done","accepted":true,
  "done_when":[{"key":"work_complete","met":true}],
  "work":[{"kind":"epic","id":44,"title":"Gift card balance","status":"DONE","own":true,"progress":"done"}]},
 {"id":"c2000010-0000-4000-8000-000000000010","text":"Card wallet","release_id":7,"horizon":"now","progress":"in_progress","accepted":false,
  "done_when":[{"key":"work_complete","met":false}],
  "work":[{"kind":"epic","id":42,"title":"Wallet screens","status":"IN_PROGRESS","own":true,"progress":"in_progress"}]}]}`

// 2c82edf3-4506-41de-bb4f-a7f16d830b38
func TestInitiativeListAndGetPrintTheStagePhraseOnTheInitiativesLine(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, h2StageLines)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "initiative", "list")
	if err != nil {
		t.Fatalf("initiative list: %v\n%s", err, out)
	}
	for _, want := range []string{
		"c2000001-0000-4000-8000-000000000001  Order history  release 31  horizon now  Built · waits for acceptance",
		"c2000002-0000-4000-8000-000000000002  Order export  release 31  horizon now  Accepted · 2 proved below the agreed level · 1 not proved yet · 1 open difference",
		"c2000008-0000-4000-8000-000000000008  Gift cards  release 7  horizon now  Done · accepted",
		// An earlier step keeps its code, as before.
		"c2000010-0000-4000-8000-000000000010  Card wallet  release 7  horizon now  in_progress",
	} {
		if !strings.Contains(out, want+"\n") {
			t.Errorf("initiative list lacks the line %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "  built\n") || strings.Contains(out, "  accepted\n") || strings.Contains(out, "Completed") {
		t.Errorf("initiative list prints a progress code or Completed where the stage phrase belongs:\n%s", out)
	}

	out, err = runRoot(t, "initiative", "get", "c2000002-0000-4000-8000-000000000002")
	if err != nil {
		t.Fatalf("initiative get: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("initiative get printed %d lines; want the Initiative's line and its work:\n%s", len(lines), out)
	}
	if want := "c2000002-0000-4000-8000-000000000002  Order export  release 31  horizon now  Accepted · 2 proved below the agreed level · 1 not proved yet · 1 open difference"; lines[0] != want {
		t.Errorf("the Initiative's line = %q; want %q", lines[0], want)
	}
	// The work item's line keeps its progress code (plan review 3).
	if work := roadmapLineWith(out, "Export engine"); !strings.HasSuffix(work, "  done") || strings.Contains(work, "Accepted") {
		t.Errorf("the work line = %q; want it to end with its progress code done", work)
	}
}
