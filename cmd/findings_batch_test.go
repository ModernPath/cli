package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-443 (EPIC-CLI-TURNS): findings in few calls. add --file records
// every finding of a review in one call with each scope's own aggregate, even
// while several pieces are held, and skips ids already recorded; disposition
// reads the finding's fingerprint itself, and disposition --file changes only
// the listed findings, each guarded on the disposition the file expects.

func findingCreates(s *fakeAuthorStore) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, p := range s.posts {
		record, _ := p.body["record"].(map[string]any)
		if str(p.body, "action") == "create" && str(record, "kind") == "finding" {
			out[str(record, "external_id")] = record
		}
	}
	return out
}

func TestREQCROSS443AddFileUsesEachScopesAggregateAndSkipsExistingIDs(t *testing.T) {
	store := newFakeAuthorStore()
	store.held = []string{"EPIC-A", "EPIC-B"}
	store.contexts["EPIC-A"] = map[string]any{"packet_fingerprint": "agg-A"}
	store.contexts["EPIC-B"] = map[string]any{"packet_fingerprint": "agg-B"}
	store.seedFinding("F-1", "epic:EPIC-A", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "findings.json", `[
 {"id":"F-1","scope":"epic:EPIC-A","category":"correctness","severity":"major","owner":"core","source":"the reviewer","body":"one"},
 {"id":"F-2","scope":"epic:EPIC-A","category":"contract","severity":"minor","owner":"core","source":"the reviewer","body":"two"},
 {"id":"F-3","scope":"epic:EPIC-B","category":"scope","severity":"note","owner":"core","source":"the reviewer","body":"three"}]`)

	out, err := runRoot(t, "process", "findings", "add", "--file", file)
	if err != nil {
		t.Fatalf("findings add --file while several pieces are held: %v\n%s", err, out)
	}
	creates := findingCreates(store)
	if _, again := creates["F-1"]; again {
		t.Errorf("an id the scope already holds must be skipped, posted %v", creates["F-1"])
	}
	if len(creates) != 2 {
		t.Fatalf("one create per new finding, got %v", creates)
	}
	if creates["F-2"]["aggregate_fingerprint"] != "agg-A" || creates["F-3"]["aggregate_fingerprint"] != "agg-B" {
		t.Errorf("each finding carries its own scope's aggregate, got F-2 %v and F-3 %v",
			creates["F-2"]["aggregate_fingerprint"], creates["F-3"]["aggregate_fingerprint"])
	}
	if creates["F-2"]["category"] != "contract" || creates["F-3"]["scope_external_id"] != "EPIC-B" || creates["F-2"]["body"] != "two" {
		t.Errorf("the file's fields reach the record, got %v / %v", creates["F-2"], creates["F-3"])
	}
	if !strings.Contains(out, "F-1") || !strings.Contains(out, "skipped") || !strings.Contains(out, "F-3") {
		t.Errorf("each finding's result is printed:\n%s", out)
	}

	// A re-run adds nothing.
	before := len(store.posts)
	if out, err := runRoot(t, "process", "findings", "add", "--file", file); err != nil {
		t.Fatalf("re-run: %v\n%s", err, out)
	}
	if len(store.posts) != before {
		t.Errorf("a re-run posts nothing, posted %v", store.posts[before:])
	}
}

func TestREQCROSS443DispositionReadsTheFingerprintWithScope(t *testing.T) {
	store := newFakeAuthorStore().withFindingResolution()
	store.seedFinding("F-1", "epic:EPIC-A", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "process", "findings", "disposition", "--id", "F-1", "--disposition", "RESOLVED", "--resolution", "packet-edit")
	if err == nil || !strings.Contains(err.Error(), "--expected-fingerprint is required — a finding disposition is fingerprint-guarded") ||
		!strings.Contains(err.Error(), "--scope") {
		t.Fatalf("without the guard and without --scope it refuses naming both, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Fatalf("a refused disposition posts nothing")
	}

	out, err = runRoot(t, "process", "findings", "disposition", "--id", "F-1", "--scope", "epic:EPIC-A", "--from", "OPEN", "--disposition", "RESOLVED", "--resolution", "packet-edit", "--ref", "abc123")
	if err != nil {
		t.Fatalf("disposition with --scope: %v\n%s", err, out)
	}
	upd := store.postsFor("F-1", "update")
	if len(upd) != 1 || upd[0].status != 200 {
		t.Fatalf("one guarded update, got %v", store.posts)
	}
	rec, _ := upd[0].body["record"].(map[string]any)
	if rec["expected_fingerprint"] != "fp-seed-F-1" || rec["disposition"] != "RESOLVED" || rec["disposition_ref"] != "abc123" {
		t.Errorf("the update carries the read fingerprint, got %v", rec)
	}
}

func TestREQCROSS443DispositionFileChangesOnlyListedFindingsGuardedOnFrom(t *testing.T) {
	store := newFakeAuthorStore().withFindingResolution()
	store.seedFinding("F-1", "epic:EPIC-A", "OPEN", "correctness", "major")
	store.seedFinding("F-2", "epic:EPIC-A", "DEFERRED", "contract", "major")
	store.seedFinding("F-3", "epic:EPIC-A", "OPEN", "security", "major")
	cobraWorkspace(t, store.serve(t))
	file := writePlanFile(t, "dispositions.json", `[
 {"id":"F-2","scope":"epic:EPIC-A","from":"OPEN","disposition":"REJECTED","ref":"out of scope"},
 {"id":"F-1","scope":"epic:EPIC-A","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"}]`)

	out, err := runRoot(t, "process", "findings", "disposition", "--file", file)
	if err == nil {
		t.Fatalf("a refused disposition makes the call exit non-zero\n%s", out)
	}
	if len(store.postsFor("F-2", "update")) != 0 {
		t.Errorf("F-2 is DEFERRED, not the file's OPEN: it must not be written")
	}
	if len(store.postsFor("F-3", "update")) != 0 {
		t.Errorf("a finding the file does not list is never changed")
	}
	upd := store.postsFor("F-1", "update")
	if len(upd) != 1 {
		t.Fatalf("a refusal does not stop the rest: F-1 wants one update, got %v", store.posts)
	}
	rec, _ := upd[0].body["record"].(map[string]any)
	if rec["expected_fingerprint"] != "fp-seed-F-1" || rec["disposition"] != "RESOLVED" || rec["disposition_ref"] != "abc123" {
		t.Errorf("F-1's update carries its read fingerprint, got %v", rec)
	}
	if !strings.Contains(out, "F-2") || !strings.Contains(out, "DEFERRED") || !strings.Contains(out, "F-1") {
		t.Errorf("each finding's result is reported, the refused one naming its current disposition:\n%s", out)
	}
}

func TestREQCROSS443NoResolveEverythingFlag(t *testing.T) {
	for _, name := range []string{"all", "all-open"} {
		if processFindingsDispositionCmd.Flags().Lookup(name) != nil {
			t.Errorf("disposition must not carry --%s: dispositions name their findings", name)
		}
	}
}
