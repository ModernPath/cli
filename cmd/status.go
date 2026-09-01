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
	Long:  `Display the current ModernPath project status including system, sync status, and documentation.`,
	RunE:  runStatus,
}

// exportRoot is one system export tree found under .modernpath/.
type exportRoot struct {
	Rel   string // workspace-relative, e.g. ".modernpath/my-app"
	Files int
}

// statusReport is everything `modernpath status` states about a workspace,
// built from the config and the disk so it can be asserted without a terminal.
type statusReport struct {
	DocsSyncAt  string // config last_sync_at — written by `sync` / `docs sync`
	StateSyncAt string // .modernpath/last-sync-ok — written by every `factory sync` that lands
	Exports     []exportRoot
}

// buildStatusReport derives the report from the .modernpath directory and config.
//
// The export is discovered on disk rather than derived from cfg.SystemSlug: a
// workspace bound by `factory connect` has no slug, and the export root is the
// zip's own top-level folder under .modernpath/, not docs/<slug>.
func buildStatusReport(configDir string, cfg *config.Config) statusReport {
	root := filepath.Dir(configDir)
	rep := statusReport{
		DocsSyncAt:  cfg.LastSyncAt,
		StateSyncAt: readStateSyncStamp(lastSyncOkPath(root)),
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
	if slug := cfg.SystemSlug; slug != "" && !found[slug] && !modernpathReservedTopDir(slug) {
		for _, dir := range []string{filepath.Join(configDir, slug), filepath.Join(configDir, "docs", slug)} {
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				continue
			}
			if n := countFiles(dir); n > 0 {
				rel, err := filepath.Rel(root, dir)
				if err != nil {
					rel = dir
				}
				rep.Exports = append(rep.Exports, exportRoot{Rel: rel, Files: n})
				break
			}
		}
	}
	return rep
}

// readStateSyncStamp reads the timestamp `factory sync` writes on success.
// An unparseable stamp still evidences a sync, so fall back to the file time.
func readStateSyncStamp(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if data, rerr := os.ReadFile(path); rerr == nil {
		stamp := strings.TrimSpace(string(data))
		if _, perr := time.Parse(time.RFC3339, stamp); perr == nil {
			return stamp
		}
	}
	return info.ModTime().UTC().Format(time.RFC3339)
}

// renderSyncTime presents a recorded stamp in one zone. The two lanes'
// writers disagree — `docs sync` records a local offset, `factory sync`
// records Z — so comparing them as stored means doing arithmetic to read
// the command. Normalizing is a display concern: what the files hold is
// left alone, since other readers depend on it.
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

func runStatus(cmd *cobra.Command, args []string) error {
	configDir, err := config.FindConfigDir()
	if err != nil || configDir == "" {
		printWarning("Not a ModernPath project. Run 'modernpath init' to initialize.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	fmt.Println()
	bold.Println("ModernPath Project Status")
	fmt.Println("─────────────────────────────────────────")

	// Environment info (prominent)
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = config.DefaultAPIURL
	}
	envName := environmentName(apiURL)
	fmt.Printf("Environment:   ")
	if envName == "production" {
		green.Printf("%s", envName)
	} else if envName == "local" {
		yellow.Printf("%s", envName)
	} else {
		fmt.Printf("%s", envName)
	}
	fmt.Printf(" (%s)\n", apiURL)

	if cfg.SystemID > 0 {
		fmt.Printf("System:  %s (ID: %d)\n", cfg.SystemName, cfg.SystemID)
		fmt.Printf("Slug:          %s\n", cfg.SystemSlug)
		if warning := statusReachabilityWarning(apiURL, cfg.SystemID); warning != "" {
			printWarning("%s\n", warning)
		}
	} else {
		fmt.Println("System:  Not configured")
	}

	if cfg.EpicID > 0 {
		fmt.Printf("Epic:          %s (ID: %d)\n", cfg.EpicName, cfg.EpicID)
	} else {
		fmt.Println("Epic:          Not configured")
	}

	rep := buildStatusReport(configDir, cfg)

	// Two lanes, two writers: `docs sync` downloads the export, `factory sync`
	// pushes workspace state. Reporting one as "Last Sync" hides the other.
	if rep.DocsSyncAt != "" {
		fmt.Printf("Docs Sync:     %s  (modernpath docs sync)\n", renderSyncTime(rep.DocsSyncAt, time.Local))
	} else {
		fmt.Println("Docs Sync:     Never  (run 'modernpath docs sync')")
	}
	if rep.StateSyncAt != "" {
		fmt.Printf("State Sync:    %s  (factory sync)\n", renderSyncTime(rep.StateSyncAt, time.Local))
	} else {
		fmt.Println("State Sync:    Never  (run 'modernpath factory sync')")
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
