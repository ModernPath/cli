package kit

import (
	"strings"
	"testing"
)

// The plan review loop: each plan submission gets one independent review and
// then goes to the human, who decides on any revision. The planner asks the
// human's questions before drafting, checks the draft before the first review
// and checks each requested change for side effects; the reviewer marks each
// note as requirement text or build, and the build applies the notes left to it.
func TestKitStatesThePlanReviewRules(t *testing.T) {
	rules := map[string][]string{
		"assets/rdd/PROCESS.md": {
			"Each plan submission gets one independent cold review",
			"Do not revise the plan or repeat the review in response to findings before that decision.",
			"A human-requested revision gets one new independent review, then returns to the human.",
			"list the questions only the human can answer and ask them together",
			"The reviewer marks each note as requirement text or build.",
			"each change for side effects",
			"Record the disposition of each note the human leaves to the build",
			"after a first reconnaissance",
			"or the kit's own rules",
			"its answer is recorded once the saved plan lists it",
			"the actual output of any tool it names",
			"A fix is a design change",
			"how a changed record shape behaves through approval, evidence recording and completion",
			"a read-only agent that did not write the fixes checks them",
		},
		"assets/rdd/skills/rdd-plan/SKILL.md": {
			"do not start an autonomous revision-and-review loop",
			"list the questions only the human can answer and ask them in one batch",
			"turn each claim it depends on into a question for the code",
			"Before the first review, check the draft",
			"each change for side effects",
			"search the stored requirements and the kit's own rules",
			"ask the ones that change scope",
			"the server accepts an answer only for a decision the saved plan lists",
			"the actual output of any tool it names",
			"Make the smallest change that removes each finding",
			"as a claim to check, not as a verified fix",
			"a passing plan-ready check does not show that it works there",
			"give the changes to a read-only agent that did not write them",
		},
		"assets/rdd/skills/rdd-discover/SKILL.md": {
			"Ask these questions together",
		},
		"assets/rdd/skills/rdd-cold-review/SKILL.md": {
			"Return one review with all findings and limitations.",
			"Mark each note as requirement text",
			"Check a correction you suggest in the code before you suggest it",
		},
		"assets/agents/rdd-cold-reviewer.md": {
			"Mark each note as requirement text",
			"the agent handling the task hands the plan, verdict and all findings to the human",
			"Check each correction you suggest against the same code",
		},
		"assets/agents-codex/rdd-cold-reviewer.toml": {
			"Mark each note as requirement text",
			"the agent handling the task hands the plan, verdict and all findings to the human",
			"Check each correction you suggest against the same code",
		},
		"assets/rdd/skills/rdd-entry-review/SKILL.md": {
			"request a revision or further review, or reject the plan",
			"Group the notes as the reviewer marked them",
			"give each material finding with the correction you propose",
			"Record the disposition of each note the human leaves to the build",
			"so the human decides on facts",
		},
		"assets/rdd/skills/rdd-build/SKILL.md": {
			"Read the plan review notes the human left to the build",
			"Change a test title before that test's first recorded result",
		},
	}
	for path, phrases := range rules {
		text := speedRuleText(t, path)
		for _, phrase := range phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("%s is missing %q", path, phrase)
			}
		}
	}
}
