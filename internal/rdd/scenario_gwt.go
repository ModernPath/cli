package rdd

// Splitting a scenario definition into its GIVEN / WHEN / THEN clauses.
//
// The corpus declares the triple in several notations and the parser read one:
// an uppercase run starting at the definition's first character. A requirement
// reference before the triple, the title-cased Gherkin the records fence, and a
// hard-wrapped sentence that lower-cases the joints all fell whole into
// `statement` — a lossless landing, but an unqueryable one (REQ-CROSS-252).
//
// Order matters. Run over a block that writes one clause per line, the inline
// matcher folds every `And` continuation and every `Sources:` annotation into
// whichever clause happens to precede it, so the line-led form is recognised
// first and the inline matcher only ever sees text with no clause lines to lose.

import (
	"regexp"
	"strings"
	"unicode"
)

var (
	// Unanchored and case-insensitive. What precedes GIVEN must be decoration —
	// a requirement reference, a title — never a preceding sentence: see
	// declaresTriple for the bound that keeps ordinary prose out.
	inlineGWTRe = regexp.MustCompile(`(?is)^(.*?)(?:^|[^\pL])given\s+(.+?)(?:[,;\s]+when\s+(.+?))?[,;\s]+then\s+(.+)$`)

	// A clause line: the keyword leads the line, in any case.
	clauseLineRe = regexp.MustCompile(`(?i)^\s*(given|when|then|and|but)\b[:,]?\s+(.*\S)\s*$`)

	// A fence delimiter frames a block and says nothing.
	gherkinFenceRe = regexp.MustCompile("^\\s*(```|~~~)")

	// A Gherkin declaration line. Its keyword is frame, but the title after it
	// is content: a heading block titled "(rewritten)" whose Gherkin names the
	// behavior would lose that name if the whole line were dropped.
	gherkinDeclRe = regexp.MustCompile(`(?i)^\s*(?:scenario(?: outline)?|feature|background):\s*(.*)$`)

	// A completed sentence — the signal that the words are being used rather
	// than declared. A new sentence starts with a capital; an abbreviation
	// mid-clause ("incl. injected contradictions", "e.g. a drift report") does
	// not, and treating its period as a sentence end refused a canonical
	// uppercase triple that only happened to abbreviate a word.
	sentenceBreakRe = regexp.MustCompile(`[.!?](\s+\p{Lu}|$)`)
)

// A prefix longer than this is prose the triple sits inside, not decoration in
// front of it. The corpus's real prefixes are a bracketed requirement reference
// or a scenario title.
const gwtPrefixMax = 160

// splitGWT returns the clause triple a definition declares, plus whatever ran
// ahead of it — a title, an author's narrative, a requirement reference. ok is
// false when the text declares no triple, which is the signal to keep it whole
// as a statement.
//
// The preamble is returned rather than discarded because splitting must not
// cost content: the clauses replace the text they were cut out of, so prose
// that was never part of a clause has to be handed back to a column that keeps
// it.
func splitGWT(text string) (given, when, then, preamble string, ok bool) {
	if g, w, t, pre, ok := splitClauseLines(text); ok {
		return g, w, t, pre, true
	}
	return splitInlineTriple(text)
}

// declaresTriple reports whether a run of text declares a clause triple. Used
// to pick the scenario cell out of a table row whose header names no text
// column.
func declaresTriple(text string) bool {
	_, _, _, _, ok := splitGWT(text)
	return ok
}

