package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-134: a state directory's README names every document in it.
//
// Three separate indexes were found stale in three days (RUN:2026-08-14):
// tasks/README.md listed 9 of 14 ledgers, process/README.md 4 of 7 files
// including the release registry, and PROCESS.md §9 four of eight skills. All
// rotted the same way — an index rots in the direction nothing contradicts,
// because no reader misses a row that is absent.
//
// Scoped to FLAT state directories. epics/ is excluded on purpose: its README is
// a convention document, not a catalogue of 160 records.
func TestStateDirIndexesAreComplete(t *testing.T) {
	setup := func(t *testing.T, readme string, files ...string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "process", "README.md"), []byte(readme), 0o644); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(root, "process", f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return root
	}

	t.Run("an unlisted document is a violation", func(t *testing.T) {
		root := setup(t, "| `08-open-questions.md` | questions |\n", "08-open-questions.md", "releases.md")
		v, err := CheckStateIndexes(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0].Subject != "process/releases.md" {
			t.Fatalf("got %v, want releases.md unlisted", v)
		}
		if v[0].Rule != "unlisted-state-file" {
			t.Fatalf("rule = %q", v[0].Rule)
		}
	})

	t.Run("every document listed passes", func(t *testing.T) {
		root := setup(t, "| `08-open-questions.md` | q |\n| `releases.md` | r |\n", "08-open-questions.md", "releases.md")
		if v, _ := CheckStateIndexes(root); len(v) != 0 {
			t.Fatalf("got %v, want none", v)
		}
	})

	t.Run("a directory with no README is not checked", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "process", "releases.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if v, _ := CheckStateIndexes(root); len(v) != 0 {
			t.Fatalf("got %v — an index that does not exist makes no claim", v)
		}
	})

	t.Run("non-markdown files are not owed a row", func(t *testing.T) {
		root := setup(t, "| `releases.md` | r |\n", "releases.md", "handover.html")
		if v, _ := CheckStateIndexes(root); len(v) != 0 {
			t.Fatalf("got %v — the rule is about documents", v)
		}
	})
}
