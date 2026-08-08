package cmd

import (
	"fmt"
	"os"

	"github.com/modernpath/cli/internal/gate"
	"github.com/spf13/cobra"
)

var checkWriteBaseline bool

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check the process rules that must hold (status hygiene, approval before DONE)",
	Long: `Check the process rules that cannot be left to good intentions.

  status-hygiene         a ledger's dashboard rows must agree with its Totals line
  approval-before-done   an epic the work-list calls DONE must have a recorded
                         approval carrying a USER: source

Both were written rules long before they were checks. Written rules are context,
not configuration: they are usually followed, and the times they are not look
exactly like the times they are. This exits non-zero so a hook or CI can act.

Adopting the gate on an existing repository: run with --baseline once to accept
the violations already there. New ones still block; the accepted list lives in
.claude/gate-baseline and shrinks as you fix them.`,
	RunE: runCheck,
}

func init() {
	checkCmd.Flags().BoolVar(&checkWriteBaseline, "baseline", false,
		"record the current violations as accepted, so only new ones block")
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	all, err := gate.CheckAll(root)
	if err != nil {
		return err
	}

	if checkWriteBaseline {
		if err := gate.WriteBaseline(root, all); err != nil {
			return err
		}
		fmt.Printf("✓ baselined %d existing violation(s) → .claude/gate-baseline\n", len(all))
		fmt.Println("  New violations will now block. Remove a line to start enforcing it.")
		return nil
	}

	baseline, err := gate.LoadBaseline(root)
	if err != nil {
		return err
	}
	fresh := gate.Unbaselined(all, baseline)

	if len(fresh) == 0 {
		if accepted := len(all); accepted > 0 {
			fmt.Printf("✓ no new violations (%d baselined and still outstanding)\n", accepted)
		} else {
			fmt.Println("✓ process checks pass")
		}
		return nil
	}

	fmt.Printf("✗ %d process violation(s):\n", len(fresh))
	for _, v := range fresh {
		fmt.Printf("    %s\n      %s\n", v.File, v.Detail)
	}
	fmt.Println("\nStatus lives in three places — the dashboard row, the detail block, and the")
	fmt.Println("Totals line. An epic is not DONE until its approval is recorded with a source.")
	return fmt.Errorf("%d process violation(s)", len(fresh))
}
