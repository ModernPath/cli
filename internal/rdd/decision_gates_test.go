package rdd

// REQ-CROSS-247 (EPIC-CLI-003 T11): every corpus D-* decision imports as one
// decision gate — answered and applied, answer the decision text, consequence
// carried, sources resolved, exact scope the owning epic and named items.
// Gate ids are minted epic-scoped from the verbatim local suffix.

import (
	"strings"
	"testing"
)

func decisionOpsOf(t *testing.T, record string) []Op {
	t.Helper()
	return BuildDecisionGateOps(Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001-only.md"}, record)
}

func payloadOf(t *testing.T, ops []Op, externalID string) map[string]any {
	t.Helper()
	for _, op := range ops {
		if op.Payload["external_id"] == externalID {
			return op.Payload
		}
	}
	t.Fatalf("no op with external_id %q in %v", externalID, opIDs(ops))
	return nil
}

func opIDs(ops []Op) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i], _ = op.Payload["external_id"].(string)
	}
	return out
}

// The dominant table shape: Decision id | Decision | Source | Consequence.
func TestDecisionTableRowBecomesAnAnsweredGate(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| Decision id | Decision | Source | Consequence |
|---|---|---|---|
| D-CV-1 | The store is authoritative. | USER:2026-08-01 | Files retire after import; REQ-CV-001 re-verifies. |
`
	ops := decisionOpsOf(t, record)
	if len(ops) != 1 {
		t.Fatalf("ops = %d, want 1", len(ops))
	}
	p := payloadOf(t, ops, "D-CV-001-CV-1")
	if p["kind"] != "decision" || p["state"] != "answered" || p["applied_state"] != "applied" {
		t.Fatalf("gate shape wrong: kind=%v state=%v applied=%v", p["kind"], p["state"], p["applied_state"])
	}
	if p["answer"] != "The store is authoritative." {
		t.Fatalf("answer = %v", p["answer"])
	}
	if body, _ := p["body_md"].(string); !strings.Contains(body, "Files retire after import") {
		t.Fatalf("consequence not carried in body: %q", body)
	}
	if p["answered_at"] != "2026-08-01T00:00:00.000000Z" {
		t.Fatalf("answered_at = %v, want the USER tag date", p["answered_at"])
	}
	if p["source_tag"] != "USER:2026-08-01" {
		t.Fatalf("source_tag = %v", p["source_tag"])
	}
	scope, _ := p["exact_scope"].([]any)
	joined := strings.Join(anyStrings(scope), " ")
	if !strings.Contains(joined, "EPIC-CV-001") || !strings.Contains(joined, "REQ-CV-001") {
		t.Fatalf("exact_scope must name the owning epic and named items: %v", scope)
	}
	sources, _ := p["sources"].([]any)
	if len(sources) == 0 {
		t.Fatalf("sources must carry the resolved USER tag")
	}
}

// Header variants: ID | Decision | Source, and Source before Consequence.
func TestDecisionHeaderVariantsAllParse(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-CV-1 | First form. | USER:2026-08-01 |

## Design decisions

| id | Decision | Source/status |
|---|---|---|
| D-CV-2 | Second form. | USER:2026-08-02 |
`
	ops := decisionOpsOf(t, record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2 across both section and header forms: %v", len(ops), opIDs(ops))
	}
	if p := payloadOf(t, ops, "D-CV-001-CV-2"); p["answer"] != "Second form." {
		t.Fatalf("variant answer = %v", p["answer"])
	}
}

