package cmd

// REQ-CROSS-228 (EPIC-CLI-003) — `modernpath author`: the PERMANENT
// intentional-authoring verb (the decided command surface). Once the tracked
// files retire, this is how a session creates a requirement, epic, or gate
// and advances a record — with creation and transition legality enforced by
// the server, never by client discipline. Distinct from the bulk sync
// channel, and accepted for a store-backed system.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

var (
	authorTitle              string
	authorContext            string
	authorStatus             string
	authorBody               string
	authorGateKind           string
	authorGatePurpose        string
	authorGateScope          []string
	authorGateOptions        []string
	authorGateTransition     string
	authorTransFrom          string
	authorTransTo            string
	authorKind               string
	authorRequirementKind    string
	authorTo                 string
	authorExpected           string
	authorGateRef            string
	authorGateFinger         string
	authorGateAnswer         string
	authorGateWithdrawReason string
	authorDecisionRef        string
	authorTraceFingerprint   string
	authorTraceReviewContext string
	authorTraceVerdict       string
	authorTraceSources       []string
	authorTracePrereqs       []string
	authorTraceRevision      string
	// EPIC-CLI-007: `author update`/`author relate` edit and relate an
	// existing requirement. --expected-fingerprint is the record's current
	// content fingerprint, distinct from `author advance --expected` (a
	// work_status), so it gets its own var and never reuses authorExpected.
	authorDescription         string
	authorStage               string
	authorPriority            string
	authorOwner               string
	authorDetail              string
	authorCriteria            string
	authorExpectedFingerprint string
	authorParents             []string
	// SR-CLI-0071: the SR content fields the server's persist step accepts but
	// the CLI had no flags for — so the sanctioned path could not edit them.
	authorBoundary           string
	authorRationale          string
	authorVerificationMethod string
	// REQ-CROSS-458: the small-change lane class, set before the narrow review.
	authorLaneClass string
	// EPIC-CLI-007: `author relate --mode` reaches the server's
	// declare/withdraw/confirm; `author member` authors epic membership.
	authorRelateMode string
	authorMembers    []string
	authorMemberMode string
	// `author update --kind requirement|epic` targets the epic tables too
	// (SR-CLI-0071 scoped epic content); requirement stays the default.
	authorUpdateKind string
	// REQ-CROSS-310 (SR-CLI-0081): `author update` gains --context and repeatable
	// --source (source citations) — the server accepts both; `author gate` gains
	// the brief block PROCESS requires on a human gate, plus recommendation,
	// sources and prerequisite gate ids.
	authorSources               []string
	authorCitationsFile         string
	authorGateBriefWhat         string
	authorGateBriefWhyNow       string
	authorGateBriefChanges      string
	authorGateBriefRisk         string
	authorGateBriefRecommend    string
	authorGateBriefFile         string
	authorGateRecommendation    string
	authorGateRecommendedOption string
	authorGateSources           []string
	authorGatePrereqs           []string
	authorGateSupersedes        string
)

var authorCmd = &cobra.Command{
	Use:   "author",
	Short: "Create or advance a process record on the store, legality server-enforced",
	Long: `Intentional single-record authoring (the permanent write path once the
tracked files retire):

  author requirement <id> --title … --context …   born PROPOSED (or DERIVED)
  author epic <id> --title …                      born in its entry state
  author gate <id> --title … [--gate-kind …]      born open, never answered
  author trace <id> --title … --verdict …          completed machine trace
  author advance <id> --kind … --to … --expected …

Automatic transitions flow; a human-gated transition demands --gate (the
exact ANSWERED gate naming the record) with --gate-fingerprint at its
current identity, or --decision USER:… for DEFERRED. The expected state
makes concurrent advances conflict instead of overwrite.`,
}

func authorEnv() (*factoryEnv, error) { return factoryEnvLoad() }

