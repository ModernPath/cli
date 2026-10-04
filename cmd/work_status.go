package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"net/http"
	"time"
)

func runWorkStatus(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.EpicID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return fmt.Errorf("no epic configured")
	}

	bold := color.New(color.Bold)

	fmt.Println()
	bold.Printf("📋 Epic Status: %s\n", cfg.EpicName)
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("  Epic ID: %d\n", cfg.EpicID)
	fmt.Println()

	// Fetch spec status
	specURL := fmt.Sprintf("%s/api/work/epics/%d/spec-status", cfg.APIURL, cfg.EpicID)
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
	taskURL := fmt.Sprintf("%s/api/work/epics/%d/task-derivation-status", cfg.APIURL, cfg.EpicID)
	taskResp, err := api.DoAuthenticatedGet(taskURL, 30*time.Second)
	if err == nil && taskResp.StatusCode == http.StatusOK {
		var taskResult struct {
			Data struct {
				TaskCount int `json:"task_count"`
				Tasks     []struct {
					Code                 string `json:"code"`
					Title                string `json:"title"`
					Status               string `json:"status"`
					EstimatedStoryPoints int    `json:"estimated_story_points"`
				} `json:"tasks"`
			} `json:"data"`
		}
		if json.NewDecoder(taskResp.Body).Decode(&taskResult) == nil {
			fmt.Printf("📌 Tasks: %d\n", taskResult.Data.TaskCount)
			for _, task := range taskResult.Data.Tasks {
				statusIcon := "⬜"
				switch task.Status {
				case "done":
					statusIcon = "✅"
				case "in_progress":
					statusIcon = "🔄"
				case "blocked":
					statusIcon = "🚫"
				}
				fmt.Printf("   %s [%s] %s (%d pts)\n", statusIcon, task.Code, task.Title, task.EstimatedStoryPoints)
			}
			fmt.Println()
		}
		taskResp.Body.Close()
	}

	return nil
}
