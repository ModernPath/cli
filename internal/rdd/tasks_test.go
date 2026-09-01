package rdd

// REQ-CROSS-264 (EPIC-CLI-003 tranche 4): every task the corpus declares
// imports as a `tasks` row through a new `upsert_task` batch op.
//
// Recognition is declaration-SHAPE-led, not heading-led: only 275 of the 375
// declaring table rows sit under a `## Task…` heading, 79 declarations are
// bullets, and 30 are epic-local `T<n>` ordinals. The one negative family is
// the evidence/coverage/traceability heading family, whose task-led rows are
// evidence ABOUT tasks rather than declarations.
//
// Identity is the epic-scoped composite `<epic-code>#<local-id>` — the
// scenario precedent — because 14 cross-epic `TASK-*` reuses and 12 ordinal
// keys are 26 duplicate `(system, external_id)` keys that would otherwise make
// the run refuse whole.

import (
	"strings"
	"testing"
)

func taskOpsOf(t *testing.T, epicID, record string) []Op {
	t.Helper()
	e := Epic{ID: epicID, Record: "epics/" + epicID + ".md"}
	return BuildTaskOps(Data{Epics: []Epic{e}}, map[string]string{epicID: record})
}

func taskPayload(t *testing.T, ops []Op, externalID string) map[string]any {
	t.Helper()
	for _, op := range ops {
		if op.Type == "upsert_task" && op.Payload["external_id"] == externalID {
			return op.Payload
		}
	}
	t.Fatalf("no upsert_task op with external_id %q in %v", externalID, opIDs(ops))
	return nil
}

// duplicateOpIDsOf mirrors the import's own refusal predicate: a duplicate
// (type, external_id) in one batch makes idempotence unprovable, so the run
// refuses whole. The composite exists to make that impossible for tasks.
func duplicateOpIDsOf(ops []Op) []string {
	seen := map[string]bool{}
	var dups []string
	for _, op := range ops {
		id, _ := op.Payload["external_id"].(string)
		key := op.Type + " " + id
		if seen[key] {
			dups = append(dups, key)
		}
		seen[key] = true
	}
	return dups
}

// ---------------------------------------------------------------- shape rules

func TestTaskTableRowAnywhereDeclaresATask(t *testing.T) {
	// The heading is NOT `## Tasks` — 37% of the corpus's declaring rows live
	// under other heading forms, so a heading rule closes nothing.
	record := `# EPIC-CV-001 — Only

## 4. Delivery slices

| Task | Scope | Status | Owner |
|---|---|---|---|
| TASK-CV-001 | Carry the parser | DONE | ada |
`
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1 (%v)", len(ops), opIDs(ops))
	}
	p := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-001")
	if p["code"] != "TASK-CV-001" {
		t.Fatalf("code = %v, want the LOCAL id", p["code"])
	}
	if p["title"] != "Carry the parser" {
		t.Fatalf("title = %v, want the row's title/scope cell", p["title"])
	}
	if p["process_status"] != "DONE" {
		t.Fatalf("process_status = %v, want the declared token", p["process_status"])
	}
	if p["declared_owner_external_id"] != "EPIC-CV-001" || p["declared_owner_type"] != "epic" {
		t.Fatalf("owner = %v/%v, want the declaring epic", p["declared_owner_type"], p["declared_owner_external_id"])
	}
	if p["source_path"] != "epics/EPIC-CV-001.md" {
		t.Fatalf("source_path = %v", p["source_path"])
	}
	if raw, _ := p["source_raw"].(string); !strings.Contains(raw, "TASK-CV-001") {
		t.Fatalf("source_raw must carry the declaration verbatim, got %q", raw)
	}
	if p["source_line"] != 7 {
		t.Fatalf("source_line = %v, want 7", p["source_line"])
	}
	// The product Task table is shared with Board/MCP, so every imported row is
	// stamped sync-born; the read surface and the readback filter on the stamp.
	if p["sync_born"] != true {
		t.Fatalf("sync_born = %v, want true", p["sync_born"])
	}
	if _, ok := p["content_hash"].(string); !ok {
		t.Fatal("content_hash missing")
	}
}

