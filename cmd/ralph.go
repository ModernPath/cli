package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	ralphMaxIterations     int
	ralphMinIterations     int
	ralphCompletionPromise string
	ralphTool              string
	ralphNoCommit          bool
	ralphVerbose           bool
	ralphAll               bool
	ralphYesAll            bool
)

// RalphState tracks loop state
type RalphState struct {
	Active            bool      `json:"active"`
	Iteration         int       `json:"iteration"`
	MaxIterations     int       `json:"max_iterations"`
	MinIterations     int       `json:"min_iterations"`
	CompletionPromise string    `json:"completion_promise"`
	EpicID            string    `json:"epic_id"`
	EpicTitle         string    `json:"epic_title"`
	Tool              string    `json:"tool"`
	StartedAt         time.Time `json:"started_at"`
}

// RalphHistory tracks iteration history
type RalphHistory struct {
	Iterations      []IterationRecord `json:"iterations"`
	TotalDurationMs int64             `json:"total_duration_ms"`
}

type IterationRecord struct {
	Iteration           int       `json:"iteration"`
	StartedAt           time.Time `json:"started_at"`
	EndedAt             time.Time `json:"ended_at"`
	DurationMs          int64     `json:"duration_ms"`
	ExitCode            int       `json:"exit_code"`
	CompletionDetected  bool      `json:"completion_detected"`
	FilesModified       int       `json:"files_modified"`
}

var ralphCmd = &cobra.Command{
	Use:   "ralph [epic_id]",
	Short: "Run iterative AI development loop for an epic (Ralph Wiggum technique)",
	Long: `Run an AI coding agent in an iterative loop until the task is complete.

Based on the Ralph Wiggum technique (ghuntley.com/ralph), this command:
1. Builds a comprehensive prompt from epic specs and context
2. Launches an AI coding agent (OpenCode, Cursor, etc.)
3. Checks output for completion promise
4. Loops until success or max iterations

The AI sees its previous work in files each iteration, enabling
incremental progress on complex tasks.

Examples:
  modernpath ralph                           # Interactive epic selection
  modernpath ralph abc123-uuid               # Specific epic  
  modernpath ralph --max-iterations 10       # Limit iterations
  modernpath ralph --tool opencode           # Use specific tool
  modernpath ralph --status                  # Check loop status`,
	RunE: runRalph,
}

var ralphStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current Ralph loop status",
	RunE:  runRalphStatus,
}

func init() {
	// Command moved to dev.go as dev ralph subcommand
	// Keeping file for reference but not registering command
	// Flags are registered in dev.go init()

	ralphCmd.Flags().IntVar(&ralphMaxIterations, "max-iterations", 20, "Maximum iterations before stopping")
	ralphCmd.Flags().IntVar(&ralphMinIterations, "min-iterations", 1, "Minimum iterations before completion allowed")
	ralphCmd.Flags().StringVar(&ralphCompletionPromise, "completion-promise", "COMPLETE", "Text that signals completion")
	ralphCmd.Flags().StringVarP(&ralphTool, "tool", "t", "", "AI tool to use (opencode, cursor, claude)")
	ralphCmd.Flags().BoolVar(&ralphNoCommit, "no-commit", false, "Don't auto-commit after iterations")
	ralphCmd.Flags().BoolVarP(&ralphVerbose, "verbose", "V", false, "Verbose output")
	ralphCmd.Flags().BoolVar(&ralphAll, "all", false, "Work through ALL pending epics in logical order")
	ralphCmd.Flags().BoolVar(&ralphYesAll, "yes-all", false, "Tell AI to auto-approve all actions (bash, file writes, etc)")
}

