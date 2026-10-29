package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
)

// DevTool represents a supported AI coding agent
type DevTool struct {
	Name        string
	Command     string
	Description string
	ConfigFile  string
}

var supportedTools = map[string]DevTool{
	"opencode": {
		Name:        "OpenCode",
		Command:     "opencode",
		Description: "Open source AI coding agent (opencode.ai)",
		ConfigFile:  "opencode.json",
	},
	"cursor": {
		Name:        "Cursor",
		Command:     "cursor",
		Description: "Cursor AI-powered code editor",
		ConfigFile:  ".cursor/mcp.json",
	},
	"claude": {
		Name:        "Claude Code",
		Command:     "claude",
		Description: "Anthropic's Claude coding assistant CLI",
		ConfigFile:  ".mcp.json",
	},
	"codex": {
		Name:        "Codex CLI",
		Command:     "codex",
		Description: "OpenAI Codex CLI",
		ConfigFile:  ".codex/config.toml",
	},
}

func getConfigPath(toolName string) string {
	switch toolName {
	case "opencode":
		if _, err := os.Stat("opencode.json"); err == nil {
			return "opencode.json"
		}
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "opencode", "opencode.json")
	case "cursor":
		return ".cursor/mcp.json"
	case "claude":
		// Project scope — the location Claude Code loads (REQ-CROSS-211).
		return ".mcp.json"
	default:
		return supportedTools[toolName].ConfigFile
	}
}

func setupMCPForTool(toolName string, cfg *config.Config) error {
	switch toolName {
	case "opencode":
		return setupOpenCodeMCP(cfg)
	case "cursor":
		return setupCursorMCP(cfg)
	case "claude":
		return setupClaudeMCP(cfg)
	default:
		return fmt.Errorf("automatic MCP setup is unavailable for %s; configure %s in the tool settings", toolName, mcpURL(cfg))
	}
}

// The local stack's MCP sign-in (README §Calling the MCP endpoint locally).
// Core's metadata names the gateway's /api/ex/mcp as the resource, so an
// assistant signs in only there, not on core's own :4000. The OIDC mock
// registers Claude Code as a public client with a fixed callback port.
const (
	localMCPURL          = "http://127.0.0.1:4123/api/ex/mcp"
	localMCPClientID     = "modernpath-local-claude"
	localMCPCallbackPort = 4153
)

// localStack reports whether apiURL is core on the local stack's :4000.
func localStack(apiURL string) bool {
	u, err := url.Parse(apiURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1") && u.Port() == "4000"
}

// mcpURL is ModernPath's one MCP address (SR-MCP-001-009), built the way the
// API client builds a request to core's `/api/mcp`: on a platform API host
// platform.CoreURL moves it onto core's `/api/ex` prefix, and on the local
// stack it is the local gateway's. The address names no workspace; the
// assistant signs in at ZITADEL and its token names one.
func mcpURL(cfg *config.Config) string {
	if localStack(cfg.APIURL) {
		return localMCPURL
	}
	return platform.CoreURL(strings.TrimRight(cfg.APIURL, "/") + "/api/mcp")
}

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Launch AI coding agents for development",
	Long: `Launch AI coding agents (OpenCode, Cursor, Claude Code, etc.) for development tasks.

The ModernPath MCP server is automatically configured for the selected tool,
giving the AI agent access to your project's architecture, specs, and context.

Subcommands:
  dev run <tool> [prompt] - Simple one-shot task execution
  dev setup [tool]      - Setup MCP server for a tool
  dev list              - List supported AI coding agents

Examples:
  modernpath dev run opencode "implement the game loop"
  modernpath dev run opencode "implement the game loop"`,
}

var devSetupCmd = &cobra.Command{
	Use:   "setup [tool]",
	Short: "Setup MCP server for an AI coding agent",
	Long: `Configure the ModernPath MCP server for a specific AI coding agent.

This adds the ModernPath MCP server to the tool's configuration, giving it
access to your project's architecture, specs, search, and context building.

Supported tools: opencode, cursor, claude, codex`,
	RunE: runDevSetup,
}

var devListCmd = &cobra.Command{
	Use:   "list",
	Short: "List supported AI coding agents",
	RunE:  runDevList,
}

var devRunCmd = &cobra.Command{
	Use:   "run [tool] [task]",
	Short: "Launch AI coding agent with a task (one-shot)",
	Long: `Launch an AI coding agent with an optional task.

Examples:
  modernpath dev run opencode "implement the game loop"
  modernpath dev run cursor "fix the authentication bug"`,
	RunE: runDev,
}

func init() {
	rootCmd.AddCommand(devCmd)

	// Add subcommands
	devCmd.AddCommand(devRunCmd)
	devCmd.AddCommand(devSetupCmd)
	devCmd.AddCommand(devListCmd)
	devListCmd.Flags().Bool("json", false, "Emit supported agents as JSON")
	devSetupCmd.Flags().Bool("json", false, "Emit the setup result as JSON")

	// --setup rides dev run — registering it on the RunE-less dev group made
	// it unreachable (pre-PR review finding A4).
	devRunCmd.Flags().Bool("setup", false, "Setup MCP for tool without running")
	devRunCmd.Flags().Bool("json", false, "Emit the setup result when used with --setup")

}

