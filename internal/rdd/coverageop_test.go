package rdd

// SR-CMP-9022 (EPIC-CMP-902): the coverage receipt rides the batch. The CLI's
// `factory sync` appends ONE report_coverage op carrying the `modernpath
// coverage` summary — deterministic (no timestamps; the server stamps receipt
// time), hashed over the summary only so the Go and node builders must agree
// on the NUMBERS while each names itself in generated_from.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedCoverageOpWorkspace is a miniature workspace: one ledger with a citation
// that resolves at its written path, one that resolves nowhere, and a row that
// explicitly declares no tests. Backticks cannot live in a Go raw string, so
// the ledger uses ' as a stand-in and replaces it.
func seedCoverageOpWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ledger := strings.ReplaceAll(`# DEMO — demo context requirements

### REQ-DEMO-001 — resolves at its written path, test included
- **Status:** READY
- **Code:** 'CODE:appx/src/good.ts'
- **Tests:** 'TEST:appx/test/good.spec.ts'

### REQ-DEMO-002 — cites a file that exists nowhere
- **Status:** READY
- **Code:** 'CODE:appx/src/ghost.ts'

### REQ-DEMO-003 — explicitly declares it has no tests
- **Status:** PROPOSED
- **Tests:** —
`, "'", "`")
	for rel, content := range map[string]string{
		"tasks/DEMO-REQUIREMENTS.md": ledger,
		"appx/src/good.ts":           "export {}\n",
		"appx/src/other.ts":          "export {}\n",
		"appx/test/good.spec.ts":     "export {}\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestBuildCoverageOpCarriesTheMeasuredSummary(t *testing.T) {
	root := seedCoverageOpWorkspace(t)
	op, ok := BuildCoverageOp(root, "modernpath@test")
	if !ok {
		t.Fatal("a workspace with ledgers must produce a coverage receipt")
	}
	if op.Type != "report_coverage" {
		t.Fatalf("op type must be report_coverage, got %q", op.Type)
	}
	if op.Payload["external_id"] != "WORKSPACE-COVERAGE" {
		t.Fatalf("the receipt identity is stable, got %v", op.Payload["external_id"])
	}
	if op.Payload["generated_from"] != "modernpath@test" {
		t.Fatalf("generated_from must name the builder, got %v", op.Payload["generated_from"])
	}

	summary, _ := op.Payload["summary"].(map[string]any)
	if summary == nil {
		t.Fatal("payload must carry the summary object")
	}
	if summary["total_rows"] != 3 {
		t.Fatalf("total_rows must be 3, got %v", summary["total_rows"])
	}
	byLedger, _ := summary["rows_by_ledger"].(map[string]any)
	if byLedger["DEMO-REQUIREMENTS.md"] != 3 {
		t.Fatalf("rows_by_ledger must count the ledger's rows, got %v", summary["rows_by_ledger"])
	}
	citations, _ := summary["citations"].(map[string]any)
	if citations["at_path"] != 2 || citations["basename_only"] != 0 || citations["nowhere"] != 1 {
		t.Fatalf("citation integrity must be 2/0/1, got %v", citations)
	}
	files, _ := summary["files"].(map[string]any)
	if files["inventory"] != 3 || files["covered"] != 2 {
		t.Fatalf("file coverage must be 2/3, got %v", files)
	}
	if files["pct"] != 66.7 {
		t.Fatalf("pct must round 2/3 to one decimal (66.7), got %v", files["pct"])
	}
	perApp, _ := summary["per_app"].([]any)
	if len(perApp) != 1 {
		t.Fatalf("one app in the tree, got %v", summary["per_app"])
	}
	appx, _ := perApp[0].(map[string]any)
	if appx["app"] != "appx" || appx["files"] != 3 || appx["covered"] != 2 {
		t.Fatalf("per_app must carry appx 2/3, got %v", appx)
	}
	axis, _ := summary["test_axis"].(map[string]any)
	if axis["rows_with_tests"] != 1 || axis["rows_dash"] != 1 || axis["rows_neither"] != 1 {
		t.Fatalf("test-axis rows must split 1/1/1, got %v", axis)
	}
	if axis["test_files"] != 1 || axis["test_files_cited"] != 1 {
		t.Fatalf("test-file inventory must be 1/1, got %v", axis)
	}
}

// The hash certifies the measurement, not the instrument: it covers the
// canonical summary only, so two builders measuring the same tree agree even
// though each writes its own generated_from.
func TestBuildCoverageOpHashCoversSummaryOnly(t *testing.T) {
	root := seedCoverageOpWorkspace(t)
	a, _ := BuildCoverageOp(root, "modernpath@one")
	b, _ := BuildCoverageOp(root, "mission-control@two")
	hashA, _ := a.Payload["content_hash"].(string)
	hashB, _ := b.Payload["content_hash"].(string)
	if hashA == "" || hashA != hashB {
		t.Fatalf("two builders over one tree must hash alike: %q vs %q", hashA, hashB)
	}
	summary, _ := a.Payload["summary"].(map[string]any)
	if hashA != ContentHash(summary) {
		t.Fatal("content_hash must be ContentHash over the summary object only")
	}
}

func TestBuildCoverageOpAbsentWithoutLedgers(t *testing.T) {
	if _, ok := BuildCoverageOp(t.TempDir(), "modernpath@test"); ok {
		t.Fatal("a workspace without ledgers must not fabricate a coverage receipt")
	}
}
