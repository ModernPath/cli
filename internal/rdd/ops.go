// Op-builder — originally a Go port of a node op-builder (REQ-CROSS-013,
// TASK-SY-404), now the only one. Same op shapes, same ordering, same content
// hashes as that port produced: canonical
// JSON (sorted keys, JS JSON.stringify string escaping) sha256'd — so the
// switch from the node extractor is hash-stable and causes zero re-sync churn.
package rdd

// Sync operation assembly. Focused sibling files own parsing and record builders.

import ()

// Actor: P2 (attributable actors) — every sync write names its agent.
// Kept "mp-cli" for hash parity with the node op-builder.
var actor = map[string]any{"kind": "agent", "agent_slug": "mp-cli"}

// Op is one typed sync op. Payloads are map[string]any trees of
// string | int | nil | []any | map[string]any.
type Op struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// pruneURedges drops user-requirement edges whose target this batch does not
// emit, re-hashing since the edge list is content. An epic left with none loses
// the key entirely rather than sending an empty list, which sync reads as "no
// declaration" instead of "an authoritative empty set".
func pruneURedges(op Op, emitted map[string]bool) Op {
	ids, ok := op.Payload["user_requirement_external_ids"].([]string)
	if !ok || len(ids) == 0 {
		return op
	}
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if emitted[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(ids) {
		return op
	}
	if len(kept) == 0 {
		delete(op.Payload, "user_requirement_external_ids")
	} else {
		op.Payload["user_requirement_external_ids"] = kept
	}
	return rehashOp(op)
}

// ---------------------------------------------------------------- assembly

// BuildOps assembles the full op batch in dependency order: requirements,
// epics (which link them), gates (string refs — order-independent), burst.
// OBSOLETE rows are not synced; 'finding' RQs stay records, not gates.
func BuildOps(data Data, readRecord func(string) string, today string) []Op {
	var ops []Op

	// Epic records are read first because the user requirements they define are
	// the PARENTS of the ledger rows below, and a parent that arrives after its
	// child leaves the link unresolved for a batch (REQ-CROSS-048/049).
	records := map[string]string{}
	for _, e := range data.Epics {
		text := ""
		if e.Record != "" && readRecord != nil {
			// RecordFS resolves from the sync root when the worklist lives
			// above it (SCN-SY-046); Record stays the payload identity.
			readPath := e.Record
			if e.RecordFS != "" {
				readPath = e.RecordFS
			}
			text = readRecord(readPath)
		}
		records[e.ID] = text
	}
	// SR-SY-1403: file-derived user requirements first — they are the richer
	// records (statement, status, sources, scenarios) — then the epic-defined
	// ones, an id declared in both places keeping its file-derived row. Last,
	// REQ-CROSS-261 recovers a rollup-only id only when its named record supplies
	// the statement; it never promotes rollup prose into content.
	seenUR := map[string]bool{}
	for _, ur := range data.UserReqs {
		if seenUR[ur.ID] {
			continue
		}
		seenUR[ur.ID] = true
		ops = append(ops, BuildLedgerUserRequirementOp(ur))
	}
	for _, e := range data.Epics {
		// One op per declared user requirement: a membership edge needs an
		// entity to point at, and a second declaration is as real as the first.
		for _, ur := range ParseEpicUserRequirements(records[e.ID]) {
			if seenUR[ur.ID] {
				continue
			}
			seenUR[ur.ID] = true
			ops = append(ops, BuildUserRequirementOp(ur, e))
		}
	}
	ledgerMentionedUR := map[string]bool{}
	for _, req := range data.Reqs {
		for _, id := range urTokenRe.FindAllString(req.UR, -1) {
			ledgerMentionedUR[id] = true
		}
	}
	for _, e := range data.Epics {
		for _, ur := range rollupUserRequirements(e.URCell, records[e.ID]) {
			// The rollup carrier closes the newly exposed denominator only. An
			// id already named by a ledger row is existing, accepted residue and
			// is not silently reclassified by this recovery path.
			if seenUR[ur.ID] || ledgerMentionedUR[ur.ID] {
				continue
			}
			seenUR[ur.ID] = true
			// The record says what the outcome is, but carries no lifecycle for
			// this undeclared UR. PROPOSED is the deliberate non-invented state;
			// fidelity discloses any weaker-than-epic discrepancy.
			ops = append(ops, BuildUserRequirementOp(ur, Epic{ID: e.ID, State: "proposed"}))
		}
	}

	// REQ-CROSS-064: which requirements an as-built epic carries.
	asBuilt := map[string]bool{}
	asBuiltURs := map[string]bool{}
	for _, e := range data.Epics {
		if !isAsBuiltEpic(records[e.ID]) {
			continue
		}
		section := records[e.ID]
		if m := reqSectionRe.FindStringSubmatch(section); m != nil {
			section = m[1]
		}
		for _, id := range reqIDGlobalRe.FindAllString(section, -1) {
			asBuilt[id] = true
		}
		// The epic's own list writes ranges ("REQ-SYS-050 … REQ-SYS-059"), so it
		// names a fraction of what it carries. The exact set is every row whose
		// PARENT is the user requirement this as-built epic defines — the link
		// REQ-CROSS-049 already puts on every derived row.
		if ur, ok := ParseEpicUserRequirement(records[e.ID]); ok {
			asBuiltURs[ur.ID] = true
		}
	}

	// REQ-CROSS-265: the requirement inventory a scenario edge may point at.
	// Built from the ops this same build emits, so an edge can never outlive
	// the requirement it names.
	known := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			if id, _ := op.Payload["external_id"].(string); id != "" {
				known[id] = true
			}
		}
	}
	for _, r := range data.Reqs {
		// REQ-CROSS-222 §222.4: OBSOLETE rows sync with their terminal status
		// and superseding reference — a terminal decision is process history,
		// not something to drop (they were skipped before the import).
		parentUR := firstURRef(r.UR)
		op := BuildRequirementOpWithExemption(r, asBuilt[r.ID] || asBuiltURs[parentUR])
		if id, _ := op.Payload["external_id"].(string); id != "" {
			known[id] = true
		}
		ops = append(ops, op)
	}
	for _, b := range data.Backlog {
		ops = append(ops, BuildBacklogOp(b))
	}
	for _, e := range data.Epics {
		record := records[e.ID]
		epicOp := BuildEpicOp(e, record)
		// A membership edge needs an entity to point at. The corpus names user
		// requirements this batch cannot define — the accepted irrecoverable
		// UR-FE-* set — and the server drops an id it cannot resolve, so the
		// claim comes back absent and the run's readback fails on it. Those
		// ids are already disclosed by their own `ur-record` accept keys, and
		// the epic→UR arm still counts declarations against emitted ops, so
		// dropping the edge here removes a false claim, not a disclosure.
		epicOp = pruneURedges(epicOp, seenUR)
		// REQ-CROSS-248: register acceptances become gates; the completion
		// acceptance feeds the quartet under the stated precedence.
		accOps, quartet := BuildAcceptanceGateOps(e, record)
		specOp, specOK := BuildSpecApprovalGateOp(e, record)
		apprOp, apprOK := BuildApprovalGateOp(e, record)
		if quartet == nil && !apprOK {
			// the rollup cell is the only recorded acceptance — unless it
			// merely references a register row already gated above
			if wOp, wq, ok := BuildWorklistAcceptanceGate(e); ok && !cellReferencesGated(e.ApprovalCell, accOps) {
				accOps = append(accOps, wOp)
				quartet = wq
			}
		}
		// One fact, one gate: a register row that reuses the APPROVE-<epic>
		// id names the same acceptance the record-derived gate answers — the
		// record gate wins, the row's attribution already rode the quartet.
		if apprOK {
			apprID, _ := apprOp.Payload["external_id"].(string)
			kept := accOps[:0]
			for _, op := range accOps {
				if op.Payload["external_id"] != apprID {
					kept = append(kept, op)
				}
			}
			accOps = kept
		}
		epicOp = applyEpicApproval(epicOp, quartet)
		ops = append(ops, epicOp)
		// REQ-CROSS-247: the record's D-* decisions ride as decision gates
		ops = append(ops, BuildDecisionGateOps(e, record)...)
		// REQ-CROSS-252: every declared scenario definition, spec files
		// included, rides as a first-class scenario record
		ops = append(ops, BuildScenarioOps(e, record, known)...)
		ops = append(ops, accOps...)
		if specOK {
			ops = append(ops, specOp)
		}
		if apprOK {
			ops = append(ops, apprOp)
		}
	}
	// REQ-CROSS-264: every declared task, after the epic ops — the applier
	// resolves the owning epic by code, so a task ahead of its epic has no
	// parent to land under.
	ops = append(ops, BuildTaskOps(data, records)...)

	for _, rq := range data.RQs {
		if rq.State == "finding" {
			continue
		}
		ops = append(ops, BuildRQGateOp(rq))
	}
	for _, oq := range data.OQs {
		ops = append(ops, BuildOQGateOp(oq))
	}
	// REQ-CROSS-237: confirmation gates from the gate flat-file. Warnings are
	// dropped here — `Snapshot` is the one that reports to the user — but the
	// ops are what a derived corpus needs to become answerable.
	if len(data.Gates) > 0 {
		gateOps, _ := BuildGateFileOps(data.Gates, data.GatesRef)
		ops = append(ops, gateOps...)
	}
	if op, ok := BuildCommitBurstOp(data.Commits, today); ok {
		ops = append(ops, op)
	}
	// REQ-CROSS-084: the documents that explain this codebase. System-scoped, so
	// they are neither specs (Epic-scoped) nor ledger rows.
	for _, d := range data.Docs {
		ops = append(ops, BuildDocumentOp(d))
	}

	// D5 "lands as preserved text", §225.6 successor (USER:2026-08-24): every
	// epic record archives byte-exact in process_records — the flip retires
	// the FILE while its bytes survive with sha256 identity, unsplit.
	archiveRevision := ""
	if len(data.Commits) > 0 {
		archiveRevision = data.Commits[0].Hash
	}
	seenRecordDoc := map[string]bool{}
	for _, e := range data.Epics {
		text := records[e.ID]
		if text == "" || e.Record == "" || seenRecordDoc[e.Record] {
			continue
		}
		seenRecordDoc[e.Record] = true
		ops = append(ops, BuildProcessRecordOp(e.Record, "epic", text, archiveRevision))
	}

	// Every other retiring file rides the same way, verbatim. WORKLIST work
	// rows, docs/85 finding blocks and over-cap OQ bodies build no structured
	// op by design; the ledgers, the backlog, the gap register and everything
	// beside an epic record inside epics/** build a structured op that carries
	// SOME of the file and never its bytes. After the flip the repository does
	// not hold any of them, so whatever no carrier names exists nowhere.
	//
	// Extension is irrelevant and content is untouched: a contract file is
	// carried as the bytes it is, not reshaped into markdown. The type stays
	// `process` for every archival document — the server's document ingest
	// waives the embedding requirement for exactly that type, and preservation
	// must not depend on an embedding provider being reachable.
	//
	// Snapshot resolves the list; the default names are only a fallback for
	// callers that built Data without one.
	archival := data.ArchivalFiles
	if archival == nil {
		archival = archivalProcessFiles
	}
	// Carried once is carried: an epic record already rode above as its
	// verbatim document, and an epic spec rides inside its epic payload. A
	// second document for the same path would be a duplicate carrier, not
	// better coverage.
	carriedElsewhere := map[string]bool{}
	for recordRel := range seenRecordDoc {
		carriedElsewhere[recordRel] = true
	}
	for _, e := range data.Epics {
		for _, s := range e.Specs {
			carriedElsewhere[s.Rel] = true
		}
	}
	for _, rel := range archival {
		if carriedElsewhere[rel] {
			continue
		}
		text := ""
		if readRecord != nil {
			text = readRecord(rel)
		}
		if text == "" {
			continue
		}
		// §225.6 successor: the byte archive does not split — bytea has no
		// content cap, and reassembly seams stop existing.
		ops = append(ops, BuildProcessRecordOp(rel, "document", text, archiveRevision))
	}

	return ops
}

// archivalProcessFiles are the flip-retired files whose rows or blocks build
// no structured op; each is carried whole so nothing is lost with the file.
var archivalProcessFiles = []string{
	"WORKLIST.md",
	"docs/85-loop-review-queue.md",
	"process/08-open-questions.md",
}
