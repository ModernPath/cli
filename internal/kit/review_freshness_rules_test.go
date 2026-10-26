package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCase 3b59260c (criterion 11aeb74e, Epic 348): a review is recorded as it
// is (USER:2026-10-26). No packaged text describes review digests, the
// re-review diff or the round digest, and PROCESS.md keeps the
// no-fingerprints rule and says what a round review records.
func TestKitCLIHelpCLIGuideAndDataDocsDescribeReviewFreshnessFromRecordedChangesWithNoDigests(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	read := func(t *testing.T, parts ...string) []byte {
		t.Helper()
		body, err := os.ReadFile(filepath.Join(parts...))
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	process := installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md")
	texts := []struct {
		name string
		body []byte
	}{
		{"PROCESS.md", process},
		{"rdd-cold-review SKILL.md", installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-cold-review", "SKILL.md")},
		{"rdd-cold-reviewer agent", installedKitFile(t, root, ".claude", "agents", "rdd-cold-reviewer.md")},
		{"rdd-cold-reviewer Codex agent", installedKitFile(t, root, ".codex", "agents", "rdd-cold-reviewer.toml")},
		{"cli.md", read(t, "assets", "cli-guide", "cli.md")},
		// The help texts are raw string literals in the cmd package.
		{"epics gates help", read(t, "..", "..", "cmd", "epic_gates.go")},
		{"initiative help", read(t, "..", "..", "cmd", "initiative.go")},
		{"CLI CHANGELOG", read(t, "..", "..", "CHANGELOG.md")},
	}
	// The data docs exist only in the monorepo checkout; the core image tests
	// this module alone.
	docs := filepath.Join("..", "..", "..", "..", "..", "docs", "data", "minimal-rdd-evidence.md")
	if body, err := os.ReadFile(docs); err == nil {
		texts = append(texts, struct {
			name string
			body []byte
		}{"docs/data/minimal-rdd-evidence.md", body})
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}

	gone := []string{
		"phase digest", "bundle_digest", "round_digest", "round digest", "digests.plan",
		"digests.implementation", "review_bundle_changed", "--since-review",
		"changed since the bundle was read", "diff since the last reviewed bundle",
	}
	for _, text := range texts {
		// Lowercased with collapsed whitespace, so wrapped lines still match.
		normalised := strings.ToLower(kitWhitespaceNormalised(text.body))
		for _, phrase := range gone {
			if strings.Contains(normalised, phrase) {
				t.Errorf("the %s still says %q", text.name, phrase)
			}
		}
	}

	for _, file := range []struct {
		name    string
		body    []byte
		clauses []string
	}{
		{"PROCESS.md", process, []string{
			"Add no fingerprints, ETags, tokens, counters or reviewed-state fields.",
			"A round review records the Initiative and the Epic list it covered as ordinary review content.",
		}},
		{"initiative help", read(t, "..", "..", "cmd", "initiative.go"), []string{
			"recorded on this Initiative for the same Epic list",
		}},
	} {
		normalised := kitWhitespaceNormalised(file.body)
		for _, clause := range file.clauses {
			if !strings.Contains(normalised, clause) {
				t.Errorf("the %s does not say %q", file.name, clause)
			}
		}
	}
}
