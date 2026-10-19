package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// REQ-SYS-211 (EPIC-ANALYSIS-009): `modernpath source push` keeps an
// upload-sourced system current from a network the platform cannot reach.
// It packs the selected source files and posts the archive to the bound
// system's upload repository (REQ-SYS-210), carrying the checkout's git
// revision as metadata. The server computes the source revision. It never
// prompts, so it runs from cron or CI.

var (
	sourcePushMaxSizeMB int
	sourcePushExclude   []string
	sourcePushKeep      []string
	sourcePushNoIgnore  bool
	sourcePushJSON      bool
)

var sourceCmd = &cobra.Command{
	Use:   "source",
	Short: "The bound system's uploaded source",
	Long: `The bound system's uploaded source: a system created by ` + "`modernpath import --local`" + `
holds a copy of the tree the CLI packed. ` + "`source push`" + ` sends the current tree
so the knowledge core describes the new code.`,
}

var sourcePushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the working directory to the bound system's upload repository",
	Long: `Push the working directory as the bound system's current source.

The tree is packed with the same filters as ` + "`modernpath import --local`" + `
(skip lists, .gitignore, --exclude, --keep, --max-size), and the scan report
says what each filter left out. The selected archive is always sent; the server
ingests it, computes its revision and reports whether it changed.

--max-size bounds the compressed zip. Before uploading, the request is
compared with the server's upload limit; one that does not fit is refused
without sending, with the zip size, the limit, the largest top-level
directories and an --exclude example.

A server refusal — the system is being analysed or its documentation
updated, or the repository is linked to a git provider the platform
refreshes itself — exits non-zero with the
server's error code and message, so a CI job fails loudly. The command never
prompts. The API URL and token come from the workspace config and
environment, as for every other verb.

Examples:
  modernpath source push
  modernpath source push --exclude fixtures --max-size 200`,
	// Each error is printed once, by RunE (REQ-CROSS-500, D4).
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := commandConfig()
		if err != nil {
			return reportFailure(err)
		}
		if cfg.SystemID == 0 {
			return reportFailure(fmt.Errorf("no bound system; run 'modernpath import --local' here first"))
		}
		root, err := pushRoot()
		if err != nil {
			return reportFailure(err)
		}
		opts := sourcePushOptions{
			Exclude:     sourcePushExclude,
			Keep:        sourcePushKeep,
			MaxSizeMB:   sourcePushMaxSizeMB,
			NoGitignore: sourcePushNoIgnore,
			JSON:        sourcePushJSON,
			Root:        root,
		}
		if err := sourcePush(cfg, opts, os.Stdout); err != nil {
			return reportFailure(err)
		}
		return nil
	},
}

func init() {
	sourcePushCmd.Flags().IntVar(&sourcePushMaxSizeMB, "max-size", defaultMaxSizeMB, maxSizeUsage)
	sourcePushCmd.Flags().StringArrayVar(&sourcePushExclude, "exclude", nil, excludeUsage)
	sourcePushCmd.Flags().StringArrayVar(&sourcePushKeep, "keep", nil, keepUsage)
	sourcePushCmd.Flags().BoolVar(&sourcePushNoIgnore, "no-gitignore", false, noIgnoreUsage)
	sourcePushCmd.Flags().BoolVar(&sourcePushJSON, "json", false, "print the server response as JSON")
	sourceCmd.AddCommand(sourcePushCmd)
	rootCmd.AddCommand(sourceCmd)
}

// sourcePushOptions are the import filters plus the directory to pack.
type sourcePushOptions struct {
	Exclude     []string
	Keep        []string
	MaxSizeMB   int
	NoGitignore bool
	JSON        bool
	// Root is the tree to pack: the directory the binding lives in, never
	// merely the working directory (see pushRoot). Empty means the working
	// directory, for callers that resolved it already.
	Root string
}

func (o sourcePushOptions) filters() importFilterOptions {
	return importFilterOptions{Exclude: o.Exclude, Keep: o.Keep, MaxSizeMB: o.MaxSizeMB, NoGitignore: o.NoGitignore}
}

