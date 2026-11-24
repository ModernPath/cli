package cmd

// Epic 341 (G3), test case d2de1c0b (TC-G3-18), criterion 26c86e22: the JSON
// document `modernpath roadmap selection --out` writes carries each legacy
// requirement's what_it_has, expected_level and expected_level_basis as the
// selection route serves them: a requirement recorded as having a screen,
// screens_compared, basis recorded; one with nothing recorded, null and the
// basis estimated. B3's command writes the route's rows unchanged, so this
// holds on the first slice and guards that the document carries what the
// route gives; the route test in roadmap_selection_what_it_has_api_test.exs
// is the RED of the criterion.
//
// Test case 3981fe20 (TC-G3-19): the help of roadmap selection gives the code
// recorded the words "recorded by reverse engineering". B4 already wrote those
// words into the help as a planned change, so this holds on the first slice
// too (guard).
//
// The tests reuse B3's test server and add no production symbol.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	g3SelectionRecordedID = "e1e1e1e1-1111-4111-8111-111111111111"
	g3SelectionEstimateID = "e2e2e2e2-2222-4222-8222-222222222222"
)

// The in-place Release Renewal (id 23) of b3SelectionRoadmapOne holds PAY.
const g3SelectionHead = `"release":{"id":23,"name":"Renewal"},
"legacy_system":{"id":1,"name":"T"},
"context":{"code":"PAY","name":"Payments"},
"initiative":null,
"counts":{"in_scope":2,"undecided":1,"decided":1,"keep":1,"change":0,"drop":0}`

// Page 1: a kept row recorded as having a screen. Page 2: an undecided row
// with nothing recorded, its level estimated from its files.
const g3SelectionRowsPage1 = `[
 {"requirement_kind":"user_requirement","requirement_id":"` + g3SelectionRecordedID + `","display_ref":"UR-50","title":"See the receipt",
  "description":"A customer sees the receipt of a payment.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"passing","criteria":{"total":1,"passing":1,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"keep","reason":null,"expected_level":null,"expected_level_reason":null,
   "initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-10-27T08:30:00Z","agent":null},
  "what_it_has":["screen"],
  "expected_level":"screens_compared","expected_level_basis":"recorded","expected_level_files":null,
  "at_risk":[]}]`

const g3SelectionRowsPage2 = `[
 {"requirement_kind":"system_requirement","requirement_id":"` + g3SelectionEstimateID + `","display_ref":"SR-51","title":"Post refunds to the ledger",
  "description":"The service posts each refund as a ledger entry.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"missing","criteria":{"total":0,"passing":0,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":null,
  "what_it_has":null,
  "expected_level":"compared","expected_level_basis":"estimated","expected_level_files":{"roles":["controller"],"count":1},
  "at_risk":[]}]`

func g3SelectionPages() map[string]string {
	return map[string]string{
		"":                `{"data":{` + g3SelectionHead + `,"requirements":` + g3SelectionRowsPage1 + `,"next_cursor":"` + b3SelectionCursor + `"}}`,
		b3SelectionCursor: `{"data":{` + g3SelectionHead + `,"requirements":` + g3SelectionRowsPage2 + `,"next_cursor":null}}`,
	}
}

func TestG3_d2de1c0b_SelectionDocumentCarriesWhatItHasAndTheBasisRecorded(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, g3SelectionPages())
	cobraWorkspace(t, server, 1)
	file := filepath.Join(t.TempDir(), "pay-selection.json")

	out, err := runRoot(t, "roadmap", "selection", "--context", "PAY", "--release", "23", "--out", file)
	if err != nil {
		t.Fatalf("roadmap selection --context PAY --release 23 --out: %v\n%s", err, out)
	}
	if got := len(b3SelectionRequests(seen)); got != 2 {
		t.Errorf("the selection route received %d requests; want 2, one per page", got)
	}
	written, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("--out %s wrote no file: %v\n%s", file, err, out)
	}
	var doc map[string]any
	if err := json.Unmarshal(written, &doc); err != nil {
		t.Fatalf("the --out file is not one JSON document: %v\n%s", err, written)
	}
	var first, second []any
	if err := json.Unmarshal([]byte(g3SelectionRowsPage1), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(g3SelectionRowsPage2), &second); err != nil {
		t.Fatal(err)
	}
	served := append(first, second...)
	byID := map[string]map[string]any{}
	rows, _ := doc["requirements"].([]any)
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			if id, ok := row["requirement_id"].(string); ok {
				byID[id] = row
			}
		}
	}
	for _, s := range served {
		want := s.(map[string]any)
		id, ref := want["requirement_id"].(string), want["display_ref"]
		got, ok := byID[id]
		if !ok {
			t.Errorf("the --out file has no requirement %v (%s):\n%s", ref, id, written)
			continue
		}
		for _, field := range []string{"what_it_has", "expected_level", "expected_level_basis", "expected_level_files"} {
			value, present := got[field]
			if !present || !reflect.DeepEqual(value, want[field]) {
				gotJSON, _ := json.Marshal(value)
				wantJSON, _ := json.Marshal(want[field])
				t.Errorf("the --out file %v %s = %s (present %v); want %s as served", ref, field, gotJSON, present, wantJSON)
			}
		}
	}
	if strings.Contains(out, "recorded by reverse engineering") {
		t.Errorf("roadmap selection --out prints words for the basis; it prints none:\n%s", out)
	}
}

func TestG3_3981fe20_SelectionHelpGivesTheWordsForRecorded(t *testing.T) {
	out, err := runRoot(t, "roadmap", "selection", "--help")
	if err != nil {
		t.Fatalf("roadmap selection --help: %v\n%s", err, out)
	}
	if phrase := "recorded by reverse engineering"; !strings.Contains(g3Plain(out), phrase) {
		t.Errorf("the help of roadmap selection does not give the code recorded the words %q:\n%s", phrase, out)
	}
	// Like B4's codes ("estimated is \"estimated from its files\""), and no
	// longer as a planned change (B4 wrote "a planned change adds recorded").
	if phrase := `recorded is "recorded by reverse engineering"`; !strings.Contains(g3Plain(out), phrase) {
		t.Errorf("the help of roadmap selection does not give the code recorded its words as it gives B4's codes, %q:\n%s", phrase, out)
	}
	if stale := "a planned change"; strings.Contains(g3Plain(out), stale) {
		t.Errorf("the help of roadmap selection still calls the code recorded %q:\n%s", stale, out)
	}
}
