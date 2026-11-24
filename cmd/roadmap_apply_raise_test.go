package cmd

// Epic 336 (D2), test case 1c378219 (TC-D2-23), criterion d0930ec6: a raise
// of a changed requirement's expected level to compared is refused through
// the Roadmap plan with the reason raise_needs_accepted_differences and its
// sentence in message; `roadmap apply`, with or without --dry-run, prints the
// sentence for the refused row. B3's command prints a refused row's reason in
// words it knows, or the code, and never its message, so the assertion on the
// printed sentence fails. The test uses no new production symbol.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// Epic 337 (D3) admits a raise once accepted differences name the
// requirement and none is open, so the sentences the server answers say what
// the raise waits for (the fixture changes with the server; it is not RED).
const d2RaiseSentence = "Export invoices can expect compared only once each intended change is an accepted difference. At least one accepted difference must name it and none may be open; 0 accepted and 1 open name it now."

const d2RaiseAloneSentence = "A raise of Provider connection retries to compared is sent as the level alone, without the outcome, on its row already decided change."

const d2RaiseSelectionError = `{"index":1,"op":"set_selection","reason":"selection_rows_refused","rows":[
 {"position":1,"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","title":"Export invoices",
  "reason":"raise_needs_accepted_differences","message":"` + d2RaiseSentence + `"},
 {"position":2,"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","title":"Provider connection retries",
  "reason":"raise_needs_accepted_differences","message":"` + d2RaiseAloneSentence + `"}]}`

const d2RaisePlanFile = `{"request_id":"d2-raise","operations":[
 {"op":"set_selection","line_id":"66666666-6666-4666-8666-666666666666","rows":[
  {"requirement_kind":"user_requirement","requirement_id":"a1a1a1a1-1111-4111-8111-111111111111","expected_level":"compared","expected_level_reason":"The totals are compared"},
  {"requirement_kind":"system_requirement","requirement_id":"b2b2b2b2-2222-4222-8222-222222222222","outcome":"change","reason":"A new API","expected_level":"compared","expected_level_reason":"The totals are compared"}]}]}`

func TestD2_1c378219_ApplyPrintsTheSentenceOfARefusedRaise(t *testing.T) {
	cases := []struct {
		name   string
		status int
		reply  string
		args   []string
	}{
		{"without --dry-run", http.StatusUnprocessableEntity,
			`{"error":"selection_rows_refused","index":1,"errors":[` + d2RaiseSelectionError + `]}`,
			nil},
		{"with --dry-run", http.StatusOK,
			`{"data":{"operations":[
			  {"index":1,"op":"set_selection","summary":"Set the selection of initiative \"Provider sync\": 0 keep, 1 change, 0 drop, 0 cleared, 0 moved","release":{"id":21,"ref":null,"name":"Cutover"}}],
			 "errors":[` + d2RaiseSelectionError + `],"dry_run":true}}`,
			[]string{"--dry-run"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			server, seen := roadmapServer(t, c.status, c.reply)
			cobraWorkspace(t, server, 1)
			plan := writeRoadmapPlan(t, "plan.json", d2RaisePlanFile)

			out, err := runRoot(t, append([]string{"roadmap", "apply", "--file", plan}, c.args...)...)
			text := b3Text(out, err)

			for _, line := range []struct{ what, pattern string }{
				{"the sentence of the level-only raise on row 1",
					`(?m)^[ \t]*row 1: .*` + regexp.QuoteMeta(d2RaiseSentence) + `$`},
				{"the sentence of the raise sent with the outcome on row 2",
					`(?m)^[ \t]*row 2: .*` + regexp.QuoteMeta(d2RaiseAloneSentence) + `$`},
			} {
				if !regexp.MustCompile(line.pattern).MatchString(text) {
					t.Errorf("roadmap apply %s does not print %s: no line matches %s", c.name, line.what, line.pattern)
				}
			}
			for _, line := range strings.Split(text, "\n") {
				if strings.Contains(line, "row 1:") || strings.Contains(line, "row 2:") {
					if strings.Contains(line, "raise_needs_accepted_differences") {
						t.Errorf("roadmap apply %s prints the refused row's code instead of its sentence: %s", c.name, line)
					}
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
