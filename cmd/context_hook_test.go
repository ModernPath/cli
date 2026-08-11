package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-027 (EPIC-SYNC-009, RUN:2026-08-11): the context hook is the second
// member of the hook family. It ran as a repo-tracked shell script that shelled
// out to `jq` — the same shape that broke the sync hook when the file went
// missing. These tests pin the behaviour the script used to carry so it can move
// into the CLI.

func TestContextHookIsSilentWhenThePromptIsEmpty(t *testing.T) {
	got := contextHookOutput("UserPromptSubmit", "   ", func(string) string {
		t.Fatal("no prompt should mean no server call")
		return ""
	})
	if got != "{}" {
		t.Fatalf("want an empty envelope, got %s", got)
	}
}

func TestContextHookIsSilentWhenNothingIsRelevant(t *testing.T) {
	got := contextHookOutput("UserPromptSubmit", "how do I use React?", func(string) string { return "" })
	if got != "{}" {
		t.Fatalf("an irrelevant prompt must inject nothing, got %s", got)
	}
}

// The agents' documented contract is hookSpecificOutput.additionalContext; a
// differently-shaped payload is parsed, matched against no known field, and
// silently discarded (measured RUN:2026-08-08).
func TestContextHookUsesTheDocumentedEnvelope(t *testing.T) {
	got := contextHookOutput("UserPromptSubmit", "how does sync work?", func(p string) string {
		return "The sync engine is Core.Sync."
	})

	var payload struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(got), &payload); err != nil {
		t.Fatalf("hook output must be JSON: %v (%s)", err, got)
	}
	if payload.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
		t.Fatalf("event name not echoed: %s", got)
	}
	if !strings.Contains(payload.HookSpecificOutput.AdditionalContext, "Core.Sync") {
		t.Fatalf("the fetched context is missing: %s", got)
	}
	if !strings.Contains(payload.HookSpecificOutput.AdditionalContext, "ModernPath Architectural Context") {
		t.Fatalf("context must be labelled so the agent knows its origin: %s", got)
	}
}

func TestContextHookReadsThePromptFromTheAgentPayload(t *testing.T) {
	for _, body := range []string{`{"prompt":"how does sync work?"}`, `{"user_prompt":"how does sync work?"}`} {
		if got := promptFromHookPayload([]byte(body)); got != "how does sync work?" {
			t.Fatalf("payload %s → %q", body, got)
		}
	}
	if got := promptFromHookPayload([]byte("not json at all")); got != "" {
		t.Fatalf("garbage in must mean silence out, got %q", got)
	}
}

// EPIC-CTX-001 (REQ-CROSS-037, D-CTX-6): a degraded hook was indistinguishable
// from a quiet one — the same `{}` for "nothing relevant", an auth failure and a
// timeout. One prompt returned 0 chars in 24.3s and 5831 chars in 17.5s minutes
// apart with nothing recorded either time (RUN:2026-08-11).

func TestContextHookLogsWhatHappened(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}

	logContextOutcome("delivered", 1400*time.Millisecond, "6 pointers · specific")
	logContextOutcome("empty", 900*time.Millisecond, "not relevant")
	logContextOutcome("failed", 8*time.Second, "deadline exceeded")

	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", "context-hook.log"))
	if err != nil {
		t.Fatalf("no log written: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want one line per run, got %d: %s", len(lines), raw)
	}
	for _, want := range []string{"delivered", "not relevant", "deadline exceeded"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("outcome %q missing from the log: %s", want, raw)
		}
	}
	// The reason must survive alongside the duration — "failed" on its own is
	// the silence this fixes.
	if !strings.Contains(lines[2], "8.0s") {
		t.Fatalf("duration missing: %s", lines[2])
	}
}

func TestContextHookGivesUpAtTheDeadline(t *testing.T) {
	chdirTemp(t)

	slow := func(string) contextOutcome {
		time.Sleep(300 * time.Millisecond)
		return contextOutcome{text: "context that arrived too late", reason: "delivered"}
	}

	start := time.Now()
	got := contextHookOutputWithin(50*time.Millisecond, "UserPromptSubmit", "how does sync work?", slow)
	elapsed := time.Since(start)

	if got != "{}" {
		t.Fatalf("a late answer must be dropped, not injected: %s", got)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("the hook held the turn for %v — the deadline is the whole point", elapsed)
	}
}

func TestContextHookKeepsAnAnswerThatArrivesInTime(t *testing.T) {
	chdirTemp(t)

	got := contextHookOutputWithin(2*time.Second, "UserPromptSubmit", "how does sync work?",
		func(string) contextOutcome {
			return contextOutcome{text: "Core.Sync is the engine.", reason: "delivered"}
		})
	if !strings.Contains(got, "Core.Sync") {
		t.Fatalf("an in-time answer must be delivered: %s", got)
	}
}

// RUN:2026-08-11: `fetchContext` collapsed no-config, auth failure, non-200,
// decode error and timeout into "", and the logger recorded the SHAPE of the
// result rather than the REASON — so every failure printed "not relevant", the
// exact conflation D-CTX-6 exists to end.
func TestContextHookLogsTheReasonNotTheShape(t *testing.T) {
	dir := chdirTemp(t)
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		outcome contextOutcome
		want    string
	}{
		{contextOutcome{reason: "no workspace config"}, "no workspace config"},
		{contextOutcome{reason: "server error 500"}, "server error 500"},
		{contextOutcome{reason: "not relevant"}, "not relevant"},
		{contextOutcome{text: "Core.Sync is the engine.", reason: "delivered"}, "delivered"},
	}

	for _, c := range cases {
		contextHookOutputWithin(time.Second, "UserPromptSubmit", "how does sync work?",
			func(string) contextOutcome { return c.outcome })
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", "context-hook.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !strings.Contains(string(raw), c.want) {
			t.Fatalf("reason %q missing from the log:\n%s", c.want, raw)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.Contains(line, "server error") && strings.Contains(line, "not relevant") {
			t.Fatalf("a failure was filed as irrelevance: %s", line)
		}
	}
}
