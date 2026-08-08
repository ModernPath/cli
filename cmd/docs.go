package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// authenticatedHTTPClient returns an http.Client configured with auth headers
// and a doRequest function that adds auth headers automatically
type authenticatedClient struct {
	client  *http.Client
	token   string
	baseURL string
}

func newAuthenticatedClient(cfg *config.Config) *authenticatedClient {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	token := ""
	auth, _ := config.ReadAuth()
	if auth != nil {
		token = auth.Token
	}

	return &authenticatedClient{
		client:  &http.Client{Timeout: 120 * time.Second},
		token:   token,
		baseURL: baseURL,
	}
}

func (c *authenticatedClient) Get(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.client.Do(req)
}

func (c *authenticatedClient) Post(url string, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest("POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.client.Do(req)
}

func (c *authenticatedClient) Delete(url string) (*http.Response, error) {
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.client.Do(req)
}

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "Manage codebase documentation and analysis",
	Long:  `Generate and sync codebase documentation from AI analysis.`,
}

var docsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync documentation from ModernPath platform",
	Long: `Download the latest documentation and analysis data
from the ModernPath platform.

This updates the local .modernpath directory with the latest documentation.
Note: Specifications are synced automatically when selecting an initiative
via 'modernpath work select'.`,
	RunE: runDocsSync,
}

var docsGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Run AI analysis to generate codebase documentation",
	Long: `Scan the codebase, get analysis estimates, and run the full AI analysis pipeline
to generate comprehensive documentation.

This performs the same analysis as the UI when creating new systems:
1. Scans the codebase for initial statistics
2. Shows estimates (cost, time, tokens)
3. Asks for confirmation
4. Creates repository if needed
5. Runs full AI analysis pipeline including documentation generation`,
	RunE: runDocsGenerate,
}

var docsRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Incrementally update documentation for changed files",
	Long: `Scan for file changes since last analysis and update only the affected documentation.

This is much faster than a full regeneration:
1. Detects changed files using content hashes
2. Re-analyzes only changed files
3. Updates module docs for affected modules
4. Updates subsystem docs for affected subsystems
5. Updates architecture docs if subsystems changed

Use this for daily documentation updates to keep docs in sync with code.`,
	RunE: runDocsRefresh,
}

var docsPreviewCmd = &cobra.Command{
	Use:   "preview",
	Short: "Preview what would be updated by docs refresh",
	Long:  `Show what files have changed and what documentation would be regenerated without making changes.`,
	RunE:  runDocsPreview,
}

var docsRepairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Re-analyze files with incomplete analysis",
	Long: `Repair files that have incomplete analysis data (missing summary, complexity, or key functions).

This is useful when:
- Initial analysis was interrupted or incomplete
- Some files were skipped during analysis
- You want to improve the quality of existing analysis

The command will:
1. Find files missing summary, complexity_assessment, or key_functions
2. Re-analyze them with full deep analysis
3. Update the database with complete analysis data

Use --dry-run to preview which files would be repaired.`,
	RunE: runDocsRepair,
}

var docsRepairLimit int
var docsRepairDryRun bool
var docsCleanupDryRun bool

var docsCleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove excluded files from analysis database",
	Long: `Remove file analysis records for generated/bundled files that shouldn't be analyzed.

This removes records for:
- priv/static/assets/* (Phoenix build artifacts)
- package-lock.json, yarn.lock (lock files)
- *.min.js, *.min.css (minified files)
- *.map (source maps)

These files inflate vibe debt scores and analysis metrics incorrectly.
Use --dry-run to preview what would be deleted.`,
	RunE: runDocsCleanup,
}

var docsPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push local documentation changes back to ModernPath",
	Long: `Upload documentation from .modernpath/ back to the ModernPath platform.

Exports place the system under .modernpath/<system-slug>/ (markdown in architecture/, README at slug root).
Legacy trees may still live under .modernpath/docs/<slug>/.

This allows you to:
- Edit documentation locally with AI tools
- Make manual improvements to generated docs
- Push your changes back to the server

Documents are matched by title and tier for updates. New documents are created
if they don't exist.

Example workflow:
1. modernpath docs sync           # Download current docs
2. Edit files under .modernpath/<slug>/ (or docs/<slug>/ if legacy)
3. modernpath docs push           # Upload changes`,
	RunE: runDocsPush,
}

