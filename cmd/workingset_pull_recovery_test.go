package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScopePullPartialWriteThenRetryUsesCompletedBaselines(t *testing.T) {
	for _, fresh := range []bool{false, true} {
		name := "refresh"
		if fresh {
			name = "initial_pull"
		}
		t.Run(name, func(t *testing.T) {
			fx := scopeFixture()
			env := wsEnv(t, wsServe(t, fx))
			dir := scopeDir(env)
			var memberBefore scopedDraftBaselineEntry
			if !fresh {
				if err := workingSetPullScope(env, false, wsNow); err != nil {
					t.Fatal(err)
				}
				memberBefore = scopedBaselineEntryForTest(t, dir, "members/REQ-CROSS-310.md")
			}
			// An unmanaged projection is written after the epic. A directory at
			// that path passes draft preflight but makes its atomic rename fail,
			// including when the tests run as root.
			obstruction := filepath.Join(dir, "findings", "COLD-REVIEW.md")
			if !fresh {
				if err := os.Remove(obstruction); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.MkdirAll(obstruction, 0o755); err != nil {
				t.Fatal(err)
			}
			fx.epics[0].(map[string]any)["title"] = "updated epic from server"
			fx.requirements[0].(map[string]any)["boundary"] = "updated boundary from server"
			err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
			if err == nil || !strings.Contains(err.Error(), "COLD-REVIEW.md") {
				t.Fatalf("expected later projection write to fail, got %v", err)
			}
			if got := readScopeFile(t, filepath.Join(dir, "EPIC-CLI-008.md")); !strings.Contains(got, "updated epic from server") {
				t.Fatal("fixture failed before the epic was updated")
			}
			assertScopedBaselineMatchesFile(t, dir, "EPIC-CLI-008.md")
			if !fresh {
				if got := scopedBaselineEntryForTest(t, dir, "members/REQ-CROSS-310.md"); got != memberBefore {
					t.Fatal("failed pull advanced the baseline of an unwritten member")
				}
			}
			if err := os.Remove(obstruction); err != nil {
				t.Fatal(err)
			}
			if err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute)); err != nil {
				t.Fatalf("retry must accept files written by the failed pull: %v", err)
			}
			assertScopedBaselineMatchesFile(t, dir, "EPIC-CLI-008.md")
			assertScopedBaselineMatchesFile(t, dir, "members/REQ-CROSS-310.md")
			if got := readScopeFile(t, memberPath(dir, "REQ-CROSS-310")); !strings.Contains(got, "updated boundary from server") {
				t.Fatal("retry did not refresh the remaining member")
			}
		})
	}
}

func TestScopePullPartialCleanupThenRetryRetainsCompletedDeletions(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	writePacketFile(t, dir, "local-notes.md", "unserved local draft\n")
	obstruction := filepath.Join(dir, contextFile)
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(obstruction, 0o755); err != nil {
		t.Fatal(err)
	}
	fx.packetSectionsStatus = 404
	err := workingSetPullScope(env, false, wsNow.Add(time.Minute))
	if err == nil || !strings.Contains(err.Error(), contextFile) {
		t.Fatalf("expected context write failure after packet cleanup, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "packet", "10-recon.md")); !os.IsNotExist(err) {
		t.Fatalf("fixture failed before the packet was removed: %v", err)
	}
	if _, known, err := readScopedDraftBaselineEntry(dir, "packet/10-recon.md"); err != nil || known {
		t.Fatalf("completed packet deletion retained a stale baseline: known=%v err=%v", known, err)
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute)); err != nil {
		t.Fatalf("retry must accept packet cleanup performed by failed pull: %v", err)
	}
	if got := readScopeFile(t, filepath.Join(dir, "packet", "local-notes.md")); got != "unserved local draft\n" {
		t.Fatalf("partial cleanup or retry changed the unserved draft: %q", got)
	}
}

func TestScopePullPartialWriteStillProtectsSubsequentLocalEdits(t *testing.T) {
	fx := scopeFixture()
	env, dir := pulledScope(t, fx)
	obstruction := filepath.Join(dir, "findings", "COLD-REVIEW.md")
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(obstruction, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := workingSetPullScope(env, false, wsNow.Add(time.Minute)); err == nil {
		t.Fatal("expected partial pull failure")
	}
	if err := os.Remove(obstruction); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "EPIC-CLI-008.md")
	authored := readScopeFile(t, path) + "\nnew local draft\n"
	if err := os.WriteFile(path, []byte(authored), 0o644); err != nil {
		t.Fatal(err)
	}
	err := workingSetPullScope(env, false, wsNow.Add(2*time.Minute))
	if err == nil || !strings.Contains(err.Error(), "has local edits") {
		t.Fatalf("retry must protect edits made after a partial pull: %v", err)
	}
	if got := readScopeFile(t, path); got != authored {
		t.Fatal("retry overwrote a draft edited after a partial pull")
	}
}
