package cmd

import (
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

func runDocsPush(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	archName := cfg.SystemName
	if archName == "" {
		archName = fmt.Sprintf("system %d", cfg.SystemID)
	}

	fmt.Println()
	fmt.Println("📤 Push Documentation")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", archName, cfg.SystemID)
	fmt.Printf("API URL: %s\n", cfg.APIURL)
	fmt.Println()

	printInfo("Scanning .modernpath/ for documentation to push...\n")

	// Use the pushDocs function from sync.go
	count, err := pushDocs(cfg)
	if err != nil {
		printError("Failed to push docs: %v\n", err)
		return err
	}

	if count == 0 {
		printInfo("No documentation files found to push.\n")
		printInfo("Make sure markdown exists under .modernpath/<slug>/ or run `modernpath docs sync`.\n")
		return nil
	}

	fmt.Println()
	printSuccess("✅ Successfully pushed %d documents to ModernPath!\n", count)
	// REQ-CROSS-502 C3: the link opens the app host, not the API host.
	printInfo("View updated docs at: %s\n", systemAppLink(readAppURL(newAuthenticatedClient(cfg)), cfg.APIURL, cfg.SystemID, "documents"))

	return nil
}
