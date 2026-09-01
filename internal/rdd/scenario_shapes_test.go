package rdd

import (
	"strings"
	"testing"
)

// REQ-CROSS-028, second pass — the epic records define acceptance scenarios in
// four shapes and only one of them ever reached a payload.
//
// The negative fixtures come FIRST in this file, deliberately. Every shape
// added below is a WIDER matcher, and a wider matcher's risk is not that it
// misses a definition — that failure is visible as a missing scenario. Its risk
// is that it swallows a REFERENCE: an evidence map's bullet, a coverage table's
// first column, a roll-up sentence naming a range of ids. Those would sync as
// the epic's acceptance content, and nothing downstream can tell the difference.
// So each negative population is pinned before the widening that could reach it.

// ---------------------------------------------------------------- negatives

// An `## Evidence map` table cites the scenarios it evidences; its first column
// is full of SCN ids and not one of them is a definition. Table rows are read
// only inside the scenario section for exactly this reason, and the widened
// heading rule must not change that.
func TestEvidenceMapTableRowsAreNotScenarioDefinitions(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Evidence map\n" +
		"| Item | Failing → Passing test | Code |\n|---|---|---|\n" +
		"| SCN-CU-001/002 | `App.codeupdates.test.tsx` (check + dismiss) | `views/CodeFacet.tsx` |\n" +
		"| SCN-CU-003 | `App.codeupdates.test.tsx` \"runs the AI re-analysis…\" | `views/CodeFacet.tsx` |\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("an evidence map is a citation table, got %d scenario(s): %v", len(got), got)
	}
}

// The same trap in bullet form, and this one is inside the reach of the bullet
// matcher. These are the corpus's real evidence bullets: the bold run does not
// stop at the id — it carries the evidence conclusion and its date, and it
// frequently names two ids at once. A matcher that only demands a bold id at
// the head of the bullet reads all seven of them as definitions.
func TestEvidenceMapBulletsAreNotScenarioDefinitions(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Evidence map\n" +
		"- **SCN-GIT-002 (read half) + SCN-GIT-005 (first live PRs) — LOWER_VERIFIED + live, 2026-07-23:** The survey correction landed.\n" +
		"- **SCN-SY-001 + SCN-SY-005 — UPPER_VALIDATED (live e2e), 2026-07-22:** `mp connect` reached the server.\n" +
		"- **SCN-SY-015 (conflict triage) — LOWER_VERIFIED, 2026-07-22:** RED 1 failing → GREEN `sync_test.exs` 13/13.\n" +
		"- **SCN-SY-013 (drift half: Done decays) — LOWER_VERIFIED + live full cycle, 2026-07-22:** RED 2 failing → GREEN.\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("evidence bullets cite scenarios, they do not define them; got %d: %v", len(got), got)
	}
}

// The same evidence bullet, moved INSIDE the scenario section — which is where
// an author writing a status note beside the scenarios puts it. The section can
// no longer rule it out, so the shape must: a definition's bold run stops at the
// id and at most one parenthetical qualifier. Two independent guards, because
// one of them is one edit away from being the only one.
func TestAnEvidenceBulletInsideTheScenarioSectionIsStillNotADefinition(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## BDD acceptance scenarios\n" +
		"- **SCN-TST-001** — GIVEN the epic WHEN it ships THEN the outcome is observable.\n" +
		"- **SCN-TST-050 (upper half) — UPPER_VALIDATED (live), 2026-08-20:** RED 1 failing → GREEN 12/12.\n" +
		"- **SCN-TST-051 + SCN-TST-052 — LOWER_VERIFIED, 2026-08-20:** the focused suite is green.\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected the one definition, got %d: %v", len(got), got)
	}
	s := got[0].(map[string]any)
	if s["external_id"] != "EPIC-T#SCN-TST-001" || s["then"] != "the outcome is observable." {
		t.Fatalf("an evidence bullet reached the payload: %v", s)
	}
}

// A roll-up sentence names a RANGE of ids and defines none of them. It has no
// bold id, which is the whole discriminator between a definition bullet and a
// mention — so this belongs to no shape and must stay outside every count.
func TestProseRollupBulletsAreNotScenarioDefinitions(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n" +
		"- SCN-EVAL-101..107 have failing-then-passing evidence recorded in the evidence map.\n" +
		"- The remaining SCN-EVAL-108 work is deferred to the next release.\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("a roll-up sentence defines nothing, got %d: %v", len(got), got)
	}
}

