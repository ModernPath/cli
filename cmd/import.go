package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	importGit       bool
	importLocal     bool
	importName      string
	importMaxSizeMB int
)

const (
	defaultMaxSizeMB = 100 // 100MB default max upload size
)

var importCmd = &cobra.Command{
	Use:   "import",
	Short: "Import an existing codebase into ModernPath",
	Long: `Import an existing codebase to create a new ModernPath system.

This command detects if the current directory is a git repository and offers
appropriate import options:

If git remote exists:
  1. Import via Git URL - ModernPath clones and analyzes the repository
  2. Import local files - Zip and upload the current directory

If no git remote:
  - Import local files only

Examples:
  modernpath import                    # Interactive - detects git and prompts
  modernpath import --git              # Force import via git URL
  modernpath import --local            # Force import local files
  modernpath import --name="My App"    # Specify system name`,
	RunE: runImport,
}

func init() {
	importCmd.Flags().BoolVar(&importGit, "git", false, "Import via git URL (requires git remote)")
	importCmd.Flags().BoolVar(&importLocal, "local", false, "Import by uploading local files")
	importCmd.Flags().StringVar(&importName, "name", "", "System name (default: folder name)")
	importCmd.Flags().IntVar(&importMaxSizeMB, "max-size", defaultMaxSizeMB, "Maximum upload size in MB")

	rootCmd.AddCommand(importCmd)
}

func runImport(cmd *cobra.Command, args []string) error {
	bold := color.New(color.Bold)
	cyan := color.New(color.FgCyan)

	fmt.Println()
	bold.Println("📦 ModernPath Import")
	fmt.Println("═══════════════════════════════════════════════════════════")
	fmt.Println()

	// Get current directory info
	cwd, err := os.Getwd()
	if err != nil {
		printError("Failed to get current directory: %v\n", err)
		return err
	}
	folderName := filepath.Base(cwd)

	// Detect git remote
	gitRemote := detectGitRemote()
	hasGitRemote := gitRemote != ""

	fmt.Printf("📁 Directory: %s\n", cwd)
	if hasGitRemote {
		cyan.Printf("🔗 Git Remote: %s\n", gitRemote)
	} else {
		fmt.Println("🔗 Git Remote: (none detected)")
	}
	fmt.Println()

	// Get API URL and check connection
	baseURL := getAPIURL()
	client := &http.Client{Timeout: 10 * time.Second}
	
	// Check auth
	auth, _ := config.ReadAuth()
	if auth == nil || auth.Token == "" {
		printError("Not authenticated. Run 'modernpath auth' first.\n")
		return fmt.Errorf("authentication required")
	}

	// Health check
	resp, err := client.Get(baseURL + "/_health")
	if err != nil {
		printError("Cannot connect to ModernPath at %s\n", baseURL)
		return err
	}
	resp.Body.Close()

	// Determine import method
	var importMethod string
	if importGit {
		if !hasGitRemote {
			printError("No git remote found. Cannot use --git flag.\n")
			return fmt.Errorf("no git remote")
		}
		importMethod = "git"
	} else if importLocal {
		importMethod = "local"
	} else {
		// Interactive selection
		importMethod, err = selectImportMethod(hasGitRemote)
		if err != nil {
			return err
		}
	}

	if importMethod == "cancel" {
		printInfo("Import cancelled.\n")
		return nil
	}

	// Get system name
	archName := importName
	if archName == "" {
		archName = folderName
	}

	// Confirm before proceeding
	fmt.Println()
	bold.Println("📋 Import Summary")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Printf("Name:     %s\n", archName)
	fmt.Printf("Method:   %s\n", importMethod)
	if importMethod == "git" {
		fmt.Printf("Git URL:  %s\n", gitRemote)
	} else {
		fmt.Printf("Source:   %s\n", cwd)
	}
	fmt.Printf("API:      %s\n", baseURL)
	fmt.Println()

	confirmPrompt := promptui.Prompt{
		Label:     "Proceed with import",
		IsConfirm: true,
	}
	_, err = confirmPrompt.Run()
	if err != nil {
		printInfo("Import cancelled.\n")
		return nil
	}

	// Execute import
	fmt.Println()
	if importMethod == "git" {
		return importViaGit(baseURL, auth.Token, archName, gitRemote)
	}
	return importViaUpload(baseURL, auth.Token, archName, cwd)
}

