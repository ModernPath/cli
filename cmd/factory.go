package cmd

// factory — the Mission Control / workspace-sync verbs (REQ-CROSS-011,
// SERVER-SYNC-DESIGN §2.3), folded into the product CLI per USER:2026-07-23
// ("let's not reinvent the wheel"). Auth and binding are the CLI's own:
// `.modernpath/config.json` (APIURL + SystemID) and `.modernpath/auth.json`
// (Bearer). Ledger parsing is the CLI's own bundled parsers; the workspace
// carries no second implementation to shell out to.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/opschema"
	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/modernpath/cli/internal/zitadel"
	"github.com/spf13/cobra"
)

var factoryCmd = &cobra.Command{
	Use:     "factory",
	Aliases: []string{"mc"},
	Short:   "Mission Control: sync the workspace, answer gates, track the loop",
	Long: `The workspace<->server datasync and decision surface (Mission Control).

Bind once with 'modernpath factory connect --system <id>', then:
  release   select the current release; sync stamps + scopes to it
  sync      push requirements/epics/gates/events (idempotent, hash-diffed)
  gates     the open decision queue
  answer    record a USER: decision (first-wins on the server)
  pull      server-born answers -> workspace records (--apply acks the echo)
  evidence  post a test/CI run as evidence
  drift     compare evidence shas to the working tree (Done decays)
  watch     the daemon: sync + heartbeat + pull --apply on a cadence`,
}

// ---------------------------------------------------------------- plumbing

type factoryEnv struct {
	Root           string // workspace root (parent of .modernpath)
	APIURL         string
	SystemID       int
	CurrentRelease string // REQ-CROSS-017: the envelope release stamp ("" = unscoped)
	token          string
	// callTimeout bounds each API call; zero means the 120s default. migrate
	// run raises it: a first import's archival document ingest is legitimately
	// minutes server-side, and an abandoned batch keeps running without the
	// client.
	callTimeout time.Duration
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

// credentialRejected turns a 401 into a statement about the credential. A bare
// `server 401` is the shape of a rejected request; the reader should not have
// to infer that the request itself was fine and the credential was not. A 401
// does not say WHY the credential was rejected — expired, revoked, malformed,
// or presented to a server that never issued it — so the message names the
// possibilities rather than asserting the common one.
func (e *factoryEnv) credentialRejected() error {
	repair := authRepairCommand(e.APIURL)
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
		return nil, fmt.Errorf("not connected — run 'modernpath factory connect --system <id>' in the workspace root")
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		return nil, err
	}
	if cfg.SystemID == 0 {
		return nil, fmt.Errorf("no system_id in %s/config.json — run 'modernpath factory connect --system <id>'", config.ConfigDir)
	}

	env := &factoryEnv{
		Root:           filepath.Dir(cfgDir),
		APIURL:         cfg.APIURL,
		SystemID:       cfg.SystemID,
		CurrentRelease: cfg.CurrentRelease,
	}
	if env.APIURL == "" {
		env.APIURL = config.DefaultAPIURL
	}
	return env, nil
}

func factoryEnvLoad() (*factoryEnv, error) {
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
	env.token = auth.Token

	// REQ-CROSS-282: refuse before any caller's env.call — factoryEnvLoad is
	// the single chokepoint every credentialed factory subcommand goes
	// through, so this protects all of them, not just sync. A check that
	// itself errors fails open: "couldn't check" is never "confirmed
	// unreachable," and a transient outage must never block every command.
	if systems, serr := listSystemsFn(env.APIURL, env.token); serr == nil {
		if !systemReachable(systems, env.SystemID) {
			return nil, fmt.Errorf("%s", systemMismatchMessage(env.SystemID, systems))
		}
	}

	return env, nil
}

// releaseWarning names the actual next step for an unscoped sync. Advice must
// be followable: "select a release" is wrong for a project that has no
// registry, because creating one is a human product decision the CLI must not
// make (REQ-CROSS-177). A fresh derived workspace is base work by design.
func releaseWarning(root string) string {
	if _, err := os.Stat(filepath.Join(root, "process", "releases.md")); err != nil {
		return "no release registry — this work lands in the system's base release, where the Ledger will show it (REQ-CROSS-283); when this project adopts releases, create process/releases.md and select one with 'modernpath factory release use <slug>'"
	}
	return "no current release — this work lands in the base release; set a delivery release with 'modernpath factory release use <slug>' (registry: process/releases.md)"
}

// REQ-CROSS-176: when a server call is slow enough to look like a hang, say
// something. Threshold and destination are variables so the tests can shrink
// one and capture the other.
var (
	slowCallNoticeAfter           = 10 * time.Second
	slowCallNoticeTo    io.Writer = os.Stderr
)

// A 5xx from the platform edge on a sync chunk is usually a per-request gateway
// timeout on a heavy chunk, not a permanent refusal (RUN:2026-08-31: a cold
// first sync into test-plat answered 504 on a 200-op chunk). Each chunk is its
// own idempotent server-side transaction, so re-POSTing it is safe. These are
// variables so a test can shrink the backoff and stub the sleep; a 4xx (401,
// 422, a validation refusal) is a deterministic answer and is never retried.
var (
	syncRetryMaxAttempts               = 4
	syncRetryBaseBackoff time.Duration = 2 * time.Second
	syncRetrySleep                     = time.Sleep
)

func (e *factoryEnv) call(method, apiPath string, payload any) (int, map[string]any, error) {
	var body *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, e.APIURL+apiPath, body)
	if err != nil {
		return 0, nil, err
	}
	platform.Prepare(req)
	req.Header.Set("Content-Type", "application/json")
	if err := platform.Authorize(req, e.token); err != nil {
		return 0, nil, err
	}

	// REQ-CROSS-176: a call that is taking long says so. Eight sequential
	// calls at 120s each can turn "a few seconds" into silent minutes on a
	// stalled connection, and a slow server is indistinguishable from a hung
	// CLI until this line exists.
	timeout := e.callTimeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	notice := time.AfterFunc(slowCallNoticeAfter, func() {
		fmt.Fprintf(slowCallNoticeTo, "… still waiting on %s %s (slow server or connection; times out at %s)\n", method, apiPath, timeout)
	})
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	notice.Stop()
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	var decoded map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&decoded)

	// Reported here rather than at each call site: all eight `server %d`
	// formatters check err first, so one point covers every factory command.
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, decoded, e.credentialRejected()
	}
	return resp.StatusCode, decoded, nil
}

