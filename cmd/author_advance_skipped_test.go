package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-444 (PR #694 review, #9): `author advance --gate` skipped a
// member that was not in the gate's FROM state and still advanced the epic,
// exit 0. A member already past the transition (at its TO state or later) is
// done; any other skipped or failed member now keeps the epic where it is and
// the call exits non-zero naming it.

func TestREQCROSS444AdvanceGateScopeKeepsTheEpicWhileAMemberIsBehind(t *testing.T) {
	store := newFakeAuthorStore()
	store.seed("EPIC-A", "epic", map[string]any{"process_status": "PROPOSED"})
	store.seed("REQ-A-1", "system", map[string]any{"work_status": "PROPOSED"})
	store.seed("REQ-A-2", "system", map[string]any{"work_status": "DERIVED"})     // behind the transition
	store.seed("REQ-A-3", "system", map[string]any{"work_status": "IN_PROGRESS"}) // past it
	seedEntryGate(store, "ENTRY-A", []any{"EPIC-A", "REQ-A-1", "REQ-A-2", "REQ-A-3"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "advance", "--gate", "ENTRY-A")
	var advanced []string
	for _, p := range advancePosts(store) {
		advanced = append(advanced, advancedID(p))
	}
	for _, id := range advanced {
		if id == "EPIC-A" {
			t.Errorf("the epic must not advance while REQ-A-2 is behind the transition, advanced %v\n%s", advanced, out)
		}
	}
	if strings.Join(advanced, ",") != "REQ-A-1" {
		t.Errorf("the member in FROM still advances, got %v", advanced)
	}
	if err == nil {
		t.Fatalf("a skipped member must exit non-zero\n%s", out)
	}
	if msg := err.Error(); !strings.Contains(msg, "REQ-A-2") || !strings.Contains(msg, "EPIC-A") || strings.Contains(msg, "REQ-A-3") {
		t.Errorf("the refusal names the member that held the epic (not the one already past) and the epic: %v", err)
	}
}
