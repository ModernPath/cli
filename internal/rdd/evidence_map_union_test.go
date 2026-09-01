package rdd

// REQ-CROSS-267 (EPIC-CLI-003 P8): the carrier reads EVERY carrying section.
//
// Measured over the 190-record corpus (`RUN:2026-08-24`): 117 SCN-led rows sit
// inside evidence-map sections (106 of them carry backticked refs) while 354
// ref-carrying SCN-led rows sit outside — 248 of those under BDD-scenario
// headings — a 471-row population the section gate reads 106 of. The gate
// becomes an EXPLICIT union of the evidence-map headings and the measured
// scenario-definition headings; the reference-table families stay out; refs
// come only from evidence-family columns.

import (
	"reflect"
	"strings"
	"testing"
)

// The census group's name, written out as the report prints it — an
// instrument whose name a reader cannot find is not an instrument.
const verifRowsGroupName = "landing home: evidence rows resolved (SCN-led rows in an evidence map or carrying backticked refs → refs landing on the named criterion)"

// Fixtures write ~ref~ where the corpus writes a backticked span: a raw string
// cannot hold a backtick, and these tables are unreadable spliced.
func ticked(s string) string { return strings.ReplaceAll(s, "~", "`") }

func refPairs(item map[string]any) []string {
	refs, _ := item["verification_refs"].([]any)
	var out []string
	for _, raw := range refs {
		if m, ok := raw.(map[string]any); ok {
			kind, _ := m["kind"].(string)
			ref, _ := m["ref"].(string)
			out = append(out, kind+":"+ref)
		}
	}
	return out
}

// Every scenario id a record's payload carries, with its typed refs — the
// shape the union arms assert against.
func refsByScenario(op Op) map[string][]string {
	out := map[string][]string{}
	items, _ := op.Payload["scenarios"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		id, _ := item["external_id"].(string)
		if i := strings.LastIndex(id, "#"); i >= 0 {
			id = id[i+1:]
		}
		out[id] = refPairs(item)
	}
	return out
}

const unionSectionRecord = `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want every carrying section read.

## BDD acceptance scenarios

| Scenario | Given / When / Then | Passing evidence |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | ~tests/bdd_carry_test.go~ |
| SCN-CV-002 | GIVEN a rerun WHEN nothing changed THEN zero writes | ~tests/bdd_rerun_test.go~ |

## Evidence map

| Item | Failing → Passing test | Code |
|---|---|---|
| SCN-CV-002 | ~tests/emap_rerun_test.go~ red→green | ~src/rerun.ts~ |
`

// The union arm and the still-admitted arm in one record: 248 of the corpus's
// outside rows sit under a BDD heading, and the evidence-map heading the
// carrier already reads must survive the widening. A design that reuses the
// scenario carrier's exclusion vocabulary fails the second half — that
// vocabulary's `evidence` term excludes `## Evidence map` itself.
func TestUnionSectionRefsResolveAndEvidenceMapStaysAdmitted(t *testing.T) {
	op := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, ticked(unionSectionRecord))
	refs := refsByScenario(op)

	if got := refs["SCN-CV-001"]; len(got) != 1 || got[0] != "test:tests/bdd_carry_test.go" {
		t.Fatalf("SCN-CV-001 refs = %v, want the BDD row's evidence", got)
	}
	joined := strings.Join(refs["SCN-CV-002"], " ")
	for _, want := range []string{"test:tests/bdd_rerun_test.go", "test:tests/emap_rerun_test.go", "code:src/rerun.ts"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("SCN-CV-002 refs = %v, want %s — both carrying sections", refs["SCN-CV-002"], want)
		}
	}
}

