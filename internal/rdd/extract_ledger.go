package rdd

import (
	"path/filepath"
	"regexp"

	"strings"
)

// ---------------------------------------------------------------- rdd-ledger-v1

// statusTermRe takes the leading vocabulary term out of a status cell.
var statusTermRe = regexp.MustCompile(`^[*_\s]*([A-Za-z_]+)`)

// normalizeStatus reduces a status cell to the vocabulary term the contract
// validates. Real ledgers annotate the term — "DONE (DEMO-GRADE)",
// "DEFERRED — waiting on GAP-009", "**DONE**" — and the annotation is prose
// that belongs to the ledger, not to the wire. Sending it whole had the server
// reject a 1427-op batch at op 1363, after 1362 had applied.
func normalizeStatus(cellText string) string {
	m := statusTermRe.FindStringSubmatch(cellText)
	if m == nil {
		return strings.ToUpper(strings.TrimSpace(cellText))
	}
	return strings.ToUpper(m[1])
}

// ledgerContext derives a ledger's bounded-context code from its path,
// supporting both documented layouts. Returns "" when the path is not a ledger.
// LedgerContextName pulls the human name out of a ledger's H1. Two shapes exist
// in the wild and both are legitimate:
//
//	# REQUIREMENTS — AGT (Agent Platform)          → "Agent Platform"
//	# ANL — Analytics · requirements ledger        → "Analytics"
//
// Mission Control showed bare three-letter codes because this name never left
// the workspace, though it sits in the first line of every ledger
// (`USER:2026-08-12`). An unnamed header yields "" rather than a guess.
func LedgerContextName(header string) string {
	h := strings.TrimSpace(header)
	if !strings.HasPrefix(h, "# ") {
		return ""
	}
	h = strings.TrimSpace(strings.TrimPrefix(h, "#"))

	// "REQUIREMENTS — AGT (Agent Platform)" — the name is parenthesised.
	if i := strings.Index(h, "("); i >= 0 {
		if j := strings.LastIndex(h, ")"); j > i {
			return strings.TrimSpace(h[i+1 : j])
		}
	}

	// "ANL — Analytics · requirements ledger" — name after the dash, cut at the
	// middot that introduces the file's own description.
	for _, dash := range []string{" — ", " – ", " - "} {
		if i := strings.Index(h, dash); i >= 0 {
			rest := strings.TrimSpace(h[i+len(dash):])
			if k := strings.Index(rest, " · "); k >= 0 {
				rest = strings.TrimSpace(rest[:k])
			}
			// "REQUIREMENTS — PLT" carries a code, not a name.
			if rest == strings.ToUpper(rest) && !strings.ContainsAny(rest, " &") {
				return ""
			}
			return rest
		}
	}
	return ""
}

func ledgerContext(path string) string {
	base := filepath.Base(path)
	if m := ledgerFileRe.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	if !strings.EqualFold(base, "REQUIREMENTS.md") {
		return ""
	}
	dir := filepath.Base(filepath.Dir(path))
	if m := ledgerDirRe.FindStringSubmatch(dir); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

var (
	ledgerFileRe = regexp.MustCompile(`^([A-Z][A-Z0-9]*)-REQUIREMENTS\.md$`)
	// the context directory: letters only, so "libs" or "." never becomes one
	ledgerDirRe = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})$`)
	// A ledger row may name a UR, an SR, or the historical REQ- form. The prefix
	// is how the ledger carries the UR/SR distinction that PROCESS.md draws and
	// that `file-state` already writes as `UR-AUTH-001` / `SR-AUTH-002`: without
	// it every row syncs as a system requirement, and a user requirement's upper
	// evidence class is silently reassigned. REQ- keeps meaning "system", so no
	// existing ledger changes shape or content hash.
	ledgerRowRe    = regexp.MustCompile(`^\|\s*((?:REQ|UR|SR)-[A-Z][A-Z0-9]*-\d+)\s*\|`)
	ledgerDetailRe = regexp.MustCompile(`^###\s+((?:REQ|UR|SR)-[A-Z][A-Z0-9]*-\d+)`)
	// A ledger also files capability GAPS beside the requirements they block:
	// same level-3 block shape, an explicit GAP-<CTX>-<n> id, no lifecycle of
	// its own. Its own matcher rather than a wider ledgerDetailRe — widening
	// that one would push gap prose into requirement detail_md and give the
	// gap a requirement's shape, which is the record it is deliberately not.
	ledgerGapDetailRe = regexp.MustCompile(`^###\s+(GAP-[A-Z][A-Z0-9]*-\d+)\b`)
	headingH2Re       = regexp.MustCompile(`^##\s`)
	headingH3Re       = regexp.MustCompile(`^###\s`)
)

