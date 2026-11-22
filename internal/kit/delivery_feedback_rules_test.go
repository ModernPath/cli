package kit

import "testing"

// Epic 465: four rules from delivery feedback. A review of the change reads
// the changes that reached the branch, the build breaks the finished code to
// see each changed criterion's test fail, an empty delegated result is a
// failed run, and a decision of the planning context is applied. Each test
// asserts the new text together with the end of the text it follows, in the
// packaged asset and the copies installed to a temporary directory.

// TestCase eb40844a-5ed3-4a86-b93d-e4e5680442f1
func TestProcessSaysAReviewOfTheChangeReadsWhatReachedTheBranch(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"and should now say what changed. A review of the change also reads the changes that, after the previous review was recorded, reached the branch of the checkout the reviewer reads, in the files the plan's traces and its `Reconnaissance` section name; a commit made before the review that reached the branch after it counts. A plan statement that such a change makes false is a finding, although the plan text did not change.",
	}, nil)
}

// TestCase 453a03bd-8a02-45a8-a2e6-fa4d383c2ca7
func TestColdReviewSkillSuppliesTheChangesThatReachedTheBranch(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"For a round, supply the history of each Epic in it. For a review of the change, also supply the changes that, after the previous review was recorded, reached the branch of the checkout the reviewer reads, with their content, in the files the plan's traces and its `Reconnaissance` section name; a commit made before the review that reached the branch after it counts. For a round, supply them for the files of each Epic in it. Say so when nothing landed.",
	}, nil)
}

// TestCase f47338de-9cc4-4d2a-a03f-d0714d6b5344
func TestEntryReviewSkillListsTheChangesThatReachedTheBranch(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"Say so when there are none. List also the changes that, after the current plan review was recorded, reached the branch of the checkout that review read, in the files the plan's traces and its `Reconnaissance` section name; a commit made before the review that reached the branch after it counts. No reviewer has read the plan against those changes. Say so when nothing landed.",
	}, nil)
}

// TestCase 8370e356-a2b8-483d-9639-31734c3d7dd3
func TestBuildSkillBreaksTheFinishedCodeForEachChangedCriterion(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-build"), []string{
		"do not upload procedures or raw reports. In ordinary implementation, for each changed criterion, after its test passes, make a change by hand to the implemented code that breaks the behavior the criterion states, confirm that the test fails, remove the change before you commit, and run the test again; do not record the run with the change. A test that still passes with the change does not prove the criterion: strengthen it under its TestCase, or raise it with the approver. Write the assertion and the change in the PR notes, and in what a delegated build returns beside the build handoff; the agent handling the task copies it to the PR notes. Where the project instructions name a mutation tool, `rdd-verify` does this with the tool instead.",
	}, nil)
}

// TestCase 1efe9267-05c2-4c6d-8486-c902fd7cbb22
func TestVerifySkillReadsEachChangedCriterionAgainstItsTests(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-verify"), []string{
		"determine proof applicability. For each changed criterion, read its statement against its tests, also after a fix that changed a test. A test that proves less than the criterion states is a gap: close it under the TestCases of the approved plan before the independent implementation review, or raise it with the approver.",
	}, nil)
}

// TestCase 820e79de-bf08-4059-b656-9591f0fc9091
func TestProcessSaysAnEmptyDelegatedResultHasFailed(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"rewriting or splitting the refused action. A delegated pass that returns an empty or missing result has failed. Record nothing from it, say that it failed, and run it again once the cause is removed. Before a build runs again, check what it left in the working tree. A failed run is not a review, and a refusal is not a failed run.",
	}, nil)
}

// TestCase aeb09b29-b1f3-4487-a826-47a7b0a2d00a
func TestPlanSkillAppliesADecisionOfThePlanningContext(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Ask the human only what no source answers. A decision in the planning context of the Epic's Release or Initiative counts as a recorded human decision here, whether or not the context is agreed or locked. Apply it, name it in the plan as the source, and do not ask the human what it decides. When a Release decision and an Initiative decision disagree, ask.",
	}, nil)
}

// TestCase af7395f0-5778-4346-890e-79530cf6402f
func TestEntryReviewSkillCountsADecisionOfThePlanningContext(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"Ask the human the question only when no source answers it. A decision of the planning context counts as a recorded human decision (`rdd-plan` step 3).",
	}, nil)
}
