package rdd

// REQ-CROSS-264 (EPIC-CLI-003 tranche 4): the tasks carrier. Every task the
// corpus declares imports as a `tasks` row through one `upsert_task` op.
//
// Recognition is declaration-SHAPE-led, not heading-led. A heading rule closes
// nothing: only 275 of the 375 declaring table rows sit under a `## Task…`
// heading — the rest live under 35 other heading forms — and 79 declarations
// are bullets rather than rows. So a table row whose first cell is led by a
// task id, or a bullet whose lead (bold or plain) is one, declares a task
// wherever it appears. The single negative family is the
// evidence/coverage/traceability heading family: its task-led rows are
// evidence ABOUT tasks, and 15 ids appear only there.
//
// Identity follows the scenario precedent. The op-level `external_id` is the
// epic-scoped composite `<epic-code>#<local-id>`; the applier maps it to the
// row identity `(tenant, epic, code)` and the read surface serves it back.
// Bare-code identity would have been 26 duplicate `(system, external_id)`
// keys — 14 cross-epic `TASK-*` reuses plus 12 ordinal keys — and the run's
// duplicate check refuses a batch carrying one.

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var (
	// A task id: the global `TASK-<AREA>-<n>` family.
	taskIDRe = regexp.MustCompile(`^TASK-[A-Z][A-Za-z0-9]*-\d+[a-z]?$`)
	// An epic-local ordinal. Exact by design: `T1a` and `TX1` are not
	// ordinals, and admitting them would mint identities the corpus never
	// declared.
	taskOrdinalRe = regexp.MustCompile(`^T\d+$`)

	// The one negative heading family (D-T4-1). Deliberately NOT
	// excludedSectionHeading: a deferred or out-of-scope section still
	// DECLARES the tasks it names, and dropping them would lose real rows.
	taskEvidenceHeadRe = regexp.MustCompile(`(?i)^#+\s+.*(evidence|coverage|traceab)`)

	// Range, list and bare forms, mirroring the membership token rules.
	taskRangeRe   = regexp.MustCompile(`\b(TASK-[A-Z][A-Za-z0-9]*-)(\d+)\.\.(\d+)\b`)
	taskListRe    = regexp.MustCompile(`\b(TASK-[A-Z][A-Za-z0-9]*-\d+)((?:/\d+)+)\b`)
	taskTokenRe   = regexp.MustCompile(`\bTASK-[A-Z][A-Za-z0-9]*-\d+[a-z]?\b`)
	ordRangeRe    = regexp.MustCompile(`\bT(\d+)\.\.T?(\d+)\b`)
	ordTokenRe    = regexp.MustCompile(`\bT\d+\b`)
	taskLeadTrim  = regexp.MustCompile("^[*`\\s]+|[*`\\s]+$")
	taskSepLeadRe = regexp.MustCompile(`^[\s—–\-:·|]+`)
)

// taskTitleCap bounds the title at the landing column's width. `tasks.title`
// is varchar(255) NOT NULL, and the unit is the one both builders slice in.
const taskTitleCap = 255

// The header keys that name a task's title, most specific first. A declaring
// row's title is its title/scope cell; length is never the rule — a row
// carrying an evidence note longer than its scope would otherwise store the
// note AS the title.
var taskTitleColumns = []string{
	"title", "task", "slice", "scope", "name", "deliverable", "work", "item",
	"description", "summary", "outcome",
}

// TaskDecl is one declared task, epic-scoped.
type TaskDecl struct {
	EpicID     string
	Code       string // the LOCAL id: TASK-… or T<n>
	Title      string
	Status     string // the declared process token, verbatim; "" = none declared
	Owner      string
	SourcePath string
	SourceLine int
	SourceRaw  string
}

// ExternalID is the epic-scoped composite the op carries.
func (d TaskDecl) ExternalID() string { return d.EpicID + "#" + d.Code }

// TaskShadow names a declaration that lost to an earlier one for the same
// `(epic, code)`. First wins; the shadowed line is flagged, never deleted.
type TaskShadow struct {
	ExternalID string
	SourcePath string
	Line       int
	Detail     string
}

