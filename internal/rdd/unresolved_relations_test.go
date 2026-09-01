package rdd

// REQ-CROSS-221 — the relations the batch CLAIMS and never defines.
//
// An epic op names its member requirements by external id, and a requirement op
// names its parent user requirement the same way. The server resolves each id
// and treats an unresolvable one as a no-op: the epic membership silently
// (Core.Sync link_requirements, whose own comment defers the counting to this
// report), the parent to a log line no HTTP response carries. So a claim about
// an entity that does not exist arrives as nothing at all, while every count on
// both sides still reconciles — the epic is there, the requirements that exist
// are there, and the missing edge appears nowhere.
//
// Paired fixtures per arm: one that fires, one that only looks like it should.

import (
	"strings"
	"testing"
)

// The firing case for the membership arm: the record's requirement section
// names an id no ledger row defines. The epic op carries it, the server drops
// it, and nothing today says so.
func TestEpicMembershipNamingAnUnemittedRequirementIsReportedAsLost(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want the dangling membership named.

## Requirements in this epic

- REQ-CV-001 — the row that exists
- REQ-CV-404 — the row that does not
`)

	r := reportOf(t, root)
	l := lossWithField(r.Losses, "epic-membership")
	if l == nil {
		t.Fatalf("a membership id no requirement op defines must be reported; losses: %+v", r.Losses)
	}
	// Keyed by the TARGET: the residue is "this id reaches no op", one fact and
	// one accept key however many epics claim it.
	if l.RecordID != "REQ-CV-404" {
		t.Fatalf("the loss must name the unresolvable id, got %q", l.RecordID)
	}
	if l.Category != LossLost {
		t.Fatalf("a dropped membership edge is lost content, not hygiene: %+v", l)
	}
	if !strings.Contains(l.Detail, "EPIC-CV-001") {
		t.Fatalf("the loss must name the epic that claims it: %q", l.Detail)
	}
	// It blocks, and the accept key states it exactly once.
	blocking := 0
	for _, b := range r.Blocking(nil) {
		if b.Key() == "REQ-CV-404|epic-membership" {
			blocking++
		}
	}
	if blocking != 1 {
		t.Fatalf("the unresolved membership must block exactly once, got %d", blocking)
	}
	if got := r.Blocking(map[string]bool{"REQ-CV-404|epic-membership": true}); len(got) != 0 {
		t.Fatalf("one accept key must cover the entry, still blocking: %+v", got)
	}

	c := countGroup(t, r, unresolvedMembershipGroup)
	if c.Rows != 2 || c.Ops != 1 {
		t.Fatalf("membership count = %d/%d, want 2 declared / 1 resolved", c.Rows, c.Ops)
	}
	if len(c.MissingFromOps) != 1 || !strings.Contains(c.MissingFromOps[0], "REQ-CV-404") {
		t.Fatalf("the count must itemize the unresolved claim, got %v", c.MissingFromOps)
	}
}

func TestExpandedEpicMembershipNamesEveryUnemittedRequirementAsLost(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want expanded dangling membership named.

## Requirements in this epic

REQ-CV-001 … REQ-CV-003
`)

	r := reportOf(t, root)
	for _, id := range []string{"REQ-CV-002", "REQ-CV-003"} {
		var found bool
		for _, loss := range r.Losses {
			found = found || (loss.RecordID == id && loss.Field == "epic-membership")
		}
		if !found {
			t.Fatalf("expanded unresolved id %s was not named; losses: %+v", id, r.Losses)
		}
	}
	c := countGroup(t, r, unresolvedMembershipGroup)
	if c.Rows != 3 || c.Ops != 1 {
		t.Fatalf("expanded membership count = %d/%d, want 3 declared / 1 resolved", c.Rows, c.Ops)
	}
}

// The negative: a membership id the batch DOES define. The whole point of the
// arm is the dangling reference — a resolvable one is an edge that arrives, and
// reporting it would make every epic in the corpus a loss.
func TestEpicMembershipNamingAnEmittedRequirementIsNotALoss(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "epics/EPIC-CV-001-only.md", `# EPIC-CV-001 — Only

## User outcome (UR-CV-001)

As a reader I want a resolvable membership to stay quiet.

## Requirements in this epic

- REQ-CV-001 — the row that exists
`)

	r := reportOf(t, root)
	if l := lossWithField(r.Losses, "epic-membership"); l != nil {
		t.Fatalf("a membership the batch defines must not be reported as lost: %+v", l)
	}
	c := countGroup(t, r, unresolvedMembershipGroup)
	if c.Rows != 1 || c.Ops != 1 {
		t.Fatalf("membership count = %d/%d, want 1 declared / 1 resolved", c.Rows, c.Ops)
	}
}

