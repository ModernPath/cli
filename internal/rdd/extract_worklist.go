package rdd

import (
	"path/filepath"
	"regexp"

	"strings"
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
