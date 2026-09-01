package rdd

// REQ-CROSS-221, disk-side arm — focused lower evidence.
//
// Every check here is paired: a fixture where the check FIRES on a synthetic
// break, and a fixture where the same check stays SILENT on content that only
// looks like the break. The negative half is not optional. Each of these
// detectors reads a shape that also occurs innocently in the corpus — an
// evidence-map table citing scenario ids, a fast-lane WORKLIST row whose Epic
// cell is an em-dash, a cell whose references carry no prose beside them — and
// a detector that cannot tell the two apart reports a number nobody can act on.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/modernpath/cli/internal/manifest"
)

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// reportOf builds the real report over a fixture root, reading epic records
// from disk exactly as the sync path does.
func reportOf(t *testing.T, root string) FidelityReport {
	t.Helper()
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")
	return BuildFidelityReport(root, m, data, ops)
}

func lossWithField(losses []FidelityLoss, field string) *FidelityLoss {
	for i := range losses {
		if losses[i].Field == field {
			return &losses[i]
		}
	}
	return nil
}

func lossesWithFieldPrefix(losses []FidelityLoss, prefix string) []FidelityLoss {
	var out []FidelityLoss
	for _, l := range losses {
		if strings.HasPrefix(l.Field, prefix) {
			out = append(out, l)
		}
	}
	return out
}

// ---------------------------------------------------------------- epic records on disk

const cvWorklistHeader = `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
`

// A record that reaches an op: one epic, one file, one id.
func cleanEpicCorpus(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | — | doc | — | — |
`)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n")
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |
`)
	return root
}

// §221 disk arm — two files claim one epic id: the extractor keeps the first,
// the second file's whole content reaches no op, and the epic COUNT still
// reconciles. That silence is what the check exists to break.
func TestEpicRecordShadowedByDuplicateIDIsFailLevel(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-shadowed-second-record.md",
		"# EPIC-CV-001 — A second record claiming the same id\n\nContent no op will ever carry.\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "epic-record-shadowed")
	if l == nil {
		t.Fatalf("a second record claiming EPIC-CV-001 must be named as shadowed; losses: %+v", r.Losses)
	}
	if l.RecordID != "epics/EPIC-CV-001-shadowed-second-record.md" {
		t.Fatalf("the loss must name the SHADOWED file (the content that is lost), got %q", l.RecordID)
	}
	if l.Category != LossLost {
		t.Fatalf("a shadowed record is lost content, not hygiene drift: %+v", l)
	}
	blocking := false
	for _, b := range r.Blocking(nil) {
		if b.Key() == l.Key() {
			blocking = true
		}
	}
	if !blocking {
		t.Fatal("a shadowed epic record must block the report")
	}
}

// The negative half: one record per id must produce no shadow finding, and the
// on-disk count must reconcile.
func TestEpicRecordCoverageSilentWhenEveryIDHasOneRecord(t *testing.T) {
	r := reportOf(t, cleanEpicCorpus(t))

	for _, field := range []string{"epic-record-shadowed", "epic-record-identity", "epic-record-unsynced", "epic-record-missing"} {
		if l := lossWithField(r.Losses, field); l != nil {
			t.Fatalf("clean corpus produced %s: %+v", field, l)
		}
	}
	c := countGroup(t, r, "epic-records-on-disk (epics/EPIC*.md + epics/*/EPIC.md)")
	if c.Rows != 1 || c.Ops != 1 {
		t.Fatalf("one record, one op — got rows %d ops %d (%v)", c.Rows, c.Ops, c.MissingFromOps)
	}
}

// A file in the epic-record population whose name yields no id can never reach
// an epic op, and epics/** retires at the flip.
func TestEpicRecordWithUnparseableNameIsFailLevel(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPICSUMMARY.md", "# Not an epic record\n\nBut it lives in the retired tree.\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "epic-record-identity")
	if l == nil || l.RecordID != "epics/EPICSUMMARY.md" || l.Category != LossLost {
		t.Fatalf("an unparseable epic-record filename must be a FAIL-level finding naming the file, got %+v", l)
	}
}

// A directory naming an epic id but holding no EPIC.md: the epic syncs with an
// empty body, and whatever the folder does hold retires uncarried.
func TestEpicDirectoryWithoutRecordIsFailLevel(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-002-no-record/tasks/TASK-CV-1.md", "# A task with no epic record\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "epic-record-missing")
	if l == nil || l.RecordID != "epics/EPIC-CV-002-no-record" {
		t.Fatalf("a record-less epic directory must be named, got %+v", l)
	}
}

