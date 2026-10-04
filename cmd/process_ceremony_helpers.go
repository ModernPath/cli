package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// errNoFacts is D14: nothing is written on the strength of zero values. A
// server that predates the read serves no facts_state at all — deploy it; a
// live server serves facts_state "unavailable" when no selection is current
// or the gather failed — a different remedy, named as such.
func errNoFacts(piece, state string) error {
	if state != "" {
		return fmt.Errorf("the server serves no delivery facts for %s (facts %s): no current selection holds it, or the facts gather failed — check `process next --piece %s` and `working-set check`; nothing was written", piece, state, piece)
	}
	return fmt.Errorf("this server serves no delivery facts for %s — deploy the server that serves them first (the ceremony verbs never write on a read that carries none)", piece)
}

// resolvePiece finds the caller's current piece that holds `item`: the piece
// itself, or an epic piece whose members include it. --piece names one when
// the caller holds several; the server's "several selections" refusal is
// surfaced as is — unless the item is itself one of the held pieces
// (REQ-CROSS-460), which is then read as its own piece.
func resolvePiece(env *factoryEnv, item, piece string) (string, error) {
	path := fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID)
	if piece != "" {
		path += "&scope=" + url.QueryEscape(piece)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return "", err
	}
	if status == 422 && piece == "" && slices.Contains(stringSlice(body["pieces"]), item) {
		path += "&scope=" + url.QueryEscape(item)
		if status, body, err = env.call("GET", path, nil); err != nil {
			return "", err
		}
	}
	if status != 200 {
		if msg, ok := body["error"].(string); ok && msg != "" {
			return "", errors.New(msg + " — name the piece that holds " + item + " with --piece")
		}
		return "", serverRefusal("work-selection read", status, body)
	}
	current, _ := dataOf(body)["current"].(map[string]any)
	if current == nil {
		return "", fmt.Errorf("no current selection holds %s — `working-set select` the scope first", item)
	}
	scope := str(current, "scope_external_id")
	if scope == item {
		return scope, nil
	}
	for _, m := range stringSlice(current["members"]) {
		if m == item {
			return scope, nil
		}
	}
	// The frozen member list holds the SRs; the stored membership the facts
	// serve also holds the epic's user requirement. Ask the facts before
	// refusing.
	if resp, err := readDeliveryContextFor(env, scope); err == nil && resp.Data.Facts != nil {
		for _, m := range resp.Data.Facts.Members {
			if m.ExternalID == item {
				return scope, nil
			}
		}
	}
	return "", fmt.Errorf("%s is not %s nor one of its members — name the piece that holds it with --piece", item, scope)
}

// freeTraceID walks the <base>, <base>-R2, -R3… series and returns either the
// first unused id (existing == "") or, when a trace in the series already
// passes at `pin`, that trace's id as existing (nothing to record — reuse it)
// together with the record it read, so a caller can ask what that trace's
// evidence already covers (BACKLOG-TOOL-100).
func freeTraceID(env *factoryEnv, base, pin string) (free, existing string, record map[string]any, err error) {
	for n := 1; n <= 50; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-R%d", base, n)
		}
		status, body, err := env.call("GET",
			fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return "", "", nil, err
		}
		if status == 200 {
			gate, _ := dataOf(body)["gate"].(map[string]any)
			if str(gate, "state") == "pass" && str(gate, "fingerprint") == pin {
				return "", id, gate, nil
			}
			continue
		}
		if status == 404 {
			if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
				return id, "", nil, nil
			}
		}
		return "", "", nil, gateShowError(status, body, id)
	}
	return "", "", nil, fmt.Errorf("no free trace id in the %s series", base)
}

// briefBullets maps the PROCESS.md brief labels to the gate's brief keys, in
// the order the shape lists them.
var briefBullets = []struct{ label, key string }{
	{"What", "what"},
	{"Why now", "why_now"},
	{"Changes if approved", "changes_if_approved"},
	{"Risk if wrong", "risk_if_wrong"},
	{"Recommendation", "recommendation"},
}