// A traceability matrix has a scenario COLUMN, so its rows lead with SCN ids in
// a section whose heading says "traceability". The heading rule must reject it
// even though it contains the word: a matrix relates definitions written
// elsewhere, and syncing its rows would replace them with their own index.
func TestTraceabilityTableRowsAreNotScenarioDefinitions(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Requirement traceability — scenario coverage\n" +
		"| Scenario | UR | SR | Status |\n|---|---|---|---|\n" +
		"| SCN-TST-001 | UR-TST-001 | SR-TST-001 | IN_PROGRESS |\n" +
		"| SCN-TST-002 | UR-TST-001 | SR-TST-002 | IN_PROGRESS |\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("a traceability matrix indexes scenarios, got %d: %v", len(got), got)
	}
}

// Inside a heading block, everything up to the next heading is that scenario's
// body — including a table that cites sibling ids and a sentence that names
// one. A body that could start a second scenario would split one definition
// into two and give the second one the first one's prose.
func TestAnIDInsideAHeadingBlockBodyStartsNoNewScenario(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Scenarios\n\n" +
		"### SCN-TST-001 — The one scenario\n\n" +
		"Given the list, when an id is selected, then its detail opens.\n" +
		"Related: SCN-TST-002 covers the reverse direction.\n\n" +
		"| Item | State |\n|---|---|\n" +
		"| SCN-TST-002 | IN_PROGRESS |\n" +
		"- **SCN-TST-003** — a bullet the author indented into the body.\n\n" +
		"## Next\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("the block body must stay one scenario, got %d: %v", len(got), got)
	}
	s := got[0].(map[string]any)
	if s["external_id"] != "EPIC-T#SCN-TST-001" {
		t.Fatalf("id = %v", s["external_id"])
	}
	if !strings.Contains(scenarioContent(s), "SCN-TST-002 covers the reverse") {
		t.Fatalf("the body is the scenario's text: %v", s)
	}
}

// A fenced block is quoted material — a trace diagram, a template excerpt, a
// gherkin sample. A `## Scenarios` heading inside one opens no section, and an
// id-led line inside one defines nothing; the real section after the fence is
// still read.
func TestAFencedScenarioHeadingOpensNoSection(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Trace\n\n" +
		"```text\n" +
		"## Scenarios\n" +
		"| SCN | Summary | Status |\n" +
		"|---|---|---|\n" +
		"| SCN-QUOTED-001 | Quoted from the template. | DONE |\n" +
		"- **SCN-QUOTED-002** — also quoted.\n" +
		"### SCN-QUOTED-003 — quoted heading\n" +
		"```\n\n" +
		"## Acceptance scenarios\n" +
		"| SCN | Summary | Status |\n|---|---|---|\n" +
		"| SCN-TST-001 | The real observable outcome. | UPPER_VALIDATED |\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected the real section's 1 scenario, got %d: %v", len(got), got)
	}
	if id := got[0].(map[string]any)["external_id"]; id != "EPIC-T#SCN-TST-001" {
		t.Fatalf("the fence hijacked the parse: got %v", id)
	}
}

// The out-of-scope rule now has to hold for bullets and heading blocks too,
// because those are read wherever they sit. A deferred section's definitions
// are real definitions of work this epic is NOT accepting.
func TestDefinitionsInAnOutOfScopeSectionStayOut(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Deferred acceptance scenarios (out of scope)\n" +
		"- **SCN-TST-900** — GIVEN the next release WHEN it starts THEN this is picked up.\n\n" +
		"### SCN-TST-901 — Also deferred\n\n" +
		"Given nothing, when nothing, then nothing.\n\n" +
		"## Acceptance scenarios\n" +
		"- **SCN-TST-001** — GIVEN the epic WHEN it ships THEN the outcome is observable.\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected 1 in-scope scenario, got %d: %v", len(got), got)
	}
	if id := got[0].(map[string]any)["external_id"]; id != "EPIC-T#SCN-TST-001" {
		t.Fatalf("a deferred definition reached the payload: %v", id)
	}
}

// ---------------------------------------------------------------- headings

