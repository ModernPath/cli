package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/gate"
	"github.com/spf13/cobra"
)

var (
	hooksCursor    bool
	hooksClaude    bool
	hooksCodex     bool
	hooksAll       bool
	hooksNoSync    bool
	hooksNoContext bool
	hooksNoGate    bool
	hooksNoBrief   bool
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage IDE hooks for ModernPath integration",
	Long: `Manage IDE hooks that integrate ModernPath's architectural intelligence
into your AI coding workflow.

Supported agents:
  - Cursor      (.cursor/hooks.json)
  - Claude Code (.claude/settings.json)
  - Codex       (.codex/hooks.json)

The hook auto-injects codebase context when asking questions using
LLM-based intelligent relevance filtering.`,
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install ModernPath hooks for AI coding agents",
	Long: `Install hooks that automatically inject ModernPath context.

By default, installs for all detected agents. Use flags to target specific agents:
  --cursor    Install for Cursor only
  --claude    Install for Claude Code only
  --codex     Install for Codex only
  --all       Install for all agents (regardless of detection)

The hook uses LLM-based intelligent filtering to determine when to inject context.`,
	RunE: runHooksInstall,
}

var hooksUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ModernPath hooks",
	RunE:  runHooksUninstall,
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check hook installation status for all agents",
	RunE:  runHooksStatus,
}

func init() {
	hooksInstallCmd.Flags().BoolVar(&hooksCursor, "cursor", false, "Install for Cursor only")
	hooksInstallCmd.Flags().BoolVar(&hooksClaude, "claude", false, "Install for Claude Code only")
	hooksInstallCmd.Flags().BoolVar(&hooksCodex, "codex", false, "Install for Codex only")
	hooksInstallCmd.Flags().BoolVar(&hooksAll, "all", false, "Install for all agents")
	hooksInstallCmd.Flags().BoolVar(&hooksNoSync, "no-sync", false, "Skip the auto-sync hook family (EPIC-SYNC-009)")
	hooksInstallCmd.Flags().BoolVar(&hooksNoContext, "no-context", false, "Skip the context-injection hook family")
	hooksInstallCmd.Flags().BoolVar(&hooksNoGate, "no-gate", false, "Skip the process-gate hook family (REQ-CROSS-030)")
	hooksInstallCmd.Flags().BoolVar(&hooksNoBrief, "no-brief", false, "Skip the session-brief hook family (REQ-CROSS-277)")

	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)
}

// Hook script shared by all agents
// contextHookMarker identifies our context entry in an agent's config — the
// command itself, since the hook no longer owns a file to be named after.
const contextHookMarker = "context --hook"

// contextHookCommand runs the installed CLI. `command -v` makes a missing binary
// a silent no-op and `|| echo '{}'` guarantees the agent gets a well-formed
// response whatever happens — a context hook must never cost the user a prompt.
func contextHookCommand(event string) string {
	return "command -v modernpath >/dev/null 2>&1 && " +
		"modernpath context --hook " + event + " 2>/dev/null || echo '{}'"
}

type agentConfig struct {
	name       string
	hooksDir   string
	configPath string
	scriptName string
	eventName  string
}

var hookAgents = map[string]agentConfig{
	"cursor": {
		name:       "Cursor",
		hooksDir:   ".cursor/hooks",
		configPath: ".cursor/hooks.json",
		scriptName: "modernpath-context.sh",
		eventName:  "beforeSubmitPrompt",
	},
	"claude": {
		name:       "Claude Code",
		hooksDir:   ".claude/hooks",
		configPath: ".claude/settings.json",
		scriptName: "modernpath-context.sh",
		eventName:  "UserPromptSubmit",
	},
	"codex": {
		name:       "Codex",
		hooksDir:   ".codex/hooks",
		configPath: ".codex/hooks.json",
		scriptName: "modernpath-context.sh",
		eventName:  "UserPromptSubmit",
	},
}

