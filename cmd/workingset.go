package cmd

// Working-set command wiring. Implementation is grouped by responsibility in
// workingset_*.go, following the factory and process command layout.

import (
	"fmt"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/authoring"
	"github.com/spf13/cobra"
)

// REQ-CROSS-313 (SR-CLI-0084): a pulled scope directory stamps its authoring or
// review context in this file; push (SR-CLI-0085) and finding creation
// (SR-CLI-0086) send it. editableFieldMarker is the authoring render's editable
// fence — a --for-review render carries none.
const (
	contextFile         = ".context"
	editableFieldMarker = authoring.EditableFieldMarker
)

const yourMoveDir = ".modernpath/your-move"

const workingSetDir = ".modernpath/working-set"

var workingSetRefresh bool

// REQ-CROSS-308 (EPIC-CLI-007): a single flag shared by `pull` and `check`,
// read where each calls wsIndex. Off by default — a candidate is opt-in, never
// pulled into the working set by surprise.
var workingSetIncludeCandidates bool

var workingSetCmd = &cobra.Command{
	Use:   "working-set",
	Short: "Pull server state into " + workingSetDir + " as uncommitted shape files",
}

var (
	wsPullScope     bool
	wsPullForReview bool
	wsPullSince     string
	// REQ-CROSS-345: names which of the caller's own current pieces a read
	// resolves, carried as ?scope=. Empty reads the sole current (or asks the
	// caller to name one when they hold several).
	wsPiece string
)

var workingSetPullCmd = &cobra.Command{
	Use:   "pull [<external-id>...]",
	Short: "Materialize named items, or the current selection's scope with --scope",
	Long: `Materialize named items, or the current selection's scope with --scope.

Read the selection overview with any of these aliases:
  modernpath working-set pull selection
  modernpath working-set pull WORK-SELECTION
  modernpath working-set pull WORK-SELECTION.md

The overview shows all parked work, holders and blockers even when several
current pieces are held. It reports that ambiguity without choosing a piece.
Use --piece <id> to resolve a named current selection. Reads do not select,
resume or claim work. Scope-dependent reads and writes still require an
unambiguous current piece.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		if wsPullSince != "" {
			// REQ-CROSS-464: a delta bundle is a later round of an epic's
			// review; the narrow review is one pass with no rounds.
			if !wsPullScope && wsPullForReview {
				return fmt.Errorf("--since is refused with a by-id pull: the small-change lane's narrow review is one pass with no later rounds — pull the scope with --scope --for-review --since")
			}
			if !wsPullScope || !wsPullForReview {
				return fmt.Errorf("--since renders a later review round: use it with --scope --for-review")
			}
		}
		if wsPullScope {
			return workingSetPullScopeSince(env, wsPullForReview, wsPullSince, time.Now().UTC())
		}
		if wsPullForReview {
			// REQ-CROSS-458: a by-id review pull renders one system
			// requirement for the small-change lane's narrow review.
			if len(args) == 0 {
				return fmt.Errorf("--for-review needs --scope (an epic's review) or a system requirement's id (a small change's narrow review)")
			}
			return workingSetPullForReview(env, args, time.Now().UTC())
		}
		if len(args) == 0 {
			return fmt.Errorf("name at least one external id, or use --scope to pull the current selection")
		}
		return workingSetPull(env, args, time.Now().UTC())
	},
}

var workingSetCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Check snapshot source and local body integrity; --refresh updates safely",
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return workingSetCheck(env, workingSetRefresh, time.Now().UTC())
	},
}

var wsPushDryRun bool

// REQ-CROSS-384 (EPIC-CLI-018): --restamp re-puts every filled canonical
// section so the server re-stamps it at the current scope context.
var wsPushRestamp bool

var workingSetPushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push the current scope directory: item files as atomic patches, packet sections whole",
	Long: `Push the current scope directory. Item files (the epic, each requirement)
go as atomic patches; packet section files go whole, each stamped server-side
with the scope context the packet had when it was pushed; a file whose
content is unchanged is skipped.

A push that patches a member record moves the packet's scope context. The
sections pushed in the same call are stamped after the patch; every other
filled section the server then reports as stale is re-put unchanged so it
carries the current stamp, and the push says "re-stamped N section(s)"
instead of "nothing to do". --restamp re-puts every filled canonical section
regardless. --dry-run prints the op plan (and what would be re-stamped at
the current context) without applying anything.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return workingSetPush(env, wsPushDryRun)
	},
}

var wsSelectFlags wsSelectOpts

var wsSelectMembers string

