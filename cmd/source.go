package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// REQ-SYS-211 (EPIC-ANALYSIS-009): `modernpath source push` keeps an
// upload-sourced system current from a network the platform cannot reach.
// It re-packs the working directory with exactly the import filters, computes
// the content revision the server uses, and posts the archive to the bound
// system's upload repository (REQ-SYS-210) — uploading only when the content
// changed, and carrying the checkout's git revision as metadata. It never
// prompts, so it runs from cron or CI.

var (
	sourcePushMaxSizeMB int
	sourcePushExclude   []string
	sourcePushNoIgnore  bool
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
(skip lists, .gitignore, --exclude, --max-size) and its content revision is
compared with the server's before anything is uploaded:

  unchanged, refresh completed   nothing is uploaded or re-analysed (exit 0)
  unchanged, refresh not done    the refresh is queued again, no upload (exit 0)
  changed                        the archive is uploaded, the previous source
                                 is superseded and an incremental refresh of
                                 the knowledge core is queued (exit 0)

A server refusal — the repository is being analysed, or it is linked to a
git provider the platform refreshes itself — exits non-zero with the
server's error code and message, so a CI job fails loudly. The command never
prompts. The API URL and token come from the workspace config and
environment, as for every other verb.

Examples:
  modernpath source push
  modernpath source push --exclude fixtures --max-size 200`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		if apiURL != "" {
			cfg.APIURL = apiURL
		}
		if cfg.SystemID == 0 {
			printError("No bound system. Run 'modernpath import --local' here first.\n")
			return fmt.Errorf("no bound system")
		}
		root, err := pushRoot()
		if err != nil {
			printError("%v\n", err)
			return err
		}
		opts := sourcePushOptions{
			Exclude:     sourcePushExclude,
			MaxSizeMB:   sourcePushMaxSizeMB,
			NoGitignore: sourcePushNoIgnore,
			Root:        root,
		}
		if err := sourcePush(cfg, opts, os.Stdout); err != nil {
			printError("%v\n", err)
			return err
		}
		return nil
	},
}

func init() {
	sourcePushCmd.Flags().IntVar(&sourcePushMaxSizeMB, "max-size", defaultMaxSizeMB, "Maximum upload size in MB")
	sourcePushCmd.Flags().StringArrayVar(&sourcePushExclude, "exclude", nil, "Exclude paths matching a name, path glob or basename glob (repeatable)")
	sourcePushCmd.Flags().BoolVar(&sourcePushNoIgnore, "no-gitignore", false, "Upload files that .gitignore excludes (off by default)")
	sourceCmd.AddCommand(sourcePushCmd)
	rootCmd.AddCommand(sourceCmd)
}

// sourcePushOptions are the import filters plus the directory to pack.
type sourcePushOptions struct {
	Exclude     []string
	MaxSizeMB   int
	NoGitignore bool
	// Root is the tree to pack: the directory the binding lives in, never
	// merely the working directory (see pushRoot). Empty means the working
	// directory, for callers that resolved it already.
	Root string
}

func (o sourcePushOptions) filters() importFilterOptions {
	return importFilterOptions{Exclude: o.Exclude, MaxSizeMB: o.MaxSizeMB, NoGitignore: o.NoGitignore}
}

// pushRoot is the directory `import --local` bound: the parent of the
// `.modernpath` directory ReadConfig resolves. The config is found by walking
// up from the working directory, so a push run from a subdirectory (a CI
// step with a working-directory, a developer in services/api) would
// otherwise pack the subtree alone and, since its digest differs, replace
// the repository's whole source with it.
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

// sourceState is what the repository read (REQ-SYS-210 AC8) says about the
// server's current source: enough to decide before packing whether to
// upload, to ask for a retry, or to do nothing.
type sourceState struct {
	ID            int    `json:"id"`
	Provider      string `json:"provider"`
	RepositoryURL string `json:"repository_url"`
	Revision      string `json:"source_revision"`
	Metadata      struct {
		Trigger string `json:"trigger"`
	} `json:"source_metadata"`
	Refresh *struct {
		Status string `json:"status"`
	} `json:"source_refresh"`
}

// pushResult is the endpoint's whole response vocabulary (REQ-SYS-210 AC9).
type pushResult struct {
	Result           string `json:"result"`
	Revision         string `json:"revision"`
	PreviousRevision string `json:"previous_revision"`
	RefreshJobID     int    `json:"refresh_job_id"`
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

	// AC1: exactly the import filters.
	files, report, err := collectImportFiles(root, opts.filters())
	if err != nil {
		return err
	}
	if report.TotalSize > int64(opts.MaxSizeMB)*1024*1024 {
		return fmt.Errorf("directory too large (%.2f MB > %d MB limit); narrow it with --exclude or raise --max-size",
			float64(report.TotalSize)/(1024*1024), opts.MaxSizeMB)
	}
	local, err := treeDigest(files)
	if err != nil {
		return err
	}

	pushURL := fmt.Sprintf("%s/api/systems/%d/repositories/%d/source-archive", client.baseURL, cfg.SystemID, repositoryID)

	// AC2: the same tree.
	if remote.Revision == local {
		if !remote.retryable() {
			fmt.Fprintf(out, "unchanged at %s\n", shortRevision(local))
			return nil
		}
		body, _ := json.Marshal(map[string]string{"revision": local})
		result, err := postPush(client, pushURL, "application/json", bytes.NewReader(body))
		if err != nil {
			return err
		}
		return reportPush(out, result, local)
	}

	// Changed: pack and upload with the checkout's revision as metadata (AC3).
	archive, err := zipFiles(files)
	if err != nil {
		return err
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
	fmt.Fprintf(out, "pushing %d files (%.2f MB) at %s\n", len(files), float64(archive.Len())/(1024*1024), shortRevision(local))
	result, err := postPush(client, pushURL, form.FormDataContentType(), &body)
	if err != nil {
		return err
	}
	return reportPush(out, result, local)
}

// A same-tree push retries only a push-sealed manifest whose own refresh did
// not complete (USER:2026-09-25); an import-sealed one never retries.
func (s sourceState) retryable() bool {
	if s.Metadata.Trigger == "" || s.Metadata.Trigger == "import" {
		return false
	}
	return s.Refresh == nil || s.Refresh.Status != "completed"
}

func reportPush(out io.Writer, result *pushResult, local string) error {
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
		return fmt.Errorf("unexpected result %q from the server (revision %s)", result.Result, shortRevision(local))
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
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, pushRefusal(resp, raw)
	}
	var result pushResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("unexpected response %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	return &result, nil
}

// AC4: the server's error code and message, so a CI job fails loudly; on
// source_active the time the repository has been busy since, so the customer
// knows the refusal lifts after the platform's eight-hour bound at the latest.
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

// treeDigest is the content revision the server computes for the same tree
// (Core.Storage.Digest.tree/2): SHA-256 over, in byte-sorted path order,
// each slash-separated relative path followed by the lower-case hex SHA-256
// of the file's contents.
func treeDigest(files []importFile) (string, error) {
	sorted := append([]importFile(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].rel < sorted[j].rel })
	hash := sha256.New()
	for _, file := range sorted {
		contents, err := os.ReadFile(file.abs)
		if err != nil {
			continue // zipFiles skips what it cannot read; the digest covers the same set
		}
		sum := sha256.Sum256(contents)
		hash.Write([]byte(file.rel))
		hash.Write([]byte(hex.EncodeToString(sum[:])))
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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
