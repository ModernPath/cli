package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"io"
	"net/http"
	"os"
)

type Repository struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	LocalPath string `json:"local_path"`
}

func findOrCreateRepository(cfg *config.Config, folderName, folderPath string, scanResult *ScanResult) (*Repository, error) {
	client := newAuthenticatedClient(cfg)

	// First, try to find existing repository by checking system's repositories
	url := fmt.Sprintf("%s/api/systems/%d", client.baseURL, cfg.SystemID)
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get system: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var sysResult struct {
		Repositories []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			LocalPath string `json:"local_path"`
		} `json:"repositories"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sysResult); err != nil {
		return nil, fmt.Errorf("failed to parse system: %w", err)
	}

	// Check if repository with this path already exists
	for _, repo := range sysResult.Repositories {
		if repo.LocalPath == folderPath || repo.Name == folderName {
			return &Repository{
				ID:        repo.ID,
				Name:      repo.Name,
				LocalPath: repo.LocalPath,
			}, nil
		}
	}

	// Create new repository
	printInfo("Creating new repository...\n")
	createURL := fmt.Sprintf("%s/api/systems/%d/repositories", client.baseURL, cfg.SystemID)

	payload := map[string]interface{}{
		"name":             folderName,
		"local_path":       folderPath,
		"total_files":      scanResult.TotalFiles,
		"total_lines":      scanResult.TotalLines,
		"primary_language": scanResult.PrimaryLanguage,
	}

	jsonPayload, _ := json.Marshal(payload)
	resp, err = client.Post(createURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API error creating repository: %s - %s", resp.Status, string(body))
	}

	var createResult struct {
		Data Repository `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&createResult); err != nil {
		return nil, fmt.Errorf("failed to parse repository: %w", err)
	}

	return &createResult.Data, nil
}

func startAnalysis(cfg *config.Config, repoID int) error {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/analyze", client.baseURL, cfg.SystemID, repoID)

	resp, err := client.Post(url, "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to start analysis: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	return nil
}

func findRepositoryForCurrentDir(cfg *config.Config) (int, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}
	folderPath := cwd

	client := newAuthenticatedClient(cfg)

	// Get system's repositories
	url := fmt.Sprintf("%s/api/systems/%d", client.baseURL, cfg.SystemID)
	resp, err := client.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to get system: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var sysResult struct {
		Repositories []struct {
			ID        int    `json:"id"`
			Name      string `json:"name"`
			LocalPath string `json:"local_path"`
		} `json:"repositories"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&sysResult); err != nil {
		return 0, fmt.Errorf("failed to parse system: %w", err)
	}

	// Find repository matching current path
	for _, repo := range sysResult.Repositories {
		if repo.LocalPath == folderPath {
			return repo.ID, nil
		}
	}

	return 0, fmt.Errorf("no repository found for path: %s", folderPath)
}