// parseBriefSection reads the PROCESS.md brief shape from a packet section:
// `- What:`, `- Why now:`, `- Changes if approved:`, `- Risk if wrong:`,
// `- Recommendation:`; continuation lines join the bullet above. Every bullet
// is required — a gate is the decision itself and is born with its brief.
func parseBriefSection(content string) (map[string]any, error) {
	brief := map[string]any{}
	current := ""
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		indented := len(line) > 0 && (line[0] == ' ' || line[0] == '\t')
		// A top-level bullet starts a label; an indented bullet is a sub-point
		// of the bullet above and joins it (review nit 4).
		if strings.HasPrefix(trimmed, "- ") && !indented {
			label := strings.TrimSpace(trimmed[2:])
			matched := false
			for _, b := range briefBullets {
				for _, form := range []string{b.label + ":", "**" + b.label + ":**", "**" + b.label + "**:"} {
					if rest, ok := strings.CutPrefix(label, form); ok {
						brief[b.key] = strings.TrimSpace(rest)
						current = b.key
						matched = true
						break
					}
				}
				if matched {
					break
				}
			}
			if !matched {
				current = ""
			}
			continue
		}
		if current != "" && trimmed != "" {
			brief[current] = strings.TrimSpace(fmt.Sprint(brief[current]) + " " + trimmed)
		}
	}
	var missing []string
	for _, b := range briefBullets {
		if v, _ := brief[b.key].(string); v == "" {
			missing = append(missing, "- "+b.label+":")
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("the brief lacks %s — every bullet of the PROCESS.md brief shape is required", strings.Join(missing, ", "))
	}
	return brief, nil
}

// readBrief returns the gate brief: --brief-file when given, else the named
// packet section of the scope. A brief file is a JSON object or the
// PROCESS.md markdown bullets (REQ-CROSS-446); anything else is refused with
// the parse error, never ignored.
func readBrief(env *factoryEnv, scopeKind, scope, sectionKey, briefFile string) (map[string]any, error) {
	if briefFile != "" {
		raw, err := os.ReadFile(filepath.Clean(briefFile))
		if err != nil {
			return nil, fmt.Errorf("--brief-file: %w", err)
		}
		var brief map[string]any
		if json.Unmarshal(raw, &brief) == nil && len(brief) > 0 {
			return brief, nil
		}
		brief, err = parseBriefSection(string(raw))
		if err != nil {
			return nil, fmt.Errorf("--brief-file %s is neither a JSON brief object nor the markdown brief: %w", briefFile, err)
		}
		return brief, nil
	}
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, url.QueryEscape(scope)),
		"packet_sections")
	if err != nil {
		return nil, err
	}
	for _, sec := range sections {
		m, _ := sec.(map[string]any)
		if str(m, "section_key") == sectionKey {
			brief, err := parseBriefSection(str(m, "content"))
			if err != nil {
				return nil, fmt.Errorf("packet section %s: %w", sectionKey, err)
			}
			return brief, nil
		}
	}
	return nil, fmt.Errorf("no %s packet section for %s and no --brief-file — write packet/%s.md in the PROCESS.md brief shape and working-set push it, or pass --brief-file", sectionKey, scope, sectionKey)
}

// gateIDFree refuses a taken gate id (a gate is opened once; a withdrawn id
// stays reserved) naming the successor id to pass.
func gateIDFree(env *factoryEnv, id string) error {
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
	if err != nil {
		return err
	}
	if status == 200 {
		gate, _ := dataOf(body)["gate"].(map[string]any)
		state := str(gate, "state")
		if err := takenGateError(id, state); err != nil {
			return err
		}
		return fmt.Errorf("gate %s already exists (%s) — a gate is opened once and a withdrawn id stays reserved; pass --gate-id %s-R2 (or the next free -R<n>) for a successor", id, state, id)
	}
	if status == 404 {
		if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
			return nil
		}
	}
	return gateShowError(status, body, id)
}

// takenGateError is the refusal for an id that is open or answered: that gate
// is answered or applied, never rotated past (review round 2, finding 4).
// nil for any other state.
func takenGateError(id, state string) error {
	switch state {
	case "open":
		return fmt.Errorf("gate %s is already open — answer it (Mission Control, or `factory answer %s --options approve --text …`); nothing to open", id, id)
	case "answered":
		return fmt.Errorf("gate %s is already answered — apply it with `author advance … --gate %s` (members first); nothing to open", id, id)
	}
	return nil
}

