package cmd

// REQ-CROSS-375 (EPIC-CLI-017): `process complete <scope>` — at the delivered
// revision — records one passing run naming the epic and its members, the
// completion trace at the packet aggregate, and the completion gate naming
// that trace, in that order and in one call; on a branch head, behind the
// delivered tip, with a member not IN_REVIEW, or without delivery facts it
// refuses naming the fact before any write.

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const completionBrief = `**Brief:**
- What: Accept EPIC-A as delivered.
- Why now: Merged and green.
- Changes if approved: The epic and its members read DONE.
- Risk if wrong: Low.
- Recommendation: approve.
`

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// deliveredRepo is a repository whose HEAD is the tip of origin/main — the
// remote refs are set by hand, so no network is involved (--no-fetch).
func deliveredRepo(t *testing.T) (root, tip string) {
	t.Helper()
	root = t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "one\n")
	tip = gitCommitAll(t, root, "one")
	gitRun(t, root, "update-ref", "refs/remotes/origin/main", tip)
	gitRun(t, root, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return root, tip
}

func completeFacts(scopeStatus string, members []map[string]any) map[string]any {
	return map[string]any{
		"aggregate":   pinAggregate,
		"scope":       map[string]any{"external_id": "EPIC-A", "kind": "epic", "status": scopeStatus},
		"members":     members,
		"cold_review": map[string]any{"verdict": "pass", "independent": true, "trace_external_id": "CR-A"},
		"sections":    map[string]any{"complete": true, "missing": []string{}},
	}
}

func completeServer(t *testing.T, facts map[string]any, serveFacts bool) *ceremonyServer {
	t.Helper()
	cs := newCeremonyServer(t, facts, serveFacts)
	cs.packetSections = []map[string]any{{"section_key": "completion_brief", "content": completionBrief}}
	return cs
}

func completeEnv(t *testing.T, cs *ceremonyServer, root string) *factoryEnv {
	t.Helper()
	return &factoryEnv{Root: root, APIURL: cs.srv.URL, SystemID: 4, token: "t"}
}

func TestDeliveredHeadAcceptsTheTip(t *testing.T) {
	root, tip := deliveredRepo(t)
	got, err := deliveredHead(root, true)
	if err != nil || got != tip {
		t.Fatalf("HEAD at the tip of origin/main is delivered, got %q, %v", got, err)
	}
}

func TestProcessCompleteRefusesOffTheDeliveredBranch(t *testing.T) {
	root, _ := deliveredRepo(t)
	gitRun(t, root, "checkout", "-q", "-b", "feature")
	writeFile(t, filepath.Join(root, "b.txt"), "two\n")
	gitCommitAll(t, root, "two")
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "not on the delivered branch") {
		t.Fatalf("a branch head is refused naming the delivered branch, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessCompleteRefusesBehindTheDeliveredTip(t *testing.T) {
	root, first := deliveredRepo(t)
	writeFile(t, filepath.Join(root, "b.txt"), "two\n")
	second := gitCommitAll(t, root, "two")
	gitRun(t, root, "update-ref", "refs/remotes/origin/main", second)
	gitRun(t, root, "checkout", "-q", first)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "behind") || !strings.Contains(err.Error(), "1 commit") {
		t.Fatalf("an ancestor of the tip is refused as behind, naming the distance, got %v", err)
	}
	assertNoWrites(t, cs)
}

