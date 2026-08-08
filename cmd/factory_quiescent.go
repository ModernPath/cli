package cmd

// EPIC-SYNC-009 (REQ-CROSS-027): the quiescence gate for hook-triggered syncs.
// `factory sync --if-quiescent` never errors into the harness (D-AS-4): every
// outcome — run, debounce-skip, lock-skip, incoherence-skip, even a failed
// sync — is one line in .modernpath/sync-hooks.log and exit 0.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	rdd "github.com/modernpath/cli/internal/rdd"
)

const quiescentLockStaleAfter = 10 * time.Minute

func hookLogPath(root string) string { return filepath.Join(root, ".modernpath", "sync-hooks.log") }
func lastSyncOkPath(root string) string {
	return filepath.Join(root, ".modernpath", "last-sync-ok")
}
func syncLockPath(root string) string { return filepath.Join(root, ".modernpath", "sync.lock") }

func hookLog(root, trigger, outcome, reason string) {
	line := fmt.Sprintf("%s · %s · %s · %s\n",
		time.Now().UTC().Format(time.RFC3339), trigger, outcome, reason)
	appendFile(hookLogPath(root), line)
}

// acquireSyncLock returns a release func, or nil when another sync holds the
// lock (a stale lock older than 10 minutes is broken — D-AS-2).
func acquireSyncLock(root string) func() {
	path := syncLockPath(root)

	if info, err := os.Stat(path); err == nil {
		if time.Since(info.ModTime()) < quiescentLockStaleAfter {
			return nil
		}
		_ = os.Remove(path) // stale — a crashed detached run
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil
	}
	fmt.Fprintf(f, "%d %s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	f.Close()
	return func() { _ = os.Remove(path) }
}

// quiescentPrecheck runs debounce + coherence (D-AS-2). Returns "" to
// proceed, or the skip reason.
func quiescentPrecheck(root string, minInterval time.Duration) string {
	if info, err := os.Stat(lastSyncOkPath(root)); err == nil {
		if age := time.Since(info.ModTime()); age < minInterval {
			return fmt.Sprintf("debounce: last sync %s ago (< %s)", age.Round(time.Second), minInterval)
		}
	}

	if problems := rdd.CheckLedgerCoherence(root); len(problems) > 0 {
		return "incoherent: " + problems[0]
	}

	return ""
}

// factorySyncQuiescent is the hook entrypoint: gate, sync, log — exit 0 always.
func factorySyncQuiescent(trigger string, minInterval time.Duration) error {
	env, err := factoryEnvLoad()
	if err != nil {
		// not connected / not initialized — a hook in a fresh clone; log-and-quiet
		if root, rerr := os.Getwd(); rerr == nil {
			if _, serr := os.Stat(filepath.Join(root, ".modernpath")); serr == nil {
				hookLog(root, trigger, "skip", "env: "+err.Error())
			}
		}
		return nil
	}

	release := acquireSyncLock(env.Root)
	if release == nil {
		hookLog(env.Root, trigger, "skip", "another sync holds the lock")
		return nil
	}
	defer release()

	if reason := quiescentPrecheck(env.Root, minInterval); reason != "" {
		hookLog(env.Root, trigger, "skip", reason)
		return nil
	}

	// schema-invalid ops = mid-edit too (the builders validate before the wire)
	if _, _, err := env.workspaceOps(); err != nil {
		hookLog(env.Root, trigger, "skip", "ops: "+err.Error())
		return nil
	}

	if err := factorySyncRun(env, false); err != nil {
		hookLog(env.Root, trigger, "sync-failed", err.Error())
		return nil
	}

	_ = os.WriteFile(lastSyncOkPath(env.Root), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644)
	hookLog(env.Root, trigger, "synced", "ok")
	return nil
}
