package cmd

// REQ-CROSS-093 / REQ-CROSS-094 · EPIC-SYNC-012 upper-loop executable acceptance. These tests exercise the
// complete project-local lifecycle as one user-visible capability; focused
// config, protocol, and status tests live beside the family implementations.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const expectedCodexGateMarker = "check --hook PreToolUse"

var expectedCodexSyncEvents = []string{"SessionStart", "Stop", "SessionEnd"}

func selectCodexOnly(t *testing.T) agentConfig {
	t.Helper()
	hooksCodex = true
	t.Cleanup(func() { hooksCodex = false })

	agent := hookAgents["codex"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return agent
}

func TestCodexAcceptanceCompleteLifecyclePreservesProjectHooks(t *testing.T) {
	chdirTemp(t)
	agent := selectCodexOnly(t)
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".modernpath/config.json", []byte(`{"system_id":46543}`), 0o644); err != nil {
		t.Fatal(err)
	}

	const projectConfig = `{
  "description": "project hooks must survive",
  "hooks": {
    "Stop": [{"hooks":[{"type":"command","command":"project-stop"}]}],
    "AfterToolUse": [{"matcher":"Bash","hooks":[{"type":"command","command":"project-after"}]}]
  }
}`
	if err := os.WriteFile(agent.configPath, []byte(projectConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	installOutput := captureCLIOutput(t)
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}

	configured := settingsText(t, agent.configPath)
	for _, want := range []string{
		`"description"`, "project-stop", "project-after",
		contextHookMarker, syncHookMarker, expectedCodexGateMarker,
		"UserPromptSubmit", "SessionStart", "Stop", "SessionEnd", "PreToolUse",
	} {
		if !strings.Contains(configured, want) {
			t.Fatalf("complete Codex install is missing %q:\n%s", want, configured)
		}
	}
	if got := strings.Count(configured, contextHookMarker); got != 1 {
		t.Fatalf("context family was duplicated: got %d entries\n%s", got, configured)
	}
	if got := strings.Count(configured, syncHookMarker); got != len(expectedCodexSyncEvents) {
		t.Fatalf("sync family must have one entry per Codex event: got %d\n%s", got, configured)
	}
	if got := strings.Count(configured, expectedCodexGateMarker); got != 1 {
		t.Fatalf("gate family was duplicated: got %d entries\n%s", got, configured)
	}
	if strings.Contains(configured, ".claude/") {
		t.Fatalf("Codex hooks depend on a Claude-owned path:\n%s", configured)
	}
	if !strings.Contains(installOutput(), "/hooks") {
		t.Fatalf("install did not name Codex's required review step:\n%s", installOutput())
	}

	statusOutput := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Context hook: configured", "Sync hooks: configured", "Process gate: configured", "/hooks",
	} {
		if !strings.Contains(statusOutput(), want) {
			t.Fatalf("Codex status must be trust-aware and complete — missing %q:\n%s", want, statusOutput())
		}
	}

	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	after := settingsText(t, agent.configPath)
	for _, marker := range []string{contextHookMarker, syncHookMarker, expectedCodexGateMarker} {
		if strings.Contains(after, marker) {
			t.Fatalf("%q survived Codex uninstall:\n%s", marker, after)
		}
	}
	for _, want := range []string{`"description"`, "project-stop", "project-after"} {
		if !strings.Contains(after, want) {
			t.Fatalf("Codex uninstall removed project config %q:\n%s", want, after)
		}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(after), &parsed); err != nil {
		t.Fatalf("Codex uninstall left invalid JSON: %v", err)
	}
}

func TestCodexAcceptanceProcessOnlyInstallArmsGate(t *testing.T) {
	root := chdirTemp(t)
	agent := selectCodexOnly(t)

	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	configured := settingsText(t, filepath.Join(root, agent.configPath))
	if !strings.Contains(configured, expectedCodexGateMarker) {
		t.Fatalf("process-only Codex install did not configure the gate:\n%s", configured)
	}
	if strings.Contains(configured, contextHookMarker) || strings.Contains(configured, syncHookMarker) {
		t.Fatalf("process-only install configured server-dependent families:\n%s", configured)
	}
}
