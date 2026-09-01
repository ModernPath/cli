// REQ-CROSS-093 / REQ-CROSS-094: the Codex hook family — context delivery
// through the native prompt hook, and trust-aware activation and health.
package cmd

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func codexTestAgent(t *testing.T) agentConfig {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".codex")
	if err := os.MkdirAll(filepath.Join(dir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	agent := hookAgents["codex"]
	agent.hooksDir = filepath.Join(dir, "hooks")
	agent.configPath = filepath.Join(dir, "hooks.json")
	return agent
}

func TestCodexConfigMergePreservesProjectHooksAndIsIdempotent(t *testing.T) {
	agent := codexTestAgent(t)
	const existing = `{
  "description": "project hooks must survive",
  "hooks": {
    "UserPromptSubmit": [{"hooks":[{"type":"command","command":"project-prompt"}]}],
    "AfterToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"project-after"}]}]
  }
}`
	if err := os.WriteFile(agent.configPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCodexConfig(agent); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexConfig(agent); err != nil {
		t.Fatal(err)
	}
	configured := settingsText(t, agent.configPath)
	for _, want := range []string{`"description"`, "project-prompt", "project-after", contextHookMarker} {
		if !strings.Contains(configured, want) {
			t.Fatalf("Codex merge lost %q:\n%s", want, configured)
		}
	}
	if got := strings.Count(configured, contextHookMarker); got != 1 {
		t.Fatalf("Codex context install is not idempotent: got %d entries\n%s", got, configured)
	}
}

func TestCodexConfigRefusesMalformedJSONUnchanged(t *testing.T) {
	agent := codexTestAgent(t)
	const malformed = `{"hooks":{"Stop":[},"description":"must survive"}`
	if err := os.WriteFile(agent.configPath, []byte(malformed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCodexConfig(agent); err == nil {
		t.Fatal("Codex installer treated malformed project config as empty")
	}
	if got := settingsText(t, agent.configPath); got != malformed {
		t.Fatalf("Codex installer rewrote malformed project config:\n%s", got)
	}
}

func TestCodexConfigMigratesLegacyContextAndOmitsIgnoredMatcher(t *testing.T) {
	agent := codexTestAgent(t)
	legacy := `{"hooks":{"UserPromptSubmit":[{"matcher":"*","hooks":[{"type":"command","command":".codex/hooks/modernpath-context.sh"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCodexConfig(agent); err != nil {
		t.Fatal(err)
	}
	configured := settingsText(t, agent.configPath)
	if strings.Contains(configured, agent.scriptName) {
		t.Fatalf("legacy Codex context entry survived migration:\n%s", configured)
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(configured), &settings); err != nil {
		t.Fatal(err)
	}
	hooks := settings["hooks"].(map[string]any)
	entries := hooks["UserPromptSubmit"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected one migrated context entry, got %d", len(entries))
	}
	entry := entries[0].(map[string]any)
	if _, hasMatcher := entry["matcher"]; hasMatcher {
		t.Fatalf("Codex ignores UserPromptSubmit matchers; installer must omit it: %v", entry)
	}
}

func TestCodexSyncFamilyWiresLifecycleWithValidOutputAndTimeouts(t *testing.T) {
	agent := codexTestAgent(t)
	hooksNoContext = true
	hooksNoGate = true
	t.Cleanup(func() {
		hooksNoContext = false
		hooksNoGate = false
	})

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	configured := settingsText(t, agent.configPath)

	var settings map[string]any
	if err := json.Unmarshal([]byte(configured), &settings); err != nil {
		t.Fatal(err)
	}
	hooks := settings["hooks"].(map[string]any)
	for _, event := range expectedCodexSyncEvents {
		entries, _ := hooks[event].([]any)
		if len(entries) != 1 {
			t.Fatalf("%s must have exactly one sync entry, got %d:\n%s", event, len(entries), configured)
		}
		entry := entries[0].(map[string]any)
		if _, hasMatcher := entry["matcher"]; hasMatcher {
			t.Fatalf("%s must run for every Codex event source: %v", event, entry)
		}
		handlers := entry["hooks"].([]any)
		handler := handlers[0].(map[string]any)
		command := handler["command"].(string)
		if !strings.Contains(command, syncHookMarker) || !strings.Contains(command, "--trigger "+event) {
			t.Fatalf("%s carries the wrong sync command: %s", event, command)
		}
		if !strings.Contains(command, "printf '{}'") {
			t.Fatalf("%s must emit valid empty JSON for Codex: %s", event, command)
		}
		wantTimeout := float64(10)
		if event == "SessionEnd" {
			wantTimeout = 3
		}
		if got := handler["timeout"]; got != wantTimeout {
			t.Fatalf("%s timeout = %v, want %v", event, got, wantTimeout)
		}
	}
}

func TestCodexSyncCommandReturnsJSONWhenCLIIsUnavailable(t *testing.T) {
	agent := codexTestAgent(t)
	hooksNoContext = true
	hooksNoGate = true
	t.Cleanup(func() {
		hooksNoContext = false
		hooksNoGate = false
	})
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsText(t, agent.configPath)), &settings); err != nil {
		t.Fatal(err)
	}
	hooks := settings["hooks"].(map[string]any)
	entry := hooks["Stop"].([]any)[0].(map[string]any)
	handler := entry["hooks"].([]any)[0].(map[string]any)
	command := handler["command"].(string)

	cmd := exec.Command("/bin/sh", "-c", command)
	cmd.Env = []string{"PATH=" + t.TempDir()}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Codex sync no-op failed: %v", err)
	}
	if !json.Valid(out) || strings.TrimSpace(string(out)) != "{}" {
		t.Fatalf("Codex Stop no-op returned %q, want valid empty JSON", out)
	}
}

func TestCodexGateConfigUsesAgentNeutralCLIAdapter(t *testing.T) {
	agent := codexTestAgent(t)
	hooksNoContext = true
	hooksNoSync = true
	t.Cleanup(func() {
		hooksNoContext = false
		hooksNoSync = false
	})

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	configured := settingsText(t, agent.configPath)
	for _, want := range []string{"PreToolUse", `"matcher": "Bash"`, expectedCodexGateMarker, "printf '{}'"} {
		if !strings.Contains(configured, want) {
			t.Fatalf("Codex gate config is missing %q:\n%s", want, configured)
		}
	}
	if strings.Contains(configured, ".claude/") || strings.Contains(configured, gateHookScriptName) {
		t.Fatalf("Codex gate depends on the Claude legacy adapter:\n%s", configured)
	}
}

func TestCodexGateHookDeniesOnlyRealViolationFromRepositorySubdirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "epics"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "init", "-q", root).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	worklist := "| Epic | Record | UR | SCN | SR | Tasks | Upper | Lower | Overall | Approval | Notes |\n" +
		"|---|---|---|---|---|---|---|---|---|---|---|\n" +
		"| EPIC-X-001 | [r](epics/EPIC-X-001.md) | u | s | s | t | UPPER_VALIDATED | LOWER_VERIFIED | **DONE** | — | n |\n"
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "epics", "EPIC-X-001.md"),
		[]byte("# EPIC-X-001 — missing approval\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(map[string]any{
		"cwd": filepath.Join(root, "src", "nested"),
		"tool_input": map[string]any{
			"command": "git commit -m blocked",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := processGateHookPayload(payload)
	var envelope struct {
		HookSpecificOutput struct {
			HookEventName            string `json:"hookEventName"`
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(response), &envelope); err != nil {
		t.Fatalf("gate response is not JSON: %v (%s)", err, response)
	}
	if envelope.HookSpecificOutput.HookEventName != "PreToolUse" ||
		envelope.HookSpecificOutput.PermissionDecision != "deny" ||
		!strings.Contains(envelope.HookSpecificOutput.PermissionDecisionReason, "process violation(s)") {
		t.Fatalf("real process violation was not denied with its report: %s", response)
	}

	for _, passPayload := range [][]byte{
		[]byte(`{"tool_input":{"command":"npm test"}}`),
		[]byte(`not json`),
		[]byte(`{"cwd":"/does/not/exist","tool_input":{"command":"git commit -m fail-open"}}`),
	} {
		if got := processGateHookPayload(passPayload); got != "{}" {
			t.Fatalf("non-violation/tooling failure must continue, got %s for %s", got, passPayload)
		}
	}
}

func TestCodexStatusDistinguishesConfiguredPartialLegacyAndAbsent(t *testing.T) {
	root := chdirTemp(t)
	agent := hookAgents["codex"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	configuredOutput := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Context hook: configured", "Sync hooks: configured", "Process gate: configured", "/hooks",
	} {
		if !strings.Contains(configuredOutput(), want) {
			t.Fatalf("complete Codex status is missing %q:\n%s", want, configuredOutput())
		}
	}

	partial := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger Stop"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(partial), 0o644); err != nil {
		t.Fatal(err)
	}
	partialOutput := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Context hook: not configured", "Sync hooks: partial", "Process gate: not configured"} {
		if !strings.Contains(partialOutput(), want) {
			t.Fatalf("partial Codex status is missing %q:\n%s", want, partialOutput())
		}
	}

	legacy := `{"hooks":{
  "UserPromptSubmit":[{"hooks":[{"type":"command","command":".codex/hooks/modernpath-context.sh"}]}],
  "Stop":[{"hooks":[{"type":"command","command":".codex/hooks/modernpath-sync.sh"}]}],
  "PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":".claude/hooks/rdd-gate.sh"}]}]
}}`
	if err := os.WriteFile(agent.configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyOutput := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Context hook: legacy", "Sync hooks: legacy", "Process gate: legacy"} {
		if !strings.Contains(legacyOutput(), want) {
			t.Fatalf("legacy Codex status is missing %q:\n%s", want, legacyOutput())
		}
	}

	if err := os.WriteFile(filepath.Join(root, agent.configPath), []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	absentOutput := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Context hook: not configured", "Sync hooks: not configured", "Process gate: not configured"} {
		if !strings.Contains(absentOutput(), want) {
			t.Fatalf("absent Codex status is missing %q:\n%s", want, absentOutput())
		}
	}
}

func TestCodexDoctorReportsAllFamiliesAndTrustAuthority(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["codex"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "modernpath"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	out := captureCLIOutput(t)
	if err := runHooksDoctor(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Codex: context configured", "Codex: sync configured", "Codex: process gate configured", "/hooks",
	} {
		if !strings.Contains(out(), want) {
			t.Fatalf("Codex doctor is missing %q:\n%s", want, out())
		}
	}
}

func TestCodexUninstallReportsAndRemovesEveryFamilyOnly(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["codex"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"description":"kept","hooks":{"Stop":[{"hooks":[{"type":"command","command":"project-stop"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	out := captureCLIOutput(t)
	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Codex (context)", "Codex (sync)", "Codex (gate)"} {
		if !strings.Contains(out(), want) {
			t.Fatalf("Codex uninstall did not report %q:\n%s", want, out())
		}
	}
	after := settingsText(t, agent.configPath)
	for _, marker := range []string{contextHookMarker, syncHookMarker, gateHookMarker} {
		if strings.Contains(after, marker) {
			t.Fatalf("Codex uninstall left %q:\n%s", marker, after)
		}
	}
	for _, want := range []string{"description", "project-stop"} {
		if !strings.Contains(after, want) {
			t.Fatalf("Codex uninstall removed %q:\n%s", want, after)
		}
	}
}

func TestCodexFamilyOptOutsAreIndependent(t *testing.T) {
	for _, tc := range []struct {
		name   string
		optOut *bool
		absent string
		kept   []string
	}{
		{"context", &hooksNoContext, contextHookMarker, []string{syncHookMarker, gateHookMarker}},
		{"sync", &hooksNoSync, syncHookMarker, []string{contextHookMarker, gateHookMarker}},
		{"gate", &hooksNoGate, gateHookMarker, []string{contextHookMarker, syncHookMarker}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agent := codexTestAgent(t)
			*tc.optOut = true
			t.Cleanup(func() { *tc.optOut = false })
			if err := installForAgent(agent); err != nil {
				t.Fatal(err)
			}
			configured := settingsText(t, agent.configPath)
			if strings.Contains(configured, tc.absent) {
				t.Fatalf("--no-%s did not omit its family:\n%s", tc.name, configured)
			}
			for _, marker := range tc.kept {
				if !strings.Contains(configured, marker) {
					t.Fatalf("--no-%s also removed %q:\n%s", tc.name, marker, configured)
				}
			}
		})
	}
}
