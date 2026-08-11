package cmd

// EPIC-SYNC-009 (SCN-AS-005): the sync family installs beside existing hooks —
// user-authored entries and the context family survive, install is idempotent,
// uninstall removes only its own entries.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func chdirTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

func claudeAgent() agentConfig { return hookAgents["claude"] }

func TestSyncFamilyInstallPreservesExistingHooks(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}

	existing := `{"hooks":{"UserPromptSubmit":[{"hooks":[{"type":"command","command":"my-own-hook.sh"}]}]},"other":"kept"}`
	if err := os.WriteFile(agent.configPath, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)
	for _, want := range []string{"my-own-hook.sh", `"other"`, "SessionStart", "Stop", "SessionEnd", syncHookMarker} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings lost %q: %s", want, text)
		}
	}

	// idempotent: a second install adds nothing
	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(agent.configPath)
	if strings.Count(string(raw2), syncHookMarker) != strings.Count(text, syncHookMarker) {
		t.Fatal("re-install duplicated sync entries")
	}
}

func TestSyncFamilyUninstallRemovesOnlyItself(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath,
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"my-own-stop.sh"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	uninstallSyncFamilyClaude(agent)

	raw, _ := os.ReadFile(agent.configPath)
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, syncHookMarker) {
		t.Fatal("sync entries survived uninstall")
	}
	if !strings.Contains(text, "my-own-stop.sh") {
		t.Fatal("user's own Stop hook was removed")
	}
	if syncFamilyInstalled(agent) {
		t.Fatal("sync family still reports installed after uninstall")
	}
}

func TestClaudeContextConfigMergesNotClobbers(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath,
		[]byte(`{"hooks":{"SessionEnd":[{"hooks":[{"type":"command","command":"user-end.sh"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeClaudeConfig(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)
	if !strings.Contains(text, "user-end.sh") {
		t.Fatalf("the pre-existing clobber bug is back — user hooks lost: %s", text)
	}
	if !strings.Contains(text, contextHookMarker) {
		t.Fatalf("context hook not written: %s", text)
	}
}

// RUN:2026-08-10: the hook pointed at a repo-tracked script by relative path, so
// it depended on both the file surviving in the working tree and the harness
// running from the repo root. Every Stop failed with "No such file or directory"
// — the opposite of a hook that never errors into the harness.
func TestSyncHookDependsOnNoRepoFile(t *testing.T) {
	cmd := syncHookCommand("Stop")

	if strings.Contains(cmd, ".claude/hooks") || strings.Contains(cmd, syncHookScriptName) {
		t.Fatalf("hook must not depend on a repo file: %s", cmd)
	}
	if !strings.Contains(cmd, "command -v modernpath") {
		t.Fatalf("hook must no-op when the CLI is absent: %s", cmd)
	}
	if !strings.Contains(cmd, "exit 0") {
		t.Fatalf("hook must never fail the harness: %s", cmd)
	}
	if !strings.Contains(cmd, "--trigger Stop") {
		t.Fatalf("hook must carry its trigger: %s", cmd)
	}
}

func TestInstallRemovesTheLegacyScript(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(agent.hooksDir, syncHookScriptName)
	if err := os.WriteFile(legacy, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("the legacy script should be gone — nothing may point at it")
	}
}

// RUN:2026-08-10: a legacy entry pointing at the removed script counted as
// "already installed", so reinstalling left the broken wiring in place.
func TestInstallMigratesLegacyEntries(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"MP_SYNC_TRIGGER=Stop .claude/hooks/modernpath-sync.sh"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)
	if strings.Contains(text, syncHookScriptName) {
		t.Fatalf("legacy entry survived the reinstall: %s", text)
	}
	if !strings.Contains(text, syncHookMarker) {
		t.Fatalf("new wiring missing: %s", text)
	}
}

// RUN:2026-08-11: the context family had the sync family's original flaw — a
// repo-tracked script, called by relative path, that also needed `jq` on PATH.
func TestContextHookDependsOnNoRepoFile(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	hooksNoSync = true
	defer func() { hooksNoSync = false }()

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(agent.hooksDir, agent.scriptName)); !os.IsNotExist(err) {
		t.Fatalf("the install still writes %s — the file can go missing and take the hook with it", agent.scriptName)
	}
	raw, _ := os.ReadFile(agent.configPath)
	if !strings.Contains(string(raw), contextHookMarker) {
		t.Fatalf("context hook not wired to the CLI: %s", raw)
	}
	if strings.Contains(string(raw), agent.scriptName) {
		t.Fatalf("settings still point at the script: %s", raw)
	}
}

func TestContextInstallMigratesTheLegacyScript(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	hooksNoSync = true
	defer func() { hooksNoSync = false }()

	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(agent.hooksDir, agent.scriptName)
	if err := os.WriteFile(script, []byte("#!/bin/bash\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"hooks":{"UserPromptSubmit":[{"matcher":"*","hooks":[{"type":"command","command":".claude/hooks/modernpath-context.sh"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	if strings.Contains(string(raw), agent.scriptName) {
		t.Fatalf("legacy entry survived: %s", raw)
	}
	if _, err := os.Stat(script); !os.IsNotExist(err) {
		t.Fatal("the legacy script was left behind")
	}
	// installing twice must not stack duplicates
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(agent.configPath)
	if strings.Count(string(after), contextHookMarker) != 1 {
		t.Fatalf("install is not idempotent: %s", after)
	}
}