func TestTaskBulletDeclaresATaskWithEitherLead(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Plan

- **TASK-CV-010** — bold lead, the dominant bullet form (52 measured)
- TASK-CV-011 — plain lead, the other 27
`
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 (%v)", len(ops), opIDs(ops))
	}
	bold := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-010")
	plain := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-011")
	if !strings.Contains(bold["title"].(string), "bold lead") {
		t.Fatalf("bold-lead title = %v", bold["title"])
	}
	if !strings.Contains(plain["title"].(string), "plain lead") {
		t.Fatalf("plain-lead title = %v — a plain lead must land exactly as a bold one does", plain["title"])
	}
}

// D-T4-2: bulletTaskTitle trimmed the bullet's markdown/separator debris off
// its edges in a SINGLE pass. The corpus's dominant bullet shape is
// "- **TASK-ID:** text" — removing the id leaves "**:** text" — and a
// one-pass trim strips the leading "**" but not the "**" the following
// colon-strip then exposes, leaking a bare "**" into 17 titles (e.g.
// TASK-AU2-004, `epics/EPIC-AUTH-002-session-and-workspace-switching/EPIC.md:71`).
func TestBulletBoldMarkerFullyStripped(t *testing.T) {
	record := "# EPIC-CV-001 — Only\n\n## Plan\n\n" +
		"- **TASK-CV-510:** move the existing list/switch state and actions\n"
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	title := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-510")["title"].(string)
	if strings.Contains(title, "*") {
		t.Fatalf("title = %q, a stray bold marker survived the strip", title)
	}
	if !strings.HasPrefix(title, "move the existing") {
		t.Fatalf("title = %q, want the bullet's own words with no markdown residue", title)
	}
}

// D-T4-4: parseRecordTasks read one physical line per bullet, so a bullet
// that WRAPS — the corpus's own convention for a long task description —
// lost every continuation line. A table cell keeps the full text in
// source_raw regardless of what the title rule does with it; a wrapped
// bullet has no such second home, so a dropped continuation is gone from the
// corpus entirely (e.g. TASK-NS-002, `epics/EPIC-FE-042-new-system-entry.md:49`).
func TestWrappedBulletKeepsItsContinuation(t *testing.T) {
	record := "# EPIC-CV-001 — Only\n\n## Tasks\n\n" +
		"- **TASK-CV-511** — `src/views/Widget.tsx` (button + modal) + wire into\n" +
		"  `WidgetOverview` gallery toolbar.\n" +
		"- **TASK-CV-512** — a normal, unwrapped bullet.\n"
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 (%v)", len(ops), opIDs(ops))
	}
	p := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-511")
	title := p["title"].(string)
	if !strings.Contains(title, "WidgetOverview") {
		t.Fatalf("title = %q, want the wrapped continuation line folded in", title)
	}
	raw, _ := p["source_raw"].(string)
	if !strings.Contains(raw, "WidgetOverview") {
		t.Fatalf("source_raw = %q, want the continuation preserved verbatim too", raw)
	}
	other := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-512")["title"].(string)
	if !strings.Contains(other, "a normal, unwrapped bullet") {
		t.Fatalf("unrelated next bullet title = %q, must not be swallowed by the wrap above it", other)
	}
}

// D-T4-2 regression: fixing the stray "**" must not start eating a
// DIFFERENT edge marker. "- **TASK-SH-002** — `App.shell.test.tsx` (…)"
// exposes a leading backtick the same way TASK-AU2-004 exposes a leading
// "**" once the em-dash is stripped — but that backtick OPENS an inline
// code span whose closing partner sits further into the text, not an
// orphaned duplicate of a marker already consumed with the id. An
// asterisk-only-after-the-first-pass fix must leave the pair intact; an
// iterate-everything fix (an earlier version of this same change) stranded
// the closing backtick on 10 corpus rows, caught only by the before/after
// corpus measurement, not by a narrower unit test — this test is that gap
// closed.
func TestBulletCodeSpanAfterTheIdSurvivesTheMarkdownStrip(t *testing.T) {
	record := "# EPIC-CV-001 — Only\n\n## Plan\n\n" +
		"- **TASK-CV-513** — `App.shell.test.tsx` (SCN-SH-001/002).\n"
	title := taskPayload(t, taskOpsOf(t, "EPIC-CV-001", record), "EPIC-CV-001#TASK-CV-513")["title"].(string)
	if title != "`App.shell.test.tsx` (SCN-SH-001/002)." {
		t.Fatalf("title = %q, want the code span's backtick pair intact, not one stranded", title)
	}
}

func TestTaskLedRowInAnEvidenceSectionDeclaresNothing(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Tasks

| Task | Scope |
|---|---|
| TASK-CV-020 | the real declaration |

## Evidence map

| Task | Test | Result |
|---|---|---|
| TASK-CV-021 | ` + "`suite_test.exs`" + ` | PASS |

## Coverage

- **TASK-CV-022** — evidence about a task, not a declaration

## Traceability matrix

| Task | Requirement |
|---|---|
| TASK-CV-023 | REQ-CV-001 |
`
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1 — evidence/coverage/traceability sections declare nothing (%v)", len(ops), opIDs(ops))
	}
	taskPayload(t, ops, "EPIC-CV-001#TASK-CV-020")

	census := TaskDeclarations(Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}}},
		map[string]string{"EPIC-CV-001": record})
	if census.ExcludedRows != 3 {
		t.Fatalf("excluded rows = %d, want 3 — the class is counted, never silent", census.ExcludedRows)
	}
	if len(census.ExcludedOnlyIDs) != 3 {
		t.Fatalf("evidence-only ids = %v, want the three that appear only there", census.ExcludedOnlyIDs)
	}
}

