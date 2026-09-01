package cmd

// Two failure shapes these tests pin:
//
//	1 — `hooks install --claude` in a repo that had only run `modernpath
//	     install` printed "not initialized" and exited 0 with no settings
//	     entry, so the REQ-CROSS-030 gate never armed for the process-only
//	     audience. The gate runs `modernpath check`, which needs no server.
//	2 — `hooks uninstall` detected sync (and context) by a script file this
//	     version never writes, so it removed neither, and it removed the gate
//	     without saying so. The report read "No ModernPath hooks found to
//	     remove" over a settings file still full of them.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func hooksTestCmd() *cobra.Command { return &cobra.Command{} }

// selectClaudeOnly points the install/uninstall commands at Claude Code and
// restores the package-level flags afterwards.
func selectClaudeOnly(t *testing.T) agentConfig {
	t.Helper()
	hooksClaude = true
	t.Cleanup(func() { hooksClaude = false })

	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return agent
}

// captureCLIOutput redirects the print helpers (which write through
// fatih/color's package-level writer) and returns a reader for what was said.
func captureCLIOutput(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := color.Output
	prevErr := color.Error
	color.Output = &buf
	// Warnings and errors ride stderr since REQ-CROSS-210 (D13: --json stdout
	// purity); the CLI's "what was said" is both streams together.
	color.Error = &buf
	t.Cleanup(func() {
		color.Output = prev
		color.Error = prevErr
	})
	return buf.String
}

func settingsText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no settings written at %s: %v", path, err)
	}
	return string(raw)
}

// ── F2: the gate arms without a server binding ────────────────────────────

func TestInstallArmsTheGateInAnUnboundRepository(t *testing.T) {
	chdirTemp(t)
	agent := selectClaudeOnly(t)

	// no .modernpath/config.json: `modernpath install` ran, `init` did not
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatalf("the gate needs no server, so this must succeed: %v", err)
	}

	text := settingsText(t, agent.configPath)
	if !strings.Contains(text, gateHookMarker) {
		t.Fatalf("the process gate did not arm: %s", text)
	}
	// the server-dependent families must stay out
	if strings.Contains(text, syncHookMarker) {
		t.Fatalf("auto-sync was wired without a binding: %s", text)
	}
	if strings.Contains(text, contextHookMarker) {
		t.Fatalf("context injection was wired without a binding: %s", text)
	}
}

// Exiting 0 when nothing could be installed is deliberate; the defect this
// pins against was *silence*, not the exit code.
// Nothing is broken when a user asks Cursor for a gate that is Claude-only, so
// the command explains and exits 0 — a non-zero exit would fail every CI step
// and setup script that installs hooks best-effort.
func TestUnboundInstallExplainsInsteadOfFailingWhenNothingApplies(t *testing.T) {
	chdirTemp(t)
	hooksCursor = true
	t.Cleanup(func() { hooksCursor = false })

	out := captureCLIOutput(t)
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatalf("nothing to install is not a failure: %v", err)
	}

	text := out()
	for _, want := range []string{"No hooks were installed", "Claude Code and Codex", "hooks install --claude", "--codex", "modernpath init"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the exit-0 path must still say what happened and what to do — missing %q in:\n%s", want, text)
		}
	}
	if _, statErr := os.Stat(hookAgents["cursor"].configPath); statErr == nil {
		t.Fatal("no config should have been written for a skipped agent")
	}
}

func TestUnboundInstallExplainsWhenTheGateIsOptedOut(t *testing.T) {
	chdirTemp(t)
	agent := selectClaudeOnly(t)
	hooksNoGate = true
	t.Cleanup(func() { hooksNoGate = false })

	out := captureCLIOutput(t)
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatalf("--no-gate is the caller's own choice, not an error: %v", err)
	}

	text := out()
	for _, want := range []string{"--no-gate", "Nothing left to install"} {
		if !strings.Contains(text, want) {
			t.Fatalf("opting out must be narrated, not silent — missing %q in:\n%s", want, text)
		}
	}
	if _, statErr := os.Stat(agent.configPath); statErr == nil {
		t.Fatal("--no-gate must leave no settings entry behind")
	}
}

