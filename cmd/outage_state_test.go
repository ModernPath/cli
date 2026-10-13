package cmd

// REQ-CROSS-535 (EPIC-CLI-OUTAGE) — the CLI tells an outage apart from a
// refusal and remembers it. Every test drives a command through run(args)
// against a fake store in a temp workspace and asserts only the exit code, the
// printed text, the availability state file and the requests the fake saw.

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fatih/color"
)

const exitTempFail = 75 // EX_TEMPFAIL

// outageWorkspace enters a fresh workspace bound to apiURL with a stored
// bearer, the shape `run` resolves from disk as the installed binary does.
func outageWorkspace(t *testing.T, apiURL string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"api_url":"` + apiURL + `","system_id":1,"system_name":"T","system_slug":"t"}`
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "auth.json"), []byte(`{"token":"t","actor":"dev@example.com"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	return root
}

// runCLI runs one invocation through run and returns its exit code and
// everything it printed on stdout and stderr.
func runCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	resetTreeFlags(rootCmd)
	t.Cleanup(func() { resetTreeFlags(rootCmd) })
	defer rootCmd.SetArgs(nil)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	savedOut, savedErr, savedCOut, savedCErr := os.Stdout, os.Stderr, color.Output, color.Error
	os.Stdout, os.Stderr, color.Output, color.Error = w, w, w, w
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() { _, _ = io.Copy(&buf, r); close(done) }()

	code := run(args)

	_ = w.Close()
	os.Stdout, os.Stderr, color.Output, color.Error = savedOut, savedErr, savedCOut, savedCErr
	<-done
	_ = r.Close()
	return code, buf.String()
}

// scaleOutageTimeouts shrinks the probe and call bounds for one test.
func scaleOutageTimeouts(t *testing.T, probe, call time.Duration) {
	t.Helper()
	savedProbe, savedCall := probeTimeout, defaultCallTimeout
	probeTimeout, defaultCallTimeout = probe, call
	t.Cleanup(func() { probeTimeout, defaultCallTimeout = savedProbe, savedCall })
}

// closedURL is an address nothing listens on: the connection is refused.
func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return "http://" + addr
}

// storeFake answers the reachability probe (GET /api/systems) with probe and
// every other store request with store; the contract read is a 404, as an
// older server answers it. hits counts the store requests.
func storeFake(t *testing.T, probe, store http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/systems":
			probe(w, r)
		case "/api/v1/sync/contract":
			w.WriteHeader(http.StatusNotFound)
		default:
			hits.Add(1)
			store(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func healthyProbe(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`[{"id":1,"name":"T"}]`))
}

// gatewayText answers as an edge does: a plain-text body, no store header.
func gatewayText(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = w.Write([]byte("upstream connect error or disconnect/reset before headers"))
	}
}

// coreRefusal answers as core does: its JSON error object and its store header.
func coreRefusal(status int, reason, message string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-modernpath-store-revision", "rev-1")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": reason, "message": message}})
	}
}

func emptyGates(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("x-modernpath-store-revision", "rev-1")
	_, _ = w.Write([]byte(`{"data":{"gates":[]}}`))
}

func availabilityPath(root string) string {
	return filepath.Join(root, ".modernpath", "server-availability.json")
}

// readAvailability returns the availability state file, or nil when absent.
func readAvailability(t *testing.T, root string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(availabilityPath(root))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var state map[string]any
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("server-availability.json is not JSON: %v\n%s", err, raw)
	}
	return state
}

// AC1 — a refused connection, a client timeout, or a 502/503/504 without
// core's body, with the probe failing too, is an outage: exit 75, a message
// that is not a refusal, and the availability state recorded.
func TestUnavailableIsNotARefusal(t *testing.T) {
	cases := []struct {
		name   string
		apiURL func(t *testing.T) string
	}{
		{"refused connection", closedURL},
		{"client timeout", func(t *testing.T) string {
			release := make(chan struct{})
			hang := func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-release:
				case <-r.Context().Done():
				}
			}
			srv, _ := storeFake(t, hang, hang)
			t.Cleanup(func() { close(release) }) // runs before srv.Close
			return srv.URL
		}},
		{"plain-text 502", func(t *testing.T) string {
			srv, _ := storeFake(t, gatewayText(502), gatewayText(502))
			return srv.URL
		}},
		{"plain-text 503", func(t *testing.T) string {
			srv, _ := storeFake(t, gatewayText(503), gatewayText(503))
			return srv.URL
		}},
		{"plain-text 504", func(t *testing.T) string {
			srv, _ := storeFake(t, gatewayText(504), gatewayText(504))
			return srv.URL
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scaleOutageTimeouts(t, 100*time.Millisecond, 300*time.Millisecond)
			root := outageWorkspace(t, tc.apiURL(t))

			code, out := runCLI(t, "factory", "gates")

			if code != exitTempFail {
				t.Errorf("exit %d, want %d (EX_TEMPFAIL) for an unavailable server; output:\n%s", code, exitTempFail, out)
			}
			if !strings.Contains(out, "ModernPath server unavailable") || !strings.Contains(out, "not a refusal") {
				t.Errorf("the message must say the ModernPath server is unavailable and that this is not a refusal; got:\n%s", out)
			}
			state := readAvailability(t, root)
			if state == nil {
				t.Fatalf("no availability state recorded at %s", availabilityPath(root))
			}
			if state["state"] != "unavailable" {
				t.Errorf("state %v, want unavailable", state["state"])
			}
			if since, _ := state["since"].(string); since == "" {
				t.Errorf("the state must record since: %v", state)
			} else if _, err := time.Parse(time.RFC3339, since); err != nil {
				t.Errorf("since %q is not RFC 3339: %v", since, err)
			}
			if reason, _ := state["reason"].(string); reason == "" {
				t.Errorf("the state must record the reason: %v", state)
			}
		})
	}
}

