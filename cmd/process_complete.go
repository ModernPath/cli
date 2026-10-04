package cmd

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// completionTraceBody is the audit body a completion trace carries: the
// delivered revision, the run and the targets it names, and the members the
// gate moves. traceRunTargets reads the "naming" clause back out of it, so
// the writer and the reader share one format here rather than agreeing by
// hand in two places (BACKLOG-TOOL-100).
func completionTraceBody(head, kind, log string, runTargets, gateScope []string) string {
	return fmt.Sprintf("Completion audit recorded by process complete at delivered revision %s. Evidence: %s run %s naming %s. Members moved by the gate: %s.",
		head, kind, log, strings.Join(runTargets, ", "), strings.Join(gateScope, ", "))
}

// traceRunTargets recovers the targets the run a completion trace cites
// already names, from the body completionTraceBody writes. ok is false when
// the body carries no such sentence — an older trace, or one written by hand.
// The caller then posts the run for every target rather than reading silence
// as coverage (BACKLOG-TOOL-100).
//
// The cut is on the first " naming " after "Evidence: ", so a --log value
// carrying that literal ends the list early and leaves real targets out of
// the covered set. That errs toward posting the run for more targets than
// strictly needed — a duplicate row, never a stranded member — which is the
// direction this parser is allowed to be wrong in.
func traceRunTargets(body string) (targets []string, ok bool) {
	_, after, found := strings.Cut(body, "Evidence: ")
	if !found {
		return nil, false
	}
	_, list, found := strings.Cut(after, " naming ")
	if !found {
		return nil, false
	}
	if end := strings.Index(list, ". "); end >= 0 {
		list = list[:end]
	}
	for _, t := range strings.Split(strings.TrimSuffix(strings.TrimSpace(list), "."), ",") {
		if t = strings.TrimSpace(t); t != "" {
			targets = append(targets, t)
		}
	}
	return targets, len(targets) > 0
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
trace still names every member.

When a COMPLETE-TRACE-<scope> in the series already PASSes at the unchanged
packet aggregate it is reused rather than recorded again, and the run is
posted only for the targets that trace's own run did not name — no run at
all when it named them all, and the whole run when its body carries no
readable target list, since unread coverage is never taken for coverage. A
trace is immutable, so the remainder run is not recorded on it: a repeat at
the same aggregate posts that remainder again.

A reopened epic — IN_PROGRESS after a defect demotion,
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
			remedy += "; a user requirement needs its upper trace by hand (`author trace TRACE-UPPER-<UR> --purpose upper --scope <UR> --verdict PASS`) and `process reconcile --apply`"
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
		// A DONE epic with a member still IN_REVIEW would open a members-only
		// completion gate (gateScope = toMove below), and the server refuses
		// that for a done owner however many members it names: "does not reach
		// into a done, obsolete or deferred epic; demote or reopen the epic
		// first". The refusal used to arrive after the delivered run and the
		// completion trace were already written, leaving an unusable trace
		// behind; refuse here instead, before the first write. A DONE epic with
		// nothing stranded still falls through to "nothing to do".
		if len(toMove) > 0 {
			return fmt.Errorf("%s is DONE and %s still IN_REVIEW — a completion gate naming only members does not reach into a done epic and the server refuses it, so nothing was posted; reopen the epic first with `author demote %s --kind epic --to IN_PROGRESS --basis defect --reason USER:<date>:<why>`, answer that gate, `author demote %s --kind epic --apply`, then rerun `process complete %s` — the reopened-epic arm records the successor trace, folds the epic and opens the successor gate",
				scope, strings.Join(toMove, ", "), scope, scope, scope)
		}
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
	traceID, existingTrace, existingRecord, err := freeTraceID(env, "COMPLETE-TRACE-"+scope, facts.Aggregate)
	if err != nil {
		return err
	}

	// BACKLOG-TOOL-100 (defect against REQ-CROSS-419, D4a: the posted run names
	// every member this completion moves). A trace that already PASSes at the
	// unchanged aggregate is reused — but the run it cites named the targets of
	// the completion that recorded it, so a member this completion adds would
	// get no delivered-revision evidence at all and the gate would refuse "not
	// yet" for it. The rerun posts the remainder: the targets that run does not
	// already name. Coverage that cannot be read is never assumed — the whole
	// run is posted, which costs one duplicate row and strands nothing.
	postTargets, coveredHere := runTargets, []string{}
	traceLine := traceID
	if existingTrace != "" {
		traceLine = existingTrace + " (an existing PASS at the aggregate, reused)"
		covered, ok := traceRunTargets(str(existingRecord, "body_md"))
		if !ok {
			traceLine = existingTrace + " (an existing PASS at the aggregate, reused — the targets its run names could not be read, so the run is posted for every target)"
		} else {
			postTargets = nil
			for _, t := range runTargets {
				if contains(covered, t) {
					coveredHere = append(coveredHere, t)
				} else {
					postTargets = append(postTargets, t)
				}
			}
		}
	}
	evidenceLine := fmt.Sprintf("%s run %s naming %s", kind, o.log, strings.Join(postTargets, ", "))
	if len(postTargets) == 0 {
		evidenceLine = "none — the reused trace's run already names every target"
	}

	fmt.Printf("completion of %s at delivered revision %s\n  evidence: %s\n  trace: %s PASS at packet aggregate %s, scope %s\n  gate: %s naming the trace, scope %s\n",
		scope, head[:7], evidenceLine, traceLine, facts.Aggregate, strings.Join(named, ", "), gateID, strings.Join(gateScope, ", "))
	if len(coveredHere) > 0 {
		fmt.Printf("  already named by the reused trace's run, not re-posted: %s\n", strings.Join(coveredHere, ", "))
	}
	// The covered set is read off the reused trace's own body, and a trace is
	// immutable — this run is never recorded on it. The delivery facts carry
	// no revision-scoped evidence field either: `evidence_state` reads passing
	// for every IN_REVIEW member before any completion run (REQ-CROSS-375,
	// F-CLI022-PR2-01), so it cannot stand in for coverage at HEAD. A repeat
	// at the same aggregate therefore posts this remainder again. That is a
	// duplicate row, not a wrong one, and the plan says so rather than leaving
	// it to be discovered.
	if existingTrace != "" && len(postTargets) > 0 {
		fmt.Printf("  the reused trace is immutable, so this run is not recorded on it: a repeat at this aggregate posts the remainder again\n")
	}
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

	// REQ-CROSS-419 (D4a): the posted run names every member this completion
	// moves, and no more. A reused trace already cites the run recorded with
	// it, so that run is not posted a second time — only the targets it left
	// uncovered (BACKLOG-TOOL-100).
	if len(postTargets) > 0 {
		if err := factoryEvidenceRun(env, evidenceOpts{kind: kind, log: o.log, pass: strings.Join(postTargets, ","), revision: head}); err != nil {
			return err
		}
	}

	if existingTrace == "" {
		body := completionTraceBody(head, kind, o.log, runTargets, gateScope)
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

	// The epic's status after the fold is what its apply recipe must expect;
	// the served facts predate the fold (a reopened epic reads IN_PROGRESS there).
	epicStatus := facts.Scope.Status
	if reopened {
		reached, err := foldReopenedEpic(env, scope, traceID)
		if err != nil {
			return err
		}
		epicStatus = reached
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
	if _, err := authorCreate(env, "gate", gateID, gateFields); err != nil {
		return fmt.Errorf("%w\n  the completion trace %s is recorded; fix the fact the refusal names and rerun — the trace is reused", err, traceID)
	}
	if err := workingSetSelect(env, wsSelectOpts{scope: scope, kind: selectionKind(facts.Scope.Kind), phase: "completion"}, time.Now()); err != nil {
		return err
	}
	// Two recipes, not one: `author advance --kind` defaults to requirement, so
	// the member line moves every SR and the UR, and the epic needs its own line
	// with --kind epic. Pasting the member line for the epic is refused 422.
	if len(toMove) == 0 {
		// A reopened epic whose members are all DONE moves nothing but itself.
		fmt.Println("apply order after the answer: no members to advance")
	} else {
		fmt.Printf("apply order after the answer: members first (%s) — `author advance <id> --to DONE --expected IN_REVIEW --gate %s --gate-answer approve --gate-fingerprint <gate Fingerprint>` (--kind defaults to requirement, which is what an SR and the UR need)\n", strings.Join(toMove, ", "), gateID)
	}
	if epic {
		fmt.Printf("then the epic itself — `author advance %s --kind epic --to DONE --expected %s --gate %s --gate-answer approve --gate-fingerprint <gate Fingerprint>`\n", scope, epicStatus, gateID)
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`", gateID)
	return nil
}

// foldReopenedEpic reconciles the piece until nothing remains and requires
// the epic to have reached IN_REVIEW — the one state reconcile's epic edges
// lead to — on the successor completion trace; a fold that does not happen,
// and a fold that lands anywhere else, are both refusals naming the fact, and
// no gate is opened over either. It returns the state the epic reached, which
// is what its own apply recipe expects.
func foldReopenedEpic(env *factoryEnv, scope, traceID string) (string, error) {
	reached := ""
	var fails []string
	for pass := 0; pass < 4; pass++ {
		rc, err := reconcileOnce(env, scope, true)
		if err != nil {
			return "", err
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
	if reached == "IN_REVIEW" {
		return reached, nil
	}
	if reached == "" {
		detail := "reconcile applied no transition for it"
		if len(fails) > 0 {
			detail = strings.Join(fails, "; ")
		}
		return "", fmt.Errorf("the successor completion trace %s is recorded, but reconcile did not fold %s to IN_REVIEW (it is still IN_PROGRESS): %s — read `process reconcile --piece %s` for the proof it is missing; a rerun reuses the trace and opens no gate until the epic folds", traceID, scope, detail, scope)
	}
	// IN_REVIEW is the only state this fold has: reconcile's epic edges are
	// entry -> IN_PROGRESS and IN_PROGRESS -> IN_REVIEW, and the completion
	// step is human-gated. Any other state means the server's edges changed
	// under this verb, so it stops instead of opening a gate and printing an
	// apply recipe (`--to DONE --expected DONE` is refused 422).
	unexpected := fmt.Sprintf("the successor completion trace %s is recorded, but the fold of %s reached an unexpected state %s, not IN_REVIEW — no gate was opened and no apply recipe is printed; read `process reconcile --piece %s` and the epic's status before rerunning (a rerun reuses the trace)", traceID, scope, reached, scope)
	if len(fails) > 0 {
		unexpected += ". Reconcile also reported: " + strings.Join(fails, "; ")
	}
	return "", errors.New(unexpected)
}
