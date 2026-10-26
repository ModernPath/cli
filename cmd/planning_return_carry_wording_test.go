package cmd

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

// TestCase 5d479343 (criterion a17434d0, Epic 348): "return-to-planning help,
// CLI reference, reference loop and PROCESS.md state the decision and RED carry
// rules and that the plan review is renewed".
func TestReturnToPlanningHelpCLIReferenceReferenceLoopAndPROCESSMdStateTheCarryRulesAndTheRenamedOrNewTestCaseRule(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	asset, err := fs.ReadFile(kit.Assets(), "assets/rdd/PROCESS.md")
	if err != nil {
		t.Fatal(err)
	}
	documents := []struct{ name, text string }{
		{"return-to-planning help", epicPlanOperationHelp["return-to-planning"]},
		{"rendered CLI reference", renderCLIReference(rootCmd)},
		{"cmd/reference-loop.md", referenceLoop},
		{"kit asset internal/kit/assets/rdd/PROCESS.md", string(asset)},
	}
	// The installed copies exist only in the monorepo checkout; a module built
	// alone (the core image's go test stage) has the documents above.
	installed := filepath.Join("..", "..", "..", "..", ".modernpath")
	if _, err := os.Stat(installed); err == nil {
		documents = append(documents,
			struct{ name, text string }{"installed .modernpath/rdd/PROCESS.md", read(t, filepath.Join(installed, "rdd", "PROCESS.md"))},
			struct{ name, text string }{"installed .modernpath/cli-reference.md", read(t, filepath.Join(installed, "cli-reference.md"))},
		)
	}
	required := []struct {
		rule    string
		phrases []string
	}{
		{"listed decisions carry", []string{"carry_decisions", "--carry-decision"}},
		{"the plan review is renewed", []string{"a new plan review"}},
		{"RED stands for an unchanged criterion and TestCase", []string{"red stands"}},
		{"a rewritten test needs a renamed or new TestCase", []string{
			"a rewritten test needs a renamed or new testcase, or a changed criterion, to require a new red",
		}},
		{"approval, candidate, GREEN and implementation review are renewed", []string{
			"are always renewed",
			"collect green on a new candidate and a new implementation review",
		}},
	}
	obsolete := []string{"fresh review, human approval and executions", "plan-phase digest", "plan review stands"}
	for _, document := range documents {
		t.Run(document.name, func(t *testing.T) {
			// Markdown wraps lines, so compare lowercased text with collapsed whitespace.
			text := strings.ToLower(strings.Join(strings.Fields(document.text), " "))
			for _, rule := range required {
				found := false
				for _, phrase := range rule.phrases {
					found = found || strings.Contains(text, phrase)
				}
				if !found {
					t.Errorf("%s must state that %s (one of %q)", document.name, rule.rule, rule.phrases)
				}
			}
			for _, phrase := range obsolete {
				if strings.Contains(text, phrase) {
					t.Errorf("%s still says %q", document.name, phrase)
				}
			}
		})
	}
}