func TestFencedSampleDeclaresNoTask(t *testing.T) {
	record := "# EPIC-CV-001 — Only\n\n## Tasks\n\n```\n| TASK-CV-030 | a template excerpt |\n- **TASK-CV-031** — a quoted sample\n```\n\n| Task | Scope |\n|---|---|\n| TASK-CV-032 | the real one |\n"
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1 — fenced text is quoted typography (%v)", len(ops), opIDs(ops))
	}
	taskPayload(t, ops, "EPIC-CV-001#TASK-CV-032")
}

func TestEpicLocalOrdinalIsATask(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Slices

| # | Slice | Gate |
|---|---|---|
| T1 | first slice | CLI |
| T13 | thirteenth slice | CLI |
`
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 (%v)", len(ops), opIDs(ops))
	}
	p := taskPayload(t, ops, "EPIC-CV-001#T1")
	if p["code"] != "T1" {
		t.Fatalf("ordinal code = %v, want T1 — identity is already epic-scoped, nothing is minted", p["code"])
	}
	if p["title"] != "first slice" {
		t.Fatalf("ordinal title = %v", p["title"])
	}
	taskPayload(t, ops, "EPIC-CV-001#T13")
}

func TestOrdinalShapeIsExact(t *testing.T) {
	for _, tok := range []string{"T1", "T13", "T007"} {
		if !taskOrdinalRe.MatchString(tok) {
			t.Fatalf("%q must be an ordinal", tok)
		}
	}
	for _, tok := range []string{"T", "T1a", "TASK-1", "TX1", "T1..T3"} {
		if taskOrdinalRe.MatchString(tok) {
			t.Fatalf("%q must NOT be an ordinal", tok)
		}
	}
}

// ---------------------------------------------------------------- identity

func TestOneLocalIDInTwoEpicsIsTwoRows(t *testing.T) {
	// 14 cross-epic `TASK-*` reuses were measured. Bare-code identity would
	// hard-refuse the run on `duplicateOpIDs`; the composite makes them two
	// legitimate rows.
	a := "# A\n\n## Tasks\n\n| Task | Scope |\n|---|---|\n| TASK-SH-001 | in A |\n"
	b := "# B\n\n## Tasks\n\n| Task | Scope |\n|---|---|\n| TASK-SH-001 | in B |\n"
	data := Data{Epics: []Epic{
		{ID: "EPIC-AA-001", Record: "epics/a.md"},
		{ID: "EPIC-BB-001", Record: "epics/b.md"},
	}}
	ops := BuildTaskOps(data, map[string]string{"EPIC-AA-001": a, "EPIC-BB-001": b})
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 (%v)", len(ops), opIDs(ops))
	}
	if dups := duplicateOpIDsOf(ops); len(dups) > 0 {
		t.Fatalf("the composite must be collision-free, got duplicates %v", dups)
	}
	if taskPayload(t, ops, "EPIC-AA-001#TASK-SH-001")["title"] != "in A" {
		t.Fatal("epic A's row lost its own title")
	}
	if taskPayload(t, ops, "EPIC-BB-001#TASK-SH-001")["title"] != "in B" {
		t.Fatal("epic B's row lost its own title")
	}
}

func TestDuplicateWithinOneEpicIsFirstWinsAndFlagged(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Tasks

| Task | Scope |
|---|---|
| TASK-CV-040 | the first declaration |
| TASK-CV-040 | the shadowed one |
`
	ops := taskOpsOf(t, "EPIC-CV-001", record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1 — first wins within one epic (%v)", len(ops), opIDs(ops))
	}
	if taskPayload(t, ops, "EPIC-CV-001#TASK-CV-040")["title"] != "the first declaration" {
		t.Fatal("first-wins lost")
	}
	census := TaskDeclarations(Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}}},
		map[string]string{"EPIC-CV-001": record})
	if len(census.Shadowed) != 1 {
		t.Fatalf("shadowed = %v, want the second line flagged — a guard flags, it does not delete", census.Shadowed)
	}
	if census.Shadowed[0].Line != 8 {
		t.Fatalf("shadowed line = %d, want 8", census.Shadowed[0].Line)
	}
}

