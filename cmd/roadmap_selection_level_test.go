package cmd

// Epic 333 (B4), test case f3cd9a0b (TC-B4-6), criterion d1349b22:
//   - the JSON document `modernpath roadmap selection` writes gives each legacy
//     requirement expected_level, expected_level_basis (changed, person,
//     estimated), expected_level_files and at_risk (the change rows behind At
//     risk, each with requirement_kind, requirement_id and display_ref; empty
//     when not at risk) exactly as the selection route serves them, and the
//     command prints no words for them;
//   - the command's help and the CLI guide's `roadmap selection` section give
//     the words for each basis code ("changed requirement", "set by {person}",
//     "estimated from its files") and say that in place at_risk lists the
//     change rows behind At risk.
//
// The document and the printed words hold on B3's command, which writes the
// route's rows unchanged (guards). B3's help and guide give no words for the
// basis codes and say nothing of at_risk, so those assertions fail on the
// first slice. The tests reuse B3's test server and add no production symbol.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The in-place Release Renewal (id 23) of b3SelectionRoadmapOne holds PAY.
const b4f3SelectionHead = `"release":{"id":23,"name":"Renewal"},
"legacy_system":{"id":1,"name":"T"},
"context":{"code":"PAY","name":"Payments"},
"initiative":null,
"counts":{"in_scope":3,"undecided":1,"decided":2,"keep":1,"change":1,"drop":0}`

const (
	b4f3KeptID     = "a7a7a7a7-7777-4777-8777-777777777777"
	b4f3ChangedID  = "b8b8b8b8-8888-4888-8888-888888888888"
	b4f3UndecideID = "c9c9c9c9-9999-4999-8999-999999999999"
)

// Page 1: a kept row with a person's level, at risk in place behind the
// change row SR-31 of the same Initiative; the change row SR-31.
const b4f3SelectionRowsPage1 = `[
 {"requirement_kind":"user_requirement","requirement_id":"` + b4f3KeptID + `","display_ref":"UR-40","title":"Refund a payment",
  "description":"A clerk refunds a card payment.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"passing","criteria":{"total":2,"passing":2,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"keep","reason":null,"expected_level":"compared","expected_level_reason":"The card network reads the refund file.",
   "initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-10-22T08:30:00Z","agent":null},
  "expected_level":"compared","expected_level_basis":"person","expected_level_files":null,
  "at_risk":[{"requirement_kind":"system_requirement","requirement_id":"` + b4f3ChangedID + `","display_ref":"SR-31"}]},
 {"requirement_kind":"system_requirement","requirement_id":"` + b4f3ChangedID + `","display_ref":"SR-31","title":"Post refunds to the ledger",
  "description":"The service posts each refund as a ledger entry.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"failing","criteria":{"total":3,"passing":2,"failing":1,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"change","reason":"The ledger moves to double entry.","expected_level":null,"expected_level_reason":null,
   "initiative":{"id":"44444444-4444-4444-8444-444444444444","title":"Ledger rewrite"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-10-22T08:31:00Z","agent":"claude-code"},
  "expected_level":"tests_only","expected_level_basis":"changed","expected_level_files":null,
  "at_risk":[]}]`

// Page 2: an undecided row with the level estimated from its files.
const b4f3SelectionRowsPage2 = `[
 {"requirement_kind":"user_requirement","requirement_id":"` + b4f3UndecideID + `","display_ref":"UR-42","title":"Show the payment history",
  "description":"A customer sees the payments of the last year.","context":"PAY","work_status":"DONE",
  "legacy_proof":{"state":"missing","criteria":{"total":0,"passing":0,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":null,
  "expected_level":"screens_compared","expected_level_basis":"estimated","expected_level_files":{"roles":["component","view"],"count":2},
  "at_risk":[]}]`

func b4f3SelectionPages() map[string]string {
	return map[string]string{
		"":                `{"data":{` + b4f3SelectionHead + `,"requirements":` + b4f3SelectionRowsPage1 + `,"next_cursor":"` + b3SelectionCursor + `"}}`,
		b3SelectionCursor: `{"data":{` + b4f3SelectionHead + `,"requirements":` + b4f3SelectionRowsPage2 + `,"next_cursor":null}}`,
	}
}

// b4f3BasisWords are the words for each basis code that the help and the
// guide give, and that the command never prints.
var b4f3BasisWords = []string{"changed requirement", "set by {person}", "estimated from its files"}

