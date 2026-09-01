package rdd

// REQ-CROSS-249 (EPIC-CLI-003 T12): historical RUN: tokens import as
// evidence records — kind migration, explicit inherited_unverified validity
// on every result, preserved raw text, run identity encoding the historical
// run so a re-import rewrites nothing.

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

func evidenceImportOf(t *testing.T, root string) EvidenceImport {
	t.Helper()
	data, _ := Snapshot(root, manifest.Default())
	records := map[string]string{}
	for _, e := range data.Epics {
		rel := e.RecordFS
		if rel == "" {
			rel = e.Record
		}
		records[e.ID] = ReadEpicRecord(root, rel)
	}
	return BuildEvidenceImport(data, records)
}

func TestRunTokensBecomeExplicitValidityEvidence(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | `+"`RUN:2026-08-20:harness-green`"+` 12/12 GREEN | — |

### REQ-CV-001 — Only behavior
- **Status:** DONE · **Stage:** MVP
- **Statement:** It behaves.
- **Evidence:** RED first `+"`RUN:2026-08-19`"+`, then green
`)
	imp := evidenceImportOf(t, root)

	if len(imp.Runs) != 2 {
		t.Fatalf("two distinct RUN tags must yield two runs, got %d: %v", len(imp.Runs), runIDs(imp))
	}
	for _, run := range imp.Runs {
		if run["kind"] != "migration" {
			t.Fatalf("an imported run carries kind migration: %v", run["kind"])
		}
		id, _ := run["external_id"].(string)
		if !strings.HasPrefix(id, "HIST-RUN:2026-08-") {
			t.Fatalf("run identity must encode the historical tag: %q", id)
		}
	}
	if len(imp.Results) != 2 {
		t.Fatalf("one result per token occurrence, got %d", len(imp.Results))
	}
	for _, res := range imp.Results {
		if res["validity"] != "inherited_unverified" {
			t.Fatalf("every historical result states inherited_unverified explicitly: %v", res["validity"])
		}
		if res["target_external_id"] != "REQ-CV-001" {
			t.Fatalf("target = %v", res["target_external_id"])
		}
		raw, _ := res["raw_evidence"].(string)
		if raw == "" {
			t.Fatalf("the raw token line must be preserved")
		}
	}
	// the verdict-bearing token reads as pass; the RED-first token as fail —
	// and the tokenless default would be skip, never pass
	byRun := map[string]string{}
	for _, res := range imp.Results {
		id, _ := res["run_external_id"].(string)
		result, _ := res["result"].(string)
		byRun[id] = result
	}
	if byRun["HIST-RUN:2026-08-20:harness-green"] != "pass" {
		t.Fatalf("GREEN verdict must read pass: %v", byRun)
	}
	if byRun["HIST-RUN:2026-08-19"] != "fail" {
		t.Fatalf("a RED-first token records the expected failure: %v", byRun)
	}
}

func TestTokenWithoutVerdictIsSkipNeverPass(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | derived `+"`RUN:2026-08-12`"+` | — | — |
`)
	imp := evidenceImportOf(t, root)
	if len(imp.Results) != 1 {
		t.Fatalf("results = %d", len(imp.Results))
	}
	if imp.Results[0]["result"] != "skip" {
		t.Fatalf("an outcome-less token asserts no outcome: %v", imp.Results[0]["result"])
	}
}

func runIDs(imp EvidenceImport) []string {
	out := make([]string, len(imp.Runs))
	for i, r := range imp.Runs {
		out[i], _ = r["external_id"].(string)
	}
	return out
}

