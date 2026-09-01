package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
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
		ConfigFile:  ".codex/config.json",
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
		return setupGenericMCP(cfg)
	}
}

var devCmd = &cobra.Command{
	Use:   "dev",
	Short: "Launch AI coding agents for development tasks",
	Long: `Launch AI coding agents (OpenCode, Cursor, Claude Code, etc.) for development tasks.

The ModernPath MCP server is automatically configured for the selected tool,
giving the AI agent access to your project's architecture, specs, and context.

Subcommands:
  dev run <tool> [prompt] - Simple one-shot task execution
  dev task [task_id]    - Implement a task once
  dev ralph [task_id]   - Iterative implementation loop (Ralph Wiggum technique)
  dev setup [tool]      - Setup MCP server for a tool
  dev list              - List supported AI coding agents

Examples:
  modernpath dev run opencode "implement the game loop"
  modernpath dev task                           # Interactive task selection
  modernpath dev ralph abc123-uuid --tool opencode`,
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

var devTaskCmd = &cobra.Command{
	Use:   "task [task_id]",
	Short: "Implement a task with an AI coding agent",
	Long: `Start implementing a task using your preferred AI coding agent.

This command:
1. Fetches the task details and related specs from ModernPath
2. Builds a comprehensive prompt with all the context
3. Launches your chosen AI coding agent with the task

Examples:
  modernpath dev task                      # Interactive task selection
  modernpath dev task abc123-uuid          # Specific task
  modernpath dev task --tool opencode      # Use specific tool`,
	RunE: runDevTask,
}

var devRalphCmd = &cobra.Command{
	Use:   "ralph [task_id]",
	Short: "Run iterative AI development loop for a task (Ralph Wiggum technique)",
	Long: `Run an AI coding agent in an iterative loop until the task is complete.

Based on the Ralph Wiggum technique (ghuntley.com/ralph), this command:
1. Builds a comprehensive prompt from task specs and context
2. Launches an AI coding agent (OpenCode, Cursor, etc.)
3. Checks output for completion promise
4. Loops until success or max iterations

The AI sees its previous work in files each iteration, enabling
incremental progress on complex tasks.

Examples:
  modernpath dev ralph                           # Interactive task selection
  modernpath dev ralph abc123-uuid               # Specific task  
  modernpath dev ralph --max-iterations 10       # Limit iterations
  modernpath dev ralph --tool opencode           # Use specific tool
  modernpath dev ralph status                    # Check loop status`,
	RunE: runDevRalph,
}

var devRalphStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current Ralph loop status",
	RunE:  runDevRalphStatus,
}

func init() {
	rootCmd.AddCommand(devCmd)

	// Add subcommands
	devCmd.AddCommand(devRunCmd)
	devCmd.AddCommand(devTaskCmd)
	devCmd.AddCommand(devRalphCmd)
	devCmd.AddCommand(devSetupCmd)
	devCmd.AddCommand(devListCmd)

	devRalphCmd.AddCommand(devRalphStatusCmd)

	// --setup rides dev run — registering it on the RunE-less dev group made
	// it unreachable (pre-PR review finding A4).
	devRunCmd.Flags().Bool("setup", false, "Setup MCP for tool without running")

	// Flags for dev task
	devTaskCmd.Flags().StringVarP(&implementTool, "tool", "t", "", "AI coding agent to use (opencode, cursor, claude, codex)")

	// Flags for dev ralph
	devRalphCmd.Flags().IntVar(&ralphMaxIterations, "max-iterations", 20, "Maximum iterations before stopping")
	devRalphCmd.Flags().IntVar(&ralphMinIterations, "min-iterations", 1, "Minimum iterations before completion allowed")
	devRalphCmd.Flags().StringVar(&ralphCompletionPromise, "completion-promise", "COMPLETE", "Text that signals completion")
	devRalphCmd.Flags().StringVarP(&ralphTool, "tool", "t", "", "AI tool to use (opencode, cursor, claude)")
	devRalphCmd.Flags().BoolVar(&ralphNoCommit, "no-commit", false, "Don't auto-commit after iterations")
	devRalphCmd.Flags().BoolVar(&ralphAll, "all", false, "Work through ALL pending tasks in logical order")
	devRalphCmd.Flags().BoolVar(&ralphYesAll, "yes-all", false, "Tell AI to auto-approve all actions (bash, file writes, etc)")
}

