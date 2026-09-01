package rdd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-044 (`USER:2026-08-12`): the kit ships two halves of one contract —
// the SKILL tells an agent what to write, the EXTRACTOR decides what is read —
// and nothing tested them against each other. Three shapes the skill prescribes
// were written correctly by real passes and silently read as nothing:
//
//	60 questions parsed as 0   (skill writes headings; parser wanted a table)
//	16 user requirements as 0  (## User requirement vs ## User outcome)
//	11 domain names as 0       (the ledger H1 carried a name nothing read)
//
// This fixture is written EXACTLY as the skill prescribes. If it stops
// extracting, one half has drifted from the other — which is invisible in
// production, because an extractor that recognises nothing reports nothing.
func TestSkillShapedWorkspaceRoundTrips(t *testing.T) {
	root := "testdata/skillshaped"
	read := func(p string) string {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}

	t.Run("requirements and their context name", func(t *testing.T) {
		reqs := ParseLedger("tasks/CON-REQUIREMENTS.md", read("tasks/CON-REQUIREMENTS.md"))
		if len(reqs) != 3 {
			t.Fatalf("want 3 rows, got %d", len(reqs))
		}
		if reqs[0].CtxName != "Consortiums" {
			t.Errorf("context name lost: %q", reqs[0].CtxName)
		}
		if reqs[2].Status != "BLOCKED" {
			t.Errorf("BLOCKED status lost: %q", reqs[2].Status)
		}
	})

	t.Run("the epic's user requirement", func(t *testing.T) {
		desc := ParseEpicDescription(read("epics/EPIC-CON-001/EPIC.md"))
		if desc == "" {
			t.Fatal("the UR did not parse — the product's own description reaches the platform empty")
		}
		if !strings.Contains(desc, "public-sector organization") {
			t.Errorf("UR text mangled: %q", desc)
		}
	})

	t.Run("the questions a human must answer", func(t *testing.T) {
		oqs := ParseOpenQuestions(read("process/08-open-questions.md"))
		if len(oqs) != 1 {
			t.Fatalf("want 1 question, got %d — nothing reaches the human queue", len(oqs))
		}
		if !strings.Contains(oqs[0].Title, "business rule") {
			t.Errorf("question title lost: %q", oqs[0].Title)
		}
	})
}

// The same drift, third instance: the skill writes epic FOLDERS and a compact
// four-column worklist; the extractor discovered epics only from an
// eleven-column worklist row. Sixteen epics on a real system synced as zero, so
// nothing carried the product's own description (`RUN:2026-08-12`).
func TestEpicsAreFoundWhenTheWorklistIsNotThisWorkspacesShape(t *testing.T) {
	root := "testdata/skillshaped"
	worklist, err := os.ReadFile(filepath.Join(root, "WORKLIST.md"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"EPIC-CON-001"}
	isDir := map[string]bool{"EPIC-CON-001": true}

	epics := ParseWorklist(string(worklist), files, isDir)
	if len(epics) == 0 {
		t.Fatal("no epics discovered — the product's journeys never reach the platform")
	}
	if epics[0].ID != "EPIC-CON-001" {
		t.Fatalf("wrong epic id: %q", epics[0].ID)
	}
	if epics[0].Record != "epics/EPIC-CON-001/EPIC.md" {
		t.Fatalf("record path lost: %q", epics[0].Record)
	}
}

// Fourth instance of the same drift: the skill wraps a statement across lines,
// the extractor read only the first, and every long description reached the
// platform cut mid-sentence — "…not a database enum. The model" (`RUN:2026-08-12`).
func TestAWrappedStatementIsReadWhole(t *testing.T) {
	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md",
		mustRead(t, "testdata/skillshaped/tasks/CON-REQUIREMENTS.md"))
	var target Req
	for _, r := range reqs {
		if r.ID == "REQ-CON-002" {
			target = r
		}
	}
	desc := ParseDescription(target.Detail)
	if !strings.Contains(desc, "human commitment") {
		t.Fatalf("statement truncated at its first line: %q", desc)
	}
}

