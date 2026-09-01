package cmd

// REQ-CROSS-221 (EPIC-CLI-003) — `modernpath migrate report`: the dry-run
// ledger-import fidelity report. The decided command surface:
// the migration verbs live under `modernpath migrate`.
//
// The report writes nothing anywhere. It exits non-zero while the corpus
// carries losses the op path would not survive (lost/truncated/transformed
// and not accepted by an applied human gate answer) — a gate input, not a
// repair tool.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/opschema"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

var (
	migrateAcceptPath    string
	migrateRunAcceptPath string
	// migrateRunFromEmpty asserts the target holds no process rows for this
	// system, which is what makes the final import self-proving: every gate is
	// created rather than updated, so its answer block is written from the
	// corpus instead of meeting the server's answered-gate freeze
	// (REQ-CROSS-257).
	migrateRunFromEmpty bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "One-time ledger→server migration (EPIC-CLI-003)",
	Long: `The ledger→server import: measure fidelity, run the import, flip authority.

  report   dry-run fidelity report over the tracked corpus (writes nothing)

The tracked ledgers stay authoritative until the import is done, verified,
and the flip is accepted at its own human gate.`,
}

var migrateRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the one-time ledger import against the bound server, verified",
	Long: `The existing sync path run to completion, then proven: an immediate
second pass must change nothing (idempotence), every emitted id must read
back from the store, refusals fail loudly, and the run records its identity
durably as an evidence run. The import never deletes anything server-side;
the tracked ledgers stay authoritative until the flip's own human gate.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if !hasProcessRecords(root) {
			return fmt.Errorf("no process records under %s — nothing to import (tasks/*-REQUIREMENTS.md and WORKLIST.md absent; wrong directory?)", root)
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return migrateRun(env)
	},
}

var (
	migrateFlipGate        string
	migrateFlipFingerprint string
	migrateFlipAnswer      string
)

var migrateFlipCmd = &cobra.Command{
	Use:   "flip",
	Short: "Prepare the authority flip (completion-gate material — refuses without a USER: source)",
	Long: `Re-verifies through the import run, activates the store-backed
declaration with the gate answer's USER: source, and freezes the retired
list in the tracked marker (process/store-backed.md). It deletes nothing:
the retirement itself is the one reviewable flip commit a human session
makes on the completion gate's applied answer.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := os.Getwd()
		if err != nil {
			return err
		}
		if !hasProcessRecords(root) {
			return fmt.Errorf("no process records under %s — nothing to flip", root)
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return migrateFlip(env, migrateFlipGate, migrateFlipFingerprint, migrateFlipAnswer)
	},
}

var migrateReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Dry-run fidelity report: what the sync op path drops, truncates, or transforms",
	Long: `Reconciles the tracked corpus against the ops the sync path would emit,
in both directions, and itemizes every field that would be lost, truncated,
or transformed — per record, never summarized away. Corpus hygiene drift is
flagged with file and line, never repaired.

Exit is non-zero while blocking losses remain: lost/truncated/transformed
entries not accepted by an applied human gate answer (--accept). Entries
excluded by design never block. The command writes nothing.`,
	Args: cobra.NoArgs,
	RunE: runMigrateReport,
}

func init() {
	migrateReportCmd.Flags().StringVar(&migrateAcceptPath, "accept", "",
		"file of accepted residue keys (one RecordID|Field per line, # comments) from an applied gate answer")
	migrateRunCmd.Flags().StringVar(&migrateRunAcceptPath, "accept", "",
		"file of accepted residue keys, the same one the report takes — the import refuses on any blocking loss it does not cover")
	migrateRunCmd.Flags().BoolVar(&migrateRunFromEmpty, "from-empty", false,
		"the final import to a flip target: refuse before writing unless the store holds no process rows for this system")
	migrateFlipCmd.Flags().StringVar(&migrateRunAcceptPath, "accept", "",
		"file of accepted residue keys, for the flip's re-verification import")
	migrateFlipCmd.Flags().StringVar(&migrateFlipGate, "gate", "",
		"the ANSWERED completion gate's external id (required)")
	migrateFlipCmd.Flags().StringVar(&migrateFlipFingerprint, "gate-fingerprint", "",
		"the gate's current content-shadow identity (required)")
	migrateFlipCmd.Flags().StringVar(&migrateFlipAnswer, "gate-answer", "",
		"the gate's stored answer, echoed verbatim (required) — a declaration rides only the answer actually given")
	migrateCmd.AddCommand(migrateReportCmd, migrateRunCmd, migrateFlipCmd)
	rootCmd.AddCommand(migrateCmd)
}

