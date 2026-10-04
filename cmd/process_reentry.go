package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

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
	if _, err := authorCreate(env, "gate", gateID, fields); err != nil {
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
