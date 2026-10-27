package cmd

// Epic 332 (B3), test case 74721edf (TC-B3-18), criteria bebface5 and
// 4a09fbce:
//   - `modernpath roadmap selection (--context <code> [--release <id>] |
//     --initiative <line-id>) (--out <file> | --json)` reads the selection
//     route of the bound System, follows next_cursor until null and writes one
//     JSON document holding every page's rows unchanged. Without --release it
//     takes the one open Modernization Release of the Roadmap read whose scope
//     holds the context. It refuses, writing nothing, without a bound System,
//     with both or neither of --context and --initiative, and with none or
//     several such Releases (naming each).
//   - `roadmap show` prints each replacing Initiative's keep, change and drop
//     counts and each context's decided count, and the legacy requirements new
//     since the agreement.
//   - `roadmap apply --help` documents set_selection and its row fields, and
//     `roadmap apply`, with or without --dry-run, prints each refused row of a
//     refused set_selection.
//
// The tests drive the root command and use no new production symbol, so they
// build on the first slice, where `roadmap selection` is unknown and the
// printers do not know the new fields; they fail on their assertions.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

const b3SelectionPath = "/api/v1/systems/1/roadmap/selection"

type b3Request struct {
	method, path string
	query        url.Values
}

// b3SelectionServer answers the reachability probe, the Roadmap read with
// roadmap, and the selection route with the page its cursor names ("" for the
// first page); it records every other request.
func b3SelectionServer(t *testing.T, roadmap string, pages map[string]string) (*httptest.Server, *[]b3Request) {
	t.Helper()
	seen := []b3Request{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		seen = append(seen, b3Request{method: r.Method, path: r.URL.Path, query: r.URL.Query()})
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/systems/1/roadmap":
			_, _ = w.Write([]byte(roadmap))
		case b3SelectionPath:
			page, ok := pages[r.URL.Query().Get("cursor")]
			if !ok {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"invalid_cursor"}`))
				return
			}
			_, _ = w.Write([]byte(page))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not_found"}`))
		}
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func b3SelectionRequests(seen *[]b3Request) []b3Request {
	var selection []b3Request
	for _, r := range *seen {
		if r.path == b3SelectionPath {
			selection = append(selection, r)
		}
	}
	return selection
}

// b3Text is what the person sees of a run: its output and the error it ends with.
func b3Text(out string, err error) string {
	if err == nil {
		return out
	}
	return out + "\n" + err.Error()
}

// The Roadmap read the CLI picks the Release from: Cutover (active) is the one
// open Modernization Release whose scope holds INTG; Renewal is one that does
// not; Autumn is a Delivery Release; Winter cutover holds INTG but is released.
const b3SelectionRoadmapOne = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INTG","name":"Integrations"},{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL","INTG"],"legacy_reference_revision":"a1b2c3d4",
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":4,"undecided":1,"decided":3},
   {"code":"INTG","name":"Integrations","in_scope":3,"undecided":1,"decided":2}]},
  "initiatives":[]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,"initiatives":[]},
 {"id":23,"name":"Renewal","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":1,"legacy_system_name":"T","in_place":true,
  "legacy_scope":["PAY"],"legacy_reference_revision":null,
  "modernization":{"contexts":[{"code":"PAY","name":"Payments","in_scope":6,"undecided":6,"decided":0}]},
  "initiatives":[]},
 {"id":19,"name":"Winter cutover","status":"released","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["INTG"],"legacy_reference_revision":"0f0e0d0c",
  "modernization":{"contexts":[{"code":"INTG","name":"Integrations","in_scope":3,"undecided":0,"decided":3}]},
  "initiatives":[{"id":"99999999-9999-4999-8999-999999999999","text":"Old provider sync","horizon":"now","context":"INTG","new_context":false,"work_kind":"replaces_legacy","release_id":19,"epic_ids":[]}]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

// Two open Modernization Releases whose scope holds INTG.
const b3SelectionRoadmapSeveral = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"INTG","name":"Integrations"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["INTG"],"legacy_reference_revision":null,
  "modernization":{"contexts":[{"code":"INTG","name":"Integrations","in_scope":3,"undecided":1,"decided":2}]},
  "initiatives":[]},
 {"id":24,"name":"Provider switch","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":42,"legacy_system_name":"Legacy gateway","in_place":false,
  "legacy_scope":["INTG"],"legacy_reference_revision":null,
  "modernization":{"contexts":[{"code":"INTG","name":"Integrations","in_scope":5,"undecided":5,"decided":0}]},
  "initiatives":[]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

// No open Modernization Release whose scope holds INTG.
const b3SelectionRoadmapNone = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INTG","name":"Integrations"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL"],"legacy_reference_revision":null,
  "modernization":{"contexts":[{"code":"BILL","name":"Billing","in_scope":4,"undecided":1,"decided":3}]},
  "initiatives":[]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,"initiatives":[]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

