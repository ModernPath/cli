package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// TestCase e14986a8 (Epic 345): "initiative help, the CLI guide, PROCESS.md,
// the three review skills, ADR 0006, the vocabulary and the roadmap feature
// describe approval rounds and drop one Epic as the rule".

// roundWordingRepoRoot is the monorepo root, four levels above this package.
var roundWordingRepoRoot = filepath.Join("..", "..", "..", "..")

// roundWordingText lowercases and collapses whitespace, since Markdown wraps lines.
func roundWordingText(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

func TestInitiativeHelpTheCLIGuidePROCESSMdTheThreeReviewSkillsADR0006TheVocabularyAndTheRoadmapFeatureDescribeApprovalRoundsAndDropOneEpicAsTheRule(t *testing.T) {
	asset := func(t *testing.T, path string) string {
		t.Helper()
		body, err := fs.ReadFile(kit.Assets(), path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	// Each document states the round rules it uses, in the approved wording
	// (plan §Wording and the corrections after return to planning). Phrases
	// are compared lowercased with collapsed whitespace.
	helpAndGuide := []string{
		"explicit list of the initiative's epics",
		"each epic's own records, all or nothing",
		"each epic's implementation approver",
		"any one person may approve a round's completion",
	}
	documents := []struct {
		name, text string
		phrases    []string
	}{
		{"rendered CLI reference (initiative help)", renderCLIReference(rootCmd), helpAndGuide},
		{"kit asset cli-guide/cli.md", asset(t, "assets/cli-guide/cli.md"), helpAndGuide},
		{"kit asset rdd/PROCESS.md", asset(t, "assets/rdd/PROCESS.md"), []string{
			"a round is the explicit list of the initiative's epics that one command names",
			"a round command writes the same records as the single-epic commands for every epic in the round, in one transaction: every epic passes or nothing is written",
			"a round submission needs the caller to be each epic's implementation approver",
			"any one person may approve a round's completion",
		}},
		{"kit asset rdd-cold-review", asset(t, "assets/rdd/skills/rdd-cold-review/SKILL.md"), []string{
			"for an initiative round, one review covers the round: read the round review bundle of the explicit list of epics and record one review for all of them",
		}},
		{"kit asset rdd-entry-review", asset(t, "assets/rdd/skills/rdd-entry-review/SKILL.md"), []string{
			"an initiative round is the explicit list of the initiative's epics given to the command; one round approval writes each epic's own approval records, all or nothing",
		}},
		{"kit asset rdd-completion-review", asset(t, "assets/rdd/skills/rdd-completion-review/SKILL.md"), []string{
			"a round submission needs the caller to be each epic's implementation approver; any one person may approve a round's completion",
		}},
	}
	// docs/cli.md is identical to the kit copy (reference_test.go); it is read
	// when present, as are the monorepo documents below.
	if body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "cli.md")); err == nil {
		documents = append(documents, struct {
			name, text string
			phrases    []string
		}{"docs/cli.md", string(body), helpAndGuide})
	}
	for _, document := range documents {
		t.Run(document.name, func(t *testing.T) {
			text := roundWordingText(document.text)
			for _, phrase := range document.phrases {
				if !strings.Contains(text, phrase) {
					t.Errorf("%s must state %q", document.name, phrase)
				}
			}
		})
	}

	t.Run("initiative help names the round commands", func(t *testing.T) {
		text := roundWordingText(renderCLIReference(rootCmd))
		for _, command := range []string{
			"modernpath initiative review-bundle", "modernpath initiative review", "modernpath initiative check",
			"modernpath initiative approve-implementation", "modernpath initiative submit-review",
			"modernpath initiative presentation", "modernpath initiative approve-completion",
		} {
			if !strings.Contains(text, command) {
				t.Errorf("the CLI reference must document %q", command)
			}
		}
	})

	// ADR 0006, the vocabulary and the roadmap feature exist only in the
	// monorepo checkout; a module built alone checks the kit and help above.
	docs := filepath.Join(roundWordingRepoRoot, "docs")
	if _, err := os.Stat(filepath.Join(docs, "adr")); err != nil {
		return
	}
	for _, name := range []string{
		filepath.Join("adr", "0006-initiative-and-modernization-release.md"),
		"01-ubiquitous-language.md",
		filepath.Join("features", "roadmap.md"),
	} {
		t.Run(name, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(docs, name))
			if err != nil {
				t.Fatal(err)
			}
			text := roundWordingText(string(body))
			if !strings.Contains(text, "delivered through its epics, one or several, in approval rounds") {
				t.Errorf("%s must say the Initiative is delivered through its Epics, one or several, in approval rounds", name)
			}
			if strings.Contains(text, "delivered by one epic as the rule") {
				t.Errorf("%s must not say %q", name, "delivered by one Epic as the rule")
			}
			if name == "01-ubiquitous-language.md" && !regexp.MustCompile(`(?m)^\|\s*Round\s*\|`).MatchString(string(body)) {
				t.Errorf("the vocabulary must have a Round row")
			}
		})
	}
}
