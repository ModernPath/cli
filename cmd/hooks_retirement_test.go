package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func writeUpgradedHooks(t *testing.T, agent agentConfig) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	var config string
	switch agent.name {
	case "Claude Code":
		config = `{
  "description": "keep this project config",
  "hooks": {
    "PreToolUse": [{"matcher":"Bash","label":"preserve group metadata","hooks":[
      {"type":"command","command":"__OLD_CHECK__","timeout":30},
      {"type":"command","command":"project-custom.sh","timeout":11},
      {"type":"command","command":"project-inline"}
    ]}],
    "SessionStart": [{"hooks":[{"type":"command","command":".claude/hooks/rdd-gate.sh"}]}],
    "Stop": [{"hooks":[{"type":"command","command":"project-stop"}]}]
  }
}`
	case "Cursor":
		config = `{
  "description": "keep this project config",
  "version": 1,
  "hooks": {
    "beforeSubmitPrompt": [
      {"command":"modernpath your-move --hook SessionStart","timeout":45},
      {"command":"project-custom.sh","timeout":12}
    ],
    "afterFileEdit": [{"command":"project-after-edit"}]
  }
}`
	case "Codex":
		config = `{
  "description": "keep this project config",
  "hooks": {
    "SessionStart": [{"matcher":"*","groupLabel":"keep me","hooks":[
      {"type":"command","command":"__OLD_BRIEF__"},
      {"type":"command","command":"project-custom.sh"}
    ]}],
    "Stop": [{"hooks":[{"type":"command","command":"project-stop"}]}]
  }
}`
	default:
		t.Fatalf("fixture not defined for %s", agent.name)
	}
	if agent.name == "Claude Code" {
		config = strings.Replace(config, `"__OLD_CHECK__"`, mustJSONString(t, hookCommand("check --hook PreToolUse", "echo '{}'")), 1)
	}
	if agent.name == "Codex" {
		config = strings.Replace(config, `"__OLD_BRIEF__"`, mustJSONString(t, hookCommand("your-move --hook SessionStart", "echo '{}'")), 1)
	}
	if err := os.WriteFile(agent.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agent.hooksDir, "project-custom.sh"), []byte("modernpath check --hook is user-owned content\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeRetiredPiGate(t *testing.T) {
	t.Helper()
	path := piExtensionPath(hookAgents["pi"])
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	oldManagedExtension := piManagedHeader + ` — older generated extension
// modernpath check --hook PreToolUse
export default function (pi) {
  pi.on("tool_call", () => runModernpath(["check", "--hook", "PreToolUse"]));
}
`
	if err := os.WriteFile(path, []byte(oldManagedExtension), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "custom.ts"), []byte("// modernpath check --hook in user-owned extension\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustJSONString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func initializeHooksRetirementWorkspace(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".modernpath/config.json", []byte(`{"system_id":7}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func selectJSONAgents(t *testing.T) []agentConfig {
	t.Helper()
	hooksClaude, hooksCursor, hooksCodex = true, true, true
	t.Cleanup(func() { hooksClaude, hooksCursor, hooksCodex = false, false, false })
	return []agentConfig{hookAgents["claude"], hookAgents["cursor"], hookAgents["codex"]}
}

func assertRetiredConfigPruned(t *testing.T, agent agentConfig) {
	t.Helper()
	got := settingsText(t, agent.configPath)
	for _, retired := range []string{
		hookCommand("check --hook PreToolUse", "echo '{}'"),
		hookCommand("your-move --hook SessionStart", "echo '{}'"),
		"modernpath your-move --hook", "rdd-gate.sh",
	} {
		if strings.Contains(got, retired) {
			t.Fatalf("retired config marker %q survived for %s:\n%s", retired, agent.name, got)
		}
	}
	preserved := []string{"keep this project config", "project-custom.sh"}
	if agent.name != "Cursor" {
		preserved = append(preserved, "project-stop")
	}
	for _, preserved := range preserved {
		if !strings.Contains(got, preserved) {
			t.Fatalf("%s config lost %q:\n%s", agent.name, preserved, got)
		}
	}
	if agent.name == "Claude Code" && (!strings.Contains(got, "preserve group metadata") || !strings.Contains(got, "project-inline")) {
		t.Fatalf("retirement discarded matcher metadata or a sibling command:\n%s", got)
	}
	if agent.name == "Claude Code" && strings.Contains(got, "SessionStart") {
		t.Fatalf("retirement left an empty group for the retired hook:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(agent.hooksDir, "project-custom.sh")); err != nil {
		t.Fatalf("retirement removed the user's custom hook script: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agent.hooksDir, "rdd-gate.sh")); !os.IsNotExist(err) {
		t.Fatalf("retired managed script remains, stat error: %v", err)
	}
}

func TestHooksInstallRetiresOldEventsAcrossConfigShapes(t *testing.T) {
	chdirTemp(t)
	agents := selectJSONAgents(t)
	initializeHooksRetirementWorkspace(t)
	for _, agent := range agents {
		writeUpgradedHooks(t, agent)
	}
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		assertRetiredConfigPruned(t, agent)
		if !strings.Contains(settingsText(t, agent.configPath), contextHookMarker) {
			t.Fatalf("%s context hook was not installed", agent.name)
		}
	}
}

func TestNoContextInstallStillRetiresRemovedFamilies(t *testing.T) {
	chdirTemp(t)
	agent := selectClaudeOnly(t)
	initializeHooksRetirementWorkspace(t)
	writeUpgradedHooks(t, agent)
	hooksNoContext = true
	t.Cleanup(func() { hooksNoContext = false })
	if err := runHooksInstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	assertRetiredConfigPruned(t, agent)
	if strings.Contains(settingsText(t, agent.configPath), contextHookMarker) {
		t.Fatal("--no-context unexpectedly installed the context family")
	}
}

func TestHooksUninstallRetiresOldEventsAcrossConfigShapes(t *testing.T) {
	chdirTemp(t)
	agents := selectJSONAgents(t)
	for _, agent := range agents {
		writeUpgradedHooks(t, agent)
	}
	managedGate := filepath.Join(agents[0].hooksDir, "rdd-gate.sh")
	if err := os.WriteFile(managedGate, []byte("retired managed gate\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	for _, agent := range agents {
		assertRetiredConfigPruned(t, agent)
	}
}

func TestHooksDoctorWarnsOnlyForRetiredHookEntries(t *testing.T) {
	chdirTemp(t)
	claude := hookAgents["claude"]
	writeUpgradedHooks(t, claude)
	cursor := hookAgents["cursor"]
	if err := os.MkdirAll(filepath.Dir(cursor.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cursor.configPath, []byte(`{"description":"modernpath check --hook is only documentation","hooks":{"afterFileEdit":[{"command":"project-hook"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRetiredPiGate(t)
	pathDir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(pathDir, "modernpath")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathDir)

	output := captureCLIOutput(t)
	cmd := &cobra.Command{}
	cmd.Flags().Bool("json", false, "")
	addRecordReadFlags(cmd)
	if err := runHooksDoctor(cmd, nil); err != nil {
		t.Fatal(err)
	}
	got := output()
	if strings.Count(got, "retired ModernPath check/your-move or rdd-gate hooks remain configured") != 1 ||
		!strings.Contains(got, "Pi: retired managed check/your-move hooks remain") {
		t.Fatalf("doctor should identify retired Claude and managed Pi hooks, and ignore documentation text:\n%s", got)
	}
}

func TestHooksDoctorJSONReportsRetiredHooks(t *testing.T) {
	t.Chdir(t.TempDir())
	writeUpgradedHooks(t, hookAgents["claude"])
	stdout, stderr, err := runAskStreams(t, "hooks", "doctor", "--json")
	if err != nil {
		t.Fatalf("hooks doctor --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var report struct {
		Agents []struct {
			Agent        string `json:"agent"`
			RetiredHooks bool   `json:"retired_hooks"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor stdout must be JSON: %v: %s", err, stdout)
	}
	states := make(map[string]bool, len(report.Agents))
	for _, agent := range report.Agents {
		states[agent.Agent] = agent.RetiredHooks
	}
	if !states["Claude Code"] {
		t.Fatalf("doctor JSON did not flag the retired Claude hooks: %#v", states)
	}
	if states["Pi"] {
		t.Fatalf("Pi extension was incorrectly reported by the JSON sweep: %#v", states)
	}
	if stderr != "" {
		t.Fatalf("successful JSON doctor should not emit diagnostics: %s", stderr)
	}
}

func TestHooksUninstallRemovesOnlyRetiredManagedPiExtension(t *testing.T) {
	chdirTemp(t)
	writeRetiredPiGate(t)
	if err := runHooksUninstall(hooksTestCmd(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(piExtensionPath(hookAgents["pi"])); !os.IsNotExist(err) {
		t.Fatalf("retired managed Pi extension remains, stat error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hookAgents["pi"].hooksDir, "custom.ts")); err != nil {
		t.Fatalf("uninstall removed an unmanaged Pi extension: %v", err)
	}
}

func TestHooksDoctorJSONReportsRetiredManagedPiGate(t *testing.T) {
	t.Chdir(t.TempDir())
	writeRetiredPiGate(t)
	stdout, stderr, err := runAskStreams(t, "hooks", "doctor", "--json")
	if err != nil {
		t.Fatalf("hooks doctor --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var report struct {
		Agents []struct {
			Agent        string `json:"agent"`
			RetiredHooks bool   `json:"retired_hooks"`
		} `json:"agents"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("doctor stdout must be JSON: %v: %s", err, stdout)
	}
	for _, agent := range report.Agents {
		if agent.Agent == "Pi" {
			if !agent.RetiredHooks {
				t.Fatal("doctor JSON missed the managed retired Pi gate")
			}
			if stderr != "" {
				t.Fatalf("successful JSON doctor should not emit diagnostics: %s", stderr)
			}
			return
		}
	}
	t.Fatalf("doctor JSON omitted Pi: %#v", report.Agents)
}