// N-CLI017-R2-01: a failed fetch is a refusal, not a stale tip taken as current.
func TestProcessCompleteRefusesWhenFetchFails(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci"})
	if err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Fatalf("a repository with no reachable remote must refuse on the fetch, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessCompleteRunsTheCeremonyInOrder(t *testing.T) {
	root, tip := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW"), member("REQ-A-2", "DONE")}), true)
	env := completeEnv(t, cs, root)

	var err error
	out := captureOut(t, func() {
		err = processComplete(env, "EPIC-A", completeOpts{log: "https://ci/run/1", noFetch: true, body: "audit: nothing deferred"})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if got := strings.Join(cs.order, " "); got != "evidence author:evaluate_trace author:create work-selection" {
		t.Fatalf("the ceremony is evidence -> trace -> gate -> phase, got %q", got)
	}

	ev := cs.evidence[0]
	if ev["kind"] != "ci" || ev["sha"] != tip[:7] || ev["log_ref"] != "https://ci/run/1" {
		t.Fatalf("one ci run at the delivered revision, got %v", ev)
	}
	var passed []string
	for _, r := range ev["results"].([]any) {
		m := r.(map[string]any)
		if m["result"] == "pass" {
			passed = append(passed, m["target_external_id"].(string))
		}
	}
	// REQ-CROSS-419 (D4a): the run names the epic and every member this
	// completion moves; a DONE member an earlier completion accepted keeps its
	// current evidence and is not re-posted (it is still named by the trace).
	if strings.Join(passed, ",") != "EPIC-A,REQ-A-1" {
		t.Fatalf("the run names the epic and every IN_REVIEW member, never a DONE sibling, got %v", passed)
	}

	trace, _ := cs.authored[0]["record"].(map[string]any)
	if trace["external_id"] != "COMPLETE-TRACE-EPIC-A" || trace["purpose"] != "completion" ||
		trace["fingerprint"] != pinAggregate || trace["transition"] != "IN_REVIEW->DONE" || trace["verdict"] != "PASS" {
		t.Fatalf("the completion trace is pinned to the packet aggregate: %v", trace)
	}
	if scope := stringSlice(trace["exact_scope"]); strings.Join(scope, ",") != "EPIC-A,REQ-A-1,REQ-A-2" {
		t.Fatalf("the trace names the epic and its members, got %v", scope)
	}
	if !strings.Contains(str(trace, "body_md"), "audit: nothing deferred") || !strings.Contains(str(trace, "application_revision"), tip) {
		t.Fatalf("the trace carries the audit body and the delivered revision: %v", trace)
	}

	gate, _ := cs.authored[1]["record"].(map[string]any)
	if gate["external_id"] != "COMPLETE-EPIC-A" || gate["purpose"] != "completion" || gate["transition"] != "IN_REVIEW->DONE" || gate["gate_kind"] != "approval_request" {
		t.Fatalf("the completion gate shape is wrong: %v", gate)
	}
	if scope := stringSlice(gate["exact_scope"]); strings.Join(scope, ",") != "EPIC-A,REQ-A-1" {
		t.Fatalf("the gate names the epic plus the members still IN_REVIEW (a DONE member is not moved), got %v", scope)
	}
	if prereq := stringSlice(gate["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "COMPLETE-TRACE-EPIC-A" {
		t.Fatalf("the gate names the completion trace as its prerequisite, got %v", gate["prerequisite_gate_external_ids"])
	}
	if brief, _ := gate["brief"].(map[string]any); brief["what"] != "Accept EPIC-A as delivered." {
		t.Fatalf("the brief is read from the completion_brief section, got %v", gate["brief"])
	}
	if cs.selections[0]["phase"] != "completion" {
		t.Fatalf("the selection moves to phase completion, got %v", cs.selections)
	}
	if !strings.Contains(out, "members first") {
		t.Fatalf("the apply order is documented in the output: %q", out)
	}
}

func TestProcessCompleteRefusesAMemberNotInReview(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW"), member("REQ-A-2", "IN_PROGRESS")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "REQ-A-2") || !strings.Contains(err.Error(), "process advance") {
		t.Fatalf("a member not IN_REVIEW is named with its remedy, got %v", err)
	}
	assertNoWrites(t, cs)
}

// REQ-CROSS-431 (F-CLI024-R2-03): a user requirement not yet IN_REVIEW is
// named with its upper trace, without the pin or the transition the CLI reads.
func TestProcessCompleteNamesTheUpperTraceWithoutItsPin(t *testing.T) {
	root, _ := deliveredRepo(t)
	ur := member("UR-A", "IN_PROGRESS")
	ur["kind"] = "ur"
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW"), ur}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "UR-A") || !strings.Contains(err.Error(), "--purpose upper --scope <UR> --verdict PASS") {
		t.Fatalf("a UR not IN_REVIEW is named with its upper trace, got %v", err)
	}
	for _, typed := range []string{"--fingerprint", "--from", "--to"} {
		if strings.Contains(err.Error(), typed) {
			t.Errorf("the remedy must not ask for %s, which the CLI reads, got %v", typed, err)
		}
	}
	assertNoWrites(t, cs)
}

func TestProcessCompleteExcludesDeferredAndObsoleteMembers(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW"), member("REQ-A-2", "DEFERRED"), member("REQ-A-3", "OBSOLETE")}), true)
	if err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true}); err != nil {
		t.Fatalf("a DEFERRED or OBSOLETE member is neither pre-checked nor named: %v", err)
	}
	trace, _ := cs.authored[0]["record"].(map[string]any)
	gate, _ := cs.authored[1]["record"].(map[string]any)
	if s := strings.Join(stringSlice(trace["exact_scope"]), ","); s != "EPIC-A,REQ-A-1" {
		t.Fatalf("the trace scope excludes them, got %v", s)
	}
	if s := strings.Join(stringSlice(gate["exact_scope"]), ","); s != "EPIC-A,REQ-A-1" {
		t.Fatalf("the gate scope excludes them, got %v", s)
	}
	if body := str(trace, "body_md"); !strings.Contains(body, "REQ-A-2") || !strings.Contains(body, "REQ-A-3") {
		t.Fatalf("the excluded members are disclosed in the trace body: %q", body)
	}
}