func detectInstalledAgents() []string {
	var detected []string

	// Check for Cursor
	if _, err := os.Stat(".cursor"); err == nil {
		detected = append(detected, "cursor")
	}

	// Check for Claude Code
	if _, err := os.Stat(".claude"); err == nil {
		detected = append(detected, "claude")
	}

	// Check for Codex
	if _, err := os.Stat(".codex"); err == nil {
		detected = append(detected, "codex")
	}

	return detected
}

func runHooksInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true

	// Determine which agents to install for
	var targetAgents []string

	if hooksAll {
		targetAgents = []string{"cursor", "claude", "codex"}
	} else if hooksCursor || hooksClaude || hooksCodex {
		if hooksCursor {
			targetAgents = append(targetAgents, "cursor")
		}
		if hooksClaude {
			targetAgents = append(targetAgents, "claude")
		}
		if hooksCodex {
			targetAgents = append(targetAgents, "codex")
		}
	} else {
		// Auto-detect
		targetAgents = detectInstalledAgents()
		if len(targetAgents) == 0 {
			// Default to cursor if nothing detected
			targetAgents = []string{"cursor"}
			printInfo("No agent configs detected, defaulting to Cursor.\n")
		} else {
			printInfo("Detected agents: ")
			for i, a := range targetAgents {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(hookAgents[a].name)
			}
			fmt.Println()
		}
	}

	if !hooksNoGate && targetsProcessGate(targetAgents) {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := gate.MigrateLegacyBaseline(root); err != nil {
			return fmt.Errorf("cannot migrate the process-gate baseline: %w", err)
		}
	}

	if !config.IsInitialized() {
		return installProcessOnlyHooks(targetAgents)
	}

	// Install for each target agent
	var installed []string
	for _, agentKey := range targetAgents {
		agent := hookAgents[agentKey]
		if err := installForAgent(agent); err != nil {
			printError("Failed to install for %s: %v\n", agent.name, err)
		} else {
			installed = append(installed, agent.name)
		}
	}

	if len(installed) > 0 {
		fmt.Println()
		printSuccess("ModernPath hooks configured for: ")
		for i, name := range installed {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(name)
		}
		fmt.Println()

		fmt.Println()
		printInfo("The hook uses LLM-based intelligent filtering:\n")
		fmt.Println("  ✓ Triggers: \"How does authentication work?\"")
		fmt.Println("  ✓ Triggers: \"Where is the user model?\"")
		fmt.Println("  ✗ Skips: \"Add a login button\" (task)")
		fmt.Println("  ✗ Skips: \"How do I use React?\" (general)")
		fmt.Println()
		if !containsAgentName(installed, "Codex") {
			printInfo("Restart your IDE to activate the hooks.\n")
		}
		printCodexTrustGuidance(installed)
	}

	return nil
}

func containsAgentName(agentNames []string, wanted string) bool {
	for _, name := range agentNames {
		if name == wanted {
			return true
		}
	}
	return false
}

func printCodexTrustGuidance(agentNames []string) {
	if containsAgentName(agentNames, "Codex") {
		printInfo("Codex hooks are configured, not yet confirmed active. Open /hooks in Codex and review each project hook definition.\n")
	}
}

func targetsProcessGate(agentKeys []string) bool {
	for _, key := range agentKeys {
		if key == "claude" || key == "codex" {
			return true
		}
	}
	return false
}

