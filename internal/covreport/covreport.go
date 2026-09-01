package covreport

// REQ-CROSS-179: requirement→code coverage is a measured fact.
//
// `modernpath coverage` parses every tasks/*-REQUIREMENTS.md ledger, extracts
// the CODE:/TEST: citations from the detail blocks, resolves them against the
// workspace tree, and reports citation integrity (resolved at the written
// path / resolved by basename only / resolves nowhere) plus what fraction of
// the real source inventory is cited by at least one requirement — so a
// reverse-engineering sweep can target the largest uncovered directories
// next.
//
// The parsing semantics are a faithful port of the field audit prototype
// (`req_coverage.py`, RUN:2026-08-16): rows are `### REQ-<CTX>-NNN` detail
// headings (not dashboard rows — internal/rdd's ParseLedger reads those, a
// different axis), status is the block's first `**Status:**`, a directory
// citation covers every source file under it, and a dead path falls back to
// a basename match before it counts as missing.

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ------------------------------------------------------------------ report

type Report struct {
	Root          string    `json:"root"`
	Ledgers       []Ledger  `json:"ledgers"`
	Rows          Rows      `json:"rows"`
	Citations     Citations `json:"citations"`
	Inventory     Inventory `json:"inventory"`
	PerApp        []App     `json:"per_app"`
	UncoveredDirs []Dir     `json:"uncovered_dirs"`
	TestAxis      TestAxis  `json:"test_axis"`
}

// TestAxis is the separate test-file axis (REQ-CROSS-178 names test
// files as their own denominator class): which rows claim tests, which test
// files the tree actually holds, and which claimed tests do not exist.
type TestAxis struct {
	Rows      TestRows     `json:"rows"`
	PerLedger []TestLedger `json:"per_ledger"`
	Inventory TestInv      `json:"inventory"`
	// Nowhere lists the TEST citations resolving nowhere — requirements
	// claiming verification that does not exist.
	Nowhere []Citation `json:"nowhere"`
}

type TestRows struct {
	WithTests    int `json:"with_tests"`
	ExplicitNone int `json:"explicit_none"`
	Neither      int `json:"neither"`
}

type TestLedger struct {
	Name         string `json:"name"`
	WithTests    int    `json:"with_tests"`
	ExplicitNone int    `json:"explicit_none"`
	Neither      int    `json:"neither"`
}

type TestInv struct {
	Files  int       `json:"files"`
	Cited  int       `json:"cited"`
	PerApp []TestApp `json:"per_app"`
}

type TestApp struct {
	App   string `json:"app"`
	Files int    `json:"files"`
	Cited int    `json:"cited"`
}

type Ledger struct {
	Name string `json:"name"`
	Rows int    `json:"rows"`
}

