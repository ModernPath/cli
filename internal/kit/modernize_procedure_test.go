package kit

import (
	"strings"
	"testing"
)

// Epic 335 (D1), test case 25690857 ("D1 · the procedure rdd-modernize and
// its capture steps: phrase tests"), the kit check's test case. It verifies,
// through the procedure's sentences, the three scenarios of "Record the
// legacy system's behaviour and link each replay test to its capture"
// (ceb2dee2, c437b9a2 and 32f35836, as Epic 390's plan revised scenarios 1 and
// 3); no criterion has it as its own test.
//
// The kit check runs exactly this function, so every phrase test of the
// procedure is a subtest of it (contract §11). The skill the kit embeds,
// assets/rdd/skills/rdd-modernize/SKILL.md, holds each sentence of the five
// stages, the skill each stage uses, the stop at the business owner's
// agreement and the capture steps; the agents block names the skill with its
// stages and capture steps. Each text is read with the whitespace folded and
// the code marks (backticks) stripped, so it may wrap or mark its words. None
// of D3's steps is asserted. Epic 390 (D5) rewrote the capture steps: the
// capture is recorded from a folder bound to the new system as an entry with
// no criterion, the sentence on the in-place Epic's relates trace went, and the
// closing paragraph keeps the replay's first failing run in the pull request's
// notes; the removed sentences are asserted absent.
//
// On the first slice the skill holds only its frontmatter, its heading and one
// sentence, and the agents block names the skill with a stub line, so every
// subtest but the first fails on its assertion. The first, a guard, holds on
// the slice: the kit's AGENTS.md names skills/rdd-modernize/SKILL.md.
func TestModernizeProcedure(t *testing.T) {
	plain := func(path string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(speedRuleText(t, path), "`", "")), " ")
	}

	t.Run("D1 25690857 the kit's AGENTS.md names the skill (guard)", func(t *testing.T) {
		if want := "skills/rdd-modernize/SKILL.md"; !strings.Contains(plain("assets/rdd/AGENTS.md"), want) {
			t.Errorf("the kit's AGENTS.md does not name %s", want)
		}
	})

	skill := plain("assets/rdd/skills/rdd-modernize/SKILL.md")
	for _, sentence := range []struct{ name, text string }{
		{"D1 25690857 the five stages in order",
			"A modernization goes through five stages in order: Get ready, Plan Initiatives, Deliver an Initiative, Prove and accept, and Close the Release."},
		{"D1 25690857 Get ready records the legacy requirements with rdd-reverse-engineer",
			"In Get ready, reverse engineering records the legacy requirements with rdd-reverse-engineer."},
		{"D1 25690857 Plan Initiatives has no skill of its own",
			"Plan Initiatives has no skill of its own."},
		{"D1 25690857 Deliver an Initiative starts with the capture steps and runs rdd-deliver",
			"In Deliver an Initiative, the capture steps below come first, and each Epic then runs the ordinary loop with rdd-deliver."},
		{"D1 25690857 Prove and accept and Close the Release have no skill of their own",
			"Prove and accept and Close the Release have no skill of their own."},
		{"D1 25690857 the stop at the business owner's agreement",
			"Stop where the business owner agrees the selection: the agent never agrees it."},
		{"D1 25690857 a capture only for a kept or changed legacy requirement",
			"Record a capture only for a kept or changed legacy requirement whose proof needs one."},
		{"D1 25690857 a capture is a recording of the legacy system's behaviour",
			"A capture is a recording of the legacy system's behaviour, not the source capture the process removed."},
		{"D1 25690857 the legacy runs and the captures at a commit that is not a merge commit",
			"Record the legacy runs and the captures at a commit that is not a merge commit, because the platform's history of the legacy repository holds no merge commit, so it can place no later capture or legacy change against one."},
		{"D1 25690857 the folder bound to the legacy system and its revision",
			"Work from a folder bound to the legacy system: at the reference revision, or, onto a new system, at a later legacy revision for one requirement fixed on the legacy system; in place, at the reference revision before anything changes."},
		{"D1 25690857 capture where the legacy system's own tests run",
			"Capture where the legacy system's own tests run, on their test data and with their stand-ins for outside systems, never on production data."},
		{"D1 25690857 one capture test case per requirement linked with relates",
			"Create one capture test case per requirement, trace it to its capture script and to the legacy files it exercises, and link it to the requirement with relates, never verifies."},
		{"D1 25690857 capture twice with masks",
			"Capture twice, naming the values that change on every run as masks before the second capture."},
		{"D5 25690857 the capture is recorded from a folder bound to the new system, under the Release",
			"Record the second capture as the capture test case's run with the capture command, from a folder bound to the new system, which in place is the same folder, under the Release; the command passes when the two captures match."},
		{"D5 25690857 the capture test case's entry names no criterion",
			"Give the capture test case's entry no criterion, because the platform takes such an entry only for a capture test case, and a capture test case that verifies a criterion stops being one."},
		{"D5 25690857 in place the chain is sent, onto a new system the capture is recorded without one",
			"In place the folder holds the run's commit, so the command also sends the chain of commits the platform checks and keeps with the run; onto a new system the capture is recorded without a chain, and the command says so."},
		{"D5 25690857 a moved replay and its break test run again",
			"When the capture runs again and the replay is linked to its newer passing result (a move), the platform clears the replay's current results, so run the replay and its break test again and record their results after the move."},
		{"D5 25690857 a replay keeps its break test's results when its link moves",
			"A replay keeps its break test's results when its link moves."},
		{"D1 25690857 the capture's procedure says what it ran on",
			"Write in the capture's procedure what it ran on: its starting data, its clock, the outside systems it stubbed with their recorded calls, and the class of the data, and where a stored capture lives when it stays outside the platform."},
		{"D1 25690857 no link to a capture that did not match",
			"Link no capture that did not match, but tell the person."},
		{"D5 25690857 the capture is read back in the requirement's recording",
			"Read the capture back in the requirement's recording before you go on."},
		{"D1 25690857 the replay and its break test traced to their files",
			"In the new system's repository, create the replay and its break test, and trace each to its files with relates: the replay to its test file, its stored capture, its masks and its data mapping."},
		{"D1 25690857 both linked to the capture before either runs",
			"Link both to the capture before either runs, so that the masks are named before the replay first fails."},
		{"D5 25690857 the replay's first failing run is kept in the pull request's notes and the passing outcomes become current results",
			"Keep the replay's first failing run in the pull request's notes, and record the passing outcomes of the replay and its break test with the evidence command as the current results of the criterion they verify: onto a new system the new requirement's criterion, in place the kept requirement's own."},
		{"D1 25690857 the procedure names no command",
			"This procedure names no command: the commands are in the CLI reference."},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(skill, sentence.text) {
				t.Errorf("rdd-modernize/SKILL.md is missing the sentence %q", sentence.text)
			}
		})
	}

	// Epic 390 (D5) removes these sentences: a plan keeps the traces it does not
	// remove, so nothing goes stale; the replay's runs are not the Epic's own RED
	// and GREEN runs; the capture is recorded from the new system's folder, not
	// from the checkout that holds the commit; and when a run of the new system
	// is captured is D3's step, not this procedure's.
	for _, removed := range []struct{ name, text string }{
		{"D5 25690857 the in-place Epic no longer carries the capture test case's relates trace in its plan",
			"carries its capture test case's relates trace in its plan"},
		{"D5 25690857 the replay's runs are no longer the Epic's RED and GREEN runs",
			"RED and GREEN runs"},
		{"D5 25690857 the capture is no longer recorded from the checkout that holds the commit",
			"Run the command in the checkout that holds the run's commit"},
		{"D5 25690857 the procedure does not say when a run of the new system is captured",
			"Record their runs with the capture command too"},
	} {
		t.Run(removed.name, func(t *testing.T) {
			if strings.Contains(skill, removed.text) {
				t.Errorf("rdd-modernize/SKILL.md still holds the removed sentence %q", removed.text)
			}
		})
	}

	t.Run("D1 25690857 the agents block names rdd-modernize with its stages and capture steps", func(t *testing.T) {
		want := "rdd-modernize — the stages of a modernization, and the capture steps that record how the legacy system behaves and link each replay test to its capture"
		if !strings.Contains(plain("assets/agents-block.md"), want) {
			t.Errorf("the agents block is missing %q", want)
		}
	})
}
