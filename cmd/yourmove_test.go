package cmd

// REQ-CROSS-276 (EPIC-NEXT-003) — RED first. `modernpath your-move` prints the
// personal brief from GET /api/v1/feed AFTER regenerating its GATES.md
// projection, with names beside ids and three CTA lines that never dead-end.
// Each test names the clause it pins from
// epics/EPIC-NEXT-003-personal-feed/specs/requirements.md §REQ-CROSS-276.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func feedItem(kind, id, title string, terms, holds, domains []any) map[string]any {
	return map[string]any{
		"kind": kind, "external_id": id, "title": title,
		"score_terms": terms, "holds": holds, "domains": domains,
	}
}

func scoreTerm(name, chip string) map[string]any { return map[string]any{"term": name, "chip": chip} }
func hold(id, title string) map[string]any {
	return map[string]any{"held_external_id": id, "title": title}
}
func ownedDomain(name, owner string) map[string]any {
	return map[string]any{"name": name, "owner": map[string]any{"name": owner}}
}
func bareDomain(name string) map[string]any {
	return map[string]any{"name": name, "owner": nil}
}

func feedFixture() map[string]any {
	items := []any{
		feedItem("decision", "RQ-268", "Board column semantics",
			[]any{scoreTerm("preset", "decision"), scoreTerm("dependency", "releases 3 · one blocks Pasi (in flow now)"), scoreTerm("ownership", "your domain: sync"), scoreTerm("age", "waiting 2 d")},
			[]any{hold("REQ-CROSS-081", "Answer attribution on gates"), hold("REQ-CROSS-082", "Release scope on gates")},
			[]any{ownedDomain("sync", "Jussi Rajala")}),
		feedItem("ready", "REQ-CROSS-274", "GET /api/v1/feed returns the ranked feed",
			[]any{scoreTerm("preset", "ready"), scoreTerm("ownership", "your domain: sync"), scoreTerm("age", "waiting 1 d")},
			[]any{}, []any{ownedDomain("sync", "Jussi Rajala")}),
		feedItem("review", "EPIC-NEXT-001", "Attribution spine",
			[]any{scoreTerm("preset", "review"), scoreTerm("flow", "your work, awaiting acceptance"), scoreTerm("age", "today")},
			[]any{}, []any{}),
		feedItem("drift", "REQ-PLN-044", "Answering a decision gate is attributed",
			[]any{scoreTerm("preset", "drift"), scoreTerm("age", "evidence stale · drift 4 d ago")},
			[]any{}, []any{bareDomain("planning")}),
		feedItem("ready", "REQ-KNW-108", "Placeholder pattern owners are not rendered",
			[]any{scoreTerm("preset", "ready"), scoreTerm("age", "waiting 3 d")},
			[]any{}, []any{ownedDomain("knowledge", "Mattias")}),
		feedItem("decision", "OQ-NX-04", "Focus inference beyond explicit references?",
			[]any{scoreTerm("preset", "decision"), scoreTerm("dependency", "releases 1"), scoreTerm("age", "waiting 6 d")},
			[]any{hold("REQ-PLN-135", "Focus is inferred")}, []any{}),
		feedItem("ready", "REQ-INT-058", "An outbound worker posts to Slack",
			[]any{scoreTerm("preset", "ready"), scoreTerm("age", "waiting 1 d")}, []any{}, []any{}),
		feedItem("ready", "REQ-INT-059", "A workspace admin configures a push channel",
			[]any{scoreTerm("preset", "ready"), scoreTerm("age", "waiting 2 d")}, []any{}, []any{}),
	}

	return map[string]any{
		"view":    "personal",
		"release": map[string]any{"scope": "active", "active": []any{map[string]any{"slug": "modernpath-v1-09", "status": "active"}}},
		"person":  map[string]any{"id": 7, "name": "Jussi Rajala"},
		"totals":  map[string]any{"in_scope": 12, "by_kind": map[string]any{"decision": 4, "ready": 5}},
		"items":   items,
		"domains": []any{
			map[string]any{"id": 12, "name": "sync", "owner": map[string]any{"name": "Jussi Rajala"}, "count": 2},
			map[string]any{"id": 18, "name": "knowledge", "owner": map[string]any{"name": "Mattias"}, "count": 2},
		},
		"away": map[string]any{"basis": "own_last_event", "since": "x", "events": []any{}},
	}
}

