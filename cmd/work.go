package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/agents"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage epics, tasks, and subtasks",
	Long:  `View and manage work items including epics, tasks, and subtasks.`,
}

// work list - List epics (was: work initiatives)
var workListCmd = &cobra.Command{
	Use:   "list",
	Short: "List epics for current system",
	RunE:  runWorkList,
}

// work select - Select an epic (was: select initiative)
var workSelectCmd = &cobra.Command{
	Use:   "select [epic_id]",
	Short: "Select an epic to work on",
	Long:  `Select an epic to set as the current epic in .modernpath/config.json. If no ID is provided, shows an interactive list.`,
	RunE:  runWorkSelect,
}

// work status - Combined status view
var workStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show epic status (specs, tasks, progress)",
	Long:  `Display comprehensive status for the current epic including specs and tasks.`,
	RunE:  runWorkStatus,
}

// work subtasks - List subtasks for a task
var workSubtasksCmd = &cobra.Command{
	Use:   "subtasks <task_id>",
	Short: "List subtasks for a task",
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkSubtasks,
}

// work new - Create new epic (was: plan new)
var workNewCmd = &cobra.Command{
	Use:   "new [description]",
	Short: "Create a new epic from a description",
	Long: `Create a new epic from a text or markdown description.

This command:
1. Uses LLM to generate a structured idea from your description
2. Creates an epic linked to the idea
3. Automatically selects the new epic as active

Examples:
  modernpath work new "Add user authentication with OAuth2"
  modernpath work new --type=innovation "Implement AI-powered code review"
  cat feature.md | modernpath work new`,
	RunE: runWorkNew,
}

// work derive - Derive tasks from specs (was: plan derive)
var workDeriveCmd = &cobra.Command{
	Use:   "derive",
	Short: "Derive tasks and subtasks from specifications",
	Long: `Analyze specifications and create actionable work items:
  
  - Tasks from architecture components
  - Stories from requirements and flows  
  - Subtasks from interfaces and data entities
  - Dependencies between work items
  
This converts your specs into a complete development task plan.`,
	RunE: runWorkDerive,
}

// work specs - Specification subcommand group
var workSpecsCmd = &cobra.Command{
	Use:   "specs",
	Short: "Manage specifications for an epic",
	Long:  `Generate, sync, and manage specifications for a ModernPath epic.`,
}

var workSpecsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run the specification pipeline for the current epic",
	Long: `Run the full 6-phase specification pipeline:
  1. Discovery - Requirements & User Stories
  2. Architecture - Component specs & C4 diagrams  
  3. Data - ERD & Data dictionary
  4. UI/UX - Wireframes & Design system
  5. Testing - Test plans & Coverage matrix
  6. Validation - Cross-check all outputs`,
	RunE: runWorkSpecsGenerate,
}

var workSpecsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Download specifications to the active epic folder under .modernpath/tasks/",
	RunE:  runWorkSpecsSync,
}

var workSpecsPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push local specifications back to the ModernPath platform",
	RunE:  runWorkSpecsPush,
}

var (
	workNewType string
)

// workInitiative mirrors GET /api/work/initiatives list items (Initiative schema uses "title", not "name").
type workInitiative struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"` // legacy alias; prefer Title
	Status        string `json:"status"`
	WorkflowPhase string `json:"workflow_phase"`
	ProjectType   string `json:"project_type"`
}

func (i workInitiative) displayTitle() string {
	if i.Title != "" {
		return i.Title
	}
	return i.Name
}

func init() {
	rootCmd.AddCommand(workCmd)
	
	// Main work commands
	workCmd.AddCommand(workListCmd)
	workCmd.AddCommand(workSelectCmd)
	workCmd.AddCommand(workStatusCmd)
	workCmd.AddCommand(workSubtasksCmd)
	workCmd.AddCommand(workNewCmd)
	workCmd.AddCommand(workDeriveCmd)
	workCmd.AddCommand(workReviewCmd)
	
	// Specs subcommands
	workCmd.AddCommand(workSpecsCmd)
	workSpecsCmd.AddCommand(workSpecsGenerateCmd)
	workSpecsCmd.AddCommand(workSpecsSyncCmd)
	workSpecsCmd.AddCommand(workSpecsPushCmd)
	
	// Flags
	workNewCmd.Flags().StringVarP(&workNewType, "type", "t", "feature", "Idea type: feature, innovation, gap, trend")
}

