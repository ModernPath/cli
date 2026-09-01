// Package rdd — the CLI-bundled parsers for the RDD process-document formats
// (REQ-CROSS-013, EPIC-SYNC-004 TASK-SY-404). Originally a faithful Go port of
// a node extractor + op-builder that lived in the workspace: same fields, same
// heuristics, same content hashes, so the switch was hash-stable. That node
// side is retired; these parsers are the only implementation, and the hash
// pins in ops_test.go are what still holds them to the ported behaviour.
//
// Formats: rdd-ledger-v1 (tasks/<CTX>-REQUIREMENTS.md), rdd-worklist-v1
// (WORKLIST.md), rdd-epic-v1 (epics/*), rdd-review-queue-v1 (docs/85),
// rdd-open-questions-v1 (process/08).
package rdd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const bodyCap = 15000 // extract.js BODY_CAP (UTF-16 code units, like JS .slice)

// Req is one requirements-ledger row (+ its detail block).
type Req struct {
	ID      string
	Title   string
	Stage   string
	Status  string
	Source  string
	Ctx     string
	CtxName string // the context's human name from the ledger H1; "" = unnamed
	// Columns 6 and 7 of the dashboard row. The node mirror parsed them and this
	// did not, so every code and test citation in every ledger stopped at the
	// workspace boundary — the compliance views reported "Code: Missing" on all
	// 1,014 rows of a real system (`USER:2026-08-12`).
	Tests string
	Code  string
	// The user requirement this row serves, when the ledger carries a UR column.
	UR     string
	Detail string // "" = none (JS null)
	// REQ-CROSS-222: first-class capture of the Priority/Owner/Release
	// columns, and every other column the header names that the extractor
	// does not recognize — carried, never dropped.
	Priority string
	Owner    string
	Release  string
	Extras   []NamedCell
}

// NamedCell is one unrecognized ledger column's header and cell value
// (REQ-CROSS-222 §222.2: carried as a named extra).
type NamedCell struct {
	Name  string
	Value string
}

// Epic is one WORKLIST rollup row.
type Epic struct {
	ID     string
	State  string // proposed | awaiting-approval | done | in-progress | other
	Record string // workspace-relative epic record path ("" = none) — payload identity (origin_ref), never rebased
	// RecordFS is the path that resolves the record from the SYNC root when it
	// differs from Record (manifest pointing one level up — SCN-SY-046).
	// Empty = Record already resolves. Never enters op payloads.
	RecordFS string
	// Specs are the folder epic's specs/*.md files (REQ-CROSS-023), sorted by
	// name. Rel is payload identity (derived from Record's directory, never
	// RecordFS); Content is the file body (capped at op build).
	Specs []EpicSpec
	// Upper/Lower are the WORKLIST row's own "Upper status" / "Lower status"
	// cells (REQ-CROSS-028). 127 of 146 rows carry both, and they were read by
	// nothing — the loop state the app shows comes from here, NOT from
	// epics.status, which is the board column (SERVER-SYNC-DESIGN §1.3).
	// "" = the cell was empty or an em-dash: absence, not a status.
	Upper string
	Lower string
	// REQ-CROSS-223: the PROCESS.md status token leading the WORKLIST
	// Overall-status cell — the epic lifecycle the store must carry.
	// "" = the cell was empty or carried no recognizable token.
	ProcessStatus string
	// URCell is the Epic Rollup's user-requirements cell verbatim. It names
	// the denominator even when the named epic record does not repeat the id;
	// statements never come from this cell.
	URCell string
	// ApprovalCell is the WORKLIST row's "Human approval" cell, verbatim
	// (REQ-CROSS-248): the rollup's recorded acceptance — an APP-* register
	// reference and/or a USER: tag. "" = empty or no row.
	ApprovalCell string
	// TasksCell is the Epic Rollup's Tasks cell verbatim (REQ-CROSS-264). It
	// is harvested BEFORE the range-row drop and the first-wins dedupe: those
	// two run on the row's epic IDENTITY, and a shadowed row's Tasks cell
	// still names tasks that exist. "" = empty or no row.
	TasksCell string
}

// BacklogRow is one BACKLOG.md discovery row or gap-register row
// (REQ-CROSS-223: they gain a store home for the ledger import).
type BacklogRow struct {
	ExternalID string // explicit GAP-* id, else a stable derived BL-… id
	Kind       string // "backlog" | "gap"
	Title      string
	NotesMD    string
	Route      string
	SourcePath string
	Line       int
	// REQ-CROSS-251: the folded facts, typed. Empty = the row states none.
	RaisedAt       string // YYYY-MM-DD from the title's provenance parenthetical
	RaisedBy       string
	Disposition    string // routed | deferred
	DispositionRef string
	CandidateRoute string
	WhyUnrouted    string
	GapKind        string // capability (register) | ledger (### GAP block)
	AffectedIDs    []string
	RawBody        string // the row verbatim — the split is a transform, not a loss
}

// EpicSpec is one epic-folder spec file (EPIC-SYNC-006).
type EpicSpec struct {
	Rel     string
	Name    string
	Content string
}

// RQ is one docs/85 review-queue block.
type RQ struct {
	ID             string
	Title          string
	Heading        string
	Date           string // "" = none
	State          string // closed | delegated | decision-needed | finding
	Line           int
	Body           string
	Pool           []string
	Options        []Option
	Recommendation string // "" = none
	// Optional trace-evaluation identity carried independently from content hash.
	EvaluatedScopeFingerprint string
}

type Option struct {
	Label string
	Text  string
}

