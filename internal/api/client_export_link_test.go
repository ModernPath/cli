package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// REQ-OBAN-018 AC4 (CLI): the client downloads through /link. The link's url is
// either an absolute signed bucket URL, fetched with no Authorization header,
// or the API's own file route, resolved against the API base and fetched with
// the bearer. The bucket stand-in refuses any request that carries
// Authorization, the way a V4 signed URL refuses a second credential.

const linkTestToken = "t"

type exportLinkAPI struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	// link answers GET link_path; the default serves nothing.
	link func(w http.ResponseWriter, r *http.Request)
	// pollStatus and pollError are the poll answer's status and error.
	pollStatus string
	pollError  any
}

func (a *exportLinkAPI) seen(method, path string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for _, r := range a.requests {
		if r == method+" "+path {
			n++
		}
	}
	return n
}

func newExportLinkAPI(t *testing.T) *exportLinkAPI {
	t.Helper()
	a := &exportLinkAPI{pollStatus: "ready"}
	a.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.requests = append(a.requests, r.Method+" "+r.URL.Path)
		a.mu.Unlock()

		if r.Header.Get("Authorization") != "Bearer "+linkTestToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		reply := func(code int, v any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(v)
		}

		switch r.URL.Path {
		case "/api/systems/42/export/jobs":
			if r.Method != http.MethodPost {
				t.Errorf("start method = %s, want POST", r.Method)
			}
			reply(http.StatusAccepted, map[string]any{
				"export_id": "exp-1",
				"status":    "queued",
				"poll_path": "/api/systems/42/export/jobs/exp-1",
				"link_path": "/api/systems/42/export/jobs/exp-1/link",
			})
		case "/api/systems/42/export/jobs/exp-1":
			reply(http.StatusOK, map[string]any{
				"export_id": "exp-1",
				"status":    a.pollStatus,
				"error":     a.pollError,
			})
		case "/api/systems/42/export/jobs/exp-1/link":
			if a.link == nil {
				t.Errorf("unexpected link request")
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			a.link(w, r)
		case "/api/systems/42/export/jobs/exp-1/file":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("PK\x03\x04api-zip"))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(a.Close)
	return a
}

func newExportLinkClient(api *exportLinkAPI) *Client {
	return &Client{BaseURL: api.URL, Token: linkTestToken, HTTPClient: api.Client()}
}

func writeLink(w http.ResponseWriter, url string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"url":        url,
		"filename":   "demo-system.modernpath.zip",
		"expires_at": "2026-09-28T12:15:00Z",
	})
}

// L18-5: a signed URL goes straight to the bucket, without the bearer.
func TestDownloadExportFetchesSignedURLWithoutAuthorization(t *testing.T) {
	withFastExportPolling(t)

	var bucketMu sync.Mutex
	bucketHits := 0
	var bucketAuth []string
	bucket := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucketMu.Lock()
		bucketHits++
		bucketMu.Unlock()
		if auth := r.Header.Get("Authorization"); auth != "" {
			bucketMu.Lock()
			bucketAuth = append(bucketAuth, auth)
			bucketMu.Unlock()
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`<Error><Code>InvalidArgument</Code><Message>Multiple authentication methods</Message></Error>`))
			return
		}
		if r.URL.Path != "/bucket/.export-cache/exp-1.zip" || r.URL.Query().Get("X-Goog-Signature") != "sig" {
			t.Errorf("bucket request = %s, want the signed object URL", r.URL.String())
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK\x03\x04bucket-zip"))
	}))
	t.Cleanup(bucket.Close)

	api := newExportLinkAPI(t)
	api.link = func(w http.ResponseWriter, r *http.Request) {
		writeLink(w, bucket.URL+"/bucket/.export-cache/exp-1.zip?X-Goog-Algorithm=GOOG4-RSA-SHA256&X-Goog-Signature=sig")
	}

	zipData, err := newExportLinkClient(api).DownloadExport(42)
	if err != nil {
		t.Fatalf("DownloadExport returned error: %v", err)
	}
	if string(zipData) != "PK\x03\x04bucket-zip" {
		t.Fatalf("zipData = %q, want the bucket's zip", string(zipData))
	}
	bucketMu.Lock()
	defer bucketMu.Unlock()
	if len(bucketAuth) != 0 {
		t.Fatalf("the signed URL was fetched with Authorization %q — the bearer must not leave for the bucket", bucketAuth)
	}
	if bucketHits != 1 {
		t.Fatalf("bucket hits = %d, want 1", bucketHits)
	}
	if n := api.seen(http.MethodGet, "/api/systems/42/export/jobs/exp-1/link"); n != 1 {
		t.Fatalf("link requests = %d, want 1 (with the bearer)", n)
	}
	if n := api.seen(http.MethodGet, "/api/systems/42/export/jobs/exp-1/file"); n != 0 {
		t.Fatalf("the API file route was fetched %d time(s) for a signed URL", n)
	}
}

// AC4: an API-path url is resolved against the API base and fetched with the bearer.
func TestDownloadExportFetchesAPIPathLinkWithAuthorization(t *testing.T) {
	withFastExportPolling(t)

	api := newExportLinkAPI(t)
	api.link = func(w http.ResponseWriter, r *http.Request) {
		writeLink(w, "/api/systems/42/export/jobs/exp-1/file")
	}

	zipData, err := newExportLinkClient(api).DownloadExport(42)
	if err != nil {
		t.Fatalf("DownloadExport returned error: %v", err)
	}
	if string(zipData) != "PK\x03\x04api-zip" {
		t.Fatalf("zipData = %q, want the API file route's zip", string(zipData))
	}
	if n := api.seen(http.MethodGet, "/api/systems/42/export/jobs/exp-1/file"); n != 1 {
		t.Fatalf("file route requests = %d, want 1", n)
	}
}

// AC1/AC4: a 410 from /link says the export expired and to run it again.
func TestDownloadExportExpiredLinkSaysExportAgain(t *testing.T) {
	withFastExportPolling(t)

	api := newExportLinkAPI(t)
	api.link = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"export_expired"}`))
	}

	_, err := newExportLinkClient(api).DownloadExport(42)
	if err == nil {
		t.Fatalf("DownloadExport returned nil error for an expired export")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "expired") || !strings.Contains(msg, "again") {
		t.Fatalf("error = %q, want it to say the export expired and to run it again", err)
	}
	if n := api.seen(http.MethodGet, "/api/systems/42/export/jobs/exp-1/link"); n != 1 {
		t.Fatalf("link requests = %d, want 1", n)
	}
	if n := api.seen(http.MethodGet, "/api/systems/42/export/jobs/exp-1/file"); n != 0 {
		t.Fatalf("the file route was fetched %d time(s) after a 410", n)
	}
}

// The poll answer's failure reason is `error`, not `error_message`.
func TestDownloadExportFailedPollCarriesTheErrorReason(t *testing.T) {
	withFastExportPolling(t)

	api := newExportLinkAPI(t)
	api.pollStatus = "failed"
	api.pollError = "export_timeout"

	_, err := newExportLinkClient(api).DownloadExport(42)
	if err == nil {
		t.Fatalf("DownloadExport returned nil error for a failed export")
	}
	if !strings.Contains(err.Error(), "export_timeout") {
		t.Fatalf("error = %q, want it to carry the poll's error reason", err)
	}
}