// freeGateID walks <base>, <base>-R2, -R3… and returns the first id no gate
// holds, with the last taken id in the series as the predecessor — the
// closed or withdrawn gate the successor follows (REQ-CROSS-422,
// SCN-DEMOTE-006: a scope demoted to PROPOSED and re-planned re-enters under
// ENTRY-<scope>-R2 with no by-hand id). An open or answered id in the series
// stops the walk with takenGateError.
func freeGateID(env *factoryEnv, base string) (free, predecessor string, err error) {
	for n := 1; n <= 50; n++ {
		id := base
		if n > 1 {
			id = fmt.Sprintf("%s-R%d", base, n)
		}
		status, body, err := env.call("GET",
			fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(id), env.SystemID), nil)
		if err != nil {
			return "", "", err
		}
		if status == 200 {
			gate, _ := dataOf(body)["gate"].(map[string]any)
			if err := takenGateError(id, str(gate, "state")); err != nil {
				return "", "", err
			}
			predecessor = id
			continue
		}
		if status == 404 {
			if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
				return id, predecessor, nil
			}
		}
		return "", "", gateShowError(status, body, id)
	}
	return "", "", fmt.Errorf("no free id in the %s series after 50 successors", base)
}

// passingColdReview refuses a scope with no independent passing cold-review
// trace at the current packet aggregate.
func passingColdReview(facts *deliveryFacts, scope string) error {
	cr := facts.ColdReview
	if cr.Verdict != "pass" || !cr.Independent || cr.TraceExternalID == "" {
		return fmt.Errorf("no independent passing cold-review trace at the current packet aggregate %s for %s (verdict %s, independent %v) — run rdd-cold-review and record its verdict with `author trace --purpose cold-review`", facts.Aggregate, scope, presentPin(cr.Verdict), cr.Independent)
	}
	return nil
}

// staleServedSections is the missing sections the store serves with content:
// unchanged, but stamped under a scope context that has since moved
// (REQ-CROSS-446). A section the store does not serve, or serves as an
// unfilled stub, is genuinely missing and is not among them.
func staleServedSections(env *factoryEnv, scopeKind, scope string, missing []string) ([]map[string]any, error) {
	sections, err := fetchList(env,
		fmt.Sprintf("/api/v1/sync/packet-sections?system_id=%d&scope=%s:%s", env.SystemID, scopeKind, url.QueryEscape(scope)),
		"packet_sections")
	if err != nil {
		return nil, err
	}
	var out []map[string]any
	for _, sec := range sections {
		m, _ := sec.(map[string]any)
		key, content := str(m, "section_key"), str(m, "content")
		if !slices.Contains(missing, key) || strings.TrimSpace(content) == "" || unfilledPacketSection(content, key, scopeKind, scope) {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// restampSections re-puts each section's SERVED content under its served
// content fingerprint and authoring context, so the store re-stamps it at the
// current scope context; a concurrent edit conflicts instead of being
// overwritten.
func restampSections(env *factoryEnv, scopeKind, scope string, sections []map[string]any) error {
	for _, m := range sections {
		record := map[string]any{"kind": "packet_section", "scope_kind": scopeKind, "scope_external_id": scope,
			"section_key": str(m, "section_key"), "content": str(m, "content"),
			"expected_fingerprint": str(m, "content_fingerprint"), "authoring_context_id": str(m, "authoring_context_id")}
		status, body, err := postAuthor(env, map[string]any{"action": "update", "record": record})
		if err != nil {
			return fmt.Errorf("re-stamping packet section %s: %w", str(m, "section_key"), err)
		}
		if status != 200 {
			return serverRefusal("re-stamping packet section "+str(m, "section_key"), status, body)
		}
	}
	fmt.Printf("re-stamped %d section(s) whose content is unchanged but whose scope context moved: %s\n", len(sections), strings.Join(sectionKeys(sections), ", "))
	return nil
}

func sectionKeys(sections []map[string]any) []string {
	keys := make([]string, 0, len(sections))
	for _, m := range sections {
		keys = append(keys, str(m, "section_key"))
	}
	return keys
}

// selectionKind maps the facts' scope kind to the selection vocabulary.
func selectionKind(factsKind string) string {
	if factsKind == "epic" {
		return "epic"
	}
	return "single_sr"
}

// enteredEpicStates is REQ-CROSS-414's admission set: the owning-epic states
// from which a members-only gate may recover un-entered members — already
// entered and not terminal. The store holds the same list
// (`Core.RDD.PacketFingerprint.members_only_owner_states/0`); it is the
// authority, this copy only decides what the verb sends.
var enteredEpicStates = []string{"TODO", "READY", "IN_PROGRESS", "IN_REVIEW", "BLOCKED"}

func enteredEpic(status string) bool { return slices.Contains(enteredEpicStates, status) }
