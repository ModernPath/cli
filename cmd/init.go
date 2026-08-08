package cmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

type initFinalizeOptions struct {
	force           bool
	initMode        string
	localRepos      []localGitRepo
	platformMembers []api.WorkspaceMember
}

var (
	systemIDFlag   int
	systemNameFlag string
	force          bool
	initLocal      bool
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize ModernPath in the current directory",
	Long: `Initialize a ModernPath project in the current directory.

This command will:
1. Connect to the ModernPath platform (beta.modernpath.ai by default)
2. Let you select a system
3. Download the documentation and analysis data
4. Create a .modernpath directory with all the data

Example:
  modernpath init                    # Connect to beta.modernpath.ai
  modernpath init --local            # Connect to localhost:4000
  modernpath init --system-id=3      # Specify system ID
  modernpath init --system="my-app"  # Specify system by name`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().IntVar(&systemIDFlag, "system-id", 0, "System ID to initialize")
	initCmd.Flags().StringVar(&systemNameFlag, "system", "", "System name to initialize")
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing .modernpath directory")
	initCmd.Flags().BoolVar(&initLocal, "local", false, "Use local server (localhost:4000) instead of beta.modernpath.ai")
}

func runInit(cmd *cobra.Command, args []string) error {
	if config.IsInitialized() && !force {
		cfg, _ := config.ReadConfig()
		printWarning("Already initialized with system: %s\n", cfg.SystemName)
		printInfo("Use --force to reinitialize or 'modernpath sync' to update\n")
		return nil
	}

	client, baseURL, err := connectInitClient()
	if err != nil {
		return err
	}

	systems, err := client.ListSystems()
	if err != nil {
		printError("Failed to list systems: %v\n", err)
		return err
	}

	selectedSystem, err := resolveSingleRepoSystemSelection(client, systems)
	if err != nil {
		return err
	}

	return finalizeSystemInit(client, baseURL, selectedSystem, initFinalizeOptions{force: force, initMode: "repo"})
}

func connectInitClient() (*api.Client, string, error) {
	baseURL := apiURL
	if baseURL == "" {
		if initLocal {
			baseURL = config.LocalAPIURL
		} else {
			cfg, _ := config.ReadConfig()
			if cfg != nil && cfg.APIURL != "" {
				baseURL = cfg.APIURL
			} else {
				baseURL = config.DefaultAPIURL
			}
		}
	}

	printInfo("Connecting to ModernPath at %s...\n", baseURL)

	auth, _ := config.ReadAuth()
	token := ""
	if auth != nil {
		token = auth.Token
	}

	client := api.NewClient(baseURL, token)

	if err := client.HealthCheck(); err != nil {
		printError("Cannot connect to ModernPath: %v\n", err)
		printInfo("Make sure the ModernPath server is running\n")
		return nil, "", err
	}

	printSuccess("Connected to ModernPath\n")
	return client, baseURL, nil
}

func resolveSingleRepoSystemSelection(client *api.Client, systems []api.System) (*api.System, error) {
	if systemIDFlag > 0 {
		for i := range systems {
			if systems[i].ID == systemIDFlag {
				selected := systems[i]
				return &selected, nil
			}
		}
		printError("System with ID %d not found\n", systemIDFlag)
		return nil, fmt.Errorf("system not found")
	}

	if systemNameFlag != "" {
		for i := range systems {
			if strings.Contains(strings.ToLower(systems[i].Name), strings.ToLower(systemNameFlag)) {
				selected := systems[i]
				return &selected, nil
			}
		}
		printError("System matching '%s' not found\n", systemNameFlag)
		return nil, fmt.Errorf("system not found")
	}

	return selectSystem(client, systems)
}

