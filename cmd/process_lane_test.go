package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// REQ-CROSS-458 (EPIC-RDD-LANE): the `process lane` verbs. Each posts the
// records the server contract names and refuses before any write, with the
// server's own reason when the server is the one refusing.

const laneSR = "REQ-LN-1"

const laneReviewPass = `{"verdict":"PASS","body":"the one-line change holds","source":"RUN:2026-09-28:narrow","findings":[],"dispositions":[]}`

func laneWorkspace(t *testing.T, l *laneStore) {
	t.Helper()
	cobraWorkspace(t, l.serve(t))
}

// laneGit runs git in the workspace and returns its trimmed output.
func laneGit(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func laneContextID(t *testing.T, sr string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(workingSetDir, sr, contextFile))
	if err != nil {
		t.Fatalf("the review pull left no stamp: %v", err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if v, ok := strings.CutPrefix(line, "context_id: "); ok {
			return strings.TrimSpace(v)
		}
	}
	t.Fatalf("no context_id in the stamp:\n%s", raw)
	return ""
}

func laneReviewed(t *testing.T, l *laneStore, sr string) {
	t.Helper()
	if _, err := runRoot(t, "working-set", "pull", sr, "--for-review"); err != nil {
		t.Fatalf("review pull: %v", err)
	}
	if out, err := runRoot(t, "process", "lane", "review", sr, "--file", writePlanFile(t, "review.json", laneReviewPass)); err != nil {
		t.Fatalf("lane review: %v\n%s", err, out)
	}
}

// ---------------------------------------------------------------- authorize

func TestLaneAuthorizeOpensTheGateAndNamesBothWaysToAnswer(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)

	out, err := runRoot(t, "process", "lane", "authorize", "--classes", "wording,defect_with_failing_test",
		"--appliers", "7,9", "--expires", "2026-10-20T00:00:00Z", "--cap", "5", "--exclude", "docs/legal/**")
	if err != nil {
		t.Fatalf("authorize: %v\n%s", err, out)
	}
	creates := l.lanePosts("create")
	if len(creates) != 1 {
		t.Fatalf("authorize posts one create, got %d", len(creates))
	}
	rec, _ := creates[0]["record"].(map[string]any)
	if rec["kind"] != "gate" || rec["purpose"] != "lane_authorization" {
		t.Fatalf("want a lane_authorization gate, posted %v", rec)
	}
	if got := stringSlice(rec["exact_scope"]); !slices.Equal(got, []string{"system:1"}) {
		t.Errorf("the authorization names exactly this System, got %v", got)
	}
	if _, present := rec["transition"]; present {
		t.Errorf("the authorization declares no transition, posted %v", rec["transition"])
	}
	if !slices.ContainsFunc(anySlice(rec["options"]), func(o any) bool { return str(o.(map[string]any), "key") == "approve" }) {
		t.Errorf("the authorization needs an approve option, posted %v", rec["options"])
	}
	payload, _ := rec["raw_payload"].(map[string]any)
	if got := stringSlice(payload["classes"]); !slices.Equal(got, []string{"wording", "defect_with_failing_test"}) {
		t.Errorf("classes: %v", got)
	}
	if got := anySlice(payload["appliers"]); len(got) != 2 || got[0] != float64(7) || got[1] != float64(9) {
		t.Errorf("appliers are user ids (numbers), got %v", payload["appliers"])
	}
	if payload["daily_cap"] != float64(5) || payload["expires_at"] != "2026-10-20T00:00:00Z" {
		t.Errorf("cap and expiry: %v %v", payload["daily_cap"], payload["expires_at"])
	}
	if got := stringSlice(payload["excluded_globs"]); !slices.Equal(got, []string{"docs/legal/**"}) {
		t.Errorf("excluded globs: %v", got)
	}
	if !strings.Contains(out, str(rec, "external_id")) {
		t.Errorf("the gate id must be printed:\n%s", out)
	}
	// DL-12: the next step names both ways a workspace admin answers it.
	if !strings.Contains(out, "process lane approve "+str(rec, "external_id")) || !strings.Contains(out, "web app") {
		t.Errorf("authorize names both ways to answer it (process lane approve <id>, or the web app):\n%s", out)
	}
	if strings.Contains(out, "the CLI cannot") {
		t.Errorf("the CLI can answer it now (DL-12):\n%s", out)
	}
	if len(l.posts) != 1 {
		t.Errorf("authorize writes nothing but the gate, posted %d", len(l.posts))
	}
}

