package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
	"github.com/spf13/cobra"
)

// REQ-CROSS-503 (EPIC-CLI-029, USER:2026-09-29 D7): `modernpath analysis`
// starts, re-runs, resets and reports the analysis of the bound system
// through the lifecycle routes the UI uses — POST
// /api/systems/:id/git-sources/analyze, …/:repository_id/reanalyze and
// …/reset — and two reads: the repository (REQ-SYS-210 AC8) and the current
// analysis run (REQ-OBAN-005). An import starts its analysis through the
// same Start, so its run is read like any other; a system with no run
// answers 404, which status reports as "no run" (D7). Every refusal prints
// once by its error code and exits non-zero (D4).

const defaultAnalysisMode = "independent_repos"

var analysisModes = []string{"independent_repos", "unified_workspace"}

var (
	analysisStartMode string
	analysisResetMode string
	analysisResetYes  bool
	analysisJSON      bool
	analysisWait      bool
	analysisTimeout   time.Duration
)

const (
	defaultAnalysisTimeout = 10 * time.Minute
	maxAnalysisTimeout     = 30 * time.Minute
)

// analysisResetWipes is what a reset deletes and keeps
// (Core.Analysis.FullAnalysisReset); the confirmation shows it.
const analysisResetWipes = `A reset cancels a running analysis and deletes the system's analysis results:
file analyses and dependencies, subsystems, capabilities, patterns, findings,
libraries and generated documentation. The repositories, their uploaded
source and planning data are kept. A new analysis starts right after.`

var analysisCmd = &cobra.Command{
	Use:   "analysis",
	Short: "Start, re-run, reset and report the analysis of the bound system",
	Long: `Start, re-run, reset and report the analysis of the system this checkout is
bound to, through the same lifecycle the UI uses.

  start       start the analysis of the system's repositories
  reanalyze   re-run the analysis of one repository
  reset       delete the analysis results and start again
  status      the repository's status, its source and the current run

A server refusal prints its error code and message and exits non-zero, so a
script fails loudly. Only reset asks for a confirmation; without a terminal it
needs --yes. Mutating commands accept --json for native response data and
--wait --timeout to wait up to 30 minutes for that exact run.`,
}

var analysisStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the analysis of the bound system",
	Long: `Start the analysis of the bound system's repositories: the request the UI's
Start analysis button sends. It creates no repository.

--mode is independent_repos (each repository is analysed on its own, the
default) or unified_workspace (the repositories are analysed as one
workspace). When an analysis is already running, its run is printed and no
second one starts. A refusal (the analysis is already complete, the sources
are not ready, the system has no repository) prints its error code and
message and exits non-zero. The command never prompts.

Examples:
  modernpath analysis start
  modernpath analysis start --mode unified_workspace`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE:          runAnalysisStart,
}

var analysisReanalyzeCmd = &cobra.Command{
	Use:   "reanalyze <repository-id>",
	Short: "Re-run the analysis of one repository of the bound system",
	Long: `Re-run the analysis of one repository of the bound system: the request the
UI's Re-analyze action sends. 'modernpath analysis status' prints the bound
repository's id.

A refusal (the repository is not in the system, it is a member of a unified
workspace whose root must be re-analysed instead, the sources are not ready)
prints its error code and message and exits non-zero. The command never
prompts.

Example:
  modernpath analysis reanalyze 7`,
	Args:          cobra.ExactArgs(1),
	SilenceErrors: true,
	RunE:          runAnalysisReanalyze,
}

var analysisResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Delete the analysis results of the bound system and start again",
	Long: `Delete the analysis results of the bound system and start a new analysis: the
request the UI's Reset analysis action sends.

` + analysisResetWipes + `

The command asks for a confirmation first. --yes skips it; without a terminal
--yes is required, so a script never resets by accident. --mode sets the
analysis mode of the new run; without it the server uses the mode the system
was last analysed with (independent_repos when there is none).

Examples:
  modernpath analysis reset
  modernpath analysis reset --yes --mode unified_workspace`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE:          runAnalysisReset,
}

var analysisStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Report the analysis of the bound system and repository",
	Long: `Report the analysis of the bound system: the repository's analysis,
documentation and embedding status, its current source revision and how the
last refresh of that source ended, then the current analysis run with its
status, mode, phase and steps by stage.

The repository is the one .modernpath/config.json records (repository_id,
written by 'modernpath import --local'), else the system's single upload
repository, else the repository whose local path is this directory.

A system with no recorded run, which is the state right after an import,
prints "no analysis run recorded" and is not an error.`,
	Args:          cobra.NoArgs,
	SilenceErrors: true,
	RunE:          runAnalysisStatus,
}