func finalizeSystemInit(client *api.Client, baseURL string, selectedSystem *api.System, opts initFinalizeOptions) error {
	fmt.Printf("\n")
	printInfo("Selected: %s\n", selectedSystem.Name)
	if selectedSystem.Description != "" {
		fmt.Printf("  %s\n", selectedSystem.Description)
	}
	fmt.Printf("\n")

	printInfo("Downloading documentation and analysis data...\n")

	zipData, err := downloadExportWithStatus(client, selectedSystem.ID)
	if err != nil {
		printError("Failed to download export: %v\n", err)
		return err
	}

	printSuccess("Downloaded %d bytes\n", len(zipData))

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if opts.force {
		if err := cleanSystemExport(cwd, selectedSystem.Slug); err != nil {
			printError("Failed to clean existing export: %v\n", err)
			return err
		}
	}

	printInfo("Extracting to .modernpath/...\n")

	if err := extractZip(zipData); err != nil {
		printError("Failed to extract: %v\n", err)
		return err
	}

	cfg := &config.Config{
		APIURL:     baseURL,
		SystemID:   selectedSystem.ID,
		SystemName: selectedSystem.Name,
		SystemSlug: selectedSystem.Slug,
		InitMode:   opts.initMode,
	}

	if opts.initMode == "workspace" {
		cfg.WorkspaceMembers = buildWorkspaceConfigMembers(opts.localRepos, opts.platformMembers)
	}

	if err := config.WriteConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}

	if err := addToGitignore(cwd); err != nil {
		printWarning("Could not update .gitignore: %v\n", err)
	} else {
		printInfo("Added .modernpath/ to .gitignore\n")
	}

	fmt.Printf("\n")
	if opts.initMode == "workspace" {
		printSuccess("ModernPath workspace initialized!\n")
	} else {
		printSuccess("ModernPath initialized!\n")
	}
	fmt.Printf("\n")
	fmt.Println("Available commands:")
	fmt.Println("  modernpath search <query>   Search documentation")
	fmt.Println("  modernpath ask <query> --format=json  Build context for AI")
	fmt.Println("  modernpath review           Review git changes")
	fmt.Println("  modernpath docs sync        Update from platform")
	fmt.Println("  modernpath status           Show current status")
	fmt.Printf("\n")

	return nil
}

func downloadExportWithStatus(client *api.Client, systemID int) ([]byte, error) {
	return client.DownloadExportWithProgress(systemID, func(status, progress string) {
		if progress != "" {
			printInfo("Export %s: %s\n", status, progress)
			return
		}
		printInfo("Export %s...\n", status)
	})
}

func selectSystem(client *api.Client, systems []api.System) (*api.System, error) {
	// Build items for promptui — "+ New project" first, then existing
	items := []string{"+ New project"}
	for _, sys := range systems {
		desc := sys.Description
		if len(desc) > 50 {
			desc = desc[:47] + "..."
		}
		if desc == "" {
			desc = sys.SystemType
		}
		items = append(items, fmt.Sprintf("%s - %s", sys.Name, desc))
	}

	prompt := promptui.Select{
		Label:    "Select a project",
		Items:    items,
		Size:     10,
		HideHelp: true,
	}

	index, _, err := prompt.Run()
	if err != nil {
		return nil, err
	}

	if index == 0 {
		// Create new project
		return createNewProject(client)
	}

	return &systems[index-1], nil
}

func createNewProject(client *api.Client) (*api.System, error) {
	namePrompt := promptui.Prompt{
		Label: "Project name",
	}
	name, err := namePrompt.Run()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("project name cannot be empty")
	}

	descPrompt := promptui.Prompt{
		Label:   "Description (optional)",
		Default: "",
	}
	description, _ := descPrompt.Run()

	printInfo("Creating project '%s'...\n", name)

	arch, err := client.CreateSystem(name, description)
	if err != nil {
		printError("Failed to create project: %v\n", err)
		return nil, err
	}

	printSuccess("Project created (ID: %d)\n", arch.ID)
	return arch, nil
}