// The negative half, and the shape the corpus actually writes: a directory
// beside a top-level `epics/<name>.md` record. The record is not missing — it
// is the sibling file — so the directory's own contents are the archival
// diff's business, counted in its population and answered by its carriers.
func TestEpicDirectoryBesideItsRecordFileIsNotAMissingRecord(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only/TASK-CV-1.md", "# A task beside the record file\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "epic-record-missing"); l != nil {
		t.Fatalf("the sibling epics/EPIC-CV-001-only.md IS the record; nothing is missing: %+v", l)
	}
	c := countGroup(t, r, "archival-coverage (files matched by the retired-path families → document/spec carriers)")
	if c.Rows == 0 || c.Ops != c.Rows {
		t.Fatalf("the directory's contents belong to the archival population and must reach a carrier, got rows %d ops %d (missing %v)",
			c.Rows, c.Ops, c.MissingFromOps)
	}
	if l := lossWithField(r.Losses, "uncarried-retired-files"); l != nil {
		t.Fatalf("no retired file is uncarried in this corpus: %+v", l)
	}
}

// scanEpicRecordCoverage is exercised directly for the arm a fixture cannot
// reach through the extractor: a record whose id no epic op carries at all.
func TestEpicRecordUnsyncedIDIsFailLevel(t *testing.T) {
	root := cleanEpicCorpus(t)
	var r FidelityReport
	r.scanEpicRecordCoverage(root, map[string]bool{}, map[string]string{})

	l := lossWithField(r.Losses, "epic-record-unsynced")
	if l == nil || l.RecordID != "epics/EPIC-CV-001-only.md" || l.Category != LossLost {
		t.Fatalf("a record whose id reaches no epic op must be FAIL-level, got %+v", l)
	}
}

// ---------------------------------------------------------------- duplicate identity

// A duplicate rollup id is lost content, not untidiness: the second row's
// record link, loop status and evidence reach no op.
func TestDuplicateWorklistIdentityIsFailLevel(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| EPIC-CV-001 | `epics/EPIC-CV-001-only.md` | UR-CV-009 | SCN-CV-009 | REQ-CV-001 | T2 | — | — | IN_PROGRESS | — | second row |\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "duplicate-identity:EPIC-CV-001")
	if l == nil || l.Category != LossLost {
		t.Fatalf("a duplicate WORKLIST rollup id must be FAIL-level, got %+v (losses %+v)", l, r.Losses)
	}
	if !strings.HasPrefix(l.RecordID, "WORKLIST.md:") {
		t.Fatalf("the loss must locate the shadowed row, got %q", l.RecordID)
	}
	// Both rows link the same record file: the reader's move is a row merge,
	// not an epic rename, and the detail must say so.
	if !strings.Contains(l.Detail, "same record") {
		t.Fatalf("two rows for one record must read as a same-record duplicate, got %q", l.Detail)
	}
	// The hygiene note stays: a guard flags, and it flags in both places.
	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "duplicate WORKLIST rollup id EPIC-CV-001") {
			found = true
		}
	}
	if !found {
		t.Fatal("escalating to a loss must not remove the located hygiene flag")
	}
}

// The other arm: two DIFFERENT record files claiming one id shadow a whole
// record, and the reader's move is a rename, not a row merge.
func TestTwoRecordsClaimingOneIDReadAsACollision(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-second.md", "# EPIC-CV-001 — Second\n\nAnother record entirely.\n")
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-second.md) | UR-CV-002 | SCN-CV-002 | REQ-CV-001 | T2 | — | — | DONE | — | — |\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "duplicate-identity:EPIC-CV-001")
	if l == nil || l.Category != LossLost {
		t.Fatalf("two records claiming one id must be FAIL-level, got %+v", l)
	}
	if strings.Contains(l.Detail, "same record") || !strings.Contains(l.Detail, "epics/EPIC-CV-001-second.md") {
		t.Fatalf("a genuine collision must name the shadowed record, not read as a row duplicate: %q", l.Detail)
	}
}

