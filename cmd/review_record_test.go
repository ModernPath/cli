package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-450 (EPIC-CLI-TURNS): `process review record --file review.json`
// records a delegated cold review in one call — its findings, its guarded
// dispositions and the cold-review trace pinned to the aggregate the review
// pull stamped — and refuses before any write without the stamp, on a moved
// aggregate, or on a PASS that would leave a material finding open.

var (
	reviewAggStamped = strings.Repeat("a", 64)
	reviewAggMoved   = strings.Repeat("b", 64)
)

// reviewStore serves EPIC-R with one member and the served open material
// finding ids, at the given current aggregate.
func reviewStore(current string, open ...string) *fakeAuthorStore {
	store := newFakeAuthorStore().withFindingResolution()
	store.held = []string{"EPIC-R", "EPIC-OTHER"}
	if open == nil {
		open = []string{}
	}
	store.contexts["EPIC-R"] = map[string]any{
		"packet_fingerprint": current,
		"facts": map[string]any{
			"aggregate":   current,
			"scope":       map[string]any{"external_id": "EPIC-R", "kind": "epic", "status": "PLANNED"},
			"members":     []any{map[string]any{"external_id": "REQ-R-1", "kind": "system", "status": "PROPOSED"}},
			"cold_review": map[string]any{"open_finding_ids": open},
		},
	}
	return store
}

// writeReviewStamp writes the .context a review pull of EPIC-R leaves.
func writeReviewStamp(t *testing.T, mode, aggregate string) {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, workingSetDir, "EPIC-R")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := "# working-set context\n\nmode: " + mode + "\ncontext_id: " + mode + "-ctx-1\nscope: epic:EPIC-R\npulled_at: now\n"
	if aggregate != "" {
		stamp += "aggregate: " + aggregate + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}
}

func postActions(s *fakeAuthorStore) []string {
	out := []string{}
	for _, p := range s.posts {
		record, _ := p.body["record"].(map[string]any)
		out = append(out, str(p.body, "action")+":"+str(record, "external_id"))
	}
	return out
}

const reviewPassFile = `{"verdict":"PASS","body":"the packet holds","source":"RUN:2026-09-27:cold-review",
 "findings":[
  {"id":"F-OLD","category":"correctness","severity":"major","owner":"core","source":"r","body":"already recorded"},
  {"id":"F-NEW","category":"traceability","severity":"note","owner":"core","source":"r","body":"a wording note"}],
 "dispositions":[{"id":"F-OLD","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"}]}`

func TestREQCROSS450RecordPostsFindingsDispositionsAndTraceInOrder(t *testing.T) {
	store := reviewStore(reviewAggStamped, "F-OLD")
	store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	file := writePlanFile(t, "review.json", reviewPassFile)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
	if err != nil {
		t.Fatalf("review record: %v\n%s", err, out)
	}
	got := strings.Join(postActions(store), " ")
	want := "create:F-NEW update:F-OLD evaluate_trace:"
	if !strings.HasPrefix(got, want) || len(store.posts) != 3 {
		t.Fatalf("one call posts the new finding, the disposition and the trace, in that order: got %s", got)
	}
	created, _ := store.posts[0].body["record"].(map[string]any)
	if created["aggregate_fingerprint"] != reviewAggStamped || created["review_context_id"] != "review-ctx-1" ||
		created["scope_external_id"] != "EPIC-R" || created["scope_kind"] != "epic" {
		t.Errorf("the finding carries the stamped aggregate, the review context and the review's scope, got %v", created)
	}
	disp, _ := store.posts[1].body["record"].(map[string]any)
	if disp["expected_fingerprint"] != "fp-seed-F-OLD" || disp["disposition"] != "RESOLVED" || disp["resolution_kind"] != "packet_edit" {
		t.Errorf("the disposition is guarded on the read fingerprint and carries the file's resolution kind, got %v", disp)
	}
	trace, _ := store.posts[2].body["record"].(map[string]any)
	if trace["purpose"] != "cold-review" || trace["verdict"] != "PASS" || trace["fingerprint"] != reviewAggStamped ||
		trace["review_context_id"] != "review-ctx-1" || trace["transition"] != "plan->entry" {
		t.Errorf("the trace is a PASS cold review pinned to the stamped aggregate under the review context, got %v", trace)
	}
	if scope := stringSlice(trace["exact_scope"]); strings.Join(scope, ",") != "EPIC-R,REQ-R-1" {
		t.Errorf("the trace names the scope and each member, got %v", trace["exact_scope"])
	}
	if trace["body_md"] != "the packet holds" {
		t.Errorf("the verdict body rides the trace, got %v", trace["body_md"])
	}
	if !strings.Contains(out, "F-OLD") || !strings.Contains(out, "skipped") || !strings.Contains(out, "F-NEW") {
		t.Errorf("each result is printed:\n%s", out)
	}
}