var authorRequirementCmd = &cobra.Command{
	Use:   "requirement <external-id>",
	Short: "Create a requirement (born DERIVED or PROPOSED — never further)",
	Long: `Create a requirement with its title and bounded context; --kind ur|sr
sets the requirement kind (sr when omitted) and --status DERIVED births a
candidate.

--title and --context are bounded at 255 characters. A value over the bound
is refused as a 422 naming the field and the limit, and nothing is written.
The record's prose is written afterwards with author update --detail, which
is unbounded.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		fields, err := authorRequirementFields()
		if err != nil {
			return err
		}
		return errOnly(authorCreate(env, "requirement", args[0], fields))
	},
}

// REQ-CROSS-307 (EPIC-CLI-007): `author requirement --kind ur|sr` carries the
// UR/SR discriminator as requirement_kind so the post-flip CLI can create a
// user requirement, not only a system one. A distinct selector from `author
// advance --kind requirement|epic`. Absent the flag the field is omitted and
// the server preserves the system default (CODE:apps/core/lib/core/author.ex:302).
func authorRequirementFields() (map[string]any, error) {
	fields := map[string]any{"title": authorTitle, "context": authorContext}
	if authorStatus != "" {
		fields["work_status"] = authorStatus
	}
	switch strings.ToLower(strings.TrimSpace(authorRequirementKind)) {
	case "":
		// no discriminator — the server defaults to a system requirement
	case "ur", "user":
		fields["requirement_kind"] = "user"
	case "sr", "system":
		fields["requirement_kind"] = "system"
	default:
		return nil, fmt.Errorf("--kind %q: expected ur or sr", authorRequirementKind)
	}
	return fields, nil
}

// REQ-CROSS-393 (EPIC-CLI-019): `author backlog` — one backlog, gap or
// tooling record as a single actor-attributed store record, the carrier the
// flip left missing (GAP-017). `author update --kind backlog` edits it against
// its fingerprint; a disposition change carries its --source.
var (
	authorBacklogKind        string
	authorBacklogObserved    string
	authorBacklogWhy         string
	authorBacklogRoute       string
	authorBacklogAffected    string
	authorBacklogGapKind     string
	authorBacklogTraces      string
	authorBacklogConsequence string
	authorBacklogNotes       string
	authorDisposition        string
)

var authorBacklogCmd = &cobra.Command{
	Use:   "backlog <external-id>",
	Short: "Record a backlog, gap or tooling discovery as one store record (born OPEN)",
	Long: `Write one backlog, gap or tooling record — a triage discovery that is
neither a requirement, an epic nor a gate — attributed to you and born OPEN.

The id starts with BACKLOG- or GAP- (BACKLOG-7, BACKLOG-CROSS-0001,
GAP-CLI-004). --kind backlog|gap|tooling; a gap also needs --gap-kind,
--affected-trace and --consequence. The record's fingerprint is printed:
pass it as --expected-fingerprint to 'author update --kind backlog', where
a disposition change (--disposition "ROUTED to <id>") needs its --source.

Examples:
  modernpath author backlog BACKLOG-CROSS-0001 --kind backlog --title "…" --observed "…" --why-unrouted "…"
  modernpath author backlog GAP-CLI-004 --kind gap --title "…" --observed "…" --why-unrouted "…" --gap-kind capability --affected-trace REQ-X --consequence "…"
  modernpath author update BACKLOG-CROSS-0001 --kind backlog --disposition "ROUTED to REQ-X" --source USER:… --expected-fingerprint <fp>`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fields, err := authorBacklogFields()
		if err != nil {
			return err
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return errOnly(authorCreate(env, "backlog", args[0], fields))
	},
}

// authorBacklogFields assembles the record from the flags; a gap without its
// three fields is refused here, before any post, naming the flags.
func authorBacklogFields() (map[string]any, error) {
	kind := strings.ToLower(strings.TrimSpace(authorBacklogKind))
	switch kind {
	case "backlog", "gap", "tooling":
	default:
		return nil, fmt.Errorf("--kind %q: expected backlog, gap or tooling", authorBacklogKind)
	}
	fields := map[string]any{"backlog_kind": kind, "title": authorTitle}
	setIf := func(field, val string) {
		if strings.TrimSpace(val) != "" {
			fields[field] = val
		}
	}
	setIf("observation", authorBacklogObserved)
	setIf("why_unrouted", authorBacklogWhy)
	setIf("candidate_route", authorBacklogRoute)
	setIf("notes_md", authorBacklogNotes)
	// REQ-CROSS-432: a backlog or tooling record has no gap fields; the server
	// refuses them too. Checked before the vocabulary, so any gap flag on such
	// a record is refused as one.
	if kind != "gap" {
		var given []string
		for _, f := range []struct{ flag, value string }{
			{"--gap-kind", authorBacklogGapKind},
			{"--affected-trace", authorBacklogTraces},
			{"--consequence", authorBacklogConsequence},
		} {
			if strings.TrimSpace(f.value) != "" {
				given = append(given, f.flag)
			}
		}
		if len(given) > 0 {
			return nil, fmt.Errorf("%s: for a gap only — this record is --kind %s", strings.Join(given, ", "), kind)
		}
	}
	// REQ-CROSS-423: the gap kind is checked against the store's vocabulary
	// here, naming the set, rather than posted for the server to refuse.
	if authorBacklogGapKind != "" && !contains(backlogGapKinds, authorBacklogGapKind) {
		return nil, fmt.Errorf("--gap-kind %q: expected %s", authorBacklogGapKind, strings.Join(backlogGapKinds, "|"))
	}
	setIf("gap_kind", authorBacklogGapKind)
	setIf("consequence", authorBacklogConsequence)
	if ids := splitIDs(authorBacklogAffected); len(ids) > 0 {
		fields["affected_external_ids"] = ids
	}
	traces := splitIDs(authorBacklogTraces)
	if len(traces) > 0 {
		fields["affected_trace_external_ids"] = traces
	}
	if kind == "gap" {
		var missing []string
		if authorBacklogGapKind == "" {
			missing = append(missing, "--gap-kind")
		}
		if len(traces) == 0 {
			missing = append(missing, "--affected-trace")
		}
		if authorBacklogConsequence == "" {
			missing = append(missing, "--consequence")
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("a gap needs %s", strings.Join(missing, ", "))
		}
	}
	return fields, nil
}

func splitIDs(raw string) []any {
	var out []any
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

var authorEpicCmd = &cobra.Command{
	Use:   "epic <external-id>",
	Short: "Create an epic in its entry state",
	Long: `Create an epic with a title and, optionally, a description. --title is
capped at 255 characters server-side — a longer one is refused and nothing
is written; --description is free text and takes the epic's prose. The
packet's own sections are authored afterwards (working-set pull --scope,
then working-set push).`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return errOnly(authorCreate(env, "epic", args[0], authorEpicFields()))
	},
}

// An epic is born with a title and, optionally, a description — the server's
// create path already accepts `description` (SR-CLI-0071); the CLI just sends it.
func authorEpicFields() map[string]any {
	fields := map[string]any{"title": authorTitle}
	if authorDescription != "" {
		fields["description"] = authorDescription
	}
	return fields
}

var authorGateCmd = &cobra.Command{
	Use:   "gate <external-id>",
	Short: "Open a decision gate (born open; answers arrive through the answer machinery)",
	Long: `Open a human gate. A gate is opened once: it cannot be edited after birth,
only withdrawn (author gate-withdraw) and re-opened under a new id.

Governed gates, by --purpose:
  entry        --prerequisite must name a passing cold-review trace pinned to
               the scope's current packet aggregate (author trace
               --purpose cold-review --fingerprint <full aggregate>); the gate
               is refused otherwise. Scope: the epic plus every member still
               in the FROM state, or the single requirement.
  completion   --prerequisite must name a passing completion trace at the same
               aggregate, and every named item must be IN_REVIEW or DONE with
               posted passing evidence (factory evidence --pass) at the
               delivered revision, or the gate refuses "IN_REVIEW or DONE —
               not yet". Scope: the epic, every SR member the DONE advance
               will move, AND the epic's user requirement (UR-<suffix> for
               EPIC-<suffix>) while it is IN_REVIEW — the UR is a member even
               though working-set pull <epic> lists only SRs under Members.
               Leave one out and the server refuses with "completion gate
               names <epic> but omits members not yet DONE: <ids>". The epic
               and the UR each need evidence of their own; a run on the SRs
               does not cover them. OBSOLETE and DEFERRED members are not
               moved and need not be named.

Order: record the trace first, then open the gate naming it. A governed gate
is answered by a review submission — Mission Control, or factory answer, which
attaches the review — and the answer moves nothing until author advance
applies it, members before the epic, with --gate-fingerprint at the gate's
current content-shadow hash.

Retiring a stuck gate: a governed gate born without a passing prerequisite,
or whose prerequisite has gone stale, cannot be approved, and a gate is
opened once — its id is not reusable, a withdrawn id stays reserved. Record
the missing trace, then open a successor under a NEW id with
--prerequisite <trace> --supersedes <old>: the server retires the old
decision in the same write and links both ways (it must still be open or
answered). author gate-withdraw <old> --reason … dismisses one that gets no
successor.

Every gate carries a brief (--brief-file, or the --brief-* flags) and its
structured options (--option key=label); an approving answer rides the key.
The transition is --from/--to (or one quoted --transition FROM->TO); omitted,
an entry gate infers PROPOSED->TODO and a completion gate IN_REVIEW->DONE —
an as-built scope enters with --from PENDING_VERIFICATION --to TODO. A
fragment is refused before any write.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		// A repeatable flag the caller never passed carries nothing — read it
		// only where cobra recorded it as set, so a reset default can never
		// read back as a user-supplied value.
		if !cmd.Flags().Changed("scope") {
			authorGateScope = nil
		}
		if !cmd.Flags().Changed("option") {
			authorGateOptions = nil
		}
		// Refuse a malformed option flag here rather than dropping it: a gate
		// silently born without its options is one the server refuses later,
		// or worse, one nobody can answer approvingly.
		if _, err := parseGateOptions(authorGateOptions); err != nil {
			return err
		}
		// REQ-CROSS-377: the transition is composed or inferred here and never
		// posted as a fragment.
		transition, err := resolveTransition("gate", authorGatePurpose, authorGateTransition, authorTransFrom, authorTransTo)
		if err != nil {
			return err
		}
		inferred := transition != "" && authorGateTransition == "" && authorTransFrom == ""
		authorGateTransition = transition
		return withEntrySourceHint(errOnly(authorCreate(env, "gate", args[0], authorGateFields())), inferred)
	},
}

// REQ-CROSS-356 (EPIC-CLI-013): withdraw a mistakenly-opened standalone human
// gate. A reason is required — it is the audit — and the server dismisses the
// gate and emits a gate_withdrawn event, refusing a trace gate, an applied or
// terminal gate, or a gate still a live prerequisite of another open decision.
var authorGateWithdrawCmd = &cobra.Command{
	Use:   "gate-withdraw <external-id>",
	Short: "Withdraw a mistakenly-opened standalone human gate (dismiss it; a reason is required)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return authorGateWithdraw(env, args[0], authorGateWithdrawReason)
	},
}

func authorGateWithdraw(env *factoryEnv, externalID, reason string) error {
	data, err := authorPost(env, map[string]any{
		"action": "withdraw",
		"record": map[string]any{"kind": "gate", "external_id": externalID},
		"reason": reason,
	})
	if err != nil {
		return err
	}
	if row, ok := data["gate"].(map[string]any); ok {
		printSuccess("withdrew gate %s (%s)", str(row, "external_id"), str(row, "state"))
	}
	return nil
}

// REQ-CROSS-259: the store-backed declaration's gate carries a purpose, the
// exact scope naming its system, and the structured options its approving
// answer rides — the server refuses a declaration gate born without them, and
// a gate the server refuses is one nobody can clear the declaration with.
// REQ-CROSS-258 reads `transition` from the same take for its binding.
func authorGateFields() map[string]any {
	fields := map[string]any{"title": authorTitle, "gate_kind": authorGateKind}
	if authorBody != "" {
		fields["body_md"] = authorBody
	}
	if authorGatePurpose != "" {
		fields["purpose"] = authorGatePurpose
	}
	if authorGateTransition != "" {
		fields["transition"] = authorGateTransition
	}
	if len(authorGateScope) > 0 {
		fields["exact_scope"] = authorGateScope
	}
	if options, err := parseGateOptions(authorGateOptions); err == nil && len(options) > 0 {
		fields["options"] = options
	}
	// REQ-CROSS-310 (SR-CLI-0081): the brief block + its structured neighbours
	// PROCESS requires on a human gate. An entry/completion gate is refused by the
	// server without a brief; every other human gate still gets the server fill.
	if brief := authorGateBrief(); brief != nil {
		fields["brief"] = brief
	}
	if authorGateRecommendation != "" {
		fields["recommendation"] = authorGateRecommendation
	}
	if authorGateRecommendedOption != "" {
		fields["recommended_option_key"] = authorGateRecommendedOption
	}
	if len(authorGateSources) > 0 {
		fields["sources"] = sourceCitations(authorGateSources)
	}
	if len(authorGatePrereqs) > 0 {
		fields["prerequisite_gate_external_ids"] = authorGatePrereqs
	}
	// REQ-CROSS-353: the decision this one supersedes — the server retires it in
	// the same write that births this gate, linking both ways.
	if authorGateSupersedes != "" {
		fields["predecessor_external_id"] = authorGateSupersedes
	}
	return fields
}

// authorGateBrief assembles the plain-language brief from --brief-file (a JSON
// object) or the individual --brief-* flags. Returns nil when none is given, so
// a non-packet gate still reaches the server's D-DEC-1 fill path.
func authorGateBrief() map[string]any {
	if authorGateBriefFile != "" {
		if data, err := os.ReadFile(filepath.Clean(authorGateBriefFile)); err == nil {
			var brief map[string]any
			if json.Unmarshal(data, &brief) == nil && len(brief) > 0 {
				return brief
			}
		}
	}

	brief := map[string]any{}
	put := func(key, val string) {
		if val != "" {
			brief[key] = val
		}
	}
	put("what", authorGateBriefWhat)
	put("why_now", authorGateBriefWhyNow)
	put("changes_if_approved", authorGateBriefChanges)
	put("risk_if_wrong", authorGateBriefRisk)
	put("recommendation", authorGateBriefRecommend)
	if len(brief) == 0 {
		return nil
	}
	return brief
}

// `--option key=label`. The key is the answer's carrier, so a flag with no key
// would build a gate that can only ever be answered in free text.
func parseGateOptions(raw []string) ([]map[string]any, error) {
	options := make([]map[string]any, 0, len(raw))
	for _, spec := range raw {
		key, label, found := strings.Cut(spec, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("--option %q: expected key=label — the option key is what an approving answer carries", spec)
		}
		options = append(options, map[string]any{"key": key, "label": strings.TrimSpace(label)})
	}
	return options, nil
}

var authorAdvanceCmd = &cobra.Command{
	Use:   "advance [<external-id>]",
	Short: "Advance a requirement or epic under server-enforced transition legality",
	Long: `Apply a lifecycle transition. Automatic transitions (TODO -> IN_PROGRESS ->
IN_REVIEW) are normally applied by process reconcile from trace proofs; this
verb applies the human-gated ones and any transition the server allows.

A human-gated transition names the exact ANSWERED gate (--gate), echoes the
answer it rides (--gate-answer: the option key, its label, or the stored
answer text) and pins to the gate's current content-shadow hash
(--gate-fingerprint) — the Fingerprint: line of working-set pull <gate-id>,
also served by factory gates <gate-id> --json as fingerprint: the content
shadow's hash, falling back to the gate row's own when no shadow exists.
That read also carries content_fingerprint, the row's column — do not pass
that one. A stale hash conflicts instead of applying.

With --gate the verb reads the gate itself: an omitted --gate-fingerprint is
the gate's fingerprint, an omitted --to and --expected are the TO and FROM
states of the gate's transition, and an omitted --gate-answer is approve when
the gate's chosen options are exactly approve (any other answer must be
echoed). --kind is read from the record. Explicit flags always win. A gate
that carries no transition is refused unless --to and --expected are given.

Members before the epic: apply each member the gate names, then the epic;
the gate closes and reads applied once every named scope has its
application. The server does not enforce that order — the recipe does, so
an epic advanced first leaves its members to remember. --gate <GATE> with no
id does it in one call: every record the gate's exact scope names that is
still in the FROM state, members first, then the epic when the gate names
it; it prints each result, skips a record already past, and stops before the
epic when a member is refused.

DEFERRED needs --decision USER:<date>:<why> instead of a gate. --expected is
the state the transition starts from (the gate's FROM state, never a fresh
read of the record), so two concurrent advances conflict rather than
overwrite.`,
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 && authorGateRef == "" {
			return fmt.Errorf("name the record to advance, or --gate <GATE> to advance every record its scope names")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return runAuthorAdvance(env, cmd, args)
	},
}

// runAuthorAdvance fills what the caller left out from one gate read and one
// item read (REQ-CROSS-444), following authorDemoteApply's gate read: the
// gate's fingerprint, its transition's FROM and TO, and an approve answer;
// the record's kind. Explicit flags win. Without an id it advances the gate's
// exact scope, members first.
func runAuthorAdvance(env *factoryEnv, cmd *cobra.Command, args []string) error {
	to, expected, gateFP, answer := authorTo, authorExpected, authorGateFinger, authorGateAnswer
	var scope []string
	if authorGateRef != "" && (len(args) == 0 || to == "" || expected == "" || gateFP == "" || answer == "") {
		status, body, err := env.call("GET",
			fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(authorGateRef), env.SystemID), nil)
		if err != nil {
			return err
		}
		if status != 200 {
			return gateShowError(status, body, authorGateRef)
		}
		gate, _ := dataOf(body)["gate"].(map[string]any)
		if gateFP == "" {
			// The shadow value advance guards on; the row's content_fingerprint
			// only for a server that predates it (as authorDemoteApply reads it).
			gateFP = str(gate, "fingerprint")
			if gateFP == "" {
				gateFP = str(gate, "content_fingerprint")
			}
		}
		if keys := stringSlice(gate["chosen_option_keys"]); answer == "" && len(keys) == 1 && keys[0] == "approve" {
			answer = "approve"
		}
		if len(args) == 0 || to == "" || expected == "" {
			from, gateTo, ok := strings.Cut(str(gate, "transition"), "->")
			if !ok {
				return fmt.Errorf("%s carries no FROM->TO transition — pass --to and --expected with the record id; nothing was advanced", authorGateRef)
			}
			if expected == "" {
				expected = strings.TrimSpace(from)
			}
			if to == "" {
				to = strings.TrimSpace(gateTo)
			}
		}
		scope, _ = normalizeScopeTokens(gate["exact_scope"])
	}

	ids := args
	if len(args) == 0 {
		ids = scope
	}
	var items map[string]wsItem
	if len(args) == 0 || !cmd.Flags().Changed("kind") {
		var err error
		if items, err = fetchDirectItems(env, ids, false, false); err != nil {
			return err
		}
	}
	kindOf := func(id string) string {
		if cmd.Flags().Changed("kind") {
			return authorKind
		}
		if item, ok := items[id]; ok && item.kind == "epic" {
			return "epic"
		}
		return "requirement"
	}
	advance := func(id string) error {
		return authorAdvance(env, kindOf(id), id, to, expected, authorGateRef, gateFP, answer, authorDecisionRef)
	}
	if len(args) == 1 {
		return advance(args[0])
	}

	// The gate's whole scope: members in scope order, then the epic. A member
	// already past the transition is done; any other member that cannot
	// advance keeps the epic where it is (PR #694 review, #9).
	var members, epics, held []string
	for _, id := range scope {
		item, ok := items[id]
		if !ok {
			printWarning("skipped %s: not a record in this system", id)
			held = append(held, id+" (not a record in this system)")
			continue
		}
		current := str(item.payload, "work_status")
		if item.kind == "epic" {
			current = str(item.payload, "process_status")
		}
		if current != expected {
			printInfo("skipped %s: %s, not %s", id, presentPin(current), expected)
			if item.kind != "epic" && !pastTransition(current, to) {
				held = append(held, fmt.Sprintf("%s (%s, not %s)", id, presentPin(current), expected))
			}
			continue
		}
		if item.kind == "epic" {
			epics = append(epics, id)
		} else {
			members = append(members, id)
		}
	}
	if len(members) == 0 && len(epics) == 0 && len(held) == 0 {
		printInfo("nothing to advance: no record %s names is in %s", authorGateRef, expected)
		return nil
	}
	for i, id := range members {
		if err := advance(id); err != nil {
			if rest := append(members[i+1:], epics...); len(rest) > 0 {
				return fmt.Errorf("%s: %w\n  stopped; not advanced: %s", id, err, strings.Join(rest, ", "))
			}
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	if len(held) > 0 {
		if len(epics) > 0 {
			return fmt.Errorf("%s not advanced: %d member(s) of %s did not advance — %s; move them to %s (or past it) and run the same command again",
				strings.Join(epics, ", "), len(held), authorGateRef, strings.Join(held, "; "), expected)
		}
		return fmt.Errorf("%d record(s) %s names did not advance — %s", len(held), authorGateRef, strings.Join(held, "; "))
	}
	for _, id := range epics {
		if err := advance(id); err != nil {
			return fmt.Errorf("%s: %w", id, err)
		}
	}
	return nil
}

// workLifecycle is the main line a requirement moves along.
var workLifecycle = []string{"PROPOSED", "TODO", "IN_PROGRESS", "IN_REVIEW", "DONE"}

// pastTransition reports whether a member in status current has already made
// the move to `to`: it is at `to`, later on the main line, or OBSOLETE.
func pastTransition(current, to string) bool {
	if current == to || current == "OBSOLETE" {
		return true
	}
	ci, ti := slices.Index(workLifecycle, current), slices.Index(workLifecycle, to)
	return ci >= 0 && ti >= 0 && ci > ti
}

var authorTraceCmd = &cobra.Command{
	Use:   "trace <external-id>",
	Short: "Record an immutable PASS/FAIL/STALE trace gate at an exact fingerprint",
	Long: `Record a machine trace. Traces are immutable: a trace pinned to the wrong
fingerprint never matches and can never be removed, so read the pin from the
tool, never from memory.

Which pin each --purpose takes — read from the store when --fingerprint is
omitted, validated when given (a display prefix is refused, the wrong class is
refused by name, a full hash matching nothing is recorded with a warning):
  cold-review, entry, completion   the scope's full packet aggregate — the full
                                   packet_fingerprint line of process next -v
                                   or process check ... -v
  lower                            the requirement's content hash — the
                                   Fingerprint: line of working-set pull <SR>
The third value, the process revision, is never a trace pin.

What each purpose feeds: a cold-review trace is the entry gate's required
prerequisite (plan->entry); a lower trace lets process reconcile apply
IN_PROGRESS -> IN_REVIEW; a completion trace is the completion gate's
required prerequisite (IN_REVIEW->DONE). PROPOSED->TODO rides the answered
entry gate through author advance — an entry-purpose trace is accepted but
no phase check reads it. The transition is --from/--to (or one quoted
--transition FROM->TO); omitted, it is inferred from the purpose (cold-review
plan->entry, entry PROPOSED->TODO, lower build->verify, completion
IN_REVIEW->DONE). A fragment — what the shell leaves of an unquoted arrow —
is refused before any write; the server refuses it too.

--scope takes bare ids (EPIC-X, REQ-X), never kind:id. A cold-review trace
requires an explicit --review-context from an immutable review snapshot created
by working-set pull --scope --for-review; its aggregate is the trace pin, and
its context and digest are added to body_md. --review-context is rejected for
other purposes. Purposes the phase checks recognise:
cold-review, entry, lower, completion.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("review-context") && authorGatePurpose != "cold-review" {
			return fmt.Errorf("--review-context is only valid with --purpose cold-review; nothing was written")
		}
		if authorGatePurpose == "cold-review" && strings.TrimSpace(authorTraceReviewContext) == "" {
			return fmt.Errorf("--purpose cold-review requires --review-context from `working-set pull --scope --for-review`; nothing was written")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("scope") {
			authorGateScope = nil
		}
		if !cmd.Flags().Changed("source") {
			authorTraceSources = nil
		}
		if !cmd.Flags().Changed("prerequisite") {
			authorTracePrereqs = nil
		}
		revision := authorTraceRevision
		if revision == "" {
			revision = gitHead(env.Root)
		}
		// REQ-CROSS-377: composed from --from/--to, validated, or inferred from
		// the purpose — a fragment never reaches the immutable record.
		transition, err := resolveTransition("trace", authorGatePurpose, authorGateTransition, authorTransFrom, authorTransTo)
		if err != nil {
			return err
		}
		// REQ-CROSS-376: the pin is read from the store by purpose, or validated
		// when given — never posted as a display prefix or the wrong class.
		scope := authorGateScope
		if norm, ok := normalizeScopeTokens(scope); ok {
			scope = norm
		}
		pin := ""
		var warnings []string
		var selectedReview *selectedReviewSnapshot
		if authorGatePurpose == "cold-review" && authorTraceReviewContext != "" {
			selectedReview, err = resolveReviewSnapshot(env, authorTraceReviewContext, scope, authorTraceFingerprint)
			if err != nil {
				return err
			}
			pin = selectedReview.Manifest.AggregateFingerprint
		} else {
			pin, warnings, err = resolveTracePin(env, authorGatePurpose, scope, authorTraceFingerprint)
			if err != nil {
				return err
			}
		}
		for _, w := range warnings {
			printWarning("%s", w)
		}
		if authorTraceFingerprint == "" && pin != "" {
			printInfo("pinned to the %s: %s", pinClass(authorGatePurpose), pin)
		}
		fields := map[string]any{
			"title":                          authorTitle,
			"purpose":                        authorGatePurpose,
			"transition":                     transition,
			"exact_scope":                    authorGateScope,
			"fingerprint":                    pin,
			"verdict":                        strings.ToUpper(authorTraceVerdict),
			"sources":                        traceSources(authorTraceSources),
			"prerequisite_gate_external_ids": authorTracePrereqs,
			"application_revision":           revision,
		}
		if selectedReview != nil {
			fields["review_context_id"] = selectedReview.Manifest.ContextID
		}
		if authorBody != "" {
			fields["body_md"] = authorBody
		}
		return authorTrace(env, args[0], fields)
	},
}

