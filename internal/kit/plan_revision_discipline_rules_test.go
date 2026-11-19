package kit

import "testing"

// SR-4965: rdd-plan keeps a plan revision small, reads a handoff for the
// cases it leaves open and states a process rule in one place.

func planSkillText(t *testing.T) string {
	t.Helper()
	return buildRecordText(t, "assets/rdd/skills/rdd-plan/SKILL.md")
}

// TestCase 0cd88607-71f5-4cd0-bc98-aca6d8669dfd
func TestPlanKeepsARevisionToTheMaterialFindingsAndTheHumansRequest(t *testing.T) {
	requireBuildRecordPhrases(t, "rdd-plan", planSkillText(t), []string{
		"- Change only what the material findings and the human's request need. Fix no other note in the same revision: each further fix adds text for the next review to fault. Propose to the human only the notes that would change behavior or publish false requirement content, and leave the rest to the build or a follow-up record.",
	})
}

// TestCase 5236aa13-17a4-425c-bd61-73ea20b9228d
func TestPlanReadsAnApprovedHandoffForTheCasesItLeavesOpen(t *testing.T) {
	requireBuildRecordPhrases(t, "rdd-plan", planSkillText(t), []string{
		"An approved handoff or specification is a source, not an answer to every question. Read it for the cases it leaves open, such as what happens on a failure, who may start an action and what counts as done, and put those to the human in step 3.",
	})
}

// TestCase f98676ff-d5c9-4870-89e0-b72f18c83cb4
func TestPlanStatesARuleOfProcessTextInOnePlace(t *testing.T) {
	requireBuildRecordPhrases(t, "rdd-plan", planSkillText(t), []string{
		"When the change is process or prompt text, state each rule in one place and let the other files name that place. A rule that several files repeat must change in all of them at once, so the plan lists every file that repeats it.",
	})
}
