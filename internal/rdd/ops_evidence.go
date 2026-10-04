package rdd

import (
	"regexp"
	"sort"
	"strings"
)

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
