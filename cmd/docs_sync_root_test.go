package cmd

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func exportZip(t *testing.T, paths ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, p := range paths {
		f, err := w.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte("content")); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A sync run from a subdirectory extracts into the BOUND workspace. Anchoring
// on the invocation directory forked the workspace: the export landed under
// <cwd>/.modernpath, and the very next WriteConfig resolved to that
// just-created nested directory, leaving the root binding stale and the
// subtree shadowed by a copy with no credential and no sync time.
func TestSyncExtractLandsInTheBoundWorkspaceNotTheCwd(t *testing.T) {
	root := t.TempDir()
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(&config.Config{SystemID: 243}); err != nil {
		t.Fatal(err)
	}

	sub := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	zipData := exportZip(t, ".modernpath/my-app/architecture/overview.md")
	if err := extractZipIntoWorkspace(zipData); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(&config.Config{SystemID: 243, LastSyncAt: "2026-08-14T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(sub, ".modernpath")); err == nil {
		t.Fatal("syncing from a subdirectory forked a nested .modernpath")
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath", "my-app", "architecture", "overview.md")); err != nil {
		t.Fatalf("the export did not reach the bound workspace: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("2026-08-14")) {
		t.Fatalf("the root binding did not receive the sync stamp: %s", data)
	}
}

// Guard, expected green: `modernpath init` extracts into the directory being
// initialized, even inside an already-bound workspace — init means HERE.
func TestInitExtractStillLandsInTheCurrentDirectory(t *testing.T) {
	root := t.TempDir()
	old, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(&config.Config{SystemID: 243}); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "packages", "widget")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}

	zipData := exportZip(t, ".modernpath/my-app/architecture/overview.md")
	if err := extractZip(sub, zipData); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(sub, ".modernpath", "my-app", "architecture", "overview.md")); err != nil {
		t.Fatalf("init's extract must land where the user is standing: %v", err)
	}
}
