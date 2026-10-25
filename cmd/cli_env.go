package cmd

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// ---------------------------------------------------------------- plumbing

type cliEnv struct {
	Root           string // workspace root (parent of .modernpath)
	APIURL         string
	SystemID       int
	Config         *config.Config
	Auth           *config.Auth
	CurrentRelease string // REQ-CROSS-017: the envelope release stamp ("" = unscoped)
	token          string
	// callTimeout bounds each API call; zero means the 120s default. migrate
	// run raises it: a first import's archival document ingest is legitimately
	// minutes server-side, and an abandoned batch keeps running without the
	// client.
	callTimeout time.Duration
	// tokenExpiry is the stored credential's known expiry, zero when none is
	// known, so a 401 can say whether the token had already expired
	// (REQ-CROSS-389).
	tokenExpiry time.Time
	// storeRevision is the revision the server named on its latest response
	// (storeRevisionHeader), empty while none has named one.
	storeRevision string
}

// credentialError marks a failure that re-running the failing command cannot
// fix: the fix is a different command (`modernpath auth`).
// Callers that suggest a repair ask errors.As for this type before pointing
// the reader at a retry loop.
type credentialError struct {
	err    error
	repair string
	// expired marks a token past its expiry, which outage mode can still
	// carry while the server is unreachable (REQ-CROSS-536 AC5).
	expired bool
}

func (e credentialError) Error() string { return e.err.Error() }

func (e credentialError) Unwrap() error { return e.err }

// bindingError marks a workspace that names no system: `init` or
// `init` fixes it, re-running the failing command does not. Callers that can
// degrade rather than exit — `feedback`, whose whole job is not to lose the
// line — ask errors.As for it (REQ-CROSS-434; BACKLOG-TOOL-74).
type bindingError struct {
	err error
	// why is the short reason a caller that degrades names; empty means the
	// workspace names no system.
	why string
}

func (e bindingError) Error() string { return e.err.Error() }

func (e bindingError) Unwrap() error { return e.err }

// credentialRejected turns a 401 into a statement about the credential. A bare
// `server 401` is the shape of a rejected request; the reader should not have
// to infer that the request itself was fine and the credential was not. A 401
// does not say WHY the credential was rejected — expired, revoked, malformed,
// or presented to a server that never issued it — so the message names the
// possibilities rather than asserting the common one.
func (e *cliEnv) credentialRejected() error {
	repair := authRepairCommand(e.APIURL)
	if !e.tokenExpiry.IsZero() && !e.tokenExpiry.After(time.Now()) {
		// The one cause the client can know for certain (REQ-CROSS-389).
		return credentialError{
			err:    fmt.Errorf("server rejected the session token (expired at %s) — run '%s' to sign in again", e.tokenExpiry.UTC().Format(time.RFC3339), repair),
			repair: repair,
		}
	}
	return credentialError{
		err:    fmt.Errorf("server rejected the session token — expired, revoked, or issued for a different server; run '%s' to sign in again", repair),
		repair: repair,
	}
}

func authRepairCommand(apiURL string) string {
	switch apiURL {
	case zitadel.ProdProfile.APIURL:
		return "modernpath auth --sso"
	case zitadel.TestProfile.APIURL:
		return "modernpath auth --sso --test"
	case config.LocalAPIURL:
		return "modernpath auth --local"
	default:
		return "modernpath auth --api-url=" + apiURL
	}
}

func cliBindingLoad() (*cliEnv, error) {
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		return nil, bindingError{err: fmt.Errorf("not connected — run 'modernpath init --system-id <id>' in the workspace root")}
	}

	cfg, err := commandConfig()
	if err != nil {
		return nil, err
	}
	if cfg.SystemID == 0 {
		return nil, bindingError{err: fmt.Errorf("no system_id in %s/config.json — run 'modernpath init --system-id <id>'", config.ConfigDir)}
	}

	// REQ-CROSS-535 AC5: an address that is not a URL is a configuration
	// error, refused before anything is sent and never taken as an outage.
	if err := validAPIURL(cfg.APIURL); cfg.APIURL != "" && err != nil {
		return nil, fmt.Errorf("%v in %s/config.json — run 'modernpath init --api-url <url>'", err, config.ConfigDir)
	}
	env := &cliEnv{
		Root:           filepath.Dir(cfgDir),
		APIURL:         cfg.APIURL,
		SystemID:       cfg.SystemID,
		Config:         cfg,
		CurrentRelease: cfg.CurrentRelease,
	}
	if env.APIURL == "" {
		env.APIURL = config.DefaultAPIURL
	}
	return env, nil
}

