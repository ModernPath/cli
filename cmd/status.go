package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current ModernPath status",
	Long:  `Display the current ModernPath project status including system, sync status, and documentation.`,
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	configDir, err := config.FindConfigDir()
	if err != nil || configDir == "" {
		printWarning("Not a ModernPath project. Run 'modernpath init' to initialize.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	fmt.Println()
	bold.Println("ModernPath Project Status")
	fmt.Println("─────────────────────────────────────────")

	// Environment info (prominent)
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	envName := getEnvName(apiURL)
	fmt.Printf("Environment:   ")
	if envName == "production" {
		green.Printf("%s", envName)
	} else if envName == "local" {
		yellow.Printf("%s", envName)
	} else {
		fmt.Printf("%s", envName)
	}
	fmt.Printf(" (%s)\n", apiURL)

	if cfg.SystemID > 0 {
		fmt.Printf("System:  %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
		fmt.Printf("Slug:          %s\n", cfg.SystemSlug)
	} else {
		fmt.Println("System:  Not configured")
	}

	if cfg.InitiativeID > 0 {
		fmt.Printf("Initiative:    %s (ID: %d)\n", cfg.InitiativeName, cfg.InitiativeID)
	} else {
		fmt.Println("Initiative:    Not configured")
	}

	if cfg.LastSyncAt != "" {
		fmt.Printf("Last Sync:     %s\n", cfg.LastSyncAt)
	} else {
		fmt.Println("Last Sync:     Never")
	}

	// Check docs directory
	docsPath, err := config.GetDocsPath()
	if err == nil && docsPath != "" {
		if info, err := os.Stat(docsPath); err == nil && info.IsDir() {
			mdCount := 0
			filepath.Walk(docsPath, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && filepath.Ext(path) == ".md" {
					mdCount++
				}
				return nil
			})
			fmt.Printf("Local Docs:    %d markdown files\n", mdCount)
		}
	} else {
		fmt.Println("Local Docs:    Not synced (run 'modernpath docs sync')")
	}

	fmt.Println()
	return nil
}

func getEnvName(url string) string {
	switch url {
	case config.DefaultAPIURL:
		return "production"
	case config.LocalAPIURL:
		return "local"
	default:
		return "custom"
	}
}
