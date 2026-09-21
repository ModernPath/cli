package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-337 — the credential file carries the workspace the token was
// issued for and the issuer that issued it, omits them when empty, and a
// file written before they existed still reads.

func TestAuthWorkspaceFieldsRoundTrip(t *testing.T) {
	chdirTemp(t)
	in := &Auth{Token: "t0ken", RefreshToken: "r", WorkspaceID: "org-b", WorkspaceName: "Beta", Issuer: "https://id.example.test"}
	if err := WriteAuth(in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if out.WorkspaceID != in.WorkspaceID || out.WorkspaceName != in.WorkspaceName || out.Issuer != in.Issuer {
		t.Fatalf("read back %+v, want %+v", out, in)
	}
	raw, _ := os.ReadFile(filepath.Join(ConfigDir, AuthFile))
	for _, key := range []string{`"workspace_id"`, `"workspace_name"`, `"issuer"`} {
		if !strings.Contains(string(raw), key) {
			t.Fatalf("auth.json %s lacks %s", raw, key)
		}
	}
}

func TestAuthWorkspaceFieldsAreOmittedWhenEmpty(t *testing.T) {
	chdirTemp(t)
	if err := WriteAuth(&Auth{Token: "t0ken"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(ConfigDir, AuthFile))
	for _, key := range []string{"workspace_id", "workspace_name", "issuer"} {
		if strings.Contains(string(raw), key) {
			t.Fatalf("auth.json %s must omit %s when empty", raw, key)
		}
	}
}

func TestAuthFileWrittenBeforeTheWorkspaceFieldsStillReads(t *testing.T) {
	chdirTemp(t)
	if err := os.WriteFile(filepath.Join(ConfigDir, AuthFile), []byte(`{"token":"t0ken","refresh_token":"r"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := ReadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if out.Token != "t0ken" || out.WorkspaceID != "" || out.Issuer != "" {
		t.Fatalf("read back %+v, want the old file with no workspace", out)
	}
}

// A linked git worktree is another checkout of the same workspace. Its tracked
// .modernpath has no config.json, so the binding and the credential are the
// main checkout's: one auth.json, because ZITADEL rotates refresh tokens and a
// per-worktree copy would stop working once another copy refreshes.
func TestLinkedWorktreeUsesTheMainCheckoutBinding(t *testing.T) {
	main := chdirTemp(t)
	if err := os.MkdirAll(filepath.Join(main, ".git", "worktrees", "wt"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(&Config{SystemID: 3}); err != nil {
		t.Fatal(err)
	}
	gitDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(filepath.Join(wt, ConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(wt, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(filepath.Join(wt, "apps")); err != nil {
		t.Fatal(err)
	}

	cfg, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemID != 3 {
		t.Fatalf("worktree read system_id %d, want the main checkout's 3", cfg.SystemID)
	}
	if err := WriteAuth(&Auth{Token: "t0ken"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(main, ConfigDir, AuthFile)); err != nil {
		t.Fatalf("the credential did not reach the main checkout: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ConfigDir, AuthFile)); err == nil {
		t.Fatal("the worktree got its own credential copy")
	}
	own, _ := filepath.EvalSymlinks(filepath.Join(wt, ConfigDir))
	if dir, _ := FindConfigDir(); dir != own {
		t.Fatalf("FindConfigDir = %q, want the worktree's own .modernpath", dir)
	}
}

// A worktree that carries its own binding keeps it.
func TestLinkedWorktreeWithItsOwnBindingKeepsIt(t *testing.T) {
	main := chdirTemp(t)
	gitDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(&Config{SystemID: 3}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if err := os.MkdirAll(filepath.Join(wt, ConfigDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ConfigDir, ConfigFile), []byte(`{"system_id": 9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(wt); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemID != 9 {
		t.Fatalf("system_id %d, want the worktree's own 9", cfg.SystemID)
	}
}