func init() {
	analysisStartCmd.Flags().StringVar(&analysisStartMode, "mode", defaultAnalysisMode, "Analysis mode: independent_repos or unified_workspace")
	analysisResetCmd.Flags().StringVar(&analysisResetMode, "mode", "", "Analysis mode of the new run: independent_repos or unified_workspace (default: the system's last mode)")
	analysisResetCmd.Flags().BoolVarP(&analysisResetYes, "yes", "y", false, "Reset without asking (required without a terminal)")
	for _, command := range []*cobra.Command{analysisStartCmd, analysisReanalyzeCmd, analysisResetCmd} {
		command.Flags().BoolVar(&analysisJSON, "json", false, "print the server response as JSON")
		command.Flags().BoolVar(&analysisWait, "wait", false, "wait for the started analysis run to finish")
		command.Flags().DurationVar(&analysisTimeout, "timeout", defaultAnalysisTimeout, "maximum time to wait (requires --wait; at most 30m)")
	}
	analysisStatusCmd.Flags().BoolVar(&analysisJSON, "json", false, "print status as JSON")
	analysisCmd.AddCommand(analysisStartCmd, analysisReanalyzeCmd, analysisResetCmd, analysisStatusCmd)
	rootCmd.AddCommand(analysisCmd)
}

// errNoBoundSystem refuses a verb that needs a bound system in a checkout
// without one.
var errNoBoundSystem = errors.New("no system is bound here; run 'modernpath init' or 'modernpath import --local' first")

// errUnattendedConfirmation refuses a confirming verb run without a terminal
// and without --yes: a CI job fails loudly instead of exiting 0 with
// "cancelled" (REQ-CROSS-503 AC5, D11, USER:2026-09-30).
var errUnattendedConfirmation = errors.New("this command asks for a confirmation and there is no terminal; --yes is required for unattended runs")

// reportFailure prints err once and returns it marked as printed, so the
// command exits 1 without Execute printing it again (D4). The command sets
// SilenceErrors, or cobra prints it a second time.
func reportFailure(err error) error {
	printError("%v\n", err)
	return reportedError{err}
}

// confirmAction asks label as a yes/no question unless yes is set. Without a
// terminal it returns errUnattendedConfirmation. A declined or interrupted
// prompt returns false and no error: the person chose not to go on.
func confirmAction(label string, yes, defaultYes bool) (bool, error) {
	if yes {
		return true, nil
	}
	if !stdinIsTerminal() {
		return false, errUnattendedConfirmation
	}
	prompt := promptui.Prompt{Label: label, IsConfirm: true}
	if defaultYes {
		prompt.Default = "y"
	}
	result, err := prompt.Run()
	return err == nil && strings.EqualFold(result, "y"), nil
}

func validateAnalysisMode(mode string) error {
	if slices.Contains(analysisModes, mode) {
		return nil
	}
	return fmt.Errorf("unknown --mode %q; use independent_repos or unified_workspace", mode)
}

// analysisConfig is the bound workspace the analysis verbs act on; the
// global --api-url overrides its server, as for `source push`.
func analysisConfig() (*config.Config, error) {
	cfg, err := commandConfig()
	if err != nil {
		return nil, err
	}
	if cfg.SystemID == 0 {
		return nil, errNoBoundSystem
	}
	return cfg, nil
}

func boundSystemLabel(cfg *config.Config) string {
	if cfg.SystemName == "" {
		return fmt.Sprintf("system %d", cfg.SystemID)
	}
	return fmt.Sprintf("%s (system %d)", cfg.SystemName, cfg.SystemID)
}

// lifecycleAnswer is the 202 the lifecycle Start, Reanalyze and Reset send:
// the mode, the repositories started and the run. Their ids are strings.
type lifecycleAnswer struct {
	AnalysisMode string `json:"analysis_mode"`
	Started      []struct {
		RepositoryID   string `json:"repository_id"`
		RepositoryName string `json:"repository_name"`
	} `json:"started"`
	Run struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		SourceRevision string `json:"source_revision"`
	} `json:"run"`
	raw json.RawMessage
}

