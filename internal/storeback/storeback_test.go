package storeback

import (
	"os"
	"path/filepath"
	"testing"
)

// TestActiveTracksTheMarker: Active is false with no marker and true once the
// tracked declaration file exists — the behavior every standing-down gate and
// the manifest yield rely on.
func TestActiveTracksTheMarker(t *testing.T) {
	root := t.TempDir()
	if Active(root) {
		t.Fatalf("Active(%q) = true with no marker; want false", root)
	}
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, MarkerRel), []byte("# Store-backed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Active(root) {
		t.Fatalf("Active(%q) = false with marker present; want true", root)
	}
}
