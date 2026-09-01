package rdd

import (
	"strings"
	"testing"
)

// REQ-CROSS-223, second pass — the gap records the backlog parser never saw.
//
// Two families were invisible to the row-shaped parser: a `### GAP-…` detail
// block written inside a requirements ledger, and the narrative body a gap
// register writes UNDER its table to say what the row's one-line description
// cannot. Both are prose a human wrote deliberately, and both retire with their
// files at the flip, so neither may reach the store as a summary line alone.
//
// The negative fixtures come first. The ledger scan is a SECOND matcher over a
// file the requirement parser already reads, and its risk is not missing a gap
// — that shows up as a missing record. Its risk is stealing content from the
// requirement blocks around it, or minting a gap record out of a passing
// mention. Those are pinned before the widening that could reach them.

// ---------------------------------------------------------------- negatives

// A requirement block that DISCUSSES gaps is still a requirement block. The gap
// ids in its prose name records that live elsewhere; reading one as a heading
// would both mint a phantom backlog record and cut the requirement's own body
// short at the mention.
func TestGapIDsInsideARequirementBlockAreNotGapRecords(t *testing.T) {
	ledger := "# REQUIREMENTS — SYS\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n" +
		"| REQ-SYS-160 | one system's prose | MVP | DONE | — | — | — |\n\n" +
		"### REQ-SYS-160 — one system's prose\n" +
		"- **Statement:** enrichment writes to the system's own row.\n" +
		"- **Deferred / notes:** the underlying cause is NOT fixed — see GAP-SYS-159.\n" +
		"  GAP-SYS-154 covers the grader denominator.\n"

	if gaps := ParseLedgerGaps("tasks/SYS-REQUIREMENTS.md", ledger); len(gaps) != 0 {
		t.Fatalf("prose mentions are citations, not gap records; got %d: %v", len(gaps), gaps)
	}
	reqs := ParseLedger("tasks/SYS-REQUIREMENTS.md", ledger)
	if len(reqs) != 1 {
		t.Fatalf("the requirement row must still parse; got %d", len(reqs))
	}
	if !strings.Contains(reqs[0].Detail, "GAP-SYS-154 covers the grader denominator") {
		t.Fatalf("the mention must stay inside the requirement body, got %q", reqs[0].Detail)
	}
}

// The same trap in a table cell: the dashboard's Source and Notes columns name
// gap ids constantly. Only a level-3 HEADING declares one.
func TestAGapIDInARowCellIsNotAGapRecord(t *testing.T) {
	ledger := "# REQUIREMENTS — SYS\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n" +
		"| REQ-SYS-158 | document barrier | MVP | DONE | blocked by GAP-SYS-159 | — | — |\n"
	if gaps := ParseLedgerGaps("tasks/SYS-REQUIREMENTS.md", ledger); len(gaps) != 0 {
		t.Fatalf("a cell citation is not a declaration; got %d: %v", len(gaps), gaps)
	}
}

// A level-4 heading is a subsection of the block it sits in, not a sibling
// record. The requirement body must swallow it whole — which is also what the
// requirement scan already does, so the two matchers must agree about the
// boundary rather than each claiming the text.
func TestAnH4GapHeadingBelongsToItsRequirementBlock(t *testing.T) {
	ledger := "# REQUIREMENTS — SYS\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n" +
		"| REQ-SYS-158 | document barrier | MVP | DONE | — | — | — |\n\n" +
		"### REQ-SYS-158 — document barrier\n" +
		"- **Statement:** the barrier proceeds on partial success.\n\n" +
		"#### GAP-SYS-159 — the undetermined cause\n" +
		"Four files fail to decrypt and nobody knows why yet.\n"

	if gaps := ParseLedgerGaps("tasks/SYS-REQUIREMENTS.md", ledger); len(gaps) != 0 {
		t.Fatalf("an h4 is a subsection, not a record; got %d: %v", len(gaps), gaps)
	}
	reqs := ParseLedger("tasks/SYS-REQUIREMENTS.md", ledger)
	if len(reqs) != 1 || !strings.Contains(reqs[0].Detail, "Four files fail to decrypt") {
		t.Fatalf("the h4 body must ride the requirement detail, got %q", reqs[0].Detail)
	}
}

