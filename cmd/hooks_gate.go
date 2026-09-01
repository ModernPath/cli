package cmd

// REQ-CROSS-030: the third hook family — the process gate. Claude Code and
// Codex share the documented PreToolUse payload and deny envelope, so both call
// one agent-neutral CLI adapter rather than a repository script owned by one
// harness.

import (
	"encoding/json"
	"os"
)

const gateHookScriptName = "rdd-gate.sh"
const gateHookMarker = "check --hook PreToolUse"

// A missing or old CLI is a tooling failure, not a process violation. The
// adapter itself always exits zero and expresses a proven violation through
// the PreToolUse deny envelope.
const gateHookCommand = "command -v modernpath >/dev/null 2>&1 && modernpath " +
	gateHookMarker + " 2>/dev/null || printf '{}'"

// installGateFamilyClaude MERGES the PreToolUse gate entry into
// .claude/settings.json; every other event and hook entry is preserved.
func installGateFamilyClaude(agent agentConfig) error {
	return installGateFamily(agent)
}

func installGateFamilyCodex(agent agentConfig) error {
	return installGateFamily(agent)
}

func installGateFamily(agent agentConfig) error {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	entry := map[string]interface{}{
		"matcher": "Bash",
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": gateHookCommand,
				"timeout": 30,
			},
		},
	}

	existing, _ := hooks["PreToolUse"].([]interface{})
	kept := dropEntriesWithMarkers(existing, gateHookMarker, gateHookScriptName)
	hooks["PreToolUse"] = append(kept, interface{}(entry))
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(agent.configPath, data, 0o644)
}

func gateFamilyInstalled(agent agentConfig) bool {
	return gateFamilyState(agent) == hookStateConfigured
}

// uninstallGateFamilyClaude removes the gate entries, leaving every other hook
// untouched. The script file belongs to the kit and is not removed here. It
// reports whether it removed any entry.
func uninstallGateFamilyClaude(agent agentConfig) (bool, error) {
	return uninstallGateFamily(agent)
}

func uninstallGateFamilyCodex(agent agentConfig) (bool, error) {
	return uninstallGateFamily(agent)
}

func uninstallGateFamily(agent agentConfig) (bool, error) {
	return removeHookEntries(agent.configPath, []string{"PreToolUse"}, gateHookMarker, gateHookScriptName)
}
