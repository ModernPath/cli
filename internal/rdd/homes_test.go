package rdd

// REQ-CROSS-223 — focused lower evidence, clause-mapped to
// epics/EPIC-CLI-003-ledger-import/specs/requirements.md §223.
// RED first: the store homes and their op paths do not exist today.

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

const homesWorklist = `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-HM-001 | — | UR-HM-001 | SCN-HM-001 | REQ-HM-001 | T1 | UPPER_VALIDATED | LOWER_VERIFIED | **IN_REVIEW** — evidence complete, completion gate next | — | — |
| EPIC-HM-002 | — | UR-HM-002 | SCN-HM-002 | REQ-HM-002 | T1 | — | — | — | — | — |
`

// §223.3 — the WORKLIST Overall-status cell's PROCESS.md token is captured
// and rides the epic payload as process_status; an empty cell stays absent.
func TestWorklistCapturesProcessStatus(t *testing.T) {
	epics := ParseWorklist(homesWorklist, nil, map[string]bool{})
	if len(epics) != 2 {
		t.Fatalf("fixture parsed %d epics, want 2", len(epics))
	}
	if epics[0].ProcessStatus != "IN_REVIEW" {
		t.Fatalf("ProcessStatus = %q, want IN_REVIEW from the Overall cell", epics[0].ProcessStatus)
	}
	if epics[1].ProcessStatus != "" {
		t.Fatalf("empty Overall cell must stay absent, got %q", epics[1].ProcessStatus)
	}

	op := BuildEpicOp(epics[0], "")
	if op.Payload["process_status"] != "IN_REVIEW" {
		t.Fatalf("epic payload process_status = %v, want IN_REVIEW", op.Payload["process_status"])
	}
	bare := BuildEpicOp(epics[1], "")
	if _, present := bare.Payload["process_status"]; present {
		t.Fatalf("empty Overall must omit process_status, carries %v", bare.Payload["process_status"])
	}
}

const homesBacklog = `# Backlog

| Discovery | Notes | Tracked as / suggested route |
|---|---|---|
| **A live discovery** (fixture) | long notes about it | NEW — route it |
`

const homesGaps = `# Gap register

| Gap | Affected | Route |
|---|---|---|
| GAP-HM-1 — something missing | REQ-HM-001 | open |
`

func homesRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("BACKLOG.md", homesBacklog)
	write("process/gap-register.md", homesGaps)
	// Mandated documents so Snapshot has a corpus at all.
	write("tasks/HM-REQUIREMENTS.md", "# REQUIREMENTS — HM\n\n| ID | Title | Stage | Status | UR | Source | Tests | Code |\n|---|---|---|---|---|---|---|---|\n| REQ-HM-001 | A row | MVP | PROPOSED | UR-HM-001 | doc | — | — |\n")
	write("WORKLIST.md", homesWorklist)
	return root
}

// §223.2 — BACKLOG discovery rows and gap-register rows parse into the
// corpus with stable identities and emit upsert_backlog_record ops.
func TestBacklogAndGapRowsParseAndEmit(t *testing.T) {
	root := homesRoot(t)
	data, _ := Snapshot(root, manifest.Default())

	if len(data.Backlog) != 2 {
		t.Fatalf("parsed %d backlog/gap rows, want 2 (one each): %+v", len(data.Backlog), data.Backlog)
	}
	var backlog, gap *BacklogRow
	for i := range data.Backlog {
		switch data.Backlog[i].Kind {
		case "backlog":
			backlog = &data.Backlog[i]
		case "gap":
			gap = &data.Backlog[i]
		}
	}
	if backlog == nil || gap == nil {
		t.Fatalf("kinds not classified: %+v", data.Backlog)
	}
	if gap.ExternalID != "GAP-HM-1" {
		t.Fatalf("a gap row with an explicit id must keep it, got %q", gap.ExternalID)
	}
	if len(backlog.ExternalID) < 5 || backlog.ExternalID[:3] != "BL-" {
		t.Fatalf("a backlog row without an id gets a stable derived one (BL-…), got %q", backlog.ExternalID)
	}
	if backlog.NotesMD == "" || backlog.Route == "" {
		t.Fatalf("notes and route must be carried: %+v", backlog)
	}

	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	found := 0
	for _, op := range ops {
		if op.Type != "upsert_backlog_record" {
			continue
		}
		found++
		p := op.Payload
		if p["external_id"] == nil || p["kind"] == nil || p["title"] == nil ||
			p["content_hash"] == nil || p["actor"] == nil {
			t.Fatalf("backlog op payload incomplete: %v", p)
		}
	}
	if found != 2 {
		t.Fatalf("want 2 upsert_backlog_record ops, got %d", found)
	}
}

