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
)

// pushDocs reads markdown from .modernpath/ (system exports + legacy docs/{slug}/) and uploads to the server.
// Each item may include export_path (relative to the system root) for subsystem/module resolution on import.
func pushDocs(cfg *config.Config) (int, error) {
	if cfg == nil {
		return 0, fmt.Errorf("config is required")
	}

	configDir, err := config.WorkspaceConfigDir()
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

		// The server's import_docs clause matches "architecture_id" — its
		// current shape is canonical (USER:2026-08-18, REQ-CROSS-207).
		payload := map[string]interface{}{
			"architecture_id": systemID,
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
			Data    *struct {
				Imported *int `json:"imported"`
				Updated  int  `json:"updated"`
				Created  int  `json:"created"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &result); err != nil {
			return totalImported, fmt.Errorf("invalid docs import response: %w", err)
		}
		if !result.Success || result.Data == nil || result.Data.Imported == nil {
			return totalImported, fmt.Errorf("invalid docs import response: expected success and data.imported")
		}
		totalImported += *result.Data.Imported
		if result.Data.Updated > 0 || result.Data.Created > 0 {
			printInfo("  Updated: %d, Created: %d\n", result.Data.Updated, result.Data.Created)
		}
	}

	return totalImported, nil
}
