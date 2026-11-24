package cmd

// Epic 337 (D3), test case TC-D3-11 (45d46836-5c6c-4b09-9ed6-d450e362dd45),
// criterion 212da91c ("Results on a requirement's spec, a Fail raised as a
// difference at the sign-off, and a new report after a spec moved" at 6):
// modernpath acceptance report open --retest sends retest and prints the new
// draft, modernpath acceptance specs list prints each spec's requirement and
// revision or a dash for a person's spec, and modernpath acceptance specs
// refresh --line posts the refresh and prints the specs. A refused acceptance
// command prints the server's code and its sentence, and the help of
// acceptance release says that a superseded report is closed and never the
// live one. The help of report record says that a requirement's spec needs
// notes for fail, blocked and not_run (the plan's change boundary).
//
// On slice 1 specs list prints no requirement, specs refresh prints nothing,
// report open --retest sends no retest, a refusal {"error": "retest_not_needed",
// "message": ...} prints the bare code (cmd/refusal.go), and the help of
// acceptance release does not name superseded, so each assertion fails. That
// report open without --retest sends no retest holds on the slice. The test
// drives the root command and uses no new Go symbol.

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const (
	d3SpecMoved   = "44444444-4444-4444-8444-444444444444"
	d3SpecPerson  = "55555555-5555-4555-8555-555555555555"
	d3NewReport   = "66666666-6666-4666-8666-666666666666"
	d3UserReqOne  = "a1a1a1a1-1111-4111-8111-111111111111"
	d3UserReqMove = "a2a2a2a2-2222-4222-8222-222222222222"
)

// The sentences the server answers with the acceptance codes.
const (
	d3RetestNotNeeded = "The signed-off report still covers every spec at its current revision, so no new test is needed."
	d3SpecReadOnly    = "This spec follows its requirement, so the platform writes it. Write a spec of your own to test something else."
	d3NotesRequired   = "Write notes before recording Fail, Blocked or Not run on a requirement's spec, so the reason is kept."
)

// d3Specs are an Initiative's live specs as the server serves them: one the
// platform wrote at its requirement's current revision, one whose requirement
// has a newer revision, and a person's spec.
var d3Specs = []any{
	map[string]any{"id": accSpec, "line_id": accLine, "title": "Export invoices as PDF", "revision": 3,
		"requirement_id": d3UserReqOne, "requirement_revision": 2,
		"requirement": map[string]any{"kind": "user_requirement", "id": d3UserReqOne, "display_ref": "UR-40-0", "title": "Export invoices as PDF", "revision": 2}},
	map[string]any{"id": d3SpecMoved, "line_id": accLine, "title": "Retry webhooks", "revision": 1,
		"requirement_id": d3UserReqMove, "requirement_revision": 1,
		"requirement": map[string]any{"kind": "user_requirement", "id": d3UserReqMove, "display_ref": "UR-41-0", "title": "Retry webhooks", "revision": 2}},
	map[string]any{"id": d3SpecPerson, "line_id": accLine, "title": "Pay by card", "revision": 1,
		"requirement_id": nil, "requirement_revision": nil, "requirement": nil},
}

// d3RequireSpecLines checks that out prints each spec of d3Specs on its line
// with its requirement's short reference and revision, or a dash.
func d3RequireSpecLines(t *testing.T, what, out string) {
	t.Helper()
	for _, line := range []struct{ name, pattern string }{
		{"the spec of UR-40-0 at revision 2",
			d3SpecLine(accSpec, 3, "UR-40-0 revision 2", "Export invoices as PDF")},
		{"the spec of UR-41-0 at revision 1, whose requirement is now at revision 2",
			d3SpecLine(d3SpecMoved, 1, "UR-41-0 revision 1, now revision 2", "Retry webhooks")},
		{"the person's spec with a dash",
			d3SpecLine(d3SpecPerson, 1, "—", "Pay by card")},
	} {
		if !regexp.MustCompile(line.pattern).MatchString(out) {
			t.Errorf("%s does not print %s: no line matches %s\n%s", what, line.name, line.pattern, out)
		}
	}
}

