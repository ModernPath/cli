package rdd

// REQ-CROSS-222 — focused lower evidence, clause-mapped to
// epics/EPIC-CLI-003-ledger-import/specs/requirements.md §222.
// RED first: the op path does not carry these fields today.

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

const captureLedger = `# REQUIREMENTS — XX (fixture)

## Dashboard — XX
Totals: 2 PROPOSED · 1 OBSOLETE

| ID | Title | Stage | Status | Priority | Owner | Release | Lane | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|---|---|---|---|
| REQ-XX-001 | First behavior | MVP | PROPOSED | must | cli | v1-09 | fast | UR-XX-001 · UR-XX-002 | doc-a §3 | t.go | c.go |
| REQ-XX-002 | Dead end | MVP | OBSOLETE | — | — | — | — | UR-XX-001 | superseded by REQ-XX-001 | — | — |
| REQ-XX-003 | Bare row | MVP | PROPOSED | — | — | — | — | UR-XX-001 | doc-b | — | — |

### REQ-XX-001 — First behavior

- **Status:** PROPOSED · **Stage:** MVP
- **Raised-by:** a provenance story worth preserving verbatim.
- **Statement:** It does the thing.
- **Acceptance criteria:**
  - GIVEN x WHEN y THEN z
- **Tests:** t.go
- **Code:** c.go
`

func captureReqs(t *testing.T) []Req {
	t.Helper()
	reqs := ParseLedger("tasks/XX-REQUIREMENTS.md", captureLedger)
	if len(reqs) != 3 {
		t.Fatalf("fixture parsed %d rows, want 3", len(reqs))
	}
	return reqs
}

func opByID(ops []Op, id string) map[string]any {
	for _, op := range ops {
		if op.Type != "upsert_requirement" {
			continue
		}
		if got, _ := op.Payload["external_id"].(string); got == id {
			return op.Payload
		}
	}
	return nil
}

// §222.2 — Priority/Owner/Release columns are recognized first-class; an
// unrecognized column (Lane) is carried as a named extra, never dropped.
func TestLedgerParseCapturesNewColumns(t *testing.T) {
	reqs := captureReqs(t)
	q := reqs[0]
	if q.Priority != "must" || q.Owner != "cli" || q.Release != "v1-09" {
		t.Fatalf("priority/owner/release not captured: %q %q %q", q.Priority, q.Owner, q.Release)
	}
	if len(q.Extras) != 1 || q.Extras[0].Name != "Lane" || q.Extras[0].Value != "fast" {
		t.Fatalf("unrecognized column not carried as a named extra: %+v", q.Extras)
	}
	// Em-dash cells are absence, not content.
	if bare := reqs[2]; bare.Priority != "" || len(bare.Extras) != 0 {
		t.Fatalf("em-dash cells must stay empty: %q %+v", bare.Priority, bare.Extras)
	}
}

// §222.1/.2/.3/.6 — the payload carries every field the row does: all UR
// relations, the new columns, the verbatim detail block, the raw source cell.
func TestRequirementOpCarriesFullRow(t *testing.T) {
	reqs := captureReqs(t)
	ops := BuildOps(Data{Reqs: reqs}, func(string) string { return "" }, "2026-08-21")

	p := opByID(ops, "REQ-XX-001")
	if p == nil {
		t.Fatal("no op for REQ-XX-001")
	}
	parents, _ := p["parent_external_ids"].([]string)
	if len(parents) != 2 || parents[0] != "UR-XX-001" || parents[1] != "UR-XX-002" {
		t.Fatalf("parent_external_ids = %v, want both URs declared", p["parent_external_ids"])
	}
	if p["parent_external_id"] != "UR-XX-001" {
		t.Fatalf("parent_external_id must stay the first UR for existing consumers: %v", p["parent_external_id"])
	}
	if p["priority"] != "must" || p["owner"] != "cli" || p["release_note"] != "v1-09" {
		t.Fatalf("priority/owner/release_note missing: %v %v %v", p["priority"], p["owner"], p["release_note"])
	}
	extras, _ := p["extra_columns"].([]map[string]any)
	if len(extras) == 0 || extras[0]["name"] != "Lane" || extras[0]["value"] != "fast" {
		t.Fatalf("extra_columns = %v, want the Lane column carried first", p["extra_columns"])
	}
	detail, _ := p["detail_md"].(string)
	if !strings.Contains(detail, "a provenance story worth preserving verbatim") {
		t.Fatalf("detail_md does not carry the detail block verbatim: %q", detail)
	}
	if p["source_raw"] != "doc-a §3" {
		t.Fatalf("source_raw = %v, want the exact source cell", p["source_raw"])
	}

	// The relation list is the FULL parent set whenever the row names one at
	// all, single parent included. Absence of the array therefore means the row
	// named no parent — not that it named exactly one. The server unions the
	// singular and the array and dedups by ref, so the repeated first parent
	// costs nothing and no consumer has to guess which field is authoritative.
	bare := opByID(ops, "REQ-XX-003")
	oneParent, _ := bare["parent_external_ids"].([]string)
	if len(oneParent) != 1 || oneParent[0] != "UR-XX-001" {
		t.Fatalf("a single-parent row must carry the full set: %v", bare["parent_external_ids"])
	}
	if bare["parent_external_id"] != "UR-XX-001" {
		t.Fatalf("parent_external_id must stay the first parent: %v", bare["parent_external_id"])
	}

	// Omission-when-empty: a bare row gains none of the new keys, so
	// unchanged corpora do not churn hashes for absent content.
	for _, key := range []string{"priority", "owner", "release_note", "extra_columns", "detail_md", "superseding_ref"} {
		if _, present := bare[key]; present {
			t.Fatalf("bare row must omit %q, carries %v", key, bare[key])
		}
	}
	if bare["source_raw"] != "doc-b" {
		t.Fatalf("source_raw carries any non-empty source cell: %v", bare["source_raw"])
	}
}

