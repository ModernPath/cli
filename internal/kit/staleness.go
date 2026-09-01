package kit

import (
	"os"
	"path/filepath"
	"time"
)

// AssetsNewerThan reports whether the embedded assets a binary carries are older
// than the ones on disk — i.e. whether `install` would write the previous build's
// bytes (REQ-CROSS-146).
//
// Trivial by itself; it exists as a named function so the staleness rule has one
// definition and a test, rather than being a sentence in platform.md that the
// person who wrote it broke twice in two days.
func AssetsNewerThan(binaryTime, newestAsset time.Time) bool {
	return newestAsset.After(binaryTime)
}

// StaleAssetWarning returns a warning when the running binary predates the kit
// assets in this working tree, and "" otherwise — including in every workspace
// that does not carry the kit source, where the question is meaningless.
func StaleAssetWarning(root string) string {
	assets := filepath.Join(root, "modernpath-core/tools/modernpath/internal/kit/assets")
	if _, err := os.Stat(assets); err != nil {
		return "" // not the kit-owning workspace
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exeInfo, err := os.Stat(exe)
	if err != nil {
		return ""
	}

	var newest time.Time
	var name string
	_ = filepath.Walk(assets, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if info.ModTime().After(newest) {
			newest, name = info.ModTime(), p
		}
		return nil
	})
	if newest.IsZero() || !AssetsNewerThan(exeInfo.ModTime(), newest) {
		return ""
	}
	rel, _ := filepath.Rel(root, name)
	return "⚠ " + rel + " is newer than this binary — assets are compiled in, so install would write the previous build's bytes. Rebuild first."
}