// The corpus's other live header families: Question|Resolution (the answer is
// the resolution, the question rides as context) and Kind|Statement.
func TestDecisionResolutionAndStatementHeaders(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions (all resolved USER:2026-08-06)

| ID | Question | Resolution | Source |
|---|---|---|---|
| D-CV-6 | Brief generation site? | Both, workspace-first. | USER:2026-08-06 |

## Decision register

| ID | Kind | Statement | Source/trigger | Consequence/status |
|---|---|---|---|---|
| D-CV-7 | scope | Ingestion stays readiness-gated. | USER:2026-08-07 | REQ-CV-001 re-verifies |
`
	ops := decisionOpsOf(t, record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2: %v", len(ops), opIDs(ops))
	}
	p := payloadOf(t, ops, "D-CV-001-CV-6")
	if p["answer"] != "Both, workspace-first." {
		t.Fatalf("resolution must be the answer: %v", p["answer"])
	}
	if body, _ := p["body_md"].(string); !strings.Contains(body, "Brief generation site?") {
		t.Fatalf("the question must ride as context: %q", body)
	}
	p = payloadOf(t, ops, "D-CV-001-CV-7")
	if p["answer"] != "Ingestion stays readiness-gated." {
		t.Fatalf("statement must be the answer: %v", p["answer"])
	}
	if body, _ := p["body_md"].(string); !strings.Contains(body, "REQ-CV-001 re-verifies") {
		t.Fatalf("consequence/status must be carried: %q", body)
	}
}

// Prose/bullet declarations count too, and a letter-suffixed family keeps its
// verbatim suffix so siblings never collide.
func TestBulletAndLetterSuffixDecisions(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decision summary

- **D-061a** — Split the payload. USER:2026-08-03
- **D-061b** — Keep the seams. USER:2026-08-03
`
	ops := decisionOpsOf(t, record)
	if len(ops) != 2 {
		t.Fatalf("ops = %d, want 2: %v", len(ops), opIDs(ops))
	}
	payloadOf(t, ops, "D-CV-001-061a")
	payloadOf(t, ops, "D-CV-001-061b")
}

// A ratified row carrying both a register id (DEC-*) and a local D-* mints
// from the local id; the register id lands in sources.
func TestDualIdentityRowMintsFromTheLocalId(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-CV-3 | Ratified as DEC-SYNC-061. | DEC-SYNC-061 · USER:2026-08-04 |
`
	ops := decisionOpsOf(t, record)
	p := payloadOf(t, ops, "D-CV-001-CV-3")
	sources, _ := p["sources"].([]any)
	joined := strings.Join(sourceRefs(sources), " ")
	if !strings.Contains(joined, "DEC-SYNC-061") {
		t.Fatalf("register id must land in sources: %v", sources)
	}
}

// A SRC-* reference resolves through the record's own source register to the
// citations the register entry names.
func TestSrcReferenceResolvesThroughTheRegister(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Sources and observed facts

| ID | Fact | Where |
|---|---|---|
| SRC-CV-001 | The schema exists. | CODE:apps/storage/lib/storage/schema/epic.ex |

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-CV-4 | Built on the schema. | SRC-CV-001 |
`
	ops := decisionOpsOf(t, record)
	p := payloadOf(t, ops, "D-CV-001-CV-4")
	sources, _ := p["sources"].([]any)
	joined := strings.Join(sourceRefs(sources), " ")
	if !strings.Contains(joined, "apps/storage/lib/storage/schema/epic.ex") {
		t.Fatalf("SRC ref must resolve to the register entry's citation: %v", sources)
	}
}

// Negative: a D-* id merely CITED outside a decision-family section declares
// nothing, and a tagless decision keeps the DOC: source with no invented date.
func TestDecisionNegatives(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Grounded facts

D-CV-9 is discussed here but declared nowhere.

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-CV-5 | Undated but decided. | the review round |
`
	ops := decisionOpsOf(t, record)
	if len(ops) != 1 {
		t.Fatalf("a citation outside a decision section declared a gate: %v", opIDs(ops))
	}
	p := payloadOf(t, ops, "D-CV-001-CV-5")
	if tag, _ := p["source_tag"].(string); !strings.HasPrefix(tag, "DOC:") {
		t.Fatalf("tagless decision must carry a DOC: source, got %v", tag)
	}
}

// The batch integration: BuildOps emits the decision gates and no two records
// using the same bare local number collide.
func TestBuildOpsEmitsEpicScopedDecisionGates(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-1 | First epic's decision. | USER:2026-08-01 |
`)
	writeFixtureFile(t, root, "epics/EPIC-CV-002-second.md", `# EPIC-CV-002 — Second

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-1 | Second epic's decision. | USER:2026-08-02 |
`)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n"+
		"| EPIC-CV-002 | [record](epics/EPIC-CV-002-second.md) | — | — | — | — | — | — | PROPOSED | — | — |\n")

	r := reportOf(t, root)
	g := countGroup(t, r, homeDecisionsGroup)
	if g.Rows != 2 || g.Ops != 2 {
		t.Fatalf("decision home = %d/%d, want 2/2 once the emitter exists", g.Rows, g.Ops)
	}
}

