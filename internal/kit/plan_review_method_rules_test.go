package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// The cold reviewer's method for a plan: sample the citations, then check the
// plan itself, the consequences it did not consider and its proof, in that
// order, and rate a promised rule with no mechanism as material. Measured on
// the first plans of Epics 427 and 428 (SR-4883).
//
// TestCase 792da203-2408-49d1-997b-aa7281b6fb03
func TestColdReviewerStatesThePlanReviewOrderAndSeverityRule(t *testing.T) {
	phrases := []string{
		"For a plan, a citation that checks out proves nothing about the design: sample the plan's file and line claims rather than confirming each one, and spend the effort in this order.",
		"1. The plan itself. Is every rule in every criterion fully specified and deterministic for each format, language, state and case it claims, so that two builders would build the same thing?",
		"Does every person or agent who must act get the instruction the mechanism depends on?",
		"Does the plan do what each human decision and the originating need say, and does anything it leaves to the build or to an assumption change approved content?",
		"Do the plan body, criteria, requirement fields and TestCase titles agree?",
		"2. Consequences the plan did not consider.",
		"state that already exists before the request, such as earlier outcomes, records, traces, files and bindings, and what the rule does with it;",
		"every later reader of what is written, including the process's own gates, readiness, completion and closing steps and what a person is shown there;",
		"the product function that already decides it: search for it, open it and compare branch by branch, including its argument forms;",
		"for every check the plan relaxes, each command that now passes it and what that command does next;",
		"for every matching, naming or keying rule, a second thing that would also match or share the key;",
		"an old client or server against a new one, and empty sections.",
		"3. Proof. Each expected RED fails at the first commit for the stated reason, each refusal has a named code and message, and the project check runs the test files that cover the changed code.",
		"Then check the largest input, retry and dry run, locks and audit.",
		"It is also material when the published rule would be false or would differ from the product's existing rule, even without a concrete harm case, when the plan promises a rule but gives no mechanism for it, and when a deferral names a home that does not hold it.",
	}

	requireBuildRecordPhrases(t, "assets/agents/rdd-cold-reviewer.md", buildRecordText(t, "assets/agents/rdd-cold-reviewer.md"), phrases)
	requireBuildRecordPhrases(t, "assets/agents-codex/rdd-cold-reviewer.toml", buildRecordText(t, "assets/agents-codex/rdd-cold-reviewer.toml"), phrases)

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".claude/agents/rdd-cold-reviewer.md", ".codex/agents/rdd-cold-reviewer.toml"} {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Errorf("install must write %s: %v", path, err)
			continue
		}
		requireBuildRecordPhrases(t, "installed "+path, foldBuildRecordText(string(body)), phrases)
	}
}
