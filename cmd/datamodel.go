package cmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/spf13/cobra"
)

var datamodelCmd = &cobra.Command{
	Use:   "datamodel",
	Short: "Data model operations",
	Long:  `Commands for working with data model exports.`,
}

var datamodelExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export only the data model",
	Long: `Export only the data model (domains, tables, relationships) for the current system.

This is a lightweight export containing just the data model - much faster than a full export.

Example:
  modernpath datamodel export          # Export to .modernpath/<system>/datamodel/
  modernpath datamodel export --json   # Output raw JSON to stdout`,
	RunE: runDatamodelExport,
}

var (
	datamodelJSONOutput bool
)

func init() {
	datamodelExportCmd.Flags().BoolVar(&datamodelJSONOutput, "json", false, "Output raw JSON to stdout instead of extracting")
	datamodelCmd.AddCommand(datamodelExportCmd)
	rootCmd.AddCommand(datamodelCmd)
}

func runDatamodelExport(cmd *cobra.Command, args []string) error {
	// Check if initialized
	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return fmt.Errorf("not initialized")
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	// --json promises parseable stdout — informational lines go to stderr
	// there (pre-PR review: this was a live D13 instance).
	if datamodelJSONOutput {
		fmt.Fprintf(os.Stderr, "exporting data model for %s\n", cfg.SystemName)
	} else {
		printInfo("Exporting data model for: %s\n", cfg.SystemName)
	}

	// Get API URL and token
	baseURL := apiURL
	if baseURL == "" {
		baseURL = cfg.APIURL
		if baseURL == "" {
			baseURL = config.DefaultAPIURL
		}
	}

	auth, _ := config.ReadAuth()
	token := ""
	if auth != nil {
		token = auth.Token
	}

	// Download data model export
	if datamodelJSONOutput {
		fmt.Fprintln(os.Stderr, "downloading data model...")
	} else {
		printInfo("Downloading data model...\n")
	}

	zipData, err := downloadDatamodelExport(baseURL, token, cfg.SystemID)
	if err != nil {
		printError("Failed to download data model: %v\n", err)
		return err
	}

	if datamodelJSONOutput {
		// Extract and print just the JSON
		jsonData, err := extractDatamodelJSON(zipData)
		if err != nil {
			printError("Failed to extract JSON: %v\n", err)
			return err
		}
		fmt.Println(string(jsonData))
		return nil
	}

	// Extract to .modernpath/
	printInfo("Extracting to .modernpath/...\n")

	if err := extractDatamodelZip(zipData); err != nil {
		printError("Failed to extract: %v\n", err)
		return err
	}

	printSuccess("Data model exported!\n")
	fmt.Printf("\nFiles written to .modernpath/%s/datamodel/\n", cfg.SystemSlug)
	fmt.Println("  - data_model.json (full artifact)")
	fmt.Println("  - README.md (overview)")
	fmt.Println("  - domains/*.md (one per domain)")

	return nil
}

func downloadDatamodelExport(baseURL, token string, systemID int) ([]byte, error) {
	url := fmt.Sprintf("%s/api/systems/%d/datamodel/export", strings.TrimRight(baseURL, "/"), systemID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)

	if err := platform.Authorize(req, token); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "ModernPath-CLI")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Try to extract error message
		if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
			return nil, fmt.Errorf("export failed: %s", string(body))
		}
		return nil, fmt.Errorf("export failed: HTTP %d", resp.StatusCode)
	}

	return body, nil
}

func extractDatamodelJSON(zipData []byte) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to open zip: %w", err)
	}

	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, "data_model.json") {
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}

	return nil, fmt.Errorf("data_model.json not found in export")
}

func extractDatamodelZip(zipData []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	for _, file := range reader.File {
		// Skip macOS metadata
		if strings.HasPrefix(file.Name, "__MACOSX/") || strings.Contains(file.Name, ".DS_Store") {
			continue
		}

		// Security: ensure path doesn't escape
		destPath := filepath.Join(cwd, file.Name)
		if !strings.HasPrefix(destPath, cwd) {
			continue
		}

		if file.FileInfo().IsDir() {
			continue
		}

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}

		// Extract file
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open zip entry: %w", err)
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file: %w", err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to write file: %w", err)
		}

		if verbose {
			fmt.Printf("  Extracted: %s\n", file.Name)
		}
	}

	return nil
}

// Ensure api package is used
var _ = api.NewClient
