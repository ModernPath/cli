package cmd

// REQ-CROSS-224 — focused lower evidence, clause-mapped to
// epics/EPIC-CLI-003-ledger-import/specs/requirements.md §224.
// RED first: the import run does not exist.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/rdd"
)

// migrateStore is an in-memory store the fixture server applies batches to —
// hash-diffed like the real one, so idempotence is observable.
type migrateStore struct {
	mu           sync.Mutex
	hashes       map[string]string         // "type|id" → content_hash
	payloads     map[string]map[string]any // "type|id" → last applied payload
	applied      []map[string]int          // per-batch result counts
	evidenceRuns []map[string]any
	declarations []map[string]any // POST /sync/store-backed bodies (REQ-CROSS-227)
	// declaredState is what GET /sync/store-backed serves as the declaration's
	// current state. The handler used to answer every method with the POST's
	// echo and no process_store key at all, so `alreadyActive` was never true
	// and the flip's retry branch had no test reaching it.
	declaredState string
	dropServe     string // an id the read surface withholds (verification arm)
	conflictID    string // an id the batch refuses as a conflict
	mangleTitle   string // an id the read surface serves with a wrong title (spot-check arm)
	systemID      any    // the system the last batch declared (document rows are system-scoped)
	// dropField makes a read surface omit one field of one record, so a field
	// nothing serves reads as unverified rather than as verified.
	dropField [2]string // {external_id, field}
	// mangleField makes a read surface serve one field of one record with a
	// changed value — the full-field arm, for the types the old four-field
	// spot check never looked at.
	mangleField [3]string // {external_id, field, served value}
	// mangleDocRevision makes the document surface serve a sync revision that
	// does not match the content hash the batch sent.
	mangleDocRevision string
	// mangleArchiveSha makes the archive surface serve a content_sha256 that
	// is not the sha of the bytes that were sent (REQ-CROSS-253 verification).
	mangleArchiveSha string
	// extraArchiveGeneration makes the archive surface serve one file's older
	// generation alongside its current one, which is what a real archive holds
	// once that file has changed: several rows under one external_id.
	extraArchiveGeneration string
	// extraProductTask makes the tasks surface serve one task the PRODUCT
	// made (Board/MCP), unstamped — the reverse direction must not report it
	// as a corpus record gone missing, and --from-empty must not refuse on it.
	extraProductTask bool
	// noDocumentSurface removes the document read HANDLER entirely — the
	// deployment that serves none of that surface at all, which answers 404.
	noDocumentSurface bool
	// offlineSurfaces takes named read surfaces off the air by their served
	// key ("requirements", "epics", "gates", "backlog", "scenarios",
	// "process_records", "documents", "evidence"): the handler answers 500
	// instead of a list. Several may be offline at once, which is what forces
	// the verifier to accumulate rather than return on the first one it cannot
	// reach.
	offlineSurfaces map[string]bool
	// keepFirstScenarioPosition makes the epics surface serve position on the
	// FIRST scenario item and on no other — partial, not systematic, so the
	// disclosure rule must read it as a loss.
	keepFirstScenarioPosition string // external_id of the epic
	// mangleFirstScenarioText changes the served text of one epic's first
	// scenario: a nested value genuinely dropped, which no disclosure excuses.
	mangleFirstScenarioText string // external_id of the epic
	// foreignWriterID stands in for a second client: after every batch this
	// id's stored hash is reset, so it changes on pass after pass — the
	// ping-pong two writers produce, never the settling one writer produces.
	foreignWriterID string
	// staleOnceID changes on the FIRST rerun and then settles: a record that
	// had not landed, which must not be blamed on a second writer.
	staleOnceID string
	stalePasses int
	// lockProbeRoot makes the batch handler try to take the workspace sync
	// lock, recording whether the import was holding it at that moment.
	lockProbeRoot          string
	lockWasHeldDuringBatch bool
	// serveNull makes a read surface serve one field as JSON null — the shape a
	// column Ecto never wrote comes back as. An empty payload string is cast
	// away before it reaches the column, so this is what "" round-trips into.
	serveNull [2]string // {external_id, field}
	// serveGateOpen makes the gate surface serve one gate as still OPEN, so its
	// answer block is comparable rather than frozen server-side.
	serveGateOpen string
	// preAnsweredGates are gates the store ALREADY held answered before this
	// run — an earlier import's rows. They are what the server's answered-gate
	// freeze legitimately owns; a gate the run itself writes is not.
	preAnsweredGates []string
	// evidenceReads counts GETs of the imported-evidence surface, so a
	// readback arm can prove it read rather than assumed.
	evidenceReads int
	// mangleEvidenceRaw makes the imported-evidence surface serve one result's
	// raw text changed: {run external_id, replacement raw text}.
	mangleEvidenceRaw [2]string
	// importingAccountID is the account the store attributes an unresolvable
	// approver to — the caller's own identity. The declared name is not
	// persisted when resolution fails, so what serves back for such a row is
	// the importing account's name, never the corpus's.
	importingAccountID any
	// epicApprovalFallback is the epic whose served approval carries the
	// importing-account fallback basis instead of a resolved human.
	epicApprovalFallback string
	// mangleApprovalName serves a RESOLVED approval whose approver name is not
	// the one that was sent — a real loss the fallback exemption must not cover.
	mangleApprovalName string
	// gateAnswererIDs is what the gate surface serves as answerer_user_id per
	// gate external_id. A gate with no entry serves nothing — no declared name.
	gateAnswererIDs map[string]any
	// declarationGates are the gates the store holds for the store-backed
	// declaration's predicates (REQ-CROSS-259), keyed by external_id. The POST
	// handler checks them the way the server does, so a flip that does not
	// satisfy a predicate is refused here too — a fixture that activates on any
	// gate proves the flip works against a server that no longer exists.
	declarationGates map[string]*declarationGate
}

