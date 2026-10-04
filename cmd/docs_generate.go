package cmd

import (
	"fmt"
	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
	"strings"
)

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
