package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"strings"
	"time"
)

// workSubtask is one subtask under a Task.
type workSubtask struct {
	ID              string `json:"id"`
	Code            string `json:"code"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	Priority        int    `json:"priority"`
	EstimatedPoints int    `json:"estimated_points"`
	SubtaskType     string `json:"subtask_type"`
}

// fetchSubtasks returns the subtasks embedded in GET /api/work/tasks/:id.
// A non-200 answer is an error — the command must not report success over
// an API failure.
func fetchSubtasks(baseURL, taskID string) ([]workSubtask, error) {
	url := fmt.Sprintf("%s/api/work/tasks/%s", baseURL, taskID)

	resp, err := api.DoAuthenticatedGet(url, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var result struct {
		Data struct {
			Subtasks []workSubtask `json:"subtasks"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}

	return result.Data.Subtasks, nil
}

func runWorkSubtasks(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	taskID := args[0]

	subtasks, err := fetchSubtasks(cfg.APIURL, taskID)
	if err != nil {
		printError("%v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	fmt.Println()
	bold.Printf("Subtasks for Task %s\n", taskID)
	fmt.Println("─────────────────────────────────────────")

	if len(subtasks) == 0 {
		fmt.Println("No subtasks found.")
	} else {
		for _, subtask := range subtasks {
			statusColor := color.New(color.FgWhite)
			switch subtask.Status {
			case "done":
				statusColor = green
			case "in_progress":
				statusColor = yellow
			}

			fmt.Printf("[%s] %s\n", subtask.Code, subtask.Title)
			fmt.Printf("      Status: ")
			statusColor.Printf("%s", subtask.Status)
			fmt.Printf(" | Points: %d\n", subtask.EstimatedPoints)
		}
	}

	fmt.Println()
	return nil
}