type Rows struct {
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"by_status"`
	Linked   int            `json:"linked"`
	Unlinked []string       `json:"unlinked"`
}

// Citation is one triaged citation: the path as written, the REQ ids
// that cite it, and — for basename-only resolutions — where it landed.
type Citation struct {
	Path string   `json:"path"`
	Reqs []string `json:"reqs"`
	Hits []string `json:"hits,omitempty"`
}

type Citations struct {
	Distinct           int        `json:"distinct"`
	ResolvedAtPath     int        `json:"resolved_at_path"`
	ResolvedFiles      int        `json:"resolved_files"`
	ResolvedByBasename []Citation `json:"resolved_by_basename"`
	Nowhere            []Citation `json:"nowhere"`
}

type Inventory struct {
	Apps    []string `json:"apps"`
	Files   int      `json:"files"`
	Covered int      `json:"covered"`
}

type App struct {
	App     string `json:"app"`
	Files   int    `json:"files"`
	Covered int    `json:"covered"`
}

type Dir struct {
	Dir       string `json:"dir"`
	Uncovered int    `json:"uncovered"`
	Total     int    `json:"total"`
}

// ------------------------------------------------------------------ parsing

// The prototype's matchers, paren-hardened. covLinkRe stops a bare path at
// whitespace, a backtick, ',' or ';' — NOT at ')': Next.js route groups put
// balanced parens inside real paths (`src/app/[locale]/(app)/…/page.tsx`),
// and stopping there truncated them into NOWHERE. A trailing ')' that closes
// a prose paren is trimmed afterwards by covTrimUnbalanced. Backtick-wrapped
// citations are taken as their exact content — backticks are the house
// convention and delimit precisely. covSuffixRe strips one trailing :line,
// :157+, :10-20 or :lowercasesymbol.
var (
	covHeadingRe  = regexp.MustCompile(`^### ((?:REQ|UR|SR)-[A-Z][A-Z0-9]*-\d+)`) // UR-/SR- carry the requirement kind; REQ- is the historical system form
	covStatusRe   = regexp.MustCompile(`\*\*Status:\*\*\s*([A-Z_]+)`)
	covLinkRe     = regexp.MustCompile("(CODE|TEST):([^\\s`,;]+)")
	covBarePathRe = regexp.MustCompile("`([^`]+\\.(?:ts|tsx|js|jsx|mjs|py|prisma|sql))(?::\\d+)?`")
	// One trailing :suffix — a line (`:12`, `:157+`), a range (`:10-20`), a
	// comma-separated list of either (`:16-35,382-387`, `:42,154,259`), or a
	// lowercase symbol. Backtick-exact citations carry the full list, so the
	// matcher must swallow it whole or the file reads as missing.
	covSuffixRe    = regexp.MustCompile(`:(\d+(?:\+|-\d+)?(?:,\d+(?:\+|-\d+)?)*|[a-z]+)$`)
	covBarePrefRe  = regexp.MustCompile(`^(CODE|TEST):`)
	covBacktickRe  = regexp.MustCompile("`([^`]+)`")
	covTestsLineRe = regexp.MustCompile(`\*\*Tests:\*\*\s*(.*)$`)
)

// covTrimUnbalanced trims trailing ')' while the token holds more closers
// than openers: `(see CODE:foo/bar.ts)` cites foo/bar.ts, but a route-group
// path's balanced parens stay untouched.
func covTrimUnbalanced(token string) string {
	for strings.HasSuffix(token, ")") && strings.Count(token, ")") > strings.Count(token, "(") {
		token = token[:len(token)-1]
	}
	return token
}

// A language missing here is invisible to coverage, and coverage reads as if
// the code were simply uncovered. A .NET estate of 4,963 C# files reported a
// denominator of 10 because `.cs` was absent — the instrument was measuring a
// different repository than the one on disk.
var covSourceExt = map[string]bool{
	".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true,
	".cjs": true, ".py": true, ".prisma": true, ".sql": true, ".go": true,
	".css": true, ".scss": true,
	".cs": true, ".vb": true, ".fs": true, ".proto": true,
	".java": true, ".kt": true, ".rb": true, ".rs": true, ".php": true,
	".swift": true, ".ex": true, ".exs": true, ".erl": true, ".scala": true,
}

var covExcludeDirs = map[string]bool{
	"node_modules": true, ".next": true, "dist": true, "build": true,
	".git": true, "coverage": true, "generated": true, ".modernpath": true,
}

// covRow is one requirement detail block. paths maps each cited path to its
// kinds ("code" and/or "test"); explicitNoTests records a `- **Tests:** —`
// line — the row saying out loud that no test exists.
type covRow struct {
	id              string
	status          string
	ledger          string
	paths           map[string]map[string]bool
	explicitNoTests bool
}

// covPathShaped is the noise filter for bare Tests-line tokens: only a token
// that looks like a file path counts — it contains a separator or ends in a
// known source extension. `RUN:2026-08-15` tags and prose fragments do not.
func covPathShaped(token string) bool {
	return strings.Contains(token, "/") || covSourceExt[filepath.Ext(token)]
}

