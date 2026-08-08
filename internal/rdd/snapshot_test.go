package rdd

// SCN-SY-046 (EPIC-SYNC-005, REQ-CROSS-016): a repo whose manifest points one
// level up (../WORKLIST.md, ../epics/*) must resolve the worklist's
// workspace-relative record links ("[record](epics/...)") — the record text
// feeds epic titles, requirement links, and approval gates, so a silent miss
// changes op content between roots.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

func writeFixtureWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	for _, d := range []string{"epics", "tasks", "sub", "epics/EPIC-X-002-folder/specs"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worklist := strings.Join([]string{
		"| Epic | Epic record | UR | SCN | SR | Tasks | Upper | Lower | Overall status | Human approval | Notes |",
		"|---|---|---|---|---|---|---|---|---|---|---|",
		"| EPIC-X-001 | [record](epics/EPIC-X-001-fixture.md) | u | s | r | t | — | — | IN_PROGRESS | pending | n |",
		"| EPIC-X-002 | [record](epics/EPIC-X-002-folder/EPIC.md) | u | s | r | t | — | — | PROPOSED (spec ready) | — | n |",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(ws, "WORKLIST.md"), []byte(worklist), 0o644); err != nil {
		t.Fatal(err)
	}
	record := "# EPIC-X-001 — Fixture epic\n\n## Requirements in this epic\nREQ-XX-001\n"
	if err := os.WriteFile(filepath.Join(ws, "epics", "EPIC-X-001-fixture.md"), []byte(record), 0o644); err != nil {
		t.Fatal(err)
	}
	folderRecord := "# EPIC-X-002 — Folder epic\n\n## Specification status\nSPEC-READY — awaiting the gate.\n\n## Requirements in this epic\nREQ-XX-002\n"
	if err := os.WriteFile(filepath.Join(ws, "epics", "EPIC-X-002-folder", "EPIC.md"), []byte(folderRecord), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"requirements.md": "# Reqs spec", "architecture.md": "# Arch spec"} {
		if err := os.WriteFile(filepath.Join(ws, "epics", "EPIC-X-002-folder", "specs", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func specsOf(t *testing.T, data Data, id string) []EpicSpec {
	t.Helper()
	for _, e := range data.Epics {
		if e.ID == id {
			return e.Specs
		}
	}
	t.Fatalf("epic %s not found", id)
	return nil
}

func TestSnapshotCollectsFolderEpicSpecs(t *testing.T) {
	ws := writeFixtureWorkspace(t)
	data, _ := Snapshot(ws, manifest.Default())

	specs := specsOf(t, data, "EPIC-X-002")
	if len(specs) != 2 {
		t.Fatalf("want 2 specs, got %d", len(specs))
	}
	// sorted by name; Rel is the workspace path (payload identity)
	if specs[0].Name != "architecture.md" || specs[0].Rel != "epics/EPIC-X-002-folder/specs/architecture.md" ||
		specs[0].Content != "# Arch spec" {
		t.Fatalf("spec collection wrong: %+v", specs[0])
	}
	// file-form epic collects nothing
	if got := specsOf(t, data, "EPIC-X-001"); len(got) != 0 {
		t.Fatalf("file epic must have no specs, got %+v", got)
	}
}

func TestSnapshotCollectsSpecsFromSubRootWithCanonicalIdentity(t *testing.T) {
	ws := writeFixtureWorkspace(t)
	sub := filepath.Join(ws, "sub")
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Documents: map[string]manifest.DocSpec{
			manifest.DocRequirements: {Paths: []string{"../tasks/*-REQUIREMENTS.md"}, Format: "rdd-ledger-v1"},
			manifest.DocWorklist:     {Path: "../WORKLIST.md", Format: "rdd-worklist-v1"},
			manifest.DocEpics:        {Paths: []string{"../epics/*"}, Format: "rdd-epic-v1"},
		},
	}
	data, _ := Snapshot(sub, m)
	specs := specsOf(t, data, "EPIC-X-002")
	if len(specs) != 2 {
		t.Fatalf("want 2 specs from sub-root, got %d", len(specs))
	}
	// Rel stays canonical (workspace-relative) even though bytes were read via RecordFS
	if specs[0].Rel != "epics/EPIC-X-002-folder/specs/architecture.md" {
		t.Fatalf("Rel must stay payload-canonical from a sub-root, got %q", specs[0].Rel)
	}
	if specs[0].Content != "# Arch spec" {
		t.Fatalf("spec bytes must resolve via RecordFS, got %q", specs[0].Content)
	}
}

func TestSnapshotResolvesRecordPathsFromWorkspaceRoot(t *testing.T) {
	ws := writeFixtureWorkspace(t)
	data, _ := Snapshot(ws, manifest.Default())
	epic := epicByID(t, data, "EPIC-X-001")
	text := ReadEpicRecord(ws, epic.Record)
	if !strings.Contains(text, "Fixture epic") {
		t.Fatalf("record text not resolved from workspace root (record=%q)", epic.Record)
	}
}

func epicByID(t *testing.T, data Data, id string) Epic {
	t.Helper()
	for _, e := range data.Epics {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("epic %s not found", id)
	return Epic{}
}

func TestSnapshotResolvesRecordPathsFromSubRoot(t *testing.T) {
	ws := writeFixtureWorkspace(t)
	sub := filepath.Join(ws, "sub")
	m := &manifest.Manifest{
		SchemaVersion: 1,
		Documents: map[string]manifest.DocSpec{
			manifest.DocRequirements: {Paths: []string{"../tasks/*-REQUIREMENTS.md"}, Format: "rdd-ledger-v1"},
			manifest.DocWorklist:     {Path: "../WORKLIST.md", Format: "rdd-worklist-v1"},
			manifest.DocEpics:        {Paths: []string{"../epics/*"}, Format: "rdd-epic-v1"},
		},
	}
	data, _ := Snapshot(sub, m)
	epic := epicByID(t, data, "EPIC-X-001")
	// Record is payload identity (origin_ref) — must stay workspace-relative,
	// byte-identical to what a workspace-root sync produces.
	if epic.Record != "epics/EPIC-X-001-fixture.md" {
		t.Fatalf("Record must stay canonical, got %q", epic.Record)
	}
	if epic.RecordFS == "" {
		t.Fatal("RecordFS must carry the sync-root-resolvable path")
	}
	text := ReadEpicRecord(sub, epic.RecordFS)
	if !strings.Contains(text, "Fixture epic") {
		t.Fatalf("worklist record link must resolve relative to the worklist file, not the sync root (recordFS=%q)", epic.RecordFS)
	}
}

func TestSnapshotWarnsOnImplementationWithoutSpecApproval(t *testing.T) {
	ws := t.TempDir()
	for _, d := range []string{"epics/EPIC-W-001-w/specs", "tasks"} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worklist := strings.Join([]string{
		"| Epic | Epic record | UR | SCN | SR | Tasks | Upper | Lower | Overall status | Human approval | Notes |",
		"|---|---|---|---|---|---|---|---|---|---|---|",
		"| EPIC-W-001 | [record](epics/EPIC-W-001-w/EPIC.md) | u | s | r | t | — | — | IN_PROGRESS (spec ready) | — | n |",
		"",
	}, "\n")
	os.WriteFile(filepath.Join(ws, "WORKLIST.md"), []byte(worklist), 0o644)
	record := "# EPIC-W-001 — Working early\n\n## Specification status\nSPEC-READY — awaiting the gate.\n"
	os.WriteFile(filepath.Join(ws, "epics", "EPIC-W-001-w", "EPIC.md"), []byte(record), 0o644)
	os.WriteFile(filepath.Join(ws, "epics", "EPIC-W-001-w", "specs", "requirements.md"), []byte("# R"), 0o644)

	_, warnings := Snapshot(ws, manifest.Default())
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "SPEC-APPROVE-EPIC-W-001") {
			found = true
		}
	}
	if !found {
		t.Fatalf("in-progress epic without spec approval must warn loudly, got %v", warnings)
	}
}
