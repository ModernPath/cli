package rdd

// REQ-CROSS-013 / TASK-SY-404 — parser tests for the bundled formats,
// fixtures distilled from the real modernpath-v1 files the retired node
// extractor parsed.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const ledgerFixture = `# REQUIREMENTS — TST

## Dashboard — TST
Totals: 1 DONE · 1 IN_PROGRESS

| ID | Title | Stage | Status | Source | Tests | Code |
|----|-------|-------|--------|--------|-------|------|
| REQ-TST-001 | First thing | MVP | done | ` + "`docs/10`" + ` | t.exs | c.ex |
| REQ-TST-002 | Second thing | Later | IN_PROGRESS | — | — | — |

## Detail blocks

### REQ-TST-001 — First thing
- **Status:** DONE
- **Acceptance criteria:**
  - GIVEN a WHEN b THEN c.
- **Tests:** ` + "`t.exs`" + `

### REQ-TST-002 — Second thing
- **Status:** IN_PROGRESS

## Trailer section
not a detail.
`

func TestParseLedger(t *testing.T) {
	reqs := ParseLedger("/ws/tasks/TST-REQUIREMENTS.md", ledgerFixture)
	if len(reqs) != 2 {
		t.Fatalf("want 2 rows, got %d", len(reqs))
	}
	r := reqs[0]
	if r.ID != "REQ-TST-001" || r.Ctx != "TST" || r.Status != "DONE" || r.Stage != "MVP" {
		t.Fatalf("row parse wrong: %+v", r)
	}
	if !strings.Contains(r.Detail, "Acceptance criteria") || strings.Contains(r.Detail, "Second thing") {
		t.Fatalf("detail block boundaries wrong: %q", r.Detail)
	}
	if strings.Contains(reqs[1].Detail, "Trailer") {
		t.Fatalf("detail must stop at the next ## heading: %q", reqs[1].Detail)
	}
	if ParseLedger("/ws/tasks/notes.md", ledgerFixture) != nil {
		t.Fatal("non-ledger filename must be rejected")
	}
}

const worklistFixture = `# Work-List

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-TST-001 | [record](epics/EPIC-TST-001-first.md) | UR-1 | SCN | TASK | per record | — | — | **IN_REVIEW** (evidence-complete) | pending | notes |
| EPIC-TST-002 | — | UR-2 | SCN | TASK | per record | — | — | **IN_PROGRESS** (building) | — | notes |
| EPIC-TST-003 | — | UR-3 | SCN | TASK | per record | — | — | **DONE** | **APPROVED** USER:2026-07-20 | notes |
| EPIC-TST-004 | — | UR-4 | SCN | TASK | per record | — | — | PROPOSED (spec only) | pending | notes |
`

func TestParseWorklist(t *testing.T) {
	files := []string{"EPIC-TST-002-second.md", "EPIC-TST-004-dir"}
	isDir := map[string]bool{"EPIC-TST-004-dir": true}
	epics := ParseWorklist(worklistFixture, files, isDir)
	if len(epics) != 4 {
		t.Fatalf("want 4 rows, got %d", len(epics))
	}
	if epics[0].State != "awaiting-approval" || epics[0].Record != "epics/EPIC-TST-001-first.md" {
		t.Fatalf("pending-approval row wrong: %+v", epics[0])
	}
	if epics[1].State != "in-progress" || epics[1].Record != "epics/EPIC-TST-002-second.md" {
		t.Fatalf("in-progress row / prefix record match wrong: %+v", epics[1])
	}
	if epics[2].State != "done" {
		t.Fatalf("done row wrong: %+v", epics[2])
	}
	// PROPOSED outranks the pending approval cell — spec drafts never gate
	if epics[3].State != "proposed" || epics[3].Record != "epics/EPIC-TST-004-dir/EPIC.md" {
		t.Fatalf("proposed row / dir record wrong: %+v", epics[3])
	}
}

const reviewQueueFixture = `# docs/85

## Queue

### RQ-201 (2026-07-20) Pick a direction — DECISION NEEDED
Body mentions REQ-TST-001 and EPIC-TST-001.
Options: **(a)** do the simple thing now. **(b)** build the full machine instead.
I recommend option (a) for scope reasons.

### RQ-202 (2026-07-21) Something shipped ✅ APPROVED
Resolved per USER:2026-07-21 note. Touches REQ-TST-002.

### RQ-203 observed detail
Just a finding.