// parseCoverageLedgers reads every ledger and returns the rows keyed by REQ id
// (a re-declared id overwrites, as in the prototype) plus per-ledger counts.
func parseCoverageLedgers(paths []string) (map[string]*covRow, []string, []Ledger, error) {
	rows := map[string]*covRow{}
	var order []string
	var ledgers []Ledger
	for _, ledger := range paths {
		content, err := os.ReadFile(ledger)
		if err != nil {
			return nil, nil, nil, err
		}
		name := filepath.Base(ledger)
		count := 0
		var cur *covRow
		addPath := func(p, kind string) {
			if cur.paths[p] == nil {
				cur.paths[p] = map[string]bool{}
			}
			cur.paths[p][kind] = true
		}
		for _, line := range strings.Split(string(content), "\n") {
			if m := covHeadingRe.FindStringSubmatch(line); m != nil {
				count++
				if _, seen := rows[m[1]]; !seen {
					order = append(order, m[1])
				}
				cur = &covRow{id: m[1], status: "?", ledger: name, paths: map[string]map[string]bool{}}
				rows[m[1]] = cur
				continue
			}
			if cur == nil {
				continue
			}
			if m := covStatusRe.FindStringSubmatch(line); m != nil && cur.status == "?" {
				cur.status = m[1]
			}
			testsLine := covTestsLineRe.FindStringSubmatch(line)
			if testsLine != nil {
				switch strings.TrimSpace(testsLine[1]) {
				case "—", "-", "–":
					cur.explicitNoTests = true
				}
			}
			if !strings.Contains(line, "**Code:**") && testsLine == nil &&
				!strings.Contains(line, "CODE:") && !strings.Contains(line, "TEST:") {
				continue
			}
			// Backtick-wrapped citations are exact: a CODE:/TEST:-prefixed
			// span is that path verbatim (parens included) on any line; a
			// bare span on a `- **Tests:**` line is a TEST citation when it
			// is path-shaped — RUN: tags and prose fragments are ignored.
			for _, bm := range covBacktickRe.FindAllStringSubmatch(line, -1) {
				token := bm[1]
				if pref := covBarePrefRe.FindStringSubmatch(token); pref != nil {
					token = covSuffixRe.ReplaceAllString(covBarePrefRe.ReplaceAllString(token, ""), "")
					addPath(token, strings.ToLower(pref[1]))
					continue
				}
				if testsLine != nil {
					token = covSuffixRe.ReplaceAllString(token, "")
					if covPathShaped(token) {
						addPath(token, "test")
					}
				}
			}
			// Bare CODE:/TEST: tokens are read outside the backtick spans, so
			// the exact span above is not shadowed by a re-scan of its text.
			masked := covBacktickRe.ReplaceAllLiteralString(line, "`")
			for _, lm := range covLinkRe.FindAllStringSubmatch(masked, -1) {
				addPath(covSuffixRe.ReplaceAllString(covTrimUnbalanced(lm[2]), ""), strings.ToLower(lm[1]))
			}
			if testsLine != nil {
				continue
			}
			for _, bm := range covBarePathRe.FindAllStringSubmatch(line, -1) {
				kind := "code"
				if strings.HasPrefix(bm[1], "TEST:") {
					kind = "test"
				}
				addPath(covBarePrefRe.ReplaceAllString(bm[1], ""), kind)
			}
		}
		ledgers = append(ledgers, Ledger{Name: name, Rows: count})
	}
	return rows, order, ledgers, nil
}

