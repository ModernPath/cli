package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	hooksCursor bool
	hooksClaude bool
	hooksCodex  bool
	hooksAll    bool
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

	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksUninstallCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)
}

// Hook script shared by all agents
const hookScript = `#!/bin/bash
# ModernPath Context Injection Hook
# Uses LLM-based relevance evaluation and intelligent query decomposition
# Compatible with: Cursor, Claude Code, Codex

set -e

input=$(cat)
prompt=$(echo "$input" | jq -r '.prompt // empty')

if [ -z "$prompt" ]; then
  echo '{}'
  exit 0
fi

if [ ! -f ".modernpath/config.json" ]; then
  echo '{}'
  exit 0
fi

context=$(timeout 55 modernpath context "$prompt" 2>/dev/null || echo "")

if [ -n "$context" ] && [ "$context" != "" ]; then
  full_context="## ModernPath Architectural Context

$context

---"

  escaped=$(echo "$full_context" | jq -Rs .)
  echo "{\"additional_context\": $escaped}"
else
  echo '{}'
fi
`

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
	if !config.IsInitialized() {
		printWarning("ModernPath not initialized in this directory.\n")
		printInfo("Run 'modernpath init' first, then install hooks.\n")
		return nil
	}

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
		printSuccess("ModernPath hooks installed for: ")
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
		printInfo("Restart your IDE to activate the hooks.\n")
	}

	return nil
}

func installForAgent(agent agentConfig) error {
	// Create hooks directory
	if err := os.MkdirAll(agent.hooksDir, 0755); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	// Write hook script
	hookPath := filepath.Join(agent.hooksDir, agent.scriptName)
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		return fmt.Errorf("failed to write hook script: %w", err)
	}

	// Write/update config file based on agent type
	switch agent.name {
	case "Cursor":
		return writeCursorConfig(agent)
	case "Claude Code":
		return writeClaudeConfig(agent)
	case "Codex":
		return writeCodexConfig(agent)
	}

	return nil
}

func writeCursorConfig(agent agentConfig) error {
	config := map[string]interface{}{
		"version": 1,
		"hooks": map[string]interface{}{
			agent.eventName: []map[string]interface{}{
				{
					"command":    filepath.Join(agent.hooksDir, agent.scriptName),
					"timeout":    60,
					"failClosed": false,
				},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

func writeClaudeConfig(agent agentConfig) error {
	// Claude Code uses settings.json with a hooks section
	// We need to merge with existing settings if present
	var settings map[string]interface{}

	if data, err := os.ReadFile(agent.configPath); err == nil {
		json.Unmarshal(data, &settings)
	}
	if settings == nil {
		settings = make(map[string]interface{})
	}

	// Add hooks section
	hooks := map[string]interface{}{
		agent.eventName: []map[string]interface{}{
			{
				"matcher": "*",
				"hooks": []map[string]interface{}{
					{
						"type":    "command",
						"command": filepath.Join(agent.hooksDir, agent.scriptName),
						"args":    []string{},
						"timeout": 60,
					},
				},
			},
		},
	}
	settings["hooks"] = hooks

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

func writeCodexConfig(agent agentConfig) error {
	// Codex uses similar format to Cursor
	config := map[string]interface{}{
		"hooks": map[string]interface{}{
			agent.eventName: []map[string]interface{}{
				{
					"matcher": "*",
					"hooks": []map[string]interface{}{
						{
							"type":    "command",
							"command": filepath.Join(agent.hooksDir, agent.scriptName),
							"timeout": 60,
						},
					},
				},
			},
		},
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(agent.configPath, data, 0644)
}

func runHooksUninstall(cmd *cobra.Command, args []string) error {
	removed := []string{}

	for agentKey, agent := range hookAgents {
		hookPath := filepath.Join(agent.hooksDir, agent.scriptName)
		if _, err := os.Stat(hookPath); err == nil {
			if err := os.Remove(hookPath); err == nil {
				removed = append(removed, hookAgents[agentKey].name)
			}
		}
	}

	if len(removed) > 0 {
		printSuccess("ModernPath hooks removed from: ")
		for i, name := range removed {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Print(name)
		}
		fmt.Println()
		printInfo("Note: Config files may need manual cleanup.\n")
	} else {
		printInfo("No ModernPath hooks found to remove.\n")
	}

	return nil
}

func runHooksStatus(cmd *cobra.Command, args []string) error {
	fmt.Println("ModernPath Hooks Status")
	fmt.Println("═══════════════════════════════════════")
	fmt.Println()

	for _, agent := range hookAgents {
		hookPath := filepath.Join(agent.hooksDir, agent.scriptName)

		fmt.Printf("%s:\n", agent.name)

		// Check hook script
		if _, err := os.Stat(hookPath); err == nil {
			printSuccess("  ✓ Hook script: %s\n", hookPath)
		} else {
			printWarning("  ✗ Hook script not found\n")
		}

		// Check config
		if _, err := os.Stat(agent.configPath); err == nil {
			printSuccess("  ✓ Config: %s\n", agent.configPath)
		} else {
			printWarning("  ✗ Config not found\n")
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
