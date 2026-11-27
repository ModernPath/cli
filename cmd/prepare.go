package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"sync"
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
install, upgrade or sign in.

On a released build it also looks up the latest CLI release on GitHub on every
run. It waits up to 3 seconds for the answer and runs its other checks in the
meantime. It keeps the answer in release-latest.json in the user's cache
folder. The CLI: line and the cli_release object of --json name the release
state: newer (an upgrade is available, its command is the first Next: step and
prepare exits non-zero), current (up to date), local_build (a local build looks
nothing up), unknown (the lookup failed) or off. After a failed lookup, a cache
under 4 hours old that shows a newer release still gives newer, with reason
lookup_failed. The update check is off, with reason ci or opt_out, when CI or
MODERNPATH_NO_UPDATE_CHECK is set to any value except empty, 0, false, no or
off.`,
	Args: cobra.NoArgs, RunE: runPrepare,
}

const prepareReadTimeout = 2 * time.Second

type prepareReport struct {
	Actor                     string            `json:"actor,omitempty"`
	Server                    string            `json:"server"`
	SystemID                  int               `json:"system_id,omitempty"`
	SystemName                string            `json:"system_name,omitempty"`
	Session                   string            `json:"session"`
	SessionDetail             string            `json:"session_detail,omitempty"`
	Access                    string            `json:"access"`
	AccessDetail              string            `json:"access_detail,omitempty"`
	CLIVersion                string            `json:"cli_version"`
	LatestCLI                 string            `json:"latest_cli"`
	CLIRelease                *cliReleaseReport `json:"cli_release,omitempty"`
	Kit                       string            `json:"kit"`
	KitDifferences            []string          `json:"kit_differences,omitempty"`
	DocsState                 string            `json:"docs_state"`
	DocsMetadata              string            `json:"docs_metadata"`
	DocsSyncAt                string            `json:"docs_sync_at,omitempty"`
	ExportGeneratedAt         string            `json:"export_generated_at,omitempty"`
	ObservedNewerDocumentEdit string            `json:"observed_newer_document_edit,omitempty"`
	DocsDetail                string            `json:"docs_detail,omitempty"`
	Analysis                  string            `json:"analysis"`
	AnalysisDetail            string            `json:"analysis_detail,omitempty"`
	NextSteps                 []string          `json:"next_steps"`
}

// cliReleaseReport is the release status in the JSON report: latest and
// checked_at in the states newer and current, a reason for unknown and off,
// and lookup_failed also on a newer state taken from the cache.
type cliReleaseReport struct {
	State     string `json:"state"`
	Latest    string `json:"latest,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

func newCLIReleaseReport(status releaseStatus) *cliReleaseReport {
	report := &cliReleaseReport{State: status.State, Reason: status.Reason}
	if status.State == releaseNewer || status.State == releaseCurrent {
		report.Latest = status.Latest
		report.CheckedAt = status.CheckedAt.UTC().Format(time.RFC3339)
	}
	return report
}

// cliReleaseText is the release state on prepare's CLI: line. It never says
// up to date without a successful lookup, and never a bare unknown.
func cliReleaseText(release *cliReleaseReport) string {
	if release == nil {
		return "latest release unknown: the lookup failed"
	}
	switch release.State {
	case releaseNewer:
		if release.Reason == reasonLookupFailed {
			found := release.CheckedAt
			if at, err := time.Parse(time.RFC3339, release.CheckedAt); err == nil {
				found = at.UTC().Format("2006-01-02T15:04Z")
			}
			return fmt.Sprintf("latest release: %s, found %s; upgrade available; this lookup failed", release.Latest, found)
		}
		return "latest release: " + release.Latest + "; upgrade available"
	case releaseCurrent:
		return "latest release: " + release.Latest + "; up to date"
	case releaseLocalBuild:
		return "local build; update check not used"
	case releaseOff:
		if release.Reason == reasonOptOut {
			return "update check off: MODERNPATH_NO_UPDATE_CHECK is set"
		}
		return "update check off: CI is set"
	}
	return "latest release unknown: the lookup failed"
}

func init() {
	prepareCmd.Flags().Bool("json", false, "Print one bounded JSON report")
	rootCmd.AddCommand(prepareCmd)
}

