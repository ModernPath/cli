package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// EPIC-CLI-023 — read back what the loop records. REQ-CROSS-430 (SCN-PULL-001):
// the by-id pull spells the selection both ways, never ignores --piece, and
// says which surfaces it searched when an id is not served.

func TestREQCROSS430PullWorkSelectionAliasRendersTheSelection(t *testing.T) {
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	env := wsEnv(t, wsServe(t, fx))

	for _, alias := range []string{"WORK-SELECTION", "WORK-SELECTION.md"} {
		if err := workingSetPull(env, []string{alias}, wsNow); err != nil {
			t.Fatalf("pull %s: %v", alias, err)
		}
		raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "WORK-SELECTION.md"))
		if err != nil {
			t.Fatalf("pull %s wrote no WORK-SELECTION.md: %v", alias, err)
		}
		if !strings.Contains(string(raw), "EPIC-B3") {
			t.Fatalf("pull %s rendered no selection:\n%s", alias, raw)
		}
	}
}

func TestREQCROSS430ByIdPullRefusesPieceInsteadOfIgnoringIt(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-A", "A")}, requirements: []any{wsReq("REQ-X", "X")}}
	env := wsEnv(t, wsServe(t, fx))
	wsPiece = "EPIC-A"
	t.Cleanup(func() { wsPiece = "" })

	err := workingSetPull(env, []string{"REQ-X"}, wsNow)
	if err == nil {
		t.Fatal("--piece on a by-id pull was honoured silently — it applies to pull --scope, push and check")
	}
	for _, want := range []string{"--piece", "pull --scope", "push", "check", "whole system"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must say %q, got: %v", want, err)
		}
	}
	if len(fx.requests) != 0 {
		t.Fatalf("the refusal must come before any request, got %v", fx.requests)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, workingSetDir, "REQ-X.md")); statErr == nil {
		t.Fatal("REQ-X.md was written despite the refusal")
	}
}

func TestREQCROSS430UnknownIdNamesTheFourSurfacesSearched(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-A", "A")}}
	env := wsEnv(t, wsServe(t, fx))

	out := captureOut(t, func() {
		if err := workingSetPull(env, []string{"NOPE-1"}, wsNow); err == nil {
			t.Error("an unknown id must fail the pull")
		}
	})
	for _, want := range []string{"NOPE-1", "not served by", "searched backlog, epics, requirements, gates"} {
		if !strings.Contains(out, want) {
			t.Errorf("the miss line must say %q, got:\n%s", want, out)
		}
	}
}

func TestREQCROSS430BacklogReadFailureIsReportedNotSwallowed(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-A", "A")}, backlogStatus: 500}
	env := wsEnv(t, wsServe(t, fx))

	// The other three surfaces still serve: an epic pulls.
	if err := workingSetPull(env, []string{"EPIC-A"}, wsNow); err != nil {
		t.Fatalf("a backlog failure must not stop an epic pull: %v", err)
	}
	// And a miss says the backlog surface could not be read, with the reason.
	out := captureOut(t, func() { _ = workingSetPull(env, []string{"BACKLOG-TOOL-9"}, wsNow) })
	for _, want := range []string{"searched backlog (unreadable: ", "backlog store unavailable", "epics, requirements, gates"} {
		if !strings.Contains(out, want) {
			t.Errorf("the miss line must say %q, got:\n%s", want, out)
		}
	}
}

func TestREQCROSS430PieceUsageNamesTheFlagNotTheWireParameter(t *testing.T) {
	for _, f := range []struct {
		name  string
		usage string
	}{
		{"working-set --piece", workingSetCmd.PersistentFlags().Lookup("piece").Usage},
		{"process --piece", processCmd.PersistentFlags().Lookup("piece").Usage},
	} {
		if strings.Contains(f.usage, "?scope=") {
			t.Errorf("%s usage names the wire parameter, not the flag: %q", f.name, f.usage)
		}
		if !strings.Contains(f.usage, "--piece") {
			t.Errorf("%s usage must name --piece and what it applies to: %q", f.name, f.usage)
		}
	}
}

// REQ-CROSS-426 (SCN-CRIT-001): each acceptance line carries the id and kind the
// store serves, so a criterion can be named in a finding or replaced through
// author update --criteria; a line without an id renders as before.

