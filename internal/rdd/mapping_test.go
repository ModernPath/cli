package rdd

// Data-model mapping audit fixes (independent mapping
// review over the live store). Each test pins one measured loss:
// four epics' scenario tables lost to a heading-regex miss, membership
// over-capture from the whole-record fallback, ten URs unextracted from
// the bullet declaration form, UR work_status derived from the lossy
// five-bucket state, WORKLIST range rows becoming phantom epics,
// gap-register columns dropped, and the epic→UR edge never emitted.
// RED first: every arm fails against the current extractors.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// Mapping audit #1: EPIC-CLI-001/002/003 + EPIC-CMP-903 use a bare
// `## Scenarios` heading and synced 0 of their scenario rows — including
// this import epic's own 8 acceptance scenarios.
func TestScenarioSectionAdmitsBareHeading(t *testing.T) {
	record := `# EPIC-MAP-001 — Fixture

## Scenarios
| SCN | Given / When / Then | Realizes |
|---|---|---|
| SCN-MAP-001 | Given a thing, When poked, Then it reacts | REQ-MAP-001 |

## Risks
`
	scenarios := ParseScenarios(record, "EPIC-MAP-001")
	if len(scenarios) != 1 {
		t.Fatalf("a bare '## Scenarios' table must parse, got %d rows", len(scenarios))
	}
}

// Mapping audit #2: with no members section, the whole-record scan harvested
// incidental mentions (EPIC-CLI-002 gained three false members). The
// `**Realizes:**` line is the declaration and must win over the fallback.
func TestRequirementIDsPreferRealizesLine(t *testing.T) {
	record := `# EPIC-MAP-002 — Fixture

**Realizes:** REQ-MAP-001, REQ-MAP-002

## Evidence

A prose mention of REQ-OTHER-099 that is not a member.
`
	ids := requirementIDsOf(record)
	if len(ids) != 2 || ids[0] != "REQ-MAP-001" || ids[1] != "REQ-MAP-002" {
		t.Fatalf("the Realizes line must bound membership, got %v", ids)
	}
}

// Membership is a declaration, never a reading of the prose. A record that
// declares nothing names nothing: the whole-record scan turned every incidental
// mention — a decision citing a neighbouring requirement, an evidence map, a
// source list — into an asserted edge, and 270 of the corpus's 384 membership
// ids came from that scan. Nine of them named requirements that exist in no
// ledger at all, which is what makes the direction wrong rather than generous:
// the scan cannot tell a member from a mention, so it asserts both.
func TestRequirementIDsWithoutDeclarationAreEmpty(t *testing.T) {
	record := `# EPIC-MAP-006 — Fixture

## Decisions

Settled alongside REQ-MAP-101 and superseding REQ-MAP-102.

## Evidence map

| Scenario | Requirement |
|---|---|
| SCN-MAP-001 | REQ-MAP-103 |
`
	if ids := requirementIDsOf(record); len(ids) != 0 {
		t.Fatalf("a record with no membership declaration must name no members, got %v", ids)
	}
}

// The negative half of the same rule: a record that DOES declare keeps exactly
// its declared set, and the prose around it stays prose. Without this arm the
// change above is satisfied by returning nothing at all.
func TestRequirementIDsFromMembersSectionExcludeProse(t *testing.T) {
	record := `# EPIC-MAP-007 — Fixture

## Requirements in this epic

` + "`REQ-MAP-001`" + ` · ` + "`REQ-MAP-002`" + `

## Decisions

Bounded by REQ-MAP-101, which is not a member.
`
	ids := requirementIDsOf(record)
	if len(ids) != 2 || ids[0] != "REQ-MAP-001" || ids[1] != "REQ-MAP-002" {
		t.Fatalf("the members section must bound membership, got %v", ids)
	}
}

