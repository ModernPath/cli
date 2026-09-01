package rdd

// REQ-CROSS-247 (EPIC-CLI-003 T11): every corpus D-* decision imports as one
// decision_gates row — kind decision, answered and applied, answer the
// decision text, consequence carried in the body, sources resolving the
// record's SRC-*/USER: references, exact scope the owning epic and named
// items. Gate external ids are minted epic-scoped from the verbatim local
// suffix (`D-<epic-code>-<suffix>`), so records using bare local numbers
// cannot collide across epics and letter families (D-061a/b) keep their
// identity. A row carrying both a register id (DEC-*) and a local D-* mints
// from the local id; the register id lands in sources.

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// A declaration id is either a local D-* or a register DEC-*. The register
	// family had no matcher at all, so 281 declarations across 63 epics minted
	// nothing and the coverage group could not see it — its denominator counts
	// the same shape the builder matches.
	// A trailing parenthetical is how this corpus annotates a declaration it
	// is revising — `| DEC-AD-001 (REVISED in rework) |`. Without it the row
	// read as prose and minted nothing, which is what the widened coverage
	// denominator exposed (432 declarations, 431 gates). Only a parenthetical:
	// arbitrary trailing prose would let a sentence that merely mentions an id
	// mint a gate.
	decisionCellIDRe = regexp.MustCompile("^`?\\*{0,2}((?:DEC|D)-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)\\*{0,2}`?(?:\\s*\\([^)]*\\))?$")
	decisionBulletDeclRe = regexp.MustCompile(`^-\s+\*\*((?:DEC|D)-[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*)\*\*\s*(?:[—–-]\s*)?(.*)$`)
	srcRegisterRefRe     = regexp.MustCompile(`\bSRC-[A-Z0-9-]+\b`)
	decRegisterIDRe      = regexp.MustCompile(`\bDEC-[A-Z0-9-]+\b`)
	sourceHeadRe         = regexp.MustCompile(`(?i)^##+\s+.*source`)
	codeTagTokenRe       = regexp.MustCompile("(?:CODE:|DOC:|TEST:)?`?([A-Za-z0-9_./-]+\\.[A-Za-z0-9]{1,4})(?:[:#][A-Za-z0-9_:-]+)?`?")
)

type decisionDecl struct {
	localID     string
	decision    string
	question    string
	source      string
	consequence string
}

// BuildDecisionGateOps emits one answered, applied decision gate per D-*
// declaration in the record's decision-family sections.
func BuildDecisionGateOps(epic Epic, recordText string) []Op {
	decls := parseDecisionDecls(recordText)
	if len(decls) == 0 {
		return nil
	}
	register := parseSourceRegister(recordText)
	epicCode := strings.TrimPrefix(epic.ID, "EPIC-")
	originRef := epic.Record
	if originRef == "" {
		originRef = epic.ID
	}

	seen := map[string]bool{}
	var ops []Op
	for _, d := range decls {
		// Epic-scoped for both families. A DEC-* reads as a globally unique
		// register id and is not: seven collide across epic pairs in this
		// corpus (DEC-SD-001, DEC-TM-001/002, DEC-RA-001/002 …), so minting
		// verbatim would let one epic's declaration overwrite another's with
		// no error. The verbatim id is preserved in sources and in the body.
		// One prefix, never two. Chaining the trims would reduce `DEC-D-001`
		// — a legal declaration, since the recognizing regex accepts a
		// single-letter area code — to `001`, colliding it with `DEC-001` in
		// the same epic and diverging from the node mirror's single anchored
		// alternation. Same order as that alternation: DEC- first.
		suffix := d.localID
		if strings.HasPrefix(suffix, "DEC-") {
			suffix = strings.TrimPrefix(suffix, "DEC-")
		} else {
			suffix = strings.TrimPrefix(suffix, "D-")
		}
		minted := "D-" + epicCode + "-" + suffix
		if seen[minted] {
			continue // one declaration, one gate — a summary repeating it adds nothing
		}
		seen[minted] = true

		var body strings.Builder
		if d.question != "" {
			fmt.Fprintf(&body, "Question — %s\n\n", d.question)
		}
		fmt.Fprintf(&body, "Decision — %s", d.decision)
		if d.consequence != "" {
			fmt.Fprintf(&body, "\n\nConsequence — %s", d.consequence)
		}
		// A bullet declaration passes its own text as `source` so the tokens in
		// it resolve; rendering that as `Source — <the decision>` made all 30
		// bullet-declared gates cite themselves. A table with no source column
		// simply omits the line, which is the honest shape.
		if d.source != "" && strings.TrimSpace(d.source) != strings.TrimSpace(d.decision) {
			fmt.Fprintf(&body, "\n\nSource — %s", d.source)
		}
		fmt.Fprintf(&body, "\n\nRecorded as %s in %s.", d.localID, originRef)

		payload := map[string]any{
			"external_id": minted,
			"kind":        "decision",
			"title":       decisionTitle(d.decision),
			"body_md":     capRunes(body.String(), 6000),
			"origin":      "workspace",
			"origin_ref":  originRef,
			"state":       "answered",
			"answer":      capRunes(d.decision, 1200),
			// a decision row records an already-applied human decision — it
			// never re-opens as a pending intent
			"applied_state": "applied",
		}

		tagSpace := d.source + " " + d.decision + " " + d.consequence
		if tag := userTagRe.FindString(tagSpace); tag != "" {
			payload["opened_at"] = tag[5:] + "T00:00:00.000000Z"
			payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
			payload["source_tag"] = tag
		} else {
			payload["opened_at"] = "2026-07-01T00:00:00.000000Z"
			payload["answered_at"] = "2026-07-01T00:00:00.000000Z"
			payload["source_tag"] = "DOC:" + originRef
		}

		// resolveDecisionSources reads the source text; a register-only row
		// carries its id in the id cell instead, so name it explicitly —
		// after the flip this is the only route back to the declaration.
		sourceText := d.source
		if strings.HasPrefix(d.localID, "DEC-") && !strings.Contains(sourceText, d.localID) {
			sourceText = strings.TrimSpace(sourceText + " " + d.localID)
		}
		if sources := resolveDecisionSources(sourceText, register); len(sources) > 0 {
			payload["sources"] = sources
		}

		scope := []any{epic.ID}
		for _, id := range idMentions(d.decision+" "+d.consequence, "REQ", "EPIC") {
			if id != epic.ID {
				scope = append(scope, id)
			}
		}
		payload["exact_scope"] = scope

		payload["content_hash"] = ContentHash(payload)
		payload["actor"] = actor
		payload["answerer"] = map[string]any{"kind": "human"}
		ops = append(ops, Op{Type: "upsert_gate", Payload: payload})
	}
	return ops
}