// OQ is one process/08 open-questions row.
type OQ struct {
	ID    string
	Title string
	Full  string
	// Suggested is the row's "*Suggested default:*" clause — the author's
	// recommendation, which rides to the gate so the question is answerable
	// without opening the source doc (REQ-PLN-059).
	Suggested string
	Resolved  bool
	Line      int
	Pool      []string
	// Raw is the question's body with its line structure intact. Full collapses
	// whitespace for display; a **Brief:** block is a bullet list and cannot be
	// parsed once its newlines are gone (REQ-CROSS-142).
	Raw string
	// Optional trace-evaluation identity carried independently from content hash.
	EvaluatedScopeFingerprint string
}

// Commit is one recent git-log entry (newest first).
type Commit struct {
	Hash    string
	Date    string
	Subject string
	IDs     []string
}

// Data is the workspace snapshot the op-builder consumes — the Go analogue of
// extract.js buildData(), restricted to the fields ops.js uses.
type Data struct {
	Reqs []Req
	// UserReqs are the file-derived user requirements
	// (requirements/*-USER-REQUIREMENTS.md, SR-SY-1403). Empty in a workspace
	// that has none; the type is optional, never mandated.
	UserReqs []FileUR
	Epics    []Epic
	RQs      []RQ
	OQs      []OQ
	Commits  []Commit
	// REQ-CROSS-084: the system-scoped explanatory documents and the discovery
	// guides. Empty in a workspace that has neither, and an empty set is a
	// no-op on sync rather than a deletion.
	Docs []Document
	// REQ-CROSS-223: BACKLOG.md discovery rows and gap-register rows.
	// Empty in a workspace without the files.
	Backlog []BacklogRow
	// REQ-CROSS-237: gate records from `file-state/GATES.md`, and the path they
	// came from (the op's origin_ref). Empty in a workspace with no gate file.
	Gates    []GateRecord
	GatesRef string
	// REQ-CROSS-264: the WORKLIST rows OUTSIDE the Epic Rollup that declare
	// tasks — the Work Rows and Blocked/Deferred sections, whose TASK-led
	// rows the `worklist-row` loss population assigns to the tasks carrier.
	WorklistTaskRows []WorklistTaskRow
	// ArchivalFiles are the flip-retired carrier files (worklist, review
	// queue, open questions) as workspace-relative paths, resolved through
	// the manifest by Snapshot — a binding that relocates them keeps its
	// carriers. Nil means "not resolved": BuildOps falls back to the
	// default root-relative names.
	ArchivalFiles []string
}

// ---------------------------------------------------------------- helpers

var idPatterns = map[string]*regexp.Regexp{
	"REQ":  regexp.MustCompile(`REQ-[A-Z][A-Z0-9]*-\d+`),
	"EPIC": regexp.MustCompile(`EPIC-[A-Z]+-\d+`),
	"RQ":   regexp.MustCompile(`RQ-\d+[a-z]?`),
	"OQ":   regexp.MustCompile(`OQ-[A-Z0-9x]+\b`),
}

func idMentions(text string, kinds ...string) []string {
	var found []string
	seen := map[string]bool{}
	for _, k := range kinds {
		for _, m := range idPatterns[k].FindAllString(text, -1) {
			if !seen[m] {
				seen[m] = true
				found = append(found, m)
			}
		}
	}
	return found
}

func strip(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(s, "**", ""), "`", ""))
}

// capRunes = JS String.slice(0, n): n counts UTF-16 code units (non-BMP
// runes count 2). A boundary that would split a surrogate pair cuts before
// the pair — the one deviation from JS, which would keep a lone surrogate.
func capRunes(s string, n int) string {
	units := 0
	for i, r := range s {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > n {
			return s[:i]
		}
		units += w
	}
	return s
}