// postSyncBatch POSTs one batch to /api/v1/sync/batch, retrying a 5xx or a
// transient transport failure with exponential backoff. It returns the last
// (status, body, err), so an exhausted retry reads exactly like a single failed
// call and the chunk/refusal handling downstream is unchanged. A 4xx — a
// credential rejection (status 401, non-nil err) or a server refusal such as
// 422 — is deterministic and returned at once; only a 5xx (status >= 500) or a
// transport error (status 0 with a non-nil err) is retried.
func (e *factoryEnv) postSyncBatch(batchBody map[string]any) (int, map[string]any, error) {
	var (
		status int
		body   map[string]any
		err    error
	)
	for attempt := 1; ; attempt++ {
		status, body, err = e.call("POST", "/api/v1/sync/batch", batchBody)
		retriable := status >= 500 || (status == 0 && err != nil)
		if !retriable || attempt >= syncRetryMaxAttempts {
			return status, body, err
		}
		wait := syncRetryBaseBackoff << (attempt - 1)
		if err != nil {
			printWarning("sync POST failed (%v) — attempt %d/%d, retrying in %s", err, attempt, syncRetryMaxAttempts, wait)
		} else {
			printWarning("sync POST got server %d — attempt %d/%d, retrying in %s", status, attempt, syncRetryMaxAttempts, wait)
		}
		syncRetrySleep(wait)
	}
}

// syncChunkSizeDefault is how many ops one /api/v1/sync/batch POST carries by
// default. MODERNPATH_SYNC_CHUNK_SIZE overrides it: on a platform edge with a
// tight per-request timeout, a large first sync's heaviest chunk can exceed the
// gateway window, and a smaller chunk brings each POST back under it. Bigger is
// fewer round trips; smaller is more, each cheaper server-side.
const syncChunkSizeDefault = 200

// syncChunkSize resolves the per-POST op count from the environment, falling
// back to the default and warning — never failing — on a value that is not a
// positive integer (AGENTS.md "a guard flags, it does not delete").
func syncChunkSize() int {
	raw := strings.TrimSpace(os.Getenv("MODERNPATH_SYNC_CHUNK_SIZE"))
	if raw == "" {
		return syncChunkSizeDefault
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		printWarning("ignoring MODERNPATH_SYNC_CHUNK_SIZE=%q (want a positive integer); using %d", raw, syncChunkSizeDefault)
		return syncChunkSizeDefault
	}
	return n
}

// workspaceOps builds the sync op batch from the CLI-bundled parsers, driven
// by .modernpath/manifest.json (defaults = the modernpath-v1 layout —
// REQ-CROSS-013). Ops are validated against
// the vendored op schema before they can reach the wire (REQ-CROSS-012).
// warnings carries the loud mandated-gap report (D2: never a silent skip).
func (e *factoryEnv) workspaceOps() (ops []map[string]any, warnings []string, err error) {
	m, fromFile, err := manifest.Load(e.Root)
	if err != nil {
		return nil, nil, err
	}
	if !fromFile {
		// zero-config: defaults match this workspace's layout
		m = manifest.Default()
	}
	data, warnings := rdd.Snapshot(e.Root, m)
	built := rdd.BuildOps(data, func(rel string) string { return rdd.ReadEpicRecord(e.Root, rel) },
		time.Now().UTC().Format("2006-01-02"))
	// SR-CMP-9022: the coverage receipt rides every batch that has ledgers to
	// measure. Deterministic (no timestamp; hash over the summary only), so an
	// unchanged tree re-syncs an unchanged op.
	if covOp, ok := rdd.BuildCoverageOp(e.Root, "modernpath@"+Version); ok {
		built = append(built, covOp)
	}
	for _, op := range built {
		ops = append(ops, map[string]any{"type": op.Type, "payload": op.Payload})
	}
	if err := opschema.ValidateOps(ops); err != nil {
		return nil, warnings, fmt.Errorf("built ops fail schema v%d validation: %w", opschema.SchemaVersion, err)
	}
	return ops, warnings, nil
}

// workspaceTracePaths returns each requirement's traced file paths (drift's
// diff scope) from the same extraction path sync uses.
func (e *factoryEnv) workspaceTracePaths() (map[string][]string, error) {
	m, _, err := manifest.Load(e.Root)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	data, _ := rdd.Snapshot(e.Root, m)
	for _, req := range data.Reqs {
		if paths := rdd.ExtractTracePaths(req.Detail); len(paths) > 0 {
			out[req.ID] = paths
		}
	}
	return out, nil
}

func printGapWarnings(warnings []string) {
	for _, w := range warnings {
		printWarning("%s", w)
	}
}

