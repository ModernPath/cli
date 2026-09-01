package cmd

// REQ-CROSS-277 (EPIC-NEXT-003) — RED first. The SessionStart hook prints the
// compact personal brief once per agent session and {} on every other path,
// always exit 0. Each test names the clause it pins from
// epics/EPIC-NEXT-003-personal-feed/specs/requirements.md §REQ-CROSS-277.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readBriefLog(t *testing.T, root string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, ".modernpath", "session-briefs.log"))
	if err != nil {
		return ""
	}
	return string(b)
}

func hookLoader(env *factoryEnv) func() (*factoryEnv, error) {
	return func() (*factoryEnv, error) { return env, nil }
}

// §277.1/.2 — a first SessionStart delivers the compact brief once; a repeat is {}.
func TestYourMoveHookDeliversOncePerSession(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture()}))

	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"s1","hook_event_name":"SessionStart","source":"startup"}`), &out, time.Second, hookLoader(env))

	want(t, out.String(), `"hookEventName":"SessionStart"`)
	want(t, out.String(), "## Your move —")
	want(t, out.String(), "1. [Decision] RQ-268")
	want(t, out.String(), "Everything: modernpath your-move --queue")
	want(t, readBriefLog(t, env.Root), "s1 · startup · delivered")

	var out2 bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"s1","source":"resume"}`), &out2, time.Second, hookLoader(env))
	if strings.TrimSpace(out2.String()) != "{}" {
		t.Fatalf("a briefed session must be {}: %s", out2.String())
	}
	want(t, readBriefLog(t, env.Root), "s1 · resume · already-briefed")
}

// §277.2 — compaction is mid-work, not a start: {} even for an unseen id.
func TestYourMoveHookCompactSkips(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture()}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"fresh","source":"compact"}`), &out, time.Second, hookLoader(env))
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("compact must be {}: %s", out.String())
	}
	want(t, readBriefLog(t, env.Root), "compact · skipped")
}

// §277.3 — no session id: the brief prints and the doubt is logged, not silenced.
func TestYourMoveHookNoSessionIdPrintsAndFlags(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture()}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{}`), &out, time.Second, hookLoader(env))
	want(t, out.String(), "## Your move —")
	want(t, readBriefLog(t, env.Root), "none · none · delivered · no-session-id")
}

// §277.4 — a fetch past the deadline is {} and failed; the hook does not hang.
func TestYourMoveHookDeadlineExceeded(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture(), feedDelay: 2 * time.Second}))
	var out bytes.Buffer
	start := time.Now()
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"slow","source":"startup"}`), &out, 200*time.Millisecond, hookLoader(env))
	elapsed := time.Since(start)

	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("deadline must be {}: %s", out.String())
	}
	if elapsed > time.Second {
		t.Fatalf("hook held the turn %v past its deadline", elapsed)
	}
	want(t, readBriefLog(t, env.Root), "failed · deadline exceeded")
}

// §277.4 — an unbound workspace is {} and failed, never an error.
func TestYourMoveHookNoConfigIsSilent(t *testing.T) {
	root := chdirTemp(t)
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"x","source":"startup"}`), &out, time.Second,
		func() (*factoryEnv, error) { return nil, fmt.Errorf("no config") })
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("no config must be {}: %s", out.String())
	}
	want(t, readBriefLog(t, root), "failed · no workspace config")
}

// §277.4 — a server error is {} and failed.
func TestYourMoveHookServerErrorIsSilent(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{failFeed: true}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"e","source":"startup"}`), &out, time.Second, hookLoader(env))
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("server error must be {}: %s", out.String())
	}
	want(t, readBriefLog(t, env.Root), "failed")
}

// §277.5 — hook mode writes no GATES.md projection.
func TestYourMoveHookWritesNoProjection(t *testing.T) {
	env := wsEnv(t, wsServe(t, &wsFixture{feed: feedFixture()}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"p","source":"startup"}`), &out, time.Second, hookLoader(env))
	if _, err := os.Stat(filepath.Join(env.Root, yourMoveDir, "GATES.md")); !os.IsNotExist(err) {
		t.Fatalf("hook mode must not write GATES.md")
	}
}
