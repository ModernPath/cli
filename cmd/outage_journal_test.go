package cmd

// REQ-CROSS-538 (EPIC-CLI-OUTAGE) — while the server is unavailable, process
// writes are kept on the machine with their ids fixed. Every case drives the
// command through run(args) as the installed binary does, and asserts only the
// exit code, the printed text, the journal files written and the requests the
// fake store saw.

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/zitadel"
)

const keptWaitLine = "nothing was kept."

// downWorkspace is a git workspace bound to a store known to be unavailable
// (checked just now), signed in as the person who keeps the writes. The fake
// store counts every request: none may reach it.
func downWorkspace(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	srv, requests := countingStore(t)
	root := outageWorkspace(t, srv.URL)
	gitRun(t, root, "init", "-q", "-b", "main")
	gitRun(t, root, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "-q", "--allow-empty", "-m", "init")
	signedInAs(t, root, map[string]any{"sub": "user-538", "email": "dev@example.com"})
	freshUnavailable(t, root)
	return root, requests
}

// signedInAs stores a bearer carrying claims, valid for an hour.
func signedInAs(t *testing.T, root string, claims map[string]any) {
	t.Helper()
	claims["exp"] = time.Now().Add(time.Hour).Unix()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	auth := `{"token":"` + token + `","actor":"dev@example.com"}`
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
}

func journalPath(root string) string { return filepath.Join(root, ".modernpath", "outage-journal") }

type keptFile struct {
	name  string
	entry map[string]any
}