func dataOf(decoded map[string]any) map[string]any {
	if d, ok := decoded["data"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

func str(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func machineFingerprint(root string) string {
	host, _ := os.Hostname()
	return host + ":" + root
}

func gitOut(root string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------------------------------------------------------------- connect / status

var factoryConnectSystem int

// systemLookup reads a bound system's identity from the server.
type systemLookup func(id int) (name, slug string, err error)

// resolveBinding sets the system id and API URL on cfg. A systemID of 0 means
// "keep the system this workspace is already bound to", which is what lets a
// re-run repair an existing config instead of demanding a re-init.
func resolveBinding(cfg *config.Config, systemID int, apiURLFlag string) error {
	if systemID == 0 && cfg.SystemID == 0 {
		return fmt.Errorf("usage: modernpath factory connect --system <id> [--api-url <url>]")
	}
	if systemID != 0 && systemID != cfg.SystemID {
		// a different system: drop the old identity rather than let the new id
		// wear the old name if the lookup below cannot reach the server
		cfg.SystemID = systemID
		cfg.SystemName, cfg.SystemSlug = "", ""
	}
	if apiURLFlag != "" {
		if cfg.APIURL != "" && apiURLFlag != cfg.APIURL {
			// a different server: the recorded identity was read from the OLD
			// one, and the same numeric id on another host is another system
			cfg.SystemName, cfg.SystemSlug = "", ""
		}
		cfg.APIURL = apiURLFlag
	}
	if cfg.APIURL == "" {
		cfg.APIURL = config.LocalAPIURL
	}
	return nil
}

// recordSystemIdentity fills in the system's name and slug from the server.
// Its error is advisory: the binding is written either way, because binding a
// workspace must not require the network.
func recordSystemIdentity(cfg *config.Config, lookup systemLookup) error {
	if lookup == nil {
		return nil
	}
	name, slug, err := lookup(cfg.SystemID)
	if err != nil {
		return err // keep whatever was already recorded for this same system
	}
	cfg.SystemName, cfg.SystemSlug = name, slug
	return nil
}

// connectResult reports what a connect achieved beyond binding.
type connectResult struct {
	// IdentityErr is advisory: the binding is saved whether or not the
	// system's name and slug could be read.
	IdentityErr error
}

// connectWorkspace binds, persists, and records the system's identity.
func connectWorkspace(cfg *config.Config, systemID int, apiURLFlag string,
	save func(*config.Config) error, lookup systemLookup) (connectResult, error) {

	if err := resolveBinding(cfg, systemID, apiURLFlag); err != nil {
		return connectResult{}, err
	}
	// Save the binding before looking anything up: the lookup authenticates
	// with credentials it reads back from this workspace's own config, so on a
	// first connect it cannot succeed until the config exists.
	if err := save(cfg); err != nil {
		return connectResult{}, err
	}
	res := connectResult{IdentityErr: recordSystemIdentity(cfg, lookup)}
	if res.IdentityErr != nil {
		return res, nil
	}
	return res, save(cfg)
}

var factoryConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Bind this workspace to a System (writes .modernpath/config.json)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, _ := config.ReadConfig()
		if cfg == nil {
			cfg = &config.Config{}
		}
		res, err := connectWorkspace(cfg, factoryConnectSystem, apiURL, config.WriteConfig, serverSystemLookup())
		if err != nil {
			return err
		}
		// The written path, not a relative literal: run from a subdirectory,
		// ".modernpath/config.json" reads as "here" when the binding it updated
		// is a level or more up.
		printSuccess("connected: system %d via %s (%s)", cfg.SystemID, cfg.APIURL, connectedConfigPath())
		if res.IdentityErr != nil {
			printWarning("could not read the system's name and slug: %v\n  the binding is written — %s\n", res.IdentityErr, identityRepairHint(res.IdentityErr))
		}
		return nil
	},
}

// identityRepairHint names the step that can actually fix the failed identity
// lookup. Re-running connect repairs a reachability failure; a credential
// failure re-fails identically until the credential itself is fixed, and
// pointing the reader at a retry loop hides the real repair.
func identityRepairHint(err error) string {
	var ce credentialError
	if errors.As(err, &ce) {
		return fmt.Sprintf("run '%s', then re-run 'modernpath factory connect'", ce.repair)
	}
	return "re-run 'modernpath factory connect' once the server is reachable"
}

// connectedConfigPath names the binding that was just written, relative to the
// working directory when that is shorter to read than the absolute path.
func connectedConfigPath() string {
	dir, err := config.FindConfigDir()
	if err != nil || dir == "" {
		return filepath.Join(config.ConfigDir, config.ConfigFile)
	}
	full := filepath.Join(dir, config.ConfigFile)
	if cwd, err := os.Getwd(); err == nil {
		if rel, err := filepath.Rel(cwd, full); err == nil && len(rel) < len(full) {
			return rel
		}
	}
	return full
}

// serverSystemLookup reads identity through the factory lane's bearer.
func serverSystemLookup() systemLookup {
	return func(id int) (string, string, error) {
		env, err := factoryEnvLoad()
		if err != nil {
			return "", "", err
		}
		status, body, err := env.call("GET", fmt.Sprintf("/api/systems/%d", id), nil)
		if err != nil {
			return "", "", err
		}
		if status != 200 {
			return "", "", fmt.Errorf("server %d reading system %d", status, id)
		}
		name, _ := body["name"].(string)
		slug, _ := body["slug"].(string)
		if slug == "" {
			return "", "", fmt.Errorf("system %d returned no slug", id)
		}
		return name, slug, nil
	}
}

var factoryStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the local binding and pending op count",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryBindingLoad()
		if err != nil {
			return err
		}
		release := env.CurrentRelease
		if release == "" {
			release = "(none — sync runs unscoped; set one with 'factory release use <slug>')"
		}
		fmt.Printf("workspace: %s\nserver:    %s\nsystem:    %d\nrelease:   %s\n", env.Root, env.APIURL, env.SystemID, release)

		if ops, warnings, err := env.workspaceOps(); err == nil {
			printGapWarnings(warnings)
			fmt.Printf("pending:   %d ops on next sync (server hash-diffs; unchanged ops are no-ops)\n", len(ops))
		}
		if _, fromFile, err := manifest.Load(env.Root); err == nil {
			if fromFile {
				fmt.Printf("manifest:  %s\n", manifest.Path(env.Root))
			} else {
				fmt.Println("manifest:  defaults (modernpath-v1 layout; write one with 'factory manifest init')")
			}
		}
		return nil
	},
}

// ---------------------------------------------------------------- release

// factory release use|show|clear — the workspace-level release selector
// (REQ-CROSS-017). Writes only local config; the server materializes the
// release (find-or-create by slug) on the first scoped sync. The tracked
// source of truth is process/releases.md — keep the two in step.
var factoryReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Select the current release factory sync stamps and scopes to",
}

var factoryReleaseUseCmd = &cobra.Command{
	Use:   "use <slug>",
	Short: "Set current_release in .modernpath/config.json (e.g. modernpath-v1-09)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := strings.TrimSpace(args[0])
		if slug == "" {
			return fmt.Errorf("usage: modernpath factory release use <slug>")
		}
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = slug
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release: %s (.modernpath/config.json — mirror of the active row in process/releases.md)", slug)
		return nil
	},
}

var factoryReleaseShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Print the current release",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		if cfg.CurrentRelease == "" {
			fmt.Println("(none — sync runs unscoped; set one with 'factory release use <slug>')")
		} else {
			fmt.Println(cfg.CurrentRelease)
		}
		return nil
	},
}

var factoryReleaseClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Unset the current release (sync runs unscoped; existing stamps stay)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.ReadConfig()
		if err != nil {
			return err
		}
		cfg.CurrentRelease = ""
		if err := config.WriteConfig(cfg); err != nil {
			return err
		}
		printSuccess("current release cleared — sync runs unscoped (server stamps are left untouched)")
		return nil
	},
}

// ---------------------------------------------------------------- sync

var factorySyncDryRun bool
var factorySyncJSON bool
var factorySyncNoDocs bool

var (
	factorySyncIfQuiescent bool
	factorySyncTrigger     string
	factorySyncMinInterval time.Duration
)

var factorySyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Push the workspace state: typed op batch + projections + heartbeat",
	RunE: func(cmd *cobra.Command, args []string) error {
		// EPIC-SYNC-009: the hook path — gated, logged, never errors out
		if factorySyncIfQuiescent {
			return factorySyncQuiescent(factorySyncTrigger, factorySyncMinInterval)
		}

		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factorySyncRun(env, factorySyncDryRun)
	},
}

