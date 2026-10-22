package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/kit"
	"github.com/spf13/cobra"
)

// prepare is a bounded, read-only orientation pass before rdd-start. It does
// not choose an Epic or turn observations into delivery approval.
var prepareCmd = &cobra.Command{
	Use: "prepare", Short: "Check the current session and show useful next steps",
	Long: `Check the signed-in actor, bound System access, installed process kit,
local documentation export, live document edits and analysis status. The
command makes no delivery-store writes, starts no analysis and does not sync,
install, upgrade or sign in.`,
	Args: cobra.NoArgs, RunE: runPrepare,
}

const prepareReadTimeout = 2 * time.Second

type prepareReport struct {
	Actor                     string   `json:"actor,omitempty"`
	Server                    string   `json:"server"`
	SystemID                  int      `json:"system_id,omitempty"`
	SystemName                string   `json:"system_name,omitempty"`
	Session                   string   `json:"session"`
	SessionDetail             string   `json:"session_detail,omitempty"`
	Access                    string   `json:"access"`
	AccessDetail              string   `json:"access_detail,omitempty"`
	CLIVersion                string   `json:"cli_version"`
	LatestCLI                 string   `json:"latest_cli"`
	Kit                       string   `json:"kit"`
	KitDifferences            []string `json:"kit_differences,omitempty"`
	DocsState                 string   `json:"docs_state"`
	DocsMetadata              string   `json:"docs_metadata"`
	DocsSyncAt                string   `json:"docs_sync_at,omitempty"`
	ExportGeneratedAt         string   `json:"export_generated_at,omitempty"`
	ObservedNewerDocumentEdit string   `json:"observed_newer_document_edit,omitempty"`
	DocsDetail                string   `json:"docs_detail,omitempty"`
	Analysis                  string   `json:"analysis"`
	AnalysisDetail            string   `json:"analysis_detail,omitempty"`
	NextSteps                 []string `json:"next_steps"`
}

func init() {
	prepareCmd.Flags().Bool("json", false, "Print one bounded JSON report")
	rootCmd.AddCommand(prepareCmd)
}

