package rdd

// REQ-CROSS-013 / TASK-SY-404 — parser tests for the bundled formats,
// fixtures distilled from the real modernpath-v1 files the node extractor
// parses today (mission-control/extract.js).

import (
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
