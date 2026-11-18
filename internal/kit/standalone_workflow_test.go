package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 8dfbf917-331d-4da4-884e-e84ebca67fb2
func TestStandaloneSmallChangeGuidance(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"assets/rdd/PROCESS.md", "assets/rdd/skills/rdd-start/SKILL.md", "assets/rdd/skills/rdd-build/SKILL.md", "assets/rdd/skills/rdd-verify/SKILL.md", "assets/cli-guide/cli.md"} {
		body, err := assets.ReadFile(path)
		if strings.HasPrefix(path, "assets/rdd/") {
			body, err = os.ReadFile(filepath.Join(root, ".modernpath", strings.TrimPrefix(path, "assets/")))
		}
		if err != nil {
			t.Fatal(err)
		}
		phrases := []string{"no_user_requirement_reason", "proposal_generation", "requirements get", "same-System", "never waives", "read-only", "human completion"}
		if path == "assets/rdd/PROCESS.md" {
			phrases = append(phrases, "check what the small change affects", "Create a requirement only for", "without a separate saved assessment")
		}
		for _, phrase := range phrases {
			if !strings.Contains(string(body), phrase) {
				t.Errorf("%s must explain %s for standalone small changes", path, phrase)
			}
		}
	}
}