// REQ-CROSS-263: every membership carrier is one declaration vocabulary. The
// epic payload and the approval-gate holds must read the same union, with
// section declarations before Realizes lines and first occurrence winning.
func TestRequirementMembershipUnionsEveryDeclaredShapeForBothReaders(t *testing.T) {
	record := `# EPIC-MAP-263 — Fixture

## Requirements realized
- **REQ-MAP-001** — first section declaration

## Linked requirements
- REQ-MAP-002

## Requirements
- REQ-MAP-003

- **Realizes:** REQ-MAP-004 · REQ-MAP-002
**Realizes:** REQ-MAP-005

## Approval
pending
`
	want := []string{"REQ-MAP-001", "REQ-MAP-002", "REQ-MAP-003", "REQ-MAP-004", "REQ-MAP-005"}
	if got := requirementIDsOf(record); !slices.Equal(got, want) {
		t.Fatalf("epic membership = %v, want %v", got, want)
	}
	if got := requirementIDsOfSection(record); !slices.Equal(got, want) {
		t.Fatalf("gate-hold membership = %v, want %v", got, want)
	}
}

func TestRequirementMembershipClassifiesRangesAlternatesAndProse(t *testing.T) {
	record := `# EPIC-MAP-263 — Fixture

## Requirements in this epic

REQ-MAP-050 … REQ-MAP-052
REQ-MAP-060..062
REQ-MAP-070/071
(REQ-MAP-999 is only an annotation) REQ-MAP-080
REQ-MAP-081 is EPIC-MAP-OTHER.
`
	want := []string{
		"REQ-MAP-050", "REQ-MAP-051", "REQ-MAP-052",
		"REQ-MAP-060", "REQ-MAP-061", "REQ-MAP-062",
		"REQ-MAP-070", "REQ-MAP-071", "REQ-MAP-080",
	}
	if got := requirementIDsOf(record); !slices.Equal(got, want) {
		t.Fatalf("classified membership = %v, want %v", got, want)
	}
}

func TestRequirementMembershipExcludesMixedHeadingsAndPreservesEmptySemantics(t *testing.T) {
	excluded := `# EPIC-MAP-263 — Fixture

## System requirements
Evidence mentions REQ-MAP-101 and the prose range REQ-MAP-102 … REQ-MAP-109.

## Linked user requirements
UR-MAP-001 is supported by REQ-MAP-110.
`
	if got := requirementIDsOf(excluded); len(got) != 0 {
		t.Fatalf("mixed headings are not membership declarations: %v", got)
	}
	if p := BuildEpicOp(Epic{ID: "EPIC-MAP-263"}, excluded).Payload; !slices.Equal(payloadIDList(p, "requirement_external_ids"), []string{}) {
		t.Fatalf("a record with no declaration keeps explicit pruning, payload=%v", p)
	}

	recognizedEmpty := `# EPIC-MAP-263 — Fixture

## Requirements realized
(REQ-MAP-999 is a citation, not a member.)
`
	if p := BuildEpicOp(Epic{ID: "EPIC-MAP-263"}, recognizedEmpty).Payload; p["requirement_external_ids"] != nil {
		t.Fatalf("a recognized empty declaration must omit membership, payload=%v", p)
	}
}