func init() {
	rootCmd.AddCommand(docsCmd)
	docsCmd.AddCommand(docsSyncCmd)
	docsCmd.AddCommand(docsGenerateCmd)
	docsCmd.AddCommand(docsRefreshCmd)
	docsCmd.AddCommand(docsPreviewCmd)
	docsCmd.AddCommand(docsRepairCmd)
	docsCmd.AddCommand(docsCleanupCmd)
	docsCmd.AddCommand(docsPushCmd)

	docsRepairCmd.Flags().IntVar(&docsRepairLimit, "limit", 50, "Maximum files to analyze at once")
	docsRepairCmd.Flags().BoolVar(&docsRepairDryRun, "dry-run", false, "Preview which files would be repaired")
	docsCleanupCmd.Flags().BoolVar(&docsCleanupDryRun, "dry-run", false, "Preview what would be deleted")
}

func runDocsSync(cmd *cobra.Command, args []string) error {
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

	printInfo("Syncing documentation for %s from %s...\n", archName, cfg.APIURL)

	// Create API client (same as init command)
	client := api.NewClient(cfg.APIURL, "")

	// Health check
	if err := client.HealthCheck(); err != nil {
		printError("Cannot connect to ModernPath: %v\n", err)
		return err
	}

	// Download system export (documentation only, not specs)
	printInfo("Downloading system documentation...\n")

	zipData, err := downloadExportWithStatus(client, cfg.SystemID)
	if err != nil {
		printError("Failed to download: %v\n", err)
		return err
	}

	printSuccess("Downloaded %d bytes\n", len(zipData))

	// Extract (updates .modernpath export tree; does not touch specs)
	printInfo("Extracting documentation...\n")

	if err := extractZip(zipData); err != nil {
		printError("Failed to extract: %v\n", err)
		return err
	}

	// Update last sync time
	cfg.LastSyncAt = time.Now().Format(time.RFC3339)
	if err := config.WriteConfig(cfg); err != nil {
		printWarning("Failed to update config: %v\n", err)
	}

	printSuccess("Documentation sync complete!\n")
	return nil
}

