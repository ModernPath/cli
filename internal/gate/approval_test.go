package gate

import (
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-030: an epic may not be DONE without a recorded human approval.
// EPIC-MC-001 sat "awaiting approval" for a day with no gate ever opened, and
// nothing noticed — the claim and the evidence were never compared.
func TestApprovalGateCatchesDoneWithoutApproval(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| Epic | Record | UR | SCN | SR | Tasks | Upper | Lower | Overall | Approval | Notes |\n"+
			"|---|---|---|---|---|---|---|---|---|---|---|\n"+
			"| EPIC-X-001 | [r](epics/EPIC-X-001.md) | u | s | s | t | UPPER_VALIDATED | LOWER_VERIFIED | **DONE** | — | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-001.md"), []byte(
		"# EPIC-X-001 — a thing\n\n## Approval\n_pending — awaiting review._\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected one violation, got %d: %v", len(found), found)
	}
	if found[0].Rule != "approval-before-done" {
		t.Errorf("wrong rule id: %s", found[0].Rule)
	}
}

func TestApprovalGatePassesWhenApprovalIsRecordedWithASource(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-001 | [r](epics/EPIC-X-001.md) | u | s | s | t | U | L | **DONE** | approved | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-001.md"), []byte(
		"# EPIC-X-001 — a thing\n\n## Approval\n**APPROVED** — evidence reviewed · USER:2026-08-08\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("an approved epic must pass, got %v", found)
	}
}

// An approval with no USER: source is a claim, not an approval — the same
// honesty rule the sync extractor already applies.
func TestApprovalGateRejectsAnUnsourcedApproval(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-001 | [r](epics/EPIC-X-001.md) | u | s | s | t | U | L | **DONE** | ok | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-001.md"), []byte(
		"# EPIC-X-001 — a thing\n\n## Approval\n**APPROVED** — looks good to me\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("an unsourced approval must not satisfy the gate, got %v", found)
	}
}

// Epics that are not DONE are none of this gate's business, and a repository
// with no WORKLIST must stay silent.
func TestApprovalGateIgnoresUnfinishedEpicsAndMissingWorklist(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-002 | [r](epics/EPIC-X-002.md) | u | s | s | t | U | L | **IN_PROGRESS** | — | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-002.md"), []byte("# EPIC-X-002 — wip\n"), 0o644)
	if found, err := CheckApprovals(root); err != nil || len(found) != 0 {
		t.Fatalf("in-progress epic must not be reported: %v %v", found, err)
	}
	if found, err := CheckApprovals(t.TempDir()); err != nil || len(found) != 0 {
		t.Fatalf("no WORKLIST means nothing to check: %v %v", found, err)
	}
}

// A work-list row may reference its record as a markdown link or as a plain
// backticked path — both are in use. Recognising only the link shape reported
// EPIC-ARCH-001 as record-less when its record was on disk all along, which is
// the worst kind of gate output: a true rule, a false accusation.
func TestApprovalGateAcceptsABacktickedRecordPath(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-001 | `epics/EPIC-X-001.md` | u | s | s | t | U | L | **DONE** | — | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-001.md"), []byte(
		"# EPIC-X-001 — a thing\n\n## Approval\n**APPROVED** — reviewed · USER:2026-08-08\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a backticked record path must be recognised, got %v", found)
	}
}
