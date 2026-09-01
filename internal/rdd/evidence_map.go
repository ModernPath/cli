package rdd

// REQ-CROSS-250 (EPIC-CLI-003 T12): the clause↔test edge. Epic evidence-map
// rows name a scenario (single id, `..` range, `/` list) and the tests that
// prove it; each resolves to `verification_refs` on that scenario's criterion
// payload — the edge PROCESS.md's invalidation cascade walks. A row whose
// first cell names no scenario resolves to nothing and is REPORTED by the
// landing-home group, never silently dropped.
//
// REQ-CROSS-267 (P8): a record writes those rows in more than one place. Of
// the corpus's 471 SCN-led evidence rows, 117 sit in an evidence-map section
// and 354 sit outside — 248 of them in the BDD-scenario tables that carry a
// "Passing evidence" column beside the definition. The section gate is
// therefore an explicit UNION of the two heading families, and the column
// vocabulary decides what in a row is evidence at all.

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	scnTokenRe = regexp.MustCompile(`\bSCN-[A-Z][A-Z0-9]*-\d+[a-z]?\b`)
	scnRangeRe = regexp.MustCompile(`\b(SCN-[A-Z][A-Z0-9]*-)(\d+)\.\.(\d+)\b`)
	scnListRe  = regexp.MustCompile(`\b(SCN-[A-Z][A-Z0-9]*-\d+)((?:/\d+)+)\b`)
	emapTickRe = regexp.MustCompile("`([^`]+)`")
	// The second half of the carrying union: a scenario-definition heading.
	// Measured `RUN:2026-08-24`, these carry 343 of the 354 ref-carrying rows
	// outside the evidence maps.
	scenarioDefinitionHeadRe = regexp.MustCompile(`(?i)^##+\s+.*scenario`)
	// Column-header words, for the evidence-family vocabulary below.
	headerWordRe = regexp.MustCompile(`[a-z0-9]+`)
)

// evidenceCarryingSection reports whether a heading line opens a section whose
// table rows carry a scenario's evidence.
//
// The union is written out here on purpose. It cannot be delegated to the
// scenario carrier's scenarioSectionHeading/excludedSectionHeading pair: that
// vocabulary excludes any heading containing "evidence" — it exists to keep
// citation tables out of the scenario census — and reusing it would throw away
// `## Evidence map` itself, the 106 rows this carrier already resolves.
func evidenceCarryingSection(headingLine string) bool {
	return evidenceMapHeadRe.MatchString(headingLine) ||
		scenarioDefinitionHeadRe.MatchString(headingLine)
}

// evidenceRow is one data row of one table in a record: the shape an evidence
// map, a BDD-scenario table, a trace roll-up and a coverage matrix all share.
// The section it sits in and the columns it fills are the only things that
// separate a row carrying evidence from a row citing an id.
type evidenceRow struct {
	Line      int                 // 1-based line in the record — how the report locates it
	Cell      string              // the first cell, verbatim
	SCNs      []string            // every scenario the first cell names
	Refs      []map[string]string // typed {kind, ref}, evidence-family columns only
	Ticked    bool                // some cell past the first carries a backticked span
	InMap     bool                // inside an evidence-map section
	InCarrier bool                // inside any carrying section (the union)
}

// evidenceRowsOf walks a record's tables once. Every data row comes back with
// its location and its section, so the parser and the census instrument can
// never disagree about what a row is.
func evidenceRowsOf(recordText string) []evidenceRow {
	var out []evidenceRow
	inMap, inCarrier := false, false
	var header []string
	for i, line := range strings.Split(recordText, "\n") {
		if strings.HasPrefix(line, "#") {
			inMap = evidenceMapHeadRe.MatchString(line)
			inCarrier = evidenceCarryingSection(line)
			header = nil
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			// a table ends at the first line that is not one of its rows; the
			// next table in the same section labels its own columns
			header = nil
			continue
		}
		cells := decisionCells(trimmed)
		if len(cells) == 0 || decisionSeparatorRow(cells) {
			continue
		}
		if header == nil {
			header = lowerCells(cells) // the first non-separator row labels the columns
			continue
		}
		row := evidenceRow{
			Line:      i + 1,
			Cell:      cells[0],
			SCNs:      expandSCNRefs(cells[0]),
			InMap:     inMap,
			InCarrier: inCarrier,
		}
		for col, cell := range cells[1:] {
			spans := emapTickRe.FindAllStringSubmatch(cell, -1)
			if len(spans) > 0 {
				row.Ticked = true
			}
			// TYPED by its column — an untyped column carries no evidence, so
			// it yields no ref at all
			kind := evidenceRefKind(header, col+1)
			if kind == "" {
				continue
			}
			for _, m := range spans {
				row.Refs = append(row.Refs, map[string]string{"kind": kind, "ref": strings.TrimSpace(m[1])})
			}
		}
		out = append(out, row)
	}
	return out
}

