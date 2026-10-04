package cmd

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------- enter (REQ-CROSS-373)

type enterOpts struct {
	gateID     string // --gate-id: a successor id when ENTRY-<scope> is taken
	briefFile  string // --brief-file: overrides the packet's entry_brief section
	dryRun     bool   // --dry-run: print the plan, post nothing
	allowDrift string // --allow-drift: the USER: source accepting reconnaissance drift (SR-CLI-028-002)
	noFetch    bool   // --no-fetch: skip the fetch of the default branch (offline fixtures only)
}

var (
	enterGateID     string
	enterBriefFile  string
	enterDryRun     bool
	enterAllowDrift string
	enterNoFetch    bool
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
the server first); no independent passing cold-review trace at the aggregate
(rdd-cold-review, then author trace --purpose cold-review); packet sections
missing (working-set push); an incomplete brief; a DONE or OBSOLETE epic whose
as-built members would enter (demote or reopen it first). --dry-run prints the
plan and posts nothing.

A section whose content is unchanged but whose scope context moved (a record
edit moves it) is re-stamped rather than refused: once the cold review
passes, the verb re-puts the section's served content under its served
fingerprint, so a concurrent edit conflicts, then re-reads and refuses only
the sections still missing, by name. --dry-run names the sections it would
re-stamp and writes nothing. --brief-file takes a JSON brief object or the
markdown brief bullets; a file that is neither is refused with the parse
error. The verb records no trace of its own: it
names the cold-review trace the store already holds, so a gate can never be
born before its prerequisite.

Reconnaissance drift (SR-CLI-028-002): the selection must record the
revision the packet was reconnoitred at (working-set select --recon-revision;
missing, the verb refuses before the facts read). The verb fetches the remote
default branch (--no-fetch skips the fetch, offline fixtures only) and compares
its tip: a tip equal to the revision, or an ancestor of it (a packet
reconnoitred on a branch ahead of main), is current; otherwise the paths
changed from the merge-base to the tip are intersected with the paths the
packet cites as CODE: or TEST:, and a non-empty intersection refuses naming
the tip and each path. --allow-drift USER:<date>:<why> accepts the drift on
the human's word and the gate body records the source, the tip and the paths;
in a two-call entry both calls run the check. Material drift makes the packet
and its reviews stale (PROCESS.md): re-reconnoitre rather than accept by
habit.

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
		return processEnter(env, args[0], enterOpts{
			gateID: enterGateID, briefFile: enterBriefFile, dryRun: enterDryRun,
			allowDrift: enterAllowDrift, noFetch: enterNoFetch,
		})
	},
}