// AC2 — refusals stay refusals. Regression guard: passes before and after the
// change; nothing here is an outage, so nothing is recorded.
func TestRefusalsStayRefusals(t *testing.T) {
	plainWithStoreHeader := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-modernpath-store-revision", "rev-1")
		w.WriteHeader(http.StatusGatewayTimeout)
		_, _ = w.Write([]byte("timed out evaluating the batch"))
	}
	cases := []struct {
		name  string
		store http.HandlerFunc
		want  string
	}{
		{"401", rejectingServerHandler(http.StatusUnauthorized), "rejected the session token"},
		{"409", coreRefusal(409, "conflict", "the record moved"), "server 409"},
		{"422", coreRefusal(422, "invalid", "bad state filter"), "server 422"},
		{"reasoned 500", coreRefusal(500, "internal", "the gate store failed"), "server 500"},
		{"502 with core body", coreRefusal(502, "upstream_failed", "the analysis service failed"), "server 502"},
		{"503 with core body", coreRefusal(503, "unavailable", "the gate store is migrating"), "server 503"},
		{"504 with store header", plainWithStoreHeader, "server 504"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits := storeFake(t, healthyProbe, tc.store)
			root := outageWorkspace(t, srv.URL)

			code, out := runCLI(t, "factory", "gates")

			if code != 1 {
				t.Errorf("exit %d, want 1 for a refusal; output:\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("want today's text %q; got:\n%s", tc.want, out)
			}
			if strings.Contains(out, "ModernPath server unavailable") {
				t.Errorf("a refusal must not read as an outage:\n%s", out)
			}
			if hits.Load() == 0 {
				t.Errorf("the request never reached the store")
			}
			if state := readAvailability(t, root); state != nil {
				t.Errorf("a refusal must record no availability state, got %v", state)
			}
		})
	}
}

func rejectingServerHandler(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}
}

// AC2 — a sync chunk answered 504 by the gateway, with the probe healthy, is
// retried as before and the sync succeeds. Regression guard.
func TestSyncChunk504StillRetries(t *testing.T) {
	stubSyncSleep(t)
	var batches atomic.Int32
	srv, _ := storeFake(t, healthyProbe, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/sync/batch" {
			if batches.Add(1) == 1 {
				gatewayText(http.StatusGatewayTimeout)(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"data":{"results":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	root := outageWorkspace(t, srv.URL)

	code, out := runCLI(t, "factory", "sync")

	if code != 0 {
		t.Fatalf("a chunk that succeeds on retry must end the sync successfully, exit %d:\n%s", code, out)
	}
	if got := batches.Load(); got != 2 {
		t.Errorf("want the chunk posted twice (a 504, then the retry), got %d", got)
	}
	if state := readAvailability(t, root); state != nil {
		t.Errorf("a retried gateway 504 is not an outage, got state %v", state)
	}
}

// AC3 — the first response that is not an outage candidate clears a stored
// unavailable state.
func TestGoodResponseClearsUnavailable(t *testing.T) {
	srv, _ := storeFake(t, healthyProbe, emptyGates)
	root := outageWorkspace(t, srv.URL)
	// Ten minutes old: past any fresh-outage window, so the command calls.
	then := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	stored := `{"state":"unavailable","since":"` + then + `","last_checked":"` + then + `","reason":"connection refused"}`
	if err := os.WriteFile(availabilityPath(root), []byte(stored), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runCLI(t, "factory", "gates")

	if code != 0 {
		t.Fatalf("exit %d, want 0 from a healthy server:\n%s", code, out)
	}
	state := readAvailability(t, root)
	if state == nil || state["state"] != "available" {
		t.Fatalf("a good response must clear the state to available, file holds %v", state)
	}
}

// AC4 — a 429 is the server limiting requests, not an outage: it says when to
// retry, records nothing and exits 75.
func TestRateLimitedIsNotAnOutage(t *testing.T) {
	recordLimitWaits(t)
	srv, _ := storeFake(t, healthyProbe, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too Many Requests"))
	})
	root := outageWorkspace(t, srv.URL)

	code, out := runCLI(t, "factory", "gates")

	if code != exitTempFail {
		t.Errorf("exit %d, want %d for a limited request; output:\n%s", code, exitTempFail, out)
	}
	if !strings.Contains(out, "limiting requests") || !strings.Contains(out, "retry after 7") {
		t.Errorf("the message must say the server is limiting requests and to retry after 7 s; got:\n%s", out)
	}
	if strings.Contains(out, "ModernPath server unavailable") {
		t.Errorf("a 429 is not an outage:\n%s", out)
	}
	if state := readAvailability(t, root); state != nil {
		t.Errorf("a 429 must record no availability state, got %v", state)
	}
}