// installProcessOnlyHooks is the unbound repository's install: the process
// gate and nothing else.
//
// This path used to print "not
// initialized" and return nil, so `hooks install --claude` in a repo that had
// only run `modernpath install` exited 0 with no settings entry — the
// REQ-CROSS-030 gate never armed in exactly the repositories the process-only
// audience keeps. The gate runs `modernpath check`, which reads the working
// tree and talks to no server (CODE:cmd/check.go:runCheck), so a binding is not
// its business. Context injection and auto-sync do call the server; they are
// skipped out loud rather than silently.
//
// Exiting 0 on "nothing installed" is deliberate — a non-zero exit here was
// tried and rejected: the defect was the silence, not the exit code,
// and nothing is actually broken when a Cursor-only target asks for a
// Claude-only family. Every branch below narrates what it did and did not do;
// none of them fails the command.
func installProcessOnlyHooks(targetAgents []string) error {
	printWarning("ModernPath is not initialized here — no server binding.\n")
	printInfo("Skipping context injection and auto-sync: both call the server. Run 'modernpath init' to add them.\n")

	if hooksNoGate {
		printWarning("Nothing left to install: --no-gate skips the only family that works unbound.\n")
		printInfo("Drop --no-gate to arm the process gate, or run 'modernpath init' to bind this repository.\n")
		return nil
	}

	var armed []string
	var failed bool
	for _, agentKey := range targetAgents {
		agent := hookAgents[agentKey]
		if agent.name != "Claude Code" && agent.name != "Codex" {
			printInfo("%s: process gate deferred (it speaks the PreToolUse deny protocol — REQ-CROSS-030)\n", agent.name)
			continue
		}
		if err := os.MkdirAll(agent.hooksDir, 0755); err != nil {
			printError("Failed to create %s: %v\n", agent.hooksDir, err)
			failed = true
			continue
		}
		var err error
		if agent.name == "Codex" {
			err = installGateFamilyCodex(agent)
		} else {
			err = installGateFamilyClaude(agent)
		}
		if err != nil {
			printError("Failed to arm the process gate for %s: %v\n", agent.name, err)
			failed = true
			continue
		}
		armed = append(armed, agent.name)
	}

	if len(armed) == 0 {
		// "no target was selected" would be a false statement over a selected
		// target whose install just failed and said why.
		if !failed {
			printWarning("No hooks were installed: the process gate supports Claude Code and Codex, and neither target was selected.\n")
			printInfo("Run 'modernpath hooks install --claude' or '--codex', or 'modernpath init' to bind this repository.\n")
		}
		return nil
	}

	fmt.Println()
	printSuccess("Process gate configured for: %s\n", strings.Join(armed, ", "))
	printInfo("When enabled, it runs 'modernpath check' before every git commit and denies on a real violation.\n")
	if !containsAgentName(armed, "Codex") {
		printInfo("Restart your IDE to activate the hook.\n")
	}
	printCodexTrustGuidance(armed)
	return nil
}

func installForAgent(agent agentConfig) error {
	// Create hooks directory
	if err := os.MkdirAll(agent.hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// Context-injection family (the original hook). It writes no file: the
	// event name is baked into the command per agent (Claude/Codex use
	// UserPromptSubmit, Cursor beforeSubmitPrompt) and the CLI emits the
	// envelope itself.
	if !hooksNoContext {
		os.Remove(filepath.Join(agent.hooksDir, agent.scriptName))

		var err error
		switch agent.name {
		case "Cursor":
			err = writeCursorConfig(agent)
		case "Claude Code":
			err = writeClaudeConfig(agent)
		case "Codex":
			err = writeCodexConfig(agent)
		}
		if err != nil {
			return err
		}
	}

	// Sync family (EPIC-SYNC-009 / EPIC-SYNC-012): Claude Code and Codex carry
	// the full verified trigger set. Cursor remains deferred.
	if !hooksNoSync {
		switch agent.name {
		case "Claude Code":
			if err := installSyncFamilyClaude(agent); err != nil {
				return err
			}
		case "Codex":
			if err := installSyncFamilyCodex(agent); err != nil {
				return err
			}
		default:
			printInfo("%s: sync-hook wiring deferred (event vocabulary unverified — EPIC-SYNC-009)\n", agent.name)
		}
	}

	// Gate family (REQ-CROSS-030 / EPIC-SYNC-012). Claude Code and Codex share
	// the agent-neutral PreToolUse adapter.
	if !hooksNoGate {
		switch agent.name {
		case "Claude Code":
			if err := installGateFamilyClaude(agent); err != nil {
				return err
			}
		case "Codex":
			if err := installGateFamilyCodex(agent); err != nil {
				return err
			}
		}
	}

	// Brief family (REQ-CROSS-277): the SessionStart personal brief. Claude Code
	// only — Codex's acceptance of additionalContext on SessionStart is
	// unverified, so it is reported deferred, not silently skipped.
	if !hooksNoBrief {
		switch agent.name {
		case "Claude Code":
			if err := installBriefFamilyClaude(agent); err != nil {
				return err
			}
		case "Codex":
			printInfo("%s: session-brief hook deferred (SessionStart additionalContext unverified — REQ-CROSS-277)\n", agent.name)
		}
	}

	return nil
}

// readSettingsForMerge loads an agent's settings file for a merge-and-rewrite.
// A missing file is an empty document; an unreadable or unparseable one is an
// error — every installer rewrites the whole file, so treating a file it
// cannot parse as empty would replace the user's entire configuration with
// only our entries.
func readSettingsForMerge(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]interface{}{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON (%v) — fix or remove it; nothing was changed", path, err)
	}
	if settings == nil {
		settings = map[string]interface{}{}
	}
	return settings, nil
}

