package cmd

// Epic 471 (H5b), TestCase TC-H5B-S10, SR-3 criterion 1 (adfad808): roadmap
// show's help, as the CLI reference renders it, and the roadmap show section of
// modernpath-core/docs/cli.md and of the kit's CLI guide state when the
// planning lines appear: a line for the agreement of Now, with both of its
// limits, and a line for the undecided legacy requirements, before the other
// lines; that --json carries them as planning_waits; and that the acceptance
// line appears only once a linked item's own status is in progress, in review
// or done. Before the build none of them names planning_waits, so the test
// fails on its first phrase.

import (
	"os"
	"strings"
	"testing"
)

var h5bPlanningPhrases = []string{
	"planning_waits",
	"before those lines, an Initiative that replaces part of the legacy system and is not done has a line for the agreement of Now",
	"with a proposed Epic, under any Release, while the approval gate would refuse that Epic for the agreement",
	"with no Epic yet, in Now under a planned or active Modernization Release, while it is not in the latest agreement or, since it, its context, kind of work or decisions changed or its own or its Release's planning context changed or was opened",
	"a line for its undecided legacy requirements, with a context under a planned or active Modernization Release, while it has no Epic yet or the agreement line shows",
	"its acceptance line appears only once a linked item's own status is in progress, in review or done",
}

// h5bPlain is the text in lower case, with no backticks or line breaks.
func h5bPlain(text string) string {
	text = strings.ReplaceAll(text, "`", "")
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

// 25177583-3139-4915-b726-c05181e4e07b
func TestH5B_RoadmapShowHelpAndCLIGuideStateThePlanningLinesAndTheAcceptanceLine(t *testing.T) {
	docs := map[string]string{
		"the CLI reference's roadmap show section": referenceSection(t, renderCLIReference(rootCmd), "modernpath roadmap show"),
	}
	for _, path := range []string{"../../../docs/cli.md", "../internal/kit/assets/cli-guide/cli.md"} {
		body, err := os.ReadFile(path)
		// docs/cli.md lies outside this module; the core image tests the module
		// alone. Its identical kit copy is always checked.
		if os.IsNotExist(err) && path == "../../../docs/cli.md" {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		section := d2GuideSection(string(body), "roadmap show")
		if section == "" {
			t.Fatalf("%s has no section that documents roadmap show", path)
		}
		docs["the roadmap show section of "+path] = section
	}
	for name, text := range docs {
		plain := h5bPlain(text)
		for _, phrase := range h5bPlanningPhrases {
			if !strings.Contains(plain, strings.ToLower(phrase)) {
				t.Errorf("%s does not say %q", name, phrase)
			}
		}
	}
}
