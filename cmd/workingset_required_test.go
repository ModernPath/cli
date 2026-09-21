package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-383 (EPIC-CLI-018): the server serves the canonical section keys
// the plan check requires for the scope (facts.sections.required, computed
// from stored membership); `working-set pull --scope` scaffolds exactly those,
// so a member stored but absent from the frozen selection list is scaffolded
// too, and the scaffold and the check can never disagree on the key set.
func TestREQCROSS383ScopePullScaffoldsTheServedRequiredKeys(t *testing.T) {
	fx := scaffoldFixture()
	// The frozen list is EMPTY (a selection recorded without --members); the
	// store still knows REQ-CROSS-310 as a member and serves its key.
	fx.workSelection["current"].(map[string]any)["members"] = []any{}
	fx.deliveryContext = map[string]any{
		"packet_fingerprint": strings.Repeat("a", 64),
		"facts_state":        "served",
		"facts": map[string]any{
			"sections": map[string]any{
				"complete": false,
				"missing":  []any{"reconnaissance", "red_strategy", "decisions", "enrichment:REQ-CROSS-310"},
				"required": []any{"reconnaissance", "red_strategy", "decisions", "enrichment:REQ-CROSS-310"},
			},
		},
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	path := filepath.Join(scopeDir(env), "packet", "20-enrichment-REQ-CROSS-310.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the served required key enrichment:REQ-CROSS-310 must be scaffolded even though the frozen list omits it: %v", err)
	}
}

// A server that serves no facts (older than the read) keeps the client-side
// derivation from the frozen list — a read, so a fallback is safe — and says so.
func TestREQCROSS383ScopePullFallsBackToTheFrozenListWithoutFacts(t *testing.T) {
	fx := scaffoldFixture() // deliveryContext nil: {"data": null}
	env := wsEnv(t, wsServe(t, fx))
	var err error
	out := captureWarnings(t, func() { err = workingSetPullScope(env, false, wsNow) })
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(scopeDir(env), "packet", "20-enrichment-REQ-CROSS-310.md")); serr != nil {
		t.Fatalf("without served keys the frozen-list derivation must still scaffold the SR member: %v", serr)
	}
	if !strings.Contains(out, "frozen") {
		t.Fatalf("the fallback must be named as one, got %q", out)
	}
}