// declarationGate mirrors the columns Core.Author's declaration predicates
// read. Field names match the server's error keys so a refusal reads the same.
type declarationGate struct {
	gateClass        string   // "human" | "trace"
	kind             string   // "approval_request" | "question" | …
	purpose          string   // store_backed_activation | store_backed_clearing
	exactScope       []string // must contain "system:<id>"
	chosenOptionKeys []string // must contain "approve"
	answer           string   // the stored answer the caller must echo
	fingerprint      string   // the current content-shadow hash
	sourceTag        string   // what the declaration records, taken from here
}

// satisfyingDeclarationGate is the genuine flip gate: human, approving,
// purposed for this direction, scoped to this system, answered with the
// approving option key.
func satisfyingDeclarationGate(systemID int, purpose, fingerprint string) *declarationGate {
	return &declarationGate{
		gateClass:        "human",
		kind:             "approval_request",
		purpose:          purpose,
		exactScope:       []string{fmt.Sprintf("system:%d", systemID)},
		chosenOptionKeys: []string{"approve"},
		answer:           "approved",
		fingerprint:      fingerprint,
		sourceTag:        "USER:fixture-gate-answer",
	}
}

// verifyDeclarationGate applies the server's predicates in the server's order,
// returning the HTTP status and the error body it would answer with.
func (st *migrateStore) verifyDeclarationGate(state string, body map[string]any) (int, map[string]any) {
	want := map[string]string{
		"active":  "store_backed_activation",
		"cleared": "store_backed_clearing",
	}[state]
	legality := func(key, detail string) (int, map[string]any) {
		return http.StatusUnprocessableEntity,
			map[string]any{"error": map[string]any{"details": map[string]any{key: []string{detail}}}}
	}
	conflict := func(reason string) (int, map[string]any) {
		return http.StatusConflict, map[string]any{"error": map[string]any{"reason": reason}}
	}

	ref, _ := body["gate_ref"].(string)
	if ref == "" {
		return legality("gate_ref", "an ANSWERED gate reference is required")
	}
	gate := st.declarationGates[ref]
	if gate == nil {
		return legality("gate_ref", "no such gate")
	}
	if fp, _ := body["gate_fingerprint"].(string); fp != gate.fingerprint {
		return conflict("the gate's content has moved since that fingerprint — re-read it before applying its answer")
	}
	if gate.gateClass != "human" {
		return legality("gate_class", "the gate is "+gate.gateClass+" — store authority moves only on a human decision")
	}
	if gate.kind != "approval_request" {
		return legality("gate_kind", "the gate is "+gate.kind+", not an approval_request — a question's answer approves nothing")
	}
	if gate.purpose != want {
		return legality("purpose", "the gate's purpose is "+gate.purpose+"; declaring "+state+" needs "+want)
	}
	scopeToken := fmt.Sprint("system:", jsonInt(body["system_id"]))
	if !containsString(gate.exactScope, scopeToken) {
		return legality("exact_scope", "the gate's exact scope does not name "+scopeToken+" — an approval for one server never moves another")
	}
	if !containsString(gate.chosenOptionKeys, "approve") {
		return legality("chosen_option_keys", "the gate was not answered with the approve option")
	}
	if echo, _ := body["gate_answer"].(string); echo != gate.answer {
		return conflict("the gate's stored answer is " + gate.answer +
			" — echo it as gate_answer to apply it; a declaration may only ride the answer actually given")
	}
	return 0, nil
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// serveScenarios renders scenario items the way the epics read surface does:
// external_id, kind and the Given/When/Then or statement text, with the
// absent halves served as explicit nulls — and NO position. The store orders
// acceptance criteria itself and serves them in order; the ordinal the batch
// sent is not a column any read surface has.
func (st *migrateStore) serveScenarios(epicID string, items []any) []any {
	out := make([]any, 0, len(items))
	for i, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		row := map[string]any{"external_id": m["external_id"], "kind": m["kind"]}
		for _, k := range []string{"statement", "given", "when", "then"} {
			row[k] = m[k] // nil where the payload sent nothing, as the API does
		}
		if i == 0 && epicID == st.keepFirstScenarioPosition {
			row["position"] = m["position"]
		}
		if i == 0 && epicID == st.mangleFirstScenarioText {
			for _, k := range []string{"statement", "given", "when", "then"} {
				if row[k] != nil {
					row[k] = "MANGLED"
				}
			}
		}
		out = append(out, row)
	}
	return out
}

