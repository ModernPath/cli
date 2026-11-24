package kit

import (
	"strings"
	"testing"
)

// Epic 390 (D5), test case TC-D5-4: PROCESS.md, which the kit embeds, says what
// a historical result carries once a capture test case's entry names no
// criterion. The sentence is read with the whitespace folded and the code marks
// stripped, so it may wrap or mark its words.
func TestProcessHistoryResultSentence(t *testing.T) {
	process := strings.Join(strings.Fields(strings.ReplaceAll(speedRuleText(t, "assets/rdd/PROCESS.md"), "`", "")), " ")

	for _, sentence := range []struct{ name, text string }{
		{"D5 TC-D5-4 a historical result has a TestCase Trace and the criterion Traces its entry names; a capture test case's result names none",
			"Each historical result has a TestCase Trace and the criterion Traces its entry names; a capture test case's result names none."},
		{"D5 TC-D5-4 runs have no lifecycle (guard)",
			"Runs have no pre-registration or finalization lifecycle."},
		{"D5 TC-D5-4 the capture command (guard)",
			"Capture with modernpath modernization record; use modernpath evidence record, or modernpath epics build record after a delegated build, for current direct proof."},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(process, sentence.text) {
				t.Errorf("PROCESS.md is missing the sentence %q", sentence.text)
			}
		})
	}
}
