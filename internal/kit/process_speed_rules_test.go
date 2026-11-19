package kit

import (
	"strings"
	"testing"
)

// speedRuleText reads a packaged asset with its whitespace folded, so a phrase
// may wrap across lines in the Markdown source.
func speedRuleText(t *testing.T, path string) string {
	t.Helper()
	body, err := assets.ReadFile(path)
	if err != nil {
		t.Errorf("the kit must embed %s: %v", path, err)
		return ""
	}
	return strings.Join(strings.Fields(string(body)), " ")
}

func TestProcessStatesTheDeliverySpeedRules(t *testing.T) {
	process := speedRuleText(t, "assets/rdd/PROCESS.md")
	for _, phrase := range []string{
		// Deterministic tests gate; model runs measure.
		"Gating evidence comes from deterministic tests",
		"is a measurement, never a RED or GREEN obligation",
		"Prompt and model tuning is research outside an Epic's gates",
		// Criteria state behavior; wording is tested by phrase tests.
		"A criterion states observable behavior",
		"a wording change that keeps the behavior stays under the same criterion and needs no return to planning",
	} {
		if !strings.Contains(process, phrase) {
			t.Errorf("PROCESS.md is missing %q", phrase)
		}
	}
	skills := map[string][]string{
		"assets/rdd/skills/rdd-plan/SKILL.md": {
			"Write criteria as observable behavior",
			"Gate only on deterministic tests",
		},
		"assets/rdd/skills/rdd-verify/SKILL.md": {
			"A run whose outcome depends on a language model is a measurement",
		},
	}
	for path, phrases := range skills {
		text := speedRuleText(t, path)
		for _, phrase := range phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s is missing %q", path, phrase)
			}
		}
	}
}
