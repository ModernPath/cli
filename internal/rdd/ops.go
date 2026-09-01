// Op-builder — originally a Go port of a node op-builder (REQ-CROSS-013,
// TASK-SY-404), now the only one. Same op shapes, same ordering, same content
// hashes as that port produced: canonical
// JSON (sorted keys, JS JSON.stringify string escaping) sha256'd — so the
// switch from the node extractor is hash-stable and causes zero re-sync churn.
package rdd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Actor: P2 (attributable actors) — every sync write names its agent.
// Kept "mp-cli" for hash parity with the node op-builder.
var actor = map[string]any{"kind": "agent", "agent_slug": "mp-cli"}

// Op is one typed sync op. Payloads are map[string]any trees of
// string | int | nil | []any | map[string]any.
type Op struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// ---------------------------------------------------------------- canonical hash

// canonical mirrors ops.js canonical(): arrays in order, object keys sorted,
// scalars JSON-encoded the way JS JSON.stringify does.
func canonical(v any) string {
	switch val := v.(type) {
	case nil:
		return "null"
	case string:
		return jsString(val)
	case int:
		return strconv.Itoa(val)
	case float64:
		// JS JSON.stringify prints the shortest round-trip decimal; Go's 'g'
		// with -1 precision is the same algorithm for the plain range. The
		// only float the builders emit is a 0–100 percentage rounded to one
		// decimal (SR-CMP-9022), so exponent forms never arise.
		return strconv.FormatFloat(val, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(val)
	case []any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []string:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case []map[string]any:
		parts := make([]string, len(val))
		for i, item := range val {
			parts[i] = canonical(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = jsString(k) + ":" + canonical(val[k])
		}
		return "{" + strings.Join(parts, ",") + "}"
	default:
		panic(fmt.Sprintf("canonical: unsupported type %T", v))
	}
}

// jsString = JSON.stringify(s): escape `"` `\` and control chars only
// (no HTML escaping, no   handling — matching V8 for BMP text).
func jsString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// ContentHash = ops.js contentHash: sha256 hex over the canonical form.
func ContentHash(payload map[string]any) string {
	sum := sha256.Sum256([]byte(canonical(payload)))
	return hex.EncodeToString(sum[:])
}

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

// REQ-CROSS-051 (EPIC-SYNC-011): evidence is read from the RECORD, not from the
// summary cell. The detail block is what the author wrote out; the row cell is
// whatever fitted in a table. On a real system the difference is 2,322 file
// references against 1,225, leaving 598 of 874 rows poorer on the platform than
// in the repository (`RUN:2026-08-12`).
var (
	// A bullet starts a field; everything until the next bullet belongs to it.
	// This is a split rather than a match with a trailing terminator, because
	// Go's RE2 has no lookahead: a terminator-consuming match eats the start of
	// the next bullet, and consecutive "- **Evidence:**" / "- **Code:**" lines —
	// exactly the shape every reverse-engineered ledger writes — would read as
	// one. That silently dropped every Code bullet (`RUN:2026-08-12`).
	detailBulletRe  = regexp.MustCompile(`(?m)^\s*-\s+\*\*`)
	evidenceLabelRe = regexp.MustCompile(`^(Evidence|Tests?|Code):\*\*\s*([\s\S]*)$`)
	emptyTicksRe    = regexp.MustCompile("``+")
	// Path-shaped: a slash or a known source extension, optionally suffixed
	// with :line, :line-line or :line,line. This is the line between a
	// reference and a word in a sentence — `AsyncMock` is a class being
	// described, `tests/test_admin.py:470-475` is a place to look.
	// Longer extensions first: Go's regexp alternation is leftmost-FIRST, so
	// `ts|tsx` truncates MetricsPage.tsx:91-104 to "MetricsPage.ts" — a
	// citation pointing at a file that does not exist (`RUN:2026-08-12`).
	pathRefRe = regexp.MustCompile(`(?:[\w.@/-]+/)?[\w.@-]+\.(?:tsx|ts|jsx|js|exs|ex|heex|eex|scss|css|go|py|rb|rs|java|kt|cs|sql|md|yaml|yml|json|toml|sh|html|vue|svelte)(?::\d+(?:[-,]\d+)*)*`)
)

// SR-SY-1401 (EPIC-SYNC-014): the reference semantics of cmd/coverage.go
// (REQ-CROSS-179), ported so a citation leaves the workspace as the exact
// cited path the server can match against FileAnalysis.relative_path.
//
//   - a backtick-wrapped citation is its exact content: the CODE:/TEST: tag
//     names the axis and comes off, and ONE trailing :line/:range suffix —
//     comma lists included (`:16-35,382-387`) — folds off, because the suffix
//     locates the rule inside the file and is not part of the file's identity;
//   - a bare CODE:/TEST: token stops at whitespace, a backtick, ',' or ';' —
//     NOT at ')': Next.js route groups put balanced parens inside real paths
//     (`src/app/[locale]/(app)/…/page.tsx`), and stopping there truncated them
//     into refs that resolve NOWHERE. A trailing ')' closing a prose paren is
//     trimmed afterwards by trimUnbalancedParens;
//   - an untagged bare token still goes through pathRefRe, so a path inside a
//     backticked command (`mix test apps/…/foo_test.exs`) is not lost.
var (
	citeTagPrefixRe = regexp.MustCompile(`^(CODE|TEST):`)
	citeBareTagRe   = regexp.MustCompile("(CODE|TEST):([^\\s`,;]+)")
	// One trailing :suffix — a line (`:12`, `:157+`), a range (`:10-20`), a
	// comma-separated list of either, or a lowercase symbol.
	citeSuffixRe = regexp.MustCompile(`:(\d+(?:\+|-\d+)?(?:,\d+(?:\+|-\d+)?)*|[a-z]+)$`)
	// Path-shaped, for exact backtick spans: no whitespace, and a separator or
	// a known source extension. `create_consortium` is a word in a sentence;
	// `tests/test_admin.py` is a place to look.
	citeExtRe = regexp.MustCompile(`\.(?:tsx|ts|jsx|js|mjs|cjs|exs|ex|heex|eex|scss|css|go|py|rb|rs|java|kt|cs|sql|md|yaml|yml|json|toml|sh|html|vue|svelte|prisma)$`)
	// A bare Go-style test identifier — `TestXxx`, optionally with a t.Run
	// subtest path after '/' — is cited by name alone, with no path or
	// extension: Go convention makes the exported function name itself the
	// stable identity (measured in the corpus: `TestSyncFamilyPartialInstall...`,
	// `TestSessionLogout/{...}`).
	bareGoTestNameRe = regexp.MustCompile(`^Test[A-Z]\w*`)
)

// citeRef is one extracted citation: the whole path, and the axis its tag
// pins — "code", "test", or "" when untagged (the bullet label decides).
type citeRef struct{ ref, kind string }

func trimUnbalancedParens(token string) string {
	for strings.HasSuffix(token, ")") && strings.Count(token, ")") > strings.Count(token, "(") {
		token = token[:len(token)-1]
	}
	return token
}

func citePathShaped(token string) bool {
	return token != "" && !strings.ContainsAny(token, " \t") &&
		(strings.Contains(token, "/") || citeExtRe.MatchString(token) || bareGoTestNameRe.MatchString(token))
}

// extractCitationRefs reads one evidence bullet's body. It returns the refs in
// reading order — exact backtick spans, then bare tagged tokens, then bare
// paths — and the body with every extracted span masked out, which is what the
// note is built from: prose survives, citations do not repeat as prose.
func extractCitationRefs(body string) ([]citeRef, string) {
	var out []citeRef

	// Backtick spans are exact. An extracted span masks to "``" so the note
	// side drops it (emptyTicksRe) and the bare-token scan cannot re-read it;
	// a non-path span (`create_consortium`, a command) stays for both.
	masked := backtickRefRe.ReplaceAllStringFunc(body, func(span string) string {
		token := strings.TrimSpace(span[1 : len(span)-1])
		if tag := citeTagPrefixRe.FindStringSubmatch(token); tag != nil {
			ref := citeSuffixRe.ReplaceAllString(citeTagPrefixRe.ReplaceAllString(token, ""), "")
			out = append(out, citeRef{ref: ref, kind: strings.ToLower(tag[1])})
			return "``"
		}
		if bare := citeSuffixRe.ReplaceAllString(token, ""); citePathShaped(bare) {
			out = append(out, citeRef{ref: bare})
			return "``"
		}
		return span
	})

	// Bare CODE:/TEST: tokens, read outside the backtick spans.
	for _, m := range citeBareTagRe.FindAllStringSubmatch(masked, -1) {
		ref := citeSuffixRe.ReplaceAllString(trimUnbalancedParens(m[2]), "")
		out = append(out, citeRef{ref: ref, kind: strings.ToLower(m[1])})
	}
	masked = citeBareTagRe.ReplaceAllString(masked, " ")

	// Untagged bare paths — inside surviving command spans and plain prose.
	for _, ref := range pathRefRe.FindAllString(masked, -1) {
		out = append(out, citeRef{ref: citeSuffixRe.ReplaceAllString(ref, "")})
	}
	return out, masked
}

// evidenceRefsFromDetail pulls whole-path references out of the detail block's
// evidence bullets, plus any prose that survives — the sentence "no backend test
// opens a database" is a finding worth reading, and it is not a test.
func evidenceRefsFromDetail(detail string) (code, tests, notes []string) {
	for _, bullet := range detailBulletRe.Split(detail, -1) {
		m := evidenceLabelRe.FindStringSubmatch(bullet)
		if m == nil {
			continue
		}
		label, body := m[1], strings.Join(strings.Fields(m[2]), " ")
		refs, masked := extractCitationRefs(body)

		// The explicit tag names the axis. Untagged: a path under a test
		// directory or named like a test is a test regardless of label. A
		// Code: bullet trusts its own label unconditionally for whatever is
		// left (unchanged — that axis measured clean). Under any other label,
		// an untagged, non-test-shaped ref is not test evidence just because
		// it sat under "Evidence:"/"Tests:" — a bare extension enumerated in
		// prose (`.md`), an MFA/arity mention (`Foo.bar/2`), or a directory
		// fragment is not a citation to anything and is dropped rather than
		// fabricated; a real file (extension with a non-empty name) still
		// becomes code, so the content is not lost.
		for _, r := range refs {
			switch {
			case r.kind == "code":
				code = append(code, r.ref)
			case r.kind == "test":
				tests = append(tests, r.ref)
			case looksLikeTest(r.ref):
				tests = append(tests, r.ref)
			case label == "Code":
				code = append(code, r.ref)
			case looksLikeFileRef(r.ref):
				code = append(code, r.ref)
			}
		}

		// What remains once the citations are removed: keep it only when it
		// says something, and only for the evidence side, where "no test" is
		// the finding the coverage cliff is made of.
		if label != "Code" {
			note := emptyTicksRe.ReplaceAllString(pathRefRe.ReplaceAllString(masked, ""), "")
			note = strings.Join(strings.Fields(note), " ")
			if noteWorthKeeping(note) {
				notes = append(notes, capRunes(strings.Trim(note, " ,;·"), 300))
			}
		}
	}
	return code, tests, notes
}

var testPathRe = regexp.MustCompile(`(?i)(^|/)(tests?|spec|__tests__)/|(^|/)[\w.-]*(_test|_spec|\.test|\.spec|test_)[\w.-]*\.`)

func looksLikeTest(ref string) bool {
	return testPathRe.MatchString(ref) || bareGoTestNameRe.MatchString(ref)
}

// looksLikeFileRef reports whether ref names an actual file: a recognized
// source extension with a non-empty basename before it. ".md" — the extension
// alone, cited bare when a sentence enumerates extensions ("contains only
// `.ex`, `.exs`, …", REQ-SYS-179) — names no file and does not qualify. A
// trailing :line/:range suffix is tolerated and ignored for the shape check
// only (cellEvidenceCitations does not strip one; the detail-block extractor
// already has by the time this runs, so this is a no-op there).
func looksLikeFileRef(ref string) bool {
	bare := citeSuffixRe.ReplaceAllString(ref, "")
	ext := citeExtRe.FindString(bare)
	if ext == "" {
		return false
	}
	base := bare
	if i := strings.LastIndexByte(base, '/'); i >= 0 {
		base = base[i+1:]
	}
	return base != ext
}

// A note is worth keeping when it carries a claim, not leftover punctuation
// from a sentence whose references were removed.
func noteWorthKeeping(note string) bool {
	trimmed := strings.Trim(note, " -—–,;:·()`*")
	return len([]rune(trimmed)) >= 12
}

// evidenceCitations turns a requirement's evidence into citations: the detail
// block's references first, falling back to the Tests and Code columns when the
// block carries none. `source_citations` is an untyped array in the contract, so
// this needs no schema change — the refs simply arrive with a kind the reader
// can act on.
//
// An em-dash or an empty cell means the evidence is absent; that must stay
// absent rather than becoming a citation pointing at "—", because a row with a
// fake citation looks verified and a row with none looks like what it is.
func evidenceCitations(req Req) []any {
	code, tests, notes := evidenceRefsFromDetail(req.Detail)

	var out []any
	if len(code) > 0 || len(tests) > 0 {
		for _, pair := range []struct {
			kind string
			refs []string
		}{{"test", tests}, {"code", code}} {
			for _, ref := range dedupeRefs(pair.refs) {
				out = append(out, map[string]any{"kind": pair.kind, "ref": ref})
			}
		}
	} else {
		out = append(out, cellEvidenceCitations(req)...)
	}
	for _, note := range notes {
		out = append(out, map[string]any{"kind": "note", "ref": note})
	}
	return out
}

func dedupeRefs(refs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// cellEvidenceCitations is the fallback for a row whose detail block says
// nothing about evidence — the summary cell is thinner, but it is what there
// is. Which column a ref came from no longer decides its kind (cellRefKind
// does, from the ref's own shape), so both cells are walked the same way.
func cellEvidenceCitations(req Req) []any {
	var out []any
	for _, cell := range []string{req.Tests, req.Code} {
		for _, ref := range splitEvidenceRefs(cell) {
			out = append(out, map[string]any{"kind": cellRefKind(ref), "ref": ref})
		}
	}
	return out
}

// cellRefKind re-derives a cell-fallback citation's kind from its own shape
// rather than trusting which column it was typed into. The column is a weak
// prior at best: this path only runs when the detail block named no citation
// at all, and a Tests cell often carries a verdict sentence with none either
// ("not verified — no test covers the middleware branch", REQ-AUTH-021) —
// under the old unconditional tagging that became a literal test citation.
// looksLikeTest wins outright; a real file becomes code whichever column it
// rode in on (a non-test file named in the Tests cell is still not a test);
// anything else is prose or a bare word, not a citation to anything, and rides
// as a note instead of a fabricated test/code reference so the text is not
// lost.
func cellRefKind(ref string) string {
	switch {
	case looksLikeTest(ref):
		return "test"
	case looksLikeFileRef(ref):
		return "code"
	default:
		return "note"
	}
}

// splitEvidenceRefs pulls the individual references out of a cell. Ledgers write
// several separated by "·" or ",", often in backticks, and prose around them.
func splitEvidenceRefs(cell string) []string {
	c := strings.TrimSpace(cell)
	if c == "" || c == "—" || c == "-" || c == "–" {
		return nil
	}
	var refs []string
	for _, m := range backtickRefRe.FindAllStringSubmatch(c, -1) {
		if r := strings.TrimSpace(m[1]); r != "" && r != "—" {
			refs = append(refs, r)
		}
	}
	if refs == nil {
		// No backticks: take the cell whole, so a bare path is not lost. Prose
		// like "no test" is still a statement about evidence and worth carrying.
		refs = append(refs, capRunes(strings.Join(strings.Fields(c), " "), evidenceRefCap))
	}
	return refs
}

var backtickRefRe = regexp.MustCompile("`([^`]+)`")

// The named extras that carry the evidence cells verbatim (REQ-CROSS-222 §6).
// Deliberately not the column headings ("Tests"/"Code"): those name recognized
// columns, and a recognized column carried as an extra is what the report uses
// to prove an UNrecognized one survived.
const (
	rawTestsCellExtra = "tests_raw"
	rawCodeCellExtra  = "code_raw"
	rawURCellExtra    = "ur_raw"
)

// A cell with no backticked span becomes one reference, shortened to this
// many runes. The cap belongs to the reference list only; the cell's own text
// rides the raw carrier uncut, and the report names every cell it shortens.
const evidenceRefCap = 200

// REQ-CROSS-064: an as-built epic says so in its Specification status. Its
// requirements describe behaviour that ALREADY SHIPS, so they belong to the
// base and must never be stamped into a release — 800+ of them clogged the
// active release before this existed (`USER:2026-08-13`).
var specDerivedRe = regexp.MustCompile(`(?i)SPEC-DERIVED`)

func isAsBuiltEpic(recordText string) bool {
	m := specStatusSectionRe.FindStringSubmatch(recordText)
	if m == nil {
		return false
	}
	return specDerivedRe.MatchString(m[1])
}

func BuildRequirementOpWithExemption(req Req, exempt bool) Op {
	op := BuildRequirementOp(req)
	if !exempt {
		return op
	}
	// Re-hash: the exemption is content, not routing — a row that becomes
	// exempt must reach the server as a changed row.
	payload := op.Payload
	delete(payload, "content_hash")
	delete(payload, "actor")
	payload["release_exempt"] = true
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_requirement", Payload: payload}
}

// urTokenRe is the shape a parent reference must have (§245.9). The UI
// ledger writes provenance prose in the UR column of as-built rows
// ("finding RUN:2026-08-12 · fixed RUN:2026-08-20"); reading those cells as
// ids minted parents no corpus record declares.
var urTokenRe = regexp.MustCompile(`\bUR-[A-Z][A-Z0-9]*-\d+[a-z]?\b`)

// firstURRef normalizes a UR cell or detail-line value to the single parent id
// the payload can carry: backticks off, whitespace trimmed, a ` · ` list
// folded to its first entry (SR-SY-1402), and only a UR-shaped token counts
// (§245.9) — decorations around it are dropped, prose yields nothing.
func firstURRef(raw string) string {
	ur := strings.TrimSpace(strings.ReplaceAll(raw, "`", ""))
	if i := strings.Index(ur, "·"); i >= 0 {
		ur = strings.TrimSpace(ur[:i])
	}
	return urTokenRe.FindString(ur)
}

func BuildRequirementOp(req Req) Op {
	// §245.2: a dash Stage cell is absence, exactly like Priority/Owner/Release.
	var stage any
	if s := dashless(req.Stage); s != "" {
		stage = s
	}
	payload := map[string]any{
		"external_id": req.ID,
		// The id's prefix decides this. Hardcoding "system" flattened every
		// user requirement into a system one on the way to the store — a UR
		// owns acceptance scenarios and upper evidence, an SR owns lower
		// evidence, and the server routes to a different table per kind.
		"kind":        RequirementKind(req.ID),
		"title":       req.Title,
		"context":     req.Ctx,
		"stage":       stage,
		"work_status": req.Status,
		"source_citations": append(
			ParseCitations(req.Source),
			evidenceCitations(req)...,
		),
		"criteria":    ParseCriteria(req.Detail, req.ID),
		"description": ParseDescription(req.Detail),
	}
	// The context's human name, when the ledger's H1 gives one. Omitted rather
	// than sent empty, so a workspace that never named its contexts sees no
	// content-hash churn (`USER:2026-08-12`).
	if req.CtxName != "" {
		payload["context_name"] = req.CtxName
	}
	// REQ-CROSS-049: the user requirement this row serves. Omitted rather than
	// sent null when the ledger has no UR column, so workspaces that never had
	// one see no content-hash churn.
	// An em-dash is how a ledger writes "no parent" — the same convention the
	// evidence columns use. Sending it as a literal id made 33 rows on a real
	// system report an unresolved parent that was never claimed (`RUN:2026-08-12`).
	// SR-SY-1402: a line naming several URs (`UR-A · UR-B`) folds to its FIRST —
	// parent_external_id is one edge; the Snapshot warns about the extras.
	if ur := firstURRef(req.UR); ur != "" {
		payload["parent_external_id"] = ur
	}

	// REQ-CROSS-222: full-fidelity capture for the one-time ledger import.
	// parent_external_id above stays the FIRST parent for consumers that read
	// one; the array is the FULL set whenever the row names a parent at all,
	// one included. Sending it only for multi-parent rows made absence
	// ambiguous — "no parent" and "exactly one parent" looked alike, so a
	// consumer reading the array alone silently lost 848 single-parent
	// relations. The server unions the two fields and dedups by ref, so the
	// repeated first parent writes one edge.
	if urs := allURRefs(req.UR); len(urs) > 0 {
		payload["parent_external_ids"] = urs
	}
	if req.Priority != "" {
		payload["priority"] = req.Priority
	}
	if req.Owner != "" {
		payload["owner"] = req.Owner
	}
	if req.Release != "" {
		payload["release_note"] = req.Release
	}
	// REQ-CROSS-222 §222.2 and §222.6 share one carrier: the ledger's
	// unrecognized columns first, then the two evidence cells verbatim.
	//
	// The cells need a raw home because splitEvidenceRefs answers a different
	// question — which references does this cell name? With a backtick present
	// it keeps only the backticked spans, so the counts, RED/GREEN notes,
	// review outcomes and scope qualifiers written beside a reference reached
	// no payload at all; with none it takes the cell whole but shortens it to
	// 200 runes. Both are right for a reference list and wrong for
	// preservation, so preservation is carried separately and the parsing is
	// left exactly as it was — additive, never a replacement.
	//
	// Named extras rather than columns of their own: the store already carries
	// this jsonb, casts it verbatim, and serves it back on the sync read
	// surface, so the raw text round-trips with no server change. Order is
	// content identity — a stable order keeps an unchanged row's hash stable.
	extras := make([]map[string]any, 0, len(req.Extras)+2)
	for _, e := range req.Extras {
		extras = append(extras, map[string]any{"name": e.Name, "value": e.Value})
	}
	for _, cell := range []struct{ name, text string }{
		{rawTestsCellExtra, req.Tests},
		{rawCodeCellExtra, req.Code},
	} {
		if v := dashless(cell.text); v != "" {
			extras = append(extras, map[string]any{"name": cell.name, "value": v})
		}
	}
	// §245.9: a UR cell saying more than the extracted token — provenance
	// prose, a decorated reference, a multi-parent list — is preserved
	// verbatim; a clean single token adds nothing.
	if raw := dashless(req.UR); raw != "" && raw != firstURRef(req.UR) {
		extras = append(extras, map[string]any{"name": rawURCellExtra, "value": raw})
	}
	if len(extras) > 0 {
		payload["extra_columns"] = extras
	}
	if req.Detail != "" {
		payload["detail_md"] = req.Detail
	}
	if s := strings.TrimSpace(req.Source); s != "" && s != "—" {
		payload["source_raw"] = s
	}
	// §222.4 as sharpened by §245.8: an OBSOLETE row names what superseded it
	// only when its source cell says so — the first requirement or epic id
	// AFTER an explicit "superseded by". A bare id mention is context, not a
	// supersession claim.
	if req.Status == "OBSOLETE" {
		if loc := supersededByRe.FindStringIndex(req.Source); loc != nil {
			if refs := idMentions(req.Source[loc[1]:], "REQ", "EPIC"); len(refs) > 0 {
				payload["superseding_ref"] = refs[0]
			}
		}
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor // after hashing — who synced is not content identity
	return Op{Type: "upsert_requirement", Payload: payload}
}

// BuildBacklogOp serializes one BACKLOG.md discovery row or gap-register row
// (REQ-CROSS-223 §223.2: they gain a store home for the ledger import).
func BuildBacklogOp(b BacklogRow) Op {
	payload := map[string]any{
		"external_id": b.ExternalID,
		"kind":        b.Kind,
		"title":       b.Title,
		"source_path": b.SourcePath,
	}
	if b.NotesMD != "" {
		payload["notes_md"] = b.NotesMD
	}
	if b.Route != "" {
		payload["route"] = b.Route
	}
	// REQ-CROSS-251: the typed fields, present-only so absence never clears
	for key, value := range map[string]string{
		"raised_at":       b.RaisedAt,
		"raised_by":       b.RaisedBy,
		"disposition":     b.Disposition,
		"disposition_ref": b.DispositionRef,
		"candidate_route": b.CandidateRoute,
		"why_unrouted":    b.WhyUnrouted,
		"gap_kind":        b.GapKind,
		"raw_body":        b.RawBody,
	} {
		if value != "" {
			payload[key] = value
		}
	}
	if len(b.AffectedIDs) > 0 {
		ids := make([]any, len(b.AffectedIDs))
		for i, id := range b.AffectedIDs {
			ids[i] = id
		}
		payload["affected_external_ids"] = ids
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_backlog_record", Payload: payload}
}

// allURRefs expands a UR cell's ` · `-separated list into every named parent
// (REQ-CROSS-222 §222.1: each one is a declared relation).
func allURRefs(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, "·") {
		// firstURRef returns only a UR-shaped token (§245.9), so dash
		// placeholders and prose fall out here by yielding nothing.
		if ur := firstURRef(part); ur != "" {
			out = append(out, ur)
		}
	}
	return out
}

var (
	// §245.1: the corpus joins H1 id and title with an em-dash, en-dash, or
	// plain hyphen — all spaced; an unspaced hyphen is part of the id.
	epicH1Re     = regexp.MustCompile(`(?m)^#\s+\S+\s+[—–-]\s+(.+)$`)
	reqSectionRe = regexp.MustCompile(`(?s)## Requirements in this epic\n(.*?)(\n##|$)`)
	// Membership declarations deliberately recognize only the four named H2
	// shapes plus a `**Realizes:**`/`**Requirements:**` bold label line.
	// Nearby System/User requirement sections are different relations and must
	// never leak into epic membership.
	membershipSectionRe = regexp.MustCompile(`(?m)^##[ \t]+(?:Requirements in this epic|Requirements realized|Linked requirements|Requirements)[ \t]*\r?\n`)
	membershipNextH2Re  = regexp.MustCompile(`(?m)^##[ \t]+`)
	// A fifth shape: a top-of-record metadata line — `**Requirements:**`
	// alongside the record's `**Release:**`/`**Specification:**` banner, or
	// `**Realizes:**` elsewhere in the body — is the same declaration as the
	// four headings above, just spelled as one bold-label line instead of a
	// section. EPIC-CLI-004, EPIC-CMP-903 and EPIC-CMP-904 declare membership
	// only this way and had 0 rows in epic_system_requirements until this was
	// recognized.
	membershipRealizesRe = regexp.MustCompile(`(?m)^[ \t]*(?:-[ \t]*)?\*\*(?:Realizes|Requirements):\*\*[ \t]*(.*)$`)
	membershipParenRe    = regexp.MustCompile(`\([^()\n]*\)`)
	membershipReassignRe = regexp.MustCompile(`\bREQ-[A-Z][A-Z0-9]*-\d+[ \t]+is[ \t]+EPIC-[A-Z0-9][A-Z0-9-]*\b`)
	membershipTokenRe    = regexp.MustCompile(`REQ-([A-Z][A-Z0-9]*)-(\d+)(?:[ \t]*(\.\.|…)[ \t]*(?:REQ-([A-Z][A-Z0-9]*)-)?(\d+)|/(\d+))?`)
	// The completion approval's heading, in the three shapes the corpus and the
	// installed template use. Alternation rather than a tolerated prefix on
	// purpose: `## Specification approval` is a DIFFERENT gate living one
	// heading above this one in every template-shaped record, and reading it
	// here is the substitution REQ-CROSS-110 exists to prevent.
	approvalHeadingRe = regexp.MustCompile(`(?i)## (?:human |completion )?approval\n`)
	approvalPendingRe = regexp.MustCompile(`(?i)_pending`)
	userTagRe         = regexp.MustCompile(`USER:\d{4}-\d{2}-\d{2}`)
	supersededByRe    = regexp.MustCompile(`(?i)superseded\s+by`)
	approvedLineRe    = regexp.MustCompile(`(?m)^\*{0,2}APPROVED\b[^\n]*USER:\d{4}-\d{2}-\d{2}[^\n]*$`)
	reqIDGlobalRe     = regexp.MustCompile(`REQ-[A-Z][A-Z0-9]*-\d+`)
)

// approvalLineOf finds the epic record's recorded approval, if any: an
// "## Approval", "## Human approval" or "## Completion approval" section
// carrying a USER: tag (not marked pending), or a line starting with
// **APPROVED carrying one.
//
// EVERY matching section is scanned, and each section line by line, because a
// record migrating between conventions keeps its old `## Approval` stub —
// often still `_pending_` — above the section that actually records the grant,
// and the approval inside any one section is a bullet OR a markdown table
// whose first line is a header. Across sections, as within one, the newest
// date wins.
//
// EVERY returned line carries a USER: tag. Both other callers slice it
// positionally as tag[5:]; a tag-less line panics them mid-sync.
func approvalLineOf(text string) string {
	var best, bestTag string
	for _, loc := range approvalHeadingRe.FindAllStringIndex(text, -1) {
		body := text[loc[1]:]
		if end := strings.Index(body, "\n##"); end >= 0 {
			body = body[:end]
		}
		if approvalPendingRe.MatchString(body) {
			continue
		}
		line := latestApprovalLine(body)
		if line == "" {
			continue
		}
		if tag := approvalTagOf(line); bestTag == "" || tag > bestTag {
			best, bestTag = line, tag
		}
	}
	if best != "" {
		return best
	}
	// The fallback is judged by the same rule, not merely matched: it re-scans
	// the whole document, so without this it readmits the very line
	// latestApprovalLine has just rejected. It also joins a wrapped paragraph
	// the way a headed approval does: a fallback line lives outside every
	// "## Approval" heading (a stray "###" subsection, or none at all), so it
	// never reaches latestApprovalLine's own join, and was cut at its
	// author's line wrap the same way completion approvals used to be.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if approvedLineRe.MatchString(line) && isCompletionApproval(line, "") {
			return joinWrappedApproval(lines, i, "")
		}
	}
	return ""
}

// latestApprovalLine picks the tagged line carrying the newest date, so a
// section listing a specification approval and a later completion approval
// yields the completion one. Choosing by date rather than by position is what
// keeps existing records unchanged: where the dates tie — the common case — the
// first tagged line still wins, which is what the previous reader returned.
func latestApprovalLine(section string) string {
	var best, bestTag string
	lines := strings.Split(section, "\n")
	header := ""
	for i, line := range lines {
		if isTableHeader(lines, i) {
			header = line
			continue
		}
		tag := approvalTagOf(line)
		if tag == "" || !isCompletionApproval(line, header) {
			continue
		}
		if bestTag == "" || tag > bestTag { // ISO-8601 dates sort lexically
			best, bestTag = joinWrappedApproval(lines, i, header), tag
		}
	}
	return best
}

// joinWrappedApproval reads the approval as its author wrote it: one logical
// unit, which in hand-wrapped markdown spans several physical lines. Taking
// only the first cut 28 of 154 stored bases mid-sentence — at 73-79
// characters, markdown wrap width — severing the quotation of the human's own
// words that IS the justification.
//
// The unit ends where a new one begins: a blank line, a list item, a table row
// or a heading. A table row is already complete, so it is never extended —
// otherwise one approval row would swallow the row beneath it; its own
// justification is read out by tableRowBasis instead.
func joinWrappedApproval(lines []string, i int, header string) string {
	first := strings.TrimSpace(lines[i])
	if strings.HasPrefix(first, "|") {
		return tableRowBasis(first, header)
	}
	out := []string{first}
	for _, next := range lines[i+1:] {
		t := strings.TrimSpace(next)
		if t == "" || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "#") ||
			strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
			break
		}
		out = append(out, t)
	}
	return strings.Join(strings.Fields(strings.Join(out, " ")), " ")
}

