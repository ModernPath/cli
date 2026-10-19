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
	installJSON   bool
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install the requirement-driven process and agent adapters",
	Long: `Install the requirement-driven process and agent adapters.

The CLI embeds a versioned snapshot of the canonical req-driven-dev repository.
No source checkout or vendored process repository is required. This command
writes the process package and the adapter files each agent channel loads:

  .modernpath/rdd/PROCESS.md       the canonical process
  .modernpath/rdd/skills/          the procedures that apply it
  CLAUDE.md                        imports the installed process
                                   (Claude Code and Cursor read this)
  .claude/skills/rdd-*             skill-discovery pointers
  .claude/skills/mp-*              ModernPath tooling skills
  .modernpath/cli-reference.md     this binary's command reference, rendered
                                   from its own command tree
  .github/copilot-instructions.md  GitHub Copilot's channel

and merges one managed block into AGENTS.md, which is how Codex finds the
installed files — it reads AGENTS.md and does not read CLAUDE.md.

The installed process and adapters are tool-owned and replaced on upgrade.
Change shared process instructions in the req-driven-dev source repository and
ship a new CLI. Project instructions live in the rest of AGENTS.md and
.claude/rules/.

The process store is server-only. Connect the repository with 'modernpath init',
then install the process files. --check and --dry-run do not write files.`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&installJSON, "json", false, "Emit the install result as JSON")
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

	generated := []kit.Generated{{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))}}
	if installCheck {
		if installJSON {
			drift, err := kit.Check(root, generated...)
			if err != nil {
				return err
			}
			sort.Strings(drift)
			if err := writeCommandJSON(cmd, map[string]any{"matches": len(drift) == 0, "differences": drift}); err != nil {
				return err
			}
			if len(drift) > 0 {
				return fmt.Errorf("%d tool-owned file(s) drifted", len(drift))
			}
			return nil
		}
		return reportDrift(root, generated...)
	}
	// REQ-CROSS-146: in the workspace that owns the kit, warn when the binary
	// predates the assets on disk. The assets are compiled in, so install would
	// otherwise write the previous build's bytes with nothing to show for it.
	if w := kit.StaleAssetWarning(root); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}

	if installDryRun {
		if installJSON {
			return writeCommandJSON(cmd, installPreview(generated...))
		}
		if !verbose {
			preview := installPreview(generated...)
			fmt.Printf("Install preview: %d managed files, %d kit files, %d retired files\n", len(preview["managed_targets"].([]string)), len(preview["kit_targets"].([]string)), len(preview["retired_targets"].([]string)))
			return nil
		}
		return reportPlan(root, generated...)
	}

	if err := kit.ValidateInstall(root); err != nil {
		return fmt.Errorf("install preflight failed: %w", err)
	}

	res, err := kit.Install(root, generated...)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	if err := addToGitignore(root); err != nil {
		printWarning("Could not update .gitignore: %v\n", err)
	}

	sort.Strings(res.Written)
	if installJSON {
		return writeCommandJSON(cmd, map[string]any{
			"installed": true, "written": res.Written, "created": sortInstallStrings(res.Created),
			"merged": sortInstallStrings(res.Merged), "aliased": res.Aliased, "removed": sortInstallStrings(res.Removed),
		})
	}
	printSuccess("Installed %d process and adapter files\n", len(res.Written))
	if verbose {
		for _, f := range res.Written {
			fmt.Printf("  %s\n", f)
		}
		for _, f := range sortInstallStrings(res.Created) {
			fmt.Printf("  %s: managed block created\n", f)
		}
		for _, f := range sortInstallStrings(res.Merged) {
			fmt.Printf("  %s: managed block merged\n", f)
		}
		for f, owner := range res.Aliased {
			fmt.Printf("  %s: shared with %s\n", f, owner)
		}
		for _, f := range sortInstallStrings(res.Removed) {
			fmt.Printf("  %s: retired file removed\n", f)
		}
	}
	return nil
}

func sortInstallStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func installPreview(generated ...kit.Generated) map[string]any {
	assets := make([]string, 0, len(installTargetsForReport()))
	for target := range installTargetsForReport() {
		if name, ok := kit.TargetForAsset(target); ok {
			assets = append(assets, name)
		}
	}
	sort.Strings(assets)
	generatedTargets := make([]string, 0, len(generated))
	for _, item := range generated {
		generatedTargets = append(generatedTargets, item.Target)
	}
	retired, err := kit.RetiredTargets()
	if err != nil {
		retired = nil
	}
	sort.Strings(retired)
	return map[string]any{"dry_run": true, "managed_targets": sortInstallStrings(kit.MergeTargets()), "kit_targets": assets, "generated_targets": generatedTargets, "retired_targets": retired}
}

// reportPlan lists what an install would touch. Useful before upgrading a
// repository whose AGENTS.md has been edited, since that is the one file the
// installer shares with its owner.
func reportPlan(root string, generated ...kit.Generated) error {
	var damaged []string
	aliases, err := kit.MergeAliases(root)
	if err != nil {
		return err
	}
	fmt.Println("Files you may own — merged, never replaced:")
	for _, t := range kit.MergeTargets() {
		if owner, ok := aliases[t]; ok {
			fmt.Printf("  %-32s is %s's file — carries that managed block, left as is\n", t, owner)
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, t))
		switch {
		case os.IsNotExist(err):
			fmt.Printf("  %-32s would be created\n", t)
		case err != nil:
			return err
		case kit.MarkersDamaged(string(body)):
			fmt.Printf("  %-32s managed block markers are damaged — install will refuse to touch this repository\n", t)
			damaged = append(damaged, t)
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
	for _, g := range generated {
		state := "would be created"
		if _, err := os.Stat(filepath.Join(root, g.Target)); err == nil {
			state = "would be replaced"
		}
		fmt.Printf("  %-40s %s (generated from this build)\n", g.Target, state)
	}

	retiredTargets, err := kit.RetiredTargets()
	if err != nil {
		return err
	}
	retiredPresent := []string{}
	for _, t := range retiredTargets {
		if _, err := os.Stat(filepath.Join(root, t)); err == nil {
			retiredPresent = append(retiredPresent, t)
		} else if !os.IsNotExist(err) {
			return err
		}
	}
	if len(retiredPresent) > 0 {
		fmt.Println("\nRetired process files — removed after replacement:")
		for _, t := range retiredPresent {
			fmt.Printf("  %s\n", t)
		}
	}
	fmt.Println("\nRepository ignore rules:")
	fmt.Println("  .gitignore                    local .modernpath state ignored; .modernpath/rdd versioned")
	if len(damaged) > 0 {
		return fmt.Errorf("install would abort: damaged managed block markers in %s — fix them by hand first",
			strings.Join(damaged, ", "))
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

// reportDrift surfaces edits to generated files. The installed process is
// readable, so it is also tempting to edit — and an install would revert such
// an edit silently. Exits non-zero so CI or a hook can catch it.
func reportDrift(root string, generated ...kit.Generated) error {
	drift, err := kit.Check(root, generated...)
	if err != nil {
		return err
	}
	if len(drift) == 0 {
		printSuccess("Installed process matches this CLI\n")
		return nil
	}
	if verbose {
		fmt.Println("Tool-owned files differ from this CLI's process:")
		for _, f := range drift {
			fmt.Printf("  %s\n", f)
		}
	}
	return fmt.Errorf("%d tool-owned file(s) drifted", len(drift))
}