func TestREQCROSS426AcceptanceLinesCarryTheirIdAndKind(t *testing.T) {
	req := wsReq("REQ-ID-001", "With ids")
	req["criteria"] = []any{
		map[string]any{"external_id": "C-1", "kind": "criterion", "statement": "the list prints"},
		map[string]any{"statement": "a legacy criterion without an id"},
	}
	ur := wsUserReq("UR-ID-001", "With scenarios")
	ur["scenarios"] = []any{
		map[string]any{"external_id": "SCN-A", "kind": "scenario", "given": "a system", "when": "I run it", "then": "it prints"},
	}
	fx := &wsFixture{requirements: []any{req}, userRequirements: []any{ur}}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"REQ-ID-001", "UR-ID-001"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-ID-001.md"))
	got := string(raw)
	for _, want := range []string{
		"- C-1 (criterion) — the list prints\n",
		"- a legacy criterion without an id\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the SR acceptance block must carry %q, got:\n%s", want, got)
		}
	}
	raw, _ = os.ReadFile(filepath.Join(env.Root, workingSetDir, "UR-ID-001.md"))
	got = string(raw)
	if want := "- SCN-A (scenario) — GIVEN a system WHEN I run it THEN it prints\n"; !strings.Contains(got, want) {
		t.Errorf("the UR acceptance block must carry %q, got:\n%s", want, got)
	}
}

// The round-trip guard (F-CLI023-R1-05): the authoring pull renders the same
// prefixed lines, and a push with no edits still posts nothing — the block is
// compared against the store re-rendered through the same function.
func TestREQCROSS426PrefixedAcceptanceRoundTripsThroughPush(t *testing.T) {
	fx := scaffoldFixture()
	fx.requirements[0].(map[string]any)["criteria"] = []any{
		map[string]any{"external_id": "C-310-1", "kind": "criterion", "given": "g", "when": "w", "then": "t"},
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull --scope: %v", err)
	}
	got := readScopeFile(t, filepath.Join(scopeDir(env), "members", "REQ-CROSS-310.md"))
	if !strings.Contains(got, "C-310-1 (criterion) — GIVEN g WHEN w THEN t") {
		t.Fatalf("the authoring pull must carry the prefixed line:\n%s", got)
	}
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("an unedited prefixed acceptance block must round-trip without a post, got %v", fx.authorPosts)
	}
}

// REQ-CROSS-425 (SCN-TRACE-001): a trace gate renders in full — verdict, who or
// what evaluated it and when, the git revision it was recorded at, the
// fingerprint it is pinned to and its class, scope, purpose, transition and
// prerequisites (`none` when empty); -v prints any gate's body. The answered
// gate renders as today (factory_gates_test.go pins that).

func readbackTraceGate() map[string]any {
	return map[string]any{
		"external_id": "TRACE-LOWER-REQ-X", "kind": "trace", "gate_class": "trace", "title": "Lower trace for REQ-X",
		"state": "pass", "verdict": "pass", "purpose": "lower", "transition": "build->verify",
		"evaluator_kind": "agent", "evaluator_agent_slug": "modernpath-author",
		"evaluated_at": "2026-09-21T10:00:00Z", "application_revision": "abc1234",
		"evaluated_scope_fingerprint":    strings.Repeat("c", 64),
		"exact_scope":                    []any{"REQ-X"},
		"prerequisite_gate_external_ids": []any{},
		"body_md":                        "recorded by process advance",
		"fingerprint":                    strings.Repeat("d", 64),
	}
}

func TestREQCROSS425FactoryGatesRendersATraceInFull(t *testing.T) {
	fx := &wsFixture{gate: readbackTraceGate()}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "TRACE-LOWER-REQ-X", &out, &errOut); err != nil {
		t.Fatalf("show: %v", err)
	}
	s := out.String()
	for _, want := range []string{
		"verdict: pass",
		"evaluated by agent modernpath-author at 2026-09-21T10:00:00Z",
		"recorded at: abc1234",
		"pinned to: " + strings.Repeat("c", 64) + " (content hash)",
		"scope: REQ-X",
		"purpose: lower",
		"transition: build->verify",
		"prerequisites: none",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("a trace must render %q, got:\n%s", want, s)
		}
	}
	if strings.Contains(s, "recorded by process advance") {
		t.Errorf("the body prints only under -v:\n%s", s)
	}
}

func TestREQCROSS425ColdReviewTraceIsPinnedToThePacketAggregate(t *testing.T) {
	g := readbackTraceGate()
	g["external_id"], g["purpose"], g["transition"] = "CR-TRACE-EPIC-X", "cold_review", "cold_review->entry"
	fx := &wsFixture{gate: g}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "CR-TRACE-EPIC-X", &out, &errOut); err != nil {
		t.Fatalf("show: %v", err)
	}
	if want := "(packet aggregate)"; !strings.Contains(out.String(), want) {
		t.Errorf("a cold-review trace is pinned to the packet aggregate, got:\n%s", out.String())
	}
}

