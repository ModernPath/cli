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
	installDryRun      bool
	installCheck       bool
	installStoreBacked bool
	installSource      string
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
  .modernpath/rdd/file-state/      the record serialization shapes
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

--store-backed --source USER:… declares a bound workspace that never had file
ledgers store-backed in one step: the server records the store-backed
activation gate, answered by you with the USER: source, and sets the system's
process-store state active in one action; then process/store-backed.md is
written in the same shape 'migrate flip' writes — the accepted source with its
gate, the server and system, and no retired files — and the install that
follows withholds the ledger skill. The server decides by its own state: a
system seeded by an import or cleared after a flip is refused by name and goes
through 'migrate flip'; an already-declared system is reported and left as it
is; a marker present over an undeclared server, or a declaration recorded from
another checkout, has its missing half completed. A workspace with ledgers
under tasks/ is refused here and goes through the flip.`,
	RunE: runInstall,
}

func init() {
	installCmd.Flags().BoolVar(&installDryRun, "dry-run", false, "show what would change without writing")
	installCmd.Flags().BoolVar(&installCheck, "check", false,
		"report tool-owned files that were edited or are missing, and exit non-zero (for CI)")
	installCmd.Flags().BoolVar(&installStoreBacked, "store-backed", false,
		"declare this bound, ledgerless workspace store-backed (gate answered with --source, server state active, marker written) before installing")
	installCmd.Flags().StringVar(&installSource, "source", "",
		"the USER:<date>:<decision> source the store-backed declaration is answered with (with --store-backed)")
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

	if installStoreBacked && (installDryRun || installCheck) {
		flag := "--dry-run"
		if installCheck {
			flag = "--check"
		}
		return fmt.Errorf("--store-backed declares the workspace and does not combine with %s — run it on its own", flag)
	}

	generated := []kit.Generated{{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))}}
	if installCheck {
		return reportDrift(root, generated...)
	}
	// REQ-CROSS-146: in the workspace that owns the kit, warn when the binary
	// predates the assets on disk. The assets are compiled in, so install would
	// otherwise write the previous build's bytes with nothing to show for it.
	if w := kit.StaleAssetWarning(root); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}

	if installDryRun {
		return reportPlan(root, generated...)
	}

	// REQ-CROSS-406: the declaration runs before the install so the marker is
	// on disk when Withheld decides the ledger skill on the same run.
	if installStoreBacked {
		if err := declareStoreBacked(root, installSource); err != nil {
			return err
		}
	}

	res, err := kit.Install(root, generated...)
	if err != nil {
		return fmt.Errorf("install failed: %w", err)
	}
	if err := addToGitignore(root); err != nil {
		printWarning("Could not update .gitignore: %v\n", err)
	}

	sort.Strings(res.Written)
	fmt.Println("✓ Process snapshot and agent adapters installed")
	for _, f := range res.Written {
		fmt.Printf("    %s\n", f)
	}
	sort.Strings(res.Created)
	for _, f := range res.Created {
		fmt.Printf("    %-32s created with the managed block\n", f)
	}
	sort.Strings(res.Merged)
	for _, f := range res.Merged {
		fmt.Printf("    %-32s managed block merged; your content untouched\n", f)
	}
	aliased := make([]string, 0, len(res.Aliased))
	for f := range res.Aliased {
		aliased = append(aliased, f)
	}
	sort.Strings(aliased)
	for _, f := range aliased {
		fmt.Printf("    %-32s is %s's file; carries that managed block\n", f, res.Aliased[f])
	}
	sort.Strings(res.Removed)
	for _, f := range res.Removed {
		fmt.Printf("    %-32s retired process file removed\n", f)
	}

	fmt.Println("\nNext:")
	fmt.Println("  • read .modernpath/rdd/AGENTS.md")
	fmt.Println("  • put your stack, commands and boundaries in AGENTS.md")
	fmt.Println("  • commit the installed process and adapter files")
	return nil
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
		if kit.Withheld(root, asset) {
			continue
		}
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
		fmt.Println("✓ installed process matches this CLI")
		return nil
	}
	fmt.Println("✗ tool-owned files differ from this CLI's process:")
	for _, f := range drift {
		fmt.Printf("    %s\n", f)
	}
	fmt.Println("\nThese are generated. `modernpath install` will overwrite them.")
	fmt.Println("To change the process, update req-driven-dev, refresh the embedded snapshot, and ship a new CLI;")
	fmt.Println("to keep a project-local rule, put it in AGENTS.md or .claude/rules/.")
	return fmt.Errorf("%d tool-owned file(s) drifted", len(drift))
}