// containsSyncMarker reports whether any entry's JSON mentions the marker.
func containsSyncMarker(entries []interface{}, marker string) bool {
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if raw != nil && containsStr(string(raw), marker) {
			return true
		}
	}
	return false
}

// A family is identified by the marker its entries carry, never by a script on
// disk. The current install writes no scripts, so a file probe reports every
// family as absent — which is how uninstall came to skip sync and context
// entirely while reporting "no hooks found".
func configHasMarker(agent agentConfig, markers ...string) bool {
	raw, err := os.ReadFile(agent.configPath)
	if err != nil {
		return false
	}
	return matchesAnyMarker(string(raw), markers)
}

type hookFamilyState string

const (
	hookStateConfigured hookFamilyState = "configured"
	hookStatePartial    hookFamilyState = "partial"
	hookStateLegacy     hookFamilyState = "legacy"
	hookStateAbsent     hookFamilyState = "not configured"
	hookStateInvalid    hookFamilyState = "invalid config"
)

// familyConfigState verifies the markers on their exact events. A marker in a
// wrong event or a duplicate entry is partial, not configured; legacy adapters
// are called out so reinstall is an actionable repair.
func familyConfigState(agent agentConfig, events []string, current, legacy []string) hookFamilyState {
	raw, err := os.ReadFile(agent.configPath)
	if os.IsNotExist(err) {
		return hookStateAbsent
	}
	if err != nil {
		return hookStateInvalid
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return hookStateInvalid
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return hookStateAbsent
	}

	expected := make(map[string]bool, len(events))
	for _, event := range events {
		expected[event] = true
	}
	currentTotal := 0
	legacyTotal := 0
	currentByEvent := map[string]int{}
	for event, rawEntries := range hooks {
		entries, _ := rawEntries.([]interface{})
		for _, entry := range entries {
			encoded, _ := json.Marshal(entry)
			entryText := string(encoded)
			if matchesAnyMarker(entryText, current) {
				currentTotal++
				if expected[event] {
					currentByEvent[event]++
				}
			}
			if matchesAnyMarker(entryText, legacy) {
				legacyTotal++
			}
		}
	}

	if legacyTotal > 0 {
		return hookStateLegacy
	}
	complete := currentTotal == len(events)
	for _, event := range events {
		complete = complete && currentByEvent[event] == 1
	}
	if complete {
		return hookStateConfigured
	}
	if currentTotal > 0 {
		return hookStatePartial
	}
	return hookStateAbsent
}

func contextFamilyState(agent agentConfig) hookFamilyState {
	return familyConfigState(agent, []string{agent.eventName}, []string{contextHookMarker}, []string{agent.scriptName})
}

func syncFamilyState(agent agentConfig) hookFamilyState {
	events := claudeSyncEvents
	if agent.name == "Codex" {
		events = codexSyncEvents
	}
	return familyConfigState(agent, events, []string{syncHookMarker}, []string{syncHookScriptName})
}

