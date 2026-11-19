package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// Epic 432: the Cold review row of the loop table, with the kit sentences of
// the same two criteria. The row is isolated with phaseRow because another
// row keeps the review bundle command without a phase. Each text is read as
// rendered or packaged and from a copy installed to a temporary directory,
// with whitespace folded.

type planningLoopText struct{ name, text string }

func planningLoopInstalledRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	reference := kit.Generated{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))}
	if _, err := kit.Install(root, reference); err != nil {
		t.Fatal(err)
	}
	return root
}

func planningLoopInstalledFile(t *testing.T, root, path string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// planningLoopColdReviewRows returns the Cold review row of the rendered
// reference and of the installed .modernpath/cli-reference.md.
func planningLoopColdReviewRows(t *testing.T, root string) []planningLoopText {
	t.Helper()
	var rows []planningLoopText
	for _, ref := range []planningLoopText{
		{"rendered CLI reference", renderCLIReference(rootCmd)},
		{"installed " + cliReferenceTarget, planningLoopInstalledFile(t, root, cliReferenceTarget)},
	} {
		end := strings.Index(ref.text, "## Global flags")
		if end < 0 {
			t.Fatalf("the %s has no Global flags section after the loop table", ref.name)
		}
		row := phaseRow(t, ref.text[:end], "| Cold review |")
		rows = append(rows, planningLoopText{"Cold review row of the " + ref.name, g1Folded(row)})
	}
	return rows
}

func planningLoopKitTexts(t *testing.T, root string, assetPaths []string, installed ...string) []planningLoopText {
	t.Helper()
	var texts []planningLoopText
	for _, asset := range assetPaths {
		body, err := fs.ReadFile(kit.Assets(), asset)
		if err != nil {
			t.Fatal(err)
		}
		texts = append(texts, planningLoopText{asset, g1Folded(string(body))})
	}
	for _, path := range installed {
		texts = append(texts, planningLoopText{"installed " + path, g1Folded(planningLoopInstalledFile(t, root, path))})
	}
	return texts
}

func requirePlanningLoopWording(t *testing.T, texts []planningLoopText, says, gone []string) {
	t.Helper()
	for _, text := range texts {
		for _, phrase := range says {
			if !strings.Contains(text.text, phrase) {
				t.Errorf("the %s does not say %q", text.name, phrase)
			}
		}
		for _, phrase := range gone {
			if strings.Contains(text.text, phrase) {
				t.Errorf("the %s still says %q", text.name, phrase)
			}
		}
	}
}

// TestCase 0374c364-2172-4f23-9c32-ef1518031e8f
func TestLoopTableProcessAndReviewerNameTheFullReviewBeforeApprovalAsTheOneException(t *testing.T) {
	root := planningLoopInstalledRoot(t)
	requirePlanningLoopWording(t, planningLoopColdReviewRows(t, root),
		[]string{"a human-requested plan revision uses `epics plan save`, one new review, the full review before approval when it is due, and another human decision"},
		[]string{"one new review and another human decision"})
	requirePlanningLoopWording(t,
		planningLoopKitTexts(t, root, []string{"assets/rdd/PROCESS.md"}, ".modernpath/rdd/PROCESS.md"),
		[]string{
			"A human-requested revision gets one new independent review, then returns to the human. The one exception to one review for each plan submission and each revision is the full review before approval, defined above.",
			"INDEPENDENT PLAN REVIEW, AND THE FULL REVIEW BEFORE APPROVAL WHEN DUE",
			"One review recorded, and the full review before approval when it is due; exact plan",
		},
		[]string{
			"ONE INDEPENDENT PLAN REVIEW",
			"One review recorded; exact plan",
		})
	requirePlanningLoopWording(t,
		planningLoopKitTexts(t, root,
			[]string{"assets/agents/rdd-cold-reviewer.md", "assets/agents-codex/rdd-cold-reviewer.toml"},
			".claude/agents/rdd-cold-reviewer.md", ".codex/agents/rdd-cold-reviewer.toml"),
		[]string{"The full review before approval, which the agent handling the task obtains itself, is not a review to obtain PASS."},
		nil)
}

// TestCase 0374c364-2172-4f23-9c32-ef1518031e8f
func TestLoopTableAllowsTheFullReviewBeforeApprovalBeforeTheHumanDecision(t *testing.T) {
	root := planningLoopInstalledRoot(t)
	requirePlanningLoopWording(t, planningLoopColdReviewRows(t, root),
		[]string{"do not revise or repeat review before that decision, except the full review before approval;"},
		nil)
}

// TestCase ae90bc7e-1a86-4e6b-87a4-578ea16c00e0
func TestColdReviewFetchesThePlanReviewBundleWithThePlanReadyPhase(t *testing.T) {
	root := planningLoopInstalledRoot(t)
	requirePlanningLoopWording(t, planningLoopColdReviewRows(t, root),
		[]string{"`epics gates review-bundle <id> --phase plan-ready --json`"},
		[]string{"`epics gates review-bundle <id> --json`"})
	requirePlanningLoopWording(t,
		planningLoopKitTexts(t, root, []string{"assets/rdd/skills/rdd-cold-review/SKILL.md"},
			".modernpath/rdd/skills/rdd-cold-review/SKILL.md", ".claude/skills/rdd-cold-review/SKILL.md"),
		[]string{"Fetch the plan review bundle with `--phase plan-ready`. Only with the phase does it hold the earlier reviews of this planning cycle, their findings and their recorded resolutions, and in `readiness` the gate's causes."},
		nil)
}
