package cmd

// EPIC-CLI-017 — the ceremony verbs. The tool does the ceremony: a sequence
// fully determined by store facts — which trace to record at which pin, which
// gate to open with which prerequisite, which transitions reconcile allows — is
// a verb, never a documented recipe. Every verb reads its facts from the one
// server read (`delivery-context.facts`), refuses before its first write when
// a fact is unmet, and prints the server's refusal verbatim otherwise.
//
// REQ-CROSS-374: `process advance <SR>`. REQ-CROSS-373: `process enter <scope>`.
// REQ-CROSS-375: `process complete <scope>`.

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// deliveryFacts is the additive `facts` block of the delivery-context read.
// A nil pointer means the server serves none (an undeployed server): every
// verb refuses before any write on nil (D14).
type deliveryFacts struct {
	Aggregate string `json:"aggregate"`
	Scope     struct {
		ExternalID string `json:"external_id"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
	} `json:"scope"`
	Members    []factMember `json:"members"`
	ColdReview struct {
		Verdict         string `json:"verdict"`
		Independent     bool   `json:"independent"`
		TraceExternalID string `json:"trace_external_id"`
		// REQ-CROSS-408: why the chosen verdict does not count, the open or
		// deferred material finding ids, and the scope's cold-review traces
		// pinned at other aggregates. nil on a server that predates them.
		IndependenceReason string   `json:"independence_reason"`
		OpenFindingIDs     []string `json:"open_finding_ids"`
		StaleTraces        []struct {
			ExternalID string `json:"external_id"`
			Aggregate  string `json:"aggregate"`
		} `json:"stale_traces"`
	} `json:"cold_review"`
	// REQ-CROSS-408: the completion facts with their reasons.
	Completion struct {
		DeliveredCurrentReconciled bool     `json:"delivered_current_reconciled"`
		GateAnswered               bool     `json:"gate_answered"`
		SettledGateExternalID      string   `json:"settled_gate_external_id"`
		MembersNotReviewed         []string `json:"members_not_reviewed"`
	} `json:"completion"`
	Sections struct {
		Complete bool     `json:"complete"`
		Missing  []string `json:"missing"`
		// REQ-CROSS-383: the canonical keys the plan check requires for the
		// scope, computed server-side from stored membership — the scaffold's
		// denominator. nil on a server that predates the key.
		Required []string `json:"required"`
	} `json:"sections"`
	EntryGate struct {
		Applied         bool   `json:"applied"`
		PinnedAggregate string `json:"pinned_aggregate"`
	} `json:"entry_gate"`
}

type factMember struct {
	ExternalID         string `json:"external_id"`
	Kind               string `json:"kind"`
	Status             string `json:"status"`
	ContentFingerprint string `json:"content_fingerprint"`
	EvidenceState      string `json:"evidence_state"`
	RedRecorded        bool   `json:"red_recorded"`
	LowerTracePass     bool   `json:"lower_trace_pass"`
}

type advanceOpts struct {
	piece string // --piece: the held selection the SR belongs to, when ambiguous
	log   string // --log: the RUN: source the lower trace cites
	body  string // --body: the trace's verdict details
}

var (
	advanceLog  string
	advanceBody string
)

var processAdvanceCmd = &cobra.Command{
	Use:   "advance <SR>",
	Short: "Record the SR's lower trace from its recorded evidence and reconcile it to IN_REVIEW",
	Long: `Take one system requirement from recorded evidence to IN_REVIEW in one call.
The verb reads the store facts (the delivery-context read of the piece that
holds the SR): a current RED must be recorded and the evidence state must be
passing. It then records TRACE-LOWER-<SR> at the SR's content hash (the
lower purpose, build->verify) and runs process reconcile --apply until no
automatic transition remains — TODO -> IN_PROGRESS -> IN_REVIEW for the SR,
and the epic's own step when its members allow it.

It refuses before any write, naming the fact: no RED recorded (record it
with factory evidence --fail <SR> --role RED at the RED commit, or with
--revision); evidence not passing (failing, claimed, stale); the SR not a
member of a piece you hold (--piece names one when you hold several); a
server that serves no delivery facts. A second call on an SR already
IN_REVIEW at its current hash reports nothing to do. Transitions and FAILs
of sibling members are printed as information and never fail the advance;
a FAIL naming the SR itself does, after the trace is recorded.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processAdvance(env, args[0], advanceOpts{piece: processPiece, log: advanceLog, body: advanceBody})
	},
}

// REQ-CROSS-413 (BACKLOG-TOOL-22): `process reapply-entry <scope> --decision
// USER:...` re-pins an applied entry gate a process repin stranded. The server
// resolves the one applied entry gate for the named scope and re-pins it to the
// current packet aggregate; the client checks the USER: attestation up front,
// exactly as `process cascade-mode` does.
var processReapplyDecision string