// ---------------------------------------------------------------- approve

// DL-12 (USER:2026-09-28): a workspace admin answers the lane authorization
// with the developer CLI through the server's lane_approve action, in one call.
func TestLaneApprovePostsOneLaneApproveWithTheText(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)
	l.gates["LANE-AUTH"] = map[string]any{"external_id": "LANE-AUTH", "gate_class": "human", "kind": "approval_request",
		"purpose": "lane_authorization", "exact_scope": []any{"system:1"}, "state": "open", "fingerprint": "fp-LANE-AUTH",
		"options": []any{map[string]any{"key": "approve", "label": "Authorize the small-change lane"}}}

	out, err := runRoot(t, "process", "lane", "approve", "LANE-AUTH", "--text", "approve the pilot lane")
	if err != nil {
		t.Fatalf("lane approve: %v\n%s", err, out)
	}
	if len(l.posts) != 1 {
		t.Fatalf("lane approve is one call, posted %d", len(l.posts))
	}
	body := l.posts[0].body
	if str(body, "action") != "lane_approve" {
		t.Fatalf("want the lane_approve action, posted %v", body)
	}
	rec, _ := body["record"].(map[string]any)
	if str(rec, "external_id") != "LANE-AUTH" {
		t.Errorf("the record names the gate, posted %v", rec)
	}
	if str(body, "text") != "approve the pilot lane" {
		t.Errorf("the text rides as the USER: decision, posted %v", body)
	}
	if _, forged := body["answer_channel"]; forged {
		t.Errorf("the channel is the server's to derive from the token, posted %v", body)
	}
	g := l.gates["LANE-AUTH"]
	if str(g, "state") != "answered" || !slices.Equal(stringSlice(g["chosen_option_keys"]), []string{"approve"}) {
		t.Errorf("the authorization is answered with approve: %v", g)
	}
	if !strings.Contains(out, "LANE-AUTH") || !strings.Contains(out, "approve") {
		t.Errorf("the answer is reported:\n%s", out)
	}
}

func TestLaneApproveRefusesANonLaneGateBeforeAnyWrite(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)
	l.gates["ENTRY-EPIC-X"] = map[string]any{"external_id": "ENTRY-EPIC-X", "gate_class": "human", "purpose": "entry",
		"state": "open", "exact_scope": []any{"EPIC-X"}}

	out, err := runRoot(t, "process", "lane", "approve", "ENTRY-EPIC-X")
	if err == nil {
		t.Fatalf("lane approve of an entry gate must refuse:\n%s", out)
	}
	if !strings.Contains(err.Error(), "lane_authorization") || !strings.Contains(err.Error(), "nothing was written") {
		t.Errorf("the refusal names the purpose and that nothing was written: %v", err)
	}
	if l.writes() != 0 {
		t.Errorf("refused before any write, sent %d", l.writes())
	}
}

func TestLaneApproveRefusesAnUnknownGateBeforeAnyWrite(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)

	if out, err := runRoot(t, "process", "lane", "approve", "LANE-AUTH-NOPE"); err == nil {
		t.Fatalf("lane approve of an unknown gate must refuse:\n%s", out)
	}
	if l.writes() != 0 {
		t.Errorf("refused before any write, sent %d", l.writes())
	}
}

func TestLaneAuthorizeReadsAFile(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)
	file := writePlanFile(t, "lane.json", `{"classes":["presentation"],"appliers":[3],"expires_at":"2026-10-01","daily_cap":2,"excluded_globs":[]}`)

	if out, err := runRoot(t, "process", "lane", "authorize", "--file", file, "--id", "LANE-AUTH-F"); err != nil {
		t.Fatalf("authorize --file: %v\n%s", err, out)
	}
	rec, _ := l.lanePosts("create")[0]["record"].(map[string]any)
	payload, _ := rec["raw_payload"].(map[string]any)
	if rec["external_id"] != "LANE-AUTH-F" || payload["expires_at"] != "2026-10-01T00:00:00Z" || payload["daily_cap"] != float64(2) {
		t.Errorf("the file's terms must ride the gate (a date expires at its UTC midnight), posted %v", rec)
	}
}

