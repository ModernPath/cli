package api

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/platform"
)

const exportDownloadTimeout = 30 * time.Minute

var exportPollInterval = 2 * time.Second

// SetExportPollInterval changes how often an export job is polled and returns
// a function that restores the previous interval. Tests in other packages use
// it so a fake export server does not cost two real seconds per sync.
func SetExportPollInterval(d time.Duration) (restore func()) {
	previous := exportPollInterval
	exportPollInterval = d
	return func() { exportPollInterval = previous }
}

// REQ-CROSS-176: how long an unchanged export status may stay silent. A big
// export sits in "running" for minutes; without a periodic report the wait
// is indistinguishable from a hang, and users kill syncs that were working.
var exportHeartbeatEvery = 15 * time.Second

// ErrUnauthorized marks a response the server answered 401: the request was
// well-formed and the credential was not accepted. Callers render it as a
// statement about the credential rather than as the status line.
var ErrUnauthorized = errors.New("the server rejected the credential (HTTP 401)")

// NewAuthenticatedRequest creates an HTTP request with auth token from config.
// This is a convenience function for commands that don't use the full API client.
func NewAuthenticatedRequest(method, url string, body io.Reader) (*http.Request, error) {
	auth, _ := config.ReadAuth()
	token := ""
	if auth != nil {
		token = auth.Token
	}
	return NewRequestWithToken(method, url, body, token)
}

// NewRequestWithToken creates an HTTP request carrying the given bearer; an
// empty token attaches no header. The caller has already decided the token is
// present and fresh — the api-client verbs check that before any request.
func NewRequestWithToken(method, url string, body io.Reader, token string) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if token != "" {
		if err := platform.Authorize(req, token); err != nil {
			return nil, err
		}
	}

	return req, nil
}

// DoGetWithToken performs a GET carrying the given bearer.
func DoGetWithToken(url, token string, timeout time.Duration) (*http.Response, error) {
	req, err := NewRequestWithToken("GET", url, nil, token)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

// DoPostWithToken performs a POST carrying the given bearer.
func DoPostWithToken(url string, body io.Reader, token string, timeout time.Duration) (*http.Response, error) {
	req, err := NewRequestWithToken("POST", url, body, token)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

// DoAuthenticatedGet performs an authenticated GET request
func DoAuthenticatedGet(url string, timeout time.Duration) (*http.Response, error) {
	req, err := NewAuthenticatedRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

// DoAuthenticatedPost performs an authenticated POST request
func DoAuthenticatedPost(url string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	req, err := NewAuthenticatedRequest("POST", url, body)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: timeout}
	return client.Do(req)
}

// DoAuthenticatedPostRaw performs an authenticated POST request (alias for DoAuthenticatedPost)
func DoAuthenticatedPostRaw(url string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	return DoAuthenticatedPost(url, body, timeout)
}

// System represents a ModernPath system
type System struct {
	ID               int               `json:"id"`
	Name             string            `json:"name"`
	Slug             string            `json:"slug"`
	Description      string            `json:"description"`
	SystemType       string            `json:"system_type"`
	ArchitectureType string            `json:"architecture_type"`
	Status           string            `json:"status"`
	AISummary        string            `json:"ai_summary"`
	AnalysisMode     string            `json:"analysis_mode"`
	WorkspaceMembers []WorkspaceMember `json:"workspace_members"`
}

// WorkspaceMember describes one repository in a unified workspace system.
type WorkspaceMember struct {
	Slug        string `json:"slug"`
	FullName    string `json:"full_name"`
	DisplayName string `json:"display_name"`
}

// Client is the ModernPath API client
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// NewClient creates a new API client
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		cfg, _ := config.ReadConfig()
		if cfg != nil && cfg.APIURL != "" {
			baseURL = cfg.APIURL
		} else {
			baseURL = config.DefaultAPIURL
		}
	}

	if token == "" {
		auth, _ := config.ReadAuth()
		if auth != nil {
			token = auth.Token
		}
	}

	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) doRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := c.BaseURL + path

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if err := platform.Authorize(req, c.Token); err != nil {
		return nil, err
	}

	return c.HTTPClient.Do(req)
}

