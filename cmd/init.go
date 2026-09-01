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
1. Connect to the ModernPath platform (cloud production, api.modernpath.ai, by default)
2. Let you select a system
3. Download the documentation and analysis data
4. Create a .modernpath directory with all the data

Example:
  modernpath init                    # Connect to cloud production
  modernpath init --local            # Connect to localhost:4000
  modernpath init --system-id=3      # Specify system ID
  modernpath init --system="my-app"  # Specify system by name`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().IntVar(&systemIDFlag, "system-id", 0, "System ID to initialize")
	initCmd.Flags().StringVar(&systemNameFlag, "system", "", "System name to initialize")
	initCmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing .modernpath directory")
	initCmd.Flags().BoolVar(&initLocal, "local", false, "Use local server (localhost:4000) instead of cloud production")
}

func runInit(cmd *cobra.Command, args []string) error {
	if config.IsInitialized() && !force {
		cfg, _ := config.ReadConfig()
		printWarning("Already initialized with system: %s\n", cfg.SystemName)
		printInfo("Use --force to reinitialize or 'modernpath docs sync' to update\n")
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

	// Captured before extraction: extractZip creates the local .modernpath, and
	// FindConfigDir matches the first such directory walking up, so afterwards
	// the "prior" binding would resolve to the one init is about to create.
	priorBindingDir, err := config.FindConfigDir()
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

	if err := extractZip(cwd, zipData); err != nil {
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

	// InitConfig, not WriteConfig: `init` binds the directory the user is
	// standing in, which is the one case where a nested binding is intended.
	if err := config.InitConfig(cfg); err != nil {
		printError("Failed to save config: %v\n", err)
		return err
	}

	newBindingDir := filepath.Join(cwd, config.ConfigDir)

	carried, err := carryCredentialFromShadowedBinding(priorBindingDir, newBindingDir)
	if err != nil {
		printWarning("Could not carry the existing credential: %v\n", err)
	} else if carried {
		printInfo("Carried your existing credential into this workspace's binding\n")
	}

	// A binding without a credential answers 401 on every call, and ReadAuth
	// reports a missing file as "not signed in" rather than as an error — so
	// without this the next command is the first sign anything is wrong.
	if auth, err := config.ReadAuth(); err == nil && auth.Token == "" {
		printWarning("No credential for this workspace — run 'modernpath auth' before syncing\n")
	}

	if err := addToGitignore(cwd); err != nil {
		printWarning("Could not update .gitignore: %v\n", err)
	} else {
		printInfo("Ignored local .modernpath state; kept .modernpath/rdd versioned\n")
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
	fmt.Println("  modernpath work review      Review git changes")
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

const modernpathGitignoreBlock = `# ModernPath CLI local data (process package is versioned)
/.modernpath/*
!/.modernpath/rdd/
!/.modernpath/rdd/**
`

// addToGitignore ignores ModernPath credentials and machine state while
// leaving the installed process package versioned. It also upgrades the broad
// .modernpath/ rule written by older CLI releases.
func addToGitignore(projectDir string) error {
	gitignorePath := filepath.Join(projectDir, ".gitignore")

	existingContent := ""
	if data, err := os.ReadFile(gitignorePath); err == nil {
		existingContent = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}

	controlled := map[string]bool{
		"# ModernPath CLI local data (process package is versioned)": true,
		".modernpath":          true,
		"/.modernpath":         true,
		".modernpath/":         true,
		"/.modernpath/":        true,
		".modernpath/*":        true,
		"/.modernpath/*":       true,
		"!.modernpath/rdd/":    true,
		"!/.modernpath/rdd/":   true,
		"!.modernpath/rdd/**":  true,
		"!/.modernpath/rdd/**": true,
	}
	kept := make([]string, 0)
	for _, line := range strings.Split(existingContent, "\n") {
		if !controlled[strings.TrimSpace(line)] {
			kept = append(kept, line)
		}
	}

	content := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if content != "" {
		content += "\n\n"
	}
	content += modernpathGitignoreBlock
	return os.WriteFile(gitignorePath, []byte(content), 0o644)
}

// extractZipIntoWorkspace extracts a system export into the BOUND workspace —
// the directory whose .modernpath the config was read from. Anchoring on the
// invocation directory instead forked the workspace when a sync ran from a
// subdirectory: the export landed under <cwd>/.modernpath, and the next
// WriteConfig resolved to that just-created nested binding.
func extractZipIntoWorkspace(zipData []byte) error {
	configDir, err := config.WorkspaceConfigDir()
	if err != nil {
		return err
	}
	return extractZip(filepath.Dir(configDir), zipData)
}

// extractZip extracts a system export under root/.modernpath. Only `modernpath
// init` passes the current directory: init binds HERE, and its extract runs
// before the binding is written.
func extractZip(root string, zipData []byte) error {
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
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
		destPath := filepath.Join(root, exportPath)
		if !strings.HasPrefix(destPath, root) {
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

// carryCredentialFromShadowedBinding copies the credential from a binding that
// `init` is about to shadow into the new nested one.
//
// `init` is the one command that means "here" (see config.WriteConfig), so it
// can create a .modernpath nested under an existing one. The nested binding
// then shadows the parent for every later command — and the credential lives
// beside the binding, so a user who authenticated moments earlier becomes
// silently unauthenticated. config.ReadAuth returns an empty Auth for a missing
// file rather than an error, so nothing complains until the next API call
// answers 401.
//
// RUN:2026-08-23 (nextpath-ai): auth succeeded and wrote auth.json to the bound
// parent workspace; `init --force` in the subdirectory then created a
// credential-less nested binding, and an entire reverse-engineering pass ran
// against a store it could never reach.
//
// Same user, same machine, same API URL, and init gitignores auth.json in the
// new directory — so carrying it is the behaviour the user already expects.
// An existing credential in the target is never overwritten.
func carryCredentialFromShadowedBinding(priorDir, newDir string) (bool, error) {
	if priorDir == "" || priorDir == newDir {
		return false, nil
	}

	dst := filepath.Join(newDir, config.AuthFile)
	if _, err := os.Stat(dst); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}

	data, err := os.ReadFile(filepath.Join(priorDir, config.AuthFile))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return false, err
	}

	return true, nil
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