// REQ-CROSS-260 — one result per distinct corpus line, each with its OWN
// verdict. The old key was (run, target): the first citing line's verdict stood
// for the whole group and every later line was appended into merged raw text.
// Measured on the corpus that produced this requirement: 834 lines discarded
// into 376 merged groups, 89 of them hiding a stated outcome behind a silent
// first line and 8 recording pass over a later RED.
func TestEachCitingLineBecomesItsOwnResultWithItsOwnVerdict(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Status:** DONE · **Stage:** MVP
- **Statement:** It behaves.
- **Evidence:** RED first `+"`RUN:2026-08-20:one-tag`"+` — the focused failure stood
- **Evidence:** then GREEN `+"`RUN:2026-08-20:one-tag`"+` — 12/12 passing
- **Evidence:** and GREEN again `+"`RUN:2026-08-20:one-tag`"+` — 12/12 after cleanup
`)
	imp := evidenceImportOf(t, root)

	if len(imp.Runs) != 1 {
		t.Fatalf("one tag is one run, got %d: %v", len(imp.Runs), runIDs(imp))
	}
	if len(imp.Results) != 3 {
		t.Fatalf("three citing lines must yield three results, got %d", len(imp.Results))
	}
	verdicts := map[string]int{}
	for _, res := range imp.Results {
		v, _ := res["result"].(string)
		verdicts[v]++
		raw, _ := res["raw_evidence"].(string)
		if !strings.Contains(raw, "Evidence:") {
			t.Fatalf("each result carries its own line's full text, got %q", raw)
		}
	}
	if verdicts["fail"] != 1 || verdicts["pass"] != 2 {
		t.Fatalf("each line keeps its own verdict — want 1 fail and 2 pass, got %v", verdicts)
	}
}

// No cap. The 600-rune ceiling existed only in the importer: the store column
// is unbounded text. It cost 263,715 runes across 198 results on the corpus
// this requirement was measured against.
func TestRawEvidenceIsNotTruncated(t *testing.T) {
	root := cleanEpicCorpus(t)
	long := "- **Evidence:** `RUN:2026-08-20:long` " + strings.Repeat("verbatim ", 200) + "END"
	if len([]rune(long)) <= 600 {
		t.Fatalf("the fixture line must exceed the old cap for this to mean anything, got %d runes", len([]rune(long)))
	}
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Status:** DONE · **Stage:** MVP
- **Statement:** It behaves.
`+long+"\n")
	imp := evidenceImportOf(t, root)

	if len(imp.Results) != 1 {
		t.Fatalf("results = %d", len(imp.Results))
	}
	raw, _ := imp.Results[0]["raw_evidence"].(string)
	if raw != long {
		t.Fatalf("the stored raw text must equal the source line exactly:\n got  (%d runes) %q\n want (%d runes) %q",
			len([]rune(raw)), raw, len([]rune(long)), long)
	}
}

// Identity is content-derived, so a rebuild over an unchanged corpus produces
// the same identities and an import changes nothing; editing ONE line moves
// exactly one identity. Both halves matter: an identity that moves on every run
// makes the evidence unmaintainable, and one that never moves cannot represent
// a corrected line.
func TestLineIdentityIsStableAcrossRebuildsAndMovesOnlyForTheEditedLine(t *testing.T) {
	root := cleanEpicCorpus(t)
	ledger := func(second string) string {
		return `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Status:** DONE · **Stage:** MVP
- **Evidence:** first ` + "`RUN:2026-08-20:idem`" + ` — 1/1 GREEN
- **Evidence:** ` + second + " `RUN:2026-08-20:idem`" + ` — 2/2 GREEN
`
	}
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", ledger("second"))
	first := resultIdentities(t, evidenceImportOf(t, root))
	again := resultIdentities(t, evidenceImportOf(t, root))
	if strings.Join(first, ",") != strings.Join(again, ",") {
		t.Fatalf("an immediate rebuild must change no identity:\n %v\n %v", first, again)
	}
	if len(first) != 2 {
		t.Fatalf("two distinct lines must yield two identities, got %v", first)
	}

	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", ledger("second, corrected"))
	edited := resultIdentities(t, evidenceImportOf(t, root))
	if len(edited) != 2 {
		t.Fatalf("the edit must not change the result COUNT, got %v", edited)
	}
	moved := 0
	was := map[string]bool{}
	for _, id := range first {
		was[id] = true
	}
	for _, id := range edited {
		if !was[id] {
			moved++
		}
	}
	if moved != 1 {
		t.Fatalf("editing one line must move exactly one identity, %d moved:\n was  %v\n now  %v", moved, first, edited)
	}
}

