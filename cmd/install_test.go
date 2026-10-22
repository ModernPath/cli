package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/config"
)

// Epic 288 (B5): an accidental nested workspace. `install` below a bound
// workspace wrote a second process package there, and `init` answered
// `unchanged` for the ancestor's binding. Both now refuse and name the root;
// `init --force` stays the deliberate nested binding, and a linked worktree is
// exempt for `install` only (its process files are tracked, its binding is the
// main checkout's).

// boundRootAndSub binds a temp root to System 243 and enters root/packages/widget.
func boundRootAndSub(t *testing.T) (root, sub string) {
	t.Helper()
	root = chdirTemp(t)
	if err := config.WriteConfig(&config.Config{SystemID: 243, SystemName: "Bound"}); err != nil {
		t.Fatal(err)
	}
	sub = filepath.Join(root, "packages", "widget")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	return root, sub
}

func resetInstallFlags(t *testing.T) {
	t.Helper()
	savedCheck, savedDry, savedJSON := installCheck, installDryRun, installJSON
	installCheck, installDryRun, installJSON = false, false, false
	t.Cleanup(func() { installCheck, installDryRun, installJSON = savedCheck, savedDry, savedJSON })
}

func TestInstallRefusesInsideABoundWorkspace(t *testing.T) {
	root, sub := boundRootAndSub(t)
	resetInstallFlags(t)
	resolvedRoot, _ := filepath.EvalSymlinks(root)

	err := installCmd.RunE(installCmd, nil)
	if err == nil {
		t.Fatal("install below a bound workspace must be refused")
	}
	if !strings.Contains(err.Error(), resolvedRoot) || !strings.Contains(err.Error(), "already bound workspace") || !strings.Contains(err.Error(), "run it from") {
		t.Fatalf("the refusal must name the bound root %s and say to run it from there, got: %v", resolvedRoot, err)
	}
	for _, written := range []string{".modernpath", "CLAUDE.md", "AGENTS.md", ".claude"} {
		if _, statErr := os.Stat(filepath.Join(sub, written)); !os.IsNotExist(statErr) {
			t.Fatalf("a refused install must write nothing, found %s", written)
		}
	}

	// --check and --dry-run do not write, so they are never refused.
	installCheck = true
	if err := installCmd.RunE(installCmd, nil); err != nil && strings.Contains(err.Error(), "already bound") {
		t.Fatalf("install --check must not be refused below a binding: %v", err)
	}
	installCheck, installDryRun = false, true
	if out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) }); err != nil {
		t.Fatalf("install --dry-run must not be refused below a binding: %v\n%s", err, out)
	}
}