func mustRead(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// REQ-CROSS-047 (`USER:2026-08-12`): the compliance views showed "Code: Missing"
// and "Test Cases: None" on all 1,014 requirements. The cause is upstream of
// sync: the Go extractor reads cells 1-5 and never reads the Tests (6) and Code
// (7) columns at all, so the richest data in every ledger — each file:line
// citation and each covering test — never leaves the workspace. The node mirror
// reads both, which is how the two builders silently disagreed.
func TestTestsAndCodeColumnsReachTheOp(t *testing.T) {
	ledger := "# CON — Consortiums · requirements ledger\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n" +
		"|----|-------|-------|--------|--------|-------|------|\n" +
		"| REQ-CON-001 | Only public-sector orgs may lead | MVP | IN_REVIEW | UR-CON-001 | " +
		"`test_consortium_service.py:158` | `services/consortium_service.py:99` |\n" +
		"| REQ-CON-002 | A rule with no evidence | MVP | IN_REVIEW | UR-CON-001 | — | — |\n"

	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md", ledger)
	if len(reqs) != 2 {
		t.Fatalf("want 2 rows, got %d", len(reqs))
	}
	if reqs[0].Tests == "" || reqs[0].Code == "" {
		t.Fatalf("extractor dropped the columns: tests=%q code=%q", reqs[0].Tests, reqs[0].Code)
	}

	cites := BuildRequirementOp(reqs[0]).Payload["source_citations"]
	blob, _ := json.Marshal(cites)
	for _, want := range []string{"consortium_service.py:99", "test_consortium_service.py:158", `"code"`, `"test"`} {
		if !strings.Contains(string(blob), want) {
			t.Fatalf("citation %s missing from %s", want, blob)
		}
	}

	// An em-dash means no evidence — it must not become a citation pointing at "—".
	empty, _ := json.Marshal(BuildRequirementOp(reqs[1]).Payload["source_citations"])
	if strings.Contains(string(empty), "—") {
		t.Fatalf("an absent citation was written as one: %s", empty)
	}
}

// RUN:2026-08-12: two ledger layouts exist in the wild and positional column
// indexing silently mislabels one of them —
//   workspace     | ID | Title | Stage | Status | Source | Tests | Code |
//   skill-written | ID | Title | Stage | Status | UR | Source | Evidence | Code |
// Reading cell 6 as "Tests" turned a doc reference into a test citation. Columns
// must be found by NAME from the header row.
func TestColumnsAreFoundByNameNotPosition(t *testing.T) {
	skillShaped := "# ANL — Analytics · requirements ledger\n\n" +
		"| ID | Title | Stage | Status | UR | Source | Evidence | Code |\n" +
		"|---|---|---|---|---|---|---|---|\n" +
		"| REQ-ANL-001 | A rule | MVP | IN_REVIEW | UR-ANL-001 | §Schema | `test_x.py:10` — run, passing | `models.py:44` |\n"
	r := ParseLedger("tasks/ANL-REQUIREMENTS.md", skillShaped)[0]
	if !strings.Contains(r.Code, "models.py:44") {
		t.Fatalf("Code column mislabelled: %q", r.Code)
	}
	if !strings.Contains(r.Tests, "test_x.py:10") {
		t.Fatalf("Evidence column not read as tests: %q", r.Tests)
	}
	if r.UR != "UR-ANL-001" {
		t.Fatalf("the UR link was dropped: %q", r.UR)
	}

	workspaceShaped := "# REQUIREMENTS — CROSS (cross-cutting)\n\n" +
		"| ID | Title | Stage | Status | Source | Tests | Code |\n" +
		"|---|---|---|---|---|---|---|\n" +
		"| REQ-CROSS-001 | A rule | MVP | DONE | `USER:2026-01-01` | `a_test.go` | `a.go:12` |\n"
	w := ParseLedger("tasks/CROSS-REQUIREMENTS.md", workspaceShaped)[0]
	if !strings.Contains(w.Code, "a.go:12") || !strings.Contains(w.Tests, "a_test.go") {
		t.Fatalf("workspace layout broken: tests=%q code=%q", w.Tests, w.Code)
	}
	if w.UR != "" {
		t.Fatalf("a layout with no UR column must yield none, got %q", w.UR)
	}
}

// EPIC-SYNC-011 (`USER:2026-08-12`): "all requirements are system requirements?"
// — yes, because `kind` is a literal in both builders while the server has
// routed `kind: "user"` to UserRequirement all along. Sixteen user requirements
// sit in the epic records of a real system and the platform holds none, so its
// traceability matrix is 874 orphans.
func TestAnEpicsUserRequirementBecomesAUserRequirement(t *testing.T) {
	record := mustRead(t, "testdata/skillshaped/epics/EPIC-CON-001/EPIC.md")

	ur, ok := ParseEpicUserRequirement(record)
	if !ok {
		t.Fatal("no user requirement parsed — the epic's own outcome reaches the platform as nothing")
	}
	if ur.ID != "UR-CON-001" {
		t.Fatalf("id lost: %q", ur.ID)
	}
	if !strings.Contains(ur.Statement, "public-sector organization") {
		t.Fatalf("statement lost: %q", ur.Statement)
	}

	op := BuildUserRequirementOp(ur, Epic{ID: "EPIC-CON-001", State: "awaiting-approval"})
	if op.Type != "upsert_requirement" {
		t.Fatalf("wrong op type: %q", op.Type)
	}
	if got := op.Payload["kind"]; got != "user" {
		t.Fatalf("kind is %v — it must be \"user\", or this lands as a 875th system requirement", got)
	}
	if got := op.Payload["external_id"]; got != "UR-CON-001" {
		t.Fatalf("external_id: %v", got)
	}
	if got := op.Payload["context"]; got != "CON" {
		t.Fatalf("context should come from the id's middle segment, got %v", got)
	}
}

// A missing user requirement must stay visible as missing. An invented one is
// invisible — it looks exactly like a real one.
func TestAnEpicWithNoUserRequirementSectionYieldsNone(t *testing.T) {
	for _, record := range []string{
		"# EPIC-X-001 — Something\n\n## Grounded facts\n- nothing here\n",
		"# EPIC-X-001 — Something\n\n## User requirement\n\nA paragraph that names no id at all.\n",
		"# EPIC-X-001 — Something\n\n## User requirement\n\n**Not an id** — prose.\n",
	} {
		if ur, ok := ParseEpicUserRequirement(record); ok {
			t.Fatalf("a user requirement was invented from %q: %+v", record, ur)
		}
	}
}

// REQ-CROSS-049: the `UR` column nine of eleven real ledgers carry is parsed
// (REQ-CROSS-047) and goes nowhere, so every system requirement is an orphan.
func TestTheParentLinkReachesTheOp(t *testing.T) {
	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md",
		mustRead(t, "testdata/skillshaped/tasks/CON-REQUIREMENTS.md"))

	byID := map[string]Req{}
	for _, r := range reqs {
		byID[r.ID] = r
	}

	linked := BuildRequirementOp(byID["REQ-CON-001"]).Payload
	if got := linked["parent_external_id"]; got != "UR-CON-001" {
		t.Fatalf("parent link dropped: %v", got)
	}

	// A row with no UR must not carry the key at all — an absent column must
	// not churn every content hash in workspaces that never had one.
	unlinked := BuildRequirementOp(byID["REQ-CON-003"]).Payload
	if _, present := unlinked["parent_external_id"]; present {
		t.Fatalf("an empty UR cell sent a key: %v", unlinked["parent_external_id"])
	}

	// An em-dash is how a ledger writes "no parent", exactly as in the evidence
	// columns. Sent literally it becomes a parent id nothing can resolve.
	for _, dash := range []string{"—", "-", "–", " — "} {
		p := BuildRequirementOp(Req{ID: "REQ-X-001", UR: dash}).Payload
		if v, present := p["parent_external_id"]; present {
			t.Fatalf("an em-dash was sent as a parent id: %q", v)
		}
	}
}

