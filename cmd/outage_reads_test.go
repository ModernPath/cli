package cmd

// REQ-CROSS-537 (EPIC-CLI-OUTAGE) — while the server is unavailable, the
// loop's reads answer from the last snapshot with its age. Every case drives
// the command or hook through run(args) as the installed binary does, and
// asserts only the exit code, the printed text, the snapshot files' bytes and
// the requests the fake saw.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// snapshotFile writes a snapshot under root/rel with the two header lines
// every snapshot carries, taken at `at` from store revision rev.
func snapshotFile(t *testing.T, root, rel, title, apiURL, rev string, at time.Time, body string) []byte {
	t.Helper()
	content := "# " + title + "\n\n" +
		"- **Snapshot at:** " + at.UTC().Format(time.RFC3339) + "\n" +
		"- **Source store/revision:** " + apiURL + " · system 1 · store " + rev + "\n" +
		"- **CLI build:** modernpath test\n\n" + body
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return []byte(content)
}

func gatesSnapshot(t *testing.T, root, apiURL string, at time.Time) []byte {
	t.Helper()
	return snapshotFile(t, root, yourMoveDir+"/GATES.md", "Your move — open human decisions", apiURL, "rev-snap", at,
		"1 open gate(s). Only `OPEN` human gates appear here; this file is a\nregenerated projection, never an authority.\n\n"+
			"## ENTRY-EPIC-S — Enter EPIC-S\n\n- **Kind:** approval_request\n")
}

func selectionSnapshot(t *testing.T, root, apiURL string, at time.Time) []byte {
	t.Helper()
	return snapshotFile(t, root, workingSetDir+"/WORK-SELECTION.md", "WORK-SELECTION — working-set snapshot", apiURL, "rev-snap", at,
		"## Work selection\n\n- **Active release:** rel-1 (active)\n\n### Current selection\n\n"+
			"- **Selected scope:** EPIC-S\n- **Members:** REQ-S-1\n- **Current phase:** build\n")
}

func unchanged(t *testing.T, root, rel string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	if string(got) != string(want) {
		t.Errorf("%s was rewritten during the outage:\n%s", rel, got)
	}
}

const oldSnapshotWarning = "Warning: this snapshot is more than 24 hours old."

// AC1 — your-move answers from GATES.md under its age line and exits 0; with
// no GATES.md it names the missing snapshot and exits 75.
func TestYourMoveFromSnapshot(t *testing.T) {
	scaleOutageTimeouts(t, 200*time.Millisecond, 2*time.Second)
	t.Run("snapshot", func(t *testing.T) {
		apiURL := closedURL(t)
		root := outageWorkspace(t, apiURL)
		want := gatesSnapshot(t, root, apiURL, time.Now().Add(-2*time.Hour-30*time.Second))

		code, out := runCLI(t, "your-move")

		if code != 0 {
			t.Fatalf("exit %d, want 0: the snapshot answers the read; output:\n%s", code, out)
		}
		for _, s := range []string{"From the snapshot of", "(2 h old, store rev-snap)", "the ModernPath server is unavailable since", "ENTRY-EPIC-S"} {
			if !strings.Contains(out, s) {
				t.Errorf("output must contain %q:\n%s", s, out)
			}
		}
		if strings.Contains(out, oldSnapshotWarning) {
			t.Errorf("a 2-hour-old snapshot must not warn:\n%s", out)
		}
		unchanged(t, root, yourMoveDir+"/GATES.md", want)
	})
	t.Run("no snapshot", func(t *testing.T) {
		root := outageWorkspace(t, closedURL(t))

		code, out := runCLI(t, "your-move")

		if code != exitTempFail {
			t.Errorf("exit %d, want %d when no snapshot exists; output:\n%s", code, exitTempFail, out)
		}
		if !strings.Contains(out, "GATES.md") {
			t.Errorf("output must name the missing GATES.md snapshot:\n%s", out)
		}
		if _, err := os.Stat(filepath.Join(root, yourMoveDir, "GATES.md")); !os.IsNotExist(err) {
			t.Errorf("GATES.md must not be written from an unavailable answer (stat: %v)", err)
		}
	})
}