// startAnalysis posts the lifecycle Start the UI sends. It is system-scoped,
// so no repository is needed or created (REQ-CROSS-503 C3).
func startAnalysis(cfg *config.Config, mode string) (*lifecycleAnswer, error) {
	return postLifecycle(cfg, "analyze", map[string]string{"analysis_mode": mode})
}

// postLifecycle posts body to the bound system's git-sources/<route> and
// returns the answer, or the refusal by its error code and message.
func postLifecycle(cfg *config.Config, route string, body map[string]string) (*lifecycleAnswer, error) {
	client := newAuthenticatedClient(cfg)
	payload, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/systems/%d/git-sources/%s", client.baseURL, cfg.SystemID, route)
	resp, err := client.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, pushRefusal(resp, raw)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var answer lifecycleAnswer
	if err := json.Unmarshal(envelope.Data, &answer); err != nil {
		return nil, fmt.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	if _, err := canonicalRunID(answer.Run.ID); err != nil || strings.TrimSpace(answer.Run.Status) == "" {
		return nil, fmt.Errorf("unexpected response %s: analysis run is missing a valid id or status", resp.Status)
	}
	if answer.AnalysisMode != "independent_repos" && answer.AnalysisMode != "unified_workspace" {
		return nil, fmt.Errorf("unexpected response %s: analysis_mode is missing or invalid", resp.Status)
	}
	answer.raw = envelope.Data
	return &answer, nil
}

func printLifecycleRun(answer *lifecycleAnswer) {
	printLifecycleRunTo(os.Stdout, answer)
}

func printLifecycleRunTo(out io.Writer, answer *lifecycleAnswer) {
	fmt.Fprintf(out, "Analysis run %s: %s\n", answer.Run.ID, answer.Run.Status)
	fmt.Fprintf(out, "  mode:          %s\n", answer.AnalysisMode)
	if answer.Run.SourceRevision != "" {
		fmt.Fprintf(out, "  source:        %s\n", shortRevision(answer.Run.SourceRevision))
	}
	if len(answer.Started) > 0 {
		names := make([]string, 0, len(answer.Started))
		for _, repository := range answer.Started {
			names = append(names, fmt.Sprintf("%s (%s)", repository.RepositoryName, repository.RepositoryID))
		}
		fmt.Fprintf(out, "  repositories:  %s\n", strings.Join(names, ", "))
	}
}

func lifecycleOptions(cmd *cobra.Command) (jsonOutput, wait bool, timeout time.Duration, err error) {
	jsonOutput, err = cmd.Flags().GetBool("json")
	if err != nil {
		return
	}
	wait, err = cmd.Flags().GetBool("wait")
	if err != nil {
		return
	}
	timeout, err = cmd.Flags().GetDuration("timeout")
	if err != nil {
		return
	}
	if !wait && cmd.Flags().Changed("timeout") {
		err = fmt.Errorf("--timeout requires --wait")
		return
	}
	if wait && (timeout <= 0 || timeout > maxAnalysisTimeout) {
		err = fmt.Errorf("--timeout must be greater than 0 and at most %s", maxAnalysisTimeout)
		return
	}
	return
}

func reportLifecycleResult(cmd *cobra.Command, cfg *config.Config, answer *lifecycleAnswer) error {
	jsonOutput, wait, timeout, err := lifecycleOptions(cmd)
	if err != nil {
		return reportFailure(err)
	}
	if !wait {
		if jsonOutput {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), string(answer.raw))
			return err
		}
		printLifecycleRunTo(cmd.OutOrStdout(), answer)
		return nil
	}

	current, err := waitForAnalysisRun(cfg, answer.Run.ID, timeout)
	if err != nil {
		return reportFailure(err)
	}
	if jsonOutput {
		result := struct {
			Started json.RawMessage `json:"started"`
			Current json.RawMessage `json:"current"`
		}{Started: answer.raw, Current: current.raw}
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return err
		}
	} else {
		printLifecycleRunTo(cmd.OutOrStdout(), answer)
		fmt.Fprintf(cmd.OutOrStdout(), "  final status:  %s\n", current.Run.Status)
	}
	if current.Run.Status == "failed" || current.Run.Status == "cancelled" {
		return reportFailure(fmt.Errorf("analysis run %s ended with status %s", answer.Run.ID, current.Run.Status))
	}
	return nil
}

