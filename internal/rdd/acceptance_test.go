package rdd

// REQ-CROSS-248 (EPIC-CLI-003 T11): recorded epic acceptances import
// attributable — WORKLIST's Human-approval column and the record's approval
// registers land as acceptance gates with their USER: tags and the named
// deciding human, the completion acceptance feeds the epic's single approval
// quartet, and a pending register row asserts nothing.

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

const acceptanceRecord = `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.

## Human approval

| Approval id | Approver | Role | Source | Scope | Decision |
|---|---|---|---|---|---|
| APP-CV-001 | Pasi | owner | ` + "`USER:2026-08-05`" + ` (build sanction) | EPIC-CV-001 | approved |
| APP-CV-002 | Mattias | product owner | USER:2026-08-03:spec-round | SCN-CV-001, REQ-CV-001 | specification approved; implementation authorized |
| APP-CV-003 | Ivan | reviewer | — | EPIC-CV-001 | (pending) |
`

func acceptanceOps(t *testing.T) []Op {
	t.Helper()
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", acceptanceRecord)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | DONE | APP-CV-001 (approved `USER:2026-08-05`) | — |\n"+
		"| EPIC-CV-002 | — | — | — | — | — | — | — | DONE | approved `USER:2026-07-16` | — |\n")
	data, _ := Snapshot(root, manifest.Default())
	return BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
}

func gateOpPayload(ops []Op, id string) map[string]any {
	for _, op := range ops {
		if op.Type == "upsert_gate" && op.Payload["external_id"] == id {
			return op.Payload
		}
	}
	return nil
}

func epicOpPayload(ops []Op, id string) map[string]any {
	for _, op := range ops {
		if op.Type == "upsert_epic" && op.Payload["external_id"] == id {
			return op.Payload
		}
	}
	return nil
}

func TestRegisterRowsBecomeAttributableAcceptanceGates(t *testing.T) {
	ops := acceptanceOps(t)

	g := gateOpPayload(ops, "APP-CV-001")
	if g == nil {
		t.Fatalf("register acceptance APP-CV-001 built no gate")
	}
	if g["kind"] != "approval_request" || g["state"] != "answered" {
		t.Fatalf("gate shape: kind=%v state=%v", g["kind"], g["state"])
	}
	if g["answered_at"] != "2026-08-05T00:00:00.000000Z" {
		t.Fatalf("answered_at = %v, want the row's USER date", g["answered_at"])
	}
	sources, _ := g["sources"].([]any)
	joined := strings.Join(sourceRefs(sources), " ")
	if !strings.Contains(joined, "Pasi") {
		t.Fatalf("the deciding human must land in sources: %v", sources)
	}
	answerer, _ := g["answerer"].(map[string]any)
	if answerer["kind"] != "human" || answerer["name"] != "Pasi" {
		t.Fatalf("answerer = %v, want the named deciding human", answerer)
	}

	// the specification acceptance is its own gate with its own scope
	g2 := gateOpPayload(ops, "APP-CV-002")
	if g2 == nil {
		t.Fatalf("specification acceptance built no gate")
	}
	scope, _ := g2["exact_scope"].([]any)
	if !strings.Contains(strings.Join(anyStrings(scope), " "), "SCN-CV-001") {
		t.Fatalf("scope cell ids must ride exact_scope: %v", scope)
	}

	// a pending row asserts nothing
	if g3 := gateOpPayload(ops, "APP-CV-003"); g3 != nil {
		t.Fatalf("a pending register row must build no acceptance gate")
	}
}

