package kit

import (
	"bytes"
	"strings"
	"testing"
)

// Epic 329 (A6), test case bd44c890: the delivery process text the CLI
// installs says how a planning context locks and is opened for change, and
// how an assumed default is recorded, routed and confirmed.
//
// Criterion 928a5d6c: PROCESS.md says a planning context locks when the
// business owner agrees it and at the first approval of an Epic that reads
// it, that it changes only through Open for change, by a team member with a
// reason, that the next agreement locks it again, and that only the approver
// of an approved Epic that reads it returns the Epic to planning, and only if
// the change needs a new plan. rdd-build reads the changes since approval with
// `epics gates presentation` before building and tells the approver about one
// the plan did not know.
//
// Criterion a7fba45b: PROCESS.md and rdd-autopilot say an assumed default is a
// backlog record of kind assumption with its question, routed once to the Epic
// being built; that the server refuses any later change of its disposition
// without a USER: source; and that the agent writes that source only from the
// person's own words, never from a template. rdd-triage routes an assumed
// default with the plan-save reference only for its first routing, and moving
// one that is already routed needs the person's own words. rdd-completion-review
// and rdd-deliver close only backlog, gap and tooling records by the Epic,
// never an assumed default, and present the changes since approval and the
// assumed defaults not yet confirmed, which block nothing.
//
// The phrases are read from the installed copy under .modernpath/rdd with
// planningContextText (whitespace folded, Markdown code and emphasis marks
// dropped, typographic quotes made plain, letters lowered), so they may wrap
// or be formatted. Each skill's .claude/skills copy equals its .modernpath/rdd
// copy. The sentences TestInstalledKitRoutesAndClosesBacklogRecords pins stay
// as they are.

func installedA6Process(t *testing.T, root string) string {
	t.Helper()
	return planningContextText(installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md"))
}

func installedA6Skill(t *testing.T, root, skill string) string {
	t.Helper()
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", skill, "SKILL.md")
	claude := installedKitFile(t, root, ".claude", "skills", skill, "SKILL.md")
	if !bytes.Equal(canonical, claude) {
		t.Errorf("%s: the .claude/skills copy differs from the .modernpath/rdd copy", skill)
	}
	return planningContextText(canonical)
}

func requireA6Phrases(t *testing.T, where, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("the installed %s is missing %q", where, phrase)
		}
	}
}

func installA6Kit(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	return root
}

// a6AssumedDefaultPhrases are what PROCESS.md and rdd-autopilot both say of an
// assumed default (criterion a7fba45b).
var a6AssumedDefaultPhrases = []string{
	"an assumed default is recorded as a backlog record of kind assumption with its question",
	"routed once to the epic being built",
	"the server refuses any later change of its disposition without a user: source",
	"the agent writes that source only from the person's own words, never from a template",
}

func TestA6BD44C890ProcessSaysHowAPlanningContextLocksAndOpens(t *testing.T) {
	root := installA6Kit(t)
	requireA6Phrases(t, "PROCESS.md", installedA6Process(t, root), []string{
		// How it locks.
		"a planning context locks when the business owner agrees it and at the first approval of an epic that reads it",
		// How it is opened for change, and what locks it again.
		"changes only through open for change, by a team member with a reason",
		"the next agreement locks it again",
		// What an approved Epic that reads it does with the change.
		"each approved epic that reads a changed planning context lists the change",
		"only its approver returns it to planning, and only if the change needs a new plan",
	})
}

func TestA6BD44C890BuildReadsTheChangesSinceApproval(t *testing.T) {
	root := installA6Kit(t)
	requireA6Phrases(t, "rdd-build", installedA6Skill(t, root, "rdd-build"), []string{
		"before building, read the changes since approval with epics gates presentation",
		"tell the approver about a change the plan did not know",
	})
}

func TestA6BD44C890ProcessAndAutopilotRecordAnAssumedDefault(t *testing.T) {
	root := installA6Kit(t)
	requireA6Phrases(t, "PROCESS.md", installedA6Process(t, root), a6AssumedDefaultPhrases)
	requireA6Phrases(t, "rdd-autopilot", installedA6Skill(t, root, "rdd-autopilot"), a6AssumedDefaultPhrases)
}

func TestA6BD44C890TriageRoutesAnAssumedDefaultOnlyOnce(t *testing.T) {
	root := installA6Kit(t)
	requireA6Phrases(t, "rdd-triage", installedA6Skill(t, root, "rdd-triage"), []string{
		"an assumed default is routed that way only for its first routing",
		"moving an assumed default that is already routed needs the person's own words",
	})
}

func TestA6BD44C890CompletionClosesNoAssumedDefault(t *testing.T) {
	root := installA6Kit(t)
	for _, skill := range []string{"rdd-completion-review", "rdd-deliver"} {
		requireA6Phrases(t, skill, installedA6Skill(t, root, skill), []string{
			"an assumed default is never closed by the epic",
			"present the changes since approval and the assumed defaults not yet confirmed, which block nothing",
		})
	}
}
