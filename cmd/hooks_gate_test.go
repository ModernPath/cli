package cmd

// REQ-CROSS-030: both supported PreToolUse harnesses call the same CLI adapter.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func gateTestAgent(t *testing.T) agentConfig {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return agentConfig{
		name:       "Claude Code",
		hooksDir:   filepath.Join(dir, "hooks"),
		configPath: filepath.Join(dir, "settings.json"),
	}
}

func preToolUseEntries(t *testing.T, configPath string) []interface{} {
	t.Helper()
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	entries, _ := hooks["PreToolUse"].([]interface{})
	return entries
}

func TestGateFamilyRegistersThePreToolUseEntry(t *testing.T) {
	agent := gateTestAgent(t)
	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	entries := preToolUseEntries(t, agent.configPath)
	if len(entries) != 1 {
		t.Fatalf("expected exactly one PreToolUse entry, got %d", len(entries))
	}
	raw, _ := json.Marshal(entries[0])
	if !strings.Contains(string(raw), gateHookMarker) {
		t.Fatalf("the entry must invoke the CLI gate adapter, got %s", raw)
	}
	if strings.Contains(string(raw), ".claude/") || strings.Contains(string(raw), gateHookScriptName) {
		t.Fatalf("the agent-neutral gate still depends on the Claude script, got %s", raw)
	}
	// The guard is the fail-open half of the design: a missing or old CLI must
	// degrade to a no-op, not error on every Bash call.
	if !strings.Contains(string(raw), "printf '{}'") {
		t.Fatalf("the entry must fail open when the script is missing, got %s", raw)
	}
}

func TestGateFamilyIsIdempotentAndPreservesOtherHooks(t *testing.T) {
	agent := gateTestAgent(t)
	seed := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"my-own-guard"}]}],"Stop":[{"hooks":[{"type":"command","command":"my-stop"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	entries := preToolUseEntries(t, agent.configPath)
	if len(entries) != 2 {
		t.Fatalf("expected the client entry plus one gate entry, got %d", len(entries))
	}
	data, _ := os.ReadFile(agent.configPath)
	for _, kept := range []string{"my-own-guard", "my-stop"} {
		if !strings.Contains(string(data), kept) {
			t.Fatalf("client hook %q was clobbered", kept)
		}
	}
}

func TestGateFamilyUninstallRemovesOnlyItsEntry(t *testing.T) {
	agent := gateTestAgent(t)
	seed := `{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"my-own-guard"}]}]}}`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	uninstallGateFamilyClaude(agent)

	entries := preToolUseEntries(t, agent.configPath)
	if len(entries) != 1 {
		t.Fatalf("expected only the client entry to remain, got %d", len(entries))
	}
	raw, _ := json.Marshal(entries[0])
	if strings.Contains(string(raw), gateHookMarker) {
		t.Fatal("the gate entry survived uninstall")
	}
}