// tableRowBasis is a table row's own justification: the cell its header names
// Decision, not the whole row's markup — the same column decisionGrantedIn
// reads to judge the row, so what gets stored is what the human decided
// rather than the id/approver/source columns around it. Measured: 12 of 157
// stored approval bases were the entire raw row, pipes and all, because
// nothing here read the row at all.
//
// Every caller already required the ROW to carry a USER: tag before reaching
// this point (approvalTagOf gates latestApprovalLine's loop); the Decision
// cell alone usually does not — the tag lives in a sibling Source or Approver
// column instead. approvalTagOf(the returned text) must still find one: two
// callers slice it positionally at tag[5:]. So the tag is appended when the
// decision cell doesn't already carry it, rather than lost.
func tableRowBasis(row, header string) string {
	col := decisionColumnOf(header)
	cells := splitCells(row)
	if col < 0 || col >= len(cells) {
		// No recognized Decision column. decisionGrantedIn already requires
		// one to grant the row, so this is defensive, not reachable through
		// latestApprovalLine today — keep the row rather than guessing at the
		// wrong cell.
		return row
	}
	cell := strings.Join(strings.Fields(cells[col]), " ")
	if cell == "" {
		return row
	}
	if userTagRe.MatchString(cell) {
		return cell
	}
	if tag := approvalTagOf(row); tag != "" {
		return cell + " (" + tag + ")"
	}
	return row
}