func waitForAnalysisRun(cfg *config.Config, startedID string, timeout time.Duration) (*runSnapshot, error) {
	wantedID, err := canonicalRunID(startedID)
	if err != nil {
		return nil, fmt.Errorf("cannot wait for analysis run with unsupported id %q: %w", startedID, err)
	}
	client := newAuthenticatedClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, err := readCurrentRunContext(ctx, client, cfg.SystemID)
		if err != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("timed out after %s waiting for analysis run %s", timeout, wantedID)
			}
			return nil, err
		}
		if current != nil {
			currentID, err := canonicalRunID(string(current.Run.ID))
			if err != nil {
				return nil, fmt.Errorf("cannot wait for current analysis run with unsupported id %q: %w", current.Run.ID, err)
			}
			if currentID != wantedID {
				return nil, fmt.Errorf("current analysis run changed from %s to %s; refusing to wait on another run", wantedID, currentID)
			}
			if current.Run.Status == "completed" || current.Run.Status == "failed" || current.Run.Status == "cancelled" {
				return current, nil
			}
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timed out after %s waiting for analysis run %s", timeout, wantedID)
		case <-ticker.C:
		}
	}
}

func canonicalRunID(id string) (string, error) {
	number, err := strconv.ParseInt(id, 10, 64)
	if err != nil || number <= 0 {
		return "", fmt.Errorf("expected a positive numeric run id")
	}
	return strconv.FormatInt(number, 10), nil
}

func runAnalysisStart(cmd *cobra.Command, args []string) error {
	if _, _, _, err := lifecycleOptions(cmd); err != nil {
		return reportFailure(err)
	}
	if err := validateAnalysisMode(analysisStartMode); err != nil {
		return reportFailure(err)
	}
	cfg, err := analysisConfig()
	if err != nil {
		return reportFailure(err)
	}
	answer, err := startAnalysis(cfg, analysisStartMode)
	if err != nil {
		return reportFailure(err)
	}
	return reportLifecycleResult(cmd, cfg, answer)
}

func runAnalysisReanalyze(cmd *cobra.Command, args []string) error {
	if _, _, _, err := lifecycleOptions(cmd); err != nil {
		return reportFailure(err)
	}
	repositoryID, err := strconv.Atoi(args[0])
	if err != nil || repositoryID <= 0 {
		return reportFailure(fmt.Errorf("repository id must be a positive number, got %q; 'modernpath analysis status' prints the bound repository's id", args[0]))
	}
	cfg, err := analysisConfig()
	if err != nil {
		return reportFailure(err)
	}
	answer, err := postLifecycle(cfg, fmt.Sprintf("%d/reanalyze", repositoryID), map[string]string{})
	if err != nil {
		return reportFailure(err)
	}
	return reportLifecycleResult(cmd, cfg, answer)
}

func runAnalysisReset(cmd *cobra.Command, args []string) error {
	if _, _, _, err := lifecycleOptions(cmd); err != nil {
		return reportFailure(err)
	}
	body := map[string]string{}
	if analysisResetMode != "" {
		if err := validateAnalysisMode(analysisResetMode); err != nil {
			return reportFailure(err)
		}
		body["analysis_mode"] = analysisResetMode
	}
	cfg, err := analysisConfig()
	if err != nil {
		return reportFailure(err)
	}
	if !analysisResetYes {
		jsonOutput, _, _, optionsErr := lifecycleOptions(cmd)
		if optionsErr != nil {
			return reportFailure(optionsErr)
		}
		if jsonOutput {
			fmt.Fprintln(cmd.ErrOrStderr(), analysisResetWipes)
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), analysisResetWipes)
		}
		var ok bool
		var err error
		if jsonOutput {
			ok, err = confirmActionToStderr("Reset the analysis of " + boundSystemLabel(cfg))
		} else {
			ok, err = confirmAction("Reset the analysis of "+boundSystemLabel(cfg), false, false)
		}
		if err != nil {
			return reportFailure(err)
		}
		if !ok {
			if jsonOutput {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), `{"cancelled":true}`)
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "Reset cancelled.")
			return nil
		}
	}
	answer, err := postLifecycle(cfg, "reset", body)
	if err != nil {
		return reportFailure(err)
	}
	return reportLifecycleResult(cmd, cfg, answer)
}

func confirmActionToStderr(label string) (bool, error) {
	if !stdinIsTerminal() {
		return false, errUnattendedConfirmation
	}
	prompt := promptui.Prompt{Label: label, IsConfirm: true, Stdin: os.Stdin, Stdout: os.Stderr}
	result, err := prompt.Run()
	return err == nil && strings.EqualFold(result, "y"), err
}

