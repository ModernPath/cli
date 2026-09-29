package cmd

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSystemExportPathKeepsReservedNamesInsideDocsNamespace(t *testing.T) {
	root := t.TempDir()
	for _, slug := range []string{"runtime", "docs", "source", "tasks", "specs", "skills", "rdd"} {
		if err := validateSystemExportSlug(slug); err != nil {
			t.Errorf("reserved slug %q should be valid: %v", slug, err)
			continue
		}
		got := systemExportRootDir(root, slug)
		want := filepath.Join(root, "docs", slug)
		if got != want {
			t.Errorf("new root for %q = %q, want %q", slug, got, want)
		}
	}
	if got := systemExportRootDir(root, "demo"); got != filepath.Join(root, "demo") {
		t.Errorf("ordinary system root = %q", got)
	}
}

func TestSystemExportPathRecognizesExistingLegacyAndFlatRoots(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, "docs", "runtime")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "blueprint.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := systemExportRootDir(root, "runtime"); got != legacy {
		t.Fatalf("legacy reserved root = %q, want %q", got, legacy)
	}
	flat := filepath.Join(root, "docs")
	if err := os.MkdirAll(flat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(flat, "docs_push_manifest.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := systemExportRootDir(root, "docs"); got != filepath.Join(root, "docs", "docs") {
		t.Fatalf("protected flat reserved root = %q; sync destination must be nested", got)
	}
}

func TestDocsPushDiscoversReservedSlugInNestedNamespace(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "docs", "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs_push_manifest.json"), []byte(`{"version":1,"system_doc_files":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := listSystemExportSlugs(root)
	if len(got) != 1 || got[0] != "skills" {
		t.Fatalf("discovered slugs = %v, want [skills]", got)
	}
	if resolved, ok := resolveSystemRootDir(root, "skills"); !ok || resolved != dir {
		t.Fatalf("resolved nested reserved root = %q, %v", resolved, ok)
	}
	for _, rel := range []string{"docs", "docs/skills"} {
		if shouldSkipDocPushSubtree(rel) {
			t.Errorf("docpush skipped legacy/nested system container %q", rel)
		}
	}
}

func TestExportGeneratedAtReadsServerMetadata(t *testing.T) {
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for _, entry := range []struct{ path, timestamp string }{
		{".modernpath/viewer/docs_push_manifest.json", "2030-01-01T00:00:00Z"},
		{".modernpath/other/docs_push_manifest.json", "2030-01-01T00:00:00Z"},
		{".modernpath/demo/docs_push_manifest.json", "2026-09-20T17:23:11.123456+03:00"},
	} {
		file, err := w.Create(entry.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(`{"version":1,"generated_at":"` + entry.timestamp + `"}`)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := exportGeneratedAt(archive.Bytes(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got != "2026-09-20T14:23:11.123456Z" {
		t.Fatalf("generated timestamp = %q", got)
	}
}
