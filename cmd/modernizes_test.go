package cmd

// Epic 334 (C1), test case 84b06f8f, criteria f100edb4, ee7c0a33 and bb09aac3:
//   - `epics plan show --fields modernization` (and `--full`) prints the plan
//     read's modernization block as the server serves it;
//   - `epics plan --help` describes the modernizes kind, its rule and the
//     in-place verifies exception, and `traces list --help` names modernizes
//     among the relations (`traces list --relation modernizes` sends the
//     relation: a guard);
//   - the CLI guide, as the kit embeds it, says what a modernizes trace is and
//     which rule admits it;
//   - `roadmap apply`, with or without --dry-run, prints a held row of a
//     refused set_selection with its title, its holder, the person who
//     approved it and who can act;
//   - `roadmap show` prints drafted and built after B3's decided count on each
//     context line of a Modernization Release and after each replacing
//     Initiative's keep, change and drop counts (contract §7);
//   - `roadmap selection` writes each row's counterpart, and the counts'
//     drafted and built, as served (a guard: the command writes the rows
//     unchanged since B3).
//
// The tests use no new production symbol, so they build on the first slice,
// where the command line knows none of this; they fail on their assertions,
// except the roadmap selection guard.

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// c1FoldedText folds whitespace and strips code marks, so a phrase may wrap or
// mark its words in a help text or Markdown source.
func c1FoldedText(text string) string {
	return strings.ReplaceAll(strings.Join(strings.Fields(text), " "), "`", "")
}

// The plan read's modernization block (contract §3): onto a new system, with a
// created and a carried trace, and in place, with a taken requirement, a test
// linked to a kept requirement and a kept requirement at risk.
const c1ModernizationOntoNewSystem = `{
 "initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Billing on the new platform"},
 "legacy_system":{"id":9,"name":"Legacy billing"},
 "release":{"id":21,"name":"Cutover"},
 "in_place":false,
 "traces":[
  {"source":{"kind":"user_requirement","id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-3","title":"Export invoices"},
   "target":{"kind":"user_requirement","id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"UR-12","title":"Invoice export"},
   "decision":"keep","carried":false},
  {"source":{"kind":"system_requirement","id":"d4d4d4d4-4444-4444-8444-444444444444","display_ref":"SR-4","title":"Send reminders"},
   "target":{"kind":"system_requirement","id":"e5e5e5e5-5555-4555-8555-555555555555","display_ref":"SR-40","title":"Reminder batch"},
   "decision":null,"carried":true}],
 "taken":[],"kept_reached":[],"kept_at_risk":[]}`

const c1ModernizationInPlace = `{
 "initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Billing in place"},
 "legacy_system":{"id":4,"name":"Billing"},
 "release":{"id":22,"name":"Renewal"},
 "in_place":true,
 "traces":[],
 "taken":[{"kind":"user_requirement","id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"UR-12","title":"Invoice export","decision":"change"}],
 "kept_reached":[{"test_case":{"id":"f6f6f6f6-6666-4666-8666-666666666666","title":"Exports a month of invoices"},
   "requirement":{"kind":"user_requirement","id":"a7a7a7a7-7777-4777-8777-777777777777","display_ref":"UR-13","title":"Monthly statement"},
   "criterion_id":null,"decision":"keep"}],
 "kept_at_risk":[{"requirement_kind":"system_requirement","requirement_id":"e5e5e5e5-5555-4555-8555-555555555555","display_ref":"SR-40","title":"Reminder batch",
   "shares":[{"requirement_kind":"user_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"UR-12","title":"Invoice export","path":"lib/billing/export.ex"}]}]}`

