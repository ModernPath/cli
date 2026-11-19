package kit

import (
	"path/filepath"
	"strings"
	"testing"
)

// Epic 432: the planning loop in the kit. A later plan review reads the
// change, one more full review comes before approval after a change of plan
// content, the entry review checks a note before leaving it to the build and
// lists the changes made after the review, and the planner keeps the plan
// small and sourced. Each test reads the packaged asset and the copies
// installed to a temporary directory, with whitespace folded.

type planningLoopCopy struct{ name, text string }

func planningLoopInstall(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func planningLoopCopies(t *testing.T, root string, assetPaths []string, installed ...string) []planningLoopCopy {
	t.Helper()
	var copies []planningLoopCopy
	for _, asset := range assetPaths {
		copies = append(copies, planningLoopCopy{asset, speedRuleText(t, asset)})
	}
	for _, path := range installed {
		body := installedKitFile(t, root, filepath.FromSlash(path))
		copies = append(copies, planningLoopCopy{"installed " + path, kitWhitespaceNormalised(body)})
	}
	return copies
}

func planningLoopProcess(t *testing.T, root string) []planningLoopCopy {
	t.Helper()
	return planningLoopCopies(t, root, []string{"assets/rdd/PROCESS.md"}, ".modernpath/rdd/PROCESS.md")
}

func planningLoopSkill(t *testing.T, root, skill string) []planningLoopCopy {
	t.Helper()
	return planningLoopCopies(t, root,
		[]string{"assets/rdd/skills/" + skill + "/SKILL.md"},
		".modernpath/rdd/skills/"+skill+"/SKILL.md",
		".claude/skills/"+skill+"/SKILL.md")
}

// The reviewer agent has a Claude Code copy and a Codex copy.
func planningLoopReviewer(t *testing.T, root string) []planningLoopCopy {
	t.Helper()
	return planningLoopCopies(t, root,
		[]string{"assets/agents/rdd-cold-reviewer.md", "assets/agents-codex/rdd-cold-reviewer.toml"},
		".claude/agents/rdd-cold-reviewer.md",
		".codex/agents/rdd-cold-reviewer.toml")
}

func requirePlanningLoopText(t *testing.T, copies []planningLoopCopy, says, gone []string) {
	t.Helper()
	for _, c := range copies {
		for _, phrase := range says {
			if !strings.Contains(c.text, phrase) {
				t.Errorf("%s does not say %q", c.name, phrase)
			}
		}
		for _, phrase := range gone {
			if strings.Contains(c.text, phrase) {
				t.Errorf("%s still says %q", c.name, phrase)
			}
		}
	}
}

// TestCase a7e29240-bbc1-4bbf-8458-9e810fd157a6
func TestKitSaysWhatAFullReviewAndAReviewOfTheChangeRead(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"The first plan review of a planning cycle is a full review: it reads the whole plan. A later review in the same cycle is a review of the change, unless it is the full review before approval, the gate names no cause, or, in a round, it is the first plan review of any Epic in it. A review of the change reads each earlier finding with its recorded resolution, what the gate's `causes` name, and each criterion, requirement field, plan section and test title that uses a changed term, rule or record.",
		"For `plan_content`, `verification`, `decision`, `trace_removal` and `modernizes_trace` the reviewer reads the `changed` parts and ids and the `comment` of each plan save the plan history lists since the previous review; for a changed `body` it reads the sections the `comment` names, and the whole body when it names none. For `requirement_edit` the gate names no ids: read every requirement field and criterion of the plan again, against the stored content in the bundle's `review.baseline`. For `decision` also read the answers recorded since the previous review, for `planning_context`, `release` and `initiative` the planning contexts in the bundle, and for `stored_trace` it finds each id of `trace_ids` in the traces of the bundle's `review.baseline`. An id that is not there is a removed trace, or a trace of a test or of a retired requirement; the supplied trace lists show the last two without ids, and a removed trace is in neither the baseline nor the lists, so read the lists in full. When no plan save explains a cause, read in full the part of the plan the cause names.",
	}, nil)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"The agent handling the task says whether this is a full review or a review of the change, as `PROCESS.md` (installed as `.modernpath/rdd/PROCESS.md`) defines them. In a review of the change, read what it lists for each cause and apply the method below to that text.",
	}, nil)
}