func TestREQCROSS425VerbosePrintsAnyGatesBody(t *testing.T) {
	fx := &wsFixture{gate: req109AnsweredGate()}
	fx.gate["body_md"] = "## Brief\n\nthe decision brief"
	env := wsEnv(t, wsServe(t, fx))
	verbose = true
	t.Cleanup(func() { verbose = false })
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "APPROVE-EPIC-X", &out, &errOut); err != nil {
		t.Fatalf("show: %v", err)
	}
	if !strings.Contains(out.String(), "the decision brief") {
		t.Errorf("-v must print the gate's body, got:\n%s", out.String())
	}
}

func TestREQCROSS425PulledTraceBlockCarriesTheSameFieldsAndNone(t *testing.T) {
	fx := &wsFixture{requirements: []any{wsReq("REQ-X", "X")}, gates: []any{readbackTraceGate()}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"TRACE-LOWER-REQ-X"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "TRACE-LOWER-REQ-X.md"))
	got := string(raw)
	for _, want := range []string{
		"**Verdict:** pass",
		"**Evaluated by:** agent modernpath-author at 2026-09-21T10:00:00Z",
		"**Recorded at:** abc1234",
		"**Pinned to:** " + strings.Repeat("c", 64) + " (content hash)",
		"**Scope:** REQ-X",
		"**Purpose / transition:** lower / build->verify",
		"**Prerequisites:** none",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the pulled trace block must carry %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Prerequisites:** "+notServed) {
		t.Errorf("an empty prerequisite list is served and reads none, never not served:\n%s", got)
	}
}

// REQ-CROSS-428 (SCN-DOMAIN-001): --domain composes with --queue and --more —
// filter first, then slice — the header names the filter, an empty result says
// so, and without --domain the queue is unchanged.

func readbackDomainFeed() map[string]any {
	fx := feedFixture()
	var items []any
	for i := 1; i <= 7; i++ {
		items = append(items, feedItem("ready", "REQ-A-"+strconvItoa(i), "a item", []any{scoreTerm("preset", "ready")}, nil, []any{bareDomain("a")}))
	}
	for i := 1; i <= 3; i++ {
		items = append(items, feedItem("ready", "REQ-B-"+strconvItoa(i), "b item", []any{scoreTerm("preset", "ready")}, nil, []any{bareDomain("b")}))
	}
	fx["items"] = items
	return fx
}

