package rdd

import (
	"fmt"

	"regexp"

	"strings"
)

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
