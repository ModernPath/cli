package kit

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Epic 338 (D4), test case f0fe0dff-c3a1-469b-8b54-de08d7895cc0:
// "D4 · the completion review skill and the modernization procedure carry
// D4's sentences on the completion reads: phrase test". Through extra verifies
// traces it is a required test of the completion presentation's criteria at 1
// to 4 and of the coding agent's scenario of the completion journey (83af7502,
// "Before approving an Epic's completion, see which kept requirements its
// change touched and how the test suite moved" at 2). The kit check runs
// exactly this function by its name, so every phrase test is a subtest of it.
//
// The test installs the kit into an empty folder with Install and reads the
// installed rdd-completion-review and rdd-modernize skills, each under
// .modernpath/rdd/skills and .claude/skills. Each text is read with the
// whitespace folded and the code marks (backticks) stripped, so a sentence may
// wrap or mark its keys. Step 4 of the completion review runs from its "4."
// line to its "5." line, and the Prove step of rdd-modernize from "### Prove"
// to "### Accept".
//
// Step 4 tells the agent to present the three completion reads with the rest
// of the presentation, before it asks for the completion answer, as reads that
// never block the answer: the revisions, with the sentences on which run will
// count in the words of the presentation's criterion on the revisions and the
// Proof tab's reasons; in place, the kept requirements the change touched, with
// the newest captured result of each replay; and the whole-suite comparison.
// Under assumed default AS-D4-1, step 4 and the Prove step both tell the agent
// to capture, after the merge, every test of each touched kept requirement in
// the Release's scope, all the tests of one requirement in one run, with the
// runs that prove the Epic's requirements or in runs of their own when the
// capture's limits require it.
//
// On the first slice neither skill holds D4's sentences, so those subtests
// fail on their assertions. That each .claude/skills copy equals its
// .modernpath/rdd copy, and that the Prove step names no modernpath command,
// hold on the slice and guard them.
func TestInstalledCompletionReviewPresentsCompletionReads(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	review := d4InstalledSkill(t, root, "rdd-completion-review")
	modernize := d4InstalledSkill(t, root, "rdd-modernize")

	step4 := d4Step(t, review, 4)
	prove := d4Section(t, modernize, "### Prove", "### Accept")

	// The capture of the touched kept requirements' tests after the merge
	// (AS-D4-1), the same two sentences in both skills.
	const captureTouched = "capture every test of the criteria of each kept requirement in the Release's scope that the completion presentation listed as touched by the change, its replay and break test among them."
	const captureRuns = "Put all the tests of one requirement in one run, with the runs that prove the Epic's requirements, or in runs of their own when the capture's limits of 500 tests and 50 criteria in one run require it."

	presents := "For an Epic of a Modernization Release, also present the three completion reads, which inform the answer and never block it: the presentation lists them as completion_reads."
	for _, sentence := range []struct{ name, text string }{
		{"D4 f0fe0dff completion review step 4: present the three completion reads, which never block the answer",
			presents},
		{"D4 f0fe0dff completion review step 4: the revisions name the reference revision, the totals of the baseline and each captured run",
			"revisions names the Release's reference revision, the totals of the baseline (the legacy tests' results captured at that revision) and each captured run the reads use, with its commit and when it was captured."},
		{"D4 f0fe0dff completion review step 4: the revisions say which run will count, in the words of the presentation's criterion",
			"It says that no run will count until the merge commit of the last pull request that carried the change is recorded on the Epic. It also says that, once that commit is recorded, only a run at the merge commit, or at a commit that contains it, will count for the requirements this Epic takes or reaches."},
		{"D4 f0fe0dff completion review step 4: an Epic reaches a requirement through a verifies trace in its saved plan",
			"An Epic reaches a requirement when its saved plan holds a verifies trace to the requirement or one of its criteria."},
		{"D4 f0fe0dff completion review step 4: the revisions give the Proof tab's reasons in that tab's words",
			"It gives the Proof tab's reasons in that tab's words."},
		{"D4 f0fe0dff completion review step 4: in place, the kept requirements the change touched with each replay's newest captured result",
			"In place, kept_touched lists the kept requirements (legacy requirements an Initiative keeps as they are) whose files the change touched, each with the newest captured result of each of its replays."},
		{"D4 f0fe0dff completion review step 4: the whole-suite comparison, in place and onto a new system",
			"suite_comparison compares, in place, each legacy test's newest captured result since the baseline with its result at the reference revision; onto a new system it compares each replay's newest captured result with the one captured before it."},
		{"D4 f0fe0dff completion review step 4: after the merge, capture the tests of the touched kept requirements (AS-D4-1)",
			"After the merge, once the Epic's merge commit is recorded, also " + captureTouched},
		{"D4 f0fe0dff completion review step 4: all the tests of one requirement in one run, within the capture's limits (AS-D4-1)",
			captureRuns},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(step4, sentence.text) {
				t.Errorf("step 4 of rdd-completion-review/SKILL.md is missing the sentence %q", sentence.text)
			}
		})
	}

	t.Run("D4 f0fe0dff completion review step 4: the reads come before the completion answer", func(t *testing.T) {
		at, answer := strings.Index(step4, presents), strings.Index(step4, "Obtain the separate explicit completion answer")
		if at < 0 || answer < 0 || at > answer {
			t.Errorf("step 4 of rdd-completion-review/SKILL.md does not present the completion reads before it asks for the completion answer")
		}
	})

	for _, sentence := range []struct{ name, text string }{
		{"D4 f0fe0dff procedure Prove step: also capture the tests of the touched kept requirements (AS-D4-1)",
			"Also " + captureTouched},
		{"D4 f0fe0dff procedure Prove step: all the tests of one requirement in one run, within the capture's limits (AS-D4-1)",
			captureRuns},
		{"D4 f0fe0dff procedure Prove step: a touched kept requirement outside the scope is left out, with the reason",
			"A touched kept requirement outside the Release's scope (the legacy contexts the Release covers) is left out, because the capture refuses a whole run that holds one criterion outside it."},
	} {
		t.Run(sentence.name, func(t *testing.T) {
			if !strings.Contains(prove, sentence.text) {
				t.Errorf("the Prove step of rdd-modernize/SKILL.md is missing the sentence %q", sentence.text)
			}
		})
	}

	t.Run("D4 f0fe0dff procedure Prove step: it names no modernpath command (guard)", func(t *testing.T) {
		if strings.Contains(prove, "modernpath ") {
			t.Errorf("the Prove step of rdd-modernize/SKILL.md names a modernpath command; the procedure names commands by what they do")
		}
	})
}