func runPrepare(cmd *cobra.Command, _ []string) error {
	report := prepareReport{Session: "unknown", Access: "unknown", CLIVersion: Version, LatestCLI: "unknown", Kit: "unknown", DocsState: "unknown", DocsMetadata: "not_checked", Analysis: "unknown", NextSteps: []string{}}
	needsUpgrade := false
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		report.Session = "unbound"
		report.Access = "unbound"
		report.NextSteps = append(report.NextSteps, "modernpath init")
		return finishPrepare(cmd, report)
	}
	root := filepath.Dir(cfgDir)
	cfg, err := commandConfig()
	if err != nil {
		report.SessionDetail = "workspace configuration could not be read"
		report.NextSteps = append(report.NextSteps, "modernpath init --help")
		return finishPrepare(cmd, report)
	}
	report.Server = cfg.APIURL
	if report.Server == "" {
		report.Server = config.DefaultAPIURL
	}
	report.SystemID, report.SystemName, report.DocsSyncAt = cfg.SystemID, cfg.SystemName, cfg.LastSyncAt
	if releasedBuild(Version) {
		if latest := latestPublishedRelease(); latest != "" {
			report.LatestCLI = latest
			if newerRelease(Version, latest) {
				needsUpgrade = true
				report.NextSteps = append(report.NextSteps, upgradeAdviceForCurrentBuild(root)...)
			}
		}
	}
	if cfg.SystemID == 0 {
		report.Access = "unbound"
		report.NextSteps = append(report.NextSteps, "modernpath init")
	}

	generated := []kit.Generated{{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))}}
	if drift, err := kit.Check(root, generated...); err == nil {
		report.Kit = "matches"
		if len(drift) > 0 {
			report.Kit = "drifted"
			if len(drift) > 5 {
				drift = drift[:5]
			}
			report.KitDifferences = drift
			if report.LatestCLI == "unknown" && releasedBuild(Version) {
				report.NextSteps = append(report.NextSteps, "Check your CLI release channel for a newer version before modernpath install (latest release could not be verified)")
			} else if !releasedBuild(Version) {
				report.NextSteps = append(report.NextSteps, upgradeAdviceForCurrentBuild(root)...)
			}
			report.NextSteps = append(report.NextSteps, "modernpath install", "modernpath install --check")
		}
	} else {
		report.NextSteps = append(report.NextSteps, "modernpath install --check")
	}
	if needsUpgrade && report.Kit == "matches" {
		report.NextSteps = append(report.NextSteps, "modernpath install", "modernpath install --check")
	}

	auth, authErr := config.ReadAuth()
	if authErr != nil {
		report.Session = "unknown"
		report.SessionDetail = "stored credential could not be read"
		report.NextSteps = append(report.NextSteps, "modernpath auth status")
		localDocsState(root, cfg, &report)
		return finishPrepare(cmd, report)
	}
	st := evaluateAuthStatusAt(report.Server, cfg.SystemID, auth, true, time.Now())
	report.Actor = st.Actor
	report.SessionDetail = st.Detail
	if !st.Authenticated {
		report.Session = "unavailable"
		report.NextSteps = append(report.NextSteps, authRepairCommand(report.Server))
	} else if cfg.SystemID > 0 {
		// One scoped system listing verifies both the session and access. The
		// offline evaluation above checks expiry and audience without a second GET.
		systems, accessErr := listSystemsFn(report.Server, auth.Token)
		switch {
		case errors.Is(accessErr, api.ErrUnauthorized):
			report.Session = "unavailable"
			report.SessionDetail = "server rejected the credential"
			report.NextSteps = append(report.NextSteps, authRepairCommand(report.Server))
		case accessErr != nil:
			report.Session = "unknown"
			report.SessionDetail = "server verification unavailable"
			report.AccessDetail = "bound System access could not be checked"
			report.NextSteps = append(report.NextSteps, "modernpath auth status")
		case systemReachable(systems, cfg.SystemID):
			report.Session = "authenticated"
			report.SessionDetail = "credential accepted by the server"
			report.Access = "reachable"
		default:
			report.Session = "authenticated"
			report.SessionDetail = "credential accepted by the server"
			report.Access = "unreachable"
			report.AccessDetail = systemMismatchMessage(cfg.SystemID, systems)
			report.NextSteps = append(report.NextSteps, "modernpath init --system-id <reachable-id>")
		}
	}

	localDocsState(root, cfg, &report)
	if report.Access == "reachable" {
		checkLiveDocuments(&authenticatedClient{client: &http.Client{Timeout: prepareReadTimeout}, token: auth.Token, baseURL: report.Server}, cfg.SystemID, &report)
		checkPrepareAnalysis(cfg, &report)
	}
	if report.DocsState == "newer_edits" || report.DocsState == "missing" {
		report.NextSteps = append(report.NextSteps, "modernpath docs sync")
	}
	if report.Access == "reachable" {
		report.NextSteps = append(report.NextSteps, "read .modernpath/rdd/skills/rdd-start/SKILL.md")
	}
	return finishPrepare(cmd, report)
}

func finishPrepare(cmd *cobra.Command, report prepareReport) error {
	if err := writePrepare(cmd, report); err != nil {
		return err
	}
	if report.Session != "authenticated" || report.Access != "reachable" || report.Kit != "matches" || report.DocsState == "missing" || report.DocsState == "newer_edits" || report.DocsMetadata != "checked" || newerRelease(report.CLIVersion, report.LatestCLI) {
		return reportedError{fmt.Errorf("preparation needs attention; see the report and next steps")}
	}
	return nil
}

func localDocsState(root string, cfg *config.Config, report *prepareReport) {
	rep := buildStatusReport(filepath.Join(root, ".modernpath"), cfg)
	if len(rep.Exports) == 0 {
		report.DocsState = "missing"
		report.DocsMetadata = "not_applicable"
		return
	}
	report.DocsState = "unknown" // A local export alone cannot establish server currency.
	slug := cfg.SystemSlug
	if slug == "" {
		report.DocsDetail = "bound System has no export slug; local export identity is unknown"
		return
	}
	matched := false
	for _, export := range rep.Exports {
		if filepath.Base(export.Rel) == slug {
			matched = true
			break
		}
	}
	if !matched {
		report.DocsState = "missing"
		return
	}
	stamp, err := readExportGeneratedAt(filepath.Join(root, ".modernpath"), slug)
	if err == nil && stamp != nil {
		report.ExportGeneratedAt = *stamp
	}
}

