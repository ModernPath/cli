package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"time"
)

// workEpic mirrors GET /api/work/epics list items.
type workEpic struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Name          string `json:"name"` // legacy alias; prefer Title
	Status        string `json:"status"`
	WorkflowPhase string `json:"workflow_phase"`
	ProjectType   string `json:"project_type"`
}

func (i workEpic) displayTitle() string {
	if i.Title != "" {
		return i.Title
	}
	return i.Name
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

	url := fmt.Sprintf("%s/api/work/epics?system_id=%d", cfg.APIURL, cfg.SystemID)

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
		Data []workEpic `json:"data"`
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
			if epic.ID == cfg.EpicID {
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

	url := fmt.Sprintf("%s/api/work/epics?system_id=%d", cfg.APIURL, cfg.SystemID)

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
		Data []workEpic `json:"data"`
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
			if epic.ID == cfg.EpicID {
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

	cfg.EpicID = selectedID
	cfg.EpicName = selectedName

	fmt.Println()
	printInfo("Syncing specifications for epic [%d]...\n", selectedID)
	specsRelPath, err := syncSpecs(cfg.APIURL, selectedID, selectedName)
	if err != nil {
		printWarning("Failed to sync specs: %v\n", err)
	} else {
		cfg.EpicSpecsDir = specsRelPath
		printSuccess("Specifications synced to .modernpath/%s/\n", specsRelPath)
	}

	if err := config.WriteConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}

	if err := fetchTasksForEpic(cfg, selectedID); err != nil {
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
