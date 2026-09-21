package cmd

// Store-backed retirement of the file-derived auto-sync (REQ-CROSS-329): after
// the flip the ledgers a sync reads are retired and the bulk channel is
// refused, so `factory sync` has nothing to push. Three surfaces must know it:
// the manual command says where process state is written instead; the quiescent
// hook path skips (before env/network) and logs it; and `hooks install` retires
// the sync family rather than re-adding it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustWriteStoreBackedMarker(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "process", "store-backed.md"),
		[]byte("# Store-backed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The manual command must not sync a retired file corpus: it exits 0 with a
// notice naming the store-backed write path (author), not a pushed batch — and
// without needing credentials or the network, so the guard sits ahead of
// factoryEnvLoad.
func TestFactorySyncManualStoreBackedNotice(t *testing.T) {
	root := chdirTemp(t)
	mustWriteStoreBackedMarker(t, root)

	savedQ, savedDry := factorySyncIfQuiescent, factorySyncDryRun
	factorySyncIfQuiescent, factorySyncDryRun = false, false
	defer func() { factorySyncIfQuiescent, factorySyncDryRun = savedQ, savedDry }()

	out := captureCLIOutput(t)
	if err := factorySyncCmd.RunE(factorySyncCmd, nil); err != nil {
		t.Fatalf("store-backed manual sync must not error: %v", err)
	}
	text := out()
	for _, want := range []string{"store-backed", "nothing to sync", "author"} {
		if !strings.Contains(text, want) {
			t.Fatalf("store-backed notice missing %q, got: %q", want, text)
		}
	}
}

// The quiescent hook path skips before it needs env or network — a flipped
// workspace with no credentials still exits 0 and logs the reason.
func TestFactorySyncQuiescentSkipsWhenStoreBacked(t *testing.T) {
	root := chdirTemp(t)
	mustWriteStoreBackedMarker(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := factorySyncQuiescent("Stop", 0); err != nil {
		t.Fatalf("quiescent must never error: %v", err)
	}

	raw, err := os.ReadFile(hookLogPath(root))
	if err != nil {
		t.Fatalf("expected a hook log line: %v", err)
	}
	log := string(raw)
	if !strings.Contains(log, "skip") || !strings.Contains(log, "store-backed") {
		t.Fatalf("expected a store-backed skip in the hook log, got: %q", log)
	}
}

// `hooks install` on a workspace that has flipped must RETIRE the sync family —
// remove the entries a pre-flip install wired — while leaving the other
// families in place, so re-running install cleans the workspace instead of
// re-adding a retired hook.
func TestHooksInstallRetiresSyncFamilyWhenStoreBacked(t *testing.T) {
	root := chdirTemp(t)
	agent := selectClaudeOnly(t)

	// a pre-flip workspace has the sync family wired
	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(agent.configPath)
	if !strings.Contains(string(before), syncHookMarker) {
		t.Fatalf("precondition: sync family should be wired, got: %q", before)
	}

	// flip, then re-install
	mustWriteStoreBackedMarker(t, root)
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(agent.configPath)
	if strings.Contains(string(after), syncHookMarker) {
		t.Fatalf("store-backed install must retire the sync family, still present: %q", after)
	}
	// the context family is still installed by the same pass
	if !strings.Contains(string(after), contextHookMarker) {
		t.Fatalf("context family must survive the store-backed install, got: %q", after)
	}
}

// chdirSubdirOfMarker builds root/{.modernpath, process/store-backed.md} and a
// nested subdir, chdirs into the subdir, and returns root. It models the
// monorepo case: the CLI is invoked from a subdirectory while the marker (and
// the binding) live at the workspace root, reachable only by walking up.
func chdirSubdirOfMarker(t *testing.T, rel string) string {
	t.Helper()
	root := t.TempDir()
	mustWriteStoreBackedMarker(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, rel)
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	old, _ := os.Getwd()
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

// The store-backed guards must resolve the workspace the way the commands do —
// by walking up to .modernpath — so a subdirectory invocation (the monorepo
// norm) still sees the marker. A bare os.Getwd() stat would miss it and fall
// through to the very file-derived sync the guard exists to prevent.
func TestFactorySyncManualStoreBackedNoticeFromSubdir(t *testing.T) {
	chdirSubdirOfMarker(t, "modernpath-core")

	savedQ, savedDry := factorySyncIfQuiescent, factorySyncDryRun
	factorySyncIfQuiescent, factorySyncDryRun = false, false
	defer func() { factorySyncIfQuiescent, factorySyncDryRun = savedQ, savedDry }()

	out := captureCLIOutput(t)
	if err := factorySyncCmd.RunE(factorySyncCmd, nil); err != nil {
		t.Fatalf("subdir store-backed manual sync must not error: %v", err)
	}
	if text := out(); !strings.Contains(text, "store-backed") {
		t.Fatalf("subdir invocation must still get the store-backed notice, got: %q", text)
	}
}

func TestFactorySyncQuiescentSkipsWhenStoreBackedFromSubdir(t *testing.T) {
	root := chdirSubdirOfMarker(t, filepath.Join("modernpath-core", "apps"))

	if err := factorySyncQuiescent("Stop", 0); err != nil {
		t.Fatalf("quiescent must never error: %v", err)
	}
	// the skip is logged at the RESOLVED root, not the subdir
	raw, err := os.ReadFile(hookLogPath(root))
	if err != nil {
		t.Fatalf("expected a hook log at the resolved workspace root: %v", err)
	}
	if !strings.Contains(string(raw), "store-backed") {
		t.Fatalf("subdir invocation must still detect store-backed, got: %q", raw)
	}
}

// factory watch is the fourth sync surface: its per-cycle factorySyncRun is the
// same file-derived push, so it must decline under the marker rather than loop.
func TestFactoryWatchStoreBackedNotice(t *testing.T) {
	root := chdirTemp(t)
	mustWriteStoreBackedMarker(t, root)

	out := captureCLIOutput(t)
	if err := factoryWatchCmd.RunE(factoryWatchCmd, nil); err != nil {
		t.Fatalf("store-backed watch must not error: %v", err)
	}
	if text := out(); !strings.Contains(text, "store-backed") {
		t.Fatalf("store-backed watch must print the notice, got: %q", text)
	}
}

// The Codex sync family retires under the marker exactly as Claude's does.
func TestHooksInstallRetiresCodexSyncFamilyWhenStoreBacked(t *testing.T) {
	root := chdirTemp(t)
	agent := hookAgents["codex"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installSyncFamilyCodex(agent); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(agent.configPath)
	if !strings.Contains(string(before), syncHookMarker) {
		t.Fatalf("precondition: codex sync family should be wired, got: %q", before)
	}

	mustWriteStoreBackedMarker(t, root)
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(agent.configPath)
	if strings.Contains(string(after), syncHookMarker) {
		t.Fatalf("store-backed install must retire the codex sync family, still present: %q", after)
	}
}

// Regression guard: a file-backed workspace (no marker) still WIRES the sync
// family — the store-backed branch must not leak into normal installs.
func TestHooksInstallStillWiresSyncFamilyWhenNotStoreBacked(t *testing.T) {
	chdirTemp(t)
	agent := selectClaudeOnly(t)

	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(agent.configPath)
	if !strings.Contains(string(after), syncHookMarker) {
		t.Fatalf("file-backed install must still wire the sync family, got: %q", after)
	}
}