// pushRoot is the directory `import --local` bound: the parent of the
// `.modernpath` directory ReadConfig resolves. The config is found by walking
// up from the working directory, so a push run from a subdirectory (a CI
// step with a working-directory, a developer in services/api) would
// otherwise upload only that subtree and replace the repository's source.
func pushRoot() (string, error) {
	bindingDir, err := config.BindingDir()
	if err != nil {
		return "", err
	}
	if bindingDir == "" {
		return "", fmt.Errorf("no .modernpath binding found here or above; run 'modernpath import --local' in the checkout first")
	}
	root := filepath.Dir(bindingDir)
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if root != cwd {
		fmt.Fprintf(os.Stderr, "packing the bound checkout %s, not the working directory\n", root)
	}
	return root, nil
}

// sourceState provides the repository URL needed to reject provider-linked
// repositories, which the platform refreshes itself.
type sourceState struct {
	RepositoryURL string `json:"repository_url"`
}

// pushResult is the endpoint's whole response vocabulary (REQ-SYS-210 AC9).
type pushResult struct {
	Result           string `json:"result"`
	Revision         string `json:"revision"`
	PreviousRevision string `json:"previous_revision"`
	RefreshJobID     int    `json:"refresh_job_id"`
	raw              []byte
}

func sourcePush(cfg *config.Config, opts sourcePushOptions, out io.Writer) error {
	root := opts.Root
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		root = cwd
	}
	var err error
	client := newAuthenticatedClient(cfg)

	// AC5/AC6: the repository is the one import recorded, or — for a config
	// that predates the field — the system's single upload repository.
	repositoryID := cfg.RepositoryID
	if repositoryID == 0 {
		repositoryID, err = resolveUploadRepository(client, cfg.SystemID)
		if err != nil {
			return err
		}
	}

	remote, err := fetchSourceState(client, cfg.SystemID, repositoryID)
	if err != nil {
		return err
	}
	if remote.RepositoryURL != "" {
		return fmt.Errorf("repository %d is linked to a git provider (%s); the platform refreshes it from there, not from a push",
			repositoryID, remote.RepositoryURL)
	}

	progress := out
	if opts.JSON {
		progress = os.Stderr
	}

	// AC1: exactly the import filters, reported as import reports them
	// (REQ-CROSS-501).
	files, report, err := collectImportFiles(root, opts.filters())
	if err != nil {
		return err
	}
	printImportReport(progress, report)

	pushURL := fmt.Sprintf("%s/api/systems/%d/repositories/%d/source-archive", client.baseURL, cfg.SystemID, repositoryID)

	// The server ingests the archive and decides whether it changes source.
	archive, err := zipFiles(files)
	if err != nil {
		return err
	}
	// REQ-CROSS-500: --max-size bounds the zip; the body must fit the
	// server's upload limit before anything is sent.
	sizes := uploadSizes{command: "modernpath source push", zip: archive.Bytes()}
	if int64(archive.Len()) > int64(opts.MaxSizeMB)*1024*1024 {
		return sizes.overMaxSize(opts.MaxSizeMB)
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for key, value := range gitRevisionMetadata(root) {
		_ = form.WriteField(key, value)
	}
	_ = form.WriteField("captured_at", time.Now().UTC().Format(time.RFC3339))
	part, err := form.CreateFormFile("file", "source.zip")
	if err != nil {
		return err
	}
	if _, err := part.Write(archive.Bytes()); err != nil {
		return err
	}
	if err := form.Close(); err != nil {
		return err
	}
	sizes.body = int64(body.Len())
	capabilities, err := readUploadCapabilities(client)
	if err != nil {
		return err
	}
	limit := capabilities.limit
	if limit.note != "" {
		printWarning("%s\n", limit.note)
	}
	if sizes.body > limit.bytes {
		return sizes.overLimit(limit.bytes)
	}
	fmt.Fprintf(progress, "pushing %d files (%.2f MB)\n", len(files), float64(archive.Len())/(1024*1024))
	result, err := postPush(client, pushURL, form.FormDataContentType(), &body)
	var tooLarge bodyTooLargeError
	if errors.As(err, &tooLarge) {
		return sizes.refused(tooLarge.limitOr(limit.bytes))
	}
	if err != nil {
		return err
	}
	return reportPush(out, result, opts.JSON)
}

