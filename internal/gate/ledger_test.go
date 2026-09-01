package gate

import (
	"fmt"
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

// The baseline distinguishes individual subjects,
// so a second broken status in an already-baselined ledger still blocks. One
// per-file violation let a baselined ledger drift arbitrarily further, forever.
func TestLedgerHygieneReportsEachMismatchedStatusSeparately(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	// DONE and READY both disagree with the Totals line
	ledger := `## Dashboard — USR
Totals: 3 DONE · 0 READY · 0 PROPOSED

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-USR-001 | a | MVP | DONE | x | t | c |
| REQ-USR-002 | b | MVP | READY | x | — | — |
`
	os.WriteFile(filepath.Join(root, "tasks", "USR-REQUIREMENTS.md"), []byte(ledger), 0o644)

	found, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 2 {
		t.Fatalf("two mismatched statuses must be two violations, got %d: %v", len(found), found)
	}
	if found[0].Subject == found[1].Subject {
		t.Fatalf("violations must carry distinct subjects for baselining, both got %q", found[0].Subject)
	}
	if found[0].Key() == found[1].Key() {
		t.Fatal("baseline keys must differ, or accepting one mismatch accepts them all")
	}
}

// Keying
// status-hygiene per status means a *second* broken status blocks — but the same
// status could go on worsening inside its own baselined key forever. Accepting
// "DONE: rows say 2, Totals says 1" must accept exactly that gap and nothing
// wider.
func TestABaselinedStatusMismatchStillBlocksWhenItWorsens(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	path := filepath.Join(root, "tasks", "USR-REQUIREMENTS.md")

	ledger := func(doneRows int) string {
		body := "## Dashboard — USR\nTotals: 1 DONE\n\n" +
			"| ID | Title | Stage | Status | Source | Tests | Code |\n" +
			"|----|-------|-------|--------|--------|-------|------|\n"
		for i := 1; i <= doneRows; i++ {
			body += fmt.Sprintf("| REQ-USR-%03d | a | MVP | DONE | x | t | c |\n", i)
		}
		return body
	}

	// accept the known debt: two DONE rows against a Totals line claiming one
	os.WriteFile(path, []byte(ledger(2)), 0o644)
	accepted, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(accepted) != 1 {
		t.Fatalf("fixture must produce exactly the one mismatch to baseline, got %v", accepted)
	}
	if err := WriteBaseline(root, accepted); err != nil {
		t.Fatal(err)
	}
	baseline, err := LoadBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if left := Unbaselined(accepted, baseline); len(left) != 0 {
		t.Fatalf("the baselined mismatch must be accepted as it stands, got %v", left)
	}

	// the same status drifts further: three rows against the same claim of one
	os.WriteFile(path, []byte(ledger(3)), 0o644)
	worse, err := CheckLedgers(root)
	if err != nil {
		t.Fatal(err)
	}
	if left := Unbaselined(worse, baseline); len(left) != 1 {
		t.Fatalf("a baselined mismatch that grows must block, got %v", left)
	}
}

// A deliberate trade: the baseline is sensitive to the size of the debt, so a
// mismatch that shrinks also blocks until the baseline is rewritten. Pinned so
// nobody "fixes" it as a bug.
func TestABaselinedStatusMismatchAlsoBlocksWhenItImproves(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tasks"), 0o755)
	path := filepath.Join(root, "tasks", "USR-REQUIREMENTS.md")
	head := "## Dashboard — USR\nTotals: 1 DONE\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n" +
		"|----|-------|-------|--------|--------|-------|------|\n"
	row := "| REQ-USR-%03d | a | MVP | DONE | x | t | c |\n"

	os.WriteFile(path, []byte(head+fmt.Sprintf(row, 1)+fmt.Sprintf(row, 2)+fmt.Sprintf(row, 3)), 0o644)
	accepted, _ := CheckLedgers(root)
	if err := WriteBaseline(root, accepted); err != nil {
		t.Fatal(err)
	}
	baseline, _ := LoadBaseline(root)

	// one row removed — closer to the truth, still not the truth
	os.WriteFile(path, []byte(head+fmt.Sprintf(row, 1)+fmt.Sprintf(row, 2)), 0o644)
	better, _ := CheckLedgers(root)
	if left := Unbaselined(better, baseline); len(left) != 1 {
		t.Fatalf("any movement of a baselined mismatch blocks; that is the accepted trade, got %v", left)
	}

	// and the fix itself is clean: no rows left over, nothing to re-baseline
	os.WriteFile(path, []byte(head+fmt.Sprintf(row, 1)), 0o644)
	fixed, _ := CheckLedgers(root)
	if len(Unbaselined(fixed, baseline)) != 0 {
		t.Fatalf("a corrected ledger must pass, got %v", fixed)
	}
}

// approval-before-done has no magnitude: its subject already names the whole
// violation, so its baseline keys must not move under this change.
func TestApprovalKeysAreUnchangedByStatusHygieneMagnitude(t *testing.T) {
	v := Violation{Rule: "approval-before-done", File: "WORKLIST.md", Subject: "EPIC-FE-071"}
	if got, want := v.Key(), "approval-before-done\tWORKLIST.md\tEPIC-FE-071"; got != want {
		t.Fatalf("existing baselines would be invalidated:\n got %q\nwant %q", got, want)
	}
}