// REQ-CROSS-051 (`USER:2026-08-12` "In workspace we seem to have more"):
// citations were read from the DASHBOARD CELL, while the detail block below
// holds nearly twice as much — 2,322 file references against 1,225, with 598 of
// 874 rows poorer on the platform than in the repository (`RUN:2026-08-12`).
// And every backticked token counted as a reference, so identifiers lifted out
// of a sentence shipped as test citations while the real test path never left.
func TestEvidenceIsReadFromTheRecordNotTheSummaryCell(t *testing.T) {
	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md",
		mustRead(t, "testdata/skillshaped/tasks/CON-REQUIREMENTS.md"))

	var target Req
	for _, r := range reqs {
		if r.ID == "REQ-CON-002" {
			target = r
		}
	}

	blob, _ := json.Marshal(BuildRequirementOp(target).Payload["source_citations"])
	body := string(blob)

	// The detail block names three paths; the row cell names one. SR-SY-1401
	// (EPIC-SYNC-014): each arrives as the exact file path — the :line/:range
	// suffix locates the rule inside the file and is not part of the file's
	// identity, which is what the server matches FileAnalysis rows by.
	for _, want := range []string{
		`"services/consortium_service.py"`,
		`"tests/test_consortium_service.py"`,
		`"tests/test_synthesis.py"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the record's citation %s never left the workspace: %s", want, body)
		}
	}

	// Identifiers inside prose are prose. `NeedLocked` and `SynthesisRunner` are
	// classes being described, not tests that cover anything.
	for _, never := range []string{"NeedLocked", "SynthesisRunner"} {
		if strings.Contains(body, `"`+never+`"`) {
			t.Fatalf("a word in a sentence became a citation: %s", body)
		}
	}
}

// A statement that no test exists is a finding worth reading. It is not a test,
// and it must not become a citation that looks like one.
func TestProseAboutMissingEvidenceSurvivesAsProse(t *testing.T) {
	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md",
		mustRead(t, "testdata/skillshaped/tasks/CON-REQUIREMENTS.md"))

	var target Req
	for _, r := range reqs {
		if r.ID == "REQ-CON-001" {
			target = r
		}
	}

	cites, _ := json.Marshal(BuildRequirementOp(target).Payload["source_citations"])
	body := string(cites)

	if !strings.Contains(body, `"note"`) {
		t.Fatalf("the finding that no test exists was dropped entirely: %s", body)
	}
	if !strings.Contains(body, "never run") {
		t.Fatalf("the note lost its text: %s", body)
	}
	// It must not masquerade as a test.
	if strings.Contains(body, `{"kind":"test","ref":"**no test**`) {
		t.Fatalf("an absence was written as a test citation: %s", body)
	}
	// The code the row does cite still arrives — as the whole path, its range
	// suffix folded off (SR-SY-1401).
	if !strings.Contains(body, `"services/consortium_service.py"`) {
		t.Fatalf("code citation lost: %s", body)
	}
}

