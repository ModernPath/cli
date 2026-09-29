package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-444 (EPIC-CLI-TURNS): `author advance` reads what it can — the
// record's kind, the gate's fingerprint, and --expected/--to from the gate's
// transition (never from the record's current status) — and `--gate G` with no
// id advances the gate's whole exact scope, members before the epic.

func seedEntryGate(s *fakeAuthorStore, id string, scope []any, transition string, keys []any) {
	s.gates[id] = map[string]any{"external_id": id, "state": "answered", "fingerprint": "gfp-" + id,
		"content_fingerprint": "row-fp-not-this-one", "exact_scope": scope, "transition": transition,
		"chosen_option_keys": keys}
}

func advancePosts(s *fakeAuthorStore) []map[string]any {
	var out []map[string]any
	for _, p := range s.posts {
		if str(p.body, "action") == "advance" {
			out = append(out, p.body)
		}
	}
	return out
}

func advancedID(body map[string]any) string {
	record, _ := body["record"].(map[string]any)
	return str(record, "external_id")
}

func seedAdvanceScope(s *fakeAuthorStore) {
	s.seed("EPIC-A", "epic", map[string]any{"process_status": "PROPOSED"})
	s.seed("REQ-A-1", "system", map[string]any{"work_status": "PROPOSED"})
	s.seed("REQ-A-2", "user", map[string]any{"work_status": "PROPOSED"})
	s.seed("REQ-A-3", "system", map[string]any{"work_status": "TODO"}) // already past
}

