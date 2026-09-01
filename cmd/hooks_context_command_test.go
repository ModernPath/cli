package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-093 criterion 3: "GIVEN an irrelevant prompt or a missing/failing
// CLI WHEN the handler runs THEN Codex receives valid empty JSON at exit zero
// and the prompt continues."
//
// The install wiring was well covered — duplicate collapse, project-hook
// preservation, legacy migration all fail when broken. The command string those
// entries carry was not: deleting the `|| echo '{}'` fallback, deleting the
// `command -v` guard, and hardcoding the event name all left the suite green.
// Every one of those turns a developer's prompt into a broken hook, which is the
// failure this criterion exists to prevent.
//
// Asserting the substrings would restate the source, so this runs the string the
// way an agent harness does and checks what the harness would receive.
func runContextHook(t *testing.T, stub string) (stdout string, code int) {
	t.Helper()
	dir := t.TempDir()
	if stub != "" {
		if err := os.WriteFile(filepath.Join(dir, "modernpath"), []byte(stub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cmd := exec.Command("sh", "-c", contextHookCommand("UserPromptSubmit"))
	// An empty stub dir still goes first, so a real modernpath installed on this
	// machine cannot make the missing-CLI case pass by accident.
	cmd.Env = append(os.Environ(), "PATH="+dir)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		t.Fatal(err)
	}
	return string(out), 0
}

func TestTheContextHookNeverBreaksThePrompt(t *testing.T) {
	for _, tc := range []struct {
		name string
		stub string
	}{
		{"the CLI is not installed", ""},
		{"the CLI exits non-zero", "#!/bin/sh\necho 'boom' >&2\nexit 1\n"},
		{"the CLI is not executable as a program", "#!/bin/sh\nexit 127\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, code := runContextHook(t, tc.stub)

			if code != 0 {
				t.Fatalf("exit %d — a non-zero hook stops the developer's prompt; %q", code, out)
			}
			var any interface{}
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &any); err != nil {
				t.Fatalf("Codex parses this as JSON and got %q: %v", out, err)
			}
		})
	}
}

// The other half: when the CLI IS there, its output must actually reach Codex,
// and it must be told which event fired. A hook that safely returns {} in every
// case is not a hook.
func TestTheContextHookPassesTheCLIOutputAndTheEvent(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" > " + argsFile + "\n" +
		`printf '{"hookSpecificOutput":{"additionalContext":"ctx"}}'` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "modernpath"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("sh", "-c", contextHookCommand("UserPromptSubmit"))
	cmd.Env = append(os.Environ(), "PATH="+dir)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("hook failed: %v", err)
	}

	var payload struct {
		HookSpecificOutput struct {
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("the CLI's JSON did not reach Codex intact: %q", out)
	}
	if payload.HookSpecificOutput.AdditionalContext != "ctx" {
		t.Fatalf("additionalContext = %q, want ctx — the CLI's answer was swallowed",
			payload.HookSpecificOutput.AdditionalContext)
	}

	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("the CLI was never invoked: %v", err)
	}
	if got := strings.TrimSpace(string(args)); got != "context --hook UserPromptSubmit" {
		t.Fatalf("CLI called with %q, want `context --hook UserPromptSubmit` — the event name is part of the contract", got)
	}
}

// Each agent's own event reaches its own CLI invocation; a hardcoded event would
// tell Cursor's beforeSubmitPrompt hook that a UserPromptSubmit fired.
func TestEachAgentsEventReachesTheCLI(t *testing.T) {
	seen := map[string]bool{}
	for name, agent := range hookAgents {
		cmd := contextHookCommand(agent.eventName)
		if !strings.Contains(cmd, "--hook "+agent.eventName) {
			t.Errorf("%s wires event %q but its command is %q", name, agent.eventName, cmd)
		}
		seen[agent.eventName] = true
	}
	if len(seen) < 2 {
		t.Fatalf("only %d distinct event name(s) across agents — this test cannot detect a hardcoded one", len(seen))
	}
}

// The hook's `|| echo '{}'` only fires when the CLI exits non-zero, so the
// "valid empty JSON" half of criterion 3 rests on the CLI never succeeding
// silently. That is a property of the CLI, not of the shell string, and it is
// asserted here rather than in the hook — a fourth guard in the hook string
// would be unreachable defensive code.
func TestHookModeAlwaysEmitsJSON(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prompt  string
		outcome contextOutcome
		wait    time.Duration
	}{
		{name: "an empty prompt", prompt: "   "},
		{name: "an irrelevant prompt", prompt: "what time is it", outcome: contextOutcome{reason: "no match"}},
		{name: "the fetch returns nothing with no reason", prompt: "billing rules"},
		{name: "the fetch outruns the deadline", prompt: "billing rules", wait: 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetch := func(string) contextOutcome {
				if tc.wait > 0 {
					time.Sleep(tc.wait)
				}
				return tc.outcome
			}
			out := contextHookOutputWithin(10*time.Millisecond, "UserPromptSubmit", tc.prompt, fetch)

			if strings.TrimSpace(out) == "" {
				t.Fatalf("hook mode printed nothing — Codex reads stdout as JSON and an empty string is not")
			}
			var any interface{}
			if err := json.Unmarshal([]byte(out), &any); err != nil {
				t.Fatalf("hook mode printed %q, which is not JSON: %v", out, err)
			}
		})
	}
}
