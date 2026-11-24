package cmd

// Epic 337 (D3), test case TC-D3-16 (79d07d53-8a9f-476b-8abd-955a77a49f7e).
//
// Criterion 6866ccc8 ("An Initiative that replaces part of the legacy system
// is done only with a signed-off report that covers it and every difference
// resolved" at 6): roadmap proof prints the acceptance, with the report's
// state and each requirement's state, the open differences with their
// references, and the Initiative's Done when items with where each stands, as
// the Overview tab lists them. roadmap show prints, under an Initiative that is
// not done, the acceptance line as Acceptance for and the Release's name, and
// each difference line as its reference and title, each with its step and
// whose turn it is in the words the Overview tab uses.
//
// Criterion de2006b8 ("roadmap show says whether each Initiative is done" at
// 2): roadmap show prints, for each Initiative, Done, No work yet, or Not done
// with n of m Epics, user requirements and system requirements complete,
// naming only the kinds with an item that counts. An Initiative that replaces
// part of the legacy system prints Done only when the read says so; one whose
// read gives work_complete alone, as the read gives one under a Release whose
// last close came before D3 was deployed, prints Done when its work is
// complete, as before. Under an Initiative that is not done it prints one line
// per item of waiting_for, with Waits for the team to shape it into work for
// one with no linked work that counts; for a replacing Initiative these lines
// include its acceptance line and each difference line. An Initiative of new
// work whose own link is a system requirement prints Done when that
// requirement is DONE, and Not done with 0 of 1 system requirements and one
// line when it is PROPOSED. --json carries done, done_when and waiting_for.
//
// On slice 1 the test server serves the new fields and the commands print
// none of them: roadmap proof prints no acceptance, difference or Done when
// line, and roadmap show prints the acceptance item's title and the step codes
// as served, so the assertions on the printed lines and on the step and turn
// words fail. The assertions that take over A3's test case a0258698, and that
// a replacing Initiative whose read gives work_complete alone prints as
// before, hold on the slice and guard them. The test uses no new Go symbol.
//
// Review round 2: roadmap proof prints a difference under Waits for only while
// done_when holds differences_resolved unmet (SB5, criterion 901145a1: a done
// Initiative has none, and neither has one under a Release closed before D3's
// deploy). Before the fix it printed every difference open today, whatever
// done_when said. An entry with done true and every item met prints Done with
// each item met (SP8), which no fixture covered.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const d3ProofReport = "77777777-7777-4777-8777-777777777777"

