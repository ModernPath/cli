package covreport

// REQ-CROSS-179: requirement→code coverage is a measured fact.
//
// The fixture is a miniature of a real audit workspace: one ledger whose
// detail blocks carry (a) a citation that resolves at its written path, (b) a
// citation whose path moved so only the basename still resolves, (c) a
// citation that resolves nowhere, (d) a row with no link at all, and (e) a
// directory citation that expands to the source files under it. The numbers
// asserted here are the same numbers the field anchor run must reproduce at
// scale (307 rows; exactly 1 nowhere-citation).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedCoverageWorkspace writes the fixture workspace. Backticks cannot live in
// a Go raw string, so the ledger uses ' as a stand-in and replaces it.
func seedCoverageWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	ledger := strings.ReplaceAll(`# DEMO — demo context requirements

| ID | Title | Stage | Status | Source | Tests | Code |
|---|---|---|---|---|---|---|
| REQ-DEMO-001 | Resolves at path | MVP | READY | — | — | 'appx/src/good.ts' |
| REQ-DEMO-002 | Moved file | MVP | DONE | — | — | 'appx/wrong/dir/moved.ts' |

### REQ-DEMO-001 — resolves at its written path
- **Status:** READY · **Stage:** MVP
- **Code:** 'CODE:appx/src/good.ts:helper' · 'appx/src/util.ts:12'

### REQ-DEMO-002 — the cited path moved; only the basename still resolves
- **Status:** DONE
- **Code:** 'CODE:appx/wrong/dir/moved.ts:42'

### REQ-DEMO-003 — cites a test that exists nowhere
- **Status:** IN_PROGRESS
- **Tests:** 'TEST:appx/test/ghost.spec.ts'

### REQ-DEMO-004 — carries no code or test link at all
- **Status:** PROPOSED

### REQ-DEMO-005 — cites a directory, expanded to its source files
- **Status:** READY
- **Code:** 'CODE:appy/src'

### REQ-DEMO-006 — cites its test bare on the Tests line, beside run-log noise
- **Status:** READY
- **Tests:** 'appx/test/real.spec.ts' · 'RUN:2026-08-15' (green, 3 cases)

### REQ-DEMO-007 — explicitly declares it has no tests
- **Status:** DONE
- **Tests:** —

### REQ-DEMO-008 — cites two tests in one span, each with a line range
- **Status:** READY
- **Tests:** 'two.spec.ts:10-20 · other.spec.ts:30-40' — seen red, then green

### REQ-DEMO-009 — cites a Next.js route-group path, parentheses and all
- **Status:** READY
- **Source:** observed live (see CODE:appx/src/good.ts)
- **Code:** 'CODE:appx/src/app/(grp)/page.tsx' · 'CODE:appx/src/good.ts:16-35,382-387'
`, "'", "`")
	files := map[string]string{
		"tasks/DEMO-REQUIREMENTS.md":   ledger,
		"appx/src/good.ts":             "export {}\n",
		"appx/src/util.ts":             "export {}\n",
		"appx/src/uncov1.ts":           "export {}\n",
		"appx/src/uncov2.ts":           "export {}\n",
		"appx/src/lib/moved.ts":        "export {}\n",
		"appy/src/a.ts":                "export {}\n",
		"appy/src/b.ts":                "export {}\n",
		"appx/test/real.spec.ts":       "export {}\n",
		"appx/test/lonely.spec.ts":     "export {}\n",
		"appx/src/lib/two.spec.ts":     "export {}\n",
		"appx/src/app/(grp)/page.tsx":  "export {}\n",
		"appx/node_modules/d/index.ts": "// excluded from inventory\n",
		"docs/notes.md":                "not a source file, docs is not an app\n",
	}
	for rel, content := range files {
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

func TestCoverageCountsRowsAndStatusSplit(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Rows.Total != 9 {
		t.Fatalf("9 detail headings must count as 9 rows, got %d", rep.Rows.Total)
	}
	if len(rep.Ledgers) != 1 || rep.Ledgers[0].Name != "DEMO-REQUIREMENTS.md" || rep.Ledgers[0].Rows != 9 {
		t.Fatalf("per-ledger counts wrong: %+v", rep.Ledgers)
	}
	want := map[string]int{"READY": 5, "DONE": 2, "IN_PROGRESS": 1, "PROPOSED": 1}
	for status, n := range want {
		if rep.Rows.ByStatus[status] != n {
			t.Fatalf("status split: want %s=%d, got %+v", status, n, rep.Rows.ByStatus)
		}
	}
	if rep.Rows.Linked != 7 {
		t.Fatalf("7 rows carry a citation (Tests: — is not one), got linked=%d", rep.Rows.Linked)
	}
	if len(rep.Rows.Unlinked) != 2 || rep.Rows.Unlinked[0] != "REQ-DEMO-004" || rep.Rows.Unlinked[1] != "REQ-DEMO-007" {
		t.Fatalf("the linkless rows must be listed by id, got %v", rep.Rows.Unlinked)
	}
}

func TestCoverageTriagesCitations(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Citations.Distinct != 8 {
		t.Fatalf("8 distinct citations expected (RUN: noise filtered), got %d", rep.Citations.Distinct)
	}
	// good.ts (suffix :helper stripped), util.ts (bare backtick path, :12
	// stripped), the appy/src directory, real.spec.ts and the route-group
	// page resolve at their written path.
	if rep.Citations.ResolvedAtPath != 5 {
		t.Fatalf("5 citations resolve at their written path, got %d", rep.Citations.ResolvedAtPath)
	}
	if len(rep.Citations.ResolvedByBasename) != 2 {
		t.Fatalf("the moved file and the two-span citation resolve by basename, got %+v", rep.Citations.ResolvedByBasename)
	}
	moved := rep.Citations.ResolvedByBasename[0]
	if moved.Path != "appx/wrong/dir/moved.ts" ||
		len(moved.Reqs) != 1 || moved.Reqs[0] != "REQ-DEMO-002" ||
		len(moved.Hits) != 1 || moved.Hits[0] != "appx/src/lib/moved.ts" {
		t.Fatalf("basename triage must name the citation, its REQ and where it landed: %+v", moved)
	}
	// A span citing two tests with line ranges: the first segment's basename
	// — with its :10-20 range stripped — resolves, so the requirement's claim
	// is non-canonical, not missing.
	span := rep.Citations.ResolvedByBasename[1]
	if span.Path != "two.spec.ts:10-20 · other.spec.ts" ||
		len(span.Reqs) != 1 || span.Reqs[0] != "REQ-DEMO-008" ||
		len(span.Hits) != 1 || span.Hits[0] != "appx/src/lib/two.spec.ts" {
		t.Fatalf("a ranged two-test span must resolve by its first basename: %+v", span)
	}
	if len(rep.Citations.Nowhere) != 1 {
		t.Fatalf("exactly one citation resolves nowhere, got %+v", rep.Citations.Nowhere)
	}
	ghost := rep.Citations.Nowhere[0]
	if ghost.Path != "appx/test/ghost.spec.ts" || len(ghost.Reqs) != 1 || ghost.Reqs[0] != "REQ-DEMO-003" {
		t.Fatalf("a nowhere-citation must be listed with its owning REQ id: %+v", ghost)
	}
}

func TestCoverageComputesInventoryAndPerApp(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	// Apps derive from the tree: appx and appy carry source files; docs does
	// not; node_modules content never enters the inventory.
	if len(rep.Inventory.Apps) != 2 || rep.Inventory.Apps[0] != "appx" || rep.Inventory.Apps[1] != "appy" {
		t.Fatalf("apps must derive from the tree, got %v", rep.Inventory.Apps)
	}
	if rep.Inventory.Files != 11 {
		t.Fatalf("11 source files in inventory (node_modules excluded), got %d", rep.Inventory.Files)
	}
	// good.ts + util.ts + real.spec.ts + the route-group page at path,
	// moved.ts + two.spec.ts by basename, a.ts + b.ts by directory expansion.
	if rep.Inventory.Covered != 8 {
		t.Fatalf("8 files covered by ≥1 requirement, got %d", rep.Inventory.Covered)
	}
	byApp := map[string][2]int{}
	for _, a := range rep.PerApp {
		byApp[a.App] = [2]int{a.Covered, a.Files}
	}
	if byApp["appx"] != [2]int{6, 9} || byApp["appy"] != [2]int{2, 2} {
		t.Fatalf("per-app coverage wrong: %+v", rep.PerApp)
	}
	if len(rep.UncoveredDirs) != 2 ||
		rep.UncoveredDirs[0].Dir != "appx/src" || rep.UncoveredDirs[0].Uncovered != 2 || rep.UncoveredDirs[0].Total != 4 ||
		rep.UncoveredDirs[1].Dir != "appx/test" || rep.UncoveredDirs[1].Uncovered != 1 || rep.UncoveredDirs[1].Total != 2 {
		t.Fatalf("appx/src 2/4 and appx/test 1/2 hold the uncovered files, got %+v", rep.UncoveredDirs)
	}
}

func TestCoverageTestAxisRowsAndInventory(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	// REQ-DEMO-003 (TEST: prefix), REQ-DEMO-006 (bare path on its Tests line)
	// and REQ-DEMO-008 (ranged span) carry test citations; REQ-DEMO-007
	// writes `Tests: —`; the other four rows say nothing about tests.
	if rep.TestAxis.Rows.WithTests != 3 {
		t.Fatalf("3 rows carry a test citation, got %d", rep.TestAxis.Rows.WithTests)
	}
	if rep.TestAxis.Rows.ExplicitNone != 1 {
		t.Fatalf("1 row explicitly declares no tests, got %d", rep.TestAxis.Rows.ExplicitNone)
	}
	if rep.TestAxis.Rows.Neither != 5 {
		t.Fatalf("5 rows say nothing about tests, got %d", rep.TestAxis.Rows.Neither)
	}
	if len(rep.TestAxis.PerLedger) != 1 || rep.TestAxis.PerLedger[0].Name != "DEMO-REQUIREMENTS.md" ||
		rep.TestAxis.PerLedger[0].WithTests != 3 || rep.TestAxis.PerLedger[0].ExplicitNone != 1 ||
		rep.TestAxis.PerLedger[0].Neither != 5 {
		t.Fatalf("per-ledger test-rows split wrong: %+v", rep.TestAxis.PerLedger)
	}
	// real.spec.ts, lonely.spec.ts and two.spec.ts are the test-file
	// inventory; real.spec.ts (at path) and two.spec.ts (by basename) are
	// cited. The ghost citation names a file that does not exist, so it is
	// no denominator.
	if rep.TestAxis.Inventory.Files != 3 || rep.TestAxis.Inventory.Cited != 2 {
		t.Fatalf("test-file inventory must be 2/3, got %d/%d",
			rep.TestAxis.Inventory.Cited, rep.TestAxis.Inventory.Files)
	}
	byApp := map[string][2]int{}
	for _, a := range rep.TestAxis.Inventory.PerApp {
		byApp[a.App] = [2]int{a.Cited, a.Files}
	}
	if byApp["appx"] != [2]int{2, 3} || byApp["appy"] != [2]int{0, 0} {
		t.Fatalf("per-app test-file split wrong: %+v", rep.TestAxis.Inventory.PerApp)
	}
}

func TestCoverageTestAxisFiltersNoiseAndListsMissing(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	// The `RUN:2026-08-15` token and the un-backticked `(green, 3 cases)`
	// fragment on REQ-DEMO-006's Tests line are not path-shaped and must not
	// become citations — dead or otherwise.
	for _, c := range append(rep.Citations.ResolvedByBasename, rep.Citations.Nowhere...) {
		if strings.Contains(c.Path, "RUN:") || strings.Contains(c.Path, "cases") {
			t.Fatalf("run-log noise leaked into the citation triage: %+v", c)
		}
	}
	if rep.Citations.Distinct != 8 {
		t.Fatalf("noise must not add citations: want 8 distinct, got %d", rep.Citations.Distinct)
	}
	// The TEST citations that resolve nowhere are their own list: claimed
	// verification that does not exist.
	if len(rep.TestAxis.Nowhere) != 1 {
		t.Fatalf("exactly the ghost test is claimed but missing, got %+v", rep.TestAxis.Nowhere)
	}
	ghost := rep.TestAxis.Nowhere[0]
	if ghost.Path != "appx/test/ghost.spec.ts" || len(ghost.Reqs) != 1 || ghost.Reqs[0] != "REQ-DEMO-003" {
		t.Fatalf("the claimed-but-missing test must carry its owning REQ id: %+v", ghost)
	}
}

func TestCoverageAcceptsParenthesizedRoutePaths(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	// A Next.js route-group path carries balanced parentheses. It must
	// resolve whole — never truncate at `(grp` and land in NOWHERE (the
	// live field defect: `src/app/[locale]/(app` ← 16 REQ ids).
	for _, c := range append(rep.Citations.ResolvedByBasename, rep.Citations.Nowhere...) {
		if strings.Contains(c.Path, "(grp") {
			t.Fatalf("the route-group citation truncated at its paren: %+v", c)
		}
		// A bare citation inside prose parens — `(see CODE:…good.ts)` —
		// must not swallow the closing paren either.
		if strings.HasSuffix(c.Path, ")") && !strings.Contains(c.Path, "(") {
			t.Fatalf("a prose paren clung to a citation: %+v", c)
		}
	}
	if len(rep.Citations.Nowhere) != 1 {
		t.Fatalf("only the ghost test resolves nowhere, got %+v", rep.Citations.Nowhere)
	}
}

func TestCoverageStripsMultiRangeSuffixes(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	// `CODE:…good.ts:16-35,382-387` cites line ranges of a file that exists.
	// The multi-range suffix must strip like a single one — the live field
	// anchor showed 34 such citations reading as NOWHERE while every file
	// existed at its written path.
	for _, c := range append(rep.Citations.ResolvedByBasename, rep.Citations.Nowhere...) {
		if strings.Contains(c.Path, ",") {
			t.Fatalf("a multi-range suffix survived and broke resolution: %+v", c)
		}
	}
	// The citation folds into the row-001 citation of the same file: no new
	// distinct path, nothing dead.
	if rep.Citations.Distinct != 8 {
		t.Fatalf("the multi-range citation must normalize into the existing one: want 8 distinct, got %d",
			rep.Citations.Distinct)
	}
}

func TestCoverageStrictFailsOnlyOnNowhereCitations(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	if StrictErr(rep) == nil {
		t.Fatal("--strict must exit non-zero while a citation resolves nowhere")
	}

	// A workspace whose every citation resolves — basename-only included —
	// passes strict: only NOWHERE is an integrity failure.
	root := t.TempDir()
	for rel, content := range map[string]string{
		"tasks/OK-REQUIREMENTS.md": "# OK\n\n### REQ-OK-001 — fine\n- **Status:** READY\n- **Code:** CODE:app/src/main.ts\n",
		"app/src/main.ts":          "export {}\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clean, err := BuildReport(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Citations.Nowhere) != 0 {
		t.Fatalf("clean fixture must have no nowhere-citations: %+v", clean.Citations.Nowhere)
	}
	if err := StrictErr(clean); err != nil {
		t.Fatalf("--strict must pass when every citation resolves, got %v", err)
	}
}

func TestCoverageJSONCarriesTheSameNumbers(t *testing.T) {
	rep, err := BuildReport(seedCoverageWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"root", "ledgers", "rows", "citations", "inventory", "per_app", "uncovered_dirs", "test_axis"} {
		if _, ok := doc[key]; !ok {
			t.Fatalf("--json must carry %q, got keys %v", key, doc)
		}
	}
	rows, ok := doc["rows"].(map[string]any)
	if !ok || rows["total"].(float64) != 9 {
		t.Fatalf("rows.total must be 9 in the JSON document, got %v", doc["rows"])
	}
	citations := doc["citations"].(map[string]any)
	nowhere := citations["nowhere"].([]any)
	if len(nowhere) != 1 {
		t.Fatalf("citations.nowhere must list the one dead citation, got %v", citations["nowhere"])
	}
	entry := nowhere[0].(map[string]any)
	if entry["path"] != "appx/test/ghost.spec.ts" {
		t.Fatalf("nowhere entry must carry the path, got %v", entry)
	}
	inventory := doc["inventory"].(map[string]any)
	if inventory["files"].(float64) != 11 || inventory["covered"].(float64) != 8 {
		t.Fatalf("inventory numbers must match the text report, got %v", inventory)
	}
	axis := doc["test_axis"].(map[string]any)
	for _, key := range []string{"rows", "per_ledger", "inventory", "nowhere"} {
		if _, ok := axis[key]; !ok {
			t.Fatalf("test_axis must carry %q, got %v", key, axis)
		}
	}
	axisRows := axis["rows"].(map[string]any)
	if axisRows["with_tests"].(float64) != 3 || axisRows["explicit_none"].(float64) != 1 || axisRows["neither"].(float64) != 5 {
		t.Fatalf("test_axis.rows must match the text report, got %v", axisRows)
	}
	axisInv := axis["inventory"].(map[string]any)
	if axisInv["files"].(float64) != 3 || axisInv["cited"].(float64) != 2 {
		t.Fatalf("test_axis.inventory must be 2/3, got %v", axisInv)
	}
}
