package rdd

import (
	"regexp"
	"strings"
	"unicode"
)

// ---------------------------------------------------------------- scenarios

var (
	scenarioRowRe  = regexp.MustCompile(`^\s*\|`)
	tableDividerRe = regexp.MustCompile(`^\s*\|[\s\-:|]+\|\s*$`)
	// An SCN id may carry a letter suffix: SCN-BD-070a..d are four distinct
	// scenarios of one slice, and a pattern that stops at the digits gives all
	// four the same external id — which, under replace-set semantics, is one
	// scenario overwritten three times.
	scnIDRe = regexp.MustCompile(`\bSCN-[A-Z][A-Z0-9]*-\d+[a-z]?\b`)
	// A scenario DEFINITION written as a bullet. The bold run is the
	// discriminator: a definition bolds the id and at most one parenthetical
	// qualifier, then closes. An evidence bullet keeps going inside the bold —
	// "**SCN-SY-013 (drift half: Done decays) — LOWER_VERIFIED + live, 2026-07-22:**"
	// — and frequently bolds two ids at once. Both shapes lead with a bold SCN
	// id, so only where the bold run ENDS separates the epic's acceptance
	// content from a record of how it was evidenced.
	scnBulletDefRe = regexp.MustCompile(`^-\s+\*\*(SCN-[A-Z][A-Z0-9]*-\d+[a-z]?)\s*(?:\([^)]*\))?\s*:?\*\*\s*(.*)$`)
	// A scenario DEFINITION written as its own heading block: `### SCN-AK-001 — …`,
	// its body running to the next heading.
	scnHeadingDefRe = regexp.MustCompile(`^#{3,}\s+(SCN-[A-Z][A-Z0-9]*-\d+[a-z]?)\b\s*(.*)$`)
	// A scenario DEFINITION written as Gherkin inside a fence. The KEYWORD is
	// the discriminator, not the id: a fence is where the corpus quotes things,
	// and a trace diagram writes `-> SCN-NAM-201..205` while a coverage note
	// writes `Scenario evidence: SCN-…`. Both name ids and neither declares one.
	// Only `Scenario:` and `Scenario Outline:` immediately followed by an id do.
	// Shared with the fidelity fence arm, so the parser and the instrument that
	// measures it cannot disagree about what a declaration is.
	fencedScenarioDeclRe = regexp.MustCompile(`^\s*Scenario(?: Outline)?:\s*(SCN-[A-Z][A-Z0-9]*-\d+[a-z]?)\b\s*(.*)$`)
	anyHeadingRe         = regexp.MustCompile(`^#{1,6}\s`)
	codeFenceRe          = regexp.MustCompile("^\\s*(```|~~~)")
	// Separators between a definition's id and its text, in every shape the
	// records write them.
	scnLeadSepRe = regexp.MustCompile(`^[\s—–\-:·]+`)
)

// textColumns are the header names that hold the scenario's prose, in the order
// they are preferred. Six header shapes exist across the epic records; naming
// the column beats guessing, because an evidence cell can be longer than the
// summary it describes.
var textColumns = []string{"gherkin", "givenwhenthen", "statement", "summary", "description"}

// Definition shapes, ranked. When one id is written twice in one record the
// richer form wins: six records carry a summary TABLE indexing the same ids
// their heading BLOCKS define in full, and the table cell is the index.
//
// A fenced Gherkin declaration outranks the table cell and the bullet for the
// same reason — it carries the whole Given/When/Then body where they carry one
// line. It sits BELOW the heading block because a `### SCN-…` block already
// carries any fence in its body verbatim: the block is a superset, so letting
// the declaration inside it win would trade the author's narrative for a
// fragment of itself.
const (
	shapeTableRow = iota + 1
	shapeBullet
	shapeFencedGherkin
	shapeHeadingBlock
)

type scenarioDef struct {
	id     string
	title  string
	text   string
	shape  int
	line   int
	status string
	raw    string
	// REQ-CROSS-266: the evidence conclusion the row's status-family cell
	// declares. Separate from `status` because PROCESS.md is explicit that an
	// evidence conclusion is not a lifecycle state — widening the lifecycle
	// vocabulary would put an evidence fact where no lifecycle transition can
	// read it. "" = the row declares none.
	conclusion string
}

