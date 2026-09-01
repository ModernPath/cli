package rdd

import (
	"strings"
	"testing"
)

// REQ-CROSS-028, fourth shape — a record writes its acceptance scenarios as
// Gherkin inside a ```gherkin fence, under sub-headings of its scenario section.
// The parser treated every fenced line as quoted material, so the record read as
// defining nothing while its definitions sat in plain sight.
//
// The negatives come first, as they do for every widening in this file's
// sibling: a fence is where the corpus quotes things, so the risk of reading
// inside one is not a missed definition but a swallowed reference — a trace
// diagram's range of ids, an evidence line naming what covered what. The
// discriminator is the Gherkin keyword and its colon before an SCN id; nothing
// else in a fence declares.

// ---------------------------------------------------------------- negatives

// A trace diagram quoted in a ```text fence names a RANGE of ids and declares
// nothing. It sits INSIDE the scenario section here, so the section cannot rule
// it out — the keyword rule must.
func TestAFencedTraceDiagramInTheScenarioSectionDefinesNothing(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"```text\n" +
		"UR-NAM-201\n" +
		"  -> EPIC-NAM-201\n" +
		"  -> SCN-NAM-201..205\n" +
		"  -> SR-NAM-201..205\n" +
		"```\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("a quoted trace diagram declares nothing, got %d: %v", len(got), got)
	}
}

// A line that begins with the word Scenario but is not a declaration. The
// corpus writes `Scenario evidence: SCN-…` as a coverage note, and reading it as
// acceptance content would sync a record of how a scenario was evidenced as the
// scenario itself. Only `Scenario:` and `Scenario Outline:` declare.
func TestAFencedScenarioEvidenceLineDeclaresNothing(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"```text\n" +
		"Scenario evidence: SCN-TST-030 (found unaided) · SCN-TST-031 (nothing lost)\n" +
		"Scenarios covered: SCN-TST-032\n" +
		"```\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("an evidence line cites, it does not declare; got %d: %v", len(got), got)
	}
}

// A Gherkin declaration with no SCN id is a template excerpt or a sample. It
// names no identity, so it can reach no scenario record.
func TestAFencedDeclarationWithoutAnIDDefinesNothing(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"```gherkin\n" +
		"Scenario: the shape a record should write\n" +
		"  Given a template\n" +
		"  Then it is copied, not synced\n" +
		"```\n"
	if got := ParseScenarios(record, "EPIC-T"); len(got) != 0 {
		t.Fatalf("an unidentified declaration defines nothing, got %d: %v", len(got), got)
	}
}

// Outside the scenario section a fenced declaration stays quoted. A record
// quotes the implementer's Gherkin beside a design note constantly, and the
// section is what separates this epic's acceptance content from a sample of it.
// The fidelity fence arm still reports the population, so it is visible rather
// than dropped.
func TestAFencedDeclarationOutsideTheScenarioSectionIsNotCaptured(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Design notes\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-050 — quoted for the implementer\n" +
		"  Given a note\n" +
		"  Then nothing is accepted here\n" +
		"```\n\n" +
		"## Acceptance scenarios\n" +
		"| SCN | Summary | Status |\n|---|---|---|\n" +
		"| SCN-TST-001 | The real observable outcome. | UPPER_VALIDATED |\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected the section's 1 scenario, got %d: %v", len(got), got)
	}
	if id := got[0].(map[string]any)["external_id"]; id != "EPIC-T#SCN-TST-001" {
		t.Fatalf("a fence outside the section reached the payload: %v", id)
	}
}

// An out-of-scope scenario section defines work this epic is not accepting,
// whatever shape it writes — the same rule the table, bullet and heading shapes
// already follow.
func TestAFencedDeclarationInAnOutOfScopeSectionStaysOut(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Deferred acceptance scenarios (out of scope)\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-900 — deliberately not built here\n" +
		"  Given a deferred behaviour\n" +
		"  Then it belongs to another epic\n" +
		"```\n\n" +
		"## Acceptance scenarios\n" +
		"- **SCN-TST-001** — GIVEN the epic WHEN it ships THEN the outcome is observable.\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("expected 1 in-scope scenario, got %d: %v", len(got), got)
	}
	if id := got[0].(map[string]any)["external_id"]; id != "EPIC-T#SCN-TST-001" {
		t.Fatalf("a deferred declaration reached the payload: %v", id)
	}
}