func TestUnresolvableEpicPrefixIsANamedLossNeverARow(t *testing.T) {
	// Zero measured today — a guard, not a population.
	data := Data{
		Epics: []Epic{{ID: "EPIC-AA-001", Record: "epics/a.md"}},
		WorklistTaskRows: []WorklistTaskRow{
			{EpicID: "EPIC-GONE-999", Cell: "TASK-GG-001", SourcePath: "WORKLIST.md", Line: 12},
		},
	}
	ops := BuildTaskOps(data, map[string]string{"EPIC-AA-001": "# A\n"})
	for _, op := range ops {
		if id, _ := op.Payload["external_id"].(string); strings.HasPrefix(id, "EPIC-GONE-999") {
			t.Fatalf("a composite whose epic prefix resolves to no emitted epic must never land: %s", id)
		}
	}
	census := TaskDeclarations(data, map[string]string{"EPIC-AA-001": "# A\n"})
	if len(census.UnresolvedEpic) != 1 {
		t.Fatalf("unresolved-epic losses = %v, want one named loss", census.UnresolvedEpic)
	}
}

// ---------------------------------------------------------------- WORKLIST

func TestRollupTasksCellExpandsAndLandsEpicParented(t *testing.T) {
	data := Data{Epics: []Epic{{
		ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md",
		TasksCell: "TASK-CV-100..102 DONE; T1..T2 (EPIC.md)",
	}}}
	ops := BuildTaskOps(data, map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"})
	want := []string{
		"EPIC-CV-001#TASK-CV-100", "EPIC-CV-001#TASK-CV-101", "EPIC-CV-001#TASK-CV-102",
		"EPIC-CV-001#T1", "EPIC-CV-001#T2",
	}
	if len(ops) != len(want) {
		t.Fatalf("ops = %d, want %d (%v)", len(ops), len(want), opIDs(ops))
	}
	for _, id := range want {
		p := taskPayload(t, ops, id)
		if p["source_path"] != "WORKLIST.md" {
			t.Fatalf("%s source_path = %v, want WORKLIST.md", id, p["source_path"])
		}
		if p["declared_owner_external_id"] != "EPIC-CV-001" {
			t.Fatalf("%s is not epic-parented", id)
		}
	}
}

func TestRollupCellWithProseAndNoIDDeclaresNothing(t *testing.T) {
	data := Data{Epics: []Epic{{
		ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md",
		TasksCell: "all slices delivered; see the record",
	}}}
	records := map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"}
	if ops := BuildTaskOps(data, records); len(ops) != 0 {
		t.Fatalf("ops = %v, want none — prose declares nothing", opIDs(ops))
	}
	if got := TaskDeclarations(data, records).ProseCells; got != 1 {
		t.Fatalf("prose cells counted = %d, want 1 — counted, never silent", got)
	}
}

func TestWorkRowsTaskLedRowLandsUnderItsEpic(t *testing.T) {
	data := Data{
		Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}},
		WorklistTaskRows: []WorklistTaskRow{{
			EpicID: "EPIC-CV-001", Cell: "TASK-CV-200..201",
			Title: "the work row's scope", SourcePath: "WORKLIST.md", Line: 260,
			Raw: "| TASK-CV-200..201 | EPIC-CV-001 | … |",
		}},
	}
	ops := BuildTaskOps(data, map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"})
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 (%v)", len(ops), opIDs(ops))
	}
	p := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-200")
	if p["source_line"] != 260 {
		t.Fatalf("source_line = %v, want the work row's line", p["source_line"])
	}
	if raw, _ := p["source_raw"].(string); !strings.Contains(raw, "TASK-CV-200..201") {
		t.Fatalf("source_raw = %q, want the row verbatim", raw)
	}
}

