package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var prepareInputsJSON bool

type prepareInputsDecision struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type prepareInputsReport struct {
	Status                       string                  `json:"status"`
	Ready                        bool                    `json:"ready"`
	Actor                        string                  `json:"actor,omitempty"`
	SystemID                     int                     `json:"system_id,omitempty"`
	System                       string                  `json:"system,omitempty"`
	Server                       string                  `json:"server,omitempty"`
	StoreRevision                string                  `json:"store_revision,omitempty"`
	ContractVersion              int                     `json:"contract_version,omitempty"`
	CLIVersion                   string                  `json:"cli_version,omitempty"`
	CLICapability                string                  `json:"cli_capability,omitempty"`
	KitStatus                    string                  `json:"kit_status,omitempty"`
	SourceStatus                 string                  `json:"source_status,omitempty"`
	ActiveRelease                string                  `json:"active_release,omitempty"`
	ReleaseSource                string                  `json:"release_source,omitempty"`
	HeldPieceCount               int                     `json:"held_piece_count"`
	HeldAmbiguous                bool                    `json:"held_ambiguous"`
	HeldWorkNote                 string                  `json:"held_work_note,omitempty"`
	LocalDocumentsStatus         string                  `json:"local_documents_status"`
	LocalDocumentsLastSyncedAt   *string                 `json:"local_documents_last_synced_at"`
	LocalDocumentsRemedy         string                  `json:"local_documents_remedy,omitempty"`
	ServerDocumentsLastUpdatedAt *string                 `json:"server_documents_last_updated_at"`
	PendingDecisions             []prepareInputsDecision `json:"pending_decisions"`
	Reason                       string                  `json:"reason,omitempty"`
	Remedy                       string                  `json:"remedy,omitempty"`
}

var prepareInputsCmd = &cobra.Command{
	Use:           "prepare-inputs",
	Short:         "Show current delivery context and document timestamps",
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runPrepareInputsCommand,
}

func init() {
	prepareInputsCmd.Flags().BoolVar(&prepareInputsJSON, "json", false, "Print machine-readable preparation status")
	processCmd.AddCommand(prepareInputsCmd)
}

func runPrepareInputsCommand(cmd *cobra.Command, _ []string) error {
	report := prepareInputsReport{
		Status: "not_ready", PendingDecisions: []prepareInputsDecision{},
		LocalDocumentsStatus: "unavailable",
	}
	finish := func(err error) error {
		if err != nil {
			report.Reason = err.Error()
		}
		if prepareInputsJSON {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(report)
		} else {
			renderPrepareInputsHuman(cmd.OutOrStdout(), report)
			if err != nil {
				return reportedError{err}
			}
		}
		return err
	}

	env, err := credentialLoad(false) // preparation must never refresh or write auth.json
	if err != nil {
		report.Remedy = "Check the binding, then run modernpath auth if the credential is missing or expired."
		return finish(err)
	}
	report.SystemID = env.SystemID
	cfg := env.Config
	if cfg == nil {
		return finish(errors.New("bound workspace configuration is unavailable"))
	}
	systemName := cfg.SystemName
	if systemName == "" {
		systemName = configSystemName(env.SystemID)
	}
	report.System = systemName
	report.Server = env.APIURL
	if auth := env.Auth; auth != nil {
		report.Actor = auth.Actor
		if report.Actor == "" {
			report.Actor = evaluateAuthStatusAt(env.APIURL, env.SystemID, auth, true, time.Now()).Actor
		}
	}
	if report.Actor == "" {
		report.Actor = "not recorded"
	}

	report.CLIVersion = Version
	report.CLICapability = "modernpath process prepare-inputs (available in this build)"
	localKitDrift := kitDrift(env.Root)
	if localKitDrift == nil {
		report.KitStatus = "not installed or not applicable"
	} else if len(localKitDrift) > 0 {
		report.KitStatus = "stale: " + strings.Join(localKitDrift, ", ")
	} else {
		report.KitStatus = "current"
	}
	source := sourceFreshness(cliSourcePattern, Version, freshnessGit, freshnessModeDoctor)
	switch source.state {
	case freshnessStale:
		report.SourceStatus = source.detail
	case freshnessCurrent:
		report.SourceStatus = "current"
	default:
		report.SourceStatus = "unverified"
	}
	if err := readPrepareInputsContext(env, cfg, &report); err != nil {
		if report.Remedy == "" {
			report.Remedy = "Restore the read-only process API and rerun process prepare-inputs."
		}
		return finish(err)
	}
	if signal, line := freshnessLine(freshnessInputs{
		version: Version, servedContract: fmt.Sprint(report.ContractVersion),
		kitDrift: localKitDrift, source: source,
	}); signal != "" {
		report.Remedy = freshnessRemedy(signal)
		return finish(errors.New(line))
	}
	report.StoreRevision = env.storeRevision
	if report.StoreRevision == "" {
		report.StoreRevision = "not served"
	}
	report.Status = "ready"
	report.Ready = true
	return finish(nil)
}