// TaskRangeMismatch names one `TASK-X-<n>..<n>` token whose two ends carry
// different digit widths (D-T4-5): `9031..90310` has no defined numeric span
// — a positional `hi-lo` is meaningless when the ends don't share a width, so
// nothing is expanded from it. Flagged for a human to rewrite as explicit
// ids, never guessed: the corpus's only measured instance is
// `TASK-CMP-9031..90310` (WORKLIST.md:55, RUN:2026-08-25).
type TaskRangeMismatch struct {
	EpicID     string
	SourcePath string
	Line       int
	Token      string // the raw matched range, verbatim
	Detail     string
}

// TaskCensus is the corpus-side measurement the fidelity group reads. Every
// number here is defined by the CORPUS, never by the parser's own output.
type TaskCensus struct {
	Decls []TaskDecl
	// ExcludedRows counts task-led rows and bullets inside the
	// evidence/coverage/traceability heading family — evidence about tasks.
	ExcludedRows int
	// ExcludedOnlyIDs are the ids that appear ONLY there: the named exclusion
	// class, counted and reported, never landed and never silent.
	ExcludedOnlyIDs []string
	// ProseCells counts WORKLIST Tasks cells that carry prose and no id.
	ProseCells int
	// Shadowed are the within-epic duplicates first-wins dropped.
	Shadowed []TaskShadow
	// UnresolvedEpic names every declaration whose epic prefix resolves to no
	// emitted epic op — a named loss, never a row.
	UnresolvedEpic []string
	// MismatchedRanges names every digit-width-mismatched range token: a
	// distinct, counted class from ProseCells (that cell names a task, it
	// just can't be expanded), never landed and never silent.
	MismatchedRanges []TaskRangeMismatch
}

// WorklistTaskRow is one WORKLIST row outside the Epic Rollup that declares
// tasks — the Work Rows and Blocked/Deferred sections, whose TASK-led rows
// the `worklist-row` loss population assigned to this carrier.
type WorklistTaskRow struct {
	EpicID     string // the row's Epic cell; "" = the row names no epic
	Cell       string // the id-bearing cell, verbatim
	Title      string // the row's scope/notes cell, when it has one
	SourcePath string
	Line       int
	Raw        string
}

// ---------------------------------------------------------------- recognition

// parseRecordTasks reads one epic record's declarations by the shape rule.
func parseRecordTasks(recordText, epicID, sourcePath string) (decls []TaskDecl, excluded []TaskDecl) {
	lines := strings.Split(recordText, "\n")
	fenced := fencedLines(lines)
	inEvidence := false
	var header []string

	// Index-based, not `range`, so a wrapped bullet can advance i past the
	// continuation lines it folds in (D-T4-4) without them being re-read as
	// their own (non-matching, so harmless, but wasted) top-level lines.
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		// Fenced text is quoted TYPOGRAPHY: a template excerpt declares nothing.
		if fenced[i] {
			continue
		}
		if strings.HasPrefix(line, "#") {
			inEvidence = taskEvidenceHeadRe.MatchString(line)
			header = nil
			continue
		}
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "|") {
			cells := splitCells(trimmed)
			if len(cells) < 3 {
				continue
			}
			if isSeparatorRow(trimmed) {
				continue
			}
			code := taskLeadCode(cell(cells, 1))
			if code == "" {
				// The first non-declaring row of a table labels its columns.
				if header == nil {
					header = lowerCells(cells)
				}
				continue
			}
			d := TaskDecl{
				EpicID: epicID, Code: code, SourcePath: sourcePath,
				SourceLine: i + 1, SourceRaw: trimmed,
				Title:  taskRowTitle(header, cells),
				Status: taskRowValue(header, cells, "status"),
				Owner:  taskRowValue(header, cells, "owner"),
			}
			if inEvidence {
				excluded = append(excluded, d)
				continue
			}
			decls = append(decls, d)
			continue
		}

		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			body := strings.TrimSpace(trimmed[2:])
			code := taskLeadCode(body)
			if code == "" {
				continue
			}
			startLine, raw := i, trimmed
			// A wrapped bullet continues on indented, dash-less lines
			// directly below it (D-T4-4, mirrors ParseCriteria's §245.4
			// rule): the corpus writes long task bullets across several
			// physical lines, and unlike a table cell — where the full
			// text survives in source_raw regardless of the title rule —
			// a bullet has no second home, so a continuation not folded
			// in here is gone from the corpus for good.
			for i+1 < len(lines) && !fenced[i+1] {
				next := lines[i+1]
				nextTrimmed := strings.TrimSpace(next)
				if nextTrimmed == "" || !strings.HasPrefix(next, " ") ||
					strings.HasPrefix(nextTrimmed, "-") || strings.HasPrefix(nextTrimmed, "*") ||
					strings.HasPrefix(nextTrimmed, "|") || strings.HasPrefix(next, "#") {
					break
				}
				body += " " + nextTrimmed
				raw += " " + nextTrimmed
				i++
			}
			d := TaskDecl{
				EpicID: epicID, Code: code, SourcePath: sourcePath,
				SourceLine: startLine + 1, SourceRaw: raw,
				Title: capTaskTitle(bulletTaskTitle(body, code), code),
			}
			if inEvidence {
				excluded = append(excluded, d)
				continue
			}
			decls = append(decls, d)
		}
	}
	return decls, excluded
}