func TestREQCROSS428DomainComposesWithQueue(t *testing.T) {
	out, err := runBrief(t, &wsFixture{feed: readbackDomainFeed()}, briefOpts{domain: "a", queue: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 7; i++ {
		want(t, out, "REQ-A-"+strconvItoa(i))
	}
	if strings.Contains(out, "REQ-B-") {
		t.Fatalf("--domain a --queue printed a b item:\n%s", out)
	}
	want(t, out, "· domain: a")
}

func TestREQCROSS428DomainComposesWithMore(t *testing.T) {
	out, err := runBrief(t, &wsFixture{feed: readbackDomainFeed()}, briefOpts{domain: "a", more: true})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "REQ-A-6")
	want(t, out, "REQ-A-7")
	for _, id := range []string{"REQ-A-1", "REQ-A-5", "REQ-B-1"} {
		if strings.Contains(out, id) {
			t.Fatalf("--domain a --more must print only the next five of a, printed %s:\n%s", id, out)
		}
	}
}

func TestREQCROSS428QueueWithoutDomainIsUnchangedAndAnEmptyDomainSaysSo(t *testing.T) {
	out, err := runBrief(t, &wsFixture{feed: readbackDomainFeed()}, briefOpts{queue: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"REQ-A-7", "REQ-B-3"} {
		want(t, out, id)
	}
	if strings.Contains(out, "· domain:") {
		t.Fatalf("no --domain, so the header names no filter:\n%s", out)
	}

	out, err = runBrief(t, &wsFixture{feed: readbackDomainFeed()}, briefOpts{domain: "zzz", queue: true})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "nothing in zzz is in scope")
}

func strconvItoa(i int) string { return strconv.Itoa(i) }

// REQ-CROSS-427 (SCN-HOLD-001, CLI half): the pull lists a gate under an item
// its exact scope or title names, as the server associates it
// (gate_names_record?/4) — not only by holds and id embedding.

func TestREQCROSS427PullAssociatesAGateByExactScopeAndTitle(t *testing.T) {
	scoped := map[string]any{"external_id": "D-Q-1", "kind": "question", "title": "Which release backfills?",
		"state": "open", "exact_scope": []any{"EPIC-A", "REQ-Y-1"}}
	titled := map[string]any{"external_id": "D-Q-2", "kind": "question", "title": "Is REQ-Y-1 worth a second pass?",
		"state": "open", "exact_scope": []any{}}
	neighbour := map[string]any{"external_id": "D-Q-3", "kind": "question", "title": "REQ-Y-10 needs an owner",
		"state": "open", "exact_scope": []any{"REQ-Y-10"}}
	fx := &wsFixture{
		requirements: []any{wsReq("REQ-Y-1", "Y")},
		gates:        []any{scoped, titled, neighbour},
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"REQ-Y-1"}, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-Y-1.md"))
	got := string(raw)
	for _, want := range []string{"## GATE D-Q-1", "## GATE D-Q-2"} {
		if !strings.Contains(got, want) {
			t.Errorf("a gate naming REQ-Y-1 in its scope or title must list under its Gates (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, "## GATE D-Q-3") {
		t.Errorf("REQ-Y-1 must not claim REQ-Y-10's gate (the server's own prefix-collision rule):\n%s", got)
	}
	if strings.Contains(got, "No gates reference this item") {
		t.Errorf("the Gates section must not read empty:\n%s", got)
	}
}

// REQ-CROSS-429 (SCN-SEL-001, CLI half): --resume --claim posts the claim; the
// suspended table says whose each parked piece is, or `claimable` when the
// read serves no holder.

func TestREQCROSS429ResumeClaimPostsTheClaim(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetSelect(env, wsSelectOpts{resume: true, scope: "EPIC-X", claim: true}, wsNow); err != nil {
		t.Fatalf("select --resume --claim: %v", err)
	}
	if fx.lastSelectPost["resume"] != "EPIC-X" || fx.lastSelectPost["claim"] != true {
		t.Fatalf("want {resume: EPIC-X, claim: true}, posted %v", fx.lastSelectPost)
	}

	fx.lastSelectPost = nil
	if err := workingSetSelect(env, wsSelectOpts{resume: true, scope: "EPIC-X"}, wsNow); err != nil {
		t.Fatalf("select --resume: %v", err)
	}
	if _, sent := fx.lastSelectPost["claim"]; sent {
		t.Fatalf("without --claim no claim key is posted, posted %v", fx.lastSelectPost)
	}

	err := workingSetSelect(env, wsSelectOpts{scope: "EPIC-X", claim: true}, wsNow)
	if err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("--claim without --resume must be refused naming --resume, got %v", err)
	}
}

func TestREQCROSS429SuspendedTableNamesTheHolderOrClaimable(t *testing.T) {
	payload := wsSelectionPayload()
	payload["suspended"] = []any{
		map[string]any{"scope_external_id": "EPIC-P1", "suspended_status": "blocked", "suspended_reason": "parked by other",
			"holder": map[string]any{"name": "Other", "email": "other@example.com"}},
		map[string]any{"scope_external_id": "EPIC-P2", "suspended_status": "blocked", "suspended_reason": "holder gone",
			"holder": nil},
		map[string]any{"scope_external_id": "EPIC-P3", "suspended_status": "deferred", "suspended_reason": "owner text only",
			"owner": "pasi", "holder": nil},
		// PR #618 review (finding 2): a holder the member list cannot name is
		// still a holder — the server refuses a plain resume — so the row
		// reads the id, never claimable.
		map[string]any{"scope_external_id": "EPIC-P4", "suspended_status": "blocked", "suspended_reason": "parker left",
			"holder": map[string]any{"user_id": 42, "name": nil, "email": nil}},
	}
	fx := &wsFixture{workSelection: payload}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "WORK-SELECTION.md"))
	got := string(raw)
	for _, want := range []string{
		"| EPIC-P1 | blocked | — | parked by other | Other (other@example.com) |",
		"| EPIC-P2 | blocked | — | holder gone | claimable |",
		"| EPIC-P3 | deferred | — | owner text only | claimable |",
		"| EPIC-P4 | blocked | — | parker left | user #42 |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the suspended table must carry %q, got:\n%s", want, got)
		}
	}
}

// PR #618 review (finding 3): under a question gate the brief must not say
// `holds` for the items its scope names — the server's chip says `names N`.
func TestREQCROSS427BriefSaysNamesNotHoldsForAQuestion(t *testing.T) {
	fx := feedFixture()
	q := feedItem("decision", "D-Q-9", "Which release backfills?",
		[]any{scoreTerm("preset", "decision"), scoreTerm("dependency", "names 2")},
		[]any{hold("REQ-S-1", "one"), hold("REQ-S-2", "two")}, []any{bareDomain("sync")})
	q["gate_kind"] = "question"
	fx["items"] = []any{q}
	out, err := runBrief(t, &wsFixture{feed: fx}, briefOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "names REQ-S-1 (one) · REQ-S-2 (two)")
	if strings.Contains(out, "holds REQ-S-1") {
		t.Fatalf("a question holds nothing; the brief said it does:\n%s", out)
	}
}