// The negative half, and the exact shape that made this matcher wrong the
// first time: fast-lane rows write "—" in the Epic column. Eleven of them in a
// row are eleven rows with no epic id, not eleven identity collisions.
func TestPlaceholderEpicCellIsNotADuplicateIdentity(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| — | fast lane (REQ row is the task record) | REQ-CV-001 | — | REQ-CV-001 | — | — | — | DONE | — | — |\n"+
		"| — | fast lane (REQ row is the task record) | REQ-CV-002 | — | REQ-CV-002 | — | — | — | DONE | — | — |\n"+
		"| — | fast lane (REQ row is the task record) | REQ-CV-003 | — | REQ-CV-003 | — | — | — | DONE | — | — |\n")

	r := reportOf(t, root)
	if ls := lossesWithFieldPrefix(r.Losses, "duplicate-identity:"); len(ls) != 0 {
		t.Fatalf("an em-dash Epic cell is a placeholder, not an identity — got %+v", ls)
	}
}

// ---------------------------------------------------------------- scenario shapes

// The four shapes, counted at the unit the report states: definitions, per
// shape, distinguished from citations.
func TestCountScenarioShapesClassifiesEveryWritingShape(t *testing.T) {
	record := `# EPIC-CV-010 — Shapes

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a table row WHEN in the read section THEN it is captured |

## 4. BDD acceptance scenarios (SCN) — upper loop

| id | Scenario |
|---|---|
| SCN-CV-002 | GIVEN a numbered heading WHEN the regex does not know it THEN the section reads as absent |
| SCN-CV-003 | GIVEN a second row THEN it is lost with the first |

## Scenarios in bullet form

- **SCN-CV-004** — GIVEN a bold bullet THEN nothing reads it.
- **SCN-CV-005** — GIVEN a second bullet THEN nothing reads it either.

## Scenario detail

### SCN-CV-006 — a heading block

GIVEN a heading block THEN its body is prose no parser structures.

## Evidence map

| Item | Failing → Passing test | Code |
|---|---|---|
| SCN-CV-001 | ` + "`a_test.exs`" + ` | ` + "`a.ex`" + ` |
| SCN-CV-004/005 | ` + "`b_test.exs`" + ` | — |
`
	got := countScenarioShapes(record)
	want := map[string]int{
		"table-in-read-section":   1,
		"table-in-other-section":  2,
		"bullet":                  2,
		"heading-block":           1,
		"table-outside-a-section": 2,
	}
	for shape, n := range want {
		if len(got[shape]) != n {
			t.Errorf("shape %q = %d, want %d (all: %v)", shape, len(got[shape]), n, got)
		}
	}
	// A record with TWO scenario-titled sections still loses the second one:
	// the parser reads one section, and the shape count is what says so.
	if len(got["table-in-other-section"]) != 2 {
		t.Errorf("the second scenario section must stay visible as uncaptured: %v", got)
	}
}

// The negative population, stated as a test because it is the trap: an
// `## Evidence map` table leads its rows with SCN ids and defines nothing.
// Counting those rows would inflate every scenario gap in the corpus.
func TestEvidenceMapCitationsAreNotScenarioDefinitions(t *testing.T) {
	record := `# EPIC-CV-011 — Citations only

## Evidence map

| Item | Failing → Passing test |
|---|---|
| SCN-CV-001 | ` + "`a_test.exs`" + ` |
| SCN-CV-002 | ` + "`b_test.exs`" + ` |

## Coverage of acceptance scenarios

| Scenario | Covered |
|---|---|
| SCN-CV-003 | yes |

## Evidence map, in bullets

- **SCN-CV-004 (upper half) — UPPER_VALIDATED (live), 2026-08-20:** RED 1 failing then GREEN.
- **SCN-CV-005** — a definition-shaped bullet an author put in the evidence section.
`
	got := countScenarioShapes(record)
	for _, shape := range []string{"table-in-read-section", "table-in-other-section", "bullet", "heading-block"} {
		if len(got[shape]) != 0 {
			t.Fatalf("citation table counted as a %s definition: %v", shape, got)
		}
	}
	if len(got["table-outside-a-section"]) != 3 {
		t.Fatalf("citations must still be COUNTED and labelled, not dropped: %v", got)
	}
	// The instrument must exclude exactly what the parser excludes, or it
	// reports a loss no widening can ever close: an evidence bullet by its
	// shape, and a definition-shaped bullet by the section it was filed in.
}