func runBrief(t *testing.T, fx *wsFixture, opts briefOpts) (string, error) {
	t.Helper()
	env := wsEnv(t, wsServe(t, fx))
	var buf bytes.Buffer
	opts.now = wsNow
	err := yourMoveWithBrief(env, opts, &buf)
	return buf.String(), err
}

// §276.1/.2/.3/.4 — projection first, then the header, item lines with chips and
// hold titles, an owned domain as "D · owner", and exactly three CTA lines.
func TestYourMoveBriefRendersHeaderItemsAndCtas(t *testing.T) {
	fx := &wsFixture{gates: []any{wsGate("RQ-268", "decision")}, feed: feedFixture()}
	env := wsEnv(t, wsServe(t, fx))
	var buf bytes.Buffer
	if err := yourMoveWithBrief(env, briefOpts{now: wsNow}, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()

	// §276.2 header names person, release slug, system
	want(t, out, "Your move — Jussi Rajala · modernpath-v1-09 · system 4")
	// §276.3 item line: [Kind] id — title, chips, owned domain as "D · owner"
	want(t, out, "1. [Decision] RQ-268 — Board column semantics")
	want(t, out, "releases 3 · one blocks Pasi (in flow now)")
	want(t, out, "sync · Jussi Rajala")
	want(t, out, "waiting 2 d")
	// §276.3 hold titles beside ids
	want(t, out, "holds REQ-CROSS-081 (Answer attribution on gates)")
	// §276.4 exactly three CTA lines with live counts
	want(t, out, "Next: modernpath your-move --more")
	want(t, out, "Everything: modernpath your-move --queue (12 in scope)")
	want(t, out, "By domain: sync 2 · knowledge 2")

	// §276.1 the GATES.md projection was regenerated first
	if _, err := os.Stat(filepath.Join(env.Root, yourMoveDir, "GATES.md")); err != nil {
		t.Fatalf("projection not written: %v", err)
	}
	// §276.5 the feed was requested for the personal active view
	if !strings.Contains(fx.lastFeedQuery, "view=personal") || !strings.Contains(fx.lastFeedQuery, "release=active") {
		t.Fatalf("feed query = %q, want view=personal&release=active", fx.lastFeedQuery)
	}
	// top five only by default: the sixth item's id must not appear
	if strings.Contains(out, "OQ-NX-04") {
		t.Fatalf("default brief showed a rank-6 item:\n%s", out)
	}
}

// §276.5 --more reveals ranks 6–10
func TestYourMoveBriefMoreShowsNextFive(t *testing.T) {
	out, err := runBrief(t, &wsFixture{gates: []any{}, feed: feedFixture()}, briefOpts{more: true})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "OQ-NX-04")
	want(t, out, "REQ-INT-058")
	if strings.Contains(out, "RQ-268") {
		t.Fatalf("--more showed a top-five item:\n%s", out)
	}
}

// §276.5 --queue prints everything in scope
func TestYourMoveBriefQueueShowsEverything(t *testing.T) {
	out, err := runBrief(t, &wsFixture{feed: feedFixture()}, briefOpts{queue: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"RQ-268", "REQ-KNW-108", "OQ-NX-04", "REQ-INT-059"} {
		want(t, out, id)
	}
}

// §276.5 --domain filters to items touching that domain
func TestYourMoveBriefDomainFilters(t *testing.T) {
	out, err := runBrief(t, &wsFixture{feed: feedFixture()}, briefOpts{domain: "sync"})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "RQ-268")
	want(t, out, "REQ-CROSS-274")
	if strings.Contains(out, "REQ-KNW-108") {
		t.Fatalf("--domain sync showed a knowledge-only item:\n%s", out)
	}
}

