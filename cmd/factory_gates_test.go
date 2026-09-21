package cmd

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// REQ-CROSS-101 — a bare total reads as an approval backlog when almost none of
// it is approvals. The queue mixes sign-off with questions and product
// decisions: different work, different cadence, one number.
func openGate(id, kind string) any {
	return map[string]any{"external_id": id, "kind": kind, "title": id}
}

func mixedQueue() []any {
	return []any{
		openGate("Q-1", "question"), openGate("Q-2", "question"), openGate("Q-3", "question"),
		openGate("D-1", "decision"),
		openGate("A-1", "approval_request"), openGate("A-2", "approval_request"),
	}
}

func TestGateFooterNamesEachKindAndItsCount(t *testing.T) {
	footer := gateQueueFooter(6, gateKindBreakdown(mixedQueue()), "")

	for _, want := range []string{"3 question", "2 approval_request", "1 decision"} {
		if !strings.Contains(footer, want) {
			t.Fatalf("the footer must break the queue down by kind — missing %q in %q", want, footer)
		}
	}
}

func TestGateFooterSaysWhatAFilterNarrowedFrom(t *testing.T) {
	footer := gateQueueFooter(2, gateKindBreakdown(mixedQueue()), "approval_request")

	if !strings.Contains(footer, "2") {
		t.Fatalf("the filtered count must be shown: %q", footer)
	}
	if !strings.Contains(footer, "6") {
		t.Fatalf("a filtered view must still name the unfiltered total, or it hides the queue: %q", footer)
	}
}

// The queue is not clear — it has 6 gates, none of the requested kind. Saying
// "clear" would be the same misreport one level down.
func TestGateFooterOnAKindWithNoOpenGatesDoesNotReadAsAClearQueue(t *testing.T) {
	footer := gateQueueFooter(0, gateKindBreakdown(mixedQueue()), "roadblock")

	if strings.Contains(footer, "queue is clear") {
		t.Fatalf("an empty filter result must not claim the queue is clear: %q", footer)
	}
	if !strings.Contains(footer, "roadblock") {
		t.Fatalf("it must name the kind that matched nothing: %q", footer)
	}
	if !strings.Contains(footer, "6") {
		t.Fatalf("it must still say how many are actually waiting: %q", footer)
	}
}

func TestGatesCommandOffersAKindFlag(t *testing.T) {
	if factoryGatesCmd.Flags().Lookup("kind") == nil {
		t.Fatal("the approval queue can only be isolated by grepping — there is no --kind flag")
	}
}

// Guards — these describe the filter helper written with the seam, so they are
// expected green from the start. They are what stop the filter from silently
// dropping gates or from turning an unfiltered call into a filtered one.
func TestFilterWithNoKindReturnsTheWholeQueue(t *testing.T) {
	if got := filterGatesByKind(mixedQueue(), ""); len(got) != 6 {
		t.Fatalf("an unfiltered call must return every gate, got %d", len(got))
	}
}

func TestFilterReturnsOnlyTheRequestedKind(t *testing.T) {
	got := filterGatesByKind(mixedQueue(), "approval_request")
	if len(got) != 2 {
		t.Fatalf("want the 2 approval requests, got %d", len(got))
	}
	for _, g := range got {
		if g.(map[string]any)["kind"] != "approval_request" {
			t.Fatalf("filter leaked another kind: %v", g)
		}
	}
}

func TestBreakdownOrderIsStable(t *testing.T) {
	a := gateKindBreakdown(mixedQueue())
	b := gateKindBreakdown(mixedQueue())
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("breakdown order is not stable: %v vs %v", a, b)
		}
	}
	if a[0].kind != "question" || a[0].n != 3 {
		t.Fatalf("largest kind must lead: %v", a)
	}
}

// REQ-CROSS-109 — a gate's state is readable from the CLI, so "answered" and
// "never existed" are different observations rather than the same empty output.
// The seam is factoryGatesRun(env, state, kind, jsonOut, id, out, errOut); both
// writers are injected so the stdout-only claim under --json is assertable.

func req109AnsweredGate() map[string]any {
	return map[string]any{
		"external_id": "APPROVE-EPIC-X", "kind": "approval_request", "title": "Approve: EPIC-X",
		"state": "answered", "answer": "Ok, specs approved. Continue.",
		"chosen_option_keys": []any{"approve"},
		"source_tag":         "USER:2026-09-06", "answerer_name": "Jussi Rajala",
		"answered_at": "2026-09-06T10:00:00Z", "applied_state": "pending",
	}
}

