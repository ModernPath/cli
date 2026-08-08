package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/kit"
	"github.com/spf13/cobra"
)

var (
	installDryRun bool
	installCheck  bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the requirement-driven development process into this repository",
	Long: `Install the ModernPath requirement-driven process.

Writes the process where every agent will actually load it:

  CLAUDE.md                        the entry point — imports the process
                                   (Claude Code and Cursor read this)
  .claude/rdd/PROCESS.md           the method
  .claude/rdd/platform.md          platform integration: sync, releases, gates
  .claude/skills/rdd-*             procedures, loaded on demand
  .github/copilot-instructions.md  GitHub Copilot's channel

and merges one managed block into AGENTS.md, which is how Codex finds the
process — it reads AGENTS.md and does not read CLAUDE.md.

Everything above is tool-owned and replaced on upgrade. Your own instructions
live outside it: the rest of AGENTS.md, and .claude/rules/ for conventions
scoped to particular files. The installer never touches those.`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "show what would change without writing")
	installCmd.Flags().BoolVar(&installCheck, "check", false,
		"report tool-owned files that were edited or are missing, and exit non-zero (for CI)")
	rootCmd.AddCommand(installCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	// A drifted check is a report, not a misuse of the command — don't dump
	// usage after it.
	cmd.SilenceUsage = true
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	if installCheck {
		return reportDrift(root)
	}
	if installDryRun {
		return reportPlan(root)
	}

	res, err := kit.Install(root)
	if err != nil {
		// Install aborts before writing anything it cannot do safely, so this
		// is always "nothing changed", never "half done".
		return fmt.Errorf("install aborted, repository unchanged: %w", err)
	}

	sort.Strings(res.Written)
	fmt.Println("✓ Process installed")
	for _, f := range res.Written {
		fmt.Printf("    %s\n", f)
	}
	sort.Strings(res.Merged)
	for _, f := range res.Merged {
		fmt.Printf("    %-32s managed block merged; your content untouched\n", f)
	}

	fmt.Println("\nNext:")
	fmt.Println("  • read .claude/rdd/README.md for what is tool-owned vs yours")
	fmt.Println("  • put your stack, commands and boundaries in AGENTS.md")
	fmt.Println("  • commit these files — the process belongs in source control")
	return nil
}

// reportPlan lists what an install would touch. Useful before upgrading a
// repository whose AGENTS.md has been edited, since that is the one file the
// installer shares with its owner.
func reportPlan(root string) error {
	fmt.Println("Files you may own — merged, never replaced:")
	for _, t := range kit.MergeTargets() {
		body, err := os.ReadFile(filepath.Join(root, t))
		switch {
		case os.IsNotExist(err):
			fmt.Printf("  %-32s would be created\n", t)
		case err != nil:
			return err
		case kit.HasManagedBlock(string(body)):
			fmt.Printf("  %-32s managed block refreshed; the rest of your file untouched\n", t)
		default:
			fmt.Printf("  %-32s managed block added after the title; your %d lines kept\n",
				t, strings.Count(string(body), "\n"))
		}
	}

	fmt.Println("\nKit-owned — replaced wholesale:")
	targets := []string{}
	for asset := range installTargetsForReport() {
		if t, ok := kit.TargetForAsset(asset); ok {
			targets = append(targets, t)
		}
	}
	sort.Strings(targets)
	for _, t := range targets {
		state := "would be created"
		if _, err := os.Stat(filepath.Join(root, t)); err == nil {
			state = "would be replaced"
		}
		fmt.Printf("  %-40s %s\n", t, state)
	}
	return nil
}

// installTargetsForReport walks the embedded tree so the dry run stays in step
// with what Install actually writes, rather than duplicating the list.
func installTargetsForReport() map[string]struct{} {
	out := map[string]struct{}{}
	entries, err := kit.ListAssets()
	if err != nil {
		return out
	}
	for _, e := range entries {
		out[e] = struct{}{}
	}
	return out
}

// reportDrift surfaces edits to generated files. Those files are the readable
// copy of the process, so they are also the ones people edit — and an install
// reverts such an edit silently. Exits non-zero so CI or a hook can catch it.
func reportDrift(root string) error {
	drift, err := kit.Check(root)
	if err != nil {
		return err
	}
	if len(drift) == 0 {
		fmt.Println("✓ installed process matches this CLI")
		return nil
	}
	fmt.Println("✗ tool-owned files differ from this CLI's process:")
	for _, f := range drift {
		fmt.Printf("    %s\n", f)
	}
	fmt.Println("\nThese are generated. `modernpath install` will overwrite them.")
	fmt.Println("To change the process, edit internal/kit/assets/ and ship a new CLI;")
	fmt.Println("to keep a project-local rule, put it in AGENTS.md or .claude/rules/.")
	return fmt.Errorf("%d tool-owned file(s) drifted", len(drift))
}
