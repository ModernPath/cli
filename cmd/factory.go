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
	"net/url"
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
	// REQ-CROSS-390: a write this build cannot mean is refused before it leaves.
	if err := e.checkWrite(method, apiPath, payload); err != nil {
		return 0, nil, err
	}
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

	// REQ-CROSS-372: keep an undecodable body (a proxy or gateway error page)
	// as the refusal text instead of dropping it — every `server %d` formatter
	// then prints what the server sent, never <nil>.
	raw, _ := io.ReadAll(resp.Body)
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		if text := strings.TrimSpace(string(raw)); text != "" && resp.StatusCode >= 300 {
			decoded = map[string]any{"error": map[string]any{"reason": text}}
		} else if resp.StatusCode >= 300 {
			// REQ-CROSS-380: a bodyless refusal still has a status and a
			// reference; never leave the formatters a nil to print.
			decoded = map[string]any{"error": map[string]any{"reason": "no body"}}
		}
	}
	// REQ-CROSS-380: the reference rides the body so every formatter — all of
	// them route through serverRefusal — can cite it.
	if decoded != nil && resp.StatusCode >= 300 {
		if ref := resp.Header.Get("x-request-id"); ref != "" {
			if _, has := decoded["request_id"]; !has {
				decoded["request_id"] = ref
			}
		}
	}

	// REQ-CROSS-348: the store names its revision on every response; keep the
	// last one seen so a snapshot header can record the store it came from.
	if rev := resp.Header.Get("x-modernpath-store-revision"); rev != "" {
		e.storeRevision = rev
	}
	// REQ-CROSS-390: the served contract version, warned about once when newer.
	e.noteServedContract(resp.Header.Get("x-modernpath-contract"))

	// Reported here rather than at each call site: all eight `server %d`
	// formatters check err first, so one point covers every factory command.
	if resp.StatusCode == http.StatusUnauthorized {
		return resp.StatusCode, decoded, e.credentialRejected()
	}
	return resp.StatusCode, decoded, nil
}

// reportSyncOutcome — REQ-CROSS-386 (EPIC-CLI-018): render a batch outcome.
// 200: the ok/conflict summary. 207: the same summary plus one line per
// failed, skipped or deferred op with the server's reason; a failed or skipped
// op exits non-zero naming the retry, a deferred document alone is a warning
// (the row is kept for the embedding backfill and a later sync retries it).
func reportSyncOutcome(status int, body map[string]any) error {
	results, _ := dataOf(body)["results"].([]any)
	counts := map[string]int{}
	var failed, skipped, deferred []map[string]any
	for _, r := range results {
		m, _ := r.(map[string]any)
		counts[str(m, "result")]++
		switch str(m, "result") {
		case "failed":
			failed = append(failed, m)
		case "skipped":
			skipped = append(skipped, m)
		case "deferred":
			deferred = append(deferred, m)
		}
	}
	parts := make([]string, 0, len(counts))
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(parts)
	summary := strings.Join(parts, ", ")
	switch {
	// REQ-CROSS-136: a conflict is the server REFUSING this change because the
	// row was edited on the platform. Printing it under a green "ok:" invites
	// the reader to skim past a refusal, so say it plainly instead.
	case syncHadConflicts(counts):
		printWarning("synced with refusals: %s", summary)
		fmt.Printf("  %d row(s) were edited on the platform and kept — this workspace's version was not applied.\n", counts["conflict"])
		fmt.Printf("  Reconcile by hand: the server's copy wins until the workspace matches it.\n")
	case status == 207 || len(failed)+len(skipped)+len(deferred) > 0:
		printWarning("synced partially (server %d): %s", status, summary)
	default:
		printSuccess("ok: %s", summary)
	}
	for _, m := range failed {
		fmt.Printf("  failed   op %v %s (%s): %s\n", m["op_index"], str(m, "external_id"), str(m, "type"), str(m, "reason"))
	}
	for _, m := range skipped {
		fmt.Printf("  skipped  op %v %s: %s\n", m["op_index"], str(m, "external_id"), str(m, "reason"))
	}
	for _, m := range deferred {
		fmt.Printf("  deferred op %v %s: %s\n", m["op_index"], str(m, "external_id"), str(m, "reason"))
	}
	if len(failed)+len(skipped) > 0 {
		return fmt.Errorf("%d op(s) failed and %d skipped — fix the cause and run `factory sync` again; the other ops landed and nothing is lost locally", len(failed), len(skipped))
	}
	if len(deferred) > 0 {
		fmt.Printf("  %d document(s) deferred — run `factory sync` again once the embedding provider recovers; the backfill re-embeds what landed.\n", len(deferred))
	}
	return nil
}

// syncChunksOutcome is what a chunked sync run produced: every chunk's rows
// (the retried chunk's included), whether any chunk answered 207, how many
// ops the server accepted, and the last status/body for a refusal.
type syncChunksOutcome struct {
	results []any
	partial bool
	landed  int
	status  int
	body    map[string]any
}

