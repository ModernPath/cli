// Package kit installs the requirement-driven process into a repository
// (REQ-CROSS-029).
//
// Everything the installer writes is tool-owned and replaced wholesale on
// upgrade — except one file. `AGENTS.md` belongs to the client (their stack,
// commands, architecture) and must also carry the block that points non-Claude
// agents at the process, because Codex reads AGENTS.md and does not read
// CLAUDE.md (measured RUN:2026-08-08: Codex 6/6 via AGENTS.md, 0 via CLAUDE.md).
//
// That shared file is the only seam where an upgrade could destroy work the
// client did, so the merge below is deliberately conservative: it rewrites only
// what lies between its own markers, and refuses outright when the markers are
// damaged rather than guessing where the boundary was.
package kit

import (
	"errors"
	"strings"
)

const (
	// BeginMarker/EndMarker delimit the region the installer owns. They are HTML
	// comments so they render as nothing in every Markdown viewer, and Claude
	// Code strips block-level comments before the file enters context — so the
	// markers cost no tokens in the sessions that load AGENTS.md.
	BeginMarker = "<!-- BEGIN modernpath-rdd (managed — do not edit; changes are overwritten) -->"
	EndMarker   = "<!-- END modernpath-rdd -->"
)

// ErrDamagedMarkers reports a document whose managed region cannot be located
// unambiguously: one marker without its partner, or an end before its begin.
var ErrDamagedMarkers = errors.New(
	"managed block markers are damaged (unpaired or out of order) — fix them by hand; " +
		"refusing to rewrite in case client content would be lost")

// MergeManagedBlock returns doc with the managed region set to block.
//
// When the region already exists it is replaced in place and every byte outside
// it is preserved. When it does not, the block is inserted after the document's
// title so the process is the first thing an agent reads, ahead of the client's
// own sections. Applying the same block twice is a no-op.
func MergeManagedBlock(doc, block string) (string, error) {
	block = strings.Trim(block, "\n")
	managed := BeginMarker + "\n" + block + "\n" + EndMarker

	begin := strings.Index(doc, BeginMarker)
	end := strings.Index(doc, EndMarker)
	switch {
	case begin >= 0 && end < 0, begin < 0 && end >= 0, begin >= 0 && end < begin:
		return "", ErrDamagedMarkers
	case begin >= 0:
		return doc[:begin] + managed + doc[end+len(EndMarker):], nil
	}

	if strings.TrimSpace(doc) == "" {
		return managed + "\n", nil
	}

	// Insert after the title (and the blank line following it) when there is
	// one; otherwise the block leads the file.
	lines := strings.SplitN(doc, "\n", 2)
	if strings.HasPrefix(lines[0], "# ") && len(lines) == 2 {
		rest := strings.TrimLeft(lines[1], "\n")
		return lines[0] + "\n\n" + managed + "\n\n" + rest, nil
	}
	return managed + "\n\n" + strings.TrimLeft(doc, "\n"), nil
}

// HasManagedBlock reports whether doc already carries a well-formed managed
// region. Used to tell "first install" from "upgrade" when reporting to the user.
func HasManagedBlock(doc string) bool {
	begin := strings.Index(doc, BeginMarker)
	end := strings.Index(doc, EndMarker)
	return begin >= 0 && end > begin
}