// migrateRun executes the one-time import against the bound server
// (REQ-CROSS-224): the existing sync path run to completion, then proven —
// an immediate second pass must change nothing (idempotence), every emitted
// id must read back from the store (bidirectional verification), refusals
// fail loudly, and the run records its identity durably as an evidence run.
// The import never deletes anything server-side.
func migrateRun(env *factoryEnv) error {
	// A first apply against an empty store is legitimately long server-side:
	// the batch ingests every document synchronously, and the phase-D
	// documents pay real per-chunk embedding calls. The 120s default and a
	// 10-minute ceiling each abandoned a live batch the server then finished
	// alone — the client walks away, the store keeps writing, and the run's
	// own verification never happens. Reruns are shadow-fast, so the room
	// costs nothing on any pass after the first.
	env.callTimeout = 45 * time.Minute

	// The import must be the only writer for its duration. A second client
	// applying DIFFERENT payloads for the same ids flips the store's shadow
	// hashes back and forth, and the second pass then reports a thousand
	// changed records that no corpus edit explains — idempotence becomes
	// unprovable, and the reader is left blaming the server. This is not
	// hypothetical here: the workspace's Stop/SessionStart hooks run
	// `modernpath factory sync` from whatever binary sits on PATH, and a
	// binary built before this one's op builders changed emits a different
	// payload for the same record. The hooks already honour this lock; the
	// import simply never took it.
	releaseLock, err := migrateHoldSyncLock(env.Root)
	if err != nil {
		return err
	}
	defer releaseLock()
	migrateWarnForeignWriter(env.Root)

	ops, warnings, err := env.workspaceOps()
	if err != nil {
		return err
	}
	for _, w := range warnings {
		printWarning("%s", w)
	}
	sha := gitOut(env.Root, "rev-parse", "HEAD")
	if sha == "" {
		sha = "no-git"
	}
	printInfo("migrate run: %d ops against %s (system %d), corpus %s\n", len(ops), env.APIURL, env.SystemID, sha)

	// REQ-CROSS-257: the from-empty claim is checkable, and it is checked
	// before any other gate because it is the one that decides whether this run
	// can prove its own answer blocks at all.
	if migrateRunFromEmpty {
		if err := migrateRefuseUnlessEmpty(env); err != nil {
			return err
		}
	}

	// A duplicate external id in one batch can never be right: the second op
	// ping-pongs the store's shadow hash forever, so idempotence is
	// unprovable. Refuse before anything is posted (observed live:
	// two duplicated WORKLIST rollup rows).
	if dups := duplicateOpIDs(ops); len(dups) > 0 {
		return fmt.Errorf("migrate run: %d duplicate id(s) in the op stream — fix the corpus first: %s",
			len(dups), strings.Join(dups, ", "))
	}

	// The same fidelity engine `migrate report` runs, BEFORE the first batch
	// (§224.3 with §221.4). A blocking loss is content the corpus holds and
	// the op path does not carry: import it and the store is authoritative
	// over a record that never arrived, with nothing left to compare it to.
	// So the import refuses while one stands, names them, and sends the
	// reader to the report — which is where the full inventory lives.
	//
	// Residue an applied gate answer accepted is not a blocker: --accept
	// carries the same keys the report takes. This gate is the import's
	// alone. Routine `sync` and the hooks stay fire-and-forget: a corpus
	// author mid-edit must not be stopped by a loss that only matters when
	// authority moves.
	report, _, _, err := migrateFidelity(env.Root)
	if err != nil {
		return err
	}
	accepted, err := loadAcceptedResidue(migrateRunAcceptPath)
	if err != nil {
		return err
	}
	blocking := report.Blocking(accepted)
	if len(blocking) > 0 {
		named := make([]string, 0, len(blocking))
		for _, l := range blocking {
			named = append(named, fmt.Sprintf("%s | %s | %s%s%s", l.RecordID, l.Field, l.Detail, acceptKeyMarker, l.Key()))
		}
		sort.Strings(named)
		return fmt.Errorf("migrate run refused: %d blocking fidelity loss(es) — the corpus does not survive the op path yet; run `modernpath migrate report` for the full inventory, fix the corpus, or accept the residue at the gate (--accept):\n  %s",
			len(blocking), strings.Join(capList(named, 20), "\n  "))
	}

	// REQ-CROSS-257: which gates the store held answered BEFORE this run. Taken
	// here, before the first batch pass, because after it every gate this run
	// wrote reads back as answered and the distinction is gone.
	preAnswered := migrateGateStatesBeforeRun(env)

	// Pass 1: apply. Pass 2: prove idempotence — the hash-diff must make an
	// immediate rerun a zero-change no-op (§224.1).
	first, _, err := migrateSyncPass(env, ops)
	if err != nil {
		return err
	}
	second, changedIDs, err := migrateSyncPass(env, ops)
	if err != nil {
		return err
	}
	if changed := second["created"] + second["updated"]; changed != 0 {
		return fmt.Errorf("migrate run: not idempotent — the immediate rerun changed %d record(s)%s: %s",
			changed, migrateSecondWriterHint(env, ops, changedIDs), strings.Join(capList(changedIDs, 20), ", "))
	}
	printSuccess("applied: %s; rerun: all unchanged", countsLine(first))

	// Bidirectional verification (§224.2): every emitted id must read back.
	// Store-only records are legitimate (server-born gates and the like) and
	// reported, never failed.
	missing, storeOnly, err := migrateVerifyIDs(env, ops, preAnswered)
	if err != nil {
		return err
	}
	if len(storeOnly) > 0 {
		printInfo("store-born records (kept, not corpus): %d\n", len(storeOnly))
	}
	if len(missing) > 0 {
		return fmt.Errorf("migrate run: %d corpus record(s) did not read back from the store: %s",
			len(missing), strings.Join(missing, ", "))
	}
	printSuccess("verified: every emitted id reads back")

	// §227.1: the import run seeds the store-backed declaration on the
	// server it imported — every replica seeded by this run carries the
	// same fact. Activation is the flip's own attributed act.
	sbStatus, sbBody, err := env.call("POST", "/api/v1/sync/store-backed", map[string]any{
		"system_id": env.SystemID,
		"state":     "seeded",
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-migrate"},
	})
	if err != nil {
		return err
	}
	if sbStatus != 200 {
		return fmt.Errorf("migrate run applied, but the store-backed seed was refused (server %d: %v)", sbStatus, sbBody["error"])
	}
	printSuccess("store-backed declaration seeded (activation is the flip's act)")

	// REQ-CROSS-249/260: the corpus's historical RUN: tokens post as evidence
	// runs — kind migration, every result explicitly inherited_unverified, one
	// result per distinct citing line with that line's own verdict and its raw
	// text verbatim and uncut. Posted while the declaration is merely seeded:
	// the server's migration-kind refusal applies only to an ACTIVE
	// store-backed system (§224.5 stays legal).
	histManifest, fromFile, err := manifest.Load(env.Root)
	if err != nil {
		return err
	}
	if !fromFile {
		histManifest = manifest.Default()
	}
	data, _ := rdd.Snapshot(env.Root, histManifest)
	histRecords := map[string]string{}
	for _, e := range data.Epics {
		rel := e.RecordFS
		if rel == "" {
			rel = e.Record
		}
		histRecords[e.ID] = rdd.ReadEpicRecord(env.Root, rel)
	}
	hist := rdd.BuildEvidenceImport(data, histRecords)
	resultsByRun := map[string][]map[string]any{}
	for _, res := range hist.Results {
		runID, _ := res["run_external_id"].(string)
		row := map[string]any{}
		for k, v := range res {
			if k != "run_external_id" {
				row[k] = v
			}
		}
		resultsByRun[runID] = append(resultsByRun[runID], row)
	}
	histPosted := 0
	for _, run := range hist.Runs {
		id, _ := run["external_id"].(string)
		payload := map[string]any{
			"system_id":   env.SystemID,
			"external_id": id,
			"kind":        "migration",
			"ran_at":      run["ran_at"],
			"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-migrate"},
			"status":      "complete",
			"results":     resultsByRun[id],
		}
		hStatus, hBody, err := env.call("POST", "/api/v1/sync/evidence", payload)
		if err != nil {
			return err
		}
		if hStatus != 200 {
			return fmt.Errorf("historical evidence run %s refused (server %d: %v)", id, hStatus, hBody["error"])
		}
		histPosted++
	}
	// The collapse is disclosed, never silent: a line repeating one tag, and an
	// identical line repeated elsewhere, become that line's single result.
	// Silent truncation reads as "covered everything".
	printSuccess("historical evidence: %d run(s), %d result(s) — one per distinct corpus line, all inherited_unverified; %d repeated occurrence(s) collapsed into their line's result (%d token occurrence(s) in the corpus)",
		histPosted, len(hist.Results), hist.Collapsed, hist.Occurrences)

	// REQ-CROSS-260: and it is read back. Without this the preserved-raw-text
	// claim is asserted by the importer and proved by nothing.
	if err := migrateVerifyEvidence(env, hist); err != nil {
		return err
	}

	// Residue disclosure (§224.3): the same report the pre-write gate read.
	// Nothing blocking is left at this point — what remains is the accepted
	// and by-design residue, stated rather than implied.
	fmt.Printf("residue (must match `migrate report`): %d blocking loss(es), %d accepted\n",
		len(blocking), len(report.Blocking(nil))-len(blocking))

	// Run identity (§224.5): durable server-side, as an evidence run.
	//
	// blocking_residue is the PRE-acceptance count and accepted_residue is what
	// the gate answer covered. Recording the post-acceptance number instead made
	// every successful run record read 0 — the one value that cannot be wrong,
	// and therefore the one that says nothing: a run that accepted nine named
	// losses and a run that had none were indistinguishable afterwards, which is
	// exactly the question the record exists to answer.
	totals := map[string]any{
		"ops":              len(ops),
		"blocking_residue": len(report.Blocking(nil)),
		"accepted_residue": len(report.Blocking(nil)) - len(blocking),
	}
	for k, v := range first {
		totals["applied_"+k] = v
	}
	status, body, err := env.call("POST", "/api/v1/sync/evidence", map[string]any{
		"system_id":   env.SystemID,
		"external_id": "MIGRATE-RUN-" + sha,
		"kind":        "migration",
		"sha":         sha,
		"ran_at":      time.Now().UTC().Format(time.RFC3339),
		"branch":      gitOut(env.Root, "rev-parse", "--abbrev-ref", "HEAD"),
		"runner":      map[string]any{"kind": "agent", "agent_slug": "modernpath-migrate"},
		"totals":      totals,
		"status":      "complete",
	})
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("migrate run applied, but its run record was refused (server %d: %v)", status, body["error"])
	}
	printSuccess("run recorded: MIGRATE-RUN-%s", sha)

	// Idempotent server-side projections, exactly as factory sync runs them.
	_, _, _ = env.call("POST", "/api/v1/sync/project", map[string]any{"system_id": env.SystemID})
	return nil
}

// migrateHoldSyncLock takes the lock the quiescent hook path already honours
// and keeps it fresh for the whole import.
//
// Freshness is the subtle half: a lock older than quiescentLockStaleAfter is
// broken as debris from a crashed run, and the import's own per-batch ceiling
// is that same ten minutes. A slow first run would therefore have its lock
// taken out from under it by the very hook it is holding off, so the lock is
// touched well inside the window for as long as the run lasts.
func migrateHoldSyncLock(root string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(syncLockPath(root)), 0o755); err != nil {
		return nil, fmt.Errorf("migrate run: cannot prepare the sync lock: %w", err)
	}
	drop := acquireSyncLock(root)
	if drop == nil {
		return nil, fmt.Errorf("migrate run: another sync holds %s — two writers applying different payloads for the same ids make idempotence unprovable, so the import refuses rather than racing one. Let it finish (or remove the lock if it is debris) and rerun",
			syncLockPath(root))
	}
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(quiescentLockStaleAfter / 5)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case now := <-tick.C:
				_ = os.Chtimes(syncLockPath(root), now, now)
			}
		}
	}()
	return func() {
		close(done)
		drop()
	}, nil
}

// migrateWarnForeignWriter says so when the binary this workspace's hooks
// invoke is not the one running the import.
//
// The lock holds a hook off for the duration of the run and not one second
// longer. A hook binary whose op builders differ from this one's rewrites the
// store's shadow hashes with its own payloads at the next Stop — so a run that
// verified clean quietly stops being verified, with nothing in the store
// saying when or why. Flagged, never fatal: a version string is evidence that
// the two binaries differ, not that their payloads do.
//
// IDENTITY first, version only as a tiebreak. Two different builds of a
// development tree both answer `--version` with the placeholder "dev", so a
// version comparison alone reports them identical and stays silent exactly
// where the risk is highest — the case this warning exists for, and the one
// that recurred. Same file on disk is the only real all-clear; a differing
// path with a matching IDENTIFYING version (a release build's embedded commit)
// is the one safe difference.
func migrateWarnForeignWriter(root string) {
	if _, err := os.Stat(hookLogPath(root)); err != nil {
		return // no hook has ever run in this workspace
	}
	path, err := exec.LookPath("modernpath")
	if err != nil {
		return // nothing for a hook to invoke
	}
	if self, err := os.Executable(); err == nil && sameFile(self, path) {
		return // the hooks invoke this very binary
	}
	got := ""
	if out, err := exec.Command(path, "--version").Output(); err == nil {
		got = strings.TrimSpace(string(out))
	}
	// "dev" identifies no build, so a match between two of them proves nothing.
	if Version != "" && Version != "dev" && strings.Contains(got, Version) {
		return
	}
	if got == "" {
		got = "version unreadable"
	}
	printWarning("this workspace's sync hooks invoke %s (%s) and the import is running a different binary (%s) — if their op builders differ, the next hook-triggered sync overwrites this run's shadow hashes with its own payloads and the import stops being verified. Install this build, or disable the hooks, before they fire again.",
		path, got, Version)
}

// sameFile reports whether two paths name one file, resolving symlinks so an
// installed shim pointing at this binary is not mistaken for a second writer.
func sameFile(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		return false
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		return false
	}
	if ra == rb {
		return true
	}
	fa, err := os.Stat(ra)
	if err != nil {
		return false
	}
	fb, err := os.Stat(rb)
	if err != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

// migrateSecondWriterHint distinguishes the two reasons a rerun changes
// records. If the corpus and this builder disagree with the store, a THIRD
// pass finds nothing left to change — pass two already wrote it. If a second
// writer is applying its own payloads for the same ids, the same ids change
// again, and again, for as long as anyone keeps posting. Naming that
// signature is the difference between "the server is broken" and "something
// else is writing here", which is a diagnosis no count can give.
//
// It costs one more batch on a run that has already failed, and it posts the
// same ops — nothing new reaches the store.
func migrateSecondWriterHint(env *factoryEnv, ops []map[string]any, changedIDs []string) string {
	third, thirdIDs, err := migrateSyncPass(env, ops)
	if err != nil {
		return ""
	}
	if third["created"]+third["updated"] == 0 {
		return " (a third pass settled: the store now holds what the corpus sent)"
	}
	was := map[string]bool{}
	for _, id := range changedIDs {
		was[id] = true
	}
	repeat := 0
	for _, id := range thirdIDs {
		if was[id] {
			repeat++
		}
	}
	if repeat == 0 {
		return ""
	}
	return fmt.Sprintf(" — %d of them changed AGAIN on a third pass, which is the signature of a second writer applying different payloads for the same ids, not of a corpus that failed to land; check %s and which `modernpath` binary the hooks invoke",
		repeat, hookLogPath(env.Root))
}