const b3SelectionCursor = "g3QAAAABZAAFYWZ0ZXJtAAAABlNSLTEy"

const b3SelectionRowsPage1 = `[
 {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-7","title":"Export invoices",
  "description":"A clerk exports the month's invoices as one file.","context":"INTG","work_status":"DONE",
  "legacy_proof":{"state":"passing","criteria":{"total":3,"passing":3,"failing":0,"missing":0}},
  "group":null,
  "dependents":{"count":1,"items":[{"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"SR-12","title":"Write the invoice file"}]},
  "flags":{"agreed":true,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"keep","reason":null,"expected_level":"compared","expected_level_reason":"The bank reads the file.",
   "initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-10-20T09:15:00Z","agent":"claude-code"}},
 {"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","display_ref":"SR-12","title":"Write the invoice file",
  "description":"The service writes the export as CSV.","context":"INTG","work_status":"DONE",
  "legacy_proof":{"state":"failing","criteria":{"total":2,"passing":1,"failing":1,"missing":0}},
  "group":{"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","display_ref":"UR-7","title":"Export invoices"},
  "dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":true,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":false},
  "decision":{"outcome":"drop","reason":"The bank takes the API now.","expected_level":null,"expected_level_reason":null,
   "initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"},
   "set_by":{"id":5,"name":"Pia Planner"},"set_at":"2026-10-21T10:00:00Z","agent":null}}]`

const b3SelectionRowsPage2 = `[
 {"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","display_ref":"UR-9","title":"Send reminders",
  "description":"The system reminds a customer of an unpaid invoice.","context":"INTG","work_status":"DONE",
  "legacy_proof":{"state":"missing","criteria":{"total":0,"passing":0,"failing":0,"missing":0}},
  "group":null,"dependents":{"count":0,"items":[]},
  "flags":{"agreed":false,"revised_since_selected":false,"context_changed_since_decided":false,"not_counted":false,"new_since_agreement":true},
  "decision":null}]`

const b3SelectionContextHead = `"release":{"id":21,"name":"Cutover"},
"legacy_system":{"id":41,"name":"Legacy billing"},
"context":{"code":"INTG","name":"Integrations"},
"initiative":null,
"counts":{"in_scope":3,"undecided":1,"decided":2,"keep":1,"change":0,"drop":1}`

const b3SelectionLineID = "c3c3c3c3-3333-4333-8333-333333333333"

const b3SelectionLineHead = `"release":{"id":21,"name":"Cutover"},
"legacy_system":{"id":41,"name":"Legacy billing"},
"context":null,
"initiative":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections","context":"INTG"},
"counts":{"in_scope":3,"undecided":1,"decided":2,"keep":1,"change":0,"drop":1}`

// b3SelectionPages is a read served in two pages: the first names the cursor
// of the second, the second names none.
func b3SelectionPages(head string) map[string]string {
	return map[string]string{
		"":                `{"data":{` + head + `,"requirements":` + b3SelectionRowsPage1 + `,"next_cursor":"` + b3SelectionCursor + `"}}`,
		b3SelectionCursor: `{"data":{` + head + `,"requirements":` + b3SelectionRowsPage2 + `,"next_cursor":null}}`,
	}
}

// b3RequireSelectionDocument checks that text is one JSON document holding
// the read's head as served and both pages' rows, unchanged and in order.
func b3RequireSelectionDocument(t *testing.T, what, text, head string) {
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
		got, present := doc[key]
		if !present || !reflect.DeepEqual(got, want[key]) {
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want[key])
			t.Errorf("%s %s = %s (present %v); want %s as served", what, key, gotJSON, present, wantJSON)
		}
	}
	var first, second []any
	if err := json.Unmarshal([]byte(b3SelectionRowsPage1), &first); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b3SelectionRowsPage2), &second); err != nil {
		t.Fatal(err)
	}
	rows := append(first, second...)
	if got := doc["requirements"]; !reflect.DeepEqual(got, rows) {
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(rows)
		t.Errorf("%s requirements = %s; want both pages' rows unchanged: %s", what, gotJSON, wantJSON)
	}
}

