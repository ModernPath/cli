// REQ-CROSS-093 / REQ-CROSS-094: the Codex hook family — context delivery
// through the native prompt hook, and trust-aware activation and health.
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func codexTestAgent(t *testing.T) agentConfig {
	t.Helper()
	root := t.TempDir()
	// The installer reads the store-backed marker from the process cwd
	// (storeBackedFromCwd walks up from it), so a test run inside a flipped
	// workspace saw the enclosing repository's marker, retired the sync
	// family, and failed three assertions that pass anywhere else. Pin the
	// cwd to the fixture so the tests describe the fixture, not the host.
	t.Chdir(root)
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