// transitionPair is the only shape a transition may have: FROM->TO, spaces
// around the arrow tolerated (PROCESS.md spells "PROPOSED -> TODO").
var transitionPair = regexp.MustCompile(`^[^\s>]+\s*->\s*[^\s>]+$`)

// inferredTransitions: the transition each loop purpose carries when the
// operator gives none. A trace purpose outside this table needs --from/--to;
// a gate purpose outside it is an ungoverned decision and carries none.
var inferredTransitions = map[string]map[string]string{
	"trace": {
		"cold-review": "plan->entry",
		"entry":       "PROPOSED->TODO",
		"lower":       "build->verify",
		// REQ-CROSS-431: a user requirement's own validation carries the same
		// pair as the lower loop it sits above — the value the skill and
		// `process advance`'s refusal have always told the operator to type
		// (BACKLOG-TOOL-93).
		"upper":      "build->verify",
		"completion": "IN_REVIEW->DONE",
	},
	"gate": {
		"entry":      "PROPOSED->TODO",
		"completion": "IN_REVIEW->DONE",
	},
}

// REQ-CROSS-377 (EPIC-CLI-017): the transition a trace or gate carries, from
// --from/--to, from a validated --transition, or inferred from the purpose. A
// fragment — what the shell leaves of an unquoted arrow — is refused here,
// before any write, because a trace is immutable and reconcile ignores a
// value it cannot read.
func resolveTransition(kind, purpose, transition, from, to string) (string, error) {
	switch {
	case transition != "" && (from != "" || to != ""):
		return "", fmt.Errorf("give --transition or --from/--to, not both")
	case (from == "") != (to == ""):
		return "", fmt.Errorf("--from and --to go together (got --from %q --to %q)", from, to)
	case from != "":
		return from + "->" + to, nil
	case transition != "":
		if !transitionPair.MatchString(transition) {
			return "", fmt.Errorf("--transition must be FROM->TO (got %q) — quote the arrow, or use --from and --to so the value never meets the shell", transition)
		}
		return transition, nil
	}
	if inferred, ok := inferredTransitions[kind][purpose]; ok {
		return inferred, nil
	}
	if kind == "trace" {
		return "", fmt.Errorf("transition is required for purpose %q: give --from and --to (e.g. --from build --to verify); it is inferred only for cold-review, entry, lower, upper and completion", purpose)
	}
	return "", nil
}

