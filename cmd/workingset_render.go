package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/authoring"
)

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
	labels := citationLabels(v)
	if len(labels) == 0 {
		return "—"
	}
	return strings.Join(labels, " · ")
}

// citationLabels names every stored citation for a read-only render, one
// "<kind>: <label>" entry each. The editable list (citationRefs) keeps only
// the citations push can send back, because push sends kind and reference
// only and would drop a typed identity.
func citationLabels(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, it := range items {
		switch c := it.(type) {
		case string:
			out = append(out, c)
		case map[string]any:
			label := citationLabel(c)
			if kind := str(c, "kind"); kind != "" {
				label = kind + ": " + label
			}
			out = append(out, label)
		default:
			out = append(out, unresolvedCitation)
		}
	}
	return out
}

const unresolvedCitation = "Unresolved source"

// citationLabel names a citation in the order the web app does: the typed
// identity (repository, revision and path), else the stored reference, else
// the path, the source file id or the document id; a legacy process source
// falls back to its id. A citation that names its test case shows it after
// the file (SR-RDD-ONBOARD-047).
func citationLabel(c map[string]any) string {
	label := citationFileLabel(c)
	if ref := citationText(c, "test_case_ref"); ref != "" && label != unresolvedCitation {
		return label + " › " + ref
	}
	return label
}

func citationFileLabel(c map[string]any) string {
	repository, revision, path := citationText(c, "repository_key"), citationText(c, "revision"), citationText(c, "path")
	if repository != "" && revision != "" && path != "" {
		return repository + "@" + revision + ":" + path
	}
	for _, key := range []string{"ref", "source_tag"} {
		if v := citationText(c, key); v != "" {
			return v
		}
	}
	keys := []string{"path", "source_file_id", "system_doc_id"}
	if str(c, "kind") == "process_source" {
		keys = append(keys, "id")
	}
	for _, key := range keys {
		if v := citationText(c, key); v != "" {
			return v
		}
	}
	return unresolvedCitation
}

