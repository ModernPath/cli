package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

type advanceOpts struct {
	piece string // --piece: the held selection the SR belongs to, when ambiguous
	log   string // --log: the RUN: source the lower trace cites
	body  string // --body: the trace's verdict details
}

var (
	advanceLog  string
	advanceBody string
	advanceAll  bool
)

var processAdvanceCmd = &cobra.Command{
	Use:   "advance <SR> | --all --piece <EPIC>",
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
member of a piece you hold (--piece names one when you hold several; an
SR that is itself one of the pieces you hold needs none); a TODO SR with no
live entry of its own at the current packet aggregate; a server that serves
no delivery facts. A second call on an SR already
IN_REVIEW at its current hash reports nothing to do. Transitions and FAILs
of sibling members are printed as information and never fail the advance;
a FAIL naming the SR itself does, after the trace is recorded.

--all --piece <EPIC> --log <RUN> does the same for every system requirement
of the piece in one call, each through the same path: it prints each SR it
moved and, for each one it did not, the refusal above that names why. It
exits non-zero when any SR was not advanced; the ones that were stay moved.`,
	Args: cobra.RangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		o := advanceOpts{piece: processPiece, log: advanceLog, body: advanceBody}
		if advanceAll != (len(args) == 0) {
			return fmt.Errorf("name one system requirement, or --all --piece <EPIC> for every one the piece holds")
		}
		env, err := authorEnv()
		if err != nil {
			return err
		}
		if advanceAll {
			return processAdvanceAll(env, o)
		}
		return processAdvance(env, args[0], o)
	},
}

// processAdvanceAll runs the per-SR advance for every system requirement of
// the piece (REQ-CROSS-445). A refusal is reported and the rest continue.
func processAdvanceAll(env *factoryEnv, o advanceOpts) error {
	if o.log == "" {
		return fmt.Errorf("--log is required: the passing run's command or report is what the lower trace cites as its RUN: source")
	}
	if o.piece == "" {
		return fmt.Errorf("--all needs --piece <EPIC>: the piece whose system requirements to advance")
	}
	resp, err := readDeliveryContextFor(env, o.piece)
	if err != nil {
		return err
	}
	if resp.Data.Facts == nil {
		return errNoFacts(o.piece, resp.Data.FactsState)
	}
	var srs []string
	for _, m := range resp.Data.Facts.Members {
		if m.Kind != "ur" {
			srs = append(srs, m.ExternalID)
		}
	}
	if len(srs) == 0 {
		fmt.Printf("nothing to do: %s holds no system requirement\n", o.piece)
		return nil
	}
	var notAdvanced []string
	for _, sr := range srs {
		fmt.Printf("── %s\n", sr)
		if err := processAdvance(env, sr, o); err != nil {
			fmt.Printf("✗ %s not advanced: %v\n", sr, err)
			notAdvanced = append(notAdvanced, sr)
		}
	}
	if len(notAdvanced) > 0 {
		return fmt.Errorf("%d of %d system requirements not advanced (%s) — each reason is printed above", len(notAdvanced), len(srs), strings.Join(notAdvanced, ", "))
	}
	return nil
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
		return fmt.Errorf("%s is a user requirement — process advance takes a system requirement; once its required SRs are IN_REVIEW, record its upper trace by hand (`author trace TRACE-UPPER-%s --purpose upper --scope %s --verdict PASS --source RUN:…` — the pin defaults to its content hash %s and the transition to build->verify) and run `process reconcile --apply`", sr, sr, sr, presentPin(member.ContentFingerprint))
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
		// Reconcile promotes TODO -> IN_PROGRESS only when the member's entry is
		// current at the CURRENT aggregate; a trace recorded before that would
		// advance nothing. REQ-CROSS-459: a served per-member fact is reconcile's
		// own test and decides; without it the epic-level pin stands in.
		epicPinCurrent := facts.EntryGate.Applied && facts.EntryGate.PinnedAggregate == facts.Aggregate
		if member.EntryCurrent != nil && *member.EntryCurrent {
			break
		}
		if member.EntryCurrent != nil && epicPinCurrent {
			return fmt.Errorf("%s is %s and has no live entry of its own at the current packet aggregate %s — the entry gate of %s is applied there, but no applied entry names %s at this aggregate, so reconcile would not move it and a lower trace would advance nothing; read `process reconcile --piece %s` (dry run) for what the store holds for it. Nothing was written", sr, member.Status, facts.Aggregate, piece, sr, piece)
		}
		if !epicPinCurrent {
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
		id, existing, _, err := freeTraceID(env, "TRACE-LOWER-"+sr, member.ContentFingerprint)
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
