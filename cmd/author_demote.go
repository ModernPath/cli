package cmd

// REQ-CROSS-421 (EPIC-CLI-022): `author demote` — the sanctioned way to reopen
// a delivered item. It opens the demotion gate PROCESS.md §Attributable
// demotions names (purpose demotion, the destination by basis, the human's
// USER: reason, no prerequisite trace) and, after the human's answer, applies
// it: the server runs the invalidation, retires the item's own entry on a
// demotion to PROPOSED (a defect keeps it, REQ-CROSS-435), and moves the user
// requirement and the epic that follow on the same gate
// (REQ-CROSS-418/419). The dedicated flag set never rebinds `author advance`'s
// --to (author_advance_flags_test.go documents the hazard).

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var demotionDestinations = map[string]string{
	"reversed-decision": "PROPOSED",
	"defect":            "IN_PROGRESS",
	"superseded":        "OBSOLETE",
}

type demoteOpts struct {
	id, kind, to, basis, reason, supersededBy, gateID string
}

var (
	demoteTo           string
	demoteBasis        string
	demoteReason       string
	demoteSupersededBy string
	demoteGateID       string
	demoteKind         string
	demoteApply        bool
)

var authorDemoteCmd = &cobra.Command{
	Use:   "demote <external-id>",
	Short: "Reopen a delivered item on an attributable demotion gate: --to PROPOSED|IN_PROGRESS|OBSOLETE --basis reversed-decision|defect|superseded --reason USER:…; --apply after the answer",
	Long: `Send delivered work back with one attributable decision (PROCESS.md
§Attributable demotions). The item must be IN_REVIEW or DONE; its owning epic
and the user requirement that requires it follow on the same gate, and the
siblings the decision did not touch keep their evidence.

Open (no --apply): refuses locally — before any request — without a
--reason that is a USER: source (USER:<date>:<why>), when the basis does not
match the destination (reversed-decision -> PROPOSED, defect -> IN_PROGRESS,
superseded -> OBSOLETE, which also needs --superseded-by <id>); then reads
the item's current status from the store as the FROM and refuses, before any
write, an item that is not IN_REVIEW or DONE; then posts one gate DEMOTE-<id> (purpose
demotion, approval_request, transition <from>-><to>, the item as its exact
scope, approve/decline options, a brief in plain words, the reason and the
basis as sources). A demotion gate names no prerequisite trace and needs
none: it is an attributable decision, not a fingerprint check. --gate-id
names a successor when DEMOTE-<id> is taken; --kind epic demotes an epic.

Apply (--apply): after the human answers approve (Mission Control, or
factory answer DEMOTE-<id> --options approve --text "USER:<date>: …"), reads
the answered gate and advances the item on it — the gate id, its current
fingerprint and the answer are read from the store, never typed — then
prints the server's transition basis, the dependents that followed (the
user requirement and the epic) and the next step: for PROPOSED, re-plan and
cold-review, then process enter opens the successor entry gate; for
IN_PROGRESS, rebuild red-first (rdd-build) and process complete re-completes
the epic; for OBSOLETE, the epic's completion excludes the item.

A delegated agent is denied this verb by the subagent guard; the permission
placement is ask, beside process supersede and process reenter.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if demoteApply {
			return authorDemoteApply(env, args[0], demoteKind, demoteGateID)
		}
		return authorDemoteOpen(env, demoteOpts{
			id: args[0], kind: demoteKind, to: demoteTo, basis: demoteBasis, reason: demoteReason,
			supersededBy: demoteSupersededBy, gateID: demoteGateID,
		})
	},
}

func init() {
	authorDemoteCmd.Flags().StringVar(&demoteTo, "to", "", "the destination: PROPOSED (reversed-decision), IN_PROGRESS (defect) or OBSOLETE (superseded)")
	authorDemoteCmd.Flags().StringVar(&demoteBasis, "basis", "", "the basis: reversed-decision | defect | superseded")
	authorDemoteCmd.Flags().StringVar(&demoteReason, "reason", "", "the human's reason as a USER: source (USER:<date>:<why>)")
	authorDemoteCmd.Flags().StringVar(&demoteSupersededBy, "superseded-by", "", "the record that supersedes the item (required with --basis superseded)")
	authorDemoteCmd.Flags().StringVar(&demoteGateID, "gate-id", "", "the gate id to open or apply (default DEMOTE-<id>; a successor when that id is taken)")
	authorDemoteCmd.Flags().StringVar(&demoteKind, "kind", "requirement", "requirement | epic")
	authorDemoteCmd.Flags().BoolVar(&demoteApply, "apply", false, "apply the answered demotion gate: advance the item on it and report what followed")
}

// authorDemoteOpen posts the demotion gate after the local checks.
func authorDemoteOpen(env *factoryEnv, o demoteOpts) error {
	if !strings.HasPrefix(o.reason, "USER:") {
		return fmt.Errorf("a demotion carries the human's reason as a USER: source — pass --reason USER:<date>:<why>; nothing was opened")
	}
	destination, known := demotionDestinations[o.basis]
	if !known {
		return fmt.Errorf("--basis %q is not a demotion basis — one of reversed-decision (to PROPOSED), defect (to IN_PROGRESS) or superseded (to OBSOLETE); nothing was opened", o.basis)
	}
	if o.to != destination {
		return fmt.Errorf("basis %s sends an item to %s, not %s — reversed-decision -> PROPOSED, defect -> IN_PROGRESS, superseded -> OBSOLETE; nothing was opened", o.basis, destination, presentPin(o.to))
	}
	if o.basis == "superseded" && o.supersededBy == "" {
		return fmt.Errorf("a superseded demotion names the record that supersedes the item — pass --superseded-by <id>; nothing was opened")
	}
	kind := o.kind
	if kind == "" {
		kind = "requirement"
	}
	from, err := readRecordStatus(env, kind, o.id)
	if err != nil {
		return err
	}
	if from != "IN_REVIEW" && from != "DONE" {
		return fmt.Errorf("only delivered work is demoted: %s is %s, not IN_REVIEW or DONE; nothing was opened", o.id, presentPin(from))
	}
	gateID := o.gateID
	if gateID == "" {
		gateID = "DEMOTE-" + o.id
	}
	if err := gateIDFree(env, gateID); err != nil {
		return err
	}

	fields := map[string]any{
		"title":       fmt.Sprintf("Demote %s to %s — %s", o.id, o.to, o.basis),
		"gate_kind":   "approval_request",
		"purpose":     "demotion",
		"transition":  from + "->" + o.to,
		"exact_scope": []string{o.id},
		"basis":       o.basis,
		"options": []map[string]any{
			{"key": "approve", "label": "Approve the demotion"},
			{"key": "decline", "label": "Keep it as delivered"},
		},
		"recommended_option_key": "approve",
		"brief":                  demotionBrief(o, from),
		"sources":                []map[string]any{{"kind": "user", "ref": o.reason}},
	}
	if o.supersededBy != "" {
		fields["superseded_by"] = o.supersededBy
	}
	fmt.Printf("demotion gate %s\n  item: %s (%s -> %s, basis %s)\n  reason: %s\n", gateID, o.id, from, o.to, o.basis, o.reason)
	if err := authorCreate(env, "gate", gateID, fields); err != nil {
		return err
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`, then run `author demote %s --apply`", gateID, o.id)
	return nil
}