// ParseLedger parses one ledger file (rdd-ledger-v1). Two layouts are the
// documented convention and both are accepted:
//
//	tasks/<CTX>-REQUIREMENTS.md   context in the filename (this workspace)
//	libs/<ctx>/REQUIREMENTS.md    context in the directory (generic framework)
//
// The second was rejected until 2026-08-08, when the first real client
// repository turned out to use it and had all 19 of its ledgers skipped.
// A bare REQUIREMENTS.md with no context directory is still skipped: there is
// nothing to derive a context from, and guessing would mislabel every row.
// ledgerColumns maps a logical field to its 1-based cell index, read from the
// ledger's header row. Unknown headers are ignored; a missing field maps to 0,
// which `cell` returns as "".
func ledgerColumns(lines []string) map[string]int {
	// The workspace's historical order, used when no header row is found.
	col := map[string]int{"id": 1, "title": 2, "stage": 3, "status": 4, "source": 5, "tests": 6, "code": 7}

	for _, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		// A header row starts with the id column, and agents name it "ID" or
		// "REQ". Requiring "ID" made a real 1,464-row ledger fall through to
		// positional order and sync its evidence text as work_status
		// (`RUN:2026-08-12`).
		if !strings.HasPrefix(l, "| id ") && !strings.HasPrefix(l, "|id ") &&
			!strings.HasPrefix(l, "| req ") && !strings.HasPrefix(l, "|req ") {
			continue
		}
		found := map[string]int{}
		for i, raw := range splitCells(line) {
			h := strings.ToLower(strings.TrimSpace(raw))
			switch {
			case h == "id" || h == "req":
				found["id"] = i
			// "Behaviour" is what a derived ledger calls the thing this
			// workspace calls "Title", and other modern ledgers head it
			// "Requirement" — one column under three names. A name that does not
			// bind is not a soft failure: the title serializes as "" and the
			// server refuses the whole batch with `422 title: can't be blank`
			// (`RUN:2026-08-13`, 1,786 requirements).
			case h == "title" || h == "behaviour" || h == "behavior" || h == "requirement":
				found["title"] = i
			case h == "stage":
				found["stage"] = i
			case h == "status":
				found["status"] = i
			case h == "ur" || h == "user requirement":
				found["ur"] = i
			case h == "source" || h == "sources" || strings.HasPrefix(h, "src"):
				found["source"] = i
			// "Evidence" is what the skill calls the column the workspace calls
			// "Tests" — the same thing under two names.
			case h == "tests" || h == "evidence" || h == "test":
				found["tests"] = i
			case h == "code":
				found["code"] = i
			// REQ-CROSS-222: first-class capture — these were dropped before.
			case h == "priority":
				found["priority"] = i
			case h == "owner":
				found["owner"] = i
			case h == "release":
				found["release"] = i
			}
		}
		// Three recognised columns is a header. Requiring four rejected the
		// | REQ | Behaviour | Status | Evidence | Src | shape, which carries no
		// Stage and no Code — absence of a column is a fact about the ledger,
		// not a reason to guess every column's position.
		if len(found) >= 3 {
			return found
		}
	}
	return col
}