func runRalph(cmd *cobra.Command, args []string) error {
	fmt.Println("🚀 Starting Ralph...")
	fmt.Println("   Reading config...")
	
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}
	
	fmt.Printf("   Config loaded: initiative=%d, arch=%d\n", cfg.InitiativeID, cfg.SystemID)

	if cfg.InitiativeID == 0 {
		printError("No initiative configured. Run 'modernpath init' or 'modernpath new' first.\n")
		return nil
	}

	// Check for existing loop
	state, _ := loadRalphState()
	if state != nil && state.Active {
		printError("A Ralph loop is already active (iteration %d)\n", state.Iteration)
		printInfo("Started at: %s\n", state.StartedAt.Format(time.RFC3339))
		printInfo("To check status: modernpath ralph status\n")
		printInfo("To cancel, press Ctrl+C in its terminal or delete .modernpath/ralph-state.json\n")
		return nil
	}

	// Handle --all mode: work through all pending epics
	if ralphAll {
		return runRalphAll(cfg)
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
	specs, _ := fetchRelatedSpecs(cfg)

	// Select tool
	toolName := ralphTool
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

	// Check if tool supports non-interactive mode
	if toolName != "opencode" {
		printWarning("Ralph loop works best with OpenCode. Other tools may require manual interaction.\n")
	}

	// Setup MCP
	if err := setupMCPForTool(toolName, cfg); err != nil {
		printWarning("Failed to setup MCP: %v\n", err)
	}

	// Initialize state
	newState := &RalphState{
		Active:            true,
		Iteration:         1,
		MaxIterations:     ralphMaxIterations,
		MinIterations:     ralphMinIterations,
		CompletionPromise: ralphCompletionPromise,
		EpicID:            taskID,
		EpicTitle:         task.Name,
		Tool:              toolName,
		StartedAt:         time.Now(),
	}

	if err := saveRalphState(newState); err != nil {
		printError("Failed to save state: %v\n", err)
		return err
	}

	// Initialize history
	history := &RalphHistory{
		Iterations:      []IterationRecord{},
		TotalDurationMs: 0,
	}

	// Print banner
	fmt.Println(`
╔══════════════════════════════════════════════════════════════════╗
║                    ModernPath Ralph Loop                         ║
║            Iterative AI Development with Context                 ║
╚══════════════════════════════════════════════════════════════════╝`)

	fmt.Printf("\nTask: [%s] %s\n", task.Code, task.Name)
	fmt.Printf("Tool: %s\n", tool.Name)
	fmt.Printf("Completion promise: %s\n", ralphCompletionPromise)
	fmt.Printf("Max iterations: %d\n", ralphMaxIterations)
	fmt.Println("\nStarting loop... (Ctrl+C to stop)")
	fmt.Println("═══════════════════════════════════════════════════════════════════")

	// Build base prompt
	basePrompt := buildRalphPrompt(task, specs, cfg, newState)

	// Main loop
	for {
		// Check max iterations
		if newState.Iteration > ralphMaxIterations {
			fmt.Printf("\n╔══════════════════════════════════════════════════════════════════╗\n")
			fmt.Printf("║  Max iterations (%d) reached. Loop stopped.\n", ralphMaxIterations)
			fmt.Printf("║  Total time: %s\n", formatDuration(history.TotalDurationMs))
			fmt.Printf("╚══════════════════════════════════════════════════════════════════╝\n")
			clearRalphState()
			break
		}

		iterInfo := fmt.Sprintf("%d / %d", newState.Iteration, ralphMaxIterations)
		fmt.Printf("\n🔄 Iteration %s\n", iterInfo)
		fmt.Println("────────────────────────────────────────────────────────────────────")

		iterStart := time.Now()

		// Run AI tool
		output, exitCode, err := runAITool(tool, basePrompt, newState)
		if err != nil {
			printError("Error running tool: %v\n", err)
			newState.Iteration++
			saveRalphState(newState)
			time.Sleep(2 * time.Second)
			continue
		}

		iterDuration := time.Since(iterStart).Milliseconds()

		// Check for completion
		completionDetected := checkCompletionPromise(output, ralphCompletionPromise)

		// Count modified files
		filesModified := countModifiedFiles()

		// Record iteration
		record := IterationRecord{
			Iteration:          newState.Iteration,
			StartedAt:          iterStart,
			EndedAt:            time.Now(),
			DurationMs:         iterDuration,
			ExitCode:           exitCode,
			CompletionDetected: completionDetected,
			FilesModified:      filesModified,
		}
		history.Iterations = append(history.Iterations, record)
		history.TotalDurationMs += iterDuration
		saveRalphHistory(history)

		// Print iteration summary
		fmt.Println("\nIteration Summary")
		fmt.Println("────────────────────────────────────────────────────────────────────")
		fmt.Printf("Iteration:    %d\n", newState.Iteration)
		fmt.Printf("Duration:     %s\n", formatDuration(iterDuration))
		fmt.Printf("Exit code:    %d\n", exitCode)
		fmt.Printf("Files changed: %d\n", filesModified)
		fmt.Printf("Completion:   %v\n", completionDetected)

		if completionDetected {
			if newState.Iteration < ralphMinIterations {
				fmt.Printf("\n⏳ Completion detected, but min iterations (%d) not reached.\n", ralphMinIterations)
			} else {
				fmt.Printf("\n╔══════════════════════════════════════════════════════════════════╗\n")
				fmt.Printf("║  ✅ Completion promise detected: <promise>%s</promise>\n", ralphCompletionPromise)
				fmt.Printf("║  Task completed in %d iteration(s)\n", newState.Iteration)
				fmt.Printf("║  Total time: %s\n", formatDuration(history.TotalDurationMs))
				fmt.Printf("╚══════════════════════════════════════════════════════════════════╝\n")
				clearRalphState()
				clearRalphHistory()
				break
			}
		}

		// Auto-commit if enabled
		if !ralphNoCommit && filesModified > 0 {
			commitChanges(newState.Iteration)
		}

		// Update state for next iteration
		newState.Iteration++
		saveRalphState(newState)

		// Brief pause between iterations
		time.Sleep(1 * time.Second)
	}

	return nil
}

