package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// --- REQ-CROSS-220: the store-held selection ---

const selectionTarget = "selection"

const selectionFile = "WORK-SELECTION.md"

func fetchWorkSelection(env *factoryEnv) (map[string]any, error) {
	// REQ-CROSS-345: the read is caller-scoped. --piece names which of the
	// caller's own current pieces to resolve, carried as ?scope=.
	return fetchWorkSelectionFor(env, wsPiece)
}

// fetchWorkSelectionFor reads the caller's selection for one named piece
// (empty: the sole current piece). `process enter` reads the piece it is
// entering this way for its reconnaissance revision (SR-CLI-028-002).
func fetchWorkSelectionFor(env *factoryEnv, piece string) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID)
	if piece != "" {
		path += "&scope=" + url.QueryEscape(piece)
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
				fieldOr(rm, "suspended_reason", "—"), selectionHolder(rm),
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

// selectionHolder is the Owner column of a parked row (REQ-CROSS-429): who
// parked it, as the read serves the holder, or `claimable` when the read
// says nobody is recorded — `working-set select <scope> --resume` takes it.
// The free-text owner is a label, not a holder, and never stands in. A read
// that predates the holder field falls back to that label.
func selectionHolder(rm map[string]any) string {
	holder, served := rm["holder"]
	if !served {
		return fieldOr(rm, "owner", "—")
	}
	hm, _ := holder.(map[string]any)
	name, email := str(hm, "name"), str(hm, "email")
	switch {
	case name != "" && email != "":
		return name + " (" + email + ")"
	case name != "":
		return name
	case email != "":
		return email
	}
	// A holder the member list could not name is still a holder — the server
	// refuses a plain resume — so the id shows, never claimable (PR #618
	// review, finding 2).
	if id := numericID(hm["user_id"]); id != "" {
		return "user #" + id
	}
	return "claimable"
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
	// REQ-CROSS-429: a claim is a write on a colleague's parked row — explicit,
	// and only meaningful on a resume.
	if opts.claim && !opts.resume {
		return fmt.Errorf("--claim applies to --resume: `working-set select <scope> --resume --claim` takes over a colleague's parked piece")
	}
	switch {
	case opts.resume:
		if opts.scope == "" {
			return fmt.Errorf("--resume needs the suspended scope's external id")
		}
		payload["resume"] = opts.scope
		if opts.claim {
			payload["claim"] = true
		}
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
		// REQ-CROSS-220/345: scope_kind rides every take. The server owns the
		// state machine and picks the arm — advance in place, or insert — so only
		// it knows which applies: advance_attrs discards the key, while the insert
		// requires it. Withholding it on the strength of a held read taken moments
		// earlier 422s whenever that read goes stale in between. What the caller
		// sees is corrected at the other end, in selectionSummary (BACKLOG-TOOL-10).
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
		if opts.reconRevision != "" {
			// REQ-CROSS-220: unnamed, the key is absent — advance_attrs drops
			// nils, so an advance keeps the revision the take froze.
			payload["recon_revision"] = opts.reconRevision
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
	// REQ-CROSS-220/345: report what the STORE now holds. The old line printed
	// the request payload as a Go map, so a phase advance on a single_sr piece
	// read back "scope_kind:epic" — a value the client had defaulted and the
	// server had discarded.
	row, _ := dataOf(body)["selection"].(map[string]any)
	printSuccess("selection recorded: %s", selectionSummary(row, opts))
	return nil
}

// selectionSummary renders the stored work-selection row the POST answered
// with. REQ-CROSS-220/345: a server that serves no row back is said so rather
// than papered over with the request's own values (BACKLOG-TOOL-10).
func selectionSummary(row map[string]any, opts wsSelectOpts) string {
	named := firstNonEmpty(opts.scope, "the selection")
	if len(row) == 0 {
		return named + " (the server returned no row to read back)"
	}
	var parts []string
	for _, f := range []struct{ label, key string }{
		{"kind", "scope_kind"},
		{"phase", "phase"},
		{"lane", "lane"},
		{"waiting on", "waiting_on"},
		{"outcome", "outcome"},
	} {
		if v := str(row, f.key); v != "" {
			parts = append(parts, f.label+" "+v)
		}
	}
	if status := str(row, "status"); status != "" {
		parts = append(parts, status)
	}
	summary := firstNonEmpty(str(row, "scope_external_id"), named)
	if len(parts) > 0 {
		summary += " (" + strings.Join(parts, " · ") + ")"
	}
	return summary
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
	// REQ-CROSS-429: with --resume, take over a colleague's parked piece.
	claim bool
	// lane is planned work or a customer-blocking defect (REQ-CROSS-392).
	lane string
	// reconRevision is the repository revision the scope's reconnaissance was
	// taken at (REQ-CROSS-220; BACKLOG-TOOL-5); sent only when given, so an
	// advance keeps the stored value.
	reconRevision string
}