// taskLeadCode returns the task id a cell or bullet LEADS with, or "". The
// lead position is what makes a row a declaration: an id mentioned mid-cell
// is a reference to a task, not a declaration of one.
func taskLeadCode(s string) string {
	s = taskLeadTrim.ReplaceAllString(strings.TrimSpace(s), "")
	if s == "" {
		return ""
	}
	first := s
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		first = s[:i]
	}
	first = strings.Trim(first, "*`,;:.")
	if taskIDRe.MatchString(first) || taskOrdinalRe.MatchString(first) {
		return first
	}
	return ""
}

// taskRowTitle picks the declaring row's title: its named title/scope column
// when the header has one, else the first non-empty cell after the id.
func taskRowTitle(header, cells []string) string {
	body := cells
	if len(body) > 2 {
		body = body[1 : len(body)-1] // splitCells keeps the leading/trailing empties
	}
	hdr := header
	if len(hdr) > 2 {
		hdr = hdr[1 : len(hdr)-1]
	}
	for _, want := range taskTitleColumns {
		for i, h := range hdr {
			if i == 0 || i >= len(body) {
				continue
			}
			if headerKey(h) == want {
				if v := strings.TrimSpace(body[i]); v != "" {
					return capTaskTitle(v, taskLeadCode(cell(cells, 1)))
				}
			}
		}
	}
	for i := 1; i < len(body); i++ {
		if v := strings.TrimSpace(body[i]); v != "" {
			return capTaskTitle(v, taskLeadCode(cell(cells, 1)))
		}
	}
	return capTaskTitle("", taskLeadCode(cell(cells, 1)))
}

// taskRowValue reads a named column off a declaring row.
func taskRowValue(header, cells []string, want string) string {
	if header == nil {
		return ""
	}
	body, hdr := cells, header
	if len(body) > 2 {
		body = body[1 : len(body)-1]
	}
	if len(hdr) > 2 {
		hdr = hdr[1 : len(hdr)-1]
	}
	for i, h := range hdr {
		if i == 0 || i >= len(body) {
			continue
		}
		if strings.Contains(h, want) {
			return strings.TrimSpace(strings.Trim(strings.TrimSpace(body[i]), "*"))
		}
	}
	return ""
}

// bulletTaskTitle is what a bullet says after its id — the same content a
// table row puts in its title cell.
//
// The first trim covers both markdown chars, as before; every trim after
// that covers ONLY "*" (D-T4-2). The corpus's dominant shape is
// "- **TASK-ID:** text": cutting the id out of `**TASK-ID:** text` leaves
// `**:** text`, and a single trim pass removes the leading "**" but stops at
// the colon — the "**" the colon strip then exposes never gets a second
// pass, leaking a bare "**" into 17 titles. Repeating catches any depth of
// "**"/separator interleaving. Backtick is excluded from every pass after
// the first on purpose: "- **TASK-SH-002** — `App.shell.test.tsx` (…)"
// exposes a leading backtick the SAME way once the em-dash is stripped, but
// that backtick OPENS an inline code span whose closing partner sits further
// into the text — trimming it strands that partner instead of freeing an
// orphan, which a first pass over "**TASK-SH-002**"'s already-adjacent pair
// never does.
func bulletTaskTitle(body, code string) string {
	rest := strings.Replace(body, code, "", 1)
	rest = strings.TrimSpace(strings.Trim(strings.TrimSpace(rest), "*`"))
	rest = strings.TrimSpace(taskSepLeadRe.ReplaceAllString(rest, ""))
	for {
		next := strings.TrimSpace(strings.Trim(rest, "*"))
		next = strings.TrimSpace(taskSepLeadRe.ReplaceAllString(next, ""))
		if next == rest {
			return next
		}
		rest = next
	}
}