func runDev(cmd *cobra.Command, args []string) error {
	// If no subcommand, treat as "dev run" for backward compatibility
	setupOnly, _ := cmd.Flags().GetBool("setup")
	machine, _ := cmd.Flags().GetBool("json")
	if machine && !setupOnly {
		return fmt.Errorf("dev run --json requires --setup; launched tools write their own output to the terminal")
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printWarning("No ModernPath config found. Some features may be limited.\n")
		cfg = &config.Config{APIURL: "http://localhost:4000"}
	}

	// Determine tool
	var toolName string
	if len(args) > 0 {
		toolName = strings.ToLower(args[0])
	} else {
		return fmt.Errorf("tool is required; choose one with 'modernpath dev list'")
	}

	tool, ok := supportedTools[toolName]
	if !ok {
		return fmt.Errorf("unknown tool %q; choose one with 'modernpath dev list'", toolName)
	}

	// Check if tool is installed
	if _, err := exec.LookPath(tool.Command); err != nil {
		return fmt.Errorf("%s is not installed or not in PATH", tool.Name)
	}

	// Setup MCP if not already configured
	if err := setupMCPForTool(toolName, cfg); err != nil {
		return fmt.Errorf("failed to setup MCP: %w", err)
	}

	if setupOnly {
		if machine {
			return writeCommandJSON(cmd, map[string]any{"configured": true, "tool": toolName, "config_path": getConfigPath(toolName)})
		}
		printSuccess("ModernPath MCP configured for %s\n", tool.Name)
		return nil
	}
	printSuccess("ModernPath MCP configured for %s\n", tool.Name)

	// Get task
	var task string
	if len(args) > 1 {
		task = strings.Join(args[1:], " ")
	}

	// Launch the tool
	return launchTool(tool, task, cfg)
}

func runDevSetup(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		cfg = &config.Config{APIURL: "http://localhost:4000"}
	}

	var toolName string
	if len(args) > 0 {
		toolName = strings.ToLower(args[0])
	} else {
		return fmt.Errorf("tool is required; choose one with 'modernpath dev list'")
	}

	tool, ok := supportedTools[toolName]
	if !ok {
		return fmt.Errorf("unknown tool %q; choose one with 'modernpath dev list'", toolName)
	}

	if err := setupMCPForTool(toolName, cfg); err != nil {
		printError("Failed to setup MCP: %v\n", err)
		return err
	}

	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, map[string]any{"configured": true, "tool": toolName, "config_path": getConfigPath(toolName)})
	}
	printSuccess("ModernPath MCP configured for %s!\n", tool.Name)
	printInfo("Config file: %s\n", getConfigPath(toolName))
	return nil
}

func runDevList(cmd *cobra.Command, args []string) error {
	names := make([]string, 0, len(supportedTools))
	for name := range supportedTools {
		names = append(names, name)
	}
	sort.Strings(names)
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		rows := make([]map[string]any, 0, len(names))
		for _, name := range names {
			tool := supportedTools[name]
			_, lookupErr := exec.LookPath(tool.Command)
			rows = append(rows, map[string]any{"name": name, "description": tool.Description, "installed": lookupErr == nil})
		}
		return writeCommandJSON(cmd, rows)
	}
	fmt.Println()
	fmt.Println("Supported AI Coding Agents:")
	fmt.Println("─────────────────────────────────────────")

	for _, name := range names {
		tool := supportedTools[name]
		installed := "✗"
		if _, err := exec.LookPath(tool.Command); err == nil {
			installed = "✓"
		}
		fmt.Printf("  [%s] %-10s  %s\n", installed, name, tool.Description)
	}

	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  modernpath dev run <tool> [prompt] Launch tool with an optional prompt")
	fmt.Println("  modernpath dev setup <tool>      Configure MCP for tool")
	fmt.Println()
	return nil
}

func selectTool() (string, error) {
	items := []string{}
	for name, tool := range supportedTools {
		installed := ""
		if _, err := exec.LookPath(tool.Command); err == nil {
			installed = " ✓"
		}
		items = append(items, fmt.Sprintf("%s - %s%s", name, tool.Description, installed))
	}

	prompt := promptui.Select{
		Label:    "Select AI coding agent",
		Items:    items,
		HideHelp: true,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return "", err
	}

	// Extract tool name from selection
	toolNames := []string{"opencode", "cursor", "claude", "codex"}
	return toolNames[idx], nil
}

