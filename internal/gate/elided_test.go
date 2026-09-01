package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-148: an elided citation resolves to nothing and looks like a
// citation. 68 of them were found across one workspace's docs, epics and
// ledgers on RUN:2026-08-14 — the single most common form of citation rot, and
// invisible to a reader and to a suffix-matching resolver alike.
func TestCheckElidedCitations(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"docs", "epics", "tasks"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write(t, root, "docs/ok.md", "Cite: `CODE:services/app/main.py:12`. Also `DOC:AGENTS.md`.\n")
	if v, err := CheckElidedCitations(root); err != nil || len(v) != 0 {
		t.Fatalf("clean: got %d %v err %v", len(v), v, err)
	}

	// A commit gate must not fail on prose. An ellipsis inside a temp path, a URL
	// or a directory is not a citation — RUN:2026-08-14 found /var/folders/.../
	// in a review log and failed the whole workspace on it.
	write(t, root, "docs/prose.md", "Staged under `/var/folders/.../modernpath_repos/` at runtime.\n"+
		"See https://example.com/a/.../b for the upstream note.\n")
	if v, err := CheckElidedCitations(root); err != nil || len(v) != 0 {
		t.Fatalf("prose must not fail: got %d %v err %v", len(v), v, err)
	}

	write(t, root, "docs/bad.md", "Cite: `CODE:.../ai/proxy.ex`.\n")
	write(t, root, "epics/E.md", "| SRC-1 | CODE | CODE:apps/storage/.../user_requirement.ex:51 | note |\n")
	v, err := CheckElidedCitations(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(v) != 2 {
		t.Fatalf("got %d violations, want 2: %v", len(v), v)
	}
	for _, x := range v {
		if x.Rule != "elided-citation" {
			t.Errorf("rule = %q", x.Rule)
		}
	}
}
