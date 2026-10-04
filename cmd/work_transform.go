package cmd

import (
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/agents"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// autoSyncTransformFiles syncs source files for transformation epics
func autoSyncTransformFiles(cfg *config.Config, epicID int) error {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	requestURL := fmt.Sprintf("%s/api/transform/source-files?epic_id=%d", baseURL, epicID)

	resp, err := api.DoAuthenticatedGet(requestURL, 120*time.Second)
	if err != nil {
		return fmt.Errorf("failed to fetch source files: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	var filesResp TransformSourceFilesResponse
	if err := json.Unmarshal(body, &filesResp); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if !filesResp.Success {
		return fmt.Errorf("API error: %s", filesResp.Error)
	}

	d := filesResp.Data
	if d.TotalFiles == 0 {
		printInfo("No transform items configured for this epic.\n")
		return nil
	}

	sourceDir := ".modernpath/source"
	if err := createDirIfNotExists(sourceDir); err != nil {
		return err
	}

	syncedCount := 0
	for _, file := range d.Files {
		if file.ContentError != "" || file.Content == "" {
			continue
		}

		filePath := file.FilePath
		if filePath == "" {
			filePath = file.Name
		}

		fullPath := filepath.Join(sourceDir, filePath)
		parentDir := filepath.Dir(fullPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			continue
		}

		if err := os.WriteFile(fullPath, []byte(file.Content), 0644); err != nil {
			continue
		}

		syncedCount++
	}

	if syncedCount > 0 {
		printInfo("Synced %d source files\n", syncedCount)
		generateTransformAgentsMD(cfg, &filesResp)
	}

	return nil
}

func createDirIfNotExists(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.MkdirAll(path, 0755)
	}
	return nil
}

func generateTransformAgentsMD(cfg *config.Config, filesResp *TransformSourceFilesResponse) {
	d := filesResp.Data

	agentsCtx := &agents.TransformContext{
		EpicID:           d.EpicID,
		EpicName:         d.EpicName,
		SpecsRelPath:     config.ResolveEpicSpecsRelPath(cfg),
		SourceSystemID:   d.SourceArchitectureID,
		TargetSystemID:   d.TargetArchitectureID,
		SourceSystemName: fmt.Sprintf("Source System %d", d.SourceArchitectureID),
		TargetSystemName: fmt.Sprintf("Target System %d", d.TargetArchitectureID),
		SourceFiles:      make([]agents.SourceFile, 0, len(d.Files)),
	}

	for _, file := range d.Files {
		if file.ContentError == "" && file.Content != "" {
			agentsCtx.SourceFiles = append(agentsCtx.SourceFiles, agents.SourceFile{
				Name:       file.Name,
				FilePath:   file.FilePath,
				Language:   file.Language,
				TotalLines: file.TotalLines,
				Purpose:    file.Purpose,
				Summary:    file.Summary,
			})
		}
	}

	specsDir, err := config.ResolveEpicSpecsDir(cfg)
	if err == nil {
		if entries, readErr := os.ReadDir(specsDir); readErr == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					agentsCtx.SpecCategories = append(agentsCtx.SpecCategories, entry.Name())
				}
			}
		}
	}

	mpRoot := ".modernpath"
	seenRoot := map[string]bool{}
	var slugRoots []string
	addRoot := func(abs string) {
		if seenRoot[abs] {
			return
		}
		seenRoot[abs] = true
		slugRoots = append(slugRoots, abs)
	}
	if entries, err := os.ReadDir(mpRoot); err == nil {
		for _, e := range entries {
			if e.IsDir() && !modernpathReservedTopDir(e.Name()) {
				addRoot(filepath.Join(mpRoot, e.Name()))
			}
		}
	}
	if entries, err := os.ReadDir(filepath.Join(mpRoot, "docs")); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				addRoot(filepath.Join(mpRoot, "docs", e.Name()))
			}
		}
	}
	for _, root := range slugRoots {
		if entries, err := os.ReadDir(root); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if len(name) > 3 && strings.EqualFold(name[len(name)-3:], ".md") {
					agentsCtx.Docs = append(agentsCtx.Docs, agents.Document{
						Title: name[:len(name)-3],
						Tier:  "synced",
						Angle: "local",
					})
				}
			}
		}
	}

	agentsMDPath := "modernpath_agents.md"
	if err := agents.Generate(agentsCtx, agentsMDPath); err != nil {
		printWarning("Failed to generate modernpath_agents.md: %v\n", err)
	} else {
		printSuccess("Generated %s\n", agentsMDPath)
	}
}

type TransformSourceFilesResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		EpicID               int    `json:"epic_id"`
		EpicName             string `json:"epic_name"`
		SourceArchitectureID int    `json:"source_architecture_id"`
		TargetArchitectureID int    `json:"target_architecture_id"`
		Repository           *struct {
			Name      string `json:"name"`
			LocalPath string `json:"local_path"`
		} `json:"repository"`
		Files []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FilePath     string `json:"file_path"`
			Language     string `json:"language"`
			Content      string `json:"content"`
			ContentError string `json:"content_error,omitempty"`
			TotalLines   int    `json:"total_lines"`
			Purpose      string `json:"purpose,omitempty"`
			Summary      string `json:"summary,omitempty"`
			Complexity   string `json:"complexity,omitempty"`
		} `json:"files"`
		TotalFiles     int `json:"total_files"`
		TransformItems []struct {
			ID       int    `json:"id"`
			Name     string `json:"name"`
			Approach string `json:"approach,omitempty"`
			Priority string `json:"priority,omitempty"`
			Notes    string `json:"notes,omitempty"`
			Status   string `json:"status,omitempty"`
		} `json:"transform_items"`
		TransformItemCount int `json:"transform_item_count"`
	} `json:"data"`
}
