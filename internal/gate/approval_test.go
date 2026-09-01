package gate

import (
	"os"
	"path/filepath"
	"strings"
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

func TestProcessGateBaselineMigratesOffTheAgentOwnedPath(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, legacyBaselinePath)
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatal(err)
	}
	const body = "# accepted debt\nstatus-hygiene|tasks/X.md|REQ-X\n"
	if err := os.WriteFile(legacy, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := MigrateLegacyBaseline(root); err != nil {
		t.Fatal(err)
	}
	current := filepath.Join(root, baselinePath)
	got, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("agent-neutral baseline was not written: %v", err)
	}
	if string(got) != body {
		t.Fatalf("baseline changed during migration:\n%s", got)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("install migration removed the legacy project file: %v", err)
	}
	baseline, err := LoadBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if !baseline["status-hygiene|tasks/X.md|REQ-X"] {
		t.Fatalf("migrated baseline was not loaded: %v", baseline)
	}
}

// REQ-CROSS-115 — the work-list cell is judged by the same rule as the record.
//
// The doc comment on CheckApprovals promises the judgment is "delegated to
// rdd.ApprovalLineOf rather than reimplemented, because two answers to one rule
// inevitably drift apart". The cell short-circuit reimplemented it as a bare
// USER:-tag match, so the spec-versus-completion rule was enforced only on the
// records — the minority path, by the check's own comment.
func TestASpecApprovalInTheWorklistCellIsNotACompletionApproval(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-007 | [r](epics/EPIC-X-007.md) | u | s | s | t | U | L | **DONE** | SPEC-APPROVE-EPIC-X-007 (approved `USER:2026-08-06`) | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-007.md"), []byte(
		"# EPIC-X-007 — specified, not signed off\n\n## Approval\n_pending._\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("a DONE epic whose only approval is its specification gate must violate, got %d: %v", len(found), found)
	}
}

// The same rule, the other decision: a cell that records the gate as still open.
func TestAWithheldWorklistCellIsNotACompletionApproval(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-008 | [r](epics/EPIC-X-008.md) | u | s | s | t | U | L | **DONE** | pending — awaiting confirm (USER:2026-07-16) | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-008.md"), []byte(
		"# EPIC-X-008 — awaiting\n\n## Approval\n_pending._\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("a DONE epic whose cell says the gate is still open must violate, got %d: %v", len(found), found)
	}
}

// Guards, expected green from the start: a genuine approval in the cell still
// passes without the record being read, and a cell that fails still falls
// through to a record that carries the real approval.
func TestAGrantedWorklistCellStillPasses(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-009 | [r](epics/EPIC-X-009.md) | u | s | s | t | U | L | **DONE** | approved `USER:2026-08-06` after the walkthrough | n |\n"), 0o644)
	// deliberately absent: a satisfied cell must not need the record
	if found, err := CheckApprovals(root); err != nil || len(found) != 0 {
		t.Fatalf("a granted cell must pass on its own, got %v (err %v)", found, err)
	}
}

// REQ-CROSS-167 criterion 1: a sourced Approval cell satisfies the gate "with
// or without a record link".
//
// It did not, until `RUN:2026-09-01`. The missing-link branch appended a
// violation and CONTINUED, so the cell was never read — the same shape the row
// describes as fixed, repaired for the link-present path only. On the live
// corpus that produced five false reports against rows carrying
// `approved USER:…`, found by turning on TestRealWorkspaceApprovals.
//
// This test previously pinned the defect and said it would be rewritten when the
// fix landed. This is that rewrite.
func TestASourcedCellWithoutARecordLinkSatisfiesTheGate(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-101 | no link here | u | s | s | t | U | L | **DONE** | approved `USER:2026-08-06` | n |\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a sourced approval in the cell satisfies the gate with or without a record link, got %v", found)
	}
}

// The other direction, so the fix cannot be satisfied by accepting everything: a
// DONE row with NO approval anywhere and no record link is still a violation.
func TestADoneRowWithNoApprovalAnywhereIsStillFlagged(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-102 | no link here | u | s | s | t | U | L | **DONE** | — | n |\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || !strings.Contains(found[0].Detail, "links no epic record") {
		t.Fatalf("an unapproved DONE row must still be flagged, got %v", found)
	}
}

// REQ-CROSS-167 criterion 2: an approval written inline in the Overall cell —
// "**DONE — approved `USER:2026-07-21`**" — satisfies the gate.
//
// It did not: only cells[10] was consulted, and cells[9] was read for the word
// DONE and nothing else. EPIC-INFRA-011 records its approval exactly this way.
func TestAnInlineApprovalInTheOverallCellSatisfiesTheGate(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-103 | [r](epics/EPIC-X-103.md) | u | s | s | t | U | L | "+
			"**DONE — approved `USER:2026-07-21`** | — | n |\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("an inline approval in the Overall cell satisfies the gate, got %v", found)
	}
}

// And the Overall cell is judged by the same rule as the record, not by a bare
// USER: tag — a tag proves someone wrote a date, not that they approved.
func TestABareDateInTheOverallCellIsNotAnApproval(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-104 | no link here | u | s | s | t | U | L | "+
			"**DONE — landed `USER:2026-07-21`** | — | n |\n"), 0o644)

	found, err := CheckApprovals(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("a date without an approval word must not satisfy the gate, got %v", found)
	}
}

func TestAFailingCellFallsThroughToTheRecord(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "epics"), 0o755)
	os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(
		"| EPIC-X-010 | [r](epics/EPIC-X-010.md) | u | s | s | t | U | L | **DONE** | SPEC-APPROVE-EPIC-X-010 (approved `USER:2026-08-06`) | n |\n"), 0o644)
	os.WriteFile(filepath.Join(root, "epics", "EPIC-X-010.md"), []byte(
		"# EPIC-X-010 — signed off in the record\n\n## Approval\n"+
			"SPEC-APPROVE-EPIC-X-010: approved `USER:2026-08-06`. APPROVE-EPIC-X-010: approved `USER:2026-08-06` (\"approve\" after the summary).\n"), 0o644)

	if found, err := CheckApprovals(root); err != nil || len(found) != 0 {
		t.Fatalf("the record carries a real completion approval, got %v (err %v)", found, err)
	}
}

// REQ-CROSS-115 — where both baselines exist, only the primary is read.
//
// Both files are tracked in this workspace, and the legacy one is the obvious
// place to look: it is the path the gate was adopted at, and the one an editor
// finds first under .claude/. Pruning it alone changes nothing, silently — the
// suppression the edit meant to lift stays in force, and `check` prints the
// same reassuring count either way.
func TestOnlyThePrimaryBaselineIsReadWhenBothExist(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".modernpath", "rdd"), 0o755)
	os.MkdirAll(filepath.Join(root, ".claude"), 0o755)
	os.WriteFile(filepath.Join(root, baselinePath), []byte("approval-before-done|WORKLIST.md|EPIC-PRIMARY\n"), 0o644)
	os.WriteFile(filepath.Join(root, legacyBaselinePath), []byte("approval-before-done|WORKLIST.md|EPIC-LEGACY\n"), 0o644)

	baseline, err := LoadBaseline(root)
	if err != nil {
		t.Fatal(err)
	}
	if !baseline["approval-before-done|WORKLIST.md|EPIC-PRIMARY"] {
		t.Fatalf("the primary baseline was not read: %v", baseline)
	}
	if baseline["approval-before-done|WORKLIST.md|EPIC-LEGACY"] {
		t.Fatalf("the legacy baseline was merged in, so pruning either file would appear to work: %v", baseline)
	}
}
