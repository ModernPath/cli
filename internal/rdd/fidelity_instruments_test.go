package rdd

// REQ-CROSS-221, third pass — four things the report could not see.
//
// Each arm here reports a population the existing instruments walk straight
// past: a scenario definition quoted inside a code fence, a table row that is
// scenario-SHAPED but sits under a section nobody recognizes, a loop-status
// cell the extractor cuts at its cap, and a narrative block the register writes
// under its own table. Every one of them reads as clean today, which is the
// property that makes them worth measuring.
//
// Paired fixtures again: each check has a firing case and a case that only
// looks like one. The fence arm in particular is the dangerous one — a record
// quotes SCN ids inside `text` trace diagrams and template excerpts constantly,
// and a matcher that counts "an SCN id inside a fence" reports those as lost
// acceptance content.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// ---------------------------------------------------------------- fence-masked definitions

// The firing case: a record declares Gherkin inside a fence that sits under no
// scenario section — a sample quoted beside a design note, a record whose
// scenario heading the vocabulary does not open. The parser leaves those quoted
// on purpose, and every citation counter skips them too, so the record reads as
// defining nothing while the declarations sit in plain sight.
func TestFencedGherkinScenariosOutsideASectionAreReportedAsMasked(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want the masked definitions counted.\n\n"+
		"## Design notes\n\n"+
		"```gherkin\n"+
		"Scenario: SCN-CV-030 — a sync attributes itself to a build\n"+
		"  Given a workspace syncing with a CLI that stamps its build\n"+
		"  Then the batch carries that client identity\n\n"+
		"Scenario: SCN-CV-031 — a client that sends no identity is still served\n"+
		"  Given an older CLI\n"+
		"  Then the batch is accepted exactly as today\n"+
		"```\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "scenario-shape:fence-masked")
	if l == nil || l.Category != LossLost {
		t.Fatalf("fenced Gherkin definitions must be reported as lost, got %+v (losses %+v)", l, r.Losses)
	}
	for _, want := range []string{"2 scenario definition(s)", "1 record(s)"} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("the loss must state its population, %q missing from %q", want, l.Detail)
		}
	}
	c := countGroup(t, r, "  scenario-shape: "+fenceMaskedShape+", in 1 file(s)")
	if c.Rows != 2 || c.Ops != 0 {
		t.Fatalf("fence-masked count = %d/%d, want 2/0", c.Rows, c.Ops)
	}
}

// The same declarations moved INSIDE the scenario section, which is where the
// corpus's own record writes them. They are this epic's acceptance content, so
// both sides of the coverage diff must see them: a shape row that counts them,
// and no masked loss claiming they reached nothing.
func TestFencedGherkinInTheScenarioSectionIsCountedAsCaptured(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want the declarations captured.\n\n"+
		"## Acceptance scenarios\n\n"+
		"### Slice 1 — the batch says what produced it\n\n"+
		"```gherkin\n"+
		"Scenario: SCN-CV-030 — a sync attributes itself to a build\n"+
		"  Given a workspace syncing with a CLI that stamps its build\n"+
		"  Then the batch carries that client identity\n\n"+
		"Scenario: SCN-CV-031 — a client that sends no identity is still served\n"+
		"  Given an older CLI\n"+
		"  Then the batch is accepted exactly as today\n"+
		"```\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "scenario-shape:fence-masked"); l != nil {
		t.Fatalf("a captured declaration is not masked: %+v", l)
	}
	if l := lossWithField(r.Losses, "scenario-shape:fenced-gherkin"); l != nil {
		t.Fatalf("the parser carries these, so the shape must report no loss: %+v", l)
	}
	c := countGroup(t, r, "  scenario-shape: fenced-gherkin, in 1 file(s)")
	if c.Rows != 2 || c.Ops != 2 {
		t.Fatalf("fenced-gherkin count = %d/%d, want 2/2", c.Rows, c.Ops)
	}
}