## Another section
### RQ-201 (2026-07-22) Pick a direction ✅ RESOLVED
Closed per USER:2026-07-22. Also REQ-TST-003.
`

func TestParseReviewQueue(t *testing.T) {
	rqs := ParseReviewQueue(reviewQueueFixture)
	if len(rqs) != 3 {
		t.Fatalf("want 3 unique RQs, got %d", len(rqs))
	}
	byID := map[string]RQ{}
	for _, rq := range rqs {
		byID[rq.ID] = rq
	}
	merged := byID["RQ-201"]
	if merged.State != "closed" {
		t.Fatalf("closed duplicate must win: %+v", merged.State)
	}
	for _, want := range []string{"REQ-TST-001", "EPIC-TST-001", "REQ-TST-003"} {
		found := false
		for _, p := range merged.Pool {
			if p == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("merged pool missing %s: %v", want, merged.Pool)
		}
	}
	if !strings.Contains(merged.Body, "---") {
		t.Fatal("duplicate bodies must join")
	}
	// the closed duplicate wins, so its heading feeds cleanTitle — which strips
	// the RQ id, date paren, and "— DECISION NEEDED", but keeps the ✅ marker
	// (extract.js parity)
	first := byID["RQ-201"]
	if first.Title != "Pick a direction ✅ RESOLVED" {
		t.Fatalf("cleanTitle wrong: %q", first.Title)
	}
	q := rqs[0]
	if q.State != "closed" {
		// order preserved: RQ-201 slot holds the merged/closed entry
		t.Fatalf("first slot state: %v", q.State)
	}
	if byID["RQ-202"].State != "closed" || byID["RQ-203"].State != "finding" {
		t.Fatalf("states wrong: %v / %v", byID["RQ-202"].State, byID["RQ-203"].State)
	}
	open := ParseReviewQueue("### RQ-300 (2026-07-01) Choose — DECISION NEEDED\nPick **(a)** first option here. **(b)** second option here.\nI recommend (b) because reasons.\n")[0]
	if len(open.Options) != 2 || open.Options[0].Label != "(a)" {
		t.Fatalf("options parse wrong: %+v", open.Options)
	}
	if !strings.Contains(open.Recommendation, "recommend") {
		t.Fatalf("recommendation parse wrong: %q", open.Recommendation)
	}
}

const oqFixture = `# process/08

| ID | Question | Owner | Phase |
|---|---|---|---|
| OQ-A1x | Where does the schema live? Own repo vs frontend. | Paavo | open |
| OQ-B2 | Resolved thing. ✅ | — | done |
`

func TestParseOpenQuestions(t *testing.T) {
	oqs := ParseOpenQuestions(oqFixture)
	if len(oqs) != 2 {
		t.Fatalf("want 2 OQs, got %d", len(oqs))
	}
	if oqs[0].ID != "OQ-A1x" || oqs[0].Resolved {
		t.Fatalf("open OQ wrong: %+v", oqs[0])
	}
	if oqs[0].Title != "Where does the schema live?" {
		t.Fatalf("first-sentence title wrong: %q", oqs[0].Title)
	}
	if !oqs[1].Resolved {
		t.Fatalf("✅ row must resolve: %+v", oqs[1])
	}
}

// REQ-CROSS-074 (`RUN:2026-08-13`): a third name for the behaviour column.
// Other modern ledgers head it "Requirement"; this workspace says "Title" and
// a derived ledger says "Behaviour". Without the synonym the column simply does
// not bind, the title serializes as "", and the server refuses the whole batch
// with `422 title: can't be blank` — which is what blocked syncing 1,786
// requirements.
func TestLedgerColumnsAcceptsRequirementAsTheTitle(t *testing.T) {
	lines := []string{
		"| ID | Requirement | Stage | Status | Source | Evidence | Owner | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-BUL-001 | The web bulletin list identifies the reader from the session | shipped | PENDING_VERIFICATION | UR-BUL-1 | no test | — | `x.cs:40` |",
	}
	col := ledgerColumns(lines)
	if _, ok := col["title"]; !ok {
		t.Fatal(`the "Requirement" header did not bind the title column`)
	}
	if col["title"] != 2 {
		t.Errorf("title column = %d, want 2", col["title"])
	}
	// the columns beside it must still land where they belong
	for name, want := range map[string]int{"id": 1, "stage": 3, "status": 4, "code": 8} {
		if col[name] != want {
			t.Errorf("%s column = %d, want %d", name, col[name], want)
		}
	}
}

// A narrower table that merely mentions requirements must not be mistaken for
// the dashboard — a real journey table opens with the same two headers.
func TestLedgerColumnsIgnoresANarrowJourneyTable(t *testing.T) {
	lines := []string{
		"| ID | Requirement | Actor | Views | Serves |",
		"|---|---|---|---|---|",
		"| UR-BUL-2 | An employee sees how many bulletins they have not read | any user | `/` | tile |",
		"",
		"| ID | Requirement | Stage | Status | Source | Evidence | Owner | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-BUL-001 | The web bulletin list identifies the reader | shipped | PENDING_VERIFICATION | UR-BUL-1 | no test | — | `x.cs:40` |",
	}
	col := ledgerColumns(lines)
	if col["status"] != 4 {
		t.Errorf("status column = %d, want 4 — the journey table was mistaken for the dashboard", col["status"])
	}
}