// withEntrySourceHint decorates a server FROM-state refusal of an INFERRED
// entry transition with the as-built alternative: the inferred source is
// PROPOSED, and a PENDING_VERIFICATION scope is refused by the server for not
// being in that state — a true refusal whose remedy the operator should not
// have to guess (N-CLI017-R1-06).
// errOnly drops the fingerprint an author helper returns, for a caller that
// only reports the helper's own printed lines.
func errOnly(_ string, err error) error { return err }

func withEntrySourceHint(err error, inferred bool) error {
	if err == nil || !inferred || !strings.Contains(err.Error(), "FROM state") {
		return err
	}
	return fmt.Errorf("%w\n  the transition was inferred as PROPOSED->TODO; for an as-built scope give --from PENDING_VERIFICATION --to TODO", err)
}

// fullHash is the only shape a loop pin has: a 64-character lowercase hex
// SHA-256 — the packet aggregate or a record's content hash. Anything shorter
// is a display prefix.
var fullHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// aggregatePurposes pin to the scope's packet aggregate; contentHashPurposes
// pin to the scoped record's own content hash — `lower` to a system
// requirement's, `upper` to the user requirement's (REQ-CROSS-431;
// BACKLOG-TOOL-93). Every other purpose is free and untouched.
var aggregatePurposes = map[string]bool{"cold-review": true, "entry": true, "completion": true}

