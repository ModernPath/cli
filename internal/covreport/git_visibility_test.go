// Covers REQ-CROSS-179 — the source inventory is what git sees, so an ignored
// build directory cannot dilute the coverage denominator.
package covreport

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// The coverage denominator is the repository's source, and git is what decides
// what that is.
//
// RUN:2026-08-24 — a real workspace carried a gitignored `test-runs/` lab
// directory holding a vendored Python package: 2481 files, 61% of the
// denominator, none of them TypeScript in a TypeScript project. Coverage read
// 31.2% while the tracked source was 84.6%. A number that moves because someone
// ran a local experiment is not a measurement.
func TestWalkSourceFilesCountsOnlyWhatGitSees(t *testing.T) {
	root := t.TempDir()
	write := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(".gitignore", "vendored/\n")
	write("lib/real.ts", "export const a = 1")
	write("vendored/ignored.ts", "export const b = 2")

	for _, args := range [][]string{
		{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
		{"add", "."}, {"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("git unavailable: %v %s", err, out)
		}
	}

	got := walkSourceFiles(root, root)

	var sawReal, sawIgnored bool
	for _, f := range got {
		switch f {
		case "lib/real.ts":
			sawReal = true
		case "vendored/ignored.ts":
			sawIgnored = true
		}
	}
	if !sawReal {
		t.Errorf("tracked source missing from the denominator; got %v", got)
	}
	if sawIgnored {
		t.Errorf("gitignored file counted as source; got %v", got)
	}
	if n := GitIgnoredCount(root); n != 1 {
		t.Errorf("GitIgnoredCount = %d, want 1 — an exclusion nobody can see is a different way to be wrong", n)
	}
}

// Outside a repository there is nothing to ask, so counting everything is the
// only honest fallback — and it is the behaviour that existed before.
func TestWalkSourceFilesCountsEverythingWithoutGit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "loose.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := walkSourceFiles(root, root)
	if len(got) != 1 || got[0] != "loose.ts" {
		t.Errorf("non-repo walk = %v, want [loose.ts]", got)
	}
}

// A container workspace holds several repositories rather than being one
// itself, so `git ls-files` at the top answers for nothing. Counting every file
// on disk there is the same failure the single-repo case had: vendored and
// generated trees inside each child would silently join the denominator.
func TestWalkSourceFilesUnionsChildRepositories(t *testing.T) {
	root := t.TempDir()
	mk := func(rel, body string) {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	initRepo := func(dir string) {
		for _, args := range [][]string{
			{"init"}, {"config", "user.email", "t@example.com"}, {"config", "user.name", "t"},
			{"add", "."}, {"commit", "-m", "init"},
		} {
			cmd := exec.Command("git", args...)
			cmd.Dir = filepath.Join(root, dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Skipf("git unavailable: %v %s", err, out)
			}
		}
	}

	// Two child repositories; the container itself is not one.
	mk("svc-a/.gitignore", "vendor/\n")
	mk("svc-a/src/real.ts", "export const a = 1")
	mk("svc-a/vendor/ignored.ts", "export const b = 2")
	mk("svc-b/lib/also-real.ts", "export const c = 3")
	initRepo("svc-a")
	initRepo("svc-b")

	got := map[string]bool{}
	for _, f := range walkSourceFiles(root, root) {
		got[f] = true
	}

	for _, want := range []string{"svc-a/src/real.ts", "svc-b/lib/also-real.ts"} {
		if !got[want] {
			t.Errorf("missing %s from the container denominator; got %v", want, got)
		}
	}
	if got["svc-a/vendor/ignored.ts"] {
		t.Errorf("a child repository's ignored file counted as source; got %v", got)
	}
}