// keptEntries reads the entry files at the top of the journal, in name order.
func keptEntries(t *testing.T, root string) []keptFile {
	t.Helper()
	dirents, err := os.ReadDir(journalPath(root))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []keptFile
	for _, d := range dirents {
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(journalPath(root), d.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatalf("%s is not a whole JSON entry: %v\n%s", d.Name(), err, raw)
		}
		out = append(out, keptFile{d.Name(), entry})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

func list(m map[string]any, key string) []any {
	v, _ := m[key].([]any)
	return v
}

// gateSnapshot writes a pulled gate's working-set snapshot; an empty pin is
// rendered as a file pulled before the pins were rendered (no line at all).
func gateSnapshot(t *testing.T, root, rel, gateID, kind, cfp, sfp string, at time.Time) {
	t.Helper()
	body := "## GATE " + gateID + " — " + gateID + "\n\n" +
		"- **Kind:** human / " + kind + "\n" +
		"- **State:** OPEN\n" +
		"- **Actor / evaluator:** opener human / answerer —\n" +
		"- **Prerequisites:** none\n" +
		"- **Fingerprint:** —\n"
	if cfp != "" {
		body += "- **Content fingerprint:** " + cfp + "\n"
		body += "- **Scope fingerprint:** " + firstNonEmpty(sfp, "—") + "\n"
	}
	body += "- **Application:** —\n- **Predecessor / successor:** — / —\n"
	snapshotFile(t, root, rel, gateID+" — working-set snapshot", "http://store", "rev-snap", at, body)
}

// keptOnce asserts one kept write: exit 0, the kept line, one new entry with
// its verb, record, sequence, the person who kept it and when, and no request.
func keptOnce(t *testing.T, root string, requests *atomic.Int32, code int, out, verb, record string, before int) map[string]any {
	t.Helper()
	if code != 0 {
		t.Fatalf("exit %d, want 0 — the write is kept on this machine; output:\n%s", code, out)
	}
	want := "Kept on this machine (" + itoa(before+1) + " waiting): " + verb + " " + record + ". It is recorded when the server is back."
	if !strings.Contains(out, want) {
		t.Errorf("want %q in the output:\n%s", want, out)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached the unavailable server; want 0", n)
	}
	entries := keptEntries(t, root)
	if len(entries) != before+1 {
		t.Fatalf("want %d journal entr(ies), found %d", before+1, len(entries))
	}
	e := entries[len(entries)-1].entry
	if e["verb"] != verb || e["record"] != record {
		t.Errorf("entry names %v %v, want %s %s", e["verb"], e["record"], verb, record)
	}
	if seq, _ := e["seq"].(float64); int(seq) != before+1 {
		t.Errorf("entry seq %v, want %d", e["seq"], before+1)
	}
	keptAt, err := time.Parse(time.RFC3339, str(e, "kept_at"))
	if err != nil || time.Since(keptAt) > time.Minute || time.Until(keptAt) > time.Minute {
		t.Errorf("kept_at %q must be the keep time", str(e, "kept_at"))
	}
	keeper := obj(e, "keeper")
	if str(keeper, "email") != "dev@example.com" {
		t.Errorf("the entry must name the person who kept it, got %v", e["keeper"])
	}
	if obj(e, "inputs") == nil {
		t.Errorf("the entry must hold the verb's inputs: %v", e)
	}
	return e
}

func itoa(n int) string {
	raw, _ := json.Marshal(n)
	return string(raw)
}

// AC1 — `factory evidence`, a single run and a --file of runs, keeps one entry
// each with the run id, ran_at and branch fixed, exits 0 and says so.
func TestEvidenceKept(t *testing.T) {
	t.Run("single run", func(t *testing.T) {
		root, requests := downWorkspace(t)
		code, out := runCLI(t, "factory", "evidence", "--pass", "REQ-K-1", "--log", "go test ./...")
		e := keptOnce(t, root, requests, code, out, "factory evidence", "REQ-K-1", 0)
		runs := list(obj(e, "fixed"), "runs")
		if len(runs) != 1 {
			t.Fatalf("want the one run's fixed values, got %v", e["fixed"])
		}
		run, _ := runs[0].(map[string]any)
		if !strings.HasPrefix(str(run, "run_id"), "RUN-") {
			t.Errorf("the run id must be fixed at keep, got %v", run)
		}
		if _, err := time.Parse(time.RFC3339, str(run, "ran_at")); err != nil {
			t.Errorf("ran_at must be fixed at keep, got %v", run)
		}
		if str(run, "branch") != "main" {
			t.Errorf("the branch must be fixed at keep, got %v", run)
		}
	})

	t.Run("--file of runs", func(t *testing.T) {
		root, requests := downWorkspace(t)
		file := filepath.Join(t.TempDir(), "runs.json")
		if err := os.WriteFile(file, []byte(`[{"pass":["REQ-K-1"],"log":"a"},{"fail":["REQ-K-2"],"role":"RED","log":"b"}]`), 0o644); err != nil {
			t.Fatal(err)
		}
		code, out := runCLI(t, "factory", "evidence", "--file", file)
		e := keptOnce(t, root, requests, code, out, "factory evidence", "REQ-K-1, REQ-K-2", 0)
		runs := list(obj(e, "fixed"), "runs")
		if len(runs) != 2 {
			t.Fatalf("one entry must hold both runs, got %v", e["fixed"])
		}
		ids := map[string]bool{}
		for _, r := range runs {
			run, _ := r.(map[string]any)
			ids[str(run, "run_id")] = true
			if str(run, "branch") != "main" || str(run, "ran_at") == "" {
				t.Errorf("each run's ran_at and branch are fixed at keep, got %v", run)
			}
		}
		if len(ids) != 2 {
			t.Errorf("the two runs must keep two run ids, got %v", runs)
		}

		// The --file run ids are derived from the entry (REQ-CROSS-445): the
		// same file kept again names the same runs.
		code, out = runCLI(t, "factory", "evidence", "--file", file)
		again := keptOnce(t, root, requests, code, out, "factory evidence", "REQ-K-1, REQ-K-2", 1)
		for _, r := range list(obj(again, "fixed"), "runs") {
			run, _ := r.(map[string]any)
			if !ids[str(run, "run_id")] {
				t.Errorf("a --file run kept again must keep its derived id; %s is new (first: %v)", str(run, "run_id"), ids)
			}
		}
	})

	t.Run("first contact with a server that is down", func(t *testing.T) {
		scaleOutageTimeouts(t, 200*time.Millisecond, time.Second)
		root := outageWorkspace(t, closedURL(t))
		gitRun(t, root, "init", "-q", "-b", "main")
		gitRun(t, root, "-c", "user.email=dev@example.com", "-c", "user.name=Dev", "commit", "-q", "--allow-empty", "-m", "init")
		code, out := runCLI(t, "factory", "evidence", "--pass", "REQ-K-1")
		if code != 0 || !strings.Contains(out, "Kept on this machine (1 waiting): factory evidence REQ-K-1.") {
			t.Fatalf("a confirmed outage keeps the run; exit %d:\n%s", code, out)
		}
		if n := len(keptEntries(t, root)); n != 1 {
			t.Errorf("want 1 journal entry, found %d", n)
		}
	})
}

// AC2 — each keeping verb writes one entry with its inputs, the person who
// kept it, the keep time, its fixed values and its basis, and exits 0.
// Feedback waits instead (USER:2026-10-04:feedback-waits).
func TestWritesKept(t *testing.T) {
	today := time.Now().UTC().Format("2006-01-02")
	cases := []struct {
		name, verb, record string
		prepare            func(t *testing.T, root string)
		args               []string
		check              func(t *testing.T, e map[string]any)
	}{
		{
			name: "author update", verb: "author update", record: "REQ-K-1",
			args: []string{"author", "update", "REQ-K-1", "--title", "A clearer title", "--expected-fingerprint", "fp-req-1"},
			check: func(t *testing.T, e map[string]any) {
				if str(obj(e, "basis"), "expected_fingerprint") != "fp-req-1" {
					t.Errorf("an edit is kept on its expected fingerprint, basis %v", e["basis"])
				}
			},
		},
		{
			name: "author backlog", verb: "author backlog", record: "BACKLOG-K-1",
			args: []string{"author", "backlog", "BACKLOG-K-1", "--kind", "backlog", "--title", "A discovery",
				"--observed", "seen during the outage", "--why-unrouted", "owner unclear"},
		},
		{
			name: "findings add with --aggregate", verb: "process findings add", record: "F-K-1",
			args: []string{"process", "findings", "add", "--scope", "epic:EPIC-K", "--id", "F-K-1",
				"--category", "correctness", "--severity", "major", "--body", "see packet §2", "--aggregate", "agg-k"},
			check: func(t *testing.T, e map[string]any) {
				if str(obj(e, "basis"), "aggregate_fingerprint") != "agg-k" {
					t.Errorf("a finding is kept on its packet aggregate, basis %v", e["basis"])
				}
			},
		},
		{
			name: "findings disposition with --expected-fingerprint", verb: "process findings disposition", record: "F-K-1",
			args: []string{"process", "findings", "disposition", "--id", "F-K-1", "--disposition", "REJECTED",
				"--ref", "out of scope", "--expected-fingerprint", "fp-f-1"},
			check: func(t *testing.T, e map[string]any) {
				if str(obj(e, "basis"), "expected_fingerprint") != "fp-f-1" {
					t.Errorf("a disposition is kept on its expected fingerprint, basis %v", e["basis"])
				}
			},
		},
		{
			name: "answer to an entry gate with both pins", verb: "factory answer", record: "ENTRY-EPIC-K",
			prepare: func(t *testing.T, root string) {
				// An older snapshot of the same gate carries other pins: the
				// newest one is the basis.
				gateSnapshot(t, root, ".modernpath/working-set/EPIC-K/gates-old.md", "ENTRY-EPIC-K", "approval_request",
					strings.Repeat("0", 64), strings.Repeat("1", 64), time.Now().Add(-3*time.Hour))
				gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-K.md", "ENTRY-EPIC-K", "approval_request",
					strings.Repeat("c", 64), strings.Repeat("5", 64), time.Now().Add(-time.Hour))
			},
			args: []string{"factory", "answer", "ENTRY-EPIC-K", "--text", "approve", "--options", "approve", "--source", "USER:2026-10-04:k"},
			check: func(t *testing.T, e map[string]any) {
				basis := obj(e, "basis")
				if str(basis, "content_fingerprint") != strings.Repeat("c", 64) || str(basis, "evaluated_scope_fingerprint") != strings.Repeat("5", 64) {
					t.Errorf("the answer is kept on the newest snapshot's two review pins, basis %v", e["basis"])
				}
				if str(obj(e, "fixed"), "source_tag") != "USER:2026-10-04:k" {
					t.Errorf("the answer's USER: source is fixed at keep, fixed %v", e["fixed"])
				}
			},
		},
		{
			name: "answer to a question gate with only the content pin", verb: "factory answer", record: "Q-K-1",
			prepare: func(t *testing.T, root string) {
				gateSnapshot(t, root, ".modernpath/working-set/Q-K-1.md", "Q-K-1", "question", strings.Repeat("d", 64), "", time.Now().Add(-time.Hour))
			},
			args: []string{"factory", "answer", "Q-K-1", "--text", "yes"},
			check: func(t *testing.T, e map[string]any) {
				basis := obj(e, "basis")
				if str(basis, "content_fingerprint") != strings.Repeat("d", 64) {
					t.Errorf("the answer is kept on the snapshot's content pin, basis %v", e["basis"])
				}
				if _, has := basis["evaluated_scope_fingerprint"]; has {
					t.Errorf("a gate with no scope pin keeps none, basis %v", e["basis"])
				}
				if str(obj(e, "fixed"), "source_tag") != "USER:"+today {
					t.Errorf("an answer with no --source fixes USER:<keep date>, fixed %v", e["fixed"])
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, requests := downWorkspace(t)
			if tc.prepare != nil {
				tc.prepare(t, root)
			}
			code, out := runCLI(t, tc.args...)
			e := keptOnce(t, root, requests, code, out, tc.verb, tc.record, 0)
			if str(obj(e, "keeper"), "subject") != "user-538" {
				t.Errorf("the entry names the signed-in user, got %v", e["keeper"])
			}
			if tc.check != nil {
				tc.check(t, e)
			}
		})
	}
}

// The gate block of a pulled snapshot renders the two review pins an answer
// is kept against.
func TestGateBlockRendersReviewPins(t *testing.T) {
	var b strings.Builder
	writeGateBlock(&b, map[string]any{
		"external_id": "ENTRY-EPIC-K", "title": "Enter", "kind": "approval_request", "state": "open",
		"content_fingerprint": strings.Repeat("c", 64), "evaluated_scope_fingerprint": strings.Repeat("5", 64),
	})
	writeGateBlock(&b, map[string]any{"external_id": "Q-K-1", "title": "Ask", "kind": "question", "state": "open",
		"content_fingerprint": strings.Repeat("d", 64)})
	out := b.String()
	for _, want := range []string{
		"- **Content fingerprint:** " + strings.Repeat("c", 64) + "\n",
		"- **Scope fingerprint:** " + strings.Repeat("5", 64) + "\n",
		"- **Content fingerprint:** " + strings.Repeat("d", 64) + "\n",
		"- **Scope fingerprint:** —\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("want %q in the gate block:\n%s", want, out)
		}
	}
}

// AC3 — an offline push leaves the edited files as they are and keeps one push
// naming the piece: --piece when given, else the piece in WORK-SELECTION.md.
func TestOfflinePushKept(t *testing.T) {
	edited := func(t *testing.T, root, scope string) []byte {
		t.Helper()
		rel := filepath.Join(".modernpath", "working-set", scope, scope+".md")
		return snapshotFile(t, root, rel, scope+" — working-set (authoring)", "http://store", "rev-snap", time.Now().Add(-time.Hour),
			"## title\n```authoring:editable\nEdited while the server was down\n```\n")
	}
	selection := func(t *testing.T, root, scope string) {
		t.Helper()
		snapshotFile(t, root, ".modernpath/working-set/WORK-SELECTION.md", "WORK-SELECTION — working-set snapshot", "http://store", "rev-snap",
			time.Now().Add(-time.Hour), "## Work selection\n\n### Current selection\n\n- **Selected scope:** "+scope+"\n- **Members:** REQ-S-1\n")
	}

	t.Run("the piece in WORK-SELECTION.md", func(t *testing.T) {
		root, requests := downWorkspace(t)
		selection(t, root, "EPIC-S")
		want := edited(t, root, "EPIC-S")

		code, out := runCLI(t, "working-set", "push")
		e := keptOnce(t, root, requests, code, out, "working-set push", "EPIC-S", 0)
		if str(obj(e, "inputs"), "piece") != "EPIC-S" {
			t.Errorf("the kept push names the piece, inputs %v", e["inputs"])
		}
		unchanged(t, root, filepath.Join(".modernpath", "working-set", "EPIC-S", "EPIC-S.md"), want)

		// The same piece pushed again is the same kept push.
		code, out = runCLI(t, "working-set", "push")
		if code != 0 {
			t.Fatalf("a second offline push exits 0; exit %d:\n%s", code, out)
		}
		if n := len(keptEntries(t, root)); n != 1 {
			t.Errorf("one kept push per piece; found %d entries", n)
		}
	})

	t.Run("--piece names another held piece", func(t *testing.T) {
		root, requests := downWorkspace(t)
		selection(t, root, "EPIC-Y")
		edited(t, root, "EPIC-Y")
		want := edited(t, root, "EPIC-X")

		code, out := runCLI(t, "working-set", "push", "--piece", "EPIC-X")
		e := keptOnce(t, root, requests, code, out, "working-set push", "EPIC-X", 0)
		if str(obj(e, "inputs"), "piece") != "EPIC-X" {
			t.Errorf("--piece names the kept push's piece, inputs %v", e["inputs"])
		}
		unchanged(t, root, filepath.Join(".modernpath", "working-set", "EPIC-X", "EPIC-X.md"), want)
	})
}

// AC4 — a second kept edit of the same record waits on the first.
func TestSecondEditWaits(t *testing.T) {
	root, requests := downWorkspace(t)
	update := func(id, fp string) (int, string) {
		return runCLI(t, "author", "update", id, "--title", "t "+fp, "--expected-fingerprint", fp)
	}
	code, out := update("REQ-K-1", "fp-1")
	keptOnce(t, root, requests, code, out, "author update", "REQ-K-1", 0)
	code, out = update("REQ-K-1", "fp-2")
	second := keptOnce(t, root, requests, code, out, "author update", "REQ-K-1", 1)
	code, out = update("REQ-K-2", "fp-3")
	other := keptOnce(t, root, requests, code, out, "author update", "REQ-K-2", 2)

	first := strings.TrimSuffix(keptEntries(t, root)[0].name, ".json")
	if str(second, "waits_on") != first {
		t.Errorf("the second edit of REQ-K-1 waits on %s, got waits_on %q", first, str(second, "waits_on"))
	}
	if w := str(other, "waits_on"); w != "" {
		t.Errorf("an edit of another record waits on nothing, got %q", w)
	}
}

// AC5 — the writes that cannot be kept say they wait for the server, keep
// nothing and exit 75.
func TestWaitingVerbsSayTheyWait(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, root string) []string // returns extra args
		args    []string
	}{
		{name: "working-set select", args: []string{"working-set", "select", "EPIC-W"}},
		{name: "author apply", args: []string{"author", "apply"}, prepare: func(t *testing.T, root string) []string {
			plan := filepath.Join(root, "plan.json")
			if err := os.WriteFile(plan, []byte(`{"epic":{"id":"EPIC-W","title":"W"}}`), 0o644); err != nil {
				t.Fatal(err)
			}
			return []string{"--file", plan}
		}},
		{name: "process enter", args: []string{"process", "enter", "EPIC-W"}},
		{name: "process advance", args: []string{"process", "advance", "REQ-W-1"}},
		{name: "process complete", args: []string{"process", "complete", "EPIC-W"}},
		{name: "answer with no content pin", args: []string{"factory", "answer", "ENTRY-EPIC-W", "--text", "approve"},
			prepare: func(t *testing.T, root string) []string {
				gateSnapshot(t, root, ".modernpath/working-set/ENTRY-EPIC-W.md", "ENTRY-EPIC-W", "approval_request", "", "", time.Now().Add(-time.Hour))
				return nil
			}},
		{name: "push with neither --piece nor WORK-SELECTION.md", args: []string{"working-set", "push"}},
		{name: "findings add without --aggregate", args: []string{"process", "findings", "add", "--scope", "epic:EPIC-W",
			"--id", "F-W-1", "--category", "correctness", "--severity", "major", "--body", "b"}},
		{name: "feedback", args: []string{"feedback", "a tooling defect"}, prepare: func(t *testing.T, root string) []string {
			// USER:2026-10-04:feedback-waits — feedback waits for the server
			// even with a ModernPath credential.
			signedInAs(t, root, map[string]any{
				"iss": zitadel.ProdProfile.Issuer, "sub": "user-538", "email": "dev@example.com",
				"urn:modernpath:token:v2": map[string]any{"org_id": feedbackOrganizationID},
			})
			return nil
		}},
		{name: "findings disposition without --expected-fingerprint", args: []string{"process", "findings", "disposition",
			"--id", "F-W-1", "--disposition", "REJECTED", "--ref", "out of scope", "--scope", "epic:EPIC-W", "--from", "OPEN"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, requests := downWorkspace(t)
			args := tc.args
			if tc.prepare != nil {
				args = append(append([]string(nil), args...), tc.prepare(t, root)...)
			}
			since, _ := time.Parse(time.RFC3339, str(readAvailability(t, root), "since"))

			code, out := runCLI(t, args...)

			if code != exitTempFail {
				t.Errorf("exit %d, want %d; output:\n%s", code, exitTempFail, out)
			}
			want := "Waits for the ModernPath server (unavailable since " + since.Local().Format("15:04") + "); " + keptWaitLine
			if !strings.Contains(out, want) {
				t.Errorf("want %q in the output:\n%s", want, out)
			}
			if n := len(keptEntries(t, root)); n != 0 {
				t.Errorf("a waiting write keeps nothing; found %d journal entries", n)
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("%d request(s) reached the unavailable server; want 0", n)
			}
		})
	}
}

