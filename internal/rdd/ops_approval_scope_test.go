package rdd

import "testing"

// An epic's approval gate must name the epic in `exact_scope`.
//
// `Core.Planning.EpicDetail` selects an epic's gates with
// `epic.code in exact_scope or id in held_gate_ids`, and this builder set
// neither: it holds the epic's REQUIREMENT ids (so those rows show "awaiting
// the <epic> gate") but never names the epic itself. The gate therefore
// existed, carried the right title, body and answer, and was invisible on the
// page of the very epic it approves — EPIC-CLI-007 rendered "No human or trace
// gates recorded" while the store held 961 gates.
//
// Measured on the imported corpus: 47 of 231 approval_request gates were
// unreachable from their epic this way. Its two siblings already do it —
// BuildWorklistAcceptanceGate sets exact_scope outright, and
// BuildSpecApprovalGateOp adds an epic-typed hold, which is why 41 of 41
// spec-approval gates were reachable.
func TestApprovalGateNamesItsEpicInExactScope(t *testing.T) {
	for _, tc := range []struct {
		name   string
		epic   Epic
		record string
	}{
		{
			// The open case: no approval line, evidence recorded.
			name:   "open",
			epic:   Epic{ID: "EPIC-X-001", State: "awaiting-approval", Record: "epics/x.md", Lower: "**LOWER_VERIFIED** — suite green"},
			record: "# EPIC-X-001 — a real epic\n",
		},
		{
			// The answered case matters more: 44 of the 47 are answered, and an
			// answered gate's answer block is frozen server-side, so a later
			// sync cannot repair one that was born without its scope.
			name:   "answered",
			epic:   Epic{ID: "EPIC-Y-002", State: "done", Record: "epics/y.md", Lower: "**LOWER_VERIFIED**"},
			record: "# EPIC-Y-002 — an accepted epic\n\n## Approval\nApproved `USER:2026-08-01` — evidence reviewed\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op, ok := BuildApprovalGateOp(tc.epic, tc.record)
			if !ok {
				t.Fatal("no approval gate emitted — the scope assertion below would be vacuous")
			}
			scope, present := op.Payload["exact_scope"].([]any)
			if !present {
				t.Fatalf("approval gate carries no exact_scope at all: %#v", op.Payload["exact_scope"])
			}
			found := false
			for _, s := range scope {
				if s == tc.epic.ID {
					found = true
				}
			}
			if !found {
				t.Errorf("exact_scope = %v, want it to name %s — otherwise the gate is invisible on that epic's page", scope, tc.epic.ID)
			}
		})
	}
}