// The heading decoration is open-ended in practice — a numbered prefix from a
// record written as a numbered document, a "(SCN)" tag before the loop
// qualifier, a parenthetical naming the source feature file. An alternation of
// the exact shapes seen so far reads every unlisted one as "this record has no
// scenarios at all", which is how 15 definitions in two records reached nothing.
func TestScenarioSectionHeadingsTheCorpusActuallyWrites(t *testing.T) {
	for _, heading := range []string{
		"## Scenarios",
		"## Acceptance scenarios",
		"## BDD acceptance scenarios",
		"## BDD acceptance scenarios (upper loop)",
		"## BDD acceptance scenarios (SCN — upper loop)",
		"## BDD acceptance scenarios (SCN) — upper loop",
		"## 4. BDD acceptance scenarios (SCN) — upper loop",
		"## BDD acceptance scenarios (outline — sharpened at build)",
		"## BDD acceptance scenarios (outline — sharpened at SPEC-READY)",
		"## BDD acceptance scenarios (from `features/chat-sparring.md` §6; upper-loop form)",
		"## SCN — acceptance scenarios",
		"## SCN — BDD acceptance scenarios (upper loop)",
		"## Journeys and acceptance scenarios",
	} {
		record := "# EPIC-T — t\n\n" + heading + "\n" +
			"| SCN | Summary | Status |\n|---|---|---|\n" +
			"| SCN-TST-001 | An outcome. | READY |\n\n## Next\n"
		if got := ParseScenarios(record, "EPIC-T"); len(got) != 1 {
			t.Errorf("%q: expected 1 scenario, got %d", heading, len(got))
		}
	}
}

// The negative half of the same rule, kept beside it: a heading that contains
// the word and answers a different question.
func TestHeadingsThatNameScenariosButOpenNoSection(t *testing.T) {
	for _, heading := range []string{
		"## Deferred acceptance scenarios",
		"## Acceptance scenarios (deferred/out of scope)",
		// The hyphenated spelling of the same phrase. The positive rule is now
		// wide enough that only this list keeps the section out, so a phrase
		// that matches one spelling and not the other is a hole in it.
		"## Out-of-scope scenarios",
		"## Acceptance scenarios (out-of-scope)",
		"## Rejected scenarios",
		"## Superseded scenarios",
		"## Scenario evidence map",
		"## Scenario coverage",
		"## Requirement traceability — scenarios",
	} {
		record := "# EPIC-T — t\n\n" + heading + "\n" +
			"| SCN | Summary | Status |\n|---|---|---|\n" +
			"| SCN-TST-900 | Not this epic's acceptance. | DEFERRED |\n\n## Next\n"
		if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
			t.Errorf("%q: expected no scenarios, got %d", heading, len(got))
		}
	}
}

// ---------------------------------------------------------------- bullets

// The bullet form is the corpus's most common uncaptured shape: 107 definitions
// across 29 records. The bold run may carry one parenthetical qualifier and may
// close before or after a trailing colon; the id may carry a letter suffix
// (SCN-BD-070a..d are four different scenarios, and an id pattern that stops at
// the digits collapses all four onto one external id).
func TestBulletDefinitionsBecomeScenarios(t *testing.T) {
	cases := []struct{ name, bullet, wantID, wantText string }{
		{
			name:     "bold id, em-dash separator, GWT",
			bullet:   "- **SCN-CU-001** — GIVEN the Code facet WHEN \"Check for updates\" is clicked THEN the change plan renders.",
			wantID:   "SCN-CU-001",
			wantText: "GIVEN the Code facet WHEN \"Check for updates\" is clicked THEN the change plan renders.",
		},
		{
			name:   "parenthetical qualifier inside the bold run, colon separator",
			bullet: "- **SCN-MAP-001 (real nodes):** GIVEN a tenant with analyzed systems, WHEN I open `/map`, THEN each system renders as a node.",
			wantID: "SCN-MAP-001",
			// The comma joins the clauses; it is separator, not clause content.
			wantText: "GIVEN a tenant with analyzed systems WHEN I open `/map` THEN each system renders as a node.",
		},
		{
			name:     "qualifier inside the bold run, em dash outside it",
			bullet:   "- **SCN-RLS-003 (no regression)** — the Data Model API still returns the caller's real domains.",
			wantID:   "SCN-RLS-003",
			wantText: "the Data Model API still returns the caller's real domains.",
		},
		{
			name:     "a letter-suffixed id is its own scenario",
			bullet:   "- **SCN-BD-070a** — GIVEN a board card WHEN it is opened THEN the CardPanel is shown.",
			wantID:   "SCN-BD-070a",
			wantText: "GIVEN a board card WHEN it is opened THEN the CardPanel is shown.",
		},
	}
	for _, c := range cases {
		record := "# EPIC-T — t\n\n## BDD acceptance scenarios\n" + c.bullet + "\n\n## Next\n"
		got := ParseScenarios(record, "EPIC-T")
		if len(got) != 1 {
			t.Errorf("%s: expected 1 scenario, got %d", c.name, len(got))
			continue
		}
		s := got[0].(map[string]any)
		if s["external_id"] != "EPIC-T#"+c.wantID {
			t.Errorf("%s: id = %v, want EPIC-T#%s", c.name, s["external_id"], c.wantID)
		}
		text, _ := s["statement"].(string)
		if g, ok := s["given"].(string); ok {
			text = "GIVEN " + g + " WHEN " + s["when"].(string) + " THEN " + s["then"].(string)
		}
		if text != c.wantText {
			t.Errorf("%s:\n  got  %q\n  want %q", c.name, text, c.wantText)
		}
	}
}