// evidenceMapResult is one record's parsed evidence rows: refs per scenario
// id, plus the evidence-map row ledger the named coverage instrument reads.
type evidenceMapResult struct {
	refsBySCN  map[string][]map[string]string // typed {kind, ref} — the citation shape
	rows       int                            // data rows inside the evidence-map sections
	resolved   int
	unresolved []string // the first cell of each evidence-map row that resolved to nothing
}

// ParseEvidenceMapRefs reads every carrying section's data rows.
func ParseEvidenceMapRefs(recordText string) evidenceMapResult {
	res := evidenceMapResult{refsBySCN: map[string][]map[string]string{}}
	for _, row := range evidenceRowsOf(recordText) {
		if row.InCarrier && len(row.SCNs) > 0 && len(row.Refs) > 0 {
			for _, scn := range row.SCNs {
				res.refsBySCN[scn] = append(res.refsBySCN[scn], row.Refs...)
			}
		}
		// the named evidence-map group keeps its own population: the rows the
		// maps themselves write, resolved or not
		if !row.InMap {
			continue
		}
		res.rows++
		if len(row.SCNs) == 0 || len(row.Refs) == 0 {
			res.unresolved = append(res.unresolved, row.Cell)
			continue
		}
		res.resolved++
	}
	return res
}

// evidenceRowCensus is REQ-CROSS-267's instrument. Its denominator is
// corpus-defined — every SCN-led row that sits in an evidence map or carries a
// backticked ref, 471 of them at this revision — so it measures the gate
// instead of restating it, and its coverage is the refs that actually reach a
// criterion. A row that reaches none is named by file and line.
func evidenceRowCensus(data Data, records map[string]string, ops []Op) FidelityCount {
	count := FidelityCount{Group: homeVerifRowsGroup}
	landed := criterionRefIndex(ops)
	for _, e := range data.Epics {
		text := records[e.ID]
		if text == "" {
			continue
		}
		rel := e.Record
		if rel == "" {
			rel = e.RecordFS
		}
		for _, row := range evidenceRowsOf(text) {
			if len(row.SCNs) == 0 || !(row.InMap || row.Ticked) {
				continue
			}
			count.Rows++
			if evidenceRowLanded(landed, e.ID, row) {
				count.Ops++
				continue
			}
			count.MissingFromOps = append(count.MissingFromOps,
				fmt.Sprintf("%s:%d %s", rel, row.Line, row.Cell))
		}
	}
	return count
}

// criterionRefIndex is the refs each emitted criterion carries, keyed by the
// scenario's external id — the landing side of the census.
func criterionRefIndex(ops []Op) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, op := range ops {
		if op.Type != "upsert_epic" {
			continue
		}
		items, _ := op.Payload["scenarios"].([]any)
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["external_id"].(string)
			refs, _ := item["verification_refs"].([]any)
			set := map[string]bool{}
			for _, entry := range refs {
				if m, ok := entry.(map[string]any); ok {
					if ref, _ := m["ref"].(string); ref != "" {
						set[ref] = true
					}
				}
			}
			out[id] = set
		}
	}
	return out
}

// evidenceRowLanded reports whether any scenario the row names carries any of
// the row's refs on its criterion.
func evidenceRowLanded(index map[string]map[string]bool, epicID string, row evidenceRow) bool {
	for _, scn := range row.SCNs {
		carried := index[epicID+"#"+scn]
		for _, ref := range row.Refs {
			if carried[ref["ref"]] {
				return true
			}
		}
	}
	return false
}

// expandSCNRefs reads every scenario a row's first cell names: bare ids,
// `SCN-X-001..004` ranges, and `SCN-X-001/002` lists.
func expandSCNRefs(cell string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	rest := cell
	for _, m := range scnRangeRe.FindAllStringSubmatch(cell, -1) {
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
	for _, m := range scnListRe.FindAllStringSubmatch(rest, -1) {
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
	for _, id := range scnTokenRe.FindAllString(rest, -1) {
		add(id)
	}
	return out
}

func atoiOr(s string, fallback int) int {
	n := 0
	if s == "" {
		return fallback
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return fallback
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// evidenceRefKind types a column by its header: code columns carry code
// references, test/evidence columns carry tests — and a column in neither
// family carries no reference at all.
//
// The vocabulary is measured, not guessed: every header the corpus's carrying
// sections put a backticked span under is one of these words. The empty return
// is the guard — a token in a Summary, Statement, Gherkin or Status column is
// prose, an id or a lifecycle token, and calling it a `note` reference put 176
// of them into the landing model from one table shape alone.
//
// Whole words, not substrings: "required" contains "red", and a Required
// column is not evidence of anything.
func evidenceRefKind(header []string, col int) string {
	name := ""
	if col < len(header) {
		name = header[col]
	}
	code, evidence := false, false
	for _, word := range headerWordRe.FindAllString(strings.ToLower(name), -1) {
		switch word {
		case "code":
			code = true
		case "evidence", "test", "tests", "e2e", "red", "green", "result", "results",
			"report", "revision", "run", "proof", "verification", "upper", "lower", "command":
			evidence = true
		}
	}
	switch {
	case code:
		return "code"
	case evidence:
		return "test"
	}
	return ""
}
