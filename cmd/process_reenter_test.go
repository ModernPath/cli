package cmd

// BACKLOG-TOOL-44: `process reenter <scope>` opens a re-entry approval gate
// (purpose "reentry") backed by an independent cold-review PASS at the current
// aggregate; --apply posts the reenter_entry action that re-pins the stranded
// entry gate. Open refuses without an independent passing cold review.

import (
	"strings"
	"testing"
)

func TestProcessReenterOpensReentryGate(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "IN_PROGRESS")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)

	var err error
	out := captureOut(t, func() { err = processReenterOpen(env, "EPIC-A") })
	if err != nil {
		t.Fatalf("reenter open: %v\n%s", err, out)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["kind"] != "gate" || record["external_id"] != "REENTRY-EPIC-A" ||
		record["purpose"] != "reentry" || record["gate_kind"] != "decision" {
		t.Fatalf("the re-entry gate shape is wrong: %v", record)
	}
	if record["evaluated_scope_fingerprint"] != pinAggregate {
		t.Fatalf("the re-entry gate is pinned to the current aggregate, got %v", record["evaluated_scope_fingerprint"])
	}
	if prereq := stringSlice(record["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "CR-A" {
		t.Fatalf("the cold-review trace is the prerequisite, got %v", record["prerequisite_gate_external_ids"])
	}
	if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "EPIC-A" {
		t.Fatalf("the re-entry gate names the scope, got %v", scope)
	}
	if opts, _ := record["options"].([]any); len(opts) == 0 || opts[0].(map[string]any)["key"] != "approve" {
		t.Fatalf("an approving option is what the answer rides, got %v", record["options"])
	}
}

func TestProcessReenterOpenRefusesWithoutIndependentColdReview(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict string
		indep   bool
		trace   string
	}{{"absent", "absent", false, ""}, {"fail", "fail", true, "CR-A"}, {"not independent", "pass", false, "CR-A"}} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "IN_PROGRESS")}, "IN_PROGRESS", tc.verdict, tc.indep, tc.trace, nil), true)
		env := wsEnv(t, cs.srv)
		err := processReenterOpen(env, "EPIC-A")
		if err == nil || !strings.Contains(err.Error(), "cold-review") {
			t.Fatalf("%s: a material re-entry needs an independent passing cold review, got %v", tc.name, err)
		}
		assertNoWrites(t, cs)
	}
}

func TestProcessReenterApplyPostsTheAction(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "IN_PROGRESS")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)
	if err := processReenterApplyRepin(env, "EPIC-A"); err != nil {
		t.Fatalf("reenter apply: %v", err)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "reenter_entry" {
		t.Fatalf("--apply posts the reenter_entry action, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["scope_external_id"] != "EPIC-A" {
		t.Fatalf("the scope is named on the record, got %v", record)
	}
}

// BACKLOG-TOOL-52: a withdrawn REENTRY-<scope> id stays reserved, so open must
// accept a successor id via --gate-id — mirroring enter/complete. Without it a
// re-entry gate could never be re-opened after one was withdrawn.
func TestProcessReenterOpenHonorsGateIDOverride(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "IN_PROGRESS")}, "IN_PROGRESS", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)

	processReenterGateID = "REENTRY-EPIC-A-R2"
	defer func() { processReenterGateID = "" }()

	var err error
	out := captureOut(t, func() { err = processReenterOpen(env, "EPIC-A") })
	if err != nil {
		t.Fatalf("reenter open with --gate-id: %v\n%s", err, out)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["external_id"] != "REENTRY-EPIC-A-R2" {
		t.Fatalf("the successor gate id from --gate-id is used, got %v", record["external_id"])
	}
	// the rest of the re-entry gate shape is unchanged from the default-id path
	if record["purpose"] != "reentry" || record["gate_kind"] != "decision" {
		t.Fatalf("the re-entry gate shape is wrong: %v", record)
	}
	if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "EPIC-A" {
		t.Fatalf("the re-entry gate still names the scope, got %v", scope)
	}
	if record["evaluated_scope_fingerprint"] != pinAggregate {
		t.Fatalf("still pinned to the current aggregate, got %v", record["evaluated_scope_fingerprint"])
	}
}
