package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The sprint verification rule of Epic 444: an Epic runs its minimal tests,
// the full run is on the sprint pull request, and review and completion come
// after a passing full run. Phrases are compared with whitespace folded and
// Markdown code ticks removed.

func sprintRuleText(t *testing.T, path string) string {
	t.Helper()
	return buildRecordText(t, path)
}

func sprintProcessSection(t *testing.T, heading string) string {
	t.Helper()
	body, err := assets.ReadFile("assets/rdd/PROCESS.md")
	if err != nil {
		t.Errorf("the kit must embed PROCESS.md: %v", err)
		return ""
	}
	return processSection(t, string(body), heading)
}

func requireSprintPhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if !strings.Contains(text, foldBuildRecordText(phrase)) {
			t.Errorf("%s is missing %q", name, phrase)
		}
	}
}

func forbidSprintPhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if strings.Contains(text, foldBuildRecordText(phrase)) {
			t.Errorf("%s still contains %q", name, phrase)
		}
	}
}

// TestCase 61b527b8-f65f-42d9-add8-272d10556543
func TestProcessDefinesTheSprintTermsAndAnEpicsMinimalTests(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md §Minimal tests and the full run", sprintProcessSection(t, "### Minimal tests and the full run"), []string{
		"A sprint is one Epic or several built together on one pull request, with or without an autopilot grant. The sprint branch is that pull request's branch. The release branch is the branch the sprint branch starts from and merges into; it is usually the main branch and is not a Release record. The quick checks are the project's fast checks, such as build, static and architecture checks, and unit and domain tests; the project instructions name them. An Epic lands when `rdd-verify`'s run of its minimal tests passes; its commits are then pushed to the sprint branch (§Returning to planning). The sprint report is the agent's report to the human at the end of the sprint, before it asks for completion approval.",
		"An Epic's minimal tests are the TestCases of its changed criteria, at unit level first and at integration level where the criterion's boundary needs it; mutation testing of the code the Epic changed, where the project instructions name a mutation tool, with its results kept in the PR notes; the project's quick checks; and any check the plan names that the full run does not run. `rdd-verify` runs these and no more.",
		"The project instructions name the quick checks, the full-run command and where it runs, how many long runs one machine may run at a time, the sprint branch and the release branch, the mutation tool if the project uses one, and the known flakes.",
	})
}

// TestCase 84817f24-116b-4796-8771-e6457e747e85
func TestProcessBuildsAnEpicFromTheSprintBranchTipAndNoLongerPinsTheBase(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"Build an Epic from the tip of the sprint branch (§Minimal tests and the full run). When the Epic lands, push its commits to the sprint branch under the project's rules for pushing, and start the next Epic from the branch's tip. Until it lands, its commits are not pushed to the sprint branch. A fix is built from the tip and pushed, under the same rules, when its red-first test and the quick checks pass. An Epic whose commits are not yet on the sprint branch brings the new tip in before they are pushed and reruns the affected checks. Bringing the release branch into the sprint branch with no conflict in the Epic's files is not a return to planning. Rerun the affected deterministic checks after bringing it in. A conflict that changes the Epic's behavior or scope requires a return to planning.",
	})
	forbidSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"Pin the base",
		"do not stack an Epic on an open pull request",
		"Rebase once before implementation review",
	})
}

// TestCase 7aeecdcd-5115-4e4d-bf40-c1d12fa5aaf5
func TestProcessSaysWhereTheFullRunRunsAndThatCodingNeverWaits(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md §Minimal tests and the full run", sprintProcessSection(t, "### Minimal tests and the full run"), []string{
		"The full run is the project's full test suite, with mutation testing in full where the project uses it. It runs on the sprint pull request, as its CI or as the project's full-run command, during the sprint as well as at its end. Coding never waits for a full run. The full run's result and a link to it go in the pull request description and the sprint report. The platform records criterion outcomes only: a full-run result with no TestCase traced to a criterion is not recorded and blocks no gate.",
	})
}