// 51 of the 107 bullets wrap. A matcher that reads only the first line cuts the
// statement mid-sentence — and for a GWT bullet it cuts the THEN clause, which
// is the observable outcome the scenario exists to state.
func TestAWrappedBulletKeepsItsContinuationLines(t *testing.T) {
	record := "# EPIC-T — t\n\n## BDD acceptance scenarios\n" +
		"- **SCN-CU-001** — GIVEN the Code facet WHEN \"Check for updates\" is clicked THEN the per-repository\n" +
		"  change plan (added/modified/deleted + est. API calls) renders.\n" +
		"- **SCN-CU-002** — GIVEN the change plan WHEN \"Dismiss\" is clicked THEN it clears back to the button.\n\n" +
		"## Next\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 2 {
		t.Fatalf("expected 2 scenarios, got %d: %v", len(got), got)
	}
	first := got[0].(map[string]any)
	then, _ := first["then"].(string)
	if then != "the per-repository change plan (added/modified/deleted + est. API calls) renders." {
		t.Fatalf("the continuation line was dropped: THEN = %q", then)
	}
	if got[1].(map[string]any)["external_id"] != "EPIC-T#SCN-CU-002" {
		t.Fatalf("the second bullet is its own scenario: %v", got[1])
	}
}

// A bullet definition is recognized by its shape, not by the section it sits
// in — one record writes its acceptance under `## Acceptance (SLICE-1)`, a
// heading that names no scenarios and never will.
func TestBulletDefinitionsOutsideAScenarioSectionAreStillDefinitions(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance (SLICE-1)\n" +
		"- **SCN-SH-001** — the sidebar groups nav into labeled sections.\n" +
		"- **SCN-SH-002** — each system in the SYSTEMS list expands to its facet links.\n\n" +
		"## Tasks (SLICE-1)\n" +
		"- **TASK-SH-002** — `App.shell.test.tsx` (SCN-SH-001/002).\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 2 {
		t.Fatalf("expected 2 scenarios, got %d: %v", len(got), got)
	}
	if got[1].(map[string]any)["external_id"] != "EPIC-T#SCN-SH-002" {
		t.Fatalf("ids = %v", got)
	}
}

// ---------------------------------------------------------------- heading blocks

// The heading-block form carries the richest bodies in the corpus — full
// gherkin with its source citations — and the fence is content, not structure.
func TestHeadingBlockDefinitionsCarryTheirWholeBody(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## BDD acceptance scenarios\n\n" +
		"### SCN-PF-001 - Gallery cards show server-computed counts\n\n" +
		"```gherkin\n" +
		"Scenario: Gallery cards show server-computed counts\n" +
		"  Given a tenant has an analyzed system\n" +
		"  When the user opens /systems\n" +
		"  Then each card shows its counts\n" +
		"```\n\n" +
		"### SCN-PF-002 — Active/Drafts pills filter by status\n\n" +
		"Given systems exist with status active and draft, when the Drafts pill is selected, then only drafts remain.\n\n" +
		"## Next\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 2 {
		t.Fatalf("expected 2 scenarios, got %d: %v", len(got), got)
	}
	first := got[0].(map[string]any)
	if first["external_id"] != "EPIC-T#SCN-PF-001" {
		t.Fatalf("id = %v", first["external_id"])
	}
	body := scenarioContent(first)
	for _, want := range []string{
		"Gallery cards show server-computed counts",
		"a tenant has an analyzed system",
		"each card shows its counts",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the block body lost %q:\n%s", want, body)
		}
	}
	// The clause triple is the scenario's own content whichever notation writes
	// it — fenced and title-cased here, hard-wrapped prose below. Reading only
	// the uppercase one-line form left both of these unqueryable (REQ-CROSS-252).
	second := got[1].(map[string]any)
	if !strings.Contains(scenarioContent(second), "only drafts remain") {
		t.Fatalf("the second block body: %v", second)
	}
	if g, _ := second["given"].(string); !strings.Contains(g, "systems exist with status active and draft") {
		t.Fatalf("the prose triple must split: %v", second)
	}
}

