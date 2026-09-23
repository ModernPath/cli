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
	// The served facts predate the fold (the epic reads IN_PROGRESS there), so
	// the epic's own apply recipe expects the state the fold reached.
	if !strings.Contains(out, "author advance EPIC-A --kind epic --to DONE --expected IN_REVIEW") {
		t.Fatalf("the epic recipe expects the folded status, not the served IN_PROGRESS: %q", out)
	}
}

// A reopened epic whose members an earlier completion already accepted moves
// nothing but itself: the member recipe has no ids to put in it, so it is
// replaced rather than printed as "members first ()".
func TestProcessCompleteReopenedEpicWithNoMembersToMoveSaysSo(t *testing.T) {
	root, _ := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{memberWith("REQ-A-1", "sr", "DONE", "passing")})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{map[string]any{"external_id": "EPIC-A", "kind": "epic", "from": "IN_PROGRESS", "to": "IN_REVIEW", "basis": "members_reviewed"}}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if strings.Contains(out, "members first ()") {
		t.Fatalf("an empty member list is not printed as a recipe: %q", out)
	}
	if !strings.Contains(out, "no members to advance") {
		t.Fatalf("the output says there is nothing to advance before the epic: %q", out)
	}
	if !strings.Contains(out, "author advance EPIC-A --kind epic --to DONE --expected IN_REVIEW") {
		t.Fatalf("the epic still gets its own recipe at the folded status: %q", out)
	}
}