func demotionBrief(o demoteOpts, from string) map[string]any {
	what := map[string]string{
		"PROPOSED":    fmt.Sprintf("Send %s back to planning: the decision it rests on is reversed.", o.id),
		"IN_PROGRESS": fmt.Sprintf("Send %s back to build: its delivered behaviour is defective.", o.id),
		"OBSOLETE":    fmt.Sprintf("Retire %s: it is superseded by %s.", o.id, o.supersededBy),
	}[o.to]
	changes := map[string]string{
		"PROPOSED":    "The item returns to PROPOSED; its evidence, its traces (the plan's cold review included) and its own entry approval are invalidated; the user requirement that requires it and its epic follow — the epic back to planning. Siblings keep their evidence and approval.",
		"IN_PROGRESS": "The item returns to IN_PROGRESS; its own evidence, lower trace and the completion traces are invalidated while the epic's cold review and the user requirement's upper validation stand; the user requirement and the epic follow to IN_PROGRESS. Siblings keep their evidence.",
		"OBSOLETE":    "The item becomes OBSOLETE with the replacement linked; the evidence that stood on it is invalidated; the user requirement and the epic that were done return to IN_PROGRESS; the epic's completion excludes the item.",
	}[o.to]
	return map[string]any{
		"what":                what,
		"why_now":             fmt.Sprintf("%s is %s and %s. %s", o.id, from, map[string]string{"PROPOSED": "the decision has changed", "IN_PROGRESS": "a defect was found in what shipped", "OBSOLETE": "a newer record replaces it"}[o.to], o.reason),
		"changes_if_approved": changes,
		"risk_if_wrong":       "Reversible: a demoted item re-enters or re-completes through the ordinary ceremony; declining keeps it as delivered.",
		"recommendation":      "approve — the reason is recorded on the gate and the loop reconciles the consequences.",
	}
}