// Six records write BOTH a summary table and a heading block for the same ids.
// One id is one scenario: the payload is a replace-set keyed by external_id, so
// two entries for one id are a row fighting itself. The block body wins because
// it is the definition the table indexes, and the position stays where the
// table put it so an unchanged record keeps its order.
func TestASummaryTableAndItsHeadingBlockAreOneScenario(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## BDD acceptance scenarios\n\n" +
		"| Scenario | Story | Summary | Status |\n|---|---|---|---|\n" +
		"| SCN-PF-001 | STORY-PF-001 | Gallery cards show server-computed counts. | UPPER_VALIDATED |\n" +
		"| SCN-PF-002 | STORY-PF-002 | Active/Drafts pills filter by status. | UPPER_VALIDATED |\n\n" +
		"### SCN-PF-002 — Active/Drafts pills filter by status\n\n" +
		"Given systems exist with status active and draft, when the Drafts pill is selected,\n" +
		"then only the draft systems remain and the pill counts stay true.\n\n" +
		"## Next\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 2 {
		t.Fatalf("one id is one scenario; got %d: %v", len(got), got)
	}
	second := got[1].(map[string]any)
	if second["external_id"] != "EPIC-T#SCN-PF-002" {
		t.Fatalf("position must follow the table's order: %v", second["external_id"])
	}
	if second["position"] != 2 {
		t.Fatalf("position = %v, want 2", second["position"])
	}
	if !strings.Contains(scenarioContent(second), "the pill counts stay true") {
		t.Fatalf("the richer body must win: %v", second)
	}
	if got[0].(map[string]any)["statement"] != "Gallery cards show server-computed counts." {
		t.Fatalf("the table-only row keeps its summary: %v", got[0])
	}
}

// The same merge with the shapes in the other order — the block FIRST and the
// index row later in the file. The rank is a rank, not "the last one wins": a
// merge that replaced on any change of shape would pass the fixture above,
// where the richer form happens to come second, and quietly overwrite every
// heading block whose record also indexes it further down.
func TestAPoorerShapeLaterInTheRecordDoesNotReplaceTheBlock(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## BDD acceptance scenarios\n\n" +
		"### SCN-PF-002 — Active/Drafts pills filter by status\n\n" +
		"Given systems exist with status active and draft, when the Drafts pill is selected,\n" +
		"then only the draft systems remain and the pill counts stay true.\n\n" +
		"### Summary index\n\n" +
		"| Scenario | Summary | Status |\n|---|---|---|\n" +
		"| SCN-PF-002 | Active/Drafts pills filter by status. | UPPER_VALIDATED |\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("one id is one scenario; got %d: %v", len(got), got)
	}
	body := scenarioContent(got[0].(map[string]any))
	if !strings.Contains(body, "the pill counts stay true") {
		t.Fatalf("the block's body must survive the index row that follows it: %q", body)
	}
}

// A scenario row's cells split on UNESCAPED pipes only, like every other table
// row. The node mirror split this one row on every pipe while Go unescaped it,
// so the two builders read a different statement out of the same row — the kind
// of divergence only the live parity run can catch, and only if the corpus
// happens to contain the escape.
func TestAnEscapedPipeIsScenarioCellContent(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n" +
		"| Scenario | Summary | Status |\n|---|---|---|\n" +
		`| SCN-TST-001 | The guard reduces \|difference\| toward zero. | DONE |` + "\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected 1 scenario, got %d: %v", len(got), got)
	}
	if s := got[0].(map[string]any)["statement"]; s != "The guard reduces |difference| toward zero." {
		t.Fatalf("statement = %q — the escape split the row", s)
	}
}