// b4f3RequireLevelFields checks that text is one JSON document whose
// requirements carry expected_level, expected_level_basis,
// expected_level_files and at_risk exactly as served, and whose rows are the
// served rows unchanged.
func b4f3RequireLevelFields(t *testing.T, what, text string) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Errorf("%s is not one JSON document: %v\n%s", what, err, text)
		return
	}
	var first, second []any
	if err := json.Unmarshal([]byte(b4f3SelectionRowsPage1), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b4f3SelectionRowsPage2), &second); err != nil {
		t.Fatal(err)
	}
	served := append(first, second...)

	written := map[string]map[string]any{}
	rows, _ := doc["requirements"].([]any)
	for _, r := range rows {
		if row, ok := r.(map[string]any); ok {
			if id, ok := row["requirement_id"].(string); ok {
				written[id] = row
			}
		}
	}
	for _, s := range served {
		want := s.(map[string]any)
		id, ref := want["requirement_id"].(string), want["display_ref"]
		got, ok := written[id]
		if !ok {
			t.Errorf("%s has no requirement %v (%s)", what, ref, id)
			continue
		}
		for _, field := range []string{"expected_level", "expected_level_basis", "expected_level_files", "at_risk"} {
			value, present := got[field]
			if !present || !reflect.DeepEqual(value, want[field]) {
				gotJSON, _ := json.Marshal(value)
				wantJSON, _ := json.Marshal(want[field])
				t.Errorf("%s %v %s = %s (present %v); want %s as served", what, ref, field, gotJSON, present, wantJSON)
			}
		}
	}
	if !reflect.DeepEqual(doc["requirements"], served) {
		gotJSON, _ := json.Marshal(doc["requirements"])
		wantJSON, _ := json.Marshal(served)
		t.Errorf("%s requirements = %s; want both pages' rows unchanged: %s", what, gotJSON, wantJSON)
	}
}

// b4f3RequireNoWords checks that a run printed no words for the basis codes,
// the expected level or At risk.
func b4f3RequireNoWords(t *testing.T, what, text string) {
	t.Helper()
	for _, words := range append([]string{"set by"}, b4f3BasisWords...) {
		if strings.Contains(text, words) {
			t.Errorf("%s prints %q; the command prints no words for the expected level or its basis", what, words)
		}
	}
	lower := strings.ToLower(text)
	for _, words := range []string{"at risk", "expected level"} {
		if strings.Contains(lower, words) {
			t.Errorf("%s prints %q; the command prints no words for the expected level or At risk", what, words)
		}
	}
}

// b4f3Paragraphs splits text on blank lines and gives each paragraph with its
// whitespace collapsed, so a wrapped line does not split a phrase.
func b4f3Paragraphs(text string) []string {
	var paragraphs []string
	for _, p := range regexp.MustCompile(`\n[ \t]*\n`).Split(text, -1) {
		if p = strings.Join(strings.Fields(p), " "); p != "" {
			paragraphs = append(paragraphs, p)
		}
	}
	return paragraphs
}

// b4f3RequireBasisWords checks that text gives the words for each basis code
// and says, in one paragraph, that in place at_risk lists the change rows
// behind At risk.
func b4f3RequireBasisWords(t *testing.T, what, text string) {
	t.Helper()
	paragraphs := b4f3Paragraphs(text)
	flat := strings.Join(paragraphs, " ")
	if !regexp.MustCompile(`\bexpected_level_basis\b`).MatchString(flat) {
		t.Errorf("%s does not name expected_level_basis, the field whose codes it gives words for", what)
	}
	for _, words := range []struct{ code, words string }{
		{"changed", "changed requirement"},
		{"person", "set by {person}"},
		{"estimated", "estimated from its files"},
	} {
		if !strings.Contains(flat, words.words) {
			t.Errorf("%s does not give the words for the basis %s: no %q", what, words.code, words.words)
		}
	}
	atRisk := regexp.MustCompile(`\bat_risk\b`)
	named, inPlace := false, false
	for _, p := range paragraphs {
		if atRisk.MatchString(p) {
			named = true
			inPlace = inPlace || strings.Contains(p, "in place")
		}
	}
	switch {
	case !named:
		t.Errorf("%s does not name at_risk, the change rows behind At risk", what)
	case !inPlace:
		t.Errorf("%s names at_risk but no paragraph that names it says \"in place\"", what)
	}
}

