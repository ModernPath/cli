package kit

import (
	"strings"
	"testing"
)

// Epic 337 (D3), test case TC-D3-26 (0ecb90df-37dc-426f-af77-7566693a78f1):
// the delivery process text on the step for Prove and accept, differences,
// the domain expert's acceptance and their human stops. It verifies, through
// extra verifies traces, the coding agent's scenario of each journey:
// 3691e0c1 ("Resolve each difference by a fix or by accepting it by name" at
// 4) and d1468d75 ("Record the domain expert's acceptance on the Initiative's
// acceptance report" at 4). The kit check (CHECK-D3-3) runs exactly this
// function, so every phrase test is a subtest of it.
//
// The kit's embedded PROCESS.md and rdd-modernize SKILL.md are read with the
// whitespace folded and the code marks stripped, as D1's TestModernizeProcedure
// reads them, and each assertion is a whole sentence, never a bare word such as
// difference or acceptance, which PROCESS.md may already hold.
//
// On slice 1 neither text holds D3's sentences, so those subtests fail on
// their assertions. The subtests that D1's sentences stay and that the
// procedure names no modernpath command hold on the slice and guard D1's and
// D5's text beside D1's own test.
func TestModernizeDifferencesAndAcceptanceText(t *testing.T) {
	plain := func(path string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(speedRuleText(t, path), "`", "")), " ")
	}
	requireSentences := func(t *testing.T, where, text string, sentences []struct{ name, text string }) {
		t.Helper()
		for _, sentence := range sentences {
			t.Run(sentence.name, func(t *testing.T) {
				if !strings.Contains(text, sentence.text) {
					t.Errorf("%s is missing the sentence %q", where, sentence.text)
				}
			})
		}
	}

	process := plain("assets/rdd/PROCESS.md")
	requireSentences(t, "PROCESS.md", process, []struct{ name, text string }{
		{"D3 0ecb90df PROCESS.md: the agent records a difference with modernpath backlog create",
			"Record a difference with modernpath backlog create as a gap of kind difference that names the kept or changed legacy requirement it concerns, what changed and the evidence."},
		{"D3 0ecb90df PROCESS.md: a person accepts a difference by name and the agent never does",
			"A person on the team accepts a difference by name on the Initiative's Proof tab, with their own words, and the agent never does."},
		{"D3 0ecb90df PROCESS.md: a difference that changes stored values is fixed as a defect",
			"A difference that changes stored values or fails a write is fixed as a defect, never accepted."},
		{"D3 0ecb90df PROCESS.md: the two forms of closing a difference",
			"Close a difference only by the done Epic that fixed it, with CLOSED by and the Epic's number, or by the merged pull request that fixed it, with CLOSED by PR #<number> or the pull request's address."},
		{"D3 0ecb90df PROCESS.md: a mask added after a replay's first failing run cites an accepted difference",
			"A mask added to a replay after its first failing run cites an accepted difference by its reference."},
		{"D3 0ecb90df PROCESS.md: roadmap proof lists the differences with their references",
			"modernpath roadmap proof lists the Initiative's differences with their references, its acceptance and what it waits for."},
		{"D3 0ecb90df PROCESS.md: the two human stops in Human approvals",
			"The domain expert's acceptance of an Initiative and a person's acceptance of a difference are human stops: no agent records either on its own."},
		{"D3 0ecb90df PROCESS.md: the domain expert's results and sign-off only on their word",
			"An agent records the domain expert's results and sign-off only on their word, with a USER: reference to their decision as the source and their words as the note."},
		{"D3 0ecb90df PROCESS.md: the small-change lane's forms of closing a difference by a pull request",
			"A difference closed by a merged pull request is written CLOSED by PR #<number> or with the pull request's address."},
		{"D3 0ecb90df PROCESS.md: the business owner's stop stays (guard)",
			"The business owner's agreement of the Now plan, on the Roadmap, is the human stop for a selection, which no agent and no command records."},
	})

	skill := plain("assets/rdd/skills/rdd-modernize/SKILL.md")
	requireSentences(t, "rdd-modernize/SKILL.md", skill, []struct{ name, text string }{
		// Differences.
		{"D3 0ecb90df procedure: record a difference with the backlog command",
			"Record a difference with the backlog command, as a gap of kind difference that names the kept or changed legacy requirement, what changed and the evidence."},
		{"D3 0ecb90df procedure: never accept a difference",
			"Never accept a difference: a person on the team accepts it by name on the Initiative's Proof tab, in their own words, and sets its classification and whether it changes stored values there."},
		{"D3 0ecb90df procedure: close a difference by the done Epic or the merged pull request",
			"Close a difference only by the done Epic that fixed it or by the merged pull request that fixed it, written PR #<number> or its address."},
		{"D3 0ecb90df procedure: a mask cites an accepted difference by its reference",
			"In a mask added to a replay after its first failing run, cite an accepted difference by its reference, read in the Initiative's proof."},
		// Prove and accept: the merge commit, the run of the merged code and the proof.
		{"D3 0ecb90df procedure: record the merge commit with the merge-commit command",
			"Record on the Epic the merge commit of the last pull request that carried the change, with the merge-commit command; recording it again replaces it."},
		{"D3 0ecb90df procedure: run every test of the requirements the Epic took or replayed on the merged code",
			"Run every test of the criteria of each kept or changed requirement the Epic took or replayed on the merged code, the replay and its break test among them."},
		{"D3 0ecb90df procedure: capture the run with the capture command from a full-depth checkout under the Release",
			"Capture their outcomes under the Release with the capture command, from a folder bound to the new system, in a full-depth checkout at the merge commit or at a commit that contains it, in runs of at most 50 criteria and 500 tests, all the tests of one requirement in the same run."},
		{"D3 0ecb90df procedure: read the proof with the proof command",
			"Read the Initiative's proof with the proof command."},
		// D2's rule, word for word as docs/features/roadmap.md gives it.
		{"D3 0ecb90df procedure: D2's rule of which run counts",
			"Only a run of the modernized system captured at the commit that merged the change, or at a commit whose verified chain of commits reaches it, counts, and only when it holds a passing result for every test of the requirement's criteria; the newest run that counts decides."},
		// D2's reasons, word for word as Core.Roadmap.Proof gives them.
		{"D3 0ecb90df procedure: the reasons are given in the proof's words",
			"Until a run counts, the proof gives each row's reason in these words; each says what to do next:"},
		{"D3 0ecb90df procedure: reason waiting for a run captured after {Epic}",
			`"waiting for a run captured after {Epic}": the Epic is in progress or in review; finish it, and once it is done record its merge commit.`},
		{"D3 0ecb90df procedure: reason waiting for the merge commit to be recorded on {Epic}",
			`"waiting for the merge commit to be recorded on {Epic}": record the Epic's merge commit with the merge-commit command.`},
		{"D3 0ecb90df procedure: reason waiting for a run at the merge commit or later",
			`"waiting for a run at the merge commit or later": capture a run at the merge commit or at a commit that contains it.`},
		{"D3 0ecb90df procedure: reason the run's commit does not contain the merge commit",
			`"does not contain the change: this run's commit does not contain the merge commit of {Epic}": capture a run at a commit that contains the merge commit.`},
		{"D3 0ecb90df procedure: reason the commits sent with the run do not reach the merge commit",
			`"the commits sent with this run do not reach the merge commit of {Epic}; record a run at a commit that contains it, from a full-depth checkout": capture the run again from a full-depth checkout.`},
		{"D3 0ecb90df procedure: reason no chain of commits came with the run",
			`"no chain of commits came with this run; record it again from a full-depth checkout": capture the run again from a full-depth checkout that holds its commit.`},
		{"D3 0ecb90df procedure: reason no result in the latest run for some tests",
			`"no result in the latest run captured after the change for {tests}": capture a run that holds every test of the requirement's criteria.`},
		{"D3 0ecb90df procedure: reason its test fails",
			`"its test fails": fix the new system; when a replay fails because the new system behaves differently from the legacy system, record a difference.`},
		{"D3 0ecb90df procedure: reason no passing result",
			`"no passing result": the criterion has no test, or a test of it neither passed nor failed; give it a test and capture a run in which it passes.`},
		{"D3 0ecb90df procedure: reason no run captured after the change yet",
			`"no run captured after the change yet": deliver the Epic whose saved plan takes or reaches the requirement.`},
		{"D3 0ecb90df procedure: reason not drafted yet",
			`"not drafted yet": no Epic's saved plan takes or reaches the requirement yet.`},
		{"D3 0ecb90df procedure: reviewed only for a requirement the done Epic only took",
			"A requirement that the done Epic only took, without a replay, reads Reviewed only when an implementation review of the Epic names its criteria and no run decides or the run lacks a passing result."},
		// The acceptance steps.
		{"D3 0ecb90df procedure: refresh the specs and open the report with the acceptance commands",
			"Refresh the Initiative's specs and open its acceptance report with the acceptance commands."},
		{"D3 0ecb90df procedure: record each result only on the domain expert's word",
			"Record each result only on the domain expert's word, with notes for Fail, Blocked and Not run."},
		{"D3 0ecb90df procedure: sign off with a USER: reference as the source and the domain expert's words as the note",
			"Sign the report off only with a USER: reference to the domain expert's decision as the source and their words as the note."},
		{"D3 0ecb90df procedure: a Fail at the sign-off raises a difference",
			"A sign-off with a Fail on a requirement's spec raises a difference on each legacy requirement that the spec's requirement modernizes, with the notes as what changed; resolve it as the Differences section says."},
		{"D3 0ecb90df procedure: a new report with a new test after a spec moved",
			"When a spec moved after the sign-off, open a new report with a new test."},
		// The two human stops.
		{"D3 0ecb90df procedure: the two human stops",
			"Stop where the domain expert accepts the Initiative and where a person accepts a difference: the agent records neither on its own."},
		// D1's sentences stay (guards).
		{"D3 0ecb90df procedure: D1's sentence that the procedure names no command stays (guard)",
			"This procedure names no command: the commands are in the CLI reference."},
		{"D3 0ecb90df procedure: D1's sentence that Prove and accept has no skill of its own stays (guard)",
			"Prove and accept and Close the Release have no skill of their own."},
	})

	t.Run("D3 0ecb90df procedure: it holds no modernpath command (guard)", func(t *testing.T) {
		if strings.Contains(skill, "modernpath ") {
			t.Errorf("rdd-modernize/SKILL.md names a modernpath command; it names commands by what they do")
		}
	})
}