func TestApproverCellTagStillResolvesTheHumanAndDatesTheGate(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Decision |
|---|---|---|
| APP-CV-009 | Pasi (` + "`USER:2026-07-21`" + `) | approved |
`
	ops, _ := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001", Record: "epics/EPIC-CV-001-only.md"}, record)
	g := gateOpPayload(ops, "APP-CV-009")
	answerer, _ := g["answerer"].(map[string]any)
	if answerer["name"] != "Pasi" {
		t.Fatalf("answerer = %v, want the clean provisioned-user name", answerer)
	}
	if g["source_tag"] != "USER:2026-07-21" || g["answered_at"] != "2026-07-21T00:00:00.000000Z" {
		t.Fatalf("gate date/source = %v/%v", g["answered_at"], g["source_tag"])
	}
}

func TestCompactRegisterTagsStillAttributeTheAcceptance(t *testing.T) {
	tests := []struct {
		name   string
		record string
		id     string
		date   string
	}{
		{
			name: "decision cell",
			record: `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Decision |
|---|---|---|
| APP-CV-010 | Pasi | **approved ` + "`USER:2026-08-15`" + `** — reviewed in the product shell |
`,
			id:   "APP-CV-010",
			date: "USER:2026-08-15",
		},
		{
			name: "role cell",
			record: `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Role | Decision |
|---|---|---|---|
| APP-CV-011 | Mattias | product owner ` + "`USER:2026-07-09`" + ` | approved |
`,
			id:   "APP-CV-011",
			date: "USER:2026-07-09",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ops, quartet := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001"}, tt.record)
			g := gateOpPayload(ops, tt.id)
			if g == nil {
				t.Fatalf("%s built no gate", tt.id)
			}
			if g["source_tag"] != tt.date {
				t.Fatalf("source_tag = %v, want %s", g["source_tag"], tt.date)
			}
			answerer, _ := g["answerer"].(map[string]any)
			if answerer["name"] == nil {
				t.Fatalf("answerer must retain the named deciding human: %v", answerer)
			}
			if quartet == nil || quartet["source_tag"] != tt.date || quartet["approver_name"] != answerer["name"] {
				t.Fatalf("quartet must carry the same attributable acceptance: %v", quartet)
			}
		})
	}
}

func TestAcceptanceUsesTheLatestUserTagOnTheCompletionRow(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Role | Source | Scope | Decision |
|---|---|---|---|---|---|
| APP-CV-012 | Pasi | owner | ` + "`USER:2026-08-05`" + ` (build sanction) · ` + "`USER:2026-08-06`" + ` (completion) | EPIC-CV-001 | approved |
`
	ops, quartet := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001"}, record)
	g := gateOpPayload(ops, "APP-CV-012")
	if g["source_tag"] != "USER:2026-08-06" {
		t.Fatalf("gate source_tag = %v, want the latest completion tag", g["source_tag"])
	}
	if quartet == nil || quartet["source_tag"] != "USER:2026-08-06" {
		t.Fatalf("quartet must use the latest completion tag: %v", quartet)
	}
}

func TestLatestCompletionRegisterRowFeedsTheQuartet(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Source | Decision |
|---|---|---|---|
| APP-CV-013 | Pasi | ` + "`USER:2026-08-05`" + ` | approved |
| APP-CV-014 | Mattias | ` + "`USER:2026-08-06`" + ` | approved after the correction |
`
	_, quartet := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001"}, record)
	if quartet == nil || quartet["source_tag"] != "USER:2026-08-06" || quartet["approver_name"] != "Mattias" {
		t.Fatalf("quartet = %v, want the latest completion acceptance", quartet)
	}
}

func TestSpecificationAcceptanceNeverFeedsTheCompletionQuartet(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Source | Decision |
|---|---|---|---|
| APP-CV-020 | Pasi | ` + "`USER:2026-08-05`" + ` | specification approved; implementation may start |
| APPROVE-EPIC-CV-001 | Pasi | ` + "`USER:2026-08-06`" + ` | approved |
`
	ops, quartet := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001"}, record)
	if gateOpPayload(ops, "APP-CV-020") == nil || gateOpPayload(ops, "APPROVE-EPIC-CV-001") == nil {
		t.Fatalf("specification and completion acceptances must both remain gates")
	}
	if quartet == nil || quartet["source_tag"] != "USER:2026-08-06" {
		t.Fatalf("completion quartet was fed by the specification acceptance: %v", quartet)
	}

	op, worklistQuartet, ok := BuildWorklistAcceptanceGate(Epic{
		ID:           "EPIC-CV-002",
		ApprovalCell: "SPEC-APPROVED `USER:2026-08-20` — implementation may start",
	})
	if !ok || op.Payload["external_id"] != "APPROVE-EPIC-CV-002" {
		t.Fatalf("specification acceptance must still land as a gate: ok=%v op=%v", ok, op)
	}
	if worklistQuartet != nil {
		t.Fatalf("specification acceptance must not feed completion fields: %v", worklistQuartet)
	}
}