// A heading marking its section out of scope names scenarios the record is
// explicitly not asking for; it must not read as an uncaptured definition.
func TestOutOfScopeScenarioHeadingsAreNotDefinitions(t *testing.T) {
	for _, heading := range []string{
		"Deferred acceptance scenarios",
		"Acceptance scenarios (deferred/out of scope)",
		"Evidence map",
		"Scenario coverage",
		"Traceability — scenarios",
	} {
		if scenarioSectionHeading(heading) {
			t.Errorf("%q must not be read as a scenario-definition section", heading)
		}
	}
	for _, heading := range []string{
		"Acceptance scenarios",
		"BDD acceptance scenarios",
		"4. BDD acceptance scenarios (SCN) — upper loop",
		"Scenarios",
	} {
		if !scenarioSectionHeading(heading) {
			t.Errorf("%q must be read as a scenario-definition section", heading)
		}
	}
}

// End to end: every definition shape is reported with its own captured arm, so
// the coverage line is checkable per shape rather than in one total.
func TestScenarioShapeCoverageIsReportedPerShape(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the shapes counted so that the gap is a number.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a read section THEN the row is captured |

## Acceptance, continued

- **SCN-CV-002** — GIVEN a bullet THEN it is captured by its shape.

### SCN-CV-003 — heading block

GIVEN a heading block THEN its body is the scenario.
`)
	r := reportOf(t, root)

	c := countGroup(t, r, scenarioCoverageGroup)
	if c.Rows != 3 || c.Ops != 3 {
		t.Fatalf("scenario coverage = %d on disk / %d captured, want 3/3", c.Rows, c.Ops)
	}
	for _, shape := range []string{"table-in-read-section", "bullet", "heading-block"} {
		s := countGroup(t, r, "  scenario-shape: "+shape+", in 1 file(s)")
		if s.Rows != 1 || s.Ops != 1 {
			t.Errorf("shape %q = %d/%d, want 1/1", shape, s.Rows, s.Ops)
		}
	}
	if ls := lossesWithFieldPrefix(r.Losses, "scenario-shape:"); len(ls) != 0 {
		t.Fatalf("nothing is uncaptured here: %+v", ls)
	}
}

// The check still has to FIRE. One record cannot have two scenario sections
// read as one — the parser takes the first, by design, so a record that writes
// a second one loses it, and the report says so with its file count rather than
// letting the total absorb it.
func TestASecondScenarioSectionIsReportedAsLost(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the shapes counted so that the gap is a number.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a read section THEN the row is captured |

## BDD acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-002 | GIVEN a second scenario section THEN its rows reach nothing |
`)
	r := reportOf(t, root)

	c := countGroup(t, r, scenarioCoverageGroup)
	if c.Rows != 2 || c.Ops != 1 {
		t.Fatalf("scenario coverage = %d on disk / %d captured, want 2/1", c.Rows, c.Ops)
	}
	l := lossWithField(r.Losses, "scenario-shape:table-in-other-section")
	if l == nil || l.Category != LossLost {
		t.Fatalf("the second section must be reported as lost, got %+v", l)
	}
	if !strings.Contains(l.Detail, "1 record(s)") {
		t.Fatalf("the shape loss must state its file count: %+v", l)
	}
	if lossWithField(r.Losses, "scenario-section-heading") == nil {
		t.Fatal("the heading arm of the same loss must be reported too")
	}
}

// The negative half: a corpus whose scenarios are all written in the shape the
// parser reads reports no shape loss at all.
func TestScenarioShapeCoverageSilentWhenEveryScenarioIsCaptured(t *testing.T) {
	r := reportOf(t, cleanEpicCorpus(t))

	if ls := lossesWithFieldPrefix(r.Losses, "scenario-shape:"); len(ls) != 0 {
		t.Fatalf("every scenario is in the read shape; no shape loss may be claimed: %+v", ls)
	}
	if l := lossWithField(r.Losses, "scenario-section-heading"); l != nil {
		t.Fatalf("the recognized heading must not be reported as unrecognized: %+v", l)
	}
}

// ---------------------------------------------------------------- archival coverage