func runDocsGenerate(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Get current directory name
	cwd, err := os.Getwd()
	if err != nil {
		printError("Failed to get current directory: %v\n", err)
		return err
	}
	folderName := filepath.Base(cwd)
	folderPath := cwd

	fmt.Println()
	printInfo("📊 Step 1: Scanning codebase for initial statistics...\n")
	fmt.Println()

	// Scan folder to get initial stats
	scanResult, err := scanFolder(folderPath)
	if err != nil {
		printError("Failed to scan folder: %v\n", err)
		return err
	}

	// Calculate estimates (similar to UI)
	tokensPerLine := 4.0 // Average tokens per line
	estimatedTokens := float64(scanResult.TotalLines) * tokensPerLine
	pricePer1kTokens := 0.15 // Approximate cost per 1k tokens
	estimatedCost := (estimatedTokens / 1000.0) * pricePer1kTokens
	estimatedMinutes := max(1, scanResult.TotalLines/10000) // ~1 min per 10k lines

	// Display analysis overview
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("📋 Analysis Overview")
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println()
	fmt.Printf("  📁 Folder:        %s\n", folderName)
	fmt.Printf("  📂 Path:          %s\n", folderPath)
	fmt.Println()
	fmt.Printf("  📊 Statistics:\n")
	fmt.Printf("     • Codebases:   %d\n", 1)
	fmt.Printf("     • Files:       %d\n", scanResult.TotalFiles)
	fmt.Printf("     • Lines:       %s\n", formatNumber(scanResult.TotalLines))
	if scanResult.PrimaryLanguage != "" {
		fmt.Printf("     • Language:    %s\n", scanResult.PrimaryLanguage)
	}
	fmt.Println()
	fmt.Printf("  💰 Estimates:\n")
	fmt.Printf("     • Est. Tokens: %s\n", formatTokenCount(int(estimatedTokens)))
	fmt.Printf("     • Est. Cost:   ~$%.2f\n", estimatedCost)
	fmt.Printf("     • Est. Time:   ~%d min\n", estimatedMinutes)
	fmt.Println()
	fmt.Println("  ℹ️  AI will extract architecture patterns, identify technologies,")
	fmt.Println("     map dependencies, and generate comprehensive documentation.")
	fmt.Println()

	// Ask for confirmation
	prompt := promptui.Prompt{
		Label:     "Start AI Analysis",
		IsConfirm: true,
		Default:   "y",
	}

	result, err := prompt.Run()
	if err != nil || strings.ToLower(result) != "y" {
		fmt.Println("Analysis cancelled.")
		return nil
	}

	fmt.Println()
	printInfo("🔍 Step 2: Checking for existing repository...\n")

	// Check if repository exists for this system and folder
	repo, err := findOrCreateRepository(cfg, folderName, folderPath, scanResult)
	if err != nil {
		printError("Failed to find/create repository: %v\n", err)
		return err
	}

	fmt.Printf("✓ Repository: %s (ID: %d)\n", repo.Name, repo.ID)
	fmt.Println()

	printInfo("🚀 Step 3: Starting AI analysis pipeline...\n")
	fmt.Println("   This may take a while. The analysis will:")
	fmt.Println("   • Extract architecture patterns")
	fmt.Println("   • Identify technologies and frameworks")
	fmt.Println("   • Map dependencies")
	fmt.Println("   • Generate comprehensive documentation")
	fmt.Println()

	// Start analysis
	if err := startAnalysis(cfg, repo.ID); err != nil {
		printError("Failed to start analysis: %v\n", err)
		return err
	}

	fmt.Println()
	printSuccess("✅ Analysis started!\n")
	fmt.Println()
	printInfo("The analysis is running in the background.\n")
	fmt.Printf("  • View progress in UI: %s/systems/%d\n", cfg.APIURL, cfg.SystemID)
	fmt.Println("  • Sync documentation when complete: modernpath docs sync")
	fmt.Println()

	return nil
}

type ScanResult struct {
	TotalFiles      int
	TotalLines      int
	PrimaryLanguage string
	Languages       []string
}