// TestCase 476b9e44-a239-4cbc-ac88-2aa63472feb3
func TestProcessSaysHowAFullRunFailureIsHandled(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md §Minimal tests and the full run", sprintProcessSection(t, "### Minimal tests and the full run"), []string{
		"A full-run failure goes to `rdd-triage`. A failure the project instructions list as a known flake is rerun alone three times. It is a flake only when all three pass, and it is listed in the sprint report. A confirmed flake does not make the full run pass: rerun the failed part of the full run until it passes, or the whole run where it cannot be rerun in parts. The full run has passed when every part of it has passed on the same commit. Every other failure is a defect. A defect that a sprint Epic caused is fixed red-first under that Epic, on the same pull request, while that Epic is not DONE. A defect that was already on the release branch also fails on the tip of the release branch and belongs to no approved Epic. The agent fixes a defect that was already on the release branch, and a defect that a DONE Epic caused, red-first as a small change (§Small changes) on the same pull request, without waiting for the human, and lists it in the sprint report. This rule authorizes that small change in place of a user's request and of the fresh approval §Work scope asks for. With or without a grant, the stops of §Autopilot apply to it; under a grant, so do the grant's excluded areas. The human still decides the small change's completion (§Small changes). A fix that changes what a published requirement or its criteria state, or that is not narrow, is its own Epic, with its own plan and approval; for a DONE Epic that is its follow-up Epic.",
	})
	requireSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"| A full-run failure | §Minimal tests and the full run |",
	})
}

// TestCase f575b7ba-12dd-47b4-b772-4c76e17f0739
func TestProcessPlacesReviewAndCompletionAfterAPassingFullRun(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md §Minimal tests and the full run", sprintProcessSection(t, "### Minimal tests and the full run"), []string{
		"Request the independent implementation review after the full run passes, because a criterion outcome that changes after the review outdates it. The human approves an Epic's completion after a full run that contains the Epic has passed. This is a rule for the agent and the approver; no gate checks it. The human merges the pull request after its full run has passed; how it is merged is the project's choice. An Epic that changes no code or tests has no full run to wait for.",
	})
}

// TestCase 7c1c3148-356c-4f42-aa8e-ed1c80db500b
func TestProcessLoopInnerLoopAndPhaseTableNameTheMinimalTestsAndTheFullRun(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"-> RUN EXPECTED FAILING ASSERTIONS -> IMPLEMENT -> RUN FOCUSED AND QUICK CHECKS\n-> RECORD CURRENT CRITERION RESULTS -> CLEANUP -> VERIFY WITH THE EPIC'S MINIMAL TESTS\n-> LAND ON THE SPRINT BRANCH -> FULL RUN ON THE SPRINT PULL REQUEST\n-> FIXES ON THE SAME PULL REQUEST -> PASSING FULL RUN\n-> RECORDED INDEPENDENT IMPLEMENTATION REVIEW -> IN_REVIEW",
		"Run the Epic's minimal tests after cleanup; the full run covers the regression suites, including regression checks a plan names. A check a plan names that the full run does not run is one of the Epic's minimal tests.",
		"Before completion review, record the actual outcomes of the Epic's criteria and, unless the Epic changes no code or tests, wait for a passing full run of the sprint pull request.",
		"Implement the change and run the focused checks. Epic work also runs the project's quick checks; a small change runs its proportional regression checks.",
		"Human-approved content atomically published/versioned/audited after a passing full run, ownership released, or incomplete work reported",
	})
	forbidSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"Run project-required checks and proportional regression suites after cleanup.",
		"Before completion review, run the required checks and record their actual outcomes.",
		"run the focused and proportional regression checks",
	})
}

// TestCase 3089ae60-07f6-4e1a-b91e-6a9cd0b530ad
func TestProcessSaysAnAutopilotSprintIsOnePullRequest(t *testing.T) {
	requireSprintPhrases(t, "PROCESS.md", sprintRuleText(t, "assets/rdd/PROCESS.md"), []string{
		"An autopilot sprint is one pull request. Each Epic lands on the sprint branch when its minimal tests pass, the full run runs during the sprint, and the pull request description has one section for each Epic: its record, criteria, minimal-test evidence and changed files. An instruction to verify at the end names the full run. The sprint report lists each full run with its result and link, and each fix.",
	})
}

// TestCase 0677d7ef-f675-4975-bfe1-6423e955a280
func TestVerifyRunsTheEpicsMinimalTestsAndNotTheFullSuite(t *testing.T) {
	requireSprintPhrases(t, "rdd-verify", sprintRuleText(t, "assets/rdd/skills/rdd-verify/SKILL.md"), []string{
		"description: Verify implementation under an approved Epic plan with the Epic's minimal tests and current criterion evidence.",
		"Run the Epic's minimal tests after cleanup (`PROCESS.md` §Minimal tests and the full run): the TestCases of the changed criteria, mutation testing of the changed code where the project instructions name a mutation tool, the project's quick checks, and any check the plan names that the full run does not run. Do not run the full suite here: it runs on the sprint pull request.",
	})
	forbidSprintPhrases(t, "rdd-verify", sprintRuleText(t, "assets/rdd/skills/rdd-verify/SKILL.md"), []string{
		"proportional boundary and regression checks",
	})
}