// The union is explicit: the evidence-map headings the carrier already reads
// PLUS the scenario-definition headings measured to carry refs. Every heading
// below is one the corpus writes.
func TestCarryingSectionHeadingsAreAnExplicitUnion(t *testing.T) {
	lands := func(heading string) bool {
		record := ticked(`# EPIC-CV-001 — Only

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |

` + heading + `

| Item | Passing evidence |
|---|---|
| SCN-CV-001 | ~tests/heading_probe_test.go~ |
`)
		op := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, record)
		return strings.Contains(strings.Join(refsByScenario(op)["SCN-CV-001"], " "), "tests/heading_probe_test.go")
	}
	// carrying: the evidence-map family (117 rows) and the scenario-definition
	// family measured to carry refs (343 rows)
	for _, heading := range []string{
		"## Evidence map",
		"## 6. Evidence map",
		"## Evidence map — RED → GREEN",
		"## BDD acceptance scenarios",
		"## BDD acceptance scenarios (SCN — upper loop)",
		"## Journeys and acceptance scenarios",
		"## Scenarios",
		"### 3.2 Scenarios",
	} {
		if !lands(heading) {
			t.Errorf("%q carries evidence rows in the corpus and must be read", heading)
		}
	}
	// not carrying: the reference shapes that CITE scenario ids
	for _, heading := range []string{
		"## Trace",
		"## Trace matrix",
		"## Coverage",
		"## Coverage of this pass",
		"## Requirement traceability",
		"## Ownership matrix",
		"## Upper-loop matrix",
		"## Tasks",
		"## Decisions",
	} {
		if lands(heading) {
			t.Errorf("%q cites scenario ids, it does not carry their evidence", heading)
		}
	}
}

// Column typing: refs come only from evidence-family columns. A backticked
// token in a Summary, Statement or Gherkin column is not evidence of anything
// — today it lands as kind `note`, which is the leak this arm pins.
func TestEvidenceRefsComeOnlyFromEvidenceFamilyColumns(t *testing.T) {
	record := ticked(`# EPIC-CV-001 — Only

## BDD acceptance scenarios

| Scenario | Summary | Statement | Gherkin | Passing evidence |
|---|---|---|---|---|
| SCN-CV-001 | ~docs/summary.md~ | ~REQ-CV-001~ | ~features/cv.feature~ | ~tests/cv_test.go~ |
| SCN-CV-002 | ~docs/other.md~ | ~REQ-CV-002~ | ~features/rerun.feature~ | — |

## Evidence map

| Item | Summary | Passing evidence |
|---|---|---|
| SCN-CV-002 | ~docs/emap-summary.md~ | ~tests/rerun_test.go~ |
`)
	op := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, record)
	refs := refsByScenario(op)

	if got := refs["SCN-CV-001"]; len(got) != 1 || got[0] != "test:tests/cv_test.go" {
		t.Fatalf("SCN-CV-001 refs = %v, want only the evidence column", got)
	}
	if got := refs["SCN-CV-002"]; len(got) != 1 || got[0] != "test:tests/rerun_test.go" {
		t.Fatalf("SCN-CV-002 refs = %v, want only the evidence column — the in-map Summary cell is not evidence", got)
	}
	for id, got := range refs {
		for _, ref := range got {
			if strings.HasPrefix(ref, "note:") {
				t.Fatalf("%s carries %q: an untyped column must yield no ref at all", id, ref)
			}
		}
	}
}

// The three reference-table families, each a shape the corpus really writes
// (12, 13 and 12 SCN-led rows measured `RUN:2026-08-24`) under a heading the
// corpus really uses. A design with no section gate admits all three; the
// carrying half of every fixture keeps the arm honest about what must resolve.
func TestReferenceTableFamiliesAdmitNothing(t *testing.T) {
	carrying := `## BDD acceptance scenarios

| Scenario | Given / When / Then | Passing evidence |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | ~tests/bdd_carry_test.go~ |
| SCN-CV-002 | GIVEN a rerun WHEN nothing changed THEN zero writes | — |
`
	for _, family := range []struct {
		name  string
		table string
	}{
		{"trace", `## Trace

| Trace | Failing evidence | Passing evidence | Revision/report | Verdict |
|---|---|---|---|---|
| SR-CV-301 / SCN-CV-002 / TASK-CV-301 | ~RUN:2026-08-14~ 6/6 failed | ~RUN:2026-08-15~ 19/19 pass | branch ~feat/cv~ | LOWER_VERIFIED |
`},
		{"coverage", `## Coverage

| Item | Evidence | Result |
|---|---|---|
| SCN-CV-002 / SR-CV-302 | ~tests/coverage_probe_test.exs:434~ | **RED first** → **GREEN** |
`},
		{"traceability matrix", `## Requirement traceability

| Item | Failing → Passing test | Code |
|---|---|---|
| SCN-CV-002 | ~tests/matrix_probe.spec.ts~ red→green | ~src/matrix.ts~ |
`},
	} {
		t.Run(family.name, func(t *testing.T) {
			op := BuildEpicOp(Epic{ID: "EPIC-CV-001"},
				ticked("# EPIC-CV-001 — Only\n\n"+carrying+"\n"+family.table))
			refs := refsByScenario(op)
			if got := refs["SCN-CV-001"]; len(got) != 1 || got[0] != "test:tests/bdd_carry_test.go" {
				t.Fatalf("SCN-CV-001 refs = %v, want the carrying section's evidence", got)
			}
			if got := refs["SCN-CV-002"]; len(got) != 0 {
				t.Fatalf("SCN-CV-002 refs = %v, want none — a %s table cites ids, it does not carry evidence", got, family.name)
			}
		})
	}
}

