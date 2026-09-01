package cmd

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// stubSyncSleep replaces the retry backoff sleep with a no-op for the duration
// of a test, so a retry test does not actually wait out the exponential
// backoff. It restores the previous sleeper on cleanup.
func stubSyncSleep(t *testing.T) {
	t.Helper()
	prev := syncRetrySleep
	syncRetrySleep = func(time.Duration) {}
	t.Cleanup(func() { syncRetrySleep = prev })
}

// A 5xx on a sync chunk is a transient edge failure (a gateway timeout on a
// heavy chunk), and each chunk is an idempotent server-side transaction — so
// postSyncBatch re-POSTs it rather than aborting the whole sync on the first
// 504. Without this, the size of a workspace's heaviest chunk silently decided
// whether it could sync at all (RUN:2026-08-31, test-plat).
func TestPostSyncBatchRetriesOn5xxThenSucceeds(t *testing.T) {
	stubSyncSleep(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) <= 2 { // first two attempts time out at the gateway
			w.WriteHeader(http.StatusGatewayTimeout)
			return
		}
		w.Write([]byte(`{"data":{"results":[]}}`))
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	status, _, err := env.postSyncBatch(map[string]any{"ops": []any{}})
	if err != nil {
		t.Fatalf("a chunk that succeeds on retry must not surface an error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("want 200 after the edge recovers, got %d", status)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("want 3 attempts (two 504s then a 200), got %d", got)
	}
}

// Retries are bounded: a persistently-504ing chunk stops after
// syncRetryMaxAttempts and returns the last status, so the run ends with the
// same "server 504" it would have without retry — never an infinite loop.
func TestPostSyncBatchGivesUpAfterMaxAttempts(t *testing.T) {
	stubSyncSleep(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	status, _, err := env.postSyncBatch(map[string]any{"ops": []any{}})
	if err != nil {
		t.Fatalf("a 504 carries no transport error: %v", err)
	}
	if status != http.StatusGatewayTimeout {
		t.Fatalf("want the last status (504) returned, got %d", status)
	}
	if got := int(hits.Load()); got != syncRetryMaxAttempts {
		t.Fatalf("want exactly %d attempts, got %d", syncRetryMaxAttempts, got)
	}
}

// A 422 is a deterministic refusal (an op the server does not understand), not
// a transient failure. Retrying it wastes time and would delay the unknown-op
// recovery path; postSyncBatch must return it on the first call.
func TestPostSyncBatchDoesNotRetry422(t *testing.T) {
	stubSyncSleep(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error":"unknown op"}`))
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	status, _, err := env.postSyncBatch(map[string]any{"ops": []any{}})
	if err != nil {
		t.Fatalf("a 422 is a normal server answer, not an error: %v", err)
	}
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d", status)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("a 422 must not be retried; want 1 attempt, got %d", got)
	}
}

// A 401 is a credential failure that re-running cannot fix (the fix is
// `modernpath auth`). It arrives as status 401 with a non-nil credentialError,
// and must not be swept into the transport-error retry branch.
func TestPostSyncBatchDoesNotRetry401(t *testing.T) {
	stubSyncSleep(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t"}
	status, _, err := env.postSyncBatch(map[string]any{"ops": []any{}})
	if err == nil {
		t.Fatal("a 401 must surface the credential error")
	}
	if status != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", status)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("a 401 must not be retried; want 1 attempt, got %d", got)
	}
}

// MODERNPATH_SYNC_CHUNK_SIZE tunes the per-POST op count so an operator can
// shrink chunks under a tight edge timeout without a rebuild. An unset or
// invalid value falls back to the default rather than failing the sync.
func TestSyncChunkSizeFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want int
	}{
		{"unset uses default", false, "", syncChunkSizeDefault},
		{"valid override", true, "50", 50},
		{"one is honored", true, "1", 1},
		{"zero falls back", true, "0", syncChunkSizeDefault},
		{"negative falls back", true, "-5", syncChunkSizeDefault},
		{"non-numeric falls back", true, "lots", syncChunkSizeDefault},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv("MODERNPATH_SYNC_CHUNK_SIZE", tc.val)
			} else {
				t.Setenv("MODERNPATH_SYNC_CHUNK_SIZE", "")
			}
			if got := syncChunkSize(); got != tc.want {
				t.Fatalf("MODERNPATH_SYNC_CHUNK_SIZE=%q: want %d, got %d", tc.val, tc.want, got)
			}
		})
	}
}
