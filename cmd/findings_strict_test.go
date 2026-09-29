package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-443 / REQ-CROSS-450 (PR #694 review, #8): the single disposition
// that reads the finding's fingerprint for --scope read it just before the
// write, so it overwrote a decision another session made in between. It now
// needs --from, the disposition the caller saw, as the --file form does. The
// findings, dispositions and review files refuse a key they do not know,
// naming it, instead of dropping it (a misspelt "bodyy" was lost silently).

func TestREQCROSS443SingleDispositionThatReadsTheFingerprintNeedsFrom(t *testing.T) {
	store := newFakeAuthorStore()
	store.seedFinding("F-1", "epic:EPIC-A", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))

	out, err := runRoot(t, "process", "findings", "disposition", "--id", "F-1", "--scope", "epic:EPIC-A", "--disposition", "RESOLVED", "--resolution", "packet-edit")
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("reading the fingerprint for --scope without --from must refuse naming --from, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("the refusal comes before any write, posted %v", store.posts)
	}
}

func TestREQCROSS443SingleDispositionWritesOnlyWhileStillInFrom(t *testing.T) {
	store := newFakeAuthorStore().withFindingResolution()
	store.seedFinding("F-1", "epic:EPIC-A", "DEFERRED", "correctness", "major")
	store.seedFinding("F-2", "epic:EPIC-A", "OPEN", "contract", "major")
	cobraWorkspace(t, store.serve(t))

	// Another session deferred F-1 after this one saw it OPEN.
	out, err := runRoot(t, "process", "findings", "disposition", "--id", "F-1", "--scope", "epic:EPIC-A", "--from", "OPEN", "--disposition", "RESOLVED", "--resolution", "packet-edit")
	if err == nil || !strings.Contains(err.Error(), "DEFERRED") {
		t.Fatalf("a finding no longer in --from must refuse naming its current disposition, got %v\n%s", err, out)
	}
	if len(store.postsFor("F-1", "update")) != 0 {
		t.Errorf("the other session's decision must not be overwritten")
	}

	out, err = runRoot(t, "process", "findings", "disposition", "--id", "F-2", "--scope", "epic:EPIC-A", "--from", "OPEN", "--disposition", "RESOLVED", "--resolution", "packet-edit")
	if err != nil {
		t.Fatalf("a finding still in --from is written: %v\n%s", err, out)
	}
	upd := store.postsFor("F-2", "update")
	if len(upd) != 1 {
		t.Fatalf("one guarded update, got %v", store.posts)
	}
	if rec, _ := upd[0].body["record"].(map[string]any); rec["expected_fingerprint"] != "fp-seed-F-2" {
		t.Errorf("the update carries the read fingerprint, got %v", rec)
	}
}

func TestREQCROSS443FindingsFilesRefuseUnknownKeys(t *testing.T) {
	store := newFakeAuthorStore()
	store.contexts[""] = map[string]any{"packet_fingerprint": "agg-A"}
	store.contexts["EPIC-A"] = map[string]any{"packet_fingerprint": "agg-A"}
	store.seedFinding("F-9", "epic:EPIC-A", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))

	adds := writePlanFile(t, "findings.json", `[
 {"id":"F-1","scope":"epic:EPIC-A","category":"correctness","severity":"major","bodyy":"the prose","ownr":"core"}]`)
	out, err := runRoot(t, "process", "findings", "add", "--file", adds)
	if err == nil || !strings.Contains(err.Error(), "bodyy") || !strings.Contains(err.Error(), "ownr") {
		t.Fatalf("an unknown key is refused, naming each one, got %v\n%s", err, out)
	}

	dispositions := writePlanFile(t, "dispositions.json", `[
 {"id":"F-9","scope":"epic:EPIC-A","form":"OPEN","disposition":"RESOLVED"}]`)
	out, err = runRoot(t, "process", "findings", "disposition", "--file", dispositions)
	if err == nil || !strings.Contains(err.Error(), "form") {
		t.Fatalf("an unknown disposition key is refused, naming it, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("a file with an unknown key writes nothing, posted %v", store.posts)
	}
}

func TestREQCROSS450ReviewFileRefusesUnknownKeys(t *testing.T) {
	store := reviewStore(reviewAggStamped)
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"x","source":"r","summary":"stray",
 "findings":[{"id":"F-1","category":"correctness","severity":"major","owner":"core","source":"r","bodyy":"lost"}]}`)

	out, err := runRoot(t, "process", "review", "record", "--scope", "EPIC-R", "--file", file)
	if err == nil || !strings.Contains(err.Error(), "summary") || !strings.Contains(err.Error(), "bodyy") {
		t.Fatalf("the review file refuses unknown keys, naming each one, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("a file with an unknown key writes nothing, posted %v", store.posts)
	}
}