func TestB3_74721edf_SelectionWritesEveryPageOfAContextToOneFile(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapOne, b3SelectionPages(b3SelectionContextHead))
	cobraWorkspace(t, server, 1)
	file := filepath.Join(t.TempDir(), "intg-selection.json")

	out, err := runRoot(t, "roadmap", "selection", "--context", "INTG", "--out", file)

	// Two pages of the context, each read in the Release picked from the Roadmap read.
	selection := b3SelectionRequests(seen)
	if len(selection) != 2 {
		t.Errorf("the selection route received %d requests; want 2, one per page (all requests %#v)\n%s",
			len(selection), *seen, b3Text(out, err))
	}
	for i, r := range selection {
		if r.method != http.MethodGet {
			t.Errorf("selection request %d is %s; want GET", i+1, r.method)
		}
		if got := r.query.Get("context"); got != "INTG" {
			t.Errorf("selection request %d context = %q; want INTG (query %v)", i+1, got, r.query)
		}
		if got := r.query.Get("release_id"); got != "21" {
			t.Errorf("selection request %d release_id = %q; want 21, the one open Modernization Release whose scope holds INTG (query %v)", i+1, got, r.query)
		}
		if r.query.Has("line_id") {
			t.Errorf("selection request %d of a context read sends line_id (query %v)", i+1, r.query)
		}
	}
	if len(selection) == 2 {
		if got := selection[0].query.Get("cursor"); got != "" {
			t.Errorf("the first selection request sends cursor %q; want none", got)
		}
		if got := selection[1].query.Get("cursor"); got != b3SelectionCursor {
			t.Errorf("the second selection request cursor = %q; want the first page's next_cursor %q", got, b3SelectionCursor)
		}
	}
	readRoadmap := false
	for _, r := range *seen {
		if r.method == http.MethodGet && r.path == "/api/v1/systems/1/roadmap" {
			readRoadmap = true
		}
	}
	if !readRoadmap {
		t.Errorf("without --release the command did not read the Roadmap to pick the Release (requests %#v)", *seen)
	}

	// One document with both pages' rows, unchanged.
	written, readErr := os.ReadFile(file)
	if readErr != nil {
		t.Errorf("--out %s wrote no file: %v\n%s", file, readErr, b3Text(out, err))
	} else {
		b3RequireSelectionDocument(t, "the --out file", string(written), b3SelectionContextHead)
	}
	if want := "Wrote 3 legacy requirements to " + file; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, b3Text(out, err))
	}
	if err != nil {
		t.Errorf("roadmap selection --context INTG --out: %v\n%s", err, out)
	}
}

func TestB3_74721edf_SelectionJSONPrintsTheDocumentOfTheGivenRelease(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapSeveral, b3SelectionPages(b3SelectionContextHead))
	cobraWorkspace(t, server, 1)

	out, err := runRootStdout(t, "roadmap", "selection", "--context", "INTG", "--release", "24", "--json")

	selection := b3SelectionRequests(seen)
	if len(selection) != 2 {
		t.Errorf("the selection route received %d requests; want 2, one per page (all requests %#v)\n%s",
			len(selection), *seen, b3Text(out, err))
	}
	for i, r := range selection {
		if got := r.query.Get("context"); got != "INTG" {
			t.Errorf("selection request %d context = %q; want INTG (query %v)", i+1, got, r.query)
		}
		if got := r.query.Get("release_id"); got != "24" {
			t.Errorf("selection request %d release_id = %q; want 24, the --release given (query %v)", i+1, got, r.query)
		}
	}
	b3RequireSelectionDocument(t, "the --json output", out, b3SelectionContextHead)
	if err != nil {
		t.Errorf("roadmap selection --context INTG --release 24 --json: %v\n%s", err, out)
	}
}