// d3SpecLine is the pattern of one line of the specs: the spec's id, its own
// revision, its requirement (or a dash) and its title.
func d3SpecLine(id string, revision int, requirement, title string) string {
	return `(?m)^` + regexp.QuoteMeta(id) + `\s+r` + strconv.Itoa(revision) + `\s+` + regexp.QuoteMeta(requirement) + `\s+` + regexp.QuoteMeta(title) + `$`
}

func TestD3_45d46836_SpecsListPrintsEachSpecsRequirementOrADash(t *testing.T) {
	server, seen := accServer(t, http.StatusOK, d3Specs)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "acceptance", "specs", "list", "--line", accLine)
	if err != nil {
		t.Fatalf("acceptance specs list: %v\n%s", err, out)
	}
	if len(*seen) != 1 || (*seen)[0].method != http.MethodGet || (*seen)[0].path != "/api/roadmap/lines/"+accLine+"/acceptance-specs" {
		t.Fatalf("requests = %#v", *seen)
	}
	d3RequireSpecLines(t, "acceptance specs list", out)
}

func TestD3_45d46836_SpecsRefreshPostsTheRefreshAndPrintsTheSpecs(t *testing.T) {
	server, seen := accServer(t, http.StatusOK, d3Specs)
	cobraWorkspace(t, server, 1)
	out, err := runRoot(t, "acceptance", "specs", "refresh", "--line", accLine)
	if err != nil {
		t.Fatalf("acceptance specs refresh: %v\n%s", err, out)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %#v; want one", *seen)
	}
	got := (*seen)[0]
	if got.method != http.MethodPost || got.path != "/api/roadmap/lines/"+accLine+"/acceptance-specs/refresh" || got.body["system_id"] != float64(1) {
		t.Fatalf("request = %s %s %#v; want POST /api/roadmap/lines/%s/acceptance-specs/refresh with system_id 1", got.method, got.path, got.body, accLine)
	}
	d3RequireSpecLines(t, "acceptance specs refresh", out)

	server, _ = accServer(t, http.StatusOK, d3Specs)
	cobraWorkspace(t, server, 1)
	stdout, err := runRootStdout(t, "acceptance", "specs", "refresh", "--line", accLine, "--json")
	if err != nil {
		t.Fatalf("acceptance specs refresh --json: %v\n%s", err, stdout)
	}
	var printed []any
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil {
		t.Fatalf("acceptance specs refresh --json did not print the served list: %v\n%s", err, stdout)
	}
	want, _ := json.Marshal(d3Specs)
	have, _ := json.Marshal(printed)
	if string(want) != string(have) {
		t.Errorf("acceptance specs refresh --json printed %s; want the served list %s", have, want)
	}
}

func TestD3_45d46836_ReportOpenRetestSendsRetestAndPrintsTheNewDraft(t *testing.T) {
	draft := map[string]any{"id": d3NewReport, "line_id": accLine, "release_id": 7, "status": "draft",
		"replaces_report_id": accReport, "tester_name": "Tess Tester", "signed_off_by_name": nil, "results": []any{}}
	for _, c := range []struct {
		name string
		file string
		want map[string]any
	}{
		{"without a file", "", map[string]any{"system_id": float64(1), "retest": true}},
		{"with the release in a file", `{"release_id":7}`, map[string]any{"system_id": float64(1), "release_id": float64(7), "retest": true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			server, seen := accServer(t, http.StatusCreated, draft)
			cobraWorkspace(t, server, 1)
			args := []string{"acceptance", "report", "open", "--line", accLine, "--retest"}
			if c.file != "" {
				args = append(args, "--file", writeAccFile(t, "report.json", c.file))
			}
			out, err := runRoot(t, args...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", args, err, out)
			}
			if len(*seen) != 1 || (*seen)[0].method != http.MethodPost || (*seen)[0].path != "/api/roadmap/lines/"+accLine+"/acceptance-reports" {
				t.Fatalf("requests = %#v", *seen)
			}
			gotBody, _ := json.Marshal((*seen)[0].body)
			wantBody, _ := json.Marshal(c.want)
			if string(gotBody) != string(wantBody) {
				t.Errorf("report open --retest sent %s; want %s", gotBody, wantBody)
			}
			for _, want := range []string{"id: " + d3NewReport, "status: draft", "replaces_report_id: " + accReport} {
				if !strings.Contains(out, want) {
					t.Errorf("report open --retest does not print %q of the new draft:\n%s", want, out)
				}
			}
		})
	}

	// Guard: without --retest the command sends no retest (holds on the slice).
	server, seen := accServer(t, http.StatusOK, draft)
	cobraWorkspace(t, server, 1)
	if out, err := runRoot(t, "acceptance", "report", "open", "--line", accLine); err != nil {
		t.Fatalf("report open: %v\n%s", err, out)
	}
	if len(*seen) != 1 {
		t.Fatalf("requests = %#v", *seen)
	}
	if _, sent := (*seen)[0].body["retest"]; sent {
		t.Errorf("report open without --retest sent retest: %#v", (*seen)[0].body)
	}
}