func req109ClosedGate() map[string]any {
	return map[string]any{
		"external_id": "APPROVE-EPIC-Y", "kind": "approval_request", "title": "Approve: EPIC-Y",
		"state": "closed", "answer": "approved",
		"chosen_option_keys": []any{"approve"},
		"source_tag":         "USER:2026-09-05", "answerer_name": "Jussi Rajala",
		"answered_at": "2026-09-05T10:00:00Z", "applied_state": "applied",
	}
}

func TestREQCROSS109GatesOffersAStateFlag(t *testing.T) {
	if factoryGatesCmd.Flags().Lookup("state") == nil {
		t.Fatal("an answered gate can only be reached by grepping the event stream — there is no --state flag")
	}
}

func TestREQCROSS109ShowRendersAnAnsweredGateWithSourceAndAnswerer(t *testing.T) {
	fx := &wsFixture{gate: req109AnsweredGate()}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "APPROVE-EPIC-X", &out, &errOut); err != nil {
		t.Fatalf("show: %v", err)
	}
	s := out.String()
	for _, want := range []string{"answered", "Ok, specs approved. Continue.", "USER:2026-09-06", "Jussi Rajala", "2026-09-06T10:00:00Z", "pending"} {
		if !strings.Contains(s, want) {
			t.Fatalf("by-id show must include %q, got:\n%s", want, s)
		}
	}
	if !strings.Contains(fx.lastGatesQuery, "system_id=4") {
		t.Fatalf("the by-id request must carry system_id, got query %q", fx.lastGatesQuery)
	}
}

// req109NamelessAnswererGate — §297.7: a real user (FK-valid) carrying no
// membership name, so the server serves a numeric answerer_user_id and
// answerer_name:null. The numeric id must stay visible, not collapse to
// "answered by human". The int literal round-trips the httptest server and
// reaches gateAnswerer as a float64 — the type str() silently drops.
func req109NamelessAnswererGate() map[string]any {
	return map[string]any{
		"external_id": "APPROVE-EPIC-Z", "kind": "approval_request", "title": "Approve: EPIC-Z",
		"state": "answered", "answer": "approved", "source_tag": "USER:2026-09-06",
		"answerer_kind": "human", "answerer_user_id": 42,
		"answered_at": "2026-09-06T10:00:00Z", "applied_state": "pending",
	}
}

// REQ-CROSS-109 / §297.7 — the by-id detail keeps an unresolved answerer's
// numeric user id (a superuser with no membership row) rather than discarding
// it down to a bare "answered by human".
func TestREQCROSS109AnUnresolvedAnswererKeepsTheNumericUserID(t *testing.T) {
	fx := &wsFixture{gate: req109NamelessAnswererGate()}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "APPROVE-EPIC-Z", &out, &errOut); err != nil {
		t.Fatalf("show: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "answered by user #42") {
		t.Fatalf("an unresolved answerer must keep its numeric id visible (user #42), got:\n%s", s)
	}
	if strings.Contains(s, "answered by human\n") {
		t.Fatalf("the numeric id must not collapse to a bare kind, got:\n%s", s)
	}
}

