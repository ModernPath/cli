package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-146: in the workspace that OWNS the kit, an installed file and its
// embedded source must be byte-identical. installTargets are "replaced wholesale
// on upgrade", so editing .claude/rdd/platform.md without mirroring it into
// assets/ means the next `modernpath install` silently deletes the edit — and on
// RUN:2026-08-14 that had already happened to two files, one of them the only
// documentation of a shipped feature.
//
// Skips anywhere else: another checkout legitimately has its own installed copies.
func TestInstalledFilesMatchTheirSource(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("not the kit-owning workspace")
	}
	compared := 0
	for asset, target := range installTargets {
		if _, isMerge := mergeTargets[asset]; isMerge {
			continue // the client owns everything outside the marked block
		}
		want, err := assets.ReadFile(asset)
		if err != nil {
			t.Errorf("%s: not embedded: %v", asset, err)
			continue
		}
		got, err := os.ReadFile(filepath.Join(root, target))
		if os.IsNotExist(err) {
			continue // not installed here
		}
		if err != nil {
			t.Errorf("%s: %v", target, err)
			continue
		}
		compared++
		if !bytes.Equal(want, got) {
			t.Errorf("%s has drifted from %s — mirror the edit into assets/, or the next install deletes it", target, asset)
		}
	}
	// Every not-installed file is skipped by the continue above, so a workspace
	// with none of them compares nothing and passes. That is the same shape as
	// the retired marker that silenced this whole test — say so instead.
	if compared == 0 {
		t.Fatal("no installed file was compared — this guard passed without checking anything")
	}
	t.Logf("compared %d installed files against their embedded source", compared)
}

// repoRoot walks up for the marker that identifies the kit-owning workspace.
func repoRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 8; i++ {
		// RUN:2026-09-01: this looked for .claude/rdd/PROCESS.md, the pre-
		// consolidation location. When the process moved to .modernpath/rdd the
		// marker vanished, repoRoot returned "" everywhere, and both guards that
		// depend on it — this one and TestStaleBinaryIsDetectable — skipped in
		// EVERY checkout including the kit-owning one. `go test` prints ok for a
		// skip, so a guard written to catch drift that "had already happened to
		// two files" stopped running and nothing said so.
		if _, err := os.Stat(filepath.Join(dir, ".modernpath", "rdd", "PROCESS.md")); err == nil {
			if b, err := os.ReadFile(filepath.Join(dir, "AGENTS.md")); err == nil &&
				strings.Contains(string(b), "modernpath-core/") {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