func launchTool(tool DevTool, task string, cfg *config.Config) error {
	printInfo("Launching %s...\n", tool.Name)

	var cmd *exec.Cmd

	if task != "" {
		// Build context-rich prompt
		prompt := buildDevPrompt(task, cfg)

		switch tool.Command {
		case "opencode":
			// OpenCode can receive initial prompt via stdin or argument
			cmd = exec.Command(tool.Command)
			// Write prompt to a temp file and use it
			promptFile := filepath.Join(os.TempDir(), "modernpath-prompt.txt")
			os.WriteFile(promptFile, []byte(prompt), 0644)
			printInfo("Task prompt saved to: %s\n", promptFile)
			printInfo("Paste this in %s:\n\n%s\n\n", tool.Name, task)
		case "cursor":
			cmd = exec.Command(tool.Command, ".")
			printInfo("Open Cursor and use this prompt:\n\n%s\n\n", prompt)
		default:
			cmd = exec.Command(tool.Command)
			printInfo("Use this prompt:\n\n%s\n\n", prompt)
		}
	} else {
		cmd = exec.Command(tool.Command)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func buildDevPrompt(task string, cfg *config.Config) string {
	var sb strings.Builder

	sb.WriteString(task)
	sb.WriteString("\n\n")
	sb.WriteString("Use the modernpath MCP tools to:\n")
	sb.WriteString("- Find the system to work in with `list_systems`\n")
	sb.WriteString("- Get context about the code for this task with `context_search`\n")
	sb.WriteString("- Find epics and requirements with `search_epics` and `search_requirements`\n")
	sb.WriteString("- See how a requirement is implemented and tested with `trace_requirement`\n")
	sb.WriteString("- Ask questions about the system with `chat`\n")

	if cfg.SystemName != "" {
		sb.WriteString(fmt.Sprintf("\nProject: %s\n", cfg.SystemName))
	}

	return sb.String()
}

// MCP Setup functions for each tool

func setupOpenCodeMCP(cfg *config.Config) error {
	configPath := getConfigPath("opencode")

	// Ensure directory exists
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Read existing config or create new
	var config map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &config)
	}
	if config == nil {
		config = make(map[string]interface{})
	}

	// Add MCP configuration
	mcp, ok := config["mcp"].(map[string]interface{})
	if !ok {
		mcp = make(map[string]interface{})
	}

	mcp["modernpath"] = map[string]interface{}{
		"type":    "remote",
		"url":     mcpURL(cfg),
		"enabled": true,
	}

	config["mcp"] = mcp
	config["$schema"] = "https://opencode.ai/config.json"

	// Write config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func setupCursorMCP(cfg *config.Config) error {
	configPath := ".cursor/mcp.json"

	// Ensure directory exists
	if err := os.MkdirAll(".cursor", 0755); err != nil {
		return err
	}

	// Read existing config or create new
	var mcpConfig map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		json.Unmarshal(data, &mcpConfig)
	}
	if mcpConfig == nil {
		mcpConfig = make(map[string]interface{})
	}

	// Add MCP server
	servers, ok := mcpConfig["mcpServers"].(map[string]interface{})
	if !ok {
		servers = make(map[string]interface{})
	}

	servers["modernpath"] = map[string]interface{}{
		"url": mcpURL(cfg),
	}

	mcpConfig["mcpServers"] = servers

	// Write config
	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

// claudeMCPOAuth is the pre-registered app Claude Code signs in with: neither
// ZITADEL nor the local mock offers dynamic client registration. Nil where
// no app is known, as on production today.
func claudeMCPOAuth(cfg *config.Config) map[string]interface{} {
	if localStack(cfg.APIURL) {
		return map[string]interface{}{"clientId": localMCPClientID, "callbackPort": localMCPCallbackPort}
	}
	if p, ok := zitadel.ProfileForAPIURL(strings.TrimRight(cfg.APIURL, "/")); ok && p.MCPClientID != "" {
		return map[string]interface{}{"clientId": p.MCPClientID}
	}
	return nil
}

func setupClaudeMCP(cfg *config.Config) error {
	// Project-scope .mcp.json is the location Claude Code actually loads;
	// the old ~/.claude/mcp_servers.json target was a file nothing reads —
	// setup claimed success over a no-op (REQ-CROSS-211, USER:2026-08-18).
	configPath := ".mcp.json"

	// Merge-preserving: existing servers survive.
	var mcpConfig map[string]interface{}
	if data, err := os.ReadFile(configPath); err == nil {
		if err := json.Unmarshal(data, &mcpConfig); err != nil {
			return fmt.Errorf(".mcp.json exists but is not valid JSON — fix or remove it: %w", err)
		}
	}
	if mcpConfig == nil {
		mcpConfig = make(map[string]interface{})
	}

	servers, _ := mcpConfig["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = make(map[string]interface{})
	}

	server := map[string]interface{}{
		"type": "http",
		"url":  mcpURL(cfg),
	}
	if oauth := claudeMCPOAuth(cfg); oauth != nil {
		server["oauth"] = oauth
	}
	servers["modernpath"] = server
	mcpConfig["mcpServers"] = servers

	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}
