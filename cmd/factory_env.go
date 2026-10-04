package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

// ---------------------------------------------------------------- plumbing

type factoryEnv struct {
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
	// storeRevision is the last x-modernpath-store-revision the server sent
	// (REQ-CROSS-348).
	storeRevision string
	// tokenExpiry is the stored credential's known expiry, zero when none is
	// known, so a 401 can say whether the token had already expired
	// (REQ-CROSS-389).
	tokenExpiry time.Time
	// contractVersion is the sync contract version the server advertised on
	// this process's responses (REQ-CROSS-390); "" until served. contract is
	// the advertisement, fetched once per process before the first write.
	contractVersion string
	contract        *serverContract
	warned          map[string]bool
}

// credentialError marks a failure that re-running the failing command cannot
// fix: the fix is a different command (`modernpath auth`).
// Callers that suggest a repair ask errors.As for this type before pointing
// the reader at a retry loop.
type credentialError struct {
	err    error
	repair string
}

func (e credentialError) Error() string { return e.err.Error() }

func (e credentialError) Unwrap() error { return e.err }

// bindingError marks a workspace that names no system: `factory connect` or
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
func (e *factoryEnv) credentialRejected() error {
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

func factoryBindingLoad() (*factoryEnv, error) {
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		return nil, bindingError{err: fmt.Errorf("not connected — run 'modernpath factory connect --system <id>' in the workspace root")}
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.SystemID == 0 {
		return nil, bindingError{err: fmt.Errorf("no system_id in %s/config.json — run 'modernpath factory connect --system <id>'", config.ConfigDir)}
	}

	env := &factoryEnv{
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

// factoryCredentialLoad is the binding plus the three credential statements —
// no binding, no or unreadable bearer, expired token — and nothing sent. The
// factory verbs add the reachability probe (factoryEnvLoad); the api-client
// verbs (`docs sync`, `search`, `ask`, `read-doc`, `read-file`) stop here
// (REQ-CROSS-405): the same refusals, the same repair command, before any
// request.
func factoryCredentialLoad() (*factoryEnv, error) { return credentialLoad(true) }

// credentialLoad is factoryCredentialLoad with the refresh made optional. A
// caller that cannot wait for a refresh to finish passes false: the stored
// token is used while it is valid and refused once expired (REQ-CROSS-405).
func credentialLoad(refresh bool) (*factoryEnv, error) {
	env, err := factoryBindingLoad()
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

// apiClientCredentialLoad is factoryCredentialLoad for the api-client verbs
// (`docs sync`, `search`, `ask`, `read-doc`, `read-file`): the same
// credential statements, but an unbound workspace is told to run `init` —
// the on-ramp — rather than a `factory connect` with a system id a fresh
// checkout does not have (PR #487 review, finding 8).
func apiClientCredentialLoad() (*factoryEnv, error) { return apiClientLoad(true) }

// apiClientLoadWithoutRefresh is apiClientCredentialLoad for a caller that
// returns at a deadline — the context hook. Its process exits then, and a
// refresh cut off after the issuer rotated the refresh token, but before
// auth.json was written, would leave a dead refresh token on disk.
func apiClientLoadWithoutRefresh() (*factoryEnv, error) { return apiClientLoad(false) }

func apiClientLoad(refresh bool) (*factoryEnv, error) {
	if cfgDir, err := config.FindConfigDir(); err != nil || cfgDir == "" {
		return nil, fmt.Errorf("no system is bound here — run 'modernpath init' in the repository root (or 'modernpath factory connect --system <id>' for a system you already know)")
	}
	if cfg, err := config.ReadConfig(); err == nil && cfg.SystemID == 0 {
		return nil, fmt.Errorf("no system is bound in %s/config.json — run 'modernpath init' (or 'modernpath factory connect --system <id>' for a system you already know)", config.ConfigDir)
	}
	return credentialLoad(refresh)
}

func factoryEnvLoad() (*factoryEnv, error) {
	env, err := factoryCredentialLoad()
	if err != nil {
		return nil, err
	}

	// REQ-CROSS-282: refuse before any caller's env.call — factoryEnvLoad is
	// the single chokepoint every credentialed factory subcommand goes
	// through, so this protects all of them, not just sync. A check that
	// itself errors fails open: "couldn't check" is never "confirmed
	// unreachable," and a transient outage must never block every command.
	if systems, serr := listSystemsFn(env.APIURL, env.token); serr == nil {
		if !systemReachable(systems, env.SystemID) {
			// A binding the credential cannot reach is fixed by `factory
			// connect`, like no binding at all (REQ-CROSS-434).
			return nil, bindingError{err: fmt.Errorf("%s", systemMismatchMessage(env.SystemID, systems)), why: "the bound system is not reachable"}
		}
	}

	return env, nil
}

// releaseWarning names the actual next step for an unscoped sync. Advice must
// be followable: "select a release" is wrong for a project that has no
// registry, because creating one is a human product decision the CLI must not
// make (REQ-CROSS-177). A fresh derived workspace is base work by design.
func releaseWarning(root string) string {
	// SR-CROSS-328: store-backed, process/releases.md is retired (REQ-CROSS-329),
	// so its absence is the declared configuration — not a missing registry.
	// The active release and its USER: source live in the store; point the
	// reader there rather than at a file the flip deleted.
	if storeBackedWorkspace(root) {
		return "no current release stamped for sync — this work lands in the system's base release; the active release and its USER: source live in the store (read: 'modernpath working-set pull selection' / 'modernpath factory status'), not in a registry file"
	}
	if _, err := os.Stat(filepath.Join(root, "process", "releases.md")); err != nil {
		return "no release registry — this work lands in the system's base release, where the Ledger will show it (REQ-CROSS-283); when this project adopts releases, create process/releases.md and select one with 'modernpath factory release use <slug>'"
	}
	return "no current release — this work lands in the base release; set a delivery release with 'modernpath factory release use <slug>' (registry: process/releases.md)"
}

// storeBackedFromCwd resolves the workspace root the way factoryEnvLoad does —
// by walking up to the .modernpath dir (config.FindConfigDir) — and reports
// whether that root carries the store-backed marker. It therefore matches the
// command's actual workspace even when the CLI is invoked from a subdirectory,
// where a bare os.Getwd() stat would miss the marker and wrongly fall through
// to a file-derived sync. It is a pure filesystem walk with no network, so the
// credential-free hook path uses it too; it falls back to cwd when no
// .modernpath is found.
func storeBackedFromCwd() (root string, active bool) {
	if cfgDir, err := config.FindConfigDir(); err == nil && cfgDir != "" {
		root = filepath.Dir(cfgDir)
		return root, storeBackedWorkspace(root)
	}
	if wd, err := os.Getwd(); err == nil {
		return wd, storeBackedWorkspace(wd)
	}
	return "", false
}
