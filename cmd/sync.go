package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync documentation from ModernPath platform",
	Long: `Download the latest documentation and analysis data
from the ModernPath platform.

This updates the local .modernpath directory with the latest data.
Note: Specifications are automatically synced when selecting an initiative
via 'modernpath work select'.`,
	RunE: runSync,
}

func runSync(cmd *cobra.Command, args []string) error {
	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	printInfo("Syncing %s from %s...\n", cfg.SystemName, cfg.APIURL)

	// Create API client
	client := api.NewClient(cfg.APIURL, "")

	// Health check
	if err := client.HealthCheck(); err != nil {
		printError("Cannot connect to ModernPath: %v\n", err)
		return err
	}

	// Download system export
	printInfo("Downloading system data...\n")

	zipData, err := client.DownloadExport(cfg.SystemID)
	if err != nil {
		printError("Failed to download: %v\n", err)
		return err
	}

	printSuccess("Downloaded %d bytes\n", len(zipData))

	// Extract
	printInfo("Extracting...\n")

	if err := extractZip(zipData); err != nil {
		printError("Failed to extract: %v\n", err)
		return err
	}

	// Note: Specs are now synced automatically when selecting an initiative
	// via 'modernpath work select' command

	// Update last sync time
	cfg.LastSyncAt = time.Now().Format(time.RFC3339)
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Failed to update config: %v\n", err)
	}

	printSuccess("Sync complete!\n")
	return nil
}

// syncSpecs downloads specs from the API and saves them to .modernpath/specs/ folder
// It clears the existing specs folder before downloading new ones
func syncSpecs(baseURL string, initiativeID int) error {
	// Get config directory
	configDir, err := config.GetConfigDir(true)
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	// Specs directory is inside .modernpath
	specsDir := filepath.Join(configDir, "specs")

	// Clear existing specs if directory exists
	if _, err := os.Stat(specsDir); err == nil {
		printInfo("Clearing existing specs...\n")
		if err := os.RemoveAll(specsDir); err != nil {
			printWarning("Failed to clear existing specs: %v\n", err)
		}
	}

	// Create specs directory
	if err := os.MkdirAll(specsDir, 0755); err != nil {
		return fmt.Errorf("failed to create specs directory: %w", err)
	}

	url := fmt.Sprintf("%s/api/work/initiatives/%d/export-specs", baseURL, initiativeID)

	// Use authenticated GET to include the auth token
	resp, err := api.DoAuthenticatedGet(url, 60*time.Second)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data struct {
			InitiativeID   int    `json:"initiative_id"`
			InitiativeName string `json:"initiative_name"`
			ExportedAt     string `json:"exported_at"`
			Specs          map[string][]struct {
				ID           int         `json:"id"`
				Name         string      `json:"name"`
				Filename     string      `json:"filename"`
				ArtifactType string      `json:"artifact_type"`
				Category     string      `json:"category"`
				Status       string      `json:"status"`
				Content      interface{} `json:"content"` // Can be string or map
				InsertedAt   string      `json:"inserted_at"`
			} `json:"specs"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	// Save specs by category
	specCount := 0
	for category, specs := range result.Data.Specs {
		// Create category directory
		categoryDir := filepath.Join(specsDir, category)
		if err := os.MkdirAll(categoryDir, 0755); err != nil {
			return fmt.Errorf("failed to create category directory %s: %w", category, err)
		}

		for _, spec := range specs {
			// Get content - handle both string and map content
			var content string
			switch v := spec.Content.(type) {
			case string:
				content = v
			case map[string]interface{}:
				// If content is a map, try to get "text" or serialize to JSON
				if text, ok := v["text"].(string); ok {
					content = text
				} else {
					jsonBytes, _ := json.MarshalIndent(v, "", "  ")
					content = string(jsonBytes)
				}
			default:
				continue
			}

			if content == "" {
				continue
			}

			// Save the file
			filePath := filepath.Join(categoryDir, spec.Filename)
			if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
				printWarning("Failed to write %s: %v\n", filePath, err)
				continue
			}
			specCount++
		}
	}

	printInfo("Saved %d specification files to .modernpath/specs/\n", specCount)
	return nil
}

// pushDocs reads markdown from .modernpath/ (system exports + legacy docs/{slug}/) and uploads to the server.
// Each item may include export_path (relative to the system root) for subsystem/module resolution on import.
func pushDocs(cfg *config.Config) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("config is required")
	}

	configDir, err := config.GetConfigDir(true)
	if err != nil {
		return 0, fmt.Errorf("failed to get config directory: %w", err)
	}

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		return 0, fmt.Errorf(".modernpath directory not found: %s", configDir)
	}

	baseURL := cfg.APIURL
	systemID := cfg.SystemID
	systemSlug := strings.TrimSpace(cfg.SystemSlug)

	docs := []map[string]string{}

	manifest := loadMergedDocPushManifests(configDir)

	err = filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		relDir, relErr := filepath.Rel(configDir, path)
		if relErr != nil {
			return nil
		}
		relDirSlash := filepath.ToSlash(relDir)
		if info.IsDir() {
			if shouldSkipDocPushSubtree(relDirSlash) {
				return filepath.SkipDir
			}
			return nil
		}

		if filepath.Ext(info.Name()) != ".md" {
			return nil
		}

		if info.Name() == "MANIFEST.json" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			printWarning("Failed to read %s: %v\n", path, err)
			return nil
		}

		relNorm := filepath.ToSlash(relDir)
		lookupKey := docPushLookupKey(relNorm)

		if shouldSkipDocPush(lookupKey) {
			return nil
		}

		title := strings.TrimSuffix(info.Name(), ".md")

		var tier, angle string
		if te, ok := manifest[lookupKey]; ok {
			tier, angle = te.Tier, te.Angle
		} else {
			tier, angle = inferTierAngleFromExportPath(lookupKey)
		}

		// Extract title from frontmatter or first heading if present
		contentStr := string(content)
		if lines := strings.Split(contentStr, "\n"); len(lines) > 0 {
			for _, line := range lines {
				if strings.HasPrefix(line, "# ") {
					title = strings.TrimPrefix(line, "# ")
					title = strings.TrimSpace(title)
					break
				}
			}
		}

		// Extract summary from frontmatter or first paragraph
		summary := ""
		if idx := strings.Index(contentStr, "\n\n"); idx > 0 && idx < 500 {
			firstPara := strings.TrimSpace(contentStr[idx:])
			if nextIdx := strings.Index(firstPara, "\n\n"); nextIdx > 0 {
				summary = strings.TrimSpace(firstPara[:nextIdx])
			}
		}
		if len(summary) > 200 {
			summary = summary[:197] + "..."
		}

		docEntry := map[string]string{
			"title":   title,
			"tier":    tier,
			"angle":   angle,
			"content": contentStr,
			"summary": summary,
		}
		if relExport := exportPathForDocPush(lookupKey, systemSlug); relExport != "" {
			docEntry["export_path"] = relExport
		}

		docs = append(docs, docEntry)

		return nil
	})

	if err != nil {
		return 0, fmt.Errorf("failed to scan .modernpath directory: %w", err)
	}

	if len(docs) == 0 {
		return 0, nil
	}

	printInfo("Found %d doc files to push\n", len(docs))

	// Send to server in batches to avoid request body size limits
	const batchSize = 50
	url := fmt.Sprintf("%s/api/docs/import", baseURL)
	totalImported := 0

	for i := 0; i < len(docs); i += batchSize {
		end := i + batchSize
		if end > len(docs) {
			end = len(docs)
		}
		batch := docs[i:end]

		if len(docs) > batchSize {
			printInfo("  Pushing batch %d-%d of %d...\n", i+1, end, len(docs))
		}

		payload := map[string]interface{}{
			"system_id": systemID,
			"docs":            batch,
		}

		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			return totalImported, fmt.Errorf("failed to marshal payload: %w", err)
		}

		resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer(jsonPayload), 120*time.Second)
		if err != nil {
			return totalImported, fmt.Errorf("API request failed: %w", err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errorResp struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != "" {
				return totalImported, fmt.Errorf("server error: %s", errorResp.Error)
			}
			return totalImported, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
		}

		var result struct {
			Success bool `json:"success"`
			Data    struct {
				Imported int `json:"imported"`
				Updated  int `json:"updated"`
				Created  int `json:"created"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err == nil {
			totalImported += result.Data.Imported
			if result.Data.Updated > 0 || result.Data.Created > 0 {
				printInfo("  Updated: %d, Created: %d\n", result.Data.Updated, result.Data.Created)
			}
		} else {
			totalImported += len(batch)
		}
	}

	return totalImported, nil
}

