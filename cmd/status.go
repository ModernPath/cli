package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current ModernPath status",
	Long: `Display the current ModernPath project status: the binding, local
documentation exports, and documentation sync time.`,
	RunE: runStatus,
}

// exportRoot is one system export tree found under .modernpath/.
type exportRoot struct {
	Rel   string `json:"path"` // workspace-relative, e.g. ".modernpath/my-app"
	Files int    `json:"files"`
}

// statusReport is everything `modernpath status` states about a workspace,
// built from the config and the disk so it can be asserted without a terminal.
type statusReport struct {
	DocsSyncAt string // config last_sync_at — written by `sync` / `docs sync`
	Exports    []exportRoot
}

// buildStatusReport derives the report from the .modernpath directory and config.
//
// The export is discovered on disk rather than derived from cfg.SystemSlug: a
// workspace bound by `init` has no slug, and the export root is the
// zip's own top-level folder under .modernpath/, not docs/<slug>.
func buildStatusReport(configDir string, cfg *config.Config) statusReport {
	root := filepath.Dir(configDir)
	rep := statusReport{
		DocsSyncAt: cfg.LastSyncAt,
	}

	found := map[string]bool{}
	for _, slug := range listSystemExportSlugs(configDir) {
		dir, ok := resolveSystemRootDir(configDir, slug)
		if !ok {
			continue
		}
		found[slug] = true
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			rel = dir
		}
		rep.Exports = append(rep.Exports, exportRoot{Rel: rel, Files: countFiles(dir)})
	}

	// An export produced before Core.Export wrote marker files carries neither
	// docs_push_manifest.json nor blueprint.json, so marker discovery cannot
	// see it. The config's own slug vouches for exactly one such directory;
	// anything the config does not name stays invisible, which is what keeps
	// CLI-owned directories out of the list.
	if slug := cfg.SystemSlug; !found[slug] {
		if dir, ok := boundSystemRootDir(configDir, slug); ok {
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				rel = dir
			}
			rep.Exports = append(rep.Exports, exportRoot{Rel: rel, Files: countFiles(dir)})
		}
	}
	return rep
}

// renderSyncTime presents a documentation-sync stamp in the local time zone
// while preserving the value written to the config.
func renderSyncTime(raw string, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw // hand-edited or an older format — show it as recorded
	}
	return t.In(loc).Format(time.RFC3339)
}

func countFiles(dir string) int {
	n := 0
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func init() {
	statusCmd.Flags().Bool("json", false, "Emit the binding and local documentation status as JSON")
	addRecordReadFlags(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	if err := validateRecordSelection(cmd, []string{"api_url", "system_id", "system_slug", "docs_sync_at", "exports"}); err != nil {
		return err
	}
	machine, _ := cmd.Flags().GetBool("json")
	configDir, err := config.FindConfigDir()
	if err != nil || configDir == "" {
		if machine {
			return fmt.Errorf("not a ModernPath project. Run 'modernpath init' to initialize")
		}
		printWarning("Not a ModernPath project. Run 'modernpath init' to initialize.\n")
		return nil
	}

	cfg, err := commandConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	rep := buildStatusReport(configDir, cfg)
	result := map[string]any{"api_url": apiURL, "system_id": cfg.SystemID, "system_slug": cfg.SystemSlug, "docs_sync_at": rep.DocsSyncAt, "exports": rep.Exports}
	full, _ := cmd.Flags().GetBool("full")
	if machine || cmd.Flags().Changed("fields") || full {
		selected, err := selectRecordFields(cmd, result, []string{"api_url", "system_id", "system_slug", "docs_sync_at", "exports"}, []string{"api_url", "system_id", "system_slug", "docs_sync_at", "exports"}, nil)
		if err != nil {
			return err
		}
		if machine {
			return writeCommandJSON(cmd, selected)
		}
		return writeRecord(cmd, selected)
	}

	bold := color.New(color.Bold)

	fmt.Println()
	bold.Println("ModernPath Project Status")
	fmt.Println("─────────────────────────────────────────")

	// REQ-CROSS-391: the binding lines are the renderer for the binding lines shown below.
	auth, _ := config.ReadAuth()
	for _, line := range bindingLines(cfg, auth) {
		fmt.Println(line)
	}
	if cfg.SystemID > 0 {
		fmt.Printf("Slug:          %s\n", cfg.SystemSlug)
		if warning := statusReachabilityWarning(apiURL, cfg.SystemID); warning != "" {
			printWarning("%s\n", warning)
		}

	}

	if rep.DocsSyncAt != "" {
		fmt.Printf("Docs Sync:     %s  (modernpath docs sync)\n", renderSyncTime(rep.DocsSyncAt, time.Local))
	} else {
		fmt.Println("Docs Sync:     Never  (run 'modernpath docs sync')")
	}
	if len(rep.Exports) == 0 {
		fmt.Println("Local Docs:    Not synced (run 'modernpath docs sync')")
	}
	for i, e := range rep.Exports {
		label := "Local Docs:    "
		if i > 0 {
			label = strings.Repeat(" ", len(label))
		}
		fmt.Printf("%s%d files  (%s)\n", label, e.Files, e.Rel)
	}

	fmt.Println()
	return nil
}

// statusReachabilityWarning returns a warning line when a bearer exists and
// the bound system is confirmed unreachable by it — never for systemID == 0
// (unconfigured; system ids are never 0, so this is the same gate the other
// three surfaces have, enforced here too and not only at the call site, so
// a caller can't reintroduce the gap by skipping it), an absent bearer, or
// a check that itself failed (REQ-CROSS-282: fail-open).
func statusReachabilityWarning(apiURL string, systemID int) string {
	if systemID == 0 {
		return ""
	}
	auth, err := config.ReadAuth()
	if err != nil || strings.TrimSpace(auth.Token) == "" {
		return ""
	}
	systems, err := listSystemsFn(apiURL, auth.Token)
	if err != nil {
		return ""
	}
	if systemReachable(systems, systemID) {
		return ""
	}
	return systemMismatchMessage(systemID, systems)
}