// splitCells splits a markdown table row on unescaped pipes.
//
// A cell may legitimately contain "\|" — one real ledger's REQ-AP-103 title says
// "REDUCES \|difference\| toward zero". Splitting on every pipe shifted the
// columns after it, so prose was read as the work_status and the server
// rejected the batch at op 285, with 284 ops already applied.
func splitCells(line string) []string {
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|') // escaped: literal content, not a delimiter
			i++
		case line[i] == '|':
			parts = append(parts, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	parts = append(parts, strings.TrimSpace(cur.String()))
	return parts
}

// isSeparatorRow reports whether a trimmed table row is a markdown alignment
// separator: every non-empty cell is dashes (with optional alignment colons).
// A row merely *containing* "---" in a cell's text is content.
func isSeparatorRow(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	any := false
	for _, cell := range splitCells(trimmed) {
		if cell == "" {
			continue
		}
		body := strings.Trim(cell, ":")
		if body == "" || strings.Trim(body, "-") != "" || len(body) < 2 {
			return false
		}
		any = true
	}
	return any
}

func cell(cells []string, i int) string {
	if i < len(cells) {
		return cells[i]
	}
	return ""
}

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

// ---------------------------------------------------------------- rdd-backlog-v1 / rdd-gap-register-v1

// backlogIDRe pulls an explicit id from a row's first cell; rows without one
// get a stable derived id from the kind and title.
var backlogIDRe = regexp.MustCompile(`^(GAP|DEF|BL)-[A-Za-z0-9-]*[A-Za-z0-9]`)

// backlogNarrativeRe matches the level-3 block a register writes UNDER its
// table to explain one row: the heading leads with that row's explicit id.
// Anchored on the id, not on "any level-3 heading", because the backlog's own
// level-3 headings are routing-log dates — folding one of those into a
// discovery row would file a triage pass as a record's body.
var backlogNarrativeRe = regexp.MustCompile(`^###\s+((?:GAP|DEF|BL)-[A-Za-z0-9-]*[A-Za-z0-9])\b`)

// backlogNarrativeSeparator joins a row's cells to the narrative that expands
// them. The register's own thematic break, so the joined notes read the way the
// file reads — and so the seam is findable rather than inferred from blank
// lines that the prose itself contains.
const backlogNarrativeSeparator = "\n\n---\n\n"

// backlogNarratives collects the register's per-record narrative bodies, keyed
// by the id in the heading. A body runs to the next level-2 or level-3 heading,
// exactly like a ledger detail block, so a `## Closed Gaps` divider ends the
// block above it rather than swallowing the section.
func backlogNarratives(lines []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(lines); i++ {
		m := backlogNarrativeRe.FindStringSubmatch(lines[i])
		if m == nil {
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
		// First block wins, like every other first-wins identity here: a second
		// block for one id is a corpus duplicate, and silently concatenating
		// them would make the record's text depend on file order.
		if _, seen := out[m[1]]; !seen {
			out[m[1]] = strings.TrimSpace(strings.Join(body, "\n"))
		}
		i = j - 1
	}
	return out
}

// ParseLedgerGaps reads the `### GAP-<CTX>-<n>` blocks a requirements ledger
// files beside its requirements (REQ-CROSS-223).
//
// A ledger gap is a backlog record, not a requirement: it has no lifecycle, no
// evidence and no trace, and the row-shaped backlog parser has no column for
// the account the block actually contains. So the whole body rides notes_md
// verbatim and the explicit id is preserved rather than derived — the ledgers,
// the register and the requirements that cite these ids must keep agreeing
// about them after the flip.
//
// The scan is deliberately parallel to ParseLedger's rather than folded into
// it: a requirement block that MENTIONS a gap keeps that mention in its own
// detail, and a level-4 heading stays a subsection of the block it sits in.
func ParseLedgerGaps(sourcePath, content string) []BacklogRow {
	var rows []BacklogRow
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		m := ledgerGapDetailRe.FindStringSubmatch(lines[i])
		if m == nil {
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
		rows = append(rows, BacklogRow{
			ExternalID: m[1],
			Kind:       "gap",
			// REQ-CROSS-251: a ledger gap is a different gap kind from the
			// register's capability rows — the unbundled column says which
			GapKind:    "ledger",
			Title:      strip(strings.TrimPrefix(strings.TrimSpace(lines[i]), "###")),
			NotesMD:    strings.TrimSpace(strings.Join(body, "\n")),
			SourcePath: sourcePath,
			Line:       i + 1,
			RawBody:    strings.TrimSpace(lines[i]),
		})
		i = j - 1
	}
	return rows
}

// ParseBacklogRows parses BACKLOG.md discovery rows or gap-register rows
// (REQ-CROSS-223). Table-shaped: the first data row after the header and
// separator of each table. Title is the stripped first cell; notes and route
// ride the following cells verbatim.
//
// A register that also writes a narrative block for a row — `### GAP-003` and
// the paragraphs under it — folds that block into the SAME record's notes. One
// id is one record, so a second op would fight the first for the store's shadow
// hash; and a row that synced as its one-line description alone would leave the
// account of the gap to retire with the file.
func ParseBacklogRows(sourcePath, content, kind string) []BacklogRow {
	var rows []BacklogRow
	lines := strings.Split(content, "\n")
	// Narratives are collected before the rows so a block may sit anywhere in
	// the file relative to the table it explains. A block whose id matches no
	// row enriches nothing and mints nothing — the register's closed-gaps
	// template is exactly that shape.
	narratives := backlogNarratives(lines)
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "|") {
			continue
		}
		// Coverage validation: a separator is a row whose
		// every cell is dashes — cell text quoting "---" (a git-diff
		// marker) is content. A header is only a first table row directly
		// followed by a separator; a blank line splitting a table leaves a
		// continuation with data in its first row, not a phantom header.
		if isSeparatorRow(t) {
			continue
		}
		prevPiped := i > 0 && strings.HasPrefix(strings.TrimSpace(lines[i-1]), "|")
		nextSep := i+1 < len(lines) && isSeparatorRow(strings.TrimSpace(lines[i+1]))
		if !prevPiped && nextSep { // header row
			continue
		}
		cells := splitCells(line)
		var fields []string
		for _, c := range cells {
			if strings.TrimSpace(c) != "" || len(fields) > 0 {
				fields = append(fields, c)
			}
		}
		if len(fields) == 0 || strings.TrimSpace(fields[0]) == "" {
			continue
		}
		title := strip(fields[0])
		id := backlogIDRe.FindString(title)
		if id == "" {
			sum := sha256.Sum256([]byte(kind + "|" + title))
			id = "BL-" + hex.EncodeToString(sum[:])[:12]
		}
		row := BacklogRow{
			ExternalID: id, Kind: kind, Title: title,
			SourcePath: sourcePath, Line: i + 1,
		}
		if len(fields) > 1 {
			row.NotesMD = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			row.Route = strings.TrimSpace(fields[2])
		}
		// Mapping audit: wider tables (the gap register is
		// five columns) folded to three silently — every further cell folds
		// into the notes, named nothing, dropped never.
		for _, extra := range fields[min(3, len(fields)):] {
			if v := strings.TrimSpace(extra); v != "" {
				if row.NotesMD != "" {
					row.NotesMD += " · "
				}
				row.NotesMD += v
			}
		}
		if body := narratives[id]; body != "" {
			if row.NotesMD != "" {
				row.NotesMD += backlogNarrativeSeparator
			}
			row.NotesMD += body
		}
		// REQ-CROSS-251: the folded facts land typed. The row rides verbatim
		// as RawBody first, so the split is a transform, never a loss; the id
		// above derives from the full original title, keeping identities
		// stable against this enrichment.
		row.RawBody = strings.TrimSpace(line)
		if m := backlogProvRe.FindStringSubmatch(row.Title); m != nil {
			row.Title = strings.TrimSpace(strings.TrimSuffix(row.Title, m[0]))
			if d := dateOnlyRe.FindString(m[1]); d != "" {
				row.RaisedAt = d
			}
			if ci := strings.Index(m[1], ","); ci >= 0 {
				row.RaisedBy = strip(m[1][ci+1:])
			}
		}
		switch kind {
		case "backlog":
			if ref := firstIDMention(row.Route); ref != "" {
				row.Disposition = "routed"
				row.DispositionRef = ref
			} else if v := dashless(strip(row.Route)); v != "" {
				row.CandidateRoute = v
			}
			row.AffectedIDs = idMentions(row.NotesMD, "REQ", "EPIC")
		case "gap":
			row.GapKind = "capability"
			if v := dashless(strip(row.Route)); v != "" {
				row.CandidateRoute = v
			}
			if len(fields) > 3 {
				row.WhyUnrouted = strings.TrimSpace(fields[3])
			}
			if len(fields) > 4 {
				if ref := firstIDMention(fields[4]); ref != "" {
					row.Disposition = "deferred"
					row.DispositionRef = ref
				}
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// firstIDMention is the first requirement or epic id a cell names, or "".
func firstIDMention(text string) string {
	if ids := idMentions(text, "REQ", "EPIC"); len(ids) > 0 {
		return ids[0]
	}
	return ""
}

var (
	// a trailing title parenthetical carrying provenance: a RUN:/USER: tag,
	// optionally followed by who raised it
	backlogProvRe = regexp.MustCompile(`\(\s*((?:RUN|USER):[^)]*)\)\s*$`)
	dateOnlyRe    = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
)

// ---------------------------------------------------------------- rdd-worklist-v1

var (
	worklistRowRe  = regexp.MustCompile(`^\|\s*(EPIC-[A-Z][A-Z0-9]*-[\d.]+[^|]*)\|`)
	epicRecordRe   = regexp.MustCompile(`\((epics/[^)]+\.md)\)`)
	epicBaseRe     = regexp.MustCompile(`^EPIC-[A-Z][A-Z0-9]*-\d+`)
	awaitingRe     = regexp.MustCompile(`(?i)awaiting approval`)
	pendingLeadRe  = regexp.MustCompile(`(?i)^pending`)
	proposedRe     = regexp.MustCompile(`(?i)PROPOSED`)
	doneRe         = regexp.MustCompile(`(?i)DONE`)
	inProgressRe   = regexp.MustCompile(`(?i)IN_PROGRESS`)
	bracketStripRe = regexp.MustCompile(`\[|\]`)
	// REQ-CROSS-223: the PROCESS.md status vocabulary, matched as a whole
	// token in the Overall-status cell.
	processStatusRe = regexp.MustCompile(`\b(IN_PROGRESS|IN_REVIEW|PROPOSED|TODO|DONE|BLOCKED|DEFERRED|OBSOLETE)\b`)
)

// loopStatusCap bounds a loop-status cell. It mirrors the node extractor's own
// slice on these cells, so the two builders must agree exactly. Named here so
// the fidelity report measures the cap the parser applies rather than a copy of
// the number.
//
// The column holds a sentence, not a token: nine cells in the tracked corpus run
// past a hundred units and the longest is 315. At 120 every one of them was cut
// mid-sentence, and the head that survived still read as a complete status —
// "LOWER_VERIFIED at the delivered revision" without the clause that qualified
// it. Silent, because the epic count reconciled and the payload carried a value.
//
// 2000 is chosen from that measurement: ~6x the longest cell the corpus writes,
// so ordinary growth cannot quietly re-truncate, and an order of magnitude below
// the 20000 the wire contract already declares for a backlog record's prose. It
// is still a bound — a cell past it is cut, and the fidelity arm reports the cut
// — because an unbounded cell in a rollup column is a defect worth seeing.
//
// The width is declared with the carrier, not only here: the wire contract
// states it as the field's maxLength in three byte-identical copies, and the
// server column the cells land in is text rather than varchar(255) — at 255 a
// raised cap would have turned a silent cut into a rejected batch.
const loopStatusCap = 2000

// statusCell normalizes a loop-status cell: a placeholder ("", "—", "-") is
// absence and syncs as nothing at all, so those epics' hashes never move.
func statusCell(raw string) string {
	v := strip(raw)
	switch v {
	case "", "—", "-", "–":
		return ""
	}
	return capRunes(v, loopStatusCap)
}

// epicIDFromPath reads EPIC-XXX-NNN out of "epics/EPIC-XXX-NNN/EPIC.md" or
// "epics/EPIC-XXX-NNN.md" — the two record conventions the process allows.
var epicIDRe = regexp.MustCompile(`^(EPIC-[A-Z0-9]+-\d+)`)

func epicIDFromPath(p string) string {
	base := filepath.Base(p)
	if strings.EqualFold(base, "EPIC.md") {
		base = filepath.Base(filepath.Dir(p))
	}
	base = strings.TrimSuffix(base, ".md")
	if m := epicIDRe.FindStringSubmatch(base); m != nil {
		return m[1]
	}
	return ""
}

// ParseWorklist parses WORKLIST.md rollup rows (rdd-worklist-v1). epicFiles is
// the directory listing of the epics/ dir (record prefix-matching), with
// isDir saying which entries are directories (dir records resolve /EPIC.md).
func ParseWorklist(content string, epicFiles []string, isDir map[string]bool) []Epic {
	var epics []Epic
	seen := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		if !worklistRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 11 {
			continue
		}
		id := strings.SplitN(bracketStripRe.ReplaceAllString(cell(cells, 1), ""), " ", 2)[0]
		// REQ-CROSS-264: the Tasks cell is read at index 6, BEFORE the two
		// drops below. Both act on the row's epic IDENTITY — a range row is no
		// epic, a duplicate row's epic is already present — and neither says
		// anything about the tasks the dropped row names. Those rows ride
		// ParseWorklistTaskRows instead, which drops nothing.
		// Mapping audit: a range rollup row (EPIC-FE-059..064)
		// is an aggregate over members, never an epic entity — and its
		// lifecycle token belongs to them, not to a phantom.
		if strings.Contains(id, "..") {
			continue
		}
		// First occurrence wins, like ledger rows: a duplicate rollup row —
		// a decorated variant or a genuine id collision — must not emit a
		// second op that ping-pongs the store's shadow hash forever
		// (observed live). The fidelity report flags it.
		//
		// The dedup stays here, and refusing stays elsewhere: this extractor
		// also feeds routine `sync` and the fire-and-forget hooks, where a
		// mid-edit duplicate must flag and keep going rather than stop a
		// corpus author's session. Only the one-time import — where the
		// shadowed record would become an authority gap nobody can see —
		// refuses to write, on the fidelity report's blocking set.
		if seen[id] {
			continue
		}
		seen[id] = true
		overall := strip(cell(cells, 9))
		approvalRaw := cell(cells, 10)
		approval := strip(approvalRaw)
		upper := statusCell(cell(cells, 7))
		lower := statusCell(cell(cells, 8))

		state := "other"
		switch {
		case proposedRe.MatchString(overall):
			state = "proposed" // spec drafted, not built — never at the approval gate
		case awaitingRe.MatchString(overall) || pendingLeadRe.MatchString(approval):
			state = "awaiting-approval"
		case doneRe.MatchString(overall):
			state = "done"
		case inProgressRe.MatchString(overall):
			state = "in-progress"
		}
		// REQ-CROSS-223: the exact PROCESS.md token, beside the lossy
		// five-bucket state — the store lifecycle column's source.
		processStatus := processStatusRe.FindString(overall)

		record := ""
		if m := epicRecordRe.FindStringSubmatch(line); m != nil {
			record = m[1]
		} else if base := epicBaseRe.FindString(id); base != "" {
			for _, f := range epicFiles {
				if strings.HasPrefix(f, base) {
					record = "epics/" + f
					if isDir[f] {
						record += "/EPIC.md"
					}
					break
				}
			}
		}
		epics = append(epics, Epic{ID: id, State: state, Record: record, Upper: upper, Lower: lower, ProcessStatus: processStatus, URCell: strings.TrimSpace(cell(cells, 3)), ApprovalCell: strings.TrimSpace(approvalRaw), TasksCell: strings.TrimSpace(cell(cells, 6))})
	}

	// Epics the worklist did not describe. This workspace's worklist carries
	// eleven columns and one row per epic; the reverse-engineering skill writes
	// epic FOLDERS and a compact worklist that names them in a link. Sixteen
	// epics on a real system were discovered as zero, so the product's own
	// description never reached the platform (`RUN:2026-08-12`).
	//
	// The record is the truth: an epic that exists on disk is an epic, whatever
	// the worklist's shape. Loop status stays empty — only a worklist row can
	// state it, and inventing one would be worse than leaving it unknown.
	for _, f := range epicFiles {
		id := epicIDFromPath(f)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		// Same record shape the worklist branch builds above: epicFiles carries
		// base names, not paths, and a record read from a bare name resolves to
		// nothing — which produced sixteen epics with no title and no
		// description (`RUN:2026-08-12`).
		record := "epics/" + f
		if isDir[f] {
			record += "/EPIC.md"
		}
		epics = append(epics, Epic{ID: id, Record: record, RecordFS: record, State: "other"})
	}

	return epics
}

// ---------------------------------------------------------------- rdd-review-queue-v1

var (
	// §245.5: docs/85 writes items at both heading depths — 13 as H2. The
	// head case is matched before the H2 terminator, so an `## RQ-` line
	// starts a block instead of ending one.
	rqHeadRe  = regexp.MustCompile(`^##+\s+RQ-`)
	rqIDRe    = regexp.MustCompile(`RQ-\d+[a-z]?`)
	h2NotH3Re = regexp.MustCompile(`^##[^#]`)
	// §245.7: DONE / FIXED / ANSWERED close a block too — word-bounded, so
	// UNANSWERED stays a finding.
	rqClosedRe    = regexp.MustCompile(`(?i)APPROVED|RESOLVED|✅|\b(?:DONE|FIXED|ANSWERED)\b`)
	rqDelegatedRe = regexp.MustCompile(`(?i)DELEGATED`)
	rqDecisionRe  = regexp.MustCompile(`(?i)DECISION NEEDED`)
	dateRe        = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	rqTitleIDRe   = regexp.MustCompile(`^RQ-\d+[a-z]?\s*`)
	rqTitleParRe  = regexp.MustCompile(`^\([^)]*\)\s*`)
	rqTitleDashRe = regexp.MustCompile(`^[—–-]\s*`)
	rqTitleDecRe  = regexp.MustCompile(`(?i)\s*—\s*DECISION NEEDED.*$`)
	recommendRe   = regexp.MustCompile(`(?i)[^.\n]*recommend[^.\n]*\.`)
	optionMarkRe  = regexp.MustCompile(`\*{0,2}\((i{1,3}|iv|v|[a-z]|\d{1,2})\)\*{0,2}\s+`)
	headPrefixRe  = regexp.MustCompile(`^##+\s*`)
)

type rqBlock struct {
	id      string
	heading string
	lineNo  int
	body    string
}

// ParseReviewQueue parses docs/85-loop-review-queue.md (rdd-review-queue-v1).
func ParseReviewQueue(content string) []RQ {
	lines := strings.Split(content, "\n")
	var blocks []rqBlock
	var cur *rqBlock
	for i, line := range lines {
		switch {
		case rqHeadRe.MatchString(line):
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			id := rqIDRe.FindString(line)
			if id == "" {
				id = "RQ-?"
			}
			cur = &rqBlock{id: id, heading: headPrefixRe.ReplaceAllString(line, ""), lineNo: i + 1}
		case h2NotH3Re.MatchString(line):
			if cur != nil {
				blocks = append(blocks, *cur)
				cur = nil
			}
		default:
			if cur != nil {
				cur.body += line + "\n"
			}
		}
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}

	byID := map[string]*RQ{}
	var order []string
	for _, b := range blocks {
		state := "finding"
		switch {
		case rqClosedRe.MatchString(b.heading):
			state = "closed"
		case rqDelegatedRe.MatchString(b.heading):
			state = "delegated"
		case rqDecisionRe.MatchString(b.heading):
			state = "decision-needed"
		}
		entry := RQ{
			ID:             b.id,
			Title:          cleanTitle(b.heading),
			Heading:        capRunes(strip(b.heading), 240),
			Date:           dateRe.FindString(b.heading),
			State:          state,
			Line:           b.lineNo,
			Body:           capRunes(strings.TrimSpace(b.body), bodyCap),
			Pool:           idMentions(b.body, "REQ", "EPIC"),
			Options:        parseOptions(b.body),
			Recommendation: parseRecommendation(b.body),
		}
		prev, dup := byID[b.id]
		if !dup {
			e := entry
			byID[b.id] = &e
			order = append(order, b.id)
			continue
		}
		// duplicate id: the closed block wins; pools union; bodies join
		winner := prev
		if prev.State != "closed" && entry.State == "closed" {
			e := entry
			winner = &e
		}
		pool := prev.Pool
		seen := map[string]bool{}
		for _, p := range pool {
			seen[p] = true
		}
		for _, p := range entry.Pool {
			if !seen[p] {
				pool = append(pool, p)
			}
		}
		winner.Pool = pool
		winner.Body = capRunes(
			"### "+prev.Heading+"\n\n"+prev.Body+"\n\n---\n\n### "+entry.Heading+"\n\n"+entry.Body,
			bodyCap,
		)
		byID[b.id] = winner
	}

	out := make([]RQ, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out
}

func cleanTitle(head string) string {
	t := rqTitleIDRe.ReplaceAllString(head, "")
	t = rqTitleParRe.ReplaceAllString(t, "")
	t = rqTitleDashRe.ReplaceAllString(t, "")
	t = rqTitleDecRe.ReplaceAllString(t, "")
	return capRunes(strip(t), 160)
}

func parseOptions(body string) []Option {
	text := strings.ReplaceAll(body, "\n", " ")
	marks := optionMarkRe.FindAllStringSubmatchIndex(text, -1)
	var opts []Option
	seen := map[string]bool{}
	for i, m := range marks {
		end := m[1]
		var chunk string
		if i+1 < len(marks) {
			chunk = text[end:marks[i+1][0]]
		} else {
			stop := end + 240
			if stop > len(text) {
				stop = len(text)
			}
			chunk = text[end:stop]
		}
		cut := strings.TrimSpace(splitAfterSentence(chunk))
		label := "(" + text[m[2]:m[3]] + ")"
		if len([]rune(cut)) >= 8 && !seen[label] {
			seen[label] = true
			opts = append(opts, Option{Label: label, Text: capRunes(strip(cut), 220)})
		}
	}
	if len(opts) >= 2 && len(opts) <= 10 {
		return opts
	}
	return nil
}

// splitAfterSentence = chunk.split(/(?<=[.;?])\s/)[0] — RE2 has no lookbehind,
// so scan for the first [.;?] immediately followed by whitespace.
func splitAfterSentence(chunk string) string {
	for i := 0; i < len(chunk)-1; i++ {
		c := chunk[i]
		if (c == '.' || c == ';' || c == '?') && isSpaceByte(chunk[i+1]) {
			return chunk[:i+1]
		}
	}
	return chunk
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\f' || b == '\v'
}

func parseRecommendation(body string) string {
	m := recommendRe.FindString(body)
	if m == "" {
		return ""
	}
	return capRunes(strings.TrimSpace(strip(m)), 300)
}

// ---------------------------------------------------------------- rdd-open-questions-v1

var (
	oqRowRe  = regexp.MustCompile("^\\|\\s*~{0,2}((?:OQ|GAP)-[\\w.-]+?)~{0,2}\\s*\\|")
	oqDoneRe = regexp.MustCompile(`(?i)done`)
	// REQ-CROSS-145: the marker counts at an EDGE of the title, not inside it —
	// a prefix ("RESOLVED: …"), a trailing marker ("… — RESOLVED", "… ✅") or a
	// struck-through id. Matching anywhere meant a question whose subject is
	// done-ness marked itself answered and never reached the queue.
	// A TABLE ROW carries its verdict in a cell, so the marker may sit anywhere
	// on the line — layouts differ on which column holds it.
	oqResolvedRe = regexp.MustCompile(`(?i)✅|~~\s*OQ-|\bRESOLVED\b|\bDONE\b`)
	// REQ-CROSS-145: a HEADING is a sentence, and the marker only counts at an
	// edge of it — a prefix ("… — RESOLVED: why"), a trailing marker ("… — DONE",
	// "… ✅") or a struck-through id. Matching anywhere meant a question whose
	// subject is done-ness marked itself answered and never reached the queue.
	oqResolvedTitleRe = regexp.MustCompile(`(?i)~~\s*OQ-|^\s*(RESOLVED|DONE)\b|[—\-–:]\s*(RESOLVED|DONE)\b|✅\s*$`)
	sentenceRe        = regexp.MustCompile(`^([^.?]*[.?])`)
	// The clause runs from the label to the end of its cell. Which column
	// carries it differs by layout, so the row is scanned rather than indexed.
	oqSuggestedRe = regexp.MustCompile(`(?i)\*{1,2}\s*suggested default\s*:?\s*\*{1,2}\s*(.+)$`)
)

// oqSuggested returns the row's suggested-default clause, without its label.
func oqSuggested(cells []string) string {
	for _, c := range cells {
		if m := oqSuggestedRe.FindStringSubmatch(strings.TrimSpace(c)); m != nil {
			return strip(strings.TrimSpace(m[1]))
		}
	}
	return ""
}

// oqQuestionCell picks the prose cell holding the question. Two layouts are in
// use — question in column 2 (ours) and in column 3 after a source-doc column
// (a client's) — so the longest cell wins rather than a fixed index. Reading a
// fixed column against the other layout publishes source-doc references as the
// questions, which is worse than not parsing at all.
func oqQuestionCell(cells, header []string) string {
	// A named column beats a guess. Real tables carry a "Suggested default"
	// column that is routinely LONGER than the question it answers, so the
	// longest cell is only a fallback for headerless tables.
	for i, h := range header {
		if i < len(cells) && strings.EqualFold(strings.TrimSpace(h), "question") {
			return cells[i]
		}
	}
	best := ""
	for i, c := range cells {
		if i == 0 || i == 1 {
			continue // leading empty + the id
		}
		if len(c) > len(best) {
			best = c
		}
	}
	return best
}

// oqHeadingRe matches "## Q-001 — text" / "### OQ-12 — text" (the skill's shape).
var oqHeadingRe = regexp.MustCompile(`(?m)^#{2,3}\s+((?:OQ|Q)-[A-Za-z0-9-]+)\s*[—–-]\s*(.+)$`)

// parseHeadingQuestions reads the section-per-question shape. The body up to the
// next heading is the full text; the heading itself is the title.
func parseHeadingQuestions(content string) []OQ {
	ms := oqHeadingRe.FindAllStringSubmatchIndex(content, -1)
	var oqs []OQ
	for i, m := range ms {
		id := content[m[2]:m[3]]
		title := strings.TrimSpace(content[m[4]:m[5]])
		end := len(content)
		if i+1 < len(ms) {
			end = ms[i+1][0]
		}
		body := strings.TrimSpace(content[m[1]:end])
		oqs = append(oqs, OQ{
			ID:       id,
			Title:    capRunes(title, 140),
			Full:     capRunes(strings.Join(strings.Fields(title+" "+body), " "), 1200),
			Raw:      capRunes(body, 4000),
			Resolved: oqResolvedTitleRe.MatchString(title),
			Line:     strings.Count(content[:m[0]], "\n") + 1,
			// The requirements this question blocks — the same holds the table
			// form emits. Without them the gate arrives with nothing attached,
			// so a reader cannot see what is waiting on the answer.
			Pool: idMentions(title+" "+body, "REQ", "EPIC"),
		})
	}
	return oqs
}

// ParseOpenQuestions parses process/08-open-questions.md (rdd-open-questions-v1).
func ParseOpenQuestions(content string) []OQ {
	// Heading form first: the reverse-engineering skill writes questions as
	// "## Q-001 — <question>" sections with prose beneath, not table rows. Sixty
	// of them on a real system reached the human queue as zero, while the queue
	// told the reader it was clear (`RUN:2026-08-12`).
	// Both shapes can occur in one file — a heading-form question appended to a
	// workspace whose questions are table rows. Returning the first non-empty
	// set kept 1 and discarded 140 (`RUN:2026-08-12`), so they are MERGED, with
	// ids seen in headings not re-read from the table.
	oqs := parseHeadingQuestions(content)
	seen := map[string]bool{}
	for _, q := range oqs {
		seen[q.ID] = true
	}

	var header []string
	for i, line := range strings.Split(content, "\n") {
		if !oqRowRe.MatchString(line) {
			// remember the most recent table header, so the question column can
			// be found by name rather than guessed at
			if strings.HasPrefix(strings.TrimSpace(line), "|") && strings.Contains(strings.ToLower(line), "question") {
				header = splitCells(line)
			}
			continue
		}
		cells := splitCells(line)
		q := strip(oqQuestionCell(cells, header))
		title := q
		if m := sentenceRe.FindStringSubmatch(q); m != nil {
			title = m[1]
		}
		rowID := oqRowRe.FindStringSubmatch(line)[1]
		if seen[rowID] {
			continue
		}
		oqs = append(oqs, OQ{
			ID:        rowID,
			Title:     capRunes(title, 140),
			Full:      q,
			Suggested: oqSuggested(cells),
			// Scan the whole row: layouts differ on which column carries the
			// verdict, and a struck-through id is itself a resolution marker.
			Resolved: oqResolvedRe.MatchString(line),
			Line:     i + 1,
			Pool:     idMentions(line, "REQ", "EPIC"),
		})
	}
	return oqs
}

// ---------------------------------------------------------------- git activity

// ParseGitLog returns the last 14 days of commits (newest first, capped 400 —
// the extractor's data.commits shape the burst op folds).
func ParseGitLog(root string) []Commit {
	cmd := exec.Command("git", "log", "--since=14 days ago", "--date=iso-strict", "--pretty=format:%h|%ad|%s")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var commits []Commit
	for _, l := range strings.Split(string(out), "\n") {
		if l == "" {
			continue
		}
		parts := strings.SplitN(l, "|", 3)
		if len(parts) < 3 {
			continue
		}
		subject := capRunes(parts[2], 160)
		commits = append(commits, Commit{
			Hash:    parts[0],
			Date:    parts[1],
			Subject: subject,
			IDs:     idMentions(subject, "REQ", "EPIC", "RQ", "OQ"),
		})
		if len(commits) == 400 {
			break
		}
	}
	return commits
}

// ---------------------------------------------------------------- epic records

// ReadEpicRecord reads an epic's record file relative to root ("" if absent).
func ReadEpicRecord(root, rel string) string {
	if rel == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		return ""
	}
	return string(raw)
}

// LedgerContext exposes the ledger-context test so callers can tell a bad path
// from a readable path whose rows simply did not match.
func LedgerContext(path string) string { return ledgerContext(path) }

// firstUnparsedRowHint quotes the first table row that looks like a requirement
// but is not one, so the warning names the actual offender instead of leaving
// the reader to diff a regex against a file.
func firstUnparsedRowHint(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || ledgerRowRe.MatchString(line) {
			continue
		}
		if !unparsedRowRe.MatchString(line) {
			continue
		}
		id := strings.TrimSpace(unparsedRowRe.FindStringSubmatch(line)[1])
		return fmt.Sprintf(" (first offending id: %q)", id)
	}
	return ""
}

var unparsedRowRe = regexp.MustCompile(`^\|\s*([A-Z][A-Z0-9]*-[A-Z0-9]+-\d+)\s*\|`)

// RequirementKind is the payload kind a ledger id declares. `UR-` is a user
// requirement; `SR-` and the historical `REQ-` are system requirements, so
// every ledger written before the prefix existed keeps its current kind.
func RequirementKind(id string) string {
	if strings.HasPrefix(id, "UR-") {
		return "user"
	}
	return "system"
}

// REQ-CROSS-286: the fourth parent shape — the one `rdd-reverse-engineer`
// itself writes. That pass records proposed relations as prose inside the
// Candidate packet:
//
//	Proposed relations (CANDIDATE): requires SR-KERNEL-030, SR-KERNEL-031.
//	Proposed relations (CANDIDATE): serves UR-KERNEL-002.
//
// Nothing read it, so an entire derived corpus synced as orphans — 126 orphan
// URs and 778 orphan SRs on the estate that found this (`RUN:2026-08-27`).
// That is the third corpus fully orphaned by the same gap between what a pass
// writes and what the reader parses (REQ-CROSS-076, SR-SY-1402 were the first
// two), which is why `TestSkillRelationShapesAllHaveReaders` now pins the set.
//
// Deliberately narrow, in the spirit of the earlier two: only ids that follow
// the verb, and only inside the packet clause. Prose that merely mentions a
// requirement is a citation, not a relation.
var (
	packetClauseRe = regexp.MustCompile(`Proposed relations \(CANDIDATE\):\s*([^.\n]*)`)
	packetServesRe = regexp.MustCompile(`\bserves\s+((?:(?:UR|SR)-[A-Z0-9]+-\d+)(?:\s*[,·]\s*(?:UR|SR)-[A-Z0-9]+-\d+)*)`)
	packetReqRe    = regexp.MustCompile(`\brequires\s+((?:(?:UR|SR)-[A-Z0-9]+-\d+)(?:\s*[,·…]\s*(?:UR|SR)-[A-Z0-9]+-\d+)*)`)
	packetIDRe     = regexp.MustCompile(`(?:UR|SR)-[A-Z0-9]+-\d+`)
)

// PacketRelations reads a detail block's Candidate packet and returns the ids
// it declares: `serves` names this row's parent, `requires` names rows that
// take this row as theirs. Either may be empty.
func PacketRelations(detail string) (serves []string, requires []string) {
	clause := packetClauseRe.FindStringSubmatch(detail)
	if clause == nil {
		return nil, nil
	}
	if m := packetServesRe.FindStringSubmatch(clause[1]); m != nil {
		serves = packetIDRe.FindAllString(m[1], -1)
	}
	if m := packetReqRe.FindStringSubmatch(clause[1]); m != nil {
		requires = packetIDRe.FindAllString(m[1], -1)
	}
	return serves, requires
}

// ApplyPacketRelations resolves Candidate-packet relations across the whole
// corpus — relations cross ledgers, so this cannot run per file. It returns how
// many parents it filled and which named ids matched no row.
//
// It only ever fills an EMPTY parent. A row that already names one through a
// column, a Source cell, or a `**UR:**` bullet keeps it: those are declarations
// the author made in a dedicated field, and a packet sentence must not override
// them.
// `parent_external_id` is a system requirement's USER requirement, so only one
// shape is representable: an SR child under a UR parent. A packet naming a UR
// as the child, or an SR as the parent (an SR→SR dependency — a real
// declaration the payload has no field for), is counted and reported rather
// than assigned and silently dropped at payload build. Both occurred on the
// estate that found this: 6 SR→SR edges vanished between a reported 294 links
// and 288 in the batch (`RUN:2026-08-27`). AGENTS.md — a guard says what it
// drops; silent truncation reads as "covered everything".
func ApplyPacketRelations(reqs []Req) (filled int, unresolved []string, unrepresentable []string) {
	byID := make(map[string]int, len(reqs))
	for i, r := range reqs {
		byID[r.ID] = i
	}

	seenMissing := map[string]bool{}
	note := func(id string) {
		if !seenMissing[id] {
			seenMissing[id] = true
			unresolved = append(unresolved, id)
		}
	}

	// `serves` first: a row naming its own parent is the more direct statement,
	// so it wins any race with a `requires` pointed at the same row.
	for i := range reqs {
		serves, _ := PacketRelations(reqs[i].Detail)
		for _, parent := range serves {
			if _, ok := byID[parent]; !ok {
				note(parent)
				continue
			}
			if reqs[i].UR != "" {
				break
			}
			if !representableEdge(reqs[i].ID, parent) {
				unrepresentable = append(unrepresentable, reqs[i].ID+"→"+parent)
				break
			}
			reqs[i].UR = parent
			filled++
			break
		}
	}

	for i := range reqs {
		_, requires := PacketRelations(reqs[i].Detail)
		for _, child := range requires {
			j, ok := byID[child]
			if !ok {
				note(child)
				continue
			}
			if reqs[j].UR != "" {
				continue
			}
			if !representableEdge(reqs[j].ID, reqs[i].ID) {
				unrepresentable = append(unrepresentable, reqs[j].ID+"→"+reqs[i].ID)
				continue
			}
			reqs[j].UR = reqs[i].ID
			filled++
		}
	}

	sort.Strings(unresolved)
	sort.Strings(unrepresentable)
	return filled, unresolved, unrepresentable
}

// representableEdge reports whether `child -> parent` survives to the payload:
// the child must not be a user requirement, and the parent must be one.
func representableEdge(child, parent string) bool {
	return !strings.HasPrefix(child, "UR-") && strings.HasPrefix(parent, "UR-")
}