func TestArchivalCoverageNamesUncarriedRetiredFiles(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only/tasks/TASK-CV-1.md", "# A task file no carrier names\n")
	writeFixtureFile(t, root, "epics/README.md", "# The epics index\n")

	// The carrier set is built here rather than taken from a batch: this test
	// proves the CHECK fires on an uncarried population, and it must keep
	// proving that whatever the builder currently emits. What the builder
	// leaves uncarried is a separate claim, pinned by the archival tests.
	var r FidelityReport
	carried := map[string]bool{}
	for _, f := range retiredFilesOnDisk(root) {
		switch f {
		case "epics/EPIC-CV-001-only/tasks/TASK-CV-1.md", "epics/README.md":
			continue
		}
		carried[f] = true
	}
	r.scanArchivalCoverage(root, carried)

	l := lossWithField(r.Losses, "uncarried-retired-files")
	if l == nil || l.Category != LossLost {
		t.Fatalf("uncarried retired files must be FAIL-level, got %+v", l)
	}
	for _, want := range []string{"epics/EPIC-CV-001-only/tasks/TASK-CV-1.md", "byte(s)"} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("the finding must state example paths and a byte total; missing %q in %q", want, l.Detail)
		}
	}
	c := countGroup(t, r, "archival-coverage (files matched by the retired-path families → document/spec carriers)")
	if c.Rows <= c.Ops {
		t.Fatalf("the count must show the gap in both arms, got rows %d ops %d", c.Rows, c.Ops)
	}
	listed := map[string]bool{}
	for _, p := range c.MissingFromOps {
		listed[p] = true
	}
	if !listed["epics/EPIC-CV-001-only/tasks/TASK-CV-1.md"] || !listed["epics/README.md"] {
		t.Fatalf("every uncarried path must be itemized in the count group, got %v", c.MissingFromOps)
	}
}

// The reverse direction: a carrier naming a retired path that no longer exists.
func TestArchivalCoverageNamesStaleCarriers(t *testing.T) {
	root := cleanEpicCorpus(t)
	var r FidelityReport
	carried := map[string]bool{"epics/EPIC-CV-404-deleted.md": true}
	for _, f := range retiredFilesOnDisk(root) {
		carried[f] = true
	}
	r.scanArchivalCoverage(root, carried)

	l := lossWithField(r.Losses, "archival-carrier-stale")
	if l == nil || l.RecordID != "epics/EPIC-CV-404-deleted.md" {
		t.Fatalf("a carrier for a vanished retired file must be named, got %+v", l)
	}
	if lossWithField(r.Losses, "uncarried-retired-files") != nil {
		t.Fatal("every on-disk file is carried here; no forward-direction loss may be claimed")
	}
}

// The family matcher, with the near-misses that must NOT match.
func TestRetiredFamilyMatcherBoundaries(t *testing.T) {
	for _, in := range []string{
		"tasks/CV-REQUIREMENTS.md", "WORKLIST.md", "BACKLOG.md", "PROGRESS.md",
		"epics/EPIC-CV-001.md", "epics/EPIC-CV-001/specs/requirements.md",
		"process/gap-register.md", "docs/85-loop-review-queue.md",
	} {
		if !matchesRetiredFamily(in) {
			t.Errorf("%q must match the retired population", in)
		}
	}
	for _, out := range []string{
		"tasks/README.md",              // not a *-REQUIREMENTS.md ledger
		"tasks/sub/CV-REQUIREMENTS.md", // the glob does not cross a directory
		"docs/07-api-contracts.md",     // a design document, not the review queue
		"process/08-open-questions.md.bak",
		"epicsummary.md", "modernpath-core/epics/EPIC-X-1.md",
	} {
		if matchesRetiredFamily(out) {
			t.Errorf("%q must NOT match the retired population", out)
		}
	}
}

// ---------------------------------------------------------------- raw evidence cells

func TestEvidenceCellResidueDetectsDroppedProse(t *testing.T) {
	cases := []struct {
		name, cell string
		wantLoss   bool
	}{
		{"backticks plus prose", "`a_test.exs` 13/13 — RED first; live probe verified", true},
		{"backticks plus a trailing claim", "`x.ex` · `y.ex` · BFF passthrough not built", true},
		{"references only", "`a_test.exs` · `b_test.exs`", false},
		{"references with bare connectors", "`a.go`, `b.go` and `c.go`", false},
		{"no backticks at all", "pending — nothing written yet", false},
		{"empty", "—", false},
		{"blank", "", false},
	}
	for _, tc := range cases {
		got := evidenceCellResidue(tc.cell) != ""
		if got != tc.wantLoss {
			t.Errorf("%s: residue=%v want %v (cell %q → %q)", tc.name, got, tc.wantLoss, tc.cell, evidenceCellResidue(tc.cell))
		}
	}
}