func detectGitRemote() string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func getAPIURL() string {
	if apiURL != "" {
		return apiURL
	}
	cfg, _ := config.ReadConfig()
	if cfg != nil && cfg.APIURL != "" {
		return cfg.APIURL
	}
	return config.DefaultAPIURL
}

func selectImportMethod(hasGitRemote bool) (string, error) {
	var items []string
	
	if hasGitRemote {
		items = []string{
			"🌐 Import via Git URL (recommended) - ModernPath clones the repository",
			"📁 Import local files - Upload current directory as zip",
			"❌ Cancel",
		}
	} else {
		items = []string{
			"📁 Import local files - Upload current directory as zip",
			"❌ Cancel",
		}
	}

	prompt := promptui.Select{
		Label:    "How would you like to import this codebase?",
		Items:    items,
		HideHelp: true,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return "", err
	}

	if hasGitRemote {
		switch index {
		case 0:
			return "git", nil
		case 1:
			return "local", nil
		default:
			return "cancel", nil
		}
	}
	
	switch index {
	case 0:
		return "local", nil
	default:
		return "cancel", nil
	}
}

func importViaGit(baseURL, token, name, gitURL string) error {
	printInfo("Creating system from git repository...\n")

	payload := map[string]interface{}{
		"name":        name,
		"import_type": "git",
		"git_url":     gitURL,
	}

	jsonPayload, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", baseURL+"/api/systems/import", bytes.NewBuffer(jsonPayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		printError("Failed to create system: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		printError("API error: %s - %s\n", resp.Status, string(body))
		return fmt.Errorf("import failed")
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	return handleImportSuccess(baseURL, result.Data.ID, result.Data.Name, result.Data.Slug)
}

