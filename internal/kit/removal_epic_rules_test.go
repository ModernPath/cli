package kit

import (
	"strings"
	"testing"
)

// Epic 380, SR-4653-0 criterion d3c8893a: the rdd-plan skill says how a
// removal Epic ends published behaviour. Pasi approved the process text.
func TestRDDPlanStatesTheRemovalEpicRule(t *testing.T) {
	text := speedRuleText(t, "assets/rdd/skills/rdd-plan/SKILL.md")
	const rule = "A removal Epic retires published requirements through `retire` and removes their traces through `remove_traces`."
	if !strings.Contains(text, rule) {
		t.Errorf("assets/rdd/skills/rdd-plan/SKILL.md is missing %q", rule)
	}
}