// TestCase ca599c09-de7d-4c39-83a9-b804de0642d3
func TestBuildStartsFromTheSprintBranchTipAndDeliverPushesTheLandedEpic(t *testing.T) {
	requireSprintPhrases(t, "rdd-build", sprintRuleText(t, "assets/rdd/skills/rdd-build/SKILL.md"), []string{
		"description: Implement behavior within a human-approved Epic plan, using red-first development, focused checks and the project's quick checks.",
		"Implement the smallest passing change. Run focused checks and the project's quick checks.",
		"Build from the tip of the sprint branch, and do not push the Epic's commits to it before the Epic lands (`PROCESS.md` §Returning to planning).",
	})
	forbidSprintPhrases(t, "rdd-build", sprintRuleText(t, "assets/rdd/skills/rdd-build/SKILL.md"), []string{
		"Build on the main revision the Epic started from and rebase once",
		"proportional boundary and regression checks",
	})
	requireSprintPhrases(t, "rdd-deliver", sprintRuleText(t, "assets/rdd/skills/rdd-deliver/SKILL.md"), []string{
		"5. Implement red-first with `rdd-build`, then run `rdd-verify` for the Epic's minimal tests. When they pass, push the Epic's commits to the sprint branch and open the sprint pull request if it is not open, both under the project's rules for pushing, and, when the Epic changes code or tests, start or queue the full run on it. Never wait idle for a full run: continue with the next approved Epic or other approved work. Send a full-run failure to `rdd-triage`.",
		"6. After the full run has passed, or at once when the Epic changes no code or tests, use `rdd-completion-review` for independent code review, trace audit and separate human completion approval.",
	})
	forbidSprintPhrases(t, "rdd-deliver", sprintRuleText(t, "assets/rdd/skills/rdd-deliver/SKILL.md"), []string{
		"required focused, boundary and user-flow evidence",
	})
}

// TestCase 32eac4b9-78df-4279-add5-b3beef7e9436
func TestAutopilotUsesOnePullRequestAndRunsTheFullRunDuringTheSprint(t *testing.T) {
	requireSprintPhrases(t, "rdd-autopilot", sprintRuleText(t, "assets/rdd/skills/rdd-autopilot/SKILL.md"), []string{
		"Follow `rdd-build` for every slice and `rdd-verify` for each Epic, not for each slice.",
		"then focused checks and the project's quick checks; the Epic's minimal tests run once, after its cleanup.",
		"4. Use one pull request for the sprint. Build each Epic from the tip of the sprint branch; when its minimal tests pass, push it to the sprint branch under the project's rules for pushing and start the next Epic from the tip. Start or queue the full run during the sprint, not only at closeout, and never wait idle for it. Give the pull request description one section for each Epic: its record, criteria, minimal-test evidence and changed files. Do not skip project commit checks.",
		"5. At closeout, after a full run of the sprint pull request has passed, or at once when no Epic of the sprint changes code or tests, finish evidence collection,",
		"List each full run with its result and link, and each fix, in the sprint report.",
	})
	forbidSprintPhrases(t, "rdd-autopilot", sprintRuleText(t, "assets/rdd/skills/rdd-autopilot/SKILL.md"), []string{
		"reserve required full verification and hardening for closeout",
		"Follow `rdd-build` and `rdd-verify` for every slice",
		"focused and proportional regression checks after cleanup",
	})
}

// TestCase a22ea814-c602-4837-ad0d-6d1ff1469d56
func TestTriageHandlesAFullRunFailure(t *testing.T) {
	requireSprintPhrases(t, "rdd-triage", sprintRuleText(t, "assets/rdd/skills/rdd-triage/SKILL.md"), []string{
		"For a full-run failure, keep the link to its output. If the project instructions list it as a known flake, rerun it alone three times; when all three pass, list it in the sprint report and rerun the failed part of the full run, or the whole run where it cannot be rerun in parts. The full run has not passed until that rerun passes. Otherwise the failure is a defect. Fix one that a sprint Epic caused red-first under that Epic, unless that Epic is DONE. For any other defect, check which approved Epic owns the failing behavior before you fix it. When an approved Epic that is not DONE owns it, the fix belongs to that Epic: report it as a blocker with that Epic as its owner, and do not fix it as a small change. When a DONE Epic caused it, or it also fails on the tip of the release branch and no approved Epic owns it, fix it red-first as a small change (`PROCESS.md` §Small changes) on the same pull request, and list it in the sprint report. Do not wait for the human, unless the fix needs a stop of `PROCESS.md` §Autopilot or, under a grant, is in an area the grant excludes. A fix that changes what a published requirement or its criteria state, or that is not narrow, is its own Epic. An autopilot grant does not widen an Epic's scope to cover it.",
	})
}

