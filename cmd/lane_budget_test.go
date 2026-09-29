package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

// REQ-CROSS-458 (EPIC-RDD-LANE): the lane's saving is held by a test. A
// one-line defect is taken from its record to IN_REVIEW through the lane
// against the stateful fake store, through cobra, the way an agent runs the
// verbs the mp-process-cli skill gives: record it with its lane class, record
// the narrow review, enter it by the authorization, record its RED and
// passing runs, advance it. The review pull the reviewer reads from is a read
// and is counted apart; every other invocation must be one of at most five.

const laneBudgetSR = "REQ-LN-9"

type laneRun struct {
	t      *testing.T
	store  *laneStore
	skill  string
	writes int // invocations that write
	reads  int // read-only invocations (the review pull)
}

func (r *laneRun) invoke(read bool, args ...string) string {
	r.t.Helper()
	if read {
		r.reads++
	} else {
		r.writes++
	}
	n := 2
	if args[0] == "process" && len(args) > 2 && !strings.HasPrefix(args[2], "-") && (args[1] == "lane" || args[1] == "review") {
		n = 3
	}
	verb := strings.Join(args[:min(len(args), n)], " ")
	if !strings.Contains(r.skill, verb) {
		r.t.Errorf("the lane path runs `%s`, which the mp-process-cli skill never names", verb)
	}
	for _, a := range args {
		if a == "--file" || a == "--all" || a == "--for-review" {
			given := false
			for _, line := range strings.Split(r.skill, "\n") {
				if strings.Contains(line, verb) && strings.Contains(line, a) {
					given = true
				}
			}
			if !given {
				r.t.Errorf("the lane path runs `%s %s`, which the skill never gives", verb, a)
			}
		}
	}
	out, err := runRoot(r.t, args...)
	if err != nil {
		r.t.Fatalf("`modernpath %s` failed: %v\n%s\nunserved: %v", strings.Join(args, " "), err, out, r.store.unknown)
	}
	return out
}

const laneBudgetPlan = `requirements:
  - id: REQ-LN-9
    kind: sr
    context: LN
    title: The empty state names the missing repository
    description: The empty state shall say which repository is missing.
    rationale: The copy named no repository.
    boundary: modernpath-react/src/components/EmptyState.tsx
    verification_method: Vitest
    lane_class: defect_with_failing_test
    sources: ["USER:2026-09-28:empty-state-copy"]
`

func TestLaneBudgetOneLineDefectReachesInReviewInFiveInvocations(t *testing.T) {
	skill := readSkill(t)
	l := newLaneStore(t)
	l.seedAuthorization("LANE-AUTH-1")
	laneWorkspace(t, l)
	laneGit(t, "init", "-q", "-b", "main")
	laneGit(t, "commit", "-q", "--allow-empty", "-m", "red")
	red := laneGit(t, "rev-parse", "HEAD")
	laneGit(t, "commit", "-q", "--allow-empty", "-m", "green")

	r := &laneRun{t: t, store: l, skill: skill}
	r.invoke(false, "author", "apply", "--file", writePlanFile(t, "small.yaml", laneBudgetPlan))
	if l.records[laneBudgetSR]["lane_class"] != "defect_with_failing_test" {
		t.Fatalf("author apply must record the lane class, got %v", l.records[laneBudgetSR])
	}
	r.invoke(true, "working-set", "pull", laneBudgetSR, "--for-review")
	r.invoke(false, "process", "lane", "review", laneBudgetSR, "--file", writePlanFile(t, "review.json", laneReviewPass))
	r.invoke(false, "process", "lane", "enter", laneBudgetSR)
	runs, _ := json.Marshal([]map[string]any{
		{"fail": []string{laneBudgetSR}, "role": "RED", "revision": red, "log": "npx vitest run EmptyState"},
		{"pass": []string{laneBudgetSR}, "log": "npx vitest run EmptyState"},
	})
	r.invoke(false, "factory", "evidence", "--file", writePlanFile(t, "runs.json", string(runs)))
	r.invoke(false, "process", "advance", "--all", "--piece", laneBudgetSR, "--log", "npx vitest run")

	if got := l.status(laneBudgetSR); got != "IN_REVIEW" {
		t.Fatalf("the small change ends %s, not IN_REVIEW", got)
	}
	if l.entered[laneBudgetSR] == "" || l.narrowReview(laneBudgetSR) == "" {
		t.Errorf("it entered through the lane after a narrow review")
	}
	t.Logf("lane budget: %d invocations that write, %d read (the review pull); %d HTTP requests", r.writes, r.reads, l.calls)
	if r.writes > 5 {
		t.Errorf("the lane takes %d writing invocations from record to IN_REVIEW, more than five", r.writes)
	}
	if r.reads > 1 {
		t.Errorf("the lane reads %d times outside its verbs; the review pull is the only one", r.reads)
	}
}