func runDev(cmd *cobra.Command, args []string) error {
	// If no subcommand, treat as "dev run" for backward compatibility
	setupOnly, _ := cmd.Flags().GetBool("setup")

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
		// Interactive selection
		toolName, err = selectTool()
		if err != nil {
			return err
		}
	}

	tool, ok := supportedTools[toolName]
	if !ok {
		printError("Unknown tool: %s\n", toolName)
		printInfo("Supported tools: opencode, cursor, claude, codex\n")
		printInfo("Use 'modernpath dev --help' for available subcommands\n")
		return nil
	}

	// Check if tool is installed
	if _, err := exec.LookPath(tool.Command); err != nil {
		printError("%s is not installed or not in PATH.\n", tool.Name)
		printInfo("Install %s from: https://%s.ai\n", toolName, toolName)
		return nil
	}

	// Setup MCP if not already configured
	if err := setupMCPForTool(toolName, cfg); err != nil {
		printWarning("Failed to setup MCP: %v\n", err)
	} else {
		printSuccess("ModernPath MCP configured for %s\n", tool.Name)
	}

	if setupOnly {
		return nil
	}

	// Get task
	var task string
	if len(args) > 1 {
		task = strings.Join(args[1:], " ")
	}

	// Launch the tool
	return launchTool(tool, task, cfg)
}

// runDevTask implements the task subcommand
func runDevTask(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.EpicID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	// Get task ID
	var taskID string
	if len(args) > 0 {
		taskID = args[0]
	} else {
		// Interactive selection
		var err error
		taskID, err = selectTaskForImplementation(cfg)
		if err != nil {
			return err
		}
	}

	// Fetch task details
	task, err := fetchTaskDetails(cfg, taskID)
	if err != nil {
		printError("Failed to fetch task: %v\n", err)
		return err
	}

	// Fetch related specs
	specs, err := fetchRelatedSpecs(cfg)
	if err != nil {
		printWarning("Failed to fetch specs: %v\n", err)
	}

	// Select tool if not specified
	toolName := implementTool
	if toolName == "" {
		toolName, err = selectToolForImplementation()
		if err != nil {
			return err
		}
	}

	tool, ok := supportedTools[toolName]
	if !ok {
		printError("Unknown tool: %s\n", toolName)
		return nil
	}

	// Setup MCP
	if err := setupMCPForTool(toolName, cfg); err != nil {
		printWarning("Failed to setup MCP: %v\n", err)
	}

	// Build comprehensive prompt
	prompt := buildImplementationPrompt(task, specs, cfg)

	// Save prompt to file for reference
	promptFile := ".modernpath/current-task.md"
	os.WriteFile(promptFile, []byte(prompt), 0644)
	printSuccess("Task prompt saved to: %s\n", promptFile)

	// Launch tool
	printInfo("Launching %s with task: %s\n", tool.Name, task.Name)
	fmt.Println()
	fmt.Println("─────────────────────────────────────────")
	fmt.Println(prompt)
	fmt.Println("─────────────────────────────────────────")
	fmt.Println()

	return launchToolWithPrompt(tool, prompt)
}

// runDevRalph implements the ralph subcommand (moved from ralph.go)
func runDevRalph(cmd *cobra.Command, args []string) error {
	// Call the original runRalph function
	return runRalph(cmd, args)
}

// runDevRalphStatus implements ralph status subcommand
func runDevRalphStatus(cmd *cobra.Command, args []string) error {
	return runRalphStatus(cmd, args)
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
		var err error
		toolName, err = selectTool()
		if err != nil {
			return err
		}
	}

	tool, ok := supportedTools[toolName]
	if !ok {
		printError("Unknown tool: %s\n", toolName)
		return nil
	}

	if err := setupMCPForTool(toolName, cfg); err != nil {
		printError("Failed to setup MCP: %v\n", err)
		return err
	}

	printSuccess("ModernPath MCP configured for %s!\n", tool.Name)
	printInfo("Config file: %s\n", getConfigPath(toolName))
	return nil
}