// migrateSyncPass posts one batch and returns the per-result counts. A
// conflict is the server refusing a row — the import fails naming it rather
// than absorbing the refusal (§224.4).
func migrateSyncPass(env *factoryEnv, ops []map[string]any) (map[string]int, []string, error) {
	var changed []string
	batch := map[string]any{
		"schema_version": opschema.SchemaVersion,
		"system_id":      env.SystemID,
		"ops":            ops,
	}
	if env.CurrentRelease != "" {
		batch["release"] = env.CurrentRelease
	}
	status, body, err := env.call("POST", "/api/v1/sync/batch", batch)
	if err != nil {
		return nil, nil, err
	}
	if status != 200 {
		return nil, nil, fmt.Errorf("server %d: %v", status, body["error"])
	}
	counts := map[string]int{}
	var conflicts []string
	if results, ok := dataOf(body)["results"].([]any); ok {
		for _, r := range results {
			m, _ := r.(map[string]any)
			counts[str(m, "result")]++
			switch str(m, "result") {
			case "conflict":
				conflicts = append(conflicts, str(m, "external_id"))
			case "created", "updated":
				changed = append(changed, str(m, "external_id"))
			}
		}
	}
	if len(conflicts) > 0 {
		return nil, nil, fmt.Errorf("migrate run: the store refused %d row(s) as conflicts (server-edited, kept): %s",
			len(conflicts), strings.Join(conflicts, ", "))
	}
	return counts, changed, nil
}

// migrateReadSurface is one required read surface: the op type it verifies,
// the path that serves it, and the collection key inside the response.
type migrateReadSurface struct {
	opType, path, key string
	// syncBorn marks a surface that serves more than this import's rows, and
	// says how to tell them apart. nil = every served row belongs to this
	// import. The predicate is per-surface because the two shared tables do
	// not share a stamp: a document is tenant-wide and carries system_id plus
	// a `sync:` source_revision, while an imported TASK is epic-parented, so
	// its system_id is NULL — the document predicate would drop every task and
	// report the whole population as missing.
	syncBorn func(row map[string]any, env *factoryEnv) bool
}

// migrateReadSurfaces is the one definition of what the import must be able to
// read. The verification and the from-empty precondition both take it from
// here: two lists would let the run verify surfaces the precondition never
// counted, which is the same class of drift the retired-path families avoid by
// having a single home.
//
// Every entry is REQUIRED. There is no optional surface — a surface a
// deployment does not serve is a verification failure (REQ-CROSS-255).
func migrateReadSurfaces(env *factoryEnv) []migrateReadSurface {
	return []migrateReadSurface{
		{opType: "upsert_requirement", path: fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID), key: "requirements"},
		{opType: "upsert_requirement:user", path: fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID), key: "user_requirements"},
		{opType: "upsert_epic", path: fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), key: "epics"},
		{opType: "upsert_gate", path: fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), key: "gates"},
		{opType: "upsert_backlog_record", path: fmt.Sprintf("/api/v1/sync/backlog?system_id=%d", env.SystemID), key: "backlog"},
		// REQ-CROSS-252/253: the new first-class records read back like every
		// other kind. The archive surface serves identity + content_sha256,
		// never the bytes — the sha equality IS the byte proof, and raw_content
		// discloses as an unverified class by design.
		{opType: "upsert_scenario", path: fmt.Sprintf("/api/v1/sync/scenarios?system_id=%d", env.SystemID), key: "scenarios"},
		{opType: "upsert_process_record", path: fmt.Sprintf("/api/v1/sync/process-records?system_id=%d", env.SystemID), key: "process_records"},
		// REQ-CROSS-264: the ninth surface. `tasks` is the PRODUCT Task table,
		// so it is filtered on the sync-born stamp at the surface AND here —
		// and the --from-empty precondition consumes this same list, so a
		// store holding only Board-made tasks still counts as empty.
		{opType: "upsert_task", path: fmt.Sprintf("/api/v1/sync/tasks?system_id=%d", env.SystemID), key: "tasks", syncBorn: migrateTaskSyncBorn},
		// Documents were the one emitted type nothing read back: the sync
		// scope serves no document list, so verification goes through the
		// knowledge library index. It is tenant-scoped, so rows are filtered
		// to this system here.
		{opType: "upsert_document", path: "/api/v1/knw/documents", key: "documents", syncBorn: migrateDocumentSyncBorn},
	}
}

// The imported-evidence read surface (REQ-CROSS-260), declared ONCE here.
//
// The existing evidence GET projects a collapsed current-only pass/fail per
// target, which excludes the entire imported population — every historical
// result is inherited_unverified and many are skip. This surface serves the
// runs and their per-result fields: outcome, role, validity of every kind, the
// line token in test_case_ref, and the raw text verbatim.
//
// Shape (response shapes are not contract-governed, so no contract wave):
//
//	GET  /api/v1/sync/evidence/runs?system_id=<id>&kind=migration
//	200  {"data": {"runs": [
//	       {"external_id": "HIST-RUN:2026-08-20:tag", "kind": "migration",
//	        "ran_at": "...", "status": "complete",
//	        "results": [
//	          {"target_external_id": "REQ-…", "target_type": "requirement",
//	           "result": "pass|fail|skip", "role": "historical",
//	           "validity": "inherited_unverified",
//	           "test_case_ref": "line:<16 hex>", "raw_evidence": "…verbatim…"}
//	        ]}]}}
//
// Results are NESTED inside their run, mirroring the post payload, so the
// comparison is a containment check against the shape that was sent rather than
// a shape mapping.
const migrateEvidenceReadKey = "runs"

func migrateEvidenceReadPath(env *factoryEnv) string {
	return fmt.Sprintf("/api/v1/sync/evidence/runs?system_id=%d&kind=migration", env.SystemID)
}

// migrateVerifyEvidence reads the imported evidence back and proves the raw
// text survived (REQ-CROSS-260). It is what converts REQ-CROSS-249's
// preserved-raw-text clause from asserted to proved: nothing served any
// evidence-result field before, so a store that truncated every line would have
// verified clean.
//
// The surface is required. An unreachable one fails the run for the same reason
// every other read surface does (REQ-CROSS-255): ids it cannot read are ids it
// did not check.
func migrateVerifyEvidence(env *factoryEnv, hist rdd.EvidenceImport) error {
	if len(hist.Runs) == 0 {
		return nil // nothing was posted, so there is nothing to read back
	}
	items, err := fetchList(env, migrateEvidenceReadPath(env), migrateEvidenceReadKey)
	if err != nil {
		return fmt.Errorf("migrate run: the imported-evidence read surface could not be reached (%v) — %d posted run(s) and %d result(s) are UNVERIFIED",
			err, len(hist.Runs), len(hist.Results))
	}
	// run external_id → line token → served result
	served := map[string]map[string]map[string]any{}
	for _, it := range items {
		run, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := run["external_id"].(string)
		if id == "" {
			continue
		}
		byToken := map[string]map[string]any{}
		results, _ := run["results"].([]any)
		for _, raw := range results {
			res, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if ref, _ := res["test_case_ref"].(string); ref != "" {
				byToken[ref] = res
			}
		}
		served[id] = byToken
	}

	var problems []string
	for _, run := range hist.Runs {
		id, _ := run["external_id"].(string)
		if _, ok := served[id]; !ok {
			problems = append(problems, fmt.Sprintf("%s: the run did not read back", id))
		}
	}
	for _, res := range hist.Results {
		runID, _ := res["run_external_id"].(string)
		token, _ := res["test_case_ref"].(string)
		got, ok := served[runID][token]
		if !ok {
			if _, runServed := served[runID]; runServed {
				problems = append(problems, fmt.Sprintf("%s %s: the result did not read back", runID, token))
			}
			continue
		}
		if gotRaw, _ := got["raw_evidence"].(string); gotRaw != res["raw_evidence"] {
			sent, _ := res["raw_evidence"].(string)
			problems = append(problems, fmt.Sprintf("%s %s: raw text differs (sent %d rune(s), served %d)",
				runID, token, len([]rune(sent)), len([]rune(gotRaw))))
		}
		if gotValidity, _ := got["validity"].(string); gotValidity != res["validity"] {
			problems = append(problems, fmt.Sprintf("%s %s: validity served as %q, sent %q",
				runID, token, gotValidity, res["validity"]))
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return fmt.Errorf("migrate run: the store does not serve back the historical evidence this run posted — %d problem(s): %s",
			len(problems), strings.Join(capList(problems, 8), "; "))
	}
	printSuccess("historical evidence read back: %d run(s), %d result(s) — raw text round-trips verbatim", len(hist.Runs), len(hist.Results))
	return nil
}

// migrateGateStatesBeforeRun snapshots which gates the store ALREADY held
// answered, with one read taken BEFORE the first batch pass (REQ-CROSS-257).
//
// The server's S6 rule drops an answered gate's answer block on write, so those
// fields are unwritable and the readback skips them. Keyed on the SERVED state,
// that skip also covered a gate the run had just created — the store serves it
// answered because the run wrote the answer — so a from-empty import skipped
// comparing exactly the answer blocks it had written. Keyed on this snapshot,
// the skip covers only what the store owned beforehand.
//
// A snapshot that cannot be read treats no gate as pre-answered: every answer
// block is then compared, which fails loudly rather than quietly excusing
// fields. The unreachable gates surface fails verification on its own terms.
func migrateGateStatesBeforeRun(env *factoryEnv) map[string]bool {
	answered := map[string]bool{}
	items, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
	if err != nil {
		printWarning("pre-run gate-state snapshot: %v — no gate is treated as already answered, so every answer block is compared", err)
		return answered
	}
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["external_id"].(string)
		if id != "" && str(m, "state") == "answered" {
			answered[id] = true
		}
	}
	printInfo("pre-run gate snapshot: %d gate(s) the store already holds answered\n", len(answered))
	return answered
}

