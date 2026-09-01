// REQ-CROSS-098 slice 1 (EPIC-CLI-001 T11): `modernpath check` printed
// "✓ process checks pass" from ANY directory — six measured vacuous passes
// including an empty dir (RUN:2026-08-13, re-confirmed RUN:2026-08-18). A
// gate that found nothing to check has not passed: it must name the root it
// evaluated and report "no records found" as its own non-passing outcome.
// Root *resolution* (slice 2) stays out of scope.
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckRefusesToPassOverNothing(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	err := runCheck(checkCmd, nil)
	if err == nil {
		t.Fatal("an empty directory must not pass — nothing was checked")
	}
	if !strings.Contains(err.Error(), "no process records") {
		t.Fatalf("the outcome must be named, got: %v", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("the evaluated root must be named in the outcome, got: %v", err)
	}
}

func TestCheckNamesTheRootItEvaluated(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := "# L\n\nTotals: 1 READY\n\n| ID | Title | Stage | Status | UR | Source | Tests | Code |\n|---|---|---|---|---|---|---|---|\n| REQ-SBX-001 | A thing | MVP | READY | | DOC:x | — | — |\n"
	if err := os.WriteFile(filepath.Join(dir, "tasks", "SBX-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureCLIOutput(t)
	if err := runCheck(checkCmd, nil); err != nil {
		t.Fatalf("a repo with a clean ledger must pass: %v", err)
	}
	if !strings.Contains(out(), dir) {
		t.Fatalf("the evaluated root must be printed, got:\n%s", out())
	}
}
