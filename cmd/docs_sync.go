package cmd

import (
	"errors"
	"fmt"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"path/filepath"
)

func runDocsSync(cmd *cobra.Command, args []string) error {
	// REQ-CROSS-405: the binding and credential statements come first, and a
	// refusal here sends nothing.
	env, err := apiClientCredentialLoad()
	if err != nil {
		printError("%v\n", err)
		return err
	}

	cfg := env.Config
	if cfg == nil {
		return errors.New("bound workspace configuration is unavailable")
	}

	archName := cfg.SystemName
	if archName == "" {
		archName = fmt.Sprintf("system %d", cfg.SystemID)
	}

	printInfo("Syncing documentation for %s from %s...\n", archName, cfg.APIURL)

	client := api.NewClient(env.APIURL, env.token)

	// Health check
	if err := client.HealthCheck(); err != nil {
		// A reachable server that rejects the credential answers 401 here on a
		// fail-closed platform host; render that as the credential statement,
		// not a bare status line (REQ-CROSS-405).
		if errors.Is(err, api.ErrUnauthorized) {
			err = env.credentialRejected()
		}
		printError("Cannot connect to ModernPath: %v\n", err)
		return err
	}

	// The export folder is the server's slug for the system at this moment;
	// the bound slug was recorded at import or connect. The server's is read
	// before any download — from the authenticated system list, never from
	// the archive being validated — and a drift is named with its remedy
	// (SR-RDD-ONBOARD-048). The binding is not rewritten here: the local
	// store and the export folder are keyed on it.
	served, err := servedSystemSlug(client, cfg.SystemID)
	if err != nil {
		if errors.Is(err, api.ErrUnauthorized) {
			err = env.credentialRejected()
		}
		printError("Failed to identify bound system: %v\n", err)
		return err
	}
	slug := cfg.SystemSlug
	if slug == "" {
		// A factory-only binding can omit the slug.
		slug = served
	}
	if slug != served {
		err = fmt.Errorf("the server's export folder for system %d is .modernpath/%s, this workspace is bound to .modernpath/%s; refresh the binding with modernpath factory connect", cfg.SystemID, served, slug)
		printError("%v\n", err)
		return err
	}

	// Download system export (documentation only, not specs)
	printInfo("Downloading system documentation...\n")

	zipData, err := downloadExportWithStatus(client, cfg.SystemID)
	if err != nil {
		if errors.Is(err, api.ErrUnauthorized) {
			err = env.credentialRejected()
		}
		printError("Failed to download: %v\n", err)
		return err
	}

	printSuccess("Downloaded %d bytes\n", len(zipData))

	// Validate and replace only the bound system's documentation tree. `init`
	// keeps its separate extract-into-the-directory-being-initialized behavior.
	printInfo("Extracting documentation...\n")
	configDir, err := config.WorkspaceConfigDir()
	if err != nil {
		return err
	}
	root := filepath.Dir(configDir)
	syncedAt, err := exportGeneratedAt(zipData, slug)
	if err != nil {
		printError("Failed to read export timestamp: %v\n", err)
		return err
	}
	if syncedAt == "" {
		printWarning("The server export has no generated_at timestamp; local sync time will be unknown until the server provides one.\n")
	}
	_, install, err := replaceSystemDocsExport(root, slug, zipData)
	if err != nil {
		printError("Failed to extract: %v\n", err)
		return err
	}

	// Keep the server's export timestamp in shared config for existing CLI
	// status consumers; an older server leaves it unknown.
	cfg.LastSyncAt = syncedAt
	if err := config.WriteConfig(cfg); err != nil {
		if rollbackErr := install.rollback(); rollbackErr != nil {
			err = fmt.Errorf("sync stamp failed: %v; export rollback failed: %w", err, rollbackErr)
		}
		printError("Failed to update sync stamp: %v\n", err)
		return err
	}
	install.commit()

	printSuccess("Documentation sync complete!\n")
	return nil
}

// servedSystemSlug reads the export slug the server derives for the bound
// system now.
func servedSystemSlug(client *api.Client, systemID int) (string, error) {
	systems, err := client.ListSystems()
	if err != nil {
		return "", err
	}
	for _, system := range systems {
		if system.ID == systemID && system.Slug != "" {
			return system.Slug, nil
		}
	}
	return "", fmt.Errorf("bound system %d has no reachable export slug", systemID)
}