// REQ-CROSS-109 / §297.7 — the --state history path keeps the same numeric id.
func TestREQCROSS109HistoryKeepsTheNumericUserID(t *testing.T) {
	fx := &wsFixture{answeredGates: []any{req109NamelessAnswererGate()}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "answered", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("history: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "answered by user #42") {
		t.Fatalf("history must keep the numeric id (user #42), got:\n%s", s)
	}
}

func TestREQCROSS109AClosedGateShowsItsAnswerAndListsUnderAll(t *testing.T) {
	closed := req109ClosedGate()
	fx := &wsFixture{gate: closed, closedGates: []any{closed}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "APPROVE-EPIC-Y", &out, &errOut); err != nil {
		t.Fatalf("show closed: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "closed") || !strings.Contains(s, "approved") || !strings.Contains(s, "applied") {
		t.Fatalf("a closed gate must show its answer and applied state, got:\n%s", s)
	}
	out.Reset()
	errOut.Reset()
	if err := factoryGatesRun(env, "all", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("list all: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "APPROVE-EPIC-Y") || !strings.Contains(s, "closed") {
		t.Fatalf("--state all must list the closed gate with its stored state, got:\n%s", s)
	}
}

func TestREQCROSS109ShowDistinguishesNotFoundFromAnswered(t *testing.T) {
	// A served "no such gate" is absence: the id is named, the exit is non-zero.
	env := wsEnv(t, wsServe(t, &wsFixture{}))
	var out, errOut bytes.Buffer
	err := factoryGatesRun(env, "", "", false, "NOPE", &out, &errOut)
	if err == nil {
		t.Fatal("a missing gate must be a non-zero exit")
	}
	if !strings.Contains(err.Error(), "NOPE") || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("absence must name the id and say it does not exist, got %v", err)
	}

	// A routeless 404 (a server without the route: Phoenix's Not Found) is an
	// error, never read as absence.
	fx2 := &wsFixture{gatesStatus: 404, gatesBody: map[string]any{"errors": map[string]any{"detail": "Not Found"}}}
	env2 := wsEnv(t, wsServe(t, fx2))
	var out2, errOut2 bytes.Buffer
	err = factoryGatesRun(env2, "", "", false, "GATE-1", &out2, &errOut2)
	if err == nil {
		t.Fatal("a routeless 404 must be an error")
	}
	if strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("a routeless 404 must not be read as absence, got %v", err)
	}
	if !strings.Contains(err.Error(), "server 404") {
		t.Fatalf("a routeless 404 must be reported as server 404, got %v", err)
	}
}

func TestREQCROSS109AnEmptyAnsweredListingDoesNotSayTheQueueIsClear(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{}))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "answered", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("list answered: %v", err)
	}
	s := out.String()
	if strings.Contains(s, "queue is clear") {
		t.Fatalf("an empty answered listing must not claim the open queue is clear: %q", s)
	}
	if !strings.Contains(s, "answered") {
		t.Fatalf("an empty listing must name the state that matched nothing: %q", s)
	}
}

func TestREQCROSS109UnknownStateIsRefusedBeforeAnyRequest(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	err := factoryGatesRun(env, "bogus", "", false, "", &out, &errOut)
	if err == nil {
		t.Fatal("an unknown --state must be refused")
	}
	if fx.lastGatesQuery != "" {
		t.Fatalf("the refusal must happen before any request, but one was sent: %q", fx.lastGatesQuery)
	}
	for _, v := range []string{"open", "answered", "dismissed", "superseded", "all"} {
		if !strings.Contains(err.Error(), v) {
			t.Fatalf("the refusal must name the accepted vocabulary, missing %q in %v", v, err)
		}
	}
}