// ParseScenarios — the epic record's acceptance scenarios as scenario-kind
// criteria (REQ-CROSS-028). The server has accepted these since M4 (Core.Sync
// routes payload `scenarios` through sync_criteria with owner :epic_id).
// Returns an empty slice when the record defines none, so BuildEpicOp can omit
// the key and leave the content hash alone.
//
// The records write a definition in three shapes, and all three are read:
//
//   - a table row inside the scenario section — read only there, because an
//     evidence map, a coverage table and a traceability matrix lead their rows
//     with the same ids and define nothing;
//   - a bold bullet whose bold run stops at the id — read wherever it sits,
//     because one record files its acceptance under a heading that names no
//     scenarios at all;
//   - an `### SCN-…` heading and its body to the next heading, fences included
//     verbatim.
//
// Ids are namespaced by their epic — "EPIC-FE-034#SCN-AK-001" — exactly as
// requirement criteria are ("REQ-CMP-010#AC1"). SCN ids are scoped by AREA, not
// by epic (85 prefixes across the records), so two different epics working one
// area legitimately reach for the same id: SCN-AK-001 belongs to both
// EPIC-FE-034 and EPIC-PORTFOLIO-038. Since acceptance_criteria.external_id is
// unique per tenant, the bare id let the second sync steal the first's row and
// 9 epics served no scenarios at all (RUN:2026-08-07, found by reading beta
// back after deploy). Namespacing makes that impossible by construction.
func parseScenarioDefs(recordText string) []*scenarioDef {
	lines := strings.Split(recordText, "\n")
	fenced := fencedLines(lines)

	var (
		order   []string
		byID    = map[string]*scenarioDef{}
		header  []string
		heading string
		// The scenario section is read once: the FIRST level-2 section whose
		// heading titles acceptance scenarios. inSection is that span.
		inSection, sectionRead bool
	)
	// add records one definition. A second, richer form of an id already seen
	// replaces its text and keeps its position — never a second entry, because
	// the payload is a replace-set keyed by external_id and two rows for one id
	// are one row fighting itself.
	add := func(id, title, text string, shape, line int, status, raw, conclusion string) {
		text = strings.TrimSpace(text)
		if id == "" || text == "" {
			return
		}
		if status == "" {
			status = "active"
		}
		if d, ok := byID[id]; ok {
			if status != "active" {
				d.status = status
			}
			// A stated conclusion is a fact one shape declared; a shape that
			// states none does not retract it.
			if conclusion != "" {
				d.conclusion = conclusion
			}
			if shape > d.shape {
				d.title, d.text, d.shape, d.line, d.raw = title, text, shape, line, strings.TrimSpace(raw)
			}
			return
		}
		byID[id] = &scenarioDef{
			id: id, title: title, text: text, shape: shape, line: line,
			status: status, raw: strings.TrimSpace(raw), conclusion: conclusion,
		}
		order = append(order, id)
	}

	for i := 0; i < len(lines); i++ {
		if fenced[i] {
			// Fenced text is quoted TYPOGRAPHY, not quoted meaning. Inside the
			// scenario section a Gherkin declaration is this epic's acceptance
			// content written in the notation Gherkin exists for — six of them
			// in one record reached nothing at all. Outside that section a fence
			// stays quoted, because that is where a record puts a sample.
			//
			// inSection already implies the heading is not an excluded one: the
			// section rule refuses deferred, out-of-scope and reference headings
			// before it ever opens.
			if !inSection {
				continue // a template excerpt, a trace diagram, a quoted sample
			}
			m := fencedScenarioDeclRe.FindStringSubmatch(lines[i])
			if m == nil {
				continue
			}
			// The definition runs to the next declaration or to the end of the
			// fence, whichever comes first — a body that could swallow the next
			// declaration would fuse two scenarios into one.
			j := i + 1
			for ; j < len(lines); j++ {
				if !fenced[j] || codeFenceRe.MatchString(lines[j]) || fencedScenarioDeclRe.MatchString(lines[j]) {
					break
				}
			}
			title := scnLeadSepRe.ReplaceAllString(strings.TrimSpace(m[2]), "")
			body := strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
			add(m[1], title, strings.TrimSpace(title+"\n\n"+body), shapeFencedGherkin, i+1, "active", strings.Join(lines[i:j], "\n"), "")
			i = j - 1
			continue
		}
		line := lines[i]

		if m := h2HeadingRe.FindStringSubmatch(line); m != nil {
			inSection = false
			if !sectionRead && scenarioSectionHeading(strings.TrimSpace(m[1])) {
				inSection, sectionRead, header = true, true, nil
			}
			heading = strings.TrimSpace(m[1])
			continue
		}
		// A section marked deferred, rejected, superseded, or one of the
		// reference shapes (evidence map, coverage, traceability) defines no
		// acceptance content of this epic's, whatever shape it writes.
		excluded := excludedSectionHeading(heading)

		if m := scnHeadingDefRe.FindStringSubmatch(line); m != nil {
			// Everything to the next heading is this scenario's body — a table
			// citing sibling ids, a fenced gherkin block, a bullet the author
			// indented into it. A body that could start a second scenario would
			// split one definition in two.
			j := i + 1
			for ; j < len(lines); j++ {
				if !fenced[j] && anyHeadingRe.MatchString(lines[j]) {
					break
				}
			}
			if !excluded {
				title := scnLeadSepRe.ReplaceAllString(strings.TrimSpace(m[2]), "")
				body := strings.TrimSpace(strings.Join(lines[i+1:j], "\n"))
				add(m[1], title, strings.TrimSpace(title+"\n\n"+body), shapeHeadingBlock, i+1, "active", strings.Join(lines[i:j], "\n"), "")
			}
			i = j - 1
			continue
		}

		if m := scnBulletDefRe.FindStringSubmatch(line); m != nil {
			// 51 of the corpus's 107 definition bullets wrap; the continuation
			// carries the THEN clause often enough that dropping it would cut
			// the observable outcome off the scenario.
			text := m[2]
			j := i + 1
			for ; j < len(lines); j++ {
				if fenced[j] || strings.TrimSpace(lines[j]) == "" || !startsWithSpace(lines[j]) {
					break
				}
				text += " " + strings.TrimSpace(lines[j])
			}
			if !excluded {
				add(m[1], "", scnLeadSepRe.ReplaceAllString(text, ""), shapeBullet, i+1, "active", strings.Join(lines[i:j], "\n"), "")
			}
			i = j - 1
			continue
		}

		// Table rows are read ONLY inside the scenario section. Their id column
		// is the same shape an evidence map, a coverage table and a
		// traceability matrix all write, so the section is the only thing that
		// separates a definition from a citation.
		if !inSection || !scenarioRowRe.MatchString(line) || tableDividerRe.MatchString(line) {
			continue
		}
		cells := splitCells(line)
		if len(cells) < 3 { // splitCells keeps the leading/trailing empties
			continue
		}
		cells = cells[1 : len(cells)-1]
		for k := range cells {
			cells[k] = strings.TrimSpace(cells[k])
		}
		if header == nil {
			header = cells
			continue
		}

		idAt := -1
		for k, c := range cells {
			if scnIDRe.MatchString(c) {
				idAt = k
				break
			}
		}
		if idAt == -1 {
			continue // not a scenario row (a stray table, a note)
		}
		add(
			scnIDRe.FindString(cells[idAt]),
			"",
			scenarioText(header, cells, idAt),
			shapeTableRow,
			i+1,
			scenarioLifecycleStatus(header, cells),
			line,
			scenarioEvidenceConclusion(header, cells),
		)
	}

	defs := make([]*scenarioDef, 0, len(order))
	for _, id := range order {
		defs = append(defs, byID[id])
	}
	return defs
}

