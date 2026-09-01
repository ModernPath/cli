package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-178: the reverse-engineering skill carries the coverage contract.
//
// Twice in one day (2026-08-15) coverage discipline lived only in prose and
// failed: a sweep instructed at "5-10 rows per context" produced a 46-row
// "complete" ledger, and a "110/110 endpoints routed" claim audited to 65/110.
// The contract lives in the package skill's coverage-contract section —
// promoted there with the skill itself — and skill prose has ALSO been
// lost before (REQ-CROSS-174's pointerization). This test pins the
// load-bearing markers so the contract cannot be diluted or dropped without a
// red build.
func TestReverseEngineerSkillCarriesTheCoverageContract(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("assets", "rdd", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)

	for _, marker := range []string{
		// the floor and its nature
		"floor is 90%",
		"exit code — never a sentence",
		// the executable-audit rule
		"coverage-audit",
		"exits non-zero below the floor",
		"embeds the scripts' verbatim output",
		// never-give-up / no-laziness / no-silent-fallback
		"No row budgets exist",
		"Below the floor, the pass is not done",
		"No silent fallbacks",
		"no such thing as a row budget",
		// the recorded failures that justify all of it
		"65/110",
		"46-row",
		// denominator classes that history shows get dropped
		"every test file",
		"scheduled job",
		// grain calibration and the no-babysitting rule
		"978 requirements",
		"under-derived, full stop",
		"relaunches for the remainder",
		// REQ-CROSS-179/180: the platform measures first — the pass is
		// coverage-guided, and the before/after delta is the pass's receipt
		"modernpath coverage --json",
		"ranked uncovered directories",
		"the same command is the after",
		"untraced test files",
		// REQ-CROSS-181: the sweep is one autonomous invocation with a
		// file-coverage floor — no per-component human checkpoints
		"well over 50%",
		"file-coverage floor",
		"one invocation loops",
		"no human checkpoint inside the loop",
	} {
		if !strings.Contains(s, marker) {
			t.Errorf("coverage contract marker missing from the skill: %q — "+
				"the contract is being diluted; see REQ-CROSS-178 for why each marker exists", marker)
		}
	}
}
