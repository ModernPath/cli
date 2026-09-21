package cmd

// REQ-CROSS-422 (EPIC-CLI-022, D7 = A): `process enter` enters an epic with
// mixed members as-built members first — a members-only verification gate
// pinned at the epic's aggregate, which the store admits on the epic's passing
// cold review — then the epic and its PROPOSED members in a second call; on an
// epic already entered it opens the verification gate alone. `process reenter
// <member>` resolves the piece that holds the member.

import (
	"strings"
	"testing"
)

func verifyMember(id, status string) map[string]any { return member(id, status) }

func TestProcessEnterAsBuiltMembersEnterFirst(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{
		verifyMember("REQ-A-1", "PROPOSED"), verifyMember("REQ-A-2", "PENDING_VERIFICATION"), verifyMember("REQ-A-3", "PENDING_VERIFICATION"),
	}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)

	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("mixed sources enter as-built members first, got %v\n%s", err, out)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["external_id"] != "ENTRY-EPIC-A-VERIFY" || record["purpose"] != "entry" ||
		record["transition"] != "PENDING_VERIFICATION->TODO" || record["gate_kind"] != "approval_request" {
		t.Fatalf("the verification gate shape is wrong: %v", record)
	}
	if scope := stringSlice(record["exact_scope"]); strings.Join(scope, ",") != "REQ-A-2,REQ-A-3" {
		t.Fatalf("the verification gate names the as-built members only (never the PROPOSED epic or member), got %v", scope)
	}
	if record["evaluated_scope_fingerprint"] != pinAggregate {
		t.Fatalf("the verification gate is pinned at the epic's aggregate so the epic's cold review is its prerequisite, got %v", record["evaluated_scope_fingerprint"])
	}
	if prereq := stringSlice(record["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "CR-A" {
		t.Fatalf("the epic's cold-review trace is the prerequisite, got %v", record["prerequisite_gate_external_ids"])
	}
	if len(cs.selections) != 1 || cs.selections[0]["phase"] != "entry" {
		t.Fatalf("the selection moves to phase entry, got %v", cs.selections)
	}
	for _, want := range []string{"REQ-A-1", "EPIC-A", "second", "process enter EPIC-A"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the plan names the PROPOSED member, the epic and the second call (%q missing): %q", want, out)
		}
	}
}

func TestProcessEnterEnteredEpicOpensTheVerificationGateAlone(t *testing.T) {
	for _, status := range []string{"TODO", "IN_PROGRESS", "IN_REVIEW"} {
		cs := enterServer(t, enterFacts([]map[string]any{
			verifyMember("REQ-A-1", "DONE"), verifyMember("REQ-A-2", "PENDING_VERIFICATION"),
		}, status, "pass", true, "CR-A", nil), true)
		env := wsEnv(t, cs.srv)
		var err error
		out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
		if err != nil {
			t.Fatalf("%s: an entered epic's as-built member enters through the verification gate, got %v\n%s", status, err, out)
		}
		record, _ := cs.authored[0]["record"].(map[string]any)
		if record["external_id"] != "ENTRY-EPIC-A-VERIFY" || record["transition"] != "PENDING_VERIFICATION->TODO" ||
			record["evaluated_scope_fingerprint"] != pinAggregate {
			t.Fatalf("%s: the verification gate is pinned at the epic, got %v", status, record)
		}
		if scope := stringSlice(record["exact_scope"]); strings.Join(scope, ",") != "REQ-A-2" {
			t.Fatalf("%s: the gate names the as-built member alone, got %v", status, scope)
		}
		if strings.Contains(out, "second") || !strings.Contains(out, status) {
			t.Fatalf("%s: no second step is announced for an entered epic; its state is named: %q", status, out)
		}
	}
}

func TestProcessEnterRefusesAsBuiltMembersOfADoneEpic(t *testing.T) {
	for _, status := range []string{"DONE", "OBSOLETE"} {
		cs := enterServer(t, enterFacts([]map[string]any{verifyMember("REQ-A-2", "PENDING_VERIFICATION")}, status, "pass", true, "CR-A", nil), true)
		env := wsEnv(t, cs.srv)
		err := processEnter(env, "EPIC-A", enterOpts{})
		if err == nil || !strings.Contains(err.Error(), status) || !strings.Contains(err.Error(), "demote") {
			t.Fatalf("%s: a done epic's as-built member is refused naming the state and the remedy, got %v", status, err)
		}
		assertNoWrites(t, cs)
	}
}

