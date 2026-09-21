package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-384 (EPIC-CLI-018): an unchanged packet section whose scope-context
// stamp is stale — the server lists it under facts.sections.missing after a
// member patch moved the context — is re-put by the push so the server
// re-stamps it, and the push says so instead of "nothing to do". --restamp
// re-puts every filled canonical section regardless.

func servedSection(key, content string) map[string]any {
	return map[string]any{
		"scope_kind": "epic", "scope_external_id": "EPIC-CLI-008",
		"section_key": key, "content": content,
		"content_fingerprint": "served-" + key,
	}
}

func staleFacts(missing ...string) map[string]any {
	m := make([]any, 0, len(missing))
	for _, k := range missing {
		m = append(m, k)
	}
	return map[string]any{
		"packet_fingerprint": strings.Repeat("a", 64),
		"facts_state":        "served",
		"facts": map[string]any{
			"sections": map[string]any{"complete": len(m) == 0, "missing": m},
		},
	}
}

func TestREQCROSS384PushRestampsAnUnchangedStaleSection(t *testing.T) {
	fx := scopeFixture()
	fx.packetSections = []any{servedSection("red_strategy", "the red strategy body\n")}
	fx.deliveryContext = staleFacts("red_strategy")
	env, _ := pulledScope(t, fx) // the local file equals the served content

	fx.authorPosts = nil
	var err error
	out := captureOut(t, func() { err = workingSetPush(env, false) })
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	action, rec := packetPost(fx, "red_strategy")
	if rec == nil || action != "update" {
		t.Fatalf("a stale unchanged section must be re-put as an update, got %q %v", action, fx.authorPosts)
	}
	if n := len(fx.authorPosts); n != 1 {
		t.Fatalf("exactly the stale section is re-put, got %d posts: %v", n, fx.authorPosts)
	}
	if !strings.Contains(out, "re-stamped 1 section") || strings.Contains(out, "nothing to do") {
		t.Fatalf("the push must say what it re-stamped, never 'nothing to do':\n%s", out)
	}
}

func TestREQCROSS384UnchangedAndCurrentSectionsStillPostNothing(t *testing.T) {
	fx := scopeFixture()
	fx.packetSections = []any{servedSection("red_strategy", "the red strategy body\n")}
	fx.deliveryContext = staleFacts() // complete: nothing is stale
	env, _ := pulledScope(t, fx)

	fx.authorPosts = nil
	var err error
	out := captureOut(t, func() { err = workingSetPush(env, false) })
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(fx.authorPosts) != 0 {
		t.Fatalf("an unchanged, current section must not be re-put: %v", fx.authorPosts)
	}
	if !strings.Contains(out, "nothing to") || strings.Contains(out, "re-stamped") {
		t.Fatalf("with nothing stale the push is honestly idle:\n%s", out)
	}
}

func TestREQCROSS384RestampFlagRePutsEveryFilledSection(t *testing.T) {
	defer func() { wsPushRestamp = false }()
	fx := scopeFixture()
	fx.packetSections = []any{
		servedSection("red_strategy", "the red strategy body\n"),
		servedSection("decisions", "the decisions body\n"),
	}
	env, _ := pulledScope(t, fx) // no facts served at all

	wsPushRestamp = true
	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push --restamp: %v", err)
	}
	for _, key := range []string{"red_strategy", "decisions"} {
		if action, _ := packetPost(fx, key); action != "update" {
			t.Fatalf("--restamp must re-put %s, got %q: %v", key, action, fx.authorPosts)
		}
	}
	if n := len(fx.authorPosts); n != 2 {
		t.Fatalf("--restamp re-puts the filled canonical sections only, got %d posts", n)
	}
}

// The first REQ-CROSS-384 clause — sections pushed WITH a member patch carry
// the post-patch context — holds by ordering: every item patch is posted before
// the first packet section. Pinned here (F-CLI018-R1-07) so it stays true.
func TestREQCROSS384ItemPatchesPostBeforeSections(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	edit(t, memberPath(dir, "REQ-CROSS-310"), func(s string) string {
		return strings.Replace(s, "```authoring:editable\nparity\n```", "```authoring:editable\nparity renamed\n```", 1)
	})
	writePacketFile(t, dir, "30-red-strategy.md", "a new red strategy\n")

	fx.authorPosts = nil
	if err := workingSetPush(env, false); err != nil {
		t.Fatalf("push: %v", err)
	}
	firstSection, lastPatch := -1, -1
	for i, p := range fx.authorPosts {
		r, _ := p["record"].(map[string]any)
		switch {
		case str(p, "action") == "patch":
			lastPatch = i
		case str(r, "kind") == "packet_section" && firstSection < 0:
			firstSection = i
		}
	}
	if lastPatch < 0 || firstSection < 0 {
		t.Fatalf("expected one patch and one section post, got %v", fx.authorPosts)
	}
	if lastPatch > firstSection {
		t.Fatalf("item patches must post before packet sections (patch at %d, section at %d)", lastPatch, firstSection)
	}
}
