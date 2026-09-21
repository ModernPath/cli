package cmd

// REQ-CROSS-419 CLI half (EPIC-CLI-022, D4a, SCN-DEMOTE-005): `process
// complete` posts the delivered run for the epic, its user requirement and
// every member it moves (IN_REVIEW) — a DONE sibling an earlier completion
// accepted is taken on its CURRENT evidence, never re-posted — while the
// completion trace keeps naming every member. A reopened epic (IN_PROGRESS after a defect
// demotion, every member back in IN_REVIEW or DONE, its earlier completion
// gate closed) completes in the same call: the successor completion trace is
// recorded, reconcile folds the epic to IN_REVIEW, and the successor gate
// COMPLETE-<scope>-R2 opens naming the predecessor. A rebuild that moved the
// packet aggregate under the applied entry approval is refused naming
// `process reapply-entry`.

import (
	"strings"
	"testing"
)

func memberWith(id, kind, status, evidence string) map[string]any {
	m := member(id, status)
	m["kind"] = kind
	m["evidence_state"] = evidence
	return m
}

func passedTargets(ev map[string]any) []string {
	var passed []string
	for _, r := range ev["results"].([]any) {
		m := r.(map[string]any)
		if m["result"] == "pass" {
			passed = append(passed, m["target_external_id"].(string))
		}
	}
	return passed
}

func TestProcessCompleteNeverRepostsADoneSibling(t *testing.T) {
	root, _ := deliveredRepo(t)
	// Every IN_REVIEW member arrives with passing branch evidence (process
	// advance requires it) and is still posted at the delivered revision;
	// only the DONE sibling an earlier completion accepted is omitted.
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{
		memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing"),
		memberWith("REQ-A-2", "sr", "DONE", "passing"),
		memberWith("UR-A", "ur", "IN_REVIEW", "passing"),
	}), true)
	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "https://ci/run/2", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if got := strings.Join(passedTargets(cs.evidence[0]), ","); got != "EPIC-A,UR-A,REQ-A-1" {
		t.Fatalf("the run names the epic, the UR and every IN_REVIEW member at the delivered revision — never a DONE sibling on its current evidence, got %v", got)
	}
	trace, _ := cs.authored[0]["record"].(map[string]any)
	if scope := strings.Join(stringSlice(trace["exact_scope"]), ","); scope != "EPIC-A,REQ-A-1,REQ-A-2,UR-A" {
		t.Fatalf("the completion trace keeps naming every member, got %v", scope)
	}
	if !strings.Contains(out, "REQ-A-2") || !strings.Contains(out, "current evidence") {
		t.Fatalf("the plan says which members are accepted on their current evidence: %q", out)
	}
}

func TestProcessCompleteReopenedEpicRecordsTheSuccessorTraceFoldsAndOpensTheSuccessorGate(t *testing.T) {
	root, tip := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{
		memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing"),
		memberWith("REQ-A-2", "sr", "DONE", "passing"),
		memberWith("UR-A", "ur", "IN_REVIEW", "passing"),
	})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-TRACE-EPIC-A", "state": "stale", "fingerprint": pinAggregate}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{map[string]any{"external_id": "EPIC-A", "kind": "epic", "from": "IN_PROGRESS", "to": "IN_REVIEW", "basis": "members_reviewed"}}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "https://ci/run/3", noFetch: true})
	})
	if err != nil {
		t.Fatalf("a reopened epic completes in one call, got %v\n%s", err, out)
	}
	if got := strings.Join(cs.order, " "); got != "evidence author:evaluate_trace reconcile reconcile author:create work-selection" {
		t.Fatalf("the reopened ceremony is evidence -> successor trace -> reconcile -> successor gate -> phase, got %q", got)
	}
	if got := strings.Join(passedTargets(cs.evidence[0]), ","); got != "EPIC-A,UR-A,REQ-A-1" {
		t.Fatalf("the run names the rebuilt member with the epic and the UR only, got %v", got)
	}
	trace, _ := cs.authored[0]["record"].(map[string]any)
	if trace["external_id"] != "COMPLETE-TRACE-EPIC-A-R2" || trace["fingerprint"] != pinAggregate || trace["verdict"] != "PASS" {
		t.Fatalf("the successor completion trace is recorded at the aggregate, got %v", trace)
	}
	if scope := strings.Join(stringSlice(trace["exact_scope"]), ","); scope != "EPIC-A,REQ-A-1,REQ-A-2,UR-A" {
		t.Fatalf("the successor trace names every member so the epic can re-fold, got %v", scope)
	}
	if !strings.Contains(str(trace, "application_revision"), tip) {
		t.Fatalf("the trace carries the delivered revision, got %v", trace["application_revision"])
	}
	gate, _ := cs.authored[1]["record"].(map[string]any)
	if gate["external_id"] != "COMPLETE-EPIC-A-R2" || gate["purpose"] != "completion" || gate["transition"] != "IN_REVIEW->DONE" {
		t.Fatalf("the successor completion gate shape is wrong: %v", gate)
	}
	if scope := strings.Join(stringSlice(gate["exact_scope"]), ","); scope != "EPIC-A,REQ-A-1,UR-A" {
		t.Fatalf("the successor gate names the epic, the UR and the rebuilt member (the DONE sibling is not moved), got %v", scope)
	}
	if prereq := stringSlice(gate["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "COMPLETE-TRACE-EPIC-A-R2" {
		t.Fatalf("the successor gate names the successor trace, got %v", gate["prerequisite_gate_external_ids"])
	}
	if !strings.Contains(str(gate, "body_md"), "COMPLETE-EPIC-A") {
		t.Fatalf("the successor gate names its closed predecessor, got %v", gate["body_md"])
	}
	if cs.selections[0]["phase"] != "completion" {
		t.Fatalf("the selection moves to phase completion, got %v", cs.selections)
	}
	if !strings.Contains(out, "IN_REVIEW") || !strings.Contains(out, "COMPLETE-EPIC-A-R2") {
		t.Fatalf("the fold and the successor gate are reported: %q", out)
	}
}