// splitClauseLines reads the Gherkin notation: one clause per line, `And`/`But`
// continuing the clause above, and wrapped lines and `Sources:` annotations
// belonging to the clause they follow.
func splitClauseLines(text string) (string, string, string, string, bool) {
	var (
		clauses  [3][]string
		preamble []string
		cur      = -1
		led      int
	)
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || gherkinFenceRe.MatchString(line) {
			continue
		}
		if m := gherkinDeclRe.FindStringSubmatch(line); m != nil {
			if title := declarationTitle(line, m[1]); title != "" {
				if cur >= 0 {
					clauses[cur] = append(clauses[cur], title)
				} else {
					preamble = append(preamble, title)
				}
			}
			continue
		}
		m := clauseLineRe.FindStringSubmatch(line)
		if m == nil {
			// A wrapped clause or an annotation under one; ahead of the first
			// clause, the title or the author's narrative.
			if cur >= 0 {
				clauses[cur] = append(clauses[cur], trimmed)
			} else {
				preamble = append(preamble, trimmed)
			}
			continue
		}
		switch strings.ToLower(m[1]) {
		case "given":
			cur, led = 0, led+1
		case "when":
			cur, led = 1, led+1
		case "then":
			cur, led = 2, led+1
		default: // and / but — continues the clause it follows, keyword included
			if cur >= 0 {
				clauses[cur] = append(clauses[cur], trimmed)
			}
			continue
		}
		clauses[cur] = append(clauses[cur], m[2])
	}
	// One clause line is a sentence that happens to open with the keyword; a
	// block declares its triple line by line.
	if led < 2 || len(clauses[0]) == 0 || len(clauses[2]) == 0 {
		return "", "", "", "", false
	}
	return strings.Join(clauses[0], "\n"),
		strings.Join(clauses[1], "\n"),
		strings.Join(clauses[2], "\n"),
		strings.Join(preamble, "\n"),
		true
}

// splitInlineTriple reads the triple written as one run of text.
func splitInlineTriple(text string) (string, string, string, string, bool) {
	m := inlineGWTRe.FindStringSubmatch(text)
	if m == nil {
		return "", "", "", "", false
	}
	prefix := strings.TrimSpace(m[1])
	if len(prefix) > gwtPrefixMax {
		return "", "", "", "", false
	}
	// Everything ahead of THEN has to read as one declaration. A sentence that
	// ends in there means the paragraph merely uses the words — "the resolver
	// was given a stale id. The page then renders the empty state" declares no
	// scenario, and cutting it into clauses would invent one.
	if sentenceBreakRe.MatchString(prefix + " " + m[2] + " " + m[3]) {
		return "", "", "", "", false
	}
	return strings.TrimSpace(m[2]), strings.TrimSpace(m[3]), strings.TrimSpace(m[4]), prefix, true
}

// preambleFields sorts what a definition said outside its clauses and outside
// its title — nothing, most of the time. A run of pure cross-references is a
// declaration, not prose: "- **SCN-SY-004** (REQ-PLN-045) — GIVEN …" names the
// requirement the scenario realizes, in the position another record writes as a
// Realizes column, so it belongs in the declared requirement list rather than
// in a prose column or nowhere.
func preambleFields(preamble, title string) (statement string, refs []string) {
	// A record commonly writes its title twice — once on the heading, once on
	// the Gherkin declaration inside it. Keeping the title once is the whole
	// point of the title column; repeating it in a prose column is not content.
	seen := map[string]bool{}
	if title != "" {
		seen[title] = true
	}
	var kept []string
	for _, line := range strings.Split(preamble, "\n") {
		line = strings.TrimSpace(scnLeadSepRe.ReplaceAllString(strings.TrimSpace(line), ""))
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		kept = append(kept, line)
	}
	rest := strings.TrimSpace(strings.Join(kept, "\n"))
	if rest == "" {
		return "", nil
	}
	if referencesOnly(rest) {
		return "", refIDRe.FindAllString(rest, -1)
	}
	return rest, nil
}

var refIDRe = regexp.MustCompile(`\b[A-Z]{2,6}-[A-Z0-9]+-\d+[a-z]?\b`)

func referencesOnly(s string) bool {
	rest := refIDRe.ReplaceAllString(s, "")
	return strings.TrimFunc(rest, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("()[]{},;:.·—–-/|", r)
	}) == ""
}

// declarationTitle is the name a Gherkin declaration gives its scenario, with
// the id prefix the records write ahead of it removed — the same title the
// fenced-definition path takes from the line.
func declarationTitle(line, rest string) string {
	if m := fencedScenarioDeclRe.FindStringSubmatch(line); m != nil {
		rest = m[2]
	}
	return strings.TrimSpace(scnLeadSepRe.ReplaceAllString(strings.TrimSpace(rest), ""))
}
