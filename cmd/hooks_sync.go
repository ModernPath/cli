package cmd

// EPIC-SYNC-009 (REQ-CROSS-027): the second hook family — automatic syncs.
// The context-injection family is untouched; this file owns everything sync.
//
// Claude Code and Codex get the full D-AS-1 trigger set (SessionStart · Stop ·
// SessionEnd). Codex's Stop contract additionally requires JSON on stdout.

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

// Codex validates successful Stop output as JSON. The sync remains detached,
// then the hook always emits a valid empty response even when modernpath is not
// on PATH. SessionEnd shares this command but has a stricter config timeout.
func codexSyncHookCommand(event string) string {
	return "command -v modernpath >/dev/null 2>&1 && " +
		"( modernpath factory sync --if-quiescent --trigger " + event + " >/dev/null 2>&1 & ) ; printf '{}'"
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
var codexSyncEvents = []string{"SessionStart", "Stop", "SessionEnd"}

// installSyncFamilyClaude MERGES the trigger entries into
// .claude/settings.json — existing events and hooks (the context family
// included) are preserved. It writes NO script: the entries invoke the
// installed CLI directly, so nothing a merge can delete sits in the path.
func installSyncFamilyClaude(agent agentConfig) error {
	return installSyncFamily(agent, claudeSyncEvents, false)
}

func installSyncFamilyCodex(agent agentConfig) error {
	return installSyncFamily(agent, codexSyncEvents, true)
}

func installSyncFamily(agent agentConfig, events []string, codex bool) error {
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		return err
	}

	// a legacy install left a script behind; remove it so no entry can point
	// at a file that a branch switch removes (RUN:2026-08-10)
	_ = os.Remove(filepath.Join(agent.hooksDir, syncHookScriptName))

	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	for _, event := range events {
		// Replace only ModernPath-owned entries. This migrates the old script
		// adapter, updates prior command shapes, and collapses duplicates.
		existing, _ := hooks[event].([]interface{})
		kept := dropEntriesWithMarkers(existing, syncHookMarker, syncHookScriptName)

		command := syncHookCommand(event)
		timeout := 10
		if codex {
			command = codexSyncHookCommand(event)
			if event == "SessionEnd" {
				timeout = 3
			}
		}

		entry := map[string]interface{}{
			"hooks": []interface{}{
				map[string]interface{}{
					"type":    "command",
					"command": command,
					"timeout": timeout,
				},
			},
		}
		hooks[event] = append(kept, entry)
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

// uninstallSyncFamilyClaude removes the legacy script and the sync entries,
// leaving every other hook untouched. It reports whether it removed any entry.
func uninstallSyncFamilyClaude(agent agentConfig) (bool, error) {
	_ = os.Remove(filepath.Join(agent.hooksDir, syncHookScriptName))

	return removeHookEntries(agent.configPath, claudeSyncEvents, syncHookMarker, syncHookScriptName)
}

func uninstallSyncFamilyCodex(agent agentConfig) (bool, error) {
	_ = os.Remove(filepath.Join(agent.hooksDir, syncHookScriptName))

	return removeHookEntries(agent.configPath, codexSyncEvents, syncHookMarker, syncHookScriptName)
}

// syncFamilyInstalled asks the settings, not the filesystem: this install
// deliberately writes no script, so the old os.Stat probe answered "not
// installed" for every correctly wired repository — and uninstall and status
// both believed it.
func syncFamilyInstalled(agent agentConfig) bool {
	return syncFamilyState(agent) == hookStateConfigured
}

// syncFamilyWiring reports which of the family's triggers are wired and which
// are not, in claudeSyncEvents order.
//
// It is deliberately separate from syncFamilyInstalled rather than folded into
// it: uninstall and status are both built on that uniform marker predicate, and
// narrowing it to "all three or nothing" would leave a partial install
// unremovable.
func syncFamilyWiring(agent agentConfig) (wired, missing []string) {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		// A settings file this version cannot parse is not an empty one, so
		// fall back to the uniform predicate rather than reporting the family
		// absent on a file that may well wire it.
		if syncFamilyInstalled(agent) {
			return append([]string(nil), claudeSyncEvents...), nil
		}
		return nil, append([]string(nil), claudeSyncEvents...)
	}

	hooks, _ := settings["hooks"].(map[string]interface{})
	for _, event := range claudeSyncEvents {
		entries, _ := hooks[event].([]interface{})
		// Exactly one entry in the CURRENT form, which is what familyConfigState
		// requires. containsSyncHook is deliberately not used here: it also
		// matches the legacy script name and never counts, so a legacy install
		// and a duplicated one both read as wired — and the caller then names
		// all three triggers and the detached --if-quiescent behaviour, which a
		// legacy script does not run.
		if countCurrentSyncHooks(entries) == 1 {
			wired = append(wired, event)
		} else {
			missing = append(missing, event)
		}
	}
	return wired, missing
}

func countCurrentSyncHooks(entries []interface{}) int {
	n := 0
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if raw != nil && containsStr(string(raw), syncHookMarker) {
			n++
		}
	}
	return n
}
