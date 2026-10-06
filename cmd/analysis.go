package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/manifoldco/promptui"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// REQ-CROSS-503 (EPIC-CLI-029, USER:2026-09-29 D7): `modernpath analysis`
// starts, re-runs, resets and reports the analysis of the bound system
// through the lifecycle routes the UI uses — POST
// /api/systems/:id/git-sources/analyze, …/:repository_id/reanalyze and
// …/reset — and two reads: the repository (REQ-SYS-210 AC8) and the current
// analysis run (REQ-OBAN-005). The run read answers 404 after an import,
// whose worker records no run; status reports that as "no run" (D7). Every
// refusal prints once by its error code and exits non-zero (D4).

const defaultAnalysisMode = "independent_repos"

var analysisModes = []string{"independent_repos", "unified_workspace"}

var (
	analysisStartMode string
	analysisResetMode string
	analysisResetYes  bool
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
needs --yes.`,
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
	cfg, err := config.ReadConfig()
	if err != nil {
		return nil, err
	}
	if apiURL != "" {
		cfg.APIURL = apiURL
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
		Data lifecycleAnswer `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return &envelope.Data, nil
}

func printLifecycleRun(answer *lifecycleAnswer) {
	printSuccess("Analysis run %s: %s\n", answer.Run.ID, answer.Run.Status)
	fmt.Printf("  mode:          %s\n", answer.AnalysisMode)
	if answer.Run.SourceRevision != "" {
		fmt.Printf("  source:        %s\n", shortRevision(answer.Run.SourceRevision))
	}
	if len(answer.Started) > 0 {
		names := make([]string, 0, len(answer.Started))
		for _, repository := range answer.Started {
			names = append(names, fmt.Sprintf("%s (%s)", repository.RepositoryName, repository.RepositoryID))
		}
		fmt.Printf("  repositories:  %s\n", strings.Join(names, ", "))
	}
}

func runAnalysisStart(cmd *cobra.Command, args []string) error {
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
	printLifecycleRun(answer)
	return nil
}

func runAnalysisReanalyze(cmd *cobra.Command, args []string) error {
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
	printLifecycleRun(answer)
	return nil
}

func runAnalysisReset(cmd *cobra.Command, args []string) error {
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
		fmt.Println(analysisResetWipes)
		ok, err := confirmAction("Reset the analysis of "+boundSystemLabel(cfg), false, false)
		if err != nil {
			return reportFailure(err)
		}
		if !ok {
			fmt.Println("Reset cancelled.")
			return nil
		}
	}
	answer, err := postLifecycle(cfg, "reset", body)
	if err != nil {
		return reportFailure(err)
	}
	printLifecycleRun(answer)
	return nil
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
}

// runSnapshot is the current analysis run (REQ-OBAN-005). It carries no
// phase of its own; phase derives one from the steps.
type runSnapshot struct {
	Run struct {
		ID             int    `json:"id"`
		Status         string `json:"status"`
		AnalysisMode   string `json:"analysis_mode"`
		SourceRevision string `json:"source_revision"`
	} `json:"run"`
	Steps []struct {
		Stage        string `json:"stage"`
		State        string `json:"state"`
		RepositoryID *int   `json:"repository_id"`
	} `json:"steps"`
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
		Data repositoryStatus `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse repository %d: %w", repositoryID, err)
	}
	return &envelope.Data, nil
}

// readCurrentRun returns the system's current analysis run, or nil when the
// read answers 404: an import's worker records no run (D7).
func readCurrentRun(client *authenticatedClient, systemID int) (*runSnapshot, error) {
	resp, err := client.Get(fmt.Sprintf("%s/api/systems/%d/analysis/runs/current", client.baseURL, systemID))
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
		Data runSnapshot `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse the analysis run: %w", err)
	}
	return &envelope.Data, nil
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
	fmt.Fprintf(out, "Analysis run %d: %s\n", run.Run.ID, run.Run.Status)
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
