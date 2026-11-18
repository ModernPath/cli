package cmd

import (
	"strings"
	"testing"
)

// Epic 437 (SR-4922 criterion 16cae995, SR-4923): the pass-to-command map
// names the Active Engineering Constraints read where the three passes read
// the planning contexts, and the Cold review row names the bundle field the
// reviewer reads and the `EC-<number>` citation with the revision. The
// sentence of each row resolves the kit's tool-neutral wording to a command.
// TestCase 0b25817c-93cf-4e46-b471-bb4227e342cc
func TestCLIReferencePhaseMapNamesTheConstraintsRead(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	loop := ref[:strings.Index(ref, "## Global flags")]
	for _, want := range []struct{ row, text string }{
		{"| Session entry and scope |", "`constraints list --state active`"},
		{"| Session entry and scope |", "`constraints get <ref>`"},
		{"| Plan |", "`constraints list --state active`"},
		{"| Plan |", "`constraints get <ref>`"},
		{"| Plan |", "EC ref"},
		{"| Implement |", "`constraints list --state active`"},
		{"| Implement |", "`constraints get <ref>`"},
		{"| Cold review |", "`engineering_constraints`"},
		{"| Cold review |", "`planning_contexts`"},
		{"| Cold review |", "`EC-<number>`"},
		{"| Cold review |", "revision"},
		{"| Cold review |", "severity `note`"},
	} {
		if row := phaseRow(t, loop, want.row); !strings.Contains(row, want.text) {
			t.Errorf("the %s row does not carry %q: %s", want.row, want.text, row)
		}
	}
}