// IN_REVIEW is the only state the fold has: reconcile's epic edges are entry
// -> IN_PROGRESS and IN_PROGRESS -> IN_REVIEW, and completion is human-gated.
// A fold that lands anywhere else means the server's edges moved under the
// verb, so it stops there rather than opening a gate and printing an apply
// recipe (`--to DONE --expected DONE` would be refused 422).
func TestProcessCompleteReopenedEpicRefusesAFoldToAnUnexpectedState(t *testing.T) {
	root, _ := deliveredRepo(t)
	facts := completeFacts("IN_PROGRESS", []map[string]any{memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing")})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	cs := completeServer(t, facts, true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{map[string]any{"external_id": "EPIC-A", "kind": "epic", "from": "IN_REVIEW", "to": "DONE", "basis": "members_reviewed"}}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err == nil {
		t.Fatalf("a fold to a state other than IN_REVIEW must refuse, got nil\n%s", out)
	}
	for _, want := range []string{"unexpected state DONE", "not IN_REVIEW", "COMPLETE-TRACE-EPIC-A"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must say %q, got %v", want, err)
		}
	}
	for _, o := range cs.order {
		if o == "author:create" {
			t.Fatalf("no gate is opened over an unexpected fold: %v", cs.order)
		}
	}
	if strings.Contains(out, "--kind epic --to DONE --expected") {
		t.Fatalf("no apply recipe is printed for an unexpected fold: %q", out)
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

// reusedTrace is a completion trace that already PASSes at the packet
// aggregate, carrying the body `process complete` writes — the sentence that
// names the targets its evidence run covered. The body comes from the writer's
// own helper, so a reworded body cannot leave the fixtures agreeing with each
// other while production loses coverage detection.
// reopenedFactsWithRebuiltMember is an epic an earlier completion already took
// DONE, reopened by a defect demotion and rebuilt: REQ-A-2 is back IN_REVIEW
// while its DONE siblings stand. This is the shape a re-completion arrives in
// — a DONE epic with a member still IN_REVIEW is refused before any write and
// names this reopen as the remedy.
func reopenedFactsWithRebuiltMember() map[string]any {
	facts := completeFacts("IN_PROGRESS", []map[string]any{
		memberWith("REQ-A-1", "sr", "DONE", "passing"),
		memberWith("REQ-A-2", "sr", "IN_REVIEW", "passing"),
		memberWith("UR-A", "ur", "DONE", "passing"),
	})
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": pinAggregate}
	return facts
}

// foldToInReview is the reconcile script that folds the reopened epic back to
// IN_REVIEW, then reports nothing left to apply.
func foldToInReview() []map[string]any {
	return []map[string]any{
		{"applied": true, "transitions": []any{map[string]any{"external_id": "EPIC-A", "kind": "epic", "from": "IN_PROGRESS", "to": "IN_REVIEW", "basis": "members_reviewed"}}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}
}

func reusedTrace(covered []string) map[string]any {
	return map[string]any{
		"external_id": "COMPLETE-TRACE-EPIC-A", "state": "pass", "fingerprint": pinAggregate,
		"body_md": completionTraceBody("abc1234", "ci", "https://ci/run/1", covered, covered),
	}
}

// The writer and the reader are one format: a body completionTraceBody
// produced reads back as exactly the targets it named. REQ-CROSS-419 rests on
// that pairing here — a reused trace is immutable, so the only record of what
// its run covered is the body it carries — and rewording the writer without
// rewording the parser fails here, where every fixture above would still pass.
func TestCompletionTraceBodyRoundTripsThroughTraceRunTargets(t *testing.T) {
	runTargets := []string{"EPIC-A", "UR-A", "REQ-A-1"}
	got, ok := traceRunTargets(completionTraceBody("abc1234def", "ci", "https://ci/run/1", runTargets, []string{"EPIC-A", "REQ-A-1"}))
	if !ok {
		t.Fatalf("the body the writer produces must be readable by the parser, got ok=false")
	}
	if strings.Join(got, ",") != strings.Join(runTargets, ",") {
		t.Fatalf("the body reads back as the targets its run names, want %v, got %v", runTargets, got)
	}
}

// BACKLOG-TOOL-100 (defect against REQ-CROSS-419, D4a: the posted run names
// every member this completion moves). A completion trace that already PASSes
// at the unchanged aggregate is reused — but the run it cites named the
// earlier completion's targets, so a member this completion adds got no
// delivered-revision evidence at all and the completion gate refuses "not yet"
// for it. The rerun posts the remainder: the targets the reused trace's run
// does not already name. The scope is a reopened epic — the sanctioned way an
// already-completed epic takes a rebuilt member, since a DONE epic with a
// member still IN_REVIEW is refused before any write.
func TestProcessCompletePostsTheRunForTargetsTheReusedTraceLeavesUncovered(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, reopenedFactsWithRebuiltMember(), true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = reusedTrace([]string{"EPIC-A", "UR-A", "REQ-A-1"})
	cs.reconcileResp = foldToInReview()

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "https://ci/run/9", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if len(cs.evidence) != 1 {
		t.Fatalf("exactly one run is posted, for the uncovered remainder, got %v", cs.evidence)
	}
	if got := strings.Join(passedTargets(cs.evidence[0]), ","); got != "REQ-A-2" {
		t.Fatalf("the run names only the target the reused trace's run leaves uncovered, got %v", got)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the trace is reused, so only the gate is created, got %v", cs.authored)
	}
	gate, _ := cs.authored[0]["record"].(map[string]any)
	if gate["external_id"] != "COMPLETE-EPIC-A-R2" {
		t.Fatalf("the successor gate is opened, got %v", gate["external_id"])
	}
	if prereq := stringSlice(gate["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "COMPLETE-TRACE-EPIC-A" {
		t.Fatalf("the gate names the reused trace, got %v", gate["prerequisite_gate_external_ids"])
	}
	if !strings.Contains(out, "naming REQ-A-2") {
		t.Fatalf("the plan says which targets the new run names: %q", out)
	}
	if !strings.Contains(out, "already named by the reused trace") || !strings.Contains(out, "EPIC-A, UR-A") {
		t.Fatalf("the plan says which targets the reused trace's run already covers: %q", out)
	}
	// The remainder run is not recorded on the immutable trace, and the facts
	// carry no revision-scoped evidence field to read coverage back from, so a
	// repeat at this aggregate posts it again. The plan discloses that rather
	// than leaving it to be found.
	if !strings.Contains(out, "posts the remainder again") {
		t.Fatalf("the plan says a repeat at this aggregate posts the remainder again: %q", out)
	}
}

// Nothing is posted, so there is no repeat to disclose: the caveat is printed
// only where a run actually goes out against a reused trace.
func TestProcessCompleteDoesNotWarnAboutARepeatWhenNoRunIsPosted(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{
		memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing"),
		memberWith("UR-A", "ur", "IN_REVIEW", "passing"),
	}), true)
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = reusedTrace([]string{"EPIC-A", "UR-A", "REQ-A-1"})

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if strings.Contains(out, "posts the remainder again") {
		t.Fatalf("no run is posted, so no repeat is disclosed: %q", out)
	}
}

// The reused trace's run already names every target this completion would
// post: nothing is left to record, so no run is posted. REQ-CROSS-419 binds
// the run to the members this completion moves, and a member already named at
// this aggregate is one of them only once.
func TestProcessCompletePostsNoRunWhenTheReusedTraceCoversEveryTarget(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{
		memberWith("REQ-A-1", "sr", "IN_REVIEW", "passing"),
		memberWith("UR-A", "ur", "IN_REVIEW", "passing"),
	}), true)
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = reusedTrace([]string{"EPIC-A", "UR-A", "REQ-A-1"})

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if len(cs.evidence) != 0 {
		t.Fatalf("no run is posted when the reused trace's run names every target, got %v", cs.evidence)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the trace is reused, so only the gate is created, got %v", cs.authored)
	}
}

// The reused trace carries no readable target list — an older trace, or a body
// written by hand. Coverage that cannot be read is not assumed: the run is
// posted for every target, which costs one duplicate row and never strands a
// member without delivered-revision evidence.
func TestProcessCompletePostsTheWholeRunWhenTheReusedTraceCoverageCannotBeRead(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, reopenedFactsWithRebuiltMember(), true)
	cs.existingGates["COMPLETE-EPIC-A"] = map[string]any{"external_id": "COMPLETE-EPIC-A", "state": "closed", "applied_state": "applied"}
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = map[string]any{
		"external_id": "COMPLETE-TRACE-EPIC-A", "state": "pass", "fingerprint": pinAggregate,
	}
	cs.reconcileResp = foldToInReview()

	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if len(cs.evidence) != 1 {
		t.Fatalf("one run is posted when the reused trace's coverage cannot be read, got %v", cs.evidence)
	}
	if got := strings.Join(passedTargets(cs.evidence[0]), ","); got != "EPIC-A,UR-A,REQ-A-2" {
		t.Fatalf("the run names every target when coverage cannot be read, got %v", got)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("the trace is still reused, so only the gate is created, got %v", cs.authored)
	}
	if !strings.Contains(out, "could not be read") {
		t.Fatalf("the plan says the reused trace's coverage could not be read: %q", out)
	}
}