// isTableHeader — a table row directly above the |---| divider. Tracked while
// scanning a section so a row's decision can be read from the column its own
// header designates.
func isTableHeader(lines []string, i int) bool {
	return strings.HasPrefix(strings.TrimSpace(lines[i]), "|") &&
		i+1 < len(lines) && tableDividerRe.MatchString(lines[i+1])
}

// isCompletionApproval reports whether a line records a GRANTED COMPLETION
// approval — the two questions the reader used to skip.
//
// Which gate: a line naming only the specification gate answers a different
// question (implementation may START). It is skipped, unless the same line also
// names the completion gate — records write both on one line, and discarding
// those whole loses a real, attributed sign-off.
//
// What decision: a record writes the decision in a table's decision column or
// at the head of a prose line, and "pending", "changes requested" and "final
// approval at the pilot review" are decisions too — they are just not this one.
// Reading only for a USER: tag turns every one of them into an approval.
func isCompletionApproval(line, header string) bool {
	if specApprovalLineRe.MatchString(line) && !completionGateRe.MatchString(line) {
		return false
	}
	return decisionGrantedIn(line, header)
}

// isSpecApproval is the same pair of questions asked of a line found under
// `## Specification approval`, where the heading already establishes which gate
// is answered. Only the mirror veto is needed: a record that lists its
// completion approval under the specification heading has not approved its
// specification (REQ-CROSS-114).
func isSpecApproval(line, header string) bool {
	if completionGateRe.MatchString(line) && !specApprovalLineRe.MatchString(line) {
		return false
	}
	return decisionGrantedIn(line, header)
}

// decisionGrantedIn reads the DECISION a line records, independent of which
// gate it answers — the half both approval readers share. header is the table
// header row above the line, "" when the line sits in no table.
func decisionGrantedIn(line, header string) bool {
	if decisionDeferredRe.MatchString(line) {
		return false
	}

	if cells := splitCells(line); len(cells) > 2 {
		// A table row. The decision is the cell the HEADER designates, never
		// the first cell that happens to read as one — a Role cell saying
		// "sign-off authority" describes who may decide, not that they did.
		// Cells after the decision qualify it rather than make it: the
		// template's trailing Conditions column routinely says "pending
		// <remaining work>" about a row whose Decision cell granted. A table
		// designating no Decision column records no readable decision, which
		// sync makes visible; a guessed one can invent an approval.
		col := decisionColumnOf(header)
		if col < 0 || col >= len(cells) {
			return false
		}
		c := decisionNoiseRe.ReplaceAllString(cells[col], "")
		if decisionWithheldRe.MatchString(c) {
			return false
		}
		return decisionGrantedRe.MatchString(c)
	}

	// Prose. The gate's own id is its name, not its answer — stripped before
	// any vocabulary is read, or APPROVE-EPIC-X grants itself. The decision
	// leads the line, so the head decides the ambiguous withheld words — a
	// granted approval quoting one ("accept now all pending requirements") is
	// not a withheld one — while the unambiguous refusals ("not approved",
	// "no decision") veto wherever they sit. Granting additionally requires a
	// granted word SOMEWHERE on the line: a neutral tagged line (a brief
	// bullet, a pointer cell) records no decision, and no decision is no grant.
	prose := gateIDProseRe.ReplaceAllString(line, "")
	if decisionWithheldRe.MatchString(decisionNoiseRe.ReplaceAllString(prose, "")) {
		return false
	}
	if decisionRefusedAnywhereRe.MatchString(prose) {
		return false
	}
	return decisionGrantedAnywhereRe.MatchString(prose)
}

// decisionColumnOf is the index, in a line's splitCells cells, of the column
// the header names Decision — -1 when the header designates none. Header and
// row must be split by the same rule, or an escaped pipe in either moves the
// decision out from under its column.
func decisionColumnOf(header string) int {
	if header == "" {
		return -1
	}
	for i, cell := range splitCells(header) {
		if strings.EqualFold(decisionNoiseRe.ReplaceAllString(cell, ""), "decision") {
			return i
		}
	}
	return -1
}

// approvalTagOf is the tag that dates an approval line: the LATEST one it
// carries. A single row records the build sanction and, days later, the
// sign-off given after the evidence summary; taking the first stamps the
// approval with the date of permission to start.
func approvalTagOf(line string) string {
	var latest string
	for _, tag := range userTagRe.FindAllString(line, -1) {
		if tag > latest { // ISO-8601 dates sort lexically
			latest = tag
		}
	}
	return latest
}