// repositoryStatus is what the repository read (REQ-SYS-210 AC8) says about
// the repository's analysis and its current source.
type repositoryStatus struct {
	ID                  int    `json:"id"`
	Name                string `json:"name"`
	AnalysisStatus      string `json:"analysis_status"`
	DocumentationStatus string `json:"documentation_status"`
	EmbeddingStatus     string `json:"embedding_status"`
	SourceRevision      string `json:"source_revision"`
	SourceKind          string `json:"source_kind"`
	SourceSealedAt      string `json:"source_sealed_at"`
	SourceRefresh       *struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
		At     string `json:"at"`
	} `json:"source_refresh"`
	raw json.RawMessage
}

// runSnapshot is the current analysis run (REQ-OBAN-005). It carries no
// phase of its own; phase derives one from the steps.
type runSnapshot struct {
	Run struct {
		ID             analysisRunID `json:"id"`
		Status         string        `json:"status"`
		AnalysisMode   string        `json:"analysis_mode"`
		SourceRevision string        `json:"source_revision"`
	} `json:"run"`
	Steps []struct {
		Stage        string `json:"stage"`
		State        string `json:"state"`
		RepositoryID *int   `json:"repository_id"`
	} `json:"steps"`
	raw json.RawMessage
}

type analysisRunID string

func (id *analysisRunID) UnmarshalJSON(data []byte) error {
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		*id = analysisRunID(value)
		return nil
	}
	if !json.Valid(data) {
		return fmt.Errorf("invalid run id")
	}
	*id = analysisRunID(string(data))
	return nil
}

func runAnalysisStatus(cmd *cobra.Command, args []string) error {
	cfg, err := analysisConfig()
	if err != nil {
		return reportFailure(err)
	}
	repositoryID, err := resolveBoundRepository(cfg)
	if err != nil {
		return reportFailure(err)
	}
	client := newAuthenticatedClient(cfg)
	repository, err := readRepositoryStatus(client, cfg.SystemID, repositoryID)
	if err != nil {
		return reportFailure(err)
	}
	run, err := readCurrentRun(client, cfg.SystemID)
	if err != nil {
		return reportFailure(err)
	}
	if analysisJSON {
		var rawRun json.RawMessage
		if run != nil {
			rawRun = run.raw
		}
		result := struct {
			System struct {
				ID   int    `json:"id"`
				Name string `json:"name"`
			} `json:"system"`
			Repository json.RawMessage `json:"repository"`
			Run        json.RawMessage `json:"run"`
		}{}
		result.System.ID = cfg.SystemID
		result.System.Name = cfg.SystemName
		result.Repository = repository.raw
		result.Run = rawRun
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return err
		}
		return nil
	}
	printAnalysisStatus(cmd.OutOrStdout(), cfg, repository, run)
	return nil
}