// isCoverageTestFile marks a source-inventory file as a test: a basename
// carrying .spec./.test. or a test_ prefix, or a test/tests/__tests__ path
// segment.
func isCoverageTestFile(rel string) bool {
	parts := strings.Split(rel, "/")
	base := parts[len(parts)-1]
	if strings.Contains(base, ".spec.") || strings.Contains(base, ".test.") || strings.HasPrefix(base, "test_") {
		return true
	}
	for _, seg := range parts[:len(parts)-1] {
		if seg == "test" || seg == "tests" || seg == "__tests__" {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------------ tree

// gitVisibleFiles is the set of workspace-relative paths git accounts for:
// tracked files plus untracked ones it does not ignore. It is the honest
// definition of "the repository's source".
//
// Without it the denominator counted anything on disk. A real workspace held a
// gitignored `test-runs/` lab directory carrying a vendored Python package —
// 2481 files, 61% of the denominator, 0 of them TypeScript in a TypeScript
// project — and coverage read 31.2% when the tracked source was over 80%. A
// number that moves when someone runs a local experiment is not a measurement.
//
// Returns nil when git cannot answer (not a repository, git absent), and the
// caller then counts everything, which is the previous behaviour.
func gitVisibleFiles(root string) map[string]bool {
	visible := map[string]bool{}
	collect(root, "", visible)

	// Ask every child that is a repository too, not only when the root tracked
	// nothing. A superproject records each submodule as a single gitlink, so the
	// root DOES list files (docs, tooling) while the submodules' contents are
	// invisible — 4,963 C# files behind 8 gitlinks, in the case that found this.
	// Unioning is safe: a plain subdirectory of the root is already listed, and
	// the map de-duplicates.
	{
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				// A submodule's .git is a FILE pointing into the superproject;
				// a nested clone's is a directory. Both count.
				if _, err := os.Stat(filepath.Join(root, entry.Name(), ".git")); err != nil {
					continue
				}
				collect(filepath.Join(root, entry.Name()), entry.Name()+"/", visible)
			}
		}
	}

	if len(visible) == 0 {
		return nil
	}
	return visible
}

// collect adds one repository's git-visible files to the set, prefixed so the
// paths stay relative to the workspace root the walker reports against.
func collect(dir, prefix string, into map[string]bool) {
	cmd := exec.Command("git", "ls-files", "--cached", "--others", "--exclude-standard")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			into[prefix+filepath.ToSlash(line)] = true
		}
	}
}

var (
	gitIgnoredSeen  = map[string]int{}
	gitVisibleMu    sync.Mutex
	gitVisibleCache = map[string]map[string]bool{}
	gitVisibleDone  = map[string]bool{}
)

// countGitIgnored records a source-extension file skipped because git ignores
// it, so the report can say how big the exclusion was. A denominator that
// quietly shrinks is the same failure as one that quietly grows.
func countGitIgnored(root string) {
	gitVisibleMu.Lock()
	gitIgnoredSeen[root]++
	gitVisibleMu.Unlock()
}

// GitIgnoredCount is how many source-extension files were left out of the
// denominator because git ignores them.
func GitIgnoredCount(root string) int {
	gitVisibleMu.Lock()
	defer gitVisibleMu.Unlock()
	return gitIgnoredSeen[root]
}

// gitVisible memoizes gitVisibleFiles: walkSourceFiles runs once per app
// directory, and shelling out per directory would be both slow and noisy.
func gitVisible(root string) map[string]bool {
	gitVisibleMu.Lock()
	defer gitVisibleMu.Unlock()
	if !gitVisibleDone[root] {
		gitVisibleCache[root] = gitVisibleFiles(root)
		gitVisibleDone[root] = true
	}
	return gitVisibleCache[root]
}

// walkSourceFiles returns the workspace-relative source files under dir,
// pruning the excluded directories.
func walkSourceFiles(root, dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if path != dir && covExcludeDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if covSourceExt[filepath.Ext(d.Name())] {
			if rel, err := filepath.Rel(root, path); err == nil {
				slash := filepath.ToSlash(rel)
				if visible := gitVisible(root); visible != nil && !visible[slash] {
					countGitIgnored(root)
					return nil
				}
				files = append(files, slash)
			}
		}
		return nil
	})
	return files
}

// deriveApps derives the app set: top-level directories, not dot-prefixed
// and not excluded, that hold at least one source file or one resolved
// citation. Never a hardcoded list.
func deriveApps(root string, citedTop map[string]bool) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || covExcludeDirs[e.Name()] {
			continue
		}
		if citedTop[e.Name()] || len(walkSourceFiles(root, filepath.Join(root, e.Name()))) > 0 {
			apps = append(apps, e.Name())
		}
	}
	sort.Strings(apps)
	return apps, nil
}

// ------------------------------------------------------------------ build