// ---------------------------------------------------------------- title rule

func TestIDOnlyTaskTakesTheVerbatimSourceCellCappedElseTheCode(t *testing.T) {
	long := strings.Repeat("x", 400)
	data := Data{Epics: []Epic{{
		ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md",
		TasksCell: "TASK-CV-300 " + long,
	}}}
	ops := BuildTaskOps(data, map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"})
	p := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-300")
	title, _ := p["title"].(string)
	if title == "" {
		t.Fatal("title is NOT NULL in the landing table — it can never be empty")
	}
	if utf16Len(title) > taskTitleCap {
		t.Fatalf("title is %d units, cap is %d", utf16Len(title), taskTitleCap)
	}
	if !strings.HasPrefix(title, "TASK-CV-300 xxx") {
		t.Fatalf("title = %q, want the verbatim source cell text — prose is never invented", title[:40])
	}

	// Nothing but the id: the code itself, never invented prose.
	bare := Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md", TasksCell: "TASK-CV-301"}}}
	ops = BuildTaskOps(bare, map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"})
	if got := taskPayload(t, ops, "EPIC-CV-001#TASK-CV-301")["title"]; got != "TASK-CV-301" {
		t.Fatalf("title = %v, want the code", got)
	}
}

// ---------------------------------------------------------------- tokens

func TestExpandTaskRefsReadsRangesListsAndOrdinals(t *testing.T) {
	// Order is by token CLASS — ranges, lists, bare ids, then the ordinal
	// forms — and is fixed, because it decides the payload's field order and
	// therefore its content hash. Both builders expand in this order.
	got := expandTaskRefs("TASK-CV-100..102, TASK-CV-200/201, T1..T3, TASK-CV-300")
	want := []string{
		"TASK-CV-100", "TASK-CV-101", "TASK-CV-102",
		"TASK-CV-200", "TASK-CV-201",
		"TASK-CV-300",
		"T1", "T2", "T3",
	}
	if len(got) != len(want) {
		t.Fatalf("expanded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expanded %v, want %v", got, want)
		}
	}
}

// ---------------------------------------------------------------- BuildOps

func TestBuildOpsEmitsTasksAfterTheirEpic(t *testing.T) {
	record := "# EPIC-CV-001 — Only\n\n## Tasks\n\n| Task | Scope |\n|---|---|\n| TASK-CV-400 | carried |\n"
	data := Data{Epics: []Epic{{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md"}}}
	ops := BuildOps(data, func(string) string { return record }, "2026-08-24")
	epicAt, taskAt := -1, -1
	for i, op := range ops {
		switch op.Type {
		case "upsert_epic":
			epicAt = i
		case "upsert_task":
			if id, _ := op.Payload["external_id"].(string); id == "EPIC-CV-001#TASK-CV-400" {
				taskAt = i
			}
		}
	}
	if taskAt < 0 {
		t.Fatalf("BuildOps emitted no upsert_task op")
	}
	if epicAt < 0 || taskAt < epicAt {
		t.Fatalf("task op at %d must follow its epic op at %d — the applier resolves the epic by code", taskAt, epicAt)
	}
}

// REQ-CROSS-264: a rollup cell naming `TASK-X-001..004` declares four tasks.
// The ids expanded correctly, but every expanded row took the WHOLE cell as
// its title, so four rows landed titled "TASK-X-001..004" — the range string
// itself, which is not a title at all and leaves the rows indistinguishable.
//
// Measured on the corpus before this test: 148 of 631 task rows carried a
// literal `..` range as their title, 144 of them the 154 WORKLIST-sourced
// rows, producing 37 duplicate (epic, title) pairs.
//
// capTaskTitle already states the rule — "the code itself when the corpus says
// nothing else. Prose is never invented." The defect was feeding it the id
// references as though they were prose. Strip the references first: what is
// left is what the cell actually says about the work, and when it says nothing
// the code stands.
func TestIdRangeIsNotATaskTitle(t *testing.T) {
	// A cell that is only a range says nothing about any individual task.
	ids, prose := taskRefsAndProse("TASK-AN-501..504")
	if len(ids) != 4 {
		t.Fatalf("ids = %v, want the four the range names", ids)
	}
	if prose != "" {
		t.Errorf("prose = %q, want empty — a range is not a description", prose)
	}
	if got := capTaskTitle(prose, ids[0]); got != "TASK-AN-501" {
		t.Errorf("title = %q, want the task's own code", got)
	}

	// A cell that adds words keeps them; the range reference does not survive.
	ids, prose = taskRefsAndProse("TASK-AR-001..003 (guard + strand-A slices)")
	if len(ids) != 3 {
		t.Fatalf("ids = %v, want three", ids)
	}
	if strings.Contains(prose, "..") || strings.Contains(prose, "TASK-AR-001") {
		t.Errorf("prose = %q, must not carry the id references", prose)
	}
	if !strings.Contains(prose, "guard") {
		t.Errorf("prose = %q, want the cell's own words about the work", prose)
	}

	// A slash list is the same shape.
	ids, prose = taskRefsAndProse("TASK-CR-001/002/003")
	if len(ids) != 3 || prose != "" {
		t.Errorf("ids=%v prose=%q, want three ids and no prose", ids, prose)
	}

	// A cell that is only prose keeps it and names no task.
	ids, prose = taskRefsAndProse("follow-up sweep, not yet sliced")
	if len(ids) != 0 {
		t.Errorf("ids = %v, want none", ids)
	}
	if !strings.Contains(prose, "follow-up sweep") {
		t.Errorf("prose = %q, want the cell verbatim", prose)
	}
}

// D-T4-5 (WORKLIST.md:55, the corpus's only instance, RUN:2026-08-25): a
// range whose two ends carry different digit widths has no defined numeric
// span — "TASK-CMP-9031..90310" is lo=9031/hi=90310, and hi-lo is over
// 500 by construction no matter what the corpus meant. The prior behavior
// fell through to the bare leading token and minted ONE row titled with the
// raw, un-parseable range text, while the other nine named ids never landed
// at all. Flagged instead of guessed: nothing is expanded, and the token is
// named in the census so a human can rewrite it as explicit ids.
func TestMismatchedWidthRangeIsFlaggedNotGuessed(t *testing.T) {
	ids, prose := taskRefsAndProse("TASK-CMP-9031..90310")
	if len(ids) != 0 {
		t.Fatalf("ids = %v, want none — hi-lo has no defined value across mismatched widths", ids)
	}
	if prose != "" {
		t.Fatalf("prose = %q, want empty — the token is consumed, not treated as descriptive text", prose)
	}
	if got := mismatchedTaskRanges("TASK-CMP-9031..90310"); len(got) != 1 || got[0] != "TASK-CMP-9031..90310" {
		t.Fatalf("mismatchedTaskRanges = %v, want the one raw token", got)
	}
	if got := mismatchedTaskRanges("TASK-CMP-9031..9039"); len(got) != 0 {
		t.Fatalf("mismatchedTaskRanges = %v, want none — equal widths are an ordinary range", got)
	}

	data := Data{Epics: []Epic{{
		ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001.md",
		TasksCell: "TASK-CV-9031..90310",
	}}}
	records := map[string]string{"EPIC-CV-001": "# EPIC-CV-001 — Only\n"}
	if ops := BuildTaskOps(data, records); len(ops) != 0 {
		t.Fatalf("ops = %v, want none — must not mint a row off the unparsed leading id", opIDs(ops))
	}
	census := TaskDeclarations(data, records)
	if len(census.MismatchedRanges) != 1 {
		t.Fatalf("mismatched ranges = %v, want one named loss", census.MismatchedRanges)
	}
	if census.ProseCells != 0 {
		t.Fatalf("prose cells = %d, want 0 — a mismatched range is a distinct, named class from prose", census.ProseCells)
	}
}
