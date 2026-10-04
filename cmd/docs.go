package cmd

// Command registration. Focused docs_*.go files own the implementations.

import (
	"github.com/spf13/cobra"
)

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
Note: Specifications are synced automatically when selecting an Epic
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
