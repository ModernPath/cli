package rdd

// REQ-CROSS-250 (EPIC-CLI-003 T12): epic evidence-map rows land as
// verification_refs on the criterion each row targets; an unresolvable row is
// reported, never silently dropped.

import (
	"strings"
	"testing"
)

const evidenceMapRecord = `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |
| SCN-CV-002 | GIVEN a rerun WHEN nothing changed THEN zero writes |
| SCN-CV-003 | GIVEN a reader WHEN pulling THEN the shape renders |
| SCN-CV-004 | GIVEN a conflict WHEN syncing THEN it surfaces |

## Evidence map

| Item | Failing → Passing test | Code |
|---|---|---|
| SCN-CV-001 | ` + "`tests/cv.spec.ts` red→green" + ` | ` + "`src/cv.ts`" + ` |
| SCN-CV-002..003 | ` + "`tests/rerun_test.go` 2/2" + ` | ` + "`cmd/rerun.go`" + ` |
| Core | ` + "`storage_test.exs` 4/4" + ` | migration |
`

func scenarioByID(t *testing.T, op Op, scnID string) map[string]any {
	t.Helper()
	items, _ := op.Payload["scenarios"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if id, _ := item["external_id"].(string); strings.HasSuffix(id, "#"+scnID) {
			return item
		}
	}
	t.Fatalf("no scenario %s in payload", scnID)
	return nil
}

func refsOf(item map[string]any) []string {
	refs, _ := item["verification_refs"].([]any)
	var out []string
	for _, raw := range refs {
		if m, ok := raw.(map[string]any); ok {
			ref, _ := m["ref"].(string)
			out = append(out, ref)
		}
	}
	return out
}

func TestEvidenceMapRefsAttachToScenarios(t *testing.T) {
	op := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, evidenceMapRecord)

	one := scenarioByID(t, op, "SCN-CV-001")
	if got := refsOf(one); len(got) == 0 || !strings.Contains(strings.Join(got, " "), "tests/cv.spec.ts") {
		t.Fatalf("SCN-CV-001 refs = %v, want the cited test", got)
	}
	// a range row fans out to every scenario it names
	for _, scn := range []string{"SCN-CV-002", "SCN-CV-003"} {
		item := scenarioByID(t, op, scn)
		if got := refsOf(item); !strings.Contains(strings.Join(got, " "), "tests/rerun_test.go") {
			t.Fatalf("%s refs = %v, want the range row's test", scn, got)
		}
	}
	// a scenario no row names keeps an empty refs list, not an invented one
	four := scenarioByID(t, op, "SCN-CV-004")
	if got := refsOf(four); len(got) != 0 {
		t.Fatalf("SCN-CV-004 refs = %v, want none", got)
	}
}

// The prose-labeled row resolves to no clause — the landing-home group names
// it instead of dropping it.
func TestUnresolvableEvidenceMapRowIsReported(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", evidenceMapRecord)
	r := reportOf(t, root)
	g := countGroup(t, r, homeVerifRefsGroup)
	if g.Rows != 3 {
		t.Fatalf("evidence-map rows = %d, want 3", g.Rows)
	}
	if g.Ops != 2 {
		t.Fatalf("resolved rows = %d, want 2 (the prose row resolves to nothing)", g.Ops)
	}
	joined := strings.Join(g.MissingFromOps, " ")
	if !strings.Contains(joined, "Core") {
		t.Fatalf("the unresolvable row must be named: %v", g.MissingFromOps)
	}
}
