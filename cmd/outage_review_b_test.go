package cmd

// The #779 review of 5f4aaeb15 (CODE-OUTAGE-3-3, 3-7, 3-9; USER:2026-10-12):
// a limited request waits and retries, the outage window grows, a wrong
// server address is not an outage, and an expired token keeps outage mode.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordLimitWaits stubs the wait between limited requests and returns the
// waits asked for.
func recordLimitWaits(t *testing.T) *[]time.Duration {
	t.Helper()
	waits := &[]time.Duration{}
	saved := limitSleep
	limitSleep = func(d time.Duration) { *waits = append(*waits, d) }
	t.Cleanup(func() { limitSleep = saved })
	return waits
}

// limitedThen answers 429 (with retryAfter when not empty) n times, then
// gates as core does.
func limitedThen(n int32, retryAfter string) http.HandlerFunc {
	var seen atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		if seen.Add(1) <= n {
			if retryAfter != "" {
				w.Header().Set("Retry-After", retryAfter)
			}
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("Too Many Requests"))
			return
		}
		emptyGates(w, r)
	}
}

// REQ-CROSS-535 AC4 (CODE-OUTAGE-3-9) — a limited request waits as the server
// asks and is sent again; when it then succeeds the command completes.
func TestRateLimitedRequestIsRetried(t *testing.T) {
	waits := recordLimitWaits(t)
	srv, hits := storeFake(t, healthyProbe, limitedThen(2, "3"))
	outageWorkspace(t, srv.URL)

	code, out := runCLI(t, "factory", "gates")

	if code != 0 {
		t.Fatalf("exit %d, want 0 once a retry succeeds:\n%s", code, out)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("two limited answers then one success: 3 requests, got %d", got)
	}
	if want := []time.Duration{3 * time.Second, 3 * time.Second}; !equalWaits(*waits, want) {
		t.Errorf("waits follow Retry-After: want %v, got %v", want, *waits)
	}
}

// REQ-CROSS-535 AC4 (CODE-OUTAGE-3-9) — without Retry-After the waits start at
// 2 s and double, each at most 30 s and all at most 2 minutes; then the
// command says the server is limiting requests and exits 75.
func TestRateLimitBackoffIsBounded(t *testing.T) {
	waits := recordLimitWaits(t)
	srv, _ := storeFake(t, healthyProbe, limitedThen(1000, ""))
	root := outageWorkspace(t, srv.URL)

	code, out := runCLI(t, "factory", "gates")

	if code != exitTempFail || !strings.Contains(out, "limiting requests") {
		t.Fatalf("exit %d, want %d naming the limit:\n%s", code, exitTempFail, out)
	}
	want := []time.Duration{2, 4, 8, 16, 30, 30, 30}
	for i := range want {
		want[i] *= time.Second
	}
	if !equalWaits(*waits, want) {
		t.Errorf("waits: want %v, got %v", want, *waits)
	}
	if state := readAvailability(t, root); state != nil {
		t.Errorf("a limit records no availability state, got %v", state)
	}
}

// REQ-CROSS-535 AC4 (CODE-OUTAGE-3-9) — a hook never waits on a limit.
func TestHookNeverWaitsOnALimit(t *testing.T) {
	waits := recordLimitWaits(t)
	srv, _ := storeFake(t, healthyProbe, limitedThen(1000, "3"))
	root := outageWorkspace(t, srv.URL)
	saved := replayInvocation
	replayInvocation = []string{"context", "--hook"}
	t.Cleanup(func() { replayInvocation = saved })

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 1, token: "t"}
	_, _, err := env.send("GET", "/api/v1/sync/gates?system_id=1", nil)

	var limiting serverLimitingError
	if !errors.As(err, &limiting) {
		t.Fatalf("a hook gets the limit at once, got %v", err)
	}
	if len(*waits) != 0 {
		t.Errorf("a hook never waits; waited %v", *waits)
	}
}

