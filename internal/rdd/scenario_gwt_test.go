package rdd

// REQ-CROSS-252: a scenario record holds given/when/then OR a statement. The
// corpus writes the clause triple in four notations and only one of them —
// uppercase, unprefixed, on one line — was ever split; the other three fell
// whole into `statement`, which is why 159 of 397 statement rows still carry a
// Given and a Then inside the prose. Each case below is a definition taken
// verbatim from the corpus.

import (
	"strings"
	"testing"
)

func joinLines(l ...string) string { return strings.Join(l, "\n") }

func scenarioPayload(t *testing.T, epic Epic, record, id string) map[string]any {
	t.Helper()
	for _, op := range BuildScenarioOps(epic, record, nil) {
		if op.Payload["external_id"] == id {
			return op.Payload
		}
	}
	t.Fatalf("no scenario record for %s: %v", id, opIDs(BuildScenarioOps(epic, record, nil)))
	return nil
}

func wantClause(t *testing.T, p map[string]any, field, want string) {
	t.Helper()
	got, _ := p[field].(string)
	if !strings.Contains(got, want) {
		t.Fatalf("%s = %q, want it to contain %q", field, got, want)
	}
}

func wantNoStatement(t *testing.T, p map[string]any) {
	t.Helper()
	if s, _ := p["statement"].(string); s != "" {
		t.Fatalf("a split scenario keeps no statement, got %q", s)
	}
}

// A requirement reference between the id and the clause triple is decoration,
// not content: the triple is the canonical uppercase form and must still split.
func TestPrefixedUppercaseTripleSplits(t *testing.T) {
	record := joinLines(
		"# EPIC-CHAT-001 — Chat sparring console",
		"",
		"## Acceptance scenarios",
		"",
		"- **SCN-CHAT-001** (REQ-PLN-001) — GIVEN an authed user on `/console` WHEN they submit intent THEN a",
		"  Chat is created, the user turn echoes optimistically, and the agent reply streams.",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-CHAT-001", Record: "epics/EPIC-CHAT-001-chat-sparring-console.md"},
		record, "EPIC-CHAT-001#SCN-CHAT-001")

	wantClause(t, p, "given", "an authed user on `/console`")
	wantClause(t, p, "when", "they submit intent")
	wantClause(t, p, "then", "a Chat is created")
	wantNoStatement(t, p)
	if g, _ := p["given"].(string); strings.Contains(g, "REQ-PLN-001") {
		t.Fatalf("the requirement reference is not part of the GIVEN clause: %q", g)
	}
	// It is a declaration in the position another record writes as a Realizes
	// column, so it lands in the declared requirement list rather than nowhere.
	required := anyStrings(p["required_requirement_external_ids"].([]any))
	if !strings.Contains(strings.Join(required, " "), "REQ-PLN-001") {
		t.Fatalf("the inline reference must land as a required requirement: %v", required)
	}
}

// The Gherkin notation the corpus fences: title-case keywords, one clause per
// line, `And` continuations, and a `Sources:` annotation under each clause.
func TestFencedGherkinBlockSplitsByClauseLine(t *testing.T) {
	record := joinLines(
		"# EPIC-ANALYSIS-004 — Grounded architecture docs",
		"",
		"## 5. Acceptance scenarios",
		"",
		"```gherkin",
		"Scenario: SCN-ANALYSIS-040 — a generated doc knows its sources",
		"  Given a completed documentation run for a system",
		"  When any architecture/subsystem/module doc is stored",
		"  Then the doc row carries the structured list of sources the generator used",
		"  And the content hash of the generation is stamped on the row",
		"  And no citation is stripped from persistence (rendering may still hide them)",
		"```",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-ANALYSIS-004", Record: "epics/EPIC-ANALYSIS-004-grounded-architecture-docs/EPIC.md"},
		record, "EPIC-ANALYSIS-004#SCN-ANALYSIS-040")

	wantClause(t, p, "given", "a completed documentation run for a system")
	wantClause(t, p, "when", "any architecture/subsystem/module doc is stored")
	wantClause(t, p, "then", "the doc row carries the structured list of sources")
	// An `And` continues the clause it follows — dropping it would cut two
	// thirds of the observable outcome off this scenario.
	wantClause(t, p, "then", "the content hash of the generation is stamped")
	wantClause(t, p, "then", "no citation is stripped from persistence")
	wantNoStatement(t, p)
	if title, _ := p["title"].(string); title != "a generated doc knows its sources" {
		t.Fatalf("title = %q, want the declaration's title kept rather than dropped", title)
	}
}

