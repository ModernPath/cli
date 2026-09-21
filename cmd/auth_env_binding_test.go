package cmd

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// bindWorkspaceTo puts the test in a fresh workspace bound to apiURL, the way
// `modernpath env --set=<env>` binds a real one.
func bindWorkspaceTo(t *testing.T, apiURL string) {
	t.Helper()
	t.Chdir(t.TempDir())
	if err := config.WriteConfig(&config.Config{APIURL: apiURL}); err != nil {
		t.Fatalf("WriteConfig: %v", err)
	}
}

// withEmptyStdin points os.Stdin at an empty file, so an interactive token
// prompt reads EOF and returns instead of blocking on the test runner's stdin.
func withEmptyStdin(t *testing.T) {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "stdin"))
	if err != nil {
		t.Fatalf("create stdin: %v", err)
	}
	orig := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = orig
		_ = f.Close()
	})
}

// The login binds the credential to the environment the workspace is set to.
// A login that ignored the binding sent every credential to production and
// rebound the workspace there (saveTokens writes the URL it authenticated
// against), silently moving every later command off the chosen environment.
func TestBareAuthHonoursTheConfiguredEnvironment(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		return &zitadel.Token{AccessToken: "at", RefreshToken: "rt"}, nil
	})
	defer restore()
	withAuthFlags(t, false, false, "", "", false)

	bindWorkspaceTo(t, zitadel.TestProfile.APIURL)
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.APIURL != zitadel.TestProfile.APIURL {
		t.Fatalf("API URL = %q, want the configured %q — the login must not rebind the workspace", cfg.APIURL, zitadel.TestProfile.APIURL)
	}
}

// The device flow runs against the issuer that goes with the configured host,
// never a different plane's.
func TestBareAuthUsesTheIssuerForTheConfiguredHost(t *testing.T) {
	var gotIssuer string
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		gotIssuer = p.Issuer
		return &zitadel.Token{AccessToken: "at", RefreshToken: "rt"}, nil
	})
	defer restore()
	withAuthFlags(t, false, false, "", "", false)

	bindWorkspaceTo(t, zitadel.TestProfile.APIURL)
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if gotIssuer != zitadel.TestProfile.Issuer {
		t.Fatalf("issuer = %q, want the test plane's %q", gotIssuer, zitadel.TestProfile.Issuer)
	}
}

// A pasted token is validated against — and saved for — the configured host,
// not a baked-in profile the workspace is not bound to.
func TestTokenFlagBindsToTheConfiguredEnvironment(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		t.Fatal("--token must not start the device flow")
		return nil, nil
	})
	defer restore()

	srv, hits := systemsServer(t, 7)
	withAuthFlags(t, false, false, "pasted-jwt", "", false)

	bindWorkspaceTo(t, srv.URL)
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("the token must be validated against the configured host exactly once, got %d calls", hits.Load())
	}
	cfg, err := config.ReadConfig()
	if err != nil || cfg.APIURL != srv.URL {
		t.Fatalf("the configured host must stay bound: cfg=%+v err=%v", cfg, err)
	}
}

// A host with no baked-in issuer — the legacy beta server, a custom URL — has
// no device flow the CLI can run, so a bare login falls to the pasted-token
// prompt instead of logging in to a different plane.
func TestBareAuthOnAHostWithoutAProfileDoesNotRunTheDeviceFlow(t *testing.T) {
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		t.Fatal("a host outside the baked-in profiles has no issuer to run a device flow against")
		return nil, nil
	})
	defer restore()
	withAuthFlags(t, false, false, "", "", false)

	bindWorkspaceTo(t, config.BetaAPIURL)
	withEmptyStdin(t)
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatalf("ReadConfig: %v", err)
	}
	if cfg.APIURL != config.BetaAPIURL {
		t.Fatalf("API URL = %q, want the configured %q", cfg.APIURL, config.BetaAPIURL)
	}
}

// --test names a plane outright, so it wins over the stored binding.
func TestTestFlagOverridesTheConfiguredEnvironment(t *testing.T) {
	var gotIssuer string
	restore := stubZitadelLogin(func(ctx context.Context, p zitadel.Profile, out io.Writer, _ zitadel.Options) (*zitadel.Token, error) {
		gotIssuer = p.Issuer
		return &zitadel.Token{AccessToken: "at", RefreshToken: "rt"}, nil
	})
	defer restore()
	withAuthFlags(t, false, true, "", "", false)

	bindWorkspaceTo(t, zitadel.ProdProfile.APIURL)
	if err := runAuth(authCmd, nil); err != nil {
		t.Fatalf("runAuth: %v", err)
	}
	if gotIssuer != zitadel.TestProfile.Issuer {
		t.Fatalf("issuer = %q, want the test plane's %q", gotIssuer, zitadel.TestProfile.Issuer)
	}
}