func equalWaits(got, want []time.Duration) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// REQ-CROSS-535 AC5 (CODE-OUTAGE-3-3) — a server address that is not a valid
// URL is a configuration error, never an outage; factory connect refuses it.
func TestInvalidServerAddressIsNotAnOutage(t *testing.T) {
	root := outageWorkspace(t, "127.0.0.1:9")

	code, out := runCLI(t, "factory", "gates")

	if code != 1 {
		t.Errorf("exit %d, want 1 for a wrong address:\n%s", code, out)
	}
	if strings.Contains(out, "unavailable") {
		t.Errorf("a wrong address is not an outage:\n%s", out)
	}
	if state := readAvailability(t, root); state != nil {
		t.Errorf("a wrong address records no availability state, got %v", state)
	}

	before, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, out = runCLI(t, "factory", "connect", "--system", "1", "--api-url", "localhost:4000")
	if code != 1 {
		t.Errorf("factory connect refuses an address without a scheme; exit %d:\n%s", code, out)
	}
	after, _ := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if string(after) != string(before) {
		t.Errorf("a refused address leaves the binding as it was:\n%s", after)
	}
}

// outageWindow is next_check − last_checked in the availability state.
func outageWindow(t *testing.T, root string) time.Duration {
	t.Helper()
	st := readAvailability(t, root)
	last, err1 := time.Parse(time.RFC3339Nano, str(st, "last_checked"))
	next, err2 := time.Parse(time.RFC3339Nano, str(st, "next_check"))
	if err1 != nil || err2 != nil {
		t.Fatalf("the state names when the server is asked again: %v", st)
	}
	return next.Sub(last).Round(time.Second)
}

// REQ-CROSS-536 AC1 (CODE-OUTAGE-3-9) — the window in which nothing is sent is
// 15 s after the first confirmation and doubles with each further one, up to
// 5 minutes.
func TestOutageWindowGrows(t *testing.T) {
	scaleOutageTimeouts(t, 300*time.Millisecond, time.Second)
	root := outageWorkspace(t, closedURL(t))

	runCLI(t, "factory", "gates")
	if w := outageWindow(t, root); w != 15*time.Second {
		t.Errorf("first confirmation: window 15s, got %s", w)
	}
	serverBack(t, root)
	runCLI(t, "factory", "gates")
	if w := outageWindow(t, root); w != 30*time.Second {
		t.Errorf("second confirmation: window 30s, got %s", w)
	}
	st := readAvailability(t, root)
	st["confirmations"] = 9
	writeState(t, root, st)
	serverBack(t, root)
	runCLI(t, "factory", "gates")
	if w := outageWindow(t, root); w != 5*time.Minute {
		t.Errorf("the window stops at 5 minutes, got %s", w)
	}
}

// REQ-CROSS-536 AC1 (CODE-OUTAGE-3-9) — while the window is open nothing
// reaches the server, however long ago the last check was.
func TestNothingIsSentInsideTheWindow(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		emptyGates(w, r)
	}))
	t.Cleanup(srv.Close)
	root := outageWorkspace(t, srv.URL)
	now := time.Now().UTC()
	writeState(t, root, map[string]any{
		"state": "unavailable", "since": now.Add(-10 * time.Minute).Format(time.RFC3339),
		"last_checked": now.Add(-90 * time.Second).Format(time.RFC3339),
		"next_check":   now.Add(time.Minute).Format(time.RFC3339),
		"reason":       "connection refused", "confirmations": 3,
	})

	code, out := runCLI(t, "factory", "gates")

	if code != exitTempFail {
		t.Errorf("exit %d, want %d inside the window:\n%s", code, exitTempFail, out)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("nothing reaches the server inside the window; %d request(s)", n)
	}
}

func writeState(t *testing.T, root string, st map[string]any) {
	t.Helper()
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(availabilityPath(root), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// expiredSignIn stores a sign-in whose token expired an hour ago and has no
// refresh token.
func expiredSignIn(t *testing.T, root string) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"sub": "user-539", "email": "dev@example.com", "exp": time.Now().Add(-time.Hour).Unix()})
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	auth := `{"token":"` + token + `","actor":"dev@example.com"}`
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
}

// REQ-CROSS-536 AC5 (CODE-OUTAGE-3-7) — an expired token that cannot be
// refreshed keeps outage mode: writes are kept, inside the window and after
// it while the server stays unreachable.
func TestExpiredTokenKeepsOutageMode(t *testing.T) {
	root := replayWorkspaceAt(t, closedURL(t))
	expiredSignIn(t, root)

	keep(t, "author", "update", "REQ-R-2", "--title", "Inside the window", "--expected-fingerprint", "fp-1")

	serverBack(t, root)
	keep(t, "author", "update", "REQ-R-3", "--title", "After the window", "--expected-fingerprint", "fp-3")

	if n := len(keptEntries(t, root)); n != 2 {
		t.Errorf("both writes are kept; %d kept", n)
	}
}