func runWorkList(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	url := fmt.Sprintf("%s/api/work/initiatives?system_id=%d", cfg.APIURL, cfg.SystemID)

	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		printError("Failed to connect: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		printError("API error: %s - %s\n", resp.Status, string(body))
		return nil
	}

	var result struct {
		Data []workInitiative `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Printf("Epics for %s\n", cfg.SystemName)
	fmt.Println("─────────────────────────────────────────")

	if len(result.Data) == 0 {
		fmt.Println("No epics found.")
		printInfo("Create one with: modernpath work new \"description\"\n")
	} else {
		for _, epic := range result.Data {
			marker := "  "
			if epic.ID == cfg.InitiativeID {
				marker = "→ "
				cyan.Printf("%s[%d] %s\n", marker, epic.ID, epic.displayTitle())
			} else {
				fmt.Printf("%s[%d] %s\n", marker, epic.ID, epic.displayTitle())
			}
			fmt.Printf("      Status: %s | Phase: %s\n", epic.Status, epic.WorkflowPhase)
		}
	}

	fmt.Println()
	return nil
}

func runWorkSelect(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	url := fmt.Sprintf("%s/api/work/initiatives?system_id=%d", cfg.APIURL, cfg.SystemID)

	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		printError("Failed to connect: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		printError("API error: %s - %s\n", resp.Status, string(body))
		return nil
	}

	var result struct {
		Data []workInitiative `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	if len(result.Data) == 0 {
		printError("No epics found.\n")
		return nil
	}

	var selectedID int
	var selectedName string
	var selectedProjectType string

	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &selectedID)
		found := false
		for _, epic := range result.Data {
			if epic.ID == selectedID {
				selectedName = epic.displayTitle()
				selectedProjectType = epic.ProjectType
				found = true
				break
			}
		}
		if !found {
			printError("Epic with ID %d not found.\n", selectedID)
			return nil
		}
	} else {
		items := make([]string, len(result.Data))
		for i, epic := range result.Data {
			marker := ""
			if epic.ID == cfg.InitiativeID {
				marker = "→ "
			}
			typeLabel := ""
			if epic.ProjectType == "transformation" || epic.ProjectType == "transform" {
				typeLabel = " [TRANSFORM]"
			}
			items[i] = fmt.Sprintf("%s[%d] %s%s - %s (%s)", marker, epic.ID, epic.displayTitle(), typeLabel, epic.Status, epic.WorkflowPhase)
		}

		prompt := promptui.Select{
			Label:    "Select an epic",
			Items:    items,
			HideHelp: true,
		}

		idx, _, err := prompt.Run()
		if err != nil {
			return err
		}

		selectedID = result.Data[idx].ID
		selectedName = result.Data[idx].displayTitle()
		selectedProjectType = result.Data[idx].ProjectType
	}

	cfg.InitiativeID = selectedID
	cfg.InitiativeName = selectedName

	fmt.Println()
	printInfo("Syncing specifications for epic [%d]...\n", selectedID)
	specsRelPath, err := syncSpecs(cfg.APIURL, selectedID, selectedName)
	if err != nil {
		printWarning("Failed to sync specs: %v\n", err)
	} else {
		cfg.InitiativeSpecsDir = specsRelPath
		printSuccess("Specifications synced to .modernpath/%s/\n", specsRelPath)
	}

	if err := config.WriteConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}

	if err := fetchTasksForInitiative(cfg, selectedID); err != nil {
		printWarning("Failed to fetch task context: %v\n", err)
	}

	if selectedProjectType == "transformation" || selectedProjectType == "transform" {
		fmt.Println()
		printInfo("Transformation epic detected - syncing source files...\n")
		if err := autoSyncTransformFiles(cfg, selectedID); err != nil {
			printWarning("Auto-sync failed: %v\n", err)
		} else {
			printSuccess("Source files synced to .modernpath/source/\n")
		}
	}

	fmt.Println()
	printSuccess("Selected epic: [%d] %s\n", selectedID, selectedName)
	fmt.Println()
	return nil
}

func runWorkStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.InitiativeID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	bold := color.New(color.Bold)

	fmt.Println()
	bold.Printf("📋 Epic Status: %s\n", cfg.InitiativeName)
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("  Epic ID: %d\n", cfg.InitiativeID)
	fmt.Println()

	// Fetch spec status
	specURL := fmt.Sprintf("%s/api/work/initiatives/%d/spec-status", cfg.APIURL, cfg.InitiativeID)
	specResp, err := api.DoAuthenticatedGet(specURL, 30*time.Second)
	if err == nil && specResp.StatusCode == http.StatusOK {
		var specResult struct {
			Data struct {
				ArtifactCount int `json:"artifact_count"`
				Artifacts     []struct {
					Name     string `json:"name"`
					Category string `json:"category"`
					Status   string `json:"status"`
				} `json:"artifacts"`
			} `json:"data"`
		}
		if json.NewDecoder(specResp.Body).Decode(&specResult) == nil {
			fmt.Printf("📄 Specifications: %d\n", specResult.Data.ArtifactCount)
			for _, art := range specResult.Data.Artifacts {
				statusIcon := "⬜"
				if art.Status == "complete" {
					statusIcon = "✅"
				}
				fmt.Printf("   %s [%s] %s\n", statusIcon, art.Category, art.Name)
			}
			fmt.Println()
		}
		specResp.Body.Close()
	}

	// Fetch task status
	taskURL := fmt.Sprintf("%s/api/work/initiatives/%d/task-derivation-status", cfg.APIURL, cfg.InitiativeID)
	taskResp, err := api.DoAuthenticatedGet(taskURL, 30*time.Second)
	if err == nil && taskResp.StatusCode == http.StatusOK {
		var taskResult struct {
			Data struct {
				EpicCount int `json:"epic_count"`
				Epics     []struct {
					Code        string `json:"code"`
					Title       string `json:"title"`
					Status      string `json:"status"`
					StoryPoints int    `json:"story_points"`
				} `json:"epics"`
			} `json:"data"`
		}
		if json.NewDecoder(taskResp.Body).Decode(&taskResult) == nil {
			fmt.Printf("📌 Tasks: %d\n", taskResult.Data.EpicCount)
			for _, task := range taskResult.Data.Epics {
				statusIcon := "⬜"
				switch task.Status {
				case "done":
					statusIcon = "✅"
				case "in_progress":
					statusIcon = "🔄"
				case "blocked":
					statusIcon = "🚫"
				}
				fmt.Printf("   %s [%s] %s (%d pts)\n", statusIcon, task.Code, task.Title, task.StoryPoints)
			}
			fmt.Println()
		}
		taskResp.Body.Close()
	}

	return nil
}