// migrateRefuseUnlessEmpty is the --from-empty precondition (REQ-CROSS-257):
// the final import to a flip target is a from-empty act, so the target must
// hold no process rows for this system. Every gate is then CREATED and its
// answer block is written from the corpus, instead of meeting the server's
// answered-gate freeze and landing inert.
//
// A surface it cannot read refuses too: a precondition that cannot be evaluated
// is not a precondition that passed.
func migrateRefuseUnlessEmpty(env *factoryEnv) error {
	var counts []string
	total := 0
	for _, rd := range migrateReadSurfaces(env) {
		items, err := fetchList(env, rd.path, rd.key)
		if err != nil {
			return fmt.Errorf("migrate run --from-empty refused: the target's %s surface could not be read (%v), so this run cannot establish that the target is empty. Nothing was written",
				rd.key, err)
		}
		n := 0
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if id, _ := m["external_id"].(string); id == "" {
				continue
			}
			if rd.syncBorn != nil && !rd.syncBorn(m, env) {
				continue // product-born, not a process row this import owns
			}
			n++
		}
		if n > 0 {
			counts = append(counts, fmt.Sprintf("%s %d", rd.key, n))
			total += n
		}
	}
	if total == 0 {
		printSuccess("from-empty precondition: the target holds no process rows for system %d", env.SystemID)
		return nil
	}
	sort.Strings(counts)
	return fmt.Errorf("migrate run --from-empty refused: the target already holds %d process row(s) for system %d — %s. A from-empty import is what makes the final run self-proving: every gate is created, so its answer block is written from the corpus rather than dropped by the store's answered-gate freeze. Clear the target first (process/99-phase5-wipe.sql, rehearsed with process/99-phase5-wipe-dry-run.sql), then rerun. Nothing was written",
		total, env.SystemID, strings.Join(counts, ", "))
}

// migrateOpKind returns the verification key for one op: its type, with the
// requirement kinds separated because upsert_requirement carries two behind
// one type and the read surface serves them under two keys.
func migrateOpKind(op map[string]any) string {
	typ, _ := op["type"].(string)
	if typ == "upsert_requirement" {
		payload, _ := op["payload"].(map[string]any)
		if kind, _ := payload["kind"].(string); kind == "user" {
			return "upsert_requirement:user"
		}
	}
	return typ
}

// migrateVerifyIDs re-reads the store and diffs both directions against the
// emitted ops — presence AND content, for every op type the batch carries.
//
// Round-trip FIDELITY, not id presence: an id that reads back while its fields
// do not is the same silent loss as an id that does not read back, and it is
// harder to see. So every field of every payload is compared against the
// served row, for requirements, user requirements, epics, gates, backlog
// records and documents alike — the field list is DERIVED from the payload,
// because a hand-kept list only ever verifies the fields someone remembered
// when they wrote it, and stops covering each new field the builder gains.
//
// A surface that cannot be READ is a different matter from a field it does not
// SERVE (REQ-CROSS-255). An unserved field is a fact about the schema and is
// disclosed; an unreachable surface leaves every emitted id of its type
// unchecked, so the pass finishes the surfaces it can and then FAILS, naming
// each one it could not reach and how many ids it left unverified. Every
// surface below is required — there is no optional-surface skip.
//
// Three comparison rules make the field comparison safe to apply this widely:
//
//   - a field the read surface does not serve is skipped, not failed; the
//     skip is counted and printed, so an unverifiable field is disclosed
//     rather than mistaken for a verified one;
//   - a served value must CONTAIN what was sent (every element, every key,
//     scalars equal exactly). Server-side enrichment is not loss; a missing
//     element, key or value is;
//   - that skip reaches inside a list of maps, where the served elements form
//     a population: a key NO element carries is a field of the shape the read
//     surface does not serve — disclosed by path, as `scenarios[].position` —
//     while a key only SOME elements carry is a loss and still fails. Without
//     the distinction the epics surface, which serves scenario items without
//     position and spec items as metadata only, fails every run it verifies
//     and the check becomes something to switch off.
//
// preAnswered names the gates the store already held answered BEFORE this run
// (REQ-CROSS-257) — see migrateGateStatesBeforeRun for why the served state is
// the wrong predicate.
func migrateVerifyIDs(env *factoryEnv, ops []map[string]any, preAnswered map[string]bool) (missing, storeOnly []string, err error) {
	emitted := map[string]map[string]bool{}
	for _, op := range ops {
		payload, _ := op["payload"].(map[string]any)
		id, _ := payload["external_id"].(string)
		if id == "" {
			continue
		}
		typ := migrateOpKind(op)
		if emitted[typ] == nil {
			emitted[typ] = map[string]bool{}
		}
		emitted[typ][id] = true
	}
	reads := migrateReadSurfaces(env)

	// REQ-CROSS-255: a surface that does not answer is accumulated, never
	// swallowed. The loop still finishes — a pass that aborts on the first bad
	// surface proves less than one that reports exactly which surfaces it could
	// not reach — but the verdict at the end is a FAILURE, because every
	// emitted id of that type went unchecked. One 404 on /sync/requirements
	// unverifies ~1,310 ids, and the run used to print `verified` over it.
	var unreachable []string
	reached := 0

	served := map[string]map[string]map[string]any{} // opType → external_id → served row
	// The archive is the one kind that serves several rows under one
	// external_id: REQ-CROSS-253 keeps a generation per distinct content, so a
	// file that has changed is legitimately present more than once. Keep them
	// all, and let the comparison pick the generation whose bytes are the ones
	// this run sent.
	archiveGenerations := map[string][]map[string]any{}
	for _, rd := range reads {
		items, err := fetchList(env, rd.path, rd.key)
		if err != nil {
			// Say so immediately, keep verifying the rest, and record it for
			// the verdict. A surface's absence is a verification failure, never
			// an optional-surface skip: the read surfaces this list names are
			// all required.
			printWarning("verification read %s: %v — %d emitted id(s) of this type are UNVERIFIED", rd.key, err, len(emitted[rd.opType]))
			unreachable = append(unreachable, fmt.Sprintf("%s (%d emitted id(s) UNVERIFIED): %v", rd.key, len(emitted[rd.opType]), err))
			continue
		}
		reached++
		if served[rd.opType] == nil {
			served[rd.opType] = map[string]map[string]any{}
		}
		store := map[string]bool{}
		for _, it := range items {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			id, _ := m["external_id"].(string)
			if id == "" {
				continue
			}
			if rd.syncBorn != nil && !rd.syncBorn(m, env) {
				continue
			}
			store[id] = true
			served[rd.opType][id] = m
			if rd.opType == "upsert_process_record" {
				archiveGenerations[id] = append(archiveGenerations[id], m)
			}
		}
		for id := range emitted[rd.opType] {
			if !store[id] {
				missing = append(missing, id)
			}
		}
		for id := range store {
			if emitted[rd.opType][id] {
				continue
			}
			storeOnly = append(storeOnly, id)
		}
		printInfo("verify %s: emitted %d, store %d\n", rd.key, len(emitted[rd.opType]), len(store))
	}

	var fieldMismatches, contentMismatches []string
	skippedFields := map[string]int{} // opType → fields no read surface serves
	for _, op := range ops {
		typ := migrateOpKind(op)
		payload, _ := op["payload"].(map[string]any)
		id, _ := payload["external_id"].(string)
		row := served[typ][id]
		if typ == "upsert_process_record" {
			// "The store serves back what the corpus sent" means the sent bytes
			// are among the generations, not that they are in whichever row the
			// served list ended on. Falling through with no match leaves the
			// last row in place, so genuinely absent bytes still fail on
			// content_sha256 rather than being quietly excused.
			for _, gen := range archiveGenerations[id] {
				if str(gen, "content_sha256") == str(payload, "content_sha256") {
					row = gen
					break
				}
			}
		}
		if row == nil {
			continue // missing, unreadable surface, or already reported
		}
		// A gate the store ALREADY HELD answered owns its answer block: the sync
		// write path drops the whole block for such a row (Core.Sync's S6 rule —
		// the repo can neither reopen an answer nor rewrite one). Those fields
		// are UNWRITABLE, which is not the same as lost: comparing them reports
		// the corpus and the store disagreeing about something the corpus is not
		// allowed to say, on every run, forever.
		//
		// The predicate is the PRE-RUN snapshot, not the served state
		// (REQ-CROSS-257): a gate this run created is served answered because
		// this run wrote the answer, and skipping it would leave a from-empty
		// import unable to prove the one field family it exists to fix.
		frozenAnswer := typ == "upsert_gate" && preAnswered[id]
		// The archive entry's revision is store-owned in the same way, for the
		// same reason. REQ-CROSS-253 identifies an entry by its bytes and keeps
		// the revision where those bytes FIRST appeared, so every later run
		// sends the current revision against a row that legitimately holds an
		// older one. The bytes are still proven: content_sha256 is compared,
		// and this exemption only applies while it agrees — a row whose sha
		// differs is a real loss and must not be bought silence by a differing
		// revision.
		frozenArchiveRevision := typ == "upsert_process_record" &&
			str(row, "content_sha256") == str(payload, "content_sha256")
		for _, f := range sortedKeys(payload) {
			switch f {
			case "external_id", "content_hash", "actor":
				continue // identity and envelope, not carried content
			}
			if frozenAnswer && gateAnswerBlockField(f) {
				skippedFields[typ+"."+f+"[frozen]"]++
				continue
			}
			if frozenArchiveRevision && f == "archive_revision" {
				skippedFields[typ+"."+f+"[frozen]"]++
				continue
			}
			servedField := f
			if typ == "upsert_document" && f == "name" {
				// The library index serves a document's name as "title"
				// (DocumentLibrary builds title from the stored name verbatim).
				// document_type has no served counterpart: the index's ui_type
				// is a computed taxonomy category, not the stored type, so
				// comparing them would mismatch on vocabulary — it stays a
				// disclosed skip.
				servedField = "title"
			}
			got, has := row[servedField]
			if !has {
				if castAwayEmpty(payload[f]) {
					continue // nothing was sent, so nothing is unverified
				}
				skippedFields[typ+"."+f]++
				continue
			}
			sent := payload[f]
			// REQ-CROSS-262: the epic approval's approver name is comparable
			// only when the store says it RESOLVED the declared human. The
			// importer keeps the importing account when a declared name
			// resolves to nobody, and never persists the declared name — so
			// what serves back for such a row is the importing account's own
			// name. Comparing that as a resolution hard-fails 169 of 177 real
			// approvals over a difference the corpus is not allowed to fix.
			// It is named residue: excluded from the compare, and disclosed.
			if typ == "upsert_epic" && f == "approval" && !approvalNameResolved(got) &&
				approvalCarriesName(sent) {
				skippedFields["upsert_epic.approval.approver_name[attribution-fallback]"]++
				sent = withoutApprovalName(sent)
			}
			skips := &skipCollector{}
			roundTripped := payloadRoundTrips(sent, got, typ+"."+f, skips)
			for name := range skips.names {
				skippedFields[name]++
			}
			if roundTripped {
				continue
			}
			fieldMismatches = append(fieldMismatches, fmt.Sprintf("%s.%s", id, f))
		}
		// A document's content is not served by the index, but the sync
		// protocol hash is: the store stamps source_revision "sync:<hash>",
		// echoing the content_hash the batch sent (taken over the payload
		// including content_md). Matching it proves this row was last written
		// by a batch carrying exactly this content — a stale or unwritten row
		// cannot match. It does NOT prove the stored text equals what was
		// sent: the server echoes the hash without recomputing it over stored
		// text, so a server-side truncation would still match here.
		if typ == "upsert_document" {
			want, _ := payload["content_hash"].(string)
			rev, hasRev := row["source_revision"].(string)
			switch {
			case !hasRev || rev == "":
				skippedFields["upsert_document.content_md"]++
			case rev != "sync:"+want:
				contentMismatches = append(contentMismatches, fmt.Sprintf("%s (stored %s, sent sync:%s)", id, rev, want))
			}
		}
	}
	// Two reasons a field went unverified, disclosed apart because they are
	// different facts: one is a thin read surface, the other is a deliberate
	// server rule. The frozen list is printed WHOLE — it is small, bounded by
	// the answer block, and a policy skip elided behind "… N more" is a skip
	// nobody reviews.
	unservedFields, frozenFields := map[string]int{}, map[string]int{}
	for name, n := range skippedFields {
		if strings.HasSuffix(name, "[frozen]") {
			frozenFields[name] = n
			continue
		}
		unservedFields[name] = n
	}
	if len(unservedFields) > 0 {
		printInfo("verify: %d payload field(s) are not served by any read surface — unverified, not verified: %s\n",
			len(unservedFields), strings.Join(topKeys(unservedFields, 12), ", "))
	}
	if len(frozenFields) > 0 {
		printInfo("verify: %d gate field(s) are frozen server-side — the store owns an answered gate's answer block, so the corpus cannot write them and no run can verify them: %s\n",
			len(frozenFields), strings.Join(topKeys(frozenFields, len(frozenFields)), ", "))
	}
	printInfo("verify: %d/%d read surface(s) reached\n", reached, len(reads))
	migrateReportAttribution(served)
	if len(unreachable) > 0 || len(fieldMismatches) > 0 || len(contentMismatches) > 0 {
		sort.Strings(fieldMismatches)
		sort.Strings(contentMismatches)
		var parts []string
		if len(unreachable) > 0 {
			// Stated apart from the mismatches, and worded apart: these ids
			// COULD NOT BE CHECKED, which is a different fact from "did not
			// read back" and must not be reported as one.
			sort.Strings(unreachable)
			parts = append(parts, fmt.Sprintf("%d read surface(s) could not be reached, so their emitted ids are UNVERIFIED — %s",
				len(unreachable), strings.Join(unreachable, "; ")))
		}
		if len(fieldMismatches) > 0 {
			parts = append(parts, "the store no longer serves back what the corpus sent — fields: "+strings.Join(capList(fieldMismatches, 8), ", "))
		}
		if len(contentMismatches) > 0 {
			parts = append(parts, "document content: "+strings.Join(capList(contentMismatches, 8), ", "))
		}
		return nil, nil, fmt.Errorf("migrate run: verification did not complete — %s",
			strings.Join(parts, "; "))
	}
	sort.Strings(missing)
	sort.Strings(storeOnly)
	return missing, storeOnly, nil
}