// HealthCheck checks if the API is reachable.
//
// It carries the stored bearer, exactly as doRequest does: the shared platform
// Gateway fails closed and authenticates every route it fronts, core's health
// route included, so a probe sent without the bearer is answered 401 no matter
// how healthy the server is (REQ-CROSS-290/405). An empty token attaches no
// header, leaving an unauthenticated dev host's public /_health unchanged.
func (c *Client) HealthCheck() error {
	req, err := http.NewRequest("GET", c.BaseURL+platform.HealthPath(c.BaseURL), nil)
	if err != nil {
		return fmt.Errorf("cannot reach API: %w", err)
	}
	platform.Prepare(req)
	if err := platform.Authorize(req, c.Token); err != nil {
		return err
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		// The server answered — it is reachable, and it rejected the credential.
		// Surface that so callers render the credential statement instead of a
		// bare status line and a misleading "server is down" hint (REQ-CROSS-405:
		// a served 401 never renders as `HTTP 401` alone). Reachable-but-rejected
		// is a separate answer from unreachable.
		return fmt.Errorf("health check: %w", ErrUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("API health check failed: HTTP %d", resp.StatusCode)
	}

	return nil
}

// ListSystems returns all available systems
func (c *Client) ListSystems() ([]System, error) {
	resp, err := c.doRequest("GET", "/api/systems", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("failed to list systems: %w", ErrUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list systems: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var systems []System
	if err := json.NewDecoder(resp.Body).Decode(&systems); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	for i := range systems {
		if systems[i].SystemType == "" {
			systems[i].SystemType = systems[i].ArchitectureType
		}
	}

	return systems, nil
}

// GetSystem returns a specific system
func (c *Client) GetSystem(id int) (*System, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("/api/systems/%d", id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get system: HTTP %d", resp.StatusCode)
	}

	var sys System
	if err := json.NewDecoder(resp.Body).Decode(&sys); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if sys.SystemType == "" {
		sys.SystemType = sys.ArchitectureType
	}

	return &sys, nil
}

type exportJobStartResponse struct {
	ExportID string `json:"export_id"`
	Status   string `json:"status"`
	PollPath string `json:"poll_path"`
	LinkPath string `json:"link_path"`
}

type exportJobStatusResponse struct {
	ExportID string `json:"export_id"`
	Status   string `json:"status"`
	Error    string `json:"error"`
}

// exportLinkResponse is the answer of GET link_path (REQ-OBAN-018): url is an
// absolute signed storage URL, or the API's own file route.
type exportLinkResponse struct {
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	ExpiresAt string `json:"expires_at"`
}

// ErrExportExpired marks a /link answer of 410: the export's zip is gone from
// the store, and only a new export brings it back.
var ErrExportExpired = errors.New("the export expired before it was downloaded; run the export again")

// ExportProgressFunc receives the export job's status when it changes, and a
// heartbeat detail with the elapsed time while it does not.
type ExportProgressFunc func(status, progress string)

// DownloadExport downloads the system export as a zip file.
// Uses async export (POST + poll + link + file GET) so each HTTP call returns quickly — required when
// a front load balancer has a low idle timeout (e.g. Hetzner Cloud ~60s).
func (c *Client) DownloadExport(systemID int) ([]byte, error) {
	return c.DownloadExportWithProgress(systemID, nil)
}

// DownloadExportWithProgress downloads the system export as a zip file and reports
// status changes while the server-side export job is running.
func (c *Client) DownloadExportWithProgress(systemID int, progress ExportProgressFunc) ([]byte, error) {
	apiBase := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	startURL := fmt.Sprintf(
		"%s/api/systems/%d/export/jobs?include_sqlite=false&include_markdown=true",
		apiBase,
		systemID,
	)

	startReq, err := http.NewRequest("POST", startURL, nil)
	if err != nil {
		return nil, err
	}
	platform.Prepare(startReq)
	if err := platform.Authorize(startReq, c.Token); err != nil {
		return nil, err
	}
	startReq.Header.Set("Accept", "application/json")
	startReq.Header.Set("X-Requested-With", "ModernPath-CLI")

	startResp, err := c.HTTPClient.Do(startReq)
	if err != nil {
		return nil, fmt.Errorf("failed to start export: %w", err)
	}
	startBody, err := io.ReadAll(startResp.Body)
	startResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read start export response: %w", err)
	}
	if startResp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("failed to start export: %w", ErrUnauthorized)
	}
	if startResp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("failed to start export: HTTP %d — %s", startResp.StatusCode, string(startBody))
	}

	var started exportJobStartResponse
	if err := json.Unmarshal(startBody, &started); err != nil {
		return nil, fmt.Errorf("parse start export: %w", err)
	}
	if started.PollPath == "" || started.LinkPath == "" {
		return nil, fmt.Errorf("invalid start export response (missing paths)")
	}

	pollPath := canonicalExportJobPath(started.PollPath, systemID, started.ExportID, "")
	linkPath := canonicalExportJobPath(started.LinkPath, systemID, started.ExportID, "/link")
	pollURL := apiBase + pollPath
	begun := time.Now()
	deadline := begun.Add(exportDownloadTimeout)
	var lastStatus string
	lastReport := begun
	for time.Now().Before(deadline) {
		time.Sleep(exportPollInterval)

		pollReq, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			return nil, err
		}
		platform.Prepare(pollReq)
		if err := platform.Authorize(pollReq, c.Token); err != nil {
			return nil, err
		}
		pollReq.Header.Set("Accept", "application/json")
		pollReq.Header.Set("X-Requested-With", "ModernPath-CLI")

		pr, err := c.HTTPClient.Do(pollReq)
		if err != nil {
			return nil, fmt.Errorf("export status: %w", err)
		}
		pb, err := io.ReadAll(pr.Body)
		pr.Body.Close()
		if err != nil {
			return nil, err
		}
		if pr.StatusCode == http.StatusUnauthorized {
			return nil, fmt.Errorf("export status: %w", ErrUnauthorized)
		}
		if pr.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("export status: HTTP %d — %s", pr.StatusCode, string(pb))
		}

		var st exportJobStatusResponse
		if err := json.Unmarshal(pb, &st); err != nil {
			return nil, fmt.Errorf("parse export status: %w", err)
		}
		if progress != nil && st.Status != lastStatus {
			progress(st.Status, "")
			lastReport = time.Now()
		} else if progress != nil && time.Since(lastReport) >= exportHeartbeatEvery {
			// Heartbeat: nothing changed, but say so — a working wait and a
			// dead one must not look identical (REQ-CROSS-176).
			progress(st.Status, fmt.Sprintf("still working, %s elapsed", time.Since(begun).Round(time.Second)))
			lastReport = time.Now()
		}
		lastStatus = st.Status
		switch st.Status {
		case "queued", "running":
		case "ready":
			return c.downloadExportViaLink(linkPath)
		case "failed":
			msg := strings.TrimSpace(st.Error)
			if msg == "" {
				msg = "export job failed"
			}
			return nil, fmt.Errorf("export failed: %s", msg)
		default:
			return nil, fmt.Errorf("export failed: unknown export status %q", st.Status)
		}
	}
	return nil, fmt.Errorf("export timed out waiting for zip (last status %q)", lastStatus)
}