// RUN:2026-08-12 — this workspace writes its user requirement in the HEADING,
// `## User outcome (UR-SY-010)`, with the statement as plain prose beneath. The
// skill writes `**UR-CON-001** — As a …` as the section's first bold token. Both
// are in daily use; only the second parsed, so 40 epics here named a UR and 4
// extracted. The same skill↔extractor drift REQ-CROSS-044 exists to catch,
// found in the opposite direction.
func TestAUserRequirementNamedInTheHeadingIsRead(t *testing.T) {
	record := "# EPIC-SYNC-010 — Epics carry their state\n\n" +
		"## User outcome (UR-SY-010)\n" +
		"Someone opening an epic in Mission Control sees where it actually stands\n" +
		"and what it promised to prove.\n\n" +
		"## Linked user requirements\n"

	ur, ok := ParseEpicUserRequirement(record)
	if !ok {
		t.Fatal("a UR named in the heading did not parse — 40 epics here, 4 extracted")
	}
	if ur.ID != "UR-SY-010" {
		t.Fatalf("id: %q", ur.ID)
	}
	if !strings.Contains(ur.Statement, "where it actually stands") {
		t.Fatalf("statement lost: %q", ur.Statement)
	}
	// A heading with no id still yields nothing — absence stays visible.
	if _, ok := ParseEpicUserRequirement("# E — X\n\n## User outcome\nProse only.\n"); ok {
		t.Fatal("a UR was invented from a heading that names none")
	}
}