func gateFamilyState(agent agentConfig) hookFamilyState {
	return familyConfigState(agent, []string{"PreToolUse"}, []string{gateHookMarker}, []string{gateHookScriptName})
}

func matchesAnyMarker(raw string, markers []string) bool {
	for _, m := range markers {
		if containsStr(raw, m) {
			return true
		}
	}
	return false
}

// removeHookEntries drops every entry whose JSON carries one of the markers
// from each named event, preserving all other events, entries and top-level
// settings. It reports a removal only once the rewritten file is on disk: a
// marshal or write failure leaves the entries installed, and claiming
// otherwise would print "removed" over a settings file still full of them.
func removeHookEntries(configPath string, events []string, markers ...string) (bool, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, nil
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil || settings == nil {
		return false, nil
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		return false, nil
	}

	removed := false
	for _, event := range events {
		entries, ok := hooks[event].([]interface{})
		if !ok {
			continue
		}
		var kept []interface{}
		for _, e := range entries {
			raw, _ := json.Marshal(e)
			if matchesAnyMarker(string(raw), markers) {
				removed = true
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	if !removed {
		return false, nil
	}
	settings["hooks"] = hooks

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, fmt.Errorf("cannot serialize %s: %w", configPath, err)
	}
	if err := os.WriteFile(configPath, out, 0o644); err != nil {
		return false, fmt.Errorf("cannot write %s: %w", configPath, err)
	}
	return true, nil
}

func contextFamilyInstalled(agent agentConfig) bool {
	return contextFamilyState(agent) == hookStateConfigured
}

// uninstallContextFamily removes the context entries from the agent's own
// event. Cursor, Codex and Claude Code all store hooks as an event map, so one
// implementation serves all three.
func uninstallContextFamily(agent agentConfig) (bool, error) {
	return removeHookEntries(agent.configPath, []string{agent.eventName}, contextHookMarker, agent.scriptName)
}

// writeCursorConfig MERGES into .cursor/hooks.json. It used to build a fresh
// config and write it whole, which destroyed whatever hooks the project already
// ran — the same defect EPIC-SYNC-009 fixed for Claude Code and left in place
// here (`RUN:2026-08-12`, found in a client repo).
func writeCursorConfig(agent agentConfig) error {
	config := readJSONObject(agent.configPath)
	config["version"] = 1

	hooks, _ := config["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	entry := map[string]interface{}{
		"command":    contextHookCommand(agent.eventName),
		"timeout":    60,
		"failClosed": false,
	}

	existing, _ := hooks[agent.eventName].([]interface{})
	kept := dropLegacyEntries(existing, agent.scriptName)
	if !containsSyncMarker(kept, contextHookMarker) {
		kept = append(kept, interface{}(entry))
	}
	hooks[agent.eventName] = kept
	config["hooks"] = hooks

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

// readJSONObject reads a JSON object, returning an empty one when the file is
// absent or unreadable. A malformed file is NOT silently replaced — callers
// merge into what they get, so the worst case is an added key, not a lost file.
func readJSONObject(path string) map[string]interface{} {
	var obj map[string]interface{}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &obj)
	}
	if obj == nil {
		obj = map[string]interface{}{}
	}
	return obj
}

// dropLegacyEntries removes entries referencing a script this version no longer
// writes — otherwise a stale entry counts as "installed" and the repair path
// cannot repair.
func dropLegacyEntries(entries []interface{}, scriptName string) []interface{} {
	var kept []interface{}
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if !containsStr(string(raw), scriptName) {
			kept = append(kept, e)
		}
	}
	return kept
}

