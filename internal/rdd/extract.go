// Package rdd — the CLI-bundled parsers for the RDD process-document formats
// (REQ-CROSS-013, EPIC-SYNC-004 TASK-SY-404). A faithful Go port of the
// mission-control extractor (`mission-control/extract.js`) + op-builder
// (`mission-control/cli/ops.js`): same fields, same heuristics, same content
// hashes — so switching a workspace from the node extractor to these parsers
// is hash-stable (no re-sync churn).
//
// Formats: rdd-ledger-v1 (tasks/<CTX>-REQUIREMENTS.md), rdd-worklist-v1
// (WORKLIST.md), rdd-epic-v1 (epics/*), rdd-review-queue-v1 (docs/85),
// rdd-open-questions-v1 (process/08).
package rdd

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const bodyCap = 15000 // extract.js BODY_CAP (UTF-16 code units, like JS .slice)

// Req is one requirements-ledger row (+ its detail block).
type Req struct {
	ID     string
	Title  string
	Stage  string
	Status string
	Source string
	Ctx    string
	Detail string // "" = none (JS null)
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
	// initiatives.status, which is the board column (SERVER-SYNC-DESIGN §1.3).
	// "" = the cell was empty or an em-dash: absence, not a status.
	Upper string
	Lower string
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
	Reqs    []Req
	Epics   []Epic
	RQs     []RQ
	OQs     []OQ
	Commits []Commit
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
// A cell may legitimately contain "\|" — sampo's REQ-AP-103 title says
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
	ledgerDirRe    = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9]{1,9})$`)
	ledgerRowRe    = regexp.MustCompile(`^\|\s*(REQ-[A-Z][A-Z0-9]*-\d+)\s*\|`)
	ledgerDetailRe = regexp.MustCompile(`^###\s+(REQ-[A-Z][A-Z0-9]*-\d+)`)
	headingH2Re    = regexp.MustCompile(`^##\s`)
	headingH3Re    = regexp.MustCompile(`^###\s`)
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
func ParseLedger(path string, content string) []Req {
	ctx := ledgerContext(path)
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

	var reqs []Req
	for _, line := range lines {
		if !ledgerRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		// | ID | Title | Stage | Status | Source | Tests | Code |
		reqs = append(reqs, Req{
			ID:     cell(cells, 1),
			Title:  cell(cells, 2),
			Stage:  cell(cells, 3),
			Status: normalizeStatus(cell(cells, 4)),
			Source: cell(cells, 5),
			Ctx:    ctx,
			Detail: details[cell(cells, 1)],
		})
	}
	return reqs
}

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
)

// statusCell normalizes a loop-status cell: a placeholder ("", "—", "-") is
// absence and syncs as nothing at all, so those epics' hashes never move.
func statusCell(raw string) string {
	v := strip(raw)
	switch v {
	case "", "—", "-", "–":
		return ""
	}
	// 120 mirrors the node extractor's own slice on these cells — a real WORKLIST
	// cell reaches 560 chars, so the cap bites and the two must agree exactly.
	return capRunes(v, 120)
}

// ParseWorklist parses WORKLIST.md rollup rows (rdd-worklist-v1). epicFiles is
// the directory listing of the epics/ dir (record prefix-matching), with
// isDir saying which entries are directories (dir records resolve /EPIC.md).
func ParseWorklist(content string, epicFiles []string, isDir map[string]bool) []Epic {
	var epics []Epic
	for _, line := range strings.Split(content, "\n") {
		if !worklistRowRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 11 {
			continue
		}
		id := strings.SplitN(bracketStripRe.ReplaceAllString(cell(cells, 1), ""), " ", 2)[0]
		overall := strip(cell(cells, 9))
		approval := strip(cell(cells, 10))
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
		epics = append(epics, Epic{ID: id, State: state, Record: record, Upper: upper, Lower: lower})
	}
	return epics
}

// ---------------------------------------------------------------- rdd-review-queue-v1

var (
	rqHeadRe      = regexp.MustCompile(`^###\s+RQ-`)
	rqIDRe        = regexp.MustCompile(`RQ-\d+[a-z]?`)
	h2NotH3Re     = regexp.MustCompile(`^##[^#]`)
	rqClosedRe    = regexp.MustCompile(`(?i)APPROVED|RESOLVED|✅`)
	rqDelegatedRe = regexp.MustCompile(`(?i)DELEGATED`)
	rqDecisionRe  = regexp.MustCompile(`(?i)DECISION NEEDED`)
	dateRe        = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)
	rqTitleIDRe   = regexp.MustCompile(`^RQ-\d+[a-z]?\s*`)
	rqTitleParRe  = regexp.MustCompile(`^\([^)]*\)\s*`)
	rqTitleDashRe = regexp.MustCompile(`^[—–-]\s*`)
	rqTitleDecRe  = regexp.MustCompile(`(?i)\s*—\s*DECISION NEEDED.*$`)
	recommendRe   = regexp.MustCompile(`(?i)[^.\n]*recommend[^.\n]*\.`)
	optionMarkRe  = regexp.MustCompile(`\*{0,2}\((i{1,3}|iv|v|[a-z]|\d{1,2})\)\*{0,2}\s+`)
	headPrefixRe  = regexp.MustCompile(`^###\s*`)
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
	oqRowRe      = regexp.MustCompile("^\\|\\s*~{0,2}((?:OQ|GAP)-[\\w.-]+?)~{0,2}\\s*\\|")
	oqDoneRe     = regexp.MustCompile(`(?i)done`)
	oqResolvedRe = regexp.MustCompile(`(?i)✅|~~\s*OQ-|\bRESOLVED\b|\bDONE\b`)
	sentenceRe   = regexp.MustCompile(`^([^.?]*[.?])`)
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

// ParseOpenQuestions parses process/08-open-questions.md (rdd-open-questions-v1).
func ParseOpenQuestions(content string) []OQ {
	var oqs []OQ
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
		oqs = append(oqs, OQ{
			ID:        oqRowRe.FindStringSubmatch(line)[1],
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