// The negative half, first shape: a trace diagram inside a ```text fence names
// a RANGE of scenario ids. Those are references to definitions that live in the
// epic's spec files, and reporting them as masked acceptance content would
// invent a loss on a record that has none.
func TestFencedTraceDiagramIDsAreNotMaskedDefinitions(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want no phantom losses.\n\n"+
		"## Trace\n\n"+
		"```text\n"+
		"UR-CV-001\n"+
		"  -> EPIC-CV-001\n"+
		"  -> SCN-NAM-201..205\n"+
		"  -> SR-NAM-201..205\n"+
		"```\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "scenario-shape:fence-masked"); l != nil {
		t.Fatalf("a quoted trace diagram defines nothing: %+v", l)
	}
}

// Second shape: a line that begins with the word Scenario but is not a Gherkin
// declaration — the corpus writes `Scenario evidence: SCN-…` inside fenced
// blocks. Only `Scenario:` and `Scenario Outline:` declare.
func TestAFencedScenarioEvidenceLineIsNotADefinition(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want only declarations counted.\n\n"+
		"## Acceptance scenarios\n\n"+
		"```text\n"+
		"Scenario evidence: SCN-CV-030 (found unaided) · SCN-CV-031 (nothing lost)\n"+
		"```\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "scenario-shape:fence-masked"); l != nil {
		t.Fatalf("an evidence line cites, it does not declare: %+v", l)
	}
}

// Third shape, and the one that decides whether the number is worth reading: a
// fenced Gherkin block that RESTATES a scenario the record already defines in a
// shape the parser reads. The id reaches a scenario record, so nothing is
// masked — counting it would inflate the gap with content that is already in
// the store.
func TestAFencedRestatementOfACapturedScenarioIsNotMasked(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want no double counting.\n\n"+
		"## Acceptance scenarios\n\n"+
		"| id | Scenario |\n|---|---|\n"+
		"| SCN-CV-001 | GIVEN a read section THEN the row is captured |\n\n"+
		"### Reference form\n\n"+
		"```gherkin\n"+
		"Scenario: SCN-CV-001 — the same scenario, quoted for the implementer\n"+
		"  Given a read section\n"+
		"  Then the row is captured\n"+
		"```\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "scenario-shape:fence-masked"); l != nil {
		t.Fatalf("a restatement of a captured scenario is not a masked definition: %+v", l)
	}
}

// Fourth shape: a fenced declaration under an out-of-scope section is out of
// this epic's acceptance whatever shape it is written in — the same rule the
// unfenced shapes already follow.
func TestFencedDefinitionsInAnOutOfScopeSectionStayOut(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n"+
		"## User outcome (UR-CV-001)\n\nAs a reader I want scope respected.\n\n"+
		"## Out-of-scope scenarios\n\n"+
		"```gherkin\n"+
		"Scenario: SCN-CV-090 — deliberately not built here\n"+
		"  Given a deferred behaviour\n"+
		"  Then it belongs to another epic\n"+
		"```\n")

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "scenario-shape:fence-masked"); l != nil {
		t.Fatalf("an out-of-scope section defines no acceptance content: %+v", l)
	}
}

// ---------------------------------------------------------------- addendum rows

