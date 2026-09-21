package cmd

// EPIC-CLI-002 — working-set sync. Two untracked directories, never committed,
// never authoritative (PROCESS.md "State records and reconciliation"):
//
//   .modernpath/your-move/    the pending human-decision projection, regenerated
//                             wholesale from GET /api/v1/sync/gates
//   .modernpath/working-set/  explicitly pulled items in the installed
//                             file-state shapes, one snapshot header per file
//
// Identity design (EPIC-CLI-002, revised for REQ-CROSS-219): the read surface
// serves no identity field, so each working-set file's header records two
// CLI-computed hashes — "Source identity" (sha256 of the canonical
// {item payload, associated gate payloads}; staleness — gates fold in so a
// gate flipping state makes the file stale) and "Written body" (sha256 of the
// file content below the header block; local-edit conflict). The header is
// excluded from the hashed region so the hash can live there without covering
// itself.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/authoring"
	"github.com/modernpath/cli/internal/authoring/diff"
	"github.com/spf13/cobra"
)

// REQ-CROSS-313 (SR-CLI-0084): a pulled scope directory stamps its authoring or
// review context in this file; push (SR-CLI-0085) and finding creation
// (SR-CLI-0086) send it. editableFieldMarker is the authoring render's editable
// fence — a --for-review render carries none.
const (
	contextFile         = ".context"
	editableFieldMarker = authoring.EditableFieldMarker
)

const yourMoveDir = ".modernpath/your-move"
const workingSetDir = ".modernpath/working-set"

// The two absence markers are not interchangeable: the first
// states the store has no column for the field, the second that the store has
// it but the read surface does not serve it.
//
// notRecorded currently has no occupant (REQ-CROSS-262): the three slots that
// carried it — gate prerequisites, fingerprint, predecessor/successor — all
// have store columns, and one of them is served outright. The constant stays
// because the category is real and the distinction is the rule; a render test
// pins that nothing emits it, so reintroducing it needs a field that genuinely
// has no column.
const notRecorded = "«not recorded by store»"
const notServed = "«not served by the read surface»"

const writtenBodyKey = "- **Written body:** sha256:"
const sourceIdentityKey = "- **Source identity:** sha256:"

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// fetchList GETs one read endpoint and returns the named collection. A non-200
// carries the server's reason — never a bare status.
func fetchList(env *factoryEnv, path, key string) ([]any, error) {
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("", status, body)
	}
	return listFromData(body, key)
}

// listFromData extracts one named list from a decoded {"data":{…}} envelope.
// A 200 without the expected key is a changed envelope, not an empty list —
// reading it as empty overwrote a correct projection with "queue is clear" and
// reported every materialized file vanished.
func listFromData(body map[string]any, key string) ([]any, error) {
	raw, present := dataOf(body)[key]
	if !present {
		return nil, fmt.Errorf("server response has no %q — refusing to read a changed envelope as empty", key)
	}
	if raw == nil {
		return []any{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("server response %q is not a list — refusing to read a changed envelope as empty", key)
	}
	return items, nil
}

// fetchRequirementLists reads the requirements endpoint ONCE and returns both
// the system-requirement list (data.requirements) and the user-requirement list
// (data.user_requirements): the endpoint serves both keys in a single envelope
// (sync_api_controller requirements/2), so one GET indexes the whole tree.
func fetchRequirementLists(env *factoryEnv, path string) (srs []any, urs []any, err error) {
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, nil, err
	}
	if status != 200 {
		return nil, nil, serverRefusal("", status, body)
	}
	if srs, err = listFromData(body, "requirements"); err != nil {
		return nil, nil, err
	}
	if urs, err = listFromData(body, "user_requirements"); err != nil {
		return nil, nil, err
	}
	return srs, urs, nil
}

// storeRevisionLine — REQ-CROSS-348 (EPIC-CLI-018): the header names the
// revision of the STORE the content came from, read from the response header
// the server sends, never the local CLI build; a store that serves none says so.
// The CLI's own build follows on its own labelled line.
func storeRevisionLine(env *factoryEnv) string {
	revision := "store revision not served by this server"
	if env.storeRevision != "" {
		revision = "store " + env.storeRevision
	}
	return fmt.Sprintf("%s · system %d · %s\n- **CLI build:** modernpath %s", env.APIURL, env.SystemID, revision, Version)
}

// atomicWrite writes via a same-directory temp file and rename, so a failure
// at any point leaves the previous content untouched.
func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

// --- REQ-CROSS-215: the your-move projection ---

// yourMoveRun regenerates the pending human-decision projection. It is a
// projection, never an authority: previous content is replaced wholesale, and
// a failed fetch leaves the previous projection untouched (the write happens
// only after a 200).
func yourMoveRun(env *factoryEnv, now time.Time) error {
	gates, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), "gates")
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Your move — open human decisions\n\n")
	fmt.Fprintf(&b, "- **Snapshot at:** %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "- **Source store/revision:** %s\n\n", storeRevisionLine(env))
	if len(gates) == 0 {
		b.WriteString("No open human gates — the queue is clear at this snapshot.\n")
	} else {
		fmt.Fprintf(&b, "%d open gate(s). Only `OPEN` human gates appear here; this file is a\nregenerated projection, never an authority.\n", len(gates))
		for _, g := range gates {
			m, _ := g.(map[string]any)
			fmt.Fprintf(&b, "\n## %s — %s\n\n- **Kind:** %s\n", str(m, "external_id"), str(m, "title"), str(m, "kind"))
			if rec := str(m, "recommendation"); rec != "" {
				fmt.Fprintf(&b, "- **Recommends:** %s\n", rec)
			}
			if options, ok := m["options"].([]any); ok && len(options) > 0 {
				b.WriteString("- **Options:**")
				for _, o := range options {
					om, _ := o.(map[string]any)
					fmt.Fprintf(&b, " `%s`", str(om, "key"))
				}
				b.WriteString("\n")
			}
		}
	}
	path := filepath.Join(env.Root, yourMoveDir, "GATES.md")
	if err := atomicWrite(path, []byte(b.String())); err != nil {
		return err
	}
	printSuccess("your move: %d open gate(s) → %s", len(gates), filepath.Join(yourMoveDir, "GATES.md"))
	return nil
}

// --- REQ-CROSS-216/218: working-set pull ---

type wsItem struct {
	id      string
	kind    string // "epic" | "requirement"
	payload map[string]any
}

// wsIndex fetches the epics, requirements, and gates once and indexes the items
// by external id. There is no per-id GET on the read surface; an unknown id is a
// list absence, not a 404.
//
// REQ-CROSS-308 (EPIC-CLI-007): with includeCandidates the requirements read
// asks for candidates (include=candidates), so a just-authored DERIVED
// candidate is materializable by its author; the default read is unchanged and
// the rollup the server computes stays candidate-free either way.
//
// REQ-CROSS-048/308: the requirements read serves system requirements AND user
// requirements in one envelope under separate keys — both index as "requirement"
// items so `working-set pull` resolves either. Indexing only data.requirements
// left every UR (a DERIVED candidate UR included) reported unknown.
func wsIndex(env *factoryEnv, includeCandidates bool, preGates []any) (map[string]wsItem, []any, error) {
	epics, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
	if err != nil {
		return nil, nil, err
	}
	reqPath := fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID)
	if includeCandidates {
		reqPath += "&include=candidates"
	}
	requirements, userRequirements, err := fetchRequirementLists(env, reqPath)
	if err != nil {
		return nil, nil, err
	}
	// REQ-CROSS-219: pull and check read gate HISTORY (state=all, the store's
	// vocabulary). The your-move projection deliberately keeps the default
	// open-only fetch — an answered gate is not pending.
	gates := preGates
	if gates == nil {
		gates, err = fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
		if err != nil {
			return nil, nil, err
		}
	}
	// REQ-CROSS-393 (EPIC-CLI-019): backlog, gap and tooling records are
	// pullable by id too. A server without the read (404) serves none.
	backlog, err := fetchList(env, fmt.Sprintf("/api/v1/sync/backlog?system_id=%d", env.SystemID), "backlog")
	if err != nil {
		backlog = nil
	}
	index := map[string]wsItem{}
	for _, b := range backlog {
		m, _ := b.(map[string]any)
		if id := str(m, "external_id"); id != "" {
			index[id] = wsItem{id: id, kind: "backlog", payload: m}
		}
	}
	for _, e := range epics {
		m, _ := e.(map[string]any)
		if id := str(m, "external_id"); id != "" {
			index[id] = wsItem{id: id, kind: "epic", payload: m}
		}
	}
	for _, r := range append(append([]any{}, requirements...), userRequirements...) {
		m, _ := r.(map[string]any)
		if id := str(m, "external_id"); id != "" {
			index[id] = wsItem{id: id, kind: "requirement", payload: m}
		}
	}
	// REQ-CROSS-407 (EPIC-CLI-020): a gate is pullable by its own id — the
	// release-selection gate, an entry or completion gate — with the same block
	// its owning item renders and the Fingerprint line a gated advance echoes.
	// A record id wins over a gate of the same name.
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		if id := str(gm, "external_id"); id != "" {
			if _, taken := index[id]; !taken {
				index[id] = wsItem{id: id, kind: "gate", payload: gm}
			}
		}
	}
	return index, gates, nil
}

func fieldOr(m map[string]any, key, marker string) string {
	if v := str(m, key); v != "" {
		return v
	}
	return marker
}

// renderItemBody serializes one item into the installed file-state shape for
// its kind. Every slot the read surface does not provide names its actual
// cause; nothing is inferred. The item's full gate history materializes with
// it (REQ-CROSS-219), associated by holds ∪ id-embedding.
// renderBacklogBody renders a backlog, gap or tooling record in the canonical
// file-state shape (`.modernpath/rdd/file-state/BACKLOG.md`): the triage
// backlog block for backlog and tooling records, the gap block for a gap
// (REQ-CROSS-393). Every served field renders; an absent one reads "—".
func renderBacklogBody(item wsItem) string {
	m := item.payload
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n", item.id, fieldOr(m, "title", notServed))
	fmt.Fprintf(&b, "- **Fingerprint:** %s\n", servedOr(m, "content_fingerprint", notServed))
	fmt.Fprintf(&b, "- **Kind of record:** %s\n", servedOr(m, "kind", notServed))
	if str(m, "kind") == "gap" {
		fmt.Fprintf(&b, "- **Kind:** %s\n", servedOr(m, "gap_kind", "—"))
		fmt.Fprintf(&b, "- **Source:** %s\n", servedOr(m, "raised_by", "—"))
		fmt.Fprintf(&b, "- **Affected traces:** %s\n", idListOr(m, "affected_trace_external_ids", "—"))
		fmt.Fprintf(&b, "- **Consequence:** %s\n", servedOr(m, "consequence", "—"))
		fmt.Fprintf(&b, "- **Disclosed in:** %s\n", idListOr(m, "disclosed_in_gate_external_ids", "—"))
	} else {
		fmt.Fprintf(&b, "- **Raised by / at:** %s / %s\n", servedOr(m, "raised_by", "—"), servedOr(m, "raised_at", "—"))
		fmt.Fprintf(&b, "- **Observed:** %s\n", servedOr(m, "observation", "—"))
		fmt.Fprintf(&b, "- **Why unrouted:** %s\n", servedOr(m, "why_unrouted", "—"))
		fmt.Fprintf(&b, "- **Candidate route:** %s\n", servedOr(m, "candidate_route", "—"))
		fmt.Fprintf(&b, "- **Affected items:** %s\n", idListOr(m, "affected_external_ids", "—"))
	}
	fmt.Fprintf(&b, "- **Disposition:** %s", servedOr(m, "disposition", "—"))
	if ref := str(m, "disposition_ref"); ref != "" {
		fmt.Fprintf(&b, " (%s)", ref)
	}
	b.WriteString("\n")
	if notes := str(m, "notes_md"); notes != "" {
		fmt.Fprintf(&b, "\n### Notes\n\n%s\n", notes)
	}
	if meta, ok := m["metadata"].(map[string]any); ok && len(meta) > 0 {
		b.WriteString("\n### Captured\n\n")
		keys := make([]string, 0, len(meta))
		for k := range meta {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "- **%s:** %v\n", k, meta[k])
		}
	}
	return b.String()
}

