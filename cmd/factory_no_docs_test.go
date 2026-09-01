package cmd

import "testing"

// A representative slice of the ops a workspace sync builds: process state plus
// the workspace-document ops that carry the docs/ corpus.
func sampleSyncOps() []map[string]any {
	return []map[string]any{
		{"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-AUTH-047"}},
		{"type": "upsert_document", "payload": map[string]any{"external_id": "docs/03-architecture.md"}},
		{"type": "upsert_epic", "payload": map[string]any{"external_id": "EPIC-CLI-006"}},
		{"type": "upsert_document", "payload": map[string]any{"external_id": "docs/07-api-contracts.md"}},
		{"type": "upsert_gate", "payload": map[string]any{"external_id": "GATE-ENTRY-EPIC-CLI-006"}},
	}
}

// --no-docs drops exactly the workspace-document ops and keeps the process
// state, so a sync can land requirements/epics/gates on an environment whose
// embedding provider is not yet configured (core 422s an un-embeddable doc and
// halts the whole batch, blocking the state behind it).
func TestNoDocsFilterDropsOnlyDocuments(t *testing.T) {
	kept, dropped := noDocsFilter(sampleSyncOps(), true)

	if dropped != 2 {
		t.Fatalf("want 2 document ops dropped, got %d", dropped)
	}
	if len(kept) != 3 {
		t.Fatalf("want 3 state ops kept, got %d", len(kept))
	}
	for _, op := range kept {
		if str(op, "type") == "upsert_document" {
			t.Fatalf("a document op survived the filter: %v", op)
		}
	}
	// The state ops must be untouched and in order.
	wantTypes := []string{"upsert_requirement", "upsert_epic", "upsert_gate"}
	for i, want := range wantTypes {
		if got := str(kept[i], "type"); got != want {
			t.Errorf("kept[%d] = %q, want %q — order or content changed", i, got, want)
		}
	}
}

// Off by default: without --no-docs the ops pass through unchanged, so the
// normal sync still carries the full corpus.
func TestNoDocsFilterOffIsIdentity(t *testing.T) {
	in := sampleSyncOps()
	kept, dropped := noDocsFilter(in, false)

	if dropped != 0 {
		t.Fatalf("filter off must drop nothing, got %d", dropped)
	}
	if len(kept) != len(in) {
		t.Fatalf("filter off must keep every op: got %d, want %d", len(kept), len(in))
	}
}

// A workspace with no document ops (nothing under docs/) still syncs cleanly
// with --no-docs on: nothing to drop, everything kept.
func TestNoDocsFilterWithNoDocumentsDropsNothing(t *testing.T) {
	in := []map[string]any{
		{"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-AUTH-047"}},
		{"type": "upsert_gate", "payload": map[string]any{"external_id": "GATE-X"}},
	}
	kept, dropped := noDocsFilter(in, true)
	if dropped != 0 {
		t.Fatalf("no documents present: want 0 dropped, got %d", dropped)
	}
	if len(kept) != len(in) {
		t.Fatalf("want all %d ops kept, got %d", len(in), len(kept))
	}
}
