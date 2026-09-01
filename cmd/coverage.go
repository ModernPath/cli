package cmd

// REQ-CROSS-179: requirement→code coverage is a measured fact.
//
// `modernpath coverage` parses every tasks/*-REQUIREMENTS.md ledger, extracts
// the CODE:/TEST: citations from the detail blocks, resolves them against the
// workspace tree, and reports citation integrity plus what fraction of the
// real source inventory is cited by at least one requirement.
//
// The engine lives in internal/covreport (SR-CMP-9022 moved it there so the
// sync op-builder shares the same measurement instead of duplicating it);
// this file owns only the command surface: flags, rendering, exit code.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/covreport"
	"github.com/spf13/cobra"
)

var (
	coverageRoot   string
	coverageStrict bool
	coverageJSON   bool
)

var coverageCmd = &cobra.Command{
	Use:   "coverage",
	Short: "Measure requirement→code coverage: cited files vs the source inventory",
	Long: `Measure how much of the workspace's source tree the requirement ledgers
actually cite, and whether the citations still resolve.

Reads every tasks/*-REQUIREMENTS.md ledger, extracts CODE:/TEST: citations
from the requirement detail blocks, and reports:

  rows        per-ledger row counts and the status split
  integrity   citations resolved at their written path, resolved only by
              basename (non-canonical — normalize these), or resolving
              NOWHERE (listed with their owning REQ ids)
  coverage    per-app share of source files cited by ≥1 requirement, and
              the largest uncovered directories, ranked

"Apps" are the workspace's top-level directories that hold source or cited
files — derived from the tree, never configured. With --strict the exit code
is non-zero if any citation resolves nowhere; --json emits the same numbers
machine-readable.`,
	RunE: runCoverage,
}

func init() {
	coverageCmd.Flags().StringVar(&coverageRoot, "root", "",
		"workspace root to measure (default: current directory)")
	coverageCmd.Flags().BoolVar(&coverageStrict, "strict", false,
		"exit non-zero if any citation resolves nowhere")
	coverageCmd.Flags().BoolVar(&coverageJSON, "json", false,
		"emit the report as JSON (owns stdout)")
	rootCmd.AddCommand(coverageCmd)
}

func renderCoverageReport(rep *covreport.Report) {
	fmt.Printf("Requirement rows: %d across %d ledger(s)\n", rep.Rows.Total, len(rep.Ledgers))
	for _, l := range rep.Ledgers {
		fmt.Printf("    %s: %d\n", l.Name, l.Rows)
	}
	statuses := make([]string, 0, len(rep.Rows.ByStatus))
	for s := range rep.Rows.ByStatus {
		statuses = append(statuses, s)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if rep.Rows.ByStatus[statuses[i]] != rep.Rows.ByStatus[statuses[j]] {
			return rep.Rows.ByStatus[statuses[i]] > rep.Rows.ByStatus[statuses[j]]
		}
		return statuses[i] < statuses[j]
	})
	fmt.Printf("  by status:")
	for _, s := range statuses {
		fmt.Printf(" %s %d ·", s, rep.Rows.ByStatus[s])
	}
	fmt.Println()
	fmt.Printf("  rows with ≥1 code/test link: %d/%d (%s)\n",
		rep.Rows.Linked, rep.Rows.Total, covreport.Pct(rep.Rows.Linked, rep.Rows.Total))
	if n := len(rep.Rows.Unlinked); n > 0 {
		shown := rep.Rows.Unlinked
		suffix := ""
		if n > 15 {
			shown, suffix = shown[:15], " …"
		}
		fmt.Printf("  rows with NO link: %d → %s%s\n", n, strings.Join(shown, " "), suffix)
	}

	fmt.Printf("\nCitations: %d distinct\n", rep.Citations.Distinct)
	fmt.Printf("  resolved at their written path: %d (→ %d real files after directory expansion and basename fallback)\n",
		rep.Citations.ResolvedAtPath, rep.Citations.ResolvedFiles)
	fmt.Printf("  resolved by basename only (normalize these): %d\n", len(rep.Citations.ResolvedByBasename))
	for _, c := range rep.Citations.ResolvedByBasename {
		fmt.Printf("    %s → %s  [%s]\n", c.Path, c.Hits[0], strings.Join(c.Reqs, " "))
	}
	fmt.Printf("  resolve NOWHERE: %d\n", len(rep.Citations.Nowhere))
	for _, c := range rep.Citations.Nowhere {
		fmt.Printf("    %s  [%s]\n", c.Path, strings.Join(c.Reqs, " "))
	}

	fmt.Printf("\nCoverage: %d/%d source files (%s) across %d app(s)\n",
		rep.Inventory.Covered, rep.Inventory.Files,
		covreport.Pct(rep.Inventory.Covered, rep.Inventory.Files), len(rep.Inventory.Apps))

	// Say what was left out. The denominator used to include anything on disk:
	// a gitignored lab directory holding a vendored package put coverage at
	// 31.2% when the tracked source was 84.6%. Excluding those is right, but an
	// exclusion nobody can see is just a different way to be wrong.
	if n := covreport.GitIgnoredCount(rep.Root); n > 0 {
		fmt.Printf("    (%d source-extension files excluded: git ignores them)\n", n)
	}

	for _, a := range rep.PerApp {
		fmt.Printf("    %s: %d/%d (%s)\n", a.App, a.Covered, a.Files, covreport.Pct(a.Covered, a.Files))
	}
	if len(rep.UncoveredDirs) > 0 {
		fmt.Println("\nLargest uncovered directories (2nd/3rd path level):")
		for _, d := range rep.UncoveredDirs {
			fmt.Printf("    %s: %d/%d uncovered\n", d.Dir, d.Uncovered, d.Total)
		}
	}

	fmt.Printf("\nTEST AXIS\n")
	fmt.Printf("  rows with ≥1 test citation: %d/%d (%s) · explicitly no tests (—): %d · neither: %d\n",
		rep.TestAxis.Rows.WithTests, rep.Rows.Total,
		covreport.Pct(rep.TestAxis.Rows.WithTests, rep.Rows.Total),
		rep.TestAxis.Rows.ExplicitNone, rep.TestAxis.Rows.Neither)
	for _, l := range rep.TestAxis.PerLedger {
		fmt.Printf("    %s: %d with tests · %d — · %d neither\n",
			l.Name, l.WithTests, l.ExplicitNone, l.Neither)
	}
	fmt.Printf("  test files cited by ≥1 requirement: %d/%d (%s)\n",
		rep.TestAxis.Inventory.Cited, rep.TestAxis.Inventory.Files,
		covreport.Pct(rep.TestAxis.Inventory.Cited, rep.TestAxis.Inventory.Files))
	for _, a := range rep.TestAxis.Inventory.PerApp {
		fmt.Printf("    %s: %d/%d (%s)\n", a.App, a.Cited, a.Files, covreport.Pct(a.Cited, a.Files))
	}
	fmt.Printf("  test citations resolving NOWHERE (claimed verification that does not exist): %d\n",
		len(rep.TestAxis.Nowhere))
	for _, c := range rep.TestAxis.Nowhere {
		fmt.Printf("    %s  [%s]\n", c.Path, strings.Join(c.Reqs, " "))
	}
}

func runCoverage(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	root := coverageRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	rep, err := covreport.BuildReport(root)
	if err != nil {
		return err
	}

	if coverageJSON {
		// --json owns stdout (REQ-CROSS-121): the document and nothing else.
		out, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	} else {
		renderCoverageReport(rep)
	}

	if coverageStrict {
		return covreport.StrictErr(rep)
	}
	return nil
}