func runRalphStatus(cmd *cobra.Command, args []string) error {
	state, err := loadRalphState()
	history, _ := loadRalphHistory()

	fmt.Println(`
╔══════════════════════════════════════════════════════════════════╗
║                    Ralph Loop Status                             ║
╚══════════════════════════════════════════════════════════════════╝`)

	if err != nil || state == nil || !state.Active {
		fmt.Println("\n⏹️  No active loop")
	} else {
		elapsed := time.Since(state.StartedAt)
		fmt.Println("\n🔄 ACTIVE LOOP")
		fmt.Printf("   Epic:         [%s] %s\n", state.EpicID[:8], state.EpicTitle)
		fmt.Printf("   Tool:         %s\n", state.Tool)
		fmt.Printf("   Iteration:    %d / %d\n", state.Iteration, state.MaxIterations)
		fmt.Printf("   Started:      %s\n", state.StartedAt.Format(time.RFC3339))
		fmt.Printf("   Elapsed:      %s\n", formatDuration(elapsed.Milliseconds()))
		fmt.Printf("   Promise:      %s\n", state.CompletionPromise)
	}

	if history != nil && len(history.Iterations) > 0 {
		fmt.Printf("\n📊 HISTORY (%d iterations)\n", len(history.Iterations))
		fmt.Printf("   Total time:   %s\n", formatDuration(history.TotalDurationMs))

		// Show last 5 iterations
		recent := history.Iterations
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}

		fmt.Println("\n   Recent iterations:")
		for _, iter := range recent {
			status := "🔄"
			if iter.CompletionDetected {
				status = "✅"
			} else if iter.ExitCode != 0 {
				status = "❌"
			}
			fmt.Printf("   %s #%d: %s | files: %d\n",
				status, iter.Iteration, formatDuration(iter.DurationMs), iter.FilesModified)
		}
	}

	fmt.Println()
	return nil
}

