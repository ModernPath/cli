package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// --- REQ-CROSS-215: the your-move projection ---

// yourMoveRun regenerates the pending human-decision projection. It is a
// projection, never an authority: previous content is replaced wholesale, and
// a failed fetch leaves the previous projection untouched (the write happens
// only after a 200).
func yourMoveRun(env *factoryEnv, now time.Time) error {
	gates, err := fetchList(env, fmt.Sprintf("/api/v1/sync/gates?system_id=%d", env.SystemID), "gates")
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# Your move — open human decisions\n\n")
	fmt.Fprintf(&b, "- **Snapshot at:** %s\n", now.Format(time.RFC3339))
	fmt.Fprintf(&b, "- **Source store/revision:** %s\n\n", storeRevisionLine(env))
	if len(gates) == 0 {
		b.WriteString("No open human gates — the queue is clear at this snapshot.\n")
	} else {
		fmt.Fprintf(&b, "%d open gate(s). Only `OPEN` human gates appear here; this file is a\nregenerated projection, never an authority.\n", len(gates))
		for _, g := range gates {
			m, _ := g.(map[string]any)
			fmt.Fprintf(&b, "\n## %s — %s\n\n- **Kind:** %s\n", str(m, "external_id"), str(m, "title"), str(m, "kind"))
			if rec := str(m, "recommendation"); rec != "" {
				fmt.Fprintf(&b, "- **Recommends:** %s\n", rec)
			}
			if options, ok := m["options"].([]any); ok && len(options) > 0 {
				b.WriteString("- **Options:**")
				for _, o := range options {
					om, _ := o.(map[string]any)
					fmt.Fprintf(&b, " `%s`", str(om, "key"))
				}
				b.WriteString("\n")
			}
		}
	}
	path := filepath.Join(env.Root, yourMoveDir, "GATES.md")
	if err := atomicWrite(path, []byte(b.String())); err != nil {
		return err
	}
	printSuccess("your move: %d open gate(s) → %s", len(gates), filepath.Join(yourMoveDir, "GATES.md"))
	return nil
}

var (
	yourMoveMore     bool
	yourMoveQueue    bool
	yourMoveDomain   string
	yourMoveRelease  string
	yourMoveHook     string
	yourMoveDeadline int
)

var yourMoveCmd = &cobra.Command{
	Use:   "your-move",
	Short: "Regenerate " + yourMoveDir + "/GATES.md and print your personal brief",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Hook mode owns the whole exchange — it reads the agent's stdin payload,
		// prints its envelope (or {}), and never fails the session (§277).
		if yourMoveHook != "" {
			runBriefHook(yourMoveHook, os.Stdin, os.Stdout, time.Duration(yourMoveDeadline)*time.Second, factoryEnvLoad)
			return nil
		}
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		return yourMoveWithBrief(env, briefOpts{
			more:    yourMoveMore,
			queue:   yourMoveQueue,
			domain:  yourMoveDomain,
			release: yourMoveRelease,
			now:     time.Now().UTC(),
		}, os.Stdout)
	},
}