func configSystemName(id int) string { return fmt.Sprintf("system %d", id) }

func freshnessRemedy(signal string) string {
	switch signal {
	case "contract":
		return "Upgrade the ModernPath CLI to match the server contract, then retry."
	case "kit":
		return "Run modernpath install with a current CLI build, then retry."
	case "source":
		return "Rebuild and install the CLI from the checked-out source, then retry."
	default:
		return "Upgrade and install a current ModernPath CLI, then retry."
	}
}

func readPrepareInputsContext(env *factoryEnv, cfg *config.Config, report *prepareInputsReport) error {
	path := fmt.Sprintf("/api/v1/sync/prepare-inputs?system_id=%d", env.SystemID)
	status, response, err := env.call(http.MethodGet, path, nil)
	if err != nil {
		report.Remedy = "Restore access to the bound ModernPath server and credential, then retry."
		return fmt.Errorf("preparation context read failed: %w", err)
	}
	if status == http.StatusNotFound {
		refusal := strings.ToLower(refusalText(response))
		if strings.Contains(refusal, "system not found") || str(feedMap(response, "error"), "reason") == "not_found" {
			report.Remedy = "Bind a system reachable by this credential with modernpath factory connect --system <id>."
			return serverRefusal("bound system lookup failed", status, response)
		}
		report.Remedy = "Upgrade the ModernPath server to provide the preparation-context endpoint, then retry."
		return fmt.Errorf("server does not provide the preparation-context endpoint; server upgrade required: %w", serverRefusal("preparation context", status, response))
	}
	if status != http.StatusOK {
		report.Remedy = "Restore the read-only preparation-context API, then retry."
		return serverRefusal("preparation context", status, response)
	}

	data := dataOf(response)
	if len(data) == 0 {
		return errors.New("preparation context response is missing data")
	}
	system, ok := data["system"].(map[string]any)
	if !ok {
		return errors.New("preparation context response is missing system identity")
	}
	systemID, ok := integerValue(system["id"])
	if !ok || systemID != env.SystemID {
		report.Remedy = "Refresh the binding with modernpath factory connect, then retry."
		return fmt.Errorf("preparation context returned system id %v for bound system %d", system["id"], env.SystemID)
	}
	systemName := str(system, "name")
	systemSlug := str(system, "slug")
	if systemName == "" || systemSlug == "" {
		return errors.New("preparation context returned an incomplete system identity")
	}
	if cfg.SystemSlug != "" && cfg.SystemSlug != systemSlug {
		report.Remedy = "Refresh the binding with modernpath factory connect, then retry."
		return fmt.Errorf("configured system slug %q does not match bound system slug %q", cfg.SystemSlug, systemSlug)
	}
	if err := validateSystemExportSlug(systemSlug); err != nil {
		report.Remedy = "Refresh the binding with a valid system export slug, then retry."
		return err
	}
	report.System = systemName

	version, ok := integerValue(data["contract_version"])
	if !ok || version <= 0 {
		report.Remedy = "Upgrade the ModernPath server to provide a valid sync contract, then retry."
		return errors.New("preparation context returned no valid contract version")
	}
	report.ContractVersion = version

	heldCount, ok := integerValue(data["held_piece_count"])
	if !ok || heldCount < 0 {
		return errors.New("preparation context returned no valid held-piece count")
	}
	report.HeldPieceCount = heldCount
	report.HeldAmbiguous = heldCount > 1
	if report.HeldAmbiguous {
		report.HeldWorkNote = "Preparation did not select a piece; use --piece <id> for a piece-specific next action."
	}

	updatedAt, err := timestampField(data, "documents_updated_at")
	if err != nil {
		return err
	}
	report.ServerDocumentsLastUpdatedAt = updatedAt

	releases, ok := data["active_releases"].([]any)
	if !ok {
		return errors.New("preparation context returned no active-release list")
	}
	if len(releases) != 1 {
		report.Remedy = "Have a human establish exactly one active release for this system, then retry."
		return fmt.Errorf("release preflight found %d active releases; expected exactly one", len(releases))
	}
	activeRelease, ok := releases[0].(map[string]any)
	if !ok || str(activeRelease, "slug") == "" || str(activeRelease, "status") != "active" {
		return errors.New("preparation context returned an invalid active-release record")
	}
	report.ActiveRelease = str(activeRelease, "slug")
	report.ReleaseSource = str(activeRelease, "source_tag")
	if !strings.HasPrefix(report.ReleaseSource, "USER:") {
		report.Remedy = "Have a human approve a USER-sourced release selection for the active release, then retry."
		return fmt.Errorf("active release %q has no answered USER-sourced approval", report.ActiveRelease)
	}

	pending, ok := data["pending_decisions"].([]any)
	if !ok {
		return errors.New("preparation context returned no pending-decision list")
	}
	for _, raw := range pending {
		item, ok := raw.(map[string]any)
		if !ok || strings.TrimSpace(str(item, "id")) == "" || strings.TrimSpace(str(item, "title")) == "" {
			return errors.New("preparation context returned an invalid pending decision")
		}
		report.PendingDecisions = append(report.PendingDecisions, prepareInputsDecision{ID: str(item, "id"), Title: str(item, "title")})
	}

	report.LocalDocumentsStatus = "unavailable"
	localDocs := systemExportRootDir(filepath.Join(env.Root, config.ConfigDir), systemSlug)
	if info, statErr := os.Stat(localDocs); statErr == nil && info.IsDir() {
		report.LocalDocumentsStatus = "available"
		stamp, readErr := readExportGeneratedAt(filepath.Join(env.Root, config.ConfigDir), systemSlug)
		if readErr != nil {
			report.LocalDocumentsRemedy = "The server export timestamp cannot be read; run modernpath docs sync to refresh it."
		} else if stamp == nil {
			report.LocalDocumentsRemedy = "The local export has no server timestamp; run modernpath docs sync to refresh it."
		} else {
			report.LocalDocumentsLastSyncedAt = stamp
		}
	} else {
		report.LocalDocumentsRemedy = "Run modernpath docs sync to download the local documentation export."
	}
	return nil
}