var contentHashPurposes = map[string]bool{"lower": true, "upper": true}

// REQ-CROSS-376 (EPIC-CLI-017): the pin an authored trace carries, defaulted
// by purpose from the server's own reads and validated when given. A trace is
// immutable and a pin that matches nothing can never be removed, so the value
// is read from the store rather than typed: the packet aggregate for
// cold-review, entry and completion (the caller-scoped delivery-context read
// for the first scope token), the content hash for lower and upper (the
// requirements read). A given value that is not a full hash is refused; one that is the
// other class is refused naming both; one that matches nothing is recorded
// with a warning — a guard flags, it does not delete. When the reference is
// not readable from this session an explicit full hash is recorded with a
// "class unverified" warning and an omitted one is refused naming the flag.
func resolveTracePin(env *factoryEnv, purpose string, scope []string, given string) (string, []string, error) {
	var wantClass, otherClass string
	switch {
	case aggregatePurposes[purpose]:
		wantClass, otherClass = "packet aggregate", "content hash"
	case contentHashPurposes[purpose]:
		wantClass, otherClass = "content hash", "packet aggregate"
	default:
		return given, nil, nil
	}
	if given != "" && !fullHash.MatchString(given) {
		return "", nil, fmt.Errorf("--fingerprint must be a full 64-character hash (got %d characters) — the short form is a display prefix that never matches; read the packet aggregate with `process next -v` or the content hash from `working-set pull <id>`", len(given))
	}
	subject := ""
	if len(scope) > 0 {
		subject = scope[0]
	}
	// The aggregate is served for a piece the caller HOLDS: a member SR named
	// as the subject of a lower trace resolves to the epic piece that holds it
	// (review round 2, finding 2), so the wrong-class check sees the real
	// aggregate rather than an empty read.
	piece := subject
	if held, err := resolvePiece(env, subject, ""); err == nil && held != "" {
		piece = held
	}
	aggregate, aggregateErr := readAggregateFor(env, piece)
	// The content hashes of every scope token: for an aggregate purpose the
	// wrong-class value is any member's hash, not only the first token's.
	contentHashes, contentErr := readContentHashesFor(env, scope)
	var want, wantErr string
	others := map[string]string{}
	if contentHashPurposes[purpose] {
		want, wantErr = contentHashes[subject], contentErr
		if aggregate != "" {
			others[aggregate] = subject
		}
	} else {
		want, wantErr = aggregate, aggregateErr
		for id, h := range contentHashes {
			others[h] = id
		}
	}
	switch {
	case given == "" && want == "":
		return "", nil, fmt.Errorf("the %s for %s is not readable from this session (%s) — give --fingerprint explicitly", wantClass, subject, wantErr)
	case given == "":
		return want, nil, nil
	case given == want:
		return given, nil, nil
	case others[given] != "":
		return "", nil, fmt.Errorf("--purpose %s pins to the %s, but the value given is the %s of %s — the %s is %s", purpose, wantClass, otherClass, others[given], wantClass, presentPin(want))
	case want == "":
		return given, []string{fmt.Sprintf("class unverified: the %s for %s is not readable from this session (%s); recording --fingerprint %s as given", wantClass, subject, wantErr, given)}, nil
	default:
		return given, []string{fmt.Sprintf("--fingerprint %s matches no current packet aggregate or content hash for %s (the %s is %s) — recording it anyway", given, subject, wantClass, want)}, nil
	}
}

func pinClass(purpose string) string {
	if contentHashPurposes[purpose] {
		return "content hash"
	}
	return "packet aggregate"
}

func presentPin(v string) string {
	if v == "" {
		return "not readable"
	}
	return v
}

// readAggregateFor reads the packet aggregate the caller's selection of
// `piece` currently has; "" with the reason when the server serves none.
func readAggregateFor(env *factoryEnv, piece string) (string, string) {
	resp, err := readDeliveryContextFor(env, piece)
	if err != nil {
		return "", err.Error()
	}
	if resp.Data.PacketFingerprint == "" {
		return "", "no packet aggregate served — the scope is not a piece you hold, or the server serves none"
	}
	return resp.Data.PacketFingerprint, ""
}

// readContentHashesFor reads the current content hash — the same `fingerprint`
// field `working-set pull` renders — of every requirement named in scope, keyed
// by id; ids the store does not serve as requirements (an epic) are absent. The
// reason names what is missing when the first token has no hash.
func readContentHashesFor(env *factoryEnv, scope []string) (map[string]string, string) {
	out := map[string]string{}
	items, err := fetchDirectItems(env, scope, false, false)
	if err != nil {
		return out, fmt.Sprintf("could not read content hash for %s: %v", strings.Join(scope, ", "), err)
	}
	for _, id := range scope {
		item, ok := items[id]
		if ok && (item.kind == "system" || item.kind == "user") {
			if fp := str(item.payload, "fingerprint"); fp != "" {
				out[id] = fp
			}
		}
	}
	if len(scope) > 0 && out[scope[0]] == "" {
		return out, fmt.Sprintf("%s serves no content hash — not a requirement the store serves", scope[0])
	}
	return out, ""
}

func traceSources(refs []string) []map[string]any {
	result := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		kind := strings.ToLower(strings.SplitN(ref, ":", 2)[0])
		result = append(result, map[string]any{"kind": kind, "ref": ref})
	}
	return result
}

