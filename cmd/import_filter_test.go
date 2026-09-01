package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// REQ-CROSS-172: `modernpath import --local` honours .gitignore and --exclude.
//
// Found on a real workspace (RUN:2026-08-15): import refused the
// directory at 203 MB against a 100 MB limit. 181 MB of that was a MySQL data
// volume — `ib_logfile0`, `ibdata1`, `undo001` — sitting in a submodule that
// gitignores the whole directory at .gitignore:66. Git already knew the files
// were junk; the importer could not ask.
//
// The skip list it used instead is name-and-extension based, so it is blind to
// exactly this shape: extensionless binaries in a directory nobody named. That
// is not a gap you close by appending another name — the next workspace has a
// different one. Honouring the file the repository already maintains is the
// general fix, and it took the payload to 18.3 MB.
//
// The failure direction matters: over-including does not error, it uploads a
// database and analyses it as source.

func gitInit(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGitIgnoredSet(t *testing.T) {
	t.Run("a gitignored file is reported, a tracked one is not", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		writeFile(t, filepath.Join(root, ".gitignore"), "data-volume\n")
		writeFile(t, filepath.Join(root, "src", "main.go"), "package main\n")
		writeFile(t, filepath.Join(root, "data-volume", "ib_logfile0"), "binary")

		got, err := gitIgnoredSet(root, []string{"src/main.go", "data-volume/ib_logfile0"})
		if err != nil {
			t.Fatal(err)
		}
		if got["src/main.go"] {
			t.Error("source file reported ignored")
		}
		if !got["data-volume/ib_logfile0"] {
			t.Error("gitignored file not reported — this is the field case")
		}
	})

	// The decisive one. That workspace is a superproject whose real code lives in
	// submodules, and the .gitignore that matters is the SUBMODULE's. Asking
	// only the outer repo returns nothing for these paths, because the outer
	// repo does not track inside a gitlink.
	t.Run("a nested repository's own .gitignore is honoured", func(t *testing.T) {
		root := t.TempDir()
		gitInit(t, root)
		inner := filepath.Join(root, "client-api")
		gitInit(t, inner)
		writeFile(t, filepath.Join(inner, ".gitignore"), "devtools/data-volume\n")
		writeFile(t, filepath.Join(inner, "src", "app.ts"), "export {};\n")
		writeFile(t, filepath.Join(inner, "devtools", "data-volume", "ibdata1"), "binary")

		got, err := gitIgnoredSet(root, []string{
			"client-api/src/app.ts",
			"client-api/devtools/data-volume/ibdata1",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got["client-api/src/app.ts"] {
			t.Error("submodule source reported ignored")
		}
		if !got["client-api/devtools/data-volume/ibdata1"] {
			t.Error("submodule's own .gitignore not consulted — 181 MB of MySQL ships")
		}
	})

	t.Run("a directory that is not a repository is not an error", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, "a.txt"), "x")
		got, err := gitIgnoredSet(root, []string{"a.txt"})
		if err != nil {
			t.Fatalf("plain directory must import, not fail: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %v, want nothing ignored", got)
		}
	})
}

func TestMatchesExclude(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rel      string
		patterns []string
		want     bool
	}{
		// app-web was 2.8 GB, untracked AND un-ignored — git cannot help, so
		// the flag has to. This is why --exclude exists alongside .gitignore.
		{"directory prefix", "app-web/notes/x.md", []string{"app-web"}, true},
		{"trailing slash is the same pattern", "app-web/x.md", []string{"app-web/"}, true},
		{"glob on the basename", "a/b/huge.bin", []string{"*.bin"}, true},
		{"glob on a path segment", "a/fixtures/x.json", []string{"*/fixtures/*"}, true},
		{"no pattern matches nothing", "src/main.go", []string{"app-web", "*.bin"}, false},
		{"empty pattern list", "src/main.go", nil, false},
		// A pattern must not match a name that merely starts with it, or
		// excluding "build" would drop "buildkite.yml".
		{"prefix is not a substring match", "buildkite.yml", []string{"build"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesExclude(tc.rel, tc.patterns); got != tc.want {
				t.Errorf("matchesExclude(%q, %v) = %v, want %v", tc.rel, tc.patterns, got, tc.want)
			}
		})
	}
}