// RUN:2026-08-12 — a third ledger layout, from a real reverse-engineering run:
//
//	| REQ | Behaviour | Status | Evidence | Src (original id) |
//
// It has no "ID" header, so the name-based mapper did not recognise it as a
// header at all and fell back to positional order. The consequences were
// silent and total: 1,464 rows synced their EVIDENCE text as work_status
// ("FINDING", "NO", "PROCEDURE"), their real status as stage, and no code
// column existed to read. work_status is a closed enum, so the batch could
// not land. Column names must cover what agents actually write.
func TestAThirdLedgerLayoutIsReadByName(t *testing.T) {
	ledger := "# LSYS — System Infrastructure (legacy) · requirement ledger\n\n" +
		"| REQ | Behaviour | Status | Evidence | Src (original id) |\n" +
		"|---|---|---|---|---|\n" +
		"| REQ-LSYS-001 | The queue drain returns nothing for any argument | PROPOSED | " +
		"finding, this pass — null-compare in the predicate | dbo.usp_DrainQueue |\n"

	r := ParseLedger("tasks/LSYS-REQUIREMENTS.md", ledger)[0]
	if r.Status != "PROPOSED" {
		t.Fatalf("status read from the wrong column: %q", r.Status)
	}
	if !strings.Contains(r.Title, "queue drain") {
		t.Fatalf("title: %q", r.Title)
	}
	if !strings.Contains(r.Tests, "null-compare") {
		t.Fatalf("Evidence not read as evidence: %q", r.Tests)
	}
	// No Stage column exists — absence must read as absence, not as the next cell.
	if r.Stage != "" {
		t.Fatalf("a stage was invented from another column: %q", r.Stage)
	}
}

// RUN:2026-08-12 — a file may carry BOTH question shapes: a heading-form
// question appended to a workspace whose 140 questions are table rows. Trying
// heading form first and RETURNING kept 1 and discarded 140, dropping 119 open
// gates from the queue. Preferring a shape is not the same as choosing one.
func TestBothQuestionShapesInOneFileAreRead(t *testing.T) {
	content := "# Open questions\n\n" +
		"| ID | Question | Owner |\n|---|---|---|\n" +
		"| OQ-001 | Does the roster keep joined_at? | TBD |\n" +
		"| OQ-002 | Should the badge fail loudly? | TBD |\n\n" +
		"## Q-SYS-901 — Are capabilities and patterns one switch?\n\n" +
		"One flag gates both passes.\n"

	oqs := ParseOpenQuestions(content)
	if len(oqs) != 3 {
		var ids []string
		for _, q := range oqs {
			ids = append(ids, q.ID)
		}
		t.Fatalf("want all 3 questions, got %d: %v", len(oqs), ids)
	}
}

// REQ-CROSS-064 (`USER:2026-08-13`, "make sure they don't clog my release"):
// 800+ as-built rows landed in the active release because sync stamps every row
// with the batch's release. A derived requirement describes what ALREADY SHIPS,
// so it is base, not planned work. The marker is the epic: an as-built epic
// says SPEC-DERIVED in its Specification status.
func TestAsBuiltRequirementsAreReleaseExempt(t *testing.T) {
	derived := "# EPIC-SYS-901 — The analysis pipeline, as built\n\n" +
		"## Specification status\nSPEC-DERIVED — this epic records behaviour that already ships.\n\n" +
		"## Requirements in this epic\n- REQ-SYS-050\n- REQ-SYS-051\n"
	planned := "# EPIC-SYNC-011 — Requirements carry their relationships\n\n" +
		"## Specification status\nSPEC-APPROVED — implementation may start.\n\n" +
		"## Requirements in this epic\n- REQ-CROSS-048\n"

	data := Data{
		Reqs: []Req{
			{ID: "REQ-SYS-050", Ctx: "SYS", Status: "IN_REVIEW"},
			{ID: "REQ-CROSS-048", Ctx: "CROSS", Status: "IN_REVIEW"},
		},
		Epics: []Epic{
			{ID: "EPIC-SYS-901", Record: "epics/EPIC-SYS-901/EPIC.md"},
			{ID: "EPIC-SYNC-011", Record: "epics/EPIC-SYNC-011/EPIC.md"},
		},
	}
	records := map[string]string{
		"epics/EPIC-SYS-901/EPIC.md":  derived,
		"epics/EPIC-SYNC-011/EPIC.md": planned,
	}

	ops := BuildOps(data, func(rel string) string { return records[rel] }, "2026-08-13")

	payloads := map[string]map[string]any{}
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			payloads[op.Payload["external_id"].(string)] = op.Payload
		}
	}

	if payloads["REQ-SYS-050"]["release_exempt"] != true {
		t.Fatalf("an as-built requirement is not release-exempt: %v", payloads["REQ-SYS-050"])
	}
	// Planned work carries no key at all — an absent key cannot churn the hash
	// of every requirement in every workspace that has no as-built epics.
	if _, present := payloads["REQ-CROSS-048"]["release_exempt"]; present {
		t.Fatalf("planned work must carry no exemption key at all")
	}
}