// §276.6 zero items still prints the header and the CTAs — never a dead end
func TestYourMoveBriefZeroItemsNeverDeadEnds(t *testing.T) {
	empty := feedFixture()
	empty["items"] = []any{}
	empty["totals"] = map[string]any{"in_scope": 0, "by_kind": map[string]any{}}
	out, err := runBrief(t, &wsFixture{feed: empty}, briefOpts{})
	if err != nil {
		t.Fatal(err)
	}
	want(t, out, "Your move — Jussi Rajala")
	want(t, out, "Nothing ranked for you in modernpath-v1-09")
	want(t, out, "Everything: modernpath your-move --queue")
}

// §276.7 a feed failure leaves the projection written, names the reason, errors
func TestYourMoveBriefFeedFailureKeepsProjection(t *testing.T) {
	fx := &wsFixture{gates: []any{wsGate("RQ-268", "decision")}, failFeed: true}
	env := wsEnv(t, wsServe(t, fx))
	var buf bytes.Buffer
	err := yourMoveWithBrief(env, briefOpts{now: wsNow}, &buf)
	if err == nil || !strings.Contains(err.Error(), "brief unavailable: server 500: boom") {
		t.Fatalf("err = %v, want brief unavailable: server 500: boom", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, yourMoveDir, "GATES.md")); statErr != nil {
		t.Fatalf("projection must survive a feed failure: %v", statErr)
	}
}

// §276.7 a 200 without data.items is a changed envelope, refused — not "empty"
func TestYourMoveBriefRefusesAChangedEnvelope(t *testing.T) {
	_, err := runBrief(t, &wsFixture{feedNoItems: true}, briefOpts{})
	if err == nil || !strings.Contains(err.Error(), "refusing to read a changed envelope") {
		t.Fatalf("err = %v, want a changed-envelope refusal", err)
	}
}

func want(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("output missing %q:\n%s", needle, haystack)
	}
}

// Two rules the item renderer states in comments and nothing asserted.
//
// Both are about a line a person reads at the top of every session, where a
// duplicated chip or an empty " · " reads as a rendering bug and costs the brief
// its credibility — the one thing a once-per-session message cannot afford.
func TestABriefLineNeverRepeatsOrRendersAPlaceholder(t *testing.T) {
	item := feedItem("decision", "RQ-900", "A title",
		[]any{
			// `kind` is already in the bracket and `ownership` is carried by the
			// owned-domain chip; rendering their chips repeats both.
			scoreTerm("preset", "Decision"),
			scoreTerm("ownership", "owned by Jussi Rajala"),
			scoreTerm("waiting", "waiting 2 d"),
		},
		nil,
		// An owner object present but nameless is a placeholder, not an owner.
		[]any{map[string]any{"name": "sync", "owner": map[string]any{"name": ""}}},
	)

	var buf bytes.Buffer
	renderBriefItem(&buf, 1, item)
	line := buf.String()

	if strings.Contains(line, "· Decision") {
		t.Errorf("the kind is already in the bracket and was repeated as a chip:\n%s", line)
	}
	if strings.Contains(line, "owned by") {
		t.Errorf("ownership is carried by the domain chip and was repeated:\n%s", line)
	}
	if !strings.Contains(line, "waiting 2 d") {
		t.Errorf("a real chip was dropped along with the two suppressed ones:\n%s", line)
	}
	if !strings.Contains(line, "· sync") {
		t.Errorf("the touched domain is missing:\n%s", line)
	}
	if strings.Contains(line, "sync · ") || strings.Contains(line, " · \n") || strings.HasSuffix(line, "· ") {
		t.Errorf("a nameless owner rendered as an owner, leaving a dangling separator:\n%q", line)
	}
}

// The positive half, so the guard above cannot be satisfied by dropping owners
// altogether: a named owner still reaches the line.
func TestANamedOwnerStillRendersBesideItsDomain(t *testing.T) {
	item := feedItem("decision", "RQ-901", "T", nil, nil, []any{ownedDomain("sync", "Jussi Rajala")})

	var buf bytes.Buffer
	renderBriefItem(&buf, 1, item)

	if !strings.Contains(buf.String(), "sync · Jussi Rajala") {
		t.Errorf("an owned domain must read \"D · owner\":\n%s", buf.String())
	}
}
