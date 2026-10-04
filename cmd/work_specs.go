package cmd

import (
	"bytes"
	"fmt"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
	"time"
)

func runWorkSpecsGenerate(cmd *cobra.Command, args []string) error {
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

	printInfo("Starting specification pipeline for epic %d...\n", cfg.EpicID)
	printInfo("This may take several minutes. Running 6 phases:\n")
	fmt.Println("  1. Discovery - Requirements & User Stories")
	fmt.Println("  2. Architecture - Component specs & C4 diagrams")
	fmt.Println("  3. Data - ERD & Data dictionary")
	fmt.Println("  4. UI/UX - Wireframes & Design system")
	fmt.Println("  5. Testing - Test plans & Coverage matrix")
	fmt.Println("  6. Validation - Cross-check all outputs")
	fmt.Println()

	url := fmt.Sprintf("%s/api/work/epics/%d/run-pipeline", baseURL, cfg.EpicID)

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

	if cfg.EpicID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return fmt.Errorf("no epic configured")
	}

	printInfo("Syncing specifications for epic %d...\n", cfg.EpicID)
	fmt.Println()

	specsRelPath, err := syncSpecs(cfg.APIURL, cfg.EpicID, cfg.EpicName)
	if err != nil {
		printError("Failed to sync specs: %v\n", err)
		return err
	}

	cfg.EpicSpecsDir = specsRelPath
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

	if cfg.EpicID == 0 {
		printError("No epic configured. Run 'modernpath work select' first.\n")
		return fmt.Errorf("no epic configured")
	}

	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = "http://localhost:4000"
	}

	printInfo("Pushing specifications for epic %d...\n", cfg.EpicID)
	fmt.Println()

	specsDir, err := config.ResolveEpicSpecsDir(cfg)
	if err != nil {
		printError("Failed to resolve specs directory: %v\n", err)
		return err
	}

	count, err := pushSpecs(baseURL, cfg.EpicID, specsDir)
	if err != nil {
		printError("Failed to push specs: %v\n", err)
		return err
	}

	if count == 0 {
		printWarning("No specifications found to push.\n")
		printInfo("Make sure you have specs in .modernpath/%s/\n", config.ResolveEpicSpecsRelPath(cfg))
		return nil
	}

	printSuccess("Pushed %d specifications successfully!\n", count)

	return nil
}