func runWorkSubtasks(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	taskID := args[0]

	url := fmt.Sprintf("%s/api/work/epics/%s/tasks", cfg.APIURL, taskID)

	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		printError("Failed to connect: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		printError("API error: %s - %s\n", resp.Status, string(body))
		return nil
	}

	var result struct {
		Data []struct {
			ID          string `json:"id"`
			Code        string `json:"code"`
			Title       string `json:"title"`
			Status      string `json:"status"`
			Priority    int    `json:"priority"`
			StoryPoints int    `json:"story_points"`
			TaskType    string `json:"task_type"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	fmt.Println()
	bold.Printf("Subtasks for Task %s\n", taskID)
	fmt.Println("─────────────────────────────────────────")

	if len(result.Data) == 0 {
		fmt.Println("No subtasks found.")
	} else {
		for _, subtask := range result.Data {
			statusColor := color.New(color.FgWhite)
			switch subtask.Status {
			case "done":
				statusColor = green
			case "in_progress":
				statusColor = yellow
			}

			fmt.Printf("[%s] %s\n", subtask.Code, subtask.Title)
			fmt.Printf("      Type: %s | Status: ", subtask.TaskType)
			statusColor.Printf("%s", subtask.Status)
			fmt.Printf(" | Points: %d\n", subtask.StoryPoints)
		}
	}

	fmt.Println()
	return nil
}

func runWorkNew(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	var description string
	if len(args) > 0 {
		description = strings.Join(args, " ")
	} else {
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err == nil {
				description = string(data)
			}
		}
	}

	if description == "" {
		printError("No description provided. Provide it as an argument or via stdin.\n")
		printInfo("Examples:\n")
		fmt.Println("  modernpath work new \"Add user authentication\"")
		fmt.Println("  echo \"Add feature\" | modernpath work new")
		return nil
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Creating new epic...\n")
	fmt.Println()
	fmt.Printf("  Description: %s\n", truncateString(description, 100))
	fmt.Printf("  Type: %s\n", workNewType)
	fmt.Println()

	url := fmt.Sprintf("%s/api/roadmap/systems/%d/create-idea-and-initiative", baseURL, cfg.SystemID)

	payload := map[string]interface{}{
		"description": description,
		"idea_type":   workNewType,
	}

	jsonPayload, _ := json.Marshal(payload)

	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer(jsonPayload), 2*time.Minute)
	if err != nil {
		return fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusCreated {
		var errorResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errorResp) == nil {
			printError("Failed to create epic: %s\n", errorResp.Error)
		} else {
			printError("API error: %s - %s\n", resp.Status, string(body))
		}
		return fmt.Errorf("failed to create epic")
	}

	var result struct {
		Data struct {
			Idea struct {
				ID       interface{} `json:"id"`
				Title    string      `json:"title"`
				IdeaType string      `json:"idea_type"`
				Priority string      `json:"priority"`
			} `json:"idea"`
			Initiative struct {
				ID            int    `json:"id"`
				Title         string `json:"title"`
				Name          string `json:"name"`
				Goal          string `json:"goal"`
				Status        string `json:"status"`
				WorkflowPhase string `json:"workflow_phase"`
			} `json:"initiative"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	fmt.Println()
	printSuccess("Epic created!\n")
	fmt.Println()
	initiativeTitle := result.Data.Initiative.Title
	if initiativeTitle == "" {
		initiativeTitle = result.Data.Initiative.Name
	}

	fmt.Printf("  📋 Epic: %s\n", initiativeTitle)
	fmt.Printf("     ID: %d | Status: %s | Phase: %s\n",
		result.Data.Initiative.ID,
		result.Data.Initiative.Status,
		result.Data.Initiative.WorkflowPhase)
	fmt.Println()

	cfg.InitiativeID = result.Data.Initiative.ID
	cfg.InitiativeName = initiativeTitle
	cfg.InitiativeSpecsDir = config.InitiativeSpecsRelPath(result.Data.Initiative.ID, initiativeTitle)
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Failed to update config: %v\n", err)
	} else {
		printSuccess("Epic selected as active\n")
		fmt.Println()
		printInfo("Next steps:\n")
		fmt.Println("  Generate specifications: modernpath work specs generate")
		fmt.Println("  Derive tasks: modernpath work derive")
	}

	return nil
}

func runWorkDerive(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.InitiativeID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Deriving tasks from specifications for epic %d...\n", cfg.InitiativeID)
	printInfo("This analyzes your specs and creates:\n")
	fmt.Println("  - Tasks from architecture components")
	fmt.Println("  - Stories from requirements and flows")
	fmt.Println("  - Subtasks from interfaces and data entities")
	fmt.Println("  - Dependencies between work items")
	fmt.Println()

	url := fmt.Sprintf("%s/api/work/initiatives/%d/derive-tasks-agentic", baseURL, cfg.InitiativeID)

	startTime := time.Now()
	printInfo("Deriving tasks (timeout: 5 minutes)...\n")

	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer([]byte(`{"method": "enhanced"}`)), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error   string `json:"error"`
			Details string `json:"details"`
		}
		if json.Unmarshal(body, &errorResp) == nil {
			printError("Task derivation failed: %s\n", errorResp.Error)
		} else {
			printError("API error: %s - %s\n", resp.Status, string(body))
		}
		return fmt.Errorf("task derivation failed")
	}

	var result struct {
		Data struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Summary struct {
				Epics   int `json:"epics"`
				Stories int `json:"stories"`
				Tasks   int `json:"tasks"`
			} `json:"summary"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	elapsed := time.Since(startTime)

	fmt.Println()
	printSuccess("Task derivation completed!\n")
	fmt.Println()
	fmt.Printf("  📊 Created:\n")
	fmt.Printf("     - Tasks: %d\n", result.Data.Summary.Epics)
	fmt.Printf("     - Stories: %d\n", result.Data.Summary.Stories)
	fmt.Printf("     - Subtasks: %d\n", result.Data.Summary.Tasks)
	fmt.Printf("  ⏱️  Duration: %s\n", elapsed.Round(time.Second))
	fmt.Println()
	printInfo("Next steps:\n")
	fmt.Println("  List tasks: modernpath tasks list")
	fmt.Println("  Start implementing: modernpath dev task")

	return nil
}

func runWorkSpecsGenerate(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.InitiativeID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Starting specification pipeline for epic %d...\n", cfg.InitiativeID)
	printInfo("This may take several minutes. Running 6 phases:\n")
	fmt.Println("  1. Discovery - Requirements & User Stories")
	fmt.Println("  2. Architecture - Component specs & C4 diagrams")
	fmt.Println("  3. Data - ERD & Data dictionary")
	fmt.Println("  4. UI/UX - Wireframes & Design system")
	fmt.Println("  5. Testing - Test plans & Coverage matrix")
	fmt.Println("  6. Validation - Cross-check all outputs")
	fmt.Println()

	url := fmt.Sprintf("%s/api/work/initiatives/%d/run-pipeline", baseURL, cfg.InitiativeID)

	startTime := time.Now()
	printInfo("Executing pipeline (timeout: 5 minutes)...\n")

	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer([]byte("{}")), 5*time.Minute)
	if err != nil {
		return fmt.Errorf("API request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		printError("Pipeline failed: %s\n", string(body))
		return fmt.Errorf("pipeline failed")
	}

	elapsed := time.Since(startTime)

	fmt.Println()
	printSuccess("Specification pipeline started!\n")
	fmt.Printf("  ⏱️  Duration: %s\n", elapsed.Round(time.Second))
	fmt.Println()
	printInfo("Next steps:\n")
	fmt.Println("  Check status: modernpath work status")
	fmt.Println("  Download specs: modernpath work specs sync")
	fmt.Println("  Derive tasks: modernpath work derive")

	return nil
}

func runWorkSpecsSync(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.InitiativeID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	printInfo("Syncing specifications for epic %d...\n", cfg.InitiativeID)
	fmt.Println()

	specsRelPath, err := syncSpecs(cfg.APIURL, cfg.InitiativeID, cfg.InitiativeName)
	if err != nil {
		printError("Failed to sync specs: %v\n", err)
		return err
	}

	cfg.InitiativeSpecsDir = specsRelPath
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Failed to update config: %v\n", err)
	}

	printSuccess("Specifications synced successfully!\n")
	fmt.Println()
	printInfo("Specifications are now available in: .modernpath/%s/\n", specsRelPath)

	return nil
}

func runWorkSpecsPush(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.InitiativeID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return nil
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Pushing specifications for epic %d...\n", cfg.InitiativeID)
	fmt.Println()

	specsDir, err := config.ResolveInitiativeSpecsDir(cfg)
	if err != nil {
		printError("Failed to resolve specs directory: %v\n", err)
		return err
	}

	count, err := pushSpecs(baseURL, cfg.InitiativeID, specsDir)
	if err != nil {
		printError("Failed to push specs: %v\n", err)
		return err
	}

	if count == 0 {
		printWarning("No specifications found to push.\n")
		printInfo("Make sure you have specs in .modernpath/%s/\n", config.ResolveInitiativeSpecsRelPath(cfg))
		return nil
	}

	printSuccess("Pushed %d specifications successfully!\n", count)

	return nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// autoSyncTransformFiles syncs source files for transformation epics
func autoSyncTransformFiles(cfg *config.Config, epicID int) error {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	requestURL := fmt.Sprintf("%s/api/transform/source-files?initiative_id=%d", baseURL, epicID)

	resp, err := api.DoAuthenticatedGet(requestURL, 120*time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch source files: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	var filesResp TransformSourceFilesResponse
	if err := json.Unmarshal(body, &filesResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !filesResp.Success {
		return fmt.Errorf("API error: %s", filesResp.Error)
	}

	d := filesResp.Data
	if d.TotalFiles == 0 {
		printInfo("No transform items configured for this epic.\n")
		return nil
	}

	sourceDir := ".modernpath/source"
	if err := createDirIfNotExists(sourceDir); err != nil {
		return err
	}

	syncedCount := 0
	for _, file := range d.Files {
		if file.ContentError != "" || file.Content == "" {
			continue
		}

		filePath := file.FilePath
		if filePath == "" {
			filePath = file.Name
		}

		fullPath := filepath.Join(sourceDir, filePath)
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			continue
		}

		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			continue
		}

		syncedCount++
	}

	if syncedCount > 0 {
		printInfo("Synced %d source files\n", syncedCount)
		generateTransformAgentsMD(cfg, &filesResp)
	}

	return nil
}

func createDirIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

func generateTransformAgentsMD(cfg *config.Config, filesResp *TransformSourceFilesResponse) {
	d := filesResp.Data

	agentsCtx := &agents.TransformContext{
		InitiativeID:     d.InitiativeID,
		InitiativeName:   d.InitiativeName,
		SpecsRelPath:     config.ResolveInitiativeSpecsRelPath(cfg),
		SourceSystemID:   d.SourceSystemID,
		TargetSystemID:   d.TargetSystemID,
		SourceSystemName: fmt.Sprintf("Source System %d", d.SourceSystemID),
		TargetSystemName: fmt.Sprintf("Target System %d", d.TargetSystemID),
		SourceFiles:      make([]agents.SourceFile, 0, len(d.Files)),
	}

	for _, file := range d.Files {
		if file.ContentError == "" && file.Content != "" {
			agentsCtx.SourceFiles = append(agentsCtx.SourceFiles, agents.SourceFile{
				Name:       file.Name,
				FilePath:   file.FilePath,
				Language:   file.Language,
				TotalLines: file.TotalLines,
				Purpose:    file.Purpose,
				Summary:    file.Summary,
			})
		}
	}

	specsDir, err := config.ResolveInitiativeSpecsDir(cfg)
	if err == nil {
		if entries, readErr := os.ReadDir(specsDir); readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					agentsCtx.SpecCategories = append(agentsCtx.SpecCategories, entry.Name())
				}
			}
		}
	}

	mpRoot := ".modernpath"
	seenRoot := map[string]bool{}
	var slugRoots []string
	addRoot := func(abs string) {
		if seenRoot[abs] {
			return
		}
		seenRoot[abs] = true
		slugRoots = append(slugRoots, abs)
	}
	if entries, err := os.ReadDir(mpRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() && !modernpathReservedTopDir(e.Name()) {
				addRoot(filepath.Join(mpRoot, e.Name()))
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(mpRoot, "docs")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				addRoot(filepath.Join(mpRoot, "docs", e.Name()))
			}
		}
	}
	for _, root := range slugRoots {
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if len(name) > 3 && strings.EqualFold(name[len(name)-3:], ".md") {
					agentsCtx.Docs = append(agentsCtx.Docs, agents.Document{
						Title: name[:len(name)-3],
						Tier:  "synced",
						Angle: "local",
					})
				}
			}
		}
	}

	agentsMDPath := "modernpath_agents.md"
	if err := agents.Generate(agentsCtx, agentsMDPath); err != nil {
		printWarning("Failed to generate modernpath_agents.md: %v\n", err)
	} else {
		printSuccess("Generated %s\n", agentsMDPath)
	}
}

type TransformSourceFilesResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		InitiativeID   int    `json:"initiative_id"`
		InitiativeName string `json:"initiative_name"`
		SourceSystemID int    `json:"source_system_id"`
		TargetSystemID int    `json:"target_system_id"`
		Repository     *struct {
			Name      string `json:"name"`
			LocalPath string `json:"local_path"`
		} `json:"repository"`
		Files []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FilePath     string `json:"file_path"`
			Language     string `json:"language"`
			Content      string `json:"content"`
			ContentError string `json:"content_error,omitempty"`
			TotalLines   int    `json:"total_lines"`
			Purpose      string `json:"purpose,omitempty"`
			Summary      string `json:"summary,omitempty"`
			Complexity   string `json:"complexity,omitempty"`
		} `json:"files"`
		TotalFiles     int `json:"total_files"`
		TransformItems []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Approach string `json:"approach,omitempty"`
			Priority string `json:"priority,omitempty"`
			Notes    string `json:"notes,omitempty"`
			Status   string `json:"status,omitempty"`
		} `json:"transform_items"`
		TransformItemCount int `json:"transform_item_count"`
	} `json:"data"`
}