// servedOr distinguishes served-but-null ("—": the store answered, the slot
// is empty) from not-served (the marker) — the REQ-CROSS-219 honesty rule.
func servedOr(m map[string]any, key, fallback string) string {
	v, present := m[key]
	if !present {
		return fallback
	}
	if s, _ := v.(string); s != "" {
		return s
	}
	return "—"
}

// idListOr renders a served string list as an ` · `-joined line, or the
// fallback when the key is absent or empty.
func idListOr(m map[string]any, key, fallback string) string {
	raw, _ := m[key].([]any)
	var ids []string
	for _, v := range raw {
		if s, _ := v.(string); s != "" {
			ids = append(ids, s)
		}
	}
	if len(ids) == 0 {
		return fallback
	}
	return strings.Join(ids, " · ")
}

// citationLine renders the served citation list on one line (REQ-CROSS-381):
// the marker only when the key is absent, "—" when the store serves none.
func citationLine(m map[string]any, fallback string) string {
	v, present := m["source_citations"]
	if !present {
		return fallback
	}
	refs := citationRefs(v)
	if len(refs) == 0 {
		return "—"
	}
	return strings.Join(refs, " · ")
}

// acceptanceServed reports whether the payload carries an acceptance key at
// all — the read serves criteria (SR) or scenarios (UR) as a list, empty or not.
func acceptanceServed(m map[string]any) bool {
	for _, key := range []string{"scenarios", "criteria", "acceptance_scenarios"} {
		if _, present := m[key]; present {
			return true
		}
	}
	return false
}

// memberUnion renders an epic's declared members — the served SR list followed
// by the served UR list, in declared order (REQ-CROSS-382). The marker only
// when the read serves neither list; "—" when both are empty.
func memberUnion(m map[string]any, fallback string) string {
	ids := servedMembers(m, nil)
	if ids == nil {
		return fallback
	}
	if len(ids) == 0 {
		return "—"
	}
	return strings.Join(ids, " · ")
}

// servedMembers is the store's declared membership as the epic read serves it,
// SRs then URs; fallback when the read serves neither list.
func servedMembers(m map[string]any, fallback []string) []string {
	_, srServed := m["requirement_external_ids"]
	_, urServed := m["user_requirement_external_ids"]
	if !srServed && !urServed {
		return fallback
	}
	ids := append([]string{}, stringSlice(m["requirement_external_ids"])...)
	return append(ids, stringSlice(m["user_requirement_external_ids"])...)
}

// gateLineage renders the predecessor/successor pair. Neither key served is one
// fact about the read surface and gets one marker; either key served makes the
// pair renderable, and a served null half reads "—" — the gate genuinely has no
// predecessor, which is not the same as the surface withholding one.
func gateLineage(gm map[string]any) string {
	_, hasPred := gm["predecessor_external_id"]
	_, hasSucc := gm["successor_external_id"]
	if !hasPred && !hasSucc {
		return notServed
	}
	return servedOr(gm, "predecessor_external_id", notServed) + " / " +
		servedOr(gm, "successor_external_id", notServed)
}

func renderItemBody(item wsItem, gates []any) string {
	if item.kind == "backlog" {
		return renderBacklogBody(item)
	}
	if item.kind == "gate" {
		var b strings.Builder
		writeGateBlock(&b, item.payload)
		return strings.TrimPrefix(b.String(), "\n")
	}
	m := item.payload
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n", item.id, fieldOr(m, "title", notServed))
	// The record's current content fingerprint. `author update` and `author
	// relate` both require --expected-fingerprint, and it used to exist only in
	// a create/update RESPONSE — so an existing record was not editable through
	// the advertised flow unless the same session had just written it. Pulled
	// here, editing an existing record needs no side-channel read.
	fmt.Fprintf(&b, "- **Fingerprint:** %s\n", servedOr(m, "fingerprint", notServed))
	// REQ-CROSS-381 (EPIC-CLI-018): every field the read serves renders; a
	// served-but-empty slot reads "—"; the marker is only ever a statement about
	// an absent key. The epic has no `status` column, so no slot claims one.
	if item.kind == "epic" {
		b.WriteString("- **Kind:** Epic\n")
		// REQ-CROSS-223: the PROCESS.md lifecycle, distinct from the board axis.
		fmt.Fprintf(&b, "- **Process status:** %s\n", servedOr(m, "process_status", notServed))
		fmt.Fprintf(&b, "- **Loop status:** upper %s · lower %s\n",
			servedOr(m, "upper_loop_status", notServed), servedOr(m, "lower_loop_status", notServed))
		fmt.Fprintf(&b, "- **Owner / release:** %s / %s\n", servedOr(m, "owner", notServed), fieldOr(m, "release", notServed))
		// REQ-CROSS-382: every declared member, SRs then URs, in declared order.
		fmt.Fprintf(&b, "- **Members:** %s\n", memberUnion(m, notServed))
		fmt.Fprintf(&b, "- **Approval:** %s (%s)\n",
			fieldOr(m, "approved_at", notServed), fieldOr(m, "approval_source_tag", notServed))
		fmt.Fprintf(&b, "- **Scope:** %s\n", servedOr(m, "scope", notServed))
		fmt.Fprintf(&b, "- **Outcome source:** %s\n", servedOr(m, "outcome_source", notServed))
		if d := str(m, "description"); d != "" {
			b.WriteString("\n### Description\n\n" + d + "\n")
		}
	} else {
		// A UR payload carries its derived SR ids; the SR-only prose fields
		// (rationale, boundary, verification method) have no UR column and are
		// not rendered for one — an absent key there is not a withheld field.
		_, isUR := m["system_requirement_external_ids"]
		fmt.Fprintf(&b, "- **Kind / status:** requirement / %s\n", fieldOr(m, "work_status", notServed))
		fmt.Fprintf(&b, "- **Statement / source:** %s / %s\n",
			servedOr(m, "description", notServed), citationLine(m, notServed))
		fmt.Fprintf(&b, "- **Context / stage:** %s / %s\n",
			fieldOr(m, "context", notServed), fieldOr(m, "stage", notServed))
		fmt.Fprintf(&b, "- **Priority:** %s\n", servedOr(m, "priority", notServed))
		fmt.Fprintf(&b, "- **Owner / release:** %s / %s\n",
			servedOr(m, "owner", notServed), fieldOr(m, "release", notServed))
		// REQ-CROSS-223: the full declared relation list, falling back to the
		// single parent. A served-but-empty slot reads "—" (no relations
		// declared), never an absence marker claiming a cause it lacks.
		relations := idListOr(m, "parent_external_ids", "")
		if relations == "" {
			relations = fieldOr(m, "parent_external_id", "")
		}
		if relations == "" {
			if _, served := m["parent_external_ids"]; served {
				relations = "—"
			} else {
				relations = notServed
			}
		}
		fmt.Fprintf(&b, "- **Declared relations:** %s\n", relations)
		// REQ-CROSS-048: a user requirement carries its DERIVED system
		// requirements (the child edge), not the SR-side parent_external_ids.
		// Show them so a pulled UR's real relation content is not dropped;
		// the key is absent on an SR payload, so nothing is added there.
		if _, served := m["system_requirement_external_ids"]; served {
			fmt.Fprintf(&b, "- **Derives:** %s\n", idListOr(m, "system_requirement_external_ids", "—"))
		}
		if !isUR {
			fmt.Fprintf(&b, "- **Rationale:** %s\n", servedOr(m, "rationale", notServed))
			fmt.Fprintf(&b, "- **Boundary:** %s\n", servedOr(m, "boundary", notServed))
			fmt.Fprintf(&b, "- **Verification method:** %s\n", servedOr(m, "verification_method", notServed))
		}
		// Acceptance content is served structured (criteria on an SR, scenarios
		// on a UR); render one line per item so the reviewer sees what the store
		// holds instead of filing "acceptance criteria missing".
		if acceptanceServed(m) {
			b.WriteString("\n### Acceptance\n\n")
			lines := scenariosFromPayload(m)
			if len(lines) == 0 {
				b.WriteString("—\n")
			}
			for _, line := range lines {
				b.WriteString("- " + line + "\n")
			}
		}
		if d := str(m, "detail_md"); d != "" {
			b.WriteString("\n### Detail\n\n" + strings.TrimRight(d, "\n") + "\n")
		}
	}

	b.WriteString("\n### Gates\n")
	found := 0
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		if !gateAssociated(gm, item.id) {
			continue
		}
		found++
		writeGateBlock(&b, gm)
	}
	if found == 0 {
		b.WriteString("\nNo gates reference this item at this snapshot.\n")
	}
	return b.String()
}

// writeGateBlock renders one served gate: under an item's Gates section, and
// as the whole body of a gate pulled by its own id (REQ-CROSS-407).
func writeGateBlock(b *strings.Builder, gm map[string]any) {
	fmt.Fprintf(b, "\n## GATE %s — %s\n\n", str(gm, "external_id"), str(gm, "title"))
	fmt.Fprintf(b, "- **Kind:** human / %s\n", str(gm, "kind"))
	// REQ-CROSS-219: the store's state, verbatim. Process-level CLOSED is
	// not a stored state and is never derived.
	fmt.Fprintf(b, "- **State:** %s\n", fieldUpperOr(gm, "state", notServed))
	if answer := str(gm, "answer"); answer != "" {
		fmt.Fprintf(b, "- **Verdict / answer:** %s (%s)\n", answer, fieldOr(gm, "source_tag", notServed))
	}
	// A null served field is an absence, not an unserved field — "—"
	// claims nothing (live run 2026-08-20: an OPEN gate's answerer is
	// genuinely null, and the notServed marker overstated the cause).
	fmt.Fprintf(b, "- **Actor / evaluator:** opener %s / answerer %s\n",
		fieldOr(gm, "opener_kind", "—"), fieldOr(gm, "answerer_kind", "—"))
	// REQ-CROSS-262: all three of these slots used to read «not recorded by
	// store», and all three were false. The prerequisite, predecessor and
	// successor columns exist — they are recorded and simply not served, a
	// different fact with a different marker. The fingerprint is served
	// outright, and it is the value the flip and the advance both reference:
	// telling the reader to go find one they were already holding was the
	// worst of the three.
	fmt.Fprintf(b, "- **Prerequisites:** %s\n", idListOr(gm, "prerequisite_gate_external_ids", notServed))
	fmt.Fprintf(b, "- **Fingerprint:** %s\n", servedOr(gm, "fingerprint", notServed))
	fmt.Fprintf(b, "- **Application:** %s\n", fieldOr(gm, "applied_state", notServed))
	fmt.Fprintf(b, "- **Predecessor / successor:** %s\n", gateLineage(gm))
}

// gateAssociated ties a gate to an item by holds ∪ id-embedding — substring
// alone misses hold-attached gates (the RQ-268-holds-REQ-PLN-043 pattern),
// and an UNBOUNDED substring absorbs prefix-id neighbors: RQ-15 must not
// claim RQ-150's gates.
func gateAssociated(gate map[string]any, itemID string) bool {
	if gid := str(gate, "external_id"); gid != "" && containsBoundedID(gid, itemID) {
		return true
	}
	holds, _ := gate["holds"].([]any)
	for _, h := range holds {
		hm, _ := h.(map[string]any)
		if str(hm, "held_external_id") == itemID {
			return true
		}
	}
	return false
}

func isIDChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

// containsBoundedID reports whether id occurs in s with non-id characters (or
// the string edges) on both sides — "APPROVE-RQ-15" embeds RQ-15,
// "APPROVE-RQ-150" does not.
func containsBoundedID(s, id string) bool {
	if id == "" {
		return false
	}
	for start := 0; ; {
		i := strings.Index(s[start:], id)
		if i < 0 {
			return false
		}
		i += start
		after := i + len(id)
		if (i == 0 || !isIDChar(s[i-1])) && (after == len(s) || !isIDChar(s[after])) {
			return true
		}
		start = i + 1
	}
}

func fieldUpperOr(m map[string]any, key, marker string) string {
	if v := str(m, key); v != "" {
		return strings.ToUpper(v)
	}
	return marker
}

// associatedGates returns the item's gates sorted by external id — the
// deterministic order the revised source identity hashes over.
func associatedGates(item wsItem, gates []any) []map[string]any {
	var out []map[string]any
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		if gateAssociated(gm, item.id) {
			out = append(out, gm)
		}
	}
	sort.Slice(out, func(i, j int) bool { return str(out[i], "external_id") < str(out[j], "external_id") })
	return out
}

