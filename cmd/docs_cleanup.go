package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"time"
)

func runDocsCleanup(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	fmt.Println()
	fmt.Println("🧹 Documentation Cleanup")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "dry-run (preview only)", false: "delete"}[docsCleanupDryRun])
	fmt.Println()

	// Call cleanup endpoint
	client := newAuthenticatedClient(cfg)
	client.client.Timeout = 60 * time.Second

	url := fmt.Sprintf("%s/api/systems/%d/cleanup-excluded?dry_run=%v",
		client.baseURL, cfg.SystemID, docsCleanupDryRun)

	resp, err := client.Delete(url)
	if err != nil {
		printError("Failed to call cleanup API: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		printError("API error: %s - %s\n", resp.Status, string(body))
		return fmt.Errorf("cleanup failed")
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Mode          string   `json:"mode"`
			FilesToDelete int      `json:"files_to_delete"`
			TotalLines    int      `json:"total_lines"`
			SampleFiles   []string `json:"sample_files"`
			DeletedCount  int      `json:"deleted_count"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	if docsCleanupDryRun {
		if result.Data.FilesToDelete == 0 {
			printSuccess("✅ No excluded files found in database!\n")
			return nil
		}

		fmt.Printf("📊 Found %d excluded files (%.0f KB of code)\n",
			result.Data.FilesToDelete,
			float64(result.Data.TotalLines)*30/1024) // rough estimate
		fmt.Println()
		fmt.Println("📋 Sample files that would be deleted:")
		fmt.Println("─────────────────────────────────────────────────────────────")
		for i, f := range result.Data.SampleFiles {
			if i >= 15 {
				fmt.Printf("  ... and %d more files\n", result.Data.FilesToDelete-15)
				break
			}
			fmt.Printf("  %d. %s\n", i+1, f)
		}
		fmt.Println()
		printInfo("Run without --dry-run to delete these files.\n")
	} else {
		if result.Data.DeletedCount == 0 {
			printSuccess("✅ No excluded files to delete!\n")
		} else {
			printSuccess("✅ Deleted %d excluded file records from database.\n", result.Data.DeletedCount)
			printInfo("Run 'modernpath scan' to see updated scores.\n")
		}
	}

	return nil
}