func writeClaudeConfig(agent agentConfig) error {
	// Claude Code uses settings.json with a hooks section
	// We need to merge with existing settings if present
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}

	// MERGE into the hooks section (EPIC-SYNC-009 fix: the previous code
	// replaced the whole section, clobbering user hooks + other families)
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	// No "args" key: its presence switches Claude Code to exec form, which
	// posix_spawns the command string as a literal executable name. The
	// context command is a shell pipeline, so it must stay in shell form.
	entry := map[string]interface{}{
		"matcher": "*",
		"hooks": []map[string]interface{}{
			{
				"type":    "command",
				"command": contextHookCommand(agent.eventName),
				"timeout": 60,
			},
		},
	}

	// Drop any entry left by an older install: it points at a script this
	// version deletes, so keeping it would fire a missing file on every prompt
	// — and counting it as "installed" would make reinstall unable to repair
	// (RUN:2026-08-10, the sync family's defect).
	existing, _ := hooks[agent.eventName].([]interface{})
	var kept []interface{}
	for _, e := range existing {
		raw, _ := json.Marshal(e)
		if !containsStr(string(raw), agent.scriptName) {
			kept = append(kept, e)
		}
	}
	if !containsSyncMarker(kept, contextHookMarker) {
		kept = append(kept, interface{}(entry))
	}
	hooks[agent.eventName] = kept
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

func writeCodexConfig(agent agentConfig) error {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return err
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	// Codex ignores matchers for UserPromptSubmit. Keep the entry honest and
	// omit one rather than suggesting the prompt stream is filtered here.
	entry := map[string]interface{}{
		"hooks": []map[string]interface{}{
			{
				"type":    "command",
				"command": contextHookCommand(agent.eventName),
				"timeout": 60,
			},
		},
	}

	// Replace only ModernPath-owned context entries. This both migrates the
	// old script adapter and collapses duplicates from earlier installs while
	// preserving every project-owned matcher group and event.
	existing, _ := hooks[agent.eventName].([]interface{})
	kept := dropEntriesWithMarkers(existing, contextHookMarker, agent.scriptName)
	hooks[agent.eventName] = append(kept, interface{}(entry))
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

func dropEntriesWithMarkers(entries []interface{}, markers ...string) []interface{} {
	kept := make([]interface{}, 0, len(entries))
	for _, entry := range entries {
		raw, _ := json.Marshal(entry)
		if matchesAnyMarker(string(raw), markers) {
			continue
		}
		kept = append(kept, entry)
	}
	return kept
}

// runHooksUninstall removes every family by its marker and reports exactly the
// ones it removed. It used to gate sync and context on a script file that this
// version never writes, so both survived every uninstall, and the gate was
// removed without ever being mentioned — leaving "No ModernPath hooks found to
// remove" printed over a settings file still full of them.
func runHooksUninstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	var removed []string
	var failed bool

	report := func(family string, ok bool, err error) {
		if err != nil {
			failed = true
			printError("%s: %v\n", family, err)
			return
		}
		if ok {
			removed = append(removed, family)
		}
	}

	for _, agentKey := range []string{"claude", "cursor", "codex"} {
		agent := hookAgents[agentKey]

		// A legacy install owned a script; this one writes none. Remove the
		// leftover, but never read its absence as "nothing is installed".
		_ = os.Remove(filepath.Join(agent.hooksDir, agent.scriptName))

		ok, err := uninstallContextFamily(agent)
		report(agent.name+" (context)", ok, err)

		if agent.name != "Claude Code" && agent.name != "Codex" {
			continue
		}
		// sync family (EPIC-SYNC-009 / EPIC-SYNC-012): SessionStart · Stop · SessionEnd
		if agent.name == "Codex" {
			ok, err = uninstallSyncFamilyCodex(agent)
		} else {
			ok, err = uninstallSyncFamilyClaude(agent)
		}
		report(agent.name+" (sync)", ok, err)
		// gate family (REQ-CROSS-030): the PreToolUse entry
		if agent.name == "Codex" {
			ok, err = uninstallGateFamilyCodex(agent)
		} else {
			ok, err = uninstallGateFamilyClaude(agent)
		}
		report(agent.name+" (gate)", ok, err)
		// brief family (REQ-CROSS-277): Claude Code only
		if agent.name == "Claude Code" {
			ok, err = uninstallBriefFamilyClaude(agent)
			report(agent.name+" (brief)", ok, err)
		}
	}

	if len(removed) > 0 {
		printSuccess("ModernPath hooks removed: %s\n", strings.Join(removed, ", "))
	} else if !failed {
		printInfo("No ModernPath hooks found to remove.\n")
	}
	if failed {
		return fmt.Errorf("some hooks are still installed: their settings file could not be rewritten")
	}

	return nil
}

