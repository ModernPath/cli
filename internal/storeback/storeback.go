// Package storeback exposes the store-backed workspace marker predicate so
// packages that must not import cmd — internal/manifest, internal/gate,
// internal/rdd — can ask the same question the CLI commands do. It is a
// stdlib-only leaf: nothing under internal/ imports it back, so it introduces
// no import cycle.
package storeback

import (
	"os"
	"path/filepath"
)

// MarkerRel is the workspace-relative path of the flip's tracked declaration
// marker. Its presence is the whole signal (REQ-CROSS-225 §225.4): an absent
// ledger corpus is then the declared configuration, never an error.
const MarkerRel = "process/store-backed.md"

// Active reports whether the workspace at root carries the store-backed
// declaration marker. It mirrors the cmd-local storeBackedWorkspace predicate
// exactly — a bare stat, never a parse of the retired list.
func Active(root string) bool {
	_, err := os.Stat(filepath.Join(root, MarkerRel))
	return err == nil
}
