// REQ-CROSS-029: `modernpath install --check`, so process drift is visible.
package cmd

// Dry-run promised "managed block added … your
// lines kept" on a repository whose markers were damaged — the exact upgrade
// its docstring says it exists to vet — and then the real install aborted.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/kit"
)

func TestDryRunSurfacesDamagedMarkersAsTheComingAbort(t *testing.T) {
	root := t.TempDir()
	damaged := "# acme\n\n" + kit.BeginMarker + "\nhalf a block with no end marker\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(damaged), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := reportPlan(root); err == nil {
		t.Fatal("dry-run must fail on a repository the real install will refuse")
	}
}

func TestDryRunStaysQuietOnAHealthyRepository(t *testing.T) {
	root := t.TempDir()
	if _, err := kit.Install(root); err != nil {
		t.Fatal(err)
	}
	if err := reportPlan(root); err != nil {
		t.Fatalf("dry-run over a healthy install must succeed: %v", err)
	}
}
