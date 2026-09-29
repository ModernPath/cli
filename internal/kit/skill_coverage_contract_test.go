package kit

import (
	"strings"
	"testing"
)

// REQ-CROSS-178: the reverse-engineering skill carries the coverage contract.
//
// Twice in one day (2026-08-15) coverage discipline lived only in prose and
// failed: a sweep instructed at "5-10 rows per context" produced a 46-row
// "complete" ledger, and a "110/110 endpoints routed" claim audited to 65/110.
// The contract lives in the installed skill and its linked coverage reference —
// promoted there with the skill itself — and skill prose has ALSO been
// lost before (REQ-CROSS-174's pointerization). This test pins the
// load-bearing markers so the contract cannot be diluted or dropped without a
// red build.
func TestReverseEngineerSkillCarriesTheCoverageContract(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	s := installedReverseEngineeringInstructions(t, root)

	for _, marker := range []string{
		// Separate executable extraction checks from the citation count check.
		"Account for every item as extracted, already covered, excluded with a reason, or unresolved",
		"Distinct files cited by persisted rows, excluded with a reason, or unresolved",
		"does not enforce percentage coverage",
		"Counts and percentages describe the inventory; they do not define a passing gate",
		"command, output and exit code",
		"Missing denominators, unresolved mappings",
		"Do not shrink a denominator",
		"Zero recognized citations over nonempty expected input is a failed measurement",
		// Keep the complete sweep and authoritative coverage distinctions.
		"jobs/events/webhooks",
		"integrations and tests",
		"frozen inventory and authoritative read-back",
		"uncovered items for each inventory",
		"Validate any checker against known-good and known-bad inputs",
		"candidate links do not establish governed or verified behavior",
		"Unresolved in-scope work remains incomplete",
		"full sweep across remaining authorized contexts without per-context reapproval",
	} {
		if !strings.Contains(s, marker) {
			t.Errorf("coverage contract marker missing from the skill: %q — "+
				"the contract is being diluted; see REQ-CROSS-178 for why each marker exists", marker)
		}
	}
}