// pushSpecs reads local specs from .modernpath/specs/ and uploads them to the server
func pushSpecs(baseURL string, initiativeID int) (int, error) {
	// Get config directory
	configDir, err := config.GetConfigDir(true)
	if err != nil {
		return 0, fmt.Errorf("failed to get config directory: %w", err)
	}

	// Specs directory is inside .modernpath
	specsDir := filepath.Join(configDir, "specs")

	// Check if specs directory exists
	if _, err := os.Stat(specsDir); os.IsNotExist(err) {
		return 0, fmt.Errorf("specs directory not found: %s", specsDir)
	}

	// Collect all spec files
	specs := []map[string]string{}

	// Walk through all category directories
	entries, err := os.ReadDir(specsDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read specs directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		category := entry.Name()
		categoryDir := filepath.Join(specsDir, category)

		// Read all files in category
		files, err := os.ReadDir(categoryDir)
		if err != nil {
			printWarning("Failed to read category %s: %v\n", category, err)
			continue
		}

		for _, file := range files {
			if file.IsDir() {
				continue
			}

			// Only process markdown files
			if filepath.Ext(file.Name()) != ".md" {
				continue
			}

			filePath := filepath.Join(categoryDir, file.Name())
			content, err := os.ReadFile(filePath)
			if err != nil {
				printWarning("Failed to read %s: %v\n", filePath, err)
				continue
			}

			specs = append(specs, map[string]string{
				"category": category,
				"filename": file.Name(),
				"content":  string(content),
			})
		}
	}

	if len(specs) == 0 {
		return 0, nil
	}

	printInfo("Found %d spec files to push\n", len(specs))

	// Send to server
	url := fmt.Sprintf("%s/api/work/initiatives/%d/import-specs", baseURL, initiativeID)

	payload := map[string]interface{}{
		"specs": specs,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Use authenticated POST to include the auth token
	resp, err := api.DoAuthenticatedPost(url, bytes.NewBuffer(jsonPayload), 60*time.Second)
	if err != nil {
		return 0, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil && errorResp.Error != "" {
			return 0, fmt.Errorf("server error: %s", errorResp.Error)
		}
		return 0, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var result struct {
		Data struct {
			Imported int `json:"imported"`
			Updated  int `json:"updated"`
			Created  int `json:"created"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return len(specs), nil // Return count even if response parsing fails
	}

	if result.Data.Updated > 0 || result.Data.Created > 0 {
		printInfo("  Updated: %d, Created: %d\n", result.Data.Updated, result.Data.Created)
	}

	return result.Data.Imported, nil
}

// Note: syncCmd removed from root - use 'docs sync' instead
// Helper functions (syncSpecs, pushSpecs, pushDocs) are still used by other commands