// An approval answers one gate. A specification approval says implementation may
// START; a completion approval says it is finished and reviewed. Reading "an
// approval" without asking which one lets the first stand in for the second, so
// an epic whose only approval is its spec gate reads as completion-approved with
// nothing built.
//
// Skipping by date is not enough: where both exist the later one already wins,
// but where only the specification approval exists there is nothing later to
// prefer.
var specApprovalLineRe = regexp.MustCompile(`(?i)SPEC-APPROVE-|spec(?:ification)?(?:\s+approval|:)`)

var (
	// A completion gate named on the line. Case-sensitive on purpose: these ids
	// are uppercase by convention, and a miss here would let the specification
	// veto drop a real approval. The leading class is what keeps
	// SPEC-APPROVE-EPIC-X from counting as its own completion gate.
	completionGateRe = regexp.MustCompile(`(^|[^A-Za-z-])(APPROVE-|APP-[A-Z]+-\d)`)

	// Markdown emphasis and the brackets a decision is written in. Stripped
	// from both ends so the decision itself can be read from the start.
	decisionNoiseRe = regexp.MustCompile("^[\\s*_`()\\[\\]]+|[\\s*_`()\\[\\]]+$")

	// A decision that was not granted. Anchored to the start of the segment it
	// judges — mid-text these words routinely mean something else, as in the
	// batch approval whose own words are "accept now all pending requirements".
	decisionWithheldRe = regexp.MustCompile(`(?i)^(pending|awaiting|blocked|deferred|rejected|declined|withdrawn|changes requested|not approved|no decision)\b`)

	// The refusals that are unambiguous WHEREVER they sit — no granted line
	// quotes "not approved" or "no decision" about itself — so in prose they
	// veto mid-line, where the anchored set cannot reach.
	decisionRefusedAnywhereRe = regexp.MustCompile(`(?i)\b(not approved|no decision|changes requested|rejected|declined|withdrawn)\b`)

	// The one deferral the corpus writes mid-cell: a decision that grants the
	// specification gate and names a later review for the completion one.
	decisionDeferredRe = regexp.MustCompile(`(?i)final approval\s+(at|after|pending|to be|remains)`)

	// A granted decision, as a decision column writes it.
	decisionGrantedRe = regexp.MustCompile(`(?i)^(approved|approve\b|accepted|signed[ -]off|sign-off)`)

	// The same vocabulary anywhere in a prose line, where the grant follows a
	// leading tag ("USER:… — Approved") or label ("**…recorded:** … option
	// `approve`") rather than opening the line.
	decisionGrantedAnywhereRe = regexp.MustCompile(`(?i)\b(approved|approve|accepted|signed[ -]off|sign-off)\b`)

	// A gate identifier in prose, stripped before the decision vocabulary is
	// read: the APPROVE inside APPROVE-EPIC-X names the gate, it does not
	// answer it. Case-sensitive like completionGateRe — the ids are uppercase
	// by convention, and a lowercase "approve" must keep meaning the decision.
	gateIDProseRe = regexp.MustCompile(`\b(SPEC-)?(APPROVE|APP)(-[A-Z0-9]+)+\b|\bSPEC-APPROVE\b`)
)

type requirementMembership struct {
	IDs        []string
	Recognized bool
	Line       int
}

// requirementMembershipOf is the one declaration reader used by epic payloads
// and both gate builders. Sections are read first in document order, followed
// by `**Realizes:**`/`**Requirements:**` bold-label lines; ids retain
// declaration order and duplicates collapse.
func requirementMembershipOf(text string) requirementMembership {
	result := requirementMembership{IDs: []string{}}
	seen := map[string]bool{}
	mark := func(offset int) {
		result.Recognized = true
		line := 1 + strings.Count(text[:offset], "\n")
		if result.Line == 0 || line < result.Line {
			result.Line = line
		}
	}
	add := func(source string) {
		addRequirementMembershipTokens(source, seen, &result.IDs)
	}

	for _, loc := range membershipSectionRe.FindAllStringIndex(text, -1) {
		mark(loc[0])
		body := text[loc[1]:]
		if end := membershipNextH2Re.FindStringIndex(body); end != nil {
			body = body[:end[0]]
		}
		add(body)
	}
	for _, loc := range membershipRealizesRe.FindAllStringSubmatchIndex(text, -1) {
		mark(loc[0])
		add(text[loc[2]:loc[3]])
	}
	return result
}

func addRequirementMembershipTokens(source string, seen map[string]bool, ids *[]string) {
	// Parenthetical annotations are commentary, not membership. Repeat so
	// separate annotations on the same declaration all disappear.
	for membershipParenRe.MatchString(source) {
		source = membershipParenRe.ReplaceAllString(source, " ")
	}
	source = membershipReassignRe.ReplaceAllString(source, " ")
	add := func(id string) {
		if !seen[id] {
			seen[id] = true
			*ids = append(*ids, id)
		}
	}

	for _, match := range membershipTokenRe.FindAllStringSubmatch(source, -1) {
		prefix, firstDigits := match[1], match[2]
		first, err := strconv.Atoi(firstDigits)
		if err != nil {
			continue
		}
		add(requirementMembershipID(prefix, first, len(firstDigits)))

		if match[6] != "" {
			if alternate, err := strconv.Atoi(match[6]); err == nil {
				add(requirementMembershipID(prefix, alternate, len(match[6])))
			}
			continue
		}
		if match[3] == "" || match[5] == "" {
			continue
		}
		last, err := strconv.Atoi(match[5])
		if err != nil {
			continue
		}
		endPrefix := match[4]
		if endPrefix != "" && endPrefix != prefix {
			add(requirementMembershipID(endPrefix, last, len(match[5])))
			continue
		}
		if last < first {
			add(requirementMembershipID(prefix, last, len(match[5])))
			continue
		}
		width := len(firstDigits)
		if len(match[5]) > width {
			width = len(match[5])
		}
		for n := first + 1; n <= last; n++ {
			add(requirementMembershipID(prefix, n, width))
		}
	}
}

func requirementMembershipID(prefix string, number, width int) string {
	return fmt.Sprintf("REQ-%s-%0*d", prefix, width, number)
}

func requirementIDsOf(text string) []string {
	return requirementMembershipOf(text).IDs
}

// specContentCap mirrors the schema's $defs.spec content_md maxLength — the
// builders cap-and-warn, so an oversize spec can never reach the wire
// (REQ-CROSS-023; warning emitted at snapshot time, never in the payload).
const specContentCap = 65536

// `(?m)` is deliberately absent: with it, `$` would match the first line end
// and truncate a wrapped paragraph to its first line.
// Both headings are legitimate: workspace-authored epics say "User outcome",
// and the reverse-engineering skill writes "User requirement". Sixteen epics on
// a real system synced with an empty description because only the first was
// recognised, and nothing reported it (`RUN:2026-08-12`).
var epicUserOutcomeRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+User (?:outcome|requirement)[^\n]*\n(.*?)(\n##|$)`)

// The rollup recovery carrier is narrower than the regular declaration
// reader: its id comes from WORKLIST, so its other half must be an actual
// `## User outcome` statement in that row's named record. `User requirement`
// headings and rollup prose do not substitute for that missing half.
var rollupUserOutcomeRe = regexp.MustCompile(`(?s)(?:^|\n)##\s+User outcome[^\n]*\n(.*?)(\n##|$)`)

// ParseEpicDescription — the epic's "## User outcome" section, first paragraph,
// as one line. 105 of 139 records carry the heading; the rest get no
// description rather than an invented one (same honesty rule as
// ParseDescription).
func ParseEpicDescription(recordText string) string {
	if m := epicUserOutcomeRe.FindStringSubmatch(recordText); m != nil {
		para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[1]), "\n\n", 2)[0])
		return capRunes(strings.Join(strings.Fields(para), " "), 600)
	}
	// No `User outcome` heading. 34 of 190 records are in this shape and they
	// landed with no description at all, which leaves the app an id and a
	// title. Two fallbacks, ordered by how much each assumes.

	// The user requirement the existing parsers already read. Nine of the 34
	// declare their outcome as a labelled bullet — `- **User outcome
	// (UR-SYNC-001):** …`, `- **UR:** …` — and ParseEpicUserRequirement reads
	// those today. Reusing it invents nothing: the same sentence, from a
	// parser that already recognizes the shape.
	if ur, ok := ParseEpicUserRequirement(recordText); ok && ur.Statement != "" {
		return capRunes(strings.Join(strings.Fields(ur.Statement), " "), 600)
	}

	// Otherwise the record's opening paragraph, and only when it is prose.
	// Twenty-six records open with a real description; the remainder open with
	// a metadata banner (`- **Status:** … · **Owner:** …`). A list is never a
	// description, so a leading list marker disqualifies the paragraph rather
	// than filling the field with metadata — better empty than wrong.
	return epicLeadProse(recordText)
}

// epicLeadProse is the first paragraph between the record's H1 and its first
// level-2 section, when that paragraph is prose. A `**Bold** lead` is prose; a
// `- ` or `* ` list item is not.
func epicLeadProse(recordText string) string {
	var lead []string
	for i, line := range strings.Split(recordText, "\n") {
		if i == 0 && strings.HasPrefix(line, "# ") {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		lead = append(lead, line)
	}
	para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(strings.Join(lead, "\n")), "\n\n", 2)[0])
	if para == "" {
		return ""
	}
	if strings.HasPrefix(para, "- ") || strings.HasPrefix(para, "* ") ||
		strings.HasPrefix(para, "+ ") || strings.HasPrefix(para, "|") {
		return "" // a list or a table states facts about the work, not the outcome
	}
	return capRunes(strings.Join(strings.Fields(para), " "), 600)
}

func parseRollupUserOutcome(recordText string) string {
	m := rollupUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return ""
	}
	para := strings.TrimSpace(strings.SplitN(strings.TrimSpace(m[1]), "\n\n", 2)[0])
	return capRunes(strings.Join(strings.Fields(para), " "), 600)
}

func rollupUserRequirements(rawCell, recordText string) []UserRequirement {
	statement := parseRollupUserOutcome(recordText)
	if statement == "" {
		return nil
	}
	seen := map[string]bool{}
	var out []UserRequirement
	for _, id := range urTokenRe.FindAllString(rawCell, -1) {
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, UserRequirement{ID: id, Statement: statement})
	}
	return out
}

// ---------------------------------------------------------------- user requirements

// UserRequirement is the outcome an epic exists to deliver, written in its
// record as the reverse-engineering skill prescribes:
//
//	## User requirement
//
//	**UR-CON-001** — As a **public-sector organization** …, I want to …, so that …
//
//	**Actors:** `platform_admin` only (`CODE:…`)
//
// EPIC-SYNC-011 (`USER:2026-08-12`): sixteen of these sat in a real system's
// records while the platform held zero user requirements, because both builders
// wrote `kind: "system"` as a literal — leaving its whole traceability matrix as
// 874 orphans.
type UserRequirement struct {
	ID        string
	Statement string
	Actors    string
}

var (
	urIDLineRe = regexp.MustCompile(`^\*\*(UR-[A-Z][A-Z0-9]*-\d+)\*\*\s*(?:—|-|–)?\s*(.*)$`)
	urActorsRe = regexp.MustCompile(`(?m)^\*\*Actors:\*\*\s*(.+)$`)
	urCtxRe    = regexp.MustCompile(`^UR-([A-Z][A-Z0-9]*)-\d+$`)
	// `## User outcome (UR-SY-010)` — the id in the heading rather than the body.
	urHeadingIDRe = regexp.MustCompile(`##\s+User (?:outcome|requirement)[^\n]*\((UR-[A-Z][A-Z0-9]*-\d+)\)`)
	// "As a X, I want Y, so that Z" — Y is the title, the whole sentence the
	// description. The "so that" clause is the rationale, not the ask.
	urWantRe = regexp.MustCompile(`(?i),\s*I want\s+(?:to\s+)?(.+?)(?:,\s*so that\b.*)?$`)
)

// ParseEpicUserRequirement reads the record's user requirement, if it states
// one. A record with no section, or a section whose first bold token is not a
// UR id, yields nothing — a missing user requirement stays visible as missing,
// where an invented one is indistinguishable from a real one (#9 honesty rule,
// same as ParseDescription).
// REQ-CROSS-073: the third declaration shape. 52 user requirements — the whole
// EPIC-PORTFOLIO-* family — declare themselves only in a
// `## Linked user requirements` table. Their epics DO have a `## User outcome`
// heading, but without the id in it, so neither existing shape matched and no
// UserRequirement entity was ever built. Their requirements' UR column then
// named a parent the platform had no row for, leaving ~100 requirements as
// orphans in the compliance matrix with no `derives` trace possible.
//
// Scoped to that section on purpose: mining every table row that starts with a
// UR id would pull ids out of Source-material and cross-reference tables.
var (
	linkedURBulletRe  = regexp.MustCompile("^-\\s+`?(UR-[A-Z][A-Z0-9]*-\\d+)`?\\s+[—–-]\\s+(.+)$")
	linkedURSectionRe = regexp.MustCompile(`(?ms)^##+\s*Linked user requirements\s*$(.*?)(?:^##|\z)`)
	linkedURRowRe     = regexp.MustCompile(`^\|\s*(UR-[A-Z0-9]+-\d+)\s*\|([^|]*)\|`)
)

