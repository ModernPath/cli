package cmd

import (
	"slices"
	"strings"
	"testing"
)

// REQ-CROSS-456 / REQ-CROSS-458 (EPIC-RDD-LANE): `process lane complete`
// opens one lane-batch gate over the IN_REVIEW small changes with a passing
// server eligibility at their delivered revision — each member's run at that
// revision and its completion trace at its own single_sr aggregate first, the
// files of each change in the brief — and `--apply` advances the members an
// answered batch approved, leaving the rejected ones IN_REVIEW.

// laneBatchWorld: A and B are delivered and eligible, C failed eligibility and
// D is still IN_PROGRESS. Every one entered through the lane and is held.
func laneBatchWorld(t *testing.T) (*laneStore, map[string]string) {
	t.Helper()
	l := newLaneStore(t)
	l.seedAuthorization("LANE-AUTH-1")
	laneWorkspace(t, l)
	laneGit(t, "init", "-q", "-b", "main")
	commits := map[string]string{}
	for _, id := range []string{"REQ-LN-A", "REQ-LN-B", "REQ-LN-C", "REQ-LN-D"} {
		l.seedSR(id, "wording")
		l.entered[id] = l.aggregate(id)
		l.setStatus(id, "IN_REVIEW")
		l.held = append(l.held, id)
		l.sel[id] = map[string]any{"scope_kind": "single_sr", "scope_external_id": id, "phase": "build", "members": []any{}}
		laneWrite(t, "src/"+id+".tsx", id+"\n")
		laneGit(t, "add", "src/"+id+".tsx")
		laneGit(t, "commit", "-q", "-m", "deliver "+id)
		commits[id] = laneGit(t, "rev-parse", "HEAD")
	}
	l.setStatus("REQ-LN-D", "IN_PROGRESS")
	for _, id := range []string{"REQ-LN-A", "REQ-LN-B", "REQ-LN-C"} {
		files := []any{"src/" + id + ".tsx"}
		if id == "REQ-LN-C" {
			files = append(files, "oidc-bff/main.go")
		}
		l.eligibility(map[string]any{"external_id": id, "commit": commits[id], "files": files})
	}
	return l, commits
}

