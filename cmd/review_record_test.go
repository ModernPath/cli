package cmd

import (
	"encoding/json"
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
	env, err := authorEnv()
	if err != nil {
		t.Fatal(err)
	}
	dir := writeReviewConsumerSnapshot(t, root, env.APIURL, env.SystemID, mode+"-ctx-1", "epic", "EPIC-R", aggregate)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := "# working-set context\n\nmode: " + mode + "\ncontext_id: " + mode + "-ctx-1\nscope: epic:EPIC-R\npulled_at: now\n"
	if aggregate != "" {
		stamp += "aggregate: " + aggregate + "\n"
	}
	_ = os.Chmod(filepath.Join(dir, contextFile), 0o644)
	if err := os.WriteFile(filepath.Join(dir, contextFile), []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}
	rehashReviewTestFile(t, dir, contextFile)
}

func postActions(s *fakeAuthorStore) []string {
	out := []string{}
	for _, p := range s.posts {
		record, _ := p.body["record"].(map[string]any)
		out = append(out, str(p.body, "action")+":"+str(record, "external_id"))
	}
	return out
}

// SR-CLI-REVIEW-SNAPSHOT-001: trace validation must run again after finding
// writes, even though the snapshot was valid when recording began.
func TestReviewRecordRevalidatesSnapshotAfterFindingWrites(t *testing.T) {
	store := reviewStore(reviewAggStamped)
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	path := filepath.Join(reviewSnapshotDir, "EPIC-R", "review-ctx-1", "item.md")
	var mutationErr error
	store.authorHook = func(body map[string]any) (bool, int, map[string]any, string) {
		record, _ := body["record"].(map[string]any)
		if body["action"] == "create" && record["kind"] == "finding" {
			mutationErr = os.Chmod(path, 0o644)
			if mutationErr == nil {
				mutationErr = os.WriteFile(path, []byte("changed during finding write\n"), 0o644)
			}
		}
		return false, 0, nil, ""
	}
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[
 {"id":"F-NEW","category":"correctness","severity":"major","owner":"core","source":"r","body":"x"}]}`)
	_, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if err == nil || !strings.Contains(err.Error(), "file integrity mismatch") {
		t.Fatalf("changed snapshot must stop the trace: %v", err)
	}
	if got := strings.Join(postActions(store), " "); got != "create:F-NEW" {
		t.Fatalf("only the already accepted finding should be recorded, got %s", got)
	}
}

// SR-CLI-REVIEW-SNAPSHOT-001 AC4: a failed snapshot check must also stop
// dispositions and the trace when findings are recorded through review record.
func TestReviewRecordStopsWritesWhenFindingSnapshotChanges(t *testing.T) {
	store := reviewStore(reviewAggStamped, "F-OLD")
	store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	path := filepath.Join(reviewSnapshotDir, "EPIC-R", "review-ctx-1", "item.md")
	var mutationErr error
	store.authorHook = func(body map[string]any) (bool, int, map[string]any, string) {
		record, _ := body["record"].(map[string]any)
		if body["action"] == "create" && record["external_id"] == "F-ONE" {
			mutationErr = os.Chmod(path, 0o644)
			if mutationErr == nil {
				mutationErr = os.WriteFile(path, []byte("changed during finding write\n"), 0o644)
			}
		}
		return false, 0, nil, ""
	}
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[
 {"id":"F-ONE","category":"correctness","severity":"major","owner":"core","source":"r","body":"x"},
 {"id":"F-TWO","category":"correctness","severity":"major","owner":"core","source":"r","body":"y"}],
 "dispositions":[{"id":"F-OLD","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"}]}`)
	_, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
	if mutationErr != nil {
		t.Fatal(mutationErr)
	}
	if err == nil {
		t.Fatal("snapshot change must stop the remaining review writes")
	}
	if got := strings.Join(postActions(store), " "); got != "create:F-ONE" {
		t.Fatalf("only the accepted first finding may be recorded, got %s", got)
	}
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

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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
	if !strings.HasPrefix(str(trace, "body_md"), "the packet holds\n\nReview snapshot context:") {
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

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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

		out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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

		out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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
	stampPath := filepath.Join(root, reviewSnapshotDir, "EPIC-R", "review-ctx-1", contextFile)
	raw, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	stamp := string(raw) + "reviewed: EPIC-R epic-r-fp\nreviewed: REQ-R-1 sr-r1-fp\nreviewed: enrichment:REQ-R-1 enr-fp\n"
	if err := os.WriteFile(stampPath, []byte(stamp), 0o644); err != nil {
		t.Fatal(err)
	}
	rehashReviewTestFile(t, filepath.Dir(stampPath), contextFile)
	file := writePlanFile(t, "review.json", `{"verdict":"FAIL","body":"b","source":"RUN:x","findings":[],"dispositions":[]}`)

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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

	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
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

func rehashReviewTestFile(t *testing.T, dir, name string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	mutateReviewManifest(t, dir, func(m *reviewSnapshotManifest) { m.Files[name] = sha256Hex(body) }, true)
}

func TestReviewRecordRefusesChangedSnapshotBeforeAnyWrite(t *testing.T) {
	store := reviewStore(reviewAggStamped)
	cobraWorkspace(t, store.serve(t))
	writeReviewStamp(t, "review", reviewAggStamped)
	root, _ := os.Getwd()
	path := filepath.Join(root, reviewSnapshotDir, "EPIC-R", "review-ctx-1", "item.md")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed snapshot"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := writePlanFile(t, "review.json", reviewPassFile)
	out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
	if err == nil || !strings.Contains(out, "integrity mismatch") {
		t.Fatalf("expected snapshot integrity refusal: %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Fatalf("changed snapshot posted records: %v", postActions(store))
	}
}

// SR-CLI-REVIEW-SNAPSHOT-001 AC4: rehashing a replacement snapshot cannot
// change what the trace attests to after an earlier review write.
func TestReviewRecordRefusesReplacedSnapshotBeforeTrace(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		t.Run(action, func(t *testing.T) {
			store := reviewStore(reviewAggStamped, "F-OLD")
			store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
			cobraWorkspace(t, store.serve(t))
			writeReviewStamp(t, "review", reviewAggStamped)
			dir := filepath.Join(reviewSnapshotDir, "EPIC-R", "review-ctx-1")
			path := filepath.Join(dir, "item.md")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(dir, "MANIFEST.json")
			manifest, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var mutationErr error
			store.authorHook = func(body map[string]any) (bool, int, map[string]any, string) {
				record, _ := body["record"].(map[string]any)
				if body["action"] == action && record["kind"] == "finding" {
					mutationErr = os.Chmod(path, 0o644)
					if mutationErr == nil {
						mutationErr = os.WriteFile(path, []byte("replacement snapshot\n"), 0o644)
						if mutationErr == nil {
							rehashReviewTestFile(t, dir, "item.md")
						}
					}
				}
				return false, 0, nil, ""
			}
			report := `{"verdict":"FAIL","findings":[{"id":"F-NEW","category":"correctness","severity":"major","source":"review","body":"x"}]}`
			want := "create:F-NEW"
			if action == "update" {
				report = `{"verdict":"PASS","dispositions":[{"id":"F-OLD","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"}]}`
				want = "update:F-OLD"
			}
			file := writePlanFile(t, "review.json", report)
			args := []string{"process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1"}
			out, err := runRoot(t, args...)
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			if err == nil || !strings.Contains(err.Error(), "changed since it was selected") {
				t.Fatalf("replacement snapshot must stop the trace: %v\n%s", err, out)
			}
			if got := strings.Join(postActions(store), " "); got != want {
				t.Fatalf("only the accepted review write should be recorded, got %s", got)
			}
			store.authorHook = nil
			if err := atomicWrite(path, original); err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(manifestPath, manifest); err != nil {
				t.Fatal(err)
			}
			if out, err := runRoot(t, args...); err != nil {
				t.Fatalf("restoring the original snapshot must allow retry: %v\n%s", err, out)
			}
			if got := strings.Join(postActions(store), " "); got != want+" evaluate_trace:CR-TRACE-EPIC-R-review-ctx-1" {
				t.Fatalf("retry must skip the accepted write and record the trace, got %s", got)
			}
		})
	}
}

// SR-CLI-REVIEW-SNAPSHOT-001 AC4: every explicit finding aggregate must
// match the selected snapshot before any finding, disposition or trace write.
func TestReviewRecordValidatesFindingAggregatesBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct{ name, aggregate string }{
		{"omitted", ""},
		{"matching", reviewAggStamped},
		{"mismatched", reviewAggMoved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := reviewStore(reviewAggStamped, "F-OLD")
			store.seedFinding("F-OLD", "epic:EPIC-R", "OPEN", "correctness", "major")
			cobraWorkspace(t, store.serve(t))
			writeReviewStamp(t, "review", reviewAggStamped)
			review := reviewFile{
				Verdict: "FAIL",
				Findings: []findingEntry{
					{ID: "F-ONE", Category: "correctness", Severity: "major", Source: "review", Body: "first finding"},
					{ID: "F-TWO", Category: "correctness", Severity: "major", Source: "review", Body: "second finding", Aggregate: tc.aggregate},
				},
				Dispositions: []dispositionEntry{{ID: "F-OLD", From: "OPEN", Disposition: "RESOLVED", Ref: "abc123", Resolution: "packet-edit"}},
			}
			raw, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			file := writePlanFile(t, "review.json", string(raw))
			out, err := runRoot(t, "process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1")
			if tc.aggregate == reviewAggMoved {
				if err == nil || !strings.Contains(err.Error(), "F-TWO aggregate pin does not match") {
					t.Fatalf("explicit aggregate mismatch must refuse: %v\n%s", err, out)
				}
				if len(store.posts) != 0 {
					t.Fatalf("later finding mismatch must stop all review writes: %v", postActions(store))
				}
				return
			}
			if err != nil {
				t.Fatalf("matching or omitted aggregate must succeed: %v\n%s", err, out)
			}
			if got := strings.Join(postActions(store), " "); got != "create:F-ONE create:F-TWO update:F-OLD evaluate_trace:CR-TRACE-EPIC-R-review-ctx-1" {
				t.Fatalf("expected the complete review, got %s", got)
			}
			for _, id := range []string{"F-ONE", "F-TWO"} {
				record, _ := store.postsFor(id, "create")[0].body["record"].(map[string]any)
				if record["aggregate_fingerprint"] != reviewAggStamped {
					t.Fatalf("%s must use the selected aggregate, got %v", id, record)
				}
			}
		})
	}
}

