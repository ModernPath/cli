package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/modernpath/cli/internal/config"
)

// resolveBoundRepository is the repository the docs verbs and `analysis
// status` act on (REQ-CROSS-503 C2): the repository_id `import --local`
// recorded in .modernpath/config.json, else the system's single upload
// repository (one with no URL), else the repository whose local path is the
// working directory. An upload repository's local path is a sealed handle the
// server does not project, so a path match alone never finds an imported
// system.
func resolveBoundRepository(cfg *config.Config) (int, error) {
	if cfg.RepositoryID > 0 {
		return cfg.RepositoryID, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return 0, err
	}

	client := newAuthenticatedClient(cfg)
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
			ID            int    `json:"id"`
			LocalPath     string `json:"local_path"`
			RepositoryURL string `json:"repository_url"`
		} `json:"repositories"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&sysResult); err != nil {
		return 0, fmt.Errorf("failed to parse system: %w", err)
	}

	var uploads []int
	for _, repo := range sysResult.Repositories {
		if repo.RepositoryURL == "" {
			uploads = append(uploads, repo.ID)
		}
	}
	if len(uploads) == 1 {
		return uploads[0], nil
	}
	for _, repo := range sysResult.Repositories {
		if repo.LocalPath == cwd {
			return repo.ID, nil
		}
	}
	if len(sysResult.Repositories) == 0 {
		return 0, noRepositoryError(cfg.SystemID)
	}
	if len(uploads) == 0 {
		return 0, fmt.Errorf("cannot tell which repository of system %d to use: its %d repositories are linked to a git provider and none has the local path %s. The platform refreshes and analyses them itself; see them in the web app",
			cfg.SystemID, len(sysResult.Repositories), cwd)
	}
	return 0, fmt.Errorf("cannot tell which repository of system %d to use: it has %d upload repositories and none has the local path %s. Run the command in the checkout where 'modernpath import' created the repository: its binding names it",
		cfg.SystemID, len(uploads), cwd)
}

// noRepositoryError refuses a System with no repository at all, such as one
// made in the web app. The CLI's only upload of a local tree is import, which
// makes a new System; no command adds a repository to an existing one.
func noRepositoryError(systemID int) error {
	return fmt.Errorf("system %d has no repository. To upload this directory, run 'modernpath import' in this directory: it creates a new System with this directory as its repository", systemID)
}