func TestLaneCompleteOpensOneBatchOverTheEligibleSmallChanges(t *testing.T) {
	l, commits := laneBatchWorld(t)

	out, err := runRoot(t, "process", "lane", "complete", "--log", "https://ci.example/run/42")
	if err != nil {
		t.Fatalf("lane complete: %v\n%s\nunserved: %v", err, out, l.unknown)
	}
	members := []string{"REQ-LN-A", "REQ-LN-B"}

	// Each member's passing run at its own delivered revision.
	for _, id := range members {
		found := false
		for _, run := range l.evidence {
			for _, raw := range anySlice(run["results"]) {
				res, _ := raw.(map[string]any)
				if str(res, "target_external_id") == id && str(res, "result") == "pass" && str(res, "revision") == commits[id] {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("%s needs a passing run at its delivered revision %s, posted %v", id, commits[id], l.evidence)
		}
	}

	// Each member's completion trace at its own single_sr aggregate.
	traces := map[string]string{}
	for _, p := range l.lanePosts("evaluate_trace") {
		rec, _ := p["record"].(map[string]any)
		if rec["purpose"] != "completion" {
			continue
		}
		scope := stringSlice(rec["exact_scope"])
		if len(scope) != 1 || rec["transition"] != "IN_REVIEW->DONE" || rec["verdict"] != "PASS" || rec["fingerprint"] != l.aggregate(scope[0]) {
			t.Errorf("a member's completion trace names it alone at its aggregate, posted %v", rec)
		}
		traces[scope[0]] = str(rec, "external_id")
	}
	if len(traces) != 2 || traces["REQ-LN-A"] == "" || traces["REQ-LN-B"] == "" {
		t.Fatalf("one completion trace per eligible member, got %v", traces)
	}

	var gate map[string]any
	for _, p := range l.lanePosts("create") {
		if rec, _ := p["record"].(map[string]any); rec["purpose"] == "lane-batch" {
			gate = rec
		}
	}
	if gate == nil {
		t.Fatalf("no lane-batch gate was opened\n%s", out)
	}
	if got := stringSlice(gate["exact_scope"]); !slices.Equal(got, members) {
		t.Errorf("the batch names the eligible IN_REVIEW small changes only, got %v", got)
	}
	if gate["transition"] != "IN_REVIEW->DONE" {
		t.Errorf("the batch moves IN_REVIEW->DONE, posted %v", gate["transition"])
	}
	var keys []string
	for _, o := range anySlice(gate["options"]) {
		keys = append(keys, str(o.(map[string]any), "key"))
	}
	if !slices.Equal(keys, []string{"approve", "reject:REQ-LN-A", "reject:REQ-LN-B"}) {
		t.Errorf("options are approve plus reject:<SR> per member, got %v", keys)
	}
	prereq := stringSlice(gate["prerequisite_gate_external_ids"])
	slices.Sort(prereq)
	want := []string{traces["REQ-LN-A"], traces["REQ-LN-B"]}
	slices.Sort(want)
	if !slices.Equal(prereq, want) {
		t.Errorf("the batch names each member's completion trace, got %v want %v", prereq, want)
	}
	brief, _ := gate["brief"].(map[string]any)
	for _, f := range []string{"src/REQ-LN-A.tsx", "src/REQ-LN-B.tsx"} {
		if !strings.Contains(strings.Join(briefValues(brief), "\n"), f) {
			t.Errorf("the brief lists each change's files; %s is missing: %v", f, brief)
		}
	}
	for _, want := range []string{"REQ-LN-C", "REQ-LN-D", "answer it in the web app"} {
		if !strings.Contains(out, want) {
			t.Errorf("the output must name %q (the excluded changes and the hand-off):\n%s", want, out)
		}
	}
}

func briefValues(brief map[string]any) []string {
	var out []string
	for _, v := range brief {
		out = append(out, str(map[string]any{"v": v}, "v"))
	}
	return out
}

func TestLaneCompleteApplyAdvancesTheApprovedMembersOnly(t *testing.T) {
	l, _ := laneBatchWorld(t)
	if out, err := runRoot(t, "process", "lane", "complete", "--log", "ci-42"); err != nil {
		t.Fatalf("lane complete: %v\n%s", err, out)
	}
	var gateID string
	for id, g := range l.gates {
		if g["purpose"] == "lane-batch" {
			gateID = id
		}
	}
	l.answer(gateID, "approve", "reject:REQ-LN-B")

	out, err := runRoot(t, "process", "lane", "complete", "--apply")
	if err != nil {
		t.Fatalf("lane complete --apply: %v\n%s", err, out)
	}
	advances := l.lanePosts("advance")
	if len(advances) != 1 {
		t.Fatalf("only the approved member is advanced, got %d advances", len(advances))
	}
	body := advances[0]
	rec, _ := body["record"].(map[string]any)
	if rec["external_id"] != "REQ-LN-A" || body["to"] != "DONE" || body["expected"] != "IN_REVIEW" ||
		body["gate_ref"] != gateID || body["gate_fingerprint"] != str(l.gates[gateID], "fingerprint") || body["gate_answer"] != "approve" {
		t.Errorf("want REQ-LN-A IN_REVIEW->DONE on the batch's answer, posted %v", body)
	}
	if l.status("REQ-LN-A") != "DONE" || l.status("REQ-LN-B") != "IN_REVIEW" {
		t.Errorf("A is DONE and B stays IN_REVIEW, got %s and %s", l.status("REQ-LN-A"), l.status("REQ-LN-B"))
	}
	if !strings.Contains(out, "REQ-LN-B") || !strings.Contains(out, "rejected") {
		t.Errorf("the rejected member is named:\n%s", out)
	}
}

func TestLaneCompleteRefusesBeforeAnyWrite(t *testing.T) {
	l, _ := laneBatchWorld(t)
	before := l.writes()

	if _, err := runRoot(t, "process", "lane", "complete"); err == nil || !strings.Contains(err.Error(), "--log") {
		t.Errorf("opening a batch without --log is refused naming it, got %v", err)
	}
	if _, err := runRoot(t, "process", "lane", "complete", "--apply"); err == nil || !strings.Contains(err.Error(), "no answered lane-batch gate") {
		t.Errorf("--apply with no answered batch is refused, got %v", err)
	}
	if l.writes() != before {
		t.Fatalf("a refused completion writes nothing, got %d new writes", l.writes()-before)
	}
}
