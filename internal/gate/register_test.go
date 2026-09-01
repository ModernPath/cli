package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-128: a question register heading is well formed and unique.
//
// Both failures below happened for real on RUN:2026-08-14, in the same session
// that documented them as rules. A rule did not prevent either; nothing checked
// the shape at authoring time, and both fail silently — the register reads fine
// and the sync reports success.
func TestRegisterHeadings(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "process", "08-open-questions.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("a heading with no title is a violation", func(t *testing.T) {
		// `## Q-ARCH-013` alone: the parser splits it into id "Q-ARCH" and title
		// "013", so two such questions collide on one gate.
		v, err := CheckRegisterHeadings(write(t, "## Q-ARCH-013\n\nSome body.\n"))
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0].Rule != "malformed-question-heading" {
			t.Fatalf("got %v, want one malformed-question-heading", v)
		}
	})

	t.Run("a well-formed heading passes", func(t *testing.T) {
		if v, _ := CheckRegisterHeadings(write(t, "## Q-ARCH-013 — Should superseded documents stay?\n")); len(v) != 0 {
			t.Fatalf("got %v, want none", v)
		}
	})

	t.Run("an en-dash or hyphen separator both pass", func(t *testing.T) {
		if v, _ := CheckRegisterHeadings(write(t, "## Q-A-1 - plain hyphen title\n## Q-A-2 — en dash title\n")); len(v) != 0 {
			t.Fatalf("got %v, want none — the separator is not the point, the title is", v)
		}
	})

	t.Run("a duplicate id is a violation", func(t *testing.T) {
		// The RQ-265 failure: two headings claiming one id, and which one the
		// extractor picks — including whether it looks resolved — is arbitrary.
		v, _ := CheckRegisterHeadings(write(t, "## Q-A-1 — first\n## Q-A-1 — second, RESOLVED\n"))
		if len(v) != 1 || v[0].Rule != "duplicate-question-heading" {
			t.Fatalf("got %v, want one duplicate-question-heading", v)
		}
	})

	t.Run("no register file is not a violation", func(t *testing.T) {
		if v, err := CheckRegisterHeadings(t.TempDir()); err != nil || len(v) != 0 {
			t.Fatalf("got %v, %v — a workspace need not have a register", v, err)
		}
	})
}