// REQ-CROSS-076 (`RUN:2026-08-13`): a ledger with no `UR` column can still name
// the parent — a real corpus puts a bare `UR-BUL-1` in the column headed
// `Source`. 1,336 of 1,786 rows do this, and without reading it every one of
// them syncs as an orphan.
//
// The rule is deliberately narrow: only when the ledger has NO dedicated UR
// column, and only when the source cell is a BARE UR reference. The other 450
// rows hold a real source and must be left exactly as they are — a UR mentioned
// inside a sentence is a citation, not a parent.
func TestUROutOfTheSourceColumnWhenThereIsNoURColumn(t *testing.T) {
	lines := []string{
		"| ID | Requirement | Stage | Status | Source | Evidence | Owner | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-BUL-001 | The reader is identified from the session | shipped | PENDING_VERIFICATION | UR-BUL-1 | no test | — | `x.cs:40` |",
		"| REQ-BUL-002 | The page size is clamped | shipped | PENDING_VERIFICATION | `docs/06-surfaces.md` §2 | no test | — | `x.cs:39` |",
		"| REQ-BUL-003 | Marking read uses the route id | shipped | PENDING_VERIFICATION | derived from UR-BUL-1 and the code | no test | — | `x.cs:52` |",
	}
	reqs := ParseLedger("tasks/BUL-REQUIREMENTS.md", strings.Join(lines, "\n"))
	by := map[string]Req{}
	for _, r := range reqs {
		by[r.ID] = r
	}
	if got := by["REQ-BUL-001"].UR; got != "UR-BUL-1" {
		t.Errorf("REQ-BUL-001 UR = %q, want UR-BUL-1 (a bare reference in the Source column)", got)
	}
	if got := by["REQ-BUL-002"].UR; got != "" {
		t.Errorf("REQ-BUL-002 UR = %q, want empty — its source is a real source", got)
	}
	if got := by["REQ-BUL-003"].UR; got != "" {
		t.Errorf("REQ-BUL-003 UR = %q, want empty — a UR inside a sentence is a citation, not a parent", got)
	}
	// the source itself must survive untouched in every case
	if by["REQ-BUL-002"].Source == "" {
		t.Error("the Source cell was consumed")
	}
}

// A ledger that HAS a UR column keeps using it, whatever the source says.
func TestExplicitURColumnWins(t *testing.T) {
	lines := []string{
		"| ID | Title | Stage | Status | UR | Source | Evidence | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-X-001 | A thing | MVP | DONE | UR-REAL-1 | UR-DECOY-9 | t | c |",
	}
	reqs := ParseLedger("tasks/X-REQUIREMENTS.md", strings.Join(lines, "\n"))
	if len(reqs) != 1 || reqs[0].UR != "UR-REAL-1" {
		t.Fatalf("UR = %q, want UR-REAL-1 from the dedicated column", reqs[0].UR)
	}
}

// REQ-CROSS-080/081 (EPIC-ARCH-001): the NFR ledger is an ordinary context, so
// it must parse like one — and its rows must reach the platform with BLOCKED
// intact. A status silently normalised away would turn every open question into
// a claim, which is the one outcome D-ARCH-3 exists to prevent.
func TestNFRLedgerParsesAsAnOrdinaryContext(t *testing.T) {
	lines := []string{
		"# NFR — quality attributes",
		"",
		"| ID | Title | Stage | Status | UR | Source | Evidence | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-NFR-014 | Analysis fan-out is capped at 4 concurrent tenants | performance | BLOCKED | | `CODE:core/tenant_task.ex:64` | — | — |",
		"| REQ-NFR-015 | A refresh rejected by the provider ends the session | security | PENDING_VERIFICATION | | `CODE:session.go:358` | `session_integration_test.go` | — |",
	}
	reqs := ParseLedger("tasks/NFR-REQUIREMENTS.md", strings.Join(lines, "\n"))
	if len(reqs) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(reqs))
	}
	by := map[string]Req{}
	for _, r := range reqs {
		by[r.ID] = r
	}

	if got := by["REQ-NFR-014"].Ctx; got != "NFR" {
		t.Errorf("ctx = %q, want NFR", got)
	}
	if got := by["REQ-NFR-014"].Status; got != "BLOCKED" {
		t.Errorf("status = %q, want BLOCKED — a question must not become a claim", got)
	}
	// the taxonomy rides the Stage cell, so it must survive untouched
	if got := by["REQ-NFR-014"].Stage; got != "performance" {
		t.Errorf("stage = %q, want performance (the quality-attribute category)", got)
	}
	if got := by["REQ-NFR-015"].Status; got != "PENDING_VERIFICATION" {
		t.Errorf("status = %q, want PENDING_VERIFICATION — the one exception, a threshold a test proves", got)
	}
}

