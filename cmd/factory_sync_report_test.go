package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-386 (EPIC-CLI-018): a partial batch answers 207 with per-op
// failed/deferred/skipped rows; the CLI prints each with its reason, names the
// retry, and exits non-zero when any op failed.
func TestREQCROSS386SyncReportsFailedOpsAndExitsNonZero(t *testing.T) {
	body := map[string]any{"data": map[string]any{
		"results": []any{
			map[string]any{"external_id": "REQ-A", "type": "upsert_requirement", "result": "created"},
			map[string]any{"external_id": "REQ-X", "type": "upsert_requirement", "result": "failed", "op_index": float64(1), "reason": "work_status: is invalid"},
			map[string]any{"external_id": "REQ-X", "type": "upsert_requirement", "result": "skipped", "op_index": float64(2), "reason": "depends on op 1, which failed"},
			map[string]any{"external_id": "docs/a.md", "type": "upsert_document", "result": "deferred", "op_index": float64(3), "reason": "embedding: 3 of 3 current chunks lack required embeddings"},
		},
		"failed": float64(1), "skipped": float64(1), "deferred": float64(1),
	}}

	var err error
	out := captureOut(t, func() { err = reportSyncOutcome(207, body) })
	if err == nil {
		t.Fatal("a batch with a failed op must exit non-zero")
	}
	for _, want := range []string{"REQ-X", "work_status: is invalid", "skipped", "docs/a.md", "deferred"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the report must name every failed, skipped and deferred op with its reason (%q):\n%s", want, out)
		}
	}
	if !strings.Contains(err.Error(), "factory sync") {
		t.Fatalf("the error must name the retry, got %v", err)
	}
}

func TestREQCROSS386SyncWithOnlyDeferredOpsWarnsButSucceeds(t *testing.T) {
	body := map[string]any{"data": map[string]any{
		"results": []any{
			map[string]any{"external_id": "docs/a.md", "type": "upsert_document", "result": "deferred", "op_index": float64(0), "reason": "embedding: provider unavailable"},
		},
		"failed": float64(0), "skipped": float64(0), "deferred": float64(1),
	}}
	var err error
	out := captureOut(t, func() { err = reportSyncOutcome(207, body) })
	if err != nil {
		t.Fatalf("a deferred document is a warning, not a failure: %v", err)
	}
	if !strings.Contains(out, "docs/a.md") || !strings.Contains(out, "deferred") {
		t.Fatalf("the deferred op must be named:\n%s", out)
	}
}