// TestCase ecc33e70-4d6c-42ce-9c01-7ea8a3b5f960
func TestCompletionReviewShowsTheFullRunAndApprovesEpicsOneByOne(t *testing.T) {
	requireSprintPhrases(t, "rdd-completion-review", sprintRuleText(t, "assets/rdd/skills/rdd-completion-review/SKILL.md"), []string{
		"Show the full run's result and link, and each fix made after a full-run failure, beside the Epic's proof, and say so when the full run has not passed. The human's approval waits for a passing full run that contains the Epic, unless the Epic changes no code or tests; no gate checks it.",
		"Epics that are not a round of one Initiative are approved one by one.",
	})
}

// TestCase 07c3f709-4bb6-4094-a411-7117fba943e4
func TestInstructionFilesAndCliReferenceNameTheVerificationRoles(t *testing.T) {
	t.Run("rdd/AGENTS.md", func(t *testing.T) {
		text := sprintRuleText(t, "assets/rdd/AGENTS.md")
		requireSprintPhrases(t, "rdd/AGENTS.md", text, []string{
			"For verification they name the quick checks, the full-run command and where it runs, how many long runs one machine may run at a time, the sprint branch and the release branch, the mutation tool if the project uses one, and the known flakes.",
			"run the focused checks, then an Epic's minimal tests or a small change's proportional regression checks, and report commands and results.",
		})
		forbidSprintPhrases(t, "rdd/AGENTS.md", text, []string{
			"run the focused and proportional regression checks",
		})
	})
	t.Run("agents-block.md", func(t *testing.T) {
		text := sprintRuleText(t, "assets/agents-block.md")
		requireSprintPhrases(t, "agents-block.md", text, []string{
			"`rdd-autopilot` — a sprint under a human's autopilot grant: product questions asked without waiting, process refusals cleared or logged, one pull request for the sprint; implementation review and the human's completion approvals after its full run passes;",
		})
		forbidSprintPhrases(t, "agents-block.md", text, []string{
			"one review and one completion page at sprint end",
		})
	})
	t.Run("cmd/reference-loop.md", func(t *testing.T) {
		loop, err := os.ReadFile(filepath.Join("..", "..", "cmd", "reference-loop.md"))
		if err != nil {
			t.Fatalf("read cmd/reference-loop.md: %v", err)
		}
		verify, review := "", ""
		for _, line := range strings.Split(string(loop), "\n") {
			if strings.HasPrefix(line, "| Verify |") {
				verify = foldBuildRecordText(line)
			}
			if strings.HasPrefix(line, "| Review, publish and complete |") {
				review = foldBuildRecordText(line)
			}
		}
		requireSprintPhrases(t, "the Verify row", verify, []string{
			"Run the Epic's minimal tests; record actual criterion outcomes and read back `evidence latest`. The full run is the sprint pull request's CI or the project's full-run command. Builds and regressions are development checks.",
		})
		forbidSprintPhrases(t, "the Verify row", verify, []string{
			"Run proportional checks and user scenarios",
		})
		if want := foldBuildRecordText("| Review, publish and complete | `rdd-completion-review` | wait for a passing full run of the sprint pull request, then finish evidence before independent code review;"); !strings.HasPrefix(review, want) {
			t.Errorf("the Review, publish and complete row does not begin %q: %.200s", want, review)
		}
	})
	t.Run("cli-guide/cli.md", func(t *testing.T) {
		text := sprintRuleText(t, "assets/cli-guide/cli.md")
		requireSprintPhrases(t, "cli-guide/cli.md", text, []string{
			"After implementation, run the focused checks and the Epic's minimal tests and record",
		})
		forbidSprintPhrases(t, "cli-guide/cli.md", text, []string{
			"run focused and proportional checks",
		})
	})
	t.Run("docs/cli.md", func(t *testing.T) {
		// The guide's source lies outside the Go module, so it is absent from an exported CLI repository.
		body, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "docs", "cli.md"))
		if os.IsNotExist(err) {
			t.Skip("docs/cli.md is outside this checkout")
		}
		if err != nil {
			t.Fatalf("read docs/cli.md: %v", err)
		}
		text := foldBuildRecordText(string(body))
		requireSprintPhrases(t, "docs/cli.md", text, []string{
			"After implementation, run the focused checks and the Epic's minimal tests and record",
		})
		forbidSprintPhrases(t, "docs/cli.md", text, []string{
			"run focused and proportional checks",
		})
	})
}
