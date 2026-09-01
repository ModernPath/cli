package rdd

// REQ-CROSS-246 (EPIC-CLI-003 T10): the fidelity report counts every
// landing-model column family, so an unfed home reads as an explicit 0%
// count group — never as silence — and a stated cap names its real
// enforcement point.

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// The seven landing-home groups, denominators from the corpus, coverage from
// the ops the same build emits. Before the carrier SRs land every one reads
// rows > 0 → ops 0 on a corpus that exercises it.
func TestLandingHomesReadZeroBeforeCarriers(t *testing.T) {
	root := cleanEpicCorpus(t)
	// decisions + evidence map + a spec-file scenario on the epic record
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want one record so that the count means something.

## Acceptance scenarios

| id | Scenario |
|---|---|
| SCN-CV-001 | GIVEN a record WHEN it syncs THEN it is carried |

## Decisions

| ID | Decision | Source |
|---|---|---|
| D-CV-1 | The first decision. | USER:2026-08-01 |
| D-CV-2 | The second decision. | USER:2026-08-02 |

## Evidence map

| Scenario | Test |
|---|---|
| SCN-CV-001 | tests/cv.spec.ts |
`)
	// a RUN: token in a ledger detail block
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | — | doc | — | — |

### REQ-CV-001 — Only behavior
- **Status:** PROPOSED · **Stage:** MVP
- **Statement:** It behaves.
- **Evidence:** verified green `+"`RUN:2026-08-20`"+` on the harness
`)
	// a WORKLIST acceptance tag in the Human-approval column
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | DONE | APP-CV-001 (approved `USER:2026-08-03`) | — |\n")
	// a backlog row carrying labeled facts in folded prose
	writeFixtureFile(t, root, "BACKLOG.md", `# Backlog

| Item | Notes | Route |
|---|---|---|
| A discovered thing | raised 2026-08-04 · **ROUTED** → REQ-CV-001 | CV |
`)

	r := reportOf(t, root)
	// carried == true once that family's carrier SR landed: the group must
	// then reconcile instead of reading zero.
	cases := []struct {
		group   string
		minRows int
		carried bool
	}{
		{homeDecisionsGroup, 2, true},
		{"landing home: epic acceptances (WORKLIST Human-approval tags + approval-register rows → acceptance-carrying epic ops)", 1, true},
		{"landing home: historical evidence (RUN: tokens in ledgers, records, WORKLIST → evidence results built)", 1, true},
		{"landing home: verification references (evidence-map data rows → verification_refs entries on criteria)", 1, false},
		{"landing home: scenario records (captured scenario definitions → upsert_scenario ops)", 1, true},
		{"landing home: backlog typed fields (backlog/gap rows with labeled facts → payloads carrying a typed field)", 1, true},
		{"landing home: process-record archive (retired-family files → upsert_process_record ops)", 1, true},
	}
	for _, c := range cases {
		t.Run(c.group, func(t *testing.T) {
			g := countGroup(t, r, c.group)
			if g.Rows < c.minRows {
				t.Fatalf("rows = %d, want >= %d — the denominator must count the corpus", g.Rows, c.minRows)
			}
			if c.carried && g.Ops != g.Rows {
				t.Fatalf("ops = %d, want %d — this family's carrier landed and must reconcile", g.Ops, g.Rows)
			}
			if !c.carried && g.Ops != 0 {
				t.Fatalf("ops = %d, want 0 before the carrier exists", g.Ops)
			}
		})
	}
}

// A corpus that exercises none of a family still gets its group, at 0 → 0 —
// absence of the group is indistinguishable from silence.
func TestLandingHomesGroupsExistOnAQuietCorpus(t *testing.T) {
	root := cleanEpicCorpus(t)
	r := reportOf(t, root)
	g := countGroup(t, r, homeDecisionsGroup)
	if g.Rows != 0 || g.Ops != 0 {
		t.Fatalf("quiet corpus decision group = %d/%d, want 0/0", g.Rows, g.Ops)
	}
}

