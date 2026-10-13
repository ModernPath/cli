package cmd

// The #779 review of 5f4aaeb15 (CODE-OUTAGE-3-x, USER:2026-10-12): replay
// fixes for REQ-CROSS-538 and REQ-CROSS-539.

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-539 AC7 (CODE-OUTAGE-3-1) — an entry that waits on one no longer
// in the journal is sent with its own kept fingerprint, never left stuck.
func TestReplaySendsAnEntryWhosePredecessorIsGone(t *testing.T) {
	f := &replayFake{}
	root := replayWorkspace(t, f)
	keep(t, "author", "update", "REQ-R-2", "--title", "First", "--expected-fingerprint", "fp-1")
	keep(t, "author", "update", "REQ-R-2", "--title", "Second", "--expected-fingerprint", "fp-1b")
	kept := keptEntries(t, root)
	if len(kept) != 2 {
		t.Fatalf("two kept edits, found %d", len(kept))
	}
	if err := os.Remove(filepath.Join(journalPath(root), kept[0].name)); err != nil {
		t.Fatal(err)
	}
	serverBack(t, root)

	code, out := runCLI(t, "outage", "replay")

	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	recs := authorRecords(f)
	if len(recs) != 1 || str(recs[0], "title") != "Second" {
		t.Fatalf("the waiting edit must be sent; author posts %v", recs)
	}
	if got := str(recs[0], "expected_fingerprint"); got != "fp-1b" {
		t.Errorf("it is sent with its own kept fingerprint fp-1b, got %q", got)
	}
	if n := len(keptEntries(t, root)); n != 0 {
		t.Errorf("nothing stays stuck in the journal; %d remain", n)
	}
}

// REQ-CROSS-539 AC6 (CODE-OUTAGE-3-2) — a 5xx from core during replay stops it
// and keeps the entry and the rest; nothing is set aside as refused.
func TestReplayStopsOnAServerFault(t *testing.T) {
	f := &replayFake{}
	f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
		if r.path == "/api/v1/sync/author" && str(obj(r.body, "record"), "external_id") == "REQ-R-2" {
			return &replayReply{status: 500, body: map[string]any{"error": map[string]any{"message": "internal error"}}}
		}
		return nil
	}
	root := replayWorkspace(t, f)
	keep(t, "factory", "evidence", "--pass", "REQ-R-1")
	keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
	keep(t, "author", "update", "REQ-R-3", "--title", "Other title", "--expected-fingerprint", "fp-3")
	serverBack(t, root)

	runCLI(t, "outage", "replay")

	if n := len(asideEntries(t, root, "refused")); n != 0 {
		t.Errorf("a server fault sets nothing aside as refused; %d there", n)
	}
	if n := len(keptEntries(t, root)); n != 2 {
		t.Errorf("the entry the fault stopped and the one after it stay kept; %d kept", n)
	}
}

// REQ-CROSS-538 AC2 (CODE-OUTAGE-3-4) — a RESOLVED disposition and a finding
// that names the finding it was introduced by are kept during an outage; the
// server's capability is checked when they are replayed.
func TestResolvedDispositionAndIntroducedByAreKept(t *testing.T) {
	f := &replayFake{}
	replayWorkspace(t, f)
	keep(t, "process", "findings", "disposition", "--id", "F-K-1", "--disposition", "RESOLVED",
		"--resolution", "packet-edit", "--expected-fingerprint", "fp-f-1")
	keep(t, "process", "findings", "add", "--scope", "epic:EPIC-K", "--id", "F-K-2",
		"--category", "correctness", "--severity", "major", "--body", "follow-up", "--aggregate", "agg-k",
		"--introduced-by", "F-K-1")
}

// REQ-CROSS-539 AC2 (CODE-OUTAGE-3-5) — a command run with --dry-run never
// replays kept writes.
func TestDryRunDoesNotReplay(t *testing.T) {
	f := &replayFake{}
	root := replayWorkspace(t, f)
	keep(t, "author", "update", "REQ-R-2", "--title", "New title", "--expected-fingerprint", "fp-1")
	serverBack(t, root)

	runCLI(t, "author", "apply", "--file", dryRunPlanFile(t, root), "--dry-run")

	for _, p := range f.posts() {
		if p.path == "/api/v1/sync/author" && str(obj(p.body, "record"), "external_id") == "REQ-R-2" {
			t.Fatalf("a dry run must not send kept writes; posted %v", postPaths(f.posts()))
		}
	}
	if n := len(keptEntries(t, root)); n != 1 {
		t.Errorf("the kept write stays kept after a dry run; %d entr(ies)", n)
	}
}

// dryRunPlanFile is a minimal author plan for a dry run.
func dryRunPlanFile(t *testing.T, root string) string {
	t.Helper()
	p := filepath.Join(root, "plan.yaml")
	if err := os.WriteFile(p, []byte("requirements:\n  - {id: REQ-DRY-1, kind: sr, context: X, title: Dry}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// REQ-CROSS-539 AC7 (CODE-OUTAGE-3-6) — an entry whose request fails without
// an outage, a 5xx or a 429 (a client timeout while the server answers) does
// not block the rest, and is set aside as refused after its third failed
// replay.
func TestAFailingEntryDoesNotBlockTheRest(t *testing.T) {
	f := &replayFake{}
	f.on = func(_ *replayFake, r journeyRequest, _ url.Values) *replayReply {
		if r.path == "/api/v1/sync/author" && str(obj(r.body, "record"), "external_id") == "REQ-R-2" {
			time.Sleep(600 * time.Millisecond)
		}
		return nil
	}
	root := replayWorkspace(t, f)
	keep(t, "factory", "evidence", "--pass", "REQ-R-1")
	keep(t, "author", "update", "REQ-R-2", "--title", "Slow", "--expected-fingerprint", "fp-1")
	keep(t, "author", "update", "REQ-R-3", "--title", "Other title", "--expected-fingerprint", "fp-3")
	scaleOutageTimeouts(t, 500*time.Millisecond, 200*time.Millisecond)

	serverBack(t, root)
	runCLI(t, "outage", "replay")

	sentR3 := false
	for _, rec := range authorRecords(f) {
		if str(rec, "external_id") == "REQ-R-3" {
			sentR3 = true
		}
	}
	if !sentR3 {
		t.Fatalf("the entry after a failing one is sent; author posts %v", postPaths(f.posts()))
	}
	left := keptEntries(t, root)
	if len(left) != 1 || !strings.Contains(left[0].name, "author") {
		t.Fatalf("only the failing entry stays kept; found %v", left)
	}

	for i := 0; i < 2; i++ {
		serverBack(t, root)
		runCLI(t, "outage", "replay")
	}
	if n := len(keptEntries(t, root)); n != 0 {
		t.Errorf("after its third failed replay the entry leaves the journal; %d remain", n)
	}
	if n := len(asideEntries(t, root, "refused")); n != 1 {
		t.Errorf("it is set aside as refused with its error; %d refused", n)
	}
}