// The current authorization stays answered while it stands, so a successor
// takes the next id in the series instead of being refused.
func TestLaneAuthorizeTakesTheNextIDWhileTheCurrentOneStands(t *testing.T) {
	l := newLaneStore(t)
	l.seedAuthorization("LANE-AUTH")
	laneWorkspace(t, l)

	if out, err := runRoot(t, "process", "lane", "authorize", "--classes", "wording", "--appliers", "7", "--expires", "10d", "--cap", "3"); err != nil {
		t.Fatalf("a successor authorization: %v\n%s", err, out)
	}
	rec, _ := l.lanePosts("create")[0]["record"].(map[string]any)
	if rec["external_id"] != "LANE-AUTH-R2" {
		t.Errorf("the successor takes LANE-AUTH-R2, posted %v", rec["external_id"])
	}

	out, err := runRoot(t, "process", "lane")
	if err != nil || !strings.Contains(out, "* LANE-AUTH") || !strings.Contains(out, "LANE-AUTH-R2  open") {
		t.Errorf("process lane shows the current authorization and the open successor: %v\n%s", err, out)
	}
}

func TestLaneAuthorizeRefusesBeforeAnyWrite(t *testing.T) {
	l := newLaneStore(t)
	laneWorkspace(t, l)

	if _, err := runRoot(t, "process", "lane", "authorize", "--appliers", "7", "--expires", "2026-10-20", "--cap", "5"); err == nil || !strings.Contains(err.Error(), "--classes") {
		t.Errorf("a missing class list is refused naming --classes, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "authorize", "--classes", "wording", "--appliers", "seven", "--expires", "2026-10-20", "--cap", "5"); err == nil || !strings.Contains(err.Error(), "--appliers") {
		t.Errorf("a non-numeric applier is refused naming --appliers, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "authorize", "--classes", "wording", "--appliers", "7", "--expires", "next week", "--cap", "5"); err == nil || !strings.Contains(err.Error(), "--expires") {
		t.Errorf("an unreadable expiry is refused naming --expires, got %v", err)
	}
	if len(l.posts) != 0 {
		t.Fatalf("nothing may be posted for a refused authorization, got %d", len(l.posts))
	}

	// The server's refusal is printed as the server wrote it.
	_, err := runRoot(t, "process", "lane", "authorize", "--classes", "wording", "--appliers", "7", "--expires", "2026-10-20", "--cap", "12")
	if err == nil || !strings.Contains(err.Error(), "daily_cap must be an integer from 1 to 10") {
		t.Errorf("the server's refusal must reach the operator verbatim, got %v", err)
	}
}

// ---------------------------------------------------------------- lane_class

func TestAuthorUpdatePostsTheLaneClass(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "")
	laneWorkspace(t, l)

	if out, err := runRoot(t, "author", "update", laneSR, "--expected-fingerprint", "fp-seed-"+laneSR, "--lane-class", "wording"); err != nil {
		t.Fatalf("author update --lane-class: %v\n%s", err, out)
	}
	rec, _ := l.lanePosts("update")[0]["record"].(map[string]any)
	if rec["lane_class"] != "wording" {
		t.Errorf("--lane-class must post lane_class, posted %v", rec)
	}
}

// ---------------------------------------------------------------- review

func TestLaneReviewRecordsTheNarrowReviewAtTheSingleSRAggregate(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	laneWorkspace(t, l)

	if out, err := runRoot(t, "working-set", "pull", laneSR, "--for-review"); err != nil {
		t.Fatalf("working-set pull <SR> --for-review: %v\n%s", err, out)
	}
	stamp, _ := os.ReadFile(filepath.Join(workingSetDir, laneSR, contextFile))
	if !strings.Contains(string(stamp), "mode: review") || !strings.Contains(string(stamp), "scope: single_sr:"+laneSR) {
		t.Fatalf("the by-id review pull stamps a review context for the single SR:\n%s", stamp)
	}
	bundle, err := os.ReadFile(filepath.Join(workingSetDir, laneSR, reviewBundleFile))
	if err != nil || !strings.Contains(string(bundle), laneSR) || !strings.Contains(string(bundle), "wording") {
		t.Fatalf("the review pull writes REVIEW.md naming the SR and its lane class: %v\n%s", err, bundle)
	}
	if l.writes() != 0 {
		t.Fatalf("the review pull is a read, but it wrote %d", l.writes())
	}
	ctx := laneContextID(t, laneSR)

	out, err := runRoot(t, "process", "lane", "review", laneSR, "--file", writePlanFile(t, "review.json", laneReviewPass))
	if err != nil {
		t.Fatalf("lane review: %v\n%s", err, out)
	}
	traces := l.lanePosts("evaluate_trace")
	if len(traces) != 1 {
		t.Fatalf("the narrow review is one trace, got %d", len(traces))
	}
	rec, _ := traces[0]["record"].(map[string]any)
	if rec["purpose"] != "cold-review" || rec["transition"] != "PROPOSED->TODO" || rec["verdict"] != "PASS" {
		t.Errorf("want a cold-review PASS on PROPOSED->TODO, posted %v", rec)
	}
	if got := stringSlice(rec["exact_scope"]); !slices.Equal(got, []string{laneSR}) {
		t.Errorf("the narrow review names the SR alone, got %v", got)
	}
	if rec["fingerprint"] != l.aggregate(laneSR) {
		t.Errorf("the narrow review pins the SR's single_sr aggregate %s, posted %v", l.aggregate(laneSR), rec["fingerprint"])
	}
	if rec["review_context_id"] != ctx {
		t.Errorf("the narrow review carries the review pull's context %s, posted %v", ctx, rec["review_context_id"])
	}
	narrow := false
	for _, raw := range anySlice(rec["sources"]) {
		src, _ := raw.(map[string]any)
		if str(src, "kind") == "lane" && str(src, "ref") == "LANE:narrow" {
			narrow = true
		}
	}
	if !narrow {
		t.Errorf("the narrow review carries the {kind: lane, ref: LANE:narrow} source, posted %v", rec["sources"])
	}
	if !l.holds(laneSR) || str(l.sel[laneSR], "scope_kind") != "single_sr" {
		t.Errorf("the SR's aggregate is read by holding it as a single_sr piece, selections %v", l.selects)
	}
}

func TestLaneReviewRefusesBeforeAnyWrite(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	l.seedSR("REQ-LN-2", "")
	laneWorkspace(t, l)
	review := writePlanFile(t, "review.json", laneReviewPass)

	if _, err := runRoot(t, "process", "lane", "review", laneSR, "--file", review); err == nil || !strings.Contains(err.Error(), "carries no review-mode stamp") {
		t.Errorf("without a review pull the review is refused, got %v", err)
	}

	if _, err := runRoot(t, "working-set", "pull", laneSR, "--for-review"); err != nil {
		t.Fatal(err)
	}
	l.records[laneSR]["fingerprint"] = "fp-edited-after-the-pull"
	if _, err := runRoot(t, "process", "lane", "review", laneSR, "--file", review); err == nil || !strings.Contains(err.Error(), "changed since the review pull") {
		t.Errorf("an SR edited after the review pull is refused, got %v", err)
	}

	if _, err := runRoot(t, "working-set", "pull", "REQ-LN-2", "--for-review"); err != nil {
		t.Fatal(err)
	}
	if _, err := runRoot(t, "process", "lane", "review", "REQ-LN-2", "--file", review); err == nil || !strings.Contains(err.Error(), "lane_class") {
		t.Errorf("an SR with no lane class is refused naming lane_class, got %v", err)
	}
	if l.writes() != 0 {
		t.Fatalf("a refused review writes nothing, got %d writes", l.writes())
	}
}

// ---------------------------------------------------------------- enter

func TestLaneEnterPostsOneAdvanceByTheCurrentAuthorization(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	l.seedAuthorization("LANE-AUTH-OLD")
	l.seedAuthorization("LANE-AUTH-2")
	l.gates["LANE-AUTH-3"] = map[string]any{"purpose": "lane_authorization", "state": "open", "exact_scope": []any{"system:1"}}
	laneWorkspace(t, l)
	laneReviewed(t, l, laneSR)

	out, err := runRoot(t, "process", "lane", "enter", laneSR)
	if err != nil {
		t.Fatalf("lane enter: %v\n%s", err, out)
	}
	advances := l.lanePosts("advance")
	if len(advances) != 1 {
		t.Fatalf("enter posts one advance, got %d", len(advances))
	}
	body := advances[0]
	rec, _ := body["record"].(map[string]any)
	if rec["kind"] != "requirement" || rec["external_id"] != laneSR || body["to"] != "TODO" || body["expected"] != "PROPOSED" {
		t.Errorf("want PROPOSED->TODO of the SR, posted %v", body)
	}
	if body["lane_ref"] != "LANE-AUTH-2" {
		t.Errorf("the advance applies the newest answered authorization, posted lane_ref %v", body["lane_ref"])
	}
	if _, present := body["gate_ref"]; present {
		t.Errorf("a lane application carries no gate_ref, posted %v", body)
	}
	if l.status(laneSR) != "TODO" || !strings.Contains(out, "LANE-AUTH-2") {
		t.Errorf("the SR is TODO and the output names the authorization: %s\n%s", l.status(laneSR), out)
	}
}

func TestLaneEnterRefusesBeforeAnyWrite(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	l.seedSR("REQ-LN-2", "")
	laneWorkspace(t, l)

	// No narrow review.
	if _, err := runRoot(t, "process", "lane", "enter", laneSR); err == nil || !strings.Contains(err.Error(), "narrow review") {
		t.Errorf("an SR with no narrow review is refused, got %v", err)
	}
	// No lane class.
	if _, err := runRoot(t, "process", "lane", "enter", "REQ-LN-2"); err == nil || !strings.Contains(err.Error(), "lane_class") {
		t.Errorf("an SR with no lane class is refused naming lane_class, got %v", err)
	}
	// Reviewed, but no answered authorization.
	laneReviewed(t, l, laneSR)
	l.gates["LANE-AUTH-OPEN"] = map[string]any{"purpose": "lane_authorization", "state": "open", "exact_scope": []any{"system:1"}}
	if _, err := runRoot(t, "process", "lane", "enter", laneSR); err == nil || !strings.Contains(err.Error(), "no answered lane authorization") {
		t.Errorf("with no answered authorization the entry is refused, got %v", err)
	}
	if n := len(l.lanePosts("advance")); n != 0 {
		t.Fatalf("a refused entry posts no advance, got %d", n)
	}

	// The server's refusal is printed verbatim.
	l.seedAuthorization("LANE-AUTH-1")
	l.fail["applier:"+laneSR] = true
	if _, err := runRoot(t, "process", "lane", "enter", laneSR); err == nil || !strings.Contains(err.Error(), "is not an applier of `LANE-AUTH-1`") {
		t.Errorf("the server's applier refusal must reach the operator, got %v", err)
	}
	if l.status(laneSR) != "PROPOSED" {
		t.Errorf("a refused entry leaves the SR PROPOSED, got %s", l.status(laneSR))
	}
}

func TestLaneEnterReappliesAnEntrySRThatChanged(t *testing.T) {
	l := newLaneStore(t)
	l.seedSR(laneSR, "wording")
	l.seedAuthorization("LANE-AUTH-1")
	laneWorkspace(t, l)
	laneReviewed(t, l, laneSR)
	if _, err := runRoot(t, "process", "lane", "enter", laneSR); err != nil {
		t.Fatal(err)
	}

	// An edit after entry strands the SR until a new review and a re-application.
	l.records[laneSR]["fingerprint"] = "fp-edited-after-entry"
	laneReviewed(t, l, laneSR)
	out, err := runRoot(t, "process", "lane", "enter", laneSR)
	if err != nil {
		t.Fatalf("re-apply: %v\n%s", err, out)
	}
	reapply := l.lanePosts("lane_reapply")
	if len(reapply) != 1 {
		t.Fatalf("an entered SR that changed is re-applied with lane_reapply, got %d posts", len(reapply))
	}
	rec, _ := reapply[0]["record"].(map[string]any)
	if rec["external_id"] != laneSR || reapply[0]["lane_ref"] != "LANE-AUTH-1" {
		t.Errorf("lane_reapply names the SR and the authorization, posted %v", reapply[0])
	}
	if len(l.lanePosts("advance")) != 1 {
		t.Errorf("a re-application posts no second advance")
	}
	if l.entered[laneSR] != l.aggregate(laneSR) {
		t.Errorf("the re-application pins the SR's current aggregate")
	}
}
