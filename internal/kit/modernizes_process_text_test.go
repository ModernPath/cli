package kit

import (
	"strings"
	"testing"
)

// Epic 334 (C1), test case c9213d28: the delivery process text the CLI
// installs says how an Epic drafted from an Initiative that replaces part of
// the legacy system links its new requirements with a modernizes trace, which
// rule admits that trace, that implementation approval waits for the business
// owner's agreement, and how an in-place plan takes the requirements it
// changes and presents the kept requirements at risk. Each sentence is asserted
// whole, with the whitespace folded and code marks (backticks) removed, so it
// may wrap or mark its words in the Markdown source.
func TestModernizesProcessText(t *testing.T) {
	process := strings.ReplaceAll(speedRuleText(t, "assets/rdd/PROCESS.md"), "`", "")
	for _, sentence := range []struct{ name, text string }{
		{"C1 c9213d28 the modernizes trace in the saved plan",
			"An Epic drafted from an Initiative that replaces part of the legacy system links each new requirement to the legacy requirement it modernizes with a modernizes trace in its saved plan."},
		{"C1 c9213d28 the rule that admits the trace",
			"The platform admits that trace only for a keep or change decision of the Epic's own Initiative; a later Epic carries it unchanged or retires it by holding its source, and reverse engineering never records it."},
		{"C1 c9213d28 approval waits for the business owner's agreement",
			"Such an Epic's implementation approval waits until the business owner's agreement holds its Initiative unchanged; the agent reports the gate's blocker and asks the business owner rather than working around it."},
		{"C1 c9213d28 in place the plan takes requirements and presents the kept requirements at risk",
			"In place, the plan takes the requirements it changes with their revised statements and links tests to kept requirements under their keep decisions, and the agent presents the kept requirements at risk with a fallback for each before asking for approval."},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(process, sentence.text) {
				t.Errorf("PROCESS.md is missing the sentence %q", sentence.text)
			}
		})
	}
}
