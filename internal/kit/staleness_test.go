package kit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// REQ-CROSS-103b: assets are go:embed'ed, so editing one and running `install`
// writes the PREVIOUS build's bytes. Nothing errors and nothing is skipped — the
// write loop is unconditional — so the only signal is silence.
//
// Documented in platform.md ("rebuild, then install, then diff") and broken twice
// in two days by the person who wrote it. A rule nobody applies at the moment of
// action needs a mechanism, not a louder warning.
func TestStaleBinaryIsDetectable(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("not the kit-owning workspace")
	}
	newest, name := newestAssetTime(t, filepath.Join(root, "modernpath-core/tools/modernpath/internal/kit/assets"))
	if newest.IsZero() {
		t.Skip("assets not on disk")
	}
	// The function under test: given a binary time and the newest asset time,
	// does it report staleness correctly?
	if !AssetsNewerThan(newest.Add(-time.Minute), newest) {
		t.Errorf("a binary older than %s must be reported stale", name)
	}
	if AssetsNewerThan(newest.Add(time.Minute), newest) {
		t.Error("a binary newer than every asset must not be reported stale")
	}
}

func newestAssetTime(t *testing.T, dir string) (time.Time, string) {
	t.Helper()
	var newest time.Time
	var name string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().After(newest) {
			newest, name = info.ModTime(), filepath.Base(p)
		}
		return nil
	})
	return newest, name
}