// ── F3: uninstall removes every family and says which ─────────────────────

func TestUninstallRemovesEveryFamily(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"my-own-stop.sh"}]}]},"model":"opus"}`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	before := settingsText(t, agent.configPath)
	for _, marker := range []string{contextHookMarker, syncHookMarker, gateHookMarker, briefHookMarker} {
		if !strings.Contains(before, marker) {
			t.Fatalf("fixture is wrong — %q was never installed: %s", marker, before)
		}
	}

	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}

	after := settingsText(t, agent.configPath)
	for _, marker := range []string{contextHookMarker, syncHookMarker, gateHookMarker, briefHookMarker} {
		if strings.Contains(after, marker) {
			t.Fatalf("%q survived uninstall: %s", marker, after)
		}
	}

	// every client hook and setting is preserved
	if !strings.Contains(after, "my-own-stop.sh") || !strings.Contains(after, `"opus"`) {
		t.Fatalf("uninstall took the user's own configuration with it: %s", after)
	}

	// and no ModernPath event is left behind as an empty husk
	var settings map[string]interface{}
	if err := json.Unmarshal([]byte(after), &settings); err != nil {
		t.Fatal(err)
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	for _, event := range []string{"UserPromptSubmit", "SessionStart", "SessionEnd", "PreToolUse"} {
		if _, present := hooks[event]; present {
			t.Fatalf("%s should be gone once its only entry was ours: %s", event, after)
		}
	}
}

func TestUninstallReportsTheFamiliesItRemoved(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	// each family reports itself, and only while it is actually there
	for _, family := range []struct {
		name    string
		removed func(agentConfig) (bool, error)
		present func(agentConfig) bool
	}{
		{"context", uninstallContextFamily, contextFamilyInstalled},
		{"sync", uninstallSyncFamilyClaude, syncFamilyInstalled},
		{"gate", uninstallGateFamilyClaude, gateFamilyInstalled},
		{"brief", uninstallBriefFamilyClaude, briefFamilyInstalled},
	} {
		if !family.present(agent) {
			t.Fatalf("%s family should be installed before uninstall", family.name)
		}
		ok, err := family.removed(agent)
		if err != nil {
			t.Fatalf("%s family removal failed: %v", family.name, err)
		}
		if !ok {
			t.Fatalf("%s family removal was not reported", family.name)
		}
		if family.present(agent) {
			t.Fatalf("%s family still reports installed after removal", family.name)
		}
		if ok, err = family.removed(agent); err != nil || ok {
			t.Fatalf("%s family reported a second removal with nothing left to remove", family.name)
		}
	}
}

// ── a settings file the installer cannot parse is not an empty one ─────────

// Every family installer merges into .claude/settings.json by read → unmarshal
// → rewrite. Treating an unparseable file as empty turns one stray comma in a
// hand-edited file into the silent loss of the customer's entire configuration.
func TestInstallersRefuseToReplaceAMalformedSettingsFile(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const broken = `{"hooks": {"Stop": [},}`
	if err := os.WriteFile(agent.configPath, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, install := range map[string]func(agentConfig) error{
		"context": writeClaudeConfig,
		"sync":    installSyncFamilyClaude,
		"gate":    installGateFamilyClaude,
	} {
		if err := install(agent); err == nil {
			t.Errorf("the %s installer treated a malformed settings file as empty", name)
		}
		if got := settingsText(t, agent.configPath); got != broken {
			t.Fatalf("the %s installer rewrote a file it could not parse:\n%s", name, got)
		}
	}
}

// ── uninstall must not claim a removal it could not persist ────────────────

func TestUninstallDoesNotClaimRemovalItCouldNotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		// The in-build Docker test gate runs as uid 0, and root writes
		// through a 0444 file — the write-failure fixture cannot fail there.
		t.Skip("running as root: a read-only settings file is still writable, so this fixture cannot exercise the failure path")
	}
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(agent.configPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(agent.configPath, 0o644) })

	out := captureCLIOutput(t)
	err := runHooksUninstall(hooksTestCmd(), nil)
	text := out()

	if strings.Contains(text, "hooks removed") {
		t.Fatalf("uninstall claimed a removal over a write that failed:\n%s", text)
	}
	if err == nil {
		t.Fatal("an uninstall that could not persist its removals is a failure, not a success")
	}
	if !strings.Contains(text, agent.configPath) {
		t.Fatalf("the failure must name the file it could not write:\n%s", text)
	}
	if !contextFamilyInstalled(agent) || !syncFamilyInstalled(agent) || !gateFamilyInstalled(agent) {
		t.Fatal("the settings file changed despite the reported write failure")
	}
}

// ── the Claude context entry must be shell form ────────────────────────────

// Claude Code selects execution form by the presence of an "args" key: with
// it, the command string is posix_spawned as a literal executable name; only
// without it does the string reach a shell. The context command is a shell
// pipeline (`command -v … && … || …`), so an entry carrying "args" fails with
// ENOENT on every prompt instead of injecting context.
func TestClaudeContextEntryCarriesNoArgsKey(t *testing.T) {
	chdirTemp(t)
	agent := selectClaudeOnly(t)

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	var settings struct {
		Hooks map[string][]struct {
			Hooks []map[string]interface{} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(settingsText(t, agent.configPath)), &settings); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, entry := range settings.Hooks[agent.eventName] {
		for _, h := range entry.Hooks {
			command, _ := h["command"].(string)
			if !strings.Contains(command, contextHookMarker) {
				continue
			}
			found = true
			if _, has := h["args"]; has {
				t.Fatalf("the context entry carries an \"args\" key, so Claude Code execs the pipeline as a literal binary name: %v", h)
			}
		}
	}
	if !found {
		t.Fatal("fixture is wrong — no context entry was installed")
	}
}

// ── status asks the settings, not the filesystem ───────────────────────────

func TestStatusReportsEachFamilyFromTheSettings(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	out := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	text := out()

	if strings.Contains(text, "Hook script not found") {
		t.Fatalf("status still probes for a script this install never writes:\n%s", text)
	}
	for _, want := range []string{"Context hook: installed", "Sync hooks: installed", "Process gate: armed", "modernpath check --hook PreToolUse"} {
		if !strings.Contains(text, want) {
			t.Fatalf("a wired family must report installed — missing %q in:\n%s", want, text)
		}
	}
}

func TestStatusNamesTheMissingFamiliesInAnEmptyRepository(t *testing.T) {
	chdirTemp(t)

	out := captureCLIOutput(t)
	if err := runHooksStatus(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	text := out()

	for _, want := range []string{"Context hook: not installed", "Sync hooks: not installed", "Process gate: not armed"} {
		if !strings.Contains(text, want) {
			t.Fatalf("an absent family must be named, not hidden — missing %q in:\n%s", want, text)
		}
	}
}

// The old probe asked the filesystem for a script the install deliberately
// never writes, so a correctly wired repository reported every family absent.
func TestFamilyDetectionDoesNotDependOnAScriptFile(t *testing.T) {
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	for _, script := range []string{syncHookScriptName, agent.scriptName} {
		if _, err := os.Stat(filepath.Join(agent.hooksDir, script)); !os.IsNotExist(err) {
			t.Fatalf("%s should not exist — detection must not depend on it", script)
		}
	}
	if !syncFamilyInstalled(agent) {
		t.Fatal("sync family is wired in settings but reports not installed")
	}
	if !contextFamilyInstalled(agent) {
		t.Fatal("context family is wired in settings but reports not installed")
	}
}
