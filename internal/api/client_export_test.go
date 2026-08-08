package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newTestClient(handler http.HandlerFunc) *Client {
	client := NewClient("https://modernpath.test", "token")
	client.HTTPClient = &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			handler(rec, req)
			return rec.Result(), nil
		}),
	}
	return client
}

func TestDownloadExportUsesCanonicalSystemJobPaths(t *testing.T) {
	withFastExportPolling(t)

	var sawSystemPoll bool
	var sawSystemDownload bool

	client := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems/42/export/jobs":
			if r.Method != http.MethodPost {
				t.Fatalf("start method = %s, want POST", r.Method)
			}
			if got := r.URL.Query().Get("include_sqlite"); got != "false" {
				t.Fatalf("include_sqlite = %q, want false", got)
			}
			if got := r.URL.Query().Get("include_markdown"); got != "true" {
				t.Fatalf("include_markdown = %q, want true", got)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{
				"id": "job-123",
				"status": "queued",
				"poll_path": "/api/architectures/42/export/jobs/job-123",
				"download_path": "/api/architectures/42/export/jobs/job-123/file"
			}`))

		case "/api/systems/42/export/jobs/job-123":
			sawSystemPoll = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "job-123",
				"status": "ready",
				"download_path": "/api/architectures/42/export/jobs/job-123/file"
			}`))

		case "/api/systems/42/export/jobs/job-123/file":
			sawSystemDownload = true
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("PK\x03\x04zip"))

		case "/api/architectures/42/export/jobs/job-123":
			t.Fatalf("client followed legacy architecture poll path")

		case "/api/architectures/42/export/jobs/job-123/file":
			t.Fatalf("client followed legacy architecture download path")

		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	})

	zipData, err := client.DownloadExport(42)
	if err != nil {
		t.Fatalf("DownloadExport returned error: %v", err)
	}
	if string(zipData) != "PK\x03\x04zip" {
		t.Fatalf("zipData = %q, want test zip bytes", string(zipData))
	}
	if !sawSystemPoll {
		t.Fatalf("client did not poll canonical system path")
	}
	if !sawSystemDownload {
		t.Fatalf("client did not download from canonical system path")
	}
}

func TestDownloadExportReportsTerminalNonFailedStatus(t *testing.T) {
	withFastExportPolling(t)

	client := newTestClient(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems/7/export/jobs":
			if got := r.URL.Query().Get("include_sqlite"); got != "false" {
				t.Fatalf("include_sqlite = %q, want false", got)
			}
			if got := r.URL.Query().Get("include_markdown"); got != "true" {
				t.Fatalf("include_markdown = %q, want true", got)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{
				"id": "job-cancelled",
				"status": "queued",
				"poll_path": "/api/systems/7/export/jobs/job-cancelled",
				"download_path": "/api/systems/7/export/jobs/job-cancelled/file"
			}`))

		case "/api/systems/7/export/jobs/job-cancelled":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": "job-cancelled",
				"status": "cancelled"
			}`))

		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	})

	_, err := client.DownloadExport(7)
	if err == nil {
		t.Fatalf("DownloadExport returned nil error for cancelled export")
	}
}

func withFastExportPolling(t *testing.T) {
	t.Helper()

	previous := exportPollInterval
	exportPollInterval = time.Millisecond
	t.Cleanup(func() {
		exportPollInterval = previous
	})
}