func TestREQCROSS109AServer422IsSurfacedNotRenderedEmpty(t *testing.T) {
	fx := &wsFixture{gatesStatus: 422, gatesBody: map[string]any{"error": "unknown state: whatever"}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	err := factoryGatesRun(env, "answered", "", false, "", &out, &errOut)
	if err == nil {
		t.Fatal("a server 422 must surface as an error, not an empty list")
	}
	if !strings.Contains(err.Error(), "422") {
		t.Fatalf("the 422 must be surfaced, got %v", err)
	}
	if strings.Contains(out.String(), "no answered gates") {
		t.Fatalf("a 422 must not be rendered as an empty listing: %q", out.String())
	}
}

func TestREQCROSS109JSONOwnsStdout(t *testing.T) {
	// list --json parses as {"gates":[…]}, nothing on errOut.
	fx := &wsFixture{gates: []any{wsGate("A-1", "approval_request")}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", true, "", &out, &errOut); err != nil {
		t.Fatalf("list --json: %v", err)
	}
	var listEnv struct {
		Gates []map[string]any `json:"gates"`
	}
	if err := json.Unmarshal(out.Bytes(), &listEnv); err != nil {
		t.Fatalf(`--json stdout must parse as {"gates":[…]}, got %q: %v`, out.String(), err)
	}
	if len(listEnv.Gates) != 1 || listEnv.Gates[0]["external_id"] != "A-1" {
		t.Fatalf("the gates array must carry the server's gates, got %v", listEnv.Gates)
	}
	if errOut.Len() != 0 {
		t.Fatalf("nothing may go to errOut on a clean --json list, got %q", errOut.String())
	}

	// empty list --json is {"gates":[]}, not null, with the banner suppressed.
	fxE := &wsFixture{}
	envE := wsEnv(t, wsServe(t, fxE))
	var outE, errE bytes.Buffer
	if err := factoryGatesRun(envE, "", "", true, "", &outE, &errE); err != nil {
		t.Fatalf("empty --json: %v", err)
	}
	var emptyEnv struct {
		Gates []map[string]any `json:"gates"`
	}
	if err := json.Unmarshal(outE.Bytes(), &emptyEnv); err != nil {
		t.Fatalf("empty --json must parse, got %q: %v", outE.String(), err)
	}
	if emptyEnv.Gates == nil || len(emptyEnv.Gates) != 0 {
		t.Fatalf(`empty --json must be {"gates":[]}, not null, got %q`, outE.String())
	}
	if strings.Contains(outE.String(), "queue is clear") {
		t.Fatalf("the banner must be suppressed under --json, got %q", outE.String())
	}

	// by-id --json is {"gate":{…}}.
	fxG := &wsFixture{gate: req109AnsweredGate()}
	envG := wsEnv(t, wsServe(t, fxG))
	var outG, errG bytes.Buffer
	if err := factoryGatesRun(envG, "", "", true, "APPROVE-EPIC-X", &outG, &errG); err != nil {
		t.Fatalf("by-id --json: %v", err)
	}
	var gateEnv struct {
		Gate map[string]any `json:"gate"`
	}
	if err := json.Unmarshal(outG.Bytes(), &gateEnv); err != nil {
		t.Fatalf(`by-id --json must parse as {"gate":{…}}, got %q: %v`, outG.String(), err)
	}
	if gateEnv.Gate["external_id"] != "APPROVE-EPIC-X" {
		t.Fatalf("the gate object must be the served gate, got %v", gateEnv.Gate)
	}
}

func TestREQCROSS109ListingFlagsWithAnIdAreRefused(t *testing.T) {
	fx := &wsFixture{gate: req109AnsweredGate()}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "answered", "", false, "APPROVE-EPIC-X", &out, &errOut); err == nil {
		t.Fatal("--state with a gate id must be refused")
	}
	if err := factoryGatesRun(env, "", "approval_request", false, "APPROVE-EPIC-X", &out, &errOut); err == nil {
		t.Fatal("--kind with a gate id must be refused")
	}
	// --json combines with the by-id form.
	var o2, e2 bytes.Buffer
	if err := factoryGatesRun(env, "", "", true, "APPROVE-EPIC-X", &o2, &e2); err != nil {
		t.Fatalf("--json with an id must be allowed: %v", err)
	}
}

// Guards — expected green from the start; they hold the "unchanged" claim.

func TestREQCROSS109DefaultListingSendsNoStateParameter(t *testing.T) {
	fx := &wsFixture{gates: []any{wsGate("A-1", "approval_request")}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("default listing: %v", err)
	}
	if strings.Contains(fx.lastGatesQuery, "state=") {
		t.Fatalf("the default listing must send no state parameter, got query %q", fx.lastGatesQuery)
	}
	out.Reset()
	errOut.Reset()
	if err := factoryGatesRun(env, "open", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("--state open: %v", err)
	}
	if !strings.Contains(fx.lastGatesQuery, "state=open") {
		t.Fatalf("an explicit --state open must send state=open, got query %q", fx.lastGatesQuery)
	}
}

func TestREQCROSS109DefaultRenderingIsUnchanged(t *testing.T) {
	fx := &wsFixture{gates: []any{
		map[string]any{"external_id": "A-1", "kind": "approval_request", "title": "Approve one", "state": "open"},
		map[string]any{"external_id": "Q-1", "kind": "question", "title": "A question", "state": "open"},
	}}
	env := wsEnv(t, wsServe(t, fx))
	var out, errOut bytes.Buffer
	if err := factoryGatesRun(env, "", "", false, "", &out, &errOut); err != nil {
		t.Fatalf("default render: %v", err)
	}
	want := "\nA-1  [approval_request]  Approve one\n" +
		"\nQ-1  [question]  A question\n" +
		gateQueueFooter(2, gateKindBreakdown(fx.gates), "")
	if out.String() != want {
		t.Fatalf("default rendering changed.\n got: %q\nwant: %q", out.String(), want)
	}

	// The empty open queue keeps its banner.
	envE := wsEnv(t, wsServe(t, &wsFixture{}))
	var outE, errE bytes.Buffer
	if err := factoryGatesRun(envE, "", "", false, "", &outE, &errE); err != nil {
		t.Fatalf("empty render: %v", err)
	}
	if outE.String() != "✓ no open gates — the queue is clear\n" {
		t.Fatalf("empty open-queue banner changed: %q", outE.String())
	}
}
