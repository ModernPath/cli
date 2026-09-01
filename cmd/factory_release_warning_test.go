package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-177: the unscoped-sync warning distinguishes "this project has no
// release registry yet" from "the registry exists and no release is selected".
//
// A fresh workspace (RUN:2026-08-15) gets the same line on every sync:
// "set one with 'modernpath factory release use <slug>'" — advice that cannot
// be followed, because process/releases.md does not exist and creating it is a
// human product decision the CLI must not make. Repeated un-followable advice
// trains the reader to skim warnings, which is how real ones get missed.
func TestReleaseWarningNamesTheActualNextStep(t *testing.T) {
	t.Run("no registry: says how to adopt releases, not how to select one", func(t *testing.T) {
		root := t.TempDir()
		msg := releaseWarning(root)
		if !strings.Contains(msg, "no release registry") || !strings.Contains(msg, "process/releases.md") {
			t.Fatalf("want the adopt-releases message, got %q", msg)
		}
		if strings.Contains(msg, "set one with") {
			t.Fatalf("selection advice without a registry is un-followable: %q", msg)
		}
	})

	t.Run("registry exists: the selection advice stands", func(t *testing.T) {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "process", "releases.md"), []byte("# Releases\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		msg := releaseWarning(root)
		if !strings.Contains(msg, "factory release use") {
			t.Fatalf("registry exists — selection is the next step: %q", msg)
		}
	})
}

// REQ-CROSS-283 (`USER:2026-08-27`): the warning has to state its CONSEQUENCE.
// "syncing unscoped (fine for derived/base work)" was true and useless — on
// RUN:2026-08-27 it preceded 906 requirements landing where the Ledger could
// not see them, and nothing in the line said so. Naming the base release makes
// the outcome checkable by the person reading it.
func TestReleaseWarningNamesWhereTheWorkLands(t *testing.T) {
	roots := map[string]string{"no registry": t.TempDir(), "registry": t.TempDir()}
	if err := os.MkdirAll(filepath.Join(roots["registry"], "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(roots["registry"], "process", "releases.md"), []byte("# Releases\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for name, root := range roots {
		t.Run(name, func(t *testing.T) {
			msg := releaseWarning(root)
			if !strings.Contains(msg, "base release") {
				t.Fatalf("the reader cannot tell where the work went: %q", msg)
			}
			if strings.Contains(msg, "unscoped") {
				t.Fatalf("nothing is unscoped any more — base is a real release: %q", msg)
			}
		})
	}
}
