package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// Epic 437 (SR-4923, criteria 2f0c9c14, a6fde1cd, 00d05306, 7cda81bb; UR-908
// criteria b05600d3, 8b7d8ac1, 5ce76cef): the cold review reads the Active
// Engineering Constraints from the review bundle's `engineering_constraints`
// beside the planning contexts; a finding about one cites it as `EC-<number>`
// with the revision read and says what the change does against it; a finding
// whose only ground is a constraint is recorded with severity `note`, so it is
// information, not a verdict, and blocks no gate (decisions D4 and bffb8deb).
// The skill names the PR-review read as outside this skill, belonging to the
// plan Automated Code Review Skills. The rule stands in the rdd-cold-review
// skill, in the reviewer agent definition and in its generated Codex copy,
// in the assets and the installed copies. Phrases are compared on folded text
// with backticks removed, as the plan review method rules are.

// The phrases every one of the three documents carries.
var engineeringConstraintsReviewPhrases = []string{
	"engineering_constraints",
	"read beside the planning contexts",
	"cites it as EC-<number> with the revision read and says what the change does against it",
	"a finding whose only ground is a constraint is recorded with severity note",
	"information, not a verdict",
	"blocks no gate",
	"belongs to the plan Automated Code Review Skills and is outside this",
}

// TestCase a42dbf75-4288-489a-b8bc-ffebeb2563c3
func TestColdReviewSkillReadsAndCitesTheEngineeringConstraints(t *testing.T) {
	phrases := append([]string{
		"the plan or change keeps to the Active Engineering Constraints the review bundle carries (engineering_constraints: ref, revision, title, statement, rationale and verification method)",
		"the PR-review read of constraints belongs to the plan Automated Code Review Skills and is outside this skill",
	}, engineeringConstraintsReviewPhrases...)
	const asset = "assets/rdd/skills/rdd-cold-review/SKILL.md"
	requireBuildRecordPhrases(t, asset, buildRecordText(t, asset), phrases)

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-cold-review", "SKILL.md")
	claude := installedKitFile(t, root, ".claude", "skills", "rdd-cold-review", "SKILL.md")
	if string(canonical) != string(claude) {
		t.Error("rdd-cold-review: the .claude/skills copy differs from the .modernpath/rdd copy")
	}
	requireBuildRecordPhrases(t, "installed .modernpath/rdd/skills/rdd-cold-review/SKILL.md", foldBuildRecordText(string(canonical)), phrases)
}

// TestCase a42dbf75-4288-489a-b8bc-ffebeb2563c3
func TestColdReviewerReadsAndCitesTheEngineeringConstraints(t *testing.T) {
	phrases := append([]string{
		"The bundle's engineering_constraints are the System's Active Engineering Constraints (ref, revision, title, statement, rationale and verification method)",
		"A finding that concerns an Engineering Constraint cites it as EC-<number> with the revision read",
	}, engineeringConstraintsReviewPhrases...)
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