func TestProcessCompleteReopenedEpicRefusesWhenReconcileDoesNotFold(t *testing.T) {
	root, _ := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{memberWith("REQ-A-1", "sr", "IN_REVIEW", "claimed")})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{}, "fails": []any{map[string]any{"external_id": "EPIC-A", "reason": "cold-review trace stale"}}},
	}
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "cold-review trace stale") || !strings.Contains(err.Error(), "IN_PROGRESS") {
		t.Fatalf("a fold that does not happen is refused naming reconcile's FAIL, got %v", err)
	}
	for _, o := range cs.order {
		if o == "author:create" {
			t.Fatalf("no successor gate is opened when the epic did not fold: %v", cs.order)
		}
	}
}

func TestProcessCompleteReopenedEpicRefusesAContentMovedAggregate(t *testing.T) {
	root, _ := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{memberWith("REQ-A-1", "sr", "IN_REVIEW", "claimed")})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": strings.Repeat("9", 64)}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "process reapply-entry EPIC-A") {
		t.Fatalf("a rebuild that moved the aggregate under the entry approval is refused naming the re-pin, got %v", err)
	}
	assertNoWrites(t, cs)
}

// --gate-id overrides the id only: the reopened branch still runs.
func TestProcessCompleteReopenedEpicHonorsGateIDOverride(t *testing.T) {
	root, _ := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing")})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{map[string]any{"external_id": "EPIC-A", "kind": "epic", "from": "IN_PROGRESS", "to": "IN_REVIEW", "basis": "members_reviewed"}}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}
	if err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true, gateID: "COMPLETE-EPIC-A-AGAIN"}); err != nil {
		t.Fatalf("a reopened epic with --gate-id completes, got %v", err)
	}
	gate, _ := cs.authored[1]["record"].(map[string]any)
	if gate["external_id"] != "COMPLETE-EPIC-A-AGAIN" || !strings.Contains(str(gate, "body_md"), "COMPLETE-EPIC-A") {
		t.Fatalf("the override names the id and the predecessor is still named, got %v", gate)
	}
}

// --gate-id overrides the id only: an open or answered primary id is still
// refused, so a second gate is never opened over a pending decision.
func TestProcessCompleteGateIDDoesNotBypassAnOpenPrimaryId(t *testing.T) {
	for _, state := range []string{"open", "answered"} {
		root, _ := deliveredRepo(t)
		cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
		cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": state}
		err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true, gateID: "COMPLETE-EPIC-A-AGAIN"})
		if err == nil || !strings.Contains(err.Error(), "already "+state) {
			t.Fatalf("%s: the primary id is named, never rotated past under --gate-id, got %v", state, err)
		}
		assertNoWrites(t, cs)
	}
}

// An IN_PROGRESS epic with no earlier completion is not reopened: the fold
// through reconcile is still the remedy (the existing refusal stands).
func TestProcessCompleteStillRefusesAnUnfoldedEpicWithNoEarlierCompletion(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_PROGRESS", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "process reconcile --apply") {
		t.Fatalf("an epic never completed before still names reconcile, got %v", err)
	}
	assertNoWrites(t, cs)
}