// canonicalExportJobPath maps a job path the server answered under another
// route family (`/api/architectures/...`) onto the system route, with suffix
// "" for the poll path and "/link" for the link path.
func canonicalExportJobPath(path string, systemID int, exportID string, suffix string) string {
	path = strings.TrimSpace(path)
	if path == "" || systemID == 0 || exportID == "" {
		return path
	}

	canonical := fmt.Sprintf("/api/systems/%d/export/jobs/%s%s", systemID, exportID, suffix)
	if strings.HasSuffix(path, fmt.Sprintf("/export/jobs/%s%s", exportID, suffix)) {
		return canonical
	}

	return path
}

// downloadExportViaLink asks link_path where the zip is and fetches it
// (REQ-OBAN-018). An absolute url is a signed storage URL: it carries its own
// credential and is fetched without the bearer, which a bucket refuses and
// which must never leave for a host that is not ours. A path is the API's own
// file route, resolved against the API base and fetched with the bearer.
func (c *Client) downloadExportViaLink(linkPath string) ([]byte, error) {
	linkURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + linkPath
	req, err := http.NewRequest("GET", linkURL, nil)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)
	if err := platform.Authorize(req, c.Token); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Requested-With", "ModernPath-CLI")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("export link: %w", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to read export link response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("export link: %w", ErrUnauthorized)
	case http.StatusGone:
		return nil, ErrExportExpired
	default:
		return nil, exportResponseError("export link", resp.StatusCode, body)
	}

	var link exportLinkResponse
	if err := json.Unmarshal(body, &link); err != nil {
		return nil, fmt.Errorf("parse export link: %w", err)
	}
	target := strings.TrimSpace(link.URL)
	switch {
	case strings.HasPrefix(target, "/"):
		return c.downloadExportZipByPath(target)
	case isAbsoluteHTTPURL(target):
		return c.downloadSignedExport(target)
	default:
		return nil, fmt.Errorf("invalid export link response (url is neither an API path nor an http(s) URL)")
	}
}

func isAbsoluteHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http"
}