func factorySyncRun(env *factoryEnv, dryRun bool) error {
	ops, warnings, err := env.workspaceOps()
	if err != nil {
		return err
	}
	printGapWarnings(warnings)

	// --no-docs drops the workspace-document ops and sends only the process
	// state — requirements, epics, gates, evidence, sessions (see noDocsFilter).
	if kept, dropped := noDocsFilter(ops, factorySyncNoDocs); dropped > 0 {
		printInfo("--no-docs: skipping %d document op(s); syncing process state only", dropped)
		ops = kept
	}
	// --json owns stdout: a prose banner ahead of the batch makes it unparseable
	// by the very tools the flag exists for.
	if dryRun && factorySyncJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{
			"schema_version": opschema.SchemaVersion,
			"system_id":      env.SystemID,
			"release":        env.CurrentRelease,
			"ops":            ops,
		})
	}
	if env.CurrentRelease == "" {
		// D2 doctrine: never a silent skip — unscoped sync proceeds (it never
		// un-stamps anything) but says so loudly (REQ-CROSS-017).
		printWarning("%s", releaseWarning(env.Root))
		printInfo("factory sync: %d ops (schema v%d)", len(ops), opschema.SchemaVersion)
	} else {
		printInfo("factory sync: %d ops (schema v%d, release %s)", len(ops), opschema.SchemaVersion, env.CurrentRelease)
	}

	if dryRun {
		// A ten-line list of ids cannot answer "did the field I changed come
		// out right?" — which is the only question a dry run is for. --json
		// emits the batch the sync would send, so it can be read back before
		// it lands rather than after (PROCESS §1.10).
		for i, op := range ops {
			if i >= 10 {
				fmt.Printf("  … %d more (use --json for the full batch)\n", len(ops)-10)
				break
			}
			payload, _ := op["payload"].(map[string]any)
			fmt.Printf("  %s  %s\n", str(op, "type"), str(payload, "external_id"))
		}
		return nil
	}

	// A corpus is not a request. A reverse-engineered estate produces hundreds
	// of ops, and one POST carrying all of them exceeds the gateway's window:
	// 935 ops answered 504 three times on a real workspace, and 1191 did the
	// same on another. The batch never reached evaluation, so nothing synced
	// and the size of a workspace silently became a limit on whether it could
	// sync at all.
	//
	// Chunks go in order, because later ops reference earlier ones. Each chunk
	// is its own transaction server-side and the shadow makes a replay a no-op,
	// so a failure part-way leaves the earlier chunks landed rather than
	// half-applied — and re-running finishes the job. The size is tunable with
	// MODERNPATH_SYNC_CHUNK_SIZE: a platform edge with a tight per-request
	// timeout can 504 on the heaviest 200-op chunk of a large first sync.
	chunkSize := syncChunkSize()

	newBatch := func(chunk []map[string]any) map[string]any {
		body := map[string]any{
			"schema_version": opschema.SchemaVersion,
			"system_id":      env.SystemID,
			"ops":            chunk,
		}
		if env.CurrentRelease != "" {
			body["release"] = env.CurrentRelease
		}
		return body
	}

	chunks := make([][]map[string]any, 0, (len(ops)+chunkSize-1)/chunkSize)
	for start := 0; start < len(ops); start += chunkSize {
		end := start + chunkSize
		if end > len(ops) {
			end = len(ops)
		}
		chunks = append(chunks, ops[start:end])
	}
	if len(chunks) == 0 {
		chunks = append(chunks, nil)
	}
	if len(chunks) > 1 {
		printInfo("sending in %d chunks of up to %d ops", len(chunks), chunkSize)
	}

	var (
		status      int
		body        map[string]any
		mergedCount = map[string]int{}
		landed      int
	)
	for index, chunk := range chunks {
		batchBody := newBatch(chunk)
		status, body, err = env.postSyncBatch(batchBody)
		if err != nil {
			// Say which chunk, and that the earlier ones are already in the
			// store — otherwise a re-run looks like it might double-apply.
			if index > 0 {
				printWarning("chunk %d/%d failed; %d op(s) already landed — re-running syncs the rest",
					index+1, len(chunks), landed)
			}
			return err
		}
		if status != 200 {
			// A 5xx that survived retries (or any non-422 refusal) ends the run;
			// a 422 falls through to the unknown-op recovery below. Either way,
			// say how far it got: the earlier chunks are already committed
			// server-side, so a re-run resumes rather than re-applies.
			if status != 422 && index > 0 {
				printWarning("chunk %d/%d failed (server %d); %d op(s) already landed — re-running syncs the rest",
					index+1, len(chunks), status, landed)
			}
			// Fall through to the shared handling below with this chunk's
			// body, so a refusal reads the same whether or not it chunked.
			ops = chunk
			break
		}
		if results, ok := dataOf(body)["results"].([]any); ok {
			for _, r := range results {
				m, _ := r.(map[string]any)
				mergedCount[str(m, "result")]++
			}
		}
		landed += len(chunk)
	}
	batchBody := newBatch(ops)
	// REQ-CROSS-089: the server halts the batch on the first op type it does not
	// recognise, so a CLI newer than the server does not sync less — it syncs
	// NOTHING, silently, because the hooks are fire-and-forget. Retry once
	// without that op kind so the rest of the workspace still lands, and say
	// plainly what was left behind.
	if status == 422 {
		if kind := unknownOpType(body); kind != "" {
			kept, dropped := dropOpsOfType(ops, kind)
			fmt.Printf("⚠ this server does not understand %s yet — syncing the other %d ops and leaving %d behind.\n",
				kind, len(kept), dropped)
			fmt.Printf("  They will land once the server is upgraded; nothing is lost locally.\n")
			batchBody["ops"] = kept
			status, body, err = env.postSyncBatch(batchBody)
			if err != nil {
				return err
			}
		}
	}
	if status != 200 {
		return fmt.Errorf("server %d: %v", status, body["error"])
	}

	counts := mergedCount
	if len(counts) == 0 {
		if results, ok := dataOf(body)["results"].([]any); ok {
			for _, r := range results {
				m, _ := r.(map[string]any)
				counts[str(m, "result")]++
			}
		}
	}
	parts := make([]string, 0, len(counts))
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(parts)
	// REQ-CROSS-136: a conflict is the server REFUSING this change because the row
	// was edited on the platform. Printing it under a green "ok:" invites the
	// reader to skim past a refusal, so say it plainly instead.
	if syncHadConflicts(counts) {
		printWarning("synced with refusals: %s", strings.Join(parts, ", "))
		fmt.Printf("  %d row(s) were edited on the platform and kept — this workspace's version was not applied.\n", counts["conflict"])
		fmt.Printf("  Reconcile by hand: the server's copy wins until the workspace matches it.\n")
	} else {
		printSuccess("ok: %s", strings.Join(parts, ", "))
	}

	// Both lanes stamp, because both landed the batch. This used to be written
	// only by the hook lane, so `modernpath status` told a workspace without
	// sync hooks that its state had never synced — and told it to run the very
	// command that was, in fact, syncing it (REQ-CROSS-117).
	if err := recordSyncSucceeded(env.Root); err != nil {
		printWarning("sync landed, but its status stamp could not be written: %v\n  'modernpath status' will still report the previous state sync, and hook debounce will misjudge\n", err)
	}

	// idempotent server-side projections (board history + approvals)
	_, _, _ = env.call("POST", "/api/v1/sync/project", map[string]any{"system_id": env.SystemID})

	// factory session heartbeat — the loop is an observable server entity
	hbStatus, hbBody, _ := env.call("POST", "/api/v1/sync/heartbeat", map[string]any{
		"system_id":           env.SystemID,
		"workspace_ref":       filepath.Base(env.Root),
		"machine_fingerprint": machineFingerprint(env.Root),
		"branch":              gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
		"agent_slug":          "factory-loop",
		"current_ref":         gitOut(env.Root, "log", "-1", "--format=%s"),
		"capabilities":        map[string]any{"verbs": []string{"sync", "gates", "answer", "pull", "evidence", "drift", "watch"}},
	})
	if hbStatus == 200 {
		if session, ok := dataOf(hbBody)["session"].(map[string]any); ok {
			printInfo("session: %s (%s)", str(dataOf(hbBody), "result"), str(session, "current_ref"))
		}
		// REQ-PLN-135 §135.4: append the branch/commit signals, learn the current
		// focus from the heartbeat echo, and post a conclusion inline if the rule
		// fires. Off the hook's latency budget — sync already reached the server.
		recordFocusSignalsFromSync(env, hbBody)
	}
	return nil
}

