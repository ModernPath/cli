//go:build unix

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// The scan packs regular files only. A symbolic link or a named pipe is left
// out and counted in the report: zipping opens a path through its link, so a
// link to a directory stopped the upload ("is a directory"), a link to a file
// packed the bytes of whatever it pointed at, even outside the source
// directory, and a pipe would block the zip.
func TestScanLeavesOutLinksAndSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, smallTree())
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{
		"src/include-app": "app",
		"README.link":     "README.md",
		"outside.txt":     outside,
	} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}

	files, report, err := collectImportFiles(dir, importFilterOptions{NoGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	var packed []string
	for _, f := range files {
		packed = append(packed, f.rel)
	}
	slices.Sort(packed)
	if want := []string{"README.md", "src/app/Program.cs"}; !slices.Equal(packed, want) {
		t.Fatalf("only the regular files must be packed, got %v, want %v", packed, want)
	}

	var zipped bytes.Buffer
	if err := writeZip(&zipped, files); err != nil {
		t.Fatalf("the zip must be written: %v", err)
	}

	var out bytes.Buffer
	printImportReport(&out, report)
	if line := reportLine(out.String(), "Skipped (links and special files)", "4 files"); line == "" {
		t.Errorf("the report must count the three links and the pipe:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Files: 6") {
		t.Errorf("links and pipes must not be counted as packed files:\n%s", out.String())
	}
}