// The same notation unfenced, with a `Sources:` line under each clause: the
// annotation belongs to the clause above it and travels with it.
func TestClauseAnnotationsTravelWithTheirClause(t *testing.T) {
	record := joinLines(
		"# EPIC-ANALYSIS-001 — Ingest system through local harness",
		"",
		"## Acceptance scenarios",
		"",
		"### SCN-ANALYSIS-001 - Start full core analysis through BFF and core REST APIs",
		"",
		"```gherkin",
		"Scenario: Start full core analysis through BFF and core REST APIs",
		"  Given the React frontend, BFF, and modernpath-core services are running",
		"    Sources: SRC-ANALYSIS-002, SRC-ANALYSIS-003",
		"  And a readable local folder contains a small software system",
		"    Sources: SRC-ANALYSIS-001, SRC-ANALYSIS-004",
		"  When the developer starts analysis from the React frontend",
		"    Sources: SRC-ANALYSIS-021, SRC-ANALYSIS-022",
		"  Then the BFF uses modernpath-core REST APIs to create or identify the system",
		"    Sources: SRC-ANALYSIS-022, SRC-ANALYSIS-024",
		"```",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-ANALYSIS-001", Record: "epics/EPIC-ANALYSIS-001-ingest-system-through-local-harness/EPIC.md"},
		record, "EPIC-ANALYSIS-001#SCN-ANALYSIS-001")

	wantClause(t, p, "given", "the React frontend, BFF, and modernpath-core services are running")
	wantClause(t, p, "given", "SRC-ANALYSIS-002")
	wantClause(t, p, "given", "a readable local folder contains a small software system")
	wantClause(t, p, "when", "the developer starts analysis from the React frontend")
	wantClause(t, p, "then", "the BFF uses modernpath-core REST APIs")
	wantClause(t, p, "then", "SRC-ANALYSIS-024")
	wantNoStatement(t, p)
	if g, _ := p["given"].(string); strings.Contains(g, "Scenario:") {
		t.Fatalf("the Gherkin declaration line is not clause content: %q", g)
	}
}

// A heading block whose body is one hard-wrapped sentence in the same triple,
// lower-cased at the joints. The heading's title is not part of the GIVEN.
func TestHardWrappedProseTripleSplits(t *testing.T) {
	record := joinLines(
		"# EPIC-CLI-004 — RDD store landing",
		"",
		"## Acceptance scenarios",
		"",
		"### SCN-CLI004-001 — Lossless fallback",
		"",
		"Given any retired process artifact, including Markdown, YAML, task records, or",
		"an unrecognized auxiliary file, when transition import runs, then its exact",
		"bytes, path, hash, size, media type, and archive revision have a System-scoped",
		"database home before parsing.",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-CLI-004", Record: "epics/EPIC-CLI-004-rdd-store-landing/EPIC.md"},
		record, "EPIC-CLI-004#SCN-CLI004-001")

	wantClause(t, p, "given", "any retired process artifact")
	wantClause(t, p, "given", "an unrecognized auxiliary file")
	wantClause(t, p, "when", "transition import runs")
	wantClause(t, p, "then", "its exact")
	wantClause(t, p, "then", "database home before parsing.")
	wantNoStatement(t, p)
	if title, _ := p["title"].(string); title != "Lossless fallback" {
		t.Fatalf("title = %q, want the heading's title kept", title)
	}
	if g, _ := p["given"].(string); strings.Contains(g, "Lossless fallback") {
		t.Fatalf("the heading title is not part of the GIVEN clause: %q", g)
	}
}

// A table cell writing the triple title-cased and comma-separated.
func TestTitleCaseTableCellTripleSplits(t *testing.T) {
	record := joinLines(
		"# EPIC-CLI-001 — CLI surface truth",
		"",
		"## Acceptance scenarios",
		"",
		"| Scenario | Given / When / Then | Realizes |",
		"|---|---|---|",
		"| SCN-CLI-001 | Given a bound workspace, When `work new \"<desc>\"`, Then an epic is created on the server and selected locally | REQ-CROSS-205 |",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-CLI-001", Record: "epics/EPIC-CLI-001-cli-surface-truth.md"},
		record, "EPIC-CLI-001#SCN-CLI-001")

	wantClause(t, p, "given", "a bound workspace")
	wantClause(t, p, "when", "`work new \"<desc>\"`")
	wantClause(t, p, "then", "an epic is created on the server and selected locally")
	wantNoStatement(t, p)
}