// §221 detectors follow the homes: with process_status and backlog ops
// emitted, the overall-status and backlog/gap losses close and the rows
// reconcile as count groups; stripped, the losses reappear.
func TestFidelityClosesHomesLosses(t *testing.T) {
	root := homesRoot(t)
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")

	r := BuildFidelityReport(root, m, data, ops)
	if l := findLoss(r.Losses, "EPIC-HM-001", "overall-status"); l != nil {
		t.Fatalf("overall-status loss still claimed after process_status is carried: %+v", l)
	}
	if l := findLoss(r.Losses, "BACKLOG.md", "backlog-rows"); l != nil {
		t.Fatalf("backlog loss still claimed after the rows emit ops: %+v", l)
	}
	bl := countGroup(t, r, "backlog")
	if bl.Rows != 1 || bl.Ops != 1 {
		t.Fatalf("backlog count group = %d/%d, want 1/1", bl.Rows, bl.Ops)
	}
	gp := countGroup(t, r, gapRecordsGroup)
	if gp.Rows != 1 || gp.Ops != 1 {
		t.Fatalf("gap-register count group = %d/%d, want 1/1", gp.Rows, gp.Ops)
	}

	var stripped []Op
	for _, op := range ops {
		if op.Type == "upsert_backlog_record" {
			continue
		}
		if op.Type == "upsert_epic" {
			cp := map[string]any{}
			for k, v := range op.Payload {
				cp[k] = v
			}
			delete(cp, "process_status")
			op = Op{Type: op.Type, Payload: cp}
		}
		stripped = append(stripped, op)
	}
	degraded := BuildFidelityReport(root, m, data, stripped)
	if l := findLoss(degraded.Losses, "EPIC-HM-001", "overall-status"); l == nil {
		t.Fatal("stripped process_status did not re-surface the overall-status loss")
	}
	if bl := countGroup(t, degraded, "backlog"); len(bl.MissingFromOps) != 1 {
		t.Fatalf("stripped backlog ops must itemize the missing row, got %+v", bl)
	}
}

// Observed live: a duplicated WORKLIST rollup row emitted a
// second epic op whose hash ping-ponged the store shadow forever. First
// occurrence wins, like ledger rows; the fidelity report names the shadowed
// row instead of letting the duplicate sync (a guard flags).
func TestWorklistDuplicateRollupIdsDedupFirstWinsAndFlag(t *testing.T) {
	dup := homesWorklist +
		"| EPIC-HM-001 (variant) | — | UR-HM-001 | SCN-HM-001 | REQ-HM-001 | T1 | — | — | **DONE** — other | — | — |\n"
	epics := ParseWorklist(dup, nil, map[string]bool{})
	if len(epics) != 2 {
		t.Fatalf("duplicate rollup rows must dedup first-wins, got %d epics", len(epics))
	}
	if epics[0].ProcessStatus != "IN_REVIEW" {
		t.Fatalf("the FIRST row must win, got ProcessStatus %q", epics[0].ProcessStatus)
	}

	root := homesRoot(t)
	if err := os.WriteFile(filepath.Join(root, "WORKLIST.md"), []byte(dup), 0o644); err != nil {
		t.Fatal(err)
	}
	m := manifest.Default()
	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(string) string { return "" }, "2026-08-21")
	r := BuildFidelityReport(root, m, data, ops)
	found := false
	for _, h := range r.Hygiene {
		if strings.Contains(h.Detail, "duplicate WORKLIST rollup id EPIC-HM-001") && h.Line > 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the shadowed duplicate row must be flagged with a location: %+v", r.Hygiene)
	}
}

// The archival carriers are manifest-resolved: a binding that relocates its
// worklist/review-queue/open-questions files still gets them carried whole.
// A hardcoded root-relative read returned "" for a relocated file and the
// build loop skipped it with no warning — zero archival documents rode the
// batch while the checker probed the same missing paths.
func TestArchivalCarriersFollowTheManifest(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tasks/HM-REQUIREMENTS.md", "# REQUIREMENTS — HM\n\n| ID | Title | Stage | Status | UR | Source | Tests | Code |\n|---|---|---|---|---|---|---|---|\n| REQ-HM-001 | A row | MVP | PROPOSED | UR-HM-001 | doc | — | — |\n")
	write("planning/PLAN.md", "# V-Model Loop Work-List\n\nrelocated worklist prose that must survive the flip\n")

	m := manifest.Default()
	m.Documents[manifest.DocWorklist] = manifest.DocSpec{Path: "planning/PLAN.md", Format: "rdd-worklist-v1"}

	data, _ := Snapshot(root, m)
	ops := BuildOps(data, func(rel string) string { return ReadEpicRecord(root, rel) }, "2026-08-21")

	var content string
	for _, op := range ops {
		if op.Type == "upsert_process_record" && op.Payload["external_id"] == "planning/PLAN.md" {
			enc, _ := op.Payload["raw_content"].(string)
			raw, _ := base64.StdEncoding.DecodeString(enc)
			content = string(raw)
		}
	}
	if !strings.Contains(content, "relocated worklist prose") {
		t.Fatalf("relocated worklist did not ride the batch in the byte archive (got %d ops)", len(ops))
	}
}

// TODO is a legal epic lifecycle state (PROCESS.md entry approval; the
// requirement work_status vocabulary already carries it) — the Overall cell
// token must parse, not silently never sync.
func TestWorklistCapturesTodoProcessStatus(t *testing.T) {
	wl := `# WORKLIST

## Epic Rollup

| Epic | Epic record | User requirements | Acceptance scenarios | System requirements | Tasks | Upper status | Lower status | Overall status | Human approval | Evidence / gaps |
|---|---|---|---|---|---|---|---|---|---|---|
| EPIC-HM-009 | — | UR-HM-009 | SCN-HM-009 | REQ-HM-009 | T1 | — | — | **TODO** — entry approved | — | — |
`
	epics := ParseWorklist(wl, nil, map[string]bool{})
	if len(epics) != 1 {
		t.Fatalf("fixture parsed %d epics, want 1", len(epics))
	}
	if epics[0].ProcessStatus != "TODO" {
		t.Fatalf("ProcessStatus = %q, want TODO from the Overall cell", epics[0].ProcessStatus)
	}
}