func anyStrings(items []any) []string {
	out := make([]string, 0, len(items))
	for _, raw := range items {
		if s, ok := raw.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sourceRefs(items []any) []string {
	out := make([]string, 0, len(items))
	for _, raw := range items {
		if m, ok := raw.(map[string]any); ok {
			if s, ok := m["ref"].(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// REQ-CROSS-247: a decision declared with a REGISTER id (DEC-*) and no local
// D-* mints a gate like any other. Both declaration matchers required a
// literal `D-` prefix, so this whole family produced nothing: measured on the
// corpus, 281 DEC-* declarations across 63 epics and zero `DEC-` gates among
// the 577 imported.
//
// The id is minted epic-scoped, not verbatim. DEC ids read as globally unique
// register ids but are not: seven collide across epic pairs — DEC-SD-001 in
// both EPIC-PORTFOLIO-007 and -046, DEC-TM-001/002, DEC-RA-001/002. Minting
// verbatim would let the second declaration overwrite the first, silently.
// The verbatim register id is preserved in sources and in the body so it stays
// findable once the corpus files retire.
func TestRegisterOnlyDecisionMintsAnEpicScopedGate(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| Decision id | Decision | Source | Consequence |
|---|---|---|---|
| DEC-SD-001 | The API serves the option lists. | ` + "`USER:2026-08-11`" + ` | The client renders what it is given. |
| ` + "`DEC-SD-002`" + ` | The preview asks the server on each change. | ` + "`USER:2026-08-11`" + ` | Cheap enough to keep honest. |

- **DEC-SD-003** — Control measures ride the rationale field.
`
	ops := decisionOpsOf(t, record)
	for _, want := range []string{"D-CV-001-SD-001", "D-CV-001-SD-002", "D-CV-001-SD-003"} {
		p := payloadOf(t, ops, want)
		if p["kind"] != "decision" || p["state"] != "answered" {
			t.Errorf("%s: kind=%v state=%v, want an answered decision", want, p["kind"], p["state"])
		}
	}

	// The register id must survive — after the flip it is the only way back to
	// the corpus declaration.
	p := payloadOf(t, ops, "D-CV-001-SD-001")
	sources, _ := p["sources"].([]any)
	if !strings.Contains(strings.Join(sourceRefs(sources), " "), "DEC-SD-001") {
		t.Errorf("the verbatim register id was dropped from sources: %v", p["sources"])
	}
	if body, _ := p["body_md"].(string); !strings.Contains(body, "DEC-SD-001") {
		t.Errorf("the body must name the declaration it came from: %q", body)
	}

	// Two epics declaring the same register id must not collapse onto one gate.
	other := BuildDecisionGateOps(Epic{ID: "EPIC-CV-002", Record: "epics/EPIC-CV-002-other.md"}, record)
	if payloadOf(t, other, "D-CV-002-SD-001") == nil {
		t.Fatal("a second epic's declaration of the same register id must mint its own gate")
	}
}

// Minting strips ONE id prefix, never two.
//
// The recognizing regex accepts `(?:DEC|D)-[A-Za-z0-9]+...`, so a register id
// whose area code is the single letter D — `DEC-D-001` — is a legal
// declaration. Chaining TrimPrefix("DEC-") into TrimPrefix("D-") removes both,
// which does two things wrong at once: it collides `DEC-D-001` with `DEC-001`
// in the same epic, silently overwriting one gate with the other — the exact
// hazard epic-scoped minting exists to prevent — and it diverges from the node
// mirror, whose single anchored alternation strips one prefix only. The corpus
// has no single-letter area code today, so cross-builder parity cannot see it.
func TestMintingStripsOneIdPrefixNotTwo(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| Decision id | Decision | Source | Consequence |
|---|---|---|---|
| DEC-001 | The plain register id. | ` + "`USER:2026-08-11`" + ` | One gate. |
| DEC-D-001 | The register id whose area code is D. | ` + "`USER:2026-08-11`" + ` | A different gate. |
`
	ops := decisionOpsOf(t, record)
	ids := opIDs(ops)
	if len(ops) != 2 {
		t.Fatalf("two declarations must mint two gates, got %d: %v", len(ops), ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("the two declarations collapsed onto one id: %v", ids)
	}
	payloadOf(t, ops, "D-CV-001-001")
	payloadOf(t, ops, "D-CV-001-D-001")
}

// The decision text is never the id.
//
// A decision table whose header understates its columns — `| Decision |
// Consequence |` above three-cell rows, a shape the corpus uses — makes
// cellByName match "decision" at index 0, which is the ID cell. The gate then
// takes its title from the id and labels the real decision "Consequence",
// shifting every column by one.
//
// Pre-existing, but it only became visible at scale when register-only
// declarations started minting: 132 of the 288 new gates titled themselves
// with their own register id. Nothing is lost — the text still rides the body
// — but a gate whose title is its id tells a reader nothing.
func TestDecisionTextIsNeverTheIdCell(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions
| Decision | Consequence |
|---|---|
| DEC-RK-001 | The API serves the option lists, not the raw level maps. | One source for the copy. |
`
	p := payloadOf(t, decisionOpsOf(t, record), "D-CV-001-RK-001")
	title, _ := p["title"].(string)
	if strings.HasPrefix(title, "DEC-") || title == "DEC-RK-001" {
		t.Errorf("title = %q, want the decision text — the id is not a decision", title)
	}
	if !strings.Contains(title, "The API serves the option lists") {
		t.Errorf("title = %q, want the decision the row states", title)
	}
	body, _ := p["body_md"].(string)
	if strings.Contains(body, "Decision — DEC-RK-001") {
		t.Errorf("the body labelled the id as the decision:\n%s", body)
	}
	if !strings.Contains(body, "Decision — The API serves") {
		t.Errorf("the body must label the real decision:\n%s", body)
	}

	// The well-formed header keeps working exactly as before.
	ok := `# EPIC-CV-002 — Only

## Decisions
| Decision id | Decision | Source | Consequence |
|---|---|---|---|
| D-CV-2 | The store is authoritative. | ` + "`USER:2026-08-01`" + ` | Files retire. |
`
	p2 := payloadOf(t, BuildDecisionGateOps(Epic{ID: "EPIC-CV-002", Record: "epics/b.md"}, ok), "D-CV-002-CV-2")
	if got, _ := p2["title"].(string); got != "The store is authoritative." {
		t.Errorf("well-formed header regressed: title = %q", got)
	}
}

// The decisions landing-home must count the DEC-* family too.
//
// The builder now mints a gate per register-only declaration, but this group's
// denominator still matched `D-` alone, so it read 143/143 — 100% — while 288
// gates came from declarations it never counted. A coverage number that cannot
// see the population it covers is the exact instrument failure this whole
// report exists to prevent: if DEC-* handling broke, the group would still say
// 100%.
//
// A denominator must move when the builder's reach moves, or it measures the
// parser against itself.
func TestDecisionHomeCountsTheRegisterFamily(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the decision denominator honest.

## Decisions

| Decision id | Decision | Source |
|---|---|---|
| D-1 | A local decision. | USER:2026-08-01 |
| DEC-CV-001 | A register decision. | USER:2026-08-02 |

- **DEC-CV-002** — a register decision declared as a bullet.
`)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n")

	g := countGroup(t, reportOf(t, root), homeDecisionsGroup)
	if g.Rows != 3 {
		t.Errorf("denominator = %d, want 3 — the two register declarations are declarations too", g.Rows)
	}
	if g.Ops != g.Rows {
		t.Errorf("home = %d/%d — every counted declaration should have minted a gate", g.Ops, g.Rows)
	}
}

// An id cell may carry a parenthetical note and still declare a decision.
//
// Found the moment the coverage denominator was widened to the DEC-* family:
// it counted 432 declarations against 431 gates, and the one difference is
// `| DEC-AD-001 (REVISED in rework) | …` in EPIC-PORTFOLIO-003. The
// denominator matches an id-led cell by prefix; the builder required the whole
// cell to be nothing but the id, so it read the row as not a declaration and
// the decision minted nothing.
//
// Tolerate a trailing parenthetical only. Arbitrary trailing prose would let a
// sentence that merely mentions an id mint a gate; a parenthetical is how this
// corpus annotates a declaration it is revising.
func TestAnnotatedIdCellStillDeclares(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Decisions

| Decision id | Decision | Source |
|---|---|---|
| DEC-AD-001 (REVISED in rework) | Two endpoints: tree and detail. | ` + "`USER:2026-08-11`" + ` |
| ` + "`D-CV-2`" + ` (superseded) | The plain one. | ` + "`USER:2026-08-11`" + ` |
`
	ops := decisionOpsOf(t, record)
	p := payloadOf(t, ops, "D-CV-001-AD-001")
	if title, _ := p["title"].(string); !strings.Contains(title, "Two endpoints") {
		t.Errorf("title = %q, want the decision text", title)
	}
	payloadOf(t, ops, "D-CV-001-CV-2")

	// A cell that merely mentions an id in prose is not a declaration.
	prose := `# EPIC-CV-002 — Only

## Decisions

| Decision id | Decision | Source |
|---|---|---|
| D-CV-9 is superseded by the rework | not a declaration | ` + "`USER:2026-08-11`" + ` |
`
	if got := BuildDecisionGateOps(Epic{ID: "EPIC-CV-002", Record: "epics/b.md"}, prose); len(got) != 0 {
		t.Errorf("prose mentioning an id minted %v", opIDs(got))
	}
}

// A truncated title says so.
//
// The title was cut at 200 units with no mark, mid-word and sometimes
// mid-markup: 145 of 463 decision gates sit at exactly 200 characters, and a
// reader cannot tell a cut title from a short one. `decision_gates.title` is
// unbounded text and the contract sets no limit, so 200 is this builder's
// display choice — a fair one, a 600-character "title" is not a title. It just
// has to be honest, the way the OQ body cap already is. The full text stays in
// body_md either way.
func TestALongDecisionTitleIsMarkedAsCut(t *testing.T) {
	long := "Serve the option lists from the API " + strings.Repeat("and keep the copy in one place ", 12)
	record := "# EPIC-CV-030 — Only\n\n## Decisions\n\n" +
		"| Decision id | Decision | Source |\n|---|---|---|\n" +
		"| D-1 | " + long + " | `USER:2026-08-01` |\n"
	p := payloadOf(t, BuildDecisionGateOps(Epic{ID: "EPIC-CV-030", Record: "epics/a.md"}, record), "D-CV-030-1")

	title, _ := p["title"].(string)
	if !strings.HasSuffix(title, "…") {
		t.Errorf("a cut title must show it was cut\n  got: %q", title[max(0, len(title)-60):])
	}
	if strings.HasSuffix(strings.TrimSuffix(title, "…"), " ") {
		t.Errorf("cut at a word boundary, not mid-space: %q", title)
	}
	if utf16Len(title) > 200 {
		t.Errorf("title is %d units, cap is 200", utf16Len(title))
	}
	// The whole decision still reaches the reader through the body.
	if body, _ := p["body_md"].(string); !strings.Contains(body, strings.TrimSpace(long)) {
		t.Error("the body must keep the decision in full")
	}

	// A decision that fits is untouched — no ellipsis, no hash churn.
	short := "# EPIC-CV-031 — Only\n\n## Decisions\n\n" +
		"| Decision id | Decision | Source |\n|---|---|---|\n" +
		"| D-1 | The store is authoritative. | `USER:2026-08-01` |\n"
	q := payloadOf(t, BuildDecisionGateOps(Epic{ID: "EPIC-CV-031", Record: "epics/b.md"}, short), "D-CV-031-1")
	if got, _ := q["title"].(string); got != "The store is authoritative." {
		t.Errorf("a short title was altered: %q", got)
	}
}

// A bullet-declared decision does not cite itself as its own source.
//
// The bullet parser set `source` to the bullet's own text, so every one of the
// 30 bullet-declared gates rendered `Source — <the decision, verbatim>` under
// `Decision — <the same text>`. Table rows never did this: they leave the
// Source line out when the table has no source column, which is the honest
// shape. The bullet text still feeds source-token extraction — that is what it
// was for — it just is not rendered as a citation of itself.
func TestBulletDecisionDoesNotCiteItself(t *testing.T) {
	record := "# EPIC-CV-032 — Only\n\n## Decisions\n\n" +
		"- **DEC-EVAL-008** — Reviewers run independently, and disagreement is signal `USER:2026-08-04`.\n"
	p := payloadOf(t, BuildDecisionGateOps(Epic{ID: "EPIC-CV-032", Record: "epics/c.md"}, record), "D-CV-032-EVAL-008")
	body, _ := p["body_md"].(string)
	if strings.Contains(body, "Source — Reviewers run independently") {
		t.Errorf("the bullet cited itself as its own source:\n%s", body)
	}
	if !strings.Contains(body, "Decision — Reviewers run independently") {
		t.Errorf("the decision itself must still be stated:\n%s", body)
	}
	// The tokens the bullet carries are still resolved into sources.
	if refs := strings.Join(sourceRefs(p["sources"].([]any)), " "); !strings.Contains(refs, "USER:2026-08-04") {
		t.Errorf("the bullet's own USER tag must still reach sources: %v", p["sources"])
	}
}

// An escaped pipe is content, not a column break.
//
// `decisionCells` split on every `|`, so a cell writing a literal
// `` (`"snapshot"` \| `"live"`) `` broke in two: the decision truncated at the
// escape and its tail was rendered as the Source. Two rows in the corpus are
// corrupted this way — EPIC-PORTFOLIO-023 DEC-VB-001 and EPIC-PORTFOLIO-024
// DEC-HB-003 — and both read as complete, which is what makes it worth fixing
// rather than tolerating.
func TestEscapedPipeIsContentNotAColumnBreak(t *testing.T) {
	record := "# EPIC-CV-033 — Only\n\n## Decisions\n\n" +
		"| Decision id | Decision | Consequence |\n|---|---|---|\n" +
		"| DEC-VB-001 | Serve `source` (`\"snapshot\"` \\| `\"live\"`) explicitly. | The inference would be wrong. |\n"
	p := payloadOf(t, BuildDecisionGateOps(Epic{ID: "EPIC-CV-033", Record: "epics/d.md"}, record), "D-CV-033-VB-001")
	title, _ := p["title"].(string)
	if !strings.Contains(title, "explicitly") {
		t.Errorf("the decision was cut at the escaped pipe\n  got: %q", title)
	}
	body, _ := p["body_md"].(string)
	if strings.Contains(body, "Source — ") {
		t.Errorf("the decision's tail was rendered as a source:\n%s", body)
	}
	if !strings.Contains(body, "Consequence — The inference would be wrong") {
		t.Errorf("the consequence shifted out of place:\n%s", body)
	}
}