func TestREQCROSS444AdvanceInfersKindGateFingerprintAndStates(t *testing.T) {
	store := newFakeAuthorStore()
	seedAdvanceScope(store)
	seedEntryGate(store, "ENTRY-A", []any{"EPIC-A", "REQ-A-1"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "advance", "EPIC-A", "--gate", "ENTRY-A")
	posts := advancePosts(store)
	if len(posts) != 1 {
		t.Fatalf("want one advance, got %v (%v)\n%s", posts, err, out)
	}
	body := posts[0]
	record, _ := body["record"].(map[string]any)
	if record["kind"] != "epic" {
		t.Errorf("an epic advanced without --kind must post kind epic, posted %v", record)
	}
	if body["to"] != "TODO" || body["expected"] != "PROPOSED" {
		t.Errorf("--to and --expected default to the gate's TO and FROM, posted to=%v expected=%v", body["to"], body["expected"])
	}
	if body["gate_fingerprint"] != "gfp-ENTRY-A" || body["gate_answer"] != "approve" {
		t.Errorf("the gate fingerprint and an approve answer are read from the gate, posted %v", body)
	}
}

func TestREQCROSS444AdvanceExpectedIsTheGateFromNotTheCurrentStatus(t *testing.T) {
	store := newFakeAuthorStore()
	store.seed("REQ-A-1", "system", map[string]any{"work_status": "TODO"})
	seedEntryGate(store, "ENTRY-A", []any{"REQ-A-1"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	_, _ = runRoot(t, "author", "advance", "REQ-A-1", "--gate", "ENTRY-A")
	posts := advancePosts(store)
	if len(posts) != 1 || posts[0]["expected"] != "PROPOSED" {
		t.Fatalf("--expected is the gate's FROM state even when the record has moved, posted %v", posts)
	}
	record, _ := posts[0]["record"].(map[string]any)
	if record["kind"] != "requirement" {
		t.Errorf("a system requirement posts kind requirement, posted %v", record)
	}
}

func TestREQCROSS444AdvanceGateScopeMembersBeforeTheEpic(t *testing.T) {
	store := newFakeAuthorStore()
	seedAdvanceScope(store)
	seedEntryGate(store, "ENTRY-A", []any{"EPIC-A", "REQ-A-1", "REQ-A-2", "REQ-A-3"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "advance", "--gate", "ENTRY-A")
	if err != nil {
		t.Fatalf("advance --gate: %v\n%s", err, out)
	}
	var order []string
	for _, p := range advancePosts(store) {
		order = append(order, advancedID(p))
		if p["gate_fingerprint"] != "gfp-ENTRY-A" || p["expected"] != "PROPOSED" || p["to"] != "TODO" {
			t.Errorf("each scope advance carries the gate's fingerprint and transition, posted %v", p)
		}
	}
	if strings.Join(order, ",") != "REQ-A-1,REQ-A-2,EPIC-A" {
		t.Errorf("members in FROM state first, then the epic, skipping REQ-A-3 (already past): got %v", order)
	}
	if !strings.Contains(out, "REQ-A-3") {
		t.Errorf("the skipped record is reported:\n%s", out)
	}
}

func TestREQCROSS444AdvanceMembersOnlyGateLeavesTheEpic(t *testing.T) {
	store := newFakeAuthorStore()
	seedAdvanceScope(store)
	seedEntryGate(store, "ENTRY-M", []any{"REQ-A-1", "REQ-A-2"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	if out, err := runRoot(t, "author", "advance", "--gate", "ENTRY-M"); err != nil {
		t.Fatalf("advance --gate: %v\n%s", err, out)
	}
	var order []string
	for _, p := range advancePosts(store) {
		order = append(order, advancedID(p))
	}
	if strings.Join(order, ",") != "REQ-A-1,REQ-A-2" {
		t.Errorf("a members-only gate advances its members and never the epic, got %v", order)
	}
}

func TestREQCROSS444AdvanceRefusedMemberStopsBeforeTheEpic(t *testing.T) {
	store := newFakeAuthorStore()
	seedAdvanceScope(store)
	store.fail["REQ-A-1"] = true
	seedEntryGate(store, "ENTRY-A", []any{"EPIC-A", "REQ-A-1", "REQ-A-2"}, "PROPOSED->TODO", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "author", "advance", "--gate", "ENTRY-A")
	if err == nil {
		t.Fatalf("a refused member must exit non-zero\n%s", out)
	}
	for _, p := range advancePosts(store) {
		if advancedID(p) == "EPIC-A" {
			t.Errorf("the epic must not advance after a member was refused")
		}
	}
	if !strings.Contains(err.Error()+out, "refused REQ-A-1") {
		t.Errorf("the refusal is reported: %v\n%s", err, out)
	}
}

func TestREQCROSS444AdvanceInfersOnlyAnApproveAnswer(t *testing.T) {
	store := newFakeAuthorStore()
	store.seed("REQ-A-1", "system", map[string]any{"work_status": "PROPOSED"})
	seedEntryGate(store, "ENTRY-X", []any{"REQ-A-1"}, "PROPOSED->TODO", []any{"approve", "defer"})
	cobraWorkspace(t, store.serve(t))

	_, _ = runRoot(t, "author", "advance", "REQ-A-1", "--gate", "ENTRY-X")
	posts := advancePosts(store)
	if len(posts) != 1 {
		t.Fatalf("want one advance, got %v", posts)
	}
	if _, sent := posts[0]["gate_answer"]; sent {
		t.Errorf("an answer other than exactly approve is never inferred, posted %v", posts[0])
	}
	if posts[0]["gate_fingerprint"] != "gfp-ENTRY-X" || posts[0]["expected"] != "PROPOSED" {
		t.Errorf("the fingerprint and FROM state are still read, posted %v", posts[0])
	}
}

func TestREQCROSS444AdvanceExplicitFlagsWinAndNoTransitionIsRefused(t *testing.T) {
	store := newFakeAuthorStore()
	store.seed("REQ-A-1", "system", map[string]any{"work_status": "IN_REVIEW"})
	seedEntryGate(store, "ENTRY-A", []any{"REQ-A-1"}, "PROPOSED->TODO", []any{"approve"})
	seedEntryGate(store, "LEGACY-G", []any{"REQ-A-1"}, "", []any{"approve"})
	cobraWorkspace(t, store.serve(t))

	_, _ = runRoot(t, "author", "advance", "REQ-A-1", "--gate", "ENTRY-A", "--kind", "requirement",
		"--to", "DONE", "--expected", "IN_REVIEW", "--gate-fingerprint", "given-fp", "--gate-answer", "yes")
	posts := advancePosts(store)
	if len(posts) != 1 || posts[0]["to"] != "DONE" || posts[0]["expected"] != "IN_REVIEW" ||
		posts[0]["gate_fingerprint"] != "given-fp" || posts[0]["gate_answer"] != "yes" {
		t.Fatalf("explicit flags win over the gate read, posted %v", posts)
	}

	before := len(store.posts)
	_, err := runRoot(t, "author", "advance", "--gate", "LEGACY-G")
	if err == nil || !strings.Contains(err.Error(), "transition") {
		t.Errorf("a gate with no transition is refused, naming it: %v", err)
	}
	if len(store.posts) != before {
		t.Errorf("a refused gate posts nothing, posted %v", store.posts[before:])
	}
}
