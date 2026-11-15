package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRecordText reads a packaged asset with whitespace folded and Markdown
// code ticks removed, so a phrase may wrap or name a command in backticks.
func buildRecordText(t *testing.T, path string) string {
	t.Helper()
	body, err := assets.ReadFile(path)
	if err != nil {
		t.Errorf("the kit must embed %s: %v", path, err)
		return ""
	}
	return foldBuildRecordText(string(body))
}

func foldBuildRecordText(text string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
}

func requireBuildRecordPhrases(t *testing.T, name, text string, phrases []string) {
	t.Helper()
	for _, phrase := range phrases {
		if !strings.Contains(text, phrase) {
			t.Errorf("%s is missing %q", name, phrase)
		}
	}
}

// processSection returns the text of one "### " or "## " section of PROCESS.md.
func processSection(t *testing.T, process, heading string) string {
	t.Helper()
	start := strings.Index(process, heading+"\n")
	if start < 0 {
		t.Errorf("PROCESS.md has no section %q", heading)
		return ""
	}
	rest := process[start+len(heading)+1:]
	end := len(rest)
	for _, marker := range []string{"\n## ", "\n### "} {
		if i := strings.Index(rest, marker); i >= 0 && i < end {
			end = i
		}
	}
	return foldBuildRecordText(rest[:end])
}

// TestCase 8e4ece7f-19af-4035-afb2-86847bf24909
func TestKitTellsADelegatedBuildToWriteAHandoffRecordedByTheSession(t *testing.T) {
	requireBuildRecordPhrases(t, "rdd-build", buildRecordText(t, "assets/rdd/skills/rdd-build/SKILL.md"), []string{
		"A delegated build agent puts each TestCase's full UUID in its test's name or in a comment just above the test (for a Go test function, in a comment above it), or lists the test names per TestCase in the build handoff's tests.",
		"It ends by writing the build handoff",
		"It never writes to the store.",
		"After a delegated build, run modernpath epics build record <id> --file <handoff> with --dry-run first, then without it.",
		"Then read back CriterionTestResults and Traces.",
	})
	requireBuildRecordPhrases(t, "rdd-autopilot", buildRecordText(t, "assets/rdd/skills/rdd-autopilot/SKILL.md"), []string{
		"During a delegated build, the assumed defaults go into the build handoff and are recorded with the build.",
	})

	body, err := assets.ReadFile("assets/rdd/PROCESS.md")
	if err != nil {
		t.Fatalf("the kit must embed PROCESS.md: %v", err)
	}
	process := string(body)
	requireBuildRecordPhrases(t, "PROCESS.md §Delegated passes", processSection(t, process, "### Delegated passes"), []string{
		"A build pass may also run in an independent context.",
		"It never writes to the store: it ends by writing the build handoff, an input file for one command and not a record,",
		"who records the build with modernpath epics build record.",
	})
	requireBuildRecordPhrases(t, "PROCESS.md §Evidence and completion", processSection(t, process, "## Evidence and completion"), []string{
		"use modernpath evidence record, or modernpath epics build record after a delegated build, for current direct proof.",
	})

	// The pass-to-command table lives in cmd/reference-loop.md.
	loop, err := os.ReadFile(filepath.Join("..", "..", "cmd", "reference-loop.md"))
	if err != nil {
		t.Fatalf("read cmd/reference-loop.md: %v", err)
	}
	implement := ""
	for _, line := range strings.Split(string(loop), "\n") {
		if strings.HasPrefix(line, "| Implement |") {
			implement = line
		}
	}
	if implement == "" {
		t.Fatal("cmd/reference-loop.md has no Implement row")
	}
	if !strings.Contains(implement, "epics build record") {
		t.Errorf("the Implement row of the pass-to-command table does not name epics build record: %s", implement)
	}
}

// TestCase 1a062fc6-9dfd-42bb-ad08-ffbf17999249
func TestVerifyRebasesOnceAndCompletionReviewReusesCurrentResults(t *testing.T) {
	requireBuildRecordPhrases(t, "rdd-verify", buildRecordText(t, "assets/rdd/skills/rdd-verify/SKILL.md"), []string{
		"Make the single rebase onto main that rdd-build names before your final checks, rerun the affected checks after it, and name in the PR notes the commit your checks ran at and the tests they ran.",
		"Hand current results and limits to rdd-completion-review, which reuses them;",
	})
	review := buildRecordText(t, "assets/rdd/skills/rdd-completion-review/SKILL.md")
	requireBuildRecordPhrases(t, "rdd-completion-review", review, []string{
		"Keep a current passing result, without recording it again, when the PR notes list its owning tests as run at the commit the verification checks ran at, the branch has not been rebased since, and nothing in the owning package or app, a package or app it depends on, its lockfiles or its configuration has changed since that commit.",
		"git diff --name-only <commit> lists committed and uncommitted changes, and git status --porcelain lists untracked files.",
		"Run the owning checks for every other criterion, and record those outcomes and limits before obtaining the implementation review.",
	})
	if strings.Contains(review, "Run the owning checks and record current criterion outcomes and limits before obtaining the implementation review.") {
		t.Error("rdd-completion-review still tells the agent to run the owning checks for every criterion")
	}
}