// A scenario-shaped table row under a section nobody recognizes is neither a
// definition the parser reads nor a citation from a named reference table. It
// got counted as the latter, which asserts the opposite of what it is. It now
// gets a row of its own that says what it actually is.
func TestScenarioRowsInUnrecognizedSectionsGetTheirOwnCount(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want each population named.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a read section THEN the row is captured |

## Evidence map

| Item | Test |
|---|---|
| SCN-CV-001 | a test that evidences it |

## 9. Addendum slice — a later review round

| id | Scenario |
|---|---|
| SCN-CV-040 | GIVEN an addendum THEN nothing reads it |
| SCN-CV-041 | GIVEN a second addendum row THEN nothing reads that either |
`)
	r := reportOf(t, root)

	cited := countGroup(t, r, scenarioCitationGroup(1))
	if cited.Rows != 1 {
		t.Fatalf("named reference tables cite 1 row here, got %d", cited.Rows)
	}
	addendum := countGroup(t, r, scenarioAddendumGroup(1))
	if addendum.Rows != 2 || addendum.Ops != 0 {
		t.Fatalf("unrecognized-section rows = %d/%d, want 2/0", addendum.Rows, addendum.Ops)
	}
	// Counting, not capture: this arm claims no loss of its own.
	if l := lossWithField(r.Losses, "scenario-shape:table-outside-a-section"); l != nil {
		t.Fatalf("the addendum arm counts; it must not claim a loss: %+v", l)
	}
}

// The negative half: a corpus whose only out-of-section rows sit in a named
// reference table reports no addendum row at all, so the new line means
// something when it is present.
func TestAnEvidenceMapAloneProducesNoAddendumCount(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the two populations kept apart.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a read section THEN the row is captured |

## Evidence map

| Item | Test |
|---|---|
| SCN-CV-001 | a test that evidences it |
`)
	r := reportOf(t, root)
	for _, c := range r.Counts {
		if strings.Contains(c.Group, addendumGroupMarker) {
			t.Fatalf("no unrecognized section here, yet %q was reported", c.Group)
		}
	}
}

// ---------------------------------------------------------------- loop-status cap

// The extractor cuts a loop-status cell at its cap and syncs the head. Nothing
// said so: the epic count reconciled, the payload carried a status, and the
// remainder of the sentence existed only in a file the flip retires.
func TestOverCapLoopStatusCellsAreReportedAsTruncated(t *testing.T) {
	root := cleanEpicCorpus(t)
	// Synthetic, and deliberately past the RAISED cap: no cell in the tracked
	// corpus reaches it any more, so the arm can only stay measurable on a
	// fixture that builds one. Sized from the constant rather than a literal,
	// so raising the cap again cannot leave the arm asserting nothing.
	long := "lower verified " + strings.Repeat("and re-verified ", loopStatusCap/16+4) + "at the delivered revision"
	if len([]rune(long)) <= loopStatusCap {
		t.Fatalf("fixture must exceed the cap, got %d runes", len([]rune(long)))
	}
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | "+long+" | IN_REVIEW | — | — |\n")

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "loop-status:lower")
	if l == nil || l.Category != LossTruncated {
		t.Fatalf("an over-cap loop-status cell must be reported as truncated, got %+v (losses %+v)", l, r.Losses)
	}
	if l.RecordID != "EPIC-CV-001" {
		t.Fatalf("the loss must name the epic, got %q", l.RecordID)
	}
	for _, want := range []string{"WORKLIST.md:", strconv.Itoa(loopStatusCap)} {
		if !strings.Contains(l.Detail, want) {
			t.Fatalf("the detail must locate the cell and state both lengths, %q missing from %q", want, l.Detail)
		}
	}
	if lossWithField(r.Losses, "loop-status:upper") != nil {
		t.Fatal("the upper cell is a placeholder here; only the cut cell may be reported")
	}
}

// The negative half: a cell exactly at the cap loses nothing, and a placeholder
// cell is absence rather than a cut.
func TestLoopStatusAtOrUnderTheCapIsNotReported(t *testing.T) {
	root := cleanEpicCorpus(t)
	atCap := strings.Repeat("x", loopStatusCap)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | "+atCap+" | — | IN_REVIEW | — | — |\n")

	r := reportOf(t, root)
	if ls := lossesWithFieldPrefix(r.Losses, "loop-status:"); len(ls) != 0 {
		t.Fatalf("a cell at the cap is carried whole: %+v", ls)
	}
}

// ---------------------------------------------------------------- fast-lane placeholder wording