func TestProcessEnterVerificationDryRunPostsNothing(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{
		verifyMember("REQ-A-1", "PROPOSED"), verifyMember("REQ-A-2", "PENDING_VERIFICATION"),
	}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "ENTRY-EPIC-A-VERIFY") || !strings.Contains(out, "REQ-A-2") {
		t.Fatalf("the dry run prints the verification plan: %q", out)
	}
	assertNoWrites(t, cs)
}

// The verification gate's id walks the same successor series.
func TestProcessEnterVerificationSuccessorId(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{verifyMember("REQ-A-2", "PENDING_VERIFICATION")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	cs.existingGates["ENTRY-EPIC-A-VERIFY"] = map[string]any{"external_id": "ENTRY-EPIC-A-VERIFY", "state": "closed"}
	env := wsEnv(t, cs.srv)
	if err := processEnter(env, "EPIC-A", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if record, _ := cs.authored[0]["record"].(map[string]any); record["external_id"] != "ENTRY-EPIC-A-VERIFY-R2" {
		t.Fatalf("the verification successor is ENTRY-EPIC-A-VERIFY-R2, got %v", record["external_id"])
	}
}

// --gate-id overrides the id only: an open or answered primary id is still
// refused.
func TestProcessEnterGateIDDoesNotBypassAnOpenPrimaryId(t *testing.T) {
	for _, state := range []string{"open", "answered"} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
		cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": state}
		env := wsEnv(t, cs.srv)
		err := processEnter(env, "EPIC-A", enterOpts{gateID: "ENTRY-EPIC-A-AGAIN"})
		if err == nil || !strings.Contains(err.Error(), "already "+state) {
			t.Fatalf("%s: the primary id is named, never rotated past under --gate-id, got %v", state, err)
		}
		assertNoWrites(t, cs)
	}
}

// REQ-CROSS-422 (F-CLI022-R8-01): `process reenter <member>` reads the facts of
// the piece that holds the member — the fixture serves facts for EPIC-A only,
// as the live server serves none for an id that names no held selection — and
// opens REENTRY-<member> at the piece's aggregate citing the piece's cold
// review.
func TestProcessReenterOpensForAMemberUnderTheEpicPiece(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{verifyMember("REQ-A-1", "IN_PROGRESS"), verifyMember("REQ-A-2", "DONE")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	cs.factsOnlyFor = "EPIC-A"
	env := wsEnv(t, cs.srv)

	var err error
	out := captureOut(t, func() { err = processReenterOpen(env, "REQ-A-1") })
	if err != nil {
		t.Fatalf("reenter for a member resolves its piece, got %v\n%s", err, out)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["external_id"] != "REENTRY-REQ-A-1" || record["purpose"] != "reentry" {
		t.Fatalf("the re-entry gate names the member, got %v", record)
	}
	if scope := stringSlice(record["exact_scope"]); strings.Join(scope, ",") != "REQ-A-1" {
		t.Fatalf("exact_scope is the member alone, got %v", scope)
	}
	if record["evaluated_scope_fingerprint"] != pinAggregate {
		t.Fatalf("the gate is pinned at the piece's aggregate, got %v", record["evaluated_scope_fingerprint"])
	}
	if prereq := stringSlice(record["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "CR-A" {
		t.Fatalf("the piece's cold-review trace is the prerequisite, got %v", record["prerequisite_gate_external_ids"])
	}
}

// The advance refusal for a stale entry pin names the piece to re-pin.
func TestProcessAdvanceNamesThePieceToReapply(t *testing.T) {
	facts := advanceFacts("TODO", "passing", true, false)
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": strings.Repeat("9", 64)}
	cs := newCeremonyServer(t, facts, true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || !strings.Contains(err.Error(), "process reapply-entry EPIC-A") || !strings.Contains(err.Error(), "REQ-A-1") {
		t.Fatalf("the remedy names the piece (and the member for a members-only gate), got %v", err)
	}
	assertNoWrites(t, cs)
}