func buildRalphPrompt(task *TaskDetails, specs []map[string]interface{}, cfg *config.Config, state *RalphState) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf(`
# Ralph Wiggum Loop - Iteration %d

You are in an iterative development loop. Work on the task below until you can genuinely complete it.

## Your Task

### Task: %s

`, state.Iteration, task.Name))

	if task.Description != "" {
		sb.WriteString("**Description:**\n")
		sb.WriteString(task.Description)
		sb.WriteString("\n\n")
	}

	// Add user stories
	if len(task.Stories) > 0 {
		sb.WriteString("### User Stories\n\n")
		for _, story := range task.Stories {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", story.Code, story.Title))
			if story.AcceptanceCriteria != nil {
				sb.WriteString("  - Acceptance Criteria:\n")
				switch ac := story.AcceptanceCriteria.(type) {
				case string:
					sb.WriteString(fmt.Sprintf("    - %s\n", ac))
				case []interface{}:
					for _, item := range ac {
						sb.WriteString(fmt.Sprintf("    - %v\n", item))
					}
				default:
					sb.WriteString(fmt.Sprintf("    - %v\n", ac))
				}
			}
		}
		sb.WriteString("\n")
	}

	// Add specs summary
	if len(specs) > 0 {
		sb.WriteString("### Related Specifications\n\n")
		sb.WriteString("Use ModernPath MCP tools to access full specs:\n")
		for _, spec := range specs {
			name, _ := spec["name"].(string)
			category, _ := spec["category"].(string)
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", category, name))
		}
		sb.WriteString("\n")
	}

	// Add auto-approve instructions if enabled
	autoApproveInstructions := ""
	if ralphYesAll {
		autoApproveInstructions = `
## AUTO-APPROVE MODE ENABLED

You have been given permission to:
- Execute any bash/shell commands without asking
- Write/modify any files without confirmation
- Run tests and linters automatically
- Make commits if needed

DO NOT ask for permission - just do it. The user has pre-approved all actions.
`
	}

	sb.WriteString(fmt.Sprintf(`## Instructions

1. Read the current state of files to understand what's been done
2. **Update your todo list** - Track progress and plan remaining work
3. Make progress on the task
4. Run tests/verification if applicable
5. When the task is GENUINELY COMPLETE, output:
   <promise>%s</promise>
%s
## Critical Rules

- ONLY output <promise>%s</promise> when the task is truly done
- Do NOT lie or output false promises to exit the loop
- If stuck, try a different approach
- Check your work before claiming completion
- The loop will continue until you succeed

## Available MCP Tools

Use the modernpath MCP server for:
- get_architecture - Get system architecture overview
- agentic_search - Ask questions about the codebase  
- search_codebase - Search for specific code patterns
- get_epic_context - Get detailed epic context

## Current Iteration: %d / %d

Now, work on the task. Good luck!
`, state.CompletionPromise, autoApproveInstructions, state.CompletionPromise, state.Iteration, state.MaxIterations))

	return sb.String()
}

func runAITool(tool DevTool, prompt string, state *RalphState) (string, int, error) {
	// Save prompt to temp file for reference
	promptFile := filepath.Join(".modernpath", "ralph-prompt.md")
	os.WriteFile(promptFile, []byte(prompt), 0644)

	var cmd *exec.Cmd
	var output bytes.Buffer

	fmt.Printf("🚀 Launching %s...\n", tool.Name)
	fmt.Println("────────────────────────────────────────────────────────────────────")

	switch tool.Command {
	case "opencode":
		// OpenCode supports direct prompt via CLI
		fmt.Println("📝 Sending prompt to OpenCode...")
		if ralphYesAll {
			fmt.Println("   Mode: AUTO-APPROVE (using 'ralph' agent)")
		}
		fmt.Println("   (OpenCode output will appear below)")
		fmt.Println()
		
		// Build command args
		args := []string{"run"}
		
		// Use the "ralph" agent that auto-approves everything if --yes-all is set
		if ralphYesAll {
			args = append(args, "--agent", "ralph")
		}
		
		// Add the prompt as the message
		args = append(args, prompt)
		
		cmd = exec.Command("opencode", args...)
		
		// Create a custom writer that prefixes lines and captures output
		writer := &progressWriter{
			underlying: os.Stdout,
			buffer:     &output,
			prefix:     "│ ",
		}
		
		cmd.Stdout = writer
		cmd.Stderr = &progressWriter{underlying: os.Stderr, prefix: "│ "}
		cmd.Stdin = os.Stdin // Allow interactive input!
		
	default:
		// Other tools - just launch and show prompt
		printInfo("Prompt saved to: %s\n", promptFile)
		printInfo("Paste the prompt into %s and work on the task.\n", tool.Name)
		printInfo("When done, the loop will check for completion.\n\n")
		cmd = exec.Command(tool.Command)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Stdin = os.Stdin
	}

	// Print running indicator
	fmt.Println("┌─ AI Output ────────────────────────────────────────────────────────")
	
	err := cmd.Run()
	
	fmt.Println("└────────────────────────────────────────────────────────────────────")
	fmt.Println()
	
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			fmt.Printf("⚠️  OpenCode exited with code %d\n", exitCode)
		} else {
			fmt.Printf("❌ Error running tool: %v\n", err)
			return output.String(), -1, err
		}
	} else {
		fmt.Println("✓ OpenCode finished")
	}

	return output.String(), exitCode, nil
}

