package rdd

// REQ-CROSS-221 — focused lower evidence, clause-mapped to
// epics/EPIC-CLI-003-ledger-import/specs/requirements.md §221.
// RED first: these run against the fidelity stub and fail on behavior.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// fidelityFixture writes a minimal corpus that carries one instance of every
// measured loss family, plus deliberate hygiene drift.
func fidelityFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	longNote := strings.Repeat("x", 340) // over the 300-rune note-citation cap

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Priority column is unrecognized by the extractor; Totals is wrong on
	// purpose (claims a DONE that does not exist); REQ-XX-002 is OBSOLETE
	// (skipped by the op path today); REQ-XX-003 has no detail block.
	write("tasks/XX-REQUIREMENTS.md", `# REQUIREMENTS — XX (fixture)

## Dashboard — XX
Totals: 1 DONE · 2 PROPOSED

| ID | Title | Stage | Status | Priority | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|---|
| REQ-XX-001 | First behavior | MVP | PROPOSED | must | UR-XX-001 · UR-XX-002 | doc-a | t.go | c.go |
| REQ-XX-002 | Dead end | MVP | OBSOLETE | — | UR-XX-001 | superseded by REQ-XX-001 | — | — |
| REQ-XX-003 | Blockless | MVP | PROPOSED | should | UR-XX-001 | doc-b | — | — |

### REQ-XX-001 — First behavior

- **Status:** PROPOSED · **Stage:** MVP · **Priority:** must · **Owner:** cli
- **Raised-by:** a long provenance story the op path drops entirely.
- **Statement:** It does the thing.
- **Acceptance criteria:**
  - GIVEN x WHEN y THEN z
- **Tests:**
  - `+"`TEST:t.go:TestX`"+` — `+longNote+`
- **Code:** c.go
`)

	write("WORKLIST.md", `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-XX-001 | — | UR-XX-001 | SCN-XX-001 | REQ-XX-001 | T1 | — | — | PROPOSED | — | — |

## Work Rows

| Task / slice | Epic | Epic record / task file | User requirement | Acceptance scenario | System requirement | Scope | Status | Lower test evidence | Upper BDD/E2E evidence | Code reference | Notes |
|---|---|---|---|---|---|---|---|---|---|---|---|
| TASK-XX-1 | EPIC-XX-001 | — | UR-XX-001 | SCN-XX-001 | REQ-XX-001 | cli | IN_PROGRESS | — | — | — | — |

## Blocked / Deferred

| Item | Status | Reason | Required decision | Owner |
|---|---|---|---|---|
| DEF-XX-1 | DEFERRED | later | OQ-XX-1 | me |
`)

	write("BACKLOG.md", `# Backlog

| Discovery | Notes | Tracked as / suggested route |
|---|---|---|
| **A discovery** (fixture) | notes | NEW |
`)

	write("process/gap-register.md", `# Gap register

| Gap | Affected | Route |
|---|---|---|
| GAP-XX-1 | REQ-XX-001 | open |
`)

	write("process/08-open-questions.md", `# Open questions

| ID | Question | Owner | Blocking phase | See also |
|---|---|---|---|---|
| OQ-XX-1 | Is this measured? | me | none | — |
`)

	// Derived projection: excluded by design (D2), must not block.
	write("PROGRESS.md", "# Progress\n\nGenerated. Do not hand-edit.\n")

	return root
}

func buildFixtureReport(t *testing.T) FidelityReport {
	t.Helper()
	root := fidelityFixture(t)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	return BuildFidelityReport(root, m, data, ops)
}

func findLoss(losses []FidelityLoss, id, field string) *FidelityLoss {
	for i := range losses {
		if losses[i].RecordID == id && losses[i].Field == field {
			return &losses[i]
		}
	}
	return nil
}

func countGroup(t *testing.T, r FidelityReport, group string) FidelityCount {
	t.Helper()
	for _, c := range r.Counts {
		if c.Group == group {
			return c
		}
	}
	t.Fatalf("no count group %q in %+v", group, r.Counts)
	return FidelityCount{}
}

// §221.1 — counts reconcile per group, both directions. Since the
// REQ-CROSS-222 capture closure the fixture survives the op path whole:
// every row (including the OBSOLETE one) reaches an op.
func TestFidelityCountsReconcileAndItemize(t *testing.T) {
	r := buildFixtureReport(t)

	ledger := countGroup(t, r, "tasks/XX-REQUIREMENTS.md")
	if ledger.Rows != 3 || ledger.Ops != 3 {
		t.Fatalf("ledger rows/ops = %d/%d, want 3/3 (OBSOLETE rows emit since REQ-CROSS-222)", ledger.Rows, ledger.Ops)
	}
	if len(ledger.MissingFromOps) != 0 {
		t.Fatalf("MissingFromOps = %v, want none", ledger.MissingFromOps)
	}

	oq := countGroup(t, r, "open-questions")
	if oq.Rows != 1 || oq.Ops != 1 {
		t.Fatalf("open-questions rows/ops = %d/%d, want 1/1 (OQs flow as question gates)", oq.Rows, oq.Ops)
	}
}