func reportCodexStatusFamily(label string, state hookFamilyState) {
	if state == hookStateConfigured {
		printSuccess("  %s: %s\n", label, state)
		return
	}
	printWarning("  %s: %s\n", label, state)
}

// reportSyncFamily prints the sync family for Claude Code.
//
// The tri-state comes first: `legacy` and `invalid config` are neither installed
// nor partially installed, and folding them into "not installed" loses the one
// detail that tells the reader what to do about it. Only once the family is in
// the current form does the per-trigger breakdown mean anything — reported per
// trigger, not as one boolean, because one surviving entry used to report the
// whole family installed, and a family that runs on one of its three triggers
// silently stops syncing at the other two (REQ-CROSS-102, REQ-CROSS-118).
func reportSyncFamily(agent agentConfig) {
	switch state := syncFamilyState(agent); state {
	case hookStateLegacy:
		printWarning("  Sync hooks: LEGACY script form — re-run 'modernpath hooks install' to move to the CLI's own command\n")
		return
	case hookStateInvalid:
		printWarning("  Sync hooks: %s — %s could not be parsed\n", state, agent.configPath)
		return
	}

	switch wired, missing := syncFamilyWiring(agent); {
	case len(wired) == 0:
		printWarning("  Sync hooks: not installed\n")
	case len(missing) == 0:
		printSuccess("  Sync hooks: installed (%s → detached --if-quiescent sync)\n", strings.Join(wired, " · "))
	default:
		printWarning("  Sync hooks: PARTIALLY installed — %s wired, %s missing. Re-run 'modernpath hooks install'.\n",
			strings.Join(wired, " · "), strings.Join(missing, " · "))
	}
}

// runHooksStatus reports each hook family by its settings marker, never by a
// script on disk: this install writes no scripts, so a file probe reports a
// correctly wired repository as broken (the same defect uninstall had).
func runHooksStatus(cmd *cobra.Command, args []string) error {
	fmt.Println("ModernPath Hooks Status")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	for _, key := range []string{"claude", "cursor", "codex"} {
		agent := hookAgents[key]
		fmt.Printf("%s:\n", agent.name)
		if agent.name == "Codex" {
			reportCodexStatusFamily("Context hook", contextFamilyState(agent))
			reportCodexStatusFamily("Sync hooks", syncFamilyState(agent))
			reportCodexStatusFamily("Process gate", gateFamilyState(agent))
			printInfo("  Trust/execution: check /hooks in Codex; ModernPath can only report project configuration.\n")
			fmt.Println()
			continue
		}

		if contextFamilyInstalled(agent) {
			printSuccess("  Context hook: installed (%s)\n", agent.configPath)
		} else {
			printWarning("  Context hook: not installed\n")
		}

		if agent.name != "Claude Code" {
			printInfo("  Sync hooks: deferred (event vocabulary unverified — EPIC-SYNC-009)\n")
			printInfo("  Process gate: deferred (PreToolUse deny protocol — REQ-CROSS-030)\n")
			fmt.Println()
			continue
		}

		// Reported per trigger, not as one boolean: one surviving entry used to
		// report the whole family installed, and a family that runs on one of
		// its three triggers is a family that silently stops syncing at the
		// other two (REQ-CROSS-102).
		reportSyncFamily(agent)
		if gateFamilyInstalled(agent) {
			printSuccess("  Process gate: armed (PreToolUse → modernpath check --hook PreToolUse)\n")
		} else {
			printWarning("  Process gate: not armed\n")
		}
		fmt.Println()
	}

	// Check modernpath init
	if config.IsInitialized() {
		printSuccess("ModernPath: Initialized ✓\n")
	} else {
		printWarning("ModernPath: Not initialized (run 'modernpath init')\n")
	}

	return nil
}
