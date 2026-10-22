package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/modernpath/cli/internal/config"
)

// refuseNestedBinding refuses a command that would write a second workspace
// below one that is already bound (Epic 288 B5). `install` below a bound
// root wrote a second process package there, and `init` answered `unchanged`
// for the ancestor's binding; both now name the root instead.
//
// The nearest `.modernpath/config.json` at or above the current directory
// decides: in the directory itself, the command runs; in a strict ancestor,
// it is refused. With none above, the binding may still resolve through a
// linked git worktree's `.git` file to its main checkout (config.BindingDir),
// and that is refused the same way. A worktree root (a `.git` file in the
// directory itself) is exempt with allowWorktree — `install` passes there,
// its process files are tracked in the worktree — while `init` still refuses
// (`USER:2026-10-21` Q7: a worktree-local binding keeps its own refresh
// token, which dies when another copy rotates it). `init --force` is the
// deliberate nested binding and never calls this.
func refuseNestedBinding(allowWorktree bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	nearest := nearestBindingRoot(cwd)
	if nearest == cwd {
		return nil
	}
	worktreeRoot := hasGitFile(cwd)
	if worktreeRoot && allowWorktree {
		return nil
	}
	if nearest != "" {
		if worktreeRoot {
			return fmt.Errorf("%s is a linked git worktree below an already bound workspace at %s; run it from the main checkout, or run 'modernpath init --force' here to bind this worktree separately", cwd, nearest)
		}
		return fmt.Errorf("%s is below an already bound workspace at %s; run it from %s, or run 'modernpath init --force' here to bind a nested workspace deliberately", cwd, nearest, nearest)
	}
	// No binding above: a linked worktree outside its main checkout resolves
	// the binding through its .git file, from its root and from every
	// subdirectory.
	dir, err := config.BindingDir()
	if err != nil || dir == "" || filepath.Dir(dir) == cwd {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, config.ConfigFile)); err != nil {
		return nil
	}
	if worktreeRoot {
		return fmt.Errorf("%s is a linked git worktree of the workspace bound at %s; run it from the main checkout, or run 'modernpath init --force' here to bind this worktree separately", cwd, filepath.Dir(dir))
	}
	return fmt.Errorf("%s is inside a linked git worktree of the already bound workspace at %s; run it from the main checkout %s, or run 'modernpath init --force' at the worktree root to bind it separately", cwd, filepath.Dir(dir), filepath.Dir(dir))
}

// nearestBindingRoot is the closest directory at or above dir that holds
// .modernpath/config.json, or "" when none does. Unlike config.FindConfigDir
// it walks past a .modernpath without a binding (a worktree's tracked one).
func nearestBindingRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, config.ConfigDir, config.ConfigFile)); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// hasGitFile reports a linked git worktree root: .git is a file, not a directory.
func hasGitFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && !info.IsDir()
}
