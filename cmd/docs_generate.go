package cmd

import (
	"fmt"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

// runDocsGenerate starts the lifecycle the UI starts (REQ-CROSS-503 C3, D8):
// a system-scoped POST that needs no repository, so none is looked up or
// created here — a created one would be a stray second upload repository on
// an import-created system.
func runDocsGenerate(cmd *cobra.Command, args []string) error {
	if err := validateAnalysisMode(docsGenerateMode); err != nil {
		return reportFailure(err)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return reportedError{err}
	}

	if cfg.SystemID == 0 {
		return reportFailure(errNoBoundSystem)
	}

	// Get current directory name
	cwd, err := os.Getwd()
	if err != nil {
		printError("Failed to get current directory: %v\n", err)
		return reportedError{err}
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
		return reportedError{err}
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
	fmt.Printf("  🔗 System:        %s\n", boundSystemLabel(cfg))
	fmt.Printf("  🧭 Mode:          %s\n", docsGenerateMode)
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

	ok, err := confirmAction("Start AI Analysis", docsGenerateYes, true)
	if err != nil {
		return reportFailure(err)
	}
	if !ok {
		fmt.Println("Analysis cancelled.")
		return nil
	}

	fmt.Println()
	printInfo("🚀 Step 2: Starting AI analysis pipeline...\n")
	fmt.Println("   This may take a while. The analysis will:")
	fmt.Println("   • Extract architecture patterns")
	fmt.Println("   • Identify technologies and frameworks")
	fmt.Println("   • Map dependencies")
	fmt.Println("   • Generate comprehensive documentation")
	fmt.Println()

	answer, err := startAnalysis(cfg, docsGenerateMode)
	if err != nil {
		printError("Failed to start analysis: %v\n", err)
		return reportedError{err}
	}

	printLifecycleRun(answer)
	fmt.Println()
	printInfo("The analysis is running in the background.\n")
	fmt.Println("  • Follow progress: modernpath analysis status")
	fmt.Println("  • Sync documentation when complete: modernpath docs sync")
	fmt.Println()

	return nil
}