var workingSetSelectCmd = &cobra.Command{
	Use:   "select [<scope-external-id>]",
	Short: "Record, advance, suspend, or resume the store-held work selection",
	Long: `One holder per piece of work, several pieces per person. Worked example:

  working-set select EPIC-X --kind epic --members REQ-1,REQ-2 --phase plan
      take EPIC-X; with no --replaces this ADDS a piece to what you hold
  working-set select REQ-Y --kind single_sr --phase plan --lane defect
      take a customer-blocking defect: the store says so on every read
  working-set select EPIC-X --phase cold_review
      advance a piece you hold
  working-set select EPIC-Y --kind epic --replaces EPIC-X --outcome done
      take EPIC-Y and put EPIC-X down in the same move
  working-set select EPIC-X --suspend --reason "waiting on GATE-1" --waiting-on GATE-1
      park it; a suspended piece is claimable by anyone. Only a piece you
      currently hold can be suspended: to change a parked piece's reason,
      --resume it first, then suspend again; the earlier reason stays on
      the closed row
  working-set select EPIC-X --resume
      resume a parked piece
  working-set select EPIC-X --put-down --outcome returned=plan
      close it, stating how it ended

On select, suspend and resume, an omitted --waiting-on preserves the blocker;
--waiting-on "" explicitly clears it.

Reads and writes resolve against the authenticated person's pieces; with
several held, name one with --piece. Re-read the selection immediately before
mutating it. A pull with --for-review renders the scope read-only under a
review context — pushes from that directory are refused by design.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		opts := wsSelectFlags
		if len(args) == 1 {
			opts.scope = args[0]
		}
		if wsSelectMembers != "" {
			opts.members = strings.Split(wsSelectMembers, ",")
		}
		opts.waitingOnSet = cmd.Flags().Changed("waiting-on")
		return workingSetSelect(env, opts, time.Now().UTC())
	},
}

func init() {
	f := workingSetSelectCmd.Flags()
	f.StringVar(&wsSelectFlags.kind, "kind", "epic", "scope kind: epic|single_sr")
	f.StringVar(&wsSelectFlags.phase, "phase", "", "current phase")
	f.StringVar(&wsSelectMembers, "members", "", "comma-separated member external ids")
	f.StringVar(&wsSelectFlags.owner, "owner", "", "selection owner")
	f.StringVar(&wsSelectFlags.lane, "lane", "", "planned (default) or defect — a customer-blocking defect is on the clock; process next, your-move and the session brief say so")
	f.StringVar(&wsSelectFlags.waitingOn, "waiting-on", "", "gate id, blocker, or prerequisite")
	f.StringVar(&wsSelectFlags.fingerprint, "fingerprint", "", "frozen-at fingerprint (default: workspace HEAD)")
	f.StringVar(&wsSelectFlags.reconRevision, "recon-revision", "",
		"the repository revision the scope's reconnaissance was taken at; `working-set status` reads it back as the Reconnaissance revision")
	f.StringVar(&wsSelectFlags.outcome, "outcome", "", "displaced current selection's outcome: done|obsolete|returned=<phase>")
	f.BoolVar(&wsSelectFlags.suspend, "suspend", false, "suspend the current selection")
	f.StringVar(&wsSelectFlags.reason, "reason", "", "suspension reason (required with --suspend)")
	f.StringVar(&wsSelectFlags.target, "target", "", "suspension target")
	f.BoolVar(&wsSelectFlags.resume, "resume", false, "resume the named suspended scope")
	f.BoolVar(&wsSelectFlags.claim, "claim", false,
		"with --resume: take over a colleague's parked piece (recorded on their row); a parked piece nobody holds needs no --claim")
	f.StringVar(&wsSelectFlags.replaces, "replaces", "",
		"the one current piece this take displaces (pair with --outcome); unnamed, the take adds a holder")
	f.BoolVar(&wsSelectFlags.putDown, "put-down", false,
		"put down (close) the named current piece; carry --outcome done|obsolete|returned=<phase>")

	// REQ-CROSS-345: the read is caller-scoped; --piece names which of the
	// caller's own current pieces any read resolves (pull, push, check).
	workingSetCmd.PersistentFlags().StringVar(&wsPiece, "piece", "",
		"when you hold several current selections, --piece names which one pull --scope, push and check resolve (by-id pulls do not use a selected piece)")

	workingSetCheckCmd.Flags().BoolVar(&workingSetRefresh, "refresh", false, "refresh stale files; preserve edited or unverified originals and write fresh .pulled copies")
	for _, c := range []*cobra.Command{workingSetPullCmd, workingSetCheckCmd} {
		c.Flags().BoolVar(&workingSetIncludeCandidates, "include-candidates", false,
			"include DERIVED candidates in the requirements read (materialize a just-authored candidate)")
	}
	workingSetPullCmd.Flags().BoolVar(&wsPullScope, "scope", false,
		"pull the current work selection's scope as an editable directory (authoring render); packet/ is scaffolded with the required sections — reconnaissance, state inventory, red strategy, decisions, one enrichment per SR")
	workingSetPullCmd.Flags().BoolVar(&wsPullForReview, "for-review", false,
		"render the scope read-only for cold review, stamping a review context (with --scope); with a system requirement's id, render it for the small-change lane's narrow review")
	workingSetPullCmd.Flags().StringVar(&wsPullSince, "since", "",
		"with --scope --for-review: write REVIEW.md as the delta since this previous cold-review trace — what changed in full, the open findings, the previous verdict, the rest as id and fingerprint")
	workingSetPushCmd.Flags().BoolVar(&wsPushRestamp, "restamp", false,
		"re-put every filled canonical section unchanged so the server re-stamps it at the current scope context")
	workingSetPushCmd.Flags().BoolVar(&wsPushDryRun, "dry-run", false,
		"print the op plan without applying anything")
	workingSetCmd.AddCommand(workingSetPullCmd, workingSetCheckCmd, workingSetSelectCmd, workingSetPushCmd)

	ym := yourMoveCmd.Flags()
	ym.BoolVar(&yourMoveMore, "more", false, "show the next five ranked items")
	ym.BoolVar(&yourMoveQueue, "queue", false, "show everything in scope")
	ym.StringVar(&yourMoveDomain, "domain", "", "show only items touching this domain")
	ym.StringVar(&yourMoveRelease, "release", "", "release scope: active|base|all (default active)")
	ym.StringVar(&yourMoveHook, "hook", "", "hook mode: read the agent payload on stdin and emit the brief once per session (value = hook event name)")
	ym.IntVar(&yourMoveDeadline, "deadline", 8, "hook mode: seconds to wait before returning {} rather than holding the turn")

	rootCmd.AddCommand(yourMoveCmd, workingSetCmd)
}

const reviewSnapshotDir = ".modernpath/working-set-reviews"
