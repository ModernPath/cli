package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path"
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

// matchesExclude reports whether rel matches any operator-supplied pattern
// (REQ-CROSS-501 AC2, D5), the way --exclude's help describes it.
//
// A pattern without "/" matches any one segment of the path, so it names a
// file or directory at any depth: a plain name ("app-web" — one real estate
// carried 2.8 GB under such a directory, untracked and un-ignored, so git
// cannot help) or a glob ("*.min.js"). It matches whole segments only:
// excluding "build" must not drop "buildkite.yml".
//
// A pattern with "/" is a path glob anchored at the top of the tree: "*"
// stays within one segment and "**" spans any number of them. It matches the
// path or a directory on it, which leaves out everything under that
// directory. A leading "/" anchors a single name ("/build").
func matchesExclude(rel string, patterns []string) bool {
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for _, p := range patterns {
		anchored, p := excludePattern(p)
		if p == "" {
			continue
		}
		if !anchored {
			for _, segment := range segments {
				if ok, err := path.Match(p, segment); err == nil && ok {
					return true
				}
			}
			continue
		}
		pattern := strings.Split(p, "/")
		for n := 1; n <= len(segments); n++ {
			if matchSegments(pattern, segments[:n]) {
				return true
			}
		}
	}
	return false
}

// excludePattern normalises one --exclude pattern the way matchesExclude
// reads it: slash separated, without a leading "./" or surrounding slashes,
// and anchored when a "/" remains inside it.
func excludePattern(p string) (anchored bool, pattern string) {
	p = filepath.ToSlash(strings.TrimSpace(p))
	anchored = strings.Contains(strings.TrimSuffix(p, "/"), "/")
	return anchored, strings.Trim(strings.TrimPrefix(p, "./"), "/")
}

// checkExcludePatterns refuses a malformed --exclude pattern before the scan
// (REQ-CROSS-501): path.Match fails on one such as "[", so it would match
// nothing and upload what the operator meant to leave out.
func checkExcludePatterns(patterns []string) error {
	for _, raw := range patterns {
		_, p := excludePattern(raw)
		for _, segment := range strings.Split(p, "/") {
			if _, err := path.Match(segment, ""); err != nil {
				return fmt.Errorf("--exclude %q is not a valid pattern (%v); nothing was uploaded", raw, err)
			}
		}
	}
	return nil
}

// matchSegments matches a path segment by segment: "**" matches zero or more
// segments, any other pattern segment exactly one by path.Match. Go's own
// matchers have no "**" (D5: no dependency for one function).
func matchSegments(pattern, segments []string) bool {
	for len(pattern) > 0 {
		if pattern[0] == "**" {
			for skip := 0; skip <= len(segments); skip++ {
				if matchSegments(pattern[1:], segments[skip:]) {
					return true
				}
			}
			return false
		}
		if len(segments) == 0 {
			return false
		}
		if ok, err := path.Match(pattern[0], segments[0]); err != nil || !ok {
			return false
		}
		pattern, segments = pattern[1:], segments[1:]
	}
	return len(segments) == 0
}
