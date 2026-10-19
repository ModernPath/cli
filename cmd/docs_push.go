package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

func runDocsPush(cmd *cobra.Command, args []string) error {
	machine, _ := cmd.Flags().GetBool("json")
	cfg, err := commandConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		return fmt.Errorf("no system configured. Run 'modernpath init' first")
	}

	archName := cfg.SystemName
	if archName == "" {
		archName = fmt.Sprintf("system %d", cfg.SystemID)
	}

	if !machine {
		printInfo("Pushing documentation for %s (system %d)...\n", archName, cfg.SystemID)
	}

	printInfo("Scanning .modernpath/ for documentation to push...\n")

	// Use the pushDocs function from sync.go
	count, err := pushDocs(cfg)
	if err != nil {
		printError("Failed to push docs: %v\n", err)
		return err
	}

	if count == 0 {
		if machine {
			return writeCommandJSON(cmd, map[string]any{"pushed": false, "system_id": cfg.SystemID, "documents": 0})
		}
		printInfo("No documentation files found to push.\n")
		printInfo("Make sure markdown exists under .modernpath/<slug>/ or run `modernpath docs sync`.\n")
		return nil
	}

	appURL := systemAppLink(readAppURL(newAuthenticatedClient(cfg)), cfg.APIURL, cfg.SystemID, "documents")
	if machine {
		return writeCommandJSON(cmd, map[string]any{"pushed": true, "system_id": cfg.SystemID, "documents": count, "url": appURL})
	}
	printSuccess("Pushed %d documents to ModernPath\n", count)
	// REQ-CROSS-502 C3: the link opens the app host, not the API host.
	printInfo("View updated docs at: %s\n", appURL)

	return nil
}