func integerValue(value any) (int, bool) {
	n, ok := value.(float64)
	if !ok || n != float64(int(n)) {
		return 0, false
	}
	return int(n), true
}

func timestampField(data map[string]any, field string) (*string, error) {
	value, exists := data[field]
	if !exists {
		return nil, fmt.Errorf("preparation context is missing %s", field)
	}
	if value == nil {
		return nil, nil
	}
	timestamp, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("preparation context returned invalid %s", field)
	}
	if _, err := time.Parse(time.RFC3339Nano, timestamp); err != nil {
		return nil, fmt.Errorf("preparation context returned invalid %s: %w", field, err)
	}
	return &timestamp, nil
}

func renderPrepareInputsHuman(out io.Writer, report prepareInputsReport) {
	if !report.Ready {
		color.New(color.FgRed, color.Bold).Fprint(out, "✗ Not ready")
		if report.SystemID > 0 && report.System != "" {
			fmt.Fprintf(out, " · %s #%d", report.System, report.SystemID)
		}
		fmt.Fprint(out, "\n\n")
		if report.Reason != "" {
			fmt.Fprintf(out, "  %s\n", report.Reason)
		}
		if report.Remedy != "" {
			fmt.Fprintf(out, "  Next: %s\n", report.Remedy)
		}
		return
	}
	color.New(color.FgGreen, color.Bold).Fprint(out, "✓ Ready")
	fmt.Fprintf(out, " · %s #%d\n\n", report.System, report.SystemID)
	fmt.Fprintf(out, "  %-8s %s\n  %-8s %s\n  %-8s %s\n  %-8s %d held · %d pending decisions\n",
		"Server", report.Server, "Account", report.Actor, "Release", report.ActiveRelease,
		"Work", report.HeldPieceCount, len(report.PendingDecisions))
	if report.HeldAmbiguous {
		fmt.Fprintln(out, "  Next     modernpath process next --piece <id>")
	}
	color.New(color.Bold).Fprintf(out, "\nDocuments\n")
	if report.LocalDocumentsStatus == "available" {
		fmt.Fprintf(out, "  %-8s Last synced: %s\n", "Local", formatPrepareTimestamp(report.LocalDocumentsLastSyncedAt, "unknown"))
	} else {
		fmt.Fprintf(out, "  %-8s No local export\n", "Local")
	}
	if report.ServerDocumentsLastUpdatedAt == nil {
		fmt.Fprintf(out, "  %-8s No documents\n", "Server")
	} else {
		fmt.Fprintf(out, "  %-8s Updated: %s\n", "Server", formatPrepareTimestamp(report.ServerDocumentsLastUpdatedAt, "unknown"))
	}
	if report.LocalDocumentsRemedy != "" {
		fmt.Fprintf(out, "  %-8s modernpath docs sync\n", "Sync")
	}
	if len(report.PendingDecisions) > 0 {
		color.New(color.Bold).Fprintf(out, "\nPending decisions\n")
		for _, decision := range report.PendingDecisions {
			fmt.Fprintf(out, "  • %s — %s\n", decision.ID, decision.Title)
		}
	}
	if verbose {
		color.New(color.Bold).Fprintf(out, "\nDiagnostics\n")
		fmt.Fprintf(out, "  Store    %s · sync contract %d\n", report.StoreRevision, report.ContractVersion)
		fmt.Fprintf(out, "  CLI      %s · %s\n", report.CLIVersion, report.CLICapability)
		fmt.Fprintf(out, "  Kit      %s\n  Source   %s\n  Release source %s\n",
			report.KitStatus, report.SourceStatus, report.ReleaseSource)
	}
}

func formatPrepareTimestamp(timestamp *string, empty string) string {
	if timestamp == nil || *timestamp == "" {
		return empty
	}
	parsed, err := time.Parse(time.RFC3339Nano, *timestamp)
	if err != nil {
		return "unknown"
	}
	return parsed.UTC().Format("2 Jan 2006, 15:04:05 UTC")
}