func TestD3_45d46836_ARefusedAcceptanceCommandPrintsTheCodeAndItsSentence(t *testing.T) {
	for _, c := range []struct {
		code, sentence string
		args           []string
		file           string
	}{
		{"retest_not_needed", d3RetestNotNeeded, []string{"acceptance", "report", "open", "--line", accLine, "--retest"}, ""},
		{"requirement_spec_read_only", d3SpecReadOnly, []string{"acceptance", "specs", "update", accSpec}, `{"title":"Export invoices"}`},
		{"requirement_spec_read_only", d3SpecReadOnly, []string{"acceptance", "specs", "retire", accSpec}, ""},
		{"notes_required", d3NotesRequired, []string{"acceptance", "report", "record", accReport, "--spec", accSpec}, `{"outcome":"not_run"}`},
	} {
		t.Run(c.code+" "+strings.Join(c.args[1:3], " "), func(t *testing.T) {
			server, seen := accServer(t, http.StatusUnprocessableEntity, map[string]any{"error": c.code, "message": c.sentence})
			cobraWorkspace(t, server, 1)
			args := c.args
			if c.file != "" {
				args = append(append([]string{}, args...), "--file", writeAccFile(t, "input.json", c.file))
			}
			_, err := runRoot(t, args...)
			if err == nil {
				t.Fatalf("%v: a refusal ended without an error", args)
			}
			if !strings.Contains(err.Error(), c.code) {
				t.Errorf("%v: the refusal does not name the code %s: %v", args, c.code, err)
			}
			if !strings.Contains(err.Error(), c.sentence) {
				t.Errorf("%v: the refusal does not print the server's sentence %q: %v", args, c.sentence, err)
			}
			if len(*seen) != 1 {
				t.Errorf("requests = %#v", *seen)
			}
		})
	}
}

func TestD3_45d46836_ReleaseHelpSaysASupersededReportIsClosedAndNeverLive(t *testing.T) {
	out, err := runRoot(t, "acceptance", "release", "--help")
	if err != nil {
		t.Fatalf("acceptance release --help: %v\n%s", err, out)
	}
	help := d3Folded(out)
	for _, want := range []string{"superseded", "A superseded report is closed and never the live one"} {
		if !strings.Contains(help, want) {
			t.Errorf("the help of acceptance release does not say %q:\n%s", want, help)
		}
	}
}

func TestD3_45d46836_RecordHelpSaysARequirementsSpecNeedsNotes(t *testing.T) {
	out, err := runRoot(t, "acceptance", "report", "record", "--help")
	if err != nil {
		t.Fatalf("acceptance report record --help: %v\n%s", err, out)
	}
	help := d3Folded(out)
	if want := "On a spec the platform wrote from a requirement, fail, blocked and not_run need notes_md"; !strings.Contains(help, want) {
		t.Errorf("the help of acceptance report record does not say %q:\n%s", want, help)
	}
}
