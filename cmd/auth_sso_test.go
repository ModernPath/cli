package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// stubZitadelLogin swaps the package-level zitadelLogin hook so tests can
// drive authenticateWithSSO's wiring without a real Zitadel tenant. It
// returns a restore func; SelectProfile itself (which picks between the two
// baked-in profiles) is never stubbed, so profile selection is still real.
func stubZitadelLogin(fn func(context.Context, zitadel.Profile, io.Writer, zitadel.Options) (*zitadel.Token, error)) func() {
	orig := zitadelLogin
	zitadelLogin = fn
	return func() { zitadelLogin = orig }
}

// withAuthFlags sets the auth command's flag variables for one test and
// restores them afterwards.
func withAuthFlags(t *testing.T, sso, test bool, tok, url string, local bool) {
	t.Helper()
	origSSO, origTest, origToken, origURL, origLocal := ssoFlag, ssoTestFlag, token, apiURL, authLocal
	ssoFlag, ssoTestFlag, token, apiURL, authLocal = sso, test, tok, url, local
	t.Cleanup(func() {
		ssoFlag, ssoTestFlag, token, apiURL, authLocal = origSSO, origTest, origToken, origURL, origLocal
	})
}

// USER:2026-09-01: ZITADEL is the only identity provider, so a bare
// `modernpath auth` IS the device flow — there is no core-proxied login left
// for it to fall back to. Prod by default; --test picks the test profile;
// --sso is accepted and changes nothing.
func TestBareAuthRunsTheZitadelDeviceFlow(t *testing.T) {
	tests := []struct {
		name       string
		sso        bool
		testFlag   bool
		wantIssuer string
	}{
		{"bare auth picks prod", false, false, zitadel.ProdProfile.Issuer},
		{"--test picks test", false, true, zitadel.TestProfile.Issuer},
		{"--sso is a no-op alias", true, false, zitadel.ProdProfile.Issuer},
		{"--sso --test still picks test", true, true, zitadel.TestProfile.Issuer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotIssuer string
			calls := 0
			restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
				calls++
				gotIssuer = p.Issuer
				return &zitadel.Token{AccessToken: "at", RefreshToken: "rt"}, nil
			})
			defer restore()
			withAuthFlags(t, tt.sso, tt.testFlag, "", "", false)

			t.Chdir(t.TempDir())
			if err := runAuth(authCmd, nil); err != nil {
				t.Fatalf("runAuth: %v", err)
			}
			if calls != 1 {
				t.Fatalf("the ZITADEL device flow must run exactly once, ran %d times", calls)
			}
			if gotIssuer != tt.wantIssuer {
				t.Fatalf("issuer = %q, want %q — prod and test must never mix", gotIssuer, tt.wantIssuer)
			}
		})
	}
}

// REQ-CROSS-235 clause 2: the profile follows the --test flag alone.
func TestSSOFlagSelectsBakedInProfileByTestFlag(t *testing.T) {
	tests := []struct {
		name       string
		testFlag   bool
		wantIssuer string
	}{
		{"prod by default", false, zitadel.ProdProfile.Issuer},
		{"--test picks test", true, zitadel.TestProfile.Issuer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotIssuer string
			restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
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

// REQ-CROSS-235 clause 4: success persists through the two calls saveTokens
// makes — the credential file and the API URL in config.
func TestSSOSuccessPersistsThroughExistingStorage(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
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
// reports the failure and writes no credential file. Since REQ-CROSS-336
// (EPIC-CLI-009 CR-11) the failure is also returned, so a script chaining
// `modernpath auth --workspace <id> && …` stops instead of running on the
// previous credential.
func TestSSOFailureWritesNoCredential(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		return nil, fmt.Errorf("device code expired")
	})
	defer restore()

	dir := t.TempDir()
	t.Chdir(dir)
	if err := authenticateWithSSO(false); err == nil || !strings.Contains(err.Error(), "device code expired") {
		t.Fatalf("a failed SSO login must be returned as a command error naming the failure, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".modernpath", "auth.json")); err == nil {
		t.Fatal("a failed SSO login must not write a credential file")
	}
}

// A pasted token for an explicit host (--api-url) is validated against that
// host and saved; the device flow is never started for it — the CLI has no
// issuer to run one against for a host outside the baked-in profiles.
func TestTokenFlagWithExplicitHostValidatesThereAndSkipsTheDeviceFlow(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		t.Fatal("--token must not start the device flow")
		return nil, nil
	})
	defer restore()

	srv, hits := systemsServer(t, 7)
	withAuthFlags(t, false, false, "pasted-jwt", srv.URL, false)

	t.Chdir(t.TempDir())
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the pasted token must be validated against the explicit host exactly once, got %d calls", hits.Load())
	}
	auth, err := config.ReadAuth()
	if err != nil || auth.Token != "pasted-jwt" {
		t.Fatalf("the validated token must be saved: auth=%+v err=%v", auth, err)
	}
	cfg, err := config.ReadConfig()
	if err != nil || cfg.APIURL != srv.URL {
		t.Fatalf("the explicit host must be saved as the API URL: cfg=%+v err=%v", cfg, err)
	}
}

// withDeviceFlowFlag sets --device-flow for one test and restores it.
func withDeviceFlowFlag(t *testing.T, v bool) {
	t.Helper()
	orig := deviceFlowFlag
	deviceFlowFlag = v
	t.Cleanup(func() { deviceFlowFlag = orig })
}

// USER:2026-09-07: the browser + loopback flow is the default; --device-flow
// is the one switch that forces RFC 8628. The command hands zitadel.Login the
// flag, a real browser launcher, and the process's surroundings — the choice
// itself is made and tested in the zitadel package.
func TestDeviceFlowFlagIsHandedToTheLoginAsAnOption(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("device-flow=%v", force), func(t *testing.T) {
			var got zitadel.Options
			restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, opts zitadel.Options) (*zitadel.Token, error) {
				got = opts
				return &zitadel.Token{AccessToken: platformJWT(t, p.Issuer, "org-home", "org-home"), RefreshToken: "rt"}, nil
			})
			defer restore()
			withAuthFlags(t, false, false, "", "", false)
			withDeviceFlowFlag(t, force)
			t.Chdir(t.TempDir())
			if err := runAuth(authCmd, nil); err != nil {
				t.Fatalf("runAuth: %v", err)
			}
			if got.DeviceFlow != force {
				t.Fatalf("Options.DeviceFlow = %v, want %v — --device-flow must reach the login unchanged", got.DeviceFlow, force)
			}
			if got.OpenBrowser == nil {
				t.Fatal("the command must supply a browser launcher, or the loopback flow can never run")
			}
			if got.LoopbackUnavailable == nil {
				t.Fatal("the command must supply the loopback pre-check over the real host")
			}
		})
	}
}
