package rdd

// The verifies trace graph asserted verification by things that are not
// tests: measured against the local store, 1,061 verifies edges, 286 of them
// not tests — 253 prose/placeholder, 19 a real source file, 14 a bare
// extension. Two functions both assign a citation's kind by WHICH LABEL OR
// COLUMN it was typed under rather than by what the text actually is:
//
//   - evidenceRefsFromDetail's switch: under any detail-block label other than
//     "Code:", every extracted ref became a test reference outright — a bare
//     extension enumerated in prose (REQ-SYS-179: `.ex`, `.md`, …), a real but
//     non-test file (`CLAUDE.md`, `structure.sql`, `PROCESS.md`), and even an
//     unrelated slash-bearing mention (`Req.Test.transport_error/2`, an Elixir
//     MFA/arity reference, not a path) all became "verified by" the requirement.
//   - cellEvidenceCitations: the dashboard Tests/Code cell's fallback (used
//     only when the detail block names no citation at all) tagged whatever text
//     it found by which COLUMN it came from, so a verdict sentence like
//     "not verified — no test covers the middleware branch" (REQ-AUTH-021)
//     became a literal test citation.
//
// This file pins the corrected rule: looksLikeTest wins on shape (widened to
// recognize a bare Go TestXxx identifier, which has no path/extension at all);
// a Code: bullet still trusts its own label unconditionally (unchanged, that
// axis measured clean); otherwise a real file (extension with a non-empty
// basename) becomes code so the content is not lost; anything else is not a
// stable citation to anything and does not become a fabricated test or code
// edge.

import (
	"encoding/json"
	"strings"
	"testing"
)

func evidenceOf(t *testing.T, req Req) map[string]any {
	t.Helper()
	if req.ID == "" {
		req.ID = "REQ-TST-900"
	}
	if req.Title == "" {
		req.Title = "t"
	}
	if req.Ctx == "" {
		req.Ctx = "TST"
	}
	if req.Status == "" {
		req.Status = "PENDING_VERIFICATION"
	}
	return BuildRequirementOp(req).Payload
}

func kindsOf(t *testing.T, p map[string]any, ref string) []string {
	t.Helper()
	var out []string
	cits, _ := p["source_citations"].([]any)
	for _, c := range cits {
		cm, _ := c.(map[string]any)
		if r, _ := cm["ref"].(string); r == ref {
			k, _ := cm["kind"].(string)
			out = append(out, k)
		}
	}
	return out
}

// The exact REQ-SYS-179 shape: an Evidence bullet enumerating bare extensions
// while naming files that are not tests. None of it is a test citation.
func TestBareExtensionUnderEvidenceIsNotATestCitation(t *testing.T) {
	detail := strings.Join([]string{
		"- **Statement:** Change detection enumerates only analysable files.",
		"- **Evidence:** The \"added\" files are `CLAUDE.md`, `README.md`, `AGENTS.md`, `package-lock.json` and similar. `file_analyses.relative_path` contains only `.ex` (740), `.exs` (340) — **zero** `.md` or `.json`.",
		"- **Code:** `@analyzable_extensions` is exposed from `EncryptedRepoSourceRepository`.",
		"- **Tests:** `apps/core/test/core/analysis/change_tracker_scope_test.exs` — 4 tests, RED first.",
	}, "\n")
	p := evidenceOf(t, Req{Detail: detail})
	blob, _ := json.Marshal(p["source_citations"])
	body := string(blob)

	for _, bareExt := range []string{".ex", ".exs", ".md", ".json"} {
		if got := kindsOf(t, p, bareExt); len(got) != 0 {
			t.Errorf("bare extension %q must not become any citation, got kind(s) %v: %s", bareExt, got, body)
		}
	}
	for _, realFile := range []string{"CLAUDE.md", "README.md", "AGENTS.md", "package-lock.json"} {
		got := kindsOf(t, p, realFile)
		if len(got) != 1 || got[0] != "code" {
			t.Errorf("real non-test file %q must become exactly one code citation, got %v: %s", realFile, got, body)
		}
	}
	if got := kindsOf(t, p, "apps/core/test/core/analysis/change_tracker_scope_test.exs"); len(got) != 1 || got[0] != "test" {
		t.Errorf("the genuine test file must still classify as test, got %v: %s", got, body)
	}
}

