package cmd

// Epic 336 (D2), test case 5f685ede (TC-D2-26), criterion 37ec31f3 (B4's
// SR-4444-1 at 4): the JSON document `modernpath roadmap selection` writes
// carries a changed row raised to compared with expected_level compared and
// expected_level_basis raised exactly as the selection route serves them, and
// the command prints no words for them (B3's command writes the route's rows
// unchanged, a guard); the command's help and the CLI guide's roadmap
// selection section give the basis code raised its words, "raised by
// {person}" with the reason, beside the other codes. On B4's texts neither
// names raised, so those assertions fail. The test uses no new production
// symbol.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const d2RaisedID = "d3d3d3d3-3333-4333-8333-333333333333"

// The in-place Release Renewal (id 23) of b3SelectionRoadmapOne holds PAY.
const d2RaisedSelectionHead = `"release":{"id":23,"name":"Renewal"},
"legacy_system":{"id":1,"name":"T"},
"context":{"code":"PAY","name":"Payments"},
"initiative":null,
"counts":{"in_scope":1,"undecided":0,"decided":1,"keep":0,"change":1,"drop":0}`

// One page: a changed row raised to compared by Pia Planner, with her reason.
const d2RaisedSelectionRows = `[
 {"requirement_kind":"system_requirement","requirement_id":"` + d2RaisedID + `","display_ref":"SR-31","title":"Post refunds to the ledger",
  "description":"The service posts each refund as a ledger entry.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"passing","criteria":{"total":3,"passing":3,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"change","reason":"The ledger moves to double entry.","expected_level":"compared","expected_level_reason":"Only the posting format changes.",
   "initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-11-03T08:31:00Z","agent":null},
  "what_it_has":null,
  "expected_level":"compared","expected_level_basis":"raised","expected_level_files":null,
  "at_risk":[]}]`

func d2RaisedSelectionPages() map[string]string {
	return map[string]string{
		"": `{"data":{` + d2RaisedSelectionHead + `,"requirements":` + d2RaisedSelectionRows + `,"next_cursor":null}}`,
	}
}

const d2RaisedWords = "raised by {person}"

func TestD2_5f685ede_SelectionDocumentCarriesTheBasisRaisedUnchanged(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, d2RaisedSelectionPages())
	cobraWorkspace(t, server, 1)
	file := filepath.Join(t.TempDir(), "pay-selection.json")

	out, err := runRoot(t, "roadmap", "selection", "--context", "PAY", "--release", "23", "--out", file)
	if err != nil {
		t.Fatalf("roadmap selection --context PAY --release 23 --out: %v\n%s", err, out)
	}
	if got := len(b3SelectionRequests(seen)); got != 1 {
		t.Errorf("the selection route received %d requests; want 1", got)
	}
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("--out %s wrote no file: %v\n%s", file, err, out)
	}
	var doc map[string]any
	if err := json.Unmarshal(written, &doc); err != nil {
		t.Fatalf("the --out file is not one JSON document: %v\n%s", err, written)
	}
	var served []any
	if err := json.Unmarshal([]byte(d2RaisedSelectionRows), &served); err != nil {
		t.Fatal(err)
	}
	rows, _ := doc["requirements"].([]any)
	if len(rows) != 1 {
		t.Fatalf("the --out file has %d requirements; want 1:\n%s", len(rows), written)
	}
	got, want := rows[0].(map[string]any), served[0].(map[string]any)
	for _, field := range []string{"expected_level", "expected_level_basis", "expected_level_files", "decision"} {
		value, present := got[field]
		if !present || !reflect.DeepEqual(value, want[field]) {
			gotJSON, _ := json.Marshal(value)
			wantJSON, _ := json.Marshal(want[field])
			t.Errorf("the --out file's %s = %s (present %v); want %s as served", field, gotJSON, present, wantJSON)
		}
	}
	if strings.Contains(out, d2RaisedWords) || strings.Contains(out, "raised by Pia Planner") {
		t.Errorf("roadmap selection --out prints words for the basis; it prints none:\n%s", out)
	}
}

func TestD2_5f685ede_SelectionHelpGivesTheWordsForRaised(t *testing.T) {
	out, err := runRoot(t, "roadmap", "selection", "--help")
	if err != nil {
		t.Fatalf("roadmap selection --help: %v\n%s", err, out)
	}
	plain := g3Plain(out)
	// Like B4's codes ("estimated is \"estimated from its files\"").
	if phrase := `raised is "` + d2RaisedWords + `"`; !strings.Contains(plain, phrase) {
		t.Errorf("the help of roadmap selection does not give the code raised its words as it gives B4's codes, %q:\n%s", phrase, out)
	}
	if !regexp.MustCompile(`raised is "raised by \{person\}"[^.]*reason`).MatchString(plain) {
		t.Errorf("the help of roadmap selection does not say that raised comes with the reason:\n%s", out)
	}
	// Guard (B4): the other codes keep their words.
	b4f3RequireBasisWords(t, "the help of roadmap selection", out)
}

func TestD2_5f685ede_CLIGuideSelectionSectionGivesTheWordsForRaised(t *testing.T) {
	guide, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	section, names := b4f3GuideSelectionSection(string(guide))
	if section == "" {
		t.Fatalf("the kit's CLI guide has no section whose heading names a selection and that documents roadmap selection")
	}
	where := "the CLI guide's roadmap selection section (" + strings.Join(names, "; ") + ")"
	if !strings.Contains(section, "`raised`") {
		t.Errorf("%s does not name the basis code raised", where)
	}
	if !strings.Contains(section, d2RaisedWords) {
		t.Errorf("%s does not give raised the words %q", where, d2RaisedWords)
	}
	if !regexp.MustCompile("`raised`[^\n]*" + regexp.QuoteMeta(d2RaisedWords) + "[^\n]*reason").MatchString(section) {
		t.Errorf("%s does not say that raised comes with the reason", where)
	}
	// Guard (B4): the other codes keep their words.
	b4f3RequireBasisWords(t, where, section)
}