// ---------------------------------------------------------------- gates / answer

var factoryGatesCmd = &cobra.Command{
	Use:   "gates",
	Short: "The open decision queue (questions, decisions, approvals)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		gates, _ := dataOf(body)["gates"].([]any)
		if len(gates) == 0 {
			printSuccess("no open gates — the queue is clear")
			return nil
		}
		shown := filterGatesByKind(gates, gatesKind)
		for _, g := range shown {
			m, _ := g.(map[string]any)
			fmt.Printf("\n%s  [%s]  %s\n", str(m, "external_id"), str(m, "kind"), str(m, "title"))
			if rec := str(m, "recommendation"); rec != "" {
				fmt.Printf("  recommends: %.120s\n", rec)
			}
			if options, ok := m["options"].([]any); ok {
				for _, o := range options {
					om, _ := o.(map[string]any)
					fmt.Printf("  - %s: %.100s\n", str(om, "key"), str(om, "label"))
				}
			}
		}
		fmt.Print(gateQueueFooter(len(shown), gateKindBreakdown(gates), gatesKind))
		return nil
	},
}

// kindCount is one kind of gate and how many of it are open.
type kindCount struct {
	kind string
	n    int
}

// gateKindBreakdown counts the queue by kind, largest first, ties by name so the
// order is stable between runs.
func gateKindBreakdown(gates []any) []kindCount {
	n := map[string]int{}
	for _, g := range gates {
		m, _ := g.(map[string]any)
		n[str(m, "kind")]++
	}
	out := make([]kindCount, 0, len(n))
	for k, c := range n {
		out = append(out, kindCount{kind: k, n: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].n != out[j].n {
			return out[i].n > out[j].n
		}
		return out[i].kind < out[j].kind
	})
	return out
}

func filterGatesByKind(gates []any, kind string) []any {
	if kind == "" {
		return gates
	}
	out := []any{}
	for _, g := range gates {
		m, _ := g.(map[string]any)
		if str(m, "kind") == kind {
			out = append(out, g)
		}
	}
	return out
}

// gateQueueFooter closes the listing. A bare total misleads: a queue of 148 is
// read as an approval backlog when 0 of it is approvals and the rest are
// questions and product decisions — different work at a different cadence. The
// kinds are named so the number means something, and a filtered view still
// reports the unfiltered total so narrowing cannot hide the queue.
func gateQueueFooter(shown int, breakdown []kindCount, filter string) string {
	total := 0
	parts := make([]string, 0, len(breakdown))
	for _, kc := range breakdown {
		total += kc.n
		parts = append(parts, fmt.Sprintf("%d %s", kc.n, kc.kind))
	}
	answer := "answer with: modernpath factory answer <id> --text \"…\" [--options k1,k2]"

	if filter == "" {
		return fmt.Sprintf("\n%d open (%s) — %s\n", total, strings.Join(parts, " · "), answer)
	}
	// Not "the queue is clear": there is a queue, it just holds nothing of the
	// kind that was asked for. Reporting it as clear would be this row's own
	// defect one level down.
	if shown == 0 {
		return fmt.Sprintf("\nno open %s — %d open of other kinds (%s)\n", filter, total, strings.Join(parts, " · "))
	}
	return fmt.Sprintf("\n%d %s of %d open (%s) — %s\n", shown, filter, total, strings.Join(parts, " · "), answer)
}

var (
	gatesKind     string
	answerText    string
	answerOptions string
	answerSource  string
)

var factoryAnswerCmd = &cobra.Command{
	Use:   "answer <external_id>",
	Short: "Record a USER: decision on a gate (first-wins on the server)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		if answerText == "" {
			return fmt.Errorf("--text is required — the answer is recorded verbatim as the USER: decision")
		}
		payload := map[string]any{"system_id": env.SystemID, "answer": answerText}
		if answerOptions != "" {
			payload["chosen_option_keys"] = strings.Split(answerOptions, ",")
		}
		if answerSource != "" {
			payload["source_tag"] = answerSource
		}
		status, body, err := env.call("POST", "/api/v1/sync/gates/"+args[0]+"/answer", payload)
		if err != nil {
			return err
		}
		switch status {
		case 200:
			gate, _ := dataOf(body)["gate"].(map[string]any)
			printSuccess("answered %s (%s): %s", str(gate, "external_id"), str(gate, "source_tag"), str(gate, "answer"))
		case 409:
			errMap, _ := body["error"].(map[string]any)
			winner, _ := errMap["winner"].(map[string]any)
			return fmt.Errorf("already answered (first-wins) — winner: %q (%s)", str(winner, "answer"), str(winner, "source_tag"))
		default:
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		return nil
	},
}

// ---------------------------------------------------------------- pull

var pullApply bool

var factoryPullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Server-born answers -> workspace records; --apply records + acks (tracked job)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factoryPullRun(env, pullApply)
	},
}

