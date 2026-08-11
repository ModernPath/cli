package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EPIC-CTX-001 (`USER:2026-08-11`): a second, older `modernpath` earlier in PATH
// silently downgrades every hook run while exiting 0. This is the check that
// makes it visible, so it is tested on the ordering the shell actually uses.
func TestPathCopiesReportsShadowedBinariesInShellOrder(t *testing.T) {
	present := map[string]bool{
		"/Users/x/.local/bin/modernpath": true,
		"/opt/homebrew/bin/modernpath":   true,
	}
	exists := func(p string) bool { return present[p] }

	path := strings.Join([]string{"/Users/x/.local/bin", "/usr/bin", "/opt/homebrew/bin"}, string(os.PathListSeparator))
	got := pathCopies("modernpath", path, exists)

	if len(got) != 2 {
		t.Fatalf("want both copies, got %v", got)
	}
	if got[0] != "/Users/x/.local/bin/modernpath" {
		t.Fatalf("the winner must be the first PATH entry, got %s", got[0])
	}
}

func TestPathCopiesIgnoresEmptyEntriesAndDuplicates(t *testing.T) {
	present := map[string]bool{"/opt/homebrew/bin/modernpath": true}
	exists := func(p string) bool { return present[p] }

	path := strings.Join([]string{"", "/opt/homebrew/bin", "/opt/homebrew/bin", "/nowhere"}, string(os.PathListSeparator))
	got := pathCopies("modernpath", path, exists)

	if len(got) != 1 {
		t.Fatalf("a duplicated PATH entry is still one binary: %v", got)
	}
}

func TestPathCopiesFindsNothingWhenTheCLIIsAbsent(t *testing.T) {
	got := pathCopies("modernpath", "/usr/bin"+string(os.PathListSeparator)+"/bin", func(string) bool { return false })
	if len(got) != 0 {
		t.Fatalf("want none, got %v", got)
	}
}

// A directory named `modernpath`, or a non-executable file, is not a CLI.
func TestExecutableExistsRejectsDirectoriesAndPlainFiles(t *testing.T) {
	dir := chdirTemp(t)

	if err := os.Mkdir(filepath.Join(dir, "modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	if executableExists(filepath.Join(dir, "modernpath")) {
		t.Fatal("a directory is not a binary")
	}

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if executableExists(plain) {
		t.Fatal("a non-executable file is not a binary")
	}
}