// ---------------------------------------------------------------- ledger gaps

// The real shape: three of these sit between requirement blocks in the SYS
// ledger. Each is a backlog record with an explicit id, a title, and a body the
// row-shaped parser has no column for.
func TestLedgerGapBlocksBecomeGapBacklogRows(t *testing.T) {
	ledger := "# REQUIREMENTS — SYS\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n" +
		"| REQ-SYS-160 | prose | MVP | DONE | — | — | — |\n\n" +
		"### GAP-SYS-161 — Material subsystems are deleted before publication\n" +
		"- **Status:** PROPOSED · **Owner:** SYS/Core\n" +
		"- **Statement:** the blueprint discovers 15 subsystems and the export publishes 10.\n" +
		"- **Deferred / notes:** the correct rule is a product decision.\n\n" +
		"### REQ-SYS-162 — react analysis\n" +
		"- **Statement:** starting a repository from React uses the same workflow.\n"

	gaps := ParseLedgerGaps("tasks/SYS-REQUIREMENTS.md", ledger)
	if len(gaps) != 1 {
		t.Fatalf("expected 1 gap record, got %d: %v", len(gaps), gaps)
	}
	g := gaps[0]
	if g.ExternalID != "GAP-SYS-161" {
		t.Fatalf("external id = %q, want the explicit ledger id", g.ExternalID)
	}
	if g.Kind != "gap" {
		t.Fatalf("kind = %q, want gap", g.Kind)
	}
	if g.Title != "GAP-SYS-161 — Material subsystems are deleted before publication" {
		t.Fatalf("title = %q", g.Title)
	}
	if g.SourcePath != "tasks/SYS-REQUIREMENTS.md" || g.Line != 7 {
		t.Fatalf("source = %s:%d, want tasks/SYS-REQUIREMENTS.md:7", g.SourcePath, g.Line)
	}
	for _, want := range []string{"**Status:** PROPOSED", "publishes 10", "product decision"} {
		if !strings.Contains(g.NotesMD, want) {
			t.Fatalf("the body must ride notes_md verbatim, %q missing from %q", want, g.NotesMD)
		}
	}
	if strings.Contains(g.NotesMD, "react analysis") {
		t.Fatalf("the block ends at the next heading, got %q", g.NotesMD)
	}
	// The requirement blocks on either side are untouched: the gap scan is a
	// parallel reader, not a change to how requirements parse.
	reqs := ParseLedger("tasks/SYS-REQUIREMENTS.md", ledger)
	if len(reqs) != 1 || reqs[0].ID != "REQ-SYS-160" {
		t.Fatalf("requirement parsing changed: %v", reqs)
	}
}

// A gap block that opens a ledger with no requirement rows still parses: the
// two scans share the file, not each other's state.
func TestALedgerGapBlockNeedsNoRequirementRowBeforeIt(t *testing.T) {
	ledger := "# REQUIREMENTS — SYS\n\n" +
		"## Gaps\n\n" +
		"### GAP-SYS-154 — the grader's denominator is wrong\n" +
		"Vendored JavaScript is counted as source.\n"
	gaps := ParseLedgerGaps("tasks/SYS-REQUIREMENTS.md", ledger)
	if len(gaps) != 1 || gaps[0].ExternalID != "GAP-SYS-154" {
		t.Fatalf("got %v", gaps)
	}
	if strings.TrimSpace(gaps[0].NotesMD) != "Vendored JavaScript is counted as source." {
		t.Fatalf("notes = %q", gaps[0].NotesMD)
	}
}

// ---------------------------------------------------------------- register narratives

