package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
)

func runDocsRepair(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return reportedError{err}
	}

	if cfg.SystemID == 0 {
		return reportFailure(errNoBoundSystem)
	}

	repoID, err := resolveBoundRepository(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		return reportedError{err}
	}

	fmt.Println()
	fmt.Println("🔧 Documentation Repair")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
	fmt.Printf("Repository ID: %d\n", repoID)
	fmt.Printf("Limit: %d files\n", docsRepairLimit)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "dry-run (preview only)", false: "repair"}[docsRepairDryRun])
	fmt.Println()

	// First, get count of incomplete files
	previewResp, err := getRepairPreview(cfg, repoID)
	if err != nil {
		printError("Failed to get repair preview: %v\n", err)
		return reportedError{err}
	}

	if previewResp.Data.IncompleteCount == 0 {
		printSuccess("✅ All files are fully analyzed! Nothing to repair.\n")
		return nil
	}

	fmt.Printf("📊 Found %d files with incomplete analysis (%.1f%% of %d total)\n",
		previewResp.Data.IncompleteCount,
		previewResp.Data.IncompletePercentage,
		previewResp.Data.TotalFiles)
	fmt.Println()

	if docsRepairDryRun {
		fmt.Println("📋 Files that would be repaired:")
		fmt.Println("─────────────────────────────────────────────────────────────")
		limit := docsRepairLimit
		if limit > len(previewResp.Data.Files) {
			limit = len(previewResp.Data.Files)
		}
		for i, f := range previewResp.Data.Files[:limit] {
			status := ""
			if !f.HasSummary {
				status += " [no summary]"
			}
			if !f.HasComplexity {
				status += " [no complexity]"
			}
			fmt.Printf("  %d. %s%s\n", i+1, f.RelativePath, status)
		}
		if len(previewResp.Data.Files) > limit {
			fmt.Printf("  ... and %d more files\n", len(previewResp.Data.Files)-limit)
		}
		fmt.Println()
		printInfo("Run without --dry-run to repair these files.\n")
		return nil
	}

	// Confirm before proceeding
	fmt.Printf("→ This will re-analyze up to %d files. Continue? [y/N] ", docsRepairLimit)
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" {
		printInfo("Cancelled.\n")
		return nil
	}

	fmt.Println()
	fmt.Println("→ Starting repair analysis...")
	fmt.Println("  (This runs in the background - check server logs for progress)")
	fmt.Println()

	// Trigger repair
	err = triggerRepair(cfg, repoID)
	if err != nil {
		printError("Failed to start repair: %v\n", err)
		return reportedError{err}
	}

	printSuccess("✅ Repair started! Files are being analyzed in the background.\n")
	printInfo("Run 'modernpath scan' after completion to see updated scores.\n")

	return nil
}

type RepairPreviewResponse struct {
	Success bool `json:"success"`
	Data    struct {
		IncompleteCount      int     `json:"incomplete_count"`
		TotalFiles           int     `json:"total_files"`
		IncompletePercentage float64 `json:"incomplete_percentage"`
		Files                []struct {
			ID            int    `json:"id"`
			RelativePath  string `json:"relative_path"`
			Language      string `json:"language"`
			HasSummary    bool   `json:"has_summary"`
			HasComplexity bool   `json:"has_complexity"`
			LinesOfCode   int    `json:"lines_of_code"`
		} `json:"files"`
	} `json:"data"`
}

func getRepairPreview(cfg *config.Config, repoID int) (*RepairPreviewResponse, error) {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/incomplete-files",
		client.baseURL, cfg.SystemID, repoID)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get preview: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var preview RepairPreviewResponse
	if err := json.Unmarshal(body, &preview); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &preview, nil
}

func triggerRepair(cfg *config.Config, repoID int) error {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/repair-analysis",
		client.baseURL, cfg.SystemID, repoID)

	reqBody := fmt.Sprintf(`{"limit": %d}`, docsRepairLimit)

	resp, err := client.Post(url, "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		return fmt.Errorf("failed to trigger repair: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	return nil
}
