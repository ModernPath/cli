package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/opschema"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
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

// reportSyncOutcome — REQ-CROSS-386 (EPIC-CLI-018): render a batch outcome.
// 200: the ok/conflict summary. 207: the same summary plus one line per
// failed, skipped or deferred op with the server's reason; a failed or skipped
// op exits non-zero naming the retry, a deferred document alone is a warning
// (the row is kept for the embedding backfill and a later sync retries it).
func reportSyncOutcome(status int, body map[string]any) error {
	results, _ := dataOf(body)["results"].([]any)
	counts := map[string]int{}
	var failed, skipped, deferred, kept []map[string]any
	for _, r := range results {
		m, _ := r.(map[string]any)
		counts[str(m, "result")]++
		// SR-ROADMAP-012: an item a release move keeps in its open release.
		if k, ok := m["release_kept"].(map[string]any); ok {
			kept = append(kept, k)
		}
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
	for _, k := range kept {
		fmt.Printf("  kept %s in %s (workspace names %s)\n", str(k, "item"), str(k, "kept_release"), str(k, "workspace_release"))
	}
	if len(kept) > 0 {
		fmt.Printf("  %d item(s) were moved on a release page and stay in that release while it is open; change the release there, or update the workspace to match.\n", len(kept))
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