// postSyncChunks sends the chunks in order. Chunks go in order, because later
// ops reference earlier ones; each is its own transaction server-side and the
// shadow makes a replay a no-op, so a failure part-way leaves the earlier
// chunks landed and re-running finishes the job.
//
// REQ-CROSS-089: a server that halts on an op type it does not recognise
// would otherwise make a newer CLI sync NOTHING; the kind is dropped from this
// and every later chunk, the chunk is re-posted, and the run continues.
// REQ-CROSS-386 (PR #458 review): the retry happens inside the loop so its
// rows — a 207's failed and skipped ops among them — reach the report
// whichever chunk it was, and the chunks after it are still sent.
func postSyncChunks(env *factoryEnv, chunks [][]map[string]any, newBatch func([]map[string]any) map[string]any) (syncChunksOutcome, error) {
	var out syncChunksOutcome
	dropped := map[string]bool{}
	for index := 0; index < len(chunks); index++ {
		chunk := chunks[index]
		if len(dropped) > 0 {
			chunk = dropKinds(chunk, dropped)
		}
		status, body, err := env.postSyncBatch(newBatch(chunk))
		if err != nil {
			// Say which chunk, and that the earlier ones are already in the
			// store — otherwise a re-run looks like it might double-apply.
			if index > 0 {
				printWarning("chunk %d/%d failed; %d op(s) already landed — re-running syncs the rest",
					index+1, len(chunks), out.landed)
			}
			return out, err
		}
		if status == 422 {
			if kind := unknownOpType(body); kind != "" && !dropped[kind] {
				dropped[kind] = true
				kept, n := dropOpsOfType(chunk, kind)
				fmt.Printf("⚠ this server does not understand %s yet — syncing the other ops and leaving %d behind in this chunk (and any later one).\n", kind, n)
				fmt.Printf("  They will land once the server is upgraded; nothing is lost locally.\n")
				status, body, err = env.postSyncBatch(newBatch(kept))
				if err != nil {
					return out, err
				}
				chunk = kept
			}
		}
		out.status, out.body = status, body
		// A 207 is a landed chunk with reported failures, not a refusal.
		if status != 200 && status != 207 {
			if status != 422 && index > 0 {
				printWarning("chunk %d/%d failed (server %d); %d op(s) already landed — re-running syncs the rest",
					index+1, len(chunks), status, out.landed)
			}
			return out, nil
		}
		results, _ := dataOf(body)["results"].([]any)
		out.results = append(out.results, results...)
		if status == 207 {
			out.partial = true
		}
		for _, r := range results {
			m, _ := r.(map[string]any)
			switch str(m, "result") {
			case "failed", "skipped":
			default:
				out.landed++
			}
		}
	}
	return out, nil
}