// REQ-CROSS-073, fourth shape: `- **User outcome (UR-SYNC-005):** <statement>`,
// a bullet with the colon inside the bold. The statement runs to the end of that
// bullet — stopping at the next one, so a "Blast radius" line beneath does not
// get swallowed into the user outcome.
var bulletOutcomeRe = regexp.MustCompile(`(?m)^[ \t]*-[ \t]*\*\*User outcome \((UR-[A-Z0-9]+-\d+)\)[: \t]*\*\*[: \t]*(.+)$`)

func parseBulletOutcome(recordText string) (UserRequirement, bool) {
	m := bulletOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return UserRequirement{}, false
	}
	return bulletOutcomeFrom(m)
}

func parseBulletOutcomes(recordText string) []UserRequirement {
	var out []UserRequirement
	for _, m := range bulletOutcomeRe.FindAllStringSubmatch(recordText, -1) {
		if ur, ok := bulletOutcomeFrom(m); ok {
			out = append(out, ur)
		}
	}
	return out
}

func bulletOutcomeFrom(m []string) (UserRequirement, bool) {
	statement := strings.Join(strings.Fields(m[2]), " ")
	if statement == "" {
		return UserRequirement{}, false
	}
	return UserRequirement{ID: m[1], Statement: capRunes(statement, 600)}, true
}

// REQ-CROSS-075, fifth shape: `## UR-ABS-1 — the user requirement`, the id
// leading its own heading, with the statement as the paragraph beneath. One
// corpus writes its 89 modern epics this way, and no "User outcome" heading exists in
// them at all.
//
// The em-dash (or hyphen) after the id is what makes this a DECLARATION rather
// than a passing mention: `## How UR-NOPE-001 relates to this epic` must not
// match, or every cross-reference heading becomes a user requirement.
var idLedOutcomeRe = regexp.MustCompile(`(?ms)^##+[ \t]*(UR-[A-Z0-9]+-\d+)[ \t]*[—–-][^\n]*\n(.*?)(?:\n##|\z)`)

func parseIDLedOutcome(recordText string) (UserRequirement, bool) {
	m := idLedOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return UserRequirement{}, false
	}
	return idLedOutcomeFrom(m)
}

// The heading alone, with the body found by index rather than consumed by the
// same pattern. idLedOutcomeRe swallows the `\n##` that ends the first body, so
// scanning it repeatedly would eat every second heading — the plural has to walk
// the headings itself. ops.js walks them the same way for the same reason.
var (
	idLedHeadingRe   = regexp.MustCompile(`(?m)^##+[ \t]*(UR-[A-Z0-9]+-\d+)[ \t]*[—–-][^\n]*$`)
	anyHeadingLineRe = regexp.MustCompile(`(?m)^##+`)
)

// parseIDLedOutcomes reads every id-led heading, not only the first: a record
// may declare one outcome per heading.
func parseIDLedOutcomes(recordText string) []UserRequirement {
	var out []UserRequirement
	for _, loc := range idLedHeadingRe.FindAllStringSubmatchIndex(recordText, -1) {
		body := recordText[loc[1]:]
		if next := anyHeadingLineRe.FindStringIndex(body); next != nil {
			body = body[:next[0]]
		}
		if ur, ok := idLedOutcomeFrom([]string{"", recordText[loc[2]:loc[3]], body}); ok {
			out = append(out, ur)
		}
	}
	return out
}

func idLedOutcomeFrom(m []string) (UserRequirement, bool) {
	// the first non-empty paragraph beneath the heading is the statement
	for _, para := range strings.Split(strings.TrimSpace(m[2]), "\n\n") {
		text := strings.Join(strings.Fields(strings.TrimSpace(para)), " ")
		text = strings.ReplaceAll(text, "**", "")
		text = strings.TrimSpace(text)
		if text == "" || strings.HasPrefix(text, "- ") || strings.HasSuffix(text, ":") {
			continue
		}
		return UserRequirement{ID: m[1], Statement: capRunes(text, 600)}, true
	}
	return UserRequirement{}, false
}

// parseLinkedURTable reads the first data row of the linked-user-requirements
// table: the id and the statement beside it.
func parseLinkedURTable(recordText string) (UserRequirement, bool) {
	rows := parseLinkedURTableRows(recordText)
	if len(rows) == 0 {
		return UserRequirement{}, false
	}
	return rows[0], true
}

// parseLinkedURTableRows reads EVERY data row of the section. One row is one
// declared membership; returning only the first kept the epic's leading user
// requirement and dropped the rest, so the epic→UR join read as complete while
// the second and later rows had no edge at all.
//
// The section bound is what keeps this honest: a requirement-traceability table
// and a source-material table also open their rows with a UR id, and neither
// declares membership.
func parseLinkedURTableRows(recordText string) []UserRequirement {
	sec := linkedURSectionRe.FindStringSubmatch(recordText)
	if sec == nil {
		return nil
	}
	var out []UserRequirement
	for _, line := range strings.Split(sec[1], "\n") {
		m := linkedURRowRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			// Mapping audit: the sixth declaration shape — a
			// BULLET (`- UR-XXX-NNN — statement`) — cost ten URs and seventy
			// derives edges.
			m = linkedURBulletRe.FindStringSubmatch(strings.TrimSpace(line))
		}
		if m == nil {
			continue
		}
		statement := strings.Join(strings.Fields(m[2]), " ")
		if statement == "" {
			continue
		}
		out = append(out, UserRequirement{ID: m[1], Statement: capRunes(statement, 600)})
	}
	return out
}

// parseUserOutcomeIDLines reads every `**UR-…** — statement` line inside the
// `## User outcome` section. The section's first line is the one the single-UR
// reader takes; a section listing several declares several.
func parseUserOutcomeIDLines(recordText string) []UserRequirement {
	m := epicUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		return nil
	}
	var out []UserRequirement
	for _, line := range strings.Split(m[1], "\n") {
		id := urIDLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if id == nil {
			continue
		}
		statement := strings.TrimSpace(id[2])
		if statement == "" {
			continue
		}
		out = append(out, UserRequirement{ID: id[1], Statement: capRunes(statement, 600)})
	}
	return out
}

// ParseEpicUserRequirements reads EVERY user requirement the record declares,
// in declaration order and deduplicated by id.
//
// ParseEpicUserRequirement answers a narrower question — which single one leads
// the record — and every one of its shapes stops at its first match. An epic
// that names two outcomes therefore carried one membership and one entity, and
// the join read as complete because the count of epics and the count of user
// requirements both reconciled.
//
// The leading declaration stays first, so a record whose payload already named
// a user requirement keeps naming the same one; the others join it behind. Each
// shape contributes only declarations that state BOTH an id and a statement — an
// id with no statement would need a statement invented for it, and an invented
// user requirement is indistinguishable from a real one.
func ParseEpicUserRequirements(recordText string) []UserRequirement {
	seen := map[string]bool{}
	var out []UserRequirement
	add := func(ur UserRequirement, ok bool) {
		if !ok || ur.ID == "" || ur.Statement == "" || seen[ur.ID] {
			return
		}
		seen[ur.ID] = true
		out = append(out, ur)
	}
	add(ParseEpicUserRequirement(recordText))
	for _, ur := range parseUserOutcomeIDLines(recordText) {
		add(ur, true)
	}
	for _, ur := range parseIDLedOutcomes(recordText) {
		add(ur, true)
	}
	for _, ur := range parseBulletOutcomes(recordText) {
		add(ur, true)
	}
	for _, ur := range parseLinkedURTableRows(recordText) {
		add(ur, true)
	}
	return out
}

func ParseEpicUserRequirement(recordText string) (UserRequirement, bool) {
	m := epicUserOutcomeRe.FindStringSubmatch(recordText)
	if m == nil {
		// No `User outcome` section at all — an id-led heading, a bullet or the
		// linked table may still declare it.
		if ur, ok := parseIDLedOutcome(recordText); ok {
			return ur, true
		}
		if ur, ok := parseBulletOutcome(recordText); ok {
			return ur, true
		}
		return parseLinkedURTable(recordText)
	}
	section := strings.TrimSpace(m[1])
	paras := strings.Split(section, "\n\n")
	head := strings.Join(strings.Fields(strings.TrimSpace(paras[0])), " ")

	var ur UserRequirement
	switch {
	case urIDLineRe.MatchString(head):
		// The skill's shape: **UR-CON-001** — As a … I want … so that …
		id := urIDLineRe.FindStringSubmatch(head)
		ur = UserRequirement{ID: id[1], Statement: capRunes(strings.TrimSpace(id[2]), 600)}

	case urHeadingIDRe.MatchString(m[0]):
		// This workspace's shape: `## User outcome (UR-SY-010)` with the
		// statement as prose beneath. Both are in daily use; reading only the
		// first left 40 epics here naming a UR and 4 extracting it
		// (`RUN:2026-08-12`).
		ur = UserRequirement{
			ID:        urHeadingIDRe.FindStringSubmatch(m[0])[1],
			Statement: capRunes(head, 600),
		}

	default:
		// REQ-CROSS-073: the heading exists but carries no id. Fall back to the
		// bullet shape, then to the linked-user-requirements table — the two
		// places the remaining families declare both id and statement.
		if ur, ok := parseBulletOutcome(recordText); ok {
			return ur, true
		}
		return parseLinkedURTable(recordText)
	}
	if a := urActorsRe.FindStringSubmatch(section); a != nil {
		ur.Actors = capRunes(strings.Join(strings.Fields(a[1]), " "), 300)
	}
	if ur.Statement == "" {
		return UserRequirement{}, false
	}
	return ur, true
}

// urWorkStatus maps the epic's WORKLIST state onto the ledger vocabulary. The
// user requirement is delivered by the epic, so it is exactly as done as the
// epic is — nothing is inferred beyond that.
func urWorkStatus(epic Epic) string {
	// The exact lifecycle token wins (mapping audit:
	// IN_REVIEW epics were yielding PROPOSED URs); the five-bucket state
	// stays the honest fallback for token-less rows.
	if epic.ProcessStatus != "" {
		return epic.ProcessStatus
	}
	return urWorkStatusBucket(epic.State)
}

func urWorkStatusBucket(state string) string {
	switch state {
	case "done":
		return "DONE"
	case "awaiting-approval":
		return "IN_REVIEW"
	case "in-progress":
		return "IN_PROGRESS"
	default:
		return "PROPOSED"
	}
}