// taskTitleText decides what a cell can say about ONE of the tasks it names.
//
// A cell naming a single task is that task's own line, id included — the
// `TASK-X-001 does the thing` shape, and its verbatim text is the title.
//
// A cell naming several (`TASK-X-001..004`, `TASK-X-001/002/003`) describes
// none of them individually: the reference is a list of ids, not a
// description. Handing it to every expanded row put a literal range string in
// 148 of 631 task titles and left 37 (epic, title) pairs identical. So the
// shared reference is removed and only what the cell actually says about the
// work remains — and when it says nothing, capTaskTitle's own rule supplies
// the code. Nothing is invented either way; the full cell stays in source_raw.
func taskTitleText(cellText, prose string, ids []string) string {
	if len(ids) > 1 {
		return prose
	}
	return cellText
}

// capTaskTitle applies D-T4-3: the verbatim source text, capped at the landing
// column's width — and the code itself when the corpus says nothing else.
// Prose is never invented.
func capTaskTitle(text, code string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return code
	}
	if utf16Len(text) <= taskTitleCap {
		return text
	}
	out := []rune(text)
	n := 0
	for i, r := range out {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if n+w > taskTitleCap {
			return string(out[:i])
		}
		n += w
	}
	return text
}

// ---------------------------------------------------------------- tokens

// expandTaskRefs reads every task a cell names: bare ids, `TASK-X-001..004`
// ranges, `TASK-X-001/002` lists, and the epic-local `T1..T13` ordinal form.
func expandTaskRefs(cellText string) []string {
	ids, _ := taskRefsAndProse(cellText)
	return ids
}

// taskRefsAndProse reads the ids a cell names AND what the cell still says
// once those references are removed. The residue is the only part that can be
// a title: a cell reading `TASK-X-001..004` describes no individual task, and
// capTaskTitle's own rule — "the code itself when the corpus says nothing
// else" — then supplies the code. Feeding it the unstripped cell is what put a
// literal range string in 148 task titles.
func taskRefsAndProse(cellText string) ([]string, string) {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rest := cellText
	for _, m := range taskRangeRe.FindAllStringSubmatch(cellText, -1) {
		if len(m[2]) != len(m[3]) {
			// D-T4-5: different digit widths mean the two ends don't share a
			// positional value, so no numeric span is well-defined — hi-lo
			// on the raw digits is not "big", it's meaningless. Consumed out
			// of rest so the bare leading id can't leak through the
			// bare-token pass below as a false single-task declaration;
			// mismatchedTaskRanges names the token for a human instead of
			// this function guessing at it.
			rest = strings.ReplaceAll(rest, m[0], " ")
			continue
		}
		lo, hi := atoiOr(m[2], -1), atoiOr(m[3], -1)
		width := len(m[2])
		if lo < 0 || hi < lo || hi-lo > 500 {
			continue
		}
		for n := lo; n <= hi; n++ {
			add(fmt.Sprintf("%s%0*d", m[1], width, n))
		}
		rest = strings.ReplaceAll(rest, m[0], " ")
	}
	for _, m := range taskListRe.FindAllStringSubmatch(rest, -1) {
		add(m[1])
		prefix := m[1][:strings.LastIndex(m[1], "-")+1]
		width := len(m[1]) - len(prefix)
		for _, tail := range strings.Split(strings.TrimPrefix(m[2], "/"), "/") {
			if n := atoiOr(tail, -1); n >= 0 {
				add(fmt.Sprintf("%s%0*d", prefix, width, n))
			}
		}
		rest = strings.ReplaceAll(rest, m[0], " ")
	}
	for _, id := range taskTokenRe.FindAllString(rest, -1) {
		add(id)
	}
	rest = taskTokenRe.ReplaceAllString(rest, " ")
	for _, m := range ordRangeRe.FindAllStringSubmatch(rest, -1) {
		lo, hi := atoiOr(m[1], -1), atoiOr(m[2], -1)
		if lo < 0 || hi < lo || hi-lo > 500 {
			continue
		}
		for n := lo; n <= hi; n++ {
			add(fmt.Sprintf("T%d", n))
		}
		rest = strings.ReplaceAll(rest, m[0], " ")
	}
	for _, id := range ordTokenRe.FindAllString(rest, -1) {
		add(id)
	}
	rest = ordTokenRe.ReplaceAllString(rest, " ")
	return out, taskCellProse(rest)
}

