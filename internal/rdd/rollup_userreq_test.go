package rdd

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// REQ-CROSS-261: the WORKLIST rollup is an identity denominator and a
// two-part carrier. The id comes only from its UR cell; the statement comes
// only from that row's named epic record.
func TestRollupUserRequirementsAreCountedAndRecoveredWithoutInventing(t *testing.T) {
	root := t.TempDir()
	writeFixtureFile(t, root, "tasks/CV-REQUIREMENTS.md", `# REQUIREMENTS — CV

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-CV-001 | Only behavior | MVP | PROPOSED | — | doc | — | — |
| REQ-CV-002 | Pre-disclosed residue | MVP | PROPOSED | UR-CV-263 | doc | — | — |
`)
	writeFixtureFile(t, root, "WORKLIST.md", cvWorklistHeader+
		"| EPIC-CV-001 | [record](epics/EPIC-CV-001.md) | `UR-CV-261` — rollup prose must not become the statement | — | REQ-CV-001 | T1 | — | — | **DONE** | — | — |\n"+
		"| EPIC-CV-002 | [record](epics/EPIC-CV-002.md) | UR-CV-262 — invented prose is forbidden | — | — | T2 | — | — | **IN_PROGRESS** | — | — |\n"+
		"| EPIC-CV-003 | [record](epics/EPIC-CV-003.md) | UR-CV-263 — already named by the ledger | — | REQ-CV-002 | T3 | — | — | **DONE** | — | — |\n")
	writeFixtureFile(t, root, "epics/EPIC-CV-001.md", `# EPIC-CV-001 — Recoverable

## User outcome

As a reader, I want the record's own statement, so that imports stay honest.
`)
	writeFixtureFile(t, root, "epics/EPIC-CV-002.md", `# EPIC-CV-002 — Missing

## Context

This record deliberately has no user-outcome section.
`)
	writeFixtureFile(t, root, "epics/EPIC-CV-003.md", `# EPIC-CV-003 — Existing residue

## User outcome

This statement must not silently reclassify an already disclosed ledger loss.
`)

	r := reportOf(t, root)
	count := countGroup(t, r, "user-requirements")
	if count.Rows != 3 || count.Ops != 1 {
		t.Fatalf("rollup UR denominator = %d/%d, want 3 named / 1 emitted", count.Rows, count.Ops)
	}
	if len(count.MissingFromOps) != 2 || count.MissingFromOps[0] != "UR-CV-262" || count.MissingFromOps[1] != "UR-CV-263" {
		t.Fatalf("irrecoverable rollup UR not itemized: %v", count.MissingFromOps)
	}

	data, _ := Snapshot(root, manifest.Default())
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
	recovered := opPayloadByExternalID(ops, "UR-CV-261")
	if recovered == nil {
		t.Fatal("rollup id plus record outcome built no user-requirement op")
	}
	if got := recovered["description"]; got != "As a reader, I want the record's own statement, so that imports stay honest." {
		t.Fatalf("statement must come from the record outcome, got %q", got)
	}
	if got := recovered["work_status"]; got != "PROPOSED" {
		t.Fatalf("recovered rollup UR status = %v, want PROPOSED", got)
	}
	if missing := opPayloadByExternalID(ops, "UR-CV-262"); missing != nil {
		t.Fatalf("a rollup id without a user outcome must emit no entity: %v", missing)
	}
	if preDisclosed := opPayloadByExternalID(ops, "UR-CV-263"); preDisclosed != nil {
		t.Fatalf("rollup recovery must not reclassify an existing ledger-named loss: %v", preDisclosed)
	}
	for _, op := range ops {
		if op.Type == "upsert_requirement" && strings.Contains(stringPayload(op.Payload, "description"), "invented prose") {
			t.Fatalf("rollup cell prose was synthesized into a statement: %v", op.Payload)
		}
	}

	missingLoss := findLoss(r.Losses, "UR-CV-262", "ur-record")
	if missingLoss == nil || missingLoss.Category != LossLost {
		t.Fatalf("irrecoverable rollup UR must be a named loss: %+v", r.Losses)
	}
	for _, loss := range r.Blocking(map[string]bool{missingLoss.Key(): true}) {
		if loss.Key() == missingLoss.Key() {
			t.Fatalf("the exact accept-file key did not cover the missing UR: %+v", loss)
		}
	}

	statusLoss := findLoss(r.Losses, "UR-CV-261", "rollup-work-status")
	if statusLoss == nil || statusLoss.Category != LossExcluded {
		t.Fatalf("the deliberate DONE→PROPOSED discrepancy must be disclosed: %+v", r.Losses)
	}
}

func opPayloadByExternalID(ops []Op, id string) map[string]any {
	for _, op := range ops {
		if op.Payload["external_id"] == id {
			return op.Payload
		}
	}
	return nil
}

func stringPayload(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}
