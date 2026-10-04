package cmd

import (
	"github.com/spf13/cobra"
)

// REQ-CROSS-458 (EPIC-RDD-LANE): the small-change lane's CLI verbs. A lane
// authorization is a standing human decision per System, answered once by a
// workspace admin in the web app or with `process lane approve`; a small change of a class it covers enters
// by applying it after its own narrow independent review, its eligibility is
// the server's verdict over the diff it delivered, and small changes complete
// together in one lane-batch gate with per-item rejection (PROCESS.md,
// Small-change lane). Every stand-in for the per-change human answer is
// checked by the server; these verbs read the facts, refuse before any write
// when one is unmet, and otherwise print the server's refusal verbatim.
//
// The single-SR aggregate a narrow review pins is served only for a piece the
// caller holds (the delivery-context read), so the verbs that need it hold the
// SR as a single_sr work selection, taking it when it is not yet held. A
// selection is not a process record.

// laneNarrowSource is the source that marks a cold-review trace as the lane's
// narrow review (the server matches its ref, case-insensitively).
var laneNarrowSource = map[string]any{"kind": "lane", "ref": "LANE:narrow"}

var processLaneCmd = &cobra.Command{
	Use:   "lane",
	Short: "The small-change lane: authorize, review, enter, check and complete small changes",
	Long: `The small-change lane scales review and gates to a small change (PROCESS.md,
Small-change lane). With no subcommand it prints this System's lane
authorizations: the current one and any waiting for an answer (a read).

The sequence for one small change:

  author apply --file sr.yaml                   # the SR with lane_class and a source
  working-set pull <SR> --for-review            # the reviewer reads REVIEW.md
  process lane review <SR> --file review.json   # the narrow review, one pass
  process lane enter <SR>                       # PROPOSED->TODO by the authorization
  factory evidence --file runs.json             # RED, then the passing run
  process advance --all --piece <SR> --log <run>
  process lane check <SR> --commit <merged sha> # after delivery to the default branch
  process lane complete --log <run>             # one lane-batch gate for many
  process lane complete --apply                 # after the answer

A lane authorization is prepared with process lane authorize and answered
once by a workspace admin, in the web app or with process lane approve <gate>.
factory answer cannot answer it.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processLaneShow(env)
	},
}

func init() {
	f := processLaneAuthorizeCmd.Flags()
	f.StringSliceVar(&laneAuthClasses, "classes", nil, "the classes covered: defect_with_failing_test, wording, presentation, dependency_patch (comma-separated or repeated)")
	f.StringSliceVar(&laneAuthAppliers, "appliers", nil, "the user ids who may apply it (comma-separated or repeated)")
	f.StringVar(&laneAuthExpires, "expires", "", "the expiry: a date, a date and time, or <n>d from now; at most 30 days")
	f.IntVar(&laneAuthCap, "cap", 0, "the daily cap of lane applications (1 to 10)")
	f.StringArrayVar(&laneAuthExclude, "exclude", nil, "a further excluded path glob (repeatable)")
	f.StringVar(&laneAuthFile, "file", "", "read the terms from a JSON file (see the example above)")
	f.StringVar(&laneAuthID, "id", "", "the gate id (default: the first free LANE-AUTH id)")
	f.StringVar(&laneAuthTitle, "title", "", "the gate title")

	processLaneApproveCmd.Flags().StringVar(&laneApproveText, "text", "", "your decision, recorded as written (default \"approve\")")

	processLaneReviewCmd.Flags().StringVar(&laneReviewFile, "file", "", "the reviewer's report file (JSON)")

	processLaneCheckCmd.Flags().StringVar(&laneCheckCommit, "commit", "", "the commit that delivered the change to the default branch (a merge or squash commit, or a rebase delivery's tip)")
	processLaneCheckCmd.Flags().StringVar(&laneCheckBase, "base", "", "for a rebase delivery: the commit the change started from")

	c := processLaneCompleteCmd.Flags()
	c.BoolVar(&laneCompleteApply, "apply", false, "mark the approved changes DONE after the approval")
	c.StringVar(&laneCompleteLog, "log", "", "the run you report for the delivered changes, such as a CI link (required to open a batch)")
	c.StringVar(&laneCompleteKind, "kind", "ci", "the evidence run kind")
	c.StringVar(&laneCompleteGateID, "gate", "", "with --apply: the batch to apply (default: every approved one)")
	c.BoolVar(&laneCompleteDryRun, "dry-run", false, "print the batch and write nothing")

	processLaneCmd.AddCommand(processLaneAuthorizeCmd, processLaneApproveCmd, processLaneReviewCmd, processLaneEnterCmd, processLaneCheckCmd, processLaneCompleteCmd)
	processCmd.AddCommand(processLaneCmd)
}