func TestScenarioHomeRejectsAFieldIncorrectCarrier(t *testing.T) {
	root := cleanEpicCorpus(t)
	record := `# EPIC-CV-001 — Only

## Acceptance scenarios

| Scenario | Given / When / Then | Status |
|---|---|---|
| SCN-CV-001 | GIVEN a record WHEN it is superseded THEN it stays historical | OBSOLETE — consolidated into SCN-CV-002 |
`
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", record)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001-only.md) | UR-CV-001 | SCN-CV-001 | REQ-CV-001 | T1 | — | — | PROPOSED | — | — |\n")

	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
	for i := range ops {
		if ops[i].Type == "upsert_scenario" {
			ops[i].Payload["status"] = "active"
		}
	}
	r := BuildFidelityReport(root, m, data, ops)
	g := countGroup(t, r, homeScenariosGroup)
	if g.Rows != 1 || g.Ops != 0 {
		t.Fatalf("field-incorrect scenario coverage = %d/%d, want 0/1: %+v", g.Ops, g.Rows, g)
	}
	if !strings.Contains(strings.Join(g.MissingFromOps, " "), "status") {
		t.Fatalf("missing fields must name status: %+v", g.MissingFromOps)
	}
}

func TestBacklogHomeRequiresEveryParsedTypedFact(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "BACKLOG.md", `# Backlog

| Discovery | Notes | Tracked as / suggested route |
|---|---|---|
| **A found defect** (RUN:2026-08-18, CLI verification run) | The endpoint 404s; affects REQ-CV-001. | **ROUTED** → REQ-CROSS-211 |
`)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
	for i := range ops {
		if ops[i].Type == "upsert_backlog_record" {
			delete(ops[i].Payload, "raised_at")
		}
	}
	r := BuildFidelityReport(root, m, data, ops)
	g := countGroup(t, r, homeBacklogGroup)
	if g.Rows != 1 || g.Ops != 0 {
		t.Fatalf("partial backlog coverage = %d/%d, want 0/1: %+v", g.Ops, g.Rows, g)
	}
	if !strings.Contains(strings.Join(g.MissingFromOps, " "), "raised_at") {
		t.Fatalf("missing fields must name raised_at: %+v", g.MissingFromOps)
	}
}

// §246.3 — a stated cap names its real enforcement point. An RQ body is never
// cut at the 6,000-unit composition cap (that cap belongs to spec/approval
// gate composition); its real cut is the 15,000-unit parse cap.
func TestRQGateBodyLossNamesTheParseCap(t *testing.T) {
	root := cleanEpicCorpus(t)
	m := manifest.Default()
	data, _ := Snapshot(root, m)

	gate := func(id, body string) Op {
		return Op{Type: "upsert_gate", Payload: map[string]any{
			"external_id": id, "kind": "decision", "body_md": body, "state": "open",
		}}
	}
	// no archival document ops in this hand-built list → the review-queue
	// file is NOT carried whole, so suppression does not apply.
	ops := []Op{
		gate("RQ-7000", strings.Repeat("a", 7000)),
		gate("RQ-16000", strings.Repeat("b", 15000)),
	}
	r := BuildFidelityReport(root, m, data, ops)

	for _, l := range r.Losses {
		if l.RecordID == "RQ-7000" && l.Field == "gate-body" {
			t.Fatalf("an RQ body under the parse cap is not cut — phantom 6,000-unit loss: %+v", l)
		}
	}
	var cut *FidelityLoss
	for i := range r.Losses {
		if r.Losses[i].RecordID == "RQ-16000" && r.Losses[i].Field == "gate-body" {
			cut = &r.Losses[i]
		}
	}
	if cut == nil {
		t.Fatalf("an RQ body at the parse cap was cut and must be disclosed; losses: %+v", r.Losses)
	}
	if !strings.Contains(cut.Detail, "15000") && !strings.Contains(cut.Detail, "15,000") {
		t.Fatalf("the loss must name the real enforcement point (the parse cap): %q", cut.Detail)
	}
}