// ---------------------------------------------------------------- the shape

// The corpus's real record: `## Acceptance scenarios`, `### Slice N` between the
// fences, and three declarations per fence. Each definition runs from its
// declaration line to the next declaration or the fence's end, carried verbatim.
func TestFencedGherkinDefinitionsInTheScenarioSectionBecomeScenarios(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"### Slice 1 — the batch says what produced it\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-130 — a sync attributes itself to a build\n" +
		"  Given a workspace syncing with a client that stamps its build\n" +
		"  When the batch is posted\n" +
		"  Then the batch carries that client identity\n" +
		"  And the server records it against the resulting work event\n" +
		"\n" +
		"Scenario Outline: SCN-TST-131 — a client that sends no identity is still served\n" +
		"  Given an older client that knows nothing about client identity\n" +
		"  Then the batch is accepted exactly as today\n" +
		"```\n\n" +
		"### Slice 2 — the server says when that client is behind\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-132 — a client behind the current version is told so\n" +
		"  Given a server configured with a current client version\n" +
		"  Then the response carries an advisory naming both versions\n" +
		"```\n\n" +
		"`SCN-TST-132` is the one the rest of this record was written around.\n"

	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 3 {
		t.Fatalf("three fenced declarations, got %d: %v", len(got), got)
	}
	for i, want := range []string{"SCN-TST-130", "SCN-TST-131", "SCN-TST-132"} {
		s := got[i].(map[string]any)
		if s["external_id"] != "EPIC-T#"+want {
			t.Fatalf("scenario %d id = %v, want EPIC-T#%s", i, s["external_id"], want)
		}
		if s["position"] != i+1 {
			t.Fatalf("scenario %d position = %v", i, s["position"])
		}
	}

	// The declaration's own title is kept and every clause it wrote is carried:
	// an author who wrote four clauses must not have the fourth cut off.
	first := got[0].(map[string]any)
	if first["title"] != "a sync attributes itself to a build" {
		t.Fatalf("the declaration's own text must be kept as the title: %v", first["title"])
	}
	for _, want := range []string{
		"a workspace syncing with a client that stamps its build",
		"the batch is posted",
		"the batch carries that client identity",
		"And the server records it against the resulting work event",
	} {
		if !strings.Contains(scenarioContent(first), want) {
			t.Fatalf("the body must be carried, %q missing from %v", want, first)
		}
	}
	// The definition ends where the next one starts.
	if strings.Contains(scenarioContent(first), "SCN-TST-131") {
		t.Fatalf("one definition swallowed the next: %v", first)
	}
	// And it ends at the fence, not at the prose after it.
	third := got[2].(map[string]any)
	if strings.Contains(scenarioContent(third), "written around") {
		t.Fatalf("the definition ran past its fence: %v", third)
	}
}

// scenarioContent is everything a parsed scenario carries, whichever way its
// text landed — the boundary rules above hold for a split triple and for a
// statement alike.
func scenarioContent(s map[string]any) string {
	out := ""
	for _, k := range []string{"title", "given", "when", "then", "statement"} {
		if v, _ := s[k].(string); v != "" {
			out += v + "\n"
		}
	}
	return out
}

// ---------------------------------------------------------------- merging

// A fence that RESTATES an id the section already defines is one scenario, not
// two: the payload is a replace-set keyed by external_id, and two entries for
// one id are one row fighting itself. The fenced form is the richer one — a
// summary table's cell is an index into it — so it wins the text and keeps the
// position the record first gave it.
func TestAFencedRestatementMergesIntoTheScenarioItRestates(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"| SCN | Summary | Status |\n|---|---|---|\n" +
		"| SCN-TST-001 | The batch says what produced it. | UPPER_VALIDATED |\n" +
		"| SCN-TST-002 | A second, defined only here. | TODO |\n\n" +
		"### Slice 1 — in full\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-001 — the same scenario, written out\n" +
		"  Given a summary table that indexes it\n" +
		"  Then the fuller form is what reaches the store\n" +
		"```\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 2 {
		t.Fatalf("a restatement must merge, got %d: %v", len(got), got)
	}
	first := got[0].(map[string]any)
	if first["external_id"] != "EPIC-T#SCN-TST-001" {
		t.Fatalf("the merged scenario must keep its position: %v", first["external_id"])
	}
	if !strings.Contains(scenarioContent(first), "the fuller form is what reaches the store") {
		t.Fatalf("the richer fenced form must win the text: %v", first)
	}
}

