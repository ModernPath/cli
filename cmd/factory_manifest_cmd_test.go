// REQ-CROSS-213 (EPIC-CLI-001 T10, RUN:2026-08-18): `factory manifest` was
// the only factory subcommand with no ledger row and no command-level test.
// The row is PENDING_VERIFICATION — reverse-described from shipped code — so
// these tests pin the described behavior to promote it (rdd-verify: write
// the missing test, promote what goes green).
package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedManifestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)

	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := `# SBX ledger

Totals: 1 READY

| ID | Title | Stage | Status | UR | Source | Tests | Code |
|---|---|---|---|---|---|---|---|
| REQ-SBX-001 | A thing works | MVP | READY | | DOC:x | — | — |
`
	if err := os.WriteFile(filepath.Join(dir, "tasks", "SBX-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "WORKLIST.md"), []byte("# Work list\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestManifestInitDetectsAndWrites(t *testing.T) {
	dir := seedManifestRepo(t)

	if err := factoryManifestInitCmd.RunE(factoryManifestInitCmd, nil); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ".modernpath", "manifest.json"))
	if err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	for _, want := range []string{"requirements", "rdd-ledger-v1", "worklist", "WORKLIST.md"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("manifest missing %q:\n%s", want, raw)
		}
	}
}

func TestManifestInitRefusesOverwriteWithoutForce(t *testing.T) {
	seedManifestRepo(t)

	if err := factoryManifestInitCmd.RunE(factoryManifestInitCmd, nil); err != nil {
		t.Fatal(err)
	}
	err := factoryManifestInitCmd.RunE(factoryManifestInitCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("a second init must refuse and name --force, got: %v", err)
	}
}

func TestManifestInitOnEmptyRepoWritesProposalNotManifest(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// SCN-SY-036: an undetectable layout leaves the reviewable proposal and
	// exits cleanly — never a silent error, never a guessed manifest.
	if err := factoryManifestInitCmd.RunE(factoryManifestInitCmd, nil); err != nil {
		t.Fatalf("empty repo must not error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".modernpath", "manifest.json")); err == nil {
		t.Fatal("no manifest may be written when nothing was detected")
	}
	proposals, _ := filepath.Glob(filepath.Join(dir, ".modernpath", "*mapping*"))
	if len(proposals) == 0 {
		// the worksheet's exact name is the implementation's choice — find it
		entries, _ := os.ReadDir(filepath.Join(dir, ".modernpath"))
		if len(entries) == 0 {
			t.Fatal("the mapping-proposal worksheet must exist for an undetectable layout")
		}
	}
}

func TestManifestShowRendersDefaultsAndFile(t *testing.T) {
	seedManifestRepo(t)

	out := captureCLIOutput(t)
	if err := factoryManifestShowCmd.RunE(factoryManifestShowCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out(), "defaults") {
		t.Fatalf("without a file, show must say it renders defaults:\n%s", out())
	}

	if err := factoryManifestInitCmd.RunE(factoryManifestInitCmd, nil); err != nil {
		t.Fatal(err)
	}
	out2 := captureCLIOutput(t)
	if err := factoryManifestShowCmd.RunE(factoryManifestShowCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2(), "manifest.json") {
		t.Fatalf("with a file, show must name it:\n%s", out2())
	}
}