const censusRecord = `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the census to count the corpus, not the gate.

## BDD acceptance scenarios

| Scenario | Given / When / Then | Passing evidence |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried | ~tests/carry_test.go~ |
| SCN-CV-002 | GIVEN a rerun WHEN nothing changed THEN zero writes | — |

## Evidence map

| Item | Failing → Passing test | Code |
|---|---|---|
| SCN-CV-001 | ~tests/carry_red_test.go~ | ~src/carry.ts~ |
| SCN-CV-002 | (no evidence yet) | — |
| SCN-CV-404 | ~tests/ghost_test.go~ | — |
| Core | ~tests/storage_test.exs~ | — |

## Trace

| Item | Evidence | Result |
|---|---|---|
| SCN-CV-002 | ~tests/trace_only_test.go~ | LOWER_VERIFIED |
`

// The census instrument is corpus-defined: its denominator is every SCN-led
// row that sits in an evidence map or carries a backticked ref, counted from
// the corpus and not from the gate being widened. Its coverage is the refs
// that actually reach a criterion.
func TestEvidenceRowResolutionGroupCountsTheCorpusDenominator(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", ticked(censusRecord))
	r := reportOf(t, root)

	g := countGroup(t, r, verifRowsGroupName)
	// 5 SCN-led rows: 2 in the evidence map with refs, 1 in the evidence map
	// without, 1 under the BDD heading with refs, 1 under the trace heading.
	// The prose-led "Core" row names no scenario and is not this population.
	if g.Rows != 5 {
		t.Fatalf("denominator = %d, want 5 — every SCN-led row in a map or carrying refs: %v", g.Rows, g.MissingFromOps)
	}
	if g.Ops != 2 {
		t.Fatalf("resolved = %d, want 2 — SCN-CV-001's two rows land, nothing else does", g.Ops)
	}
	joined := strings.Join(g.MissingFromOps, "\n")
	for _, want := range []string{"SCN-CV-002", "SCN-CV-404"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("%s resolves to no criterion and must be named: %v", want, g.MissingFromOps)
		}
	}

	// the named evidence-map group keeps its own population — 4 data rows in
	// the map, 2 of them resolved by the parser
	old := countGroup(t, r, homeVerifRefsGroup)
	if old.Rows != 4 || old.Ops != 2 {
		t.Fatalf("evidence-map group = %d/%d, want 4/2 (its population is the map's own rows)", old.Rows, old.Ops)
	}
}

// A row that resolves to no criterion is reported where a reader can open it:
// file and line, never dropped.
func TestUnresolvableEvidenceRowNamesFileAndLine(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", ticked(censusRecord))
	r := reportOf(t, root)
	g := countGroup(t, r, verifRowsGroupName)

	// SCN-CV-404 names a scenario the record never defines: line 20 of the record
	want := "epics/EPIC-CV-001-only.md:20"
	joined := strings.Join(g.MissingFromOps, "\n")
	if !strings.Contains(joined, want) {
		t.Fatalf("the unresolvable row must be located %q: %v", want, g.MissingFromOps)
	}
	for _, entry := range g.MissingFromOps {
		if !strings.Contains(entry, ".md:") {
			t.Fatalf("every reported row carries its file and line, got %q", entry)
		}
	}
}

// Rerunning the build over unchanged content produces the identical payload:
// the refs a criterion carries are in document order, never in map order.
func TestEvidenceMapRefsAreRerunIdempotent(t *testing.T) {
	first := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, ticked(censusRecord))
	second := BuildEpicOp(Epic{ID: "EPIC-CV-001"}, ticked(censusRecord))
	if !reflect.DeepEqual(first.Payload, second.Payload) {
		t.Fatal("a rerun over unchanged content must produce the identical payload")
	}
	refs := refsByScenario(first)["SCN-CV-001"]
	want := []string{"test:tests/carry_test.go", "test:tests/carry_red_test.go", "code:src/carry.ts"}
	if !reflect.DeepEqual(refs, want) {
		t.Fatalf("SCN-CV-001 refs = %v, want %v in document order", refs, want)
	}
}