var processReapplyEntryCmd = &cobra.Command{
	Use:   "reapply-entry <scope>",
	Short: "Re-pin an applied entry gate to the current packet aggregate (requires a USER: attestation)",
	Long: `Re-apply an applied entry gate pinned to a superseded packet aggregate.

An applied entry gate is a closed, approved entry decision: PROPOSED or
PENDING_VERIFICATION to TODO or READY, or a legacy NULL entry. Recovery covers
all these entry transitions. When its pin no longer matches the current packet
aggregate, process next routes it blocked and process advance refuses, because
a lower trace cannot advance a member whose entry approval no longer matches the
current packet aggregate. This verb re-pins that one applied entry gate to the
current packet aggregate for <scope>, so the block clears and advancing resumes.

It requires an attributable --decision USER:... source and performs NO machine
content re-check: you attest that the aggregate moved for an immaterial reason
(typically only the process text changed) and that the scope's content, members
and sections did not. A material change needs re-review, not re-apply — re-pull
the scope and let cold review run again.

It re-pins the single applied entry anchor in place (it never opens a second
gate) and records the USER: source on an audit event, like gate-withdraw.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !strings.HasPrefix(processReapplyDecision, "USER:") {
			return fmt.Errorf("--decision is required and must be a USER: attribution (e.g. USER:2026-09-14:process-repin-was-immaterial) — it attests the aggregate move was immaterial")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processReapplyEntry(env, args[0], processReapplyDecision)
	},
}

func processReapplyEntry(env *factoryEnv, scope, decision string) error {
	status, body, err := env.call("POST", "/api/v1/sync/author", map[string]any{
		"system_id": env.SystemID,
		"action":    "reapply_entry",
		"record":    map[string]any{"scope_external_id": scope},
		"decision":  decision,
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-author"},
	})
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("reapply_entry", status, body)
	}
	printSuccess("re-applied the entry gate for %s at the current packet aggregate (%s)", scope, decision)
	return nil
}

// BACKLOG-TOOL-44: `process reenter <scope>` re-establishes a stranded applied
// entry gate after a reversed-decision reopen (a MATERIAL change). Unlike
// reapply-entry — an immaterial USER: attestation — it demands a fresh review: a
// human re-entry approval gate whose prerequisite is an independent cold-review
// PASS at the current packet aggregate. Open (no --apply) opens that gate; once it
// is answered `approve`, --apply re-pins the entry gate to the current aggregate.
var processReenterApply bool
var processReenterGateID string

var processReenterCmd = &cobra.Command{
	Use:   "reenter <scope>",
	Short: "Re-establish a stranded entry gate after a reversed-decision reopen (fresh cold review + human approval)",
	Long: `Re-establish an applied entry gate stranded by a reversed-decision reopen.

A scoped ` + "`process supersede --apply`" + ` reopens a shipped item by demoting it to
IN_PROGRESS and moving the packet aggregate, so its applied entry gate is stranded
at a superseded aggregate. Because the content CHANGED (a material reversal),
` + "`process reapply-entry`" + ` — an immaterial attestation that nothing changed — is the
wrong tool. This verb re-establishes entry the honest way, at a human gate backed
by a fresh independent cold-review PASS at the current aggregate.

Open (no --apply) opens a re-entry approval gate REENTRY-<scope> (purpose
"reentry"; --gate-id for a successor when that id is taken — e.g. a prior
re-entry gate was withdrawn and its id stays reserved), naming the current
cold-review trace as its prerequisite; it refuses if there is no independent
passing cold-review at the current aggregate. Answer it
` + "`approve`" + ` (Mission Control or ` + "`factory answer`" + `), then run --apply: the server
verifies the answered gate and the cold-review, re-pins the one stranded applied
entry gate to the current aggregate, and closes the re-entry gate. The item stays
IN_PROGRESS; reapply-entry's immaterial-only contract is untouched.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if processReenterApply {
			return processReenterApplyRepin(env, args[0])
		}
		return processReenterOpen(env, args[0])
	},
}

