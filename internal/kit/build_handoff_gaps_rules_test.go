package kit

import (
	"os"
	"path/filepath"
	"testing"
)

// Epic 428: rdd-build tells a delegated build agent where a partly built
// criterion, a regression run and an unchanged file go in the build handoff.
//
// TestCase 8abd9910-7e60-4344-bc92-fcad5d2cbf1b
func TestRDDBuildNamesTheBuildHandoffGapFields(t *testing.T) {
	// The helpers compare text without backticks and line breaks.
	phrases := []string{foldBuildRecordText(
		"A criterion it built only in part goes in the build handoff's `excluded` with the reason, a TestCase it reran for a criterion of a published requirement that no Epic owns goes in `regression`, and a file a requirement uses but the build did not change goes in `unchanged_paths`.",
	)}
	const asset = "assets/rdd/skills/rdd-build/SKILL.md"
	requireBuildRecordPhrases(t, asset, buildRecordText(t, asset), phrases)

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{".modernpath/rdd/skills/rdd-build/SKILL.md", ".claude/skills/rdd-build/SKILL.md"} {
		body, err := os.ReadFile(filepath.Join(root, path))
		if err != nil {
			t.Errorf("install must write %s: %v", path, err)
			continue
		}
		requireBuildRecordPhrases(t, "installed "+path, foldBuildRecordText(string(body)), phrases)
	}
}