// mismatchedTaskRanges reports every `TASK-X-<n>..<n>` token in cellText
// whose two ends carry different digit widths (D-T4-5) — see
// taskRefsAndProse for why such a token is consumed but never expanded. A
// tiny second regex pass over the same text, kept separate from
// taskRefsAndProse's own loop, rather than a third return value threaded
// through every caller: this is a reporting-only concern, and the callers
// that need it already hold the (epic, source, line) context to name it.
func mismatchedTaskRanges(cellText string) []string {
	var out []string
	for _, m := range taskRangeRe.FindAllStringSubmatch(cellText, -1) {
		if len(m[2]) != len(m[3]) {
			out = append(out, m[0])
		}
	}
	return out
}

// taskCellProse is what remains of a cell after its id references are gone:
// whitespace collapsed and the separators that only joined the ids trimmed
// away. Empty means the cell named tasks and said nothing about them.
func taskCellProse(rest string) string {
	rest = strings.Join(strings.Fields(rest), " ")
	rest = strings.Trim(rest, " ,;·+/&—–-")
	return strings.TrimSpace(rest)
}

// ---------------------------------------------------------------- census

// TaskDeclarations is the corpus census: every declaration the shape rule
// finds, plus the exclusion, duplicate and loss classes it names rather than
// drops.
func TaskDeclarations(data Data, records map[string]string) TaskCensus {
	census := TaskCensus{}
	knownEpic := map[string]bool{}
	for _, e := range data.Epics {
		knownEpic[e.ID] = true
	}

	declaredIDs := map[string]bool{}
	excludedIDs := map[string]bool{}
	wonBy := map[string]string{} // external id → the source path that won it

	keep := func(d TaskDecl) {
		if !knownEpic[d.EpicID] {
			census.UnresolvedEpic = append(census.UnresolvedEpic,
				fmt.Sprintf("%s (%s:%d): the epic prefix resolves to no emitted epic", d.ExternalID(), d.SourcePath, d.SourceLine))
			return
		}
		if won, taken := wonBy[d.ExternalID()]; taken {
			// Two CARRIERS naming one task is the corpus's normal shape — a
			// rollup cell restates what its record declares, and the record
			// wins because it is the richer source. Only a second declaration
			// in the SAME file is a shadowed one: there the losing line's own
			// cells reach no op and a reader has an edit to make.
			if won == d.SourcePath {
				census.Shadowed = append(census.Shadowed, TaskShadow{
					ExternalID: d.ExternalID(), SourcePath: d.SourcePath, Line: d.SourceLine,
					Detail: fmt.Sprintf("%s is declared twice in %s under %s; the first declaration wins, so this line's cells reach no op — merge them", d.Code, d.SourcePath, d.EpicID),
				})
			}
			return
		}
		wonBy[d.ExternalID()] = d.SourcePath
		census.Decls = append(census.Decls, d)
	}

	// (a) the epic records, read by shape.
	for _, e := range data.Epics {
		sourcePath := e.Record
		if sourcePath == "" {
			sourcePath = e.ID
		}
		decls, excluded := parseRecordTasks(records[e.ID], e.ID, sourcePath)
		for _, d := range decls {
			declaredIDs[d.Code] = true
			keep(d)
		}
		for _, d := range excluded {
			census.ExcludedRows++
			excludedIDs[d.Code] = true
		}
	}

	// (b) the WORKLIST Epic-Rollup Tasks cell, ranges expanded.
	for _, e := range data.Epics {
		cellText := strings.TrimSpace(e.TasksCell)
		if cellText == "" || strings.Trim(cellText, "—–- ") == "" {
			continue
		}
		ids, prose := taskRefsAndProse(cellText)
		mismatched := mismatchedTaskRanges(cellText)
		for _, tok := range mismatched {
			census.MismatchedRanges = append(census.MismatchedRanges, TaskRangeMismatch{
				EpicID: e.ID, SourcePath: worklistPath, Token: tok,
				Detail: fmt.Sprintf("%s names %s, whose ends carry different digit widths — not expanded; rewrite as explicit ids", e.ID, tok),
			})
		}
		if len(ids) == 0 {
			if len(mismatched) == 0 {
				census.ProseCells++ // prose and no id: counted, never silent
			}
			continue
		}
		for _, id := range ids {
			keep(TaskDecl{
				EpicID: e.ID, Code: id, SourcePath: worklistPath,
				SourceRaw: cellText, Title: capTaskTitle(taskTitleText(cellText, prose, ids), id),
			})
		}
	}

	// (c) the WORKLIST Work Rows and Blocked/Deferred sections.
	for _, row := range data.WorklistTaskRows {
		ids, prose := taskRefsAndProse(row.Cell)
		mismatched := mismatchedTaskRanges(row.Cell)
		for _, tok := range mismatched {
			census.MismatchedRanges = append(census.MismatchedRanges, TaskRangeMismatch{
				EpicID: row.EpicID, SourcePath: row.SourcePath, Line: row.Line, Token: tok,
				Detail: fmt.Sprintf("%s names %s, whose ends carry different digit widths — not expanded; rewrite as explicit ids", row.SourcePath, tok),
			})
		}
		if len(ids) == 0 {
			if len(mismatched) == 0 {
				census.ProseCells++
			}
			continue
		}
		// The row's own title column wins; otherwise the cell, with a shared
		// multi-task reference removed.
		title := row.Title
		if title == "" {
			title = taskTitleText(row.Cell, prose, ids)
		}
		for _, id := range ids {
			keep(TaskDecl{
				EpicID: row.EpicID, Code: id, SourcePath: row.SourcePath,
				SourceLine: row.Line, SourceRaw: row.Raw,
				Title: capTaskTitle(title, id),
			})
		}
	}

	for id := range excludedIDs {
		if !declaredIDs[id] {
			census.ExcludedOnlyIDs = append(census.ExcludedOnlyIDs, id)
		}
	}
	sort.Strings(census.ExcludedOnlyIDs)
	sort.Strings(census.UnresolvedEpic)
	return census
}

