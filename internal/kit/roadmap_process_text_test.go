package kit

import (
	"strings"
	"testing"
)

// Epic 324 (A1), test case 5dc338b0: the delivery process text the CLI
// installs says what a Release, an Initiative and the Roadmap are, that an
// Initiative has one bounded context its Epics' requirements share, the chain
// Initiative -> Epic -> requirement -> trace, and how an agent reads the
// Roadmap and applies a plan on a person's word; rdd-start and rdd-plan tell
// the agent to read the Roadmap with `modernpath roadmap show` before
// planning.

// roadmapText reads a packaged asset with its whitespace folded, Markdown
// emphasis and code marks removed, typographic quotes and arrows made plain
// and letters lowered, so a phrase may wrap or be formatted in the source.
func roadmapText(t *testing.T, path string) string {
	t.Helper()
	body, err := assets.ReadFile(path)
	if err != nil {
		t.Errorf("the kit must embed %s: %v", path, err)
		return ""
	}
	plain := strings.NewReplacer("`", "", "*", "", "’", "'", "→", "->").Replace(string(body))
	return strings.ToLower(strings.Join(strings.Fields(plain), " "))
}

func TestProcessNamesTheReleaseTheInitiativeAndTheRoadmap(t *testing.T) {
	process := roadmapText(t, "assets/rdd/PROCESS.md")
	for _, phrase := range []string{
		// What each is.
		"a release is",
		"an initiative is",
		"the roadmap is",
		// The chain, and where an Initiative is shown.
		"initiative -> epic -> requirement -> trace",
		"one bounded context",
		"each epic is in a release",
		"the earliest open release of its epics",
		"span several releases",
		// How an agent reads the Roadmap and applies a plan.
		"modernpath roadmap show",
		"modernpath roadmap apply",
		"on a person's word",
	} {
		if !strings.Contains(process, phrase) {
			t.Errorf("PROCESS.md is missing %q", phrase)
		}
	}
}

func TestStartAndPlanReadTheRoadmapBeforePlanning(t *testing.T) {
	for _, path := range []string{
		"assets/rdd/skills/rdd-start/SKILL.md",
		"assets/rdd/skills/rdd-plan/SKILL.md",
	} {
		text := roadmapText(t, path)
		for _, phrase := range []string{
			"modernpath roadmap show",
			"the epics it covers",
			"the context of the initiative",
			"before planning",
		} {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s is missing %q", path, phrase)
			}
		}
	}
}
