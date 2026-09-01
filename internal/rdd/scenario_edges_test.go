package rdd

// REQ-CROSS-265 (EPIC-CLI-003 tranche 4): the scenario→requirement edge reads
// every declaring header, and every id it reads is validated.
//
// Only 60 of 714 imported scenarios carry an edge because the reader knew one
// header — `Realizes` (33 rows) — while the corpus writes the same declaration
// under `Requirement` (124 rows, 90 of them carrying REQ ids). Two measured
// populations stay OUT by name: 30 rows declaring only epic-local `SR-*`
// thin-SR ids (no `SR-*` requirement exists in the store) and 34 rows carrying
// bare context tokens, which would need an id-minting rule — a decision, not a
// parse. And the 7 edges landing today for `BR`/`POL`/`INV` families resolve to
// no requirement at all: they become named losses, a disclosed correction.

import (
	"strings"
	"testing"
)

func edgesOf(t *testing.T, record string, known ...string) map[string][]string {
	t.Helper()
	inv := map[string]bool{}
	for _, id := range known {
		inv[id] = true
	}
	out := map[string][]string{}
	for _, op := range BuildScenarioOps(Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}, record, inv) {
		id, _ := op.Payload["external_id"].(string)
		var ids []string
		for _, raw := range asAnySlice(op.Payload["required_requirement_external_ids"]) {
			s, _ := raw.(string)
			ids = append(ids, s)
		}
		out[id] = ids
	}
	return out
}

func TestRealizesHeaderStillDeclaresTheEdge(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Realizes |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | REQ-CV-001 |
`
	got := edgesOf(t, record, "REQ-CV-001")["EPIC-CV-001#SCN-CV-001"]
	if len(got) != 1 || got[0] != "REQ-CV-001" {
		t.Fatalf("edges = %v, want [REQ-CV-001] — the header read today must keep working", got)
	}
}

func TestRequirementColumnHeaderDeclaresTheEdge(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Summary | Requirement | Status |
|---|---|---|---|
| SCN-CV-002 | GIVEN a record WHEN it syncs THEN it is carried | REQ-CV-002 | active |
`
	got := edgesOf(t, record, "REQ-CV-002")["EPIC-CV-001#SCN-CV-002"]
	if len(got) != 1 || got[0] != "REQ-CV-002" {
		t.Fatalf("edges = %v, want [REQ-CV-002]", got)
	}
}

// One negative fixture per widened header: an id in a prose column is a
// mention. Reading it would invent edges the corpus never declared.
func TestRequirementIDInAProseColumnIsNoEdge(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Summary | Statement | Gherkin |
|---|---|---|---|
| SCN-CV-003 | GIVEN REQ-CV-003 is delivered WHEN it syncs THEN it is carried | supersedes REQ-CV-004 | REQ-CV-005 exercised |
`
	if got := edgesOf(t, record, "REQ-CV-003", "REQ-CV-004", "REQ-CV-005")["EPIC-CV-001#SCN-CV-003"]; len(got) != 0 {
		t.Fatalf("edges = %v, want none — no declaring column is present", got)
	}
}

func TestADeclaringColumnDoesNotAdmitTheProseColumnsBesideIt(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Summary | Requirement |
|---|---|---|
| SCN-CV-004 | GIVEN REQ-CV-099 is delivered WHEN it syncs THEN it is carried | REQ-CV-004 |
`
	got := edgesOf(t, record, "REQ-CV-004", "REQ-CV-099")["EPIC-CV-001#SCN-CV-004"]
	if len(got) != 1 || got[0] != "REQ-CV-004" {
		t.Fatalf("edges = %v, want only the declaring column's id", got)
	}
}

