package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-163: a row past PROPOSED must carry a detail block.
//
// `rdd-ledger`'s criteria-first rule — "no requirement enters IN_PROGRESS
// without a detail block carrying GIVEN/WHEN/THEN criteria" — was checked by
// nothing. Swept by hand on RUN:2026-08-14 it held across 948 rows, with four
// exceptions, all OBSOLETE. A rule that holds today and is enforced by nobody is
// one careless row from being false, and the failure is silent: the row syncs
// with an empty description and reads as a bare title everywhere it appears.
//
// Two exemptions are deliberate, and both were derived from the sweep rather
// than assumed: PROPOSED means "not yet specified", and OBSOLETE means
// "superseded" — neither owes criteria.
func TestBlockRequiredPastProposed(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "tasks", "ABC-REQUIREMENTS.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	const header = "## Dashboard — ABC\n\n| ID | Title | Stage | Status | Source | Tests | Code |\n|----|-------|-------|--------|--------|-------|------|\n"

	t.Run("IN_REVIEW with no block is a violation", func(t *testing.T) {
		root := write(t, header+"| REQ-ABC-001 | A thing | MVP | IN_REVIEW | | | |\n")
		v, err := CheckBlockRequired(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(v) != 1 || v[0].Subject != "REQ-ABC-001" {
			t.Fatalf("got %v, want one violation for REQ-ABC-001", v)
		}
		if v[0].Rule != "missing-detail-block" {
			t.Fatalf("rule = %q", v[0].Rule)
		}
	})

	t.Run("PROPOSED with no block is fine", func(t *testing.T) {
		root := write(t, header+"| REQ-ABC-002 | A thing | MVP | PROPOSED | | | |\n")
		if v, _ := CheckBlockRequired(root); len(v) != 0 {
			t.Fatalf("got %v, want none — PROPOSED is not yet specified", v)
		}
	})

	t.Run("OBSOLETE with no block is fine", func(t *testing.T) {
		root := write(t, header+"| REQ-ABC-003 | ~~A thing~~ | — | OBSOLETE | | superseded by X | | |\n")
		if v, _ := CheckBlockRequired(root); len(v) != 0 {
			t.Fatalf("got %v, want none — a superseded row owes no criteria", v)
		}
	})

	t.Run("DONE with a block is fine", func(t *testing.T) {
		root := write(t, header+"| REQ-ABC-004 | A thing | MVP | DONE | | | |\n\n### REQ-ABC-004 — A thing\n- **Status:** DONE\n")
		if v, _ := CheckBlockRequired(root); len(v) != 0 {
			t.Fatalf("got %v, want none", v)
		}
	})
}