// TestCase 37fc9878-7b0f-43fc-99d4-754e7e5da496
func TestKitSaysWhenTheFullReviewBeforeApprovalIsDue(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"A review passes when it has no material finding. The full review before approval is one more full review of a revised plan. It is due when the latest plan review is a review of the change, has no material finding and is current (the `readiness` of a bundle fetched after that review was recorded has no `plan_review_required` blocker), the planning cycle has only one full review, and the reason of a review of the change in this cycle names `plan_content` or `requirement_edit`. Obtain it before you present the plan, without a human decision between the two reviews, and present both. A review is a full review unless its reason starts with `Review of the change.` Count the plan reviews in the `reviews` of the same bundle; a return to planning starts a new planning cycle. In a round it is due for the round when it is due for any Epic in it, and a round review is a full review when it is the first plan review of any Epic in it.",
	}, nil)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"After recording a review of the change, the agent handling the task fetches the bundle again; a reviewer still uses only the bundle it was given. When the full review before approval is due (`PROCESS.md`), obtain it and hand both reviews to `rdd-entry-review`.",
	}, nil)
}

// TestCase 8a7ad61d-20d1-4891-97eb-4400d6118233
func TestSkillsNameTheFullReviewBeforeApprovalAsTheExceptionToOneReview(t *testing.T) {
	root := planningLoopInstall(t)
	gone := []string{
		"Obtain one independent plan review",
		"Hand off for one independent review",
		"Review a saved Epic plan once",
		"starts another pass, which returns to the human. Do not implement",
		"another plan review or revision. Follow",
		"A requested revision gets one new review, then returns to",
		"request another review before the human decides. Keep",
		"one new independent review and another human decision",
		"its single independent review",
		"the decision point after one independent review",
		"one new review before presenting it again",
	}
	for _, c := range []struct {
		skill string
		says  []string
	}{
		{"rdd-deliver", []string{
			"Obtain the plan review with `rdd-cold-review`, and the full review before approval when it is due; reuse a recorded review.",
			"Only a human-requested revision or further review starts another pass, which returns to the human; the one exception is the full review before approval (`PROCESS.md`).",
		}},
		{"rdd-start", []string{
			"Resuming a session does not authorize another plan review or revision, except the full review before approval when it is due.",
		}},
		{"rdd-plan", []string{
			"A requested revision gets one new review and, when it is due, the full review before approval, then returns to the human; do not start an autonomous revision-and-review loop.",
			"Hand off for independent review, then present the saved plan and findings to the human.",
		}},
		{"rdd-cold-review", []string{
			"description: Review a saved Epic plan in a context separate from authoring, then hand the plan and findings to the human for a decision.",
			"or request another review before the human decides, except the full review before approval",
			"A human-requested revision gets one new independent review, then the full review before approval when it is due, and another human decision.",
		}},
		{"rdd-entry-review", []string{
			"Present the saved Epic plan and its independent review for the human",
			"This is the decision point after the plan's review, or after a review of the change and the full review before approval, not another review or repair pass.",
			"for the specified changes, one new review and, when it is due, the full review before approval, before presenting it again",
			"Do not silently edit the plan or commission another review, except the full review before approval.",
			"If the full review before approval is due and not recorded, return to `rdd-cold-review` before you present the plan.",
		}},
	} {
		requirePlanningLoopText(t, planningLoopSkill(t, root, c.skill), c.says, gone)
	}
}

// TestCase 652fe2cd-720a-4349-8a43-b0061f040d51
func TestKitSaysHowAReviewOfTheChangeFindsTheChange(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Give each plan save after a review a `--reason` that names the finding or decision each change answers. The plan history records which parts, requirements and criteria the save changed; the reason says why.",
	}, nil)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"For a review of the change, also supply the output of `modernpath epics plan history <epic-id> --limit 50 --json`, fetched after the last change, and the next page with `--offset` while `has_more` is true. The causes are in the bundle's `readiness`, in the blocker `plan_review_required`. When the causes name `stored_trace`, also supply `modernpath traces list --entity-type <kind> --entity-id <id> --status all --limit 200 --json`, and the next page with `--cursor` while `next_cursor` is not null, for each requirement, criterion and test of the plan and each requirement it retires with its criteria, and tell the reviewer which lists could not be read. For a round, supply the history of each Epic in it.",
	}, nil)
}

// TestCase f52ed879-eb27-4329-b12e-112a81d6dc2a
func TestKitMarksAReviewOfTheChangeInItsReason(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"In a review of the change, still list every criterion and obligation, because the server refuses a review that leaves one out. Start the reason with `Review of the change. Causes: ` and the causes you read, separated by commas. Then name in the reason the criteria you did not read again.",
	}, nil)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-cold-review"), []string{
		"Record the reviewer's reason with its first words unchanged. A review whose reason does not start with `Review of the change.` counts as a full review.",
	}, nil)
}

// TestCase f52ed879-eb27-4329-b12e-112a81d6dc2a
func TestReviewerKeepsTheMarkerForAReviewOfTheChangeAndNamesUnreadTraceIds(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopReviewer(t, root), []string{
		"Start the reason of a full review or of an implementation review in any other way.",
		"Name under limitations each trace id that may be in a trace list that could not be read.",
	}, nil)
}