// parseDecisionDecls reads every declaration in every decision-family
// section: a table row whose id-position cell is a D-* id (columns mapped by
// header name, positional fallback), or a bold-led bullet.
func parseDecisionDecls(recordText string) []decisionDecl {
	var out []decisionDecl
	lines := strings.Split(recordText, "\n")
	inSection := false
	var header []string
	for _, line := range lines {
		if strings.HasPrefix(line, "#") {
			inSection = decisionHeadRe.MatchString(line)
			header = nil
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "|"):
			cells := decisionCells(trimmed)
			if len(cells) == 0 {
				continue
			}
			if decisionSeparatorRow(cells) {
				continue
			}
			if header == nil && !decisionCellIDRe.MatchString(strings.TrimSpace(cells[0])) {
				header = lowerCells(cells)
				continue
			}
			m := decisionCellIDRe.FindStringSubmatch(strings.TrimSpace(cells[0]))
			if m == nil {
				continue
			}
			d := decisionDecl{localID: m[1]}
			// the corpus's live header families: Decision, Resolution
			// (often beside a Question), Statement (beside Kind)
			d.decision = cellByName(header, cells, 1, "decision", "resolution", "statement", "outcome", "answer")
			d.question = cellByName(header, cells, -1, "question")
			d.source = cellByName(header, cells, 2, "source", "source/status", "sources", "source/trigger")
			d.consequence = cellByName(header, cells, 3, "consequence", "chosen", "consequence/status", "impact")
			// A header that understates its columns — `| Decision | Consequence |`
			// above three-cell rows — makes "decision" match index 0, the ID
			// cell, and every column reads one to the left. The id is never the
			// decision, so fall back to the positions the shape rule defines.
			// After the name-based mapping, not before: placed earlier these
			// three were immediately overwritten by it.
			if decisionCellIDRe.MatchString(strings.TrimSpace(d.decision)) {
				d.decision = cell(cells, 1)
				d.source = cell(cells, 2)
				d.consequence = cell(cells, 3)
			}
			if d.decision != "" {
				out = append(out, d)
			}
		default:
			if m := decisionBulletDeclRe.FindStringSubmatch(trimmed); m != nil {
				// `source` carries the bullet text so its USER:/SRC:/doc tokens
				// resolve into sources; it is not a citation of the decision,
				// and the body must not render it as one.
				out = append(out, decisionDecl{localID: m[1], decision: strings.TrimSpace(m[2]), source: m[2]})
			}
		}
	}
	return out
}