// sourceIdentityFor implements the revised Identity design (REQ-CROSS-219):
// the hash covers the item payload AND its associated gate payloads, so a
// gate flipping state makes the materialized file stale — with the item
// payload alone, gate history went stale invisibly.
func sourceIdentityFor(item wsItem, gates []any) (string, error) {
	assoc := associatedGates(item, gates)
	canonical := map[string]any{"item": item.payload, "gates": assoc}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

func renderWorkingSetFile(env *factoryEnv, item wsItem, gates []any, now time.Time) (string, error) {
	sourceIdentity, err := sourceIdentityFor(item, gates)
	if err != nil {
		return "", err
	}
	body := renderItemBody(item, gates)
	header := fmt.Sprintf("# %s — working-set snapshot\n\n- **Snapshot at:** %s\n- **Source store/revision:** %s\n%s%s\n%s%s\n\n",
		item.id, now.Format(time.RFC3339), storeRevisionLine(env),
		sourceIdentityKey, sourceIdentity, writtenBodyKey, sha256Hex([]byte(body)))
	return header + body, nil
}

// headerValue extracts the hex value following key on its own header line.
func headerValue(content, key string) string {
	idx := strings.Index(content, key)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(key):]
	if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
		rest = rest[:nl]
	}
	return strings.TrimSpace(rest)
}

// bodyOf returns the hashed region: everything after the blank line that
// closes the header block (the line after the Written-body entry).
func bodyOf(content string) (string, bool) {
	idx := strings.Index(content, writtenBodyKey)
	if idx < 0 {
		return "", false
	}
	rest := content[idx:]
	sep := strings.Index(rest, "\n\n")
	if sep < 0 {
		return "", false
	}
	return rest[sep+2:], true
}

// writeWorkingSetItem enforces REQ-CROSS-218: a file whose current body
// diverges from its recorded written-body hash was edited locally — preserve
// it, land the fresh pull beside it as <id>.md.pulled, and report the
// conflict. A file that cannot be parsed as a snapshot is treated the same
// way: when a guard cannot tell wrong from unverified it flags, it does not
// delete.
func writeWorkingSetItem(env *factoryEnv, item wsItem, gates []any, now time.Time) (conflict bool, err error) {
	fresh, err := renderWorkingSetFile(env, item, gates, now)
	if err != nil {
		return false, err
	}
	return writeWorkingSetSnapshot(env, item.id+".md", fresh)
}

