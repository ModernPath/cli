package cmd

// REQ-CROSS-282 — the CLI validates the configured system_id against the
// authenticated credential's own reachable-systems list, across both auth
// variants (pasted token, ZITADEL device flow). The third variant this file
// covered — the core-proxied browser device flow — went with core-as-issuer
// (USER:2026-09-01). See tasks/CROSS-REQUIREMENTS.md for the full entry
// packet — Statement, acceptance criteria, change boundary, and this file's
// RED strategy.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// captureWarnings swaps color.Error — the writer printWarning/printError use
// (root.go) — for a buffer for the duration of fn, then restores it.
func captureWarnings(t *testing.T, fn func()) string {
	t.Helper()
	orig := color.Error
	var buf bytes.Buffer
	color.Error = &buf
	defer func() { color.Error = orig }()
	fn()
	return buf.String()
}

// stubListSystemsFn swaps listSystemsFn (system_reachability.go) — needed
// for authenticateWithSSO, whose target URL comes from a baked-in Zitadel
// profile that a test cannot point at a fake server.
func stubListSystemsFn(fn func(apiURL, token string) ([]api.System, error)) func() {
	orig := listSystemsFn
	listSystemsFn = fn
	return func() { listSystemsFn = orig }
}

// writeSystemIDConfig enters a fresh temp workspace bound to systemID.
func writeSystemIDConfig(t *testing.T, systemID int) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := config.WriteConfig(&config.Config{SystemID: systemID}); err != nil {
		t.Fatal(err)
	}
}

// systemsServer answers GET /api/systems with the given ids and counts hits.
func systemsServer(t *testing.T, systemIDs ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/systems" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		hits.Add(1)
		systems := make([]map[string]any, len(systemIDs))
		for i, id := range systemIDs {
			systems[i] = map[string]any{"id": id, "name": fmt.Sprintf("system-%d", id), "slug": fmt.Sprintf("sys-%d", id)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(systems)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// ---------------------------------------------------------------- token path

// TestTokenAuthStillRejectsAnInvalidTokenWithoutSaving pins the pre-existing,
// unmodified behavior this SR must not weaken: a ListSystems() error (a real
// 401, or a network failure — indistinguishable to the caller, and that's
// fine, both must still refuse) still prints "Token validation failed" and
// still calls no saveTokens.
func TestTokenAuthStillRejectsAnInvalidTokenWithoutSaving(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	t.Chdir(dir)

	if err := authenticateWithToken(srv.URL, "garbage-token"); err != nil {
		t.Fatalf("a failed token validation must be reported, not returned as a command error: %v", err)
	}
	if _, err := os.Stat(dir + "/.modernpath/auth.json"); err == nil {
		t.Fatal("an invalid token must not be saved")
	}
}

func TestTokenAuthReusesItsExistingSystemsCallToValidate(t *testing.T) {
	writeSystemIDConfig(t, 7)
	srv, hits := systemsServer(t, 7)

	if err := authenticateWithToken(srv.URL, "good-token"); err != nil {
		t.Fatalf("a valid token must not error: %v", err)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("ListSystems() called %d times, want exactly 1 — the token path must reuse its existing validation call, not make a second one", got)
	}
}

func TestTokenAuthStaysSilentWhenTheConfiguredSystemIsReachable(t *testing.T) {
	writeSystemIDConfig(t, 7)
	srv, _ := systemsServer(t, 7)

	warnings := captureWarnings(t, func() {
		if err := authenticateWithToken(srv.URL, "good-token"); err != nil {
			t.Fatalf("a valid token for a reachable system must not error: %v", err)
		}
	})
	if warnings != "" {
		t.Fatalf("a reachable configured system must print no warning, got %q", warnings)
	}
}

func TestTokenAuthStillSavesCredentialsWhenTheConfiguredSystemIsUnreachable(t *testing.T) {
	writeSystemIDConfig(t, 999)
	srv, _ := systemsServer(t, 7, 8)

	warnings := captureWarnings(t, func() {
		if err := authenticateWithToken(srv.URL, "good-token"); err != nil {
			t.Fatalf("an unreachable configured system must warn, not error: %v", err)
		}
	})
	if !strings.Contains(warnings, "999") {
		t.Fatalf("warning must name the configured system 999, got %q", warnings)
	}
	if !strings.Contains(warnings, "factory connect --system") {
		t.Fatalf("warning must name the repair command, got %q", warnings)
	}
	auth, err := config.ReadAuth()
	if err != nil || auth.Token != "good-token" {
		t.Fatalf("credentials must still be saved on a system mismatch: auth=%+v err=%v", auth, err)
	}
}

// ------------------------------------------------------------------ SSO path

func TestSSOAuthWarnsWhenTheConfiguredSystemIsUnreachable(t *testing.T) {
	writeSystemIDConfig(t, 999)
	defer stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		return &zitadel.Token{AccessToken: "AT-SSO", RefreshToken: "RT-SSO"}, nil
	})()
	defer stubListSystemsFn(func(apiURL, token string) ([]api.System, error) {
		return []api.System{{ID: 7}, {ID: 8}}, nil
	})()

	warnings := captureWarnings(t, func() {
		if err := authenticateWithSSO(true); err != nil {
			t.Fatalf("authenticateWithSSO: %v", err)
		}
	})
	if !strings.Contains(warnings, "999") {
		t.Fatalf("warning must name the configured system 999, got %q", warnings)
	}
	auth, err := config.ReadAuth()
	if err != nil || auth.Token != "AT-SSO" {
		t.Fatalf("credentials must still be saved on a system mismatch: auth=%+v err=%v", auth, err)
	}
}

func TestSSOAuthStaysSilentWhenTheConfiguredSystemIsReachable(t *testing.T) {
	writeSystemIDConfig(t, 7)
	defer stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		return &zitadel.Token{AccessToken: "AT-SSO", RefreshToken: "RT-SSO"}, nil
	})()
	defer stubListSystemsFn(func(apiURL, token string) ([]api.System, error) {
		return []api.System{{ID: 7}}, nil
	})()

	warnings := captureWarnings(t, func() {
		if err := authenticateWithSSO(true); err != nil {
			t.Fatalf("authenticateWithSSO: %v", err)
		}
	})
	if warnings != "" {
		t.Fatalf("a reachable configured system must print no warning, got %q", warnings)
	}
}