// ParseScenarios returns the criterion-shaped scenario payload embedded in an
// epic op. First-class scenario records use parseScenarioRecords below so their
// lifecycle and source position do not widen the embedded criterion contract.
func ParseScenarios(recordText, epicID string) []any {
	return scenarioItems(parseScenarioDefs(recordText), epicID, false)
}

func parseScenarioRecords(recordText, epicID string) []any {
	return scenarioItems(parseScenarioDefs(recordText), epicID, true)
}

func scenarioItems(defs []*scenarioDef, epicID string, includeSource bool) []any {
	scenarios := make([]any, 0, len(defs))
	for _, def := range defs {
		text := def.text
		c := map[string]any{
			"external_id": epicID + "#" + def.id,
			"position":    len(scenarios) + 1,
			"kind":        "scenario",
		}
		if includeSource {
			c["status"] = def.status
			c["source_line"] = def.line
			c["source_raw"] = def.raw
			// Record-shape only: an epic's embedded criterion payload has no
			// field for an evidence conclusion, and widening it would put the
			// fact in two places that can disagree.
			if def.conclusion != "" {
				c["evidence_conclusion"] = def.conclusion
			}
		}
		if given, when, then, preamble, ok := splitGWT(text); ok {
			c["given"] = given
			if when != "" {
				c["when"] = when
			}
			c["then"] = then
			// The clauses replace the text they were cut out of, so a title and
			// any narrative that sat in front of them have nowhere else to go.
			if def.title != "" {
				c["title"] = def.title
			}
			rest, refs := preambleFields(preamble, def.title)
			if rest != "" {
				c["statement"] = rest
			}
			// A reference written between the id and the triple declares the
			// requirement this scenario realizes. Only the record form carries
			// it: an epic's criterion payload has no field for it.
			if includeSource && len(refs) > 0 {
				list := make([]any, len(refs))
				for i, r := range refs {
					list[i] = r
				}
				c["declared_refs"] = list
			}
		} else {
			c["statement"] = text
		}
		scenarios = append(scenarios, c)
	}
	return scenarios
}

