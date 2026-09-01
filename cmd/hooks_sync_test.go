package cmd

// EPIC-SYNC-009 (SCN-AS-005): the sync family installs beside existing hooks —
// user-authored entries and the context family survive, install is idempotent,
// uninstall removes only its own entries.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// RUN:2026-08-12: EPIC-SYNC-009 fixed the Claude installer for replacing the
// whole hooks section and clobbering user hooks. The Cursor and Codex paths
// still did exactly that — a client repo's `.cursor/hooks.json` would be
// overwritten wholesale by `modernpath hooks install`.
func TestCursorInstallPreservesTheProjectsOwnHooks(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["cursor"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	theirs := `{"version":1,"hooks":{"afterFileEdit":[{"command":"npm run lint","timeout":30}]}}`
	if err := os.WriteFile(agent.configPath, []byte(theirs), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCursorConfig(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)
	if !strings.Contains(text, "npm run lint") {
		t.Fatalf("the project's own hook was destroyed:\n%s", text)
	}
	if !strings.Contains(text, contextHookMarker) {
		t.Fatalf("our context hook was not added:\n%s", text)
	}

	// Re-installing must not stack duplicates.
	if err := writeCursorConfig(agent); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(agent.configPath)
	if strings.Count(string(after), contextHookMarker) != 1 {
		t.Fatalf("install is not idempotent:\n%s", after)
	}
}

// REQ-CROSS-046, third criterion: an entry left by an OLD version of the
// installer must be migrated away, not left alongside the new one. That was
// covered for Claude (TestContextInstallMigratesTheLegacyScript) and not for
// Cursor — removing `dropLegacyEntries` from writeCursorConfig passed the whole
// suite, so a Cursor user upgrading would quietly accumulate a stale entry
// pointing at a script the installer had already deleted.
func TestCursorInstallMigratesLegacyEntries(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["cursor"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// A config as an older install left it: the project's own hook, plus our
	// previous script-based entry.
	legacy := `{"version":1,"hooks":{"` + agent.eventName + `":[` +
		`{"command":"npm run lint","timeout":30},` +
		`{"command":".cursor/hooks/` + agent.scriptName + `","timeout":60}` +
		`]}}`
	if err := os.WriteFile(agent.configPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeCursorConfig(agent); err != nil {
		t.Fatal(err)
	}

	raw, _ := os.ReadFile(agent.configPath)
	text := string(raw)

	if strings.Contains(text, agent.scriptName) {
		t.Fatalf("the legacy script entry survived the upgrade:\n%s", text)
	}
	if !strings.Contains(text, "npm run lint") {
		t.Fatalf("the project's own hook was destroyed while migrating:\n%s", text)
	}
	if strings.Count(text, contextHookMarker) != 1 {
		t.Fatalf("expected exactly one of our entries after migration:\n%s", text)
	}
}

// REQ-CROSS-071 (`RUN:2026-08-13`): `hooks status` reported the sync family as
// "not installed" on a workspace whose four entries were present and correct.
// The predicate stat'ed the legacy script — the very file
// TestInstallRemovesTheLegacyScript proves the installer DELETES. So it could
// never be true after a correct install, and the command someone runs to check
// their hooks told them, every time, that the working thing was missing.
func TestSyncFamilyInstalledReadsTheConfigNotAScript(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()

	if syncFamilyInstalled(agent) {
		t.Fatal("reported installed before anything was installed")
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	// exactly the state a correct install leaves: entries in the config, and
	// no script anywhere.
	if _, err := os.Stat(filepath.Join(agent.hooksDir, syncHookScriptName)); err == nil {
		t.Fatal("the installer left a script behind; this test no longer proves anything")
	}
	if !syncFamilyInstalled(agent) {
		t.Fatal("a correctly installed sync family reports as not installed")
	}

	// and a config carrying somebody else's hooks must not read as ours
	if err := os.WriteFile(agent.configPath,
		[]byte(`{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"my-own.sh"}]}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if syncFamilyInstalled(agent) {
		t.Fatal("someone else's Stop hook was mistaken for the sync family")
	}
}

// REQ-CROSS-071, second site: `hooks status` printed "✗ Hook script not found"
// for the CONTEXT family too, for the same reason — both families migrated off
// a repo-tracked script (each install now REMOVES it), and both reports still
// stat'ed one. A status line that is false for every correct install is worse
// than no status line.
func TestContextFamilyInstalledReadsTheConfigNotAScript(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()

	if contextFamilyInstalled(agent) {
		t.Fatal("reported installed before anything was installed")
	}
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeClaudeConfig(agent); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(agent.hooksDir, agent.scriptName)); err == nil {
		t.Fatal("a script was written; this test no longer proves anything")
	}
	if !contextFamilyInstalled(agent) {
		t.Fatal("a correctly installed context hook reports as not installed")
	}
}

// REQ-CROSS-102: the sync family is one marker check against three triggers, so
// ONE surviving entry reports the whole family installed — and reports it by
// naming all three. No test covered the partial case, which is why the
// completeness check could be lost without anything failing.
func TestSyncFamilyPartialInstallIsReportedAsPartial(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}

	// exactly what a half-finished install or a hand-edited settings.json
	// leaves: one of the three triggers wired.
	if err := os.WriteFile(agent.configPath,
		[]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent"}]}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}

	wired, missing := syncFamilyWiring(agent)
	if len(wired) != 1 || wired[0] != "SessionStart" {
		t.Fatalf("want SessionStart wired, got %v", wired)
	}
	if len(missing) != 2 || missing[0] != "Stop" || missing[1] != "SessionEnd" {
		t.Fatalf("a partial install must name what is missing, got %v", missing)
	}
}

// Guards, expected green from the start — the two states that already worked.
func TestSyncFamilyFullInstallHasNothingMissing(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	wired, missing := syncFamilyWiring(agent)
	if len(wired) != len(claudeSyncEvents) || len(missing) != 0 {
		t.Fatalf("a correct install must be complete: wired=%v missing=%v", wired, missing)
	}
}

func TestSyncFamilyAbsentReportsEveryTriggerMissing(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	wired, missing := syncFamilyWiring(agent)
	if len(wired) != 0 || len(missing) != len(claudeSyncEvents) {
		t.Fatalf("nothing wired must report nothing wired: wired=%v missing=%v", wired, missing)
	}
}

// The guard that stops the fix stranding a partial install as unremovable:
// uninstall is built on the uniform marker predicate, so that predicate must
// keep matching anything wired, complete or not. That is why completeness is a
// separate report rather than a narrowing of it.
func TestAPartialInstallIsStillRemovable(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath,
		[]byte(`{"hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent"}]}]}}`),
		0o644); err != nil {
		t.Fatal(err)
	}

	// Asserts the property, not the predicate: main's syncFamilyInstalled is now
	// a stricter tri-state (partial != configured), which is right for reporting
	// and must not decide removability. Uninstall removes by marker, so a
	// partial install stays removable — that is what this guards.
	removed, err := uninstallSyncFamilyClaude(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Fatal("uninstall removed nothing from a partial install")
	}
	if wired, _ := syncFamilyWiring(agent); len(wired) != 0 {
		t.Fatalf("uninstall left entries behind: %v", wired)
	}
}

// REQ-CROSS-118 — the sync-family report agrees with familyConfigState.
//
// syncFamilyWiring answers "is this trigger wired" with containsSyncHook, which
// also matches the LEGACY script name and never counts entries. So two configs
// that familyConfigState correctly calls `legacy` and `partial` were reported as
// a confident "installed" — and reported it by naming all three triggers and the
// detached --if-quiescent behaviour, which is what makes it a claim rather than
// a rounding. This is REQ-CROSS-102's own defect one level down.
func writeSettings(t *testing.T, body string) agentConfig {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return agentConfig{name: "Claude Code", configPath: path}
}

func TestALegacyScriptInstallIsNotReportedAsWired(t *testing.T) {
	agent := writeSettings(t, `{"hooks":{
      "SessionStart":[{"hooks":[{"type":"command","command":"sh .claude/hooks/modernpath-sync.sh"}]}],
      "Stop":[{"hooks":[{"type":"command","command":"sh .claude/hooks/modernpath-sync.sh"}]}],
      "SessionEnd":[{"hooks":[{"type":"command","command":"sh .claude/hooks/modernpath-sync.sh"}]}]}}`)

	if got := syncFamilyState(agent); got != hookStateLegacy {
		t.Fatalf("precondition: familyConfigState should call this legacy, got %s", got)
	}
	wired, _ := syncFamilyWiring(agent)
	if len(wired) != 0 {
		t.Fatalf("a legacy script install was reported as wiring %v to the detached --if-quiescent sync it does not run", wired)
	}
}

func TestADuplicatedEntryIsNotReportedAsCleanlyWired(t *testing.T) {
	agent := writeSettings(t, `{"hooks":{
      "SessionStart":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionStart"}]},
                      {"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionStart"}]}],
      "Stop":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger Stop"}]}],
      "SessionEnd":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionEnd"}]}]}}`)

	if got := syncFamilyState(agent); got != hookStatePartial {
		t.Fatalf("precondition: a duplicated entry is not a clean install, got %s", got)
	}
	if _, missing := syncFamilyWiring(agent); len(missing) == 0 {
		t.Fatal("a duplicated SessionStart was reported as a clean three-trigger install")
	}
}

// Guards, expected green: a real install still reads as complete, and a genuine
// partial still names exactly what is missing.
func TestACurrentInstallStillReadsAsComplete(t *testing.T) {
	agent := writeSettings(t, `{"hooks":{
      "SessionStart":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionStart"}]}],
      "Stop":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger Stop"}]}],
      "SessionEnd":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionEnd"}]}]}}`)
	wired, missing := syncFamilyWiring(agent)
	if len(wired) != 3 || len(missing) != 0 {
		t.Fatalf("a correct install must read complete, got wired=%v missing=%v", wired, missing)
	}
}

func TestAGenuinePartialStillNamesWhatIsMissing(t *testing.T) {
	agent := writeSettings(t, `{"hooks":{
      "SessionStart":[{"hooks":[{"type":"command","command":"modernpath factory sync --if-quiescent --trigger SessionStart"}]}]}}`)
	wired, missing := syncFamilyWiring(agent)
	if len(wired) != 1 || wired[0] != "SessionStart" {
		t.Fatalf("want SessionStart wired, got %v", wired)
	}
	if len(missing) != 2 {
		t.Fatalf("want Stop and SessionEnd missing, got %v", missing)
	}
}

// SCN-AS-001 is "the harness never waits", and every other clause of the hook
// string had a test while the detach itself had none: removing the `&` left the
// whole suite green. String-matching a `&` would only restate the source, so
// this runs the hook the way a harness does — with a deliberately slow
// `modernpath` on PATH — and measures.
//
// Both halves are load-bearing. Without the marker poll, a hook that launched
// nothing at all would also return instantly and pass.
func TestTheHookReturnsBeforeTheSyncFinishes(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  string
	}{
		{"claude", syncHookCommand("Stop")},
		{"codex", codexSyncHookCommand("Stop")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "sync-ran")
			stub := "#!/bin/sh\nsleep 3\ntouch " + marker + "\n"
			if err := os.WriteFile(filepath.Join(dir, "modernpath"), []byte(stub), 0o755); err != nil {
				t.Fatal(err)
			}

			run := exec.Command("sh", "-c", tc.cmd)
			run.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			start := time.Now()
			out, err := run.Output()
			elapsed := time.Since(start)

			if err != nil {
				t.Fatalf("the hook errored into the harness: %v", err)
			}
			if elapsed > 1500*time.Millisecond {
				t.Fatalf("the hook waited %v for the sync — SCN-AS-001 says it detaches and returns", elapsed)
			}
			if tc.name == "codex" && string(out) != "{}" {
				t.Fatalf("codex validates Stop output as JSON, got %q", out)
			}

			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(marker); err == nil {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("the hook returned fast because it never launched the sync at all")
				}
				time.Sleep(50 * time.Millisecond)
			}
		})
	}
}