// dropKinds removes every op of the dropped kinds from a chunk.
func dropKinds(chunk []map[string]any, dropped map[string]bool) []map[string]any {
	kept := make([]map[string]any, 0, len(chunk))
	for _, op := range chunk {
		if !dropped[str(op, "type")] {
			kept = append(kept, op)
		}
	}
	return kept
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

// chunkOps splits ops into consecutive slices of at most size, preserving
// order — later ops reference earlier ones, so a chunk boundary must never
// reorder. size is expected from syncChunkSize() (always ≥ 1); a non-positive
// size is treated defensively as "one chunk".
func chunkOps(ops []map[string]any, size int) [][]map[string]any {
	if size < 1 {
		return [][]map[string]any{ops}
	}
	chunks := make([][]map[string]any, 0, (len(ops)+size-1)/size)
	for start := 0; start < len(ops); start += size {
		end := start + size
		if end > len(ops) {
			end = len(ops)
		}
		chunks = append(chunks, ops[start:end])
	}
	return chunks
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
	Short: "Show the local sync stamp, server active release, and pending op count",
	Long: `Show the workspace binding, the local sync-release stamp, the server active
release, the pending op count, the
pieces you hold, and one server line: the store revision the bound server is
serving and the sync contract version it advertises (or why it could not be
read). The server line is a single read bounded at 2 s and writes nothing —
after a merge it tells you whether production serves it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// REQ-CROSS-391: print the binding through the renderer `status` uses
		// before refusing, so an unbound workspace reads the same in both.
		if cfgDir, err := config.FindConfigDir(); err == nil && cfgDir != "" {
			if cfg, err := config.ReadConfig(); err == nil {
				auth, _ := config.ReadAuth()
				for _, line := range bindingLines(cfg, auth) {
					fmt.Println(line)
				}
			}
		}
		env, err := factoryBindingLoad()
		if err != nil {
			return err
		}
		release := env.CurrentRelease
		if release == "" {
			release = "(none — sync runs unscoped; set one with 'factory release use <slug>')"
		}
		fmt.Printf("workspace: %s\nsync stamp: %s\n", env.Root, release)
		fmt.Printf("server:    %s\n", serverLine(env))

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
		printHeldPieces(env)
		return nil
	},
}

// serverLineTimeout bounds the server line's one read: an offline status must
// answer "not reachable" in seconds, not wait out the 120 s call default.
const serverLineTimeout = 2 * time.Second

// serverLine — REQ-CROSS-417 (EPIC-CLI-021): what the bound server is serving,
// for `factory status` and `status`: the store revision from the
// x-modernpath-store-revision header (REQ-CROSS-348, the deploy revision of
// the serving core) and the contract version from GET /api/v1/sync/contract
// (REQ-CROSS-390). One read, no write, so after a merge a session can compare
// the served revision with the merge commit (BACKLOG-TOOL-1). Best-effort like
// printHeldPieces: unsigned, unreachable and unserved each say so.
func serverLine(env *factoryEnv) string {
	if strings.TrimSpace(env.token) == "" {
		auth, err := config.ReadAuth()
		if err != nil || strings.TrimSpace(auth.Token) == "" {
			return "not signed in — `modernpath auth login`, then status names the server revision"
		}
		env.token = auth.Token
	}
	saved := env.callTimeout
	env.callTimeout = serverLineTimeout
	defer func() { env.callTimeout = saved }()

	status, body, err := env.call("GET", "/api/v1/sync/contract", nil)
	if err != nil {
		return fmt.Sprintf("not reachable (%v)", err)
	}
	revision := "store revision not served"
	if env.storeRevision != "" {
		revision = "store " + env.storeRevision
	}
	served := "contract not advertised"
	if v, ok := dataOf(body)["version"].(float64); ok && status == http.StatusOK {
		served = fmt.Sprintf("contract %d", int(v))
	} else if env.contractVersion != "" {
		served = "contract " + env.contractVersion
	}
	return revision + " · " + served
}

// printHeldPieces — REQ-CROSS-379 (EPIC-CLI-018): status lists every piece the
// caller holds. Best-effort by design: status is a binding command that must
// run without a readable token (TestFactoryStatusDoesNotLoadAuthentication),
// so a missing or malformed token says so instead of failing the command.
func printHeldPieces(env *factoryEnv) {
	auth, err := config.ReadAuth()
	if err != nil || strings.TrimSpace(auth.Token) == "" {
		fmt.Println("held:      not signed in — `modernpath auth login`, then status lists the pieces you hold")
		return
	}
	env.token = auth.Token
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID), nil)
	switch {
	case err != nil:
		fmt.Printf("held:      unavailable (%v)\n", err)
	case status != 200:
		if pieces := stringSlice(body["pieces"]); len(pieces) > 0 {
			fmt.Printf("held:      %d current pieces — %s (name one with --piece)\n", len(pieces), strings.Join(pieces, ", "))
		} else {
			fmt.Printf("held:      unavailable (server %d)\n", status)
		}
	default:
		current, _ := dataOf(body)["current"].(map[string]any)
		if current == nil {
			fmt.Println("held:      no current piece")
		} else {
			fmt.Printf("held:      %s (phase %s)\n", str(current, "scope_external_id"), str(current, "phase"))
		}
		printActiveReleaseSource(env, dataOf(body))
	}
}

// printActiveReleaseSource — REQ-CROSS-407: status names the store's active
// release with the USER: source of its selection gate on this system, or the
// active-without-gate state and its remedy. Best-effort like the rest of
// status: a failed gates read says so and never fails the command.
func printActiveReleaseSource(env *factoryEnv, selection map[string]any) {
	releases, _ := selection["active_release"].([]any)
	if len(releases) != 1 {
		return
	}
	rm, _ := releases[0].(map[string]any)
	slug := str(rm, "slug")
	gates, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
	if err != nil {
		fmt.Printf("active:    active release %s — selection gate unavailable (%v)\n", slug, err)
		return
	}
	if tag, gate, ok := releaseSelectionGateSource(gates, slug); ok {
		fmt.Printf("active:    active release %s — %s (gate %s)\n", slug, tag, gate)
		return
	}
	fmt.Printf("active:    active release %s — %s\n", slug, missingReleaseGateLine(slug))
}

// ---------------------------------------------------------------- release

// factory release use|show|clear — the workspace-level release selector
// (REQ-CROSS-017). Writes only local config; the server materializes the
// release (find-or-create by slug) on the first scoped sync. This is the
// local sync stamp; the system-wide active release lives in the store and is
// set with `factory release activate` (REQ-CROSS-339) — the two never merge.
var factoryReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Select the current release that factory sync stamps and scopes to",
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
		printSuccess("current release: %s (local sync stamp in .modernpath/config.json; the system-wide active release lives in the store — activate one with 'factory release activate <slug>')", slug)
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

// factory release activate <slug> — REQ-CROSS-339 (EPIC-CLI-010): the
// system-wide activation verb. Unlike `use`, which only stamps local config
// the bulk sync consumes, `activate` calls the guarded server operation that
// sets the release active. The server composes an attributable source when the
// caller does not supply one. The two never merge.
var (
	releaseActivateSource       string
	releaseActivatePin          string
	releaseActivateReason       string
	releaseActivateCloseCurrent bool
	releaseActivatePinStdin     bool
	activationPinFromStdin      = func() (string, error) { return readPinStdin(os.Stdin) }
	activationPinSetup          = func() (string, error) { return readPin(false) }
)

var factoryReleaseActivateCmd = &cobra.Command{
	Use:   "activate <slug>",
	Short: "Activate a delivery release on the bound system (distinct from the local `use` stamp)",
	Long: `Activate or reactivate a delivery release on the bound system.

The activation needs the release PIN of the signed-in person (set in Mission
Control; pass it with --pin or --pin-stdin). It records the release-selection gate
GATE-RELEASE-<slug> on the system it is run from. The server composes an
attributable source unless --source is supplied. Re-running
the activation on a system whose release is active but carries no such gate
records one without changing the release; a gate of that purpose and scope
that is open or answered otherwise is superseded by the next
GATE-RELEASE-<slug>-<n>, and the reads take the newest approved one.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		slug := strings.TrimSpace(args[0])
		if slug == "" {
			return fmt.Errorf("usage: modernpath factory release activate <slug>")
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		pin, err := activationPin(env, releaseActivatePin, releaseActivatePinStdin)
		if err != nil {
			return err
		}
		return activateRelease(env, slug, releaseActivateSource, pin, releaseActivateCloseCurrent, releaseActivateReason)
	},
}

// activateRelease posts the server-owned release activation operation. An
// optional source is preserved; without it the server composes the source.
func activateRelease(env *factoryEnv, slug, source, pin string, closeCurrent bool, reason string) error {
	body := map[string]any{
		"action":        "release_activate",
		"slug":          slug,
		"close_current": closeCurrent,
	}
	if source != "" {
		body["source"] = source
	}
	if reason = strings.TrimSpace(reason); reason != "" {
		body["reason"] = reason
	}
	if pin != "" {
		body["pin"] = pin
	}
	data, err := authorPost(env, body)
	if err != nil {
		return err
	}
	if row, ok := data["release_activation"].(map[string]any); ok {
		printSuccess("release %s is now %s on this system (source: %s; closed: %s)", str(row, "slug"), str(row, "status"), str(row, "source"), closedReleaseNames(row))
	} else {
		printSuccess("release %s activated on this system", slug)
	}
	return nil
}

func activationPin(env *factoryEnv, supplied string, fromStdin bool) (string, error) {
	status, response, err := env.call("GET", "/api/compliance/pin", nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("PIN status failed (HTTP %d): %s", status, str(response, "error"))
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("PIN status response is missing data.has_pin")
	}
	hasPin, ok := data["has_pin"].(bool)
	if !ok {
		return "", fmt.Errorf("PIN status response is missing data.has_pin")
	}
	if supplied != "" {
		if !hasPin {
			return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
		}
		return supplied, nil
	}
	if fromStdin {
		if !hasPin {
			return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
		}
		return activationPinFromStdin()
	}
	if hasPin {
		if !stdinIsTerminal() {
			return "", fmt.Errorf("no terminal for a PIN prompt — pipe the PIN and pass --pin-stdin")
		}
		return promptHiddenPin("Compliance PIN: ")
	}
	if !stdinIsTerminal() {
		return "", fmt.Errorf("no compliance PIN is set — run 'modernpath factory pin set --pin-stdin' before non-interactive activation")
	}
	pin, err := activationPinSetup()
	if err != nil {
		return "", err
	}
	if err := setCompliancePin(env, pin); err != nil {
		return "", err
	}
	return pin, nil
}

func closedReleaseNames(row map[string]any) string {
	closed, _ := row["closed_releases"].([]any)
	names := make([]string, 0, len(closed))
	for _, item := range closed {
		if release, ok := item.(map[string]any); ok {
			names = append(names, str(release, "name"))
		}
	}
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
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

		// Store-backed (REQ-CROSS-329): a file-derived sync is meaningless once
		// the ledgers are retired — the store is authoritative and the bulk
		// channel is refused. Say where process state is written instead, ahead
		// of factoryEnvLoad so the notice needs no credentials. A dry run still
		// falls through to print its (now empty) batch.
		if !factorySyncDryRun {
			if _, active := storeBackedFromCwd(); active {
				printInfo("this workspace is store-backed (process/store-backed.md) — the file ledgers are retired; there is nothing to sync")
				printInfo("write process state with 'modernpath author' / 'modernpath working-set push'; read it with 'modernpath working-set pull' / 'modernpath factory status'")
				return nil
			}
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
	kept, dropped := noDocsFilter(ops, factorySyncNoDocs)
	ops = kept
	// --json owns stdout: a prose banner ahead of the batch makes it unparseable
	// by the very tools the flag exists for — so the --no-docs notice waits until
	// after the --json branch has returned (emitted on the non-JSON path below).
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
	if dropped > 0 {
		printInfo("--no-docs: skipping %d document op(s); syncing process state only", dropped)
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

	chunks := chunkOps(ops, chunkSize)
	if len(chunks) == 0 {
		chunks = append(chunks, nil)
	}
	if len(chunks) > 1 {
		printInfo("sending in %d chunks of up to %d ops", len(chunks), chunkSize)
	}

	outcome, err := postSyncChunks(env, chunks, newBatch)
	if err != nil {
		return err
	}
	if outcome.status != 200 && outcome.status != 207 {
		return serverRefusal("", outcome.status, outcome.body)
	}
	finalStatus := 200
	if outcome.partial {
		finalStatus = 207
	}
	// One report renders the outcome (REQ-CROSS-386); a failed or skipped op
	// ends the run non-zero before the success stamp below.
	if err := reportSyncOutcome(finalStatus, map[string]any{"data": map[string]any{"results": outcome.results}}); err != nil {
		return err
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
	Use:   "gates [external_id]",
	Short: "The open decision queue; a gate by id, or gate history with --state",
	Long: `Without arguments, the open decision queue (questions, decisions, approvals).

With an external_id, show that one gate — its state and, when it carries an
answer, the answer, chosen options, USER: source, answerer and applied state —
so "did my approval land?" is answerable without reading the event stream. An
applied answer is stored closed; read it by id or under --state all.

With --state, list gate history: open, answered, dismissed, superseded, or all.
--json prints the server's gate envelope on stdout and nothing else.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// factoryEnvLoad stays first: a local refusal (e.g. a token without the
		// project audience) must send nothing, including the reachability check
		// (TestFactoryGatesRefusesTokenWithoutProjectAudienceBeforeSending).
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		id := ""
		if len(args) == 1 {
			id = args[0]
		}
		return factoryGatesRun(env, gatesState, gatesKind, gatesJSON, id, os.Stdout, os.Stderr)
	},
}

// validGateState reports whether s is one of the server filter's five values.
// `closed` is deliberately excluded (REQ-CROSS-109 D2): an applied answer is
// stored closed and is reached through `all` or the by-id read, not this filter.
func validGateState(s string) bool {
	switch s {
	case "open", "answered", "dismissed", "superseded", "all":
		return true
	}
	return false
}

// factoryGatesRun is the testable seam behind `factory gates`. out carries the
// rendering (or, under jsonOut, only the JSON envelope); errOut carries warnings,
// so JSON on stdout stays parseable — the requirements-corpus precedent
// (REQ-CROSS-324/121). --state is validated here, before any request, so a typo
// never becomes a call; the server stays the authority and its 422 is surfaced.
func factoryGatesRun(env *factoryEnv, state, kind string, jsonOut bool, id string, out, errOut io.Writer) error {
	if id != "" && (state != "" || kind != "") {
		return fmt.Errorf("--state and --kind apply to the listing, not to a gate by id (--json combines with either form)")
	}
	if state != "" && !validGateState(state) {
		return fmt.Errorf("unknown --state %q — use one of open, answered, dismissed, superseded, all", state)
	}
	if id != "" {
		return factoryGateShow(env, id, jsonOut, out)
	}
	return factoryGateList(env, state, kind, jsonOut, out, errOut)
}

// factoryGateShow reads one gate by id: GET /sync/gates/<id>?system_id=<bound>.
// The id is path-escaped. Absence — the server's own "no such gate" message —
// is a non-zero exit naming the id; any other 404 (a server without the route
// answers Phoenix's {"errors":{"detail":"Not Found"}}) is a server error, never
// read as absence.
func factoryGateShow(env *factoryEnv, id string, jsonOut bool, out io.Writer) error {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return gateShowError(status, body, id)
	}
	gate, _ := dataOf(body)["gate"].(map[string]any)
	if gate == nil {
		// Symmetry with the list path's nil→[] (a malformed/renamed 200 envelope):
		// emit {"gate": {}} rather than {"gate": null} so a consumer's parse holds.
		gate = map[string]any{}
	}
	if jsonOut {
		return emitJSONEnvelope(out, "gate", gate)
	}
	renderGate(out, gate)
	return nil
}

// gateShowError concludes absence ONLY from the server's nested "no such gate"
// message; every other non-200 is reported verbatim as `server <status>: …`.
func gateShowError(status int, body map[string]any, id string) error {
	if status == 404 {
		if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
			return fmt.Errorf("gate %s does not exist", id)
		}
	}
	return fmt.Errorf("server %d: %s", status, gateErrText(body))
}

// gateErrText pulls a human message out of the error shapes the server and the
// gateway use: a nested {"error":{"message":…}}, a flat {"error":"…"}, or
// Phoenix's {"errors":{"detail":…}} for a route it does not have.
func gateErrText(body map[string]any) string {
	if em, ok := body["error"].(map[string]any); ok {
		if m := str(em, "message"); m != "" {
			return m
		}
	}
	if s, ok := body["error"].(string); ok && s != "" {
		return s
	}
	if em, ok := body["errors"].(map[string]any); ok {
		if d := str(em, "detail"); d != "" {
			return d
		}
	}
	return refusalText(body)
}

func factoryGateList(env *factoryEnv, state, kind string, jsonOut bool, out, errOut io.Writer) error {
	apiPath := fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID)
	if state != "" {
		apiPath += "&state=" + url.QueryEscape(state)
	}
	status, body, err := env.call("GET", apiPath, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("server %d: %s", status, gateErrText(body))
	}
	gates, _ := dataOf(body)["gates"].([]any)
	shown := filterGatesByKind(gates, kind)
	if jsonOut {
		return emitGatesJSON(out, shown)
	}
	// History (answered|dismissed|superseded|all) renders each gate's stored
	// state; the open queue (default, or explicit --state open) is unchanged.
	history := state != "" && state != "open"
	if len(shown) == 0 {
		switch {
		case history && state == "all":
			fmt.Fprint(out, "no gates\n")
		case history:
			fmt.Fprintf(out, "no %s gates\n", state)
		case kind != "" && len(gates) > 0:
			// A queue that holds gates of other kinds is not clear.
			fmt.Fprint(out, gateQueueFooter(0, gateKindBreakdown(gates), kind))
		default:
			fmt.Fprint(out, "✓ no open gates — the queue is clear\n")
		}
		return nil
	}
	if history {
		for _, g := range shown {
			m, _ := g.(map[string]any)
			renderGateHistory(out, m)
		}
		if state == "all" {
			fmt.Fprintf(out, "\n%d gate(s)\n", len(shown))
		} else {
			fmt.Fprintf(out, "\n%d %s gate(s)\n", len(shown), state)
		}
		return nil
	}
	for _, g := range shown {
		m, _ := g.(map[string]any)
		fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(m, "external_id"), str(m, "kind"), str(m, "title"))
		if rec := str(m, "recommendation"); rec != "" {
			fmt.Fprintf(out, "  recommends: %.120s\n", rec)
		}
		if options, ok := m["options"].([]any); ok {
			for _, o := range options {
				om, _ := o.(map[string]any)
				fmt.Fprintf(out, "  - %s: %.100s\n", str(om, "key"), str(om, "label"))
			}
		}
	}
	fmt.Fprint(out, gateQueueFooter(len(shown), gateKindBreakdown(gates), kind))
	return nil
}

// renderGate is the by-id detail: state and applied state, then the answer,
// chosen options, source and answerer when the gate carries them. A trace
// gate (REQ-CROSS-425) adds its verdict, evaluator, recording revision, pin
// and class, scope, purpose, transition and prerequisites; -v prints any
// gate's body after its fields.
func renderGate(out io.Writer, g map[string]any) {
	fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(g, "external_id"), str(g, "kind"), str(g, "title"))
	fmt.Fprintf(out, "  state: %s", str(g, "state"))
	if a := str(g, "applied_state"); a != "" {
		fmt.Fprintf(out, "   applied: %s", a)
	}
	fmt.Fprintln(out)
	if ans := str(g, "answer"); ans != "" {
		fmt.Fprintf(out, "  answer: %s\n", ans)
	}
	if keys := gateOptionKeys(g); keys != "" {
		fmt.Fprintf(out, "  chosen: %s\n", keys)
	}
	if src := str(g, "source_tag"); src != "" {
		fmt.Fprintf(out, "  source: %s\n", src)
	}
	if who := gateAnswerer(g); who != "" {
		if at := str(g, "answered_at"); at != "" {
			fmt.Fprintf(out, "  answered by %s at %s\n", who, at)
		} else {
			fmt.Fprintf(out, "  answered by %s\n", who)
		}
	}
	if isTraceGate(g) {
		fmt.Fprintf(out, "  verdict: %s\n", fieldOr(g, "verdict", "—"))
		if who := gateEvaluator(g); who != "" {
			if at := str(g, "evaluated_at"); at != "" {
				fmt.Fprintf(out, "  evaluated by %s at %s\n", who, at)
			} else {
				fmt.Fprintf(out, "  evaluated by %s\n", who)
			}
		}
		// application_revision is the recording HEAD, not a test run.
		fmt.Fprintf(out, "  recorded at: %s\n", fieldOr(g, "application_revision", "—"))
		fmt.Fprintf(out, "  pinned to: %s\n", gatePin(g))
		fmt.Fprintf(out, "  scope: %s\n", idListOr(g, "exact_scope", "—"))
		fmt.Fprintf(out, "  purpose: %s\n", fieldOr(g, "purpose", "—"))
		fmt.Fprintf(out, "  transition: %s\n", fieldOr(g, "transition", "—"))
		fmt.Fprintf(out, "  prerequisites: %s\n", idListOr(g, "prerequisite_gate_external_ids", "none"))
		if _, has := g["predecessor_external_id"]; has {
			fmt.Fprintf(out, "  predecessor: %s\n", servedOr(g, "predecessor_external_id", "—"))
		}
		if _, has := g["successor_external_id"]; has {
			fmt.Fprintf(out, "  successor: %s\n", servedOr(g, "successor_external_id", "—"))
		}
	}
	if verbose {
		if body := str(g, "body_md"); body != "" {
			fmt.Fprintf(out, "\n%s\n", strings.TrimRight(body, "\n"))
		}
	}
}

// isTraceGate: the store's gate_class, or a verdict on a gate that predates it.
func isTraceGate(g map[string]any) bool {
	return str(g, "gate_class") == "trace" || str(g, "verdict") != ""
}

// gateEvaluator mirrors gateAnswerer over the evaluator quartet.
func gateEvaluator(g map[string]any) string {
	if n := str(g, "evaluator_name"); n != "" {
		return n
	}
	kind := str(g, "evaluator_kind")
	if slug := str(g, "evaluator_agent_slug"); slug != "" {
		if kind != "" {
			return kind + " " + slug
		}
		return slug
	}
	if id := numericID(g["evaluator_user_id"]); id != "" {
		return "user #" + id
	}
	return kind
}

// gatePin names the fingerprint a trace is pinned to and what that
// fingerprint is: a packet aggregate for the cold-review, entry and
// completion purposes, the item's content hash for lower and upper.
func gatePin(g map[string]any) string {
	fp := str(g, "evaluated_scope_fingerprint")
	if fp == "" {
		return "—"
	}
	return fp + " (" + gatePinClass(str(g, "purpose")) + ")"
}

func gatePinClass(purpose string) string {
	switch purpose {
	case "cold_review", "entry", "completion":
		return "packet aggregate"
	case "lower", "upper":
		return "content hash"
	}
	return "fingerprint"
}

// renderGateHistory is one line-group in a --state listing: the gate, its stored
// state, and — when it carries an answer — its source and answerer.
func renderGateHistory(out io.Writer, g map[string]any) {
	fmt.Fprintf(out, "\n%s  [%s]  %s\n", str(g, "external_id"), str(g, "kind"), str(g, "title"))
	fmt.Fprintf(out, "  state: %s\n", str(g, "state"))
	if src := str(g, "source_tag"); src != "" {
		fmt.Fprintf(out, "  source: %s\n", src)
	}
	if who := gateAnswerer(g); who != "" {
		fmt.Fprintf(out, "  answered by %s\n", who)
	}
}

// gateAnswerer prefers the resolved name; otherwise the answerer kind with the
// agent slug, or the numeric user id the server serves when a user resolves to
// no membership name — never discarding that id down to a bare kind.
func gateAnswerer(g map[string]any) string {
	if n := str(g, "answerer_name"); n != "" {
		return n
	}
	kind := str(g, "answerer_kind")
	if slug := str(g, "answerer_agent_slug"); slug != "" {
		if kind != "" {
			return kind + " " + slug
		}
		return slug
	}
	if id := answererUserID(g); id != "" {
		return "user #" + id
	}
	return kind
}

// answererUserID formats the numeric answerer_user_id the server serves when a
// user (e.g. a superuser) resolves to no membership name. A JSON number decodes
// as float64 without UseNumber, which the string-only str() silently drops, so
// format it as a base-10 integer — no trailing ".0", no scientific notation for
// a large id — keeping the identity visible instead of collapsing to "human".
func answererUserID(g map[string]any) string {
	return numericID(g["answerer_user_id"])
}

// numericID formats a served numeric user id (REQ-CROSS-425 reuses it for the
// evaluator); an absent or non-numeric value is "".
func numericID(raw any) string {
	switch v := raw.(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case json.Number:
		return v.String()
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	}
	return ""
}

func gateOptionKeys(g map[string]any) string {
	keys, _ := g["chosen_option_keys"].([]any)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := k.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// emitGatesJSON writes {"gates":[…]} to out, an empty list as [] and never null,
// so a consumer's JSON.parse on stdout cannot throw.
func emitGatesJSON(out io.Writer, gates []any) error {
	if gates == nil {
		gates = []any{}
	}
	return emitJSONEnvelope(out, "gates", gates)
}

func emitJSONEnvelope(out io.Writer, key string, val any) error {
	blob, err := json.MarshalIndent(map[string]any{key: val}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(blob))
	return nil
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
	gatesState    string
	gatesJSON     bool
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
		return factoryAnswer(env, args[0], answerText, answerOptions, answerSource)
	},
}

// factoryAnswer records a USER: decision on a gate. Extracted from the cobra
// handler (REQ-CROSS-372) so the refusal rendering is testable against a stub
// server; behavior unchanged by the extraction.
func factoryAnswer(env *factoryEnv, externalID, text, options, source string) error {
	if text == "" {
		return fmt.Errorf("--text is required — the answer is recorded verbatim as the USER: decision")
	}
	payload := map[string]any{"system_id": env.SystemID, "answer": text}
	if options != "" {
		payload["chosen_option_keys"] = strings.Split(options, ",")
	}
	if source != "" {
		payload["source_tag"] = source
	}
	// An entry/completion gate is "governed": Core.Gates.answer routes it to
	// answer_reviewed, which refuses unless the answer carries a review
	// submission proving it is made against the current reviewed state (the
	// gate's own content_fingerprint + evaluated_scope_fingerprint). No prior
	// CLI path sent it, so governed gates could only be answered in Mission
	// Control. Fetch the gate and, when governed, attach that review; a non-
	// governed gate takes none (the server refuses a review on one).
	if gs, gb, ge := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", externalID, env.SystemID), nil); ge == nil && gs == 200 {
		if g, ok := dataOf(gb)["gate"].(map[string]any); ok {
			// REQ-CROSS-372: the server's own signal decides — answer_readiness
			// .requires_review — with the purpose as the fallback for a server
			// that predates it (REQ-CROSS-354 later changes the predicate in
			// one place, on the server).
			readiness, _ := g["answer_readiness"].(map[string]any)
			requires, served := readiness["requires_review"].(bool)
			purpose := str(g, "purpose")
			if (served && requires) || (!served && (purpose == "entry" || purpose == "completion")) {
				payload["review"] = map[string]any{
					"content_fingerprint":         str(g, "content_fingerprint"),
					"evaluated_scope_fingerprint": str(g, "evaluated_scope_fingerprint"),
				}
			}
		}
	}
	status, body, err := env.call("POST", "/api/v1/sync/gates/"+externalID+"/answer", payload)
	if err != nil {
		return err
	}
	switch status {
	case 200:
		gate, _ := dataOf(body)["gate"].(map[string]any)
		printSuccess("answered %s (%s): %s", str(gate, "external_id"), str(gate, "source_tag"), str(gate, "answer"))
	default:
		// REQ-CROSS-372: the server's sentence, verbatim — a first-wins 409 still
		// carries "already answered (first-wins)" and its winner; any other
		// refusal keeps the arm the server named instead of being replaced.
		return serverRefusal("answer refused", status, body)
	}
	return nil
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
		return serverRefusal("", status, body)
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
				return fmt.Errorf("spec ack failed for %s: %v (%v)", externalID, serverRefusal("", aStatus, aBody), err)
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
			return fmt.Errorf("ack failed for %s: %v (%v)", externalID, serverRefusal("", aStatus, aBody), err)
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
	Long: "Generates the image through the platform's governed image service:\n" +
		"the backend stores it in the tenant's storage and returns a URL; the CLI\n" +
		"never handles bytes.",
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
			return serverRefusal("", status, body)
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
	evidenceRole   string
)

// buildEvidenceResults turns the --pass/--fail/--skip id lists into per-target
// result maps. When role is non-empty it is stamped on every result: a "RED"
// result is what Core.RDD.Reconcile.red_recorded? requires to promote an SR
// (reconcile.ex). Leaving role empty omits the key entirely, so a caller that
// passes no --role sends exactly the pre-role payload.
func buildEvidenceResults(pass, fail, skip, role string) []map[string]any {
	// The server matches the role exactly (reconcile.ex promotes on "RED") and
	// validates nothing, so a lower-case role posts fine and promotes nothing.
	role = strings.ToUpper(strings.TrimSpace(role))
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
	for _, pair := range []struct{ ids, result string }{{pass, "pass"}, {fail, "fail"}, {skip, "skip"}} {
		for _, id := range strings.Split(pair.ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				m := map[string]any{"target_external_id": id, "target_type": targetType(id), "result": pair.result}
				if role != "" {
					m["role"] = role
				}
				results = append(results, m)
			}
		}
	}
	return results
}

// evidenceOpts carries the `factory evidence` flags to the runnable body so the
// command is testable without cobra (REQ-CROSS-378).
type evidenceOpts struct {
	kind, log, totals, pass, fail, skip, role, revision string
}

var evidenceRevision string

var factoryEvidenceCmd = &cobra.Command{
	Use:   "evidence",
	Short: "Post a test/CI run as evidence (sha-pinned; feeds Done-decays)",
	Long: `Post one run as evidence for the items it exercised. --pass, --fail and
--skip take item ids (requirements and epics). Every result is pinned to the
repository HEAD at record time, or to --revision <commit> when given.

--role is RED for a red-first run, or empty for a passing one; it is never
lower or upper — the trace class is decided by the trace, not the evidence.
The model:
  1. record RED with --fail <SR> --role RED while HEAD is at the RED commit,
     or later with --revision <red-commit>
  2. record the passing run with --pass <SR> after the fix
Reconcile needs the RED: TODO -> IN_PROGRESS does not happen without a
recorded RED, and a passing lower trace with no RED before it is reported
as a FAIL. Currency is role-aware: a RED never shadows a passing result,
and the server warns at record time about a pass with no RED before it or
a RED recorded after a pass. An epic needs evidence of its own — without
it the epic reads :claimed and its completion gate is refused. So does the
user requirement a completion gate will name (UR-<suffix> for EPIC-<suffix>):
a run posted on the epic or the SRs does not cover the UR; --pass the UR too,
or the gate refuses "not yet". Evidence for
completion is pinned to the delivered (merged) revision, not the branch head.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return factoryEvidenceRun(env, evidenceOpts{
			kind: evidenceKind, log: evidenceLog, totals: evidenceTotals,
			pass: evidencePass, fail: evidenceFail, skip: evidenceSkip,
			role: evidenceRole, revision: evidenceRevision,
		})
	},
}

func factoryEvidenceRun(env *factoryEnv, o evidenceOpts) error {
	results := buildEvidenceResults(o.pass, o.fail, o.skip, o.role)
	if len(results) == 0 {
		return fmt.Errorf("no targets — give at least --pass or --fail")
	}

	totals := map[string]any{}
	for _, pair := range strings.Split(o.totals, ",") {
		if k, v, found := strings.Cut(pair, "="); found {
			if n, err := strconv.Atoi(v); err == nil {
				totals[k] = n
			}
		}
	}

	// REQ-CROSS-378: --revision pins the run to a named commit — the RED commit,
	// recorded after the fix landed — without a checkout. The run's sha is the
	// short form; every result carries the full revision so the row says exactly
	// what was tested. Default: HEAD, as before.
	rev := "HEAD"
	if o.revision != "" {
		rev = o.revision
	}
	full := gitOut(env.Root, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if full == "" {
		return fmt.Errorf("--revision %q is not a commit in this repository — give a sha, tag or branch git resolves", rev)
	}
	sha := gitOut(env.Root, "rev-parse", "--short", full)
	for _, r := range results {
		r["revision"] = full
	}
	status, body, err := env.call("POST", "/api/v1/sync/evidence", map[string]any{
		"system_id":   env.SystemID,
		"external_id": "RUN-" + time.Now().UTC().Format("2006-01-02T15-04-05Z") + "-" + sha,
		"kind":        o.kind,
		"sha":         sha,
		"branch":      gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
		"ran_at":      time.Now().UTC().Format(time.RFC3339),
		"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"},
		"totals":      totals,
		"log_ref":     o.log,
		"results":     results,
	})
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("", status, body)
	}
	run, _ := dataOf(body)["run"].(map[string]any)
	printSuccess("evidence %s: %s (%d targets, sha %s)", str(dataOf(body), "result"), str(run, "external_id"), len(results), sha)
	// REQ-CROSS-378: the server's record-time warnings (a pass with no RED before
	// it; a RED that a passing result already outranks) are printed, never
	// swallowed — a warning is not a refusal.
	for _, w := range stringSlice(dataOf(body)["warnings"]) {
		printWarning("%s", w)
	}
	return nil
}

// ---------------------------------------------------------------- drift

var driftReport bool

var factoryDriftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Compare each target's evidence sha to the working tree (Done decays)",
	Long: `Compare each target's evidence sha to the working tree (Done decays).

A target whose recorded run no longer matches the head, or whose run's
validity lapsed, is a drift item: your-move lists it as [Drift] with its
basis — the run's revision and the head it no longer matches, or the run's
time with its validity lapsed — and the re-record path, a fresh passing run
recorded at the current head with 'factory evidence --pass <id>'.`,
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
			return serverRefusal("", status, body)
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
		// Store-backed (REQ-CROSS-329): watch's per-cycle sync is the same
		// file-derived push the flip retires — decline up front rather than
		// looping doomed syncs. Resolved by walking up, so a subdir invocation
		// is caught too; no separate pull-only watch entrypoint exists.
		if _, active := storeBackedFromCwd(); active {
			printInfo("this workspace is store-backed (process/store-backed.md) — 'factory watch' syncs file ledgers that are retired; there is nothing to watch")
			printInfo("read store state with 'modernpath working-set pull' / 'modernpath factory status'; write it with 'modernpath author'")
			return nil
		}

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
			exit(0)
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
	factoryGatesCmd.Flags().StringVar(&gatesState, "state", "", "list gate history in this state: open, answered, dismissed, superseded, all (default: the open queue)")
	factoryGatesCmd.Flags().BoolVar(&gatesJSON, "json", false, "emit the server's gate envelope as JSON on stdout and nothing else")

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
	factoryEvidenceCmd.Flags().StringVar(&evidenceRole, "role", "", "evidence role stamped on every result this run — RED for a red-first result (upper-cased here; the server matches RED exactly); empty = unset")
	factoryEvidenceCmd.Flags().StringVar(&evidenceRevision, "revision", "", "pin the run to this commit (sha, tag or branch) instead of HEAD — record a RED at the RED commit without a checkout")

	// Q-ARCH-016 (USER:2026-08-18): --report was advertised in drift's own
	// output but never registered; the drift-report POST was unreachable.
	factoryDriftCmd.Flags().BoolVar(&driftReport, "report", false,
		"POST the drift facts to the server (marks affected evidence stale until re-verified)")

	factoryWatchCmd.Flags().IntVar(&watchInterval, "interval", 120, "seconds between cycles")
	factoryWatchCmd.Flags().IntVar(&watchCycles, "cycles", 0, "stop after N cycles (0 = forever)")

	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivateSource, "source", "", "optional attributable source (the server composes one when omitted)")
	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivatePin, "pin", "", "existing release PIN confirmation (otherwise prompt or --pin-stdin)")
	factoryReleaseActivateCmd.Flags().StringVar(&releaseActivateReason, "reason", "", "optional activation reason recorded by the server")
	factoryReleaseActivateCmd.Flags().BoolVar(&releaseActivateCloseCurrent, "close-current", false, "explicitly close other open releases in this system")
	factoryReleaseActivateCmd.Flags().BoolVar(&releaseActivatePinStdin, "pin-stdin", false, "read the existing compliance PIN from stdin")
	factoryReleaseCmd.AddCommand(factoryReleaseUseCmd, factoryReleaseShowCmd, factoryReleaseClearCmd, factoryReleaseActivateCmd)

	factoryPinCmd.AddCommand(factoryPinSetCmd)
	factoryPinSetCmd.Flags().BoolVar(&pinSetStdin, "pin-stdin", false,
		"read the PIN from stdin (one line) instead of prompting — for non-interactive use")

	factorySyncCmd.Flags().BoolVar(&factorySyncIfQuiescent, "if-quiescent", false,
		"hook mode: sync only when the workspace is coherent; log outcomes, never error")
	factorySyncCmd.Flags().StringVar(&factorySyncTrigger, "trigger", "manual", "trigger label for the hook log")
	factorySyncCmd.Flags().DurationVar(&factorySyncMinInterval, "min-interval", 60*time.Second,
		"debounce: skip when the last successful sync is younger than this")

	factoryCmd.AddCommand(factoryConnectCmd, factoryStatusCmd, factorySyncCmd, factoryGatesCmd,
		factoryAnswerCmd, factoryPullCmd, factoryEvidenceCmd, factoryDriftCmd, factoryWatchCmd,
		factoryManifestCmd, factoryReleaseCmd, factoryPinCmd, factoryImageCmd)
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
