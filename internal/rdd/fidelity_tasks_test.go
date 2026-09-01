package rdd

// REQ-CROSS-264: the tasks landing-home count group. Its denominator is
// CORPUS-defined — declarations read from the records and WORKLIST — never the
// gate being widened, which would compare the parser against itself and show
// no movement whatever landed.

import (
	"strings"
	"testing"
)

func taskCountGroup(t *testing.T, r *FidelityReport) FidelityCount {
	t.Helper()
	for _, c := range r.Counts {
		if strings.HasPrefix(c.Group, homeTasksGroup) {
			return c
		}
	}
	var names []string
	for _, c := range r.Counts {
		names = append(names, c.Group)
	}
	t.Fatalf("no tasks count group in %v", names)
	return FidelityCount{}
}

func TestTasksCountGroupMeasuresDeclarationsAgainstOps(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Tasks

| Task | Scope |
|---|---|
| TASK-CV-500 | carried |
| TASK-CV-501 | carried too |

## Evidence map

| Task | Test |
|---|---|
| TASK-CV-999 | ` + "`x_test.exs`" + ` |
`
	data := Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md", TasksCell: "TASK-CV-502"}}}
	records := map[string]string{"EPIC-CV-001": record}
	ops := BuildTaskOps(data, records)

	r := &FidelityReport{}
	r.scanTaskHome(data, records, ops)
	g := taskCountGroup(t, r)
	if g.Rows != 3 {
		t.Fatalf("denominator = %d, want 3 corpus declarations (the evidence-map row is excluded)", g.Rows)
	}
	if g.Ops != 3 {
		t.Fatalf("covered = %d, want 3 — every declaration reached an op", g.Ops)
	}
	if !strings.Contains(g.Group, "declaration") {
		t.Fatalf("the group must state its population: %q", g.Group)
	}
}

func TestTasksCountGroupNamesWhatItDidNotCarry(t *testing.T) {
	data := Data{
		Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}},
		WorklistTaskRows: []WorklistTaskRow{
			{EpicID: "EPIC-GONE-999", Cell: "TASK-GG-001", SourcePath: "WORKLIST.md", Line: 12},
		},
	}
	records := map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"}
	r := &FidelityReport{}
	r.scanTaskHome(data, records, BuildTaskOps(data, records))
	g := taskCountGroup(t, r)
	if g.Rows != 1 || g.Ops != 0 {
		t.Fatalf("rows=%d ops=%d, want 1/0 — an unresolvable epic prefix is a loss, not a row", g.Rows, g.Ops)
	}
	if len(g.MissingFromOps) != 1 || !strings.Contains(g.MissingFromOps[0], "TASK-GG-001") {
		t.Fatalf("the loss must be NAMED, got %v", g.MissingFromOps)
	}
}

// D-T4-5: a digit-width-mismatched range names no row (nothing was ever
// expanded from it, so it is not a corpus "declaration" the Rows/Ops ratio
// should count), but it must still surface — as a corpus-drift Hygiene flag,
// the same home Shadowed uses — so it is never silently dropped.
func TestMismatchedRangeSurfacesAsHygieneNotAsARow(t *testing.T) {
	data := Data{Epics: []Epic{{
		ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md",
		TasksCell: "TASK-CV-9031..90310",
	}}}
	records := map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"}
	r := &FidelityReport{}
	r.scanTaskHome(data, records, BuildTaskOps(data, records))
	g := taskCountGroup(t, r)
	if g.Rows != 0 {
		t.Fatalf("rows = %d, want 0 — a mismatched range is not a counted declaration", g.Rows)
	}
	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "TASK-CV-9031..90310") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Hygiene = %v, want the mismatched range named", r.Hygiene)
	}
}