// unsafeSnapshotName reports whether a working-set file name — derived from a
// served external id — would escape the working-set directory: filepath.Join
// collapses "../", so only a plain single-component name is writable.
func unsafeSnapshotName(filename string) bool {
	return filename != filepath.Base(filename) || strings.ContainsAny(filename, `/\`)
}

// writeWorkingSetSnapshot is the conflict-guarded writer every working-set
// file goes through: a file whose current body diverges from its recorded
// written-body hash was edited locally — preserve it, land the fresh pull
// beside it as .pulled, report the conflict.
func writeWorkingSetSnapshot(env *factoryEnv, filename, fresh string) (conflict bool, err error) {
	if unsafeSnapshotName(filename) {
		return false, fmt.Errorf("refusing %q: an external id must be a plain file name, not a path", strings.TrimSuffix(filename, ".md"))
	}
	path := filepath.Join(env.Root, workingSetDir, filename)
	existing, readErr := os.ReadFile(path)
	if readErr == nil {
		recorded := headerValue(string(existing), writtenBodyKey)
		body, ok := bodyOf(string(existing))
		if !ok || recorded == "" || sha256Hex([]byte(body)) != recorded {
			if err := atomicWrite(path+".pulled", []byte(fresh)); err != nil {
				return true, err
			}
			return true, nil
		}
	} else if !os.IsNotExist(readErr) {
		return false, readErr
	}
	return false, atomicWrite(path, []byte(fresh))
}

// --- REQ-CROSS-220: the store-held selection ---

const selectionTarget = "selection"
const selectionFile = "WORK-SELECTION.md"

func fetchWorkSelection(env *factoryEnv) (map[string]any, error) {
	// REQ-CROSS-345: the read is caller-scoped. --piece names which of the
	// caller's own current pieces to resolve, carried as ?scope=.
	path := fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID)
	if wsPiece != "" {
		path += "&scope=" + url.QueryEscape(wsPiece)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		// A caller holding several current pieces and naming none is told — by
		// the server — to name one. Surface that actionable refusal directly
		// rather than as a bare "server <status>".
		if msg, ok := body["error"].(string); ok && msg != "" {
			return nil, errors.New(msg)
		}
		return nil, serverRefusal("", status, body)
	}
	payload := dataOf(body)
	if _, present := payload["current"]; !present {
		return nil, fmt.Errorf("server response has no work-selection envelope — refusing to read it as empty")
	}
	return payload, nil
}

// selectionIdentity hashes the source state the WORK-SELECTION.md snapshot
// depends on. Store-backed, the rendered body folds in the active-release source
// resolved from its gate, so the identity that governs currency must fold it too
// — otherwise a gate-only approval leaves the snapshot reported current with its
// "Active release source: MISSING" line frozen. A file-backed (or non-single-
// active) read passes src==nil and keeps the payload-only hash byte-identical.
func selectionIdentity(payload map[string]any, src *releaseSource) (string, error) {
	canonical := any(payload)
	if src != nil {
		canonical = map[string]any{
			"selection": payload,
			"release_source": map[string]any{
				"slug": src.slug, "found": src.found, "tag": src.tag, "gate": src.gate,
			},
		}
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

// selectionReleaseSource resolves the active-release source that
// renderSelectionBody folds into WORK-SELECTION.md, so pull and check compute
// one identical source identity. A file-backed workspace (or a 0/>1 active set)
// returns nil, leaving the identity payload-only.
func selectionReleaseSource(env *factoryEnv, payload map[string]any, gates []any) (*releaseSource, error) {
	if !storeBackedWorkspace(env.Root) {
		return nil, nil
	}
	return resolveActiveReleaseSource(payload, gates)
}

// renderSelectionBody serializes the served selection into the installed
// WORK-SELECTION.md shape. The history section keeps the shape's own Outcome
// vocabulary: rows whose outcome the shape does not enumerate are excluded —
// a suspension episode never ends the selection, and the episode is visible
// in Suspended while it is live.
//
// SR-CROSS-328: a store-backed workspace passes a non-nil src for the single
// active release, so WORK-SELECTION.md — the rdd-start preflight snapshot —
// carries the active release's USER: source (or reports it missing). A
// file-backed workspace passes nil and renders exactly as before.
func renderSelectionBody(payload map[string]any, src *releaseSource) string {
	var b strings.Builder
	b.WriteString("## Work selection\n\n")

	releases, _ := payload["active_release"].([]any)
	switch len(releases) {
	case 1:
		rm, _ := releases[0].(map[string]any)
		fmt.Fprintf(&b, "- **Active release:** %s (%s)\n", str(rm, "slug"), str(rm, "status"))
		// Only a single active release has a source to bind; 0/>1 is a shape,
		// not a selection with a USER: source.
		if src != nil {
			if src.found {
				fmt.Fprintf(&b, "- **Active release source:** %s (gate %s)\n", src.tag, src.gate)
			} else {
				fmt.Fprintf(&b, "- **Active release source:** MISSING — active release %s — %s; the store-backed preflight (rdd-start step 2) stops here\n", src.slug, missingReleaseGateLine(src.slug))
			}
		}
	case 0:
		b.WriteString("- **Active release:** NONE ACTIVE — exactly one non-base release must be active; activate one with 'modernpath factory release activate <slug>'\n")
	default:
		b.WriteString("- **Active release:** VIOLATION — more than one active:")
		for _, r := range releases {
			rm, _ := r.(map[string]any)
			fmt.Fprintf(&b, " %s", str(rm, "slug"))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n### Current selection\n\n")
	if current, ok := payload["current"].(map[string]any); ok {
		fmt.Fprintf(&b, "- **Selected scope:** %s\n", fieldOr(current, "scope_external_id", "—"))
		fmt.Fprintf(&b, "- **Members:** %s\n", memberList(current))
		fmt.Fprintf(&b, "- **Scope kind:** %s\n", fieldOr(current, "scope_kind", "—"))
		fmt.Fprintf(&b, "- **Frozen at fingerprint:** %s\n", fieldOr(current, "fingerprint", "—"))
		fmt.Fprintf(&b, "- **Reconnaissance revision:** %s\n", fieldOr(current, "recon_revision", "—"))
		fmt.Fprintf(&b, "- **Current phase:** %s\n", fieldOr(current, "phase", "—"))
		fmt.Fprintf(&b, "- **Lane:** %s\n", laneLabel(str(current, "lane")))
		fmt.Fprintf(&b, "- **Waiting on:** %s\n", fieldOr(current, "waiting_on", "nothing"))
		fmt.Fprintf(&b, "- **Owner:** %s\n", fieldOr(current, "owner", "—"))
	} else {
		b.WriteString("No current selection.\n")
	}

	// The installed shape's exact columns: 7-column Suspended,
	// 5-column history. Restored-to is "—" while an episode is live — it is
	// written at resume, onto the row that leaves.
	b.WriteString("\n### Suspended selections\n\n")
	suspended, _ := payload["suspended"].([]any)
	if len(suspended) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Scope | Suspended status | Restored-to status | Reason | Owner | Target | Blocker/gate |\n|---|---|---|---|---|---|---|\n")
		for _, row := range suspended {
			rm, _ := row.(map[string]any)
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				str(rm, "scope_external_id"), str(rm, "suspended_status"),
				fieldOr(rm, "restored_to", "—"),
				fieldOr(rm, "suspended_reason", "—"), fieldOr(rm, "owner", "—"),
				fieldOr(rm, "suspended_target", "—"), fieldOr(rm, "waiting_on", "—"))
		}
	}

	b.WriteString("\n### Selection history\n\n")
	history, _ := payload["history"].([]any)
	shown := 0
	var rows strings.Builder
	for _, row := range history {
		rm, _ := row.(map[string]any)
		outcome := str(rm, "outcome")
		if outcome != "done" && outcome != "obsolete" && !strings.HasPrefix(outcome, "returned:") {
			continue
		}
		shown++
		fmt.Fprintf(&rows, "| %s | %s | %s | %s | %s |\n",
			str(rm, "scope_external_id"), fieldOr(rm, "inserted_at", "—"),
			fieldOr(rm, "left_at", "—"), outcome,
			fieldOr(rm, "successor_id", "—"))
	}
	if shown == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("| Scope | Selected at | Left at | Outcome | Successor selection |\n|---|---|---|---|---|\n")
		b.WriteString(rows.String())
	}
	return b.String()
}

// memberList renders the served members array, or an honest dash.
func memberList(current map[string]any) string {
	members, _ := current["members"].([]any)
	if len(members) == 0 {
		return "—"
	}
	names := make([]string, 0, len(members))
	for _, m := range members {
		if v, ok := m.(string); ok {
			names = append(names, v)
		}
	}
	return strings.Join(names, ", ")
}

// --- SR-CROSS-328: the store-backed active-release source ---
//
// Store-backed, the active-release selection's USER: source is an answered
// decision gate — kind approval_request, purpose release_selection, the approve
// option chosen — exactly as the store-backed flip's declaration gate carries
// its source (Core.Gates.answer takes the answerer from the authenticated
// caller, never the request). GET /api/v1/sync/gates serves external_id,
// purpose, source_tag, state AND exact_scope (verified against
// sync_api_controller gate_json), so the read binds the gate to the active slug
// by the GATE-RELEASE-<slug> external-id convention — robust whether or not
// exact_scope is served — and additionally refuses a served exact_scope that
// names a different release. The "exactly one active" invariant is the release
// rows' own (list_open_process_for_tenant/1, base-excluded), served as
// active_release; this only adds the source.

const releaseSelectionPurpose = "release_selection"

// missingReleaseGateLine names the active-without-gate state and its remedy
// (REQ-CROSS-407): the release is active but no gate on this system satisfies
// the read; re-running the activation here records one without changing the
// release.
func missingReleaseGateLine(slug string) string {
	return fmt.Sprintf("no recorded selection gate on this system; run factory release activate %s --source USER:… here to record one", slug)
}

// releaseSource is the resolved active-release source for the single-active
// case. found=false means one active release exists but no answered
// release_selection gate carries its USER: source.
type releaseSource struct {
	slug  string
	tag   string // the answered gate's USER: source_tag
	gate  string // the external_id the source came from
	found bool
}

// resolveActiveReleaseSource composes the served active_release set with the
// answered release_selection gate. It returns nil for the 0/>1 shapes — those
// have no single active release to bind a source to — so renderSelectionBody
// prints no source line there. gates must be pre-fetched with state=all.
func resolveActiveReleaseSource(payload map[string]any, gates []any) (*releaseSource, error) {
	releases, _ := payload["active_release"].([]any)
	if len(releases) != 1 {
		return nil, nil
	}
	rm, _ := releases[0].(map[string]any)
	slug := str(rm, "slug")
	src := &releaseSource{slug: slug}
	if tag, gate, ok := releaseSelectionGateSource(gates, slug); ok {
		src.tag, src.gate, src.found = tag, gate, true
	}
	return src, nil
}

// releaseSelectionGateSource returns the USER: source_tag of the newest
// approved release_selection gate bound to slug, and the gate it came from.
// REQ-CROSS-407: the read keys on what a gate is, not on its name — purposed
// release_selection, its exact_scope naming release:<slug>, answered with the
// approving option and a USER: source_tag — so a successor under another id
// (GATE-RELEASE-<slug>-<n>) is found and every gate written so far, including
// the one backfilled at the flip, still matches. Among several the newest
// answered_at wins (the external id breaks a tie). A gate scoped to a
// different release never satisfies the active slug (cold-review CR-2).
func releaseSelectionGateSource(gates []any, slug string) (tag, gate string, found bool) {
	var best map[string]any
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		if str(gm, "purpose") != releaseSelectionPurpose {
			continue
		}
		if !gateBindsRelease(gm, slug) {
			continue
		}
		src := str(gm, "source_tag")
		if !strings.HasPrefix(src, "USER:") || !gateApproved(gm) {
			continue
		}
		if best == nil || newerGate(gm, best) {
			best = gm
		}
	}
	if best == nil {
		return "", "", false
	}
	return str(best, "source_tag"), str(best, "external_id"), true
}

// newerGate orders two served gates by answered_at as a time (a served
// timestamp may or may not carry fractional seconds, so text order is not
// time order), then by external_id, so the read is deterministic across
// servers that serve the list in any order.
func newerGate(a, b map[string]any) bool {
	at, aok := parseServedTime(str(a, "answered_at"))
	bt, bok := parseServedTime(str(b, "answered_at"))
	switch {
	case aok && bok && !at.Equal(bt):
		return at.After(bt)
	case aok != bok:
		return aok
	}
	return str(a, "external_id") > str(b, "external_id")
}

func parseServedTime(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	return t, err == nil
}

// gateApproved reports whether a served gate is an authoritative, non-superseded
// approval: its state is exactly "answered" (a SUPERSEDED predecessor keeps its
// answer and answered_at but is never authoritative — PROCESS.md §Gates), and
// the approving option key was chosen ("approve"; request_changes/defer are not).
// state=all returns open, superseded and dismissed gates too, so neither a stray
// source_tag nor a retained answer on a non-authoritative gate may read as the
// active release's decision.
func gateApproved(gm map[string]any) bool {
	if !strings.EqualFold(str(gm, "state"), "answered") {
		return false
	}
	opts, _ := gm["chosen_option_keys"].([]any)
	for _, o := range opts {
		if s, _ := o.(string); s == "approve" {
			return true
		}
	}
	return false
}

// gateBindsRelease reports whether a served gate is bound to slug: its
// exact_scope names release:<slug> (the binding the read keys on,
// REQ-CROSS-407). A gate naming another release never binds. A gate naming no
// release at all — written before the scope token existed — binds only under
// the bare id GATE-RELEASE-<slug>, never as a successor (PR #487 review,
// finding 4).
func gateBindsRelease(gm map[string]any, slug string) bool {
	scope, _ := gm["exact_scope"].([]any)
	namesAnyRelease := false
	for _, s := range scope {
		v, _ := s.(string)
		if v == "release:"+slug {
			return true
		}
		if strings.HasPrefix(v, "release:") {
			namesAnyRelease = true
		}
	}
	return !namesAnyRelease && str(gm, "external_id") == "GATE-RELEASE-"+slug
}

func pullSelection(env *factoryEnv, now time.Time, preGates []any) (usedGates []any, conflict bool, err error) {
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return nil, false, err
	}
	// REQ-CROSS-379 (EPIC-CLI-018): a named --piece the caller does not hold
	// answers 200 with a nil current selection. That is a refusal to state by
	// name, never "No current selection." written over a snapshot that was
	// true a minute ago.
	if _, held := payload["current"].(map[string]any); !held && wsPiece != "" {
		return nil, false, fmt.Errorf("no current selection named %s is held by you — the snapshot is left unchanged; `working-set check` lists what you hold", wsPiece)
	}
	// SR-CROSS-328: store-backed, resolve the single active release's USER:
	// source from its answered release_selection gate so the snapshot the
	// preflight reads carries it — and so the source identity that governs its
	// currency depends on the gate too. File-backed skips the gate read entirely
	// and renders (and hashes) exactly as before.
	var src *releaseSource
	if storeBackedWorkspace(env.Root) {
		usedGates = preGates
		if usedGates == nil {
			usedGates, err = fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
			if err != nil {
				return nil, false, err
			}
		}
		if src, err = resolveActiveReleaseSource(payload, usedGates); err != nil {
			return nil, false, err
		}
	}
	identity, err := selectionIdentity(payload, src)
	if err != nil {
		return nil, false, err
	}
	body := renderSelectionBody(payload, src)
	header := fmt.Sprintf("# WORK-SELECTION — working-set snapshot\n\n- **Snapshot at:** %s\n- **Source store/revision:** %s\n%s%s\n%s%s\n\n",
		now.Format(time.RFC3339), storeRevisionLine(env),
		sourceIdentityKey, identity, writtenBodyKey, sha256Hex([]byte(body)))
	conflict, werr := writeWorkingSetSnapshot(env, selectionFile, header+body)
	return usedGates, conflict, werr
}

func gitHead(root string) string {
	return gitOut(root, "rev-parse", "HEAD")
}

// --- REQ-CROSS-313 (SR-CLI-0084): scope-shaped pull with the authoring render ---

type scopeRecord struct {
	kind    string // "epic" | "system" | "user"
	payload map[string]any
}

// scopeIndex fetches the epics and requirements once and indexes them by
// external id, KEEPING the UR/SR distinction the flat wsIndex collapses — the
// authoring render needs it to pick the right mutable-field set.
func scopeIndex(env *factoryEnv) (map[string]scopeRecord, error) {
	epics, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
	if err != nil {
		return nil, err
	}
	srs, urs, err := fetchRequirementLists(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return nil, err
	}
	idx := map[string]scopeRecord{}
	add := func(list []any, kind string) {
		for _, r := range list {
			if m, ok := r.(map[string]any); ok {
				if id := str(m, "external_id"); id != "" {
					idx[id] = scopeRecord{kind: kind, payload: m}
				}
			}
		}
	}
	add(epics, "epic")
	add(srs, "system")
	add(urs, "user")
	return idx, nil
}

func newContextID(mode string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return mode + "-" + hex.EncodeToString(b)
}

func stringSlice(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// citationRefs renders each served source-citation for the authoring body.
// source_citations are {:array,:map} on the wire — commonly {kind, ref} — so
// stringSlice dropped them, hiding existing citations from author and reviewer
// and, on push, sending bare strings into a map column (#5). Each renders as
// "<kind>: <ref>" so citationMaps can reconstruct the map losslessly; a
// citation carrying only a source_tag or already a bare string is kept as-is.
func citationRefs(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		switch c := it.(type) {
		case string:
			out = append(out, c)
		case map[string]any:
			ref := str(c, "ref")
			if ref == "" {
				ref = str(c, "source_tag")
			}
			if ref == "" {
				continue
			}
			if kind := str(c, "kind"); kind != "" {
				out = append(out, kind+": "+ref)
			} else {
				out = append(out, ref)
			}
		}
	}
	return out
}

// citationMaps turns each authored citation token back into the {kind, ref} map
// shape the source_citations column accepts. A "<kind>: <ref>" token (as
// citationRefs renders) splits on the first ": "; a bare token derives its kind
// from the tag prefix, as traceSources does for verdict sources — so a citation
// edit lands instead of being rejected as a bare string.
func citationMaps(refs []string) []any {
	out := make([]any, 0, len(refs))
	for _, r := range refs {
		kind, ref := "", r
		if i := strings.Index(r, ": "); i > 0 {
			kind, ref = r[:i], r[i+2:]
		} else {
			kind = strings.ToLower(strings.SplitN(r, ":", 2)[0])
		}
		out = append(out, map[string]any{"kind": kind, "ref": ref})
	}
	return out
}

func recordFromPayload(rec scopeRecord, members []string) authoring.Record {
	m := rec.payload
	out := authoring.Record{
		Kind:        rec.kind,
		ExternalID:  str(m, "external_id"),
		Fingerprint: str(m, "fingerprint"),
	}
	for _, f := range authoring.ScalarFields(rec.kind) {
		out.Scalars = append(out.Scalars, authoring.Scalar{Key: f, Value: str(m, f)})
	}
	if rec.kind == "epic" {
		// REQ-CROSS-382: the members block reads the store's declared membership
		// (SRs then URs) so a push computes its ops against what the store holds;
		// the selection's frozen copy is only the fallback for a read that serves
		// neither list.
		out.Members = servedMembers(m, members)
	} else {
		out.SourceCitations = citationRefs(m["source_citations"])
		out.Relations = relationsFromPayload(m["relations"])
		out.Scenarios = scenariosFromPayload(m)
	}
	statusKey := "work_status"
	if rec.kind == "epic" {
		statusKey = "process_status"
	}
	out.Projections = []authoring.Projection{{Name: "status", Content: str(m, statusKey)}}
	if rec.kind == "epic" {
		// impact_assessment and shared_context are structured (map) fields the flat
		// working-set grammar cannot round-trip as editable scalars; render them
		// read-only for review, like acceptance content (#19). Editing them through
		// the working set is a disclosed follow-up.
		for _, f := range []string{"impact_assessment", "shared_context"} {
			if c := mapFieldProjection(m[f]); c != "" {
				out.Projections = append(out.Projections, authoring.Projection{Name: f, Content: c})
			}
		}
	}
	return out
}

// mapFieldProjection renders a structured (map) mutable field's value for a
// read-only projection block: a string verbatim, anything else as indented JSON.
func mapFieldProjection(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		if b, err := json.MarshalIndent(t, "", "  "); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", t)
	}
}

func relationsFromPayload(v any) []authoring.Relation {
	rels, _ := v.([]any)
	out := []authoring.Relation{}
	for _, r := range rels {
		rm, _ := r.(map[string]any)
		out = append(out, authoring.Relation{
			Direction: str(rm, "direction"),
			Target:    str(rm, "target"),
			Authority: str(rm, "authority"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scenariosFromPayload(m map[string]any) []string {
	// The UR read serves inline scenarios; the SR read serves criteria. Both are
	// served STRUCTURED ({given,when,then,statement}) — never a "text" key (#1) —
	// and are carried read-only for review (push refuses acceptance edits rather
	// than wiping the structure with a statement-only replace-set).
	for _, key := range []string{"scenarios", "criteria", "acceptance_scenarios"} {
		if items, ok := m[key].([]any); ok && len(items) > 0 {
			out := []string{}
			for _, it := range items {
				switch v := it.(type) {
				case string:
					out = append(out, v)
				case map[string]any:
					out = append(out, composeScenario(v))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// composeScenario renders a served acceptance criterion for display: its
// statement when it has one, else the assembled GIVEN/WHEN/THEN triple.
func composeScenario(v map[string]any) string {
	if s := str(v, "statement"); s != "" {
		return s
	}
	parts := []string{}
	if g := str(v, "given"); g != "" {
		parts = append(parts, "GIVEN "+g)
	}
	if w := str(v, "when"); w != "" {
		parts = append(parts, "WHEN "+w)
	}
	if th := str(v, "then"); th != "" {
		parts = append(parts, "THEN "+th)
	}
	return strings.Join(parts, " ")
}

func packetFileName(sectionKey string) string {
	switch {
	case sectionKey == "reconnaissance":
		return "10-recon.md"
	case sectionKey == "red_strategy":
		return "30-red-strategy.md"
	case sectionKey == "decisions":
		return "40-decisions.md"
	case strings.HasPrefix(sectionKey, "enrichment:"):
		return "20-enrichment-" + strings.TrimPrefix(sectionKey, "enrichment:") + ".md"
	default:
		return sectionKey + ".md"
	}
}

// reservedPacketCollision reports whether an EXTRA (non-canonical) section key's
// file name would collide with a canonical section's file, aliasing it on pull
// and being misread as the canonical key on push (#25).
func reservedPacketCollision(key string) bool {
	switch {
	case key == "reconnaissance", key == "red_strategy", key == "decisions",
		strings.HasPrefix(key, "enrichment:"):
		return false
	}
	switch packetFileName(key) {
	case "10-recon.md", "30-red-strategy.md", "40-decisions.md":
		return true
	}
	return strings.HasPrefix(packetFileName(key), "20-enrichment-")
}

func scopeItemContent(rec authoring.Record, mode, ctxID string, forReview bool, env *factoryEnv, now time.Time) string {
	body := authoring.Render(rec)
	if forReview {
		body = authoring.RenderReadOnly(rec)
	}
	return fmt.Sprintf("# %s — working-set (%s)\n\n- **Snapshot at:** %s\n- **Source store/revision:** %s\n- **Served fingerprint:** %s\n- **Context:** %s:%s\n\n%s",
		rec.ExternalID, mode, now.Format(time.RFC3339), storeRevisionLine(env), rec.Fingerprint, mode, ctxID, body)
}

// workingSetPullScope resolves the current work selection and materializes its
// scope as an editable directory in the authoring render (SR-CLI-0084). Item
// and section files are editable (read-only under --for-review); the selection
// and findings are projections. Inside a scope directory a local edit awaiting
// push is a draft, not a `.pulled` conflict — so scope files are written whole
// (push carries the server-side 409, SR-CLI-0085).
func workingSetPullScope(env *factoryEnv, forReview bool, now time.Time) error {
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	current, _ := payload["current"].(map[string]any)
	if current == nil {
		return fmt.Errorf("no current work selection — `working-set select` a scope first")
	}
	scopeExt := str(current, "scope_external_id")
	if scopeExt == "" {
		return fmt.Errorf("the current selection names no scope")
	}
	// #1 (external review): a served identifier becomes a path component only after
	// it is proven a single safe component — never let `../` or a separator escape
	// the working-set directory (unsafeSnapshotName already guards single-item pulls).
	if unsafeSnapshotName(scopeExt) {
		return fmt.Errorf("the selection's scope id %q is not a safe path component — refusing to pull", scopeExt)
	}
	members := stringSlice(current["members"])

	idx, err := scopeIndex(env)
	if err != nil {
		return err
	}

	mode := "authoring"
	if forReview {
		mode = "review"
	}
	ctxID := newContextID(mode)

	dir := filepath.Join(env.Root, workingSetDir, scopeExt)
	for _, sub := range []string{"", "members", "packet", "findings"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return err
		}
	}

	if sr, ok := idx[scopeExt]; ok {
		content := scopeItemContent(recordFromPayload(sr, members), mode, ctxID, forReview, env, now)
		if err := atomicWrite(filepath.Join(dir, scopeExt+".md"), []byte(content)); err != nil {
			return err
		}
	}

	for _, mid := range members {
		if unsafeSnapshotName(mid) {
			return fmt.Errorf("selection member id %q is not a safe path component — refusing to pull", mid)
		}
		mr, ok := idx[mid]
		if !ok {
			continue // the selection may name a member the read has not caught up to
		}
		content := scopeItemContent(recordFromPayload(mr, nil), mode, ctxID, forReview, env, now)
		if err := atomicWrite(filepath.Join(dir, "members", mid+".md"), []byte(content)); err != nil {
			return err
		}
	}

	// The scope directory's SELECTION.md is a projection for the authoring/review
	// context, not the preflight snapshot; the active-release source line rides
	// the top-level WORK-SELECTION.md (pullSelection), so pass nil here.
	if err := atomicWrite(filepath.Join(dir, "SELECTION.md"), []byte(renderSelectionBody(payload, nil))); err != nil {
		return err
	}

	// REQ-CROSS-383 (EPIC-CLI-018): scaffold the keys the server's plan check
	// requires — one denominator for the scaffold and the check. A server that
	// serves no facts falls back to the frozen-list derivation, named as such.
	var required []string
	if !forReview {
		required = requiredSectionKeys(env, scopeExt, str(current, "scope_kind"), members, idx)
	}
	if err := pullPacketSections(env, dir, str(current, "scope_kind"), scopeExt, !forReview, required); err != nil {
		return err
	}

	if err := atomicWrite(filepath.Join(dir, "findings", "COLD-REVIEW.md"),
		[]byte(renderFindingsProjection(env, str(current, "scope_kind"), scopeExt))); err != nil {
		return err
	}

	stamp := fmt.Sprintf("# working-set context\n\nmode: %s\ncontext_id: %s\nscope: %s:%s\npulled_at: %s\n",
		mode, ctxID, str(current, "scope_kind"), scopeExt, now.Format(time.RFC3339))
	if err := atomicWrite(filepath.Join(dir, contextFile), []byte(stamp)); err != nil {
		return err
	}

	printSuccess("pulled scope %s → %s (%s context %s)", scopeExt, filepath.Join(workingSetDir, scopeExt), mode, ctxID)
	return nil
}

// requiredPacketKeys is the canonical section keys the phase table requires for
// a scope: the fixed three, plus enrichment:<SR> for each selected system-
// requirement member the store index knows (for a single_sr scope, its own SR).
// A user-requirement member and a member the index does not know get none —
// mirroring the server, which never requires them (REQ-CROSS-332 PD-5).
// requiredSectionKeys returns the section keys the plan check requires for the
// scope as the server serves them (facts.sections.required, REQ-CROSS-383);
// when the server serves none — it predates the key, or the piece cannot be
// read — the frozen-list derivation is the fallback and the caller is told.
func requiredSectionKeys(env *factoryEnv, scopeExt, scopeKind string, members []string, idx map[string]scopeRecord) []string {
	if resp, err := readDeliveryContextFor(env, scopeExt); err == nil && resp.Data.Facts != nil && resp.Data.Facts.Sections.Required != nil {
		return resp.Data.Facts.Sections.Required
	}
	printWarning("the server serves no required section keys for %s — scaffolding from the frozen member list; `process check --phase plan` is the authority", scopeExt)
	return requiredPacketKeys(scopeKind, scopeExt, members, idx)
}

// requiredPacketKeys derives the canonical keys from the frozen selection
// members — the fallback when the server serves no required list.
func requiredPacketKeys(scopeKind, scopeExt string, members []string, idx map[string]scopeRecord) []string {
	keys := []string{"reconnaissance", "red_strategy", "decisions"}
	isSR := func(id string) bool {
		r, ok := idx[id]
		return ok && r.kind == "system"
	}
	if scopeKind == "single_sr" {
		if isSR(scopeExt) {
			keys = append(keys, "enrichment:"+scopeExt)
		}
		return keys
	}
	for _, m := range members {
		if isSR(m) {
			keys = append(keys, "enrichment:"+m)
		}
	}
	return keys
}

func pullPacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, scaffold bool, requiredKeys []string) error {
	scope := scopeKind + ":" + scopeExt
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s", env.SystemID, scope), nil)
	if err != nil {
		// A transport failure is a FAILED pull, not honest absence — swallowing it
		// left a reused scope dir's prior packet files in place, stamped into the
		// new context.
		return err
	}
	if status == 404 {
		// A genuinely absent endpoint (an older server): honest absence. Remove any
		// packet files a prior pull into this reused scope dir left, so stale
		// content is never carried into the new context.
		return os.RemoveAll(filepath.Join(dir, "packet"))
	}
	if status != 200 {
		return serverRefusal("packet-sections read", status, body)
	}
	sections, err := listFromData(body, "packet_sections")
	if err != nil {
		return err
	}
	served := map[string]string{}
	for _, s := range sections {
		sm, _ := s.(map[string]any)
		key := str(sm, "section_key")
		if key == "" {
			continue
		}
		fname := packetFileName(key)
		if unsafeSnapshotName(fname) {
			return fmt.Errorf("packet section key %q maps to an unsafe file name %q — refusing to pull", key, fname)
		}
		if reservedPacketCollision(key) {
			return fmt.Errorf("extra packet section key %q maps to the reserved canonical file name %q — rename the extra section", key, fname)
		}
		if err := atomicWrite(filepath.Join(dir, "packet", fname), []byte(str(sm, "content"))); err != nil {
			return err
		}
		served[key] = str(sm, "content_fingerprint")
	}
	// Record the fingerprint each section was pulled at (#6) so push can carry it
	// as the whole-blob CAS expectation — a concurrent change then 409s instead of
	// being silently overwritten, the same guarantee item files get from their
	// served-fingerprint header. A dotfile, skipped by push's `.md` scan.
	blob, _ := json.Marshal(served)
	if err := atomicWrite(packetFingerprintManifest(dir), blob); err != nil {
		return err
	}

	// REQ-CROSS-332: on an authoring pull, lay down a stub for every canonical
	// section the phase table requires that the store does not serve, so the
	// author sees what the loop expects of the scope just pulled. A stub never
	// overwrites a local file (PD-3) and is not recorded in the served-fingerprint
	// sidecar (PD-4). Not on --for-review (PD-2), not on a 404 (returned above).
	if scaffold {
		for _, key := range requiredKeys {
			if _, isServed := served[key]; isServed {
				continue
			}
			fname := packetFileName(key)
			if unsafeSnapshotName(fname) {
				continue
			}
			path := filepath.Join(dir, "packet", fname)
			if _, err := os.Stat(path); err == nil {
				continue // never overwrite a local file
			}
			if err := atomicWrite(path, []byte(packetStubMarker(key, scopeKind, scopeExt)+"\n")); err != nil {
				return err
			}
		}
	}
	return nil
}

func packetFingerprintManifest(dir string) string {
	return filepath.Join(dir, "packet", ".served-fingerprints.json")
}

func readPacketFingerprints(dir string) map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(packetFingerprintManifest(dir)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func renderFindingsProjection(env *factoryEnv, scopeKind, scopeExt string) string {
	scope := scopeKind + ":" + scopeExt
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/findings?system_id=%d&scope=%s", env.SystemID, scope), nil)
	if err != nil || status != 200 {
		return "# COLD-REVIEW findings\n\n_(unavailable — the findings read is not served on this system yet)_\n"
	}
	findings, _ := listFromData(body, "findings")
	var b strings.Builder
	b.WriteString("# COLD-REVIEW findings (projection — read-only)\n\n")
	if len(findings) == 0 {
		b.WriteString("None recorded.\n")
		return b.String()
	}
	for _, f := range findings {
		fm, _ := f.(map[string]any)
		fmt.Fprintf(&b, "- **%s** [%s/%s] %s\n", str(fm, "external_id"), str(fm, "category"), str(fm, "disposition"), str(fm, "body"))
	}
	return b.String()
}

// workingSetPush parses each item file in the current scope directory, diffs it
// against the freshly-read store snapshot, assembles one atomic `author patch`
// per changed record under the fingerprint recorded at pull, puts each changed
// packet section whole, prints the op plan, and refuses — naming the file — an
// edit it cannot type (SR-CLI-0085). A 409 on one record leaves that file
// unpushed and reports every other file independently; a `--for-review`
// directory is refused outright.
func workingSetPush(env *factoryEnv, dryRun bool) error {
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return err
	}
	current, _ := payload["current"].(map[string]any)
	if current == nil {
		return fmt.Errorf("no current work selection — nothing to push")
	}
	scopeExt := str(current, "scope_external_id")
	scopeKind := str(current, "scope_kind")
	dir := filepath.Join(env.Root, workingSetDir, scopeExt)

	mode, ctxID := readContextStamp(dir)
	if mode == "review" {
		return fmt.Errorf("%s is a --for-review directory (context %s) — review pulls are read-only and never push; pull without --for-review to author", scopeExt, ctxID)
	}

	idx, err := scopeIndex(env)
	if err != nil {
		return err
	}
	members := stringSlice(current["members"])

	type itemFile struct{ path, rel, ext string }
	items := []itemFile{}
	if _, err := os.Stat(filepath.Join(dir, scopeExt+".md")); err == nil {
		items = append(items, itemFile{filepath.Join(dir, scopeExt+".md"), scopeExt + ".md", scopeExt})
	}
	memberSet := map[string]bool{}
	for _, m := range members {
		memberSet[m] = true
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "members"))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		ext := strings.TrimSuffix(e.Name(), ".md")
		// Push only files for the CURRENT selection's members: a pull never removes a
		// member file dropped by a later, smaller reselection, and pushing that stale
		// file would patch a record outside the current scope (#15). A stale file for
		// a REAL member (still in the store index) is skipped silently; a file for an
		// id the store never knew falls through to the unknown-file refusal below.
		if !memberSet[ext] {
			if _, known := idx[ext]; known {
				continue
			}
		}
		items = append(items, itemFile{filepath.Join(dir, "members", e.Name()), filepath.Join("members", e.Name()), ext})
	}

	var plan []string
	var skipped []string
	var conflicts []string
	var pushed []itemFile

	// PLAN PASS — read, parse and diff every item and packet section BEFORE any
	// POST, so a malformed or unreadable later file cannot abort the command after
	// an earlier file's mutation has already committed. No write happens until the
	// whole working set validates.
	type plannedItem struct {
		it   itemFile
		kind string
		fp   string
		p    *diff.Patch
	}
	var plannedItems []plannedItem
	for _, it := range items {
		raw, err := os.ReadFile(it.path)
		if err != nil {
			return err
		}
		base, known := idx[it.ext]
		if !known {
			return fmt.Errorf("%s names %s, which the store does not know — push births nothing; create it first with `author create`", it.rel, it.ext)
		}
		fp := servedFingerprint(string(raw))
		editedRec, perr := authoring.Parse(base.kind, itemBody(string(raw)))
		if perr != nil {
			return fmt.Errorf("%s: cannot parse the authoring grammar (%v)", it.rel, perr)
		}
		var baseRec authoring.Record
		if base.kind == "epic" {
			baseRec = recordFromPayload(base, members)
		} else {
			baseRec = recordFromPayload(base, nil)
		}
		p, ref := diff.Diff(baseRec, editedRec)
		if ref != nil {
			return fmt.Errorf("%s (%s): %s", it.ext, it.rel, ref.Error())
		}
		if p.Empty() {
			continue
		}
		plan = append(plan, describePatch(it.ext, p))
		plannedItems = append(plannedItems, plannedItem{it: it, kind: base.kind, fp: fp, p: p})
	}

	plannedSections, err := planPacketSections(env, dir, scopeKind, scopeExt, &plan, &skipped)
	if err != nil {
		return err
	}

	// A dry run prints the full plan and writes nothing. The re-stamp plan is
	// judged at the CURRENT scope context (the patches above are not applied).
	if dryRun {
		if stale, serr := staleUnchangedSections(env, dir, scopeKind, scopeExt, plannedKeys(plannedSections), wsPushRestamp); serr == nil {
			for _, ps := range stale {
				plan = append(plan, fmt.Sprintf("packet section %s (would re-stamp — unchanged content, stale scope context)", ps.key))
			}
		}
		printPlan(plan, skipped, dryRun)
		return nil
	}

	// APPLY PASS — every file validated; now issue the writes.
	for _, pi := range plannedItems {
		status, resp, perr := postAuthor(env, map[string]any{"action": "patch",
			"record": buildPatchRecord(pi.kind, pi.it.ext, pi.fp, ctxID, pi.p)})
		if perr != nil {
			return fmt.Errorf("%s: %v", pi.it.ext, perr)
		}
		switch {
		case status == 409:
			conflicts = append(conflicts, pi.it.ext)
		case status != 200:
			return serverRefusal(pi.it.ext, status, resp)
		default:
			pushed = append(pushed, pi.it)
		}
	}

	pconf, err := applyPacketSections(env, dir, ctxID, scopeKind, scopeExt, plannedSections)
	if err != nil {
		return err
	}
	conflicts = append(conflicts, pconf...)

	// REQ-CROSS-384 (EPIC-CLI-018): a member patch above moved the packet's scope
	// context, and an earlier push may have left sections behind at an older
	// one. Re-put every unchanged section the server now reports as stale so it
	// carries the current stamp — after the patches, never after a 409 on one
	// (the context that patch would have moved has not moved).
	var restamped []string
	if len(conflicts) == 0 {
		stale, serr := staleUnchangedSections(env, dir, scopeKind, scopeExt, plannedKeys(plannedSections), wsPushRestamp)
		if serr != nil {
			return serr
		}
		rconf, rerr := applyPacketSections(env, dir, ctxID, scopeKind, scopeExt, stale)
		if rerr != nil {
			return rerr
		}
		conflicts = append(conflicts, rconf...)
		for _, ps := range stale {
			restamped = append(restamped, ps.key)
			plan = append(plan, fmt.Sprintf("packet section %s (re-stamped — unchanged content, scope context moved)", ps.key))
		}
	}

	// Re-render each pushed item file from the FRESH store state, so operation
	// markers (a `- withdraw X`) and server-normalized fields do not linger in the
	// body and re-emit on the next push. One re-fetch reflects every patch.
	if len(pushed) > 0 {
		if fresh, ferr := scopeIndex(env); ferr == nil {
			for _, it := range pushed {
				rec, ok := fresh[it.ext]
				if !ok {
					continue
				}
				var r authoring.Record
				if rec.kind == "epic" {
					r = recordFromPayload(rec, members)
				} else {
					r = recordFromPayload(rec, nil)
				}
				_ = atomicWrite(it.path, []byte(scopeItemContent(r, mode, ctxID, false, env, time.Now())))
			}
		}
	}

	printPlan(plan, skipped, dryRun)
	if len(restamped) > 0 {
		fmt.Printf("push: re-stamped %d section(s) whose scope context moved: %s\n", len(restamped), strings.Join(restamped, ", "))
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("%d file(s) conflicted — the store moved since pull: %s; re-pull those and retry", len(conflicts), strings.Join(conflicts, ", "))
	}
	return nil
}

func plannedKeys(planned []plannedSection) map[string]bool {
	keys := map[string]bool{}
	for _, ps := range planned {
		keys[ps.key] = true
	}
	return keys
}

// staleUnchangedSections — REQ-CROSS-384 (EPIC-CLI-018): the filled, served,
// content-unchanged sections whose scope-context stamp is stale (the server
// lists them under facts.sections.missing at the current context), as update
// puts of their current content so the server re-stamps them. With all, every
// such section regardless of staleness (--restamp). A section pushed in this
// same call is excluded — it already carries the new stamp — and so is an
// unfilled stub or a section the store never held (that is authoring, not a
// re-stamp). A server that serves no facts has nothing to judge: nothing.
func staleUnchangedSections(env *factoryEnv, dir, scopeKind, scopeExt string, exclude map[string]bool, all bool) ([]plannedSection, error) {
	stale := map[string]bool{}
	if !all {
		resp, err := readDeliveryContextFor(env, scopeExt)
		if err != nil || resp.Data.Facts == nil {
			return nil, nil
		}
		for _, k := range resp.Data.Facts.Sections.Missing {
			stale[k] = true
		}
		if len(stale) == 0 {
			return nil, nil
		}
	}
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		"packet_sections")
	if err != nil {
		return nil, err
	}
	served := map[string]map[string]any{}
	for _, s := range sections {
		sm, _ := s.(map[string]any)
		served[str(sm, "section_key")] = sm
	}
	pulled := readPacketFingerprints(dir)
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	var out []plannedSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		key := sectionKeyFromFile(e.Name())
		srv := served[key]
		if exclude[key] || srv == nil || (!all && !stale[key]) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "packet", e.Name()))
		if err != nil {
			return nil, err
		}
		content := string(raw)
		if unfilledPacketSection(content, key, scopeKind, scopeExt) {
			continue
		}
		body := stripPacketStub(content, key, scopeKind, scopeExt)
		if str(srv, "content") != body {
			continue // a changed file was planned as an ordinary put already
		}
		expected := pulled[key]
		if expected == "" {
			expected = str(srv, "content_fingerprint")
		}
		out = append(out, plannedSection{key: key, content: body, action: "update", expected: expected})
	}
	return out, nil
}

func readContextStamp(dir string) (mode, ctxID string) {
	raw, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "mode: ") {
			mode = strings.TrimSpace(strings.TrimPrefix(line, "mode: "))
		}
		if strings.HasPrefix(line, "context_id: ") {
			ctxID = strings.TrimSpace(strings.TrimPrefix(line, "context_id: "))
		}
	}
	return mode, ctxID
}

func servedFingerprint(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "- **Served fingerprint:** ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "- **Served fingerprint:** "))
		}
	}
	return ""
}

// itemBody returns the file below its snapshot header (the round-trip domain):
// everything after the blank line that follows the `**Context:**` header bullet.
func itemBody(content string) string {
	i := strings.Index(content, "**Context:**")
	if i < 0 {
		return content
	}
	rest := content[i:]
	j := strings.Index(rest, "\n\n")
	if j < 0 {
		return ""
	}
	return content[i+j+2:]
}

func patchKind(renderKind string) string {
	if renderKind == "epic" {
		return "epic"
	}
	return "requirement"
}

func buildPatchRecord(kind, ext, fp, ctxID string, p *diff.Patch) map[string]any {
	rec := map[string]any{
		"kind":                 patchKind(kind),
		"external_id":          ext,
		"expected_fingerprint": fp,
		"authoring_context_id": ctxID,
	}
	for k, v := range p.Fields {
		rec[k] = v
	}
	if p.CitationsSet {
		rec["source_citations"] = citationMaps(p.Citations)
	}
	if len(p.Relations) > 0 {
		ops := make([]any, len(p.Relations))
		for i, r := range p.Relations {
			ops[i] = map[string]any{"mode": r.Mode, "parents": []any{r.Target}}
		}
		rec["relations"] = ops
	}
	if len(p.Members) > 0 {
		ops := make([]any, len(p.Members))
		for i, m := range p.Members {
			ops[i] = map[string]any{"mode": m.Mode, "member_external_ids": []any{m.Target}}
		}
		rec["members"] = ops
	}
	return rec
}

func postAuthor(env *factoryEnv, body map[string]any) (int, map[string]any, error) {
	body["system_id"] = env.SystemID
	body["actor"] = authorActor
	return env.call("POST", "/api/v1/sync/author", body)
}

// packetStubMarker is the single comment line `working-set pull` writes as an
// unfilled packet stub (REQ-CROSS-332) and `working-set push` strips before
// sending (REQ-CROSS-331). It names the section and the scope so an agent
// opening the file learns what belongs there, and says an unfilled stub is
// never pushed. One line, so the strip is a first-line comparison.
func packetStubMarker(key, scopeKind, scopeExt string) string {
	return fmt.Sprintf(
		"<!-- packet stub — %s for %s:%s — replace this line with the section; an unfilled stub is never pushed -->",
		key, scopeKind, scopeExt)
}

// stripPacketStub returns the body actually pushed: if the first line, once a
// leading byte-order mark and trailing whitespace (spaces, tabs, a carriage
// return) are removed, equals the marker, that line is dropped; otherwise the
// content is unchanged (REQ-CROSS-331 D3). Trailing horizontal whitespace is
// normalised too \u2014 an editor re-saving the stub can leave a trailing space, and
// without this the marker text would be pushed as content (review RUN:2026-09-06).
// The marker never carries trailing whitespace, so a real authored line cannot
// match by accident. A marker anywhere but the first line is ordinary content.
func stripPacketStub(content, key, scopeKind, scopeExt string) string {
	first, rest, _ := strings.Cut(content, "\n")
	norm := strings.TrimRight(strings.TrimPrefix(first, "\uFEFF"), " \t\r")
	if norm == packetStubMarker(key, scopeKind, scopeExt) {
		return rest
	}
	return content
}

// unfilledPacketSection reports whether a packet file carries no authored
// content: blank, whitespace only, or the stub marker alone (REQ-CROSS-331 D3).
func unfilledPacketSection(content, key, scopeKind, scopeExt string) bool {
	return strings.TrimSpace(stripPacketStub(content, key, scopeKind, scopeExt)) == ""
}

// plannedSection is one packet-section whole-blob write the plan pass computed
// but has not yet issued (the validate-all-before-any-write contract).
type plannedSection struct {
	key      string
	content  string
	action   string // "create" | "update"
	expected string // pull-time CAS fingerprint for an update
}

// planPacketSections reads every local packet file and computes which changed vs
// the served snapshot, WITHOUT posting — the plan half of the push's validate-
// all-before-any-write pass. A read error aborts before any write.
func planPacketSections(env *factoryEnv, dir, scopeKind, scopeExt string, plan, skipped *[]string) ([]plannedSection, error) {
	served := map[string]map[string]any{}
	if sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, scopeExt),
		"packet_sections"); err == nil {
		for _, s := range sections {
			sm, _ := s.(map[string]any)
			served[str(sm, "section_key")] = sm
		}
	}
	pulled := readPacketFingerprints(dir)
	entries, _ := os.ReadDir(filepath.Join(dir, "packet"))
	var planned []plannedSection
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		key := sectionKeyFromFile(e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "packet", e.Name()))
		if err != nil {
			return nil, err
		}
		content := string(raw)
		// REQ-CROSS-331: a blank file or an unfilled stub is not authored — skip
		// it and name it, never send an empty whole-blob put and never empty a
		// served section.
		if unfilledPacketSection(content, key, scopeKind, scopeExt) {
			*skipped = append(*skipped, fmt.Sprintf("packet section %s — unfilled stub, not pushed (%s)", key, e.Name()))
			continue
		}
		// A filled stub is sent with the marker line removed; the unchanged-file
		// comparison and the CAS plan run on that stripped body (D3).
		body := stripPacketStub(content, key, scopeKind, scopeExt)
		srv := served[key]
		if srv != nil && str(srv, "content") == body {
			continue
		}
		*plan = append(*plan, fmt.Sprintf("packet section %s (whole-blob put)", key))
		ps := plannedSection{key: key, content: body, action: "create"}
		if srv != nil {
			ps.action = "update"
			// The CAS expectation is the fingerprint recorded at PULL, not the one
			// re-read just now — otherwise a concurrent writer's change would be
			// adopted as the expectation and silently overwritten. Fall back to the
			// current fingerprint only when no pull-time record exists (a hand-made
			// file), which at least preserves prior behaviour.
			ps.expected = pulled[key]
			if ps.expected == "" {
				ps.expected = str(srv, "content_fingerprint")
			}
		}
		planned = append(planned, ps)
	}
	return planned, nil
}

// applyPacketSections posts the planned packet-section writes — the apply half,
// run only after every item and section has validated.
func applyPacketSections(env *factoryEnv, dir, ctxID, scopeKind, scopeExt string, planned []plannedSection) ([]string, error) {
	pulled := readPacketFingerprints(dir)
	conflicts := []string{}
	for _, ps := range planned {
		record := map[string]any{"kind": "packet_section", "scope_kind": scopeKind,
			"scope_external_id": scopeExt, "section_key": ps.key, "content": ps.content,
			"authoring_context_id": ctxID}
		if ps.action == "update" {
			record["expected_fingerprint"] = ps.expected
		}
		status, resp, err := postAuthor(env, map[string]any{"action": ps.action, "record": record})
		if err != nil {
			return conflicts, fmt.Errorf("packet section %s: %v", ps.key, err)
		}
		switch {
		case status == 409:
			conflicts = append(conflicts, "packet:"+ps.key)
		case status != 200:
			return conflicts, serverRefusal("packet section "+ps.key, status, resp)
		default:
			// Refresh the pull-time CAS sidecar with the fingerprint the server just
			// returned, so a second local edit without a re-pull carries the CURRENT
			// fingerprint instead of the stale pull-time one and deterministically
			// 409s.
			if data, ok := resp["data"].(map[string]any); ok {
				if obj, ok := data["packet_section"].(map[string]any); ok {
					if nf := str(obj, "fingerprint"); nf != "" {
						pulled[ps.key] = nf
						writePacketFingerprints(dir, pulled)
					}
				}
			}
		}
	}
	return conflicts, nil
}

func writePacketFingerprints(dir string, m map[string]string) {
	blob, _ := json.Marshal(m)
	_ = atomicWrite(packetFingerprintManifest(dir), blob)
}

func sectionKeyFromFile(name string) string {
	switch name {
	case "10-recon.md":
		return "reconnaissance"
	case "30-red-strategy.md":
		return "red_strategy"
	case "40-decisions.md":
		return "decisions"
	}
	if strings.HasPrefix(name, "20-enrichment-") {
		return "enrichment:" + strings.TrimSuffix(strings.TrimPrefix(name, "20-enrichment-"), ".md")
	}
	return strings.TrimSuffix(name, ".md")
}

func describePatch(ext string, p *diff.Patch) string {
	parts := []string{}
	if len(p.Fields) > 0 {
		keys := make([]string, 0, len(p.Fields))
		for k := range p.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts = append(parts, "fields "+strings.Join(keys, ","))
	}
	if p.CitationsSet {
		parts = append(parts, "source citations")
	}
	for _, r := range p.Relations {
		parts = append(parts, fmt.Sprintf("relation %s %s", r.Mode, r.Target))
	}
	for _, m := range p.Members {
		parts = append(parts, fmt.Sprintf("member %s %s", m.Mode, m.Target))
	}
	return fmt.Sprintf("patch %s: %s", ext, strings.Join(parts, "; "))
}

func printPlan(plan, skipped []string, dryRun bool) {
	if len(plan) == 0 && len(skipped) == 0 {
		printSuccess("push: nothing to do — every file is unchanged")
		return
	}
	// Only unfilled stubs and nothing to push: name them, and say so — not "every
	// file is unchanged", which would hide that a stub was silently left behind
	// (REQ-CROSS-331). A plan of strings cannot tell a skip from an op, so the
	// skip lines and their count are carried separately.
	if len(plan) == 0 {
		for _, l := range skipped {
			fmt.Println("  " + l)
		}
		fmt.Printf("push: nothing to push — %d unfilled packet stub(s) skipped\n", len(skipped))
		return
	}
	head := "push op plan"
	if dryRun {
		head = "push --dry-run op plan (nothing applied)"
	}
	fmt.Println(head + ":")
	for _, l := range plan {
		fmt.Println("  " + l)
	}
	for _, l := range skipped {
		fmt.Println("  " + l)
	}
}

// laneLabel renders a served lane in product words.
func laneLabel(lane string) string {
	if lane == "defect" {
		return "customer-blocking defect"
	}
	return "planned"
}

func workingSetSelect(env *factoryEnv, opts wsSelectOpts, now time.Time) error {
	switch opts.lane {
	case "", "planned", "defect":
	default:
		return fmt.Errorf("--lane %q: expected planned or defect", opts.lane)
	}
	payload := map[string]any{}
	switch {
	case opts.resume:
		if opts.scope == "" {
			return fmt.Errorf("--resume needs the suspended scope's external id")
		}
		payload["resume"] = opts.scope
	case opts.suspend:
		suspend := map[string]any{"reason": opts.reason}
		if opts.target != "" {
			suspend["target"] = opts.target
		}
		payload["suspend"] = suspend
		// the named scope rides along so the server refuses a mismatch
		// instead of pausing whatever happens to be current
		if opts.scope != "" {
			payload["scope_external_id"] = opts.scope
		}
	case opts.putDown:
		// REQ-CROSS-345: put down (close) the caller's own named current piece,
		// recording how it ended. A close, not a take — no scope_external_id.
		if opts.scope == "" {
			return fmt.Errorf("--put-down needs the piece's external id to close")
		}
		payload["close"] = opts.scope
	default:
		if opts.scope == "" {
			return fmt.Errorf("name the scope to select, or use --suspend/--resume/--put-down")
		}
		payload["scope_external_id"] = opts.scope
		payload["scope_kind"] = opts.kind
		// REQ-CROSS-345: naming a piece to replace displaces that own current
		// piece; unnamed, the take adds a holder beside any already held.
		if opts.replaces != "" {
			payload["replaces"] = opts.replaces
		}
		if opts.phase != "" {
			payload["phase"] = opts.phase
		}
		if opts.lane != "" {
			// REQ-CROSS-392: sent only when given, so an older server sees no key.
			payload["lane"] = opts.lane
		}
		if len(opts.members) > 0 {
			payload["members"] = opts.members
		}
		if opts.owner != "" {
			payload["owner"] = opts.owner
		}
		if opts.waitingOn != "" || opts.waitingOnSet {
			// an explicitly empty value posts the key: that is the clear
			// path for a lifted blocker
			payload["waiting_on"] = opts.waitingOn
		}
		fingerprint := opts.fingerprint
		if fingerprint == "" {
			fingerprint = gitHead(env.Root)
		}
		// a workspace without git yields an explicit absence, never an
		// invented value — the key is simply not posted
		if fingerprint != "" {
			payload["fingerprint"] = fingerprint
		}
	}
	if opts.outcome != "" {
		payload["previous_outcome"] = strings.Replace(opts.outcome, "returned=", "returned:", 1)
	}

	status, body, err := env.call("POST",
		fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID), payload)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("", status, body)
	}
	printSuccess("selection recorded: %v", payload)
	return nil
}

func workingSetPull(env *factoryEnv, ids []string, now time.Time) error {
	var items []string
	wantSelection := false
	for _, id := range ids {
		if id == selectionTarget {
			wantSelection = true
			continue
		}
		items = append(items, id)
	}

	var unknown, conflicts, refused []string
	var preGates []any
	if wantSelection {
		g, conflict, err := pullSelection(env, now, nil)
		if err != nil {
			return err
		}
		preGates = g
		if conflict {
			conflicts = append(conflicts, selectionTarget)
			fmt.Printf("✗ CONFLICT %s — local edit preserved; fresh pull at %s.pulled; resolve by hand and re-pull\n",
				selectionFile, filepath.Join(workingSetDir, selectionFile))
		} else {
			printSuccess("pulled selection → %s", filepath.Join(workingSetDir, selectionFile))
		}
		if len(items) == 0 {
			if len(conflicts) > 0 {
				return fmt.Errorf("pull incomplete — conflicts: %s", strings.Join(conflicts, ", "))
			}
			return nil
		}
	}

	index, gates, err := wsIndex(env, workingSetIncludeCandidates, preGates)
	if err != nil {
		return err
	}
	for _, id := range items {
		item, ok := index[id]
		if !ok {
			unknown = append(unknown, id)
			fmt.Printf("✗ %s — not served by %s (unknown external id)\n", id, env.APIURL)
			continue
		}
		if unsafeSnapshotName(id + ".md") {
			refused = append(refused, id)
			fmt.Printf("✗ %s — refused: an external id must be a plain file name, not a path\n", id)
			continue
		}
		conflict, err := writeWorkingSetItem(env, item, gates, now)
		if err != nil {
			return err
		}
		if conflict {
			conflicts = append(conflicts, id)
			fmt.Printf("✗ CONFLICT %s — local edit preserved; fresh pull at %s.md.pulled; resolve by hand and re-pull\n",
				id, filepath.Join(workingSetDir, id))
			continue
		}
		printSuccess("pulled %s → %s", id, filepath.Join(workingSetDir, id+".md"))
	}
	var problems []string
	if len(unknown) > 0 {
		problems = append(problems, "unresolvable: "+strings.Join(unknown, ", "))
	}
	if len(conflicts) > 0 {
		problems = append(problems, "conflicts: "+strings.Join(conflicts, ", "))
	}
	if len(refused) > 0 {
		problems = append(problems, "refused (path-escaping id): "+strings.Join(refused, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("pull incomplete — %s", strings.Join(problems, "; "))
	}
	return nil
}

// --- REQ-CROSS-217: staleness ---

func workingSetCheck(env *factoryEnv, refresh bool, now time.Time) error {
	entries, err := os.ReadDir(filepath.Join(env.Root, workingSetDir))
	if os.IsNotExist(err) {
		printSuccess("no working set materialized — nothing to check")
		return nil
	}
	if err != nil {
		return err
	}
	index, gates, err := wsIndex(env, workingSetIncludeCandidates, nil)
	if err != nil {
		return err
	}
	var stale, vanished []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == selectionFile {
			raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
			if err != nil {
				return err
			}
			payload, err := fetchWorkSelection(env)
			if err != nil {
				return err
			}
			src, err := selectionReleaseSource(env, payload, gates)
			if err != nil {
				return err
			}
			current, err := selectionIdentity(payload, src)
			if err != nil {
				return err
			}
			recorded := headerValue(string(raw), sourceIdentityKey)
			if current != recorded {
				stale = append(stale, selectionFile)
				fmt.Printf("✗ %s — stale (recorded %.12s…, current %.12s…)\n", selectionFile, recorded, current)
			} else {
				printSuccess("current: %s", selectionFile)
			}
			continue
		}
		id := strings.TrimSuffix(name, ".md")
		raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
		if err != nil {
			return err
		}
		recorded := headerValue(string(raw), sourceIdentityKey)
		item, ok := index[id]
		if !ok {
			vanished = append(vanished, id)
			fmt.Printf("? %s — no longer served by the store; file left in place (deletion is yours to decide)\n", id)
			continue
		}
		current, err := sourceIdentityFor(item, gates)
		if err != nil {
			return err
		}
		if current != recorded {
			stale = append(stale, id)
			fmt.Printf("✗ %s — stale (recorded %.12s…, current %.12s…)\n", id, recorded, current)
			continue
		}
		printSuccess("current: %s", id)
	}
	sort.Strings(stale)

	if refresh && len(stale) > 0 {
		for _, id := range stale {
			if id == selectionFile {
				_, conflict, err := pullSelection(env, now, gates)
				if err != nil {
					return err
				}
				if conflict {
					return fmt.Errorf("refresh conflict on %s — local edit preserved", selectionFile)
				}
				printSuccess("refreshed %s", selectionFile)
				continue
			}
			conflict, err := writeWorkingSetItem(env, index[id], gates, now)
			if err != nil {
				return err
			}
			if conflict {
				return fmt.Errorf("refresh conflict on %s — local edit preserved; fresh pull at %s.md.pulled", id,
					filepath.Join(workingSetDir, id))
			}
			printSuccess("refreshed %s", id)
		}
		stale = nil
	}

	var problems []string
	if len(stale) > 0 {
		problems = append(problems, "stale: "+strings.Join(stale, ", "))
	}
	if len(vanished) > 0 {
		problems = append(problems, "vanished from store: "+strings.Join(vanished, ", "))
	}
	if len(problems) > 0 {
		return fmt.Errorf("working set not current — %s", strings.Join(problems, "; "))
	}
	return nil
}

// wsSelectOpts carries what `working-set select` records (REQ-CROSS-220).
type wsSelectOpts struct {
	scope     string
	kind      string
	phase     string
	members   []string
	owner     string
	waitingOn string
	// waitingOnSet distinguishes "flag not given" from an explicit empty
	// value, which posts the key to clear the field.
	waitingOnSet bool
	fingerprint  string
	outcome      string // displaced current's outcome (also serves resume displacement)
	suspend      bool
	reason       string
	target       string
	resume       bool
	// REQ-CROSS-345: a take names the one current piece it displaces (unnamed,
	// it adds a holder); a put-down closes the caller's own named current piece.
	replaces string
	putDown  bool
	// lane is planned work or a customer-blocking defect (REQ-CROSS-392).
	lane string
}

var (
	yourMoveMore     bool
	yourMoveQueue    bool
	yourMoveDomain   string
	yourMoveRelease  string
	yourMoveHook     string
	yourMoveDeadline int
)

var yourMoveCmd = &cobra.Command{
	Use:   "your-move",
	Short: "Regenerate " + yourMoveDir + "/GATES.md and print your personal brief",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Hook mode owns the whole exchange — it reads the agent's stdin payload,
		// prints its envelope (or {}), and never fails the session (§277).
		if yourMoveHook != "" {
			runBriefHook(yourMoveHook, os.Stdin, os.Stdout, time.Duration(yourMoveDeadline)*time.Second, factoryEnvLoad)
			return nil
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return yourMoveWithBrief(env, briefOpts{
			more:    yourMoveMore,
			queue:   yourMoveQueue,
			domain:  yourMoveDomain,
			release: yourMoveRelease,
			now:     time.Now().UTC(),
		}, os.Stdout)
	},
}

var workingSetRefresh bool

// REQ-CROSS-308 (EPIC-CLI-007): a single flag shared by `pull` and `check`,
// read where each calls wsIndex. Off by default — a candidate is opt-in, never
// pulled into the working set by surprise.
var workingSetIncludeCandidates bool

var workingSetCmd = &cobra.Command{
	Use:   "working-set",
	Short: "Pull server state into " + workingSetDir + " as uncommitted shape files",
}

var (
	wsPullScope     bool
	wsPullForReview bool
	// REQ-CROSS-345: names which of the caller's own current pieces a read
	// resolves, carried as ?scope=. Empty reads the sole current (or asks the
	// caller to name one when they hold several).
	wsPiece string
)

var workingSetPullCmd = &cobra.Command{
	Use:   "pull [<external-id>...]",
	Short: "Materialize named items, or the current selection's scope with --scope",
	Args:  cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		if wsPullScope {
			return workingSetPullScope(env, wsPullForReview, time.Now().UTC())
		}
		if wsPullForReview {
			return fmt.Errorf("--for-review applies to a scope pull; add --scope")
		}
		if len(args) == 0 {
			return fmt.Errorf("name at least one external id, or use --scope to pull the current selection")
		}
		return workingSetPull(env, args, time.Now().UTC())
	},
}

var workingSetCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Report stale working-set files; --refresh re-pulls them",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return workingSetCheck(env, workingSetRefresh, time.Now().UTC())
	},
}

var wsPushDryRun bool

// REQ-CROSS-384 (EPIC-CLI-018): --restamp re-puts every filled canonical
// section so the server re-stamps it at the current scope context.
var wsPushRestamp bool

var workingSetPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the current scope directory: item files as atomic patches, packet sections whole",
	Long: `Push the current scope directory. Item files (the epic, each requirement)
go as atomic patches; packet section files go whole, each stamped server-side
with the scope context the packet had when it was pushed; a file whose
content is unchanged is skipped.

A push that patches a member record moves the packet's scope context. The
sections pushed in the same call are stamped after the patch; every other
filled section the server then reports as stale is re-put unchanged so it
carries the current stamp, and the push says "re-stamped N section(s)"
instead of "nothing to do". --restamp re-puts every filled canonical section
regardless. --dry-run prints the op plan (and what would be re-stamped at
the current context) without applying anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return workingSetPush(env, wsPushDryRun)
	},
}

