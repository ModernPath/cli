package manifest

// REQ-CROSS-013 / TASK-SY-403 — the document manifest: defaults matching the
// modernpath-v1 layout, validation, mandated-gap detection (D2).

import (
	"os"
	"path/filepath"
	"testing"
)

func scaffold(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestLoadWithoutFileReturnsDefaults(t *testing.T) {
	root := t.TempDir()
	m, fromFile, err := Load(root)
	if err != nil || fromFile {
		t.Fatalf("no-manifest load: err=%v fromFile=%v", err, fromFile)
	}
	if m.Documents[DocRequirements].Format != "rdd-ledger-v1" {
		t.Fatalf("default requirements format wrong: %+v", m.Documents[DocRequirements])
	}
	if m.Documents[DocWorklist].Path != "WORKLIST.md" {
		t.Fatalf("default worklist path wrong: %+v", m.Documents[DocWorklist])
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
}

func TestLoadReadsAndValidatesFile(t *testing.T) {
	root := scaffold(t, map[string]string{
		".modernpath/manifest.json": `{
  "schema_version": 1,
  "documents": {
    "requirements": { "paths": ["specs/*-LEDGER.md"], "format": "rdd-ledger-v1" },
    "worklist":     { "path": "PLAN.md", "format": "rdd-worklist-v1" },
    "epics":        { "paths": ["work/epics/*"], "format": "rdd-epic-v1" }
  }
}`,
	})
	m, fromFile, err := Load(root)
	if err != nil || !fromFile {
		t.Fatalf("load: err=%v fromFile=%v", err, fromFile)
	}
	if got := m.Documents[DocRequirements].Globs(); len(got) != 1 || got[0] != "specs/*-LEDGER.md" {
		t.Fatalf("custom paths wrong: %v", got)
	}
}

func TestValidateRejectsUnknownAndMismatchedFormats(t *testing.T) {
	bad := &Manifest{SchemaVersion: 1, Documents: map[string]DocSpec{
		DocRequirements: {Path: "x.md", Format: "totally-new-v9"},
	}}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown format must be rejected")
	}
	mismatched := &Manifest{SchemaVersion: 1, Documents: map[string]DocSpec{
		DocRequirements: {Path: "x.md", Format: "rdd-epic-v1"},
	}}
	if err := mismatched.Validate(); err == nil {
		t.Fatal("format/doc-type mismatch must be rejected")
	}
	wrongVersion := &Manifest{SchemaVersion: 2, Documents: Default().Documents}
	if err := wrongVersion.Validate(); err == nil {
		t.Fatal("unsupported schema_version must be rejected")
	}
}

func TestMissingMandatedIsLoudNotSilent(t *testing.T) {
	// a repo with only a worklist: requirements + epics are mandated gaps
	root := scaffold(t, map[string]string{"WORKLIST.md": "| Epic |\n"})
	missing := Default().MissingMandated(root)
	if len(missing) != 2 {
		t.Fatalf("want 2 missing mandated types, got %v", missing)
	}
	root2 := scaffold(t, map[string]string{
		"WORKLIST.md":                   "| Epic |\n",
		"tasks/TST-REQUIREMENTS.md":     "| ID |\n",
		"epics/EPIC-TST-001-example.md": "# EPIC-TST-001 — x\n",
	})
	if missing := Default().MissingMandated(root2); len(missing) != 0 {
		t.Fatalf("complete layout must report nothing, got %v", missing)
	}
}

func TestWriteRoundTrips(t *testing.T) {
	root := t.TempDir()
	m := Default()
	if err := m.Write(root); err != nil {
		t.Fatal(err)
	}
	loaded, fromFile, err := Load(root)
	if err != nil || !fromFile {
		t.Fatalf("round-trip load: err=%v fromFile=%v", err, fromFile)
	}
	if loaded.Documents[DocEpics].Format != "rdd-epic-v1" {
		t.Fatalf("round-trip lost data: %+v", loaded.Documents)
	}
}
