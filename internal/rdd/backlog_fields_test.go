package rdd

// REQ-CROSS-251 (EPIC-CLI-003 T13): backlog and gap records land
// field-complete — the folded facts reach typed payload fields, the row
// rides verbatim as raw_body, and the route cell's overload unbundles into
// disposition/disposition_ref/candidate_route.

import (
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

func backlogPayloads(t *testing.T, backlogMD, gapMD string) map[string]map[string]any {
	t.Helper()
	root := cleanEpicCorpus(t)
	if backlogMD != "" {
		writeFixtureFile(t, root, "BACKLOG.md", backlogMD)
	}
	if gapMD != "" {
		writeFixtureFile(t, root, "process/gap-register.md", gapMD)
	}
	ops := opsOfRoot(t, root)
	out := map[string]map[string]any{}
	for _, op := range ops {
		if op.Type == "upsert_backlog_record" {
			title, _ := op.Payload["title"].(string)
			out[title] = op.Payload
		}
	}
	return out
}

func opsOfRoot(t *testing.T, root string) []Op {
	t.Helper()
	data, _ := Snapshot(root, manifest.Default())
	return BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-24")
}

func TestBacklogFoldedFactsLandTyped(t *testing.T) {
	rows := backlogPayloads(t, `# Backlog

| Discovery | Notes | Tracked as / suggested route |
|---|---|---|
| **A found defect** (`+"`RUN:2026-08-18`"+`, CLI verification run) | The endpoint 404s; affects REQ-CV-001 and EPIC-CV-001. | **ROUTED** → REQ-CROSS-211 |
| **An unrouted idea** | Needs an owner. | CV |
`, "")

	routed := rows["A found defect"]
	if routed == nil {
		t.Fatalf("routed row missing: %v", keysOf(rows))
	}
	if routed["raised_at"] != "2026-08-18" {
		t.Fatalf("raised_at = %v, want the RUN date", routed["raised_at"])
	}
	if routed["raised_by"] != "CLI verification run" {
		t.Fatalf("raised_by = %v", routed["raised_by"])
	}
	if routed["disposition"] != "routed" || routed["disposition_ref"] != "REQ-CROSS-211" {
		t.Fatalf("disposition = %v/%v", routed["disposition"], routed["disposition_ref"])
	}
	affected, _ := routed["affected_external_ids"].([]any)
	if !strings.Contains(strings.Join(anyStrings(affected), " "), "REQ-CV-001") {
		t.Fatalf("affected ids = %v", affected)
	}
	if raw, _ := routed["raw_body"].(string); !strings.Contains(raw, "**ROUTED** → REQ-CROSS-211") {
		t.Fatalf("raw_body must carry the row verbatim: %q", raw)
	}

	unrouted := rows["An unrouted idea"]
	if unrouted == nil {
		t.Fatalf("unrouted row missing")
	}
	if unrouted["disposition"] != nil {
		t.Fatalf("an unrouted row asserts no disposition: %v", unrouted["disposition"])
	}
	if unrouted["candidate_route"] != "CV" {
		t.Fatalf("candidate_route = %v", unrouted["candidate_route"])
	}
}

func TestGapRegisterRowsUnbundle(t *testing.T) {
	rows := backlogPayloads(t, "", `# Gap Register

| ID | Description | Owning Context | Deferred Until | Related Reqs |
|---|---|---|---|---|
| GAP-902 | Durable partial-success presentation | KNW | Follow-up slice | REQ-KNW-105 (DEFERRED) |
`)
	var gap map[string]any
	for _, p := range rows {
		if p["external_id"] == "GAP-902" {
			gap = p
		}
	}
	if gap == nil {
		t.Fatalf("gap row missing: %v", keysOf(rows))
	}
	if gap["gap_kind"] != "capability" {
		t.Fatalf("gap_kind = %v", gap["gap_kind"])
	}
	if gap["candidate_route"] != "KNW" {
		t.Fatalf("candidate_route = %v", gap["candidate_route"])
	}
	if gap["why_unrouted"] != "Follow-up slice" {
		t.Fatalf("why_unrouted = %v", gap["why_unrouted"])
	}
	if gap["disposition"] != "deferred" || gap["disposition_ref"] != "REQ-KNW-105" {
		t.Fatalf("disposition = %v/%v", gap["disposition"], gap["disposition_ref"])
	}
}

func keysOf(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
