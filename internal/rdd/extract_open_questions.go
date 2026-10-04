package rdd

import (
	"regexp"

	"strings"
)

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