// The other direction of the same rank rule, and the mutation that a merge test
// alone cannot catch: a `### SCN-…` heading block already carries any fence in
// its own body verbatim, so it is a superset and must not be replaced by the
// declaration inside it. The block's body is skipped whole, so the fence there
// can never start a second entry either.
func TestAFencedDeclarationInsideAHeadingBlockDoesNotSplitIt(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"### SCN-TST-001 — The one scenario\n\n" +
		"The narrative the author wrote above the Gherkin.\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-001 — the same id, written inside its own block\n" +
		"  Given the block\n" +
		"  Then nothing is split off it\n" +
		"```\n\n" +
		"## Next\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("the block body must stay one scenario, got %d: %v", len(got), got)
	}
	one := got[0].(map[string]any)
	if one["title"] != "The one scenario" {
		t.Fatalf("the heading block must keep its own title: %v", one["title"])
	}
	if !strings.Contains(scenarioContent(one), "The narrative the author wrote above the Gherkin.") {
		t.Fatalf("the block's narrative must survive: %v", one)
	}
	if !strings.Contains(scenarioContent(one), "nothing is split off it") {
		t.Fatalf("the block carries its fenced clauses: %v", one)
	}
}

// The rank itself, on the only input where it decides anything: a fence
// restating an id whose heading block sits EARLIER in the same section. Inside
// the block the fence is skipped with the body and never reaches the merge at
// all, so the case above pins the skip rather than the rank — and the mutation
// "a fence outranks the block" survives it. Here the two forms really do
// compete, and the author's narrative must win over the fragment restating it.
func TestAFencedRestatementDoesNotReplaceAnEarlierHeadingBlock(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"### SCN-TST-001 — The one scenario\n\n" +
		"The narrative the author wrote, and the reason the outcome matters.\n\n" +
		"### Slice 2 — the same scenario, restated for the implementer\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-001 — a fragment of itself\n" +
		"  Given the restatement\n" +
		"  Then it must not replace the block\n" +
		"```\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("one id is one scenario, got %d: %v", len(got), got)
	}
	one := got[0].(map[string]any)
	if !strings.Contains(scenarioContent(one), "The narrative the author wrote") {
		t.Fatalf("the fenced restatement replaced the block: %v", one)
	}
	if !strings.Contains(scenarioContent(one), "The one scenario") {
		t.Fatalf("the heading block must keep its own title: %v", one)
	}
}

// A poorer shape written AFTER the fence must not replace it — the rank rule is
// about which form is richer, not which came last. Without this the mutation
// "always replace" survives every other case in this file.
func TestAPoorerShapeAfterAFencedDefinitionDoesNotReplaceIt(t *testing.T) {
	record := "# EPIC-T — t\n\n" +
		"## Acceptance scenarios\n\n" +
		"### Slice 1 — in full\n\n" +
		"```gherkin\n" +
		"Scenario: SCN-TST-001 — the fuller form\n" +
		"  Given the fenced declaration\n" +
		"  Then its body is the scenario\n" +
		"```\n\n" +
		"### Index\n\n" +
		"| SCN | Summary | Status |\n|---|---|---|\n" +
		"| SCN-TST-001 | One line indexing it. | TODO |\n"
	got := ParseScenarios(record, "EPIC-T")
	if len(got) != 1 {
		t.Fatalf("one id is one scenario, got %d: %v", len(got), got)
	}
	if body := scenarioContent(got[0].(map[string]any)); !strings.Contains(body, "its body is the scenario") {
		t.Fatalf("the table cell replaced the fuller fenced form: %q", body)
	}
}