// The store refuses two results of one run that share an extended identity, so
// per-line results are only postable if the discriminator actually
// discriminates. Two lines, one run, one target: two identities.
func TestTwoResultsOnOneTargetCarryDistinctIdentities(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Evidence:** first `+"`RUN:2026-08-20:same-target`"+` — 1/1 GREEN
- **Evidence:** second `+"`RUN:2026-08-20:same-target`"+` — 2/2 GREEN
`)
	imp := evidenceImportOf(t, root)
	if len(imp.Results) != 2 {
		t.Fatalf("results = %d", len(imp.Results))
	}
	a, b := imp.Results[0], imp.Results[1]
	if a["run_external_id"] != b["run_external_id"] || a["target_external_id"] != b["target_external_id"] {
		t.Fatalf("the fixture must put both results on one run and one target: %v / %v", a, b)
	}
	if a["test_case_ref"] == b["test_case_ref"] {
		t.Fatalf("two results of one run and target must differ in the extended identity, both %v", a["test_case_ref"])
	}
}

// The corpus's real collision shape: one physical line citing the same tag more
// than once (the measured worst case is four times on one WORKLIST line). A
// per-occurrence token would give them identical identities and the store would
// hard-refuse the WHOLE evidence post — invisible at fixture scale. They
// collapse into that line's single result, and the collapse is counted rather
// than silent.
func TestRepeatedTagOnOneLineCollapsesIntoThatLinesResult(t *testing.T) {
	root := cleanEpicCorpus(t)
	tag := "`RUN:2026-08-20:worklist-shape`"
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Evidence:** `+tag+" "+tag+" "+tag+" "+tag+` — 4/4 GREEN
`)
	imp := evidenceImportOf(t, root)

	if len(imp.Results) != 1 {
		t.Fatalf("one line is one result however often it repeats its tag, got %d", len(imp.Results))
	}
	if imp.Occurrences != 4 {
		t.Fatalf("every occurrence must still be counted for the coverage arithmetic, got %d", imp.Occurrences)
	}
	if imp.Collapsed != 3 {
		t.Fatalf("the three repeats that collapsed must be disclosed, got %d", imp.Collapsed)
	}
}

// The discriminator is a FIXED-WIDTH digest, never the line text: the store's
// reference column is varchar(255) with no changeset length guard, and 927 of
// the corpus's ~1,790 token-bearing lines are longer than that — a raw-line
// token would 500 on the first real import.
func TestLineIdentityIsAFixedWidthDigest(t *testing.T) {
	root := cleanEpicCorpus(t)
	long := "- **Evidence:** `RUN:2026-08-20:wide` " + strings.Repeat("x", 4000)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | DONE | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Evidence:** short `+"`RUN:2026-08-20:wide`"+` — 1/1 GREEN
`+long+"\n")
	imp := evidenceImportOf(t, root)
	if len(imp.Results) != 2 {
		t.Fatalf("results = %d", len(imp.Results))
	}
	widths := map[int]bool{}
	for _, res := range imp.Results {
		ref, _ := res["test_case_ref"].(string)
		if !lineTokenRe.MatchString(ref) {
			t.Fatalf("the identity token must be a fixed-width digest, got %q", ref)
		}
		if len(ref) > 255 {
			t.Fatalf("the identity token must fit the reference column by construction, got %d chars", len(ref))
		}
		widths[len(ref)] = true
	}
	if len(widths) != 1 {
		t.Fatalf("a 4,000-character line and a short one must produce the same token width, got %v", widths)
	}
}

var lineTokenRe = regexp.MustCompile(`^line:[0-9a-f]{16}$`)

func resultIdentities(t *testing.T, imp EvidenceImport) []string {
	t.Helper()
	out := make([]string, 0, len(imp.Results))
	for _, res := range imp.Results {
		run, _ := res["run_external_id"].(string)
		target, _ := res["target_external_id"].(string)
		ref, _ := res["test_case_ref"].(string)
		if ref == "" {
			t.Fatalf("every result must carry its line identity: %v", res)
		}
		out = append(out, run+"|"+target+"|"+ref)
	}
	sort.Strings(out)
	return out
}