// AC6 — a replay holding the replay lock never holds up a keep: the entry is
// written at once under the next sequence number.
func TestKeepWhileReplayLocked(t *testing.T) {
	root, requests := downWorkspace(t)
	code, out := runCLI(t, "author", "backlog", "BACKLOG-K-1", "--kind", "backlog", "--title", "A discovery",
		"--observed", "o", "--why-unrouted", "w")
	keptOnce(t, root, requests, code, out, "author backlog", "BACKLOG-K-1", 0)
	if err := os.WriteFile(filepath.Join(journalPath(root), "replay.lock"), []byte(`{"pid":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	code, out = runCLI(t, "author", "update", "REQ-K-1", "--title", "t", "--expected-fingerprint", "fp-1")
	took := time.Since(started)

	keptOnce(t, root, requests, code, out, "author update", "REQ-K-1", 1)
	if took > time.Second {
		t.Errorf("a keep waits on no replay lock; took %s", took)
	}
}

// AC7 — `outage status` lists each kept write with its verb, record, keep time
// and what it waits on, and the entries replay could not record.
func TestOutageStatusLists(t *testing.T) {
	root, requests := downWorkspace(t)
	for _, args := range [][]string{
		{"factory", "evidence", "--pass", "REQ-K-1"},
		{"author", "update", "REQ-K-1", "--title", "a", "--expected-fingerprint", "fp-1"},
		{"author", "update", "REQ-K-1", "--title", "b", "--expected-fingerprint", "fp-2"},
	} {
		if code, out := runCLI(t, args...); code != 0 {
			t.Fatalf("%v: exit %d:\n%s", args, code, out)
		}
	}
	entries := keptEntries(t, root)
	if len(entries) != 3 {
		t.Fatalf("want 3 kept writes, found %d", len(entries))
	}
	// An entry replay moved aside with the server's reason (REQ-CROSS-539).
	conflicted := filepath.Join(journalPath(root), "conflicted")
	if err := os.MkdirAll(conflicted, 0o755); err != nil {
		t.Fatal(err)
	}
	moved := map[string]any{"seq": 9, "verb": "factory answer", "record": "ENTRY-EPIC-Z", "kept_at": time.Now().UTC().Format(time.RFC3339),
		"keeper": map[string]any{"email": "dev@example.com"}, "inputs": map[string]any{}, "reason": "the gate changed after the answer was given"}
	raw, _ := json.Marshal(moved)
	if err := os.WriteFile(filepath.Join(conflicted, "000009-factory-answer.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out := runCLI(t, "outage", "status")

	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	for _, k := range entries {
		keptAt, _ := time.Parse(time.RFC3339, str(k.entry, "kept_at"))
		for _, want := range []string{str(k.entry, "verb") + " " + str(k.entry, "record"), keptAt.Local().Format("15:04")} {
			if !strings.Contains(out, want) {
				t.Errorf("want %q for %s in the listing:\n%s", want, k.name, out)
			}
		}
	}
	if first := strings.TrimSuffix(entries[1].name, ".json"); !strings.Contains(out, "waits on "+first) {
		t.Errorf("the second edit must say it waits on %s:\n%s", first, out)
	}
	for _, want := range []string{"factory answer ENTRY-EPIC-Z", "the gate changed after the answer was given"} {
		if !strings.Contains(out, want) {
			t.Errorf("want the conflicted entry %q in the listing:\n%s", want, out)
		}
	}
	for _, word := range []string{"keeper", "pending push"} {
		if strings.Contains(strings.ToLower(out), word) {
			t.Errorf("the listing says %q (D12 wording):\n%s", word, out)
		}
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("outage status sent %d request(s); it reads only this machine", n)
	}

	sub := map[string]string{"agent_id": "abc123"}
	if got := processGateHookPayload(hookPayload(sub, "modernpath outage status")); got != "{}" {
		t.Errorf("a delegated pass may read the kept writes: %s", got)
	}
	if got := processGateHookPayload(hookPayload(sub, "modernpath outage replay")); got == "{}" {
		t.Errorf("a delegated pass may not replay kept writes")
	}
}

// AC5 (CODE-OUTAGE-1-3) — a findings batch from --file stops on the outage:
// it keeps nothing, says it waits and exits 75, rather than failing each row.
func TestBatchFindingsWaitDuringOutage(t *testing.T) {
	cases := []struct {
		name string
		file string
		args []string
	}{
		{name: "findings add --file", args: []string{"process", "findings", "add"},
			file: `[{"id":"F-W-1","scope":"epic:EPIC-W","category":"correctness","severity":"major","body":"b"},
			        {"id":"F-W-2","scope":"epic:EPIC-W","category":"correctness","severity":"minor","body":"c"}]`},
		{name: "findings disposition --file", args: []string{"process", "findings", "disposition"},
			file: `[{"id":"F-W-1","scope":"epic:EPIC-W","from":"OPEN","disposition":"REJECTED","ref":"out of scope"},
			        {"id":"F-W-2","scope":"epic:EPIC-W","from":"OPEN","disposition":"REJECTED","ref":"out of scope"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, requests := downWorkspace(t)
			file := filepath.Join(root, "batch.json")
			if err := os.WriteFile(file, []byte(tc.file), 0o644); err != nil {
				t.Fatal(err)
			}
			since, _ := time.Parse(time.RFC3339, str(readAvailability(t, root), "since"))

			code, out := runCLI(t, append(append([]string(nil), tc.args...), "--file", file)...)

			if code != exitTempFail {
				t.Errorf("exit %d, want %d; output:\n%s", code, exitTempFail, out)
			}
			want := "Waits for the ModernPath server (unavailable since " + since.Local().Format("15:04") + "); " + keptWaitLine
			if !strings.Contains(out, want) {
				t.Errorf("want %q in the output:\n%s", want, out)
			}
			if strings.Contains(out, "F-W-2") {
				t.Errorf("the batch stops at the outage; it does not go on to the next row:\n%s", out)
			}
			if n := len(keptEntries(t, root)); n != 0 {
				t.Errorf("a waiting batch keeps nothing; found %d journal entries", n)
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("%d request(s) reached the unavailable server; want 0", n)
			}
		})
	}
}

// AC4 (CODE-OUTAGE-1-5) — a keep takes its sequence number only by reserving
// it exclusively, and resolves what it waits on under that reservation: a
// number another keep has reserved (its <seq>.seq marker, written before its
// entry) is never taken, and an edit of the same record reserved earlier is
// what a second edit waits on.
func TestKeepReservesItsSequence(t *testing.T) {
	root, requests := downWorkspace(t)
	if err := os.MkdirAll(journalPath(root), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := `{"verb":"author update","record":"REQ-K-1"}`
	if err := os.WriteFile(filepath.Join(journalPath(root), "000001.seq"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out := runCLI(t, "author", "update", "REQ-K-1", "--title", "t", "--expected-fingerprint", "fp-2")

	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("%d request(s) reached the unavailable server; want 0", n)
	}
	entries := keptEntries(t, root)
	if len(entries) != 1 {
		t.Fatalf("want one kept entry, found %v", entries)
	}
	if seq, _ := entries[0].entry["seq"].(float64); int(seq) != 2 || !strings.HasPrefix(entries[0].name, "000002-") {
		t.Errorf("a reserved number is never taken: want sequence 2, got %v in %s", entries[0].entry["seq"], entries[0].name)
	}
	if w := str(entries[0].entry, "waits_on"); w != "000001-author-update" {
		t.Errorf("the edit waits on the edit of the same record reserved before it, got waits_on %q", w)
	}
}

// AC4 (CODE-OUTAGE-1-5) — keeps of different verbs at the same moment each
// take their own sequence number, and leave no reservation behind.
func TestConcurrentKeepsTakeDistinctNumbers(t *testing.T) {
	root := t.TempDir()
	verbs := []string{"author update", "author backlog", "factory evidence", "factory answer", "working-set push", "process findings add"}
	const keeps = 24
	start := make(chan struct{})
	errs := make(chan error, keeps)
	var wg sync.WaitGroup
	for i := 0; i < keeps; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			e := keptEntry{Verb: verbs[i%len(verbs)], Record: "REQ-C-" + itoa(i)}
			_, err := writeJournalEntry(root, &e)
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a keep failed: %v", err)
		}
	}
	entries := keptEntries(t, root)
	if len(entries) != keeps {
		t.Fatalf("want %d kept entries, found %d", keeps, len(entries))
	}
	seen := map[int]string{}
	for _, k := range entries {
		seq := int(k.entry["seq"].(float64))
		if other, dup := seen[seq]; dup {
			t.Errorf("%s and %s share sequence number %d", other, k.name, seq)
		}
		seen[seq] = k.name
	}
	dirents, err := os.ReadDir(journalPath(root))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirents {
		if !strings.HasSuffix(d.Name(), ".json") {
			t.Errorf("a finished keep leaves nothing but its entry; found %s", d.Name())
		}
	}
}
