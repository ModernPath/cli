package rdd

// SR-CMP-9022 (EPIC-CMP-902): the coverage receipt rides the batch. One
// report_coverage op per sync carries the `modernpath coverage` summary —
// the same engine the command runs (internal/coverage), never a second
// measurement. Deterministic by design: no timestamps in the payload (the
// server stamps receipt time), and the content hash covers the canonical
// summary ONLY, so the Go and node builders must agree on the numbers while
// each names itself in generated_from.

import (
	"math"

	"github.com/modernpath/cli/internal/covreport"
)

// CoverageExternalID is the receipt's stable identity: one per workspace,
// last-wins on the server (a projection of current state, not history).
const CoverageExternalID = "WORKSPACE-COVERAGE"

// BuildCoverageOp measures the workspace at root and wraps the summary in a
// report_coverage op. ok is false when the workspace carries no ledgers —
// a workspace without a requirement corpus has no coverage to report.
func BuildCoverageOp(root, generatedFrom string) (Op, bool) {
	rep, err := covreport.BuildReport(root)
	if err != nil {
		return Op{}, false
	}
	summary := coverageSummaryOf(rep)
	payload := map[string]any{
		"external_id":    CoverageExternalID,
		"generated_from": generatedFrom,
		"summary":        summary,
		"content_hash":   ContentHash(summary),
	}
	return Op{Type: "report_coverage", Payload: payload}, true
}

// coverageSummaryOf maps the full coverage report onto the wire summary
// (contracts/sync/v1.schema.json $defs.report_coverage_payload): aggregate
// counts only — the triage lists (nowhere paths, uncovered dirs) stay local,
// where the ids they name can be acted on.
func coverageSummaryOf(rep *covreport.Report) map[string]any {
	byLedger := map[string]any{}
	for _, l := range rep.Ledgers {
		byLedger[l.Name] = l.Rows
	}
	perApp := make([]any, 0, len(rep.PerApp))
	for _, a := range rep.PerApp {
		perApp = append(perApp, map[string]any{
			"app": a.App, "files": a.Files, "covered": a.Covered,
		})
	}
	return map[string]any{
		"total_rows":     rep.Rows.Total,
		"rows_by_ledger": byLedger,
		"citations": map[string]any{
			"at_path":       rep.Citations.ResolvedAtPath,
			"basename_only": len(rep.Citations.ResolvedByBasename),
			"nowhere":       len(rep.Citations.Nowhere),
		},
		"files": map[string]any{
			"inventory": rep.Inventory.Files,
			"covered":   rep.Inventory.Covered,
			"pct":       coveragePct(rep.Inventory.Covered, rep.Inventory.Files),
		},
		"per_app": perApp,
		"test_axis": map[string]any{
			"rows_with_tests":  rep.TestAxis.Rows.WithTests,
			"rows_dash":        rep.TestAxis.Rows.ExplicitNone,
			"rows_neither":     rep.TestAxis.Rows.Neither,
			"test_files":       rep.TestAxis.Inventory.Files,
			"test_files_cited": rep.TestAxis.Inventory.Cited,
		},
	}
}

// coveragePct rounds 100*a/b to one decimal, identically to the node mirror
// (Math.round semantics agree with math.Round for positive values). 0/0 is 0,
// not NaN — an empty inventory is a real workspace state and NaN never
// canonicalizes.
func coveragePct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return math.Round(1000*float64(a)/float64(b)) / 10
}
