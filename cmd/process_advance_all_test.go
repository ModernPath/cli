package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-445 (EPIC-CLI-TURNS): `process advance --all --piece EPIC --log
// RUN` runs the per-SR advance for every system requirement of the piece,
// reporting each one it moved and why it did not move the others.

func advanceAllFacts() map[string]any {
	facts := advanceFacts("IN_PROGRESS", "passing", true, false)
	members := facts["members"].([]map[string]any)
	facts["members"] = append(members, map[string]any{"external_id": "UR-A-1", "kind": "ur", "status": "IN_PROGRESS",
		"content_fingerprint": strings.Repeat("e", 64), "evidence_state": "claimed"})
	return facts
}

func TestREQCROSS445ProcessAdvanceAllMovesEligibleAndExplainsTheRest(t *testing.T) {
	cs := newCeremonyServer(t, advanceAllFacts(), true)
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{transition("REQ-A-1", "IN_PROGRESS", "IN_REVIEW")}, "fails": []any{}},
	}
	cobraWorkspace(t, cs.srv)

	out, err := runRoot(t, "process", "advance", "--all", "--piece", "EPIC-A", "--log", "go test ./...")
	var traced []string
	for _, a := range cs.authored {
		record, _ := a["record"].(map[string]any)
		traced = append(traced, str(record, "external_id"))
	}
	if strings.Join(traced, ",") != "TRACE-LOWER-REQ-A-1" {
		t.Fatalf("only the eligible SR gets its lower trace, got %v (%v)\n%s", traced, err, out)
	}
	if !strings.Contains(out, "REQ-A-1") || !strings.Contains(out, "IN_REVIEW") {
		t.Errorf("the SR it moved is reported with its status:\n%s", out)
	}
	if !strings.Contains(out, "REQ-A-2") || !strings.Contains(out, "no RED is recorded") {
		t.Errorf("the SR it did not move is reported with the reason:\n%s", out)
	}
	if strings.Contains(out, "UR-A-1 is a user requirement") {
		t.Errorf("a user requirement is not a system requirement --all takes:\n%s", out)
	}
	if err == nil {
		t.Errorf("an SR left unadvanced exits non-zero\n%s", out)
	}
}

func TestREQCROSS445ProcessAdvanceAllNeedsTheLog(t *testing.T) {
	cs := newCeremonyServer(t, advanceAllFacts(), true)
	cobraWorkspace(t, cs.srv)

	_, err := runRoot(t, "process", "advance", "--all", "--piece", "EPIC-A")
	if err == nil || !strings.Contains(err.Error(), "--log") {
		t.Fatalf("--all without --log is refused naming it, got %v", err)
	}
	if len(cs.authored)+len(cs.reconciles) != 0 {
		t.Errorf("a refusal writes nothing, got %v %v", cs.authored, cs.reconciles)
	}
}
