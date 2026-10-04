package rdd

import (
	"crypto/sha256"
	"encoding/hex"

	"regexp"

	"strings"
)

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
			// register's capability rows — the unbundled column says which.
			// REQ-CROSS-423 (USER:2026-09-21 D2): a ledger gap is a missing
			// record, i.e. a specification gap — the store's canonical word.
			GapKind:    "specification",
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
				// REQ-CROSS-423: the canonical disposition words, as the
				// store's changeset states them.
				row.Disposition = "ROUTED to " + ref
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
					row.Disposition = "DEFERRED"
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
