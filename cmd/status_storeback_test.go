package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B1: process next and the SessionStart brief disclose the store-backed mode
// (and only then) so a session is told which half of the process model it is in.
func TestStoreBackedNotePresentUnderMarkerOnly(t *testing.T) {
	root := t.TempDir()
	if storeBackedNote(root) != "" {
		t.Fatalf("file-backed: note must be empty, got %q", storeBackedNote(root))
	}
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := storeBackedNote(root)
	if !strings.Contains(note, "working-set pull") || !strings.Contains(note, "author") {
		t.Fatalf("under a marker the note must name pull+author, got %q", note)
	}
}

// B7: after the flip `modernpath status` must not tell the reader to run
// `factory sync` — the bulk process-record channel is refused (REQ-CROSS-227).
func TestStateSyncLineStoreBackedDoesNotSayFactorySync(t *testing.T) {
	line := renderStateSyncLine("", true)
	if strings.Contains(line, "factory sync") {
		t.Fatalf("store-backed State Sync line must not mention factory sync, got %q", line)
	}
	if !strings.Contains(line, "store-backed") {
		t.Fatalf("store-backed State Sync line must say so, got %q", line)
	}
}

// File-backed is unchanged: the sync lane is still named.
func TestStateSyncLineFileBackedNamesFactorySync(t *testing.T) {
	line := renderStateSyncLine("", false)
	if !strings.Contains(line, "factory sync") {
		t.Fatalf("file-backed State Sync line must still name factory sync, got %q", line)
	}
}

// B7/B1: the store-backed disclosure names the read and write paths that
// replace factory sync — working-set pull and author.
func TestStoreBackedReadWriteNamesPullAndAuthor(t *testing.T) {
	s := storeBackedReadWrite()
	if !strings.Contains(s, "working-set pull") || !strings.Contains(s, "author") {
		t.Fatalf("storeBackedReadWrite must name working-set pull and author, got %q", s)
	}
}