var wsSelectFlags wsSelectOpts
var wsSelectMembers string

var workingSetSelectCmd = &cobra.Command{
	Use:   "select [<scope-external-id>]",
	Short: "Record, advance, suspend, or resume the store-held work selection",
	Long: `One holder per piece of work, several pieces per person. Worked example:

  working-set select EPIC-X --kind epic --members REQ-1,REQ-2 --phase plan
      take EPIC-X; with no --replaces this ADDS a piece to what you hold
  working-set select REQ-Y --kind single_sr --phase plan --lane defect
      take a customer-blocking defect: the store says so on every read
  working-set select EPIC-X --phase cold_review
      advance a piece you hold
  working-set select EPIC-Y --kind epic --replaces EPIC-X --outcome done
      take EPIC-Y and put EPIC-X down in the same move
  working-set select EPIC-X --suspend --reason "waiting on GATE-1" --waiting-on GATE-1
      park it; a suspended piece is claimable by anyone. Only a piece you
      currently hold can be suspended: to change a parked piece's reason,
      --resume it first, then suspend again; the earlier reason stays on
      the closed row
  working-set select EPIC-X --resume
      resume a parked piece
  working-set select EPIC-X --put-down --outcome returned=plan
      close it, stating how it ended

Reads and writes resolve against the authenticated person's pieces; with
several held, name one with --piece. Re-read the selection immediately before
mutating it. A pull with --for-review renders the scope read-only under a
review context — pushes from that directory are refused by design.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		opts := wsSelectFlags
		if len(args) == 1 {
			opts.scope = args[0]
		}
		if wsSelectMembers != "" {
			opts.members = strings.Split(wsSelectMembers, ",")
		}
		opts.waitingOnSet = cmd.Flags().Changed("waiting-on")
		return workingSetSelect(env, opts, time.Now().UTC())
	},
}

func init() {
	f := workingSetSelectCmd.Flags()
	f.StringVar(&wsSelectFlags.kind, "kind", "epic", "scope kind: epic|single_sr")
	f.StringVar(&wsSelectFlags.phase, "phase", "", "current phase")
	f.StringVar(&wsSelectMembers, "members", "", "comma-separated member external ids")
	f.StringVar(&wsSelectFlags.owner, "owner", "", "selection owner")
	f.StringVar(&wsSelectFlags.lane, "lane", "", "planned (default) or defect — a customer-blocking defect is on the clock; process next, your-move and the session brief say so")
	f.StringVar(&wsSelectFlags.waitingOn, "waiting-on", "", "gate id, blocker, or prerequisite")
	f.StringVar(&wsSelectFlags.fingerprint, "fingerprint", "", "frozen-at fingerprint (default: workspace HEAD)")
	f.StringVar(&wsSelectFlags.outcome, "outcome", "", "displaced current selection's outcome: done|obsolete|returned=<phase>")
	f.BoolVar(&wsSelectFlags.suspend, "suspend", false, "suspend the current selection")
	f.StringVar(&wsSelectFlags.reason, "reason", "", "suspension reason (required with --suspend)")
	f.StringVar(&wsSelectFlags.target, "target", "", "suspension target")
	f.BoolVar(&wsSelectFlags.resume, "resume", false, "resume the named suspended scope")
	f.StringVar(&wsSelectFlags.replaces, "replaces", "",
		"the one current piece this take displaces (pair with --outcome); unnamed, the take adds a holder")
	f.BoolVar(&wsSelectFlags.putDown, "put-down", false,
		"put down (close) the named current piece; carry --outcome done|obsolete|returned=<phase>")

	// REQ-CROSS-345: the read is caller-scoped; --piece names which of the
	// caller's own current pieces any read resolves (pull, push, check).
	workingSetCmd.PersistentFlags().StringVar(&wsPiece, "piece", "",
		"when you hold several current selections, name which one a read resolves (carried as ?scope=)")

	workingSetCheckCmd.Flags().BoolVar(&workingSetRefresh, "refresh", false, "re-pull files reported stale")
	for _, c := range []*cobra.Command{workingSetPullCmd, workingSetCheckCmd} {
		c.Flags().BoolVar(&workingSetIncludeCandidates, "include-candidates", false,
			"include DERIVED candidates in the requirements read (materialize a just-authored candidate)")
	}
	workingSetPullCmd.Flags().BoolVar(&wsPullScope, "scope", false,
		"pull the current work selection's scope as an editable directory (authoring render)")
	workingSetPullCmd.Flags().BoolVar(&wsPullForReview, "for-review", false,
		"render the scope read-only for cold review, stamping a review context (with --scope)")
	workingSetPushCmd.Flags().BoolVar(&wsPushRestamp, "restamp", false,
		"re-put every filled canonical section unchanged so the server re-stamps it at the current scope context")
	workingSetPushCmd.Flags().BoolVar(&wsPushDryRun, "dry-run", false,
		"print the op plan without applying anything")
	workingSetCmd.AddCommand(workingSetPullCmd, workingSetCheckCmd, workingSetSelectCmd, workingSetPushCmd)

	ym := yourMoveCmd.Flags()
	ym.BoolVar(&yourMoveMore, "more", false, "show the next five ranked items")
	ym.BoolVar(&yourMoveQueue, "queue", false, "show everything in scope")
	ym.StringVar(&yourMoveDomain, "domain", "", "show only items touching this domain")
	ym.StringVar(&yourMoveRelease, "release", "", "release scope: active|base|all (default active)")
	ym.StringVar(&yourMoveHook, "hook", "", "hook mode: read the agent payload on stdin and emit the brief once per session (value = hook event name)")
	ym.IntVar(&yourMoveDeadline, "deadline", 8, "hook mode: seconds to wait before returning {} rather than holding the turn")

	rootCmd.AddCommand(yourMoveCmd, workingSetCmd)
}