// --- REQ-CROSS-262: attribution, read back and counted ----------------------

// approvalNameResolved reports whether the store says it resolved the epic's
// declared approver to a real human. Only "named_user" is a resolution; the
// importing-account fallback and an unattributed row are not, and neither is a
// surface that serves no basis at all — an absent discriminator is not evidence
// of resolution.
func approvalNameResolved(servedApproval any) bool {
	m, ok := servedApproval.(map[string]any)
	if !ok {
		return false
	}
	return str(m, "attribution_basis") == "named_user"
}

func approvalCarriesName(sentApproval any) bool {
	m, ok := sentApproval.(map[string]any)
	if !ok {
		return false
	}
	name, _ := m["approver_name"].(string)
	return name != ""
}

// withoutApprovalName copies the sent approval without its approver name, so
// every other field of it is still compared. Copied rather than mutated: the
// payload is the op the batch posted, and a verifier must not edit it.
func withoutApprovalName(sentApproval any) any {
	m, ok := sentApproval.(map[string]any)
	if !ok {
		return sentApproval
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if k == "approver_name" {
			continue
		}
		out[k] = v
	}
	return out
}

// migrateImportingAccountID identifies the account the store falls back to when
// a declared approver name resolves to nobody.
//
// The gate surface serves the answerer's user id and no basis of its own, so
// the three attribution classes cannot be told apart from a gate row alone. The
// epic surface DOES serve the discriminator, and an approval it marks as the
// importing-account fallback names that account outright — so one served
// approval identifies the account for every gate. Returns nil when no served
// approval names it, which is a real state and is disclosed rather than
// guessed around.
func migrateImportingAccountID(served map[string]map[string]map[string]any) any {
	for _, row := range served["upsert_epic"] {
		approval, ok := row["approval"].(map[string]any)
		if !ok {
			continue
		}
		if str(approval, "attribution_basis") != "importing_account_fallback" {
			continue
		}
		if id := approval["approved_by_id"]; id != nil {
			return id
		}
	}
	return nil
}

// migrateReportAttribution prints gate attribution as a first-class count
// group (REQ-CROSS-262).
//
// It is a count group and never a silent skip: a class nobody counts is
// indistinguishable from a verified one. Only the resolved class is a
// verification; the fallback and no-name classes are NAMED RESIDUE — real,
// disclosed, and never a mismatch, because the store cannot hold what the
// corpus declared and the corpus cannot write what the store holds. On the real
// corpus 0 of 235 gates resolve to a declared human, so a class that failed the
// run would fail every run forever.
func migrateReportAttribution(served map[string]map[string]map[string]any) {
	gates := served["upsert_gate"]
	if len(gates) == 0 {
		return
	}
	importing := migrateImportingAccountID(served)
	var resolved, fallback, unnamed, undetermined int
	for _, row := range gates {
		answerer, has := row["answerer_user_id"]
		switch {
		case !has || answerer == nil:
			unnamed++
		case importing == nil:
			// Something answered, but with no served approval naming the
			// importing account there is no way to tell a resolution from the
			// fallback. Saying so beats claiming either.
			undetermined++
		case fmt.Sprint(answerer) == fmt.Sprint(importing):
			fallback++
		default:
			resolved++
		}
	}
	line := fmt.Sprintf("gate attribution: %d resolved to the declared human, %d importing-account fallback, %d with no declared name",
		resolved, fallback, unnamed)
	if undetermined > 0 {
		line += fmt.Sprintf(", %d undetermined (no served approval names the importing account, so a fallback cannot be told from a resolution)", undetermined)
	}
	printInfo("%s — %d gate(s); the fallback, no-name and undetermined classes are named residue, not mismatches\n",
		line, len(gates))
}

// migrateSyncBorn reports whether a served document row came from a sync batch
// rather than from analysis — the store stamps sync writes "sync:<hash>".
func migrateSyncBorn(row map[string]any) bool {
	rev, _ := row["source_revision"].(string)
	return strings.HasPrefix(rev, "sync:")
}

// migrateDocumentSyncBorn: the knowledge library is tenant-wide and holds
// analysis output too, so a document belongs to this import only when it is
// this system's AND its source_revision says sync wrote it.
func migrateDocumentSyncBorn(row map[string]any, env *factoryEnv) bool {
	if sysID, ok := row["system_id"]; ok && fmt.Sprint(sysID) != fmt.Sprint(env.SystemID) {
		return false
	}
	return migrateSyncBorn(row)
}