// TestCase 66ece9ae-38ee-4143-b0f9-e5620edf06fe
func TestKitDoesNotOfferForTheBuildANoteThatWouldChangeBehavior(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"Before you offer to leave a note to the build, check whether acting on it would change behavior, requirement content the plan would publish, or a file that other work reads or generates. If it would, do not offer it for the build. Look for the answer in a recorded human decision, then in a published requirement, then in the legacy system, and give the human the plan correction with that source. Ask the human the question only when no source answers it.",
	}, nil)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"Do not offer to leave to the build a note that would change behavior, requirement content the plan would publish, or a file that other work reads or generates (`rdd-entry-review`).",
	}, nil)
}

// TestCase 2a9ee09e-2874-488a-b9f2-77e249d4f7e5
func TestEntryReviewGivesCheckedWordingForNotesAndNoRuleThatNeedsAReviewWithNoNotes(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"Give each requirement-text note with the wording you propose, checked in the code as you check a correction.",
		"Do not propose an approval rule that needs a review with no notes.",
	}, nil)
}

// TestCase 842d4e68-82bb-4484-8bc3-698d5d6e7078
func TestEntryReviewListsTheChangesRecordedAfterTheCurrentPlanReview(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-entry-review"), []string{
		"Before you ask for approval, read `modernpath epics plan history <epic-id> --limit 50 --json` and list for the human each item recorded after the current plan review: for a plan save its `changed` parts and its `comment`, for an edit of the Epic its `fields`; read the next page with `--offset` while `has_more` is true. When the review is current, these changes left it current, so no reviewer has read them. Say so when there are none.",
	}, nil)
}

// TestCase 905545c8-c30c-4507-99de-3473909a4774
func TestPlanSkillAsksForASplitDecisionWhenThePlanIsLong(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Before the first review, count the characters of the saved plan body and of the criteria the plan adds or changes. Above about 25,000 in total, ask the human to split the Epic or keep it whole, as a declared decision. More than 15 criteria is a reason to consider a split, not a limit. Count again after a revision, and tell the human the size when it is above about 25,000.",
	}, []string{
		"If the plan grows beyond about 15 criteria, offer the human a split.",
	})
}

// TestCase fb3b2f70-a2a7-4928-b21e-b034df24cb9d
func TestPlanSkillKeepsReviewMarkersOutOfRequirementFieldsAndCriteria(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Write a fix into the text it corrects. Keep review markers, round numbers and finding ids out of requirement fields and criteria as well as the plan body; the review record holds them.",
	}, nil)
}

// TestCase 99d44d98-06ab-4b5a-b24d-562ebd7fcd97
func TestPlanSkillQuotesEachHumanDecisionInTheHumansOwnWords(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Write each human decision into the plan in the human's own words, in quotation marks. Put your reading of it under the plan's assumptions, where the human can correct it.",
	}, nil)
}

// TestCase 0659ba2d-dee4-4cbc-bb23-d8df29ca44d8
func TestPlanSkillAsksThreeMoreQuestionsAndDefinesEachTermOnce(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"data that is stale or partly written when the change runs",
		"the platforms and CI images the change runs on, and how its tests stay isolated",
		"who reads or generates each output and document the change alters",
		"Define each term the criteria rely on once, and use only the term elsewhere.",
	}, nil)
	requirePlanningLoopText(t, planningLoopProcess(t, root), []string{
		"`rdd-plan` holds the full list of question topics.",
	}, nil)
}

// TestCase bb59068c-8676-4398-9772-7f433430d32f
func TestPlanSkillLooksForAnAnswerInASourceBeforeAskingTheHuman(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"Before you put a product question to the human, look for its answer in a recorded human decision, then in a published requirement, then in the legacy system. When a source answers it, write the answer into the plan with the source: the decision or requirement, or the file and line. Ask the human only what no source answers.",
	}, nil)
}

// TestCase b2810a14-7c4c-4976-8e8b-be2787e8503f
func TestPlanSkillRepeatsReconnaissanceWhenAnAnswerAddsAMechanism(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"When a human answer adds a mechanism the plan did not have, such as a record, a command, a time limit or a file, do the reconnaissance for that part again before you write it into the plan.",
	}, nil)
}

// TestCase 6de551df-7be1-44d0-8232-0b51d8086d9a
func TestPlanSkillSaysTheReadOnlyCheckReadsTheTextToBeSaved(t *testing.T) {
	root := planningLoopInstall(t)
	requirePlanningLoopText(t, planningLoopSkill(t, root, "rdd-plan"), []string{
		"The read-only check reads the text you will save. Check a correction you make after the check once more, reading only the corrected text.",
	}, nil)
}