// citationText reads a citation field stored as text or as a number.
func citationText(c map[string]any, key string) string {
	switch v := c[key].(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return v
		}
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
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
		// SR-RDD-ONBOARD-047: a user requirement carries the actor and the
		// intended outcome the publisher wrote (the `intended_use` column).
		if isUR {
			fmt.Fprintf(&b, "- **Actor / outcome:** %s / %s\n",
				servedOr(m, "actor", notServed), servedOr(m, "intended_use", notServed))
		}
		fmt.Fprintf(&b, "- **Priority:** %s\n", servedOr(m, "priority", notServed))
		// The release follows the owner's rule: a served-null release is "—",
		// only a key the read does not carry keeps the marker.
		fmt.Fprintf(&b, "- **Owner / release:** %s / %s\n",
			servedOr(m, "owner", notServed), servedOr(m, "release", notServed))
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
		if item.kind == "system" || item.kind == "user" {
			writeTraceLinks(&b, item.traces)
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

// hasTraceLinks is true when a served traces value holds a link in either
// direction.
func hasTraceLinks(traces map[string]any) bool {
	from, _ := traces["from"].([]any)
	to, _ := traces["to"].([]any)
	return len(from) > 0 || len(to) > 0
}

// writeTraceLinks renders a requirement's served trace links (REQ-CROSS-489),
// read-only: outgoing then incoming, one line per link as
// `<type> <external id or label> — <label> · <link kind> · <authority>`, with
// "(stale)" when the code moved under it. A link whose record has no external
// id (a code file) is named by its label alone. An empty list reads "—"; an
// absent key reads the not-served marker.
func writeTraceLinks(b *strings.Builder, traces map[string]any) {
	b.WriteString("\n### Trace links\n\n")
	if traces == nil {
		b.WriteString(notServed + "\n")
		return
	}
	for i, dir := range []struct{ heading, key, end string }{
		{"Outgoing", "from", "target"},
		{"Incoming", "to", "source"},
	} {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "**%s**\n\n", dir.heading)
		links, _ := traces[dir.key].([]any)
		if len(links) == 0 {
			b.WriteString("—\n")
			continue
		}
		for _, raw := range links {
			link, _ := raw.(map[string]any)
			b.WriteString("- " + traceLinkLine(link, dir.end) + "\n")
		}
	}
	if truncated, _ := traces["truncated"].(bool); truncated {
		b.WriteString("\nShowing the first 50 links per direction; more are stored.\n")
	}
}

func traceLinkLine(link map[string]any, end string) string {
	label := str(link, "label")
	name := str(link, "external_id")
	line := str(link, end+"_type") + " "
	switch {
	case name != "" && label != "":
		line += name + " — " + label
	case name != "":
		line += name
	case label != "":
		line += label
	default:
		line += str(link, end+"_id")
	}
	line += " · " + fieldOr(link, "link_kind", notServed) + " · " + fieldOr(link, "authority", notServed)
	if stale, _ := link["stale"].(bool); stale {
		line += " (stale)"
	}
	return line
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
	// REQ-CROSS-425: a trace gate carries its verdict, evaluator, recording
	// revision, pin and class, scope, purpose and transition.
	if isTraceGate(gm) {
		fmt.Fprintf(b, "- **Verdict:** %s\n", fieldOr(gm, "verdict", "—"))
		who := gateEvaluator(gm)
		if at := str(gm, "evaluated_at"); who != "" && at != "" {
			who += " at " + at
		}
		fmt.Fprintf(b, "- **Evaluated by:** %s\n", orDash(who))
		fmt.Fprintf(b, "- **Recorded at:** %s\n", fieldOr(gm, "application_revision", "—"))
		fmt.Fprintf(b, "- **Pinned to:** %s\n", gatePin(gm))
		fmt.Fprintf(b, "- **Scope:** %s\n", idListOr(gm, "exact_scope", "—"))
		fmt.Fprintf(b, "- **Purpose / transition:** %s / %s\n", fieldOr(gm, "purpose", "—"), fieldOr(gm, "transition", "—"))
	}
	// A served empty list is an absence — `none` — not an unserved field.
	fmt.Fprintf(b, "- **Prerequisites:** %s\n", prerequisitesOr(gm))
	fmt.Fprintf(b, "- **Fingerprint:** %s\n", servedOr(gm, "fingerprint", notServed))
	fmt.Fprintf(b, "- **Application:** %s\n", fieldOr(gm, "applied_state", notServed))
	fmt.Fprintf(b, "- **Predecessor / successor:** %s\n", gateLineage(gm))
	if lines := gateBriefLines(gm); len(lines) > 0 {
		b.WriteString("\n**Brief:**\n\n")
		for _, line := range lines {
			fmt.Fprintf(b, "- %s\n", strings.ReplaceAll(line, "\n", "\n  "))
		}
	}
}

func prerequisitesOr(gm map[string]any) string {
	if _, served := gm["prerequisite_gate_external_ids"]; !served {
		return notServed
	}
	return idListOr(gm, "prerequisite_gate_external_ids", "none")
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// gateAssociated ties a gate to an item by holds ∪ exact scope ∪ id or title
// embedding — substring alone misses hold-attached gates (the
// RQ-268-holds-REQ-PLN-043 pattern), and an UNBOUNDED substring absorbs
// prefix-id neighbors: RQ-15 must not claim RQ-150's gates. REQ-CROSS-427:
// exact-scope membership and whole-token title embedding mirror how the
// server associates a gate with a record (Core.Author.gate_names_record?/4),
// so a member named only in an entry gate's scope, or a question that names
// its subject only in its title, lists under that item's Gates.
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
	for _, s := range stringSlice(gate["exact_scope"]) {
		if s == itemID {
			return true
		}
	}
	if title := str(gate, "title"); title != "" && containsBoundedID(title, itemID) {
		return true
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
//
// REQ-CROSS-489: served trace links join the hash only when a list is
// non-empty, so an untraced snapshot keeps the identity it had before the
// server served the key.
func sourceIdentityFor(item wsItem, gates []any) (string, error) {
	assoc := associatedGates(item, gates)
	canonical := map[string]any{"item": item.payload, "gates": assoc}
	if hasTraceLinks(item.traces) {
		canonical["traces"] = item.traces
	}
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

func scopeItemContent(rec authoring.Record, mode, ctxID string, forReview bool, env *factoryEnv, now time.Time) string {
	body := authoring.Render(rec)
	if forReview {
		body = authoring.RenderReadOnly(rec)
	}
	return fmt.Sprintf("# %s — working-set (%s)\n\n- **Snapshot at:** %s\n- **Source store/revision:** %s\n- **Served fingerprint:** %s\n- **Context:** %s:%s\n\n%s",
		rec.ExternalID, mode, now.Format(time.RFC3339), storeRevisionLine(env), rec.Fingerprint, mode, ctxID, body)
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
