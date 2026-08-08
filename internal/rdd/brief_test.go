package rdd

// EPIC-DEC-001 (REQ-PLN-047, D-DEC-1 workspace-first): a gate source may carry
// a **Brief:** block — plain-language what/why-now/changes/risk/recommendation
// (+ optional Image url). Parsed here, mirrored byte-for-byte in
// mission-control/cli/ops.js, carried on the upsert_gate payload ONLY when
// present (brief-less gates keep identical payloads — no hash churn).

import "testing"

const briefBody = `The technical body of the decision.

**Brief:**
- What: Adopt the union documents library as the only documents surface.
- Why now: Three surfaces are diverging and users see stale copies.
- Changes if approved: One library page replaces the fan-out everywhere.
- Risk if wrong: About a week of rework; no data loss.
- Recommendation: Approve — the evidence is green and the rollback is cheap.
- Image: /api/v1/images/4965cfc7-78ac-447e-94f8-b3cf99321e23

More prose after the block.`

func TestParseBriefExtractsAllFields(t *testing.T) {
	brief, image := parseBrief(briefBody)
	if brief == nil {
		t.Fatal("brief not parsed")
	}
	if brief["what"] != "Adopt the union documents library as the only documents surface." {
		t.Fatalf("what: %q", brief["what"])
	}
	if brief["why_now"] == "" || brief["changes_if_approved"] == "" ||
		brief["risk_if_wrong"] == "" || brief["recommendation"] == "" {
		t.Fatalf("missing fields: %#v", brief)
	}
	if image != "/api/v1/images/4965cfc7-78ac-447e-94f8-b3cf99321e23" {
		t.Fatalf("image: %q", image)
	}
}

func TestParseBriefAbsentAndPartial(t *testing.T) {
	if brief, _ := parseBrief("no brief here at all"); brief != nil {
		t.Fatalf("expected nil, got %#v", brief)
	}
	// a brief without What is not a brief (the keystone line)
	if brief, _ := parseBrief("**Brief:**\n- Why now: reasons."); brief != nil {
		t.Fatalf("expected nil without What, got %#v", brief)
	}
}

func TestRQGateOpCarriesBriefOnlyWhenPresent(t *testing.T) {
	withBrief := BuildRQGateOp(RQ{ID: "RQ-201", Title: "T", Heading: "RQ-201 T — DECISION NEEDED",
		State: "decision-needed", Line: 5, Body: briefBody})
	brief, ok := withBrief.Payload["brief"].(map[string]any)
	if !ok || brief["what"] == "" {
		t.Fatalf("brief missing from payload: %#v", withBrief.Payload["brief"])
	}
	if withBrief.Payload["brief_image_url"] != "/api/v1/images/4965cfc7-78ac-447e-94f8-b3cf99321e23" {
		t.Fatalf("image url: %v", withBrief.Payload["brief_image_url"])
	}

	without := BuildRQGateOp(RQ{ID: "RQ-202", Title: "T", Heading: "RQ-202 T — DECISION NEEDED",
		State: "decision-needed", Line: 6, Body: "plain body"})
	if _, present := without.Payload["brief"]; present {
		t.Fatal("brief key must be ABSENT when no brief block (hash stability)")
	}
	if _, present := without.Payload["brief_image_url"]; present {
		t.Fatal("brief_image_url must be absent too")
	}
}

func TestEpicApprovalGateCarriesBrief(t *testing.T) {
	record := "# EPIC-X — t\n\n## Approval\npending\n\n" + briefBody + "\n"
	epic := Epic{ID: "EPIC-X", State: "awaiting-approval"}
	op, ok := BuildApprovalGateOp(epic, record)
	if !ok {
		t.Skip("approval gate not emitted for this fixture shape")
	}
	if _, present := op.Payload["brief"]; !present {
		t.Fatal("epic approval gate should carry the record's brief")
	}
}

// EPIC-SYNC-009 browser-leg finding (RUN:2026-08-06): folder epics declare
// their requirements as a **Realizes:** header line, not the old
// "## Requirements in this epic" section — approval-gate holds must read both.
func TestApprovalGateHoldsFromRealizesHeader(t *testing.T) {
	record := "# EPIC-Y — t\n\n- **Status:** IN_REVIEW\n- **Realizes:** REQ-PLN-047..050 (`tasks/PLN-REQUIREMENTS.md`) · REQ-AGT-027\n\n## Approval\npending\n"
	op, ok := BuildApprovalGateOp(Epic{ID: "EPIC-Y", State: "awaiting-approval"}, record)
	if !ok {
		t.Fatal("gate not emitted")
	}
	holds, _ := op.Payload["holds"].([]any)
	if len(holds) == 0 {
		t.Fatal("Realizes header must yield holds")
	}
	first := holds[0].(map[string]any)
	if first["held_external_id"] != "REQ-PLN-047" {
		t.Fatalf("holds: %#v", holds)
	}
	seen := map[string]bool{}
	for _, h := range holds {
		seen[h.(map[string]any)["held_external_id"].(string)] = true
	}
	if !seen["REQ-AGT-027"] {
		t.Fatalf("second Realizes id missing: %#v", holds)
	}
}
