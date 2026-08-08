package api

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
)

const exportDownloadTimeout = 30 * time.Minute

var exportPollInterval = 2 * time.Second

// NewAuthenticatedRequest creates an HTTP request with auth token from config.
// This is a convenience function for commands that don't use the full API client.
func NewAuthenticatedRequest(method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// Add auth token if available
	auth, _ := config.ReadAuth()
	if auth != nil && auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+auth.Token)
	}

	return req, nil
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

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	return c.HTTPClient.Do(req)
}

// HealthCheck checks if the API is reachable
func (c *Client) HealthCheck() error {
	resp, err := c.HTTPClient.Get(c.BaseURL + "/_health")
	if err != nil {
		return fmt.Errorf("cannot reach API: %w", err)
	}
	defer resp.Body.Close()

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
	ID           string `json:"id"`
	Status       string `json:"status"`
	PollPath     string `json:"poll_path"`
	DownloadPath string `json:"download_path"`
}

type exportJobStatusResponse struct {
	ID             string `json:"id"`
	Status         string `json:"status"`
	ErrorMessage   string `json:"error_message"`
	ProgressStatus string `json:"progress_status"`
	DownloadPath   string `json:"download_path"`
}

type ExportProgressFunc func(status, progress string)

// DownloadExport downloads the system export as a zip file.
// Uses async export (POST + poll + file GET) so each HTTP call returns quickly — required when
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
	if c.Token != "" {
		startReq.Header.Set("Authorization", "Bearer "+c.Token)
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
	if startResp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("failed to start export: HTTP %d — %s", startResp.StatusCode, string(startBody))
	}

	var started exportJobStartResponse
	if err := json.Unmarshal(startBody, &started); err != nil {
		return nil, fmt.Errorf("parse start export: %w", err)
	}
	if started.PollPath == "" || started.DownloadPath == "" {
		return nil, fmt.Errorf("invalid start export response (missing paths)")
	}

	pollPath := canonicalExportJobPath(started.PollPath, systemID, started.ID, false)
	pollURL := apiBase + pollPath
	deadline := time.Now().Add(exportDownloadTimeout)
	var lastStatus string
	var lastProgress string
	for time.Now().Before(deadline) {
		time.Sleep(exportPollInterval)

		pollReq, err := http.NewRequest("GET", pollURL, nil)
		if err != nil {
			return nil, err
		}
		if c.Token != "" {
			pollReq.Header.Set("Authorization", "Bearer "+c.Token)
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
		if pr.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("export status: HTTP %d — %s", pr.StatusCode, string(pb))
		}

		var st exportJobStatusResponse
		if err := json.Unmarshal(pb, &st); err != nil {
			return nil, fmt.Errorf("parse export status: %w", err)
		}
		if progress != nil && (st.Status != lastStatus || st.ProgressStatus != lastProgress) {
			progress(st.Status, st.ProgressStatus)
			lastProgress = st.ProgressStatus
		}
		lastStatus = st.Status
		switch st.Status {
		case "ready", "completed", "complete", "succeeded", "success":
			rel := st.DownloadPath
			if rel == "" {
				rel = started.DownloadPath
			}
			rel = canonicalExportJobPath(rel, systemID, started.ID, true)
			return c.downloadExportZipByPath(rel)
		case "failed", "error", "cancelled", "canceled", "expired":
			msg := strings.TrimSpace(st.ErrorMessage)
			if msg == "" {
				msg = fmt.Sprintf("export job ended with status %q", st.Status)
			}
			return nil, fmt.Errorf("export failed: %s", msg)
		}
	}
	return nil, fmt.Errorf("export timed out waiting for zip (last status %q)", lastStatus)
}

func canonicalExportJobPath(path string, systemID int, jobID string, file bool) string {
	path = strings.TrimSpace(path)
	if path == "" || systemID == 0 || jobID == "" {
		return path
	}

	suffix := ""
	if file {
		suffix = "/file"
	}

	canonical := fmt.Sprintf("/api/systems/%d/export/jobs/%s%s", systemID, jobID, suffix)
	if strings.Contains(path, fmt.Sprintf("/export/jobs/%s%s", jobID, suffix)) {
		return canonical
	}

	return path
}

func (c *Client) downloadExportZipByPath(path string) ([]byte, error) {
	downloadURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + path
	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
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

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error   string                 `json:"error"`
			Message string                 `json:"message"`
			Details map[string]interface{} `json:"details"`
		}
		if json.Unmarshal(body, &errResp) == nil {
			// Show message if available, otherwise show error code
			if errResp.Message != "" {
				if errResp.Details != nil {
					return nil, fmt.Errorf("export failed: %s (details: %v)", errResp.Message, errResp.Details)
				}
				return nil, fmt.Errorf("export failed: %s", errResp.Message)
			}
			if errResp.Error != "" {
				return nil, fmt.Errorf("export failed: %s", errResp.Error)
			}
		}
		return nil, fmt.Errorf("failed to download export: HTTP %d", resp.StatusCode)
	}

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

// DeviceFlowResponse is the response from initiating a device flow
type DeviceFlowResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
}

// TokenResponse is the response from successful authentication
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

// DeviceFlowPollResponse is the response from polling the device flow
type DeviceFlowPollResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
	Error        string `json:"error,omitempty"`
}

// InitiateDeviceFlow starts the device flow authentication
func (c *Client) InitiateDeviceFlow() (*DeviceFlowResponse, error) {
	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/v1/auth/cli/initiate", "application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to initiate device flow: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to initiate device flow: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var result DeviceFlowResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse device flow response: %w", err)
	}

	return &result, nil
}

// PollDeviceFlow polls for device flow completion
func (c *Client) PollDeviceFlow(deviceCode string) (*DeviceFlowPollResponse, error) {
	body := fmt.Sprintf(`{"device_code": "%s"}`, deviceCode)
	resp, err := c.HTTPClient.Post(c.BaseURL+"/api/v1/auth/cli/poll", "application/json", io.NopCloser(strings.NewReader(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to poll device flow: %w", err)
	}
	defer resp.Body.Close()

	var result DeviceFlowPollResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse poll response: %w", err)
	}

	// Handle known status codes
	switch resp.StatusCode {
	case http.StatusOK:
		return &result, nil
	case 428: // Precondition Required - authorization pending
		result.Error = "authorization_pending"
		return &result, nil
	case http.StatusBadRequest:
		result.Error = "expired"
		return &result, nil
	case http.StatusNotFound:
		result.Error = "invalid_device_code"
		return &result, nil
	default:
		return nil, fmt.Errorf("unexpected status: HTTP %d", resp.StatusCode)
	}
}