// An Elixir MFA/arity mention ("Module.function/2", REQ-SYS-156's real
// "customer_docs_priors/2") contains a slash but is not a path to anything —
// it must not become a fabricated test OR code edge.
func TestArityStyleMentionIsDroppedNotFabricatedAsATest(t *testing.T) {
	detail := "- **Tests:** `apps/core/test/foo_test.exs` — RED first, proving `customer_docs_priors/2` had no caller.\n"
	p := evidenceOf(t, Req{Detail: detail})
	blob, _ := json.Marshal(p["source_citations"])
	body := string(blob)

	if got := kindsOf(t, p, "customer_docs_priors/2"); len(got) != 0 {
		t.Errorf("an MFA/arity mention must not become a citation of any kind, got %v: %s", got, body)
	}
	if got := kindsOf(t, p, "apps/core/test/foo_test.exs"); len(got) != 1 || got[0] != "test" {
		t.Errorf("the real test citation on the same line must survive: %v: %s", got, body)
	}
}

// Go convention: a bare "TestXxx" identifier IS the stable test identity, with
// no file extension or path at all. Dropping the over-broad rule must not
// start dropping these.
func TestBareGoTestFunctionNameStillClassifiesAsTest(t *testing.T) {
	detail := "- **Tests:** covered by `TestSyncFamilyPartialInstallIsReportedAsPartial` in the CLI suite\n"
	p := evidenceOf(t, Req{Detail: detail})
	if got := kindsOf(t, p, "TestSyncFamilyPartialInstallIsReportedAsPartial"); len(got) != 1 || got[0] != "test" {
		blob, _ := json.Marshal(p["source_citations"])
		t.Fatalf("a bare Go test function name must classify as test, got %v: %s", got, blob)
	}
}

// A real source file named in an Evidence bullet — not under a Code: label —
// is still a file. It must become a code citation, not a fabricated test.
func TestRealFileUnderEvidenceLabelBecomesCode(t *testing.T) {
	detail := "- **Evidence:** the migration is `structure.sql`, ratified per `PROCESS.md`\n"
	p := evidenceOf(t, Req{Detail: detail})
	for _, ref := range []string{"structure.sql", "PROCESS.md"} {
		if got := kindsOf(t, p, ref); len(got) != 1 || got[0] != "code" {
			blob, _ := json.Marshal(p["source_citations"])
			t.Errorf("%q must become a code citation (real file, not a test), got %v: %s", ref, got, blob)
		}
	}
}

// REQ-AUTH-021's real shape: no detail block, only a dashboard Tests cell
// carrying a verdict sentence with no backticks. It must not become a fake
// test citation — the content survives as a note instead.
func TestProseTestsCellBecomesANoteNotAFabricatedTest(t *testing.T) {
	p := evidenceOf(t, Req{Tests: "not verified — no test covers the middleware branch"})
	blob, _ := json.Marshal(p["source_citations"])
	body := string(blob)
	if strings.Contains(body, `"kind":"test"`) {
		t.Fatalf("the verdict sentence must not become a test citation: %s", body)
	}
	if !strings.Contains(body, "no test covers the middleware branch") {
		t.Fatalf("the sentence's content must survive somewhere (as a note): %s", body)
	}
	if !strings.Contains(body, `"kind":"note"`) {
		t.Fatalf("the sentence must be carried as a note: %s", body)
	}
}

// A non-test file cited in the dashboard Tests column (not the Code column)
// still names a file — the column is a weak prior, not the source of truth.
func TestFileShapedRefInTestsCellStillBecomesCode(t *testing.T) {
	p := evidenceOf(t, Req{Tests: "`bar.ex`"})
	if got := kindsOf(t, p, "bar.ex"); len(got) != 1 || got[0] != "code" {
		blob, _ := json.Marshal(p["source_citations"])
		t.Fatalf("bar.ex cited under Tests must still classify as code (it is not a test), got %v: %s", got, blob)
	}
}