func TestRecognizedEmptyMembershipIsFlaggedByTheReport(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want a suspicious empty declaration kept visible.

## Requirements realized

(REQ-CV-999 is only a citation.)
`)

	r := reportOf(t, root)
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "EPIC-CV-001") && strings.Contains(h.Detail, "membership") {
			return
		}
	}
	t.Fatalf("recognized-empty membership was not flagged: %+v", r.Hygiene)
}

// A fifth membership shape: a top-of-record `**Requirements:**` bold metadata
// line, alongside the existing `**Release:**`/`**Specification:**` banner —
// distinct from `**Realizes:**` only in label text, same declaration
// semantics. EPIC-CLI-004, EPIC-CMP-903 and EPIC-CMP-904 use exactly this
// shape and none of the four recognized H2 headings, so
// `requirementMembershipOf` never recognized their declaration and all three
// synced with 0 rows in epic_system_requirements (verified against the
// store).
func TestRequirementMembershipRecognizesBoldRequirementsMetadataLine(t *testing.T) {
	record := `# EPIC-MAP-264 — Fixture

**Release:** modernpath-v1-09
**Specification:** SPEC-APPROVED
**Requirements:** REQ-MAP-065 · REQ-MAP-066 · REQ-MAP-067

## User outcome

As a reader I want the metadata-line declaration recognized.
`
	want := []string{"REQ-MAP-065", "REQ-MAP-066", "REQ-MAP-067"}
	if got := requirementIDsOf(record); !slices.Equal(got, want) {
		t.Fatalf("bold Requirements: metadata line must declare membership, got %v, want %v", got, want)
	}
	if p := BuildEpicOp(Epic{ID: "EPIC-MAP-264"}, record).Payload; !slices.Equal(payloadIDList(p, "requirement_external_ids"), want) {
		t.Fatalf("epic payload must carry the metadata-line membership, payload=%v", p)
	}

	// A range, exactly as EPIC-CLI-004 declares it.
	ranged := `# EPIC-MAP-265 — Fixture

**Release:** modernpath-v1-09
**Requirements:** REQ-MAP-070..072

## User outcome
`
	if got := requirementIDsOf(ranged); !slices.Equal(got, []string{"REQ-MAP-070", "REQ-MAP-071", "REQ-MAP-072"}) {
		t.Fatalf("a ranged metadata-line declaration must expand, got %v", got)
	}
}

// Mapping audit #3: ten URs (70 derives edges) were never extracted because
// their declaration is a BULLET under `## Linked user requirements`.
func TestLinkedURBulletFormParses(t *testing.T) {
	record := `# EPIC-MAP-003 — Fixture

## User outcome

Prose without an id.

## Linked user requirements

- UR-MAP-006 — As a reader, blueprints stay faithful to the code.
`
	ur, ok := ParseEpicUserRequirement(record)
	if !ok || ur.ID != "UR-MAP-006" {
		t.Fatalf("the bullet declaration form must parse, got ok=%v %+v", ok, ur)
	}
	if !strings.Contains(ur.Statement, "faithful") {
		t.Fatalf("the bullet's statement must ride along: %q", ur.Statement)
	}
}

// Mapping audit #11: an IN_REVIEW epic's UR synced as PROPOSED — the UR is
// exactly as far along as its epic, and the exact token now exists.
func TestURWorkStatusRidesProcessStatus(t *testing.T) {
	epic := Epic{ID: "EPIC-MAP-004", State: "other", ProcessStatus: "IN_REVIEW"}
	op := BuildUserRequirementOp(UserRequirement{ID: "UR-MAP-004", Statement: "s"}, epic)
	if op.Payload["work_status"] != "IN_REVIEW" {
		t.Fatalf("the UR must carry the epic's exact lifecycle token, got %v", op.Payload["work_status"])
	}
	// Without a token the five-bucket mapping stays the honest fallback.
	bare := BuildUserRequirementOp(UserRequirement{ID: "UR-MAP-005", Statement: "s"}, Epic{ID: "E", State: "done"})
	if bare.Payload["work_status"] != "DONE" {
		t.Fatalf("the bucket fallback must hold without a token, got %v", bare.Payload["work_status"])
	}
}

// Mapping audit #6: WORKLIST range rollup rows (`EPIC-FE-059..064`) synced
// as phantom epic entities.
func TestWorklistSkipsRangeRows(t *testing.T) {
	worklist := `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-MAP-001 | — | UR-MAP-001 | SCN-MAP-001 | REQ-MAP-001 | T1 | — | — | **DONE** — done | — | — |
| EPIC-FE-059..064 | — | — | — | — | — | — | — | **DONE** — range rollup | — | — |
`
	epics := ParseWorklist(worklist, nil, map[string]bool{})
	if len(epics) != 1 || epics[0].ID != "EPIC-MAP-001" {
		t.Fatalf("a range rollup row is an aggregate, never an epic entity: %+v", epics)
	}
}

// Mapping audit #9: the gap register is five columns wide; Deferred-Until
// and Related-Reqs were silently dropped.
func TestBacklogRowsFoldExtraCells(t *testing.T) {
	gaps := `# Gap register

| ID | Description | Owning Context | Deferred Until | Related Reqs |
|---|---|---|---|---|
| GAP-MAP-2 | Something missing | CROSS | post-v1 | REQ-CROSS-029 (IN_REVIEW) |
`
	rows := ParseBacklogRows("process/gap-register.md", gaps, "gap")
	if len(rows) != 1 {
		t.Fatalf("want 1 gap row, got %d", len(rows))
	}
	for _, want := range []string{"post-v1", "REQ-CROSS-029"} {
		if !strings.Contains(rows[0].NotesMD, want) {
			t.Fatalf("extra cells must fold into notes, missing %q: %+v", want, rows[0])
		}
	}
}

// Coverage validation: a data row whose cell text contains
// "---" (a quoted git-diff marker, a horizontal-rule mention) is content,
// not a table separator. Only a row whose every cell is dashes divides a
// table.
func TestBacklogRowWithDashesInContentIsNotASeparator(t *testing.T) {
	backlog := `# BACKLOG

| Discovery | Notes | Tracked as |
|---|---|---|
| **Server WAF false-positives** | Any real diff (` + "`index a..b`, `--- a/…`" + ` markers) trips it. | ROUTED |
| **Second row** | plain notes | NEW |
`
	rows := ParseBacklogRows("BACKLOG.md", backlog, "backlog")
	if len(rows) != 2 {
		t.Fatalf("a diff marker inside a cell is content, not a separator: want 2 rows, got %d: %+v", len(rows), rows)
	}
	if !strings.Contains(rows[0].Title, "WAF") {
		t.Fatalf("the WAF row must survive: %+v", rows[0])
	}
}

// Coverage validation: a blank line splitting one logical
// table must not convert the next data row into a phantom header. A row is
// a header only when a separator row follows it directly.
func TestBacklogTableSplitByBlankLineLosesNoRow(t *testing.T) {
	backlog := `# BACKLOG

| Discovery | Notes | Tracked as |
|---|---|---|
| **First row** | notes one | NEW |

| **BFF proxy timeout kills POST /api/systems** | notes two | PROPOSED |
| **Third row** | notes three | NEW |
`
	rows := ParseBacklogRows("BACKLOG.md", backlog, "backlog")
	if len(rows) != 3 {
		t.Fatalf("a separator-less continuation has no header to skip: want 3 rows, got %d: %+v", len(rows), rows)
	}
	if !strings.Contains(rows[1].Title, "BFF proxy timeout") {
		t.Fatalf("the first row after the blank line is data: %+v", rows[1])
	}
}

// D5 "lands as preserved text", second family: WORKLIST work rows,
// docs/85 finding blocks, and over-cap OQ bodies build no structured op by
// design — but their files retire at the flip, so the files themselves ride
// the import verbatim as process documents, and their losses become carried
// text. Stripping the documents must bring every loss back.
func TestFidelityArchivedProcessFilesAreNotLosses(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	worklist := "# WORKLIST\n\n## Work Rows\n\n" +
		"| Task / slice | Epic | Status |\n|---|---|---|\n" +
		"| TASK-MP-901 | — | IN_REVIEW |\n"
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	rq := "# Review queue\n\n### RQ-902 (2026-08-22) — an observation\n\nObserved once, decided nothing.\n"
	if err := os.WriteFile(filepath.Join(root, "docs", "85-loop-review-queue.md"), []byte(rq), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	oq := "# Open questions\n\n## Q-MP-901 — a very long question\n\n" +
		strings.Repeat("Context the parse cap will cut. ", 200) + "\n"
	if err := os.WriteFile(filepath.Join(root, "process", "08-open-questions.md"), []byte(oq), 0o644); err != nil {
		t.Fatal(err)
	}

	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	carried := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_process_record" {
			id, _ := op.Payload["external_id"].(string)
			carried[id] = true
		}
	}
	for _, want := range []string{"WORKLIST.md", "docs/85-loop-review-queue.md", "process/08-open-questions.md"} {
		if !carried[want] {
			t.Fatalf("the retiring process file %s must ride the import in the byte archive; carried: %v", want, carried)
		}
	}

	r := BuildFidelityReport(root, m, data, ops)
	for _, field := range []string{"worklist-row", "finding-block", "oq-body"} {
		for _, l := range r.Losses {
			if l.Field == field {
				t.Fatalf("a document-carried file's content is preserved text, not a loss: %+v", l)
			}
		}
	}

	var withoutDocs []Op
	for _, op := range ops {
		if op.Type != "upsert_process_record" {
			withoutDocs = append(withoutDocs, op)
		}
	}
	r2 := BuildFidelityReport(root, m, data, withoutDocs)
	for _, field := range []string{"worklist-row", "finding-block", "oq-body"} {
		found := false
		for _, l := range r2.Losses {
			if l.Field == field {
				found = true
			}
		}
		if !found {
			t.Fatalf("without the carrier the %s loss must return", field)
		}
	}
}

