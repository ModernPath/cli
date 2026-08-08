package cmd

// EPIC-SYNC-007 (REQ-CROSS-025, SCN-SY-071/072/073): the write-back mechanics —
// clean apply writes the server bytes + resets the epic's spec marker; a dirty
// local file refuses (never a silent overwrite); the marker flip re-opens the gate.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedSpecEpic(t *testing.T, root, content string) string {
	t.Helper()
	dir := filepath.Join(root, "epics", "EPIC-DS-001-x")
	if err := os.MkdirAll(filepath.Join(dir, "specs"), 0o755); err != nil {
		t.Fatal(err)
	}
	epic := "# EPIC-DS-001 — x\n\n## Specification status\nSPEC-APPROVED — approved USER:2026-08-06.\n\n## Human approval\n"
	if err := os.WriteFile(filepath.Join(dir, "EPIC.md"), []byte(epic), 0o644); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("epics", "EPIC-DS-001-x", "specs", "requirements.md")
	if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestApplySpecIntentCleanWriteAndMarkerReset(t *testing.T) {
	root := t.TempDir()
	rel := seedSpecEpic(t, root, "# v1")

	status, err := applySpecIntent(root, rel, "# v2 (server)", contentSha("# v1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if status != "applied" {
		t.Fatalf("want applied, got %s", status)
	}

	got, _ := os.ReadFile(filepath.Join(root, rel))
	if string(got) != "# v2 (server)" {
		t.Fatalf("file not written: %q", got)
	}

	epic, _ := os.ReadFile(filepath.Join(root, "epics", "EPIC-DS-001-x", "EPIC.md"))
	if !strings.Contains(string(epic), "SPEC-DRAFT") {
		t.Fatalf("marker not reset to SPEC-DRAFT (SCN-SY-073): %s", epic)
	}
	if strings.Contains(string(epic), "SPEC-APPROVED") {
		t.Fatalf("stale SPEC-APPROVED marker survived: %s", epic)
	}
}

func TestApplySpecIntentRefusesDirtyLocal(t *testing.T) {
	root := t.TempDir()
	rel := seedSpecEpic(t, root, "# v2 (local edit)")

	status, err := applySpecIntent(root, rel, "# v2 (server)", contentSha("# v1"), false)
	if err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Fatalf("want conflict, got %s", status)
	}

	got, _ := os.ReadFile(filepath.Join(root, rel))
	if string(got) != "# v2 (local edit)" {
		t.Fatalf("local bytes clobbered (SCN-SY-072): %q", got)
	}
}

func TestApplySpecIntentServerFlaggedConflictRefuses(t *testing.T) {
	root := t.TempDir()
	rel := seedSpecEpic(t, root, "# v1")

	status, err := applySpecIntent(root, rel, "# v2 (server)", contentSha("# v1"), true)
	if err != nil {
		t.Fatal(err)
	}
	if status != "conflict" {
		t.Fatalf("server-flagged conflict must refuse, got %s", status)
	}
}

func TestApplySpecIntentMissingLocalFileWrites(t *testing.T) {
	root := t.TempDir()
	rel := seedSpecEpic(t, root, "# v1")
	if err := os.Remove(filepath.Join(root, rel)); err != nil {
		t.Fatal(err)
	}

	status, err := applySpecIntent(root, rel, "# v2 (server)", contentSha("# v1"), false)
	if err != nil || status != "applied" {
		t.Fatalf("missing local file should apply, got %s %v", status, err)
	}
}