func TestSpecificationRegisterRowDoesNotHideTheCompletionRow(t *testing.T) {
	record := `# EPIC-CV-001 — Only

## Human approval

| Approval id | Approver | Source | Decision |
|---|---|---|---|
| SPEC-APPROVE-EPIC-CV-001 | Pasi | ` + "`USER:2026-08-05`" + ` | approved |
| APPROVE-EPIC-CV-001 | Pasi | ` + "`USER:2026-08-06`" + ` | approved |
`
	ops, quartet := BuildAcceptanceGateOps(Epic{ID: "EPIC-CV-001"}, record)
	if gateOpPayload(ops, "SPEC-APPROVE-EPIC-CV-001") != nil {
		t.Fatal("the specification register belongs to the specification-gate builder")
	}
	if gateOpPayload(ops, "APPROVE-EPIC-CV-001") == nil {
		t.Fatal("the specification row hid the following completion acceptance")
	}
	if quartet == nil || quartet["source_tag"] != "USER:2026-08-06" || quartet["approver_name"] != "Pasi" {
		t.Fatalf("completion quartet = %v, want Pasi's completion row", quartet)
	}
}

func TestSpecificationProseIsNotACompletionApproval(t *testing.T) {
	record := `# EPIC-CV-004 — Specification only

## Approval

Specification: USER:2026-08-16 (Pasi, in-chat) — approve with Option A.
Completion: —
`
	op := BuildEpicOp(Epic{ID: "EPIC-CV-004", Record: "epics/EPIC-CV-004/EPIC.md"}, record)
	if approval := op.Payload["approval"]; approval != nil {
		t.Fatalf("specification prose populated completion fields: %v", approval)
	}
}

func TestWorklistApprovalProseCarriesTheNamedApprover(t *testing.T) {
	op, quartet, ok := BuildWorklistAcceptanceGate(Epic{
		ID:           "EPIC-CV-003",
		ApprovalCell: "approved by Mattias: USER:2026-07-09:approved-local-stack",
	})
	if !ok {
		t.Fatal("attributable WORKLIST acceptance built no gate")
	}
	answerer, _ := op.Payload["answerer"].(map[string]any)
	if answerer["name"] != "Mattias" {
		t.Fatalf("answerer = %v, want the named deciding human", answerer)
	}
	sources, _ := op.Payload["sources"].([]any)
	if !strings.Contains(strings.Join(sourceRefs(sources), " "), "Mattias") {
		t.Fatalf("the deciding human must land in sources: %v", sources)
	}
	if quartet == nil || quartet["approver_name"] != "Mattias" {
		t.Fatalf("quartet must carry the named deciding human: %v", quartet)
	}
}

func TestCompletionAcceptanceFeedsTheQuartetWithTheApprover(t *testing.T) {
	ops := acceptanceOps(t)
	e := epicOpPayload(ops, "EPIC-CV-001")
	if e == nil {
		t.Fatalf("no epic op")
	}
	approval, _ := e["approval"].(map[string]any)
	if approval == nil {
		t.Fatalf("the completion acceptance must feed the approval quartet")
	}
	if approval["approver_name"] != "Pasi" {
		t.Fatalf("approver_name = %v, want the register's decider", approval["approver_name"])
	}
	if approval["source_tag"] != "USER:2026-08-05" {
		t.Fatalf("source_tag = %v", approval["source_tag"])
	}
	// the specification acceptance must NOT have overwritten it — no last-wins
	if basis, _ := approval["basis"].(string); strings.Contains(basis, "specification") {
		t.Fatalf("quartet fed by the wrong acceptance: %q", basis)
	}
}