// c1PlanShow runs `epics plan show 42` with args against a server that serves
// the plan read with the given modernization value.
func c1PlanShow(t *testing.T, modernization string, args ...string) (string, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/work/epics/42/plan" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":42,"system_id":7,"process_status":"PROPOSED","implementation_approval":null,"completion_approval":null,` +
			`"plan":{"body":"Plan","requirements":[],"traces":[]},"modernization":` + modernization + `}}`))
	}))
	defer server.Close()
	command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	command.SetArgs(append([]string{"show", "42"}, args...))
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&output)
	command.SilenceErrors = true
	command.SilenceUsage = true
	err := command.Execute()
	return output.String(), err
}

func TestC1_84b06f8f_PlanShowPrintsTheModernizationBlock(t *testing.T) {
	for _, block := range []struct{ name, served string }{
		{"onto a new system", c1ModernizationOntoNewSystem},
		{"in place", c1ModernizationInPlace},
		{"no replacing Initiative", "null"},
	} {
		var want any
		if err := json.Unmarshal([]byte(block.served), &want); err != nil {
			t.Fatal(err)
		}
		for _, flags := range [][]string{{"--fields", "modernization"}, {"--full"}} {
			name := block.name + " " + strings.Join(flags, " ")
			t.Run(name, func(t *testing.T) {
				out, err := c1PlanShow(t, block.served, append(flags, "--json")...)
				if err != nil {
					t.Fatalf("epics plan show %s refused the modernization block: %v\n%s", strings.Join(flags, " "), err, out)
				}
				var read map[string]any
				if err := json.Unmarshal([]byte(out), &read); err != nil {
					t.Fatalf("epics plan show %s --json printed no JSON object: %v\n%s", strings.Join(flags, " "), err, out)
				}
				got, present := read["modernization"]
				if !present {
					t.Fatalf("epics plan show %s --json printed no modernization key:\n%s", strings.Join(flags, " "), out)
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("epics plan show %s --json changed the modernization block:\n got %#v\nwant %#v", strings.Join(flags, " "), got, want)
				}
			})
		}
	}
	// Without --json, the block is printed for a person too.
	t.Run("onto a new system --fields modernization, for a person", func(t *testing.T) {
		out, err := c1PlanShow(t, c1ModernizationOntoNewSystem, "--fields", "modernization")
		if err != nil {
			t.Fatalf("epics plan show --fields modernization refused the modernization block: %v\n%s", err, out)
		}
		for _, text := range []string{"modernization", "Billing on the new platform", "Legacy billing", "Cutover", "UR-3", "Export invoices", "UR-12", "Invoice export"} {
			if !strings.Contains(out, text) {
				t.Errorf("epics plan show --fields modernization does not print %q:\n%s", text, out)
			}
		}
	})
}

func TestC1_84b06f8f_EpicsPlanHelpDescribesTheModernizesKind(t *testing.T) {
	out, err := runRoot(t, "epics", "plan", "--help")
	if err != nil {
		t.Fatalf("epics plan --help failed: %v", err)
	}
	help := c1FoldedText(out)
	for _, phrase := range []struct{ what, text string }{
		{"the kind", "modernizes"},
		{"its rule", "a keep or change decision of the Epic's own Initiative"},
		{"the in-place verifies exception", "In place, a verifies trace from a test case to a kept requirement outside the plan is admitted under that requirement's keep decision"},
	} {
		if !strings.Contains(help, phrase.text) {
			t.Errorf("epics plan --help does not describe %s: missing %q", phrase.what, phrase.text)
		}
	}
	if t.Failed() {
		t.Logf("epics plan --help printed:\n%s", out)
	}
}

func TestC1_84b06f8f_TraceListNamesAndSendsTheModernizesRelation(t *testing.T) {
	// Guard: --relation modernizes reaches the server as relation=modernizes.
	relations := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/api/compliance/user-requirements/") {
			_, _ = w.Write([]byte(`{"data":{"entity":{"id":"a1a1a1a1-1111-4111-8111-111111111111","system_id":4}}}`))
			return
		}
		if r.URL.Path != "/api/compliance/traces" {
			t.Errorf("unexpected request %s", r.URL)
		}
		relations = append(relations, r.URL.Query().Get("relation"))
		_, _ = w.Write([]byte(`{"data":{"items":[{"source":{"type":"user_requirement","id":"a1a1a1a1-1111-4111-8111-111111111111"},"relation":"modernizes","target":{"type":"user_requirement","id":"b2b2b2b2-2222-4222-8222-222222222222"}}],"next_cursor":null}}`))
	}))
	defer server.Close()
	list := newTraceListCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	list.SetArgs([]string{"--entity-type", "user_requirement", "--entity-id", "a1a1a1a1-1111-4111-8111-111111111111", "--relation", "modernizes", "--json"})
	var output bytes.Buffer
	list.SetOut(&output)
	list.SetErr(&output)
	list.SilenceErrors = true
	list.SilenceUsage = true
	if err := list.Execute(); err != nil {
		t.Fatalf("traces list --relation modernizes failed: %v\n%s", err, output.String())
	}
	if !reflect.DeepEqual(relations, []string{"modernizes"}) {
		t.Errorf("traces list --relation modernizes sent relations %q; want one relation=modernizes", relations)
	}

	out, err := runRoot(t, "traces", "list", "--help")
	if err != nil {
		t.Fatalf("traces list --help failed: %v", err)
	}
	if !strings.Contains(c1FoldedText(out), "modernizes") {
		t.Errorf("traces list --help does not name modernizes among the relations:\n%s", out)
	}
}

func TestC1_84b06f8f_CLIGuideDescribesTheModernizesTrace(t *testing.T) {
	guide, err := fs.ReadFile(kit.Assets(), "assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	text := c1FoldedText(string(guide))
	for _, sentence := range []string{
		"A modernizes trace links a new requirement to the legacy requirement it modernizes in another System.",
		"The platform admits it only for a legacy requirement that a keep or change decision of the Epic's own Initiative names; reverse engineering never records it.",
	} {
		if !strings.Contains(text, sentence) {
			t.Errorf("the CLI guide the kit embeds is missing the sentence %q", sentence)
		}
	}
}

// A refused set_selection whose rows a live trace holds (contract §6): an Epic
// holds row 1, a written trace's new requirement row 2; rows 3 and 4 are moves
// of rows another Initiative holds, held by an Epic and by a requirement.
const c1HeldSelectionError = `{"index":1,"op":"set_selection","reason":"selection_rows_refused","rows":[
 {"position":1,"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","title":"Export invoices","reason":"selection_row_held",
  "holder":{"kind":"epic","id":77,"title":"Billing export","approver":{"id":5,"name":"Ada Lovelace"}}},
 {"position":2,"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","title":"Send reminders","reason":"selection_row_held",
  "holder":{"kind":"requirement","id":"f6f6f6f6-6666-4666-8666-666666666666","title":"UR-12 Remind customers","approver":{"id":6,"name":"Grace Hopper"}}},
 {"position":3,"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","title":"Archive statements","reason":"selection_row_held",
  "held_by":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"},
  "holder":{"kind":"epic","id":77,"title":"Billing export","approver":{"id":5,"name":"Ada Lovelace"}}},
 {"position":4,"requirement_kind":"system_requirement","requirement_id":"e5e5e5e5-5555-4555-8555-555555555555","title":"Print receipts","reason":"selection_row_held",
  "held_by":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"},
  "holder":{"kind":"requirement","id":"f6f6f6f6-6666-4666-8666-666666666666","title":"UR-12 Remind customers","approver":{"id":6,"name":"Grace Hopper"}}}]}`

const c1HeldSelectionPlanFile = `{"request_id":"c1-held-rows","operations":[
 {"op":"set_selection","line_id":"66666666-6666-4666-8666-666666666666","rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","outcome":"drop","reason":"The bank takes it now."},
  {"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","outcome":"drop","reason":"The bank takes it now."},
  {"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","outcome":"keep","move":true},
  {"requirement_kind":"system_requirement","requirement_id":"e5e5e5e5-5555-4555-8555-555555555555","outcome":"keep","move":true}]}]}`

func TestC1_84b06f8f_ApplyPrintsAHeldRowWithItsHolder(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		args   []string
	}{
		{"without --dry-run", http.StatusUnprocessableEntity,
			`{"error":"selection_rows_refused","index":1,"errors":[` + c1HeldSelectionError + `]}`,
			nil},
		{"with --dry-run", http.StatusOK,
			`{"data":{"operations":[
			  {"index":1,"op":"set_selection","summary":"Set the selection of initiative \"Provider sync\": 2 keep, 0 change, 2 drop, 0 cleared, 2 moved","release":{"id":21,"ref":null,"name":"Cutover"}}],
			 "errors":[` + c1HeldSelectionError + `]}}`,
			[]string{"--dry-run"}},
	}
	// The row line of contract §6: `  row <position>: <client sentence>` (the sentence starts with the title),
	// with no "(held by …)" suffix.
	rows := []struct{ what, line string }{
		{"row 1, held by an approved Epic",
			"row 1: Export invoices cannot change: the Epic Billing export, approved by Ada Lovelace, relies on its decision. Ada Lovelace can return that Epic to planning."},
		{"row 2, held by a written trace's new requirement",
			"row 2: Send reminders cannot change: UR-12 Remind customers, approved by Grace Hopper, modernizes it. A later Epic that holds UR-12 Remind customers can retire that trace."},
		{"row 3, a move of a row an approved Epic holds",
			"row 3: Archive statements cannot move to this initiative: the initiative Provider connections holds its decision, and the Epic Billing export, approved by Ada Lovelace, relies on it. Ada Lovelace can return that Epic to planning."},
		{"row 4, a move of a row a written trace's new requirement holds",
			"row 4: Print receipts cannot move to this initiative: the initiative Provider connections holds its decision, and UR-12 Remind customers, approved by Grace Hopper, modernizes it. A later Epic that holds UR-12 Remind customers can retire that trace."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, c.status, c.reply)
			cobraWorkspace(t, server, 1)
			plan := writeRoadmapPlan(t, "plan.json", c1HeldSelectionPlanFile)

			out, err := runRoot(t, append([]string{"roadmap", "apply", "--file", plan}, c.args...)...)
			text := b3Text(out, err)

			// Guards: the refusal still ends the command with an error, after one request.
			if err == nil {
				t.Errorf("roadmap apply %s of a refused plan ended without an error:\n%s", c.name, out)
			}
			if len(*seen) != 1 {
				t.Errorf("roadmap apply %s sent %d requests; want 1: %#v", c.name, len(*seen), *seen)
			}
			for _, row := range rows {
				pattern := `(?m)^[ \t]*` + regexp.QuoteMeta(row.line) + `$`
				if !regexp.MustCompile(pattern).MatchString(text) {
					t.Errorf("roadmap apply %s does not print %s: no line is %q", c.name, row.what, row.line)
				}
			}
			if t.Failed() {
				t.Logf("roadmap apply %s printed:\n%s", c.name, text)
			}
		})
	}
}

// A1's Roadmap read whose counts carry drafted and built (contract §7): the
// Modernization Release Cutover replaces Legacy billing, Renewal modernizes the
// System in place, and Autumn is a Delivery Release with an Initiative of new
// work.
const c1RoadmapShowReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INTG","name":"Integrations"},{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL","INTG"],"legacy_reference_revision":"a1b2c3d4","new_since_agreement_count":0,
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":4,"undecided":1,"decided":3,"drafted":2,"built":1},
   {"code":"INTG","name":"Integrations","in_scope":3,"undecided":3,"decided":0,"drafted":0,"built":0}]},
  "initiatives":[
   {"id":"11111111-1111-4111-8111-111111111111","text":"Invoice runs","horizon":"now","context":"BILL","context_name":"Billing","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[51],
    "selection":{"keep":2,"change":1,"drop":1,"agreed":true,"drafted":2,"built":1}}]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,"new_since_agreement_count":0,
  "initiatives":[
   {"id":"22222222-2222-4222-8222-222222222222","text":"Gift cards","horizon":"next","context":"PAY","context_name":"Payments","new_context":false,"work_kind":"new_work","release_id":22,"epic_ids":[],
    "selection":null}]},
 {"id":23,"name":"Renewal","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":1,"legacy_system_name":"T","in_place":true,
  "legacy_scope":["PAY"],"legacy_reference_revision":"0f0e0d0c","new_since_agreement_count":0,
  "modernization":{"contexts":[
   {"code":"PAY","name":"Payments","in_scope":13,"undecided":6,"decided":7,"drafted":3,"built":2}]},
  "initiatives":[
   {"id":"44444444-4444-4444-8444-444444444444","text":"Ledger rewrite","horizon":"next","context":"PAY","context_name":"Payments","new_context":false,"work_kind":"replaces_legacy","release_id":23,"epic_ids":[53],
    "selection":{"keep":0,"change":2,"drop":5,"agreed":false,"drafted":2,"built":0}}]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

func TestC1_84b06f8f_RoadmapShowPrintsDraftedAndBuilt(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, c1RoadmapShowReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
	// Guards: one read of the Roadmap, printed Release by Release.
	if err != nil {
		t.Fatalf("roadmap show: %v\n%s", err, out)
	}
	requireOneRoadmapRequest(t, seen, http.MethodGet, "/api/v1/systems/1/roadmap")
	heads := []string{"Release Cutover", "Release Autumn", "Release Renewal", "\nNo release"}
	at := make([]int, len(heads))
	for i, head := range heads {
		at[i] = strings.Index(out, head)
		if at[i] < 0 || (i > 0 && at[i] < at[i-1]) {
			t.Fatalf("output lacks %q in the served order of the Releases:\n%s", head, out)
		}
	}
	cutover, autumn, renewal := out[at[0]:at[1]], out[at[1]:at[2]], out[at[2]:at[3]]
	initiatives := roadmapBlocks(out, []string{"Invoice runs", "Gift cards", "Ledger rewrite"})
	// Guards: new work has no decisions, and a Delivery Release no counts.
	if strings.Contains(initiatives["Gift cards"], "Decisions:") || strings.Contains(initiatives["Gift cards"], "drafted") {
		t.Errorf("the new-work Initiative Gift cards prints decisions or a drafted count:\n%s", initiatives["Gift cards"])
	}
	if strings.Contains(autumn, "decided") || strings.Contains(autumn, "drafted") || strings.Contains(autumn, "built") {
		t.Errorf("the Delivery Release Autumn prints a decided, drafted or built count:\n%s", autumn)
	}

	// `, <dr> drafted, <b> built` ends B3's context line and B3's Decisions: line.
	for _, want := range []struct{ under, block, line string }{
		{"the Release Cutover", cutover, "Billing (BILL): 1 of 4 undecided, 3 decided, 2 drafted, 1 built"},
		{"the Release Cutover", cutover, "Integrations (INTG): 3 of 3 undecided, 0 decided, 0 drafted, 0 built"},
		{"the Release Renewal", renewal, "Payments (PAY): 6 of 13 undecided, 7 decided, 3 drafted, 2 built"},
		{"the Initiative Invoice runs", initiatives["Invoice runs"], "Decisions: 2 keep, 1 change, 1 drop, 2 drafted, 1 built"},
		{"the Initiative Ledger rewrite", initiatives["Ledger rewrite"], "Decisions: 0 keep, 2 change, 5 drop, 2 drafted, 0 built"},
	} {
		line := regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(want.line) + `$`)
		if !line.MatchString(out) {
			t.Errorf("roadmap show does not print the line %q", want.line)
		} else if !line.MatchString(want.block) {
			t.Errorf("roadmap show prints the line %q, but not under %s", want.line, want.under)
		}
	}
	if t.Failed() {
		t.Logf("roadmap show printed:\n%s", out)
	}
}

// The selection read with each row's counterpart (contract §7): onto a new
// system an entry names the new requirement and the Epic whose saved plan holds
// the trace (no Epic once only the written trace remains); in place it names
// the Epic and no requirement; a row nothing drafts has none.
const c1SelectionCounterpartRows1 = `[
 {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-7","title":"Export invoices",
  "decision":{"outcome":"keep","reason":null,"initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"}},
  "counterpart":[{"requirement":{"kind":"user_requirement","id":"f1f1f1f1-1111-4111-8111-111111111111","display_ref":"UR-30","title":"Export a month of invoices"},
    "epic":{"id":51,"title":"Invoice export"},"state":"drafted"}]},
 {"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"SR-12","title":"Write the invoice file",
  "decision":{"outcome":"change","reason":null,"initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"}},
  "counterpart":[{"requirement":{"kind":"system_requirement","id":"f2f2f2f2-2222-4222-8222-222222222222","display_ref":"SR-31","title":"Write the invoice file as CSV"},
    "epic":null,"state":"built"},
   {"requirement":{"kind":"system_requirement","id":"f3f3f3f3-3333-4333-8333-333333333333","display_ref":"SR-32","title":"Sign the invoice file"},
    "epic":{"id":52,"title":"Signed exports"},"state":"drafted"}]}]`

const c1SelectionCounterpartRows2 = `[
 {"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","display_ref":"UR-9","title":"Send reminders",
  "decision":null,"counterpart":[]}]`

const c1SelectionInPlaceRows = `[
 {"requirement_kind":"user_requirement","requirement_id":"a7a7a7a7-7777-4777-8777-777777777777","display_ref":"UR-13","title":"Monthly statement",
  "decision":{"outcome":"change","reason":null,"initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite"}},
  "counterpart":[{"requirement":null,"epic":{"id":53,"title":"Ledger rewrite in place"},"state":"drafted"}]}]`

const c1SelectionContextHead = `"release":{"id":21,"name":"Cutover"},
"legacy_system":{"id":41,"name":"Legacy billing"},
"context":{"code":"INTG","name":"Integrations"},
"initiative":null,
"counts":{"in_scope":3,"undecided":1,"decided":2,"keep":1,"change":1,"drop":0,"drafted":2,"built":1}`

const c1SelectionInPlaceHead = `"release":{"id":23,"name":"Renewal"},
"legacy_system":{"id":1,"name":"T"},
"context":null,
"initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite","context":"PAY"},
"counts":{"in_scope":1,"undecided":0,"decided":1,"keep":0,"change":1,"drop":0,"drafted":1,"built":0}`

// c1RequireSelectionDocument checks that text is one JSON document holding
// the read's head as served, counts with drafted and built included, and every
// page's rows unchanged, each with its counterpart as served.
func c1RequireSelectionDocument(t *testing.T, what, text, head string, pages ...string) {
	t.Helper()
	var doc, want map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Errorf("%s is not one JSON document: %v\n%s", what, err, text)
		return
	}
	if err := json.Unmarshal([]byte(`{`+head+`}`), &want); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"release", "legacy_system", "context", "initiative", "counts"} {
		if got, present := doc[key]; !present || !reflect.DeepEqual(got, want[key]) {
			t.Errorf("%s %s = %#v (present %v); want %#v as served", what, key, got, present, want[key])
		}
	}
	var rows []any
	for _, page := range pages {
		var served []any
		if err := json.Unmarshal([]byte(page), &served); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, served...)
	}
	got, _ := doc["requirements"].([]any)
	if len(got) != len(rows) {
		t.Errorf("%s has %d requirements; want %d, every page's rows", what, len(got), len(rows))
		return
	}
	for i, row := range rows {
		wantRow, _ := row.(map[string]any)
		gotRow, _ := got[i].(map[string]any)
		if counterpart, present := gotRow["counterpart"]; !present || !reflect.DeepEqual(counterpart, wantRow["counterpart"]) {
			t.Errorf("%s row %d (%v) counterpart = %#v (present %v); want %#v as served", what, i+1, wantRow["display_ref"], counterpart, present, wantRow["counterpart"])
		}
		if !reflect.DeepEqual(gotRow, wantRow) {
			t.Errorf("%s row %d = %#v; want the served row unchanged %#v", what, i+1, gotRow, wantRow)
		}
	}
}

func TestC1_84b06f8f_RoadmapSelectionWritesEachRowsCounterpart(t *testing.T) {
	// Guard: the command writes the read's rows and counts as served (B3), so
	// each row's counterpart and the counts' drafted and built reach the file.
	t.Run("a context onto a new system, --out", func(t *testing.T) {
		server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, map[string]string{
			"":                `{"data":{` + c1SelectionContextHead + `,"requirements":` + c1SelectionCounterpartRows1 + `,"next_cursor":"` + b3SelectionCursor + `"}}`,
			b3SelectionCursor: `{"data":{` + c1SelectionContextHead + `,"requirements":` + c1SelectionCounterpartRows2 + `,"next_cursor":null}}`,
		})
		cobraWorkspace(t, server, 1)
		file := filepath.Join(t.TempDir(), "intg-selection.json")

		out, err := runRoot(t, "roadmap", "selection", "--context", "INTG", "--out", file)
		if err != nil {
			t.Fatalf("roadmap selection --context INTG --out: %v\n%s", err, out)
		}
		if n := len(b3SelectionRequests(seen)); n != 2 {
			t.Errorf("the selection route received %d requests; want 2, one per page (all requests %#v)", n, *seen)
		}
		written, readErr := os.ReadFile(file)
		if readErr != nil {
			t.Fatalf("--out %s wrote no file: %v\n%s", file, readErr, out)
		}
		c1RequireSelectionDocument(t, "the --out file", string(written), c1SelectionContextHead, c1SelectionCounterpartRows1, c1SelectionCounterpartRows2)
	})
	t.Run("an Initiative in place, --json", func(t *testing.T) {
		server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, map[string]string{
			"": `{"data":{` + c1SelectionInPlaceHead + `,"requirements":` + c1SelectionInPlaceRows + `,"next_cursor":null}}`,
		})
		cobraWorkspace(t, server, 1)

		out, err := runRootStdout(t, "roadmap", "selection", "--initiative", "44444444-4444-4444-8444-444444444444", "--json")
		if err != nil {
			t.Fatalf("roadmap selection --initiative --json: %v\n%s", err, out)
		}
		selection := b3SelectionRequests(seen)
		if len(selection) != 1 || selection[0].query.Get("line_id") != "44444444-4444-4444-8444-444444444444" {
			t.Errorf("the selection route received %#v; want one request with the Initiative's line_id", selection)
		}
		c1RequireSelectionDocument(t, "the --json output", out, c1SelectionInPlaceHead, c1SelectionInPlaceRows)
	})
}
