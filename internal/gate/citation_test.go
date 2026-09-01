package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-165: a path cited in a **Code:**/**Tests:** field resolves.
//
// Verified by hand on RUN:2026-08-14 — 546 such citations, all resolving once
// two things are right, and both were learned by getting them wrong first.
func TestCitedPathsResolve(t *testing.T) {
	setup := func(t *testing.T, ledger string, files ...string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tasks", "ABC-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, p := range files {
			full := filepath.Join(root, p)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}

	t.Run("an absent path is a violation", func(t *testing.T) {
		root := setup(t, "- **Code:** `gone.ex`\n")
		v, err := CheckCitedPaths(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0].Rule != "unresolved-citation" {
			t.Fatalf("got %v, want one unresolved-citation", v)
		}
	})

	t.Run("a path resolves by suffix, not only exactly", func(t *testing.T) {
		root := setup(t, "- **Code:** `core/thing.ex`\n", "apps/core/lib/core/thing.ex")
		if v, _ := CheckCitedPaths(root); len(v) != 0 {
			t.Fatalf("got %v — ledgers cite the meaningful tail, not the full path", v)
		}
	})

	t.Run("a gitignored but real file resolves", func(t *testing.T) {
		// .modernpath/auth.json holds credentials and is gitignored, so it is
		// absent from `git ls-files` while being genuinely present. Resolving
		// against the index alone reported this correct citation as broken.
		root := setup(t, "- **Code:** `auth.json`\n", ".modernpath/auth.json")
		if v, _ := CheckCitedPaths(root); len(v) != 0 {
			t.Fatalf("got %v — gitignored is not absent", v)
		}
	})

	t.Run("prose is out of scope", func(t *testing.T) {
		// Template rows and gap statements deliberately name paths that must not
		// exist: "GIVEN a new `src/organisms/Foo.tsx`", "there is no `x.py`".
		// All three such cases in the real corpus sit in a Statement or a GIVEN
		// clause, never in a Code:/Tests: field — which is why the field is the
		// population.
		root := setup(t, "- **Statement:** a template row citing `domain/user.ts`\n  - GIVEN a new `src/organisms/Foo.tsx` THEN it fails\n")
		if v, _ := CheckCitedPaths(root); len(v) != 0 {
			t.Fatalf("got %v — illustrative paths are not citations", v)
		}
	})
}
