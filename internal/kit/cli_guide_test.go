package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedCLIGuideMatchesCanonicalSource(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("not the kit-owning workspace")
	}

	canonical, err := os.ReadFile(filepath.Join(root, "modernpath-core", "docs", "cli.md"))
	if err != nil {
		t.Fatal(err)
	}
	embedded, err := assets.ReadFile("assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(embedded, canonical) {
		t.Fatal("embedded CLI guide differs from modernpath-core/docs/cli.md")
	}
}

// embeddedCLIGuide is the kit's copy of the CLI guide, which equals
// modernpath-core/docs/cli.md (TestEmbeddedCLIGuideMatchesCanonicalSource).
func embeddedCLIGuide(t *testing.T) string {
	t.Helper()
	body, err := assets.ReadFile("assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// guideSectionRunning returns the guide section, from one heading to the
// next, that holds a line starting with command, or "".
func guideSectionRunning(guide, command string) string {
	var section []string
	found := false
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, "#") {
			if found {
				break
			}
			section = nil
		}
		section = append(section, line)
		if strings.HasPrefix(strings.TrimSpace(line), command) {
			found = true
		}
	}
	if !found {
		return ""
	}
	return strings.Join(section, "\n")
}

// Epic 340 (G1), test case 94c50400: the CLI guide has a section on
// requirements contexts, with the command and its Unclassified line, and the
// command table's Native requirements row names it. The check runs this test
// by its name.
func TestG1ListGuideNamesRequirementsContexts(t *testing.T) {
	guide := embeddedCLIGuide(t)
	section := guideSectionRunning(guide, "modernpath requirements contexts")
	if section == "" {
		t.Error("the CLI guide has no section that runs modernpath requirements contexts")
	} else {
		for _, want := range []string{"Unclassified", "--json"} {
			if !strings.Contains(section, want) {
				t.Errorf("the CLI guide's section on requirements contexts does not name %q", want)
			}
		}
	}
	row := ""
	for _, line := range strings.Split(guide, "\n") {
		if strings.HasPrefix(line, "| requirements |") {
			row = line
		}
	}
	if !strings.Contains(row, "`requirements contexts`") {
		t.Errorf("the command table's requirements row does not name `requirements contexts`: %q", row)
	}
}

// Epic 340 (G1), test case b71f8e62: the CLI guide says that a regrouped
// requirement without a name takes the listed context's name, and no longer
// that a missing name defaults to the code. The check runs this test by its
// name.
func TestG1RegroupGuideGivesTheListsName(t *testing.T) {
	guide := strings.Join(strings.Fields(embeddedCLIGuide(t)), " ")
	requirePhrases(t, "cli.md", guide, []string{
		"takes the name the System's list gives that context",
		"leave the name out for a listed context",
	})
	forbidPhrases(t, "cli.md", guide, []string{
		"context_name defaults to context",
		"defaults to the `--context` value",
	})
}

// Epic 327 (A4), test case c5728b13 (criteria e4b037bc, 4d7ad302): the CLI
// guide, modernpath-core/docs/cli.md and its copy embedded in the kit, says in
// its section on Epic records that `epics create` takes `line_id`, links the
// Epic to that Initiative, places it in the Initiative's Release unless
// `release_id` names another open Release, and returns the Initiative's
// context, which the agent writes into the requirements it creates; and what
// POST /api/work/epics refuses when `epics create` or `modernpath new` calls
// it. Phrases are matched without code marks, case or line breaks.

const a4EpicRecordsHeading = "### Read Epics and exchange specifications"

func a4EpicRecordsSection(t *testing.T, name string, guide []byte) string {
	t.Helper()
	text := string(guide)
	start := strings.Index(text, a4EpicRecordsHeading)
	if start < 0 {
		t.Fatalf("%s has no section %q", name, a4EpicRecordsHeading)
	}
	rest := text[start+len(a4EpicRecordsHeading):]
	if end := strings.Index(rest, "\n### "); end >= 0 {
		rest = rest[:end]
	}
	return planningContextText([]byte(rest))
}

func a4Guides(t *testing.T) map[string][]byte {
	t.Helper()
	embedded, err := assets.ReadFile("assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	guides := map[string][]byte{"the kit's copy of the CLI guide": embedded}
	if root := repoRoot(t); root != "" {
		canonical, err := os.ReadFile(filepath.Join(root, "modernpath-core", "docs", "cli.md"))
		if err != nil {
			t.Fatal(err)
		}
		guides["modernpath-core/docs/cli.md"] = canonical
	}
	return guides
}

func TestA4CLIGuideSaysEpicsCreateNamesTheInitiative(t *testing.T) {
	for name, guide := range a4Guides(t) {
		section := a4EpicRecordsSection(t, name, guide)
		for _, phrase := range []string{
			"epics create takes line_id",
			"links the epic to that initiative",
			"places it in the initiative's release unless release_id names another open release",
			"returns the initiative's context",
			"into the requirements it creates for that epic",
		} {
			if !strings.Contains(section, phrase) {
				t.Errorf("%s does not say %q", name, phrase)
			}
		}
	}
}

func TestA4CLIGuideSaysWhatEpicCreationRefuses(t *testing.T) {
	for name, guide := range a4Guides(t) {
		section := a4EpicRecordsSection(t, name, guide)
		for _, phrase := range []string{
			"post /api/work/epics",
			"modernpath new",
			"a system outside the workspace",
			"404",
			"invalid_system_id",
			"not a positive integer",
			"other than system_id and line_id",
			"a planning-field value that epics update would refuse on a new epic",
			"shared_context references other than []",
			"422 and the field named",
		} {
			if !strings.Contains(section, phrase) {
				t.Errorf("%s does not say %q", name, phrase)
			}
		}
	}
}
