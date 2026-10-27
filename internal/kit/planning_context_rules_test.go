package kit

import (
	"bytes"
	"strings"
	"testing"
)

// Epic 325 (A2), test case c032955d: the delivery process text the CLI
// installs names the planning contexts of the Epic's Release and Initiative.
// PROCESS.md says what they hold, that an Epic is planned and built within
// both, that the plan review reads them from the review bundle and that a
// change makes a PROPOSED Epic's plan review outdated, and that the agent
// reads them at session entry and before it plans and builds, not from the
// context hook (criterion a90edcb4). rdd-start reads them for the selected
// Epic with `epics get <id> --fields planning_contexts`; rdd-plan and
// rdd-build read them again before planning and before building; rdd-cold-review
// checks the plan against the ones the review bundle carries (criteria
// a90edcb4, b39422f9). Each skill's .claude/skills copy equals its
// .modernpath/rdd copy.

// planningContextText folds whitespace, drops Markdown code and emphasis
// marks, makes typographic quotes plain and lowers letters, so a phrase may
// wrap or be formatted in the installed file.
func planningContextText(body []byte) string {
	plain := strings.NewReplacer("`", "", "*", "", "’", "'", "→", "->").Replace(string(body))
	return strings.ToLower(strings.Join(strings.Fields(plain), " "))
}

const planningContextRead = "epics get <id> --fields planning_contexts"

func TestInstalledProcessNamesThePlanningContexts(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	process := planningContextText(installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md"))
	for _, phrase := range []string{
		// What they hold, and that an Epic is planned and built within both.
		"a release and an initiative each carry a planning context",
		"decisions, assumptions, open questions and references that hold for all the work under them",
		"an epic is planned and built within both",
		// The plan review reads them, and a change outdates a PROPOSED Epic's review.
		"the plan review reads them from the review bundle",
		"while the epic is proposed a change to either makes its plan review outdated",
		// When the agent reads them, and that the context hook does not carry them.
		"when it enters a session on an epic",
		"before it plans and before it builds",
		planningContextRead,
		"roadmap show",
		"the context hook does not carry them",
	} {
		if !strings.Contains(process, phrase) {
			t.Errorf("the installed PROCESS.md is missing %q", phrase)
		}
	}
}

func TestInstalledSkillsReadThePlanningContexts(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, skill := range []struct {
		name    string
		phrases []string
	}{
		{"rdd-start", []string{
			"read the planning contexts of the epic's release and initiative",
			planningContextRead,
			"the epic is planned and built within them",
		}},
		{"rdd-plan", []string{
			"read the planning contexts of the epic's release and initiative again before planning",
			planningContextRead,
			"plan within them",
			"the decisions and assumptions it relies on and any it conflicts with",
		}},
		{"rdd-build", []string{
			"before the first change, read the planning contexts of the epic's release and initiative again",
			planningContextRead,
			"build within them",
			"a question they leave open goes to the human",
		}},
		{"rdd-cold-review", []string{
			"the plan stays within the planning contexts of the epic's release and initiative that the review bundle carries",
			"planning_contexts",
			"names the decisions and assumptions it relies on",
			"a plan that contradicts one of them is a material finding",
		}},
	} {
		canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", skill.name, "SKILL.md")
		claude := installedKitFile(t, root, ".claude", "skills", skill.name, "SKILL.md")
		if !bytes.Equal(canonical, claude) {
			t.Errorf("%s: the .claude/skills copy differs from the .modernpath/rdd copy", skill.name)
		}
		text := planningContextText(canonical)
		for _, phrase := range skill.phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("the installed %s is missing %q", skill.name, phrase)
			}
		}
	}
}
