package rdd

// EPIC-SYNC-009 (REQ-CROSS-027, SCN-AS-003, D-AS-2): the quiescence coherence
// gate — a mid-edit workspace violates the three-place Totals invariant by
// construction (CLAUDE.md §5 #8), so Totals⇄rows disagreement means "do not
// sync". Coherent ledgers pass; missing Totals lines are legacy-tolerated.

import (
	"os"
	"path/filepath"
	"testing"
)

func writeLedger(t *testing.T, root, name, content string) {
	t.Helper()
	dir := filepath.Join(root, "tasks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const coherentLedger = `# TST ledger
Totals: 1 DONE · 1 IN_PROGRESS · 0 IN_REVIEW · 0 READY · 0 PROPOSED · 0 DEFERRED · 0 BLOCKED

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-TST-001 | One | MVP | DONE | x | — | — |
| REQ-TST-002 | Two | MVP | IN_PROGRESS | x | — | — |
`

func TestCoherencePassesOnConsistentLedger(t *testing.T) {
	root := t.TempDir()
	writeLedger(t, root, "TST-REQUIREMENTS.md", coherentLedger)

	problems := CheckLedgerCoherence(root)
	if len(problems) != 0 {
		t.Fatalf("expected coherent, got %v", problems)
	}
}

func TestCoherenceFailsOnTotalsRowDrift(t *testing.T) {
	root := t.TempDir()
	// Totals claims 2 DONE but rows carry 1 — the mid-edit signature
	writeLedger(t, root, "TST-REQUIREMENTS.md",
		"Totals: 2 DONE · 1 IN_PROGRESS · 0 READY\n\n| REQ-TST-001 | One | MVP | DONE | x | — | — |\n| REQ-TST-002 | Two | MVP | IN_PROGRESS | x | — | — |\n")

	problems := CheckLedgerCoherence(root)
	if len(problems) == 0 {
		t.Fatal("expected incoherence on Totals drift")
	}
}

func TestCoherenceToleratesMissingTotals(t *testing.T) {
	root := t.TempDir()
	writeLedger(t, root, "TST-REQUIREMENTS.md",
		"| REQ-TST-001 | One | MVP | DONE | x | — | — |\n")

	if problems := CheckLedgerCoherence(root); len(problems) != 0 {
		t.Fatalf("legacy ledger without Totals must pass, got %v", problems)
	}
}
