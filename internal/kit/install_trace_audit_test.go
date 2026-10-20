package kit

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeRemovesRetiredAuditToolsAndPreservesClientFiles(t *testing.T) {
	root := t.TempDir()
	retired := []string{
		".claude/skills/rdd-reverse-engineer/audit-citations.mjs",
		".modernpath/rdd/skills/rdd-audit/audit-citations.mjs",
		".modernpath/rdd/skills/rdd-audit/audit-citations.test.mjs",
	}
	custom := ".modernpath/rdd/skills/rdd-audit/customer-notes.md"
	for _, name := range append(retired, custom) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("client content\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range retired {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("retired tool remains: %s (%v)", name, err)
		}
	}
	content, err := os.ReadFile(filepath.Join(root, custom))
	if err != nil || string(content) != "client content\n" {
		t.Fatalf("client file changed: %q (%v)", content, err)
	}
	for _, name := range []string{".modernpath/rdd/skills/rdd-audit/SKILL.md", ".claude/skills/rdd-audit/SKILL.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("Trace audit skill missing: %s (%v)", name, err)
		}
	}
}
