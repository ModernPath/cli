package rdd

// REQ-CROSS-266 (EPIC-CLI-003 tranche 4): a scenario's evidence conclusion is
// its own field, never a widened lifecycle status.
//
// 407 of the imported scenarios carry an `UPPER_VALIDATED` / `LOWER_VERIFIED`
// token in their source text and every one of them reads `active`. PROCESS.md
// is explicit that evidence conclusions are NOT completion states, so widening
// `status` would put an evidence fact into a lifecycle vocabulary; the
// conclusion needs its own carrier and the lifecycle keeps behaving exactly as
// it does today.

import (
	"strings"
	"testing"
)

func scenarioRecordPayload(t *testing.T, record, scnID string) map[string]any {
	t.Helper()
	for _, raw := range parseScenarioRecords(record, "EPIC-CV-001") {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		if id, _ := item["external_id"].(string); id == "EPIC-CV-001#"+scnID {
			return item
		}
	}
	t.Fatalf("no scenario %s parsed from the record", scnID)
	return nil
}

func TestStatusCellConclusionLandsAsItsOwnFieldAndLeavesStatusAlone(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Status |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | **UPPER_VALIDATED** |
| SCN-CV-002 | GIVEN a rule WHEN it runs THEN it holds | LOWER_VERIFIED ` + "`RUN:2026-08-24`" + ` |
`
	upper := scenarioRecordPayload(t, record, "SCN-CV-001")
	if upper["evidence_conclusion"] != "UPPER_VALIDATED" {
		t.Fatalf("evidence_conclusion = %v, want UPPER_VALIDATED", upper["evidence_conclusion"])
	}
	if upper["status"] != "active" {
		t.Fatalf("status = %v — the lifecycle must behave exactly as it does today", upper["status"])
	}
	lower := scenarioRecordPayload(t, record, "SCN-CV-002")
	if lower["evidence_conclusion"] != "LOWER_VERIFIED" {
		t.Fatalf("evidence_conclusion = %v, want LOWER_VERIFIED", lower["evidence_conclusion"])
	}
	if lower["status"] != "active" {
		t.Fatalf("status = %v", lower["status"])
	}
}

func TestLifecycleOnlyCellSetsNoConclusion(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Status |
|---|---|---|
| SCN-CV-003 | GIVEN a rule WHEN it changes THEN it is replaced | SUPERSEDED |
| SCN-CV-004 | GIVEN a rule WHEN it runs THEN it holds | active |
`
	sup := scenarioRecordPayload(t, record, "SCN-CV-003")
	if _, ok := sup["evidence_conclusion"]; ok {
		t.Fatalf("a lifecycle token sets no conclusion, got %v", sup["evidence_conclusion"])
	}
	if sup["status"] != "superseded" {
		t.Fatalf("status = %v, want superseded — unchanged behavior", sup["status"])
	}
	act := scenarioRecordPayload(t, record, "SCN-CV-004")
	if _, ok := act["evidence_conclusion"]; ok {
		t.Fatalf("a plain active cell sets no conclusion, got %v", act["evidence_conclusion"])
	}
}

// The 23-row negative population: the token appears in the row, but not in a
// status-family cell. `source_raw` preserves it; the typed field does not
// claim it, because a conclusion written in a Notes column is a mention, not a
// declaration of the scenario's evidence state.
func TestConclusionTokenInANonStatusCellSetsNoConclusion(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Status | Notes |
|---|---|---|---|
| SCN-CV-005 | GIVEN a record WHEN it syncs THEN it is carried | active | blocked until EPIC-CV-002 is UPPER_VALIDATED |
`
	p := scenarioRecordPayload(t, record, "SCN-CV-005")
	if _, ok := p["evidence_conclusion"]; ok {
		t.Fatalf("a token outside the status family sets no conclusion, got %v", p["evidence_conclusion"])
	}
	if raw, _ := p["source_raw"].(string); !strings.Contains(raw, "UPPER_VALIDATED") {
		t.Fatalf("the mention must still survive verbatim in source_raw, got %q", raw)
	}
}

// A definition written as a bullet or a heading block has no status cell at
// all, so it declares no conclusion — the same rule its lifecycle follows.
func TestNonTableShapesCarryNoConclusion(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

- **SCN-CV-006** — GIVEN a record WHEN it syncs THEN it is carried (UPPER_VALIDATED)
`
	p := scenarioRecordPayload(t, record, "SCN-CV-006")
	if _, ok := p["evidence_conclusion"]; ok {
		t.Fatalf("a bullet has no status cell, got %v", p["evidence_conclusion"])
	}
}

// The embedded criterion payload an epic carries has no field for this — the
// conclusion is a first-class scenario record's content only.
func TestEmbeddedCriterionPayloadCarriesNoConclusion(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Status |
|---|---|---|
| SCN-CV-007 | GIVEN a record WHEN it syncs THEN it is carried | UPPER_VALIDATED |
`
	for _, raw := range ParseScenarios(record, "EPIC-CV-001") {
		item, _ := raw.(map[string]any)
		if _, ok := item["evidence_conclusion"]; ok {
			t.Fatalf("the embedded criterion shape must not widen: %v", item)
		}
	}
}

func TestConclusionsCountGroupMeasuresAgainstTheCorpusDenominator(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Status | Notes |
|---|---|---|---|
| SCN-CV-010 | GIVEN a record WHEN it syncs THEN it is carried | UPPER_VALIDATED | — |
| SCN-CV-011 | GIVEN a rule WHEN it runs THEN it holds | LOWER_VERIFIED | — |
| SCN-CV-012 | GIVEN a record WHEN it syncs THEN it is carried | active | waiting on an UPPER_VALIDATED sibling |
| SCN-CV-013 | GIVEN a rule WHEN it runs THEN it holds | active | — |
`
	data := Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}}}
	records := map[string]string{"EPIC-CV-001": record}
	ops := BuildScenarioOps(data.Epics[0], record, nil)

	r := &FidelityReport{}
	r.scanConclusionHome(data, records, ops)
	var g FidelityCount
	for _, c := range r.Counts {
		if strings.HasPrefix(c.Group, homeConclusionsGroup) {
			g = c
		}
	}
	if g.Group == "" {
		t.Fatal("no conclusions count group")
	}
	// The denominator is every row whose SOURCE carries a token — the corpus,
	// not the parser. Measured against the parser's own output the group would
	// read 100% whatever landed.
	if g.Rows != 3 {
		t.Fatalf("denominator = %d, want 3 rows carrying a token in their source", g.Rows)
	}
	if g.Ops != 2 {
		t.Fatalf("carried = %d, want 2 — the non-status-cell row is not a conclusion", g.Ops)
	}
	if len(g.MissingFromOps) != 1 || !strings.Contains(g.MissingFromOps[0], "SCN-CV-012") {
		t.Fatalf("the non-status-cell row must be NAMED, got %v", g.MissingFromOps)
	}
}