// When the header names no text column, the scenario cell is the one shaped
// like a scenario — not whichever cell is longest. An evidence note longer than
// the clause triple stored the note as the scenario and left the triple in the
// source text only.
func TestScenarioCellIsChosenByShapeNotLength(t *testing.T) {
	record := joinLines(
		"# EPIC-MC-003 — All epics visible",
		"",
		"## BDD acceptance scenarios",
		"",
		"| SCN | Given / When / Then | Upper RED | Status |",
		"|---|---|---|---|",
		"| SCN-MC3-003 | GIVEN a direct browser navigation or reload on `/epics` WHEN Mission Control serves the request THEN the Epics face remains selected and readable at narrow width. | Same RED run: `/epics` was absent from both client and server route tables. A later live-browser check exposed the desktop rail overriding its narrow hide rule; the new regression test failed before the CSS order was corrected. | UPPER_VALIDATED |",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-MC-003", Record: "epics/EPIC-MC-003-all-epics-visible/EPIC.md"},
		record, "EPIC-MC-003#SCN-MC3-003")

	wantClause(t, p, "given", "a direct browser navigation or reload on `/epics`")
	wantClause(t, p, "when", "Mission Control serves the request")
	wantClause(t, p, "then", "the Epics face remains selected")
	wantNoStatement(t, p)
}

// An abbreviation is not a sentence end. This triple is the canonical uppercase
// form and only abbreviates a word inside its GIVEN; refusing it there sent a
// fully-declared scenario to the statement column.
func TestAnAbbreviationDoesNotBlockTheSplit(t *testing.T) {
	record := joinLines(
		"# EPIC-EVAL-005 — Use-case model benchmark",
		"",
		"## Acceptance scenarios",
		"",
		"| SCN | Given / When / Then |",
		"|---|---|",
		"| SCN-EVAL-107 | GIVEN the accumulated claim set incl. injected contradictions WHEN the Class B pass runs (strongest model) THEN it emits **candidate flags only** (non-gating). |",
		"",
	)
	p := scenarioPayload(t, Epic{ID: "EPIC-EVAL-005", Record: "epics/EPIC-EVAL-005-use-case-model-benchmark.md"},
		record, "EPIC-EVAL-005#SCN-EVAL-107")

	wantClause(t, p, "given", "the accumulated claim set incl. injected contradictions")
	wantClause(t, p, "when", "the Class B pass runs (strongest model)")
	wantClause(t, p, "then", "candidate flags only")
	wantNoStatement(t, p)
}

// The negative side: a scenario the corpus writes as one prose sentence has no
// triple to find and must stay a statement. Widening the matcher may not invent
// clauses out of ordinary prose.
func TestProseScenarioStaysAStatement(t *testing.T) {
	record := joinLines(
		"# EPIC-PORTFOLIO-042 — Task context",
		"",
		"## Acceptance scenarios",
		"",
		"| SCN | Scenario | RED | Evidence | Status |",
		"|---|---|---|---|---|",
		"| SCN-TC-003 | An unknown type or a missing id is a clean **404**. | RED | core 4/4 | UPPER_VALIDATED |",
		"| SCN-TC-004 | The resolver was given a stale id. The page then renders the empty state. | RED | core 2/2 | UPPER_VALIDATED |",
		"",
	)
	epic := Epic{ID: "EPIC-PORTFOLIO-042", Record: "epics/EPIC-PORTFOLIO-042-task-context.md"}

	p := scenarioPayload(t, epic, record, "EPIC-PORTFOLIO-042#SCN-TC-003")
	if s, _ := p["statement"].(string); !strings.Contains(s, "clean **404**") {
		t.Fatalf("statement = %q, want the prose kept", s)
	}
	if g, _ := p["given"].(string); g != "" {
		t.Fatalf("prose with no triple invents no GIVEN, got %q", g)
	}

	// Two sentences that merely use the words are not a clause triple.
	q := scenarioPayload(t, epic, record, "EPIC-PORTFOLIO-042#SCN-TC-004")
	if g, _ := q["given"].(string); g != "" {
		t.Fatalf("a sentence break separates prose from a triple, got given %q", g)
	}
	if s, _ := q["statement"].(string); !strings.Contains(s, "stale id") {
		t.Fatalf("statement = %q, want the prose kept", s)
	}
}
