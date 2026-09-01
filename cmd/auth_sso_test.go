package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// stubZitadelLogin swaps the package-level zitadelLogin hook so tests can
// drive authenticateWithSSO's wiring without a real Zitadel tenant. It
// returns a restore func; SelectProfile itself (which picks between the two
// baked-in profiles) is never stubbed, so profile selection is still real.
func stubZitadelLogin(fn func(context.Context, zitadel.Profile, io.Writer) (*zitadel.Token, error)) func() {
	orig := zitadelLogin
	zitadelLogin = fn
	return func() { zitadelLogin = orig }
}

// REQ-CROSS-235 clause 1: modernpath auth with no --sso must still call the
// legacy core-proxied relay, untouched by this epic's new branch.
func TestNoSSOFlagLeavesLegacyInitiateUntouched(t *testing.T) {
	var hitInitiate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/cli/initiate" {
			hitInitiate = true
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := authenticateWithBrowser(srv.URL); err != nil {
		t.Fatalf("authenticateWithBrowser returned an error: %v", err)
	}
	if !hitInitiate {
		t.Fatal("modernpath auth with no --sso must still call /api/v1/auth/cli/initiate")
	}
}

// REQ-CROSS-235 clause 2: --sso alone selects prod; --sso --test selects
// test. zitadelLogin is swapped for a stub so the assertion never depends on
// a real Zitadel tenant, while SelectProfile itself is exercised for real.
func TestSSOFlagSelectsBakedInProfileByTestFlag(t *testing.T) {
	tests := []struct {
		name       string
		testFlag   bool
		wantIssuer string
	}{
		{"--sso only picks prod", false, zitadel.ProdProfile.Issuer},
		{"--sso --test picks test", true, zitadel.TestProfile.Issuer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotIssuer string
			restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer) (*zitadel.Token, error) {
				gotIssuer = p.Issuer
				return &zitadel.Token{AccessToken: "at", RefreshToken: "rt"}, nil
			})
			defer restore()

			t.Chdir(t.TempDir())
			if err := authenticateWithSSO(tt.testFlag); err != nil {
				t.Fatalf("authenticateWithSSO: %v", err)
			}
			if gotIssuer != tt.wantIssuer {
				t.Fatalf("issuer = %q, want %q — prod and test must never mix", gotIssuer, tt.wantIssuer)
			}
		})
	}
}

// REQ-CROSS-235 clause 4: success persists through the exact two calls
// saveTokens already makes for the legacy flow.
func TestSSOSuccessPersistsThroughExistingStorage(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer) (*zitadel.Token, error) {
		return &zitadel.Token{AccessToken: "AT-1", RefreshToken: "RT-1"}, nil
	})
	defer restore()

	t.Chdir(t.TempDir())
	if err := authenticateWithSSO(true); err != nil {
		t.Fatalf("authenticateWithSSO: %v", err)
	}

	auth, err := config.ReadAuth()
	if err != nil {
		t.Fatalf("ReadAuth: %v", err)
	}
	if auth.Token != "AT-1" || auth.RefreshToken != "RT-1" {
		t.Fatalf("stored auth = %+v, want Token=AT-1 RefreshToken=RT-1", auth)
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.APIURL != zitadel.TestProfile.APIURL {
		t.Fatalf("stored API URL = %q, want the test profile's %q", cfg.APIURL, zitadel.TestProfile.APIURL)
	}
}

// REQ-CROSS-235 clause 5: device-code expiry or any token-endpoint error
// reports the failure and writes no credential file.
func TestSSOFailureWritesNoCredential(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer) (*zitadel.Token, error) {
		return nil, fmt.Errorf("device code expired")
	})
	defer restore()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := authenticateWithSSO(false); err != nil {
		t.Fatalf("a failed SSO login must be reported, not returned as a command error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".modernpath", "auth.json")); err == nil {
		t.Fatal("a failed SSO login must not write a credential file")
	}
}

// --test only modifies --sso; it is meaningless (and disallowed) alone.
func TestSSOTestFlagRequiresSSOFlag(t *testing.T) {
	origSSO, origTest := ssoFlag, ssoTestFlag
	ssoFlag, ssoTestFlag = false, true
	defer func() { ssoFlag, ssoTestFlag = origSSO, origTest }()

	if err := runAuth(authCmd, nil); err == nil {
		t.Fatal("--test without --sso must error, not silently run the legacy flow")
	}
}