// SR-CLI-REVIEW-SNAPSHOT-001 AC4: each disposition must still use the
// originally selected snapshot, and retry must skip already accepted updates.
func TestReviewRecordStopsDispositionBatchWhenSnapshotChanges(t *testing.T) {
	for _, mutation := range []string{"edited", "replaced"} {
		t.Run(mutation, func(t *testing.T) {
			store := reviewStore(reviewAggStamped, "F-ONE", "F-TWO")
			store.seedFinding("F-ONE", "epic:EPIC-R", "OPEN", "correctness", "major")
			store.seedFinding("F-TWO", "epic:EPIC-R", "OPEN", "correctness", "major")
			cobraWorkspace(t, store.serve(t))
			writeReviewStamp(t, "review", reviewAggStamped)
			dir := filepath.Join(reviewSnapshotDir, "EPIC-R", "review-ctx-1")
			path := filepath.Join(dir, "item.md")
			original, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			manifestPath := filepath.Join(dir, "MANIFEST.json")
			manifest, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			var mutationErr error
			store.authorHook = func(body map[string]any) (bool, int, map[string]any, string) {
				record, _ := body["record"].(map[string]any)
				if body["action"] == "update" && record["external_id"] == "F-ONE" {
					mutationErr = os.Chmod(path, 0o644)
					if mutationErr == nil {
						mutationErr = os.WriteFile(path, []byte("changed during disposition write\n"), 0o644)
						if mutationErr == nil && mutation == "replaced" {
							rehashReviewTestFile(t, dir, "item.md")
						}
					}
				}
				return false, 0, nil, ""
			}
			file := writePlanFile(t, "review.json", `{"verdict":"PASS","dispositions":[
 {"id":"F-ONE","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"},
 {"id":"F-TWO","from":"OPEN","disposition":"RESOLVED","ref":"abc123","resolution":"packet-edit"}]}`)
			args := []string{"process", "review", "record", "--file", file, "--scope", "EPIC-R", "--review-context", "review-ctx-1"}
			out, err := runRoot(t, args...)
			if mutationErr != nil {
				t.Fatal(mutationErr)
			}
			wantReason := "file integrity mismatch"
			if mutation == "replaced" {
				wantReason = "changed since it was selected"
			}
			if err == nil || !strings.Contains(out, wantReason) {
				t.Fatalf("snapshot change must refuse the remaining review writes: %v\n%s", err, out)
			}
			if got := strings.Join(postActions(store), " "); got != "update:F-ONE" {
				t.Fatalf("only the already accepted disposition may be recorded, got %s", got)
			}
			store.authorHook = nil
			if err := atomicWrite(path, original); err != nil {
				t.Fatal(err)
			}
			if err := atomicWrite(manifestPath, manifest); err != nil {
				t.Fatal(err)
			}
			if out, err := runRoot(t, args...); err != nil {
				t.Fatalf("restoring the original snapshot must allow retry: %v\n%s", err, out)
			}
			want := "update:F-ONE update:F-TWO evaluate_trace:CR-TRACE-EPIC-R-review-ctx-1"
			if got := strings.Join(postActions(store), " "); got != want {
				t.Fatalf("retry must skip the accepted disposition and finish the review, got %s", got)
			}
		})
	}
}
