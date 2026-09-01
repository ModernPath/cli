package cmd

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/tasks"
	"github.com/spf13/cobra"
)

var tasksCmd = &cobra.Command{
	Use:   "tasks",
	Short: "List and download task context files",
	Long:  `List tasks for the active epic and fetch full context into .modernpath/tasks/<epic-slug>/.`,
}

var tasksListCmd = &cobra.Command{
	Use:   "list [epic_id]",
	Short: "List tasks for an epic",
	Long:  `List all tasks for an epic. Uses the current epic from config if no ID is provided.`,
	RunE:  runTasksList,
}

var tasksFetchCmd = &cobra.Command{
	Use:   "fetch [epic_id]",
	Short: "Download task context files to the active epic workspace",
	Long: `Fetch full development context for every task in an epic and save each as
.modernpath/tasks/<epic-slug>/<task-id>-<title-slug>.md`,
	RunE: runTasksFetch,
}

func init() {
	rootCmd.AddCommand(tasksCmd)
	tasksCmd.AddCommand(tasksListCmd)
	tasksCmd.AddCommand(tasksFetchCmd)
}

func resolveEpicID(cfg *config.Config, args []string) (int, error) {
	if len(args) > 0 {
		var epicID int
		if _, err := fmt.Sscanf(args[0], "%d", &epicID); err != nil || epicID == 0 {
			return 0, fmt.Errorf("invalid epic ID: %s", args[0])
		}
		return epicID, nil
	}
	if cfg.EpicID > 0 {
		return cfg.EpicID, nil
	}
	return 0, fmt.Errorf("no epic configured; run 'modernpath work select' first")
}

func runTasksList(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	epicID, err := resolveEpicID(cfg, args)
	if err != nil {
		printError("%v\n", err)
		printInfo("Usage: modernpath tasks list [epic_id]\n")
		return err
	}

	summaries, err := tasks.List(cfg.APIURL, epicID, api.DoAuthenticatedGet)
	if err != nil {
		printError("Failed to list tasks: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	fmt.Println()
	bold.Printf("Tasks for Epic %d\n", epicID)
	fmt.Println("─────────────────────────────────────────")

	if len(summaries) == 0 {
		fmt.Println("No tasks found.")
		printInfo("Run 'modernpath work specs generate' to create specifications first,\n")
		printInfo("then run 'modernpath work derive' to create tasks.\n")
	} else {
		for _, task := range summaries {
			statusColor := color.New(color.FgWhite)
			switch task.Status {
			case "done":
				statusColor = green
			case "in_progress":
				statusColor = yellow
			}

			fmt.Printf("[%s] %s\n", task.Code, task.DisplayTitle())
			fmt.Printf("      ID: %s | Status: ", task.ID)
			statusColor.Printf("%s", task.Status)
			fmt.Printf(" | Points: %d\n", task.EstimatedStoryPoints)
		}
	}

	fmt.Println()
	return nil
}

func runTasksFetch(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	epicID, err := resolveEpicID(cfg, args)
	if err != nil {
		printError("%v\n", err)
		printInfo("Usage: modernpath tasks fetch [epic_id]\n")
		return err
	}

	printInfo("Fetching task context for epic %d...\n", epicID)

	count, warnings, err := tasks.FetchAll(cfg.APIURL, epicID, cfg.EpicName, api.DoAuthenticatedGet)
	if err != nil {
		printError("Failed to fetch tasks: %v\n", err)
		for _, warning := range warnings {
			printWarning("%s\n", warning)
		}
		return err
	}

	for _, warning := range warnings {
		printWarning("%s\n", warning)
	}

	if count == 0 {
		printWarning("No tasks found to download.\n")
		return nil
	}

	relPath := config.EpicSpecsRelPath(epicID, cfg.EpicName)
	printSuccess("Saved %d task context file(s) to .modernpath/%s/\n", count, relPath)
	return nil
}

func fetchTasksForEpic(cfg *config.Config, epicID int) error {
	printInfo("Fetching task context files for epic [%d]...\n", epicID)

	count, warnings, err := tasks.FetchAll(cfg.APIURL, epicID, cfg.EpicName, api.DoAuthenticatedGet)
	if err != nil {
		return err
	}

	for _, warning := range warnings {
		printWarning("%s\n", warning)
	}

	if count == 0 {
		printInfo("No tasks to download for this epic.\n")
		return nil
	}

	relPath := config.EpicSpecsRelPath(epicID, cfg.EpicName)
	printSuccess("Saved %d task context file(s) to .modernpath/%s/\n", count, relPath)
	return nil
}