// progressWriter wraps a writer and adds a prefix to each line
type progressWriter struct {
	underlying io.Writer
	buffer     *bytes.Buffer
	prefix     string
	lineStart  bool
}

func (w *progressWriter) Write(p []byte) (n int, err error) {
	// Always write to buffer if present
	if w.buffer != nil {
		w.buffer.Write(p)
	}
	
	// Write with prefix to underlying
	for _, b := range p {
		if w.lineStart {
			w.underlying.Write([]byte(w.prefix))
			w.lineStart = false
		}
		w.underlying.Write([]byte{b})
		if b == '\n' {
			w.lineStart = true
		}
	}
	return len(p), nil
}

func checkCompletionPromise(output, promise string) bool {
	pattern := fmt.Sprintf(`<promise>\s*%s\s*</promise>`, regexp.QuoteMeta(promise))
	re := regexp.MustCompile("(?i)" + pattern)
	return re.MatchString(output)
}

func countModifiedFiles() int {
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	count := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func commitChanges(iteration int) {
	// Check if there are changes
	cmd := exec.Command("git", "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return
	}

	// Add and commit
	exec.Command("git", "add", "-A").Run()
	msg := fmt.Sprintf("Ralph iteration %d: work in progress", iteration)
	exec.Command("git", "commit", "-m", msg).Run()
	printInfo("📝 Auto-committed changes\n")
}

func formatDuration(ms int64) string {
	totalSeconds := ms / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// State persistence functions
func getRalphStatePath() string {
	return filepath.Join(".modernpath", "ralph-state.json")
}

func getRalphHistoryPath() string {
	return filepath.Join(".modernpath", "ralph-history.json")
}

func loadRalphState() (*RalphState, error) {
	data, err := os.ReadFile(getRalphStatePath())
	if err != nil {
		return nil, err
	}
	var state RalphState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveRalphState(state *RalphState) error {
	os.MkdirAll(".modernpath", 0755)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getRalphStatePath(), data, 0644)
}

func clearRalphState() {
	os.Remove(getRalphStatePath())
}

func loadRalphHistory() (*RalphHistory, error) {
	data, err := os.ReadFile(getRalphHistoryPath())
	if err != nil {
		return nil, err
	}
	var history RalphHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return &history, nil
}

func saveRalphHistory(history *RalphHistory) error {
	os.MkdirAll(".modernpath", 0755)
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(getRalphHistoryPath(), data, 0644)
}

func clearRalphHistory() {
	os.Remove(getRalphHistoryPath())
}

// OrderedEpic represents an epic in the ordered list
type OrderedEpic struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	StoryPoints int    `json:"story_points"`
}

// runRalphAll runs ralph on all pending epics in logical order
func runRalphAll(cfg *config.Config) error {
	fmt.Println(`
╔══════════════════════════════════════════════════════════════════════════════╗
║                    ModernPath Ralph - Full Initiative Mode                   ║
║              Working through ALL pending epics in logical order              ║
╠══════════════════════════════════════════════════════════════════════════════╣`)
	fmt.Printf("║  Initiative: %s (ID: %d)\n", cfg.InitiativeName, cfg.InitiativeID)
	fmt.Printf("║  System:       %s\n", cfg.SystemName)
	fmt.Printf("║  Started: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	if ralphYesAll {
		fmt.Println("║  Mode: AUTO-APPROVE (--yes-all enabled)")
	}
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════════╝")

	// Get ordered epics from API
	fmt.Println()
	fmt.Println("🧠 Step 1: Fetching and ordering epics with AI...")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	
	orderedEpics, err := getOrderedEpics(cfg)
	if err != nil {
		printError("Failed to get ordered epics: %v\n", err)
		return err
	}

	if len(orderedEpics) == 0 {
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
		fmt.Println("║  ✅ All epics are already complete! Nothing to do.              ║")
		fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
		return nil
	}

	// Calculate total story points
	totalPoints := 0
	for _, e := range orderedEpics {
		totalPoints += e.StoryPoints
	}

	fmt.Printf("\n✓ Found %d pending epics (%d story points total)\n", len(orderedEpics), totalPoints)
	fmt.Println()
	fmt.Println("📋 Implementation Order (determined by AI):")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	for i, epic := range orderedEpics {
		statusIcon := "⬜"
		if epic.Status == "in_progress" {
			statusIcon = "🔄"
		}
		fmt.Printf("  %s %2d. [%s] %s (%d pts)\n", statusIcon, i+1, epic.Code, epic.Title, epic.StoryPoints)
	}
	fmt.Println("────────────────────────────────────────────────────────────────────")
	fmt.Println()

	// Select tool once for all epics
	fmt.Println("🔧 Step 2: Selecting AI tool...")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	
	toolName := ralphTool
	if toolName == "" {
		var err error
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
	fmt.Printf("✓ Using: %s\n", tool.Name)

	// Setup MCP once (and create YOLO agent if --yes-all)
	fmt.Println()
	fmt.Println("🔌 Step 3: Setting up MCP server...")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	if toolName == "opencode" && ralphYesAll {
		// Create the special "ralph" agent that auto-approves everything
		if err := setupOpenCodeMCPWithAgent(cfg, true); err != nil {
			printWarning("Failed to setup MCP with YOLO agent: %v\n", err)
		} else {
			fmt.Println("✓ MCP server configured")
			fmt.Println("✓ Created 'ralph' agent with auto-approve permissions")
		}
	} else {
		if err := setupMCPForTool(toolName, cfg); err != nil {
			printWarning("Failed to setup MCP: %v\n", err)
		} else {
			fmt.Println("✓ MCP server configured")
		}
	}

	// Fetch specs once
	fmt.Println()
	fmt.Println("📚 Step 4: Loading specifications...")
	fmt.Println("────────────────────────────────────────────────────────────────────")
	specs, _ := fetchRelatedSpecs(cfg)
	fmt.Printf("✓ Loaded %d specification(s)\n", len(specs))

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("                    STARTING EPIC IMPLEMENTATION                    ")
	fmt.Println("════════════════════════════════════════════════════════════════════")

	// Work through each epic
	totalStart := time.Now()
	completedCount := 0
	blockedCount := 0

	for epicIdx, epic := range orderedEpics {
		fmt.Println()
		fmt.Println("╔══════════════════════════════════════════════════════════════════════════════╗")
		fmt.Printf("║  🎯 EPIC %d of %d                                                            \n", epicIdx+1, len(orderedEpics))
		fmt.Printf("║  Code: %s\n", epic.Code)
		fmt.Printf("║  Title: %s\n", truncateString(epic.Title, 60))
		fmt.Printf("║  Story Points: %d\n", epic.StoryPoints)
		fmt.Println("╚══════════════════════════════════════════════════════════════════════════════╝")

		// Mark epic as in_progress
		fmt.Println()
		fmt.Printf("📌 Marking [%s] as in_progress...\n", epic.Code)
		if err := updateEpicStatus(cfg, epic.ID, "in_progress"); err != nil {
			printWarning("   Failed: %v\n", err)
		} else {
			fmt.Println("   ✓ Status updated")
		}

		// Fetch full task details
		fmt.Printf("\n📥 Fetching full task details...\n")
		taskDetails, err := fetchTaskDetails(cfg, epic.ID)
		if err != nil {
			printError("   Failed: %v\n", err)
			printError("   Skipping this task...\n")
			continue
		}
		fmt.Println("   ✓ Task details loaded")

		// Run ralph loop for this task
		fmt.Println()
		fmt.Println("🚀 Starting Ralph loop...")
		completed, err := runRalphForTask(taskDetails, specs, cfg, tool)
		if err != nil {
			printError("Error during epic: %v\n", err)
			// Don't stop - continue with next epic
		}

		if completed {
			// Mark epic as done
			fmt.Printf("\n✅ Marking [%s] as done...\n", epic.Code)
			if err := updateEpicStatus(cfg, epic.ID, "done"); err != nil {
				printWarning("   Failed to update status: %v\n", err)
			} else {
				fmt.Println("   ✓ Status updated")
			}
			completedCount++
			
			// Print progress
			fmt.Println()
			fmt.Printf("📊 Progress: %d/%d epics completed (%.0f%%)\n", 
				completedCount, len(orderedEpics), 
				float64(completedCount)/float64(len(orderedEpics))*100)
		} else {
			fmt.Printf("\n⚠️  [%s] not completed (max iterations reached)\n", epic.Code)
			fmt.Printf("   Marking as blocked...\n")
			updateEpicStatus(cfg, epic.ID, "blocked")
			blockedCount++
		}
	}

	// Final summary
	totalDuration := time.Since(totalStart)
	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════════════════")
	fmt.Println("                         INITIATIVE SUMMARY                                     ")
	fmt.Println("════════════════════════════════════════════════════════════════════════════════")
	fmt.Println()
	
	if completedCount == len(orderedEpics) {
		fmt.Println("  🎉 ALL EPICS COMPLETED SUCCESSFULLY!")
	} else {
		fmt.Printf("  📊 Partial completion: %d/%d epics\n", completedCount, len(orderedEpics))
	}
	
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────────────┐")
	fmt.Printf("  │  ✅ Completed:    %d epics\n", completedCount)
	fmt.Printf("  │  ⛔ Blocked:      %d epics\n", blockedCount)
	fmt.Printf("  │  ⏱️  Total time:   %s\n", formatDuration(totalDuration.Milliseconds()))
	fmt.Printf("  │  📆 Finished at:  %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println("  └─────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	return nil
}

// runRalphForTask runs the ralph loop for a single task
func runRalphForTask(task *TaskDetails, specs []map[string]interface{}, cfg *config.Config, tool DevTool) (bool, error) {
	// Initialize state for this task
	state := &RalphState{
		Active:            true,
		Iteration:         1,
		MaxIterations:     ralphMaxIterations,
		MinIterations:     ralphMinIterations,
		CompletionPromise: ralphCompletionPromise,
		EpicID:            task.ID,
		EpicTitle:         task.Name,
		Tool:              tool.Command,
		StartedAt:         time.Now(),
	}
	saveRalphState(state)

	// Initialize history
	history := &RalphHistory{
		Iterations:      []IterationRecord{},
		TotalDurationMs: 0,
	}

	fmt.Printf("\n📋 Task Details:\n")
	fmt.Printf("   Code: %s\n", task.Code)
	fmt.Printf("   Name: %s\n", task.Name)
	if task.Description != "" {
		desc := task.Description
		if len(desc) > 100 {
			desc = desc[:100] + "..."
		}
		fmt.Printf("   Desc: %s\n", desc)
	}
	if len(task.Stories) > 0 {
		fmt.Printf("   Stories: %d\n", len(task.Stories))
	}
	fmt.Println()

	// Build base prompt - need to rebuild for each iteration to update iteration number
	var basePrompt string

	// Main loop
	for {
		if state.Iteration > ralphMaxIterations {
			printWarning("Max iterations (%d) reached for this task.\n", ralphMaxIterations)
			clearRalphState()
			return false, nil
		}

		// Rebuild prompt with current iteration number
		basePrompt = buildRalphPrompt(task, specs, cfg, state)

		fmt.Printf("\n🔄 ITERATION %d of %d\n", state.Iteration, ralphMaxIterations)
		fmt.Printf("   Task: [%s] %s\n", task.Code, task.Name)
		fmt.Printf("   Time: %s\n", time.Now().Format("15:04:05"))
		fmt.Println("════════════════════════════════════════════════════════════════════")

		iterStart := time.Now()

		// Run AI tool
		output, exitCode, err := runAITool(tool, basePrompt, state)
		if err != nil {
			printError("Error running tool: %v\n", err)
			state.Iteration++
			saveRalphState(state)
			fmt.Println("⏳ Waiting 2 seconds before retry...")
			time.Sleep(2 * time.Second)
			continue
		}

		iterDuration := time.Since(iterStart).Milliseconds()

		// Check for completion
		completionDetected := checkCompletionPromise(output, ralphCompletionPromise)
		filesModified := countModifiedFiles()

		// Record iteration
		record := IterationRecord{
			Iteration:          state.Iteration,
			StartedAt:          iterStart,
			EndedAt:            time.Now(),
			DurationMs:         iterDuration,
			ExitCode:           exitCode,
			CompletionDetected: completionDetected,
			FilesModified:      filesModified,
		}
		history.Iterations = append(history.Iterations, record)
		history.TotalDurationMs += iterDuration
		saveRalphHistory(history)

		// Print detailed summary
		fmt.Println()
		fmt.Println("────────────────────────────────────────────────────────────────────")
		fmt.Printf("📊 ITERATION %d SUMMARY\n", state.Iteration)
		fmt.Println("────────────────────────────────────────────────────────────────────")
		fmt.Printf("   ⏱️  Duration:     %s\n", formatDuration(iterDuration))
		fmt.Printf("   📁 Files changed: %d\n", filesModified)
		fmt.Printf("   🚪 Exit code:     %d\n", exitCode)
		fmt.Printf("   ✅ Complete:      %v\n", completionDetected)
		fmt.Printf("   📈 Total time:    %s\n", formatDuration(history.TotalDurationMs))
		fmt.Println("────────────────────────────────────────────────────────────────────")

		if completionDetected && state.Iteration >= ralphMinIterations {
			fmt.Println()
			fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
			fmt.Printf("║  🎉 TASK COMPLETED: [%s] %s\n", task.Code, truncateString(task.Name, 40))
			fmt.Printf("║  ⏱️  Total iterations: %d\n", state.Iteration)
			fmt.Printf("║  ⏱️  Total time: %s\n", formatDuration(history.TotalDurationMs))
			fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
			clearRalphState()
			return true, nil
		}

		// Auto-commit
		if !ralphNoCommit && filesModified > 0 {
			fmt.Println("📝 Auto-committing changes...")
			commitChanges(state.Iteration)
		}

		state.Iteration++
		saveRalphState(state)
		
		fmt.Println()
		fmt.Println("⏳ Starting next iteration in 1 second...")
		time.Sleep(1 * time.Second)
	}
}

// getOrderedEpics fetches and orders pending tasks using LLM
func getOrderedEpics(cfg *config.Config) ([]OrderedEpic, error) {
	url := fmt.Sprintf("%s/api/work/initiatives/%d/order-epics", cfg.APIURL, cfg.InitiativeID)

	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer([]byte("{}")), 60*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data struct {
			OrderedEpics []OrderedEpic `json:"ordered_epics"`
			Total        int           `json:"total"`
			Warning      string        `json:"warning,omitempty"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Data.Warning != "" {
		printWarning("%s\n", result.Data.Warning)
	}

	return result.Data.OrderedEpics, nil
}

// updateEpicStatus updates an epic's status via API
func updateEpicStatus(cfg *config.Config, epicID, status string) error {
	url := fmt.Sprintf("%s/api/work/epics/%s/status", cfg.APIURL, epicID)

	payload := map[string]string{"status": status}
	jsonPayload, _ := json.Marshal(payload)

	req, err := api.NewAuthenticatedRequest("PATCH", url, bytes.NewBuffer(jsonPayload))
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	return nil
}
