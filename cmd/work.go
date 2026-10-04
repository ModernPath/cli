package cmd

// Command registration. Focused work_*.go files own the implementations.

import (
	"github.com/spf13/cobra"
)

var workCmd = &cobra.Command{
	Use:   "work",
	Short: "Manage epics, tasks, and subtasks",
	Long:  `View and manage work items including epics, tasks, and subtasks.`,
}

// work list - List epics.
var workListCmd = &cobra.Command{
	Use:   "list",
	Short: "List epics for current system",
	RunE:  runWorkList,
}

// work select - Select an epic.
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
