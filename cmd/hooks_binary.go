package cmd

// BACKLOG-TOOL-33 / -43: every hook called `modernpath` by bare name, so the
// hook shell's PATH decided which build ran — or whether one ran at all. In a
// harness whose shell lacks the install directory (Codex desktop, a Claude Code
// session started from a launcher), `command -v` failed and the SessionStart
// brief, the UserPromptSubmit context and the PreToolUse store-write guard all
// printed '{}' and never ran, exiting 0 the whole time.
//
// The hooks now prefer a workspace-local link, .modernpath/hooks/modernpath,
// that `modernpath hooks install` points at the binary that ran it — so the
// binary that installs the hooks is the binary the hooks run, whatever PATH
// the harness gives them. Without the link the command is what it was: the
// bare name, PATH resolves it, and a missing binary is still a silent no-op.
// `modernpath hooks doctor` names the link's target.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// hooksBinaryRel is the workspace-relative path of the link the hooks prefer.
const hooksBinaryRel = ".modernpath/hooks/modernpath"

// hookPrelude resolves the binary a hook runs into $MP: the workspace link when
// it is there and executable, else the bare name for PATH. CLAUDE_PROJECT_DIR
// is the harness's project root under Claude Code; other harnesses run hooks
// from the project directory, which $PWD names.
const hookPrelude = `MP="${CLAUDE_PROJECT_DIR:-$PWD}/` + hooksBinaryRel + `"; [ -x "$MP" ] || MP=modernpath; `

// hookCommand runs `modernpath <args>` through the prelude: `command -v` makes
// a missing binary a silent no-op and `|| <fallback>` guarantees the harness a
// well-formed response whatever happens — a hook must never cost a prompt.
func hookCommand(args, fallback string) string {
	return hookPrelude + `command -v "$MP" >/dev/null 2>&1 && "$MP" ` + args + ` 2>/dev/null || ` + fallback
}

// hookDetachedCommand is the fire-and-forget form the sync family uses: the
// CLI is detached and `tail` is what the harness sees, immediately.
func hookDetachedCommand(args, tail string) string {
	return hookPrelude + `command -v "$MP" >/dev/null 2>&1 && ( "$MP" ` + args + ` >/dev/null 2>&1 & ) ; ` + tail
}

// runningExecutable is the file this process runs from, symlinks resolved.
func runningExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		return resolved, nil
	}
	return exe, nil
}

// linkHooksBinary writes the workspace link (hooksBinaryRel under root) so it
// resolves to the running executable. An existing link is replaced; a regular file at that path is
// somebody's deliberate binary and is left alone, reported through `note`.
// Symlinks are not portable on Windows, where the hooks are POSIX shell
// anyway, so nothing is written there. Returns the link's target.
func linkHooksBinary(root string) (target, note string, err error) {
	if runtime.GOOS == "windows" {
		return "", "not written on Windows — the hooks resolve modernpath on PATH", nil
	}
	exe, err := runningExecutable()
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(root, hooksBinaryRel)
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink == 0 {
		return "", fmt.Sprintf("%s is a regular file, left as it is — the hooks run it", hooksBinaryRel), nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}
	if err := os.Symlink(exe, path); err != nil {
		return "", "", err
	}
	return exe, "", nil
}

// hooksBinaryTarget is the executable the workspace link resolves to, when a
// usable link exists — the binary the hooks in this workspace run.
func hooksBinaryTarget(root string) (string, bool) {
	path := filepath.Join(root, hooksBinaryRel)
	if _, err := os.Lstat(path); err != nil {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !executableExists(resolved) {
		return "", false
	}
	return resolved, true
}
