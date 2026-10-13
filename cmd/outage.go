package cmd

// EPIC-CLI-OUTAGE — the outage command group. REQ-CROSS-538 lists the kept
// writes under `status`; REQ-CROSS-539 records them under `replay`.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var outageCmd = &cobra.Command{
	Use:   "outage",
	Short: "Process writes kept on this machine while the ModernPath server was unavailable",
}

var outageStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "List the process writes kept on this machine",
	Long: `List the process writes kept on this machine while the ModernPath server
was unavailable, in the order they are recorded when it is back: each with
its verb, record, when it was kept, the person who kept it and what it waits
on. Entries a replay could not record are listed with the server's reason.
Reads only this machine.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		root := workspaceRoot()
		if root == "" {
			return fmt.Errorf("no ModernPath workspace here — run it in the workspace that kept the writes")
		}
		outageStatus(root, os.Stdout, time.Now())
		return nil
	},
}

var outageReplayCmd = &cobra.Command{
	Use:   "replay",
	Short: "Record the kept process writes in the store",
	Long: `Record the process writes kept on this machine while the ModernPath server
was unavailable, in the order they were kept, each with its fixed values and
the pins it was made on. The next write command does the same before its own
request; hooks and read commands never do.

Only the person who kept these writes can record them: signed in as anyone
else, nothing is sent. A write that already landed counts as recorded. A
write the server refuses, or whose record or gate moved meanwhile, is moved
to the conflicted or refused list with the server's reason and is never
retried; writes that wait on it are moved aside with it. A write whose
request fails without an answer is kept for another try and the rest go on;
after three failed tries it is moved to the refused list. When the server is
unavailable, limiting requests or answers with a server error (5xx), the
replay stops and the rest stay kept.`,
	Args: cobra.NoArgs,
	RunE: outageReplay,
}

// outageStatus lists the kept writes, then those a replay moved aside.
func outageStatus(root string, out io.Writer, now time.Time) {
	if known, down := knownUnavailable(root, now); down {
		fmt.Fprintf(out, "The ModernPath server is unavailable since %s.\n\n", known.since.Local().Format("15:04"))
	}
	dir := journalDir(root)
	kept := readJournal(dir)
	switch len(kept) {
	case 0:
		fmt.Fprintln(out, "No process writes are kept on this machine.")
	case 1:
		fmt.Fprintln(out, "1 process write is kept on this machine; it is recorded when the server is back:")
	default:
		fmt.Fprintf(out, "%d process writes are kept on this machine; they are recorded in this order when the server is back:\n", len(kept))
	}
	for _, f := range kept {
		fmt.Fprintln(out, keptLine(f))
	}
	for _, sub := range keptAsideDirs {
		aside := readJournal(filepath.Join(dir, sub))
		if len(aside) == 0 {
			continue
		}
		fmt.Fprintf(out, "\n%s — not recorded; redo or delete each (%s):\n", strings.ToUpper(sub[:1])+sub[1:], filepath.Join(config.ConfigDir, outageJournalDir, sub))
		for _, f := range aside {
			line := keptLine(f)
			if f.entry.Reason != "" {
				line += "\n      " + f.entry.Reason
			}
			fmt.Fprintln(out, line)
		}
	}
}

// keptLine is one entry: name, verb and record, keep time, the person who
// kept it, and what it waits on.
func keptLine(f journalFile) string {
	e := f.entry
	line := fmt.Sprintf("  %s  %s %s", strings.TrimSuffix(f.name, ".json"), e.Verb, e.Record)
	if at, err := time.Parse(time.RFC3339, e.KeptAt); err == nil {
		line += " — kept " + at.Local().Format("2006-01-02 15:04")
	}
	if who := firstNonEmpty(e.Keeper.Email, e.Keeper.Subject); who != "" {
		line += " by " + who
	}
	if e.WaitsOn != "" {
		line += " — waits on " + e.WaitsOn
	}
	return line
}

func init() {
	outageCmd.AddCommand(outageStatusCmd, outageReplayCmd)
	rootCmd.AddCommand(outageCmd)
	// REQ-CROSS-539: a write command replays the kept writes after its env loads.
	rootCmd.PersistentPreRunE = armReplay
}