// REQ-CROSS-084 (EPIC-ARCH-001 Part C): the explanatory documents and the
// discovery guides are discovered from the workspace and carried to the
// platform. Identity is the workspace-relative path; provenance is what makes
// them separable from uploads and from generated per-module documentation.
func TestCollectDocumentsFindsExplanationsAndGuides(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("docs/03-architecture.md", "# 03 — Architecture\n\nsubsystems")
	write("docs/21-integrations.md", "# 21 — Integrations\n\nfailure behaviour")
	write("docs/adr/0002-job-queue.md", "# 0002 — a job queue\n\nobserved")
	write("docs/guides/configuration.md", "# Where behaviour is configured\n\nsettings tables")
	// noise that must NOT be collected: a per-context doc and an unrelated file
	write("docs/10-analytics.md", "# 10 — Analytics")
	write("README.md", "# readme")

	docs := CollectDocuments(root)
	by := map[string]Document{}
	for _, d := range docs {
		by[d.Path] = d
	}

	if len(docs) != 4 {
		t.Fatalf("collected %d documents, want 4: %v", len(docs), by)
	}
	for _, want := range []struct{ path, dtype, prov string }{
		{"docs/03-architecture.md", "architecture", "derived"},
		{"docs/21-integrations.md", "architecture", "derived"},
		{"docs/adr/0002-job-queue.md", "design", "derived"},
		{"docs/guides/configuration.md", "process", "guide"},
	} {
		d, ok := by[want.path]
		if !ok {
			t.Errorf("%s was not collected", want.path)
			continue
		}
		if d.Type != want.dtype {
			t.Errorf("%s type = %q, want %q", want.path, d.Type, want.dtype)
		}
		if d.Provenance != want.prov {
			t.Errorf("%s provenance = %q, want %q — this is what separates a guide from an output", want.path, d.Provenance, want.prov)
		}
		if d.Content == "" {
			t.Errorf("%s carried no content", want.path)
		}
	}
	if _, mined := by["docs/10-analytics.md"]; mined {
		t.Error("a per-context document was collected — phase D's set is system-wide only")
	}
}

// REQ-CROSS-088: phase D's own rule is to ADOPT a maintained architecture
// document rather than write a competing one ("never write a second document
// answering a question the repository already answers"). A collector that only
// knows `docs/03-architecture.md` therefore drops the single most important
// document in exactly the repositories that already had one — which is what
// happened on a real estate whose `03` is a root ARCHITECTURE.md (`RUN:2026-08-13`).
func TestCollectDocumentsAdoptsAnExistingArchitectureDocument(t *testing.T) {
	write := func(root, rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	arch := func(docs []Document) []Document {
		var out []Document
		for _, d := range docs {
			if d.Type == "architecture" {
				out = append(out, d)
			}
		}
		return out
	}

	t.Run("a root ARCHITECTURE.md is the architecture document", func(t *testing.T) {
		root := t.TempDir()
		write(root, "ARCHITECTURE.md", "# Architecture\n\nservices and schema")
		got := arch(CollectDocuments(root))
		if len(got) != 1 || got[0].Path != "ARCHITECTURE.md" {
			t.Fatalf("architecture documents = %v, want exactly ARCHITECTURE.md", got)
		}
		if got[0].Provenance != "derived" || got[0].Content == "" {
			t.Errorf("adopted document must carry provenance and content, got %+v", got[0])
		}
	})

	t.Run("docs/architecture.md is found too", func(t *testing.T) {
		root := t.TempDir()
		write(root, "docs/architecture.md", "# Architecture\n\nlower-case")
		got := arch(CollectDocuments(root))
		if len(got) != 1 || got[0].Path != "docs/architecture.md" {
			t.Fatalf("architecture documents = %v, want exactly docs/architecture.md", got)
		}
	})

	// The rule that keeps two answers from reaching one reader: phase D's own
	// file wins, and the adopted one is not carried beside it.
	t.Run("phase D's own document wins when both exist", func(t *testing.T) {
		root := t.TempDir()
		write(root, "ARCHITECTURE.md", "# Architecture\n\nthe old one")
		write(root, "docs/03-architecture.md", "# 03 — Architecture\n\nthe phase D one")
		got := arch(CollectDocuments(root))
		if len(got) != 1 || got[0].Path != "docs/03-architecture.md" {
			t.Fatalf("architecture documents = %v, want only docs/03-architecture.md", got)
		}
	})
}