// Found live: WORKLIST.md (290KB) and docs/85 (831KB) exceed the old
// 262,144-unit document cap, so the single-document carrier silently held
// only their first cap-worth while the report claimed them carried — the
// exact class this epic forbids. §225.6 successor (USER:2026-08-24): the
// byte archive carries the whole file in ONE unsplit process record, and
// the report suppresses losses only while the carried bytes cover the file.
func TestArchivalByteArchiveCarriesWholeFilesUnsplit(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	worklist := "# WORKLIST\n\n## Work Rows\n\n" +
		"| Task / slice | Epic | Status |\n|---|---|---|\n" +
		"| TASK-MP-903 | — | IN_REVIEW |\n\n" +
		strings.Repeat("Filler prose that pushes the file far past the old document cap. ", 10000)
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}

	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-22")

	var carried string
	for _, op := range ops {
		if op.Type == "upsert_process_record" && op.Payload["external_id"] == "WORKLIST.md" {
			enc, _ := op.Payload["raw_content"].(string)
			raw, err := base64.StdEncoding.DecodeString(enc)
			if err != nil {
				t.Fatal(err)
			}
			carried = string(raw)
		}
		if id, _ := op.Payload["external_id"].(string); strings.HasPrefix(id, "WORKLIST.md#part") {
			t.Fatalf("the byte archive must not split: %s", id)
		}
	}
	if carried != worklist {
		t.Fatalf("the over-cap file must ride whole: %d vs %d bytes", len(carried), len(worklist))
	}

	r := BuildFidelityReport(root, m, data, ops)
	for _, l := range r.Losses {
		if l.Field == "worklist-row" {
			t.Fatalf("a fully-carried file's rows are preserved text: %+v", l)
		}
	}

	// Dropping the carrier means the file is not carried — the losses return.
	var withoutArchive []Op
	for _, op := range ops {
		if op.Type == "upsert_process_record" {
			if id, _ := op.Payload["external_id"].(string); id == "WORKLIST.md" {
				continue
			}
		}
		withoutArchive = append(withoutArchive, op)
	}
	r2 := BuildFidelityReport(root, m, data, withoutArchive)
	found := false
	for _, l := range r2.Losses {
		if l.Field == "worklist-row" {
			found = true
		}
	}
	if !found {
		t.Fatalf("an absent carrier must not suppress the losses it does not carry")
	}
}