func factoryPullRun(env *factoryEnv, apply bool) error {
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/intents?system_id=%d", env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("server %d: %v", status, body["error"])
	}
	intents, _ := dataOf(body)["intents"].([]any)
	if len(intents) == 0 {
		printSuccess("no pending intents — workspace and server agree")
		return nil
	}

	for _, it := range intents {
		m, _ := it.(map[string]any)
		if str(m, "kind") == "spec_updated" {
			state := "clean"
			if b, _ := m["conflict"].(bool); b {
				state = "CONFLICT (both sides changed)"
			}
			fmt.Printf("\n%s  server-edited spec v%v — %s\n", str(m, "external_id"), m["version"], state)
			continue
		}
		fmt.Printf("\n%s  answered %s (%s)\n  Q: %s\n  A: %s\n",
			str(m, "external_id"), str(m, "answered_at"), str(m, "source_tag"), str(m, "title"), str(m, "answer"))
	}
	if !apply {
		fmt.Printf("\n%d pending — record + echo with: modernpath factory pull --apply\n", len(intents))
		return nil
	}

	jsonlPath := filepath.Join(env.Root, "answers.jsonl")
	mdPath := filepath.Join(env.Root, "ANSWERS.md")

	for _, it := range intents {
		m, _ := it.(map[string]any)
		externalID := str(m, "external_id")

		// EPIC-SYNC-007: server-edited specs write back to their workspace
		// file; a divergence refuses and stays pending (SCN-SY-072).
		if str(m, "kind") == "spec_updated" {
			conflict, _ := m["conflict"].(bool)
			status, err := applySpecIntent(env.Root, externalID, str(m, "content"), str(m, "synced_sha"), conflict)
			if err != nil {
				return fmt.Errorf("spec write-back failed for %s: %w", externalID, err)
			}
			if status == "conflict" {
				fmt.Printf("✗ CONFLICT %s — both sides changed since the last sync; resolve by hand, then re-run\n", externalID)
				continue
			}
			aStatus, aBody, err := env.call("POST", "/api/v1/sync/spec-intents/ack",
				map[string]any{"system_id": env.SystemID, "external_id": externalID})
			if err != nil || aStatus != 200 {
				return fmt.Errorf("spec ack failed for %s: %d %v (%v)", externalID, aStatus, aBody["error"], err)
			}
			printSuccess("pulled server spec edit → %s (marker reset to SPEC-DRAFT)", externalID)
			continue
		}

		if str(m, "kind") == "rdd_pending_intent" {
			application, err := applyRDDIntent(env, m, "")
			if err != nil {
				return err
			}
			printSuccess("applied %s (%s)", externalID, str(application, "application_revision"))
			continue
		}

		// SCN-SY-022: every apply is a tracked, gate-linked factory job
		jobID := ""
		jStatus, jBody, _ := env.call("POST", "/api/v1/sync/jobs", map[string]any{
			"system_id":           env.SystemID,
			"workspace_ref":       filepath.Base(env.Root),
			"machine_fingerprint": machineFingerprint(env.Root),
			"kind":                "apply_decision",
			"label":               "apply " + externalID + " answer",
			"gate_external_id":    externalID,
		})
		if jStatus == 200 {
			if job, ok := dataOf(jBody)["job"].(map[string]any); ok {
				jobID = str(job, "id")
			}
		}

		now := time.Now().UTC().Format(time.RFC3339)
		record, _ := json.Marshal(map[string]any{
			"type": "answer", "id": externalID, "title": str(m, "title"), "answer": str(m, "answer"),
			"source": str(m, "source_tag"), "origin": "server-downsync", "ts": now,
		})
		appendFile(jsonlPath, string(record)+"\n")
		appendFile(mdPath, fmt.Sprintf("- **%s** · **%s** — %s _(via factory pull)_\n", str(m, "source_tag"), externalID, str(m, "answer")))

		jobRef := "answers.jsonl@" + now
		if jobID != "" {
			jobRef = "factory_job:" + jobID
		}
		aStatus, aBody, err := env.call("POST", "/api/v1/sync/intents/"+externalID+"/ack",
			map[string]any{"system_id": env.SystemID, "applied_state": "applied", "job_ref": jobRef})
		if err != nil || aStatus != 200 {
			return fmt.Errorf("ack failed for %s: %d %v (%v)", externalID, aStatus, aBody["error"], err)
		}
		if jobID != "" {
			_, _, _ = env.call("POST", "/api/v1/sync/jobs/"+jobID+"/finish", map[string]any{
				"system_id": env.SystemID, "status": "done",
				"result_summary": "answer recorded in answers.jsonl + ANSWERS.md, acked applied",
				"log_ref":        "answers.jsonl@" + now,
			})
		}
		printSuccess("applied + acked %s (%s)", externalID, jobRef)
	}
	return nil
}

// ---------------------------------------------------------------- image generation (EPIC-DEC-001)

var imagePurpose string

var factoryImageCmd = &cobra.Command{
	Use:   "image <prompt>",
	Short: "Generate an image via the platform (tenant-stored; prints the URL)",
	Long: "EPIC-DEC-001 (REQ-AGT-027): generates through the governed image service\n" +
		"(Nano Banana 2 Lite first) — the backend stores the image in the tenant's\n" +
		"storage and returns a URL; the CLI never handles bytes.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		payload := map[string]any{"prompt": args[0]}
		if imagePurpose != "" {
			payload["purpose"] = imagePurpose
		}

		status, body, err := env.call("POST", "/api/v1/images", payload)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}

		data := dataOf(body)
		printSuccess("image generated: %s", str(data, "model"))
		fmt.Printf("  url:      %s%s\n", env.APIURL, str(data, "url"))
		fmt.Printf("  size:     %v bytes · latency: %v ms\n", data["byte_size"], data["latency_ms"])
		return nil
	},
}

// ---------------------------------------------------------------- spec down-sync (EPIC-SYNC-007)

// contentSha mirrors Core.Sync.content_sha/1 — sha256 hex of the exact bytes.
func contentSha(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// applySpecIntent writes a server-edited spec back to its workspace path
// (SCN-SY-071) unless the local file diverged from the last synced bytes or
// the server flagged a both-sides conflict — then it refuses, clobbering
// nothing (SCN-SY-072). A successful write resets the owning epic's
// specification-status marker to SPEC-DRAFT so the spec gate re-opens
// (SCN-SY-073). Returns "applied" or "conflict".
func applySpecIntent(root, externalID, content, syncedSha string, serverConflict bool) (string, error) {
	if serverConflict {
		return "conflict", nil
	}

	path := filepath.Join(root, filepath.FromSlash(externalID))
	local, err := os.ReadFile(path)
	if err == nil && contentSha(string(local)) != syncedSha && string(local) != content {
		// local edits the server never saw — a human resolves, we refuse
		return "conflict", nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}

	resetSpecMarker(root, externalID)
	return "applied", nil
}

// resetSpecMarker flips the epic's `## Specification status` marker line to
// SPEC-DRAFT (an edited spec is an unapproved spec). Best-effort: a missing
// or markerless EPIC.md (legacy) is left alone.
func resetSpecMarker(root, externalID string) {
	specDir := filepath.Dir(filepath.FromSlash(externalID)) // .../specs
	epicPath := filepath.Join(root, filepath.Dir(specDir), "EPIC.md")
	raw, err := os.ReadFile(epicPath)
	if err != nil {
		return
	}
	lines := strings.Split(string(raw), "\n")
	inSection := false
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = trimmed == "## Specification status"
			continue
		}
		if inSection && (strings.HasPrefix(trimmed, "SPEC-APPROVED") || strings.HasPrefix(trimmed, "SPEC-READY")) {
			lines[i] = "SPEC-DRAFT — server-edited spec pulled " + time.Now().UTC().Format("2006-01-02") + "; re-approval required."
			changed = true
			break
		}
	}
	if changed {
		_ = os.WriteFile(epicPath, []byte(strings.Join(lines, "\n")), 0o644)
	}
}

func appendFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}

// ---------------------------------------------------------------- evidence

var (
	evidenceKind   string
	evidenceLog    string
	evidenceTotals string
	evidencePass   string
	evidenceFail   string
	evidenceSkip   string
)

var factoryEvidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Post a test/CI run as evidence (sha-pinned; feeds Done-decays)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		targetType := func(id string) string {
			switch {
			case strings.Contains(id, "#AC"):
				return "criterion"
			case strings.HasPrefix(id, "EPIC-"):
				return "epic"
			default:
				return "requirement"
			}
		}

		results := []map[string]any{}
		for flagValue, result := range map[string]string{evidencePass: "pass", evidenceFail: "fail", evidenceSkip: "skip"} {
			for _, id := range strings.Split(flagValue, ",") {
				if id = strings.TrimSpace(id); id != "" {
					results = append(results, map[string]any{"target_external_id": id, "target_type": targetType(id), "result": result})
				}
			}
		}
		if len(results) == 0 {
			return fmt.Errorf("no targets — give at least --pass or --fail")
		}

		totals := map[string]any{}
		for _, pair := range strings.Split(evidenceTotals, ",") {
			if k, v, found := strings.Cut(pair, "="); found {
				if n, err := strconv.Atoi(v); err == nil {
					totals[k] = n
				}
			}
		}

		sha := gitOut(env.Root, "rev-parse", "--short", "HEAD")
		status, body, err := env.call("POST", "/api/v1/sync/evidence", map[string]any{
			"system_id":   env.SystemID,
			"external_id": "RUN-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + "-" + sha,
			"kind":        evidenceKind,
			"sha":         sha,
			"branch":      gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
			"ran_at":      time.Now().UTC().Format(time.RFC3339),
			"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"},
			"totals":      totals,
			"log_ref":     evidenceLog,
			"results":     results,
		})
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}
		run, _ := dataOf(body)["run"].(map[string]any)
		printSuccess("evidence %s: %s (%d targets, sha %s)", str(dataOf(body), "result"), str(run, "external_id"), len(results), sha)
		return nil
	},
}

// ---------------------------------------------------------------- drift

var driftReport bool

var factoryDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Compare each target's evidence sha to the working tree (Done decays)",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/evidence/latest?system_id=%d", env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("server %d: %v", status, body["error"])
		}

		tracePaths, err := env.workspaceTracePaths()
		if err != nil {
			return err
		}

		head := gitOut(env.Root, "rev-parse", "--short", "HEAD")
		drifted := 0
		reportFailures := 0

		targets, _ := dataOf(body)["targets"].([]any)
		for _, t := range targets {
			m, _ := t.(map[string]any)
			if str(m, "result") != "pass" || str(m, "sha") == "" || str(m, "sha") == "unknown" {
				continue
			}
			baseID := strings.SplitN(str(m, "target_external_id"), "#AC", 2)[0]
			paths := tracePaths[baseID]
			if len(paths) == 0 {
				continue
			}

			// evidence sha -> working tree, plus untracked: uncommitted AND
			// brand-new files are drift too
			changedSet := map[string]bool{}
			for _, line := range strings.Split(gitOut(env.Root, "diff", "--name-only", str(m, "sha")), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}
			for _, line := range strings.Split(gitOut(env.Root, "ls-files", "--others", "--exclude-standard"), "\n") {
				if line != "" {
					changedSet[line] = true
				}
			}

			matched := []string{}
			for changed := range changedSet {
				for _, pStr := range paths {
					if pStr != "" && (changed == pStr || strings.HasSuffix(changed, "/"+pStr) || strings.Contains(changed, pStr)) {
						matched = append(matched, changed)
						break
					}
				}
			}
			if len(matched) == 0 {
				continue
			}
			drifted++
			sort.Strings(matched)
			fmt.Printf("\n%s  evidence at %s — %d traced file(s) changed since:\n", str(m, "target_external_id"), str(m, "sha"), len(matched))
			for i, f := range matched {
				if i >= 5 {
					break
				}
				fmt.Printf("  ~ %s\n", f)
			}

			if driftReport {
				if len(matched) > 20 {
					matched = matched[:20]
				}
				if reportErr := reportDriftTarget(env, str(m, "target_external_id"), matched, head, str(m, "sha")); reportErr != nil {
					printError("  -> drift report failed: %v\n", reportErr)
					reportFailures++
				} else {
					printInfo("  -> drift_detected reported (state stale until re-verified)")
				}
			}
		}

		if reportFailures > 0 {
			return fmt.Errorf("%d drift report(s) failed — server state unchanged for those targets", reportFailures)
		}

		if drifted == 0 {
			printSuccess("no drift — evidence still covers what is on disk")
		} else if !driftReport {
			fmt.Printf("\n%d drifted — record with: modernpath factory drift --report\n", drifted)
		}
		return nil
	},
}

// ---------------------------------------------------------------- watch

var (
	watchInterval int
	watchCycles   int
)

var factoryWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "The daemon: sync + heartbeat + pull --apply on a cadence; SIGINT closes the session",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}

		closeSession := func() {
			_, _, _ = env.call("POST", "/api/v1/sync/session/close", map[string]any{
				"system_id":           env.SystemID,
				"workspace_ref":       filepath.Base(env.Root),
				"machine_fingerprint": machineFingerprint(env.Root),
			})
		}

		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigs
			fmt.Println("\nclosing session…")
			closeSession()
			os.Exit(0)
		}()

		for cycle := 1; ; cycle++ {
			printInfo("=== watch cycle %d (%s) ===", cycle, time.Now().UTC().Format(time.RFC3339))
			if err := factorySyncRun(env, false); err != nil {
				printWarning("sync failed: %v — retrying next cycle", err)
			}
			if err := factoryPullRun(env, true); err != nil {
				printWarning("pull failed: %v — retrying next cycle", err)
			}
			if watchCycles > 0 && cycle >= watchCycles {
				break
			}
			time.Sleep(time.Duration(watchInterval) * time.Second)
		}
		closeSession()
		printSuccess("watch ended — session closed")
		return nil
	},
}