// The gap register writes a one-line description in its table and the real
// account underneath it, as a `### GAP-…` block. The store holds one record per
// id, so the block has to reach that record's notes — otherwise the row syncs
// as a sentence and the paragraphs retire with the file.
func TestRegisterNarrativeBodiesRideTheirTableRow(t *testing.T) {
	register := "# Gap Register\n\n" +
		"## Gaps\n\n" +
		"| ID | Description | Owning Context | Deferred Until | Related Reqs |\n" +
		"|----|-------------|----------------|----------------|--------------|\n" +
		"| GAP-002 | The snapshot revision is unreachable | CROSS | Blocked upstream | REQ-CROSS-029 |\n" +
		"| GAP-003 | Document-ingestion failure management | KNW | After EPIC-KNW-003 | REQ-KNW-105 |\n\n" +
		"### GAP-002 — the recorded snapshot revision is unreachable\n\n" +
		"**Gap:** the recorded revision exists upstream only as the head of a PR.\n\n" +
		"**Why it is a gap and not a bug:** nothing in this workspace can fix it.\n\n" +
		"### GAP-003 — document-ingestion failure management\n\n" +
		"**Gap:** neither surface shows the durable failed phase.\n"

	rows := ParseBacklogRows("process/gap-register.md", register, "gap")
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	byID := map[string]BacklogRow{}
	for _, r := range rows {
		byID[r.ExternalID] = r
	}
	g2 := byID["GAP-002"]
	if !strings.Contains(g2.NotesMD, "The snapshot revision is unreachable") {
		t.Fatalf("the table cells must survive: %q", g2.NotesMD)
	}
	if !strings.Contains(g2.NotesMD, "only as the head of a PR") ||
		!strings.Contains(g2.NotesMD, "nothing in this workspace can fix it") {
		t.Fatalf("the whole narrative must ride the notes: %q", g2.NotesMD)
	}
	if !strings.Contains(g2.NotesMD, backlogNarrativeSeparator) {
		t.Fatalf("the seam between cells and narrative must be visible: %q", g2.NotesMD)
	}
	if strings.Contains(g2.NotesMD, "durable failed phase") {
		t.Fatalf("GAP-003's narrative leaked into GAP-002: %q", g2.NotesMD)
	}
	if !strings.Contains(byID["GAP-003"].NotesMD, "durable failed phase") {
		t.Fatalf("GAP-003 lost its narrative: %q", byID["GAP-003"].NotesMD)
	}
}

// A narrative block whose id has no table row enriches nothing. The register's
// own closed-gaps template is exactly that, and minting a record from it would
// import a placeholder as a gap.
func TestANarrativeWithNoTableRowMintsNoRecord(t *testing.T) {
	register := "# Gap Register\n\n" +
		"| ID | Description |\n|----|-------------|\n" +
		"| GAP-002 | real |\n\n" +
		"## Closed Gaps\n\n" +
		"### GAP-NNN — «Description» (CLOSED «YYYY-MM-DD»)\n\n" +
		"**Gap:** «What was missing»\n"
	rows := ParseBacklogRows("process/gap-register.md", register, "gap")
	if len(rows) != 1 || rows[0].ExternalID != "GAP-002" {
		t.Fatalf("only the table row is a record, got %v", rows)
	}
	if strings.Contains(rows[0].NotesMD, "What was missing") {
		t.Fatalf("an unmatched narrative attached itself to another row: %q", rows[0].NotesMD)
	}
}

// BACKLOG.md's level-3 headings are routing-log dates, not record narratives.
// A scan keyed on "any ### heading" would fold a triage pass into the first
// discovery row it could find.
func TestRoutingLogHeadingsAreNotRecordNarratives(t *testing.T) {
	backlog := "# Backlog\n\n" +
		"## Inbox\n\n" +
		"| Item | Notes | Tracked as |\n|---|---|---|\n" +
		"| A live discovery | needs routing | NEW |\n\n" +
		"## Routing Log\n\n" +
		"### 2026-08-07 — Scope face seeded from the grooming artifact\n\n" +
		"Everything below is a log entry, not a record body.\n"
	rows := ParseBacklogRows("BACKLOG.md", backlog, "backlog")
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %v", len(rows), rows)
	}
	if strings.Contains(rows[0].NotesMD, "log entry") {
		t.Fatalf("a routing-log heading became a narrative: %q", rows[0].NotesMD)
	}
}