func TestInitRefusesInsideABoundWorkspaceWithoutForce(t *testing.T) {
	srv := newInitFakeServer(t)
	root := initTestSetup(t, srv)
	writeTestFile(t, root, ".modernpath/config.json", `{"api_url":"`+srv.URL+`","system_id":243,"system_name":"Bound"}`)
	writeInitAuth(t, root, initGoodToken, time.Now().Add(time.Hour))
	sub := filepath.Join(root, "packages", "widget")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	resolvedRoot, _ := filepath.EvalSymlinks(root)

	for name, run := range map[string]func() error{
		"init":           func() error { return initCmd.RunE(initCmd, nil) },
		"init workspace": func() error { return initWorkspaceCmd.RunE(initWorkspaceCmd, nil) },
	} {
		out, err := runCapturing(t, run)
		if err == nil {
			t.Fatalf("%s below a bound workspace must be refused, not answered:\n%s", name, out)
		}
		if strings.Contains(out, "unchanged") {
			t.Fatalf("%s must not report the ancestor binding as unchanged:\n%s", name, out)
		}
		if !strings.Contains(err.Error(), resolvedRoot) || !strings.Contains(err.Error(), "--force") {
			t.Fatalf("%s must name the bound root and the deliberate way through, got: %v", name, err)
		}
		if n := srv.requestCount(); n != 0 {
			t.Fatalf("%s sent %d request(s) before the refusal, want none", name, n)
		}
		if _, statErr := os.Stat(filepath.Join(sub, ".modernpath")); !os.IsNotExist(statErr) {
			t.Fatalf("%s must write nothing when refused", name)
		}
	}

	// `init --force` is the deliberate nested binding (credential carry case).
	force = true
	out, err := runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil {
		t.Fatalf("init --force below a binding is the deliberate nested case: %v\n%s", err, out)
	}
	if got := boundSystemID(t, sub); got != 1 {
		t.Fatalf("init --force must bind the current directory, got system %d", got)
	}
	// The ancestor stays bound, so this is a new binding, not a rebind of it.
	if !strings.Contains(out, "operation: initialized") || strings.Contains(out, "rebound") {
		t.Errorf("a nested init --force is initialized, not a rebind of the ancestor:\n%s", out)
	}

	// Once the directory carries its own binding, install and init run there.
	force = false
	resetInstallFlags(t)
	if out, err := runCapturing(t, func() error { return installCmd.RunE(installCmd, nil) }); err != nil {
		t.Errorf("install in a directory with its own binding must pass: %v\n%s", err, out)
	}
	systemIDFlag = 1
	out, err = runCapturing(t, func() error { return initCmd.RunE(initCmd, nil) })
	if err != nil || !strings.Contains(out, "unchanged") {
		t.Errorf("init --system-id in a directory with its own binding must pass: %v\n%s", err, out)
	}
}