// addToGitignore adds .modernpath/ to the project's .gitignore file if not already present
func addToGitignore(projectDir string) error {
	gitignorePath := filepath.Join(projectDir, ".gitignore")

	// Read existing .gitignore if it exists
	existingContent := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existingContent = string(data)
	}

	// Check if .modernpath/ is already in .gitignore
	if strings.Contains(existingContent, ".modernpath/") || strings.Contains(existingContent, ".modernpath\n") {
		return nil // Already present
	}

	// Prepare the entry to add
	entry := "\n# ModernPath CLI local data\n.modernpath/\n"

	// If file doesn't end with newline, add one
	if existingContent != "" && !strings.HasSuffix(existingContent, "\n") {
		entry = "\n" + entry
	}

	// Append to .gitignore
	f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		return err
	}

	return nil
}

func extractZip(zipData []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	filesExtracted := 0
	dirsCreated := 0
	docsFilesFound := 0

	for _, file := range reader.File {
		// Skip macOS metadata files
		if strings.HasPrefix(file.Name, "__MACOSX/") || strings.Contains(file.Name, ".DS_Store") {
			continue
		}

		if strings.Contains(file.Name, ".modernpath/") &&
			(strings.HasSuffix(strings.ToLower(file.Name), ".md") ||
				strings.Contains(file.Name, "docs_push_manifest.json") ||
				strings.Contains(file.Name, "/architecture/")) {
			docsFilesFound++
		}

		// Flatten module angle folders so angle docs live beside module overview.md:
		// .../modules/<module>/<angle>/<doc>.md -> .../modules/<module>/<doc>.md
		exportPath := remapExportPath(file.Name)

		// Security: ensure path doesn't escape
		destPath := filepath.Join(cwd, exportPath)
		if !strings.HasPrefix(destPath, cwd) {
			continue
		}

		if file.FileInfo().IsDir() {
			// We create parent directories lazily for files; skipping zip dir entries
			// avoids re-creating pre-flattened angle subdirectories.
			continue
		}

		// Create parent directories under .modernpath as needed
		parentDir := filepath.Dir(destPath)
		if err := os.MkdirAll(parentDir, 0755); err != nil {
			return fmt.Errorf("failed to create parent directory %s: %w", parentDir, err)
		}

		// Extract file
		rc, err := file.Open()
		if err != nil {
			return fmt.Errorf("failed to open zip entry %s: %w", file.Name, err)
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			return fmt.Errorf("failed to create file %s: %w", destPath, err)
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()

		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", destPath, err)
		}

		filesExtracted++
		if verbose && strings.Contains(exportPath, "docs") {
			fmt.Printf("  Extracted: %s\n", exportPath)
		}
	}

	if verbose {
		fmt.Printf("  Extracted %d files, created %d directories", filesExtracted, dirsCreated)
		if docsFilesFound > 0 {
			fmt.Printf(", %d docs files", docsFilesFound)
		}
		fmt.Println()
	} else if docsFilesFound == 0 {
		// Warn if no docs files found in zip
		printWarning("No documentation files found in export (only SQLite database)\n")
		printInfo("The export may not include markdown documentation files.\n")
	}

	return nil
}

func cleanSystemExport(cwd, slug string) error {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}

	target := filepath.Join(cwd, ".modernpath", slug)
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	}

	return os.RemoveAll(target)
}

func remapExportPath(path string) string {
	path = filepath.ToSlash(path)
	parts := strings.Split(path, "/")

	// Find ".../modules/<module>/<angle>/<file>" and flatten to
	// ".../modules/<module>/<file>".
	for i := 0; i+3 < len(parts); i++ {
		if parts[i] != "modules" {
			continue
		}

		moduleName := parts[i+1]
		angle := parts[i+2]
		fileName := parts[i+3]

		if moduleName == "" || angle == "" || fileName == "" {
			continue
		}

		// Only flatten markdown angle docs, never overview.md or deeper nested paths.
		if !strings.HasSuffix(strings.ToLower(fileName), ".md") || strings.EqualFold(fileName, "overview.md") {
			continue
		}

		if len(parts) != i+4 {
			continue
		}

		flattened := append([]string{}, parts[:i+2]...)
		flattened = append(flattened, fileName)
		return strings.Join(flattened, "/")
	}

	return path
}
