package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The 13 November end-to-end walkthrough learned each verification rule from
// the review bundle's blockers over four plan saves (finding F-3-1), and the
// review file shape by a refused write (F-3-2). The help states them.

// helpText runs `<args> --help` and returns its text with whitespace
// collapsed, so wrapped lines compare as one sentence.
func helpText(t *testing.T, args ...string) string {
	t.Helper()
	out, err := runRoot(t, append(args, "--help")...)
	if err != nil {
		t.Fatalf("%s --help: %v", strings.Join(args, " "), err)
	}
	return strings.Join(strings.Fields(out), " ")
}

func requireHelpPhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("%s does not say %q", name, phrase)
		}
	}
}

// The three rules the server's gates check on a plan's verification
// (Core.Planning.EpicVerification and EpicGates planning_blockers): one
// obligation per active criterion, a verifies trace from each declared test
// to its criterion, and an obligation for each applicable boundary.
func TestEpicPlanHelpStatesTheVerificationRules(t *testing.T) {
	requireHelpPhrases(t, "epics plan --help", helpText(t, "epics", "plan"), []string{
		"One obligation per active criterion:",
		"The gates report a repeated id as a duplicate obligation ID.",
		"A verifies trace from each declared test to its criterion, in traces:",
		`{"source_type":"test_case","source_id":"<test-case UUID>", "target_type":"criterion","target_id":"<criterion UUID>","link_kind":"verifies"}`,
		"completion needs a passing result of every test that verifies the criterion",
		"An obligation for each applicable boundary:",
		"A check that names the boundary does not count.",
		"epics gates check <id> --phase plan-ready",
	})
	requireHelpPhrases(t, "epics plan save --help", helpText(t, "epics", "plan", "save"), []string{
		"one obligation per active criterion",
		"a verifies trace from each declared test to its criterion",
		"an obligation for each applicable boundary",
	})
}

// The finding shape the server accepts (Core.Planning.EpicPhaseRecords
// record_review_body): severity material or note, and a description; any
// other field of a finding is dropped.
func TestEpicGatesReviewHelpShowsTheFindingShape(t *testing.T) {
	requireHelpPhrases(t, "epics gates review --help", helpText(t, "epics", "gates", "review"), []string{
		`"findings": [{"severity": "material" | "note", "description": "<what is wrong and what to change>"}]`,
		"A finding has two fields: severity, material or note, and a description that is not empty.",
		"The server gives each finding an id and stores no other field.",
		`{"severity": "material", "description": "Criterion 'Order total' has no test for an order with no items. Add a test case and its verifies trace."}`,
		`Use "findings": [] when there are none.`,
	})
}

// An empty list printed nothing at all (F-3-12): the person could not tell an
// empty System from a failed read.
func TestEpicListWithNoEpicsSaysSo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"items":[],"next_cursor":null}}`))
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "list")
	if err != nil {
		t.Fatal(err)
	}
	if want := "No Epics in this System yet. Create one with 'modernpath epics create --file epic.json'."; !strings.Contains(out, want) {
		t.Errorf("empty list output = %q, want %q", out, want)
	}

	out, err = runRoot(t, "epics", "list", "--status", "DONE")
	if err != nil {
		t.Fatal(err)
	}
	if want := "No Epics match these filters."; !strings.Contains(out, want) {
		t.Errorf("empty filtered list output = %q, want %q", out, want)
	}

	out, err = runRoot(t, "epics", "list", "--json")
	if err != nil || !json.Valid([]byte(out)) || strings.Contains(out, "No Epics") {
		t.Errorf("--json output must stay the JSON page: %q, %v", out, err)
	}
}