func importViaUpload(baseURL, token, name, sourceDir string) error {
	printInfo("Scanning directory...\n")

	// Calculate size first
	var totalSize int64
	var fileCount int
	
	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		// Skip directories we don't want
		if info.IsDir() {
			base := filepath.Base(path)
			if shouldSkipImportDir(base) {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Skip files we don't want
		if shouldSkipImportFile(info.Name()) {
			return nil
		}
		
		totalSize += info.Size()
		fileCount++
		return nil
	})

	if err != nil {
		printError("Failed to scan directory: %v\n", err)
		return err
	}

	sizeMB := float64(totalSize) / (1024 * 1024)
	fmt.Printf("  Files: %d\n", fileCount)
	fmt.Printf("  Size:  %.2f MB\n", sizeMB)

	if sizeMB > float64(importMaxSizeMB) {
		printError("Directory too large (%.2f MB > %d MB limit)\n", sizeMB, importMaxSizeMB)
		printInfo("Consider using git import instead, or increase limit with --max-size\n")
		return fmt.Errorf("directory too large")
	}

	printInfo("Creating zip archive...\n")

	// Create zip in memory
	var zipBuffer bytes.Buffer
	zipWriter := zip.NewWriter(&zipBuffer)

	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip directories we don't want
		if info.IsDir() {
			base := filepath.Base(path)
			if shouldSkipImportDir(base) {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip files we don't want
		if shouldSkipImportFile(info.Name()) {
			return nil
		}

		relPath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return nil
		}

		// Create file in zip
		writer, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return nil // Skip files we can't read
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})

	if err != nil {
		printError("Failed to create zip: %v\n", err)
		return err
	}

	zipWriter.Close()
	zipSize := zipBuffer.Len()
	fmt.Printf("  Zip size: %.2f MB\n", float64(zipSize)/(1024*1024))

	printInfo("Uploading to ModernPath...\n")

	// Create multipart request
	var requestBody bytes.Buffer
	mpWriter := multipart.NewWriter(&requestBody)

	// Add name field
	mpWriter.WriteField("name", name)
	mpWriter.WriteField("import_type", "upload")

	// Add zip file
	part, err := mpWriter.CreateFormFile("file", name+".zip")
	if err != nil {
		printError("Failed to create form: %v\n", err)
		return err
	}
	part.Write(zipBuffer.Bytes())
	mpWriter.Close()

	req, _ := http.NewRequest("POST", baseURL+"/api/systems/import", &requestBody)
	req.Header.Set("Content-Type", mpWriter.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	// Longer timeout for upload
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		printError("Upload failed: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		printError("API error: %s - %s\n", resp.Status, string(body))
		return fmt.Errorf("import failed")
	}

	var result struct {
		Success bool `json:"success"`
		Data    struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
			Slug string `json:"slug"`
		} `json:"data"`
		Message string `json:"message"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	return handleImportSuccess(baseURL, result.Data.ID, result.Data.Name, result.Data.Slug)
}

func handleImportSuccess(baseURL string, archID int, archName, archSlug string) error {
	fmt.Println()
	printSuccess("System created successfully!\n")
	fmt.Printf("  ID:   %d\n", archID)
	fmt.Printf("  Name: %s\n", archName)
	fmt.Printf("  Slug: %s\n", archSlug)

	// Save config
	cwd, _ := os.Getwd()
	modernpathDir := filepath.Join(cwd, ".modernpath")
	
	if err := os.MkdirAll(modernpathDir, 0755); err == nil {
		cfg := &config.Config{
			APIURL:           getAPIURL(),
			SystemID:   archID,
			SystemName: archName,
			SystemSlug: archSlug,
		}

		configPath := filepath.Join(modernpathDir, "config.json")
		configData, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(configPath, configData, 0644); err == nil {
			printSuccess("Config saved to .modernpath/config.json\n")
		}

		// Create .gitignore
		gitignorePath := filepath.Join(modernpathDir, ".gitignore")
		gitignoreContent := "# ModernPath - ignore sensitive files\nauth.json\n*.log\n"
		os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644)
	}

	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. View in UI:          %s/systems/%d\n", baseURL, archID)
	fmt.Println("  2. Generate docs:       modernpath docs generate")
	fmt.Println("  3. Sync documentation:  modernpath docs sync")
	fmt.Println("  4. Search codebase:     modernpath search \"...\"")
	fmt.Println()

	return nil
}

func shouldSkipImportDir(name string) bool {
	skipDirs := []string{
		".git", ".svn", ".hg", ".modernpath",
		"node_modules", "deps", "_build", "vendor", "packages",
		"dist", "build", "target", "out", ".next", ".nuxt",
		"bin", "obj", "TestResults",
		".vscode", ".idea", ".vs",
		".tmp", ".cache", ".pytest_cache", "__pycache__", ".coverage",
		".DS_Store", "Thumbs.db",
	}
	for _, skip := range skipDirs {
		if name == skip {
			return true
		}
	}
	return false
}

func shouldSkipImportFile(name string) bool {
	// Skip large binary files and common non-code files
	skipExtensions := []string{
		".exe", ".dll", ".so", ".dylib", ".a", ".o",
		".zip", ".tar", ".gz", ".rar", ".7z",
		".pdf", ".doc", ".docx", ".xls", ".xlsx",
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".svg",
		".mp3", ".mp4", ".avi", ".mov", ".wav",
		".ttf", ".otf", ".woff", ".woff2", ".eot",
		".sqlite", ".db",
	}
	
	ext := strings.ToLower(filepath.Ext(name))
	for _, skip := range skipExtensions {
		if ext == skip {
			return true
		}
	}
	
	// Skip specific files
	skipFiles := []string{
		".DS_Store", "Thumbs.db", ".env", ".env.local",
		"package-lock.json", "yarn.lock", "pnpm-lock.yaml",
	}
	for _, skip := range skipFiles {
		if name == skip {
			return true
		}
	}
	
	return false
}