// REQ-CROSS-304/305/309 (EPIC-CLI-007): edit an existing requirement's content.
var authorUpdateCmd = &cobra.Command{
	Use:   "update <external-id>",
	Short: "Edit an existing requirement's content (a stale --expected-fingerprint conflicts)",
	Long: `Edit the fields you name; an omitted flag leaves the stored value in place.
--expected-fingerprint is required and a stale one conflicts instead of
overwriting; --criteria replaces the scenarios/criteria whole.

Keep the scalar flags — --title, --stage, --priority, --owner, --context,
--context-name and each --source tag — under 255 characters. A value over the bound is refused
as a 422 naming the field and the limit, and nothing is written. Prose
belongs in --detail, which is unbounded; --description, --boundary,
--rationale and --verification-method take a paragraph.

--citations-file replaces the source citation set with typed JSON objects.
It preserves captured-source identities and code/test kinds. It is mutually
exclusive with --source; omitting both preserves the existing citations.
The file must contain a JSON array; an empty array clears the stored set.
Legacy process_source citations may use source_tag or id as their identity.

--criteria is a JSON array — inline when the value starts with '[',
otherwise a path to a file holding the same array. It replaces the stored
set whole, keyed on external_id: omitting --criteria preserves the stored
set, and a criteria value that is present but not an array is refused
rather than silently ignored. Every object needs an external_id (one
without it is refused as a 422) and may carry position, kind (criterion,
or scenario for a user requirement's acceptance scenario), given, when,
then, statement, source_citations and verification_refs.

Set position on every object or on none. With none, the array order is the
stored order; a set that states position on some objects and not on others
is refused as a 422. A set is served by position, then external_id. To
reorder a set, send it again in the new order: a set identical to the stored
one writes nothing.

  --criteria '[{"external_id":"AC-1","position":1,"kind":"criterion","statement":"the export names the workspace"}]'
  --criteria '[{"external_id":"AS-1","kind":"scenario","given":"a stale fingerprint","when":"the edit is sent","then":"it conflicts"}]'

--kind backlog edits a backlog, gap or tooling record: its disposition (with
the --source that decided it) and its body — --notes, --observed,
--why-unrouted, --candidate-route, --affected, and --gap-kind/--consequence/
--affected-trace on a gap. A backlog record has no requirement fields:
--detail, --description, --stage, --priority, --owner, --boundary,
--rationale, --verification-method, --context, --context-name and --criteria
are refused there, and prose goes in --notes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		record, err := authorUpdateRecord(cmd)
		if err != nil {
			return err
		}
		return errOnly(authorUpdate(env, authorUpdateKind, args[0], record))
	},
}

// authorUpdateRecord assembles the edit from the flags the caller actually set:
// a field left unset is omitted so the server preserves its stored value rather
// than being overwritten with an empty one. --expected-fingerprint always rides
// (the server 409s on a stale one); --criteria, when set, replaces the record's
// scenarios/criteria, so an omitted flag sends no key rather than wiping them.
func authorUpdateRecord(cmd *cobra.Command) (map[string]any, error) {
	if err := refuseRequirementFieldsOnBacklog(cmd); err != nil {
		return nil, err
	}
	record := map[string]any{"expected_fingerprint": authorExpectedFingerprint}
	setStr := func(flag, field, val string) {
		if cmd.Flags().Changed(flag) {
			record[field] = val
		}
	}
	setStr("title", "title", authorTitle)
	// REQ-CROSS-393: a backlog disposition change and the source that decided it.
	setStr("disposition", "disposition", authorDisposition)
	if authorUpdateKind == "backlog" && cmd.Flags().Changed("source") && len(authorSources) > 0 {
		// The last --source given is the one that decided this disposition.
		record["source"] = authorSources[len(authorSources)-1]
	}
	setStr("description", "description", authorDescription)
	setStr("stage", "stage", authorStage)
	setStr("priority", "priority", authorPriority)
	setStr("owner", "owner", authorOwner)
	setStr("detail", "detail_md", authorDetail)
	setStr("boundary", "boundary", authorBoundary)
	setStr("rationale", "rationale", authorRationale)
	setStr("verification-method", "verification_method", authorVerificationMethod)
	setStr("lane-class", "lane_class", authorLaneClass)
	// REQ-CROSS-310 (SR-CLI-0081): --context and repeatable --source ride the
	// edit — the server accepts both, and a working-set pull renders them.
	setStr("context", "context", authorContext)
	// SR-RDD-ONBOARD-034: the context name rides next to its code.
	setStr("context-name", "context_name", authorContextName)
	if cmd.Flags().Changed("source") {
		record["source_citations"] = sourceCitations(authorSources)
	}
	if cmd.Flags().Changed("citations-file") {
		citations, err := readTypedCitations(authorCitationsFile)
		if err != nil {
			return nil, err
		}
		record["source_citations"] = citations
	}
	if cmd.Flags().Changed("criteria") {
		criteria, err := parseCriteria(authorCriteria)
		if err != nil {
			return nil, err
		}
		record["criteria"] = criteria
	}
	if err := setBacklogBody(cmd, record); err != nil {
		return nil, err
	}
	return record, nil
}

// readTypedCitations preserves immutable source identities for candidate repairs.
func readTypedCitations(path string) ([]map[string]any, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("--citations-file: %w", err)
	}
	var citations []map[string]any
	if err := json.Unmarshal(body, &citations); err != nil || citations == nil {
		return nil, fmt.Errorf("--citations-file: expected a JSON array of citation objects")
	}
	for i, citation := range citations {
		kind, _ := citation["kind"].(string)
		if strings.TrimSpace(kind) == "" {
			return nil, fmt.Errorf("--citations-file: citation %d needs kind", i+1)
		}
		if ref, present := citation["ref"]; present {
			if _, ok := ref.(string); !ok {
				return nil, fmt.Errorf("--citations-file: citation %d ref must be a string", i+1)
			}
		}
		identityKeys := []string{"ref", "source_file_id", "system_doc_id", "path"}
		if kind == "process_source" {
			identityKeys = append(identityKeys, "source_tag", "id")
		}
		hasIdentity := false
		for _, key := range identityKeys {
			value, _ := citation[key].(string)
			hasIdentity = hasIdentity || strings.TrimSpace(value) != ""
		}
		if !hasIdentity {
			return nil, fmt.Errorf("--citations-file: citation %d needs ref or a source identity", i+1)
		}
	}
	return citations, nil
}

// requirementOnlyFlags are the author update flags a backlog record has no
// field for.
var requirementOnlyFlags = []string{"description", "stage", "priority", "owner", "boundary", "rationale", "verification-method", "lane-class", "context", "context-name", "criteria", "citations-file"}

// refuseRequirementFieldsOnBacklog refuses, before any request, a requirement
// or epic field on a backlog record. The server reads only the backlog content
// fields, drops the rest and answers 200, so the CLI printed "✓ updated" over
// a value it never kept (REQ-CROSS-393 / REQ-BKLG-006; BACKLOG-TOOL-49).
func refuseRequirementFieldsOnBacklog(cmd *cobra.Command) error {
	if authorUpdateKind != "backlog" {
		return nil
	}
	// --detail is named apart: its prose has a home, --notes. It is not
	// remapped silently.
	if cmd.Flags().Changed("detail") {
		return fmt.Errorf("--detail: a backlog record has no detail body — put the prose in --notes; the server stores no detail_md for a backlog record and would answer 200 having kept nothing")
	}
	var given []string
	for _, flag := range requirementOnlyFlags {
		if cmd.Flags().Changed(flag) {
			given = append(given, "--"+flag)
		}
	}
	if len(given) > 0 {
		return fmt.Errorf("%s: a backlog record has no such field and the server would answer 200 having kept nothing — its body is --title, --notes, --observed, --why-unrouted, --candidate-route and --affected, and on a gap --gap-kind, --consequence and --affected-trace",
			strings.Join(given, ", "))
	}
	return nil
}

// setBacklogBody carries a backlog record's own content fields onto the edit
// (REQ-CROSS-393; BACKLOG-TOOL-79). The server takes them on the same
// fingerprint-guarded update it takes a disposition on (Core.Author
// @backlog_content_fields); they were registered on `author backlog` alone, so
// a filed record's body could only be corrected by filing a second record. On
// any other kind they are refused here, before any request, naming each flag
// given.
func setBacklogBody(cmd *cobra.Command, record map[string]any) error {
	fields := []struct{ flag, field, value string }{
		{"notes", "notes_md", authorBacklogNotes},
		{"observed", "observation", authorBacklogObserved},
		{"why-unrouted", "why_unrouted", authorBacklogWhy},
		{"candidate-route", "candidate_route", authorBacklogRoute},
		{"gap-kind", "gap_kind", authorBacklogGapKind},
		{"consequence", "consequence", authorBacklogConsequence},
		{"affected", "affected_external_ids", authorBacklogAffected},
		{"affected-trace", "affected_trace_external_ids", authorBacklogTraces},
	}
	idLists := map[string]bool{"affected_external_ids": true, "affected_trace_external_ids": true}
	backlog := authorUpdateKind == "backlog"
	var misplaced []string
	for _, f := range fields {
		if !cmd.Flags().Changed(f.flag) {
			continue
		}
		if !backlog {
			misplaced = append(misplaced, "--"+f.flag)
			continue
		}
		if idLists[f.field] {
			record[f.field] = splitIDs(f.value)
			continue
		}
		record[f.field] = f.value
	}
	if len(misplaced) > 0 {
		return fmt.Errorf("%s: backlog content — add --kind backlog, or edit a requirement or epic with --detail, --description and --rationale",
			strings.Join(misplaced, ", "))
	}
	// REQ-CROSS-423: the gap kind is checked against the store's vocabulary
	// here, naming the set, exactly as the create checks it.
	if backlog && cmd.Flags().Changed("gap-kind") && !contains(backlogGapKinds, authorBacklogGapKind) {
		return fmt.Errorf("--gap-kind %q: expected %s", authorBacklogGapKind, strings.Join(backlogGapKinds, "|"))
	}
	return nil
}

// sourceCitations turns each --source value (e.g. "USER:2026-09-01:x") into a
// process-source citation the server's source_citations column stores.
func sourceCitations(refs []string) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		out = append(out, map[string]any{"kind": "process_source", "source_tag": ref})
	}
	return out
}

// parseCriteria reads the --criteria value as a JSON array of criterion/scenario
// objects: a value beginning with '[' is inline JSON, anything else is a file
// path. A malformed array refuses here rather than sending the server content it
// will reject — an empty flag never reaches this (the caller only parses a set
// flag), so existing criteria are never silently wiped.
func parseCriteria(raw string) ([]any, error) {
	data := []byte(raw)
	if !strings.HasPrefix(strings.TrimSpace(raw), "[") {
		content, err := os.ReadFile(filepath.Clean(raw))
		if err != nil {
			return nil, fmt.Errorf("--criteria %q: %w", raw, err)
		}
		data = content
	}
	var criteria []any
	if err := json.Unmarshal(data, &criteria); err != nil {
		return nil, fmt.Errorf("--criteria: expected a JSON array of criterion/scenario objects: %w", err)
	}
	return criteria, nil
}

// REQ-CROSS-306 (EPIC-CLI-007): declare a UR<->SR relation. The external-id
// argument is the SYSTEM requirement the relation is declared on — relations
// live on the SR side, and the server refuses a UR-side target.
var authorRelateCmd = &cobra.Command{
	Use:   "relate <external-id>",
	Short: "Declare a UR<->SR relation on a system requirement (--parent names the URs)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		// A repeatable flag the caller never passed carries nothing — read it
		// only where cobra recorded it as set, so a reset default (the shared
		// cobra tree) can never read back as a user-supplied value.
		if !cmd.Flags().Changed("parent") {
			authorParents = nil
		}
		return errOnly(authorRelate(env, args[0], authorRelateFields()))
	},
}

// authorRelateFields assembles the relation record from the flags. --mode
// carries the server's declare|withdraw|confirm; declare is the default, so an
// omitted flag still names the additive declare the server would assume, and
// withdraw/confirm are reachable from the CLI (REQ-CROSS-306).
func authorRelateFields() map[string]any {
	return map[string]any{
		"parent_external_ids":  authorParents,
		"expected_fingerprint": authorExpectedFingerprint,
		"mode":                 authorRelateMode,
	}
}

// REQ-CROSS-306 (EPIC-CLI-007): author EPIC MEMBERSHIP. The external-id argument
// is the epic; --member names the member requirements (UR and/or SR) and --mode
// is declare (default) or withdraw. Posts action:"relate" with a kind:"epic"
// record, so the server's epic-membership clause writes the links.
var authorMemberCmd = &cobra.Command{
	Use:   "member <epic-external-id>",
	Short: "Author epic membership (--member names the requirements; --mode declare|withdraw)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if !cmd.Flags().Changed("member") {
			authorMembers = nil
		}
		// Membership is a relation mutation, so it is fingerprint-guarded and
		// moves the epic's fingerprint like every other edit — the guard is the
		// server's, and the CLI has to name the fingerprint it read.
		return errOnly(authorMember(env, args[0], map[string]any{
			"member_external_ids":  authorMembers,
			"mode":                 authorMemberMode,
			"expected_fingerprint": authorExpectedFingerprint,
		}))
	},
}

func init() {
	for _, c := range []*cobra.Command{authorRequirementCmd, authorEpicCmd, authorGateCmd, authorTraceCmd} {
		c.Flags().StringVar(&authorTitle, "title", "", "record title (required)")
	}
	authorEpicCmd.Flags().StringVar(&authorDescription, "description", "", "epic description (optional at birth)")
	authorRequirementCmd.Flags().StringVar(&authorContext, "context", "", "bounded-context code (e.g. CROSS)")
	authorRequirementCmd.Flags().StringVar(&authorStatus, "status", "", "birth status: PROPOSED (default) or DERIVED")
	authorRequirementCmd.Flags().StringVar(&authorRequirementKind, "kind", "", "requirement kind: ur (user requirement) or sr (system, the default when omitted)")
	authorGateCmd.Flags().StringVar(&authorGateKind, "gate-kind", "question", "question | decision | approval_request")
	authorGateCmd.Flags().StringVar(&authorBody, "body", "", "gate body markdown")
	authorGateCmd.Flags().StringVar(&authorGatePurpose, "purpose", "",
		"what the gate decides (store_backed_activation | store_backed_clearing | …)")
	authorGateCmd.Flags().StringArrayVar(&authorGateScope, "scope", nil,
		"exact scope token the gate names, repeatable (e.g. system:243, REQ-CROSS-259)")
	authorGateCmd.Flags().StringArrayVar(&authorGateOptions, "option", nil,
		"structured option key=label, repeatable — an approving answer rides the key")
	authorGateCmd.Flags().StringVar(&authorGateTransition, "transition", "",
		"the transition this gate authorizes as one FROM->TO value (quote the arrow); prefer --from/--to")
	authorGateCmd.Flags().StringVar(&authorTransFrom, "from", "", "the transition's FROM state (with --to); inferred for --purpose entry (PROPOSED) and completion (IN_REVIEW)")
	authorGateCmd.Flags().StringVar(&authorTransTo, "to", "", "the transition's TO state (with --from); inferred for --purpose entry (TODO) and completion (DONE)")
	// REQ-CROSS-310 (SR-CLI-0081): the brief block PROCESS requires on a human
	// gate — an entry/completion gate is refused without one.
	authorGateCmd.Flags().StringVar(&authorGateBriefFile, "brief-file", "", "JSON file with the gate's brief object")
	authorGateCmd.Flags().StringVar(&authorGateBriefWhat, "brief-what", "", "brief: the decision")
	authorGateCmd.Flags().StringVar(&authorGateBriefWhyNow, "brief-why-now", "", "brief: the trigger and blocked work")
	authorGateCmd.Flags().StringVar(&authorGateBriefChanges, "brief-changes", "", "brief: the visible outcome if approved")
	authorGateCmd.Flags().StringVar(&authorGateBriefRisk, "brief-risk", "", "brief: the downside and reversibility if wrong")
	authorGateCmd.Flags().StringVar(&authorGateBriefRecommend, "brief-recommendation", "", "brief: the recommended option and rationale")
	authorGateCmd.Flags().StringVar(&authorGateRecommendation, "recommendation", "", "recommendation prose")
	authorGateCmd.Flags().StringVar(&authorGateRecommendedOption, "recommended-option", "", "recommended structured option key")
	authorGateCmd.Flags().StringArrayVar(&authorGateSources, "source", nil, "gate source reference, repeatable")
	authorGateCmd.Flags().StringArrayVar(&authorGatePrereqs, "prerequisite", nil, "prerequisite gate external id, repeatable")
	authorGateCmd.Flags().StringVar(&authorGateSupersedes, "supersedes", "",
		"the open or answered decision this gate supersedes — retired and linked in the same write (REQ-CROSS-353)")
	authorGateWithdrawCmd.Flags().StringVar(&authorGateWithdrawReason, "reason", "", "why the decision is being withdrawn (required; recorded as the gate_withdrawn audit)")
	_ = authorGateWithdrawCmd.MarkFlagRequired("reason")
	authorTraceCmd.Flags().StringVar(&authorBody, "body", "", "trace verdict details markdown")
	authorTraceCmd.Flags().StringVar(&authorGatePurpose, "purpose", "", "trace purpose, e.g. cold-review")
	authorTraceCmd.Flags().StringVar(&authorGateTransition, "transition", "", "transition held by the trace as one FROM->TO value (quote the arrow); prefer --from/--to")
	authorTraceCmd.Flags().StringVar(&authorTransFrom, "from", "", "the transition's FROM state (with --to); inferred by purpose: cold-review plan, entry PROPOSED, lower build, upper build, completion IN_REVIEW")
	authorTraceCmd.Flags().StringVar(&authorTransTo, "to", "", "the transition's TO state (with --from); inferred by purpose: cold-review entry, entry TODO, lower verify, upper verify, completion DONE")
	authorTraceCmd.Flags().StringArrayVar(&authorGateScope, "scope", nil, "exact EPIC/UR/SR scope token, repeatable")
	authorTraceCmd.Flags().StringVar(&authorTraceFingerprint, "fingerprint", "", "the pin evaluated: the packet aggregate (cold-review, entry, completion) or the content hash (lower, upper); read from the store when omitted")
	authorTraceCmd.Flags().StringVar(&authorTraceReviewContext, "review-context", "", "select an immutable review snapshot context id for cold-review")
	authorTraceCmd.Flags().StringVar(&authorTraceVerdict, "verdict", "", "PASS | FAIL | STALE")
	authorTraceCmd.Flags().StringArrayVar(&authorTraceSources, "source", nil, "verdict source reference, repeatable")
	authorTraceCmd.Flags().StringArrayVar(&authorTracePrereqs, "prerequisite", nil, "prerequisite trace gate id, repeatable")
	authorTraceCmd.Flags().StringVar(&authorTraceRevision, "application-revision", "", "repository revision (default: HEAD)")
	authorAdvanceCmd.Flags().StringVar(&authorKind, "kind", "requirement", "requirement | epic")
	authorAdvanceCmd.Flags().StringVar(&authorTo, "to", "", "target status (required)")
	authorAdvanceCmd.Flags().StringVar(&authorExpected, "expected", "", "the record's current status (required; conflicts refuse)")
	authorAdvanceCmd.Flags().StringVar(&authorGateRef, "gate", "", "ANSWERED gate external id, for human-gated transitions")
	authorAdvanceCmd.Flags().StringVar(&authorGateFinger, "gate-fingerprint", "", "the gate's current content-shadow hash")
	authorAdvanceCmd.Flags().StringVar(&authorGateAnswer, "gate-answer", "", "the gate's stored answer OR a stable option key it recorded (or that key's label), echoed — a transition rides only an answer the gate actually holds")
	authorAdvanceCmd.Flags().StringVar(&authorDecisionRef, "decision", "", "attributable USER:… reference, for DEFERRED")
	authorUpdateCmd.Flags().StringVar(&authorCitationsFile, "citations-file", "", "JSON file of typed source citations; replaces the stored set, preserving immutable source identities")
	authorUpdateCmd.Flags().StringVar(&authorTitle, "title", "", "new title")
	authorUpdateCmd.Flags().StringVar(&authorDescription, "description", "", "new description")
	authorUpdateCmd.Flags().StringVar(&authorStage, "stage", "", "new stage")
	authorUpdateCmd.Flags().StringVar(&authorPriority, "priority", "", "new priority")
	authorUpdateCmd.Flags().StringVar(&authorOwner, "owner", "", "new owner")
	authorUpdateCmd.Flags().StringVar(&authorDetail, "detail", "", "new detail markdown (record detail_md)")
	authorUpdateCmd.Flags().StringVar(&authorBoundary, "boundary", "", "new change boundary (system requirement)")
	authorUpdateCmd.Flags().StringVar(&authorRationale, "rationale", "", "new rationale (system requirement)")
	authorUpdateCmd.Flags().StringVar(&authorVerificationMethod, "verification-method", "", "new verification method (system requirement)")
	authorUpdateCmd.Flags().StringVar(&authorLaneClass, "lane-class", "", "the small-change lane class, set before the narrow review: defect_with_failing_test, wording, presentation or dependency_patch (system requirement)")
	authorUpdateCmd.Flags().StringVar(&authorCriteria, "criteria", "",
		"JSON array of criterion/scenario objects — inline when it starts with '[', otherwise a file path. "+
			"Each object needs external_id (the replace-set keys on it) and takes position "+
			"(on every object or on none; none keeps the array order), "+
			"kind (criterion | scenario), given, when, then, statement, source_citations, verification_refs")
	authorUpdateCmd.Flags().StringVar(&authorExpectedFingerprint, "expected-fingerprint", "",
		"the record's current content fingerprint (required; a stale one conflicts instead of overwriting)")
	authorUpdateCmd.Flags().StringVar(&authorUpdateKind, "kind", "requirement",
		"requirement (default), epic — an epic's title/description are edited too — or backlog")
	authorUpdateCmd.Flags().StringVar(&authorDisposition, "disposition", "",
		"backlog only: "+backlogDispositionShape+" — needs --source")
	// REQ-CROSS-393: a filed backlog record's body is editable, under the same
	// flag names `author backlog` files it with (BACKLOG-TOOL-79).
	authorUpdateCmd.Flags().StringVar(&authorBacklogNotes, "notes", "", "backlog only: new free notes (markdown)")
	authorUpdateCmd.Flags().StringVar(&authorBacklogObserved, "observed", "", "backlog only: new observation — what was seen, not what it implies")
	authorUpdateCmd.Flags().StringVar(&authorBacklogWhy, "why-unrouted", "", "backlog only: unclear owner, cross-cutting, or awaiting a decision")
	authorUpdateCmd.Flags().StringVar(&authorBacklogRoute, "candidate-route", "", "backlog only: PROPOSED UR/SR, DERIVED, gap, conflict, or decision gate")
	authorUpdateCmd.Flags().StringVar(&authorBacklogAffected, "affected", "", "backlog only: affected EPIC/UR/SR ids, comma-separated (replaces the stored list)")
	authorUpdateCmd.Flags().StringVar(&authorBacklogGapKind, "gap-kind", "", "backlog only, gap: capability | specification")
	authorUpdateCmd.Flags().StringVar(&authorBacklogConsequence, "consequence", "", "backlog only, gap: what the affected traces cannot currently prove")
	authorUpdateCmd.Flags().StringVar(&authorBacklogTraces, "affected-trace", "", "backlog only, gap: ids whose trace is incomplete because of it, comma-separated (replaces the stored list)")
	authorBacklogCmd.Flags().StringVar(&authorBacklogKind, "kind", "backlog", "backlog | gap | tooling")
	authorBacklogCmd.Flags().StringVar(&authorTitle, "title", "", "the discovery in one line (required)")
	authorBacklogCmd.Flags().StringVar(&authorBacklogObserved, "observed", "", "what was seen, not what it implies")
	authorBacklogCmd.Flags().StringVar(&authorBacklogWhy, "why-unrouted", "", "unclear owner, cross-cutting, or awaiting a decision")
	authorBacklogCmd.Flags().StringVar(&authorBacklogRoute, "candidate-route", "", "PROPOSED UR/SR, DERIVED, gap, conflict, or decision gate")
	authorBacklogCmd.Flags().StringVar(&authorBacklogAffected, "affected", "", "affected EPIC/UR/SR ids, comma-separated")
	authorBacklogCmd.Flags().StringVar(&authorBacklogGapKind, "gap-kind", "", "gap only: capability | specification")
	authorBacklogCmd.Flags().StringVar(&authorBacklogTraces, "affected-trace", "", "gap only: ids whose trace is incomplete because of it, comma-separated")
	authorBacklogCmd.Flags().StringVar(&authorBacklogConsequence, "consequence", "", "gap only: what the affected traces cannot currently prove")
	authorBacklogCmd.Flags().StringVar(&authorBacklogNotes, "notes", "", "free notes (markdown)")
	// REQ-CROSS-310 (SR-CLI-0081): --context and repeatable --source ride the edit.
	authorUpdateCmd.Flags().StringVar(&authorContext, "context", "", "new bounded-context code")
	authorUpdateCmd.Flags().StringVar(&authorContextName, "context-name", "", "new bounded-context name")
	authorUpdateCmd.Flags().StringArrayVar(&authorSources, "source", nil, "source citation (e.g. USER:2026-09-01:x), repeatable")
	authorUpdateCmd.MarkFlagsMutuallyExclusive("source", "citations-file")
	authorRelateCmd.Flags().StringArrayVar(&authorParents, "parent", nil,
		"parent user-requirement external id, repeatable (at least one required)")
	authorRelateCmd.Flags().StringVar(&authorExpectedFingerprint, "expected-fingerprint", "",
		"the system requirement's current content fingerprint (required; a stale one conflicts)")
	authorRelateCmd.Flags().StringVar(&authorRelateMode, "mode", "declare",
		"declare (add, additive) | withdraw (drop) | confirm (promote a candidate edge)")
	authorMemberCmd.Flags().StringArrayVar(&authorMembers, "member", nil,
		"member requirement external id (UR or SR), repeatable (at least one required)")
	authorMemberCmd.Flags().StringVar(&authorMemberMode, "mode", "declare", "declare (add) | withdraw (remove)")
	authorMemberCmd.Flags().StringVar(&authorExpectedFingerprint, "expected-fingerprint", "",
		"the epic's current content fingerprint (required; a stale one conflicts)")
	authorCmd.AddCommand(authorRequirementCmd, authorEpicCmd, authorBacklogCmd, authorGateCmd, authorGateWithdrawCmd, authorTraceCmd, authorAdvanceCmd, authorDemoteCmd, authorUpdateCmd, authorRelateCmd, authorMemberCmd)
	rootCmd.AddCommand(authorCmd)
}
