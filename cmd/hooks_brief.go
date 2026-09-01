package cmd

// REQ-CROSS-277 §277.6 (EPIC-NEXT-003): the fourth hook family — the personal
// brief. One SessionStart entry that runs `your-move --hook`, installed beside
// the sync family's SessionStart entry (a different marker, so each family only
// touches its own). Claude Code only; Codex's acceptance of additionalContext on
// SessionStart is unverified, so it is reported deferred, not silently skipped.

import (
	"encoding/json"
	"os"
)

const briefHookMarker = "your-move --hook"

var claudeBriefEvents = []string{"SessionStart"}

func briefHookCommand() string {
	return "command -v modernpath >/dev/null 2>&1 && modernpath your-move --hook SessionStart 2>/dev/null || printf '{}'"
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
	kept := dropEntriesWithMarkers(existing, briefHookMarker)
	entry := map[string]interface{}{
		"hooks": []interface{}{
			map[string]interface{}{
				"type":    "command",
				"command": briefHookCommand(),
				"timeout": 15,
			},
		},
	}
	hooks["SessionStart"] = append(kept, entry)
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(agent.configPath, data, 0o644)
}

func uninstallBriefFamilyClaude(agent agentConfig) (bool, error) {
	return removeHookEntries(agent.configPath, claudeBriefEvents, briefHookMarker)
}

func briefFamilyState(agent agentConfig) hookFamilyState {
	return familyConfigState(agent, claudeBriefEvents, []string{briefHookMarker}, nil)
}

func briefFamilyInstalled(agent agentConfig) bool {
	return briefFamilyState(agent) == hookStateConfigured
}