// The live discovery API orders by creation, not update time. Stop on the
// first newer edit; after five metadata pages, absence remains unknown.
func checkLiveDocuments(client *authenticatedClient, systemID int, report *prepareReport) {
	if report.DocsState == "missing" {
		return
	}
	if report.ExportGeneratedAt == "" {
		report.DocsDetail = "export generation time is unknown"
		return
	}
	exportTime, err := time.Parse(time.RFC3339Nano, report.ExportGeneratedAt)
	if err != nil {
		return
	}
	cursor := ""
	for page := 0; page < 5; page++ {
		query := url.Values{"metadata": {"true"}, "limit": {"100"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		endpoint := fmt.Sprintf("%s/api/systems/%d/documents?%s", client.baseURL, systemID, query.Encode())
		resp, err := client.Get(endpoint)
		if err != nil {
			report.DocsMetadata = "unavailable"
			report.DocsDetail = "live document metadata unavailable"
			return
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			report.DocsMetadata = "unavailable"
			report.DocsDetail = "live document metadata unavailable"
			return
		}
		var body struct {
			Data *struct {
				Documents []struct {
					UpdatedAt string `json:"updated_at"`
				} `json:"documents"`
				NextCursor json.RawMessage `json:"next_cursor"`
			} `json:"data"`
		}
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil || body.Data == nil || body.Data.Documents == nil || len(body.Data.NextCursor) == 0 {
			report.DocsMetadata = "unavailable"
			report.DocsDetail = "live document metadata could not be read"
			return
		}
		var nextCursor string
		if string(body.Data.NextCursor) != "null" && json.Unmarshal(body.Data.NextCursor, &nextCursor) != nil {
			report.DocsMetadata = "unavailable"
			report.DocsDetail = "live document metadata has an invalid cursor"
			return
		}
		for _, doc := range body.Data.Documents {
			edited, err := parseDocumentEditTime(doc.UpdatedAt)
			if err != nil {
				report.DocsMetadata = "unavailable"
				report.DocsDetail = "live document metadata has an invalid edit time"
				return
			}
			if edited.After(exportTime) {
				report.DocsState, report.ObservedNewerDocumentEdit = "newer_edits", edited.UTC().Format(time.RFC3339Nano)
				report.DocsMetadata = "checked"
				report.DocsDetail = "a live document edit is newer than the local export; other server changes may also exist"
				return
			}
		}
		if nextCursor == "" {
			report.DocsMetadata = "checked"
			report.DocsDetail = "no newer document edit observed; export currency is still unknown"
			return
		}
		cursor = nextCursor
	}
	report.DocsMetadata = "incomplete"
	report.DocsDetail = "document metadata check reached its five-page limit; export currency is unknown"
}

// Ecto's naive_datetime timestamps carry no offset. Core stores them in UTC;
// the document discovery JSON sends that representation without adding Z.
func parseDocumentEditTime(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02T15:04:05.999999999", raw, time.UTC)
}

func checkPrepareAnalysis(cfg *config.Config, report *prepareReport) {
	client := newAuthenticatedClient(cfg)
	client.client.Timeout = prepareReadTimeout
	run, err := readCurrentRun(client, cfg.SystemID)
	if err != nil {
		report.AnalysisDetail = "run status unavailable; run modernpath analysis status"
		return
	}
	if run == nil {
		report.Analysis = "no_run"
		report.AnalysisDetail = "no analysis run recorded"
	} else {
		report.Analysis = run.Run.Status
		report.AnalysisDetail = fmt.Sprintf("run %s: %s", run.Run.ID, run.Run.Status)
	}
	if cfg.RepositoryID == 0 {
		report.AnalysisDetail += "; repository is not recorded in the binding"
		return
	}
	repo, err := readRepositoryStatus(client, cfg.SystemID, cfg.RepositoryID)
	if err != nil {
		report.AnalysisDetail += "; repository status unavailable"
		return
	}
	report.AnalysisDetail += "; repository " + repo.AnalysisStatus
}

func writePrepare(cmd *cobra.Command, report prepareReport) error {
	if flagIsTrue(cmd, "json") {
		return writeCommandJSON(cmd, report)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Session: %s (%s)\n", report.Session, report.Actor)
	fmt.Fprintf(out, "Server: %s\nSystem: %d %s (%s)\n", report.Server, report.SystemID, report.SystemName, report.Access)
	latest := report.LatestCLI
	if latest == "" {
		latest = "unknown"
	}
	fmt.Fprintf(out, "CLI: %s (latest release: %s)\nKit: %s\n", report.CLIVersion, latest, report.Kit)
	fmt.Fprintf(out, "Docs: %s (local sync: %s; export: %s)\n", report.DocsState, orDash(report.DocsSyncAt), orDash(report.ExportGeneratedAt))
	if report.DocsDetail != "" {
		fmt.Fprintln(out, report.DocsDetail)
	}
	fmt.Fprintf(out, "Analysis: %s", report.Analysis)
	if report.AnalysisDetail != "" {
		fmt.Fprintf(out, " (%s)", report.AnalysisDetail)
	}
	fmt.Fprintln(out)
	for _, step := range report.NextSteps {
		fmt.Fprintf(out, "Next: %s\n", step)
	}
	return nil
}
