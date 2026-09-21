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
		"exit code, not just an agent's summary",
		// the executable-audit rule
		"exits non-zero below the floor",
		"retain its verbatim output",
		// never-give-up / no-laziness / no-silent-fallback
		"No row budget replaces behavioral granularity",
		"Below the floor is incomplete",
		"never silently truncated",
		// denominator classes that history shows get dropped
		"integrations and tests",
		"jobs/events/webhooks",
		// grain calibration and the no-babysitting rule
		"independently meaningful SRs",
		"continues across remaining",
		// REQ-CROSS-179/180: the platform measures first — the pass is
		// coverage-guided, and the before/after delta is the pass's receipt
		"authoritative store projection",
		"rank uncovered directories",
		"same measurement before and after",
		"untraced test files",
		// REQ-CROSS-181: the sweep is one autonomous invocation with a
		// file-coverage floor — no per-component human checkpoints
		"60% of eligible",
		"candidate extraction",
		"governed coverage",
		"no per-context\nbaseline reapproval",
	} {
		if !strings.Contains(s, marker) {
			t.Errorf("coverage contract marker missing from the skill: %q — "+
				"the contract is being diluted; see REQ-CROSS-178 for why each marker exists", marker)
		}
	}
}