func readRepositoryStatus(client *authenticatedClient, systemID, repositoryID int) (*repositoryStatus, error) {
	resp, err := client.Get(fmt.Sprintf("%s/api/systems/%d/repositories/%d", client.baseURL, systemID, repositoryID))
	if err != nil {
		return nil, fmt.Errorf("failed to read repository %d: %w", repositoryID, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("repository %d was not found in system %d; check repository_id in .modernpath/config.json", repositoryID, systemID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, pushRefusal(resp, raw)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse repository %d: %w", repositoryID, err)
	}
	var repository repositoryStatus
	if err := json.Unmarshal(envelope.Data, &repository); err != nil {
		return nil, fmt.Errorf("failed to parse repository %d: %w", repositoryID, err)
	}
	repository.raw = envelope.Data
	return &repository, nil
}

// readCurrentRun returns the system's current analysis run, or nil when the
// read answers 404: the system has no run yet.
func readCurrentRun(client *authenticatedClient, systemID int) (*runSnapshot, error) {
	return readCurrentRunContext(context.Background(), client, systemID)
}

func readCurrentRunContext(ctx context.Context, client *authenticatedClient, systemID int) (*runSnapshot, error) {
	url := fmt.Sprintf("%s/api/systems/%d/analysis/runs/current", client.baseURL, systemID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to read the analysis run: %w", err)
	}
	platform.Prepare(req)
	req.Header.Set("Accept", "application/json")
	if err := platform.Authorize(req, client.token); err != nil {
		return nil, err
	}
	resp, err := client.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to read the analysis run: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, pushRefusal(resp, raw)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse the analysis run: %w", err)
	}
	var snapshot runSnapshot
	if err := json.Unmarshal(envelope.Data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to parse the analysis run: %w", err)
	}
	snapshot.raw = envelope.Data
	return &snapshot, nil
}

func printAnalysisStatus(out io.Writer, cfg *config.Config, repository *repositoryStatus, run *runSnapshot) {
	fmt.Fprintf(out, "System:         %s\n", boundSystemLabel(cfg))
	fmt.Fprintf(out, "Repository %d (%s)\n", repository.ID, repository.Name)
	fmt.Fprintf(out, "  analysis:       %s\n", orDash(repository.AnalysisStatus))
	fmt.Fprintf(out, "  documentation:  %s\n", orDash(repository.DocumentationStatus))
	fmt.Fprintf(out, "  embedding:      %s\n", orDash(repository.EmbeddingStatus))
	fmt.Fprintf(out, "  source:         %s\n", repository.sourceLine())
	fmt.Fprintf(out, "  refresh:        %s\n", repository.refreshLine())
	if run == nil {
		fmt.Fprintln(out, "Analysis run:   no analysis run recorded")
		return
	}
	fmt.Fprintf(out, "Analysis run %s: %s\n", run.Run.ID, run.Run.Status)
	fmt.Fprintf(out, "  mode:           %s\n", orDash(run.Run.AnalysisMode))
	fmt.Fprintf(out, "  source:         %s\n", orDash(shortRevision(run.Run.SourceRevision)))
	fmt.Fprintf(out, "  phase:          %s\n", run.phase())
	if lines := run.stepLines(); len(lines) > 0 {
		fmt.Fprintln(out, "  steps:")
		for _, line := range lines {
			fmt.Fprintf(out, "    %s\n", line)
		}
	}
}

func (r *repositoryStatus) sourceLine() string {
	if r.SourceRevision == "" {
		return "none sealed"
	}
	var details []string
	if r.SourceKind != "" {
		details = append(details, r.SourceKind)
	}
	if r.SourceSealedAt != "" {
		details = append(details, "sealed "+r.SourceSealedAt)
	}
	if len(details) == 0 {
		return shortRevision(r.SourceRevision)
	}
	return fmt.Sprintf("%s (%s)", shortRevision(r.SourceRevision), strings.Join(details, ", "))
}

// refreshLine is how the last refresh of the current source ended: the
// mark the refresh run wrote on its manifest, if any.
func (r *repositoryStatus) refreshLine() string {
	if r.SourceRefresh == nil || r.SourceRefresh.Status == "" {
		return "none recorded"
	}
	line := r.SourceRefresh.Status
	if r.SourceRefresh.At != "" {
		line += " at " + r.SourceRefresh.At
	}
	if r.SourceRefresh.Reason != "" {
		line += " (" + r.SourceRefresh.Reason + ")"
	}
	return line
}

// phase is the stage the run is in: the stage of its latest running step,
// else of its latest step.
func (s *runSnapshot) phase() string {
	for i := len(s.Steps) - 1; i >= 0; i-- {
		if s.Steps[i].State == "running" {
			return s.Steps[i].Stage
		}
	}
	if n := len(s.Steps); n > 0 {
		return s.Steps[n-1].Stage
	}
	return "-"
}

// stepLines counts the run's steps by stage and repository, in the order the
// run opened them: a run holds one module step per module, too many to list.
func (s *runSnapshot) stepLines() []string {
	type group struct {
		label  string
		counts map[string]int
	}
	var groups []*group
	byLabel := map[string]*group{}
	for _, step := range s.Steps {
		label := step.Stage
		if step.RepositoryID != nil {
			label = fmt.Sprintf("%s, repository %d", step.Stage, *step.RepositoryID)
		}
		g, ok := byLabel[label]
		if !ok {
			g = &group{label: label, counts: map[string]int{}}
			byLabel[label] = g
			groups = append(groups, g)
		}
		g.counts[step.State]++
	}
	lines := make([]string, 0, len(groups))
	for _, g := range groups {
		var parts []string
		for _, state := range []string{"running", "queued", "completed", "failed", "cancelled"} {
			if n := g.counts[state]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, state))
			}
		}
		lines = append(lines, fmt.Sprintf("%s: %s", g.label, strings.Join(parts, ", ")))
	}
	return lines
}
