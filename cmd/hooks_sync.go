package cmd

// EPIC-SYNC-009 (REQ-CROSS-027): the second hook family — automatic syncs.
// The context-injection family is untouched; this file owns everything sync.
//
// Claude Code gets the full D-AS-1 trigger set (SessionStart · Stop ·
// SessionEnd). Cursor/Codex sync wiring is deferred until their end-of-turn
// event vocabularies are verified (recorded in the epic) — `status` says so.

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const syncHookScriptName = "modernpath-sync.sh"

// The hook calls the INSTALLED CLI directly rather than a repo script. A
// tracked script is one merge away from disappearing — PR merges 30-33
// resolved .claude/ against it, and every Stop afterwards failed with
// "No such file or directory" (RUN:2026-08-10), which is the opposite of a
// hook that never errors into the harness. Guarded so a machine without the
// CLI no-ops, detached so the harness never waits.
func syncHookCommand(event string) string {
	return "command -v modernpath >/dev/null 2>&1 && " +
		"( modernpath factory sync --if-quiescent --trigger " + event + " >/dev/null 2>&1 & ) ; exit 0"
}

// The script detaches the gated sync and returns immediately (D-AS-5):
// the harness never waits on network, parsing, or the server. All outcomes
// land in .modernpath/sync-hooks.log via the CLI itself (D-AS-4).
const syncHookScript = `#!/bin/sh
# ModernPath auto-sync hook (EPIC-SYNC-009). Fire-and-forget: detach and return.
( modernpath factory sync --if-quiescent --trigger "${MP_SYNC_TRIGGER:-${1:-hook}}" >/dev/null 2>&1 & )
exit 0
`

var claudeSyncEvents = []string{"SessionStart", "Stop", "SessionEnd"}

// installSyncFamilyClaude MERGES the trigger entries into
// .claude/settings.json — existing events and hooks (the context family
// included) are preserved. It writes NO script: the entries invoke the
// installed CLI directly, so nothing a merge can delete sits in the path.
func installSyncFamilyClaude(agent agentConfig) error {
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		return err
	}

	// a legacy install left a script behind; remove it so no entry can point
	// at a file that a branch switch removes (RUN:2026-08-10)
	_ = os.Remove(filepath.Join(agent.hooksDir, syncHookScriptName))

	var settings map[string]interface{}
	if data, err := os.ReadFile(agent.configPath); err == nil {
		_ = json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = map[string]interface{}{}
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	for _, event := range claudeSyncEvents {
		// Migrate: drop any entry left by an older install (they point at a
		// script that no longer exists, so the harness errors on every
		// trigger). Without this the legacy entry counts as "installed" and
		// the broken wiring survives forever (RUN:2026-08-10).
		if existing, ok := hooks[event].([]interface{}); ok {
			var kept []interface{}
			for _, e := range existing {
				raw, _ := json.Marshal(e)
				if !containsStr(string(raw), syncHookScriptName) {
					kept = append(kept, e)
				}
			}
			hooks[event] = kept
		}

		entry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": syncHookCommand(event),
					"timeout": 10,
				},
			},
		}

		existing, _ := hooks[event].([]interface{})
		if !containsSyncHook(existing) {
			hooks[event] = append(existing, entry)
		}
	}

	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(agent.configPath, data, 0o644)
}

// our entries are recognised by the CLI invocation they carry (or, for legacy
// installs, the old script name)
const syncHookMarker = "factory sync --if-quiescent"

func containsSyncHook(entries []interface{}) bool {
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if raw == nil {
			continue
		}
		if containsStr(string(raw), syncHookMarker) || containsStr(string(raw), syncHookScriptName) {
			return true
		}
	}
	return false
}

func containsStr(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle || indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

// uninstallSyncFamilyClaude removes the script and the sync entries, leaving
// every other hook untouched.
func uninstallSyncFamilyClaude(agent agentConfig) {
	_ = os.Remove(filepath.Join(agent.hooksDir, syncHookScriptName))

	data, err := os.ReadFile(agent.configPath)
	if err != nil {
		return
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil || settings == nil {
		return
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return
	}

	for _, event := range claudeSyncEvents {
		entries, _ := hooks[event].([]interface{})
		var kept []interface{}
		for _, e := range entries {
			raw, _ := json.Marshal(e)
			if !containsStr(string(raw), syncHookMarker) && !containsStr(string(raw), syncHookScriptName) {
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	settings["hooks"] = hooks

	if out, err := json.MarshalIndent(settings, "", "  "); err == nil {
		_ = os.WriteFile(agent.configPath, out, 0o644)
	}
}

func syncFamilyInstalled(agent agentConfig) bool {
	_, err := os.Stat(filepath.Join(agent.hooksDir, syncHookScriptName))
	return err == nil
}
