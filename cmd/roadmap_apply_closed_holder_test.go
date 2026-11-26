package cmd

// Epic 529 (E1b), TestCase 1656b01d, SR 1dd91c2f criterion ae25a636: the help
// of `roadmap apply` says that a row with move: true cannot take a decision of
// an Initiative under a closed Modernization Release, also when the row is
// held; and `roadmap apply`, with and without --dry-run, prints a row refused
// with selection_row_closed as `row <n>: ` and the server's sentence, after
// one request, and ends with an error. Before the build the help had no such
// sentence, so the help test fails; the command already printed a row's
// message, so the printed row passes on its own.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

const e1bClosedSentence = "UR-41-1 Shift import stays with Scheduling: its decision was recorded when field-planner-03 closed, and it stays with that closed Release."

const e1bClosedSelectionError = `{"index":1,"op":"set_selection","reason":"selection_rows_refused","rows":[
 {"position":1,"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","title":"Shift import","reason":"selection_row_closed",
  "message":"` + e1bClosedSentence + `",
  "held_by":{"id":"c3c3c3c3-3333-4333-8333-333333333333","title":"Scheduling"}}]}`

const e1bClosedPlanFile = `{"request_id":"e1b-closed-holder","operations":[
 {"op":"set_selection","line_id":"66666666-6666-4666-8666-666666666666","rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","outcome":"keep","move":true}]}]}`

// 1656b01d-9291-432f-8b92-aaecc40995bc
func TestE1b_RoadmapApplyHelpSaysMoveCannotTakeADecisionOfAClosedRelease(t *testing.T) {
	out, err := runRoot(t, "roadmap", "apply", "--help")
	if err != nil {
		t.Fatalf("roadmap apply --help: %v\n%s", err, out)
	}
	sentence := "A row with move: true cannot take a decision of an Initiative under a closed Modernization Release, also when the row is held; that decision stays with the closed Release."
	if !strings.Contains(c1FoldedText(out), sentence) {
		t.Errorf("the help of roadmap apply does not say %q:\n%s", sentence, out)
	}
}

// 1656b01d-9291-432f-8b92-aaecc40995bc
func TestE1b_RoadmapApplyPrintsTheClosedSentenceOfARefusedMove(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		args   []string
	}{
		{"without --dry-run", http.StatusUnprocessableEntity,
			`{"error":"selection_rows_refused","index":1,"errors":[` + e1bClosedSelectionError + `]}`,
			nil},
		{"with --dry-run", http.StatusOK,
			`{"data":{"operations":[
			  {"index":1,"op":"set_selection","summary":"Set the selection of initiative \"Route planning\": 1 keep, 0 change, 0 drop, 0 cleared, 1 moved","release":{"id":21,"ref":null,"name":"field-planner-04"}}],
			 "errors":[` + e1bClosedSelectionError + `],"dry_run":true}}`,
			[]string{"--dry-run"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, c.status, c.reply)
			cobraWorkspace(t, server, 1)
			plan := writeRoadmapPlan(t, "plan.json", e1bClosedPlanFile)

			out, err := runRoot(t, append([]string{"roadmap", "apply", "--file", plan}, c.args...)...)
			text := b3Text(out, err)

			if err == nil {
				t.Errorf("roadmap apply %s of a refused plan ended without an error:\n%s", c.name, out)
			}
			if len(*seen) != 1 {
				t.Errorf("roadmap apply %s sent %d requests; want 1", c.name, len(*seen))
			}
			line := "row 1: " + e1bClosedSentence
			if !regexp.MustCompile(`(?m)^[ \t]*` + regexp.QuoteMeta(line) + `$`).MatchString(text) {
				t.Errorf("roadmap apply %s does not print the line %q:\n%s", c.name, line, text)
			}
			if strings.Contains(text, "selection_row_closed") || strings.Contains(text, "(held by") {
				t.Errorf("roadmap apply %s prints the code or Held by beside the sentence:\n%s", c.name, text)
			}
		})
	}
}
