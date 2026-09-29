package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-446 (EPIC-CLI-TURNS): process next, with several pieces held and
// no --piece, prints one block per piece — scope, phase, why, the skill to run
// and the gates waiting on it — names the --piece remedy, and exits 0. With
// one piece held its output is unchanged.

func heldNextStore() *fakeAuthorStore {
	store := newFakeAuthorStore()
	store.held = []string{"EPIC-A", "EPIC-B"}
	store.heldRows = []any{
		map[string]any{"scope_external_id": "EPIC-A", "scope_kind": "epic", "phase": "plan", "derived_phase": "plan"},
		map[string]any{"scope_external_id": "EPIC-B", "scope_kind": "epic", "phase": "cold_review", "derived_phase": "entry",
			"sections_written_since": []any{"decisions"}},
	}
	store.contexts["EPIC-A"] = map[string]any{"derived_phase": "plan", "declared_phase": "plan",
		"derived_reason": "sections_missing", "skill": "rdd-plan", "packet_fingerprint": strings.Repeat("a", 64)}
	store.contexts["EPIC-B"] = map[string]any{"derived_phase": "entry", "declared_phase": "cold_review",
		"derived_reason": "entry_gate_open", "skill": "rdd-entry-review", "divergence": true, "packet_fingerprint": strings.Repeat("b", 64)}
	store.openGates = []any{
		map[string]any{"external_id": "ENTRY-EPIC-B", "title": "Enter EPIC-B", "state": "open", "exact_scope": []any{"EPIC-B", "REQ-B-1"}},
		map[string]any{"external_id": "COMPLETE-EPIC-Z", "title": "Complete EPIC-Z", "state": "open", "exact_scope": []any{"EPIC-Z"}},
	}
	return store
}

func TestREQCROSS446ProcessNextSummarizesEachHeldPiece(t *testing.T) {
	store := heldNextStore()
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "process", "next")
	if err != nil {
		t.Fatalf("several held pieces exit 0: %v\n%s", err, out)
	}
	a, b := strings.Index(out, "EPIC-A"), strings.Index(out, "EPIC-B")
	if a < 0 || b < 0 || a > b {
		t.Fatalf("one block per held piece, in the held order:\n%s", out)
	}
	blockA, blockB := out[a:b], out[b:]
	for _, want := range []string{"plan", "sections_missing", "rdd-plan"} {
		if !strings.Contains(blockA, want) {
			t.Errorf("EPIC-A's block names %q:\n%s", want, blockA)
		}
	}
	for _, want := range []string{"entry", "entry_gate_open", "rdd-entry-review", "ENTRY-EPIC-B"} {
		if !strings.Contains(blockB, want) {
			t.Errorf("EPIC-B's block names %q:\n%s", want, blockB)
		}
	}
	if strings.Contains(blockA, "ENTRY-EPIC-B") || strings.Contains(out, "COMPLETE-EPIC-Z") {
		t.Errorf("a gate is listed only under the piece it names:\n%s", out)
	}
	if !strings.Contains(out, "--piece") {
		t.Errorf("the --piece remedy for scoped verbs is named:\n%s", out)
	}
}

func TestREQCROSS446ProcessNextWithOnePieceKeepsItsOutput(t *testing.T) {
	store := heldNextStore()
	store.held = []string{"EPIC-A"}
	store.contexts[""] = store.contexts["EPIC-A"]
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "process", "next")
	if err != nil {
		t.Fatalf("process next: %v\n%s", err, out)
	}
	for _, want := range []string{"derived phase:  plan", "why:            sections_missing", "run:            rdd-plan"} {
		if !strings.Contains(out, want) {
			t.Errorf("one held piece keeps today's output (%q):\n%s", want, out)
		}
	}
	if strings.Contains(out, "--piece") {
		t.Errorf("one held piece names no --piece remedy:\n%s", out)
	}
}
