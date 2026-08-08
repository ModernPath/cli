package cmd

// EPIC-SYNC-009 (SCN-AS-003/004/006): the gate mechanics — debounce, lock
// (incl. stale-break), coherence skip — all file-level, no network.

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedQuiescentRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestQuiescentDebounceSkips(t *testing.T) {
	root := seedQuiescentRoot(t)
	if err := os.WriteFile(lastSyncOkPath(root), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reason := quiescentPrecheck(root, time.Minute)
	if reason == "" {
		t.Fatal("fresh last-sync-ok must debounce")
	}

	// an old stamp passes the debounce
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lastSyncOkPath(root), old, old); err != nil {
		t.Fatal(err)
	}
	if reason := quiescentPrecheck(root, time.Minute); reason != "" {
		t.Fatalf("aged stamp should pass, got %q", reason)
	}
}

func TestQuiescentCoherenceSkips(t *testing.T) {
	root := seedQuiescentRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Totals claims 2 DONE, rows carry 1 — the mid-edit signature
	if err := os.WriteFile(filepath.Join(root, "tasks", "TST-REQUIREMENTS.md"),
		[]byte("Totals: 2 DONE\n\n| REQ-TST-001 | One | MVP | DONE | x | — | — |\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	reason := quiescentPrecheck(root, time.Minute)
	if reason == "" {
		t.Fatal("incoherent ledger must skip")
	}
}

func TestSyncLockSerializesAndBreaksStale(t *testing.T) {
	root := seedQuiescentRoot(t)

	release := acquireSyncLock(root)
	if release == nil {
		t.Fatal("first acquire must win")
	}
	if second := acquireSyncLock(root); second != nil {
		t.Fatal("held lock must refuse a second acquire")
	}
	release()

	// stale lock (aged past the threshold) is broken
	if err := os.WriteFile(syncLockPath(root), []byte("1 x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-11 * time.Minute)
	if err := os.Chtimes(syncLockPath(root), old, old); err != nil {
		t.Fatal(err)
	}
	if release := acquireSyncLock(root); release == nil {
		t.Fatal("stale lock must be broken")
	} else {
		release()
	}
}

func TestHookLogAppendsOneLine(t *testing.T) {
	root := seedQuiescentRoot(t)
	hookLog(root, "Stop", "skip", "debounce")
	hookLog(root, "SessionEnd", "synced", "ok")

	raw, err := os.ReadFile(hookLogPath(root))
	if err != nil {
		t.Fatal(err)
	}
	lines := 0
	for _, b := range raw {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("want 2 log lines, got %d: %q", lines, raw)
	}
}
