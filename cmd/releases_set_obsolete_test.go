package cmd

// Epic 530 (E1c), TestCase 2c8751ba, criterion b981b2e7: `releases activate`
// refused with incumbent_has_unfinished_work prints its unfinished-work line
// when has_unfinished_work is true or missing, and, when the release's
// requirements_to_set_obsolete is not empty, "<Release> (<slug>) still has
// dropped requirements to set obsolete; close it on its close page first."
// with a line "  <reference> <title>" for each item of that bounded list, then
// how many more are not shown; after the unfinished-work line when unfinished
// work also stops activation. The list is served cut at the close record's
// list bound (50); the tests use bound+1 rows. Before the build only the
// unfinished-work line was printed, so the list tests fail first.

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// e1cListBound is the close record's list bound, ReleaseClose.list_bound/0 on
// the server (50 on his word of 25 Nov 2026).
const e1cListBound = 50

const e1cUnfinishedLine = "Spring (current) still has unfinished work or decisions that go on; close it on its close page first."
const e1cObsoleteLine = "Spring (current) still has dropped requirements to set obsolete; close it on its close page first."

// e1cObsoleteList is requirements_to_set_obsolete with count and the first
// shown items, as the server cuts it at the list bound.
func e1cObsoleteList(count, shown int) string {
	items := make([]string, 0, shown)
	for i := 1; i <= shown; i++ {
		items = append(items, fmt.Sprintf(`{"kind":"system_requirement","id":"00000000-0000-4000-8000-%012d","display_ref":"SR-77-%d","title":"Dropped rule %d"}`, i, i, i))
	}
	return fmt.Sprintf(`{"count":%d,"items":[%s]}`, count, strings.Join(items, ","))
}

// e1cActivate runs `releases activate next --close-current` against a server
// that refuses with the given open_releases entry fields, and returns what the
// person sees.
func e1cActivate(t *testing.T, releaseFields string) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"data":{"items":[{"id":1,"system_id":4,"slug":"current","status":"active"},{"id":2,"system_id":4,"slug":"next","status":"planned"}]}}`)
			return
		}
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":"incumbent_has_unfinished_work","detail":"body text 8841","open_releases":[{"id":1,"name":"Spring","slug":"current","status":"active","system_id":4`+releaseFields+`}]}`)
	}))
	defer server.Close()
	cmd := newReleasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	cmd.SetArgs([]string{"activate", "next", "--close-current", "--pin-stdin"})
	cmd.SetIn(strings.NewReader("482913\n"))
	var printed bytes.Buffer
	cmd.SetOut(&printed)
	cmd.SetErr(&printed)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	err := cmd.Execute()
	if err == nil {
		t.Fatal("the refused activation must fail the command")
	}
	text := err.Error() + "\n" + printed.String()
	for _, never := range []string{"body text 8841", "482913"} {
		if strings.Contains(text, never) {
			t.Errorf("releases activate printed %q:\n%s", never, text)
		}
	}
	return text
}

// e1cLineAt is the index of the whole line in lines, or -1.
func e1cLineAt(lines []string, line string) int {
	for i, l := range lines {
		if l == line {
			return i
		}
	}
	return -1
}

// e1cRequireList checks the obsolete line, a line per shown item right under
// it, in order, and the more line after them.
func e1cRequireList(t *testing.T, text string, shown int, more string) {
	t.Helper()
	lines := strings.Split(text, "\n")
	at := e1cLineAt(lines, e1cObsoleteLine)
	if at < 0 {
		t.Fatalf("releases activate does not print the line %q:\n%s", e1cObsoleteLine, text)
	}
	for i := 1; i <= shown; i++ {
		want := fmt.Sprintf("  SR-77-%d Dropped rule %d", i, i)
		if at+i >= len(lines) || lines[at+i] != want {
			t.Fatalf("line %d under the obsolete line is not %q:\n%s", i, want, text)
		}
	}
	if at+shown+1 >= len(lines) || lines[at+shown+1] != more {
		t.Fatalf("the line after the shown items is not %q:\n%s", more, text)
	}
}

// 2c8751ba-b58f-4036-8332-3a3039823309
func TestE1c_2c8751ba_ActivationPrintsTheRequirementsToSetObsoleteCutAtTheBound(t *testing.T) {
	text := e1cActivate(t, `,"has_unfinished_work":false,"undelivered_decisions":[],"requirements_to_set_obsolete":`+e1cObsoleteList(e1cListBound+1, e1cListBound))
	e1cRequireList(t, text, e1cListBound, "  1 more dropped requirement is not shown here.")
	if strings.Contains(text, "still has unfinished work") {
		t.Errorf("only the list stops activation, but the unfinished-work line is printed:\n%s", text)
	}
	if !strings.Contains(text, "incumbent_has_unfinished_work") {
		t.Errorf("releases activate does not name the refusal:\n%s", text)
	}

	text = e1cActivate(t, `,"has_unfinished_work":false,"requirements_to_set_obsolete":`+e1cObsoleteList(e1cListBound+3, e1cListBound))
	e1cRequireList(t, text, e1cListBound, "  3 more dropped requirements are not shown here.")
}

// 2c8751ba-b58f-4036-8332-3a3039823309
func TestE1c_2c8751ba_ActivationPrintsBothLinesWhenUnfinishedWorkAlsoStopsIt(t *testing.T) {
	text := e1cActivate(t, `,"has_unfinished_work":true,"undelivered_decisions":[{"id":"44444444-4444-4444-8444-444444444444","display_number":12,"title":"Replace invoicing","decisions":2}],"requirements_to_set_obsolete":`+e1cObsoleteList(e1cListBound+1, e1cListBound))
	lines := strings.Split(text, "\n")
	unfinished := e1cLineAt(lines, e1cUnfinishedLine)
	decisions := e1cLineAt(lines, "  Replace invoicing (INIT-12): 2 decisions go on")
	obsolete := e1cLineAt(lines, e1cObsoleteLine)
	if unfinished < 0 || decisions < 0 {
		t.Fatalf("releases activate lacks the unfinished-work line or its decision line:\n%s", text)
	}
	if obsolete < 0 || obsolete < decisions || decisions < unfinished {
		t.Fatalf("the obsolete line must come after the unfinished-work line and its lines:\n%s", text)
	}
	e1cRequireList(t, text, e1cListBound, "  1 more dropped requirement is not shown here.")
}

// 2c8751ba-b58f-4036-8332-3a3039823309
func TestE1c_2c8751ba_ActivationFromAnOlderServerPrintsOnlyTheUnfinishedWorkLine(t *testing.T) {
	for name, fields := range map[string]string{
		"fields missing":   ``,
		"empty list":       `,"has_unfinished_work":true,"requirements_to_set_obsolete":{"count":0,"items":[]}`,
		"list set to null": `,"requirements_to_set_obsolete":null`,
	} {
		t.Run(name, func(t *testing.T) {
			text := e1cActivate(t, fields)
			if e1cLineAt(strings.Split(text, "\n"), e1cUnfinishedLine) < 0 {
				t.Errorf("releases activate lacks the unfinished-work line:\n%s", text)
			}
			if strings.Contains(text, "dropped requirement") {
				t.Errorf("releases activate prints a list the server did not send:\n%s", text)
			}
		})
	}
}