// The population is every cell with content, and the question is whether the
// payload carries that content verbatim. Strip the carrier and every one of
// them must resurface — a check that goes quiet because the loss it watches was
// fixed, rather than because it verified the fix, measures nothing.
const cellRawLedger = `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | — | doc | ` + "`a_test.exs`" + ` 13/13 — RED first, hardened after review | ` + "`a.ex`" + ` · BFF passthrough not built |
| REQ-CV-002 | Clean cells | MVP | PROPOSED | — | doc | ` + "`b_test.exs`" + ` · ` + "`c_test.exs`" + ` | ` + "`b.ex`" + ` |
| REQ-CV-003 | Empty cells | MVP | PROPOSED | — | doc | — | — |
`

// stripExtras removes the raw carrier from every requirement payload, so the
// same corpus can be reported with and without it.
func stripExtras(ops []Op) []Op {
	var out []Op
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			cp := map[string]any{}
			for k, v := range op.Payload {
				cp[k] = v
			}
			delete(cp, "extra_columns")
			op = Op{Type: op.Type, Payload: cp}
		}
		out = append(out, op)
	}
	return out
}

func TestEvidenceCellsAreMeasuredAgainstTheRawCarrier(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", cellRawLedger)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	r := BuildFidelityReport(root, m, data, ops)
	if ls := lossesWithFieldPrefix(r.Losses, "raw:"); len(ls) != 0 {
		t.Fatalf("the payload carries every cell verbatim; nothing is lost: %+v", ls)
	}
	c := countGroup(t, r, "evidence cells carried raw (Tests/Code columns)")
	if c.Rows != 2 || c.Ops != 2 {
		t.Fatalf("population = %d rows / %d carried, want 2/2 (the em-dash row has no cells)", c.Rows, c.Ops)
	}

	// Remove the carrier and every cell with content is reported again.
	degraded := BuildFidelityReport(root, m, data, stripExtras(ops))
	for _, id := range []string{"REQ-CV-001", "REQ-CV-002"} {
		for _, field := range []string{"raw:tests-cell", "raw:code-cell"} {
			l := findLoss(degraded.Losses, id, field)
			if l == nil || l.Category != LossLost {
				t.Fatalf("without the carrier %s %s must be reported lost, got %+v", id, field, l)
			}
		}
	}
	if l := findLoss(degraded.Losses, "REQ-CV-003", "raw:tests-cell"); l != nil {
		t.Fatalf("an em-dash cell is absence, never a loss: %+v", l)
	}
	if c := countGroup(t, degraded, "evidence cells carried raw (Tests/Code columns)"); c.Ops != 0 {
		t.Fatalf("without the carrier no row is fully carried, got %d", c.Ops)
	}
}

// A row whose DETAIL BLOCK supplies the citations still wrote something in its
// cells, and REQ-CROSS-222 §6 is unconditional: the cells are checked there too.
func TestEvidenceCellsAreCheckedWhenTheDetailBlockCarriesEvidence(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | — | doc | `+"`a_test.exs`"+` 13/13 — RED first | `+"`a.ex`"+` and prose |

### REQ-CV-001 — Only behavior

- **Statement:** It does the thing.
- **Tests:** `+"`TEST:a_test.exs:TestThing`"+`
- **Code:** `+"`a.ex`"+`
`)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	if ls := lossesWithFieldPrefix(BuildFidelityReport(root, m, data, ops).Losses, "raw:"); len(ls) != 0 {
		t.Fatalf("the cells ride raw beside the block's citations: %+v", ls)
	}
	degraded := BuildFidelityReport(root, m, data, stripExtras(ops))
	if ls := lossesWithFieldPrefix(degraded.Losses, "raw:"); len(ls) != 2 {
		t.Fatalf("without the carrier both cells are lost, got %+v", ls)
	}
}

// What the raw carrier does NOT fix: a cell with no backticks still becomes one
// parsed reference shortened to 200 runes. The text survives; the parsed
// structure is capped, and the report says so rather than implying the cell is
// fully structured.
func TestParsedEvidenceRefCapIsReported(t *testing.T) {
	root := cleanEpicCorpus(t)
	long := strings.TrimSpace(strings.Repeat("evidence pending ", 30))
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Long cell | MVP | PROPOSED | — | doc | `+long+` | `+"`b.ex`"+` |
| REQ-CV-002 | Short cell | MVP | PROPOSED | — | doc | pending, nothing written yet | `+"`c.ex`"+` |
`)
	r := reportOf(t, root)

	l := findLoss(r.Losses, "REQ-CV-001", "refs:tests-cell")
	if l == nil || l.Category != LossTruncated {
		t.Fatalf("the capped parsed reference must be reported as truncated, got %+v", l)
	}
	if ls := lossesWithFieldPrefix(r.Losses, "raw:"); len(ls) != 0 {
		t.Fatalf("the cell text itself is carried whole: %+v", ls)
	}
	// A short backtick-free cell reaches its reference intact — the cap is a
	// fact about long cells, not about prose cells.
	if l := findLoss(r.Losses, "REQ-CV-002", "refs:tests-cell"); l != nil {
		t.Fatalf("a cell under the cap is not truncated: %+v", l)
	}
}

