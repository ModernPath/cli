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
	for _, want := range []string{"my-own-hook.sh", `"other"`, "SessionStart", "Stop", "SessionEnd", syncHookScriptName} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings lost %q: %s", want, text)
		}
	}

	// idempotent: a second install adds nothing
	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	raw2, _ := os.ReadFile(agent.configPath)
	if strings.Count(string(raw2), syncHookScriptName) != strings.Count(text, syncHookScriptName) {
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
	if strings.Contains(text, syncHookScriptName) {
		t.Fatal("sync entries survived uninstall")
	}
	if !strings.Contains(text, "my-own-stop.sh") {
		t.Fatal("user's own Stop hook was removed")
	}
	if syncFamilyInstalled(agent) {
		t.Fatal("script file survived uninstall")
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
	if !strings.Contains(text, agent.scriptName) {
		t.Fatal("context hook not written")
	}
}
