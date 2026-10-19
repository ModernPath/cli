package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	hooksCursor    bool
	hooksClaude    bool
	hooksCodex     bool
	hooksPi        bool
	hooksAll       bool
	hooksNoContext bool
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
  - Pi          (.pi/settings.json + .pi/extensions/modernpath.ts)

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
  --pi        Install for Pi only
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
	hooksInstallCmd.Flags().BoolVar(&hooksPi, "pi", false, "Install for Pi only")
	hooksInstallCmd.Flags().BoolVar(&hooksAll, "all", false, "Install for all agents")
	hooksInstallCmd.Flags().BoolVar(&hooksNoContext, "no-context", false, "Skip the context-injection hook family")
	hooksInstallCmd.Flags().Bool("json", false, "Emit installed agents as JSON")
	hooksUninstallCmd.Flags().Bool("json", false, "Emit removed hook families as JSON")
	hooksStatusCmd.Flags().Bool("json", false, "Emit hook status as JSON")
	addRecordReadFlags(hooksStatusCmd)

	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)
}

// Hook script shared by all agents
// contextHookMarker identifies our context entry in an agent's config — the
// command itself, since the hook no longer owns a file to be named after.
const contextHookMarker = "context --hook"

const retiredGateScript = "rdd-gate.sh"

// contextHookCommand runs the installed CLI through the hook prelude (see
// hooks_binary.go): the workspace link first, PATH second, and `|| echo '{}'`
// guarantees the agent a well-formed response whatever happens — a context
// hook must never cost the user a prompt.
func contextHookCommand(event string) string {
	return hookCommand("context --hook "+event, "echo '{}'")
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
	"pi": {
		name:       "Pi",
		hooksDir:   ".pi/extensions",
		configPath: ".pi/settings.json",
		scriptName: piExtensionName,
		eventName:  "before_agent_start",
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

	// Check for Pi
	if _, err := os.Stat(".pi"); err == nil {
		detected = append(detected, "pi")
	}

	return detected
}

func runHooksInstall(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	machine, _ := cmd.Flags().GetBool("json")

	// Determine which agents to install for
	var targetAgents []string

	if hooksAll {
		targetAgents = []string{"cursor", "claude", "codex", "pi"}
	} else if hooksCursor || hooksClaude || hooksCodex || hooksPi {
		if hooksCursor {
			targetAgents = append(targetAgents, "cursor")
		}
		if hooksClaude {
			targetAgents = append(targetAgents, "claude")
		}
		if hooksCodex {
			targetAgents = append(targetAgents, "codex")
		}
		if hooksPi {
			targetAgents = append(targetAgents, "pi")
		}
	} else {
		// Auto-detect
		targetAgents = detectInstalledAgents()
		if len(targetAgents) == 0 {
			// Default to cursor if nothing detected
			targetAgents = []string{"cursor"}
			if !machine {
				printInfo("No agent configs detected, defaulting to Cursor.\n")
			}
		} else {
			if !machine {
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
	}

	// The binary that installs the hooks is the binary the hooks run: the
	// commands written below prefer this link over whatever PATH the harness
	// gives a hook shell (BACKLOG-TOOL-33, -43).
	binaryTarget := ""
	if root, err := os.Getwd(); err == nil {
		target, note, err := linkHooksBinary(root)
		switch {
		case err != nil:
			if !machine {
				printWarning("Hooks binary: could not write %s (%v) — the hooks resolve modernpath on PATH\n", hooksBinaryRel, err)
			}
		case note != "":
			binaryTarget = note
			if !machine {
				printInfo("Hooks binary: %s\n", note)
			}
		default:
			binaryTarget = target
			if !machine {
				printSuccess("Hooks binary: %s → %s (version %s)\n", hooksBinaryRel, target, Version)
			}
		}
	}

	if !config.IsInitialized() {
		return fmt.Errorf("no system is bound here — run 'modernpath init' before installing server-backed hooks")
	}

	// Install for each target agent
	var installed []string
	var failures []string
	for _, agentKey := range targetAgents {
		agent := hookAgents[agentKey]
		if err := installForAgent(agent); err != nil {
			failures = append(failures, agent.name+": "+err.Error())
		} else {
			installed = append(installed, agent.name)
		}
	}
	if installed == nil {
		installed = []string{}
	}
	if failures == nil {
		failures = []string{}
	}
	if machine {
		if err := writeCommandJSON(cmd, map[string]any{"installed": installed, "failed": failures, "binary_target": binaryTarget}); err != nil {
			return err
		}
		if len(failures) > 0 {
			return fmt.Errorf("hooks install failed for %s", strings.Join(failures, "; "))
		}
		return nil
	}
	if len(failures) > 0 {
		for _, failure := range failures {
			printError("%s\n", failure)
		}
		return fmt.Errorf("hooks install failed for %s", strings.Join(failures, "; "))
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
		if !containsAgentName(installed, "Codex") && !containsAgentName(installed, "Pi") {
			printInfo("Restart your IDE to activate the hooks.\n")
		}
		printCodexTrustGuidance(installed)
		printPiTrustGuidance(installed)
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

func printPiTrustGuidance(agentNames []string) {
	if containsAgentName(agentNames, "Pi") {
		printInfo("Pi loads project .pi/ only after the project is trusted. Approve the prompt, or start with `pi --approve`.\n")
	}
}

func installForAgent(agent agentConfig) error {
	if agent.name == "Pi" {
		return installPi(agent)
	}
	if _, err := removeRetiredHookEntries(agent); err != nil {
		return err
	}

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

	return nil
}

// removeRetiredHookEntries prunes retired command leaves from each hook event,
// preserving matcher groups, metadata and neighboring project hooks.
func removeRetiredHookEntries(agent agentConfig) (bool, error) {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return false, err
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	removed := false
	if hooks != nil {
		for event, rawEntries := range hooks {
			kept, changed := pruneRetiredHookValue(rawEntries)
			if changed {
				removed = true
				if entries, ok := kept.([]interface{}); ok && len(entries) == 0 {
					delete(hooks, event)
				} else {
					hooks[event] = kept
				}
			}
		}
	}
	if removed {
		settings["hooks"] = hooks
		if err := writeSettingsFile(agent.configPath, settings); err != nil {
			return false, fmt.Errorf("cannot write %s: %w", agent.configPath, err)
		}
	}
	managedPath := filepath.Join(agent.hooksDir, retiredGateScript)
	_, statErr := os.Lstat(managedPath)
	managedScriptPresent := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return removed, fmt.Errorf("cannot inspect retired hook script %s: %w", managedPath, statErr)
	}
	if err := os.Remove(managedPath); err != nil && !os.IsNotExist(err) {
		return removed, fmt.Errorf("cannot remove retired hook script %s: %w", managedPath, err)
	}
	return removed || managedScriptPresent, nil
}

func retiredHookCommand(command string) bool {
	if strings.Contains(command, hookPrelude) {
		if strings.Contains(command, `"$MP" check --hook`) || strings.Contains(command, `"$MP" your-move --hook`) {
			return true
		}
	}
	if args, ok := strings.CutPrefix(command, "modernpath "); ok {
		if strings.HasPrefix(args, "check --hook") || strings.HasPrefix(args, "your-move --hook") {
			return true
		}
	}
	return containsManagedScriptPath(command, retiredGateScript)
}

func containsManagedScriptPath(command, name string) bool {
	for start := 0; start < len(command); {
		i := strings.Index(command[start:], name)
		if i < 0 {
			return false
		}
		i += start
		end := i + len(name)
		beforeOK := i == 0 || command[i-1] == '/' || command[i-1] == '"' || command[i-1] == '\'' || command[i-1] == ' ' || command[i-1] == '\t'
		afterOK := end == len(command) || command[end] == '"' || command[end] == '\'' || command[end] == ' ' || command[end] == '\t' || command[end] == ';'
		if beforeOK && afterOK {
			return true
		}
		start = end
	}
	return false
}

// pruneRetiredHookValue follows only hook command leaves and nested `hooks`
// arrays, so descriptions, matcher metadata and unrelated settings are opaque.
func pruneRetiredHookValue(value interface{}) (interface{}, bool) {
	pruned, changed, _ := pruneRetiredHookNode(value)
	return pruned, changed
}

func pruneRetiredHookNode(value interface{}) (interface{}, bool, bool) {
	switch value := value.(type) {
	case []interface{}:
		kept := make([]interface{}, 0, len(value))
		changed := false
		for _, entry := range value {
			pruned, childChanged, remove := pruneRetiredHookNode(entry)
			if remove {
				changed = true
			} else {
				kept = append(kept, pruned)
				changed = changed || childChanged
			}
		}
		return kept, changed, false
	case map[string]interface{}:
		if command, ok := value["command"].(string); ok && retiredHookCommand(command) {
			return nil, true, true
		}
		children, ok := value["hooks"]
		if !ok {
			return value, false, false
		}
		pruned, changed, _ := pruneRetiredHookNode(children)
		if changed {
			if entries, ok := pruned.([]interface{}); ok && len(entries) == 0 {
				return nil, true, true
			}
			value["hooks"] = pruned
		}
		return value, changed, false
	default:
		return value, false, false
	}
}

func hasRetiredHook(configText string) bool {
	var settings map[string]interface{}
	if json.Unmarshal([]byte(configText), &settings) != nil {
		return false
	}
	hooks, _ := settings["hooks"].(map[string]interface{})
	for _, value := range hooks {
		if _, changed := pruneRetiredHookValue(value); changed {
			return true
		}
	}
	return false
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
		if raw != nil && strings.Contains(string(raw), marker) {
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
	if agent.name == "Pi" {
		return piExtensionState(agent, contextHookMarker)
	}
	return familyConfigState(agent, []string{agent.eventName}, []string{contextHookMarker}, []string{agent.scriptName})
}

func matchesAnyMarker(raw string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(raw, m) {
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
	if err := writeSettingsFile(configPath, settings); err != nil {
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
	return writeSettingsFile(agent.configPath, config)
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
		if !strings.Contains(string(raw), scriptName) {
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

	// Replace the entry an earlier install wrote — whether it points at the
	// script this version deletes (RUN:2026-08-10) or carries an older command
	// shape — and keep every other hook, as the gate and brief families do. The
	// previous code appended only when no entry carried the marker, so a stale
	// context command survived every reinstall (BACKLOG-TOOL-33: the bare-name
	// command stayed in .claude/settings.json after the launcher changed).
	existing, _ := hooks[agent.eventName].([]interface{})
	hooks[agent.eventName] = replaceOwnedEntry(existing, entry, contextHookMarker, agent.scriptName)
	settings["hooks"] = hooks
	return writeSettingsFile(agent.configPath, settings)
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
	hooks[agent.eventName] = replaceOwnedEntry(existing, entry, contextHookMarker, agent.scriptName)
	settings["hooks"] = hooks
	return writeSettingsFile(agent.configPath, settings)
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
	machine, _ := cmd.Flags().GetBool("json")
	var removed []string
	var failed bool
	var failures []string

	report := func(family string, ok bool, err error) {
		if err != nil {
			failed = true
			failures = append(failures, fmt.Sprintf("%s: %v", family, err))
			if !machine {
				printError("%s: %v\n", family, err)
			}
			return
		}
		if ok {
			removed = append(removed, family)
		}
	}

	for _, agentKey := range []string{"claude", "cursor", "codex", "pi"} {
		agent := hookAgents[agentKey]

		if agent.name == "Pi" {
			names, err := uninstallPi(agent)
			if err != nil {
				failed = true
				failures = append(failures, fmt.Sprintf("%s: %v", agent.name, err))
				if !machine {
					printError("%s: %v\n", agent.name, err)
				}
				continue
			}
			removed = append(removed, names...)
			continue
		}

		legacyRemoved, legacyErr := removeRetiredHookEntries(agent)
		report(agent.name+" (retired hooks)", legacyRemoved, legacyErr)

		// A legacy install owned a script; this one writes none. Remove the
		// leftover, but never read its absence as "nothing is installed".
		_ = os.Remove(filepath.Join(agent.hooksDir, agent.scriptName))

		ok, err := uninstallContextFamily(agent)
		report(agent.name+" (context)", ok, err)

	}
	if removed == nil {
		removed = []string{}
	}

	if machine {
		if err := writeCommandJSON(cmd, map[string]any{"removed": removed, "failed": failures}); err != nil {
			return err
		}
		if failed {
			return fmt.Errorf("some hooks are still installed: their settings file could not be rewritten")
		}
		return nil
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

// Keep retirement in reporting: install and uninstall still need the physical
// wiring state, including hooks left behind when a workspace flips.
func runHooksStatus(cmd *cobra.Command, args []string) error {
	machine, _ := cmd.Flags().GetBool("json")
	rows := make([]map[string]any, 0, len(hookAgents))
	for _, key := range []string{"claude", "cursor", "codex", "pi"} {
		agent := hookAgents[key]
		state := contextFamilyState(agent)
		rows = append(rows, map[string]any{"agent": agent.name, "config_path": agent.configPath, "context_hook": string(state)})
	}
	record := map[string]any{"initialized": config.IsInitialized(), "agents": rows}
	if machine {
		selected, err := selectRecordFields(cmd, record, []string{"initialized", "agents"}, []string{"initialized", "agents"}, []string{"initialized"})
		if err != nil {
			return err
		}
		return writeCommandJSON(cmd, selected)
	}
	full, _ := cmd.Flags().GetBool("full")
	if cmd.Flags().Changed("fields") || full {
		selected, err := selectRecordFields(cmd, record, nil, []string{"initialized", "agents"}, nil)
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
	}
	for _, row := range rows {
		fmt.Printf("%s: %s\n", row["agent"], row["context_hook"])
	}
	if contextFamilyState(hookAgents["codex"]) == hookStateConfigured {
		printSuccess("Context hook: configured; review /hooks in Codex to confirm activation\n")
	}

	// Check modernpath init
	fmt.Printf("ModernPath: initialized=%t\n", config.IsInitialized())

	return nil
}