// A WORKLIST Human-approval tag with no register row still lands, in the
// existing APPROVE-<epic> gate family — the tag date is the answer date and
// the cell text the basis. This is the carrier for the DONE epics whose only
// recorded acceptance is the rollup cell.
func TestWorklistOnlyAcceptanceLands(t *testing.T) {
	ops := acceptanceOps(t)
	g := gateOpPayload(ops, "APPROVE-EPIC-CV-002")
	if g == nil {
		t.Fatalf("worklist-only acceptance built no gate")
	}
	if g["state"] != "answered" || g["answered_at"] != "2026-07-16T00:00:00.000000Z" {
		t.Fatalf("gate: state=%v answered_at=%v", g["state"], g["answered_at"])
	}
	if body, _ := g["body_md"].(string); !strings.Contains(body, "approved `USER:2026-07-16`") {
		t.Fatalf("the cell must ride as the basis: %q", body)
	}
	// and the epic's quartet is fed from the same cell
	e := epicOpPayload(ops, "EPIC-CV-002")
	approval, _ := e["approval"].(map[string]any)
	if approval == nil || approval["source_tag"] != "USER:2026-07-16" {
		t.Fatalf("worklist-only acceptance must feed the quartet: %v", approval)
	}
}

// A register row that uses the APPROVE-<epic> id names the same fact the
// record-derived approval gate answers — one fact, one gate: the record gate
// wins, and the register row still feeds the quartet's attribution.
func TestRegisterRowSharingTheApprovalGateIdDoesNotDuplicate(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", "# EPIC-CV-001 — Only\n\n## User outcome (UR-CV-001)\n\nAs a reader I want one record so that the count means something.\n\n## Approval\n\nGranted `USER:2026-08-05` — accepted after evidence review.\n\n## Human approval\n\n| Approval id | Approver | Role | Source | Scope | Decision |\n|---|---|---|---|---|---|\n| APPROVE-EPIC-CV-001 | Mattias | product owner | `USER:2026-08-05` | EPIC-CV-001 | approved |\n")
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | DONE | APPROVE-EPIC-CV-001 (approved) | — |\n")
	data, _ := Snapshot(root, manifest.Default())
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")

	count := 0
	for _, op := range ops {
		if op.Type == "upsert_gate" && op.Payload["external_id"] == "APPROVE-EPIC-CV-001" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("one fact, one gate: APPROVE-EPIC-CV-001 emitted %d times", count)
	}
	e := epicOpPayload(ops, "EPIC-CV-001")
	approval, _ := e["approval"].(map[string]any)
	if approval == nil || approval["approver_name"] != "Mattias" {
		t.Fatalf("the register's attribution must still enrich the quartet: %v", approval)
	}
}

// REQ-CROSS-248: a WORKLIST cell that records a SPECIFICATION approval while
// saying the completion decision is still outstanding must not mint an
// answered completion gate. `APPROVE-<epic>` is the completion-acceptance
// gate, so `answered`/`applied` on it asserts that a human accepted
// completion. Measured before this test existed: 12 such gates stood answered
// across 12 epics still in flight.
//
// The second half is the guard that matters most. `pendingDecisionRe` matches
// the bare word, and it is right to do so for a register table's Decision
// cell, where "pending" is the whole content. A WORKLIST cell instead quotes
// the deciding human, and those sentences carry the word while meaning the
// opposite — "accept now all pending ledger work", "Let's do that pending
// one". Both cases below are real acceptances on the live corpus
// (APPROVE-EPIC-SYNC-011, APPROVE-EPIC-DIG-004) that a bare-token match would
// invert. The exact count depends on which field and population you measure,
// so the shapes are pinned here rather than a number.
func TestSpecApprovalWithCompletionPendingDoesNotAssertAnAnswer(t *testing.T) {
	outstanding := []string{
		"SPEC approved `USER:2026-08-14`; completion pending",
		"SPEC-APPROVE-EPIC-KNW-003 approved `USER:2026-08-14`; completion approval pending",
		"§10 SPEC-APPROVED; PR slice MERGE APPROVED `USER:2026-08-14`; delivery pending",
		"`SPEC-APPROVE-EPIC-PLN-005` approved `USER:2026-08-19`; implementation green; completion approval pending",
	}
	for _, cell := range outstanding {
		op, quartet, ok := BuildWorklistAcceptanceGate(Epic{ID: "EPIC-CV-010", ApprovalCell: cell})
		if !ok {
			t.Fatalf("the recorded specification approval must still land as a gate: %q", cell)
		}
		if got := op.Payload["state"]; got == "answered" {
			t.Errorf("completion gate is answered while the cell says completion is outstanding\n  cell:  %q\n  state: %v", cell, got)
		}
		if got := op.Payload["applied_state"]; got == "applied" {
			t.Errorf("completion gate is applied while the cell says completion is outstanding\n  cell: %q", cell)
		}
		if quartet != nil {
			t.Errorf("an outstanding completion fed the epic's approval quartet: %v", quartet)
		}
		if body, _ := op.Payload["body_md"].(string); !strings.Contains(body, cell) {
			t.Errorf("the cell text must survive on the gate — a guard flags, it does not delete\n  body: %q", body)
		}
	}

	// A human whose own words contain "pending" is approving, not deferring.
	approvals := []string{
		"**APPROVED** — batch review recorded `USER:2026-08-13` (\"accept now all pending ledger work regarding these\")",
		"**APPROVED** — evidence reviewed · `USER:2026-08-07` (\"Let's do that pending one\")",
	}
	for _, cell := range approvals {
		op, _, ok := BuildWorklistAcceptanceGate(Epic{ID: "EPIC-CV-011", ApprovalCell: cell})
		if !ok {
			t.Fatalf("a real acceptance built no gate: %q", cell)
		}
		if got := op.Payload["state"]; got != "answered" {
			t.Errorf("a real human acceptance was suppressed by the bare word \"pending\"\n  cell:  %q\n  state: %v", cell, got)
		}
	}
}

