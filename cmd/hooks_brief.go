package cmd

// REQ-CROSS-277 §277.6 (EPIC-NEXT-003): the fourth hook family — the personal
// brief. One SessionStart entry that runs `your-move --hook`, installed beside
// the sync family's SessionStart entry (a different marker, so each family only
// touches its own). Claude Code only; Codex's acceptance of additionalContext on
// SessionStart is unverified, so it is reported deferred, not silently skipped.

import "os"

const briefHookMarker = "your-move --hook"

var claudeBriefEvents = []string{"SessionStart"}

func briefHookCommand() string {
	return hookCommand("your-move --hook SessionStart", "printf '{}'")
}

// installBriefFamilyClaude merges one SessionStart entry into
// .claude/settings.json, replacing only a prior brief entry and preserving every
// other hook — the sync family's own SessionStart entry included.
func installBriefFamilyClaude(agent agentConfig) error {
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		return err
	}
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	existing, _ := hooks["SessionStart"].([]interface{})
	entry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": briefHookCommand(),
				"timeout": 15,
			},
		},
	}
	hooks["SessionStart"] = replaceOwnedEntry(existing, entry, briefHookMarker)
	settings["hooks"] = hooks
	return writeSettingsFile(agent.configPath, settings)
}

func uninstallBriefFamilyClaude(agent agentConfig) (bool, error) {
	return removeHookEntries(agent.configPath, claudeBriefEvents, briefHookMarker)
}

func briefFamilyState(agent agentConfig) hookFamilyState {
	if agent.name == "Pi" {
		return piExtensionState(agent, briefHookMarker)
	}
	return familyConfigState(agent, claudeBriefEvents, []string{briefHookMarker}, nil)
}

func briefFamilyInstalled(agent agentConfig) bool {
	return briefFamilyState(agent) == hookStateConfigured
}