func runPrepare(cmd *cobra.Command, _ []string) error {
	report := prepareReport{Session: "unknown", Access: "unknown", CLIVersion: Version, LatestCLI: "unknown", Kit: "unknown", DocsState: "unknown", DocsMetadata: "not_checked", Analysis: "unknown", NextSteps: []string{}}
	// Epic 422: the release lookup starts first and runs alongside the other
	// checks, for up to 3 seconds (decision 23). Every path waits for it
	// before it lists its Next: lines, so a folder with no connected
	// repository also gets a named state, the release-dependent lines still
	// come first, and the advice for a released build needs no repository.
	awaitRelease := startPrepareReleaseCheck(&report)
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		report.Session = "unbound"
		report.Access = "unbound"
		if awaitRelease().State == releaseNewer {
			report.NextSteps = append(report.NextSteps, upgradeAdviceForCurrentBuild("")...)
		}
		report.NextSteps = append(report.NextSteps, "modernpath init")
		return finishPrepare(cmd, report)
	}
	root := filepath.Dir(cfgDir)
	cfg, err := commandConfig()
	if err != nil {
		report.SessionDetail = "workspace configuration could not be read"
		if awaitRelease().State == releaseNewer {
			report.NextSteps = append(report.NextSteps, upgradeAdviceForCurrentBuild(root)...)
		}
		report.NextSteps = append(report.NextSteps, "modernpath init --help")
		return finishPrepare(cmd, report)
	}
	report.Server = cfg.APIURL
	if report.Server == "" {
		report.Server = config.DefaultAPIURL
	}
	report.SystemID, report.SystemName, report.DocsSyncAt = cfg.SystemID, cfg.SystemName, cfg.LastSyncAt
	if cfg.SystemID == 0 {
		report.Access = "unbound"
	}
	// The server reads run while the lookup runs. Their Next: lines follow
	// the release-dependent ones, as before.
	sessionSteps := checkPrepareSession(root, cfg, &report)
	release := awaitRelease()
	needsUpgrade := release.State == releaseNewer
	if needsUpgrade {
		report.NextSteps = append(report.NextSteps, upgradeAdviceForCurrentBuild(root)...)
	}
	if cfg.SystemID == 0 {
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
			switch {
			case release.State == releaseUnknown:
				report.NextSteps = append(report.NextSteps, "Check your CLI release channel for a newer version before modernpath install (latest release could not be verified)")
			case release.State == releaseOff:
				report.NextSteps = append(report.NextSteps, "Check your CLI release channel for a newer version before modernpath install (the update check is off)")
			case !releasedBuild(Version):
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
	report.NextSteps = append(report.NextSteps, sessionSteps...)
	return finishPrepare(cmd, report)
}

// startPrepareReleaseCheck starts prepare's update check in a goroutine, with
// 3 seconds for its lookup (decision 23). The function it returns waits for
// the result and puts it into report.
func startPrepareReleaseCheck(report *prepareReport) func() releaseStatus {
	result := make(chan releaseStatus, 1)
	go func(version string) {
		result <- checkRelease(context.Background(), version, releaseLookupWaitLimit)
	}(Version)
	return sync.OnceValue(func() releaseStatus {
		release := <-result
		report.CLIRelease = newCLIReleaseReport(release)
		if release.State == releaseNewer || release.State == releaseCurrent {
			report.LatestCLI = release.Latest
		}
		return release
	})
}

// checkPrepareSession checks the stored credential, the bound System's
// access, the documents and the analysis, and returns their Next: lines in
// order.
func checkPrepareSession(root string, cfg *config.Config, report *prepareReport) []string {
	auth, authErr := config.ReadAuth()
	if authErr != nil {
		report.Session = "unknown"
		report.SessionDetail = "stored credential could not be read"
		localDocsState(root, cfg, report)
		return []string{"modernpath auth status"}
	}
	var steps []string
	st := evaluateAuthStatusAt(report.Server, cfg.SystemID, auth, true, time.Now())
	report.Actor = st.Actor
	report.SessionDetail = st.Detail
	if !st.Authenticated {
		report.Session = "unavailable"
		steps = append(steps, authRepairCommand(report.Server))
	} else if cfg.SystemID > 0 {
		// One scoped system listing verifies both the session and access. The
		// offline evaluation above checks expiry and audience without a second GET.
		systems, accessErr := listSystemsFn(report.Server, auth.Token)
		switch {
		case errors.Is(accessErr, api.ErrUnauthorized):
			report.Session = "unavailable"
			report.SessionDetail = "server rejected the credential"
			steps = append(steps, authRepairCommand(report.Server))
		case accessErr != nil:
			report.Session = "unknown"
			report.SessionDetail = "server verification unavailable"
			report.AccessDetail = "bound System access could not be checked"
			steps = append(steps, "modernpath auth status")
		case systemReachable(systems, cfg.SystemID):
			report.Session = "authenticated"
			report.SessionDetail = "credential accepted by the server"
			report.Access = "reachable"
		default:
			report.Session = "authenticated"
			report.SessionDetail = "credential accepted by the server"
			report.Access = "unreachable"
			report.AccessDetail = systemMismatchMessage(cfg.SystemID, systems)
			steps = append(steps, "modernpath init --system-id <reachable-id>")
		}
	}

	localDocsState(root, cfg, report)
	if report.Access == "reachable" {
		checkLiveDocuments(&authenticatedClient{client: &http.Client{Timeout: prepareReadTimeout}, token: auth.Token, baseURL: report.Server}, cfg.SystemID, report)
		checkPrepareAnalysis(cfg, report)
	}
	if report.DocsState == "newer_edits" || report.DocsState == "missing" {
		steps = append(steps, "modernpath docs sync")
	}
	if report.Access == "reachable" {
		steps = append(steps, "read .modernpath/rdd/skills/rdd-start/SKILL.md")
	}
	return steps
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
// first newer edit or terminal page; a completed scan still cannot prove currency.
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
	seen := make(map[string]bool)
	for {
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
		if seen[nextCursor] {
			report.DocsMetadata = "unavailable"
			report.DocsDetail = "live document metadata repeated a cursor; export currency is unknown"
			return
		}
		seen[nextCursor] = true
		cursor = nextCursor
	}
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
	fmt.Fprintf(out, "CLI: %s (%s)\nKit: %s\n", report.CLIVersion, cliReleaseText(report.CLIRelease), report.Kit)
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