func TestREQCROSS450RefusesBeforeAnyWriteWhenTheAggregateMoved(t *testing.T) {
	store := reviewStore(reviewAggMoved)
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[
 {"id":"F-1","category":"correctness","severity":"major","owner":"core","source":"r","body":"x"}],"dispositions":[]}`)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
	if err == nil || !strings.Contains(err.Error(), reviewAggStamped) || !strings.Contains(err.Error(), reviewAggMoved) {
		t.Fatalf("a moved aggregate refuses naming both, got %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("nothing is written, posted %v", postActions(store))
	}
}

func TestREQCROSS450RefusesBeforeAnyWriteWithoutAReviewStamp(t *testing.T) {
	for _, mode := range []string{"authoring", ""} {
		store := reviewStore(reviewAggStamped)
		cobraWorkspace(t, store.serve(t))
		if mode != "" {
			writeReviewStamp(t, mode, reviewAggStamped)
		}
		file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[],"dispositions":[]}`)

		out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
		if err == nil || !strings.Contains(err.Error(), "--for-review") {
			t.Fatalf("stamp %q: no review-mode stamp refuses naming the review pull, got %v\n%s", mode, err, out)
		}
		if len(store.posts) != 0 {
			t.Errorf("stamp %q: nothing is written, posted %v", mode, postActions(store))
		}
	}
}

func TestREQCROSS450RefusesAPassThatLeavesAMaterialFindingOpen(t *testing.T) {
	cases := map[string]struct {
		open []string
		file string
		want string
	}{
		"served open, not dispositioned": {[]string{"F-OLD"},
			`{"verdict":"PASS","body":"b","source":"RUN:x","findings":[],"dispositions":[]}`, "F-OLD"},
		"new material finding": {nil,
			`{"verdict":"PASS","body":"b","source":"RUN:x","findings":[
 {"id":"F-NEW","category":"contract","severity":"major","owner":"core","source":"r","body":"x"}],"dispositions":[]}`, "F-NEW"},
		"deferred": {[]string{"F-OLD"},
			`{"verdict":"PASS","body":"b","source":"RUN:x","findings":[],"dispositions":[
 {"id":"F-OLD","from":"OPEN","disposition":"DEFERRED","ref":"later"}]}`, "F-OLD"},
	}
	for name, tc := range cases {
		store := reviewStore(reviewAggStamped, tc.open...)
		store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
		cobraWorkspace(t, store.serve(t))
		writeReviewStamp(t, "review", reviewAggStamped)
		file := writePlanFile(t, "review.json", tc.file)

		out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "PASS") {
			t.Errorf("%s: a PASS leaving %s open or deferred is refused naming it, got %v\n%s", name, tc.want, err, out)
		}
		if len(store.posts) != 0 {
			t.Errorf("%s: nothing is written, posted %v", name, postActions(store))
		}
	}
}

// REQ-CROSS-464 (EPIC-CLI-DELTA): the trace carries the per-item fingerprints
// the review pull stamped, as reviewed_fingerprints, beside the full aggregate
// pin; a stamp from an older CLI records none.
func TestREQCROSS464RecordSendsTheStampedReviewedFingerprints(t *testing.T) {
	store := reviewStore(reviewAggStamped)
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	stampPath := filepath.Join(root, workingSetDir, "EPIC-R", contextFile)
	raw, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	stamp := string(raw) + "reviewed: EPIC-R epic-r-fp\nreviewed: REQ-R-1 sr-r1-fp\nreviewed: enrichment:REQ-R-1 enr-fp\n"
	if err := os.WriteFile(stampPath, []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[],"dispositions":[]}`)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
	if err != nil {
		t.Fatalf("review record: %v\n%s", err, out)
	}
	if len(store.posts) == 0 {
		t.Fatalf("the trace is posted\n%s", out)
	}
	trace, _ := store.posts[len(store.posts)-1].body["record"].(map[string]any)
	got, _ := trace["reviewed_fingerprints"].(map[string]any)
	want := map[string]string{"EPIC-R": "epic-r-fp", "REQ-R-1": "sr-r1-fp", "enrichment:REQ-R-1": "enr-fp"}
	if len(got) != len(want) {
		t.Fatalf("the trace carries the stamp's reviewed fingerprints, got %v", trace["reviewed_fingerprints"])
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("reviewed_fingerprints[%s] = %v, want %s", k, got[k], v)
		}
	}
	if trace["fingerprint"] != reviewAggStamped {
		t.Errorf("the trace still pins the full aggregate, got %v", trace["fingerprint"])
	}
}

func TestREQCROSS450ARefusedDispositionStopsBeforeTheTrace(t *testing.T) {
	store := reviewStore(reviewAggStamped, "F-OLD")
	store.seedFinding("F-OLD", "epic:EPIC-R", "DEFERRED", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[
 {"id":"F-NEW","category":"scope","severity":"minor","owner":"core","source":"r","body":"x"}],
 "dispositions":[{"id":"F-OLD","from":"OPEN","disposition":"REJECTED","ref":"out of scope"}]}`)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R")
	if err == nil {
		t.Fatalf("a refused disposition exits non-zero\n%s", out)
	}
	for _, a := range postActions(store) {
		if strings.HasPrefix(a, "evaluate_trace") {
			t.Fatalf("a refused disposition stops before the trace, posted %v", postActions(store))
		}
	}
	if len(store.postsFor("F-NEW", "create")) != 1 {
		t.Errorf("the findings before it are still recorded, posted %v", postActions(store))
	}
	if !strings.Contains(out, "F-OLD") || !strings.Contains(out, "DEFERRED") {
		t.Errorf("the refused disposition is reported:\n%s", out)
	}
}