// migrateTaskSyncBorn is REQ-CROSS-264's stamp read back: source_path set AND
// the sync-born flag. An imported task is epic-parented, so it carries no
// system_id and no `sync:` source_revision — the surface already filters on
// the stamp and serves it, and this is the same predicate read a second time.
func migrateTaskSyncBorn(row map[string]any, _ *factoryEnv) bool {
	if path, _ := row["source_path"].(string); path == "" {
		return false
	}
	switch v := row["sync_born"].(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

// skipCollector gathers the payload paths a comparison could not verify because
// no served value carries them. It de-duplicates, so an epic sending forty
// scenarios that all lack one served key contributes one skip, not forty — the
// count then reads as "how many records", which is the number that matters.
type skipCollector struct{ names map[string]bool }

func (s *skipCollector) add(name string) {
	if s == nil {
		return
	}
	if s.names == nil {
		s.names = map[string]bool{}
	}
	s.names[name] = true
}

func (s *skipCollector) merge(other *skipCollector) {
	if s == nil || other == nil {
		return
	}
	for n := range other.names {
		s.add(n)
	}
}

// payloadRoundTrips reports whether a served value carries everything the
// payload sent. See migrateVerifyIDs for why containment rather than equality.
//
// path names the field being compared ("upsert_epic.scenarios") so a nested
// skip can be disclosed by the path it was found at rather than by its bare
// key; skips, when non-nil, collects them. Passing them through the comparison
// instead of inferring them afterwards is what keeps the disclosure honest —
// only the code that decided not to compare something can say what it skipped.
// dateWidened reports the one documented value transform on the wire: a
// date-only string the applier widens to a midnight timestamp (backlog
// raised_at, REQ-CROSS-251). The served value must BE that widening — any
// other difference is still a mismatch.
func dateWidened(want, got any) bool {
	w, okW := want.(string)
	g, okG := got.(string)
	if !okW || !okG || len(w) != 10 {
		return false
	}
	return strings.HasPrefix(g, w+"T00:00:00")
}

func payloadRoundTrips(want, got any, path string, skips *skipCollector) bool {
	if dateWidened(want, got) {
		return true
	}
	// Through JSON first, so the comparison sees the same shapes the wire
	// produced: an int arrives as a float, a []string as a []any.
	raw, err := json.Marshal(want)
	if err != nil {
		return fmt.Sprint(want) == fmt.Sprint(got)
	}
	var canonical any
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return fmt.Sprint(want) == fmt.Sprint(got)
	}
	return servedContains(canonical, got, path, skips)
}

func servedContains(want, got any, path string, skips *skipCollector) bool {
	switch w := want.(type) {
	case nil:
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		// A LONE served map carries no population to argue from: one absent key
		// is indistinguishable from a lost one, so it stays a failure. Only a
		// list gives the evidence that separates the two — see below.
		for k, v := range w {
			gv, has := g[k]
			if !has {
				// An empty string is not content the store could have kept:
				// see castAwayEmpty. Anything else absent is a lost key.
				if castAwayEmpty(v) {
					continue
				}
				return false
			}
			if !servedContains(v, gv, path+"."+k, skips) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok {
			return false
		}
		// Inside a list of maps the served elements ARE a population, and it
		// separates two things a single element cannot. A key that NO element
		// carries means the read surface does not serve that shape's field at
		// all — unverifiable, so it is disclosed as a skip and excluded from
		// the comparison, exactly as a top-level unserved field already is. A
		// key only SOME elements carry means the surface can serve it and for
		// one element did not: a real loss, and still a failure.
		unserved := systematicallyUnserved(w, g)
		for k := range unserved {
			skips.add(path + "[]." + k)
		}
		for _, item := range w {
			if m, ok := item.(map[string]any); ok {
				item = withoutKeys(m, unserved)
			}
			found := false
			for _, gi := range g {
				// A scratch collector per candidate, merged only on the match:
				// skips discovered while probing an element that turned out to
				// be the wrong one describe nothing that was actually compared.
				scratch := &skipCollector{}
				if servedContains(item, gi, path+"[]", scratch) {
					skips.merge(scratch)
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case string:
		if got == nil {
			// null is how a column that was never written reads back, and an
			// empty string never reaches a column: see castAwayEmpty. A
			// non-empty string against null is content the store did not keep.
			return w == ""
		}
		g, ok := got.(string)
		if !ok {
			return false
		}
		return g == w
	default:
		return fmt.Sprint(want) == fmt.Sprint(got)
	}
}

// castAwayEmpty reports whether a payload value is the empty string — the one
// value the write path is guaranteed to discard rather than store.
//
// Ecto's cast drops an empty string before it reaches a column, so the store
// answers with null or with no key at all. Reading that back as a difference
// reports a loss where there was nothing to lose, on every run, for every
// record whose optional text is blank: 59 of them on the real corpus. The rule
// is exact — EMPTY only. A non-empty string the store answers null for is
// content that did not survive, and stays a failure.
func castAwayEmpty(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

// gateAnswerBlockField names the payload fields Core.Sync's S6 rule freezes
// once a gate is answered: the server drops the whole block for an already
// answered row, so nothing the corpus sends in these fields can ever be
// written. It mirrors the server's block exactly (upsert_gate_row/2), including
// chosen_option_keys and both spellings of the answerer, because a client-side
// list that covers less than the server's freezes leaves a field that fails
// forever, and one that covers more hides a field the server does write.
func gateAnswerBlockField(field string) bool {
	switch field {
	case "state", "answer", "answered_at", "source_tag", "chosen_option_keys", "answerer":
		return true
	}
	return strings.HasPrefix(field, "answerer_")
}

// systematicallyUnserved names the keys the payload's list elements carry that
// no served element of the same list carries. An empty served population proves
// nothing — with nothing to compare against, every key would read as unserved
// — so it yields no skips and the containment check fails on its own terms.
func systematicallyUnserved(want, got []any) map[string]bool {
	servedMaps := 0
	seen := map[string]bool{}
	for _, gi := range got {
		gm, ok := gi.(map[string]any)
		if !ok {
			continue
		}
		servedMaps++
		for k := range gm {
			seen[k] = true
		}
	}
	if servedMaps == 0 {
		return nil
	}
	var unserved map[string]bool
	for _, item := range want {
		wm, ok := item.(map[string]any)
		if !ok {
			continue
		}
		for k := range wm {
			if seen[k] {
				continue
			}
			if unserved == nil {
				unserved = map[string]bool{}
			}
			unserved[k] = true
		}
	}
	return unserved
}

func withoutKeys(m map[string]any, drop map[string]bool) map[string]any {
	if len(drop) == 0 {
		return m
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if drop[k] {
			continue
		}
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// topKeys renders a count map as "key ×n" entries, busiest first, capped.
func topKeys(counts map[string]int, limit int) []string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s ×%d", k, counts[k]))
	}
	return capList(out, limit)
}

func capList(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	return append(append([]string{}, items[:limit]...), fmt.Sprintf("… %d more", len(items)-limit))
}

// duplicateOpIDs names every (type, external_id) pair that appears more than
// once in the op stream.
func duplicateOpIDs(ops []map[string]any) []string {
	seen := map[string]int{}
	var dups []string
	for _, op := range ops {
		typ, _ := op["type"].(string)
		payload, _ := op["payload"].(map[string]any)
		id, _ := payload["external_id"].(string)
		if id == "" {
			continue
		}
		key := typ + "|" + id
		seen[key]++
		if seen[key] == 2 {
			dups = append(dups, key)
		}
	}
	sort.Strings(dups)
	return dups
}

func countsLine(counts map[string]int) string {
	parts := make([]string, 0, len(counts))
	for k, v := range counts {
		parts = append(parts, fmt.Sprintf("%d %s", v, k))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// migrateFidelity builds the dry-run fidelity report over the corpus at root,
// with the ops it was measured against and the snapshot warnings — one
// definition, used by `migrate report` and by the import's pre-write gate, so
// the two can never measure different things.
func migrateFidelity(root string) (rdd.FidelityReport, []rdd.Op, []string, error) {
	m, fromFile, err := manifest.Load(root)
	if err != nil {
		return rdd.FidelityReport{}, nil, nil, err
	}
	if !fromFile {
		m = manifest.Default()
	}
	data, warnings := rdd.Snapshot(root, m)
	ops := rdd.BuildOps(data, func(rel string) string { return rdd.ReadEpicRecord(root, rel) },
		time.Now().UTC().Format("2006-01-02"))
	return rdd.BuildFidelityReport(root, m, data, ops), ops, warnings, nil
}

func runMigrateReport(cmd *cobra.Command, args []string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	// A report over nothing must not read as clean (the vacuous-pass shape
	// modernpath check refuses for the same reason).
	if !hasProcessRecords(root) {
		return fmt.Errorf("no process records under %s — nothing was measured (tasks/*-REQUIREMENTS.md and WORKLIST.md absent; wrong directory?)", root)
	}
	report, ops, warnings, err := migrateFidelity(root)
	if err != nil {
		return err
	}

	accepted, err := loadAcceptedResidue(migrateAcceptPath)
	if err != nil {
		return err
	}

	printInfo("fidelity report — corpus at %s (report writes nothing)\n\n", root)

	fmt.Println("== Counts (corpus rows → emitted ops) ==")
	for _, c := range report.Counts {
		fmt.Printf("  %-40s rows %4d → ops %4d\n", c.Group, c.Rows, c.Ops)
		for _, id := range c.MissingFromOps {
			fmt.Printf("    missing from ops: %s\n", id)
		}
		for _, id := range c.ExtraInOps {
			fmt.Printf("    extra in ops:     %s\n", id)
		}
	}

	// Store direction (§221.1, "when a server is reachable"): compare the
	// emitted ids against what the bound store serves. Unreachable is a
	// disclosed skip, never a silent one.
	storeCompare(ops)

	if len(warnings) > 0 {
		fmt.Printf("\n== Snapshot warnings (%d) ==\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  %s\n", w)
		}
	}

	if len(report.Hygiene) > 0 {
		fmt.Printf("\n== Hygiene drift (%d — flagged, never repaired) ==\n", len(report.Hygiene))
		for _, h := range report.Hygiene {
			fmt.Printf("  %s:%d  %s\n", h.File, h.Line, h.Detail)
		}
	}

	byCat := map[string][]rdd.FidelityLoss{}
	for _, l := range report.Losses {
		byCat[l.Category] = append(byCat[l.Category], l)
	}
	fmt.Printf("\n== Loss inventory (%d entries) ==\n", len(report.Losses))
	for _, cat := range []string{rdd.LossLost, rdd.LossTruncated, rdd.LossTransform, rdd.LossExcluded} {
		entries := byCat[cat]
		if len(entries) == 0 {
			continue
		}
		fmt.Printf("\n-- %s (%d) --\n", cat, len(entries))
		for _, l := range entries {
			// The accept key rides every ACCEPTABLE loss line, unwrapped, so
			// accepting one at the gate is a copy rather than a transcription
			// from a display form that spaces the two halves apart. An
			// excluded-by-design entry never blocks, so offering a key for it
			// would invite a gate answer that decides nothing.
			if cat == rdd.LossExcluded {
				fmt.Printf("  %s | %s | %s\n", l.RecordID, l.Field, l.Detail)
				continue
			}
			fmt.Printf("  %s | %s | %s%s%s\n", l.RecordID, l.Field, l.Detail, acceptKeyMarker, l.Key())
		}
	}

	blocking := report.Blocking(accepted)
	acceptedCount := len(report.Blocking(nil)) - len(blocking)
	fmt.Printf("\n== Verdict ==\n")
	fmt.Printf("  blocking: %d   accepted residue: %d   excluded by design: %d\n",
		len(blocking), acceptedCount, len(byCat[rdd.LossExcluded]))
	if len(blocking) > 0 {
		return fmt.Errorf("fidelity: %d blocking loss(es) — the corpus does not survive the op path faithfully yet", len(blocking))
	}
	fmt.Println("  the corpus survives the op path (modulo accepted and by-design residue)")
	return nil
}

// storeCompare adds the store-direction reconciliation when the bound server
// answers. Every skip states its reason — a report that silently omits the
// store arm would read as "compared and clean".
func storeCompare(ops []rdd.Op) {
	fmt.Println("\n== Store direction ==")
	env, err := factoryEnvLoad()
	if err != nil {
		fmt.Printf("  skipped: no usable binding (%v)\n", err)
		return
	}
	compare := func(label, apiPath, key, opType, kindFilter string) {
		items, err := fetchList(env, apiPath, key)
		if err != nil {
			fmt.Printf("  %s: skipped (%v)\n", label, err)
			return
		}
		store := map[string]bool{}
		for _, it := range items {
			if mm, ok := it.(map[string]any); ok {
				if id, _ := mm["external_id"].(string); id != "" {
					store[id] = true
				}
			}
		}
		emitted := map[string]bool{}
		for _, op := range ops {
			if op.Type != opType {
				continue
			}
			if kindFilter != "" {
				if kind, _ := op.Payload["kind"].(string); kind != kindFilter {
					continue
				}
			}
			if id, _ := op.Payload["external_id"].(string); id != "" {
				emitted[id] = true
			}
		}
		var notOnStore, storeOnly []string
		for id := range emitted {
			if !store[id] {
				notOnStore = append(notOnStore, id)
			}
		}
		for id := range store {
			if !emitted[id] {
				storeOnly = append(storeOnly, id)
			}
		}
		fmt.Printf("  %s: corpus %d vs store %d — %d not yet on store, %d store-only\n",
			label, len(emitted), len(store), len(notOnStore), len(storeOnly))
	}
	compare("requirements", fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID), "requirements", "upsert_requirement", "system")
	compare("epics", fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics", "upsert_epic", "")
	compare("gates", fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates", "upsert_gate", "")
}

// acceptKeyMarker precedes the accept-file key on every line that names a
// blocking loss. The key is printed unwrapped and last so the reader's move is
// a copy rather than a transcription: a key retyped from a line that reads
// "RecordID | Field | detail…" is one space away from matching nothing, and a
// key that matches nothing fails as "still blocking", which reads like the
// acceptance was refused rather than mistyped.
const acceptKeyMarker = "   accept-key: "

func loadAcceptedResidue(path string) (map[string]bool, error) {
	if path == "" {
		return nil, nil
	}
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("accepted-residue file: %w", err)
	}
	accepted := map[string]bool{}
	for _, line := range strings.Split(string(content), "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		accepted[t] = true
	}
	return accepted, nil
}

// authorActor names the session agent on every authoring write —
// intentional single-record authoring is always attributed (REQ-CROSS-228).
var authorActor = map[string]any{"kind": "agent", "agent_slug": "modernpath-author"}

// authorPost sends one authoring call and surfaces a refusal verbatim: the
// server's legality message is the useful part, never paraphrased.
func authorPost(env *factoryEnv, body map[string]any) (map[string]any, error) {
	body["system_id"] = env.SystemID
	body["actor"] = authorActor
	status, resp, err := env.call("POST", "/api/v1/sync/author", body)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		if errBody, ok := resp["error"].(map[string]any); ok {
			if details := errBody["details"]; details != nil {
				return nil, fmt.Errorf("author refused (server %d): %v", status, details)
			}
			if reason := errBody["reason"]; reason != nil {
				return nil, fmt.Errorf("author refused (server %d): %v", status, reason)
			}
		}
		return nil, fmt.Errorf("server %d: %v", status, resp["error"])
	}
	return dataOf(resp), nil
}

// authorCreate posts one intentional, actor-attributed creation
// (REQ-CROSS-228 §228.1/.2: born only in entry-side states, server-enforced).
func authorCreate(env *factoryEnv, kind, externalID string, fields map[string]any) error {
	record := map[string]any{"kind": kind, "external_id": externalID}
	for k, v := range fields {
		record[k] = v
	}
	data, err := authorPost(env, map[string]any{"action": "create", "record": record})
	if err != nil {
		return err
	}
	if row, ok := data[kind].(map[string]any); ok {
		printSuccess("authored %s %s (%s)", kind, str(row, "external_id"),
			firstNonEmpty(str(row, "work_status"), str(row, "process_status"), str(row, "state")))
		// A gate's content-shadow fingerprint is its reference identity:
		// advance and the store-backed declaration verify against it, and it
		// is not recomputable client-side. GET /sync/gates also serves it.
		if fp := str(row, "fingerprint"); fp != "" {
			printInfo("fingerprint: %s (pass as --gate-fingerprint when referencing this gate)", fp)
		}
	}
	return nil
}

// authorTrace records one immutable machine-evaluated trace gate. Its
// fingerprint is the packet/code identity evaluated, not a human gate's
// content-shadow reference identity.
func authorTrace(env *factoryEnv, externalID string, fields map[string]any) error {
	record := map[string]any{"kind": "gate", "external_id": externalID}
	for k, v := range fields {
		record[k] = v
	}
	data, err := authorPost(env, map[string]any{"action": "evaluate_trace", "record": record})
	if err != nil {
		return err
	}
	if row, ok := data["gate"].(map[string]any); ok {
		printSuccess("authored trace %s (%s at %s)", str(row, "external_id"), str(row, "state"), str(row, "fingerprint"))
	}
	return nil
}

// authorAdvance posts one legality-checked status advance (REQ-CROSS-228
// §228.3-.6): automatic transitions flow; human-gated transitions carry the
// ANSWERED gate reference (or the USER: decision for DEFERRED); the expected
// current state makes concurrent advances conflict instead of overwrite.
func authorAdvance(env *factoryEnv, kind, externalID, to, expected, gateRef, gateFingerprint, gateAnswer, decision string) error {
	body := map[string]any{
		"action":   "advance",
		"record":   map[string]any{"kind": kind, "external_id": externalID},
		"to":       to,
		"expected": expected,
	}
	if gateRef != "" {
		body["gate_ref"] = gateRef
	}
	if gateFingerprint != "" {
		body["gate_fingerprint"] = gateFingerprint
	}
	if gateAnswer != "" {
		body["gate_answer"] = gateAnswer
	}
	if decision != "" {
		body["decision"] = decision
	}
	data, err := authorPost(env, body)
	if err != nil {
		return err
	}
	if row, ok := data[kind].(map[string]any); ok {
		printSuccess("advanced %s %s → %s", kind, str(row, "external_id"),
			firstNonEmpty(str(row, "work_status"), str(row, "process_status")))
		// REQ-CROSS-258: the server says on what basis the edge was legal — the
		// automatic set, an attributable decision, a gate bound to this exact
		// transition, or the weaker naming-and-echo check an imported gate falls
		// back to. Swallowing it would hide the one distinction the server took
		// care to make: the operator could not tell a transition-bound gate from
		// one of the 414 imported gates that carry no transition at all.
		if basis := str(data, "transition_basis"); basis != "" {
			printInfo("transition basis: %s\n", basis)
		}
	}
	return nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return "—"
}

// retiredPathFamilies is the exact list §226.1 freezes at the flip — the
// tracked process-state files whose authority moves to the server store.
//
// One definition, shared with the fidelity report: the report is what proves
// this population has a carrier before the flip deletes it, so a second copy
// here would let the proof and the retirement cover different files.
var retiredPathFamilies = rdd.RetiredPathFamilies

// migrateFlip prepares the authority flip (REQ-CROSS-225 machinery). The
// flip COMMIT is completion-gate material (§225.5): this tool refuses
// without an attributable source, re-verifies through the import run
// (§225.1 — a corpus that no longer round-trips reopens the import, never
// the flip), activates the store-backed declaration with the source
// (§227.1), and freezes the retired list in the tracked marker the guard
// and modernpath check read. It deletes nothing: the retirement itself is
// the reviewable flip commit a human session makes on the gate's answer.
func migrateFlip(env *factoryEnv, gateRef, gateFingerprint, gateAnswer string) error {
	if gateRef == "" || gateFingerprint == "" {
		return fmt.Errorf("migrate flip: the flip is applied only from a human completion-gate answer — pass --gate <ANSWERED gate id> --gate-fingerprint <its current identity>")
	}
	// REQ-CROSS-259: the declaration rides the answer ACTUALLY given, echoed by
	// the caller exactly as the advance path already requires. The server
	// refuses without it, so refusing here saves a full re-verification import
	// on a call that could never land — and says which flag is missing.
	if gateAnswer == "" {
		return fmt.Errorf("migrate flip: the declaration rides the answer the gate actually carries — pass --gate-answer <the gate's stored answer, verbatim>. Read it back from the gate (working-set pull, or the gates read surface at state=all); a declaration may only ride the answer actually given")
	}

	// A previous flip attempt may have activated server-side and then failed
	// at the marker write. Activation closes the bulk sync channel, so the
	// re-verification import can no longer run for that system — read the
	// declaration first instead of deadlocking on the flip's own success.
	alreadyActive := false
	if status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/store-backed?system_id=%d", env.SystemID), nil); err == nil && status == 200 {
		if ps, ok := dataOf(body)["process_store"].(map[string]any); ok && ps["state"] == "active" {
			alreadyActive = true
		}
	}

	if alreadyActive {
		// REQ-CROSS-256: an active declaration closes the WRITE channels for
		// this system — the batch, the evidence post, the demotion to seeded.
		// It closes no read. So the retry runs the read-only half of the
		// import's verification rather than skipping verification altogether:
		// the fidelity gate, the duplicate-id refusal and the full
		// id-and-field readback, all GETs and local computation.
		//
		// One blind spot is disclosed rather than hidden: on a retry the store
		// legitimately holds every imported gate answered, so answer-block text
		// drift is invisible to the field comparison by construction. The retry
		// proves identity, structure and non-answer content.
		printWarning("store-backed declaration is already active server-side (a previous flip attempt) — the write channels are closed for this system, so the flip re-verifies READ-ONLY: fidelity, duplicate ids and the full id-and-field readback. An already-answered gate's answer block cannot be compared on this path (the store owns it), so this retry proves identity, structure and non-answer content.")
		if err := migrateReverify(env); err != nil {
			return fmt.Errorf("migrate flip refused — the active declaration does not re-verify against the current corpus: %w.\nNothing was written: the retirement marker is untouched and the declaration is unchanged. The only legal exit from an active declaration is to clear it on an ANSWERED gate (state \"cleared\" with that gate's ref and fingerprint); re-import and retry the flip after that", err)
		}
	} else {
		// §225.1: the verification must be current at this exact corpus. The
		// cheapest current proof is running it: the import is idempotent.
		if err := migrateRun(env); err != nil {
			return fmt.Errorf("migrate flip refused — the import verification is not current: %w", err)
		}
	}

	// §227.1: activation rides the exact ANSWERED completion gate — the
	// recorded source tag comes from the gate, never from this call
	// (external review: a bare string is fabricable).
	//
	// corpus_revision is the audit trail (REQ-CROSS-256): the declaration
	// records which tree was verified into it, so a later reader is not left
	// inferring it from an evidence run's external id. Content
	// re-verification, not this value, is what gates a retry.
	activationRevision := gitOut(env.Root, "rev-parse", "HEAD")
	if activationRevision == "" {
		activationRevision = "no-git"
	}
	status, body, err := env.call("POST", "/api/v1/sync/store-backed", map[string]any{
		"system_id":        env.SystemID,
		"state":            "active",
		"gate_ref":         gateRef,
		"gate_fingerprint": gateFingerprint,
		// REQ-CROSS-259: the stored answer, echoed. Identity and fingerprint
		// currency were the whole check before, so any one of the corpus's 414
		// answered gates could move store authority; the server now also demands
		// a human, approving, flip-purposed, system-scoped gate and this echo.
		"gate_answer":     gateAnswer,
		"corpus_revision": activationRevision,
		// The recorded actor is the caller's authenticated identity, taken
		// server-side; this states the kind and claims no identity of its own.
		"actor": map[string]any{"kind": "human"},
	})
	if err != nil {
		return err
	}
	if status != 200 {
		// The server names the predicate that failed — wrong class, wrong kind,
		// wrong or absent purpose, a scope that does not name this system, a
		// refusing option, a stale fingerprint, a wrong echo. Surfaced verbatim
		// and followed by what a declaration gate has to be: "activation
		// refused" alone leaves the operator guessing at the predicate set.
		return fmt.Errorf("store-backed activation refused (server %d): %v — a declaration gate is human-class, an approval_request, purposed store_backed_activation, scoped to system:%d, and answered with the approving option; --gate-answer must echo its stored answer and --gate-fingerprint its current identity",
			status, migrateRefusalDetail(body), env.SystemID)
	}

	// The tracked marker: the frozen retired list + the acceptance source.
	var b strings.Builder
	b.WriteString("# Store-backed declaration\n\n")
	b.WriteString("This workspace's process store is the server. The files below are\n")
	b.WriteString("retired: read state via `modernpath working-set pull` and `your-move`,\n")
	b.WriteString("write via `modernpath author`. The dual-authority guard\n")
	b.WriteString("(scripts/check-store-backed.sh) gates on this list.\n\n")
	acceptedTag, _ := dataOf(body)["source_tag"].(string)
	fmt.Fprintf(&b, "- **Accepted:** %s (gate %s)\n", acceptedTag, gateRef)
	fmt.Fprintf(&b, "- **Server:** %s · system %d\n\n", env.APIURL, env.SystemID)
	for _, p := range retiredPathFamilies {
		fmt.Fprintf(&b, "retired: %s\n", p)
	}
	markerPath := filepath.Join(env.Root, "process", "store-backed.md")
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(markerPath, []byte(b.String()), 0o644); err != nil {
		return err
	}
	printSuccess("store-backed declaration active; marker written: process/store-backed.md")
	fmt.Println("\nThe flip commit is yours to make on the completion gate's answer:")
	fmt.Println("  git rm -r " + strings.Join(retiredPathFamilies, " "))
	fmt.Println("  git add process/store-backed.md && commit — one reviewable flip commit.")
	return nil
}

// migrateRefusalDetail pulls the server's own reason out of an error envelope.
// A declaration refusal names the predicate that failed under `details`; a
// fingerprint-currency or echo refusal names it under `reason`. Printing the
// raw map instead buries the one sentence the operator needs inside Go's map
// syntax.
func migrateRefusalDetail(body map[string]any) any {
	errBody, ok := body["error"].(map[string]any)
	if !ok {
		return body["error"]
	}
	if details := errBody["details"]; details != nil {
		return details
	}
	if reason := errBody["reason"]; reason != nil {
		return reason
	}
	return errBody
}

// migrateReverify runs the import's verification against the current corpus
// WITHOUT writing anything (REQ-CROSS-256). It is what a flip retry runs once
// the store-backed declaration is already active: the server correctly refuses
// the batch channel, the evidence channel and a demotion to seeded for such a
// system, so the honest verification left is the read-only set — the fidelity
// gate, the duplicate-id refusal, and the full id-and-field readback, which is
// REQ-CROSS-255's verifier.
//
// Every call here is a GET or local computation. Nothing it does is refused by
// an active declaration, and nothing it does changes the store.
func migrateReverify(env *factoryEnv) error {
	ops, warnings, err := env.workspaceOps()
	if err != nil {
		return err
	}
	for _, w := range warnings {
		printWarning("%s", w)
	}
	if dups := duplicateOpIDs(ops); len(dups) > 0 {
		return fmt.Errorf("%d duplicate id(s) in the op stream — the corpus does not resolve to one record per id: %s",
			len(dups), strings.Join(capList(dups, 20), ", "))
	}
	report, _, _, err := migrateFidelity(env.Root)
	if err != nil {
		return err
	}
	accepted, err := loadAcceptedResidue(migrateRunAcceptPath)
	if err != nil {
		return err
	}
	if blocking := report.Blocking(accepted); len(blocking) > 0 {
		named := make([]string, 0, len(blocking))
		for _, l := range blocking {
			named = append(named, fmt.Sprintf("%s | %s | %s", l.RecordID, l.Field, l.Detail))
		}
		sort.Strings(named)
		return fmt.Errorf("%d blocking fidelity loss(es) the store cannot be holding: %s",
			len(blocking), strings.Join(capList(named, 20), "; "))
	}
	// The retry writes nothing, so what the store holds answered now is exactly
	// what it held answered before this verification — the same snapshot the
	// import takes before its first batch.
	missing, storeOnly, err := migrateVerifyIDs(env, ops, migrateGateStatesBeforeRun(env))
	if err != nil {
		return err
	}
	if len(storeOnly) > 0 {
		printInfo("store-born records (kept, not corpus): %d\n", len(storeOnly))
	}
	if len(missing) > 0 {
		return fmt.Errorf("%d corpus record(s) did not read back from the store: %s",
			len(missing), strings.Join(capList(missing, 20), ", "))
	}
	printSuccess("re-verified read-only: every emitted id reads back")
	return nil
}

// storeBackedWorkspace reports whether the workspace carries the flip's
// tracked declaration marker (REQ-CROSS-225 §225.4): an absent ledger is
// then the declared configuration, never an error.
func storeBackedWorkspace(root string) bool {
	_, err := os.Stat(filepath.Join(root, "process", "store-backed.md"))
	return err == nil
}