// BuildUserRequirementOp emits the epic's user requirement as a requirement of
// kind "user". The server has routed that kind to UserRequirement all along
// (`requirement_schema/1`); nothing ever sent it.
func BuildUserRequirementOp(ur UserRequirement, epic Epic) Op {
	ctx := ""
	if m := urCtxRe.FindStringSubmatch(ur.ID); m != nil {
		ctx = m[1]
	}
	// A user requirement is written "As a X, I want Y, so that Z". The title is
	// the WANT — the thing being asked for — because a list of rows all
	// beginning "As a…" is unreadable, and the full sentence is the description.
	statement := strings.TrimSpace(strings.ReplaceAll(ur.Statement, "**", ""))
	title := statement
	if m := urWantRe.FindStringSubmatch(statement); m != nil {
		title = strings.TrimSpace(m[1])
		if r := []rune(title); len(r) > 0 {
			title = strings.ToUpper(string(r[0])) + string(r[1:])
		}
	}
	title = capRunes(strings.TrimRight(title, " ,."), 200)

	payload := map[string]any{
		"external_id":      ur.ID,
		"kind":             "user",
		"title":            title,
		"description":      statement,
		"context":          ctx,
		"stage":            nil,
		"work_status":      urWorkStatus(epic),
		"source_citations": []any{map[string]any{"kind": "epic", "ref": epic.ID}},
	}
	if ur.Actors != "" {
		payload["source_citations"] = append(payload["source_citations"].([]any),
			map[string]any{"kind": "note", "ref": "Actors: " + strings.ReplaceAll(ur.Actors, "**", "")})
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_requirement", Payload: payload}
}

// folderForm reports whether the epic record is the folder convention
// (…/EPIC.md) — the presence rule for the specs payload key.
func folderForm(epic Epic) bool {
	return strings.HasSuffix(epic.Record, "/EPIC.md")
}

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

// pruneURedges drops user-requirement edges whose target this batch does not
// emit, re-hashing since the edge list is content. An epic left with none loses
// the key entirely rather than sending an empty list, which sync reads as "no
// declaration" instead of "an authoritative empty set".
func pruneURedges(op Op, emitted map[string]bool) Op {
	ids, ok := op.Payload["user_requirement_external_ids"].([]string)
	if !ok || len(ids) == 0 {
		return op
	}
	kept := make([]string, 0, len(ids))
	for _, id := range ids {
		if emitted[id] {
			kept = append(kept, id)
		}
	}
	if len(kept) == len(ids) {
		return op
	}
	if len(kept) == 0 {
		delete(op.Payload, "user_requirement_external_ids")
	} else {
		op.Payload["user_requirement_external_ids"] = kept
	}
	return rehashOp(op)
}

func BuildEpicOp(epic Epic, recordText string) Op {
	title := epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title = strings.TrimSpace(h1[1])
	}
	membership := requirementMembershipOf(recordText)
	idsAny := make([]any, len(membership.IDs))
	for i, id := range membership.IDs {
		idsAny[i] = id
	}
	payload := map[string]any{
		"external_id": epic.ID,
		"title":       title,
	}
	// No declaration is an authoritative empty set. A recognized declaration
	// that parses to zero is doubt: omit the field so sync preserves stored
	// links, while the fidelity report names the anomaly.
	if !membership.Recognized || len(membership.IDs) > 0 {
		payload["requirement_external_ids"] = idsAny
	}
	// REQ-PLN-054 TASK-MC-107: the user outcome is what an epic IS about; without
	// it the app can only show an id and a title.
	if desc := ParseEpicDescription(recordText); desc != "" {
		payload["description"] = desc
	}
	// REQ-CROSS-028: the loop state WORKLIST already records, into the two
	// fields the contract has always had. Absent cells stay absent — an epic
	// with nothing recorded keeps the hash it had before this shipped.
	// statusCell again here, not only in ParseWorklist: the placeholder rule is
	// a property of the payload, so it holds for any caller building an op.
	if upper := statusCell(epic.Upper); upper != "" {
		payload["upper_loop_status"] = upper
	}
	if lower := statusCell(epic.Lower); lower != "" {
		payload["lower_loop_status"] = lower
	}
	// REQ-CROSS-223: the epic's PROCESS.md lifecycle — the board status axis
	// is set-once and the loop columns are never-a-re-mapping, so the exact
	// Overall-status token rides its own field. Absent cells stay absent.
	if epic.ProcessStatus != "" {
		payload["process_status"] = epic.ProcessStatus
	}
	// Mapping audit: the epic→UR edge the corpus declares in
	// every record's User-outcome section finally gets a payload carrier —
	// epic_user_requirements had no sync writer.
	// Every declaration is a membership: a record that names two user
	// requirements has two edges, and the list is what carries them.
	// The edge is declared in two places: the record's own outcome section and
	// the WORKLIST row's user-requirements cell. Reading only the record left
	// 18 user requirements whose source_citations name an epic with no matching
	// edge — the store contradicting itself inside one row. URCell's contract
	// already covers this: it "names the denominator even when the named epic
	// record does not repeat the id". A membership edge is not a statement, and
	// only statements were ever barred from this cell.
	// Record order first, then cell order, so an epic whose record already
	// names its UR keeps its exact payload — and its content hash.
	{
		ids := make([]string, 0, 4)
		seen := map[string]bool{}
		addUR := func(id string) {
			if id == "" || seen[id] {
				return
			}
			seen[id] = true
			ids = append(ids, id)
		}
		for _, ur := range ParseEpicUserRequirements(recordText) {
			addUR(ur.ID)
		}
		for _, id := range urTokenRe.FindAllString(epic.URCell, -1) {
			addUR(id)
		}
		if len(ids) > 0 {
			payload["user_requirement_external_ids"] = ids
		}
	}
	if scenarios := ParseScenarios(recordText, epic.ID); len(scenarios) > 0 {
		// REQ-CROSS-250: the evidence map's clause↔test edges ride each
		// scenario criterion as verification_refs.
		emap := ParseEvidenceMapRefs(recordText)
		for _, raw := range scenarios {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := item["external_id"].(string)
			if i := strings.LastIndex(id, "#"); i >= 0 {
				if refs := emap.refsBySCN[id[i+1:]]; len(refs) > 0 {
					list := make([]any, len(refs))
					for j, ref := range refs {
						list[j] = map[string]any{"kind": ref["kind"], "ref": ref["ref"]}
					}
					item["verification_refs"] = list
				}
			}
		}
		payload["scenarios"] = scenarios
	}
	// Presence rule (REQ-CROSS-023): folder-form epics ALWAYS carry the key —
	// present-empty archives all sync-owned artifacts server-side; file-form
	// epics never touch them.
	if folderForm(epic) {
		specs := make([]any, len(epic.Specs))
		for i, s := range epic.Specs {
			specs[i] = map[string]any{
				"external_id": s.Rel,
				"name":        s.Name,
				"position":    i + 1,
				"content_md":  capRunes(s.Content, specContentCap),
			}
		}
		payload["specs"] = specs
	}
	if line := approvalLineOf(recordText); line != "" {
		tag := approvalTagOf(line)
		payload["approval"] = map[string]any{
			"approved_at": tag[5:] + "T00:00:00.000000Z",
			"basis":       capAtWordBoundary(line, 300),
			"source_tag":  tag,
		}
	}
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	return Op{Type: "upsert_epic", Payload: payload}
}

// BuildRQGateOp — docs/85 decision requests → decision gates. Honesty rules
// as ops.js: answered only with a real source; closed-without-source dismissed.
func BuildRQGateOp(item RQ) Op {
	text := item.Heading + "\n" + item.Body
	userTag := userTagRe.FindString(text)
	closed := item.State == "closed"

	openedAt := "2026-07-01T00:00:00.000000Z"
	if item.Date != "" {
		openedAt = item.Date + "T00:00:00.000000Z"
	}
	originRef := "docs/85-loop-review-queue.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "decision",
		"title":       title,
		"body_md":     item.Body,
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	addGateOptions(payload, item.Options, item.Recommendation)
	addRichGateOptions(payload, item.Body)
	addGateBrief(payload, item.Body)

	switch {
	case closed && userTag != "":
		payload["state"] = "answered"
		// §245.3: the closure's own USER: date is the answer date; the
		// opened placeholder is not a fact about when a human decided.
		payload["answered_at"] = userTag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = userTag
		payload["answer"] = fmt.Sprintf("Resolved in the record — see %s in docs/85.", item.ID)
	case closed:
		payload["state"] = "dismissed"
	default:
		payload["state"] = "open"
	}
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)
	if item.EvaluatedScopeFingerprint != "" {
		payload["evaluated_scope_fingerprint"] = item.EvaluatedScopeFingerprint
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}

// SR-MC-044 acceptance round 1, defect 3 (EPIC-MC-004): the flattened OQ body
// is capped at 1200 UTF-16 units at extraction, and on the live gate that cut
// the trailing `recommendation:` field to "recom…" — unrecoverable by any
// downstream parser. The heading-form OQ is the only gate flavor whose
// recommendation lives ONLY in the body (RQ/approval gates carry it as a
// first-class field), so the builder recomposes: the wire brief fields
// (why_now / changes_if_approved / risk_if_wrong / recommendation) move BEFORE
// the long technical detail, and the cap falls on the technical tail only,
// with an honest ellipsis. Bodies without a recommendation label keep the
// exact legacy composition — no hash churn where the defect cannot occur.
// Ported from the retired node builder's composeOqGateBody; the label
// vocabulary mirrors the frontend gateBrief.ts WIRE_LABEL.
const oqBodyCap = 1200

var oqWireLabelRe = regexp.MustCompile(
	`(?i)(?:(\*\*)?(why[ _]now|changes[ _]if[ _]approved|risk[ _]if[ _]wrong|recommendation|technical[ _]detail|affects)(\*\*)?\s*:|\*\*(technical[ _]detail)\*\*)`)

// utf16Len counts JS String.length units, the unit capRunes caps in.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// capAtWordBoundary is decisionTitle's honesty rule, generalized: n is a
// display choice over an unbounded text column (postgres `text`, no contract
// limit), so the cap stays, but a silent cut mid-word is not honest —
// measured, 9 of 157 stored approval bases sat at exactly 300 characters,
// indistinguishable from a basis that simply ended there. Cuts at a word
// boundary within the same proportion of n that decisionTitle uses for its
// own 200-unit cap (120, i.e. 3/5 of n), and marks the cut with "…"; the full
// text is never truncated elsewhere.
func capAtWordBoundary(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf16Len(s) <= n {
		return s
	}
	head := capRunes(s, n-1)
	if i := strings.LastIndexAny(head, " \t"); i > n*3/5 {
		head = head[:i]
	}
	return strings.TrimRight(head, " \t") + "…"
}

func composeOQGateBody(item OQ) string {
	if item.Raw == "" {
		return item.Full // table-form rows: the question cell, never long
	}
	flat := strings.Join(strings.Fields(item.Title+" "+item.Raw), " ")
	ms := oqWireLabelRe.FindAllStringSubmatchIndex(flat, -1)
	type seg struct {
		key  string
		text string
	}
	var segs []seg
	hasReco := false
	for i, m := range ms {
		key := "technical_detail" // the bare **Technical detail** alternative
		if m[4] >= 0 {
			key = strings.ToLower(strings.ReplaceAll(flat[m[4]:m[5]], " ", "_"))
		}
		end := len(flat)
		if i+1 < len(ms) {
			end = ms[i+1][0]
		}
		segs = append(segs, seg{key: key, text: strings.TrimSpace(flat[m[0]:end])})
		if key == "recommendation" {
			hasReco = true
		}
	}
	if !hasReco {
		return item.Full
	}
	briefKeys := []string{"why_now", "changes_if_approved", "risk_if_wrong", "recommendation"}
	head := []string{}
	if lead := strings.TrimSpace(flat[:ms[0][0]]); lead != "" {
		head = append(head, lead)
	}
	for _, want := range briefKeys {
		for _, s := range segs {
			if s.key == want {
				head = append(head, s.text)
			}
		}
	}
	var tail []string
	for _, s := range segs {
		isBrief := false
		for _, want := range briefKeys {
			if s.key == want {
				isBrief = true
			}
		}
		if !isBrief {
			tail = append(tail, s.text)
		}
	}
	headText := strings.Join(head, " ")
	if len(tail) == 0 {
		return headText
	}
	tailText := strings.Join(tail, " ")
	remaining := oqBodyCap - utf16Len(headText) - 1
	if remaining <= 0 {
		return headText + " …"
	}
	if utf16Len(tailText) <= remaining {
		return headText + " " + tailText
	}
	return headText + " " + capRunes(tailText, remaining) + "…"
}

// BuildOQGateOp — process/08 open questions → question gates.
func BuildOQGateOp(item OQ) Op {
	openedAt := "2026-07-01T00:00:00.000000Z"
	originRef := "process/08-open-questions.md"
	if item.Line > 0 {
		originRef = fmt.Sprintf("%s:%d", originRef, item.Line)
	}
	title := item.Title
	if title == "" {
		title = item.ID
	}
	payload := map[string]any{
		"external_id": item.ID,
		"kind":        "question",
		"title":       title,
		"body_md":     composeOQGateBody(item),
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   openedAt,
	}
	// REQ-PLN-059: the row's suggested default is the author's recommendation.
	// Only set when present — "recommendation attached" on a gate with none
	// would be a worse lie than showing nothing.
	if item.Suggested != "" {
		payload["recommendation"] = item.Suggested
	}
	if item.Resolved {
		userTag := userTagRe.FindString(item.Full)
		payload["state"] = "answered"
		// §245.3: date the answer from the resolution's own USER: tag; only
		// a tagless resolution keeps the opened placeholder.
		if userTag != "" {
			payload["answered_at"] = userTag[5:] + "T00:00:00.000000Z"
			payload["source_tag"] = userTag
		} else {
			payload["answered_at"] = openedAt
			payload["source_tag"] = "DOC:process/08-open-questions.md"
		}
		payload["answer"] = item.Full
	} else {
		payload["state"] = "open"
	}
	// REQ-CROSS-142: read the brief from Raw, not Full. Full is
	// strings.Join(strings.Fields(...), " "), which collapses every newline,
	// and briefLineRe is anchored at ^\s*[-*] — so a bullet list flattened onto
	// one line matches nothing. Raw exists for exactly this.
	addGateBrief(payload, item.Raw)
	addGateHolds(payload, item.Pool, "pooled behind "+item.ID)
	if item.EvaluatedScopeFingerprint != "" {
		payload["evaluated_scope_fingerprint"] = item.EvaluatedScopeFingerprint
	}

	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}
}

