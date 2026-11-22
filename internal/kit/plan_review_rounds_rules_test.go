package kit

import "testing"

// Epic 457: at most three plan reviews in a planning cycle. The planner reads
// named reconnaissance areas before the first draft, the agent checks the
// review bundle before a review, and the reviewer marks details that the build
// settles as notes. Each test reads the packaged asset and the copies installed
// to a temporary directory, with whitespace folded.

// TestCase a72be8b7-22b1-4f6b-bbdb-cd65948b2067
func TestProcessNamesTheReconnaissanceAreas(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"Before the first draft, read the code in each reconnaissance area the change touches:",
		"contracts: API specifications, generated schemas, shared error and value types, and the rules of any contract check;",
		"stored data: the schema, grants and migrations, and how an existing database is upgraded;",
		"shared mechanisms the change passes through, such as authorization, idempotency, paging and deletion;",
		"concurrency: the existing locks and transactions and their order;",
		"composition and startup: host programs, configuration, route inventories and startup checks;",
		"existing behavior: the guards and tests that already exist, so that a regression check is not planned as an expected RED;",
		"process records: the existing traces of the files the change touches, and the content of the review bundle.",
	}, nil)
}

// TestCase 7e0273ee-05fe-4431-9b95-e83c4da8585a
func TestProcessSaysTheProjectNamesFilesAndThePlanHasAReconnaissanceSection(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"The project's instructions name the files of each area. When the project names none for an area, find the files yourself. The saved plan has a section `Reconnaissance` with one line for each area: what was read, with file and line, or why the area does not apply.",
	}, nil)
}

// TestCase db902d06-1a04-4585-9de7-8df34b745b71
func TestPlanSkillReadsTheReconnaissanceAreas(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Read each reconnaissance area of `PROCESS.md` §Planning and readiness that the change touches, in the files the project's instructions name for it, before you write the design, and write the plan's `Reconnaissance` section from what you read.",
	}, nil)
}

// TestCase e092e242-00b6-4de8-afd6-bc11b94c1c01
func TestColdReviewSkillChecksTheBundleForTitlesAndPlanningContexts(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"Before you hand the bundle to the reviewer, check that it holds the title of each TestCase the plan links and the planning contexts of the Epic's Release and Initiative. Supply a missing part from `modernpath test-cases get <uuid>` or `modernpath epics get <epic-id> --fields planning_contexts`, and tell the reviewer where it came from. Name a part you cannot supply to the reviewer once, and report the tooling gap through the project's named channel.",
	}, nil)
}

// TestCase 157c914b-695f-4583-ad09-dca847698c28
func TestColdReviewerSamplesTheReconnaissanceLines(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"Sample the lines of the plan's `Reconnaissance` section as you sample its file and line claims. A missing line for an area the change touches is a note.",
	}, nil)
}

// TestCase 618f0d1e-42b4-427f-8edb-952c5704c9ba
func TestColdReviewerNamesAnOmittedBundlePartOnce(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"Name a part the server did not put in the bundle, such as TestCase titles or planning contexts, once, under limitations, and raise no finding for its absence.",
	}, nil)
}

// TestCase c7c8e23b-e061-4d01-b767-baae3309f848
func TestColdReviewerListsTheDetailsThatAreNotes(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"Anything else is a note. When nothing above makes it material, these are notes, because the build's compiler and tests settle them: the wiring of a test fixture or test host, a test helper's signature, how tests are grouped into methods or files, a mistyped command or flag in the plan's tests or checks, and a combination of edge cases that contradicts no contract, requirement or invariant the plan states.",
	}, nil)
}

// TestCase 4df4815e-3c67-4110-885b-c67ee3f51de9
func TestProcessCapsAPlanningCycleAtThreePlanReviews(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"A planning cycle has at most three plan reviews, and the full review before approval is not due when the cycle already has three or more. After the third, obtain no further plan review unless the human chooses a revision with its review or asks for a review in their own words. Present the plan with its reviews and its open findings. Say when the full review before approval was not obtained because of the cap. The human decides: approve the plan as it stands, with the human's resolution recorded for each open material finding; request one more revision with its review; split the Epic; or reject the plan. When the latest plan review is out of date, say that approval needs a renewed review, which the human may ask for. In a round the cap is reached when any Epic in it has three or more.",
	}, nil)
}

// TestCase 5798d95b-ef03-40ae-b0b4-592c4972fc3a
func TestColdReviewSkillCountsThePlanReviews(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"Before you request a plan review, count the plan reviews of the planning cycle: the records in the bundle's `reviews` whose `phase` is `plan`. At three or more, request no review unless the human chose a revision with its review or asked for a review, and follow the cap of `PROCESS.md` §Planning and readiness.",
	}, nil)
}

// TestCase 0a5bdbd0-e78d-4617-b6d9-6db8cbaeae51
func TestEntryReviewSkillPresentsTheChoicesOfTheCap(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"When the planning cycle has three or more plan reviews, say so and present the choices `PROCESS.md` §Planning and readiness gives for the cap, instead of offering a further review.",
	}, nil)
}
