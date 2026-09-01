package opschema

// SR-CMP-9022 (EPIC-CMP-902): the workspace coverage receipt rides the batch
// as its own op. `report_coverage` is a projection of measured workspace state
// (the `modernpath coverage` numbers), not an upsert: the server stores it
// last-wins under the system's metadata, so the payload carries the summary
// and its provenance — never a timestamp (the server stamps receipt time).

import (
	"bytes"
	"testing"
)

func coverageSummary() map[string]any {
	return map[string]any{
		"total_rows":     9,
		"rows_by_ledger": map[string]any{"DEMO-REQUIREMENTS.md": 9},
		"citations":      map[string]any{"at_path": 5, "basename_only": 2, "nowhere": 1},
		"files":          map[string]any{"inventory": 11, "covered": 8, "pct": 72.7},
		"per_app": []any{
			map[string]any{"app": "appx", "files": 9, "covered": 6},
			map[string]any{"app": "appy", "files": 2, "covered": 2},
		},
		"test_axis": map[string]any{
			"rows_with_tests":  3,
			"rows_dash":        1,
			"rows_neither":     5,
			"test_files":       3,
			"test_files_cited": 2,
		},
	}
}

func TestReportCoverageOpValidates(t *testing.T) {
	ops := []map[string]any{
		op("report_coverage", map[string]any{
			"external_id":    "WORKSPACE-COVERAGE",
			"generated_from": "modernpath@dev",
			"summary":        coverageSummary(),
			"content_hash":   "h",
		}),
	}
	if err := ValidateOps(ops); err != nil {
		t.Fatalf("valid report_coverage rejected: %v", err)
	}
}

func TestReportCoverageMissingSummaryFails(t *testing.T) {
	err := ValidateOps([]map[string]any{
		op("report_coverage", map[string]any{
			"external_id":    "WORKSPACE-COVERAGE",
			"generated_from": "modernpath@dev",
		}),
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("summary")) {
		t.Fatalf("a coverage receipt without its summary must fail naming the field, got %v", err)
	}
}

func TestReportCoverageMissingGeneratedFromFails(t *testing.T) {
	err := ValidateOps([]map[string]any{
		op("report_coverage", map[string]any{
			"external_id": "WORKSPACE-COVERAGE",
			"summary":     coverageSummary(),
		}),
	})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("generated_from")) {
		t.Fatalf("a coverage receipt without provenance must fail naming generated_from, got %v", err)
	}
}
