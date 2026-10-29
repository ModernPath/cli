package kit

import (
	"bytes"
	"strings"
	"testing"
)

// Epic 382 (SR-4625-0, criterion f8d17252, test case f1f06ddd): the kit's
// rdd-plan skill, PROCESS.md and CLI guide, as modernpath install renders
// them, tell an agent to give each declared question a one-line title naming
// the choice, to put its alternatives in options with the recommended one
// marked, and to keep its background in the question text. Your move shows
// the title on a two-line card; the question text is read in the drawer.
func TestKitSaysEachDecisionHasAOneLineTitleOptionsAndItsBackgroundInTheQuestion(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	markers := []string{
		"one-line title that names the choice",
		"with the recommended one marked",
		"background stays in the question text",
	}
	skill := installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-plan", "SKILL.md")
	if claude := installedKitFile(t, root, ".claude", "skills", "rdd-plan", "SKILL.md"); !bytes.Equal(skill, claude) {
		t.Errorf("rdd-plan: the .claude/skills copy differs from the .modernpath/rdd copy")
	}
	guide, err := assets.ReadFile("assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []struct {
		name string
		body []byte
	}{
		{"installed rdd-plan/SKILL.md", skill},
		{"installed PROCESS.md", installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md")},
		{"cli-guide/cli.md", guide},
	} {
		text := strings.ReplaceAll(kitWhitespaceNormalised(document.body), "`", "")
		for _, marker := range markers {
			if !strings.Contains(text, marker) {
				t.Errorf("%s does not say %q", document.name, marker)
			}
		}
		if !strings.Contains(text, "options") || !strings.Contains(text, "title") {
			t.Errorf("%s does not name the title and options fields", document.name)
		}
	}
}