func runDevList(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("Supported AI Coding Agents:")
	fmt.Println("─────────────────────────────────────────")

	for name, tool := range supportedTools {
		installed := "✗"
		if _, err := exec.LookPath(tool.Command); err == nil {
			installed = "✓"
		}
		fmt.Printf("  [%s] %-10s  %s\n", installed, name, tool.Description)
	}

	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  modernpath dev run <tool> [task] Launch tool with optional task")
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
	sb.WriteString("- Search the codebase with `search_codebase`\n")
	sb.WriteString("- Get architecture context with `get_architecture`\n")
	sb.WriteString("- Ask questions with `agentic_search`\n")
	sb.WriteString("- Build context for implementation with `search_codebase_for_context`\n")

	if cfg.SystemName != "" {
		sb.WriteString(fmt.Sprintf("\nProject: %s\n", cfg.SystemName))
	}
	if cfg.EpicName != "" {
		sb.WriteString(fmt.Sprintf("Epic: %s\n", cfg.EpicName))
	}

	return sb.String()
}

// MCP Setup functions for each tool

func setupOpenCodeMCP(cfg *config.Config) error {
	return setupOpenCodeMCPWithAgent(cfg, false)
}

// setupOpenCodeMCPWithAgent configures OpenCode MCP and optionally creates a YOLO agent
func setupOpenCodeMCPWithAgent(cfg *config.Config, createYoloAgent bool) error {
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
		"url":     fmt.Sprintf("%s/api/mcp", cfg.APIURL),
		"enabled": true,
	}

	config["mcp"] = mcp
	config["$schema"] = "https://opencode.ai/config.json"

	// Create a "ralph" agent that auto-approves everything if requested
	if createYoloAgent {
		agent, ok := config["agent"].(map[string]interface{})
		if !ok {
			agent = make(map[string]interface{})
		}

		// The "ralph" agent - auto-approves ALL permissions
		// Don't set custom permissions - let OpenCode use defaults
		// Just set the model to use
		agent["ralph"] = map[string]interface{}{
			"model": "cerebras/zai-glm-4.7",
		}

		config["agent"] = agent
	}

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
		"url": fmt.Sprintf("%s/api/mcp", cfg.APIURL),
	}

	mcpConfig["mcpServers"] = servers

	// Write config
	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
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

	servers["modernpath"] = map[string]interface{}{
		"type": "http",
		"url":  fmt.Sprintf("%s/api/mcp", cfg.APIURL),
	}
	mcpConfig["mcpServers"] = servers

	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func setupGenericMCP(cfg *config.Config) error {
	// Generic setup - just print instructions
	printInfo("To add ModernPath MCP to this tool, add the following to its config:\n\n")
	fmt.Printf("  MCP Server URL: %s/api/mcp\n\n", cfg.APIURL)
	return nil
}

// Variables for dev epic and ralph subcommands
var implementTool string

// TaskDetails and related types
type TaskDetails struct {
	ID          string            `json:"id"`
	Code        string            `json:"code"`
	Name        string            `json:"title"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Subtasks    []SubtaskDetails  `json:"subtasks"`
	Context     map[string]string `json:"context"`
}

type SubtaskDetails struct {
	ID                 string      `json:"id"`
	Code               string      `json:"code"`
	Title              string      `json:"title"`
	Description        string      `json:"description"`
	AcceptanceCriteria interface{} `json:"acceptance_criteria"`
}

func selectTaskForImplementation(cfg *config.Config) (string, error) {
	url := fmt.Sprintf("%s/api/work/epics/%d/tasks", cfg.APIURL, cfg.EpicID)
	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			ID     string `json:"id"`
			Code   string `json:"code"`
			Name   string `json:"title"`
			Status string `json:"status"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if len(result.Data) == 0 {
		printError("No tasks found. Create tasks first via the UI or 'modernpath work derive'.\n")
		return "", fmt.Errorf("no tasks")
	}

	// Build selection items
	items := make([]string, len(result.Data))
	for i, task := range result.Data {
		status := task.Status
		if status == "todo" || status == "backlog" {
			status = "📋 " + status
		} else if status == "in_progress" {
			status = "🔄 " + status
		} else if status == "done" {
			status = "✅ " + status
		}
		items[i] = fmt.Sprintf("[%s] %s (%s)", task.Code, task.Name, status)
	}

	prompt := promptui.Select{
		Label:    "Select task to implement",
		Items:    items,
		HideHelp: true,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return "", err
	}

	return result.Data[idx].ID, nil
}