func processReenterOpen(env *factoryEnv, scope string) error {
	// REQ-CROSS-422 (F-CLI022-R8-01): the argument may be a member entered
	// through its own members-only gate; its facts are the facts of the piece
	// that holds it, exactly as process advance reads them.
	piece, err := resolvePiece(env, scope, processPiece)
	if err != nil {
		return err
	}
	resp, err := readDeliveryContextFor(env, piece)
	if err != nil {
		return err
	}
	facts := resp.Data.Facts
	if facts == nil {
		return errNoFacts(piece, resp.Data.FactsState)
	}
	cr := facts.ColdReview
	if cr.Verdict != "pass" || !cr.Independent || cr.TraceExternalID == "" {
		return fmt.Errorf("no independent passing cold-review trace at the current packet aggregate %s for %s (verdict %s, independent %v) — a material re-entry needs a fresh cold review; run rdd-cold-review and record it with `author trace --purpose cold-review`", facts.Aggregate, piece, presentPin(cr.Verdict), cr.Independent)
	}

	gateID := processReenterGateID
	if gateID == "" {
		gateID = "REENTRY-" + scope
	}
	if err := gateIDFree(env, gateID); err != nil {
		return err
	}

	fmt.Printf("re-entry approval gate %s\n  scope: %s\n  prerequisite: %s (cold-review PASS at %s)\n", gateID, scope, cr.TraceExternalID, facts.Aggregate)
	if piece != scope {
		fmt.Printf("  piece: %s (the gate is pinned at its aggregate and cites its cold review)\n", piece)
	}

	fields := map[string]any{
		"title":                          fmt.Sprintf("Re-enter %s — material reversal", scope),
		"gate_kind":                      "decision",
		"purpose":                        "reentry",
		"exact_scope":                    []string{scope},
		"evaluated_scope_fingerprint":    facts.Aggregate,
		"options":                        []map[string]any{{"key": "approve", "label": "Approve re-entry"}, {"key": "decline", "label": "Do not re-enter; keep planning"}},
		"recommended_option_key":         "approve",
		"prerequisite_gate_external_ids": []string{cr.TraceExternalID},
		"brief": map[string]any{
			"what":                fmt.Sprintf("Re-establish entry for %s at the current packet aggregate after a reversed-decision reopen", scope),
			"why_now":             "The prior entry approval was stranded when the reversal moved the packet aggregate; the change is material, so it needs re-review, not re-apply.",
			"changes_if_approved": "The stranded applied entry gate is re-pinned to the current aggregate; the item can advance from IN_PROGRESS.",
			"risk_if_wrong":       "Re-entering without a genuine re-review would let a materially-changed item proceed on a stale approval.",
			"recommendation":      "Approve if the fresh cold review is sound.",
		},
	}
	if err := authorCreate(env, "gate", gateID, fields); err != nil {
		return err
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`, then run `process reenter %s --apply`", gateID, scope)
	return nil
}

func processReenterApplyRepin(env *factoryEnv, scope string) error {
	status, body, err := env.call("POST", "/api/v1/sync/author", map[string]any{
		"system_id": env.SystemID,
		"action":    "reenter_entry",
		"record":    map[string]any{"scope_external_id": scope},
		"actor":     map[string]any{"kind": "agent", "agent_slug": "modernpath-author"},
	})
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("reenter_entry", status, body)
	}
	printSuccess("re-entered %s: the entry gate is re-pinned to the current packet aggregate", scope)
	return nil
}

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
// surfaced as is.
func resolvePiece(env *factoryEnv, item, piece string) (string, error) {
	path := fmt.Sprintf("/api/v1/sync/work-selection?system_id=%d", env.SystemID)
	if piece != "" {
		path += "&scope=" + url.QueryEscape(piece)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return "", err
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
// passes at `pin`, that trace's id as existing (nothing to record — reuse it).
func freeTraceID(env *factoryEnv, base, pin string) (free, existing string, err error) {
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
			if str(gate, "state") == "pass" && str(gate, "fingerprint") == pin {
				return "", id, nil
			}
			continue
		}
		if status == 404 {
			if em, ok := body["error"].(map[string]any); ok && strings.HasPrefix(str(em, "message"), "no such gate") {
				return id, "", nil
			}
		}
		return "", "", gateShowError(status, body, id)
	}
	return "", "", fmt.Errorf("no free trace id in the %s series", base)
}

// processAdvance takes one SR from recorded evidence to IN_REVIEW: refuse on
// any unmet fact before writing, record the lower trace at the content hash,
// reconcile until nothing remains, report the statuses.
func processAdvance(env *factoryEnv, sr string, o advanceOpts) error {
	if o.log == "" {
		return fmt.Errorf("--log is required: the passing run's command or report is what the lower trace cites as its RUN: source")
	}
	piece, err := resolvePiece(env, sr, o.piece)
	if err != nil {
		return err
	}
	resp, err := readDeliveryContextFor(env, piece)
	if err != nil {
		return err
	}
	facts := resp.Data.Facts
	if facts == nil {
		return errNoFacts(piece, resp.Data.FactsState)
	}
	var member *factMember
	for i := range facts.Members {
		if facts.Members[i].ExternalID == sr {
			member = &facts.Members[i]
		}
	}
	if member == nil {
		return fmt.Errorf("%s is not a member the delivery facts of %s serve", sr, piece)
	}
	if member.Kind == "ur" {
		// Reconcile's UR path wants no RED and no lower evidence: the UR moves on
		// its required SRs and its own UPPER trace at its content hash.
		return fmt.Errorf("%s is a user requirement — process advance takes a system requirement; once its required SRs are IN_REVIEW, record its upper trace by hand (`author trace TRACE-UPPER-%s --purpose upper --from build --to verify --scope %s --fingerprint %s --verdict PASS --source RUN:…`) and run `process reconcile --apply`", sr, sr, sr, presentPin(member.ContentFingerprint))
	}
	switch member.Status {
	case "DONE", "OBSOLETE", "DEFERRED":
		fmt.Printf("nothing to do: %s is %s\n", sr, member.Status)
		return nil
	case "IN_REVIEW":
		if member.LowerTracePass {
			fmt.Printf("nothing to do: %s is already IN_REVIEW with a passing lower trace at %s\n", sr, member.ContentFingerprint)
			return nil
		}
	case "TODO", "READY":
		// Reconcile promotes TODO -> IN_PROGRESS only when the entry gate is
		// applied at the CURRENT aggregate; a trace recorded before that would
		// advance nothing.
		if !facts.EntryGate.Applied || facts.EntryGate.PinnedAggregate != facts.Aggregate {
			return fmt.Errorf("%s is %s but its entry gate is not applied at the current packet aggregate %s (pinned at %s) — run `process reapply-entry %s --decision USER:…` (or `process reapply-entry %s --decision USER:…` when %s entered through its own members-only gate) to attest the move was immaterial and re-pin the entry approval at this aggregate before a lower trace can advance it", sr, member.Status, facts.Aggregate, presentPin(facts.EntryGate.PinnedAggregate), piece, sr, sr)
		}
	case "IN_PROGRESS":
	default:
		return fmt.Errorf("%s is %s, not entered — `process enter` the scope (and apply the answer) before advancing", sr, presentPin(member.Status))
	}
	if !member.RedRecorded {
		return fmt.Errorf("no RED is recorded for %s — record the red-first result with `factory evidence --fail %s --role RED` at the RED commit (or --revision <red-commit>) before advancing", sr, sr)
	}
	if member.EvidenceState != "passing" {
		return fmt.Errorf("the evidence for %s reads %s, not passing — record the passing run with `factory evidence --pass %s` (a stale result needs a rerun at the current revision) before advancing", sr, member.EvidenceState, sr)
	}
	if member.ContentFingerprint == "" {
		return fmt.Errorf("the store serves no content hash for %s — the lower trace has nothing to pin to", sr)
	}

	if member.LowerTracePass {
		printInfo("a lower trace already passes for %s at %s — reconciling", sr, member.ContentFingerprint)
	} else {
		id, existing, err := freeTraceID(env, "TRACE-LOWER-"+sr, member.ContentFingerprint)
		if err != nil {
			return err
		}
		if existing != "" {
			printInfo("%s already passes for %s at %s — reconciling", existing, sr, member.ContentFingerprint)
		} else {
			source := o.log
			fields := map[string]any{
				"title":                fmt.Sprintf("%s lower trace — recorded by process advance", sr),
				"purpose":              "lower",
				"transition":           "build->verify",
				"exact_scope":          []string{sr},
				"fingerprint":          member.ContentFingerprint,
				"verdict":              "PASS",
				"sources":              traceSources([]string{"RUN:" + source}),
				"application_revision": gitHead(env.Root),
			}
			if o.body != "" {
				fields["body_md"] = o.body
			}
			if err := authorTrace(env, id, fields); err != nil {
				return err
			}
		}
	}

	// Reconcile until nothing remains: an SR needs two passes (TODO ->
	// IN_PROGRESS, then IN_PROGRESS -> IN_REVIEW) and the epic folds after.
	var ownFail, reached string
	for pass := 0; pass < 4; pass++ {
		rc, err := reconcileOnce(env, piece, true)
		if err != nil {
			return err
		}
		for _, t := range rc.Data.Transitions {
			mark := "  "
			if t.ExternalID != sr && t.ExternalID != piece {
				mark = "  (sibling) "
			}
			fmt.Printf("%sapplied  %s %s->%s (%s)\n", mark, t.ExternalID, t.From, t.To, t.Basis)
			if t.ExternalID == sr {
				reached = t.To
			}
		}
		for _, f := range rc.Data.Fails {
			if f.ExternalID == sr {
				ownFail = f.Reason
				fmt.Printf("  FAIL     %s — %s\n", f.ExternalID, f.Reason)
			} else {
				fmt.Printf("  (sibling) FAIL %s — %s\n", f.ExternalID, f.Reason)
			}
		}
		if len(rc.Data.Transitions) == 0 {
			break
		}
	}

	// The status reached: what reconcile applied for the SR, else what the
	// store serves after the passes.
	finalStatus := reached
	after, err := readDeliveryContextFor(env, piece)
	if err == nil && after.Data.Facts != nil {
		for _, m := range after.Data.Facts.Members {
			if m.ExternalID == sr {
				if finalStatus == "" {
					finalStatus = m.Status
				}
				fmt.Printf("%s: %s\n", sr, finalStatus)
			}
		}
		if after.Data.Facts.Scope.ExternalID != sr {
			fmt.Printf("%s: %s\n", after.Data.Facts.Scope.ExternalID, after.Data.Facts.Scope.Status)
		}
	}
	if ownFail != "" {
		return fmt.Errorf("the lower trace for %s is recorded, but reconcile refuses its transition: %s", sr, ownFail)
	}
	// A trace recorded with nothing advanced is not success: say what the SR
	// still is, so the operator is not left with an exit 0 and a stuck item.
	// The trace is reused on the next run.
	if finalStatus != "" && finalStatus != "IN_REVIEW" && finalStatus != "DONE" {
		return fmt.Errorf("the lower trace for %s is recorded, but reconcile applied no transition and it is still %s — read `process reconcile --piece %s` (dry run) for the proof reconcile is missing; a rerun reuses the trace", sr, finalStatus, piece)
	}
	return nil
}

// ---------------------------------------------------------------- enter (REQ-CROSS-373)

type enterOpts struct {
	gateID    string // --gate-id: a successor id when ENTRY-<scope> is taken
	briefFile string // --brief-file: overrides the packet's entry_brief section
	dryRun    bool   // --dry-run: print the plan, post nothing
}

var (
	enterGateID    string
	enterBriefFile string
	enterDryRun    bool
)

var processEnterCmd = &cobra.Command{
	Use:   "enter <scope>",
	Short: "Open the entry gate for a scope from store facts: the epic plus every member still in FROM, the cold-review trace as prerequisite",
	Long: `Open the human entry gate for the piece you hold, in one call, from what the
store already knows. The gate names the epic plus every member still in an
entry-source state (PROPOSED — a member already entered is skipped and
listed), or the single requirement; its prerequisite is the independent
passing cold-review trace at the current packet aggregate; its brief is the
packet's entry_brief section (the PROCESS.md brief shape: - What:, - Why now:,
- Changes if approved:, - Risk if wrong:, - Recommendation:) or --brief-file;
its transition is PROPOSED->TODO. The selection then moves to phase entry.

It refuses before any write, naming the fact: no delivery facts served (deploy
the server first); packet sections missing or stale (working-set push); no
independent passing cold-review trace at the aggregate (rdd-cold-review, then
author trace --purpose cold-review); an incomplete brief; a DONE or OBSOLETE
epic whose as-built members would enter (demote or reopen it first). --dry-run
prints the plan and posts nothing. The verb records no trace of its own: it
names the cold-review trace the store already holds, so a gate can never be
born before its prerequisite.

As-built members first: an epic whose members split between PROPOSED and
PENDING_VERIFICATION enters in two steps. The first call opens
ENTRY-<scope>-VERIFY (PENDING_VERIFICATION->TODO) naming the as-built members
only, pinned at the epic's packet aggregate so the epic's cold review is its
prerequisite — the store admits it for a still-PROPOSED epic on that passing
review — and says which PROPOSED members and the epic enter in the second
call; once those members are TODO, the second call opens ENTRY-<scope> as
usual. An epic already TODO, READY, IN_PROGRESS or IN_REVIEW with as-built
members gets the verification gate alone.

Successor ids: when ENTRY-<scope> (or -VERIFY) is already closed or withdrawn
— a scope demoted to PROPOSED and re-planned — the verb derives the next free
ENTRY-<scope>-R2, -R3… and names the closed predecessor in the gate; an open
or answered id is never rotated past, not even under --gate-id, which
overrides the id only.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processEnter(env, args[0], enterOpts{gateID: enterGateID, briefFile: enterBriefFile, dryRun: enterDryRun})
	},
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
// packet section of the scope.
func readBrief(env *factoryEnv, scopeKind, scope, sectionKey, briefFile string) (map[string]any, error) {
	if briefFile != "" {
		saved := authorGateBriefFile
		authorGateBriefFile = briefFile
		brief := authorGateBrief()
		authorGateBriefFile = saved
		if brief == nil {
			return nil, fmt.Errorf("--brief-file %s holds no brief object", briefFile)
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

// selectionKind maps the facts' scope kind to the selection vocabulary.
func selectionKind(factsKind string) string {
	if factsKind == "epic" {
		return "epic"
	}
	return "single_sr"
}

// processEnter opens the entry gate for a scope from store facts: refuse on
// any unmet fact before writing, open the gate naming the cold-review trace,
// move the selection to phase entry.
func processEnter(env *factoryEnv, scope string, o enterOpts) error {
	resp, err := readDeliveryContextFor(env, scope)
	if err != nil {
		return err
	}
	facts := resp.Data.Facts
	if facts == nil {
		return errNoFacts(scope, resp.Data.FactsState)
	}
	if !facts.Sections.Complete {
		return fmt.Errorf("packet sections missing or stale for %s: %s — author them and `working-set push` before entry", scope, strings.Join(facts.Sections.Missing, ", "))
	}
	cr := facts.ColdReview
	if cr.Verdict != "pass" || !cr.Independent || cr.TraceExternalID == "" {
		return fmt.Errorf("no independent passing cold-review trace at the current packet aggregate %s for %s (verdict %s, independent %v) — run rdd-cold-review and record its verdict with `author trace --purpose cold-review`", facts.Aggregate, scope, presentPin(cr.Verdict), cr.Independent)
	}

	var proposed, pending, entered []string
	for _, m := range facts.Members {
		switch m.Status {
		case "PROPOSED":
			proposed = append(proposed, m.ExternalID)
		case "PENDING_VERIFICATION":
			pending = append(pending, m.ExternalID)
		default:
			entered = append(entered, fmt.Sprintf("%s (%s)", m.ExternalID, presentPin(m.Status)))
		}
	}

	epic := facts.Scope.Kind == "epic"
	status := facts.Scope.Status
	from := "PROPOSED"
	base := "ENTRY-" + scope
	pin := ""
	var exactScope, notes []string
	switch {
	// REQ-CROSS-422 (EPIC-CLI-022, D7 = A): as-built members enter first. A
	// members-only verification gate names the PENDING_VERIFICATION members,
	// pinned at the epic's aggregate so the epic's cold review is its
	// prerequisite; the store admits it for a PROPOSED owner on that passing
	// review and for an entered one as before. The epic and its PROPOSED
	// members enter in a second call once the as-built members are TODO.
	case epic && len(pending) > 0:
		if status == "DONE" || status == "OBSOLETE" || status == "DEFERRED" {
			return fmt.Errorf("%s is %s — its as-built members %s cannot enter through a members-only gate; demote or reopen the epic first", scope, status, strings.Join(pending, ", "))
		}
		from = "PENDING_VERIFICATION"
		base = "ENTRY-" + scope + "-VERIFY"
		pin = facts.Aggregate
		exactScope = pending
		if status == "PROPOSED" {
			rest := append([]string{scope}, proposed...)
			notes = append(notes,
				fmt.Sprintf("  %s is PROPOSED — not named by this gate: the as-built members enter first on the epic's cold review\n", scope),
				fmt.Sprintf("  second step: once this gate is applied, run `process enter %s` again to open ENTRY-%s naming %s\n", scope, scope, strings.Join(rest, ", ")))
		} else {
			notes = append(notes, fmt.Sprintf("  %s is %s, already entered — this gate alone enters its as-built members\n", scope, presentPin(status)))
			if len(proposed) > 0 {
				notes = append(notes, fmt.Sprintf("  PROPOSED members not named here: %s — run `process enter %s` again once this gate is applied\n", strings.Join(proposed, ", "), scope))
			}
		}
	case epic:
		if status == from {
			exactScope = append(exactScope, scope)
		} else {
			notes = append(notes, fmt.Sprintf("  %s is %s, not %s — not named by this gate; a members-only gate is admitted only once the epic itself has been entered, so enter the epic with its %s members first if it has any\n", scope, presentPin(status), from, status))
		}
		exactScope = append(exactScope, proposed...)
	default:
		if len(pending) > 0 {
			from = "PENDING_VERIFICATION"
		}
		if status != from {
			return fmt.Errorf("nothing to do: %s is %s, not %s", scope, presentPin(status), from)
		}
		exactScope = []string{scope}
	}
	if len(exactScope) == 0 {
		fmt.Printf("nothing to do: every member of %s is already entered (%s)\n", scope, strings.Join(entered, ", "))
		return nil
	}

	brief, err := readBrief(env, selectionKind(facts.Scope.Kind), scope, "entry_brief", o.briefFile)
	if err != nil {
		return err
	}
	gateID, predecessor, err := freeGateID(env, base)
	if err != nil {
		return err
	}
	if o.gateID != "" {
		if err := gateIDFree(env, o.gateID); err != nil {
			return err
		}
		gateID = o.gateID
	}

	fmt.Printf("entry gate %s\n  scope: %s\n  transition: %s->TODO\n  prerequisite: %s (cold-review PASS at %s)\n", gateID, strings.Join(exactScope, ", "), from, cr.TraceExternalID, facts.Aggregate)
	if pin != "" {
		fmt.Printf("  pin: the epic's packet aggregate %s (a members-only verification gate)\n", pin)
	}
	if predecessor != "" {
		fmt.Printf("  predecessor: %s (closed or withdrawn) — this gate succeeds it\n", predecessor)
	}
	if len(entered) > 0 {
		fmt.Printf("  already entered, not named: %s\n", strings.Join(entered, ", "))
	}
	fmt.Print(strings.Join(notes, ""))
	if o.dryRun {
		fmt.Println("dry run — nothing posted")
		return nil
	}

	fields := map[string]any{
		"title":                          fmt.Sprintf("Enter %s — %s", scope, fmt.Sprint(brief["what"])),
		"gate_kind":                      "approval_request",
		"purpose":                        "entry",
		"transition":                     from + "->TODO",
		"exact_scope":                    exactScope,
		"options":                        []map[string]any{{"key": "approve", "label": "Approve entry"}, {"key": "decline", "label": "Do not enter; return to planning"}},
		"recommended_option_key":         "approve",
		"brief":                          brief,
		"prerequisite_gate_external_ids": []string{cr.TraceExternalID},
	}
	if pin != "" {
		fields["evaluated_scope_fingerprint"] = pin
	}
	if predecessor != "" {
		// A closed or withdrawn gate cannot be superseded (its answer entered
		// application), so the link is prose: the successor names it.
		fields["body_md"] = fmt.Sprintf("Successor of %s, which is closed or withdrawn: the scope re-enters at the current packet aggregate %s (after a demotion to PROPOSED and re-planning, or because the earlier id stays reserved).", predecessor, facts.Aggregate)
	}
	if err := authorCreate(env, "gate", gateID, fields); err != nil {
		return err
	}
	if err := workingSetSelect(env, wsSelectOpts{scope: scope, kind: selectionKind(facts.Scope.Kind), phase: "entry"}, time.Now()); err != nil {
		return err
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`, then apply members first with `author advance … --gate %s`", gateID, gateID)
	return nil
}

// ---------------------------------------------------------------- complete (REQ-CROSS-375)

type completeOpts struct {
	log       string // --log: the delivered run's reference (required)
	kind      string // --kind: the evidence run kind (default ci)
	body      string // --body: audit disclosures appended to the completion trace
	gateID    string // --gate-id: a successor id when COMPLETE-<scope> is taken
	briefFile string // --brief-file: overrides the packet's completion_brief section
	dryRun    bool   // --dry-run: print the plan, post nothing
	noFetch   bool   // --no-fetch: skip the fetch (offline fixtures only)
}

var (
	completeLog       string
	completeKind      string
	completeBody      string
	completeGateID    string
	completeBriefFile string
	completeDryRun    bool
	completeNoFetch   bool
)

var processCompleteCmd = &cobra.Command{
	Use:   "complete <scope>",
	Short: "At the delivered revision: record the run, the completion trace at the packet aggregate, and the completion gate naming it — in one call",
	Long: `Run the completion ceremony for the piece you hold, at the delivered revision.
The verb fetches the remote default branch and requires HEAD to be exactly its
tip (an ancestor is "behind", a branch commit is "not on the delivered
branch"); it reads the delivery facts and requires the scope and every
member to be IN_REVIEW or DONE — OBSOLETE and DEFERRED members are excluded
and disclosed, never named. It then records one passing run (--kind ci by
default, --log required) naming the epic and its members at HEAD, records
COMPLETE-TRACE-<scope> PASS at the packet aggregate with the completion
purpose and IN_REVIEW->DONE, opens COMPLETE-<scope> as an approval_request
naming that trace as prerequisite and the epic plus every member still
IN_REVIEW, with the brief from the packet's completion_brief section (or
--brief-file), and moves the selection to phase completion. The trace is
always recorded before the gate — the ordering mistake that produced a stray
gate cannot happen here.

It refuses before any write, naming the fact: HEAD not at the delivered
tip; a failed fetch (--no-fetch is for offline fixtures only); no delivery
facts served; a member not IN_REVIEW (process advance <SR>); the epic not
IN_REVIEW and never completed before (process reconcile --apply folds it);
an incomplete brief; an open or answered gate id (answer or apply it). After
the human answers, apply members first with author advance --kind
requirement, then the epic.

The run names the epic, its user requirement and every member this
completion moves (IN_REVIEW): a DONE sibling an earlier completion accepted
keeps its CURRENT evidence and is never re-posted, while the completion
trace still names every member. A reopened epic — IN_PROGRESS after a defect demotion,
every member back in IN_REVIEW or DONE, its earlier COMPLETE-<scope> closed —
completes in the same call: the successor trace COMPLETE-TRACE-<scope>-R2 is
recorded, process reconcile folds the epic to IN_REVIEW, and the successor
gate COMPLETE-<scope>-R2 opens naming the predecessor. A rebuild that moved
the packet aggregate under the applied entry approval is refused naming
process reapply-entry. A closed COMPLETE-<scope> otherwise derives the next
free -R<n> the same way; --gate-id overrides the id only — an open or
answered id in the series is still refused.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processComplete(env, args[0], completeOpts{
			log: completeLog, kind: completeKind, body: completeBody, gateID: completeGateID,
			briefFile: completeBriefFile, dryRun: completeDryRun, noFetch: completeNoFetch,
		})
	},
}

// deliveredHead checks that HEAD is exactly the fetched tip of the remote
// default branch — the delivered revision (D2, amended for F-CLI017-R1-03) —
// and returns the full sha. A failed fetch is a refusal (N-CLI017-R2-01): a
// stale origin ref that happens to equal HEAD is the same mistake by another
// door. --no-fetch exists for offline fixtures only.
func deliveredHead(root string, noFetch bool) (string, error) {
	branch := "main"
	if ref := gitOut(root, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); ref != "" {
		branch = strings.TrimPrefix(ref, "origin/")
	}
	if !noFetch {
		cmd := exec.Command("git", "fetch", "--quiet", "origin", branch)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("could not fetch origin/%s to confirm the delivered tip: %s — completion runs at the merged revision; use --no-fetch only for offline fixtures", branch, strings.TrimSpace(string(out)))
		}
	}
	head := gitOut(root, "rev-parse", "HEAD")
	tip := gitOut(root, "rev-parse", "origin/"+branch)
	switch {
	case head == "" || tip == "":
		return "", fmt.Errorf("could not resolve HEAD (%q) or origin/%s (%q) — completion runs at the delivered revision", head, branch, tip)
	case head == tip:
		return head, nil
	}
	cmd := exec.Command("git", "merge-base", "--is-ancestor", head, tip)
	cmd.Dir = root
	if cmd.Run() == nil {
		behind := gitOut(root, "rev-list", "--count", head+".."+tip)
		unit := "commits"
		if behind == "1" {
			unit = "commit"
		}
		return "", fmt.Errorf("HEAD %s is %s %s behind the delivered tip origin/%s %s — pull before completing", head[:7], behind, unit, branch, tip[:7])
	}
	return "", fmt.Errorf("HEAD %s is not on the delivered branch origin/%s (tip %s) — merge and check out the merged revision before completing", head[:7], branch, tip[:7])
}

// processComplete records delivered-revision evidence, the completion trace
// and the completion gate in one call, refusing before any write on an unmet
// fact.
func processComplete(env *factoryEnv, scope string, o completeOpts) error {
	if o.log == "" {
		return fmt.Errorf("--log is required: the delivered run's reference (a CI url or the command) is what the completion evidence cites")
	}
	kind := o.kind
	if kind == "" {
		kind = "ci"
	}
	head, err := deliveredHead(env.Root, o.noFetch)
	if err != nil {
		return err
	}
	resp, err := readDeliveryContextFor(env, scope)
	if err != nil {
		return err
	}
	facts := resp.Data.Facts
	if facts == nil {
		return errNoFacts(scope, resp.Data.FactsState)
	}
	if facts.Aggregate == "" {
		return fmt.Errorf("the store serves no packet aggregate for %s — the completion trace has nothing to pin to", scope)
	}

	var traced, toMove, excluded, notReady, rerun, urs []string
	urNotReady := false
	for _, m := range facts.Members {
		switch m.Status {
		case "IN_REVIEW", "DONE":
			traced = append(traced, m.ExternalID)
			if m.Status == "IN_REVIEW" {
				toMove = append(toMove, m.ExternalID)
			}
			// REQ-CROSS-419 (D4a): the posted run at the delivered revision
			// names the epic, its user requirement and every member this
			// completion moves (IN_REVIEW); a DONE sibling — one an earlier
			// completion already accepted — keeps its CURRENT evidence and is
			// never re-posted. Evidence state is not the criterion: every
			// IN_REVIEW member arrives with passing branch evidence (process
			// advance requires it) and still needs the run at HEAD
			// (REQ-CROSS-375, F-CLI022-PR2-01).
			switch {
			case m.Kind == "ur":
				urs = append(urs, m.ExternalID)
			case m.Status == "IN_REVIEW":
				rerun = append(rerun, m.ExternalID)
			}
		case "OBSOLETE", "DEFERRED":
			excluded = append(excluded, fmt.Sprintf("%s (%s)", m.ExternalID, m.Status))
		default:
			notReady = append(notReady, fmt.Sprintf("%s (%s)", m.ExternalID, presentPin(m.Status)))
			if m.Kind == "ur" {
				urNotReady = true
			}
		}
	}
	if len(notReady) > 0 {
		remedy := "take each SR through `process advance <SR>`"
		if urNotReady {
			remedy += "; a user requirement needs its upper trace by hand (`author trace TRACE-UPPER-<UR> --purpose upper --scope <UR> --fingerprint <its content hash> --verdict PASS`) and `process reconcile --apply`"
		}
		return fmt.Errorf("%s has members not yet IN_REVIEW: %s — %s before completing", scope, strings.Join(notReady, ", "), remedy)
	}
	epic := facts.Scope.Kind == "epic"

	// The gate id: the first free id in the COMPLETE-<scope> series with the
	// closed predecessor it succeeds; --gate-id overrides the id only, so a
	// reopened epic is still recognised by its predecessor (F-CLI022-PR2-02).
	gateID, predecessor, err := freeGateID(env, "COMPLETE-"+scope)
	if err != nil {
		return err
	}
	if o.gateID != "" {
		if err := gateIDFree(env, o.gateID); err != nil {
			return err
		}
		gateID = o.gateID
	}

	// REQ-CROSS-419 (EPIC-CLI-022, D4a, SCN-DEMOTE-005): an epic reopened by a
	// defect demotion is IN_PROGRESS with every member back in IN_REVIEW or
	// DONE and its earlier completion gate closed. Its staled completion trace
	// blocks the fold, so this verb records the successor trace naming every
	// member, folds the epic through reconcile, and opens the successor gate
	// — one call, no by-hand sequence.
	reopened := false
	switch {
	case facts.Scope.Status == "IN_REVIEW":
	case facts.Scope.Status == "DONE" && epic:
	case epic && facts.Scope.Status == "IN_PROGRESS" && predecessor != "":
		reopened = true
		if facts.EntryGate.Applied && facts.EntryGate.PinnedAggregate != facts.Aggregate {
			return fmt.Errorf("%s was reopened and its packet aggregate moved under the entry approval (pinned at %s, now %s) — the rebuild changed content, not only code; re-pin with `process reapply-entry %s --decision USER:…` if the move was immaterial, or re-review, before completing", scope, presentPin(facts.EntryGate.PinnedAggregate), facts.Aggregate, scope)
		}
	case epic:
		return fmt.Errorf("%s is %s, not IN_REVIEW — `process reconcile --apply` folds the epic once every member is IN_REVIEW or DONE and its traces pass", scope, presentPin(facts.Scope.Status))
	default:
		return fmt.Errorf("%s is %s, not IN_REVIEW — `process advance %s` before completing", scope, presentPin(facts.Scope.Status), scope)
	}

	var named, gateScope, runTargets []string
	if epic {
		named = append([]string{scope}, traced...)
		gateScope = append([]string{scope}, toMove...)
		if facts.Scope.Status == "DONE" {
			gateScope = toMove
		}
		runTargets = append(append([]string{scope}, urs...), rerun...)
	} else {
		named = []string{scope}
		gateScope = []string{scope}
		runTargets = []string{scope}
	}
	accepted := []string{}
	for _, id := range traced {
		if !contains(runTargets, id) {
			accepted = append(accepted, id)
		}
	}
	if len(gateScope) == 0 || (epic && len(toMove) == 0 && facts.Scope.Status == "DONE") {
		fmt.Printf("nothing to do: %s and every member are already DONE\n", scope)
		return nil
	}

	brief, err := readBrief(env, selectionKind(facts.Scope.Kind), scope, "completion_brief", o.briefFile)
	if err != nil {
		return err
	}
	traceID, existingTrace, err := freeTraceID(env, "COMPLETE-TRACE-"+scope, facts.Aggregate)
	if err != nil {
		return err
	}
	traceLine := traceID
	if existingTrace != "" {
		traceLine = existingTrace + " (an existing PASS at the aggregate, reused — its evidence stands, none re-posted)"
	}

	fmt.Printf("completion of %s at delivered revision %s\n  evidence: %s run %s naming %s\n  trace: %s PASS at packet aggregate %s, scope %s\n  gate: %s naming the trace, scope %s\n",
		scope, head[:7], kind, o.log, strings.Join(runTargets, ", "), traceLine, facts.Aggregate, strings.Join(named, ", "), gateID, strings.Join(gateScope, ", "))
	if len(accepted) > 0 {
		fmt.Printf("  accepted on current evidence, not re-posted: %s\n", strings.Join(accepted, ", "))
	}
	if reopened {
		fmt.Printf("  reopened epic: %s is IN_PROGRESS after a demotion; the successor trace lets reconcile fold it to IN_REVIEW before the gate opens (predecessor %s)\n", scope, predecessor)
	} else if predecessor != "" {
		fmt.Printf("  predecessor: %s (closed or withdrawn) — this gate succeeds it\n", predecessor)
	}
	if len(excluded) > 0 {
		fmt.Printf("  excluded (not moved, disclosed in the trace): %s\n", strings.Join(excluded, ", "))
	}
	if o.dryRun {
		fmt.Println("dry run — nothing posted")
		return nil
	}

	// A reused trace already cites its delivered run: a rerun after a refused
	// gate posts no second identical run (review round 2, nit 9).
	if existingTrace == "" {
		if err := factoryEvidenceRun(env, evidenceOpts{kind: kind, log: o.log, pass: strings.Join(runTargets, ","), revision: head}); err != nil {
			return err
		}
	}

	if existingTrace == "" {
		body := fmt.Sprintf("Completion audit recorded by process complete at delivered revision %s. Evidence: %s run %s naming %s. Members moved by the gate: %s.", head, kind, o.log, strings.Join(runTargets, ", "), strings.Join(gateScope, ", "))
		if len(accepted) > 0 {
			body += " Accepted on their current passing evidence, not re-posted: " + strings.Join(accepted, ", ") + "."
		}
		if len(excluded) > 0 {
			body += " Excluded from the gate (not moved): " + strings.Join(excluded, ", ") + "."
		}
		if reopened {
			body += fmt.Sprintf(" Successor completion of %s, reopened while IN_PROGRESS with every member reviewed; predecessor gate %s.", scope, predecessor)
		}
		if o.body != "" {
			body += "\n\n" + o.body
		}
		fields := map[string]any{
			"title":                fmt.Sprintf("%s completion trace — recorded by process complete at %s", scope, head[:7]),
			"purpose":              "completion",
			"transition":           "IN_REVIEW->DONE",
			"exact_scope":          named,
			"fingerprint":          facts.Aggregate,
			"verdict":              "PASS",
			"sources":              traceSources([]string{"RUN:" + o.log}),
			"application_revision": head,
			"body_md":              body,
		}
		if err := authorTrace(env, traceID, fields); err != nil {
			return err
		}
	} else {
		traceID = existingTrace
	}

	if reopened {
		if err := foldReopenedEpic(env, scope, traceID); err != nil {
			return err
		}
	}

	gateFields := map[string]any{
		"title":                          fmt.Sprintf("Complete %s — %s", scope, fmt.Sprint(brief["what"])),
		"gate_kind":                      "approval_request",
		"purpose":                        "completion",
		"transition":                     "IN_REVIEW->DONE",
		"exact_scope":                    gateScope,
		"options":                        []map[string]any{{"key": "approve", "label": "Approve completion"}, {"key": "decline", "label": "Do not accept; return to build"}},
		"recommended_option_key":         "approve",
		"brief":                          brief,
		"prerequisite_gate_external_ids": []string{traceID},
	}
	if predecessor != "" {
		// An applied gate cannot be superseded (its answer entered
		// application), so the link is prose: the successor names it.
		gateFields["body_md"] = fmt.Sprintf("Successor of %s, which is closed or withdrawn: %s completes on %s at the current packet aggregate %s.", predecessor, scope, traceID, facts.Aggregate)
	}
	if err := authorCreate(env, "gate", gateID, gateFields); err != nil {
		return fmt.Errorf("%w\n  the completion trace %s is recorded; fix the fact the refusal names and rerun — the trace is reused", err, traceID)
	}
	if err := workingSetSelect(env, wsSelectOpts{scope: scope, kind: selectionKind(facts.Scope.Kind), phase: "completion"}, time.Now()); err != nil {
		return err
	}
	fmt.Printf("apply order after the answer: members first (%s), then the epic — `author advance <id> --to DONE --expected IN_REVIEW --gate %s --gate-answer approve --gate-fingerprint <gate Fingerprint>`\n", strings.Join(toMove, ", "), gateID)
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`", gateID)
	return nil
}

// foldReopenedEpic reconciles the piece until nothing remains and requires
// the epic to have reached IN_REVIEW on the successor completion trace; a
// fold that does not happen is a refusal naming reconcile's own FAIL, and no
// gate is opened over it.
func foldReopenedEpic(env *factoryEnv, scope, traceID string) error {
	reached := ""
	var fails []string
	for pass := 0; pass < 4; pass++ {
		rc, err := reconcileOnce(env, scope, true)
		if err != nil {
			return err
		}
		for _, t := range rc.Data.Transitions {
			fmt.Printf("  applied  %s %s->%s (%s)\n", t.ExternalID, t.From, t.To, t.Basis)
			if t.ExternalID == scope {
				reached = t.To
			}
		}
		for _, f := range rc.Data.Fails {
			fmt.Printf("  FAIL     %s — %s\n", f.ExternalID, f.Reason)
			fails = append(fails, fmt.Sprintf("%s — %s", f.ExternalID, f.Reason))
		}
		if len(rc.Data.Transitions) == 0 {
			break
		}
	}
	if reached != "IN_REVIEW" && reached != "DONE" {
		detail := "reconcile applied no transition for it"
		if len(fails) > 0 {
			detail = strings.Join(fails, "; ")
		}
		return fmt.Errorf("the successor completion trace %s is recorded, but reconcile did not fold %s to IN_REVIEW (it is still IN_PROGRESS): %s — read `process reconcile --piece %s` for the proof it is missing; a rerun reuses the trace and opens no gate until the epic folds", traceID, scope, detail, scope)
	}
	return nil
}