// processEnter opens the entry gate for a scope from store facts: refuse on
// any unmet fact before writing, open the gate naming the cold-review trace,
// move the selection to phase entry.
func processEnter(env *factoryEnv, scope string, o enterOpts) error {
	// SR-CLI-028-002 C3: a drift acceptance is the human's word — refused
	// before any request when it is not a USER: source.
	if o.allowDrift != "" && !strings.HasPrefix(o.allowDrift, "USER:") {
		return fmt.Errorf("--allow-drift accepts reconnaissance drift on the human's word: pass a USER:<date>:<why> source, got %q", o.allowDrift)
	}
	// SR-CLI-028-002 C1: the selection's reconnaissance revision is read
	// before the facts — a packet with no recorded revision cannot be
	// compared, so entry refuses naming the verb that records one.
	recon, err := selectionReconRevision(env, scope)
	if err != nil {
		return err
	}
	if recon == "" {
		return fmt.Errorf("the selection records no reconnaissance revision — `working-set select %s --recon-revision <sha>` records the revision the packet was reconnoitred at; enter again after", scope)
	}
	resp, err := readDeliveryContextFor(env, scope)
	if err != nil {
		return err
	}
	facts := resp.Data.Facts
	if facts == nil {
		return errNoFacts(scope, resp.Data.FactsState)
	}
	// REQ-CROSS-446: the cold review is checked before anything is written —
	// the re-stamp below is the only write before the gate, and it happens
	// only for a scope that has passed its review.
	if err := passingColdReview(facts, scope); err != nil {
		return err
	}
	// SR-CLI-028-002 C2: the default branch is compared against the
	// reconnaissance revision before any write — the section re-stamp below
	// included; in a two-call entry (REQ-CROSS-422) each call runs it.
	driftLine, err := reconDrift(env, scope, selectionKind(facts.Scope.Kind), recon, o.allowDrift, o.noFetch)
	if err != nil {
		return err
	}
	if !facts.Sections.Complete {
		stale, err := staleServedSections(env, selectionKind(facts.Scope.Kind), scope, facts.Sections.Missing)
		if err != nil {
			return err
		}
		if len(stale) > 0 && o.dryRun {
			fmt.Printf("would re-stamp %d section(s) whose content is unchanged but whose scope context moved: %s\n", len(stale), strings.Join(sectionKeys(stale), ", "))
			facts.Sections.Missing = slices.DeleteFunc(slices.Clone(facts.Sections.Missing), func(k string) bool { return slices.Contains(sectionKeys(stale), k) })
			facts.Sections.Complete = len(facts.Sections.Missing) == 0
		} else if len(stale) > 0 {
			if err := restampSections(env, selectionKind(facts.Scope.Kind), scope, stale); err != nil {
				return err
			}
			if resp, err = readDeliveryContextFor(env, scope); err != nil {
				return err
			}
			if facts = resp.Data.Facts; facts == nil {
				return errNoFacts(scope, resp.Data.FactsState)
			}
			if err := passingColdReview(facts, scope); err != nil {
				return err
			}
		}
		if !facts.Sections.Complete {
			return fmt.Errorf("packet sections missing or stale for %s: %s — author them and `working-set push` before entry", scope, strings.Join(facts.Sections.Missing, ", "))
		}
	}
	cr := facts.ColdReview

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
		} else if enteredEpic(status) {
			// BACKLOG-TOOL-58: the epic is not named because it is already
			// entered, so this is a members-only entry gate over its NEW
			// PROPOSED members — a success, not a refusal, and the note says
			// so. With no pin the store would resolve the lone member's OWN
			// aggregate as the subject, and the epic-scoped cold-review trace
			// is never pinned there, so the open would be refused for a
			// prerequisite "pinned elsewhere". Carry the epic's aggregate,
			// the one its cold review holds, as the verification arm does.
			pin = facts.Aggregate
			notes = append(notes, fmt.Sprintf("  %s is %s, already entered — not named by this gate; it enters the epic's new PROPOSED members on the epic's own cold review\n", scope, presentPin(status)))
		} else {
			notes = append(notes, fmt.Sprintf("  %s is %s, not %s — not named by this gate; a members-only gate is admitted only once the epic itself has been entered, so enter the epic first\n", scope, presentPin(status), from))
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
	// BACKLOG-TOOL-58: state the pin this call WILL send, in both the dry run
	// and the real open — it decides which subject the store resolves the gate
	// against, and nothing else in the plan shows it.
	if pin != "" {
		fmt.Printf("  pin: the epic's packet aggregate %s (a members-only gate — the epic's cold review is its prerequisite)\n", pin)
	} else if epic && status != from {
		fmt.Printf("  pin: none — this gate does not name %s, so the store resolves the named member's own packet aggregate, not the epic's %s, and the epic's cold-review trace does not pass there\n", scope, facts.Aggregate)
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
	var body []string
	if predecessor != "" {
		// A closed or withdrawn gate cannot be superseded (its answer entered
		// application), so the link is prose: the successor names it.
		body = append(body, fmt.Sprintf("Successor of %s, which is closed or withdrawn: the scope re-enters at the current packet aggregate %s (after a demotion to PROPOSED and re-planning, or because the earlier id stays reserved).", predecessor, facts.Aggregate))
	}
	if driftLine != "" {
		// SR-CLI-028-002 C3: the accepted drift is recorded on the gate the
		// human answers, with its source, the tip and the changed paths.
		body = append(body, driftLine)
	}
	if len(body) > 0 {
		fields["body_md"] = strings.Join(body, "\n\n")
	}
	if _, err := authorCreate(env, "gate", gateID, fields); err != nil {
		return err
	}
	if err := workingSetSelect(env, wsSelectOpts{scope: scope, kind: selectionKind(facts.Scope.Kind), phase: "entry"}, time.Now()); err != nil {
		return err
	}
	printInfo("answer it in Mission Control or with `factory answer %s --options approve --text \"USER:<date>: …\"`, then apply members first with `author advance … --gate %s`", gateID, gateID)
	return nil
}