func TestB3_74721edf_SelectionOfAnInitiativeSendsItsLineID(t *testing.T) {
	server, seen := b3SelectionServer(t, b3SelectionRoadmapSeveral, b3SelectionPages(b3SelectionLineHead))
	cobraWorkspace(t, server, 1)

	out, err := runRootStdout(t, "roadmap", "selection", "--initiative", b3SelectionLineID, "--json")

	selection := b3SelectionRequests(seen)
	if len(selection) != 2 {
		t.Errorf("the selection route received %d requests; want 2, one per page (all requests %#v)\n%s",
			len(selection), *seen, b3Text(out, err))
	}
	for i, r := range selection {
		if got := r.query.Get("line_id"); got != b3SelectionLineID {
			t.Errorf("selection request %d line_id = %q; want %s (query %v)", i+1, got, b3SelectionLineID, r.query)
		}
		if r.query.Has("context") {
			t.Errorf("selection request %d of an Initiative sends context (query %v)", i+1, r.query)
		}
	}
	if len(selection) == 2 && selection[1].query.Get("cursor") != b3SelectionCursor {
		t.Errorf("the second selection request cursor = %q; want %q", selection[1].query.Get("cursor"), b3SelectionCursor)
	}
	b3RequireSelectionDocument(t, "the --json output", out, b3SelectionLineHead)
	if err != nil {
		t.Errorf("roadmap selection --initiative --json: %v\n%s", err, out)
	}
}