// authorDemoteApply advances the item on its answered demotion gate.
func authorDemoteApply(env *factoryEnv, id, kind, gateID string) error {
	if kind == "" {
		kind = "requirement"
	}
	if gateID == "" {
		gateID = "DEMOTE-" + id
	}
	status, body, err := env.call("GET",
		fmt.Sprintf("/api/v1/sync/gates/%s?system_id=%d", url.PathEscape(gateID), env.SystemID), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		if err := gateShowError(status, body, gateID); err != nil && strings.Contains(err.Error(), "does not exist") {
			return fmt.Errorf("no demotion gate %s — open it first with `author demote %s --to … --basis … --reason USER:…`", gateID, id)
		}
		return gateShowError(status, body, gateID)
	}
	gate, _ := dataOf(body)["gate"].(map[string]any)
	switch str(gate, "state") {
	case "open":
		return fmt.Errorf("%s is open — answer it first (Mission Control, or `factory answer %s --options approve --text \"USER:<date>: …\"`); nothing was applied", gateID, gateID)
	case "answered":
	case "closed":
		return fmt.Errorf("%s is closed — its answer was already applied; nothing to do", gateID)
	default:
		return fmt.Errorf("%s is %s — a demotion is applied from an answered gate; nothing was applied", gateID, presentPin(str(gate, "state")))
	}
	if !contains(stringSlice(gate["chosen_option_keys"]), "approve") {
		return fmt.Errorf("%s was answered %s, not approve — the item stays as delivered; nothing was applied", gateID, presentPin(strings.Join(stringSlice(gate["chosen_option_keys"]), ",")))
	}
	from, to, ok := strings.Cut(str(gate, "transition"), "->")
	if !ok {
		return fmt.Errorf("%s carries no FROM->TO transition — not a demotion gate this verb can apply", gateID)
	}
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	// The gate read serves `fingerprint` — the shadow value `advance
	// --gate-fingerprint` guards on; the row's content_fingerprint is the
	// fallback for a server that predates it (F-CLI022-PR2-03).
	if err := authorAdvance(env, kind, id, to, from, gateID, firstNonEmpty(str(gate, "fingerprint"), str(gate, "content_fingerprint")), "approve", ""); err != nil {
		return err
	}
	switch to {
	case "PROPOSED":
		printInfo("next: re-plan %s and record a fresh cold review; `process enter` then opens its successor entry gate (the siblings' approval under the shared gate stands)", id)
	case "IN_PROGRESS":
		printInfo("next: rebuild %s red-first (rdd-build); `process complete` re-completes the epic on the siblings' current evidence", id)
	case "OBSOLETE":
		printInfo("next: nothing for %s — the epic's completion excludes it; its user requirement and epic rebuild on what replaces it", id)
	}
	return nil
}

// readRecordStatus reads one record's current lifecycle status from the store's
// list reads — the same reads working-set pull renders — since no single-record
// status read exists.
func readRecordStatus(env *factoryEnv, kind, id string) (string, error) {
	if kind == "epic" {
		epics, err := fetchList(env, fmt.Sprintf("/api/v1/sync/epics?system_id=%d", env.SystemID), "epics")
		if err != nil {
			return "", err
		}
		for _, e := range epics {
			m, _ := e.(map[string]any)
			if str(m, "code") == id || str(m, "external_id") == id {
				return str(m, "process_status"), nil
			}
		}
		return "", fmt.Errorf("%s is not an epic the store serves", id)
	}
	srs, urs, err := fetchRequirementLists(env, fmt.Sprintf("/api/v1/sync/requirements?system_id=%d", env.SystemID))
	if err != nil {
		return "", err
	}
	for _, r := range append(append([]any{}, srs...), urs...) {
		m, _ := r.(map[string]any)
		if str(m, "external_id") == id {
			return str(m, "work_status"), nil
		}
	}
	return "", fmt.Errorf("%s is not a requirement the store serves", id)
}