// §222.4 — OBSOLETE rows emit with terminal status and their superseding
// reference; they are no longer skipped.
func TestObsoleteRowsEmitWithSupersedingRef(t *testing.T) {
	reqs := captureReqs(t)
	ops := BuildOps(Data{Reqs: reqs}, func(string) string { return "" }, "2026-08-21")

	p := opByID(ops, "REQ-XX-002")
	if p == nil {
		t.Fatal("OBSOLETE row emitted no op — still skipped")
	}
	if p["work_status"] != "OBSOLETE" {
		t.Fatalf("work_status = %v, want OBSOLETE", p["work_status"])
	}
	if p["superseding_ref"] != "REQ-XX-001" {
		t.Fatalf("superseding_ref = %v, want the id the source cell names", p["superseding_ref"])
	}
	if live := opByID(ops, "REQ-XX-001"); live["superseding_ref"] != nil {
		t.Fatalf("a live row must not carry superseding_ref: %v", live["superseding_ref"])
	}
}

// §221 detectors follow the capture: a loss is claimed only when the emitted
// payload actually lacks the field — the report shrinks as 222 closes gaps.
func TestFidelityDetectorsFollowTheCapture(t *testing.T) {
	reqs := captureReqs(t)
	ops := BuildOps(Data{Reqs: reqs}, func(string) string { return "" }, "2026-08-21")
	data := Data{Reqs: reqs}

	full := BuildFidelityReport(t.TempDir(), manifest.Default(), data, ops)
	for _, l := range full.Losses {
		if l.RecordID == "REQ-XX-001" &&
			(l.Field == "ur-relations" || l.Field == "prose:Raised-by" || l.Field == "source-citations") {
			t.Fatalf("detector still claims %q after the payload carries it", l.Field)
		}
		if l.RecordID == "REQ-XX-002" && l.Field == "row" {
			t.Fatal("detector still claims the OBSOLETE row is skipped after it emits")
		}
	}

	// Strip the capture and the same losses must reappear — the detectors
	// measure payloads, not assumptions.
	var stripped []Op
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			cp := map[string]any{}
			for k, v := range op.Payload {
				cp[k] = v
			}
			delete(cp, "parent_external_ids")
			delete(cp, "detail_md")
			delete(cp, "source_raw")
			op = Op{Type: op.Type, Payload: cp}
		}
		stripped = append(stripped, op)
	}
	degraded := BuildFidelityReport(t.TempDir(), manifest.Default(), data, stripped)
	want := map[string]bool{"ur-relations": false, "prose:Raised-by": false, "source-citations": false}
	for _, l := range degraded.Losses {
		if l.RecordID == "REQ-XX-001" {
			if _, tracked := want[l.Field]; tracked {
				want[l.Field] = true
			}
		}
	}
	for field, seen := range want {
		if !seen {
			t.Fatalf("stripped payload did not re-surface the %q loss", field)
		}
	}
}

// The array is omitted only when the row names no parent at all — an em-dash
// cell is absence, the same convention the evidence columns use.
func TestParentSetOmittedOnlyWhenNoParentIsNamed(t *testing.T) {
	none := BuildRequirementOp(Req{ID: "REQ-XX-004", Title: "No parent", Ctx: "XX", Status: "PROPOSED", UR: "—"})
	if _, present := none.Payload["parent_external_ids"]; present {
		t.Fatalf("a parent-less row must omit the array: %v", none.Payload["parent_external_ids"])
	}
	if _, present := none.Payload["parent_external_id"]; present {
		t.Fatalf("a parent-less row must omit the singular: %v", none.Payload["parent_external_id"])
	}
}
