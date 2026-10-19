package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

// runDocsGenerate starts the lifecycle the UI starts (REQ-CROSS-503 C3, D8):
// a system-scoped POST that needs no repository, so none is looked up or
// created here — a created one would be a stray second upload repository on
// an import-created system.
func runDocsGenerate(cmd *cobra.Command, args []string) error {
	machine, _ := cmd.Flags().GetBool("json")
	if machine && !docsGenerateYes {
		return fmt.Errorf("docs generate --json requires --yes because it starts an analysis run")
	}
	if err := validateAnalysisMode(docsGenerateMode); err != nil {
		return reportFailure(err)
	}
	cfg, err := commandConfig()
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

	if !machine {
		printInfo("Scanning codebase for initial statistics...\n")
	}

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
	if !machine {
		fmt.Println("════════════════════════════════════════════════════════════════════")
		fmt.Println("Analysis Overview")
		fmt.Println("════════════════════════════════════════════════════════════════════")
		fmt.Printf("System: %s\nMode: %s\nFolder: %s\nPath: %s\nFiles: %d\nLines: %s\nEstimated tokens: %s\nEstimated cost: ~$%.2f\nEstimated time: ~%d min\n",
			boundSystemLabel(cfg), docsGenerateMode, folderName, folderPath, scanResult.TotalFiles,
			formatNumber(scanResult.TotalLines), formatTokenCount(int(estimatedTokens)), estimatedCost, estimatedMinutes)
		if scanResult.PrimaryLanguage != "" {
			fmt.Printf("Language: %s\n", scanResult.PrimaryLanguage)
		}
	}

	ok, err := confirmAction("Start AI Analysis", docsGenerateYes, true)
	if err != nil {
		return reportFailure(err)
	}
	if !ok {
		if machine {
			return writeCommandJSON(cmd, map[string]any{"started": false, "cancelled": true})
		}
		fmt.Println("Analysis cancelled.")
		return nil
	}

	if !machine {
		printInfo("Starting AI analysis pipeline...\n")
	}

	answer, err := startAnalysis(cfg, docsGenerateMode)
	if err != nil {
		printError("Failed to start analysis: %v\n", err)
		return reportedError{err}
	}

	if machine {
		return writeCommandJSON(cmd, map[string]any{"started": true, "system_id": cfg.SystemID, "mode": docsGenerateMode, "run": answer})
	}
	printLifecycleRun(answer)
	printInfo("The analysis is running in the background; follow it with 'modernpath analysis status'.\n")

	return nil
}
