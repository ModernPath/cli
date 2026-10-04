package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"time"
)

func runWorkDerive(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.EpicID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return fmt.Errorf("no epic configured")
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Deriving tasks from specifications for epic %d...\n", cfg.EpicID)
	printInfo("This analyzes your specs and creates:\n")
	fmt.Println("  - Tasks from architecture components")
	fmt.Println("  - Subtasks from requirements, flows, interfaces, and data entities")
	fmt.Println("  - Dependencies between work items")
	fmt.Println()

	url := fmt.Sprintf("%s/api/work/epics/%d/derive-tasks-agentic", baseURL, cfg.EpicID)

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
				Tasks    int `json:"tasks"`
				Subtasks int `json:"subtasks"`
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
	fmt.Printf("     - Tasks: %d\n", result.Data.Summary.Tasks)
	fmt.Printf("     - Subtasks: %d\n", result.Data.Summary.Subtasks)
	fmt.Printf("  ⏱️  Duration: %s\n", elapsed.Round(time.Second))
	fmt.Println()
	printInfo("Next steps:\n")
	fmt.Println("  List tasks: modernpath tasks list")
	fmt.Println("  Start implementing: modernpath dev task")

	return nil
}