func TestSROnlyAndBareTokenRowsAreCountedExclusionsNeverEdges(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Summary | System requirements | Requirement |
|---|---|---|---|
| SCN-CV-005 | GIVEN a record WHEN it syncs THEN it is carried | SR-CV-001, SR-CV-002 | — |
| SCN-CV-006 | GIVEN a rule WHEN it runs THEN it holds | — | PLN-123 |
`
	edges := edgesOf(t, record)
	if got := edges["EPIC-CV-001#SCN-CV-005"]; len(got) != 0 {
		t.Fatalf("SR-only row produced edges %v — no SR-* requirement exists to point at", got)
	}
	if got := edges["EPIC-CV-001#SCN-CV-006"]; len(got) != 0 {
		t.Fatalf("bare-token row produced edges %v — admitting it needs an id-minting rule, a decision", got)
	}

	res := parseScenarioRealizes(record, nil)
	if len(res.srOnly) != 1 || !strings.Contains(res.srOnly[0], "SCN-CV-005") {
		t.Fatalf("the SR-only row must be COUNTED in its own class, got %v", res.srOnly)
	}
	if len(res.bareToken) != 1 || !strings.Contains(res.bareToken[0], "SCN-CV-006") {
		t.Fatalf("the bare-token row must be COUNTED in its own class, got %v", res.bareToken)
	}
}

// The disclosed correction: 7 edges land today for families that resolve to no
// requirement at all. Validation turns them into named losses.
func TestAnIDResolvingToNoRequirementIsANamedLossNeverAnEdge(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Realizes |
|---|---|---|
| SCN-CV-007 | GIVEN a rule WHEN it runs THEN it holds | BR-PLN-001 · REQ-CV-007 |
| SCN-CV-008 | GIVEN a rule WHEN it runs THEN it holds | REQ-CV-404 |
`
	edges := edgesOf(t, record, "REQ-CV-007")
	got := edges["EPIC-CV-001#SCN-CV-007"]
	if len(got) != 1 || got[0] != "REQ-CV-007" {
		t.Fatalf("edges = %v — a dangling family id is not an edge, and it must not take the resolvable one with it", got)
	}
	if got := edges["EPIC-CV-001#SCN-CV-008"]; len(got) != 0 {
		t.Fatalf("edges = %v, want none — the id resolves to no requirement", got)
	}

	res := parseScenarioRealizes(record, map[string]bool{"REQ-CV-007": true})
	if len(res.unresolved) != 1 || !strings.Contains(res.unresolved[0], "REQ-CV-404") {
		t.Fatalf("unresolvable ids must be NAMED, got %v", res.unresolved)
	}
}

func TestDeclaredRangesAndAlternatesExpand(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Requirement |
|---|---|---|
| SCN-CV-009 | GIVEN a rule WHEN it runs THEN it holds | REQ-CV-010..012 |
`
	got := edgesOf(t, record, "REQ-CV-010", "REQ-CV-011", "REQ-CV-012")["EPIC-CV-001#SCN-CV-009"]
	if len(got) != 3 {
		t.Fatalf("edges = %v, want the range expanded per the membership token rules", got)
	}
}

func TestEdgesCountGroupMeasuresAgainstTheCorpusDenominator(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Summary | Requirement |
|---|---|---|
| SCN-CV-020 | GIVEN a record WHEN it syncs THEN it is carried | REQ-CV-020 |
| SCN-CV-021 | GIVEN a rule WHEN it runs THEN it holds | REQ-CV-404 |
| SCN-CV-022 | GIVEN a rule WHEN it runs THEN it holds | PLN-123 |
| SCN-CV-023 | GIVEN a rule WHEN it runs THEN it holds | — |
`
	data := Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}}}
	records := map[string]string{"EPIC-CV-001": record}
	inv := map[string]bool{"REQ-CV-020": true}
	ops := BuildScenarioOps(data.Epics[0], record, inv)

	r := &FidelityReport{}
	r.scanEdgeHome(data, records, inv, ops)
	var g FidelityCount
	for _, c := range r.Counts {
		if strings.HasPrefix(c.Group, homeEdgesGroup) {
			g = c
		}
	}
	if g.Group == "" {
		t.Fatal("no scenario-edge count group")
	}
	// Denominator: rows whose declaring column declares SOMETHING. The empty
	// cell declares nothing and is not a shortfall.
	if g.Rows != 3 {
		t.Fatalf("denominator = %d, want 3 declaring rows", g.Rows)
	}
	if g.Ops != 1 {
		t.Fatalf("edges carried = %d, want 1", g.Ops)
	}
	if len(g.MissingFromOps) != 2 {
		t.Fatalf("both non-edge rows must be NAMED, got %v", g.MissingFromOps)
	}
}