func reportPush(out io.Writer, result *pushResult, jsonOutput bool) error {
	if jsonOutput {
		_, err := out.Write(append(result.raw, '\n'))
		return err
	}
	switch result.Result {
	case "unchanged":
		fmt.Fprintf(out, "unchanged at %s\n", shortRevision(result.Revision))
	case "refresh_retried":
		fmt.Fprintf(out, "unchanged at %s; refresh re-queued (job %d)\n", shortRevision(result.Revision), result.RefreshJobID)
	case "superseded":
		previous := "none"
		if result.PreviousRevision != "" {
			previous = shortRevision(result.PreviousRevision)
		}
		fmt.Fprintf(out, "pushed %s (previous %s); refresh queued (job %d)\n", shortRevision(result.Revision), previous, result.RefreshJobID)
	default:
		return fmt.Errorf("unexpected result %q from the server (revision %s)", result.Result, shortRevision(result.Revision))
	}
	return nil
}

func postPush(client *authenticatedClient, url, contentType string, body io.Reader) (*pushResult, error) {
	resp, err := client.Post(url, contentType, body)
	if err != nil {
		return nil, fmt.Errorf("push failed: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		return nil, bodyTooLarge(raw)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, pushRefusal(resp, raw)
	}
	var result pushResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	result.raw = raw
	return &result, nil
}

// AC4: the server's error code and message, so a CI job fails loudly; on
// source_active the time the system's active run started.
func pushRefusal(resp *http.Response, raw []byte) error {
	var refusal struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		BusySince string `json:"busy_since"`
	}
	if err := json.Unmarshal(raw, &refusal); err != nil || refusal.Error == "" {
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	// The server's message already names the time on source_active; the
	// suffix is for a message that does not.
	if refusal.BusySince != "" && !strings.Contains(refusal.Message, refusal.BusySince) {
		return fmt.Errorf("%s: %s (busy since %s)", refusal.Error, refusal.Message, refusal.BusySince)
	}
	return fmt.Errorf("%s: %s", refusal.Error, refusal.Message)
}

func fetchSourceState(client *authenticatedClient, systemID, repositoryID int) (*sourceState, error) {
	resp, err := client.Get(fmt.Sprintf("%s/api/systems/%d/repositories/%d", client.baseURL, systemID, repositoryID))
	if err != nil {
		return nil, fmt.Errorf("failed to read repository %d: %w", repositoryID, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, pushRefusal(resp, raw)
	}
	var envelope struct {
		Data sourceState `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("failed to parse repository %d: %w", repositoryID, err)
	}
	return &envelope.Data, nil
}

// The system's one repository with no URL; a system whose repositories are
// all provider-linked is refused before anything is packed (AC5).
func resolveUploadRepository(client *authenticatedClient, systemID int) (int, error) {
	resp, err := client.Get(fmt.Sprintf("%s/api/systems/%d", client.baseURL, systemID))
	if err != nil {
		return 0, fmt.Errorf("failed to read system %d: %w", systemID, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return 0, pushRefusal(resp, raw)
	}
	var system struct {
		Repositories []struct {
			ID            int    `json:"id"`
			Name          string `json:"name"`
			Provider      string `json:"provider"`
			RepositoryURL string `json:"repository_url"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(raw, &system); err != nil {
		return 0, fmt.Errorf("failed to parse system %d: %w", systemID, err)
	}
	var uploads []int
	for _, repository := range system.Repositories {
		if repository.RepositoryURL == "" {
			uploads = append(uploads, repository.ID)
		}
	}
	switch len(uploads) {
	case 1:
		return uploads[0], nil
	case 0:
		return 0, fmt.Errorf("system %d has no upload repository: its repositories are linked to a git provider, which the platform refreshes itself (scheduled refresh)", systemID)
	default:
		return 0, fmt.Errorf("system %d has %d upload repositories; set repository_id in .modernpath/config.json", systemID, len(uploads))
	}
}

// AC3: HEAD, the branch and whether the tree is dirty when the directory is a
// git checkout; nothing when it is not.
func gitRevisionMetadata(dir string) map[string]string {
	head := gitOut(dir, "rev-parse", "HEAD")
	if head == "" {
		return nil
	}
	status := exec.Command("git", "status", "--porcelain")
	status.Dir = dir
	statusOut, statusErr := status.Output()
	dirty := statusErr == nil && len(bytes.TrimSpace(statusOut)) > 0
	return map[string]string{
		"git_head":   head,
		"git_branch": gitOut(dir, "rev-parse", "--abbrev-ref", "HEAD"),
		"git_dirty":  strconv.FormatBool(dirty),
	}
}

func shortRevision(revision string) string {
	if len(revision) > 12 {
		return revision[:12]
	}
	return revision
}