const d3ProofEntry = `{"data":{
 "initiative":{"id":"` + d2ProofInitiative + `","title":"Provider connections","context":"INTG"},
 "release":{"id":21,"name":"Billing modernization","reference_revision":"c0ffee1c0ffee1c0ffee1c0ffee1c0ffee1c0ffe"},
 "legacy_system":{"id":41,"name":"Legacy billing"},
 "in_place":false,
 "proof_waits_for_reference_revision":false,
 "counts":{"kept_or_changed":2,"proved":1,"proved_below":0,"proved_below_accepted":0,"not_yet":1,"revised_since_selected":0,"capture_not_reproducible":0},
 "done":false,
 "done_when":[
  {"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
  {"key":"acceptance","met":false,"step":"acceptance_in_progress","release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ProofReport + `",
   "counts":{"passed":1,"excluded":1,"failed":1,"not_tested":1,"total":4}},
  {"key":"differences_resolved","met":false,"open":1}],
 "acceptance":{"release":{"id":21,"name":"Billing modernization"},"state":"in_progress","report":{"id":"` + d3ProofReport + `","status":"draft"},
  "counts":{"passed":1,"excluded":1,"failed":1,"not_tested":1,"total":4},
  "requirements":[
   {"requirement_kind":"user_requirement","requirement_id":"c1c1c1c1-1111-4111-8111-111111111111","display_ref":"UR-40-0","title":"Export invoices as PDF","revision":2,
    "spec_id":"d1d1d1d1-1111-4111-8111-111111111111","state":"passed","outcome":"pass","notes":null,
    "legacy":[{"kind":"user_requirement","id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-12-0","title":"Export invoices"}]},
   {"requirement_kind":"user_requirement","requirement_id":"c2c2c2c2-2222-4222-8222-222222222222","display_ref":"UR-41-0","title":"Retry webhooks","revision":1,
    "spec_id":"d2d2d2d2-2222-4222-8222-222222222222","state":"failed","outcome":"fail","notes":"The retries stop after three attempts",
    "legacy":[{"kind":"user_requirement","id":"a2a2a2a2-2222-4222-8222-222222222222","display_ref":"UR-13-0","title":"Retry the partner feed"}]},
   {"requirement_kind":"user_requirement","requirement_id":"c3c3c3c3-3333-4333-8333-333333333333","display_ref":"UR-42-0","title":"Show the monthly bill","revision":1,
    "spec_id":"d3d3d3d3-3333-4333-8333-333333333333","state":"excluded","outcome":"not_run","notes":"Needs the live payment provider",
    "legacy":[{"kind":"user_requirement","id":"a3a3a3a3-3333-4333-8333-333333333333","display_ref":"UR-14-0","title":"Bill monthly"}]},
   {"requirement_kind":"user_requirement","requirement_id":"c4c4c4c4-4444-4444-8444-444444444444","display_ref":"UR-43-0","title":"Send the bill by email","revision":3,
    "spec_id":"d4d4d4d4-4444-4444-8444-444444444444","state":"not_tested","outcome":null,"notes":null,
    "legacy":[{"kind":"user_requirement","id":"a4a4a4a4-4444-4444-8444-444444444444","display_ref":"UR-15-0","title":"Mail the bill"}]}],
  "waits_for_requirements":[]},
 "differences":[
  {"id":"e1e1e1e1-1111-4111-8111-111111111111","display_ref":"Gap 7","title":"Retries stop after three","disposition":"OPEN","state":"open","open":true,
   "legacy_requirement":{"kind":"user_requirement","id":"a2a2a2a2-2222-4222-8222-222222222222","display_ref":"UR-13-0","title":"Retry the partner feed"},
   "requirement":null,"initiative":{"id":"` + d2ProofInitiative + `","title":"Provider connections"},
   "what_changed":"Retries stop after three attempts instead of five","evidence":"The replay fails on the fourth retry",
   "classification":"unclassified","changes_stored_values":null,"accepting_accepts":null,"risk":null,
   "accepted_by":null,"accepted_words":null,"accepted_at":null},
  {"id":"e2e2e2e2-2222-4222-8222-222222222222","display_ref":"Gap 9","title":"Totals rounded to cents","disposition":"ACCEPTED with USER:accepted-on-proof-tab","state":"accepted","open":false,
   "legacy_requirement":{"kind":"user_requirement","id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-12-0","title":"Export invoices"},
   "requirement":{"kind":"user_requirement","id":"c1c1c1c1-1111-4111-8111-111111111111","display_ref":"UR-40-0","title":"Export invoices as PDF"},
   "initiative":{"id":"` + d2ProofInitiative + `","title":"Provider connections"},
   "what_changed":"Totals are rounded to cents","evidence":"Run 12, test Totals",
   "classification":"not_defect","changes_stored_values":false,"accepting_accepts":"Totals in cents","risk":"A report that sums fractions",
   "accepted_by":{"id":5,"name":"Dana Domain"},"accepted_words":"Cents are what the bank books","accepted_at":"2026-11-03T10:00:00Z"}],
 "open_differences":1,
 "rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-12-0","title":"Export invoices","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"tests_only","reached_reason":null,"expected_level":"tests_only","expected_level_basis":"estimated","proved":true,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null,
   "differences":[{"id":"e2e2e2e2-2222-4222-8222-222222222222","display_ref":"Gap 9","disposition":"ACCEPTED with USER:accepted-on-proof-tab","state":"accepted","open":false}]},
  {"requirement_kind":"user_requirement","requirement_id":"a2a2a2a2-2222-4222-8222-222222222222","display_ref":"UR-13-0","title":"Retry the partner feed","context":"INTG",
   "outcome":"keep","decision":{"reason":null,"expected_level":null,"expected_level_reason":null,"set_by":{"id":5,"name":"Pia Planner"}},
   "counterparts":[],"reached_level":"not_yet","reached_reason":"its test fails","expected_level":"tests_only","expected_level_basis":"estimated","proved":false,"drafted":true,
   "capture_not_reproducible":false,"revised_since_selected":false,"legacy_changed_since_recorded":false,"run":null,
   "differences":[{"id":"e1e1e1e1-1111-4111-8111-111111111111","display_ref":"Gap 7","disposition":"OPEN","state":"open","open":true}]}
 ]}}`

// d3LineMatching is the first line of text that matches pattern, or "".
func d3LineMatching(text, pattern string) string {
	re := regexp.MustCompile(pattern)
	for _, line := range strings.Split(text, "\n") {
		if re.MatchString(line) {
			return line
		}
	}
	return ""
}

func TestD3_79d07d53_ProofPrintsTheAcceptanceTheDifferencesAndDoneWhen(t *testing.T) {
	server, seen := d2ProofServer(t, http.StatusOK, d3ProofEntry)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	if len(*seen) != 1 {
		t.Fatalf("the proof route received %d requests; want 1", len(*seen))
	}
	plain := g3Plain(out)

	// The Initiative's Done when items, with where each stands, as the Overview tab lists them.
	for _, line := range []struct{ what, pattern string }{
		{"Done when, not done yet", `^Done when: Not done yet$`},
		{"All linked work is complete, met with 2 of 2 Epics", `^\s+All linked work is complete: met, 2 of 2 Epics$`},
		{"Acceptance recorded, not met, in progress", `^\s+Acceptance recorded: not met, Acceptance test in progress$`},
		{"No open difference, not met with 1 open", `^\s+No open difference: not met, 1 open$`},
		{"what it waits for: the acceptance line",
			`^\s+Waits for Acceptance for Billing modernization: Acceptance test in progress, A person on the team$`},
		{"what it waits for: the open difference with its reference",
			`^\s+Waits for Gap 7 Retries stop after three: Open difference: fix it or accept it by name, The team$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap proof does not print %s: no line matches %s", line.what, line.pattern)
		}
	}

	// The acceptance, with the report's state and each requirement's state.
	head := d3LineMatching(out, `^Acceptance for Billing modernization: `)
	if head == "" {
		t.Errorf("roadmap proof prints no acceptance line for the Release")
	}
	for _, want := range []string{"Acceptance test in progress", "report draft", "1 passed, 1 excluded, 1 failed, 1 not tested yet of 4"} {
		if !strings.Contains(head, want) {
			t.Errorf("the acceptance line does not say %q: %q", want, head)
		}
	}
	for _, requirement := range []struct{ ref, state string }{
		{"UR-40-0 Export invoices as PDF, revision 2", ": passed"},
		{"UR-41-0 Retry webhooks, revision 1", ": failed · The retries stop after three attempts"},
		{"UR-42-0 Show the monthly bill, revision 1", ": excluded · Needs the live payment provider"},
		{"UR-43-0 Send the bill by email, revision 3", ": not tested yet"},
	} {
		line := d3LineMatching(out, `^\s+`+regexp.QuoteMeta(requirement.ref))
		if !strings.Contains(line, requirement.ref+requirement.state) {
			t.Errorf("roadmap proof does not print %s with its state %q: %q", requirement.ref, requirement.state, line)
		}
	}

	// The differences with their references: the open one, and the accepted one a mask cites.
	for _, line := range []struct{ what, pattern string }{
		{"the count of open differences", `^Differences: 1 open of 2$`},
		{"the open difference with its reference", `^\s+Gap 7 Retries stop after three: open · names UR-13-0 Retry the partner feed$`},
		{"the accepted difference with its reference", `^\s+Gap 9 Totals rounded to cents: accepted by Dana Domain · names UR-12-0 Export invoices$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap proof does not print %s: no line matches %s", line.what, line.pattern)
		}
	}
	// Each row names its differences by reference and state.
	if row := d3LineMatching(out, `UR-13-0 Retry the partner feed: keep`); !strings.Contains(row, "differences: Gap 7 (open)") {
		t.Errorf("the row of UR-13-0 does not name its difference: %q", row)
	}
	if row := d3LineMatching(out, `UR-12-0 Export invoices: keep`); !strings.Contains(row, "differences: Gap 9 (accepted)") {
		t.Errorf("the row of UR-12-0 does not name its difference: %q", row)
	}
	for _, code := range []string{"acceptance_in_progress", "not_tested", "differences_resolved", "work_complete", "difference_open"} {
		if strings.Contains(plain, code) {
			t.Errorf("roadmap proof prints the code %q instead of its words:\n%s", code, out)
		}
	}
}

// Review round 1 (finding 5): a requirement an approved Epic still builds reads
// in progress, and one with no spec at its current revision spec pending, in
// words; the acceptance waits for the requirements. Before the fix roadmap proof
// printed the codes in_progress and spec_pending as served.
func TestD3_79d07d53_ProofPrintsInProgressAndSpecPendingInWords(t *testing.T) {
	entry := strings.Replace(d3ProofEntry, `"acceptance":{"release":{"id":21,"name":"Billing modernization"},"state":"in_progress"`,
		`"acceptance":{"release":{"id":21,"name":"Billing modernization"},"state":"waits_for_requirements"`, 1)
	entry = strings.Replace(entry, `"state":"not_tested","outcome":null,"notes":null`, `"state":"in_progress","outcome":null,"notes":null`, 1)
	entry = strings.Replace(entry, `"state":"excluded","outcome":"not_run","notes":"Needs the live payment provider"`,
		`"state":"spec_pending","outcome":null,"notes":null`, 1)
	if !strings.Contains(entry, `"state":"in_progress","outcome":null`) || !strings.Contains(entry, `"state":"spec_pending"`) {
		t.Fatal("setup: the entry does not carry the two states")
	}
	server, _ := d2ProofServer(t, http.StatusOK, entry)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	head := d3LineMatching(out, `^Acceptance for Billing modernization: `)
	if !strings.Contains(head, "Waits for the requirements that modernize its kept and changed requirements") {
		t.Errorf("the acceptance line does not say it waits for the requirements: %q", head)
	}
	for _, requirement := range []struct{ ref, state string }{
		{"UR-43-0 Send the bill by email, revision 3", ": in progress (an approved Epic is still building it)"},
		{"UR-42-0 Show the monthly bill, revision 1", ": spec pending (no spec at its current revision yet)"},
	} {
		line := d3LineMatching(out, `^\s+`+regexp.QuoteMeta(requirement.ref))
		if !strings.Contains(line, requirement.ref+requirement.state) {
			t.Errorf("roadmap proof does not print %s with its state %q: %q", requirement.ref, requirement.state, line)
		}
	}
	plain := g3Plain(out)
	for _, code := range []string{"in_progress", "spec_pending", "waits_for_requirements"} {
		if strings.Contains(plain, code) {
			t.Errorf("roadmap proof prints the code %q instead of its words:\n%s", code, out)
		}
	}
}

var d3ProofDoneWhenPattern = regexp.MustCompile(`(?s)"done":false,\s*"done_when":\[.*?\],\n "acceptance":`)

// d3ProofEntryWith is d3ProofEntry with its done and its Done when items
// replaced, as the read gives them; the differences stay as they stand today.
func d3ProofEntryWith(t *testing.T, done bool, doneWhen string) string {
	t.Helper()
	replacement := `"done":` + strconv.FormatBool(done) + `,"done_when":[` + doneWhen + `],
 "acceptance":`
	entry := d3ProofDoneWhenPattern.ReplaceAllLiteralString(d3ProofEntry, replacement)
	if entry == d3ProofEntry {
		t.Fatal("setup: the entry's Done when was not replaced")
	}
	return entry
}

const (
	d3ItemWorkMet        = `{"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}`
	d3ItemAcceptanceMet  = `{"key":"acceptance","met":true,"step":null,"release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ProofReport + `","counts":{"passed":4,"excluded":0,"failed":0,"not_tested":0,"total":4}}`
	d3ItemAcceptanceOpen = `{"key":"acceptance","met":false,"step":"acceptance_retest","release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ProofReport + `","counts":{"passed":3,"excluded":0,"failed":0,"not_tested":1,"total":4}}`
	d3ItemNoDifference   = `{"key":"differences_resolved","met":true,"open":0}`
	d3ItemOneDifference  = `{"key":"differences_resolved","met":false,"open":1}`
)

// SP8: a done Initiative prints Done with each item met, and waits for nothing.
func TestD3_79d07d53_ProofPrintsADoneInitiativeWithEveryItemMet(t *testing.T) {
	entry := d3ProofEntryWith(t, true, d3ItemWorkMet+","+d3ItemAcceptanceMet+","+d3ItemNoDifference)
	entry = strings.ReplaceAll(entry, `"disposition":"OPEN","state":"open","open":true`, `"disposition":"CLOSED by PR #12","state":"closed","open":false`)
	entry = strings.Replace(entry, `"open_differences":1`, `"open_differences":0`, 1)
	server, _ := d2ProofServer(t, http.StatusOK, entry)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	for _, line := range []struct{ what, pattern string }{
		{"Done when, done", `^Done when: Done$`},
		{"All linked work is complete, met", `^\s+All linked work is complete: met, 2 of 2 Epics$`},
		{"Acceptance recorded, met", `^\s+Acceptance recorded: met$`},
		{"No open difference, met", `^\s+No open difference: met$`},
	} {
		if d3LineMatching(out, line.pattern) == "" {
			t.Errorf("roadmap proof does not print %s: no line matches %s\n%s", line.what, line.pattern, out)
		}
	}
	if d3LineMatching(out, `Not done yet`) != "" {
		t.Errorf("roadmap proof of a done Initiative says Not done yet:\n%s", out)
	}
	if waits := d3WaitLines(out); len(waits) != 0 {
		t.Errorf("roadmap proof of a done Initiative prints what it waits for: %q", waits)
	}
}

// SB5: a difference open today is printed under Waits for only while done_when
// holds differences_resolved unmet; it stays listed under Differences, as the
// read gives it.
func TestD3_79d07d53_ProofPrintsADifferenceUnderWaitsForOnlyWhileTheItemIsUnmet(t *testing.T) {
	const gap7 = "  Waits for Gap 7 Retries stop after three: Open difference: fix it or accept it by name, The team"
	const retest = "  Waits for Acceptance for Billing modernization: New acceptance test needed: a spec changed after the sign-off, A person on the team"
	for _, c := range []struct {
		name     string
		done     bool
		doneWhen string
		waits    []string
	}{
		{"a done Initiative whose difference was raised after its Release closed", true,
			d3ItemWorkMet + "," + d3ItemAcceptanceMet + "," + d3ItemNoDifference, []string{}},
		{"an Initiative read as before D3's deploy, with work_complete alone", true,
			d3ItemWorkMet, []string{}},
		{"an Initiative that waits for its acceptance, with the item met as of its close", false,
			d3ItemWorkMet + "," + d3ItemAcceptanceOpen + "," + d3ItemNoDifference, []string{retest}},
		{"an Initiative with the item unmet", false,
			d3ItemWorkMet + "," + d3ItemAcceptanceMet + "," + d3ItemOneDifference, []string{gap7}},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, _ := d2ProofServer(t, http.StatusOK, d3ProofEntryWith(t, c.done, c.doneWhen))
			cobraWorkspace(t, server, 1)
			out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
			if err != nil {
				t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
			}
			if waits := d3WaitLines(out); !slices.Equal(waits, c.waits) {
				t.Errorf("roadmap proof waits for %q, want %q\n%s", waits, c.waits, out)
			}
			if d3LineMatching(out, `^\s+Gap 7 Retries stop after three: open · names UR-13-0 Retry the partner feed$`) == "" {
				t.Errorf("the difference is not listed under Differences as the read gives it:\n%s", out)
			}
		})
	}
}

// Guard: a proof entry without D3's keys, as D2's server answers, prints none of D3's lines.
func TestD3_79d07d53_ProofWithoutTheNewKeysPrintsAsBefore(t *testing.T) {
	server, _ := d2ProofServer(t, http.StatusOK, d2ProofEntry)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "proof", "--initiative", d2ProofInitiative)
	if err != nil {
		t.Fatalf("roadmap proof --initiative: %v\n%s", err, out)
	}
	for _, absent := range []string{"Done when", "Acceptance for", "Differences:", "Waits for"} {
		if strings.Contains(out, absent) {
			t.Errorf("roadmap proof of an entry without D3's keys prints %q:\n%s", absent, out)
		}
	}
}

const d3ReportID = "88888888-8888-4888-8888-888888888888"

// d3RoadmapReply holds replacing Initiatives under a Modernization Release,
// two replacing Initiatives under a Release closed before D3's deploy, whose
// read gives work_complete alone, and A3's Initiatives of new work.
const d3RoadmapReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"INTG","name":"Integrations"},{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":21,"name":"Billing modernization","status":"active","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy billing",
  "legacy_reference_revision":"c0ffee1","modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
  {"id":"d3000001-0000-4000-8000-000000000001","text":"Provider connections","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[61,62],
   "selection":{"keep":2,"change":1,"drop":0,"agreed":true,"drafted":3,"built":3},
   "done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":2,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_in_progress","release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ReportID + `","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":2,"total":3}},
     {"key":"differences_resolved","met":false,"open":1}],
   "waiting_for":[
     {"kind":"acceptance","id":"` + d3ReportID + `","display_ref":null,"system_id":1,"title":"Billing modernization","step":"acceptance_in_progress","turn":"person","member_epic":null},
     {"kind":"difference","id":"e1e1e1e1-1111-4111-8111-111111111111","display_ref":"Gap 7","system_id":1,"title":"Retries stop after three","step":"difference_open","turn":"team","member_epic":null}]},
  {"id":"d3000002-0000-4000-8000-000000000002","text":"Invoice archive","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[63],
   "selection":{"keep":1,"change":0,"drop":0,"agreed":true,"drafted":1,"built":1},
   "done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_not_started","release":{"id":21,"name":"Billing modernization"},"report_id":null,"counts":{"passed":0,"excluded":0,"failed":0,"not_tested":1,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[
     {"kind":"acceptance","id":null,"display_ref":null,"system_id":1,"title":"Billing modernization","step":"acceptance_not_started","turn":"person","member_epic":null}]},
  {"id":"d3000003-0000-4000-8000-000000000003","text":"Partner feed","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[64],
   "selection":{"keep":1,"change":0,"drop":0,"agreed":true,"drafted":1,"built":1},
   "done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_retest","release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ReportID + `","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[
     {"kind":"acceptance","id":"` + d3ReportID + `","display_ref":null,"system_id":1,"title":"Billing modernization","step":"acceptance_retest","turn":"person","member_epic":null}]},
  {"id":"d3000004-0000-4000-8000-000000000004","text":"Tax rules","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[65],
   "selection":{"keep":1,"change":1,"drop":0,"agreed":true,"drafted":2,"built":1},
   "done":false,
   "done_when":[
     {"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_waits_for_requirements","release":{"id":21,"name":"Billing modernization"},"report_id":null,"counts":{"passed":0,"excluded":0,"failed":0,"not_tested":1,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[
     {"kind":"epic","id":65,"display_ref":null,"system_id":1,"title":"Tax engine","step":"building","turn":"team","member_epic":null},
     {"kind":"acceptance","id":null,"display_ref":null,"system_id":1,"title":"Billing modernization","step":"acceptance_waits_for_requirements","turn":"team","member_epic":null}]},
  {"id":"d3000005-0000-4000-8000-000000000005","text":"Statements","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[66],
   "selection":{"keep":1,"change":0,"drop":1,"agreed":true,"drafted":1,"built":1},
   "done":true,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":true,"step":null,"release":{"id":21,"name":"Billing modernization"},"report_id":"` + d3ReportID + `","counts":{"passed":1,"excluded":0,"failed":0,"not_tested":0,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[]}]},
 {"id":19,"name":"Winter cutover","status":"released","release_type":"modernization","in_place":false,"legacy_system_name":"Legacy billing",
  "legacy_reference_revision":"c0ffee0","modernization":{"contexts":[]},"new_since_agreement_count":0,"initiatives":[
  {"id":"d3000006-0000-4000-8000-000000000006","text":"Old archive","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":19,"epic_ids":[51],
   "selection":{"keep":1,"change":0,"drop":0,"agreed":true,"drafted":1,"built":1},
   "done":true,
   "done_when":[{"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[]},
  {"id":"d3000007-0000-4000-8000-000000000007","text":"Old ledger","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":19,"epic_ids":[52,53],
   "selection":{"keep":1,"change":0,"drop":0,"agreed":true,"drafted":1,"built":0},
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":1,"counting":2},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"epic","id":53,"display_ref":null,"system_id":1,"title":"Ledger screens","step":"building","turn":"team","member_epic":null}]}]},
 {"id":7,"name":"Spring","status":"active","deadline":"2026-12-15","initiatives":[
  {"id":"11111111-1111-4111-8111-111111111111","text":"Card wallet","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[41,42],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":1,"counting":2},"user_requirement":{"done":0,"counting":1},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[
     {"kind":"epic","id":42,"display_ref":null,"system_id":1,"title":"Wallet screens","step":"completion_approval","turn":"person","member_epic":null},
     {"kind":"user_requirement","id":"88888888-8888-4888-8888-888888888881","display_ref":"UR-3","system_id":1,"title":"Customers see their wallet","step":"plan_approval","turn":"person","member_epic":{"id":43,"title":"Wallet engine"}}]},
  {"id":"33333333-3333-4333-8333-333333333333","text":"Gift cards","horizon":"now","context":"PAY","new_context":false,"work_kind":"new_work","release_id":7,"epic_ids":[44,45],
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
   "waiting_for":[]}]}],
"no_release":{"now":[
  {"id":"d3000008-0000-4000-8000-000000000008","text":"Ledger export","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":null,"epic_ids":[67],
   "selection":{"keep":1,"change":0,"drop":0,"agreed":false,"drafted":1,"built":1},
   "done":false,
   "done_when":[
     {"key":"work_complete","met":true,"counts":{"epic":{"done":1,"counting":1},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}},
     {"key":"acceptance","met":false,"step":"acceptance_not_started","release":null,"report_id":null,"counts":{"passed":0,"excluded":0,"failed":0,"not_tested":1,"total":1}},
     {"key":"differences_resolved","met":true,"open":0}],
   "waiting_for":[{"kind":"acceptance","id":null,"display_ref":null,"system_id":1,"title":null,"step":"acceptance_not_started","turn":"person","member_epic":null}]}],
 "next":[
  {"id":"22222222-2222-4222-8222-222222222222","text":"Points for every order","horizon":"next","context":"Loyalty","new_context":true,"work_kind":"new_work","release_id":null,"epic_ids":[],
   "done":false,
   "done_when":[{"key":"work_complete","met":false,"counts":{"epic":{"done":0,"counting":0},"user_requirement":{"done":0,"counting":0},"system_requirement":{"done":0,"counting":0}}}],
   "waiting_for":[{"kind":"initiative","id":"22222222-2222-4222-8222-222222222222","display_ref":null,"system_id":1,"title":"Points for every order","step":"not_shaped","turn":"team","member_epic":null}]}],
 "later":[]}}}`

var d3RoadmapTitles = []string{"Provider connections", "Invoice archive", "Partner feed", "Tax rules", "Statements", "Old archive", "Old ledger",
	"Card wallet", "Gift cards", "Receipts by email", "Tax report", "Ledger export", "Points for every order"}

// d3WaitLines are the lines of block that say what the Initiative waits for.
func d3WaitLines(block string) []string {
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "Waits for ") {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestD3_79d07d53_ShowListsTheAcceptanceAndDifferenceLinesInWords(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, d3RoadmapReply)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")
	blocks := roadmapBlocks(out, d3RoadmapTitles)
	for _, title := range d3RoadmapTitles {
		if _, ok := blocks[title]; !ok {
			t.Fatalf("output has no Initiative %q:\n%s", title, out)
		}
	}

	// Work complete, acceptance in progress and one open difference: not done, the acceptance line then the difference line.
	provider := blocks["Provider connections"]
	requireRoadmapMatch(t, "Provider connections", provider, `(?i)\bnot done\b`)
	requireRoadmapMatch(t, "Provider connections", provider, `\b2 of 2 Epics\b`)
	acceptance := roadmapLineWith(provider, "Acceptance for Billing modernization")
	requireRoadmapMatch(t, "the acceptance line of Provider connections", acceptance,
		`Waits for Acceptance for Billing modernization: Acceptance test in progress, A person on the team$`)
	difference := roadmapLineWith(provider, "Gap 7 Retries stop after three")
	requireRoadmapMatch(t, "the difference line of Provider connections", difference,
		`Waits for Gap 7 Retries stop after three: Open difference: fix it or accept it by name, The team$`)
	if acceptance != "" && difference != "" && strings.Index(provider, acceptance) > strings.Index(provider, difference) {
		t.Errorf("the lines under Provider connections are not in the served order:\n%s", provider)
	}

	// The other three steps of the acceptance, each with whose turn it is.
	for _, c := range []struct{ title, words string }{
		{"Invoice archive", "Acceptance test not started, A person on the team"},
		{"Partner feed", "New acceptance test needed: a spec changed after the sign-off, A person on the team"},
		{"Tax rules", "Waits for the requirements that modernize its kept and changed requirements, The team"},
	} {
		line := roadmapLineWith(blocks[c.title], "Acceptance for Billing modernization")
		requireRoadmapMatch(t, "the acceptance line of "+c.title, line,
			`Waits for Acceptance for Billing modernization: `+regexp.QuoteMeta(c.words)+`$`)
	}
	// An unfinished Epic comes before the acceptance line, as served.
	tax := blocks["Tax rules"]
	requireRoadmapMatch(t, "the Epic line of Tax rules", roadmapLineWith(tax, "Tax engine"), `Waits for Tax engine: Being built, The team$`)

	// One shown under no Release names no Release.
	ledgerExport := roadmapLineWith(blocks["Ledger export"], "Acceptance")
	requireRoadmapMatch(t, "the acceptance line of Ledger export", ledgerExport, `Waits for Acceptance: Acceptance test not started, A person on the team$`)

	// Done only when all three items are met: no waiting line.
	statements := blocks["Statements"]
	requireRoadmapMatch(t, "Statements", statements, `(?m)^\s+Done$`)
	if waits := d3WaitLines(statements); len(waits) != 0 {
		t.Errorf("a done replacing Initiative prints what it waits for: %q", waits)
	}

	// A replacing Initiative whose read gives work_complete alone prints as before (guard).
	oldArchive := blocks["Old archive"]
	requireRoadmapMatch(t, "Old archive", oldArchive, `(?m)^\s+Done$`)
	requireNoRoadmapMatch(t, "Old archive", oldArchive, `(?i)\bnot done\b|Acceptance|Open difference`)
	oldLedger := blocks["Old ledger"]
	requireRoadmapMatch(t, "Old ledger", oldLedger, `Not done: 1 of 2 Epics complete`)
	if waits := d3WaitLines(oldLedger); len(waits) != 1 || !strings.Contains(waits[0], "Ledger screens: Being built, The team") {
		t.Errorf("Old ledger prints %q; want the one line of its unfinished Epic", waits)
	}

	for _, code := range []string{"acceptance_in_progress", "acceptance_not_started", "acceptance_retest", "acceptance_waits_for_requirements", "difference_open"} {
		if strings.Contains(out, code) {
			t.Errorf("roadmap show prints the step code %q instead of its words:\n%s", code, out)
		}
	}
}

// Guards that take over A3's test case a0258698: the Initiatives of new work print as before.
func TestD3_79d07d53_ShowKeepsTheDoneLinesOfNewWork(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, d3RoadmapReply)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "roadmap", "show")
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	blocks := roadmapBlocks(out, d3RoadmapTitles)

	wallet := blocks["Card wallet"]
	requireRoadmapMatch(t, "Card wallet", wallet, `Not done: 1 of 2 Epics, 0 of 1 user requirements complete`)
	requireNoRoadmapMatch(t, "Card wallet", wallet, `system requirements`)
	requireRoadmapMatch(t, "the line of Wallet screens", roadmapLineWith(wallet, "Wallet screens"),
		`Waits for Wallet screens: Completion waiting for approval, A person on the team$`)
	requireRoadmapMatch(t, "the line of UR-3", roadmapLineWith(wallet, "UR-3 Customers see their wallet"),
		`Waits for UR-3 Customers see their wallet: Plan waiting for approval \(Epic Wallet engine\), A person on the team$`)

	for _, title := range []string{"Gift cards", "Tax report"} {
		requireRoadmapMatch(t, title, blocks[title], `(?m)^\s+Done$`)
		if waits := d3WaitLines(blocks[title]); len(waits) != 0 {
			t.Errorf("%s is done and prints what it waits for: %q", title, waits)
		}
	}

	receipts := blocks["Receipts by email"]
	requireRoadmapMatch(t, "Receipts by email", receipts, `Not done: 0 of 1 system requirements complete`)
	if waits := d3WaitLines(receipts); len(waits) != 1 || !strings.Contains(waits[0], "SR-12 Send each receipt as a message: Not shaped into work yet, The team") {
		t.Errorf("Receipts by email prints %q; want the one line of its system requirement", waits)
	}

	points := blocks["Points for every order"]
	requireRoadmapMatch(t, "Points for every order", points, `(?m)^\s+No work yet$`)
	requireRoadmapMatch(t, "Points for every order", points, `Waits for the team to shape it into work`)
}

func TestD3_79d07d53_ShowJSONCarriesDoneDoneWhenAndWaitingFor(t *testing.T) {
	server, _ := roadmapServer(t, http.StatusOK, d3RoadmapReply)
	cobraWorkspace(t, server, 1)
	out, err := runRootStdout(t, "roadmap", "show", "--json")
	if err != nil {
		t.Fatalf("roadmap show --json: %v\n%s", err, out)
	}
	var printed, served map[string]any
	if err := json.Unmarshal([]byte(out), &printed); err != nil {
		t.Fatalf("roadmap show --json is not JSON: %v\n%s", err, out)
	}
	if err := json.Unmarshal([]byte(d3RoadmapReply), &served); err != nil {
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
	if len(want) != len(d3RoadmapTitles) {
		t.Fatalf("the fixture holds %d Initiatives; want %d", len(want), len(d3RoadmapTitles))
	}
	for _, title := range d3RoadmapTitles {
		for _, key := range []string{"done", "done_when", "waiting_for"} {
			gotJSON, _ := json.Marshal(got[title][key])
			wantJSON, _ := json.Marshal(want[title][key])
			if string(gotJSON) != string(wantJSON) {
				t.Errorf("%s: %s = %s; want %s", title, key, gotJSON, wantJSON)
			}
		}
	}
}