// d4InstalledSkill reads an installed skill's .modernpath/rdd copy, after a
// subtest checks that its .claude/skills copy is byte-identical.
func d4InstalledSkill(t *testing.T, root, skill string) []byte {
	t.Helper()
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", skill, "SKILL.md")
	claude := installedKitFile(t, root, ".claude", "skills", skill, "SKILL.md")
	t.Run("D4 f0fe0dff "+skill+": the .claude/skills copy equals the .modernpath/rdd copy (guard)", func(t *testing.T) {
		if !bytes.Equal(canonical, claude) {
			t.Errorf("%s differs from %s", filepath.Join(".claude", "skills", skill, "SKILL.md"),
				filepath.Join(".modernpath", "rdd", "skills", skill, "SKILL.md"))
		}
	})
	return canonical
}

// d4Plain folds the whitespace and strips the code marks.
func d4Plain(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

// d4Step is a skill's numbered step n, from its "n. " line up to the next
// step's line, as d4Plain reads it.
func d4Step(t *testing.T, body []byte, n int) string {
	t.Helper()
	lines := strings.Split(string(body), "\n")
	start, end := -1, len(lines)
	for i, line := range lines {
		if start < 0 && strings.HasPrefix(line, fmt.Sprintf("%d. ", n)) {
			start = i
		} else if start >= 0 && strings.HasPrefix(line, fmt.Sprintf("%d. ", n+1)) {
			end = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("the skill has no step %d", n)
	}
	return d4Plain(strings.Join(lines[start:end], "\n"))
}

// d4Section is a skill's section from its heading line up to the next
// heading, as d4Plain reads it.
func d4Section(t *testing.T, body []byte, heading, next string) string {
	t.Helper()
	text := string(body)
	start := strings.Index(text, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("the skill has no section %q", heading)
	}
	rest := text[start+1:]
	if end := strings.Index(rest, "\n"+next+"\n"); end >= 0 {
		rest = rest[:end]
	}
	return d4Plain(rest)
}