// REQ-CROSS-076: a ledger with no `UR` column can still name the parent. One
// real corpus put a bare `UR-BUL-1` in the column headed `Source` — 1,336 of
// 1,786 rows — and without reading it every one of them syncs as an orphan.
//
// Narrow on purpose. Only when the ledger has NO dedicated UR column, and only
// when the cell is a BARE reference: a UR mentioned inside a sentence is a
// citation, not a parent, and the other 450 rows in that corpus hold a real
// source that must survive untouched.
var bareURRe = regexp.MustCompile(`^UR-[A-Z0-9]+-\d+$`)

func urFromSource(col map[string]int, source string) string {
	if _, explicit := col["ur"]; explicit {
		return ""
	}
	if bareURRe.MatchString(strings.TrimSpace(source)) {
		return strings.TrimSpace(source)
	}
	return ""
}

// urOrFallback prefers the dedicated column and falls back to a bare reference
// in the source cell (REQ-CROSS-076).
func urOrFallback(col map[string]int, cells []string) string {
	if v := cell(cells, col["ur"]); v != "" {
		return v
	}
	return urFromSource(col, cell(cells, col["source"]))
}

// SR-SY-1402 (EPIC-SYNC-014): the UR derivation pass writes the parent as its
// own detail-block bullet — `- **UR:** UR-DEAL-004` — in ledgers that carry no
// UR column at all. One analyzed estate holds 357 of these across 596 rows,
// and reading none of them left the whole system as orphans on the
// traceability face (`RUN:2026-08-16`). §245.6 widens it to the inline
// `· **UR:** …` decoration of a Status line: this corpus writes 614 of those
// and zero own-bullet forms, and the fallback only fires when the UR column
// is empty, so a column-carrying row is never overridden. The value runs to
// the next BOLD label (`· **Source:** …`), not to the first interpunct — an
// own-bullet multi-UR list (`UR-A · UR-B`) is UR content and must survive
// whole, or its extra parents drop with no snapshot warning.
var urDetailLineRe = regexp.MustCompile(`\*\*UR:\*\*[ \t]*([^\n]+)`)

func urFromDetail(detail string) string {
	m := urDetailLineRe.FindStringSubmatch(detail)
	if m == nil {
		return ""
	}
	// RE2 has no lookahead: capture the line, then keep interpunct segments
	// until the next bold label starts.
	var keep []string
	for _, part := range strings.Split(m[1], "·") {
		if strings.Contains(part, "**") {
			break
		}
		keep = append(keep, strings.TrimSpace(part))
	}
	return strings.Join(keep, " · ")
}

