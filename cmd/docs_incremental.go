package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"io"
	"net/http"
)

// IncrementalPreviewResponse represents the server response for preview
type IncrementalPreviewResponse struct {
	Success bool `json:"success"`
	Data    struct {
		RepositoryID   int    `json:"repository_id"`
		RepositoryName string `json:"repository_name"`
		ScannedAt      string `json:"scanned_at"`
		HasChanges     bool   `json:"has_changes"`
		FileChanges    struct {
			Added     int `json:"added"`
			Modified  int `json:"modified"`
			Deleted   int `json:"deleted"`
			Unchanged int `json:"unchanged"`
			Total     int `json:"total"`
		} `json:"file_changes"`
		AddedFiles           []string `json:"added_files"`
		ModifiedFiles        []string `json:"modified_files"`
		DeletedFiles         []string `json:"deleted_files"`
		DocumentationUpdates struct {
			Modules            []string `json:"modules"`
			ModuleCount        int      `json:"module_count"`
			Subsystems         []string `json:"subsystems"`
			SubsystemCount     int      `json:"subsystem_count"`
			UpdateArchitecture bool     `json:"update_architecture"`
		} `json:"documentation_updates"`
		EstimatedAPICalls struct {
			FileAnalysis     int `json:"file_analysis"`
			ModuleDocs       int `json:"module_docs"`
			SubsystemDocs    int `json:"subsystem_docs"`
			ArchitectureDocs int `json:"architecture_docs"`
			Total            int `json:"total"`
		} `json:"estimated_api_calls"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

// IncrementalUpdateResponse is the 202 the incremental-update route answers:
// the update is an incremental run of the system whose one step runs in the
// background (REQ-SYS-181).
type IncrementalUpdateResponse struct {
	Message      string `json:"message"`
	RepositoryID string `json:"repository_id"`
	RunID        int    `json:"run_id"`
	JobID        int    `json:"job_id"`
}

func runDocsPreview(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return reportedError{err}
	}

	if cfg.SystemID == 0 {
		return reportFailure(errNoBoundSystem)
	}

	repoID, err := resolveBoundRepository(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		return reportedError{err}
	}

	fmt.Println()
	printInfo("🔍 Scanning for changes in repository...\n")
	fmt.Println()

	// Call preview API
	preview, err := getIncrementalPreview(cfg, repoID)
	if err != nil {
		printError("Failed to get preview: %v\n", err)
		return reportedError{err}
	}

	displayPreview(preview)
	return nil
}

func runDocsRefresh(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return reportedError{err}
	}

	if cfg.SystemID == 0 {
		return reportFailure(errNoBoundSystem)
	}

	repoID, err := resolveBoundRepository(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		return reportedError{err}
	}

	fmt.Println()
	printInfo("🔍 Step 1: Scanning for changes...\n")
	fmt.Println()

	// First get preview
	preview, err := getIncrementalPreview(cfg, repoID)
	if err != nil {
		printError("Failed to scan for changes: %v\n", err)
		return reportedError{err}
	}

	// Display what will be updated
	displayPreview(preview)

	if !preview.Data.HasChanges {
		printSuccess("✅ Documentation is up to date!\n")
		return nil
	}

	// Ask for confirmation
	fmt.Println()
	ok, err := confirmAction("Proceed with incremental update", docsRefreshYes, true)
	if err != nil {
		return reportFailure(err)
	}
	if !ok {
		fmt.Println("Update cancelled.")
		return nil
	}

	fmt.Println()
	printInfo("🚀 Step 2: Queueing the incremental documentation update...\n")
	fmt.Println()

	queued, err := runIncrementalUpdate(cfg, repoID)
	if err != nil {
		return reportFailure(fmt.Errorf("failed to queue the update: %w", err))
	}

	displayUpdateResult(queued)
	return nil
}

func getIncrementalPreview(cfg *config.Config, repoID int) (*IncrementalPreviewResponse, error) {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/incremental-preview",
		client.baseURL, cfg.SystemID, repoID)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to get preview: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	var preview IncrementalPreviewResponse
	if err := json.Unmarshal(body, &preview); err != nil {
		return nil, fmt.Errorf("failed to parse preview: %w", err)
	}

	return &preview, nil
}

func runIncrementalUpdate(cfg *config.Config, repoID int) (*IncrementalUpdateResponse, error) {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/incremental-update",
		client.baseURL, cfg.SystemID, repoID)

	resp, err := client.Post(url, "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusAccepted {
		return nil, pushRefusal(resp, body)
	}

	var result IncrementalUpdateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return &result, nil
}

func displayPreview(preview *IncrementalPreviewResponse) {
	d := preview.Data

	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("📋 Change Detection Report")
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println()

	// File changes
	fmt.Printf("📁 File Changes:\n")
	fmt.Printf("   • Added:     %d files\n", d.FileChanges.Added)
	fmt.Printf("   • Modified:  %d files\n", d.FileChanges.Modified)
	fmt.Printf("   • Deleted:   %d files\n", d.FileChanges.Deleted)
	fmt.Printf("   • Unchanged: %d files\n", d.FileChanges.Unchanged)
	fmt.Printf("   • Total:     %d files\n", d.FileChanges.Total)
	fmt.Println()

	// Show changed files if any
	if len(d.AddedFiles) > 0 {
		fmt.Printf("   ➕ Added files:\n")
		for _, f := range d.AddedFiles[:min(5, len(d.AddedFiles))] {
			fmt.Printf("      • %s\n", f)
		}
		if len(d.AddedFiles) > 5 {
			fmt.Printf("      ... and %d more\n", len(d.AddedFiles)-5)
		}
		fmt.Println()
	}

	if len(d.ModifiedFiles) > 0 {
		fmt.Printf("   📝 Modified files:\n")
		for _, f := range d.ModifiedFiles[:min(5, len(d.ModifiedFiles))] {
			fmt.Printf("      • %s\n", f)
		}
		if len(d.ModifiedFiles) > 5 {
			fmt.Printf("      ... and %d more\n", len(d.ModifiedFiles)-5)
		}
		fmt.Println()
	}

	if len(d.DeletedFiles) > 0 {
		fmt.Printf("   🗑️  Deleted files:\n")
		for _, f := range d.DeletedFiles[:min(5, len(d.DeletedFiles))] {
			fmt.Printf("      • %s\n", f)
		}
		if len(d.DeletedFiles) > 5 {
			fmt.Printf("      ... and %d more\n", len(d.DeletedFiles)-5)
		}
		fmt.Println()
	}

	// Documentation updates needed
	fmt.Printf("📚 Documentation Updates Needed:\n")
	fmt.Printf("   • Modules:     %d\n", d.DocumentationUpdates.ModuleCount)
	fmt.Printf("   • Subsystems:  %d\n", d.DocumentationUpdates.SubsystemCount)
	fmt.Printf("   • Architecture: %v\n", d.DocumentationUpdates.UpdateArchitecture)
	fmt.Println()

	// Estimated effort
	fmt.Printf("⏱️  Estimated API Calls:\n")
	fmt.Printf("   • File analysis:     %d\n", d.EstimatedAPICalls.FileAnalysis)
	fmt.Printf("   • Module docs:       %d\n", d.EstimatedAPICalls.ModuleDocs)
	fmt.Printf("   • Subsystem docs:    %d\n", d.EstimatedAPICalls.SubsystemDocs)
	fmt.Printf("   • Architecture docs: %d\n", d.EstimatedAPICalls.ArchitectureDocs)
	fmt.Printf("   • Total:             %d\n", d.EstimatedAPICalls.Total)
	fmt.Println()
}

func displayUpdateResult(result *IncrementalUpdateResponse) {
	printSuccess("Incremental update queued (run %d, job %d).\n", result.RunID, result.JobID)
	printInfo("It runs in the background. Follow it with 'modernpath analysis status';\n")
	printInfo("when documentation reads completed, run 'modernpath docs sync'.\n")
}