// commandConfig reads the workspace configuration and applies the root
// --api-url override used by commands that target the configured server.
// Authentication, environment selection and init resolve their targets in
// dedicated flows and do not use this helper.
func commandConfig() (*config.Config, error) {
	cfg, err := config.ReadConfig()
	if err != nil {
		return nil, err
	}
	if apiURL != "" {
		cfg.APIURL = apiURL
	}
	return cfg, nil
}

// cliCredentialLoad is the binding plus the three credential statements —
// no binding, no or unreadable bearer, expired token — and nothing sent. The
// CLI commands add the reachability probe (cliEnvLoad); the api-client
// verbs (`docs sync`, `search`, `ask`, `read-doc`, `read-file`) stop here
// (REQ-CROSS-405): the same refusals, the same repair command, before any
// request.
func cliCredentialLoad() (*cliEnv, error) { return credentialLoad(true) }

// credentialLoad is cliCredentialLoad with the refresh made optional. A
// caller that cannot wait for a refresh to finish passes false: the stored
// token is used while it is valid and refused once expired (REQ-CROSS-405).
func credentialLoad(refresh bool) (*cliEnv, error) {
	env, err := cliBindingLoad()
	if err != nil {
		return nil, err
	}

	auth, err := config.ReadAuth()
	if err != nil {
		repair := authRepairCommand(env.APIURL)
		return nil, credentialError{
			err:    fmt.Errorf("auth.json is unreadable (%v) — fix it or run '%s'", err, repair),
			repair: repair,
		}
	}
	if strings.TrimSpace(auth.Token) == "" {
		repair := authRepairCommand(env.APIURL)
		return nil, credentialError{
			err:    fmt.Errorf("no bearer in auth.json — run '%s'", repair),
			repair: repair,
		}
	}
	// REQ-CROSS-389: refresh, warn or refuse on the credential's own expiry
	// before anything leaves the process — the reachability probe included.
	if refresh {
		auth, err = ensureFreshCredential(env, auth, time.Now())
		if err != nil {
			return nil, err
		}
	} else if expiry := storedExpiry(auth); !expiry.IsZero() && !expiry.After(time.Now()) {
		return nil, expiredCredential(expiry, authRepairCommand(env.APIURL))
	}
	env.token = auth.Token
	env.tokenExpiry = storedExpiry(auth)
	env.Auth = auth
	return env, nil
}

// validAPIURL refuses a server address that is not an http or https URL
// with a host, such as 127.0.0.1:9 without a scheme.
func validAPIURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("the server address %q is not a URL (it needs http:// or https:// and a host)", raw)
	}
	return nil
}

// apiClientCredentialLoad is cliCredentialLoad for the api-client verbs
// (`docs sync`, `search`, `ask`, `read-doc`, `read-file`): the same
// credential statements, but an unbound workspace is told to run `init` —
// the on-ramp — rather than a `init` with a system id a fresh
// checkout does not have (PR #487 review, finding 8).
func apiClientCredentialLoad() (*cliEnv, error) { return apiClientLoad(true) }

// apiClientLoadWithoutRefresh is apiClientCredentialLoad for a caller that
// returns at a deadline — the context hook. Its process exits then, and a
// refresh cut off after the issuer rotated the refresh token, but before
// auth.json was written, would leave a dead refresh token on disk.
func apiClientLoadWithoutRefresh() (*cliEnv, error) { return apiClientLoad(false) }

func apiClientLoad(refresh bool) (*cliEnv, error) {
	if cfgDir, err := config.FindConfigDir(); err != nil || cfgDir == "" {
		return nil, fmt.Errorf("no system is bound here — run 'modernpath init' in the repository root (or 'modernpath init --system-id <id>' for a system you already know)")
	}
	if cfg, err := commandConfig(); err == nil && cfg.SystemID == 0 {
		return nil, fmt.Errorf("no system is bound in %s/config.json — run 'modernpath init' (or 'modernpath init --system-id <id>' for a system you already know)", config.ConfigDir)
	}
	return credentialLoad(refresh)
}

// cliEnvLoad loads the authenticated workspace and checks its system binding.
func cliEnvLoad() (*cliEnv, error) {
	env, err := cliCredentialLoad()
	if err != nil {
		return nil, err
	}
	systems, err := listSystemsFn(env.APIURL, env.token)
	if err == nil && !systemReachable(systems, env.SystemID) {
		return nil, bindingError{err: fmt.Errorf("%s", systemMismatchMessage(env.SystemID, systems)), why: "the bound system is not reachable"}
	}
	return env, nil
}