// ---------------------------------------------------------------- wiring

func init() {
	factoryConnectCmd.Flags().IntVar(&factoryConnectSystem, "system", 0, "System id to bind this workspace to")

	factorySyncCmd.Flags().BoolVar(&factorySyncDryRun, "dry-run", false, "print the ops without sending")
	factorySyncCmd.Flags().BoolVar(&factorySyncJSON, "json", false, "with --dry-run: emit the full op batch as JSON")
	factorySyncCmd.Flags().BoolVar(&factorySyncNoDocs, "no-docs", false, "skip workspace-document ops (upsert_document); sync process state only")

	factoryGatesCmd.Flags().StringVar(&gatesKind, "kind", "", "show only this kind (approval_request, decision, question, roadblock, …)")

	factoryAnswerCmd.Flags().StringVar(&answerText, "text", "", "the answer, recorded verbatim as the USER: decision")
	factoryAnswerCmd.Flags().StringVar(&answerOptions, "options", "", "chosen option keys, comma-separated")
	factoryAnswerCmd.Flags().StringVar(&answerSource, "source", "", "override the USER:<date> source tag")

	factoryPullCmd.Flags().BoolVar(&pullApply, "apply", false, "record the answers in the workspace and ack the echo")

	factoryEvidenceCmd.Flags().StringVar(&evidenceKind, "kind", "local_test", "run kind: ci|local_test|browser_verification|manual")
	factoryEvidenceCmd.Flags().StringVar(&evidenceLog, "log", "", "log reference (command line, CI url)")
	factoryEvidenceCmd.Flags().StringVar(&evidenceTotals, "totals", "", "totals, e.g. passed=478,failed=0")
	factoryEvidenceCmd.Flags().StringVar(&evidencePass, "pass", "", "passing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceFail, "fail", "", "failing target ids, comma-separated")
	factoryEvidenceCmd.Flags().StringVar(&evidenceSkip, "skip", "", "skipped target ids, comma-separated")

	// Q-ARCH-016 (USER:2026-08-18): --report was advertised in drift's own
	// output but never registered; the drift-report POST was unreachable.
	factoryDriftCmd.Flags().BoolVar(&driftReport, "report", false,
		"POST the drift facts to the server (marks affected evidence stale until re-verified)")

	factoryWatchCmd.Flags().IntVar(&watchInterval, "interval", 120, "seconds between cycles")
	factoryWatchCmd.Flags().IntVar(&watchCycles, "cycles", 0, "stop after N cycles (0 = forever)")

	factoryReleaseCmd.AddCommand(factoryReleaseUseCmd, factoryReleaseShowCmd, factoryReleaseClearCmd)

	factorySyncCmd.Flags().BoolVar(&factorySyncIfQuiescent, "if-quiescent", false,
		"hook mode: sync only when the workspace is coherent; log outcomes, never error (EPIC-SYNC-009)")
	factorySyncCmd.Flags().StringVar(&factorySyncTrigger, "trigger", "manual", "trigger label for the hook log")
	factorySyncCmd.Flags().DurationVar(&factorySyncMinInterval, "min-interval", 60*time.Second,
		"debounce: skip when the last successful sync is younger than this")

	factoryCmd.AddCommand(factoryConnectCmd, factoryStatusCmd, factorySyncCmd, factoryGatesCmd,
		factoryAnswerCmd, factoryPullCmd, factoryEvidenceCmd, factoryDriftCmd, factoryWatchCmd,
		factoryManifestCmd, factoryReleaseCmd, factoryImageCmd)
	factoryImageCmd.Flags().StringVar(&imagePurpose, "purpose", "", "context tag stored with the image (e.g. decision-brief)")
	rootCmd.AddCommand(factoryCmd)
}

// unknownOpType extracts the op kind from the server's 422 for an op type it
// does not implement, and returns "" for every other failure — an ordinary
// validation error must NOT be worked around by dropping ops.
func unknownOpType(body map[string]any) string {
	errObj, _ := body["error"].(map[string]any)
	details, _ := errObj["details"].(map[string]any)
	msgs, _ := details["type"].([]any)
	for _, m := range msgs {
		s, _ := m.(string)
		const prefix = "unknown op type "
		if strings.HasPrefix(s, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(s, prefix))
		}
	}
	return ""
}

// dropOpsOfType removes one op kind, preserving the order of everything else.
func dropOpsOfType(ops []map[string]any, kind string) ([]map[string]any, int) {
	kept := make([]map[string]any, 0, len(ops))
	dropped := 0
	for _, op := range ops {
		if str(op, "type") == kind {
			dropped++
			continue
		}
		kept = append(kept, op)
	}
	return kept, dropped
}

// noDocsFilter applies the `--no-docs` sync option. When on it removes the
// workspace-document ops — type `upsert_document` — and returns the kept ops
// plus the count removed; when off it returns the ops unchanged with a zero
// count. The process-state ops (requirements, epics, gates, evidence, sessions)
// never reference a document op, so dropping documents leaves a coherent batch.
// Extracted so the exact op type `--no-docs` drops is asserted in one place:
// core embeds an explanatory document synchronously and 422s the whole batch
// when its embedding provider is unavailable (core/sync.ex), and this is the
// escape hatch that lands the state anyway, to be backfilled by a later
// docs-included sync once the provider works.
func noDocsFilter(ops []map[string]any, noDocs bool) ([]map[string]any, int) {
	if !noDocs {
		return ops, 0
	}
	return dropOpsOfType(ops, "upsert_document")
}

// syncHadConflicts reports whether a batch contained refusals — rows the server
// declined to overwrite because a human edited them there.
func syncHadConflicts(counts map[string]int) bool {
	return counts["conflict"] > 0
}

// reportDriftTarget posts one target's drift facts. "Reported" must mean the
// server recorded it: a discarded POST result printed success over network
// failures and 4xx/5xx (external review, RUN:2026-08-19); the second round
// asked for the branches to be pinned by tests (factory_drift_report_test.go).
func reportDriftTarget(env *factoryEnv, targetID string, changed []string, head, since string) error {
	status, _, err := env.call("POST", "/api/v1/sync/evidence/drift", map[string]any{
		"system_id": env.SystemID, "target_external_id": targetID,
		"changed": changed, "head": head, "since": since,
	})
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("rejected: HTTP %d", status)
	}
	return nil
}