var (
	specStatusSectionRe = regexp.MustCompile(`(?s)## Specification status\s*\n(.*?)(\n## |\z)`)

	// The template's home for the specification decision: who approved it, in
	// what role, on whose authority. The marker section above states the stage;
	// this section records the answer (REQ-CROSS-114).
	specApprovalSectionRe = regexp.MustCompile(`(?is)## specification approval\n(.*?)(\n##|$)`)
)

// SpecApprovalLineOf reports where an epic's specification stands, from the two
// places a record puts it.
//
// The `## Specification status` marker (REQ-CROSS-024, EPIC-SYNC-006) is the
// record's own statement of stage: the FIRST non-empty line of the section.
// "approved" requires a USER:YYYY-MM-DD tag on that line — a claimed approval
// without a source stays at "ready" (the gate stays open; same honesty rule as
// approvalLineOf), and ("", "") means a legacy or as-built record whose stage
// this reader does not recognize.
//
// A granted, sourced row under `## Specification approval` — the installed
// template's shape — is that missing source. It lifts a SPEC-READY record, and
// a record with no marker section at all, to approved and supplies the
// attributed line. It does NOT override the record's own stage: a SPEC-DRAFT or
// SPEC-DERIVED record that also carries a granted table contradicts itself, and
// the conservative reading is published while collectEpicSpecs warns about the
// contradiction rather than resolving it silently.
func SpecApprovalLineOf(text string) (marker string, line string) {
	marker, line = specStatusMarkerOf(text)
	if marker == "ready" || (marker == "" && !specStatusSectionRe.MatchString(text)) {
		if row := specApprovalRowOf(text); row != "" {
			return "approved", row
		}
	}
	return marker, line
}

// specApprovalRowOf is the granted, USER:-tagged line under
// `## Specification approval`, latest date winning — the same rule
// latestApprovalLine applies to the completion section.
func specApprovalRowOf(text string) string {
	section := specApprovalSectionRe.FindStringSubmatch(text)
	if section == nil {
		return ""
	}
	var best, bestTag string
	lines := strings.Split(section[1], "\n")
	header := ""
	for i, line := range lines {
		if isTableHeader(lines, i) {
			header = line
			continue
		}
		tag := approvalTagOf(line)
		if tag == "" || !isSpecApproval(line, header) {
			continue
		}
		if bestTag == "" || tag > bestTag { // ISO-8601 dates sort lexically
			best, bestTag = strings.TrimSpace(line), tag
		}
	}
	return best
}

func specStatusMarkerOf(text string) (marker string, line string) {
	section := specStatusSectionRe.FindStringSubmatch(text)
	if section == nil {
		return "", ""
	}
	for _, l := range strings.Split(section[1], "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "SPEC-DRAFT"):
			return "draft", l
		case strings.HasPrefix(l, "SPEC-READY"):
			return "ready", l
		case strings.HasPrefix(l, "SPEC-APPROVED"):
			// REQ-CROSS-111: the marker is prose and prose wraps, so the tag may
			// sit on a continuation line of this same section — read from the
			// marker line alone, an epic approved and shipped had its spec gate
			// re-opened. Return the line that CARRIES the tag, so the caller
			// still receives the attributed evidence rather than a bare claim.
			if userTagRe.MatchString(l) {
				return "approved", l
			}
			for _, cont := range strings.Split(section[1], "\n") {
				if userTagRe.MatchString(cont) {
					return "approved", strings.TrimSpace(cont)
				}
			}
			return "ready", l
		default:
			return "", ""
		}
	}
	return "", ""
}