// The parent arm's firing case, in the shape the ur-record arm cannot see: the
// UR cell holds text that is not a UR id at all, so the payload claims a parent
// nothing can resolve while the referenced-UR scan skips it for lacking the
// prefix. The row's derives edge is never written and nothing says so.
func TestRequirementParentThatResolvesToNothingIsReported(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | UR-CV-999 | doc | — | — |
`)

	// §245.9 closed the prose arm of this failure at the source — a non-UR-
	// shaped cell now mints no parent claim at all (and rides as ur_raw) — so
	// the shape this arm still guards is a UR-shaped reference no op defines.
	r := reportOf(t, root)
	var found *FidelityLoss
	for i := range r.Losses {
		if r.Losses[i].RecordID == "UR-CV-999" && r.Losses[i].Field == "ur-record" {
			found = &r.Losses[i]
		}
	}
	if found == nil {
		t.Fatalf("a parent reference that resolves to nothing must be reported; losses: %+v", r.Losses)
	}
	if found.Category != LossLost {
		t.Fatalf("an unwritten derives edge is lost content: %+v", found)
	}
	// One loss key per missing target; the ROW attribution lives in the
	// parent-references count group, which itemizes every claimant.
	c := countGroup(t, r, unresolvedParentGroup)
	named := false
	for _, m := range c.MissingFromOps {
		if strings.Contains(m, "REQ-CV-001") && strings.Contains(m, "UR-CV-999") {
			named = true
		}
	}
	if !named {
		t.Fatalf("the parent-references group must name the claiming row: %v", c.MissingFromOps)
	}
}

// The parent arm keys on the TARGET, and the referenced-UR arm already names
// every UR-shaped target it can see. Two entries for one missing UR would need
// two accept keys for one fact — and one of them per referencing row, which is
// exactly the accept file nobody can maintain. So: one entry, one key,
// however many rows point at it.
func TestAnUnemittedParentURIsNamedOnceForEveryRowThatCitesIt(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | UR-CV-404 | doc | — | — |
| REQ-CV-002 | Second behavior | MVP | PROPOSED | UR-CV-404 | doc | — | — |
| REQ-CV-003 | Third behavior | MVP | PROPOSED | UR-CV-404 | doc | — | — |
`)

	r := reportOf(t, root)
	entries := 0
	for _, l := range r.Losses {
		if l.Key() == "UR-CV-404|ur-record" {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("three rows citing one unemitted UR must produce ONE entry, got %d", entries)
	}
	if got := r.Blocking(map[string]bool{"UR-CV-404|ur-record": true}); len(got) != 0 {
		t.Fatalf("the one existing accept key must still cover every row's loss: %+v", got)
	}
}

// The negative for the parent arm: a parent the batch defines. UR-CV-001 is
// declared by the epic record, so its op exists and the edge is written.
func TestARequirementParentTheBatchDefinesIsNotALoss(t *testing.T) {
	root := cleanEpicCorpus(t)
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | UR-CV-001 | doc | — | — |
`)

	r := reportOf(t, root)
	for _, l := range r.Losses {
		if l.Field == "ur-record" {
			t.Fatalf("a parent the batch defines must not be reported: %+v", l)
		}
	}
	c := countGroup(t, r, unresolvedParentGroup)
	if c.Rows != 1 || c.Ops != 1 {
		t.Fatalf("parent count = %d/%d, want 1 declared / 1 resolved", c.Rows, c.Ops)
	}
}

// An em-dash is how a ledger writes "no parent", and the builder omits the
// field entirely for one. Reading the absence as an unresolved claim would
// report a loss on every parentless row in the corpus.
func TestARowWithNoParentClaimsNothing(t *testing.T) {
	root := cleanEpicCorpus(t)

	r := reportOf(t, root)
	for _, l := range r.Losses {
		if l.Field == "ur-record" {
			t.Fatalf("a row that claims no parent must produce no parent loss: %+v", l)
		}
	}
	c := countGroup(t, r, unresolvedParentGroup)
	if c.Rows != 0 {
		t.Fatalf("a parentless corpus declares no parent references, got %d", c.Rows)
	}
}

// REQ-CROSS-261 / REQ-CROSS-221 relation arm — the epic→UR edge joins the two
// reference populations this report already scans.
//
// The epic op gained user_requirement_external_ids so the corpus's declared
// epic→UR edges stop vanishing. That immediately creates the same hazard the
// membership arm exists for: the WORKLIST cell names ids the batch does not
// define — 24 of them here, the accepted irrecoverable UR-FE-* set — and the
// server resolves membership by external id and skips what it cannot find. An
// unscanned edge would reach the store as nothing while every count still
// reconciles, which is the silent-loss shape exactly.
//
// Keyed by the target and sharing the `ur-record` field with the parent arm,
// so one unresolvable user requirement is one accept key however many epics
// point at it.
func TestEpicUserRequirementEdgeNamingAnUnemittedURIsReported(t *testing.T) {
	r := &FidelityReport{}
	epicPayload := map[string]map[string]any{
		"EPIC-CV-001": {"user_requirement_external_ids": []string{"UR-CV-001", "UR-CV-404"}},
		"EPIC-CV-002": {"user_requirement_external_ids": []string{"UR-CV-404"}},
	}
	userReqOps := map[string]bool{"UR-CV-001": true}
	r.scanUnresolvedRelations(epicPayload, map[string]map[string]any{}, map[string]bool{}, userReqOps)

	var found *FidelityLoss
	for i := range r.Losses {
		if r.Losses[i].RecordID == "UR-CV-404" && r.Losses[i].Field == "ur-record" {
			found = &r.Losses[i]
		}
	}
	if found == nil {
		t.Fatalf("an epic→UR edge to an unemitted user requirement was not reported: %v", r.Losses)
	}
	if !strings.Contains(found.Detail, "EPIC-CV-001") || !strings.Contains(found.Detail, "EPIC-CV-002") {
		t.Errorf("the detail must name the epics that claim it: %q", found.Detail)
	}

	// One target, one accept key — not one per claiming epic.
	keys := map[string]int{}
	for _, l := range r.Losses {
		keys[l.Key()]++
	}
	if keys["UR-CV-404|ur-record"] != 1 {
		t.Errorf("expected exactly one accept key for the target, got %d", keys["UR-CV-404|ur-record"])
	}

	// The resolvable edge is counted as carried, not reported.
	for _, l := range r.Losses {
		if l.RecordID == "UR-CV-001" {
			t.Errorf("a resolvable edge was reported as lost: %v", l)
		}
	}
}