// serveSpecs renders spec items the way the epics read surface does: metadata
// only. The content the batch sent is stored in planning_artifacts and served
// back by nothing — the one payload field of this import that no read surface
// can confirm.
func (st *migrateStore) serveSpecs(items []any) []any {
	out := make([]any, 0, len(items))
	for _, it := range items {
		m, ok := it.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"external_id":   m["external_id"],
			"name":          m["name"],
			"artifact_type": "other",
			"status":        "draft",
			"version":       1.0,
		})
	}
	return out
}

func serveMigrateStore(t *testing.T, st *migrateStore) *httptest.Server {
	t.Helper()
	if st.importingAccountID == nil {
		st.importingAccountID = 7.0
	}
	// Rows an EARLIER import left behind: the store holds them answered before
	// this run posts anything, which is the only state the server's
	// answered-gate freeze legitimately owns.
	for _, id := range st.preAnsweredGates {
		key := "upsert_gate|" + id
		st.hashes[key] = "a-hash-an-earlier-import-left"
		if st.payloads == nil {
			st.payloads = map[string]map[string]any{}
		}
		st.payloads[key] = map[string]any{"external_id": id, "state": "answered"}
	}
	mux := http.NewServeMux()

	respond := func(w http.ResponseWriter, data map[string]any) {
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}

	mux.HandleFunc("/api/v1/sync/batch", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		ops, _ := body["ops"].([]any)
		st.mu.Lock()
		defer st.mu.Unlock()
		st.systemID = body["system_id"]
		counts := map[string]int{}
		var results []any
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			payload, _ := op["payload"].(map[string]any)
			id, _ := payload["external_id"].(string)
			typ, _ := op["type"].(string)
			hash, _ := payload["content_hash"].(string)
			key := typ + "|" + id
			result := "created"
			switch {
			case id == st.conflictID:
				result = "conflict"
			case st.hashes[key] == hash && hash != "":
				result = "unchanged"
			case st.hashes[key] != "":
				result = "updated"
			}
			if result != "conflict" {
				st.hashes[key] = hash
				if st.payloads == nil {
					st.payloads = map[string]map[string]any{}
				}
				st.payloads[key] = payload
			}
			counts[result]++
			results = append(results, map[string]any{"external_id": id, "result": result})
		}
		st.applied = append(st.applied, counts)
		// A second client, standing in for a hook-triggered sync from a
		// different binary: it leaves its own hash behind, so the next pass
		// finds the id changed again.
		if st.foreignWriterID != "" {
			for k := range st.hashes {
				if strings.HasSuffix(k, "|"+st.foreignWriterID) {
					st.hashes[k] = "a-hash-this-client-never-sent"
				}
			}
		}
		// A record that simply had not landed: one rerun writes it, and the
		// pass after that finds nothing to do.
		if st.staleOnceID != "" {
			st.stalePasses++
			if st.stalePasses == 1 {
				for k := range st.hashes {
					if strings.HasSuffix(k, "|"+st.staleOnceID) {
						st.hashes[k] = "not-yet-landed"
					}
				}
			}
		}
		if st.lockProbeRoot != "" {
			// What a hook would find if it fired right now.
			if release := acquireSyncLock(st.lockProbeRoot); release != nil {
				release()
			} else {
				st.lockWasHeldDuringBatch = true
			}
		}
		respond(w, map[string]any{"results": results})
	})

	// serveRow renders one stored payload the way a read surface does: the
	// content it was given back, minus the sync envelope. Serving the payload
	// rather than a hand-written stub is what lets the full-field comparison be
	// exercised at all — a stub that carries three fields can only ever verify
	// three fields, whatever the code under test compares.
	serveRow := func(key string) map[string]any {
		id := strings.SplitN(key, "|", 2)[1]
		row := map[string]any{"external_id": id}
		for f, v := range st.payloads[key] {
			if f == "content_hash" || f == "actor" {
				continue
			}
			row[f] = v
		}
		// The read surface serves nested items in its own, THINNER shapes —
		// echoing the payload back would verify a store no server implements,
		// and would hide exactly the fields that are in fact unverifiable.
		if scns, ok := row["scenarios"].([]any); ok {
			row["scenarios"] = st.serveScenarios(id, scns)
		}
		if specs, ok := row["specs"].([]any); ok {
			row["specs"] = st.serveSpecs(specs)
		}
		if id == st.mangleTitle {
			row["title"] = "MANGLED"
		}
		if st.dropField[0] == id {
			delete(row, st.dropField[1])
		}
		if st.mangleField[0] == id {
			row[st.mangleField[1]] = st.mangleField[2]
		}
		if st.serveNull[0] == id {
			row[st.serveNull[1]] = nil
		}
		if id == st.serveGateOpen {
			row["state"] = "open"
		}
		// REQ-CROSS-262: the epic's approval serves as a nested object mirroring
		// the payload, plus the three resolution keys the payload never sends.
		// `attribution_basis` is the honest half — an unresolvable declared name
		// keeps the importing account, and serving that as a resolution would
		// make 169 of 177 corpus approvals claim one that never happened.
		if approval, ok := row["approval"].(map[string]any); ok {
			served := map[string]any{}
			for k, v := range approval {
				served[k] = v
			}
			if id == st.epicApprovalFallback {
				served["attribution_basis"] = "importing_account_fallback"
				served["approved_by_id"] = st.importingAccountID
				served["approver_name"] = "The Importing Account"
				served["approver_email"] = "import@example.invalid"
			} else {
				served["attribution_basis"] = "named_user"
				served["approved_by_id"] = 42.0
				served["approver_email"] = "named@example.invalid"
				if id == st.mangleApprovalName {
					served["approver_name"] = "Somebody Else Entirely"
				}
			}
			row["approval"] = served
		}
		// The gate surface serves the answerer's user id; there is no gate-side
		// attribution basis, so the client reconstructs the classes itself.
		if v, ok := st.gateAnswererIDs[id]; ok {
			row["answerer_user_id"] = v
		}
		return row
	}

	// offline answers for a read surface the deployment serves but cannot
	// answer right now — the transport/500 class, as opposed to the surface
	// that does not exist at all (noDocumentSurface, a bare 404).
	offline := func(w http.ResponseWriter, key string) bool {
		if !st.offlineSurfaces[key] {
			return false
		}
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]any{"error": "read surface " + key + " is offline"})
		return true
	}

	list := func(prefix, key string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if offline(w, key) {
				return
			}
			st.mu.Lock()
			defer st.mu.Unlock()
			var items []any
			for k := range st.hashes {
				parts := strings.SplitN(k, "|", 2)
				if parts[0] != prefix || parts[1] == st.dropServe {
					continue
				}
				items = append(items, serveRow(k))
			}
			respond(w, map[string]any{key: items})
		}
	}
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		if offline(w, "requirements") {
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		items := map[string][]any{"requirements": {}, "user_requirements": {}}
		for k := range st.hashes {
			parts := strings.SplitN(k, "|", 2)
			if parts[0] != "upsert_requirement" || parts[1] == st.dropServe {
				continue
			}
			key := "requirements"
			if strings.HasPrefix(parts[1], "UR-") {
				key = "user_requirements"
			}
			items[key] = append(items[key], serveRow(k))
		}
		respond(w, map[string]any{"requirements": items["requirements"], "user_requirements": items["user_requirements"]})
	})
	// The document read surface: the knowledge library index, tenant-wide, so
	// rows carry system_id and the sync protocol hash the store stamped.
	if !st.noDocumentSurface {
		mux.HandleFunc("/api/v1/knw/documents", func(w http.ResponseWriter, r *http.Request) {
			if offline(w, "documents") {
				return
			}
			st.mu.Lock()
			defer st.mu.Unlock()
			var items []any
			for k, hash := range st.hashes {
				parts := strings.SplitN(k, "|", 2)
				if parts[0] != "upsert_document" || parts[1] == st.dropServe {
					continue
				}
				rev := "sync:" + hash
				if parts[1] == st.mangleDocRevision {
					rev = "sync:not-the-hash-that-was-sent"
				}
				row := serveRow(k)
				delete(row, "content_md") // the index serves no content, only its identity
				row["system_id"] = st.systemID
				row["source_revision"] = rev
				items = append(items, row)
			}
			// A document born of analysis, not of this import: the reverse
			// direction must not report it as a corpus record gone missing.
			items = append(items, map[string]any{
				"external_id": "analysis-born-document", "system_id": st.systemID,
				"source_revision": "analysis:2026-08-22",
			})
			respond(w, map[string]any{"documents": items})
		})
	}
	mux.HandleFunc("/api/v1/sync/epics", list("upsert_epic", "epics"))
	// REQ-CROSS-264: the tasks surface serves only sync-born rows, which is
	// what makes the from-empty precondition and the readback agree.
	mux.HandleFunc("/api/v1/sync/tasks", func(w http.ResponseWriter, r *http.Request) {
		if offline(w, "tasks") {
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		var items []any
		for k := range st.hashes {
			parts := strings.SplitN(k, "|", 2)
			if parts[0] != "upsert_task" || parts[1] == st.dropServe {
				continue
			}
			items = append(items, serveRow(k))
		}
		if st.extraProductTask {
			items = append(items, map[string]any{
				"external_id": "BOARD-1", "code": "BOARD-1",
				"title": "dragged onto the board by a human", "sync_born": false,
			})
		}
		respond(w, map[string]any{"tasks": items})
	})
	mux.HandleFunc("/api/v1/sync/scenarios", list("upsert_scenario", "scenarios"))
	// The archive surface: identity + sha, never the bytes.
	mux.HandleFunc("/api/v1/sync/process-records", func(w http.ResponseWriter, r *http.Request) {
		if offline(w, "process_records") {
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		var items []any
		for k := range st.hashes {
			parts := strings.SplitN(k, "|", 2)
			if parts[0] != "upsert_process_record" || parts[1] == st.dropServe {
				continue
			}
			row := serveRow(k)
			delete(row, "raw_content")
			if parts[1] == st.mangleArchiveSha {
				row["content_sha256"] = "not-the-sha-of-the-bytes-sent"
			}
			items = append(items, row)
			if parts[1] == st.extraArchiveGeneration {
				old := map[string]any{}
				for k, v := range row {
					old[k] = v
				}
				old["content_sha256"] = "sha-of-an-earlier-generation"
				old["size_bytes"] = 1
				old["archive_revision"] = "an-earlier-revision"
				items = append(items, old)
			}
		}
		respond(w, map[string]any{"process_records": items})
	})
	mux.HandleFunc("/api/v1/sync/gates", list("upsert_gate", "gates"))
	mux.HandleFunc("/api/v1/sync/backlog", list("upsert_backlog_record", "backlog"))
	mux.HandleFunc("/api/v1/sync/evidence/runs", func(w http.ResponseWriter, r *http.Request) {
		// The imported-evidence READ surface: runs and their results, every
		// validity including skip, with the raw text and the line token. The
		// existing latest-state surface projects a collapsed current-only
		// pass/fail per target, which excludes this whole population. Path and
		// envelope mirror the server's GET /api/v1/sync/evidence/runs.
		if offline(w, "evidence") {
			return
		}
		st.mu.Lock()
		defer st.mu.Unlock()
		st.evidenceReads++
		var runs []any
		for _, posted := range st.evidenceRuns {
			row := map[string]any{
				"external_id": posted["external_id"],
				"kind":        posted["kind"],
				"ran_at":      posted["ran_at"],
				"status":      posted["status"],
			}
			results, _ := posted["results"].([]any)
			served := make([]any, 0, len(results))
			for _, raw := range results {
				res, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				out := map[string]any{}
				for k, v := range res {
					out[k] = v
				}
				if posted["external_id"] == st.mangleEvidenceRaw[0] && len(served) == 0 {
					out["raw_evidence"] = st.mangleEvidenceRaw[1]
				}
				served = append(served, out)
			}
			row["results"] = served
			runs = append(runs, row)
		}
		respond(w, map[string]any{"runs": runs})
	})
	mux.HandleFunc("/api/v1/sync/evidence", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// The store refuses two results of one run that share an extended
		// identity (target, type, reference, role) — the identity index is
		// NULLS NOT DISTINCT, so a per-occurrence token would hard-refuse the
		// whole post rather than duplicating a row. The fixture refuses the
		// same way, or an importer that collides would look fine here and fail
		// on the first real corpus.
		if results, ok := body["results"].([]any); ok {
			seen := map[string]bool{}
			for _, raw := range results {
				res, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				key := fmt.Sprint(res["target_external_id"], "|", res["target_type"], "|",
					res["test_case_ref"], "|", res["role"])
				if seen[key] {
					w.WriteHeader(http.StatusUnprocessableEntity)
					json.NewEncoder(w).Encode(map[string]any{"error": "duplicate_result_identity: " + key})
					return
				}
				seen[key] = true
			}
		}
		st.mu.Lock()
		st.evidenceRuns = append(st.evidenceRuns, body)
		st.mu.Unlock()
		respond(w, map[string]any{"result": "created"})
	})
	mux.HandleFunc("/api/v1/sync/project", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]any{})
	})
	mux.HandleFunc("/api/v1/sync/store-backed", func(w http.ResponseWriter, r *http.Request) {
		// The read and the write are different acts: the read serves the
		// declaration record the server holds (the shape the flip's retry
		// branch reads), the write records the act. Answering both alike is
		// what left the retry branch unreachable in every test.
		if r.Method == http.MethodGet {
			st.mu.Lock()
			state := st.declaredState
			st.mu.Unlock()
			if state == "" {
				respond(w, map[string]any{})
				return
			}
			respond(w, map[string]any{"process_store": map[string]any{"state": state}})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		st.mu.Lock()
		defer st.mu.Unlock()
		state, _ := body["state"].(string)
		// REQ-CROSS-259: activation and clearing satisfy the declaration
		// predicates or they are refused. Seeding stays ungated — it is the
		// import's own act.
		sourceTag := "USER:fixture-gate-answer"
		if state == "active" || state == "cleared" {
			if status, errBody := st.verifyDeclarationGate(state, body); status != 0 {
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(errBody)
				return
			}
			if ref, _ := body["gate_ref"].(string); st.declarationGates[ref] != nil {
				sourceTag = st.declarationGates[ref].sourceTag
			}
		}
		st.declarations = append(st.declarations, body)
		if state != "" {
			st.declaredState = state
		}
		respond(w, map[string]any{"state": body["state"], "source_tag": sourceTag})
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func migrateCorpus(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tasks/MR-REQUIREMENTS.md", `# REQUIREMENTS — MR

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-MR-001 | First | MVP | PROPOSED | UR-MR-001 | doc-a | — | — |
| REQ-MR-002 | Dead | MVP | OBSOLETE | UR-MR-001 | superseded by REQ-MR-001 | — | — |
`)
	write("WORKLIST.md", `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-MR-001 | [record](epics/EPIC-MR-001-rehearsal/EPIC.md) | UR-MR-001 | SCN-MR-001, SCN-MR-002 | REQ-MR-001 | T1 | — | — | **IN_PROGRESS** — building | — | — |
`)
	// The fixture corpus is fidelity-clean on purpose: the import's pre-write
	// gate refuses on any blocking loss, so a fixture carrying one would prove
	// the gate fires rather than that the run works. The epic record declares
	// the UR the ledger rows reference — an undeclared UR is itself a loss.
	//
	// Folder form, with scenarios and a spec, because those are the two payload
	// keys carrying a LIST OF MAPS — the shape whose served form is thinner
	// than what was sent. A file-form epic with neither exercises the
	// containment rule at scalar depth only, which is where it was already
	// safe. Two scenarios, not one: one served element is not a population, so
	// nothing could tell a systematic absence from a lost value.
	write("epics/EPIC-MR-001-rehearsal/EPIC.md", `# EPIC-MR-001 — Migration rehearsal

## User outcome (UR)
**UR-MR-001** — the corpus reaches the store without losing a record.

## Acceptance scenarios (SCN)

- **SCN-MR-001** — GIVEN a ledger corpus WHEN the import runs THEN every record reaches the store.
- **SCN-MR-002** — GIVEN a store that answers WHEN the import verifies THEN what it cannot check is said out loud.
`)
	write("epics/EPIC-MR-001-rehearsal/specs/requirements.md",
		"# Requirements\n\nThe spec body the batch carries and no read surface serves back.\n")
	write("BACKLOG.md", "# Backlog\n\n| Discovery | Notes | Tracked as |\n|---|---|---|\n| **A find** | n | NEW |\n")
}

// §224.1/.2/.5 — the run applies, is idempotent on immediate rerun, verifies
// bidirectionally, and records its identity durably as an evidence run.
func TestMigrateRunAppliesVerifiesAndRecords(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	if err := migrateRun(env); err != nil {
		t.Fatalf("migrate run failed: %v", err)
	}

	if len(st.applied) < 2 {
		t.Fatalf("the run must apply and then prove idempotence with a second pass, got %d passes", len(st.applied))
	}
	second := st.applied[len(st.applied)-1]
	if second["created"]+second["updated"] != 0 {
		t.Fatalf("immediate rerun changed records — not idempotent: %v", second)
	}
	if len(st.evidenceRuns) != 1 {
		t.Fatalf("the run must record its identity as one evidence run, got %d", len(st.evidenceRuns))
	}
	rec := st.evidenceRuns[0]
	if rec["kind"] != "migration" || rec["sha"] == nil || rec["ran_at"] == nil || rec["totals"] == nil {
		t.Fatalf("run record incomplete: %v", rec)
	}

	// §227.1 — the import run SEEDS the store-backed declaration on the
	// server it imported, so replicas are covered; only the flip activates.
	if len(st.declarations) != 1 || st.declarations[0]["state"] != "seeded" {
		t.Fatalf("the run must seed the store-backed declaration, got %v", st.declarations)
	}
}

// §224.2 — a corpus id the store does not serve back fails the run, naming it.
func TestMigrateRunFailsWhenAStoreRowIsMissing(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, dropServe: "REQ-MR-002"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "REQ-MR-002") {
		t.Fatalf("a missing store row must fail the run naming the id, got: %v", err)
	}
}

// §224.4 — a conflict is a refusal, never silently absorbed: the run fails
// naming the refused id, and nothing is deleted server-side.
func TestMigrateRunFailsOnConflicts(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, conflictID: "REQ-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "REQ-MR-001") {
		t.Fatalf("a conflict must fail the run naming the id, got: %v", err)
	}
}

// §224.2 (external review): verification is round-trip FIDELITY, not id
// presence — a served field that no longer matches the corpus fails the run
// naming the id and field.
func TestMigrateRunFailsOnServedFieldMismatch(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}, mangleTitle: "REQ-MR-001"}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)

	err := migrateRun(env)
	if err == nil || !strings.Contains(err.Error(), "REQ-MR-001") || !strings.Contains(err.Error(), "title") {
		t.Fatalf("a served-field mismatch must fail naming id and field, got: %v", err)
	}
}

// §224.1's precondition, observed live: duplicate ids make idempotence
// unprovable — the run refuses before posting anything.
func TestMigrateRunRefusesDuplicateOpIDs(t *testing.T) {
	ops := []map[string]any{
		{"type": "upsert_epic", "payload": map[string]any{"external_id": "EPIC-D-1", "content_hash": "a"}},
		{"type": "upsert_epic", "payload": map[string]any{"external_id": "EPIC-D-1", "content_hash": "b"}},
	}
	dups := duplicateOpIDs(ops)
	if len(dups) != 1 || dups[0] != "upsert_epic|EPIC-D-1" {
		t.Fatalf("duplicate ids must be named once each, got %v", dups)
	}
}

// migrateCorpusDuplicateIdentity adds a second WORKLIST rollup row claiming an
// epic id that a different record already claims — the corpus shape the
// extractor resolves first-wins, shadowing the second record entirely.
func migrateCorpusDuplicateIdentity(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "WORKLIST.md")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	row := "| EPIC-MR-001 | `epics/EPIC-MR-001-a-second-record.md` | UR-MR-001 | SCN-MR-002 | REQ-MR-002 | T2 | — | — | **DONE** | — | — |\n"
	if err := os.WriteFile(p, append(content, []byte(row)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// migrateCorpusAnsweredApproval gives the fixture epic a recorded approval, so
// the batch carries an approval gate in the ANSWERED state — the shape whose
// answer block the store owns and the repo cannot rewrite.
func migrateCorpusAnsweredApproval(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "epics", "EPIC-MR-001-rehearsal", "EPIC.md")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	block := "\n## Approval\n\n**APPROVED** — the rehearsal is accepted (`USER:2026-08-22`).\n"
	if err := os.WriteFile(p, append(content, []byte(block)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// migrateCorpusNamedApprover gives the fixture epic an approval register whose
// row names the deciding human, so the epic payload carries an
// `approval.approver_name` for the store to resolve — or fail to resolve, which
// is the case REQ-CROSS-262 is about. It also emits a second answered gate, so
// the attribution count group has a population rather than a single row.
func migrateCorpusNamedApprover(t *testing.T, root string) {
	t.Helper()
	p := filepath.Join(root, "epics", "EPIC-MR-001-rehearsal", "EPIC.md")
	content, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	block := "\n## Approval register\n\n" +
		"| ID | Approver | Role | Source | Scope | Decision |\n" +
		"|---|---|---|---|---|---|\n" +
		"| APP-MR-001 | Pasi Approver | sponsor | `USER:2026-08-22` | EPIC-MR-001 | approved |\n"
	if err := os.WriteFile(p, append(content, []byte(block)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

// §224.3 with §221.4: the run must not start writing while the fidelity
// report blocks. A duplicate identity loses a whole record on the way to the
// ops, and the store would carry that silently — so the refusal comes before
// the first batch, names the loss, and points at the report.
func TestMigrateRunRefusesWhileTheFidelityReportBlocks(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusDuplicateIdentity(t, env.Root)

	err := migrateRun(env)
	if err == nil {
		t.Fatal("a blocking fidelity loss must refuse the run")
	}
	for _, want := range []string{"EPIC-MR-001", "duplicate-identity", "migrate report"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the loss and point at the report (missing %q): %v", want, err)
		}
	}
	if len(st.applied) != 0 {
		t.Fatalf("the refusal must come before anything is posted, got %d batch(es)", len(st.applied))
	}
	if len(st.declarations) != 0 || len(st.evidenceRuns) != 0 {
		t.Fatalf("a refused run writes nothing at all: %d declaration(s), %d evidence run(s)",
			len(st.declarations), len(st.evidenceRuns))
	}
}

// The other half of the same rule: residue an applied gate answer accepted is
// not a blocker. A run whose only losses are accepted proceeds and applies.
func TestMigrateRunProceedsWhenEveryLossIsAccepted(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusDuplicateIdentity(t, env.Root)

	data, _ := rdd.Snapshot(env.Root, manifest.Default())
	ops := rdd.BuildOps(data, func(rel string) string { return rdd.ReadEpicRecord(env.Root, rel) }, "2026-08-22")
	report := rdd.BuildFidelityReport(env.Root, manifest.Default(), data, ops)
	var lines []string
	for _, l := range report.Blocking(nil) {
		lines = append(lines, l.Key())
	}
	if len(lines) == 0 {
		t.Fatal("fixture must carry at least one blocking loss for this to mean anything")
	}
	acceptPath := filepath.Join(t.TempDir(), "accepted.txt")
	if err := os.WriteFile(acceptPath, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	migrateRunAcceptPath = acceptPath
	t.Cleanup(func() { migrateRunAcceptPath = "" })

	if err := migrateRun(env); err != nil {
		t.Fatalf("accepted residue must not block the run: %v", err)
	}
	if len(st.applied) < 2 {
		t.Fatalf("the accepted run must still apply and prove idempotence, got %d passes", len(st.applied))
	}

	// §224.5 — the durable run record states what it accepted. Writing
	// blocking_residue as the post-acceptance number makes every successful run
	// record read "0 residue", which is the one number that can never be wrong
	// and therefore says nothing: a run that accepted nine losses and a run that
	// had none are indistinguishable afterwards.
	totals, _ := st.evidenceRuns[0]["totals"].(map[string]any)
	if totals == nil {
		t.Fatalf("run record has no totals: %v", st.evidenceRuns[0])
	}
	if got := jsonInt(totals["blocking_residue"]); got != len(lines) {
		t.Fatalf("blocking_residue = %d, want the %d loss(es) that would have blocked", got, len(lines))
	}
	if got := jsonInt(totals["accepted_residue"]); got != len(lines) {
		t.Fatalf("accepted_residue = %d, want %d", got, len(lines))
	}
}

func jsonInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}

// The refusal has to be actionable without transcription. Its loss lines carry
// the accept-file key exactly as the file wants it — `RecordID|Field`, nothing
// wrapped around it — so the reader's move is a copy, and a reader who retypes
// a key by hand cannot get it subtly wrong.
func TestMigrateRunRefusalPrintsPasteableAcceptKeys(t *testing.T) {
	st := &migrateStore{hashes: map[string]string{}}
	srv := serveMigrateStore(t, st)
	env := wsEnv(t, srv)
	migrateCorpus(t, env.Root)
	migrateCorpusDuplicateIdentity(t, env.Root)

	err := migrateRun(env)
	if err == nil {
		t.Fatal("the fixture must refuse before this can mean anything")
	}
	var keys []string
	for _, line := range strings.Split(err.Error(), "\n") {
		if i := strings.Index(line, acceptKeyMarker); i >= 0 {
			keys = append(keys, strings.TrimSpace(line[i+len(acceptKeyMarker):]))
		}
	}
	if len(keys) == 0 {
		t.Fatalf("no pasteable accept key in the refusal:\n%v", err)
	}

	// The proof is the round trip: the keys, exactly as printed, accepted.
	acceptPath := filepath.Join(t.TempDir(), "accepted.txt")
	if err := os.WriteFile(acceptPath, []byte(strings.Join(keys, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	migrateRunAcceptPath = acceptPath
	t.Cleanup(func() { migrateRunAcceptPath = "" })
	if err := migrateRun(env); err != nil {
		t.Fatalf("the printed keys must be the file's keys verbatim, got: %v", err)
	}
}