func scanFolder(path string) (*ScanResult, error) {
	// Simple Go-based file scanner
	var totalFiles int
	var totalLines int
	languageMap := make(map[string]int)

	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip common directories
		if info.IsDir() {
			base := filepath.Base(filePath)
			if shouldSkipDir(base) {
				return filepath.SkipDir
			}
			return nil
		}

		// Only count source files
		ext := strings.ToLower(filepath.Ext(filePath))
		if !isSourceFile(ext) {
			return nil
		}

		totalFiles++

		// Count lines
		file, err := os.Open(filePath)
		if err != nil {
			return nil // Skip files we can't read
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineCount := 0
		for scanner.Scan() {
			lineCount++
		}
		totalLines += lineCount

		// Track language
		lang := extToLanguage(ext)
		if lang != "" {
			languageMap[lang]++
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Determine primary language
	primaryLang := ""
	maxCount := 0
	for lang, count := range languageMap {
		if count > maxCount {
			maxCount = count
			primaryLang = lang
		}
	}

	languages := make([]string, 0, len(languageMap))
	for lang := range languageMap {
		languages = append(languages, lang)
	}

	return &ScanResult{
		TotalFiles:      totalFiles,
		TotalLines:      totalLines,
		PrimaryLanguage: primaryLang,
		Languages:       languages,
	}, nil
}

func shouldSkipDir(name string) bool {
	skipDirs := []string{
		".git", ".svn", ".hg",
		"node_modules", "deps", "_build", "vendor", "packages",
		"dist", "build", "target", "out", ".next", ".nuxt",
		".vscode", ".idea", ".vs",
		".tmp", ".cache", ".pytest_cache", "__pycache__", ".coverage",
		".DS_Store", "Thumbs.db",
		".modernpath", // Skip our own config directory
	}
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

func isSourceFile(ext string) bool {
	sourceExts := []string{
		".go", ".js", ".ts", ".jsx", ".tsx", ".py", ".java", ".rb", ".php",
		".ex", ".exs", ".rs", ".cpp", ".c", ".h", ".hpp", ".cs", ".swift",
		".kt", ".scala", ".clj", ".sh", ".bash", ".zsh", ".fish",
		".sql", ".html", ".css", ".scss", ".sass", ".less",
		".json", ".yaml", ".yml", ".toml", ".xml", ".md",
		".tf", ".hcl", ".dockerfile",
	}
	for _, sourceExt := range sourceExts {
		if ext == sourceExt {
			return true
		}
	}
	return false
}

func extToLanguage(ext string) string {
	langMap := map[string]string{
		".go": "go", ".js": "javascript", ".ts": "typescript", ".jsx": "javascript", ".tsx": "typescript",
		".py": "python", ".java": "java", ".rb": "ruby", ".php": "php",
		".ex": "elixir", ".exs": "elixir", ".rs": "rust", ".cpp": "cpp", ".c": "c", ".h": "c", ".hpp": "cpp",
		".cs": "csharp", ".swift": "swift", ".kt": "kotlin", ".scala": "scala", ".clj": "clojure",
		".sh": "shell", ".bash": "shell", ".zsh": "shell", ".fish": "shell",
		".sql": "sql", ".html": "html", ".css": "css", ".scss": "scss", ".sass": "sass", ".less": "less",
		".json": "json", ".yaml": "yaml", ".yml": "yaml", ".toml": "toml", ".xml": "xml", ".md": "markdown",
		".tf": "hcl", ".hcl": "hcl", ".dockerfile": "dockerfile",
	}
	return langMap[ext]
}

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

func formatNumber(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatTokenCount(n int) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

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

// IncrementalUpdateResponse represents the server response for update
type IncrementalUpdateResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Status  string `json:"status"`
		Message string `json:"message,omitempty"`
		Summary struct {
			FilesAdded          int  `json:"files_added"`
			FilesModified       int  `json:"files_modified"`
			FilesDeleted        int  `json:"files_deleted"`
			ModulesUpdated      int  `json:"modules_updated"`
			SubsystemsUpdated   int  `json:"subsystems_updated"`
			ArchitectureUpdated bool `json:"architecture_updated"`
		} `json:"summary,omitempty"`
		CompletedAt string `json:"completed_at,omitempty"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

func runDocsPreview(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Find repository ID for current directory
	repoID, err := findRepositoryForCurrentDir(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		printInfo("Make sure you've run 'modernpath docs generate' first.\n")
		return nil
	}

	fmt.Println()
	printInfo("🔍 Scanning for changes in repository...\n")
	fmt.Println()

	// Call preview API
	preview, err := getIncrementalPreview(cfg, repoID)
	if err != nil {
		printError("Failed to get preview: %v\n", err)
		return err
	}

	displayPreview(preview)
	return nil
}

func runDocsRefresh(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Find repository ID for current directory
	repoID, err := findRepositoryForCurrentDir(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		printInfo("Make sure you've run 'modernpath docs generate' first.\n")
		return nil
	}

	fmt.Println()
	printInfo("🔍 Step 1: Scanning for changes...\n")
	fmt.Println()

	// First get preview
	preview, err := getIncrementalPreview(cfg, repoID)
	if err != nil {
		printError("Failed to scan for changes: %v\n", err)
		return err
	}

	// Display what will be updated
	displayPreview(preview)

	if !preview.Data.HasChanges {
		printSuccess("✅ Documentation is up to date!\n")
		return nil
	}

	// Ask for confirmation
	fmt.Println()
	prompt := promptui.Prompt{
		Label:     "Proceed with incremental update",
		IsConfirm: true,
		Default:   "y",
	}

	result, err := prompt.Run()
	if err != nil || strings.ToLower(result) != "y" {
		fmt.Println("Update cancelled.")
		return nil
	}

	fmt.Println()
	printInfo("🚀 Step 2: Running incremental documentation update...\n")
	fmt.Println()

	// Run the update
	updateResult, err := runIncrementalUpdate(cfg, repoID)
	if err != nil {
		printError("Failed to run update: %v\n", err)
		return err
	}

	// Display results
	displayUpdateResult(updateResult)
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
	// 60 minute timeout for large updates (first run may need to process all files)
	client.client.Timeout = 3600 * time.Second

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/incremental-update",
		client.baseURL, cfg.SystemID, repoID)

	resp, err := client.Post(url, "application/json", bytes.NewBuffer([]byte("{}")))
	if err != nil {
		return nil, fmt.Errorf("failed to run update: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: %s - %s", resp.Status, string(body))
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
	d := result.Data

	if d.Status == "no_changes" {
		printSuccess("✅ %s\n", d.Message)
		return
	}

	fmt.Println()
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println("✅ Incremental Update Complete")
	fmt.Println("════════════════════════════════════════════════════════════════════")
	fmt.Println()

	fmt.Printf("📊 Summary:\n")
	fmt.Printf("   • Files added:         %d\n", d.Summary.FilesAdded)
	fmt.Printf("   • Files modified:      %d\n", d.Summary.FilesModified)
	fmt.Printf("   • Files deleted:       %d\n", d.Summary.FilesDeleted)
	fmt.Printf("   • Modules updated:     %d\n", d.Summary.ModulesUpdated)
	fmt.Printf("   • Subsystems updated:  %d\n", d.Summary.SubsystemsUpdated)
	fmt.Printf("   • Architecture updated: %v\n", d.Summary.ArchitectureUpdated)
	fmt.Println()

	printSuccess("Documentation has been updated!\n")
	printInfo("Run 'modernpath docs sync' to download the updated documentation.\n")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runDocsRepair(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	// Find repository ID for current folder
	repoID, err := findRepositoryForCurrentDir(cfg)
	if err != nil {
		printError("Failed to find repository: %v\n", err)
		printInfo("Make sure the current folder is associated with this system.\n")
		return err
	}

	fmt.Println()
	fmt.Println("🔧 Documentation Repair")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
	fmt.Printf("Repository ID: %d\n", repoID)
	fmt.Printf("Limit: %d files\n", docsRepairLimit)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "dry-run (preview only)", false: "repair"}[docsRepairDryRun])
	fmt.Println()

	// First, get count of incomplete files
	previewResp, err := getRepairPreview(cfg, repoID)
	if err != nil {
		printError("Failed to get repair preview: %v\n", err)
		return err
	}

	if previewResp.Data.IncompleteCount == 0 {
		printSuccess("✅ All files are fully analyzed! Nothing to repair.\n")
		return nil
	}

	fmt.Printf("📊 Found %d files with incomplete analysis (%.1f%% of %d total)\n",
		previewResp.Data.IncompleteCount,
		previewResp.Data.IncompletePercentage,
		previewResp.Data.TotalFiles)
	fmt.Println()

	if docsRepairDryRun {
		fmt.Println("📋 Files that would be repaired:")
		fmt.Println("─────────────────────────────────────────────────────────────")
		limit := docsRepairLimit
		if limit > len(previewResp.Data.Files) {
			limit = len(previewResp.Data.Files)
		}
		for i, f := range previewResp.Data.Files[:limit] {
			status := ""
			if !f.HasSummary {
				status += " [no summary]"
			}
			if !f.HasComplexity {
				status += " [no complexity]"
			}
			fmt.Printf("  %d. %s%s\n", i+1, f.RelativePath, status)
		}
		if len(previewResp.Data.Files) > limit {
			fmt.Printf("  ... and %d more files\n", len(previewResp.Data.Files)-limit)
		}
		fmt.Println()
		printInfo("Run without --dry-run to repair these files.\n")
		return nil
	}

	// Confirm before proceeding
	fmt.Printf("→ This will re-analyze up to %d files. Continue? [y/N] ", docsRepairLimit)
	var confirm string
	fmt.Scanln(&confirm)
	if confirm != "y" && confirm != "Y" {
		printInfo("Cancelled.\n")
		return nil
	}

	fmt.Println()
	fmt.Println("→ Starting repair analysis...")
	fmt.Println("  (This runs in the background - check server logs for progress)")
	fmt.Println()

	// Trigger repair
	err = triggerRepair(cfg, repoID)
	if err != nil {
		printError("Failed to start repair: %v\n", err)
		return err
	}

	printSuccess("✅ Repair started! Files are being analyzed in the background.\n")
	printInfo("Run 'modernpath scan' after completion to see updated scores.\n")

	return nil
}

type RepairPreviewResponse struct {
	Success bool `json:"success"`
	Data    struct {
		IncompleteCount      int     `json:"incomplete_count"`
		TotalFiles           int     `json:"total_files"`
		IncompletePercentage float64 `json:"incomplete_percentage"`
		Files                []struct {
			ID            int    `json:"id"`
			RelativePath  string `json:"relative_path"`
			Language      string `json:"language"`
			HasSummary    bool   `json:"has_summary"`
			HasComplexity bool   `json:"has_complexity"`
			LinesOfCode   int    `json:"lines_of_code"`
		} `json:"files"`
	} `json:"data"`
}

func getRepairPreview(cfg *config.Config, repoID int) (*RepairPreviewResponse, error) {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/incomplete-files",
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

	var preview RepairPreviewResponse
	if err := json.Unmarshal(body, &preview); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &preview, nil
}

func triggerRepair(cfg *config.Config, repoID int) error {
	client := newAuthenticatedClient(cfg)

	url := fmt.Sprintf("%s/api/systems/%d/repositories/%d/repair-analysis",
		client.baseURL, cfg.SystemID, repoID)

	reqBody := fmt.Sprintf(`{"limit": %d}`, docsRepairLimit)

	resp, err := client.Post(url, "application/json", bytes.NewBufferString(reqBody))
	if err != nil {
		return fmt.Errorf("failed to trigger repair: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error: %s - %s", resp.Status, string(body))
	}

	return nil
}

func runDocsCleanup(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
	}

	fmt.Println()
	fmt.Println("🧹 Documentation Cleanup")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Printf("System: %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
	fmt.Printf("Mode: %s\n", map[bool]string{true: "dry-run (preview only)", false: "delete"}[docsCleanupDryRun])
	fmt.Println()

	// Call cleanup endpoint
	client := newAuthenticatedClient(cfg)
	client.client.Timeout = 60 * time.Second

	url := fmt.Sprintf("%s/api/systems/%d/cleanup-excluded?dry_run=%v",
		client.baseURL, cfg.SystemID, docsCleanupDryRun)

	resp, err := client.Delete(url)
	if err != nil {
		printError("Failed to call cleanup API: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		printError("API error: %s - %s\n", resp.Status, string(body))
		return fmt.Errorf("cleanup failed")
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			Mode          string   `json:"mode"`
			FilesToDelete int      `json:"files_to_delete"`
			TotalLines    int      `json:"total_lines"`
			SampleFiles   []string `json:"sample_files"`
			DeletedCount  int      `json:"deleted_count"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	if docsCleanupDryRun {
		if result.Data.FilesToDelete == 0 {
			printSuccess("✅ No excluded files found in database!\n")
			return nil
		}

		fmt.Printf("📊 Found %d excluded files (%.0f KB of code)\n",
			result.Data.FilesToDelete,
			float64(result.Data.TotalLines)*30/1024) // rough estimate
		fmt.Println()
		fmt.Println("📋 Sample files that would be deleted:")
		fmt.Println("─────────────────────────────────────────────────────────────")
		for i, f := range result.Data.SampleFiles {
			if i >= 15 {
				fmt.Printf("  ... and %d more files\n", result.Data.FilesToDelete-15)
				break
			}
			fmt.Printf("  %d. %s\n", i+1, f)
		}
		fmt.Println()
		printInfo("Run without --dry-run to delete these files.\n")
	} else {
		if result.Data.DeletedCount == 0 {
			printSuccess("✅ No excluded files to delete!\n")
		} else {
			printSuccess("✅ Deleted %d excluded file records from database.\n", result.Data.DeletedCount)
			printInfo("Run 'modernpath scan' to see updated scores.\n")
		}
	}

	return nil
}

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
	printInfo("View updated docs at: %s/systems/%d/docs\n", cfg.APIURL, cfg.SystemID)

	return nil
}
