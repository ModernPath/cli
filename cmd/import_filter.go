package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// REQ-CROSS-172: what an upload import leaves out.
//
// The static skip list in import.go answers "is this name one we know about".
// These two answer the questions it cannot: "does the repository already call
// this junk", and "did the operator name something git has no opinion on".

// gitIgnoredSet returns the subset of rels — paths relative to sourceDir, slash
// separated — that git considers ignored.
//
// Each path is asked of its NEAREST enclosing repository, not of sourceDir.
// A superproject does not track inside a gitlink, so asking the outer repo about
// a submodule's files returns nothing and the submodule's own .gitignore — the
// one holding the 96 MB `ib_logfile0` rule — is never read.
//
// A directory that is not a repository is not an error: it has no opinion, and
// import must still work there.
func gitIgnoredSet(sourceDir string, rels []string) (map[string]bool, error) {
	ignored := make(map[string]bool)

	byRepo := make(map[string][]string)
	for _, rel := range rels {
		if repo := nearestRepo(sourceDir, rel); repo != "" {
			byRepo[repo] = append(byRepo[repo], rel)
		}
	}

	for repo, group := range byRepo {
		// Ask in chunks: the argument list is avoided by using --stdin, but the
		// buffers are not, and a monorepo walk can be six figures of paths.
		const chunk = 2000
		for i := 0; i < len(group); i += chunk {
			end := i + chunk
			if end > len(group) {
				end = len(group)
			}
			batch := group[i:end]

			// Paths must be relative to the repo being asked, not to sourceDir.
			input := make([]string, 0, len(batch))
			back := make(map[string]string, len(batch))
			for _, rel := range batch {
				abs := filepath.Join(sourceDir, filepath.FromSlash(rel))
				r, err := filepath.Rel(repo, abs)
				if err != nil {
					continue
				}
				r = filepath.ToSlash(r)
				input = append(input, r)
				back[r] = rel
			}
			if len(input) == 0 {
				continue
			}

			// -z on both sides: a path may contain a newline, and a filter that
			// mis-parses one silently keeps a file it was told to drop.
			cmd := exec.Command("git", "-C", repo, "check-ignore", "-z", "--stdin")
			cmd.Stdin = strings.NewReader(strings.Join(input, "\x00") + "\x00")
			out, err := cmd.Output()
			if err != nil {
				// Exit 1 means "none of these are ignored" — the ordinary case,
				// not a failure. Anything else means git could not answer, and
				// we fall back to including the files rather than guessing.
				if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
					continue
				}
			}
			for _, r := range strings.Split(string(out), "\x00") {
				if r == "" {
					continue
				}
				if rel, ok := back[r]; ok {
					ignored[rel] = true
				}
			}
		}
	}
	return ignored, nil
}

// nearestRepo walks up from rel's directory to sourceDir and returns the first
// directory holding a .git entry. A submodule's .git is a FILE, not a
// directory, so this tests for existence rather than for a directory.
func nearestRepo(sourceDir, rel string) string {
	dir := filepath.Dir(filepath.Join(sourceDir, filepath.FromSlash(rel)))
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		if dir == sourceDir || len(dir) <= len(sourceDir) {
			return ""
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// matchesExclude reports whether rel matches any operator-supplied pattern.
//
// Three shapes, because operators reach for all three: a plain directory name
// ("app-web" — one real estate carried 2.8 GB under such a directory,
// untracked and un-ignored, so git cannot help), a glob on the basename
// ("*.bin"), and a glob on the path ("*/fixtures/*").
//
// A bare name matches a whole subtree but NOT a longer name: excluding "build"
// must not drop "buildkite.yml".
func matchesExclude(rel string, patterns []string) bool {
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		p = strings.TrimSuffix(strings.TrimSpace(p), "/")
		if p == "" {
			continue
		}
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return true
		}
		if ok, err := filepath.Match(p, rel); err == nil && ok {
			return true
		}
		if ok, err := filepath.Match(p, filepath.Base(rel)); err == nil && ok {
			return true
		}
	}
	return false
}
