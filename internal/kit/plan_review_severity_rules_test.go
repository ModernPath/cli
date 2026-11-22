package kit

import "testing"

// Epic 466: the narrow severity rule for plan reviews. A weakness of a planned
// test is material only when no other test catches the wrong implementation,
// and each material plan finding states its wrong outcome. Each test asserts
// the new text together with the end of the text it follows, in both packaged
// reviewer files and both copies installed to a temporary directory.

// TestCase 7e2448e3-fb83-4295-922f-ab01566e1351
func TestColdReviewerMarksATestWeaknessMaterialOnlyWhenNoOtherTestCatchesIt(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"or invariant the plan states. For a plan, a weakness of a planned test, such as an assertion that a wrong implementation would still pass, is material only when no other test the plan names or its project check runs would catch that implementation. Otherwise it is a build note that names that test. An expected RED that does not fail at the first commit for the stated reason is not such a weakness.",
	}, nil)
}

// TestCase f8ff9096-8637-4eb3-b2ca-bf4ce85cea59
func TestColdReviewerStatesTheWrongOutcomeOfEachMaterialPlanFinding(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"An expected RED that does not fail at the first commit for the stated reason is not such a weakness. State in each material plan finding the wrong outcome it leads to: the wrong behavior, the wrong published requirement content, the broken data, contract or trace, or what a person would wrongly be shown at a human gate. Record a finding for which you cannot state one as a note. For the three cases above that are material without a concrete harm case, the outcome is the false or differing rule, the rule without a mechanism, or the deferred work without a home.",
	}, nil)
}