func TestB3_74721edf_SelectionRefusesAndWritesNothing(t *testing.T) {
	cases := []struct {
		name    string
		bound   int
		roadmap string
		args    []string
		// each group needs one of its words in the refusal
		want [][]string
	}{
		{"without a bound System", 0, b3SelectionRoadmapOne,
			[]string{"--context", "INTG"},
			[][]string{{"roadmap requires a bound System; run 'modernpath init' to bind one"}}},
		{"with both --context and --initiative", 1, b3SelectionRoadmapOne,
			[]string{"--context", "INTG", "--initiative", b3SelectionLineID},
			[][]string{{"--context"}, {"--initiative"}}},
		{"with neither --context nor --initiative", 1, b3SelectionRoadmapOne,
			nil,
			[][]string{{"--context"}, {"--initiative"}}},
		{"with no open Modernization Release holding the context", 1, b3SelectionRoadmapNone,
			[]string{"--context", "INTG"},
			[][]string{{"INTG", "Integrations"}}},
		{"with several open Modernization Releases holding the context", 1, b3SelectionRoadmapSeveral,
			[]string{"--context", "INTG"},
			[][]string{{"Cutover"}, {"Provider switch"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, seen := b3SelectionServer(t, c.roadmap, b3SelectionPages(b3SelectionContextHead))
			cobraWorkspace(t, server, c.bound)
			file := filepath.Join(t.TempDir(), "selection.json")
			args := append([]string{"roadmap", "selection"}, c.args...)
			args = append(args, "--out", file)

			out, err := runRoot(t, args...)
			text := b3Text(out, err)

			for _, group := range c.want {
				named := false
				for _, word := range group {
					named = named || strings.Contains(text, word)
				}
				if !named {
					t.Errorf("the refusal does not name %s", strings.Join(group, " or "))
				}
			}
			if err == nil {
				t.Errorf("roadmap selection %s was not refused:\n%s", strings.Join(c.args, " "), out)
			}
			if got := b3SelectionRequests(seen); len(got) != 0 {
				t.Errorf("the refused command read the selection route: %#v", got)
			}
			if _, statErr := os.Stat(file); statErr == nil {
				t.Errorf("the refused command wrote %s", file)
			}
			if t.Failed() {
				t.Logf("roadmap selection %s printed:\n%s", strings.Join(c.args, " "), text)
			}
		})
	}
}

// The Roadmap read with selections: Invoice runs replaces part of Legacy
// billing and has decisions; Gift cards is new work; Ledger rewrite replaces
// part of the bound System in place. Cutover has legacy requirements new since
// the agreement, Renewal none.
const b3RoadmapShowReply = `{"data":{"system_id":1,"plan_state":"agreed",
"contexts":[{"code":"BILL","name":"Billing"},{"code":"INTG","name":"Integrations"},{"code":"PAY","name":"Payments"}],
"releases":[
 {"id":21,"name":"Cutover","status":"active","planning_context":{},
  "release_type":"modernization","legacy_system_id":41,"legacy_system_name":"Legacy billing","in_place":false,
  "legacy_scope":["BILL","INTG"],"legacy_reference_revision":"a1b2c3d4","new_since_agreement_count":4,
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":4,"undecided":1,"decided":3},
   {"code":"INTG","name":"Integrations","in_scope":3,"undecided":3,"decided":0}]},
  "initiatives":[
   {"id":"11111111-1111-4111-8111-111111111111","text":"Invoice runs","horizon":"now","context":"BILL","context_name":"Billing","new_context":false,"work_kind":"replaces_legacy","release_id":21,"epic_ids":[51],
    "selection":{"keep":2,"change":1,"drop":1,"agreed":true}}]},
 {"id":22,"name":"Autumn","status":"planned","planning_context":{},
  "release_type":"delivery","legacy_system_id":null,"legacy_system_name":null,"in_place":false,
  "legacy_scope":null,"legacy_reference_revision":null,"modernization":null,"new_since_agreement_count":0,
  "initiatives":[
   {"id":"22222222-2222-4222-8222-222222222222","text":"Gift cards","horizon":"next","context":"PAY","context_name":"Payments","new_context":false,"work_kind":"new_work","release_id":22,"epic_ids":[],
    "selection":null}]},
 {"id":23,"name":"Renewal","status":"planned","planning_context":{},
  "release_type":"modernization","legacy_system_id":1,"legacy_system_name":"T","in_place":true,
  "legacy_scope":null,"legacy_reference_revision":null,"new_since_agreement_count":0,
  "modernization":{"contexts":[
   {"code":"BILL","name":"Billing","in_scope":5,"undecided":5,"decided":0},
   {"code":"PAY","name":"Payments","in_scope":13,"undecided":6,"decided":7}]},
  "initiatives":[
   {"id":"44444444-4444-4444-8444-444444444444","text":"Ledger rewrite","horizon":"next","context":"PAY","context_name":"Payments","new_context":false,"work_kind":"replaces_legacy","release_id":23,"epic_ids":[],
    "selection":{"keep":0,"change":2,"drop":5,"agreed":false}}]}],
"no_release":{"now":[],"next":[],"later":[]}}}`

func TestB3_74721edf_RoadmapShowPrintsDecisionsAndDecidedCounts(t *testing.T) {
	server, seen := roadmapServer(t, http.StatusOK, b3RoadmapShowReply)
	cobraWorkspace(t, server, 1)

	out, err := runRoot(t, "roadmap", "show")
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

	for _, want := range []struct{ under, block, phrase string }{
		{"the Initiative Invoice runs", initiatives["Invoice runs"], "Decisions: 2 keep, 1 change, 1 drop"},
		{"the Initiative Ledger rewrite", initiatives["Ledger rewrite"], "Decisions: 0 keep, 2 change, 5 drop"},
		{"the Release Cutover", cutover, "Billing (BILL): 1 of 4 undecided, 3 decided"},
		{"the Release Cutover", cutover, "Integrations (INTG): 3 of 3 undecided, 0 decided"},
		{"the Release Cutover", cutover, "New since the agreement: 4 legacy requirements"},
		{"the Release Renewal", renewal, "Billing (BILL): 5 of 5 undecided, 0 decided"},
		{"the Release Renewal", renewal, "Payments (PAY): 6 of 13 undecided, 7 decided"},
	} {
		if !strings.Contains(out, want.phrase) {
			t.Errorf("roadmap show does not print %q", want.phrase)
		} else if !strings.Contains(want.block, want.phrase) {
			t.Errorf("roadmap show prints %q, but not under %s", want.phrase, want.under)
		}
	}
	// Guards: new work has no decisions, and a Release with nothing new since
	// the agreement, or a Delivery Release, says nothing about it.
	if strings.Contains(initiatives["Gift cards"], "Decisions:") {
		t.Errorf("the new-work Initiative Gift cards prints decisions")
	}
	for _, block := range []struct{ name, text string }{{"Autumn", autumn}, {"Renewal", renewal}} {
		if strings.Contains(block.text, "New since the agreement") {
			t.Errorf("the Release %s, with no legacy requirement new since the agreement, prints one", block.name)
		}
	}
	if strings.Contains(autumn, "decided") {
		t.Errorf("the Delivery Release Autumn prints a decided count")
	}
	if t.Failed() {
		t.Logf("roadmap show printed:\n%s", out)
	}
}

func TestB3_74721edf_ApplyHelpDocumentsSetSelection(t *testing.T) {
	out, err := runRoot(t, "roadmap", "apply", "--help")
	if err != nil {
		t.Fatalf("roadmap apply --help: %v\n%s", err, out)
	}
	for _, field := range []string{
		"set_selection", "line_id", "line_ref", "rows", "clear",
		"requirement_kind", "requirement_id", "outcome", "reason",
		"expected_level", "expected_level_reason", "move", "ref",
	} {
		if !regexp.MustCompile(`\b` + regexp.QuoteMeta(field) + `\b`).MatchString(out) {
			t.Errorf("the help of roadmap apply does not name %s", field)
		}
	}
	for _, limit := range []struct{ what, pattern string }{
		{"1,000 rows an operation", `\b1,?000\b`},
		{"5,000 rows a plan", `\b5,?000\b`},
	} {
		if !regexp.MustCompile(limit.pattern).MatchString(out) {
			t.Errorf("the help of roadmap apply does not give the limit of %s", limit.what)
		}
	}
	if t.Failed() {
		t.Logf("roadmap apply --help printed:\n%s", out)
	}
}

const b3RefusedRowID = "e5e5e5e5-5555-4555-8555-555555555555"

const b3RefusedSelectionError = `{"index":2,"op":"set_selection","reason":"selection_rows_refused","rows":[
 {"position":1,"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","title":"Export invoices","reason":"reason_required"},
 {"position":2,"requirement_kind":"system_requirement","requirement_id":"` + b3RefusedRowID + `","title":null,"reason":"requirement_not_found"},
 {"position":3,"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","title":"Send reminders","reason":"selected_by_another_initiative",
  "held_by":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Provider connections"}}]}`

const b3SelectionPlanFile = `{"request_id":"b3-provider-sync","operations":[
 {"op":"update_line","line_id":"66666666-6666-4666-8666-666666666666","text":"Provider sync"},
 {"op":"set_selection","line_id":"66666666-6666-4666-8666-666666666666","rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","outcome":"drop"},
  {"requirement_kind":"system_requirement","requirement_id":"` + b3RefusedRowID + `","outcome":"keep"},
  {"requirement_kind":"user_requirement","requirement_id":"d4d4d4d4-4444-4444-8444-444444444444","outcome":"keep"}]}]}`

func TestB3_74721edf_ApplyPrintsEachRefusedRowOfASetSelection(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		args   []string
	}{
		{"without --dry-run", http.StatusUnprocessableEntity,
			`{"error":"selection_rows_refused","index":2,"errors":[` + b3RefusedSelectionError + `]}`,
			nil},
		{"with --dry-run", http.StatusOK,
			`{"data":{"operations":[
			  {"index":1,"op":"update_line","summary":"Rename initiative \"Provider sync\"","release":null},
			  {"index":2,"op":"set_selection","summary":"Set the selection of initiative \"Provider sync\": 2 keep, 0 change, 1 drop, 0 cleared, 0 moved","release":{"id":21,"ref":null,"name":"Cutover"}}],
			 "errors":[` + b3RefusedSelectionError + `]}}`,
			[]string{"--dry-run"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, c.status, c.reply)
			cobraWorkspace(t, server, 1)
			plan := writeRoadmapPlan(t, "plan.json", b3SelectionPlanFile)

			out, err := runRoot(t, append([]string{"roadmap", "apply", "--file", plan}, c.args...)...)
			text := b3Text(out, err)

			for _, line := range []struct{ what, pattern string }{
				{"row 1 by its title, with the reason in words",
					`(?m)^[ \t]*row 1: Export invoices: A drop in an initiative's selection needs a reason\.$`},
				{"row 2 by its requirement id (its title is null), with the reason in words",
					`(?m)^[ \t]*row 2: ` + b3RefusedRowID + `: This is not a legacy requirement of the initiative's release\.$`},
				{"row 3 by its title, with the reason in words and the Initiative that holds it",
					`(?m)^[ \t]*row 3: Send reminders: Another initiative already holds this legacy requirement\. \(held by Provider connections\)$`},
			} {
				if !regexp.MustCompile(line.pattern).MatchString(text) {
					t.Errorf("roadmap apply %s does not print %s: no line matches %s", c.name, line.what, line.pattern)
				}
			}
			// Guards: the refusal still ends the command with an error, after one request.
			if err == nil {
				t.Errorf("roadmap apply %s of a refused plan ended without an error:\n%s", c.name, out)
			}
			if len(*seen) != 1 {
				t.Errorf("roadmap apply %s sent %d requests; want 1: %#v", c.name, len(*seen), *seen)
			}
			if t.Failed() {
				t.Logf("roadmap apply %s printed:\n%s", c.name, text)
			}
		})
	}
}