func TestProcessCompleteRefusesWhenTheEpicIsNotInReview(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_PROGRESS", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "process reconcile --apply") {
		t.Fatalf("an epic not yet folded to IN_REVIEW names reconcile as the remedy, got %v", err)
	}
	assertNoWrites(t, cs)
}

// The server refuses a members-only completion gate for a done owner — "does
// not reach into a done, obsolete or deferred epic; demote or reopen the epic
// first" — for one stranded member or several. The CLI used to meet that
// refusal only after the delivered run and the completion trace were written,
// so the fact is checked here, before the first write, with the sanctioned
// reopen named.
func TestProcessCompleteRefusesADoneEpicWithStrandedMembersBeforeAnyWrite(t *testing.T) {
	for _, members := range [][]map[string]any{
		{member("REQ-A-1", "IN_REVIEW"), member("REQ-A-2", "DONE")},
		{member("REQ-A-1", "IN_REVIEW"), member("REQ-A-2", "IN_REVIEW")},
	} {
		root, _ := deliveredRepo(t)
		cs := completeServer(t, completeFacts("DONE", members), true)
		err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
		if err == nil {
			t.Fatal("a DONE epic with a member still IN_REVIEW must be refused, not posted")
		}
		for _, want := range []string{"REQ-A-1", "is DONE", "author demote EPIC-A --kind epic", "--basis defect", "process complete EPIC-A"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal must name %q, got %v", want, err)
			}
		}
		assertNoWrites(t, cs)
	}
}

// A DONE epic with nothing stranded is still the quiet no-op it was.
func TestProcessCompleteDoneEpicWithNothingStrandedStillReportsNothingToDo(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("DONE", []map[string]any{member("REQ-A-1", "DONE")}), true)
	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("a fully DONE epic is not an error: %v", err)
	}
	if !strings.Contains(out, "nothing to do") {
		t.Fatalf("a fully DONE epic reports nothing to do, got %q", out)
	}
	assertNoWrites(t, cs)
}

// `author advance --kind` defaults to requirement, so the one recipe the
// epilogue printed was refused 422 when it was pasted for the epic. The
// members and the epic get a line each.
func TestProcessCompleteEpilogueGivesTheEpicItsOwnKindEpicRecipe(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	})
	if err != nil {
		t.Fatalf("complete: %v\n%s", err, out)
	}
	if !strings.Contains(out, "author advance EPIC-A --kind epic --to DONE --expected IN_REVIEW --gate COMPLETE-EPIC-A") {
		t.Fatalf("the epic needs its own --kind epic recipe: %q", out)
	}
	if !strings.Contains(out, "members first (REQ-A-1)") {
		t.Fatalf("the member recipe stays, naming the members: %q", out)
	}
}

func TestProcessCompleteRefusesWithoutFacts(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, nil, false)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("a server without facts is refused before any write, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessCompleteRefusesWithoutLog(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{noFetch: true})
	if err == nil || !strings.Contains(err.Error(), "--log") {
		t.Fatalf("the delivered run's reference is required, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessCompleteDryRunPostsNothing(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	var err error
	out := captureOut(t, func() {
		err = processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true, dryRun: true})
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "COMPLETE-TRACE-EPIC-A") || !strings.Contains(out, "COMPLETE-EPIC-A") {
		t.Fatalf("the dry run prints the plan: %q", out)
	}
	assertNoWrites(t, cs)
}

// REQ-CROSS-419 (D4a) binds the posted run to the members this completion
// moves: a rerun after a refused gate posts no second identical run, because
// the completion trace at the aggregate is reused and the evidence it cited
// stands. What "stands" covers is read off that trace's own body, which names
// the targets its run recorded, so the fixture carries the body `process
// complete` writes (BACKLOG-TOOL-100).
func TestProcessCompleteReusesTheTraceWithoutRepostingEvidence(t *testing.T) {
	root, _ := deliveredRepo(t)
	cs := completeServer(t, completeFacts("IN_REVIEW", []map[string]any{member("REQ-A-1", "IN_REVIEW")}), true)
	cs.existingGates["COMPLETE-TRACE-EPIC-A"] = reusedTrace([]string{"EPIC-A", "REQ-A-1"})
	if err := processComplete(completeEnv(t, cs, root), "EPIC-A", completeOpts{log: "ci", noFetch: true}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(cs.evidence) != 0 {
		t.Fatalf("no evidence is re-posted when the completion trace is reused, got %v", cs.evidence)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("only the gate is created, got %v", cs.authored)
	}
	gate, _ := cs.authored[0]["record"].(map[string]any)
	if prereq := stringSlice(gate["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "COMPLETE-TRACE-EPIC-A" {
		t.Fatalf("the gate names the reused trace, got %v", gate["prerequisite_gate_external_ids"])
	}
}
