package rdd

// SR-SY-1402 (EPIC-SYNC-014) — the UR back-link rides the payload. Real derived
// ledgers carry no UR column; the UR derivation pass writes the parent as a
// detail-block line `- **UR:** UR-DEAL-004`. The eval (`RUN:2026-08-16`):
// with_parent=0 across all 596 requirement ops → zero derives traces → the
// whole face reads as orphans.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

func mustWriteFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const derivedLedgerFixture = `# DEAL — M&A deal execution requirements (app-web)

## Dashboard — DEAL (M&A deal execution)
Totals: 0 DONE · 2 PENDING_VERIFICATION · 0 BLOCKED · 0 PROPOSED

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-DEAL-028 | My-deals lists a user's deals | MVP | PENDING_VERIFICATION | CODE | — | my-deals-page.tsx |
| REQ-DEAL-029 | The wizard creates a deal | MVP | PENDING_VERIFICATION | CODE | — | deal-start-wizard.tsx |

## Detail

### REQ-DEAL-028 — My-deals lists a user's deals
- **Status:** PENDING_VERIFICATION · **Stage:** MVP
- **Source:** ` + "`CODE:app-web/src/components/deals/my-deals-page.tsx`" + `
- **UR:** UR-DEAL-002
- **Statement:** The deals page lists the user's deals with badges.
- **Tests:** —
- **Code:** ` + "`app-web/src/components/deals/my-deals-page.tsx`" + `

### REQ-DEAL-029 — The wizard creates a deal
- **Status:** PENDING_VERIFICATION · **Stage:** MVP
- **Statement:** The wizard creates a deal step by step.
- **Tests:** —
- **Code:** ` + "`app-web/src/components/deals/deal-start-wizard.tsx`" + `
`

func TestDetailBlockURLineRidesAsParent(t *testing.T) {
	reqs := ParseLedger("tasks/DEAL-REQUIREMENTS.md", derivedLedgerFixture)
	if len(reqs) != 2 {
		t.Fatalf("want 2 rows, got %d", len(reqs))
	}

	withUR := BuildRequirementOp(reqs[0]).Payload
	if withUR["parent_external_id"] != "UR-DEAL-002" {
		t.Fatalf("the detail-block UR line never reached the payload: %v",
			withUR["parent_external_id"])
	}

	// Absent line → absent field, never an empty string.
	absent := BuildRequirementOp(reqs[1]).Payload
	if v, present := absent["parent_external_id"]; present {
		t.Fatalf("a row with no UR line grew a parent: %q", v)
	}
}

// A dedicated UR column still wins over the detail line — the column is the
// dashboard's own statement and the two agree in every corpus that has both.
func TestURColumnWinsOverTheDetailLine(t *testing.T) {
	ledger := "# CON — Consortiums · requirements ledger\n\n" +
		"| ID | Title | Stage | Status | UR | Source | Evidence | Code |\n" +
		"|---|---|---|---|---|---|---|---|\n" +
		"| REQ-CON-001 | A rule | MVP | IN_REVIEW | UR-CON-001 | §x | — | — |\n\n" +
		"### REQ-CON-001 — A rule\n- **UR:** UR-CON-999\n- **Tests:** —\n"
	p := BuildRequirementOp(ParseLedger("tasks/CON-REQUIREMENTS.md", ledger)[0]).Payload
	if p["parent_external_id"] != "UR-CON-001" {
		t.Fatalf("column lost to the detail line: %v", p["parent_external_id"])
	}
}

// Multiple URs on one line: the FIRST is the parent; the ledger keeps the rest.
func TestMultipleURsTakeTheFirst(t *testing.T) {
	p := BuildRequirementOp(Req{ID: "REQ-DEAL-030", Title: "t", Ctx: "DEAL",
		Status: "PENDING_VERIFICATION", UR: "UR-DEAL-002 · UR-DEAL-006"}).Payload
	if p["parent_external_id"] != "UR-DEAL-002" {
		t.Fatalf("multi-UR line not folded to its first id: %v", p["parent_external_id"])
	}
}

// The Snapshot warns about the extras rather than dropping them silently.
func TestMultiURWarnsAboutExtras(t *testing.T) {
	multi := strings.Replace(derivedLedgerFixture,
		"- **UR:** UR-DEAL-002", "- **UR:** UR-DEAL-002 · UR-DEAL-006", 1)
	root := t.TempDir()
	mustWriteFile(t, root, "tasks/DEAL-REQUIREMENTS.md", multi)
	mustWriteFile(t, root, "WORKLIST.md", "")

	_, warnings := Snapshot(root, manifest.Default())
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "REQ-DEAL-028") && strings.Contains(w, "UR-DEAL-006") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no warning about the ignored extra UR; warnings: %v", warnings)
	}
}

// REQ-CROSS-261: the epic->UR edge is declared in two places and was read from
// only one. ParseEpicUserRequirements reads the epic RECORD, so an epic whose
// `## User outcome` is prose without an id contributed no edge — even though
// its WORKLIST row names the id outright, and the rollup recovery path builds
// the UR entity FROM that same cell.
//
// The result contradicted itself inside a single store row: 160 user
// requirements carry source_citations naming an epic, 142 have the matching
// epic_user_requirements edge, and 18 do not — UR-CHAT-005 and UR-FE-001..014,
// 019..021. The row says "this came from EPIC-X" while no relation to EPIC-X
// exists.
//
// URCell's own contract already allows this: it "names the denominator even
// when the named epic record does not repeat the id". A membership edge is not
// a statement, and only statements were ever barred from this cell.
func TestWorklistURCellDeclaresTheEpicEdge(t *testing.T) {
	// EPIC-FE-001's real shape: an outcome paragraph carrying no UR id.
	record := `# EPIC-FE-001 — Portfolio re-home

## User outcome
The Portfolio renders the product-designed system cards over real systems.
`
	epic := Epic{ID: "EPIC-FE-001", Record: "epics/EPIC-FE-001-portfolio-rehome.md",
		URCell: "UR-FE-001"}
	ids, _ := BuildEpicOp(epic, record).Payload["user_requirement_external_ids"].([]string)
	if len(ids) != 1 || ids[0] != "UR-FE-001" {
		t.Errorf("epic edge = %v, want the id its WORKLIST row declares", ids)
	}

	// Two ids in the cell are two edges.
	epic.URCell = "UR-FE-001 · UR-FE-002"
	ids, _ = BuildEpicOp(epic, record).Payload["user_requirement_external_ids"].([]string)
	if len(ids) != 2 {
		t.Errorf("edges = %v, want both declared ids", ids)
	}

	// The record stays authoritative where it does name the id, and a cell
	// repeating it must not double it.
	withID := `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)
As a reader I want the record to name its own outcome.
`
	ids, _ = BuildEpicOp(Epic{ID: "EPIC-CV-001", URCell: "UR-CV-001"}, withID).
		Payload["user_requirement_external_ids"].([]string)
	if len(ids) != 1 || ids[0] != "UR-CV-001" {
		t.Errorf("edges = %v, want exactly the one id, not a duplicate", ids)
	}

	// No cell, no record id — no invented edge.
	if v := BuildEpicOp(Epic{ID: "EPIC-CV-002"}, record).Payload["user_requirement_external_ids"]; v != nil {
		t.Errorf("an edge was invented from nothing: %v", v)
	}
}