// §221.2 — per-record loss detectors measure the emitted payloads: what the
// REQ-CROSS-222 capture carries is no longer claimed as a loss (the stripped-
// payload arm in capture_test.go proves the detectors still fire on absence).
func TestFidelityNamesMeasuredLossesPerRecord(t *testing.T) {
	r := buildFixtureReport(t)

	for _, closed := range []struct{ id, field string }{
		{"REQ-XX-001", "ur-relations"},
		{"REQ-XX-001", "prose:Raised-by"},
		{"REQ-XX-001", "note-citation"},
		{"REQ-XX-001", "source-citations"},
		{"tasks/XX-REQUIREMENTS.md", "column:Priority"},
		{"REQ-XX-002", "row"},
	} {
		if l := findLoss(r.Losses, closed.id, closed.field); l != nil {
			t.Fatalf("loss %s|%s still claimed after the capture carries it: %+v", closed.id, closed.field, l)
		}
	}
}

// §221.2 — the inventory universe covers WORKLIST rollup rows, the extra
// WORKLIST tables, BACKLOG discovery rows, and gap-register rows. Since the
// REQ-CROSS-223 homes, the epic lifecycle and backlog/gap rows reconcile as
// carried; the extra WORKLIST tables remain honestly lost.
func TestFidelityUniverseCoversWorklistBacklogGapRegister(t *testing.T) {
	r := buildFixtureReport(t)

	if l := findLoss(r.Losses, "EPIC-XX-001", "overall-status"); l != nil {
		t.Fatalf("overall-status loss still claimed after process_status is carried: %+v", l)
	}
	// REQ-CROSS-264 re-points this population: a TASK-led Work-Rows row now has
	// a typed carrier, so its loss CLOSES — by carrier, not by the byte
	// archive's whole-file suppression, which proves preservation and not
	// queryability.
	if l := findLoss(r.Losses, "TASK-XX-1", "worklist-row"); l != nil {
		t.Fatalf("a TASK-led Work-Rows row is carried by the tasks carrier now: %+v", l)
	}
	if l := findLoss(r.Losses, "DEF-XX-1", "worklist-row"); l == nil || l.Category != LossLost {
		t.Fatalf("WORKLIST Blocked/Deferred row not named as lost: %+v", l)
	}
	if bl := countGroup(t, r, "backlog"); bl.Rows != 1 || bl.Ops != 1 {
		t.Fatalf("backlog rows must reconcile as a count group, got %+v", bl)
	}
	if gp := countGroup(t, r, gapRecordsGroup); gp.Rows != 1 || gp.Ops != 1 {
		t.Fatalf("gap-register rows must reconcile as a count group, got %+v", gp)
	}
}

// §221.3 + §221.4 — excluded-by-design entries exist and never block.
func TestFidelityExcludedByDesignDoesNotBlock(t *testing.T) {
	r := buildFixtureReport(t)

	l := findLoss(r.Losses, "PROGRESS.md", "derived-projection")
	if l == nil || l.Category != LossExcluded {
		t.Fatalf("PROGRESS.md not named as excluded by design: %+v", l)
	}
	for _, b := range r.Blocking(nil) {
		if b.Category == LossExcluded {
			t.Fatalf("excluded-by-design entry blocks: %+v", b)
		}
	}
	if len(r.Blocking(nil)) == 0 {
		t.Fatal("fixture carries real losses; Blocking(nil) must be non-empty")
	}
}

// §221.4 — residue accepted by an applied human gate answer stops blocking;
// everything else still does.
func TestFidelityAcceptedResidueDoesNotBlock(t *testing.T) {
	r := buildFixtureReport(t)

	target := findLoss(r.Losses, "DEF-XX-1", "worklist-row")
	if target == nil {
		t.Fatal("fixture must produce the Blocked/Deferred worklist-row loss")
	}
	accepted := map[string]bool{target.Key(): true}
	for _, b := range r.Blocking(accepted) {
		if b.Key() == target.Key() {
			t.Fatalf("accepted residue still blocks: %+v", b)
		}
	}
	if len(r.Blocking(accepted)) >= len(r.Blocking(nil)) {
		t.Fatal("accepting one residue entry must shrink the blocking set by exactly it")
	}
}

// §221.5 — hygiene drift is flagged with file and line, never repaired.
func TestFidelityHygieneFlagsWithLocation(t *testing.T) {
	root := fidelityFixture(t)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")

	before, _ := os.ReadFile(filepath.Join(root, "tasks/XX-REQUIREMENTS.md"))
	r := BuildFidelityReport(root, m, data, ops)
	after, _ := os.ReadFile(filepath.Join(root, "tasks/XX-REQUIREMENTS.md"))
	if string(before) != string(after) {
		t.Fatal("the report must write nothing — the ledger changed")
	}

	var totalsFlag, blocklessFlag *FidelityHygiene
	for i := range r.Hygiene {
		h := &r.Hygiene[i]
		if h.File == "tasks/XX-REQUIREMENTS.md" && strings.Contains(h.Detail, "Totals") {
			totalsFlag = h
		}
		if strings.Contains(h.Detail, "REQ-XX-003") {
			blocklessFlag = h
		}
	}
	if totalsFlag == nil || totalsFlag.Line == 0 {
		t.Fatalf("Totals-line drift not flagged with a location: %+v", r.Hygiene)
	}
	if blocklessFlag == nil {
		t.Fatalf("dashboard row without a detail block (REQ-XX-003) not flagged: %+v", r.Hygiene)
	}
}