// b4f3GuideSelectionSection cuts out of the guide the sections whose heading
// names a selection and whose text documents `roadmap selection`, each up to
// the next heading of its level or above; headings inside code fences do not
// count.
func b4f3GuideSelectionSection(guide string) (string, []string) {
	type heading struct {
		line, level int
		text        string
	}
	lines := strings.Split(guide, "\n")
	var headings []heading
	fenced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced {
			continue
		}
		if m := regexp.MustCompile(`^(#{1,6})\s+(.*)$`).FindStringSubmatch(line); m != nil {
			headings = append(headings, heading{line: i, level: len(m[1]), text: m[2]})
		}
	}
	var sections, names []string
	for i, h := range headings {
		if h.level < 2 || h.level > 3 || !strings.Contains(strings.ToLower(h.text), "selection") {
			continue
		}
		end := len(lines)
		for _, next := range headings[i+1:] {
			if next.level <= h.level {
				end = next.line
				break
			}
		}
		section := strings.Join(lines[h.line:end], "\n")
		if strings.Contains(section, "roadmap selection") {
			sections = append(sections, section)
			names = append(names, h.text)
		}
	}
	return strings.Join(sections, "\n\n"), names
}

func TestB4_f3cd9a0b_SelectionDocumentCarriesTheLevelAndAtRiskAsServed(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, b4f3SelectionPages())
	cobraWorkspace(t, server, 1)
	file := filepath.Join(t.TempDir(), "pay-selection.json")

	// Guard: --out writes the document with the four fields as served, and
	// prints the count, no words for them.
	out, err := runRoot(t, "roadmap", "selection", "--context", "PAY", "--release", "23", "--out", file)
	if err != nil {
		t.Errorf("roadmap selection --context PAY --release 23 --out: %v\n%s", err, out)
	}
	selection := b3SelectionRequests(seen)
	if len(selection) != 2 {
		t.Errorf("the selection route received %d requests; want 2, one per page (all requests %#v)\n%s",
			len(selection), *seen, b3Text(out, err))
	}
	for i, r := range selection {
		if r.method != http.MethodGet || r.query.Get("context") != "PAY" || r.query.Get("release_id") != "23" {
			t.Errorf("selection request %d is %s %v; want GET with context PAY and release_id 23", i+1, r.method, r.query)
		}
	}
	written, readErr := os.ReadFile(file)
	if readErr != nil {
		t.Errorf("--out %s wrote no file: %v\n%s", file, readErr, b3Text(out, err))
	} else {
		b4f3RequireLevelFields(t, "the --out file", string(written))
	}
	if want := "Wrote 3 legacy requirements to " + file; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, b3Text(out, err))
	}
	b4f3RequireNoWords(t, "roadmap selection --out", b3Text(out, err))

	// Guard: --json prints the same document on stdout, and nothing on
	// stdout or stderr gives words for the four fields.
	stdout, err := runRootStdout(t, "roadmap", "selection", "--context", "PAY", "--release", "23", "--json")
	if err != nil {
		t.Errorf("roadmap selection --context PAY --release 23 --json: %v\n%s", err, stdout)
	}
	b4f3RequireLevelFields(t, "the --json output", stdout)
	both, err := runRoot(t, "roadmap", "selection", "--context", "PAY", "--release", "23", "--json")
	if err != nil {
		t.Errorf("roadmap selection --context PAY --release 23 --json: %v\n%s", err, both)
	}
	b4f3RequireNoWords(t, "roadmap selection --json", b3Text(both, err))
}

func TestB4_f3cd9a0b_SelectionHelpGivesTheWordsForEachBasis(t *testing.T) {
	out, err := runRoot(t, "roadmap", "selection", "--help")
	if err != nil {
		t.Fatalf("roadmap selection --help: %v\n%s", err, out)
	}
	b4f3RequireBasisWords(t, "the help of roadmap selection", out)
	if t.Failed() {
		t.Logf("roadmap selection --help printed:\n%s", out)
	}
}

func TestB4_f3cd9a0b_CLIGuideSelectionSectionGivesTheWordsForEachBasis(t *testing.T) {
	guide, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	section, names := b4f3GuideSelectionSection(string(guide))
	if section == "" {
		t.Fatalf("the kit's CLI guide has no section whose heading names a selection and that documents roadmap selection")
	}
	b4f3RequireBasisWords(t, "the CLI guide's roadmap selection section ("+strings.Join(names, "; ")+")", section)
}