// Eleven fast-lane rows sharing the "—" placeholder are not eleven duplicate
// identities, and the hygiene note must not say they are. The escalation to a
// LOSS already refuses them; the note that stayed behind still read as a
// collision report.
func TestFastLanePlaceholderRowsReadAsPlaceholders(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| — | fast lane (REQ row is the task record) | REQ-CV-001 | — | REQ-CV-001 | — | — | — | DONE | — | — |\n"+
		"| — | fast lane (REQ row is the task record) | REQ-CV-002 | — | REQ-CV-002 | — | — | — | DONE | — | — |\n")

	r := reportOf(t, root)
	var placeholders int
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "duplicate WORKLIST rollup id —") {
			t.Fatalf("a shared placeholder is not a duplicate id: %q", h.Detail)
		}
		if strings.Contains(h.Detail, "fast-lane") {
			placeholders++
		}
	}
	if placeholders == 0 {
		t.Fatal("the placeholder rows must still be flagged, in their own words")
	}
}

// The genuine-id note is untouched: a real collision still reads as one.
func TestGenuineDuplicateIDsKeepTheirHygieneWording(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| EPIC-CV-001 | `epics/EPIC-CV-001-only.md` | UR-CV-009 | SCN-CV-009 | REQ-CV-001 | T2 | — | — | IN_PROGRESS | — | second |\n")

	r := reportOf(t, root)
	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "duplicate WORKLIST rollup id EPIC-CV-001") {
			found = true
		}
	}
	if !found {
		t.Fatal("a genuine duplicate id must keep its wording")
	}
}

// ---------------------------------------------------------------- register narratives

// The narrative arm reconciles both directions: blocks that found their row,
// and blocks whose id matches none. The register's closed-gaps template is the
// second case, and it must be visible rather than silently ignored.
func TestRegisterNarrativeCoverageIsReconciled(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "process/gap-register.md", `# Gap Register

## Gaps

| ID | Description | Owning Context |
|----|-------------|----------------|
| GAP-002 | The snapshot revision is unreachable | CROSS |

### GAP-002 — the recorded snapshot revision is unreachable

**Gap:** the revision exists upstream only as the head of a PR.

## Closed Gaps

### GAP-NNN — «Description» (CLOSED «YYYY-MM-DD»)

**Gap:** «What was missing»
`)
	r := reportOf(t, root)
	c := countGroup(t, r, narrativeCoverageGroup)
	if c.Rows != 2 || c.Ops != 1 {
		t.Fatalf("narrative coverage = %d/%d, want 2/1", c.Rows, c.Ops)
	}
	if len(c.MissingFromOps) != 1 || c.MissingFromOps[0] != "GAP-NNN" {
		t.Fatalf("the unmatched block must be named, got %v", c.MissingFromOps)
	}
}

// A backlog record whose notes exceed the wire contract's declared cap is
// flagged, never cut: nothing on the path enforces the cap, so a silent
// oversize payload would reach the server and read as carried.
func TestOversizeBacklogNotesAreFlaggedNotCut(t *testing.T) {
	root := cleanEpicCorpus(t)
	body := strings.Repeat("a gap nobody could summarise. ", (backlogNotesContractCap/30)+40)
	writeFixtureFile(t, root, "process/gap-register.md", "# Gap Register\n\n"+
		"| ID | Description |\n|----|-------------|\n"+
		"| GAP-002 | short |\n\n"+
		"### GAP-002 — the long one\n\n"+body+"\n")

	r := reportOf(t, root)
	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "GAP-002") && strings.Contains(h.Detail, "notes_md") {
			found = true
		}
	}
	if !found {
		t.Fatalf("an oversize backlog note must be flagged: %+v", r.Hygiene)
	}
	// Flagged, not cut: the payload still carries the whole body.
	data, _ := Snapshot(root, manifest.Default())
	for _, b := range data.Backlog {
		if b.ExternalID == "GAP-002" && utf16Len(b.NotesMD) <= backlogNotesContractCap {
			t.Fatalf("the note was shortened to %d units — a guard flags, it does not delete", utf16Len(b.NotesMD))
		}
	}
}

// gapRecordsGroup names the count line for gap-kind records. Gap records reach
// the store from two places — the register's table and the `### GAP-…` blocks a
// ledger files beside its requirements — so the group is named for the kind,
// not for one of its sources.
const gapRecordsGroup = "gap records (register + ledger blocks)"