// Epic 459 (H2), SR "A person accepts a lower level of proof by name",
// criterion 4 (f0d4f249): the modernize skill tells the agent to record an
// acceptance of a lower level only on the person's explicit word, with the
// person's own words as the USER: source and the words, never from a
// template, and otherwise to ask the person to accept on the Proof tab. The
// skill is read as TestModernizeDifferencesAndAcceptanceText reads it, and the
// assertion is the whole sentence.
//
// At the Epic's base the skill holds no such sentence, so the assertion fails.
// That the skill still names no modernpath command holds at the base and
// guards the sentence's wording.

// h2LowerLevelSentence is the modernize skill's sentence on an acceptance of a
// lower level of proof.
const h2LowerLevelSentence = "Record an acceptance of a lower level of proof, with the accept-lower-level command, only on the person's explicit word, with the person's own words as the USER: source and as the words, never from a template; otherwise ask the person to accept the lower level on the Initiative's Proof tab."

// 05888915-1591-425a-8f26-beade614c446
func TestModernizeLowerLevelAcceptanceText(t *testing.T) {
	skill := strings.Join(strings.Fields(strings.ReplaceAll(speedRuleText(t, "assets/rdd/skills/rdd-modernize/SKILL.md"), "`", "")), " ")
	t.Run("H2 f0d4f249 procedure: record an acceptance of a lower level only on the person's explicit word", func(t *testing.T) {
		if !strings.Contains(skill, h2LowerLevelSentence) {
			t.Errorf("rdd-modernize/SKILL.md is missing the sentence %q", h2LowerLevelSentence)
		}
	})
	t.Run("H2 f0d4f249 procedure: it holds no modernpath command (guard)", func(t *testing.T) {
		if strings.Contains(skill, "modernpath ") {
			t.Errorf("rdd-modernize/SKILL.md names a modernpath command; it names commands by what they do")
		}
	})
}