const worklistPath = "WORKLIST.md"

// ---------------------------------------------------------------- emission

// BuildTaskOps emits one `upsert_task` op per declaration, in census order —
// after the epic ops, because the applier resolves the owning epic by code.
func BuildTaskOps(data Data, records map[string]string) []Op {
	census := TaskDeclarations(data, records)
	ops := make([]Op, 0, len(census.Decls))
	for _, d := range census.Decls {
		payload := map[string]any{
			"external_id":                d.ExternalID(),
			"code":                       d.Code,
			"title":                      d.Title,
			"declared_owner_type":        "epic",
			"declared_owner_external_id": d.EpicID,
			// `tasks` is the product Task table, so every imported row says so:
			// the read surface, the readback and the from-empty precondition all
			// filter on the stamp.
			"sync_born":   true,
			"source_path": d.SourcePath,
		}
		if d.Status != "" {
			payload["process_status"] = d.Status
		}
		if d.Owner != "" {
			payload["owner"] = d.Owner
		}
		if d.SourceLine > 0 {
			payload["source_line"] = d.SourceLine
		}
		if d.SourceRaw != "" {
			payload["source_raw"] = d.SourceRaw
		}
		payload["content_hash"] = ContentHash(payload)
		payload["actor"] = actor
		ops = append(ops, Op{Type: "upsert_task", Payload: payload})
	}
	return ops
}