// The cap is not a fact about backticks, and it does not count runes.
// splitEvidenceRefs falls back to the whole cell whenever it extracts no
// backticked SPAN — a lone unmatched backtick extracts none — and capRunes
// cuts in UTF-16 units, so an astral-plane cell is shortened well under 200
// runes. Both are real truncations, and a detector that reads the cell's shape
// instead of asking the splitter calls both of them clean.
func TestParsedEvidenceRefCapIsReportedForCellsAShapeTestMisses(t *testing.T) {
	root := cleanEpicCorpus(t)
	stray := "pending: see `notes " + strings.Repeat("and more detail ", 15) + "end"
	astral := "no test yet " + strings.Repeat("🚀", 120) + " done"
	if utf8.RuneCountInString(astral) > evidenceRefCap {
		t.Fatalf("the astral fixture must sit UNDER the cap in runes, got %d", utf8.RuneCountInString(astral))
	}
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Lone backtick | MVP | PROPOSED | — | doc | `+stray+` | `+"`b.ex`"+` |
| REQ-CV-002 | Astral cell | MVP | PROPOSED | — | doc | `+astral+` | `+"`c.ex`"+` |
`)
	r := reportOf(t, root)

	for _, id := range []string{"REQ-CV-001", "REQ-CV-002"} {
		l := findLoss(r.Losses, id, "refs:tests-cell")
		if l == nil || l.Category != LossTruncated {
			t.Fatalf("%s: the parsed citation IS shortened, so it must be reported truncated, got %+v", id, l)
		}
	}
	// The cells themselves still ride whole — this is about the parsed side.
	if ls := lossesWithFieldPrefix(r.Losses, "raw:"); len(ls) != 0 {
		t.Fatalf("the cell text itself is carried whole: %+v", ls)
	}
}

// ---------------------------------------------------------------- epic UR membership

func TestDeclaredEpicURsReadsEveryLinkedTableRow(t *testing.T) {
	record := `# EPIC-CV-020 — Two parents

## Linked user requirements

| UR | Statement |
|---|---|
| UR-CV-001 | As a reader I want the first outcome so that it is delivered. |
| UR-CV-002 | As an operator I want the second outcome so that it is delivered too. |

## How UR-NOPE-999 relates to this epic

A passing mention is not a declaration.
`
	got := declaredEpicURs(record)
	if len(got) != 2 || got[0] != "UR-CV-001" || got[1] != "UR-CV-002" {
		t.Fatalf("every linked-UR row is a declaration, got %v", got)
	}
	for _, id := range got {
		if id == "UR-NOPE-999" {
			t.Fatal("a cross-reference heading must never become a declaration")
		}
	}
}

// The declared side has to see every id-led heading, not every second one. A
// pattern that spans the heading AND its body consumes the `\n##` that ends the
// body, so the next scan resumes inside the following heading and skips it.
// A blind spot here is worse than an undercount: the check reports declared
// minus carried, so an id the instrument cannot see and the builder does not
// carry cancels to nothing and the lost membership is reported as no loss.
func TestDeclaredEpicURsReadsEveryIDLedHeading(t *testing.T) {
	record := `# EPIC-CV-021 — three id-led headings

## UR-CV-011 — the first outcome

As a reader I want the first outcome so that it is delivered.

## UR-CV-012 — the second outcome

As an operator I want the second outcome so that it is delivered.

## UR-CV-013 — the third outcome

As an auditor I want the third outcome so that it is delivered.
`
	got := declaredEpicURs(record)
	want := []string{"UR-CV-011", "UR-CV-012", "UR-CV-013"}
	if len(got) != len(want) {
		t.Fatalf("declared = %v, want %v", got, want)
	}
	for i, id := range want {
		if got[i] != id {
			t.Fatalf("declared = %v, want %v", got, want)
		}
	}
}

// A declaration the builder cannot carry: the row names a user requirement and
// states nothing about it, so no entity can be built for it without inventing
// its statement. The membership is real in the record and absent from the
// payload, which is exactly the diff this check exists to surface — the epic
// count and the user-requirement count both still reconcile.
func TestEpicURMembershipReportsUncarriedDeclarations(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## Linked user requirements

| UR | Statement |
|---|---|
| UR-CV-001 | As a reader I want the first outcome so that it is delivered. |
| UR-CV-002 |  |

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |
`)
	r := reportOf(t, root)

	l := findLoss(r.Losses, "EPIC-CV-001", "epic-ur-membership")
	if l == nil || l.Category != LossLost {
		t.Fatalf("a second declared UR that no payload carries must be FAIL-level, got %+v", l)
	}
	if !strings.Contains(l.Detail, "UR-CV-002") {
		t.Fatalf("the finding must name the uncarried UR: %q", l.Detail)
	}
	c := countGroup(t, r, "epic user-requirement membership (declared in the record → carried by that epic's payload)")
	if c.Rows != 2 || c.Ops != 1 {
		t.Fatalf("membership = %d declared / %d carried, want 2/1", c.Rows, c.Ops)
	}
}

// The negative half: one declared UR, carried, reports nothing.
func TestEpicURMembershipSilentWhenEveryDeclarationIsCarried(t *testing.T) {
	r := reportOf(t, cleanEpicCorpus(t))

	if l := lossWithField(r.Losses, "epic-ur-membership"); l != nil {
		t.Fatalf("the single declared UR is carried; no membership loss may be claimed: %+v", l)
	}
	c := countGroup(t, r, "epic user-requirement membership (declared in the record → carried by that epic's payload)")
	if c.Rows != 1 || c.Ops != 1 {
		t.Fatalf("membership = %d declared / %d carried, want 1/1", c.Rows, c.Ops)
	}
}

// Two stated declarations, both carried: the check counts both and claims
// nothing. Silence here is what makes the finding above meaningful.
func TestEpicURMembershipSilentWhenBothDeclarationsAreCarried(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## Linked user requirements

| UR | Statement |
|---|---|
| UR-CV-001 | As a reader I want the first outcome so that it is delivered. |
| UR-CV-002 | As an operator I want the second outcome so that it is delivered too. |

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |
`)
	r := reportOf(t, root)

	if l := lossWithField(r.Losses, "epic-ur-membership"); l != nil {
		t.Fatalf("both declarations are carried; no membership loss may be claimed: %+v", l)
	}
	c := countGroup(t, r, "epic user-requirement membership (declared in the record → carried by that epic's payload)")
	if c.Rows != 2 || c.Ops != 2 {
		t.Fatalf("membership = %d declared / %d carried, want 2/2", c.Rows, c.Ops)
	}
}

// ---------------------------------------------------------------- payload neutrality

// The instruments must not change what is emitted. Same corpus, same ops:
// this is the property that lets the report be strengthened without a
// re-sync, and it is cheap enough to assert rather than assume.
func TestFidelityInstrumentsDoNotChangeTheOpStream(t *testing.T) {
	root := cleanEpicCorpus(t)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	read := func(rel string) string { return ReadEpicRecord(root, rel) }

	before := BuildOps(data, read, "2026-08-22")
	_ = BuildFidelityReport(root, m, data, before)
	after := BuildOps(data, read, "2026-08-22")

	if len(before) != len(after) {
		t.Fatalf("op count changed: %d → %d", len(before), len(after))
	}
	for i := range before {
		if before[i].Type != after[i].Type {
			t.Fatalf("op %d type changed: %s → %s", i, before[i].Type, after[i].Type)
		}
		bh, _ := before[i].Payload["content_hash"].(string)
		ah, _ := after[i].Payload["content_hash"].(string)
		if bh != ah {
			t.Fatalf("op %d (%s) content hash changed: %s → %s", i, before[i].Type, bh, ah)
		}
	}
}