func TestSSOAuthStaysSilentWhenTheReachabilityCallErrors(t *testing.T) {
	writeSystemIDConfig(t, 999)
	defer stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		return &zitadel.Token{AccessToken: "AT-SSO", RefreshToken: "RT-SSO"}, nil
	})()
	var listCalled bool
	defer stubListSystemsFn(func(apiURL, token string) ([]api.System, error) {
		listCalled = true
		return nil, fmt.Errorf("network down")
	})()

	warnings := captureWarnings(t, func() {
		if err := authenticateWithSSO(true); err != nil {
			t.Fatalf("authenticateWithSSO: %v", err)
		}
	})
	if warnings != "" {
		t.Fatalf("a check that itself errors must fail open — print nothing, got %q", warnings)
	}
	if !listCalled {
		t.Fatal("the reachability call must actually have been attempted")
	}
}

// ------------------------------------------------------------- fresh init x2

// TestFreshInitHasNothingToValidate: SystemID == 0 across both auth
// variants — nothing to check yet, no call, no warning.
func TestFreshInitHasNothingToValidate(t *testing.T) {
	t.Run("SSO", func(t *testing.T) {
		t.Chdir(t.TempDir())
		defer stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
			return &zitadel.Token{AccessToken: "AT", RefreshToken: "RT"}, nil
		})()
		defer stubListSystemsFn(func(apiURL, token string) ([]api.System, error) {
			t.Fatal("a fresh, unbound workspace must not check reachability at all")
			return nil, nil
		})()

		if err := authenticateWithSSO(true); err != nil {
			t.Fatalf("authenticateWithSSO: %v", err)
		}
	})

	t.Run("Token", func(t *testing.T) {
		t.Chdir(t.TempDir())
		srv, hits := systemsServer(t, 5) // some system that is NOT 0
		warnings := captureWarnings(t, func() {
			if err := authenticateWithToken(srv.URL, "good-token"); err != nil {
				t.Fatalf("authenticateWithToken: %v", err)
			}
		})
		if hits.Load() != 1 {
			t.Fatalf("the token path's own pre-existing validation call must still happen exactly once, got %d", hits.Load())
		}
		if warnings != "" {
			t.Fatalf("a fresh, unbound workspace must print no warning even though 0 is not in the returned list, got %q", warnings)
		}
	})
}
