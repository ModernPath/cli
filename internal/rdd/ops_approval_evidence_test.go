package rdd

import "testing"

// REQ-CROSS-139: a completion gate over nothing is as dishonest as a spec gate
// over nothing. The spec side already refuses ("an approval over nothing is
// dishonest"); the completion side opened a gate for an epic with no code, no
// tests and no evidence map, on the strength of a work-list cell reading
// "pending".
//
// The clause the recovered requirement did not state, found by checking blast
// radius (RUN:2026-08-14): 14 of 22 existing approval gates belong to epics with
// no Upper/Lower cells and are ALREADY ANSWERED. Withholding those would rewrite
// history rather than prevent a dishonest ask, so the refusal applies only to a
// gate that would be newly OPENED.
func TestApprovalGateRefusesToOpenOverNothing(t *testing.T) {
	bare := Epic{ID: "EPIC-X-001", State: "awaiting-approval", Record: "epics/x.md"}
	if _, ok := BuildApprovalGateOp(bare, "# EPIC-X-001 — outline\n"); ok {
		t.Error("opened a gate for an epic with no upper or lower evidence")
	}

	withEvidence := bare
	withEvidence.Lower = "**LOWER_VERIFIED** — suite green"
	if _, ok := BuildApprovalGateOp(withEvidence, "# EPIC-X-001 — real\n"); !ok {
		t.Error("withheld a gate from an epic whose evidence is recorded")
	}

	emDash := bare
	emDash.Upper, emDash.Lower = "—", "—"
	if _, ok := BuildApprovalGateOp(emDash, "# EPIC-X-001 — outline\n"); ok {
		t.Error("an em-dash is absence, not a status — should not open a gate")
	}

	// Already answered: must still be emitted, evidence or not.
	answered := emDash
	if _, ok := BuildApprovalGateOp(answered, "# EPIC-X-001\n\n## Approval\n\nAPPROVED `USER:2026-08-09` — evidence reviewed\n"); !ok {
		t.Error("withheld an ANSWERED gate — that strands a human's decision")
	}
}