// parseSourceRegister maps the record's SRC-* register ids to their row text,
// so a decision citing SRC-CV-001 resolves to what the register actually
// names.
func parseSourceRegister(recordText string) map[string]string {
	register := map[string]string{}
	inSection := false
	for _, line := range strings.Split(recordText, "\n") {
		if strings.HasPrefix(line, "#") {
			inSection = sourceHeadRe.MatchString(line)
			continue
		}
		if !inSection {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := decisionCells(trimmed)
		if len(cells) == 0 || decisionSeparatorRow(cells) {
			continue
		}
		if id := srcRegisterRefRe.FindString(cells[0]); id != "" {
			register[id] = strings.Join(cells, " · ")
		}
	}
	return register
}

// resolveDecisionSources types each token of a decision's source cell:
// USER tags, register ids resolved through the SRC map to the citations they
// name, DEC-* register ids preserved, paths as doc references, anything else
// as a note — never dropped in silence.
func resolveDecisionSources(sourceCell string, register map[string]string) []any {
	var out []any
	seen := map[string]bool{}
	add := func(kind, ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[kind+"|"+ref] {
			return
		}
		seen[kind+"|"+ref] = true
		out = append(out, map[string]any{"kind": kind, "ref": capRunes(ref, 300)})
	}
	var addTokens func(text string, viaRegister bool)
	addTokens = func(text string, viaRegister bool) {
		for _, tag := range userTagRe.FindAllString(text, -1) {
			add("user", tag)
		}
		for _, dec := range decRegisterIDRe.FindAllString(text, -1) {
			add("note", dec)
		}
		for _, m := range codeTagTokenRe.FindAllStringSubmatch(text, -1) {
			add("doc", m[1])
		}
		if viaRegister {
			return
		}
		for _, src := range srcRegisterRefRe.FindAllString(text, -1) {
			if entry, ok := register[src]; ok {
				addTokens(entry, true)
			} else {
				add("note", src)
			}
		}
	}
	addTokens(sourceCell, false)
	if len(out) == 0 && strings.TrimSpace(sourceCell) != "" {
		add("note", sourceCell)
	}
	return out
}

// decisionCells splits a table row, trimming each cell — the shared
// splitCells in extract.go serves the ledger's escaped-pipe rules; decision
// tables need only the plain split.
// decisionCells splits a table row on its column breaks. A `\|` is an escaped
// pipe — content, not a break — and splitting on it tore two corpus decisions
// in half, rendering the decision's own tail as its Source. The escape is
// removed after the split, so the cell reads as the author wrote it.
// decisionTitle caps the display title at 200 units and says so when it cuts.
// The column is unbounded text and the contract sets no limit, so 200 is a
// display choice — a 600-character "title" is not a title — but a silent cut
// is not honest: 145 of 463 gates sat at exactly 200 characters, mid-word,
// indistinguishable from a decision that simply ended there. The full text is
// always in body_md. Cuts at a word boundary when one is near.
func decisionTitle(decision string) string {
	d := strings.TrimSpace(decision)
	if utf16Len(d) <= 200 {
		return d
	}
	head := capRunes(d, 199)
	if i := strings.LastIndexAny(head, " \t"); i > 120 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \t") + "…"
}

func decisionCells(row string) []string {
	body := strings.Trim(strings.TrimSpace(row), "|")
	var parts []string
	var cur strings.Builder
	for i := 0; i < len(body); i++ {
		if body[i] == '\\' && i+1 < len(body) && body[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if body[i] == '|' {
			parts = append(parts, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(body[i])
	}
	parts = append(parts, cur.String())
	out := make([]string, len(parts))
	for i, p := range parts {
		out[i] = strings.TrimSpace(p)
	}
	return out
}

func decisionSeparatorRow(cells []string) bool {
	for _, c := range cells {
		if strings.Trim(c, "-: ") != "" {
			return false
		}
	}
	return true
}

func lowerCells(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = strings.ToLower(strings.TrimSpace(c))
	}
	return out
}

// cellByName reads the cell whose header matches one of the names, falling
// back to the positional index when the table has no recognized header.
func cellByName(header, cells []string, fallback int, names ...string) string {
	if header != nil {
		for i, h := range header {
			for _, n := range names {
				if h == n && i < len(cells) {
					return cells[i]
				}
			}
		}
		return ""
	}
	if fallback >= 0 && fallback < len(cells) {
		return cells[fallback]
	}
	return ""
}
