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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
		return nil, fmt.Errorf("server %d: %v", status, body["error"])
	}
	// A 200 without the expected key is a changed envelope, not an empty
	// list — reading it as empty overwrote a correct projection with "queue
	// is clear" and reported every materialized file vanished.
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

func storeRevisionLine(env *factoryEnv) string {
	return fmt.Sprintf("%s · system %d · modernpath %s", env.APIURL, env.SystemID, Version)
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

// wsIndex fetches the two item lists once and indexes them by external id.
// There is no per-id GET on the read surface; an unknown id is a list
// absence, not a 404.
func wsIndex(env *factoryEnv) (map[string]wsItem, []any, error) {
	epics, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
	if err != nil {
		return nil, nil, err
	}
	requirements, err := fetchList(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID), "requirements")
	if err != nil {
		return nil, nil, err
	}
	// REQ-CROSS-219: pull and check read gate HISTORY (state=all, the store's
	// vocabulary). The your-move projection deliberately keeps the default
	// open-only fetch — an answered gate is not pending.
	gates, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d&state=all", env.SystemID), "gates")
	if err != nil {
		return nil, nil, err
	}
	index := map[string]wsItem{}
	for _, e := range epics {
		m, _ := e.(map[string]any)
		if id := str(m, "external_id"); id != "" {
			index[id] = wsItem{id: id, kind: "epic", payload: m}
		}
	}
	for _, r := range requirements {
		m, _ := r.(map[string]any)
		if id := str(m, "external_id"); id != "" {
			index[id] = wsItem{id: id, kind: "requirement", payload: m}
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
	m := item.payload
	var b strings.Builder
	fmt.Fprintf(&b, "## %s — %s\n\n", item.id, fieldOr(m, "title", notServed))
	if item.kind == "epic" {
		fmt.Fprintf(&b, "- **Kind / status:** Epic / %s\n", fieldOr(m, "status", notServed))
		// REQ-CROSS-223: the PROCESS.md lifecycle, distinct from the board axis.
		fmt.Fprintf(&b, "- **Process status:** %s\n", fieldOr(m, "process_status", notServed))
		fmt.Fprintf(&b, "- **Loop status:** upper %s · lower %s\n",
			fieldOr(m, "upper_loop_status", notServed), fieldOr(m, "lower_loop_status", notServed))
		fmt.Fprintf(&b, "- **Owner / release:** %s / %s\n", notServed, fieldOr(m, "release", notServed))
		fmt.Fprintf(&b, "- **Members:** %s\n", idListOr(m, "requirement_external_ids", notServed))
		fmt.Fprintf(&b, "- **Approval:** %s (%s)\n",
			fieldOr(m, "approved_at", notServed), fieldOr(m, "approval_source_tag", notServed))
	} else {
		fmt.Fprintf(&b, "- **Kind / status:** requirement / %s\n", fieldOr(m, "work_status", notServed))
		fmt.Fprintf(&b, "- **Statement / source:** %s / %s\n",
			fieldOr(m, "description", notServed), fieldOr(m, "source_citations", notServed))
		fmt.Fprintf(&b, "- **Context / stage:** %s / %s\n",
			fieldOr(m, "context", notServed), fieldOr(m, "stage", notServed))
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
	}

	b.WriteString("\n### Gates\n")
	found := 0
	for _, g := range gates {
		gm, _ := g.(map[string]any)
		if !gateAssociated(gm, item.id) {
			continue
		}
		found++
		fmt.Fprintf(&b, "\n## GATE %s — %s\n\n", str(gm, "external_id"), str(gm, "title"))
		fmt.Fprintf(&b, "- **Kind:** human / %s\n", str(gm, "kind"))
		// REQ-CROSS-219: the store's state, verbatim. Process-level CLOSED is
		// not a stored state and is never derived.
		fmt.Fprintf(&b, "- **State:** %s\n", fieldUpperOr(gm, "state", notServed))
		if answer := str(gm, "answer"); answer != "" {
			fmt.Fprintf(&b, "- **Verdict / answer:** %s (%s)\n", answer, fieldOr(gm, "source_tag", notServed))
		}
		// A null served field is an absence, not an unserved field — "—"
		// claims nothing (live run 2026-08-20: an OPEN gate's answerer is
		// genuinely null, and the notServed marker overstated the cause).
		fmt.Fprintf(&b, "- **Actor / evaluator:** opener %s / answerer %s\n",
			fieldOr(gm, "opener_kind", "—"), fieldOr(gm, "answerer_kind", "—"))
		// REQ-CROSS-262: all three of these slots used to read «not recorded by
		// store», and all three were false. The prerequisite, predecessor and
		// successor columns exist — they are recorded and simply not served, a
		// different fact with a different marker. The fingerprint is served
		// outright, and it is the value the flip and the advance both reference:
		// telling the reader to go find one they were already holding was the
		// worst of the three.
		fmt.Fprintf(&b, "- **Prerequisites:** %s\n", idListOr(gm, "prerequisite_gate_external_ids", notServed))
		fmt.Fprintf(&b, "- **Fingerprint:** %s\n", servedOr(gm, "fingerprint", notServed))
		fmt.Fprintf(&b, "- **Application:** %s\n", fieldOr(gm, "applied_state", notServed))
		fmt.Fprintf(&b, "- **Predecessor / successor:** %s\n", gateLineage(gm))
	}
	if found == 0 {
		b.WriteString("\nNo gates reference this item at this snapshot.\n")
	}
	return b.String()
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
	status, body, err := env.call("GET", fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("server %d: %v", status, body["error"])
	}
	payload := dataOf(body)
	if _, present := payload["current"]; !present {
		return nil, fmt.Errorf("server response has no work-selection envelope — refusing to read it as empty")
	}
	return payload, nil
}

func selectionIdentity(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return sha256Hex(raw), nil
}

// renderSelectionBody serializes the served selection into the installed
// WORK-SELECTION.md shape. The history section keeps the shape's own Outcome
// vocabulary: rows whose outcome the shape does not enumerate are excluded —
// a suspension episode never ends the selection, and the episode is visible
// in Suspended while it is live.
func renderSelectionBody(payload map[string]any) string {
	var b strings.Builder
	b.WriteString("## Work selection\n\n")

	releases, _ := payload["active_release"].([]any)
	switch len(releases) {
	case 1:
		rm, _ := releases[0].(map[string]any)
		fmt.Fprintf(&b, "- **Active release:** %s (%s)\n", str(rm, "slug"), str(rm, "status"))
	case 0:
		b.WriteString("- **Active release:** NONE ACTIVE — the registry must hold exactly one\n")
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

func pullSelection(env *factoryEnv, now time.Time) (conflict bool, err error) {
	payload, err := fetchWorkSelection(env)
	if err != nil {
		return false, err
	}
	identity, err := selectionIdentity(payload)
	if err != nil {
		return false, err
	}
	body := renderSelectionBody(payload)
	header := fmt.Sprintf("# WORK-SELECTION — working-set snapshot\n\n- **Snapshot at:** %s\n- **Source store/revision:** %s\n%s%s\n%s%s\n\n",
		now.Format(time.RFC3339), storeRevisionLine(env),
		sourceIdentityKey, identity, writtenBodyKey, sha256Hex([]byte(body)))
	return writeWorkingSetSnapshot(env, selectionFile, header+body)
}

func gitHead(root string) string {
	return gitOut(root, "rev-parse", "HEAD")
}

func workingSetSelect(env *factoryEnv, opts wsSelectOpts, now time.Time) error {
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
	default:
		if opts.scope == "" {
			return fmt.Errorf("name the scope to select, or use --suspend/--resume")
		}
		payload["scope_external_id"] = opts.scope
		payload["scope_kind"] = opts.kind
		if opts.phase != "" {
			payload["phase"] = opts.phase
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
		return fmt.Errorf("server %d: %v", status, body["error"])
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
	if wantSelection {
		conflict, err := pullSelection(env, now)
		if err != nil {
			return err
		}
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

	index, gates, err := wsIndex(env)
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
	index, gates, err := wsIndex(env)
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
			current, err := selectionIdentity(payload)
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
				conflict, err := pullSelection(env, now)
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

var workingSetCmd = &cobra.Command{
	Use:   "working-set",
	Short: "Pull server state into " + workingSetDir + " as uncommitted shape files",
}

var workingSetPullCmd = &cobra.Command{
	Use:   "pull <external-id>...",
	Short: "Materialize named epics/requirements with their open gates",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
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

var wsSelectFlags wsSelectOpts
var wsSelectMembers string

var workingSetSelectCmd = &cobra.Command{
	Use:   "select [<scope-external-id>]",
	Short: "Record, advance, suspend, or resume the store-held work selection",
	Args:  cobra.MaximumNArgs(1),
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
	f.StringVar(&wsSelectFlags.waitingOn, "waiting-on", "", "gate id, blocker, or prerequisite")
	f.StringVar(&wsSelectFlags.fingerprint, "fingerprint", "", "frozen-at fingerprint (default: workspace HEAD)")
	f.StringVar(&wsSelectFlags.outcome, "outcome", "", "displaced current selection's outcome: done|obsolete|returned=<phase>")
	f.BoolVar(&wsSelectFlags.suspend, "suspend", false, "suspend the current selection")
	f.StringVar(&wsSelectFlags.reason, "reason", "", "suspension reason (required with --suspend)")
	f.StringVar(&wsSelectFlags.target, "target", "", "suspension target")
	f.BoolVar(&wsSelectFlags.resume, "resume", false, "resume the named suspended scope")

	workingSetCheckCmd.Flags().BoolVar(&workingSetRefresh, "refresh", false, "re-pull files reported stale")
	workingSetCmd.AddCommand(workingSetPullCmd, workingSetCheckCmd, workingSetSelectCmd)

	ym := yourMoveCmd.Flags()
	ym.BoolVar(&yourMoveMore, "more", false, "show the next five ranked items")
	ym.BoolVar(&yourMoveQueue, "queue", false, "show everything in scope")
	ym.StringVar(&yourMoveDomain, "domain", "", "show only items touching this domain")
	ym.StringVar(&yourMoveRelease, "release", "", "release scope: active|base|all (default active)")
	ym.StringVar(&yourMoveHook, "hook", "", "hook mode: read the agent payload on stdin and emit the brief once per session (value = hook event name)")
	ym.IntVar(&yourMoveDeadline, "deadline", 8, "hook mode: seconds to wait before returning {} rather than holding the turn")

	rootCmd.AddCommand(yourMoveCmd, workingSetCmd)
}