// Mapping audit #4: the epic op must name its defining UR so the
// epic_user_requirements join gains a sync writer.
func TestEpicOpCarriesUserRequirementIds(t *testing.T) {
	record := `# EPIC-MAP-005 — Fixture

## User outcome (UR-MAP-007)

As a mapper, the edge survives.
`
	op := BuildEpicOp(Epic{ID: "EPIC-MAP-005", State: "other"}, record)
	urs, _ := op.Payload["user_requirement_external_ids"].([]string)
	if len(urs) != 1 || urs[0] != "UR-MAP-007" {
		t.Fatalf("the epic payload must declare its UR, got %v", op.Payload["user_requirement_external_ids"])
	}
	bare := BuildEpicOp(Epic{ID: "EPIC-MAP-006", State: "other"}, "")
	if _, present := bare.Payload["user_requirement_external_ids"]; present {
		t.Fatal("an epic with no UR declaration must omit the key")
	}
}

// Mapping audit #7 (instrument half): a data row narrower than its header
// silently shifts columns (14 UI-ledger rows lost their source cells) —
// the fidelity report must flag the width mismatch with a location.
func TestFidelityFlagsUnderWidthRows(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	r := BuildFidelityReport(root, m, data, ops)

	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "narrower than its header") && h.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the short row must be flagged with a location: %+v", r.Hygiene)
	}
}

