package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

// `modernpath init` binds "here", so it can create a .modernpath nested under an
// existing one. The nested binding then shadows the parent for every later
// command — and the credential lives beside the binding.
//
// RUN:2026-08-23 (nextpath-ai): the user authenticated, `auth.json` was written
// to the bound parent workspace, then `init --force` in the subdirectory created
// a nested binding with no credential. `ReadAuth` returns an empty Auth for a
// missing file rather than an error, so nothing complained until every API call
// answered 401 and the whole reverse-engineering pass ran unreconciled.
func TestCarryCredentialFromShadowedBinding(t *testing.T) {
	t.Run("carries the parent credential into a newly nested binding", func(t *testing.T) {
		root := t.TempDir()
		prior := filepath.Join(root, config.ConfigDir)
		nested := filepath.Join(root, "repo", config.ConfigDir)
		mustMkdir(t, prior)
		mustMkdir(t, nested)
		mustWrite(t, filepath.Join(prior, config.AuthFile), `{"token":"tok"}`)

		carried, err := carryCredentialFromShadowedBinding(prior, nested)
		if err != nil || !carried {
			t.Fatalf("carried=%v err=%v", carried, err)
		}

		got := mustRead(t, filepath.Join(nested, config.AuthFile))
		if got != `{"token":"tok"}` {
			t.Fatalf("credential not carried: %q", got)
		}
		// A credential must not become world-readable in transit.
		info, err := os.Stat(filepath.Join(nested, config.AuthFile))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("credential mode = %o, want 600", perm)
		}
	})

	t.Run("never overwrites a credential the target already has", func(t *testing.T) {
		root := t.TempDir()
		prior := filepath.Join(root, config.ConfigDir)
		nested := filepath.Join(root, "repo", config.ConfigDir)
		mustMkdir(t, prior)
		mustMkdir(t, nested)
		mustWrite(t, filepath.Join(prior, config.AuthFile), `{"token":"parent"}`)
		mustWrite(t, filepath.Join(nested, config.AuthFile), `{"token":"own"}`)

		carried, err := carryCredentialFromShadowedBinding(prior, nested)
		if err != nil || carried {
			t.Fatalf("carried=%v err=%v — must not clobber", carried, err)
		}
		if got := mustRead(t, filepath.Join(nested, config.AuthFile)); got != `{"token":"own"}` {
			t.Fatalf("target credential was overwritten: %q", got)
		}
	})

	t.Run("no prior binding and same-directory are both no-ops", func(t *testing.T) {
		root := t.TempDir()
		nested := filepath.Join(root, config.ConfigDir)
		mustMkdir(t, nested)

		if carried, err := carryCredentialFromShadowedBinding("", nested); err != nil || carried {
			t.Fatalf("no prior binding: carried=%v err=%v", carried, err)
		}
		if carried, err := carryCredentialFromShadowedBinding(nested, nested); err != nil || carried {
			t.Fatalf("same dir: carried=%v err=%v", carried, err)
		}
	})

	t.Run("a parent with no credential is not an error", func(t *testing.T) {
		root := t.TempDir()
		prior := filepath.Join(root, config.ConfigDir)
		nested := filepath.Join(root, "repo", config.ConfigDir)
		mustMkdir(t, prior)
		mustMkdir(t, nested)

		if carried, err := carryCredentialFromShadowedBinding(prior, nested); err != nil || carried {
			t.Fatalf("carried=%v err=%v", carried, err)
		}
	})
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
