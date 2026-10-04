package rdd

import (
	"fmt"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------- criteria + citations

var (
	// REQ-CROSS-107: the label may carry a parenthetical qualifier —
	// "**Acceptance criteria (BDD):**", "(as-built)" — and an unqualified
	// pattern silently yielded zero criteria for every row that used one.
	criteriaHeadRe  = regexp.MustCompile(`^-\s+\*\*Acceptance criteria(?:\s*\([^)]*\))?:\*\*`)
	fieldHeadRe     = regexp.MustCompile(`^-\s+\*\*`)
	criterionLineRe = regexp.MustCompile(`^\s+-\s+(.*\S)\s*$`)
	gwtRe           = regexp.MustCompile(`(?s)^GIVEN\s+(.*?)(?:\s+WHEN\s+(.*?))?\s+THEN\s+(.*)$`)
	citationSplitRe = regexp.MustCompile(`\s*[·,]\s*`)
	userRefRe       = regexp.MustCompile(`^USER:`)
	epicRefRe       = regexp.MustCompile(`^EPIC-`)
	ruleRefRe       = regexp.MustCompile(`^(INV|BR|RQ)-`)
)

// ParseCriteria pulls the acceptance-criteria bullets out of a ledger detail
// block: GWT parts (WHEN optional) or a plain statement.
func ParseCriteria(detail, reqID string) []any {
	lines := strings.Split(detail, "\n")
	start := -1
	for i, l := range lines {
		if criteriaHeadRe.MatchString(l) {
			start = i
			break
		}
	}
	if start == -1 {
		return []any{}
	}
	// §245.4: collect bullet texts first so a wrapped bullet — an indented,
	// dash-less line directly continuing one — joins its clause before the
	// GWT parse runs. A blank line ends a continuation.
	var texts []string
	continuing := false
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if fieldHeadRe.MatchString(line) {
			break // next top-level field
		}
		if m := criterionLineRe.FindStringSubmatch(line); m != nil {
			texts = append(texts, m[1])
			continuing = true
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continuing = false
			continue
		}
		if continuing && strings.HasPrefix(line, " ") && !strings.HasPrefix(trimmed, "-") && len(texts) > 0 {
			texts[len(texts)-1] += " " + trimmed
		}
	}
	criteria := []any{}
	for _, text := range texts {
		position := len(criteria) + 1
		c := map[string]any{
			"external_id": fmt.Sprintf("%s#AC%d", reqID, position),
			"position":    position,
			"kind":        "criterion",
		}
		if g := gwtRe.FindStringSubmatch(text); g != nil {
			c["given"] = g[1]
			if g[2] != "" {
				c["when"] = g[2]
			}
			c["then"] = g[3]
		} else {
			c["statement"] = text
		}
		criteria = append(criteria, c)
	}
	return criteria
}

// REQ-CROSS-072: split on the interpunct always, and on a comma ONLY at the top
// level. A comma inside parentheses or quotes is prose — a thousands separator,
// a list inside a parenthetical, a quoted sentence from the user — and treating
// it as a separator shredded those citations into unreadable fragments on the
// way to the platform. Top-level commas still separate, so ledgers written that
// way keep working.
//
// This is a scanner rather than a regexp because RE2 cannot match balanced
// delimiters at all.
func splitCitations(source string) []string {
	var (
		out    []string
		buf    []rune
		depth  int
		inQuot bool
	)
	flush := func() {
		out = append(out, string(buf))
		buf = buf[:0]
	}
	for _, r := range source {
		switch r {
		case '"', '“', '”':
			inQuot = !inQuot
			buf = append(buf, r)
		case '(', '[':
			depth++
			buf = append(buf, r)
		case ')', ']':
			if depth > 0 {
				depth--
			}
			buf = append(buf, r)
		case '·':
			flush()
		case ',':
			if depth == 0 && !inQuot {
				flush()
			} else {
				buf = append(buf, r)
			}
		default:
			buf = append(buf, r)
		}
	}
	flush()
	return out
}

// ParseCitations turns a ledger Source cell into structured citations.
func ParseCitations(source string) []any {
	if source == "" || source == "—" {
		return []any{}
	}
	out := []any{}
	for _, s := range splitCitations(source) {
		ref := strings.TrimSpace(strings.ReplaceAll(s, "`", ""))
		if ref == "" {
			continue
		}
		kind := "doc"
		switch {
		case userRefRe.MatchString(ref):
			kind = "user"
		case epicRefRe.MatchString(ref):
			kind = "epic"
		case ruleRefRe.MatchString(ref):
			kind = "rule"
		}
		out = append(out, map[string]any{"kind": kind, "ref": ref})
	}
	return out
}

// ---------------------------------------------------------------- op builders

// REQ-PLN-054 / D-MC-6 (USER:2026-08-07): the row's one-line description.
// Preference order, decided from ledger coverage (63 of 201 blocks carry a
// Statement at decision time):
//  1. the detail block's **Statement:** line — already the one-line summary;
//  2. else the first acceptance criterion's THEN clause — the outcome half
//     reads as a summary where the full GIVEN/WHEN/THEN reads as a test;
//  3. else empty — a blank beats an invented sentence (#9).
var (
	// A statement may wrap: continuation lines are indented and do not start a
	// new "- **Field:**" bullet. Reading only the first line cut every long
	// statement mid-sentence on its way to the platform (`RUN:2026-08-12`).
	// REQ-CROSS-107: the label may carry a parenthetical qualifier —
	// "**Statement (unconfirmed):**" marks a description a BLOCKED row cannot
	// yet assert — and an unqualified pattern read those rows as having none.
	statementLineRe = regexp.MustCompile(`(?ms)^\s*-\s*\*\*Statement(?:\s*\([^)]*\))?:\*\*\s*(.+?)(?:\n\s*-\s*\*\*|\n\s*\n|\z)`)
	// A DEFERRED row's Reason and a BLOCKED row's Observed ARE its one-line
	// summary: the ledger requires them for those statuses, and without this
	// fallback such a row syncs with an empty description. The optional
	// backticked date in "Observed `RUN:…`:" contains a colon, so the label
	// match excludes only asterisks.
	reasonLineRe = regexp.MustCompile(`(?ms)^\s*-\s*\*\*(?:Reason|Observed)[^*]*?:\*\*\s*(.+?)(?:\n\s*-\s*\*\*|\n\s*\n|\z)`)
	thenClauseRe = regexp.MustCompile(`(?i)\bTHEN\s+(.+)$`)
)

func ParseDescription(detail string) string {
	if detail == "" {
		return ""
	}
	if m := statementLineRe.FindStringSubmatch(detail); m != nil {
		// Re-flow the wrapped lines into one sentence.
		return strings.Join(strings.Fields(m[1]), " ")
	}
	if m := reasonLineRe.FindStringSubmatch(detail); m != nil {
		return strings.Join(strings.Fields(m[1]), " ")
	}
	for _, raw := range ParseCriteria(detail, "") {
		c, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		text, _ := c["then"].(string)
		if text == "" {
			// GWT criteria store parts; plain ones store the whole statement.
			if stmt, ok := c["statement"].(string); ok {
				if m := thenClauseRe.FindStringSubmatch(stmt); m != nil {
					text = m[1]
				}
			}
		}
		if text = strings.TrimSpace(text); text != "" {
			// Sentence-case the clause so it reads as prose, not a fragment.
			r := []rune(text)
			return strings.TrimSuffix(strings.ToUpper(string(r[0]))+string(r[1:]), ".") + "."
		}
	}
	return ""
}
