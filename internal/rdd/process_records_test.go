package rdd

// REQ-CROSS-253 (EPIC-CLI-003 T13): the retired-file byte archive lands in
// process_records — exact bytes with sha256 identity, unsplit — replacing the
// split-document workaround; explanatory documents still ride as documents,
// and the fidelity whole-file suppression counts the new carriers.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

func processRecordOps(ops []Op) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, op := range ops {
		if op.Type == "upsert_process_record" {
			id, _ := op.Payload["external_id"].(string)
			out[id] = op.Payload
		}
	}
	return out
}

func TestRetiredFilesLandAsProcessRecords(t *testing.T) {
	root := cleanEpicCorpus(t)
	// an oversized review-queue file — bigger than the old document cap, so
	// the old carrier would have split it; the byte archive must not
	big := "### RQ-9001 — APPROVED\n\n" + strings.Repeat("The full text survives. ", 15000)
	writeFixtureFile(t, root, "docs/85-loop-review-queue.md", big)

	data, _ := Snapshot(root, manifest.Default())
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
	records := processRecordOps(ops)

	// the epic record archives as bytes, not as a process document
	rec := records["epics/EPIC-CV-001-only.md"]
	if rec == nil {
		t.Fatalf("epic record archived no process record: %v", keysOfAny(records))
	}
	if rec["record_type"] != "epic" {
		t.Fatalf("record_type = %v", rec["record_type"])
	}
	for _, op := range ops {
		if op.Type == "upsert_document" && op.Payload["external_id"] == "epics/EPIC-CV-001-only.md" {
			t.Fatalf("the record must not double-carry as a process document")
		}
	}

	// the oversized file is one unsplit record whose bytes round-trip
	rq := records["docs/85-loop-review-queue.md"]
	if rq == nil {
		t.Fatalf("review queue archived no process record")
	}
	raw, _ := base64.StdEncoding.DecodeString(rq["raw_content"].(string))
	if string(raw) != big {
		t.Fatalf("bytes did not round-trip: %d vs %d", len(raw), len(big))
	}
	sum := sha256.Sum256([]byte(big))
	if rq["content_sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatalf("sha mismatch: %v", rq["content_sha256"])
	}
	if rq["size_bytes"] != len(big) {
		t.Fatalf("size_bytes = %v, want %d", rq["size_bytes"], len(big))
	}
	if _, split := records["docs/85-loop-review-queue.md#part2"]; split {
		t.Fatalf("the byte archive must not split")
	}

	// suppression still holds: the whole-file carrier is the process record,
	// so the oversized RQ body raises no gate-body loss
	r := BuildFidelityReport(root, manifest.Default(), data, ops)
	for _, l := range r.Losses {
		if l.Field == "gate-body" && strings.HasPrefix(l.RecordID, "RQ-") {
			t.Fatalf("whole-file suppression lost with the carrier move: %+v", l)
		}
	}
	g := countGroup(t, r, homeArchiveGroup)
	if g.Rows == 0 || g.Ops != g.Rows {
		t.Fatalf("archive home must reconcile from process records: %+v", g)
	}
}

func keysOfAny(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