// The approval basis is the justification a human gave for accepting an epic.
// latestApprovalLine returned a single PHYSICAL line, so a hard-wrapped
// approval paragraph was cut wherever the author happened to press return —
// measured on the corpus, 28 of 154 stored bases end mid-sentence, and the cut
// lengths give it away: 79, 79, 79, 79, 79, 77, 75, 73, markdown wrap width.
// The quotation of the human's own words is severed mid-phrase.
//
// The unit is the wrapped paragraph, not the line. It ends at a blank line, a
// new list item, a table row or a heading — and a table row is already a
// complete unit, so it is never extended.
func TestWrappedApprovalIsOneApproval(t *testing.T) {
	record := "# EPIC-CV-020 — Only\n\n" +
		"## Approval\n\n" +
		"- **APPROVED** — batch review recorded `USER:2026-08-13` (\"accept now all pending\n" +
		"  ledger work regarding these, let's get the release out\").\n" +
		"- **Note:** unrelated follow-up.\n"

	line := approvalLineOf(record)
	if !strings.Contains(line, "accept now all pending") {
		t.Fatalf("approval line = %q, want the human's quotation", line)
	}
	if !strings.Contains(line, "let's get the release out") {
		t.Errorf("the approval was cut at the line wrap — the quotation is severed\n  got: %q", line)
	}
	if strings.Contains(line, "unrelated follow-up") {
		t.Errorf("the approval absorbed the following list item\n  got: %q", line)
	}

	// A table row is already a complete unit and must not swallow the next row.
	tableRec := "# EPIC-CV-021 — Only\n\n" +
		"## Approval\n\n" +
		"| Approval id | Approver | Source | Decision |\n" +
		"|---|---|---|---|\n" +
		"| APPROVE-EPIC-CV-021 | Pasi | `USER:2026-08-14` | approved |\n" +
		"| APP-CV-099 | Pasi | `USER:2026-08-02` | approved |\n"
	line = approvalLineOf(tableRec)
	if strings.Contains(line, "APP-CV-099") {
		t.Errorf("a table row absorbed the row beneath it\n  got: %q", line)
	}

	// A paragraph ending at a blank line stops there.
	paraRec := "# EPIC-CV-022 — Only\n\n" +
		"## Approval\n\n" +
		"Completion approved `USER:2026-08-16` after the walkthrough,\n" +
		"with both loops verified.\n\n" +
		"Unrelated paragraph that must not be absorbed.\n"
	line = approvalLineOf(paraRec)
	if !strings.Contains(line, "both loops verified") {
		t.Errorf("the wrapped continuation was dropped\n  got: %q", line)
	}
	if strings.Contains(line, "Unrelated paragraph") {
		t.Errorf("the approval crossed a blank line\n  got: %q", line)
	}
}