// AC1 — the SessionStart brief prints the GATES.md snapshot instead of {},
// whether the outage is met at the environment load or at the feed read.
func TestBriefFromSnapshotInOutage(t *testing.T) {
	scaleOutageTimeouts(t, 200*time.Millisecond, 2*time.Second)
	const payload = `{"session_id":"outage-brief","hook_event_name":"SessionStart","source":"startup"}`

	// Down after the first probe: the env loads, then the feed read and its
	// confirming probe both get the gateway's plain-text 503.
	var probes atomic.Int32
	probeThenDown := func(w http.ResponseWriter, r *http.Request) {
		if probes.Add(1) == 1 {
			healthyProbe(w, r)
			return
		}
		gatewayText(http.StatusServiceUnavailable)(w, r)
	}

	cases := []struct {
		name   string
		apiURL func(t *testing.T) string
	}{
		{name: "outage at environment load", apiURL: closedURL},
		{name: "outage at the feed read", apiURL: func(t *testing.T) string {
			probes.Store(0)
			srv, _ := storeFake(t, probeThenDown, gatewayText(http.StatusServiceUnavailable))
			return srv.URL
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			apiURL := tc.apiURL(t)
			root := outageWorkspace(t, apiURL)
			want := gatesSnapshot(t, root, apiURL, time.Now().Add(-2*time.Hour-30*time.Second))

			code, out := runCLIWithStdin(t, payload, "your-move", "--hook", "SessionStart", "--deadline", "5")

			if code != 0 {
				t.Errorf("the hook must exit 0, got %d", code)
			}
			var env struct {
				HookSpecificOutput struct {
					HookEventName     string `json:"hookEventName"`
					AdditionalContext string `json:"additionalContext"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &env); err != nil {
				t.Fatalf("output is not one JSON envelope: %v\n%s", err, out)
			}
			ctx := env.HookSpecificOutput.AdditionalContext
			if env.HookSpecificOutput.HookEventName != "SessionStart" {
				t.Errorf("want the SessionStart envelope, got %s", out)
			}
			for _, s := range []string{"From the snapshot of", "(2 h old, store rev-snap)", "the ModernPath server is unavailable since", "ENTRY-EPIC-S"} {
				if !strings.Contains(ctx, s) {
					t.Errorf("the brief must carry %q; got:\n%s", s, out)
				}
			}
			unchanged(t, root, yourMoveDir+"/GATES.md", want)
			log, _ := os.ReadFile(filepath.Join(root, ".modernpath", "session-briefs.log"))
			if !strings.Contains(string(log), "outage-snapshot") {
				t.Errorf("session-briefs.log must record outage-snapshot:\n%s", log)
			}
		})
	}
}

// AC2 — process next prints the selection and the pending decisions from the
// snapshots under the age line and exits 0, sending nothing; a missing
// WORK-SELECTION.md is named and exits 75.
func TestProcessNextFromSnapshot(t *testing.T) {
	t.Run("snapshots", func(t *testing.T) {
		srv, requests := countingStore(t)
		root := outageWorkspace(t, srv.URL)
		freshUnavailable(t, root)
		at := time.Now().Add(-2*time.Hour - 30*time.Second)
		sel := selectionSnapshot(t, root, srv.URL, at)
		gates := gatesSnapshot(t, root, srv.URL, at)

		code, out := runCLI(t, "process", "next")

		if code != 0 {
			t.Fatalf("exit %d, want 0: the snapshots answer the read; output:\n%s", code, out)
		}
		for _, s := range []string{"From the snapshot of", "(2 h old, store rev-snap)", "the ModernPath server is unavailable since", "EPIC-S", "build", "ENTRY-EPIC-S"} {
			if !strings.Contains(out, s) {
				t.Errorf("output must contain %q:\n%s", s, out)
			}
		}
		if n := requests.Load(); n != 0 {
			t.Errorf("%d request(s) reached the server while it is known to be unavailable; want 0", n)
		}
		unchanged(t, root, workingSetDir+"/WORK-SELECTION.md", sel)
		unchanged(t, root, yourMoveDir+"/GATES.md", gates)
	})
	t.Run("no selection snapshot", func(t *testing.T) {
		srv, _ := countingStore(t)
		root := outageWorkspace(t, srv.URL)
		freshUnavailable(t, root)
		gatesSnapshot(t, root, srv.URL, time.Now().Add(-time.Hour))

		code, out := runCLI(t, "process", "next")

		if code != exitTempFail {
			t.Errorf("exit %d, want %d when WORK-SELECTION.md is missing; output:\n%s", code, exitTempFail, out)
		}
		if !strings.Contains(out, "WORK-SELECTION.md") {
			t.Errorf("output must name the missing WORK-SELECTION.md snapshot:\n%s", out)
		}
	})
}

// AC3 — working-set pull leaves existing snapshots byte-identical and names
// each with its age, names the missing ones, and exits 75 only when one is
// missing.
func TestPullKeepsSnapshotsInOutage(t *testing.T) {
	srv, requests := countingStore(t)
	root := outageWorkspace(t, srv.URL)
	freshUnavailable(t, root)
	at := time.Now().Add(-3*time.Hour - 30*time.Second)
	item := snapshotFile(t, root, workingSetDir+"/REQ-S-1.md", "REQ-S-1 — working-set snapshot", srv.URL, "rev-snap", at,
		"## REQ REQ-S-1 — A requirement\n\n- **Status:** TODO\n")
	sel := selectionSnapshot(t, root, srv.URL, at)

	code, out := runCLI(t, "working-set", "pull", "REQ-S-1", "selection", "REQ-S-2")

	if code != exitTempFail {
		t.Errorf("exit %d, want %d when a named item has no snapshot; output:\n%s", code, exitTempFail, out)
	}
	for _, s := range []string{"REQ-S-1", "WORK-SELECTION.md", "3 h old", "REQ-S-2"} {
		if !strings.Contains(out, s) {
			t.Errorf("output must contain %q:\n%s", s, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, workingSetDir, "REQ-S-2.md")); !os.IsNotExist(err) {
		t.Errorf("no snapshot may be written for REQ-S-2 during the outage (stat: %v)", err)
	}
	unchanged(t, root, workingSetDir+"/REQ-S-1.md", item)
	unchanged(t, root, workingSetDir+"/WORK-SELECTION.md", sel)

	code, out = runCLI(t, "working-set", "pull", "REQ-S-1", "selection")

	if code != 0 {
		t.Errorf("exit %d, want 0 when every named item has a snapshot; output:\n%s", code, out)
	}
	if !strings.Contains(out, "3 h old") {
		t.Errorf("output must name each snapshot with its age:\n%s", out)
	}
	unchanged(t, root, workingSetDir+"/REQ-S-1.md", item)
	unchanged(t, root, workingSetDir+"/WORK-SELECTION.md", sel)
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached the server while it is known to be unavailable; want 0", n)
	}
}

// AC4 — a snapshot older than 24 hours answers with a warning that says how
// old it is.
func TestOldSnapshotWarns(t *testing.T) {
	srv, _ := countingStore(t)
	root := outageWorkspace(t, srv.URL)
	freshUnavailable(t, root)
	gatesSnapshot(t, root, srv.URL, time.Now().Add(-30*time.Hour-30*time.Second))

	code, out := runCLI(t, "your-move")

	if code != 0 {
		t.Fatalf("exit %d, want 0; output:\n%s", code, out)
	}
	for _, s := range []string{"From the snapshot of", "(30 h old", oldSnapshotWarning} {
		if !strings.Contains(out, s) {
			t.Errorf("output must contain %q:\n%s", s, out)
		}
	}
}
