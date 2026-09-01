package rdd

// REQ-CROSS-252 (EPIC-CLI-003 T11): scenario definitions are first-class
// records — a new upsert_scenario op carries every declared definition,
// spec-file forms included, with declared owner, required-requirement ids,
// G/W/T or statement, and source position; the epic-specs residue key reads
// zero once the spec definitions are carried.

import (
	"strings"
	"testing"
)

func scenarioOpsFor(t *testing.T, epic Epic, record string) []Op {
	t.Helper()
	return BuildScenarioOps(epic, record, nil)
}

func TestScenarioDefinitionsBecomeRecords(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario | Realizes |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | REQ-CV-001 |
`
	ops := scenarioOpsFor(t, Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001-only.md"}, record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}
	op := ops[0]
	if op.Type != "upsert_scenario" {
		t.Fatalf("type = %s", op.Type)
	}
	p := op.Payload
	if p["external_id"] != "EPIC-CV-001#SCN-CV-001" {
		t.Fatalf("external_id = %v", p["external_id"])
	}
	if p["declared_owner_type"] != "epic" || p["declared_owner_external_id"] != "EPIC-CV-001" {
		t.Fatalf("owner = %v/%v", p["declared_owner_type"], p["declared_owner_external_id"])
	}
	if p["given"] != "a record" || p["then"] != "it is carried" {
		t.Fatalf("gwt = %v/%v/%v", p["given"], p["when"], p["then"])
	}
	required, _ := p["required_requirement_external_ids"].([]any)
	if !strings.Contains(strings.Join(anyStrings(required), " "), "REQ-CV-001") {
		t.Fatalf("Realizes ids must land as required requirements: %v", required)
	}
	if p["source_path"] != "epics/EPIC-CV-001-only.md" {
		t.Fatalf("source_path = %v", p["source_path"])
	}
}

func TestScenarioRecordsPreserveLifecycleAndSourcePosition(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| Scenario | Given / When / Then | Status |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it is superseded THEN it stays historical | OBSOLETE — consolidated into SCN-CV-002 |
`
	ops := scenarioOpsFor(t, Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001-only.md"}, record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}
	p := ops[0].Payload
	if p["status"] != "superseded" {
		t.Fatalf("status = %v, want superseded", p["status"])
	}
	if p["source_line"] != 7 {
		t.Fatalf("source_line = %v, want 7", p["source_line"])
	}
	if raw, _ := p["source_raw"].(string); !strings.Contains(raw, "OBSOLETE") {
		t.Fatalf("source_raw must preserve the declaring row: %q", raw)
	}
}

func TestSpecFileScenarioDefinitionsAreCarried(t *testing.T) {
	spec := EpicSpec{
		Rel:  "epics/EPIC-CV-001-only/specs/requirements.md",
		Name: "requirements.md",
		Content: `# Requirements

## Scenarios

### SCN-CV-009 — The spec-defined case

GIVEN a spec definition WHEN the import runs THEN it lands structured
`,
	}
	epic := Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001-only/EPIC.md", Specs: []EpicSpec{spec}}
	ops := scenarioOpsFor(t, epic, "# EPIC-CV-001 — Only\n")
	var found map[string]any
	for _, op := range ops {
		if op.Payload["external_id"] == "EPIC-CV-001#SCN-CV-009" {
			found = op.Payload
		}
	}
	if found == nil {
		t.Fatalf("spec-file definition built no scenario record: %v", opIDs(ops))
	}
	if found["source_path"] != spec.Rel {
		t.Fatalf("source_path = %v, want the spec file", found["source_path"])
	}
}

// With the spec definitions carried, the epic-specs residue key reads zero —
// the report raises no scenario-definitions loss for a carried spec.
func TestCarriedSpecScenarioRaisesNoResidue(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only/EPIC.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.
`)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only/specs/requirements.md", `# Requirements

## Scenarios

### SCN-CV-009 — The spec-defined case

GIVEN a spec definition WHEN the import runs THEN it lands structured
`)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only/EPIC.md) | UR-CV-001 | SCN-CV-009 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n")

	r := reportOf(t, root)
	for _, l := range r.Losses {
		if l.RecordID == "epic-specs" && l.Field == "scenario-definitions" {
			t.Fatalf("carried spec definitions must raise no residue: %+v", l)
		}
	}
	g := countGroup(t, r, homeScenariosGroup)
	if g.Ops == 0 {
		t.Fatalf("scenario records home must count the carried ops: %+v", g)
	}
}