func fetchTaskDetails(cfg *config.Config, taskID string) (*TaskDetails, error) {
	url := fmt.Sprintf("%s/api/work/tasks/%s", cfg.APIURL, taskID)
	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data TaskDetails `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result.Data, nil
}

func fetchRelatedSpecs(cfg *config.Config) ([]map[string]interface{}, error) {
	url := fmt.Sprintf("%s/api/work/epics/%d/specs", cfg.APIURL, cfg.EpicID)
	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var result struct {
		Data struct {
			Specs []map[string]interface{} `json:"specs"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	return result.Data.Specs, nil
}

func selectToolForImplementation() (string, error) {
	// Check which tools are installed
	availableTools := []string{}
	toolDescriptions := []string{}

	for name, tool := range supportedTools {
		if _, err := exec.LookPath(tool.Command); err == nil {
			availableTools = append(availableTools, name)
			toolDescriptions = append(toolDescriptions, fmt.Sprintf("%s - %s", name, tool.Description))
		}
	}

	if len(availableTools) == 0 {
		printError("No AI coding agents found. Install one of: opencode, cursor, claude\n")
		return "", fmt.Errorf("no tools installed")
	}

	if len(availableTools) == 1 {
		return availableTools[0], nil
	}

	prompt := promptui.Select{
		Label:    "Select AI coding agent",
		Items:    toolDescriptions,
		HideHelp: true,
	}

	idx, _, err := prompt.Run()
	if err != nil {
		return "", err
	}

	return availableTools[idx], nil
}

func buildImplementationPrompt(task *TaskDetails, specs []map[string]interface{}, cfg *config.Config) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Task: %s\n\n", task.Name))

	if task.Description != "" {
		sb.WriteString("## Description\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	// Add subtasks
	if len(task.Subtasks) > 0 {
		sb.WriteString("## Subtasks\n\n")
		for _, subtask := range task.Subtasks {
			sb.WriteString(fmt.Sprintf("### %s: %s\n", subtask.Code, subtask.Title))
			if subtask.Description != "" {
				sb.WriteString(subtask.Description)
				sb.WriteString("\n")
			}
			if subtask.AcceptanceCriteria != nil {
				sb.WriteString("\n**Acceptance Criteria:**\n")
				switch ac := subtask.AcceptanceCriteria.(type) {
				case string:
					sb.WriteString(ac)
					sb.WriteString("\n")
				case []interface{}:
					for _, item := range ac {
						sb.WriteString(fmt.Sprintf("- %v\n", item))
					}
				default:
					sb.WriteString(fmt.Sprintf("%v\n", ac))
				}
			}
			sb.WriteString("\n")
		}
	}

	// Add relevant specs summary
	if len(specs) > 0 {
		sb.WriteString("## Related Specifications\n\n")
		sb.WriteString("Use the `modernpath` MCP tools to access full specs:\n")
		for _, spec := range specs {
			name, _ := spec["name"].(string)
			category, _ := spec["category"].(string)
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", category, name))
		}
		sb.WriteString("\n")
	}

	// Add MCP instructions
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("Use the `modernpath` MCP server for:\n")
	sb.WriteString("- `get_architecture` - Get system architecture overview\n")
	sb.WriteString("- `agentic_search` - Ask questions about the codebase\n")
	sb.WriteString("- `search_codebase` - Search for specific code patterns\n")
	sb.WriteString("- `search_codebase_for_context` - Build implementation context\n")
	sb.WriteString("- `get_task_context` - Get detailed task context\n")
	sb.WriteString("\n")

	// Add project context
	sb.WriteString("## Project Context\n\n")
	sb.WriteString(fmt.Sprintf("- **Project:** %s\n", cfg.SystemName))
	sb.WriteString(fmt.Sprintf("- **Epic:** %s\n", cfg.EpicName))
	sb.WriteString(fmt.Sprintf("- **Task ID:** %s\n", task.ID))

	return sb.String()
}

func launchToolWithPrompt(tool DevTool, prompt string) error {
	var cmd *exec.Cmd

	switch tool.Command {
	case "opencode":
		// OpenCode - launch and let user paste prompt
		cmd = exec.Command(tool.Command)
	case "cursor":
		// Cursor - open current directory
		cmd = exec.Command(tool.Command, ".")
	default:
		cmd = exec.Command(tool.Command)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