func ParseLedger(path string, content string) []Req {
	ctx := ledgerContext(path)
	ctxName := ""
	if i := strings.Index(content, "\n"); i >= 0 {
		ctxName = LedgerContextName(content[:i])
	} else {
		ctxName = LedgerContextName(content)
	}
	if ctx == "" {
		return nil
	}

	// detail blocks: ### REQ-XXX-NNN ... until the next ###/## heading or EOF
	// (extract.js uses a lookahead; RE2 has none, so scan lines with offsets)
	details := map[string]string{}
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		dm := ledgerDetailRe.FindStringSubmatch(lines[i])
		if dm == nil {
			continue
		}
		var body []string
		j := i + 1
		for ; j < len(lines); j++ {
			if headingH3Re.MatchString(lines[j]) || headingH2Re.MatchString(lines[j]) {
				break
			}
			body = append(body, lines[j])
		}
		details[dm[1]] = capRunes(strings.Join(body, "\n"), bodyCap)
		i = j - 1
	}

	// Columns are found by NAME from the header row, never by position. Two
	// layouts exist in the wild and positional indexing mislabels one of them:
	//   workspace     | ID | Title | Stage | Status | Source | Tests | Code |
	//   skill-written | ID | Title | Stage | Status | UR | Source | Evidence | Code |
	// Reading cell 6 as "Tests" turned a doc reference into a test citation
	// (`RUN:2026-08-12`). Where no header is found the workspace order is the
	// fallback, so older ledgers keep parsing.
	col := ledgerColumns(lines)
	extraCols := ledgerExtraColumns(lines, col)

	// The header's own width. A ledger may carry a SECOND table that reuses the
	// same ids in a different shape — an appendix of findings, three columns
	// wide — and parsing those rows against the dashboard's map put prose into
	// the status (`RUN:2026-08-12`, a live ledger at line 4437). A row narrower than
	// the header is not a row of that table.
	width := 0
	for _, i := range col {
		if i > width {
			width = i
		}
	}

	var reqs []Req
	seenRow := map[string]bool{}
	for _, line := range lines {
		if !ledgerRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) <= width {
			continue
		}
		// First occurrence wins: the dashboard comes before any appendix.
		if id := cell(cells, col["id"]); seenRow[id] {
			continue
		} else {
			seenRow[id] = true
		}
		detail := details[cell(cells, col["id"])]
		ur := urOrFallback(col, cells)
		if ur == "" {
			ur = urFromDetail(detail) // SR-SY-1402: the detail-block UR line
		}
		reqs = append(reqs, Req{
			ID:      cell(cells, col["id"]),
			Title:   cell(cells, col["title"]),
			Stage:   cell(cells, col["stage"]),
			Status:  normalizeStatus(cell(cells, col["status"])),
			Source:  cell(cells, col["source"]),
			Tests:   cell(cells, col["tests"]),
			Code:    cell(cells, col["code"]),
			UR:      ur,
			Ctx:     ctx,
			CtxName: ctxName,
			Detail:  detail,
			// REQ-CROSS-222: carried, never dropped. Dash cells are absence.
			Priority: dashless(cell(cells, col["priority"])),
			Owner:    dashless(cell(cells, col["owner"])),
			Release:  dashless(cell(cells, col["release"])),
			Extras:   extraCells(extraCols, cells),
		})
	}
	return reqs
}

// dashless treats the ledgers' em/en-dash and hyphen placeholders as absence.
func dashless(s string) string {
	switch strings.TrimSpace(s) {
	case "", "—", "-", "–":
		return ""
	}
	return strings.TrimSpace(s)
}

// extraCol is one header column the extractor does not recognize.
type extraCol struct {
	Name string
	Idx  int
}

// ledgerExtraColumns lists the header columns outside the recognized set
// (REQ-CROSS-222 §222.2), so their cells can be carried as named extras.
// Same header-selection rule as ledgerColumns: the first id-prefixed line
// with at least three recognized column names. A ledger parsed by the
// positional fallback has no named extras.
func ledgerExtraColumns(lines []string, _ map[string]int) []extraCol {
	for _, line := range lines {
		l := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(l, "| id ") && !strings.HasPrefix(l, "|id ") &&
			!strings.HasPrefix(l, "| req ") && !strings.HasPrefix(l, "|req ") {
			continue
		}
		var out []extraCol
		recognized := 0
		for i, raw := range splitCells(line) {
			name := strings.TrimSpace(raw)
			if name == "" {
				continue
			}
			if recognizedLedgerColumn(strings.ToLower(name)) {
				recognized++
				continue
			}
			out = append(out, extraCol{Name: name, Idx: i})
		}
		if recognized < 3 {
			continue // not the header ledgerColumns chose
		}
		return out
	}
	return nil
}

// extraCells reads the unrecognized columns' values for one row.
func extraCells(extraCols []extraCol, cells []string) []NamedCell {
	var out []NamedCell
	for _, ec := range extraCols {
		if v := dashless(cell(cells, ec.Idx)); v != "" {
			out = append(out, NamedCell{Name: ec.Name, Value: v})
		}
	}
	return out
}
