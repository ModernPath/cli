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
	"os"
	"strings"
	"time"
)

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

	url := fmt.Sprintf("%s/api/roadmap/systems/%d/create-idea-and-epic", baseURL, cfg.SystemID)

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
			Epic struct {
				ID            int    `json:"id"`
				Title         string `json:"title"`
				Name          string `json:"name"`
				Goal          string `json:"goal"`
				Status        string `json:"status"`
				WorkflowPhase string `json:"workflow_phase"`
			} `json:"epic"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %v", err)
	}

	fmt.Println()
	printSuccess("Epic created!\n")
	fmt.Println()
	epicTitle := result.Data.Epic.Title
	if epicTitle == "" {
		epicTitle = result.Data.Epic.Name
	}

	fmt.Printf("  📋 Epic: %s\n", epicTitle)
	fmt.Printf("     ID: %d | Status: %s | Phase: %s\n",
		result.Data.Epic.ID,
		result.Data.Epic.Status,
		result.Data.Epic.WorkflowPhase)
	fmt.Println()

	cfg.EpicID = result.Data.Epic.ID
	cfg.EpicName = epicTitle
	cfg.EpicSpecsDir = config.EpicSpecsRelPath(result.Data.Epic.ID, epicTitle)
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