// fencedLines marks every line that is a fenced-code delimiter or sits inside a
// fence. Fenced text is quoted, not structural: a `## Scenarios` heading in a
// template excerpt opens no section, and an id-led line in a gherkin sample
// defines nothing — while the same fence, inside a scenario's body, is content
// carried verbatim.
func fencedLines(lines []string) []bool {
	in := make([]bool, len(lines))
	open := false
	for i, l := range lines {
		if codeFenceRe.MatchString(l) {
			in[i] = true
			open = !open
			continue
		}
		in[i] = open
	}
	return in
}

func startsWithSpace(s string) bool {
	return s != "" && (s[0] == ' ' || s[0] == '\t')
}

// scenarioText picks the scenario cell: the named text column when the header
// has one, else the cell that declares a clause triple, else the longest.
//
// Length was the only fallback, and it is the wrong one whenever a row carries
// an evidence or RED note longer than the scenario itself — the note was then
// stored AS the scenario and the declared triple survived only in the row's
// source text.
func scenarioText(header, cells []string, idAt int) string {
	for _, want := range textColumns {
		for i, h := range header {
			if i != idAt && i < len(cells) && headerKey(h) == want {
				return cells[i]
			}
		}
	}
	for i, c := range cells {
		if i != idAt && declaresTriple(c) {
			return c
		}
	}
	best := ""
	for i, c := range cells {
		if i != idAt && len(c) > len(best) {
			best = c
		}
	}
	return best
}

// headerKey reduces a column heading to its letters, so a header naming the
// triple matches however it punctuates it ("Given / When / Then", "Given-When-Then").
func headerKey(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if unicode.IsLetter(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// scenarioEvidenceConclusion reads the evidence conclusion a row declares
// (REQ-CROSS-266) — from the STATUS-family cell only.
//
// The column matters. 407 rows carry one of these tokens somewhere in their
// text and 23 of them carry it in a Notes or Evidence column, where it is a
// mention of some other item's state ("blocked until EPIC-X is
// UPPER_VALIDATED"), not a declaration of this scenario's. Those keep the
// mention in `source_raw` and claim no conclusion — reading the whole row
// would assert 23 evidence facts the corpus never stated.
func scenarioEvidenceConclusion(header, cells []string) string {
	for i, h := range header {
		if i >= len(cells) || !strings.Contains(strings.ToLower(strings.TrimSpace(h)), "status") {
			continue
		}
		if m := evidenceConclusionRe.FindString(cells[i]); m != "" {
			return m
		}
	}
	return ""
}

// The closed vocabulary PROCESS.md names: current lower evidence for an SR,
// current upper evidence for a UR's acceptance content. Neither is a
// lifecycle state.
var evidenceConclusionRe = regexp.MustCompile(`\b(UPPER_VALIDATED|LOWER_VERIFIED)\b`)

func scenarioLifecycleStatus(header, cells []string) string {
	for i, h := range header {
		if i >= len(cells) || !strings.Contains(strings.ToLower(strings.TrimSpace(h)), "status") {
			continue
		}
		value := strings.ToLower(cells[i])
		switch {
		case strings.Contains(value, "obsolete"), strings.Contains(value, "superseded"):
			return "superseded"
		case strings.Contains(value, "retired"):
			return "retired"
		}
	}
	return "active"
}
