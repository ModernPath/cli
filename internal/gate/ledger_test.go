package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-030: status lives in three places and a change that moves only one
// is a bug. It has been a written rule since the process began, and written
// rules are context, not configuration — this makes it a check that can fail.
func TestLedgerHygieneCatchesTotalsThatDisagreeWithTheRows(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	// two DONE rows, but the Totals line still claims three
	ledger := `# Ledger

## Dashboard — USR
Totals: 3 DONE · 1 READY · 0 PROPOSED

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-USR-001 | a | MVP | DONE | x | t | c |
| REQ-USR-002 | b | MVP | DONE | x | t | c |
| REQ-USR-003 | c | MVP | READY | x | — | — |
`
	os.WriteFile(filepath.Join(root, "tasks", "USR-REQUIREMENTS.md"), []byte(ledger), 0o644)

	found, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected one violation, got %d: %v", len(found), found)
	}
	if got := found[0].Detail; got == "" {
		t.Fatal("a violation must say what disagrees, not just that something does")
	}
	t.Logf("reported: %s — %s", found[0].File, found[0].Detail)
}

func TestLedgerHygienePassesWhenRowsAndTotalsAgree(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	ledger := `## Dashboard — USR
Totals: 2 DONE · 1 READY

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-USR-001 | a | MVP | DONE | x | t | c |
| REQ-USR-002 | b | MVP | DONE | x | t | c |
| REQ-USR-003 | c | MVP | READY | x | — | — |
`
	os.WriteFile(filepath.Join(root, "tasks", "USR-REQUIREMENTS.md"), []byte(ledger), 0o644)

	found, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("consistent ledger must pass, got %v", found)
	}
}

// A ledger with no Totals line is not a violation — some contexts are seeded
// before the dashboard exists. Reporting it would train people to ignore the gate.
func TestLedgerHygieneIgnoresLedgersWithoutTotals(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	os.WriteFile(filepath.Join(root, "tasks", "NEW-REQUIREMENTS.md"),
		[]byte("# just seeded\n\n| ID | Title | Stage | Status |\n|--|--|--|--|\n| REQ-NEW-001 | a | MVP | PROPOSED |\n"), 0o644)

	found, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a ledger without a Totals line must not be reported, got %v", found)
	}
}

// The check must be quiet in a repository that does not use the process at all,
// so it can be wired into a hook that runs everywhere.
func TestLedgerHygieneIsQuietWithoutATasksDirectory(t *testing.T) {
	found, err := CheckLedgers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("no tasks/ means nothing to check, got %v", found)
	}
}