// downloadSignedExport fetches a signed storage URL. It never goes through
// platform.Authorize, so no Authorization header is sent, and the URL is kept
// out of every error: it is a credential until it expires.
func (c *Client) downloadSignedExport(signedURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", signedURL, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid signed export URL")
	}
	// A no-op on a storage host; it acts only on a platform API origin.
	platform.Prepare(req)
	req.Header.Set("Accept", "*/*")

	resp, err := c.exportHTTPClientForURL(signedURL).Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("failed to download export from storage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to download export from storage: HTTP %d", resp.StatusCode)
	}
	return checkExportZip(resp, body)
}

func (c *Client) downloadExportZipByPath(path string) ([]byte, error) {
	downloadURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + path
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}
	platform.Prepare(req)
	if err := platform.Authorize(req, c.Token); err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("X-Requested-With", "ModernPath-CLI")

	exportClient := c.exportHTTPClientForURL(downloadURL)
	resp, err := exportClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to download export: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("failed to download export: %w", ErrUnauthorized)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, exportResponseError("failed to download export", resp.StatusCode, body)
	}
	return checkExportZip(resp, body)
}

// exportResponseError renders a non-2xx export answer: the server's message,
// else its error code, else the status.
func exportResponseError(what string, status int, body []byte) error {
	var errResp struct {
		Error   string                 `json:"error"`
		Message string                 `json:"message"`
		Details map[string]interface{} `json:"details"`
	}
	if json.Unmarshal(body, &errResp) == nil {
		if errResp.Message != "" {
			if errResp.Details != nil {
				return fmt.Errorf("export failed: %s (details: %v)", errResp.Message, errResp.Details)
			}
			return fmt.Errorf("export failed: %s", errResp.Message)
		}
		if errResp.Error != "" {
			return fmt.Errorf("export failed: %s (HTTP %d)", errResp.Error, status)
		}
	}
	return fmt.Errorf("%s: HTTP %d", what, status)
}

// checkExportZip refuses a 200 whose body is not a zip.
func checkExportZip(resp *http.Response, body []byte) ([]byte, error) {
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "application/zip") && !strings.Contains(contentType, "application/octet-stream") {
		if len(body) < 4 || string(body[0:2]) != "PK" {
			var errResp struct {
				Error   string `json:"error"`
				Message string `json:"message"`
			}
			if json.Unmarshal(body, &errResp) == nil && errResp.Message != "" {
				return nil, fmt.Errorf("export failed: %s", errResp.Message)
			}
			return nil, fmt.Errorf("server returned invalid response (expected zip file, got %s)", contentType)
		}
	}

	return body, nil
}

// exportHTTPClientForURL returns a long-timeout client for zip downloads.
// TLS verification is skipped only when MODERNPATH_EXPORT_INSECURE_TLS=1 (e.g. dev origin
// with a non-public CA). MODERNPATH_EXPORT_STRICT_TLS=1 forces verification.
func (c *Client) exportHTTPClientForURL(rawURL string) *http.Client {
	insecure := exportShouldSkipTLSVerify()

	if c.HTTPClient != nil && c.HTTPClient.Transport != nil {
		return &http.Client{
			Timeout:   exportDownloadTimeout,
			Transport: c.HTTPClient.Transport,
		}
	}

	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Timeout: exportDownloadTimeout}
	}
	clone := tr.Clone()
	if insecure {
		if clone.TLSClientConfig == nil {
			clone.TLSClientConfig = &tls.Config{
				MinVersion: tls.VersionTLS12,
			}
		} else {
			clone.TLSClientConfig = clone.TLSClientConfig.Clone()
		}
		clone.TLSClientConfig.InsecureSkipVerify = true
	}
	return &http.Client{
		Timeout:   exportDownloadTimeout,
		Transport: clone,
	}
}

func exportShouldSkipTLSVerify() bool {
	if os.Getenv("MODERNPATH_EXPORT_STRICT_TLS") == "1" ||
		strings.EqualFold(os.Getenv("MODERNPATH_EXPORT_STRICT_TLS"), "true") {
		return false
	}
	return os.Getenv("MODERNPATH_EXPORT_INSECURE_TLS") == "1" ||
		strings.EqualFold(os.Getenv("MODERNPATH_EXPORT_INSECURE_TLS"), "true")
}

// CreateSystem creates a new system on the platform
func (c *Client) CreateSystem(name, description string) (*System, error) {
	body := fmt.Sprintf(`{"name": %q, "description": %q, "system_type": "web", "status": "draft"}`, name, description)
	resp, err := c.doRequest("POST", "/api/systems", strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to create system: HTTP %d - %s", resp.StatusCode, string(respBody))
	}

	var sys System
	if err := json.NewDecoder(resp.Body).Decode(&sys); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &sys, nil
}