func TestNestedBindingGuardExemptions(t *testing.T) {
	t.Run("a directory with its own config.json passes for every command", func(t *testing.T) {
		root := chdirTemp(t)
		if err := config.WriteConfig(&config.Config{SystemID: 243}); err != nil {
			t.Fatal(err)
		}
		sub := filepath.Join(root, "nested")
		if err := os.MkdirAll(filepath.Join(sub, ".modernpath"), 0o755); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, sub, ".modernpath/config.json", `{"system_id":9}`)
		if err := os.Chdir(sub); err != nil {
			t.Fatal(err)
		}
		for _, allowWorktree := range []bool{false, true} {
			if err := refuseNestedBinding(allowWorktree); err != nil {
				t.Fatalf("own binding, allowWorktree=%v: %v", allowWorktree, err)
			}
		}
	})

	t.Run("a .git file exempts install only", func(t *testing.T) {
		_, sub := boundRootAndSub(t)
		if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: /nowhere/.git/worktrees/x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := refuseNestedBinding(true); err != nil {
			t.Fatalf("install at a worktree root below a binding must pass: %v", err)
		}
		if err := refuseNestedBinding(false); err == nil {
			t.Fatal("init at a worktree root below a binding must be refused without --force")
		}
	})

	t.Run("a linked worktree of a bound main checkout", func(t *testing.T) {
		main := chdirTemp(t)
		gitDir := filepath.Join(main, ".git", "worktrees", "wt")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := config.WriteConfig(&config.Config{SystemID: 3}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wt := filepath.Join(t.TempDir(), "wt")
		if err := os.MkdirAll(filepath.Join(wt, config.ConfigDir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(wt); err != nil {
			t.Fatal(err)
		}
		if err := refuseNestedBinding(true); err != nil {
			t.Fatalf("install in a linked worktree must pass: %v", err)
		}
		err := refuseNestedBinding(false)
		if err == nil {
			t.Fatal("init at a linked worktree root must be refused without --force")
		}
		if !strings.Contains(err.Error(), "main checkout") {
			t.Fatalf("the refusal must send the user to the main checkout, got: %v", err)
		}
	})

	// A subdirectory of a worktree that lives outside its main checkout has no
	// config.json above it and no .git file of its own, yet the binding still
	// resolves to the main checkout: both commands are refused there.
	t.Run("a subdirectory of a linked worktree outside the main checkout", func(t *testing.T) {
		main := chdirTemp(t)
		gitDir := filepath.Join(main, ".git", "worktrees", "wt")
		if err := os.MkdirAll(gitDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := config.WriteConfig(&config.Config{SystemID: 3}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		wt := filepath.Join(t.TempDir(), "wt")
		sub := filepath.Join(wt, "apps", "web")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(sub); err != nil {
			t.Fatal(err)
		}
		resolvedMain, _ := filepath.EvalSymlinks(main)
		for _, allowWorktree := range []bool{true, false} {
			err := refuseNestedBinding(allowWorktree)
			if err == nil {
				t.Fatalf("allowWorktree=%v: a worktree subdirectory bound through the main checkout must be refused", allowWorktree)
			}
			// The root is named as the .git file spells it (symlinks may or may not be resolved).
			if !(strings.Contains(err.Error(), resolvedMain) || strings.Contains(err.Error(), main)) || !strings.Contains(err.Error(), "run it from") {
				t.Fatalf("allowWorktree=%v: the refusal must name the bound root %s, got: %v", allowWorktree, resolvedMain, err)
			}
		}
	})
}

// Epic 288 (B9): a bound workspace that was never synced has no export for the
// context hook or the knowledge search to read; `install` is the moment a
// fresh checkout is set up, so it says so.
func TestInstallHintsDocsSyncOnAnUnsyncedSystem(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// An unbound directory has no System to sync: no hint.
	chdirTemp(t)
	if _, stderr, err := runAskStreams(t, "install"); err != nil || strings.Contains(stderr, "modernpath docs sync") {
		t.Errorf("install in an unbound directory gets no sync hint: %v\n%s", err, stderr)
	}

	root := chdirTemp(t)
	writeTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://127.0.0.1:1","system_id":7,"system_name":"Seven","system_slug":"seven"}`)

	_, stderr, err := runAskStreams(t, "install")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "modernpath docs sync") {
		t.Fatalf("install on a never-synced binding must hint the sync, stderr:\n%s", stderr)
	}

	// --check and --dry-run write nothing and hint nothing.
	for _, flag := range []string{"--check", "--dry-run"} {
		if _, stderr, err := runAskStreams(t, "install", flag); err != nil || strings.Contains(stderr, "modernpath docs sync") {
			t.Errorf("install %s gets no sync hint: %v\n%s", flag, err, stderr)
		}
	}

	// A rebind can retain the previous System's export and sync timestamp.
	// That export does not satisfy the new binding.
	writeTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://127.0.0.1:1","system_id":7,"system_name":"Seven","system_slug":"seven","last_sync_at":"2026-10-20T10:00:00Z"}`)
	writeTestFile(t, root, ".modernpath/previous/docs_push_manifest.json", `{"version":1}`)
	_, stderr, err = runAskStreams(t, "install")
	if err != nil {
		t.Fatalf("install with an old System export: %v\n%s", err, stderr)
	}
	if !strings.Contains(stderr, "modernpath docs sync") {
		t.Fatalf("the bound System has no export; install must hint the sync, stderr:\n%s", stderr)
	}

	// A synced workspace with its export in place gets no hint.
	writeTestFile(t, root, ".modernpath/seven/docs_push_manifest.json", `{"version":1}`)
	_, stderr, err = runAskStreams(t, "install")
	if err != nil {
		t.Fatalf("install: %v\n%s", err, stderr)
	}
	if strings.Contains(stderr, "modernpath docs sync") {
		t.Fatalf("a synced workspace gets no sync hint, stderr:\n%s", stderr)
	}

	if err := os.RemoveAll(filepath.Join(root, ".modernpath", "seven")); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, ".modernpath/docs/seven/docs_push_manifest.json", `{"version":1}`)
	_, stderr, err = runAskStreams(t, "install")
	if err != nil || strings.Contains(stderr, "modernpath docs sync") {
		t.Fatalf("a synced legacy export gets no sync hint: %v\n%s", err, stderr)
	}
}