func writeMappingLedger(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := "# REQUIREMENTS — MP\n\n" +
		"| ID | Title | Stage | Status | UR | Source | Tests | Code |\n" +
		"|---|---|---|---|---|---|---|---|\n" +
		"| REQ-MP-001 | Full width | MVP | PROPOSED | UR-MP-001 | doc-a | — | — |\n" +
		"| REQ-MP-002 | Short row | MVP | PROPOSED | finding, this pass | — | — |\n"
	if err := os.WriteFile(filepath.Join(root, "tasks", "MP-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte("# WORKLIST\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Mapping documentation review: four losses the report did
// not name. §221.2 — every drop is named per record, never silent.

// The epic record body has no store carrier beyond the APPROVE gate's capped
// body — 146 of 187 records had none at all.
// D5 "lands as preserved text": the import carries every epic record
// verbatim as a system document, so the record prose survives the flip in
// the store — not only in git history.
func TestBuildOpsCarriesEpicRecordsAsDocuments(t *testing.T) {
	// §225.6 successor (USER:2026-08-24): the carrier is the byte archive.
	record := "# EPIC-MP-007 — Fixture\n\n## User outcome\n\nProse worth keeping, verbatim.\n"
	data := Data{Epics: []Epic{
		{ID: "EPIC-MP-007", Record: "epics/EPIC-MP-007-x/EPIC.md"},
		{ID: "EPIC-MP-008"}, // no record on disk — nothing to carry
	}}
	ops := BuildOps(data, func(rel string) string {
		if rel == "epics/EPIC-MP-007-x/EPIC.md" {
			return record
		}
		return "" // no archival process files in this fixture
	}, "2026-08-21")
	var rec map[string]any
	recCount := 0
	for _, op := range ops {
		if op.Type == "upsert_process_record" {
			recCount++
			if op.Payload["external_id"] == "epics/EPIC-MP-007-x/EPIC.md" {
				rec = op.Payload
			}
		}
	}
	if rec == nil || recCount != 1 {
		t.Fatalf("want exactly the recorded epic in the byte archive, got %d records (found=%v)", recCount, rec != nil)
	}
	raw, _ := base64.StdEncoding.DecodeString(rec["raw_content"].(string))
	if string(raw) != record {
		t.Fatalf("the record must be carried byte-exact: %d vs %d", len(raw), len(record))
	}
	if rec["record_type"] != "epic" {
		t.Fatalf("record_type = %v", rec["record_type"])
	}
	if h, _ := rec["content_hash"].(string); h == "" {
		t.Fatalf("process-record ops are hash-shadowed like everything else: %+v", rec)
	}
}

// With the record carried as a document, its body is preserved text — no
// record-body loss, and an APPROVE gate cut at the cap loses nothing either.
// Stripping the document ops must bring both losses back: the detector
// keys on the carrier, not on hope.
func TestFidelityRecordBodyCarriedByDocumentIsNoLoss(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	if err := os.MkdirAll(filepath.Join(root, "epics", "EPIC-MP-009-x"), 0o755); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("Decisions and reconnaissance worth keeping. ", 200) // > 6,000 units
	record := "# EPIC-MP-009 — Fixture\n\n## User outcome\n\nDone thing.\n\n" + long +
		"\n\n## Approval\n\nGranted `USER:2026-08-21` — accepted.\n"
	if err := os.WriteFile(filepath.Join(root, "epics", "EPIC-MP-009-x", "EPIC.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	worklist := "# WORKLIST\n\n## Epic Rollup\n\n" +
		"| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| EPIC-MP-009 | [r](epics/EPIC-MP-009-x/EPIC.md) | — | — | REQ-MP-001 | T1 | — | — | **DONE** | `USER:2026-08-21` | — |\n"
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-21")

	r := BuildFidelityReport(root, m, data, ops)
	if l := findLoss(r.Losses, "EPIC-MP-009", "record-body"); l != nil {
		t.Fatalf("a document-carried record body is preserved text, not a loss: %+v", l)
	}
	if l := findLoss(r.Losses, "APPROVE-EPIC-MP-009", "gate-body"); l != nil {
		t.Fatalf("content past the gate cap survives in the record document: %+v", l)
	}
	c, err := findCount(r, "epic-records")
	if err != nil || c.Rows != 1 || c.Ops != 1 {
		t.Fatalf("carried records must reconcile as a count group: %+v err=%v", c, err)
	}

	var withoutDocs []Op
	for _, op := range ops {
		if op.Type != "upsert_process_record" {
			withoutDocs = append(withoutDocs, op)
		}
	}
	r2 := BuildFidelityReport(root, m, data, withoutDocs)
	if l := findLoss(r2.Losses, "EPIC-MP-009", "record-body"); l == nil {
		t.Fatalf("without the document carrier the record body is a loss again")
	}
	if l := findLoss(r2.Losses, "APPROVE-EPIC-MP-009", "gate-body"); l == nil {
		t.Fatalf("without the carrier the gate cut is a loss again")
	}
}

func TestFidelityFlagsEpicRecordBodyWithoutCarrier(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	if err := os.MkdirAll(filepath.Join(root, "epics", "EPIC-MP-001-x"), 0o755); err != nil {
		t.Fatal(err)
	}
	record := "# EPIC-MP-001 — Fixture\n\nA record body that would retire with the flip.\n"
	if err := os.WriteFile(filepath.Join(root, "epics", "EPIC-MP-001-x", "EPIC.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	worklist := "# WORKLIST\n\n## Epic Rollup\n\n" +
		"| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| EPIC-MP-001 | [r](epics/EPIC-MP-001-x/EPIC.md) | — | — | REQ-MP-001 | T1 | — | — | **IN_PROGRESS** — building | — | — |\n"
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-21")
	// The import carries records in the byte archive; this test pins the
	// UNCARRIED path, so those ops are stripped first.
	var withoutDocs []Op
	for _, op := range ops {
		if op.Type != "upsert_process_record" {
			withoutDocs = append(withoutDocs, op)
		}
	}
	r := BuildFidelityReport(root, m, data, withoutDocs)
	l := findLoss(r.Losses, "EPIC-MP-001", "record-body")
	if l == nil || l.Category != LossLost {
		t.Fatalf("a record body with no store carrier must be a named loss: %+v", l)
	}
}

// docs/85 finding-state blocks build no op by design — but the file retires
// at the flip, so the drop must be named and the queue counted.
func TestFidelityCountsReviewQueueAndNamesFindingBlocks(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root)
	rq := "# Review queue\n\n" +
		"### RQ-900 — A live decision (decision needed, 2026-08-21)\n\nWhich way?\n\n" +
		"### RQ-901 — A finding (2026-08-21)\n\nObserved once.\n"
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "85-loop-review-queue.md"), []byte(rq), 0o644); err != nil {
		t.Fatal(err)
	}
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	if len(data.RQs) != 2 {
		t.Fatalf("fixture must parse 2 RQ blocks, got %d", len(data.RQs))
	}
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	r := BuildFidelityReport(root, m, data, ops)
	if _, err := findCount(r, "review-queue"); err != nil {
		t.Fatalf("the review queue must reconcile as a count group: %v", err)
	}
	if l := findLoss(r.Losses, "RQ-901", "finding-block"); l == nil || l.Category != LossLost {
		t.Fatalf("a finding block dropped by design must still be a named loss (its file retires): %+v", l)
	}
}

func findCount(r FidelityReport, group string) (FidelityCount, error) {
	for _, c := range r.Counts {
		if c.Group == group {
			return c, nil
		}
	}
	return FidelityCount{}, os.ErrNotExist
}

// A UR the ledgers reference but no epic record declares loses its derives
// edges silently at ingest — named per id, not just itemized in a count.
func TestFidelityNamesUnextractedURsAsLosses(t *testing.T) {
	root := t.TempDir()
	writeMappingLedger(t, root) // REQ-MP-001 names UR-MP-001; nothing defines it
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	r := BuildFidelityReport(root, m, data, ops)
	l := findLoss(r.Losses, "UR-MP-001", "ur-record")
	if l == nil || l.Category != LossLost {
		t.Fatalf("an unextracted UR must be a named loss (its derives edges drop silently): %+v", l)
	}
}