// BuildSpecApprovalGateOp — the pre-implementation specification gate
// (REQ-CROSS-024): SPEC-READY opens SPEC-APPROVE-<EPIC>, SPEC-APPROVED with a
// USER: tag closes it. Only folder-form epics WITH specs gate — an approval
// request over nothing is dishonest (the missing-specs case is warned at
// snapshot time). Holds the EPIC (the implementation-approval gate holds the
// requirements — different concern).
func BuildSpecApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	marker, line := SpecApprovalLineOf(recordText)
	if !folderForm(epic) || len(epic.Specs) == 0 || (marker != "ready" && marker != "approved") {
		return Op{}, false
	}

	title := "Approve specs: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}

	var index strings.Builder
	index.WriteString("Specs awaiting approval:\n")
	for _, s := range epic.Specs {
		fmt.Fprintf(&index, "- %s (%d chars)\n", s.Rel, len([]rune(s.Content)))
	}
	index.WriteString("\n" + line + "\n")
	if ids := requirementIDsOfSection(recordText); len(ids) > 0 {
		index.WriteString("\nRequirements: " + strings.Join(ids, " · ") + "\n")
	}

	payload := map[string]any{
		"external_id": "SPEC-APPROVE-" + epic.ID,
		"kind":        "spec_approval",
		"title":       title,
		"body_md":     capRunes(index.String(), 6000),
		"origin":      "workspace",
		"origin_ref":  epic.Record,
		"opened_at":   "2026-08-09T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve specs — implementation may start", "body": "Record the approval (USER: tag) in the record's Specification status; the loop may then write its first RED test."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send the specs back with what must change before approval."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Keep the epic at the specification stage."},
		},
		"recommendation": "The specs/ folder is the review pack — grounded claims only; ungrounded content is grounds for changes.",
		"holds": []any{
			map[string]any{"held_external_id": epic.ID, "held_entity_type": "epic", "note": "specs awaiting approval"},
		},
	}

	if marker == "approved" {
		tag := userTagRe.FindString(line)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capRunes(line, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// BuildApprovalGateOp — an epic at the human gate IS a queue item; a recorded
// approval closes the same gate as a repo-borne answer. Returns zero Op
// (ok=false) when the epic is neither.
func BuildApprovalGateOp(epic Epic, recordText string) (Op, bool) {
	approvalLine := approvalLineOf(recordText)
	awaiting := epic.State == "awaiting-approval"
	if !awaiting && approvalLine == "" {
		return Op{}, false
	}
	// REQ-CROSS-096: a completion gate over nothing is as dishonest as a spec
	// gate over nothing, and the spec side already refuses. An epic with no
	// upper or lower evidence has nothing to approve, so a work-list cell
	// reading "pending" must not open one.
	//
	// The refusal applies ONLY to a gate that would be newly opened: on this
	// corpus 14 of 22 existing approval gates belong to epics with no
	// Upper/Lower cells and are already ANSWERED, and withholding those would
	// rewrite history rather than prevent a dishonest ask.
	if approvalLine == "" && !hasEpicEvidence(epic) {
		return Op{}, false
	}

	title := "Approve: " + epic.ID
	if h1 := epicH1Re.FindStringSubmatch(recordText); h1 != nil {
		title += " — " + strings.TrimSpace(h1[1])
	}
	originRef := epic.Record
	if originRef == "" {
		originRef = "WORKLIST.md"
	}
	payload := map[string]any{
		"external_id": "APPROVE-" + epic.ID,
		"kind":        "approval_request",
		"title":       title,
		"body_md":     capRunes(recordText, 6000),
		"origin":      "workspace",
		"origin_ref":  originRef,
		"opened_at":   "2026-07-22T00:00:00.000000Z",
		"options": []any{
			map[string]any{"key": "approve", "label": "Approve — evidence reviewed", "body": "Record the approval (USER: tag); the loop materializes it into the epic record."},
			map[string]any{"key": "request_changes", "label": "Request changes", "body": "Send it back with what must change before the gate."},
			map[string]any{"key": "defer", "label": "Defer", "body": "Not now — keep it at the gate."},
		},
		"recommendation": "The record’s evidence map is the review pack — read it, then approve, request changes, or defer.",
	}
	addGateHolds(payload, requirementIDsOfSection(recordText), "awaiting the "+epic.ID+" gate")

	if approvalLine != "" {
		tag := approvalTagOf(approvalLine)
		payload["state"] = "answered"
		payload["answered_at"] = tag[5:] + "T00:00:00.000000Z"
		payload["source_tag"] = tag
		payload["answer"] = capAtWordBoundary(approvalLine, 300)
		payload["chosen_option_keys"] = []any{"approve"}
	} else {
		payload["state"] = "open"
	}

	addGateBrief(payload, recordText)
	payload["content_hash"] = ContentHash(payload)
	payload["actor"] = actor
	if payload["state"] == "answered" {
		payload["answerer"] = map[string]any{"kind": "human"}
	}
	return Op{Type: "upsert_gate", Payload: payload}, true
}

// requirementIDsOfSection remains as the gate-call compatibility boundary;
// all consumers now share the same declaration vocabulary and expansion.
func requirementIDsOfSection(text string) []string {
	return requirementIDsOf(text)
}

func addGateOptions(payload map[string]any, options []Option, recommendation string) {
	if len(options) > 0 {
		opts := make([]any, len(options))
		for i, o := range options {
			label := o.Label
			if label == "" {
				label = fmt.Sprintf("(%d)", i+1)
			}
			opts[i] = map[string]any{
				"key":   label,
				"label": capRunes(o.Text, 120),
				"body":  o.Text,
			}
		}
		payload["options"] = opts
	}
	if recommendation != "" {
		payload["recommendation"] = recommendation
	}
}

func addGateHolds(payload map[string]any, pool []string, note string) {
	if len(pool) == 0 {
		return
	}
	holds := make([]any, len(pool))
	for i, id := range pool {
		entityType := "requirement"
		if epicRefRe.MatchString(id) {
			entityType = "epic"
		}
		holds[i] = map[string]any{
			"held_external_id": id,
			"held_entity_type": entityType,
			"note":             note,
		}
	}
	payload["holds"] = holds
}

// BuildCommitBurstOp folds the recent git log into one commit_burst event per
// CLOSED day (today's burst is still growing — it lands once the day closes).
func BuildCommitBurstOp(commits []Commit, today string) (Op, bool) {
	byDay := map[string][]Commit{}
	var days []string
	for _, c := range commits {
		if len(c.Date) < 10 {
			continue
		}
		day := c.Date[:10]
		if day >= today {
			continue
		}
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], c)
	}
	if len(byDay) == 0 {
		return Op{}, false
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))

	events := make([]any, 0, len(days))
	for _, day := range days {
		dayCommits := byDay[day]
		ids := []string{}
		seen := map[string]bool{}
		for _, c := range dayCommits {
			for _, id := range c.IDs {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		related := make([]any, len(ids))
		for i, id := range ids {
			related[i] = map[string]any{"external_id": id, "type": "requirement"}
		}
		subjects := []any{}
		for i, c := range dayCommits {
			if i == 5 {
				break
			}
			subjects = append(subjects, c.Subject)
		}
		events = append(events, map[string]any{
			"occurred_at":         day + "T23:59:59.000000Z",
			"event_type":          "commit_burst",
			"subject_external_id": day,
			"subject_type":        "commit_burst",
			"source_tag":          "GIT:" + day,
			"related":             related,
			"payload": map[string]any{
				"count":    len(dayCommits),
				"head":     dayCommits[0].Hash, // newest first
				"subjects": subjects,
			},
			"dedupe_key": "commit_burst:" + day,
		})
	}

	return Op{
		Type: "emit_events",
		Payload: map[string]any{
			"external_id": "EVENTS-COMMITS-" + today,
			"actor":       actor,
			"events":      events,
		},
	}, true
}

// ---------------------------------------------------------------- assembly

// BuildOps assembles the full op batch in dependency order: requirements,
// epics (which link them), gates (string refs — order-independent), burst.
// OBSOLETE rows are not synced; 'finding' RQs stay records, not gates.
func BuildOps(data Data, readRecord func(string) string, today string) []Op {
	var ops []Op

	// Epic records are read first because the user requirements they define are
	// the PARENTS of the ledger rows below, and a parent that arrives after its
	// child leaves the link unresolved for a batch (REQ-CROSS-048/049).
	records := map[string]string{}
	for _, e := range data.Epics {
		text := ""
		if e.Record != "" && readRecord != nil {
			// RecordFS resolves from the sync root when the worklist lives
			// above it (SCN-SY-046); Record stays the payload identity.
			readPath := e.Record
			if e.RecordFS != "" {
				readPath = e.RecordFS
			}
			text = readRecord(readPath)
		}
		records[e.ID] = text
	}
	// SR-SY-1403: file-derived user requirements first — they are the richer
	// records (statement, status, sources, scenarios) — then the epic-defined
	// ones, an id declared in both places keeping its file-derived row. Last,
	// REQ-CROSS-261 recovers a rollup-only id only when its named record supplies
	// the statement; it never promotes rollup prose into content.
	seenUR := map[string]bool{}
	for _, ur := range data.UserReqs {
		if seenUR[ur.ID] {
			continue
		}
		seenUR[ur.ID] = true
		ops = append(ops, BuildLedgerUserRequirementOp(ur))
	}
	for _, e := range data.Epics {
		// One op per declared user requirement: a membership edge needs an
		// entity to point at, and a second declaration is as real as the first.
		for _, ur := range ParseEpicUserRequirements(records[e.ID]) {
			if seenUR[ur.ID] {
				continue
			}
			seenUR[ur.ID] = true
			ops = append(ops, BuildUserRequirementOp(ur, e))
		}
	}
	ledgerMentionedUR := map[string]bool{}
	for _, req := range data.Reqs {
		for _, id := range urTokenRe.FindAllString(req.UR, -1) {
			ledgerMentionedUR[id] = true
		}
	}
	for _, e := range data.Epics {
		for _, ur := range rollupUserRequirements(e.URCell, records[e.ID]) {
			// The rollup carrier closes the newly exposed denominator only. An
			// id already named by a ledger row is existing, accepted residue and
			// is not silently reclassified by this recovery path.
			if seenUR[ur.ID] || ledgerMentionedUR[ur.ID] {
				continue
			}
			seenUR[ur.ID] = true
			// The record says what the outcome is, but carries no lifecycle for
			// this undeclared UR. PROPOSED is the deliberate non-invented state;
			// fidelity discloses any weaker-than-epic discrepancy.
			ops = append(ops, BuildUserRequirementOp(ur, Epic{ID: e.ID, State: "proposed"}))
		}
	}

	// REQ-CROSS-064: which requirements an as-built epic carries.
	asBuilt := map[string]bool{}
	asBuiltURs := map[string]bool{}
	for _, e := range data.Epics {
		if !isAsBuiltEpic(records[e.ID]) {
			continue
		}
		section := records[e.ID]
		if m := reqSectionRe.FindStringSubmatch(section); m != nil {
			section = m[1]
		}
		for _, id := range reqIDGlobalRe.FindAllString(section, -1) {
			asBuilt[id] = true
		}
		// The epic's own list writes ranges ("REQ-SYS-050 … REQ-SYS-059"), so it
		// names a fraction of what it carries. The exact set is every row whose
		// PARENT is the user requirement this as-built epic defines — the link
		// REQ-CROSS-049 already puts on every derived row.
		if ur, ok := ParseEpicUserRequirement(records[e.ID]); ok {
			asBuiltURs[ur.ID] = true
		}
	}

	// REQ-CROSS-265: the requirement inventory a scenario edge may point at.
	// Built from the ops this same build emits, so an edge can never outlive
	// the requirement it names.
	known := map[string]bool{}
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			if id, _ := op.Payload["external_id"].(string); id != "" {
				known[id] = true
			}
		}
	}
	for _, r := range data.Reqs {
		// REQ-CROSS-222 §222.4: OBSOLETE rows sync with their terminal status
		// and superseding reference — a terminal decision is process history,
		// not something to drop (they were skipped before the import).
		parentUR := firstURRef(r.UR)
		op := BuildRequirementOpWithExemption(r, asBuilt[r.ID] || asBuiltURs[parentUR])
		if id, _ := op.Payload["external_id"].(string); id != "" {
			known[id] = true
		}
		ops = append(ops, op)
	}
	for _, b := range data.Backlog {
		ops = append(ops, BuildBacklogOp(b))
	}
	for _, e := range data.Epics {
		record := records[e.ID]
		epicOp := BuildEpicOp(e, record)
		// A membership edge needs an entity to point at. The corpus names user
		// requirements this batch cannot define — the accepted irrecoverable
		// UR-FE-* set — and the server drops an id it cannot resolve, so the
		// claim comes back absent and the run's readback fails on it. Those
		// ids are already disclosed by their own `ur-record` accept keys, and
		// the epic→UR arm still counts declarations against emitted ops, so
		// dropping the edge here removes a false claim, not a disclosure.
		epicOp = pruneURedges(epicOp, seenUR)
		// REQ-CROSS-248: register acceptances become gates; the completion
		// acceptance feeds the quartet under the stated precedence.
		accOps, quartet := BuildAcceptanceGateOps(e, record)
		specOp, specOK := BuildSpecApprovalGateOp(e, record)
		apprOp, apprOK := BuildApprovalGateOp(e, record)
		if quartet == nil && !apprOK {
			// the rollup cell is the only recorded acceptance — unless it
			// merely references a register row already gated above
			if wOp, wq, ok := BuildWorklistAcceptanceGate(e); ok && !cellReferencesGated(e.ApprovalCell, accOps) {
				accOps = append(accOps, wOp)
				quartet = wq
			}
		}
		// One fact, one gate: a register row that reuses the APPROVE-<epic>
		// id names the same acceptance the record-derived gate answers — the
		// record gate wins, the row's attribution already rode the quartet.
		if apprOK {
			apprID, _ := apprOp.Payload["external_id"].(string)
			kept := accOps[:0]
			for _, op := range accOps {
				if op.Payload["external_id"] != apprID {
					kept = append(kept, op)
				}
			}
			accOps = kept
		}
		epicOp = applyEpicApproval(epicOp, quartet)
		ops = append(ops, epicOp)
		// REQ-CROSS-247: the record's D-* decisions ride as decision gates
		ops = append(ops, BuildDecisionGateOps(e, record)...)
		// REQ-CROSS-252: every declared scenario definition, spec files
		// included, rides as a first-class scenario record
		ops = append(ops, BuildScenarioOps(e, record, known)...)
		ops = append(ops, accOps...)
		if specOK {
			ops = append(ops, specOp)
		}
		if apprOK {
			ops = append(ops, apprOp)
		}
	}
	// REQ-CROSS-264: every declared task, after the epic ops — the applier
	// resolves the owning epic by code, so a task ahead of its epic has no
	// parent to land under.
	ops = append(ops, BuildTaskOps(data, records)...)

	for _, rq := range data.RQs {
		if rq.State == "finding" {
			continue
		}
		ops = append(ops, BuildRQGateOp(rq))
	}
	for _, oq := range data.OQs {
		ops = append(ops, BuildOQGateOp(oq))
	}
	// REQ-CROSS-237: confirmation gates from the gate flat-file. Warnings are
	// dropped here — `Snapshot` is the one that reports to the user — but the
	// ops are what a derived corpus needs to become answerable.
	if len(data.Gates) > 0 {
		gateOps, _ := BuildGateFileOps(data.Gates, data.GatesRef)
		ops = append(ops, gateOps...)
	}
	if op, ok := BuildCommitBurstOp(data.Commits, today); ok {
		ops = append(ops, op)
	}
	// REQ-CROSS-084: the documents that explain this codebase. System-scoped, so
	// they are neither specs (Epic-scoped) nor ledger rows.
	for _, d := range data.Docs {
		ops = append(ops, BuildDocumentOp(d))
	}

	// D5 "lands as preserved text", §225.6 successor (USER:2026-08-24): every
	// epic record archives byte-exact in process_records — the flip retires
	// the FILE while its bytes survive with sha256 identity, unsplit.
	archiveRevision := ""
	if len(data.Commits) > 0 {
		archiveRevision = data.Commits[0].Hash
	}
	seenRecordDoc := map[string]bool{}
	for _, e := range data.Epics {
		text := records[e.ID]
		if text == "" || e.Record == "" || seenRecordDoc[e.Record] {
			continue
		}
		seenRecordDoc[e.Record] = true
		ops = append(ops, BuildProcessRecordOp(e.Record, "epic", text, archiveRevision))
	}

	// Every other retiring file rides the same way, verbatim. WORKLIST work
	// rows, docs/85 finding blocks and over-cap OQ bodies build no structured
	// op by design; the ledgers, the backlog, the gap register and everything
	// beside an epic record inside epics/** build a structured op that carries
	// SOME of the file and never its bytes. After the flip the repository does
	// not hold any of them, so whatever no carrier names exists nowhere.
	//
	// Extension is irrelevant and content is untouched: a contract file is
	// carried as the bytes it is, not reshaped into markdown. The type stays
	// `process` for every archival document — the server's document ingest
	// waives the embedding requirement for exactly that type, and preservation
	// must not depend on an embedding provider being reachable.
	//
	// Snapshot resolves the list; the default names are only a fallback for
	// callers that built Data without one.
	archival := data.ArchivalFiles
	if archival == nil {
		archival = archivalProcessFiles
	}
	// Carried once is carried: an epic record already rode above as its
	// verbatim document, and an epic spec rides inside its epic payload. A
	// second document for the same path would be a duplicate carrier, not
	// better coverage.
	carriedElsewhere := map[string]bool{}
	for recordRel := range seenRecordDoc {
		carriedElsewhere[recordRel] = true
	}
	for _, e := range data.Epics {
		for _, s := range e.Specs {
			carriedElsewhere[s.Rel] = true
		}
	}
	for _, rel := range archival {
		if carriedElsewhere[rel] {
			continue
		}
		text := ""
		if readRecord != nil {
			text = readRecord(rel)
		}
		if text == "" {
			continue
		}
		// §225.6 successor: the byte archive does not split — bytea has no
		// content cap, and reassembly seams stop existing.
		ops = append(ops, BuildProcessRecordOp(rel, "document", text, archiveRevision))
	}

	return ops
}

// archivalProcessFiles are the flip-retired files whose rows or blocks build
// no structured op; each is carried whole so nothing is lost with the file.
var archivalProcessFiles = []string{
	"WORKLIST.md",
	"docs/85-loop-review-queue.md",
	"process/08-open-questions.md",
}

// ---------------------------------------------------------------- trace paths

var traceLine = regexp.MustCompile(`^-\s+\*\*(Tests|Code):\*\*`)
var backtickRe = regexp.MustCompile("`([^`]+)`")
var toolPrefixRe = regexp.MustCompile(`^(npm|mix|node)\b`)

// ExtractTracePaths — the backticked file paths in a detail block's
// Tests:/Code: lines (the drift command's diff scope).
func ExtractTracePaths(detail string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, line := range strings.Split(detail, "\n") {
		if !traceLine.MatchString(line) {
			continue
		}
		for _, m := range backtickRe.FindAllStringSubmatch(line, -1) {
			token := strings.TrimSpace(m[1])
			if !strings.ContainsAny(token, " \t") && strings.ContainsAny(token, "/.") && !toolPrefixRe.MatchString(token) {
				if !seen[token] {
					seen[token] = true
					paths = append(paths, token)
				}
			}
		}
	}
	sort.Strings(paths)
	return paths
}

// ApprovalLineOf exposes the record's recorded approval to other packages
// (REQ-CROSS-030's gate). Exported deliberately rather than reimplemented: two
// answers to "is this epic approved?" would drift, and drift between two
// implementations of one rule is the defect this workspace keeps finding.
func ApprovalLineOf(recordText string) string { return approvalLineOf(recordText) }

// ApprovalTagOf exposes the tag that dates an approval line (see approvalTagOf).
func ApprovalTagOf(approvalLine string) string { return approvalTagOf(approvalLine) }

// CompletionApprovalIn returns the granted completion approval recorded in a
// fragment of a record — a work-list approval cell, or a section body — or ""
// when it records none. It is the same judgment ApprovalLineOf applies to a
// whole record, exposed so no caller has to restate the rule.
func CompletionApprovalIn(fragment string) string { return latestApprovalLine(fragment) }

// BuildDocumentOp carries one explanatory document or discovery guide. Identity
// is the workspace-relative path; the hash is over the content, so an unchanged
// document is an unchanged op and costs nothing on re-sync.
func BuildDocumentOp(d Document) Op {
	payload := map[string]any{
		"external_id":   d.Path,
		"name":          d.Name,
		"document_type": d.Type,
		"provenance":    d.Provenance,
		"content_md":    d.Content,
	}
	payload["content_hash"] = ContentHash(payload)
	return Op{Type: "upsert_document", Payload: payload}
}

// hasEpicEvidence reports whether an epic records any loop evidence. An em-dash
// or a blank cell is absence, not a status, so neither counts.
func hasEpicEvidence(e Epic) bool {
	for _, cell := range []string{e.Upper, e.Lower} {
		t := strings.TrimSpace(cell)
		t = strings.Trim(t, "—-– ")
		if t != "" {
			return true
		}
	}
	return false
}
