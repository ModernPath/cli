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
		"each epic's implementation approver or its assignee",
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
			"a round submission needs the caller to be each epic's implementation approver or its assignee",
			"any one person may approve a round's completion",
		}},
		{"kit asset rdd-cold-review", asset(t, "assets/rdd/skills/rdd-cold-review/SKILL.md"), []string{
			"for an initiative round, one review covers the round: read the round review bundle of the explicit list of epics and record one review for all of them",
		}},
		{"kit asset rdd-entry-review", asset(t, "assets/rdd/skills/rdd-entry-review/SKILL.md"), []string{
			"an initiative round is the explicit list of the initiative's epics given to the command; one round approval writes each epic's own approval records, all or nothing",
		}},
		{"kit asset rdd-completion-review", asset(t, "assets/rdd/skills/rdd-completion-review/SKILL.md"), []string{
			"a round submission needs the caller to be each epic's implementation approver or its assignee; any one person may approve a round's completion",
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

// TestCase 77e921f0 (Epic 377, criterion 7a7fa367): the Epic's implementation
// approver or its assignee carries on an approved build, so every text that
// said only the approver now says both, and none keeps the approver-only
// sentence; returning to planning stays the approver's. The help of epics
// gates declare-decision names the question's addressee.
func TestTheApproverOrTheAssigneeCarriesOnTheBuildInEveryDocumentAndDeclareDecisionNamesTheAddressee(t *testing.T) {
	asset := func(t *testing.T, path string) string {
		t.Helper()
		body, err := fs.ReadFile(kit.Assets(), path)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	help := func(t *testing.T, path ...string) string {
		t.Helper()
		cmd, _, err := rootCmd.Find(path)
		if err != nil || cmd == nil || cmd.Name() != path[len(path)-1] {
			t.Fatalf("command %v not found: %v", path, err)
		}
		return cmd.Short + "\n" + cmd.Long
	}
	// The old round sentence: "... each Epic's implementation approver; any
	// one person may approve ..." — the new phrase contains the old one's
	// beginning, so its end is what must be gone.
	roundGone := []string{"implementation approver; any one person may approve"}
	reference := renderCLIReference(rootCmd)
	documents := []struct {
		name, text      string
		present, absent []string
	}{
		{"kit asset rdd/PROCESS.md", asset(t, "assets/rdd/PROCESS.md"),
			[]string{"a round submission needs the caller to be each epic's implementation approver or its assignee"}, roundGone},
		{"kit asset rdd-completion-review", asset(t, "assets/rdd/skills/rdd-completion-review/SKILL.md"),
			[]string{"a round submission needs the caller to be each epic's implementation approver or its assignee"}, roundGone},
		{"kit asset cli-guide/cli.md", asset(t, "assets/cli-guide/cli.md"),
			[]string{"a round submission needs the caller to be each epic's implementation approver or its assignee"}, roundGone},
		{"initiative round rules help", help(t, "initiative"),
			[]string{"a round submission needs the caller to be each epic's implementation approver or its assignee"}, roundGone},
		{"initiative submit-review help", help(t, "initiative", "submit-review"),
			[]string{"the caller must be each epic's implementation approver or its assignee"},
			[]string{"implementation approver; every epic's completion-review gate"}},
		{"epics plan submit-review help", help(t, "epics", "plan", "submit-review"),
			[]string{"only the implementation approver or the epic's assignee can submit"},
			[]string{"only the implementation approver can submit"}},
		{"rendered CLI reference loop", reference,
			[]string{
				"the epic’s implementation approver or assignee may record its proof and submit",
				"(the approver or the assignee runs it)",
				"criterion proof update or `submit-review` by someone other than the implementation approver or the epic's assignee",
				"`return-to-planning` or `recover-legacy` by someone other than the implementation approver",
			},
			[]string{
				"the implementation approver's signed-in account must declare candidates",
				"(the approver runs it)",
				"`submit-review` or `return-to-planning` by someone other than the implementation approver",
			}},
	}
	// docs/cli.md is identical to the kit copy; docs/13-planning-delivery.md
	// exists only in the monorepo checkout.
	if body, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "cli.md")); err == nil {
		documents = append(documents, struct {
			name, text      string
			present, absent []string
		}{"docs/cli.md", string(body),
			[]string{"a round submission needs the caller to be each epic's implementation approver or its assignee"}, roundGone})
	}
	if body, err := os.ReadFile(filepath.Join(roundWordingRepoRoot, "docs", "13-planning-delivery.md")); err == nil {
		documents = append(documents, struct {
			name, text      string
			present, absent []string
		}{"docs/13-planning-delivery.md", string(body),
			[]string{"the epic’s implementation approver or assignee may record proof and submit"},
			[]string{"the implementation approver's account registers candidates"}})
	}
	for _, document := range documents {
		t.Run(document.name, func(t *testing.T) {
			text := roundWordingText(document.text)
			for _, phrase := range document.present {
				if !strings.Contains(text, phrase) {
					t.Errorf("%s must state %q", document.name, phrase)
				}
			}
			for _, phrase := range document.absent {
				if strings.Contains(text, phrase) {
					t.Errorf("%s must no longer state %q", document.name, phrase)
				}
			}
		})
	}

	t.Run("epics gates declare-decision help names addressee", func(t *testing.T) {
		text := roundWordingText(help(t, "epics", "gates"))
		var line string
		for _, candidate := range strings.Split(help(t, "epics", "gates"), "\n") {
			if strings.Contains(candidate, "declare-decision:") {
				line = candidate
			}
		}
		if !strings.Contains(line, `"addressee"`) {
			t.Errorf("the declare-decision input example must name \"addressee\", got %q", line)
		}
		if !strings.Contains(text, "addressee") || !strings.Contains(text, "workspace member") {
			t.Errorf("epics gates help must say the addressee is the email of a workspace member")
		}
	})

	// Epic 382 (SR-4625-0, criterion f8d17252): the input example shows the
	// optional title and options, and the batch reply lists them.
	t.Run("epics gates declare-decision help shows title and options", func(t *testing.T) {
		raw := help(t, "epics", "gates")
		text := roundWordingText(raw)
		var line string
		for _, candidate := range strings.Split(raw, "\n") {
			if strings.Contains(candidate, "declare-decision:") {
				line = candidate
			}
		}
		for _, field := range []string{`"title"`, `"options"`} {
			if !strings.Contains(line, field) {
				t.Errorf("the declare-decision input example must name %s, got %q", field, line)
			}
		}
		for _, phrase := range []string{
			`"key"`, `"text"`, `"recommended"`,
			"1 to 120 characters",
			"2 to 6 options",
			`{"items":[{"index","id","question","title","options"}]}`,
		} {
			if !strings.Contains(text, phrase) {
				t.Errorf("epics gates help must state %q", phrase)
			}
		}
	})
}
