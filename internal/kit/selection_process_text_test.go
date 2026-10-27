package kit

import (
	"strings"
	"testing"
)

// Epic 332 (B3), test case 2994750d (TC-B3-23): the delivery process text the
// CLI installs says what an Initiative's selection is, how an agent decides a
// selection at volume, and that the business owner's agreement of the Now
// plan is the human stop for a selection. Each sentence is asserted whole,
// with the whitespace folded and code marks (backticks) removed, so it may
// wrap or mark its command names in the Markdown source.
func TestSelectionProcessText(t *testing.T) {
	process := strings.ReplaceAll(speedRuleText(t, "assets/rdd/PROCESS.md"), "`", "")
	for _, sentence := range []struct{ name, text string }{
		{"B3 2994750d the selection",
			"An Initiative that replaces part of the legacy system has a selection: keep, change and drop rows, a reason on each drop, and one row per legacy requirement for each System."},
		{"B3 2994750d an agent decides a selection at volume",
			"An agent decides a selection at volume: it reads a context with roadmap selection, suggests each row's outcome with its reason, sums the file up in plain words for the person, checks it with roadmap apply --dry-run, and applies it only on the person's word, naming itself with --agent."},
		{"B3 2994750d the human stop for a selection",
			"The business owner's agreement of the Now plan, on the Roadmap, is the human stop for a selection, which no agent and no command records."},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(process, sentence.text) {
				t.Errorf("PROCESS.md is missing the sentence %q", sentence.text)
			}
		})
	}
}
