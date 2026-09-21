package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-389 — the token's expiry is visible ahead of time and survivable
// without an interactive login: every store call refreshes a near-expiry
// credential where the issuer allows, warns once when it cannot, and refuses
// an expired one before any request with the expiry time and the remedy.

// captureStderr runs fn with os.Stderr and color.Error redirected to a pipe.
func captureStderr(t *testing.T, fn func(), sink *[]byte) {
	t.Helper()
	r, w, _ := os.Pipe()
	savedErr, savedColor := os.Stderr, color.Error
	os.Stderr, color.Error = w, w
	fn()
	_ = w.Close()
	os.Stderr, color.Error = savedErr, savedColor
	out, _ := io.ReadAll(r)
	*sink = out
}

// expiryWorkspace binds a workspace to apiURL/system 7 with a credential
// carrying the given expiry and refresh token, issued by the test plane so a
// refresh profile resolves. The reachability probe is stubbed to system 7.
func expiryWorkspace(t *testing.T, apiURL string, expiry time.Time, refreshToken string) string {
	t.Helper()
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":7}`, apiURL))
	auth := config.Auth{
		Token:        jwtWithClaims(t, map[string]any{"sub": "u1", "exp": expiry.Unix()}),
		ExpiresAt:    expiry.UTC().Format(time.RFC3339),
		RefreshToken: refreshToken,
		Issuer:       zitadel.TestProfile.Issuer,
		WorkspaceID:  "org-1",
		Actor:        "jane@example.com",
	}
	blob, _ := json.Marshal(auth)
	writeFactoryTestFile(t, root, ".modernpath/auth.json", string(blob))
	return root
}

func stubSystems(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	saved := listSystemsFn
	listSystemsFn = func(apiURL, token string) ([]api.System, error) {
		calls.Add(1)
		return []api.System{{ID: 7}}, nil
	}
	t.Cleanup(func() { listSystemsFn = saved })
	return &calls
}

func stubRefresh(t *testing.T, delay time.Duration, newExpiry time.Time) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	saved := refreshCredentialFn
	refreshCredentialFn = func(ctx context.Context, profile zitadel.Profile, refreshToken string) (*zitadel.Token, error) {
		calls.Add(1)
		time.Sleep(delay)
		return &zitadel.Token{AccessToken: "refreshed-bearer", RefreshToken: "rt-2", Expiry: newExpiry, Flow: zitadel.FlowRefresh}, nil
	}
	t.Cleanup(func() { refreshCredentialFn = saved })
	return &calls
}

// (a) Five minutes from expiry with a refresh token: the credential is
// refreshed at the issuer, the store call carries the new bearer, and the
// file holds the new pair.
func TestNearExpiryCredentialIsRefreshedBeforeTheCall(t *testing.T) {
	var bearer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	root := expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "rt-1")
	stubSystems(t)
	refreshes := stubRefresh(t, 0, time.Now().Add(12*time.Hour))

	env, err := factoryEnvLoad()
	if err != nil {
		t.Fatalf("factoryEnvLoad: %v", err)
	}
	if _, _, err := env.call("GET", "/api/v1/sync/gates", nil); err != nil {
		t.Fatalf("call: %v", err)
	}

	if refreshes.Load() != 1 {
		t.Fatalf("want exactly one refresh at the issuer, got %d", refreshes.Load())
	}
	if bearer != "Bearer refreshed-bearer" {
		t.Errorf("the store call must carry the refreshed bearer, got %q", bearer)
	}
	stored := readAuthFile(t, root)
	if stored["token"] != "refreshed-bearer" || stored["refresh_token"] != "rt-2" {
		t.Errorf("auth.json must hold the new pair, got token=%v refresh=%v", stored["token"], stored["refresh_token"])
	}
	if stored["workspace_id"] != "org-1" || stored["actor"] != "jane@example.com" {
		t.Errorf("a refresh must keep the workspace and the actor, got %v", stored)
	}
}

// (b) Five minutes from expiry with no refresh token: exactly one warning
// across two loads, naming the time left and the remedy.
func TestNearExpiryWithoutRefreshWarnsOnce(t *testing.T) {
	srv, _ := factoryTestSystemsServer(t, http.StatusOK, 7)
	expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "")
	stubSystems(t)

	var out []byte
	captureStderr(t, func() {
		for i := 0; i < 2; i++ {
			if _, err := factoryEnvLoad(); err != nil {
				t.Errorf("load %d: a still-valid credential must load: %v", i, err)
			}
		}
	}, &out)

	text := string(out)
	if n := strings.Count(text, "expires in"); n != 1 {
		t.Fatalf("want exactly one expiry warning across two loads, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, authRepairCommand(srv.URL)) {
		t.Errorf("the warning must name the remedy:\n%s", text)
	}
}

// (c) An expired credential is refused before any request — not the
// reachability probe, not the call — naming the expiry and the remedy.
func TestExpiredCredentialIsRefusedBeforeAnyRequest(t *testing.T) {
	srv, hits := factoryTestSystemsServer(t, http.StatusOK, 7)
	expired := time.Now().Add(-time.Minute)
	expiryWorkspace(t, srv.URL, expired, "")
	probes := stubSystems(t)

	env, err := factoryEnvLoad()
	if err == nil || env != nil {
		t.Fatal("an expired credential must not load")
	}
	if !strings.Contains(err.Error(), "expired at "+expired.UTC().Format(time.RFC3339)) {
		t.Errorf("the refusal must name the expiry time, got %v", err)
	}
	if !strings.Contains(err.Error(), authRepairCommand(srv.URL)) {
		t.Errorf("the refusal must name the remedy, got %v", err)
	}
	if probes.Load() != 0 || hits.Load() != 0 {
		t.Errorf("no request may leave before the refusal: probes=%d hits=%d", probes.Load(), hits.Load())
	}
}

// (e) Two concurrent loads inside the window refresh exactly once; the loser
// waits for the lock, re-reads, and carries the winner's bearer.
func TestConcurrentLoadsRefreshOnce(t *testing.T) {
	srv, _ := factoryTestSystemsServer(t, http.StatusOK, 7)
	expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "rt-1")
	stubSystems(t)
	refreshes := stubRefresh(t, 200*time.Millisecond, time.Now().Add(12*time.Hour))

	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env, err := factoryEnvLoad()
			errs[i] = err
			if env != nil {
				tokens[i] = env.token
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("want exactly one refresh under the lock, got %d", refreshes.Load())
	}
	if tokens[0] != "refreshed-bearer" || tokens[1] != "refreshed-bearer" {
		t.Errorf("both loads must carry the refreshed bearer, got %q and %q", tokens[0], tokens[1])
	}
	if _, err := os.Stat(filepath.Join(".modernpath", "auth.lock")); err == nil {
		t.Error("the credential lock must be released after the refresh")
	}
}

// (f) A 401 on a credential whose local expiry has passed says so, instead of
// leaving expiry to be inferred.
func TestCredentialRejectedNamesAPastExpiry(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	env := &factoryEnv{APIURL: config.DefaultAPIURL, tokenExpiry: past}

	got := env.credentialRejected().Error()
	if !strings.Contains(got, "expired at "+past.Format(time.RFC3339)) {
		t.Errorf("the 401 message must name the past expiry, got %q", got)
	}
	if !strings.Contains(got, "modernpath auth --sso") {
		t.Errorf("the 401 message must keep the remedy, got %q", got)
	}
}