func BuildReport(root string) (*Report, error) {
	ledgerPaths, err := filepath.Glob(filepath.Join(root, "tasks", "*-REQUIREMENTS.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(ledgerPaths)
	if len(ledgerPaths) == 0 {
		return nil, fmt.Errorf("no tasks/*-REQUIREMENTS.md ledgers under %s", root)
	}

	rows, order, ledgers, err := parseCoverageLedgers(ledgerPaths)
	if err != nil {
		return nil, err
	}

	// Resolve every citation: at its written path (a directory citation
	// covers each source file under it), or record it dead for triage.
	cited := map[string]map[string]bool{} // real file -> REQ ids
	dead := map[string]map[string]bool{}  // written path -> REQ ids
	addCited := func(path, id string) {
		if cited[path] == nil {
			cited[path] = map[string]bool{}
		}
		cited[path][id] = true
	}
	distinct := map[string]bool{}
	pathKinds := map[string]map[string]bool{} // written path -> union of kinds
	for _, id := range order {
		for p, kinds := range rows[id].paths {
			distinct[p] = true
			if pathKinds[p] == nil {
				pathKinds[p] = map[string]bool{}
			}
			for k := range kinds {
				pathKinds[p][k] = true
			}
			info, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(p)))
			switch {
			case statErr == nil && !info.IsDir():
				addCited(p, id)
			case statErr == nil && info.IsDir():
				for _, f := range walkSourceFiles(root, filepath.Join(root, filepath.FromSlash(p))) {
					addCited(f, id)
				}
			default:
				if dead[p] == nil {
					dead[p] = map[string]bool{}
				}
				dead[p][id] = true
			}
		}
	}

	citedTop := map[string]bool{}
	for p := range cited {
		if i := strings.Index(p, "/"); i > 0 {
			citedTop[p[:i]] = true
		}
	}
	apps, err := deriveApps(root, citedTop)
	if err != nil {
		return nil, err
	}

	// Source inventory, and a basename index over every file in the apps for
	// the dead-citation fallback.
	inventory := map[string]bool{}
	basenames := map[string][]string{}
	for _, app := range apps {
		base := filepath.Join(root, app)
		for _, f := range walkSourceFiles(root, base) {
			inventory[f] = true
		}
		_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if path != base && covExcludeDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}
			if rel, err := filepath.Rel(root, path); err == nil {
				basenames[d.Name()] = append(basenames[d.Name()], filepath.ToSlash(rel))
			}
			return nil
		})
	}

	// Dead-citation triage: a path that no longer resolves may still name a
	// real file by basename — non-canonical, listed for normalization, and
	// its first hit counts as covered. Anything else exists nowhere.
	deadPaths := make([]string, 0, len(dead))
	for p := range dead {
		deadPaths = append(deadPaths, p)
	}
	sort.Strings(deadPaths)
	var byBasename, nowhere, testNowhere []Citation
	for _, p := range deadPaths {
		name := p
		if i := strings.IndexAny(name, " "); i >= 0 {
			name = name[:i]
		}
		if i := strings.Index(name, "·"); i >= 0 {
			name = name[:i]
		}
		// A multi-file span keeps a :line/:10-20 range glued to its first
		// segment — strip it, or a test that exists reads as missing.
		name = filepath.Base(covSuffixRe.ReplaceAllString(strings.TrimSpace(name), ""))
		entry := Citation{Path: p, Reqs: sortedKeys(dead[p])}
		if hits := basenames[name]; len(hits) > 0 {
			sort.Strings(hits)
			entry.Hits = hits
			byBasename = append(byBasename, entry)
			for id := range dead[p] {
				addCited(hits[0], id)
			}
		} else {
			nowhere = append(nowhere, entry)
			if pathKinds[p]["test"] {
				testNowhere = append(testNowhere, entry)
			}
		}
	}

	covered := map[string]bool{}
	for p := range cited {
		if inventory[p] {
			covered[p] = true
		}
	}

	// Rows rollup.
	byStatus := map[string]int{}
	linked := 0
	var unlinked []string
	for _, id := range order {
		byStatus[rows[id].status]++
		if len(rows[id].paths) > 0 {
			linked++
		} else {
			unlinked = append(unlinked, id)
		}
	}
	sort.Strings(unlinked)

	// Per-app coverage and the largest uncovered directories (2nd/3rd path
	// level), ranked by uncovered source-file count.
	var perApp []App
	for _, app := range apps {
		a := App{App: app}
		for p := range inventory {
			if strings.HasPrefix(p, app+"/") {
				a.Files++
				if covered[p] {
					a.Covered++
				}
			}
		}
		perApp = append(perApp, a)
	}
	uncTotals := map[string]int{}
	uncCounts := map[string]int{}
	for p := range inventory {
		parts := strings.Split(p, "/")
		key := strings.Join(parts[:min(2, len(parts))], "/")
		if len(parts) > 3 {
			key = strings.Join(parts[:3], "/")
		}
		uncTotals[key]++
		if !covered[p] {
			uncCounts[key]++
		}
	}
	var uncDirs []Dir
	for dir, n := range uncCounts {
		uncDirs = append(uncDirs, Dir{Dir: dir, Uncovered: n, Total: uncTotals[dir]})
	}
	sort.Slice(uncDirs, func(i, j int) bool {
		if uncDirs[i].Uncovered != uncDirs[j].Uncovered {
			return uncDirs[i].Uncovered > uncDirs[j].Uncovered
		}
		return uncDirs[i].Dir < uncDirs[j].Dir
	})
	if len(uncDirs) > 20 {
		uncDirs = uncDirs[:20]
	}

	// The separate test axis: which rows claim tests, and how much of the
	// tree's own test-file inventory any requirement cites.
	testRows := TestRows{}
	perLedgerTest := map[string]*TestRows{}
	for _, id := range order {
		row := rows[id]
		if perLedgerTest[row.ledger] == nil {
			perLedgerTest[row.ledger] = &TestRows{}
		}
		hasTest := false
		for _, kinds := range row.paths {
			if kinds["test"] {
				hasTest = true
				break
			}
		}
		switch {
		case hasTest:
			testRows.WithTests++
			perLedgerTest[row.ledger].WithTests++
		case row.explicitNoTests:
			testRows.ExplicitNone++
			perLedgerTest[row.ledger].ExplicitNone++
		default:
			testRows.Neither++
			perLedgerTest[row.ledger].Neither++
		}
	}
	var testLedgers []TestLedger
	for _, l := range ledgers {
		split := perLedgerTest[l.Name]
		if split == nil {
			split = &TestRows{}
		}
		testLedgers = append(testLedgers, TestLedger{
			Name: l.Name, WithTests: split.WithTests,
			ExplicitNone: split.ExplicitNone, Neither: split.Neither,
		})
	}
	testInv := TestInv{}
	testByApp := map[string]*TestApp{}
	for _, app := range apps {
		testByApp[app] = &TestApp{App: app}
	}
	for p := range inventory {
		if !isCoverageTestFile(p) {
			continue
		}
		testInv.Files++
		app := p[:strings.Index(p, "/")]
		testByApp[app].Files++
		if covered[p] {
			testInv.Cited++
			testByApp[app].Cited++
		}
	}
	for _, app := range apps {
		testInv.PerApp = append(testInv.PerApp, *testByApp[app])
	}

	return &Report{
		Root:    root,
		Ledgers: ledgers,
		Rows: Rows{
			Total:    len(rows),
			ByStatus: byStatus,
			Linked:   linked,
			Unlinked: unlinked,
		},
		Citations: Citations{
			Distinct:           len(distinct),
			ResolvedAtPath:     len(distinct) - len(dead),
			ResolvedFiles:      len(cited),
			ResolvedByBasename: byBasename,
			Nowhere:            nowhere,
		},
		Inventory: Inventory{
			Apps:    apps,
			Files:   len(inventory),
			Covered: len(covered),
		},
		PerApp:        perApp,
		UncoveredDirs: uncDirs,
		TestAxis: TestAxis{
			Rows:      testRows,
			PerLedger: testLedgers,
			Inventory: testInv,
			Nowhere:   testNowhere,
		},
	}, nil
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// StrictErr is the --strict verdict: only a citation that resolves
// NOWHERE fails the run — basename-only resolutions are flagged for
// normalization but do not block.
func StrictErr(rep *Report) error {
	if n := len(rep.Citations.Nowhere); n > 0 {
		return fmt.Errorf("%d citation(s) resolve nowhere", n)
	}
	return nil
}

// ------------------------------------------------------------------ output

func Pct(a, b int) string {
	if b == 0 {
		return "n/a"
	}
	return fmt.Sprintf("%.1f%%", 100.0*float64(a)/float64(b))
}
