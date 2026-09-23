package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

// REQ-CROSS-387 (EPIC-CLI-019) — `modernpath feedback "<line>"` files a
// tooling gap as a BACKLOG-TOOL-<n> record with the CLI build captured,
// attaches the previous user command with --last, and falls back to the
// interim file only when no token is stored or the server is unreachable.

func feedbackServer(t *testing.T, existing []any, authorStatus int) (*httptest.Server, *map[string]any) {
	t.Helper()
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/backlog", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"backlog": existing}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		if authorStatus != 0 && authorStatus != 200 {
			w.WriteHeader(authorStatus)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": "title: is too long"}})
			return
		}
		record, _ := got["record"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"backlog": map[string]any{"external_id": record["external_id"], "disposition": "OPEN", "fingerprint": strings.Repeat("e", 64)},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &got
}

func toolRows(ids ...string) []any {
	var rows []any
	for _, id := range ids {
		rows = append(rows, map[string]any{"external_id": id, "kind": "tooling", "title": id})
	}
	return rows
}

// (a) The next free BACKLOG-TOOL id is allocated from the read; the record is
// a tooling create carrying the line and the build; the id is printed.
func TestFeedbackFilesAToolingRecordWithTheBuildCaptured(t *testing.T) {
	srv, got := feedbackServer(t, toolRows("BACKLOG-TOOL-1", "BACKLOG-TOOL-2"), 200)
	cobraWorkspace(t, srv)

	out, err := runRoot(t, "feedback", "process check prints only a check name")
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	if (*got)["action"] != "create" || record["kind"] != "backlog" || record["backlog_kind"] != "tooling" {
		t.Fatalf("want a tooling backlog create, posted %v", *got)
	}
	if record["external_id"] != "BACKLOG-TOOL-3" {
		t.Errorf("id = %v, want BACKLOG-TOOL-3 (the next after the two served)", record["external_id"])
	}
	if record["title"] != "process check prints only a check name" {
		t.Errorf("title must be the line, got %v", record["title"])
	}
	meta, _ := record["metadata"].(map[string]any)
	if meta["cli_version"] != Version || meta["server_contract"] == nil || meta["filed_at"] == nil {
		t.Errorf("metadata must capture the build, the contract and the time, got %v", meta)
	}
	if !strings.Contains(out, "BACKLOG-TOOL-3") {
		t.Errorf("the id must be printed:\n%s", out)
	}
}

// (b) --last attaches the previous user-run command and its output.
func TestFeedbackLastAttachesThePreviousCommand(t *testing.T) {
	srv, got := feedbackServer(t, nil, 200)
	cobraWorkspace(t, srv)
	previous := historyEntry{Argv: []string{"process", "check", "--phase", "plan"}, Started: time.Now(), Exit: 1, Output: "FAIL canonical_sections\n"}
	writeHistory(t, ".", previous)

	if _, err := runRoot(t, "feedback", "--last", "process check prints only a check name"); err != nil {
		t.Fatalf("feedback --last: %v", err)
	}
	record, _ := (*got)["record"].(map[string]any)
	observation, _ := record["observation"].(string)
	for _, want := range []string{"process check --phase plan", "exit 1", "FAIL canonical_sections"} {
		if !strings.Contains(observation, want) {
			t.Errorf("observation must carry %q, got %q", want, observation)
		}
	}
}

// (c) No stored token: the line lands in the interim file and the output
// names it; exit 0.
func TestFeedbackWithoutATokenAppendsToTheInterimFile(t *testing.T) {
	srv, got := feedbackServer(t, nil, 200)
	cobraWorkspace(t, srv)
	_ = os.Remove(filepath.Join(".modernpath", "auth.json"))

	out, err := runRoot(t, "feedback", "the pull refuses the release gate")
	if err != nil {
		t.Fatalf("offline feedback must succeed: %v", err)
	}
	raw, readErr := os.ReadFile(filepath.Join("process", "tooling-gaps.md"))
	if readErr != nil {
		t.Fatalf("the interim file must be written: %v", readErr)
	}
	if !strings.Contains(string(raw), "the pull refuses the release gate") {
		t.Errorf("the row must carry the line:\n%s", raw)
	}
	if !strings.Contains(out, "tooling-gaps.md") {
		t.Errorf("the output must name the file:\n%s", out)
	}
	if *got != nil {
		t.Errorf("nothing may be posted without a token, posted %v", *got)
	}
}

// (c″) REQ-CROSS-434: the same fallback for a workspace that names no
// system. `feedback` promises the interim file "without a stored credential,
// or when the server cannot be reached", but an unbound workspace is a plain
// error — so the one command that exists to surface a gap exited on one,
// losing the line, in exactly the situation a newcomer meets it.
func TestFeedbackWithoutABindingAppendsToTheInterimFile(t *testing.T) {
	for _, c := range []struct {
		name, config, want string
	}{
		{"config names no system", `{"api_url":"http://127.0.0.1:1"}`, "system"},
		{"no config at all", "", "connect"},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, got := feedbackServer(t, nil, 200)
			cobraWorkspace(t, srv)
			if c.config == "" {
				_ = os.RemoveAll(".modernpath")
			} else {
				_ = os.WriteFile(filepath.Join(".modernpath", "config.json"), []byte(c.config), 0o644)
			}

			// The "why" rides printWarning, which writes to color.Error — a
			// writer captureOutput does not swap. Capture it here so the whole
			// message the operator sees is under test.
			var warnings bytes.Buffer
			savedErr := color.Error
			color.Error = &warnings
			out, err := runRoot(t, "feedback", "the gap met before the workspace was bound")
			color.Error = savedErr

			if err != nil {
				t.Fatalf("an unbound workspace must still keep the line: %v", err)
			}
			raw, readErr := os.ReadFile(filepath.Join("process", "tooling-gaps.md"))
			if readErr != nil {
				t.Fatalf("the interim file must be written: %v", readErr)
			}
			if !strings.Contains(string(raw), "the gap met before the workspace was bound") {
				t.Errorf("the row must carry the line:\n%s", raw)
			}
			if !strings.Contains(out, "tooling-gaps.md") {
				t.Errorf("the output must name the file:\n%s", out)
			}
			said := out + warnings.String()
			if !strings.Contains(said, c.want) {
				t.Errorf("the output must say why the store was not written, got:\n%s", said)
			}
			if *got != nil {
				t.Errorf("nothing may be posted by an unbound workspace, posted %v", *got)
			}
		})
	}
}

// (c‴) REQ-CROSS-434: a credential the server rejects, and a binding to a
// system this user cannot reach, leave the store unwritable just as a missing
// credential or binding does. Both are fixed by another command (`auth`,
// `factory connect`), not by retrying, so the line goes to the interim file
// instead of being lost with the exit.
func TestFeedbackKeepsTheLineWhenTheStoreCannotBeWritten(t *testing.T) {
	for _, c := range []struct {
		name, want, notWant string
		setup               func(t *testing.T)
	}{
		{"the server rejects the credential", "rejected the session token", "", func(t *testing.T) {
			mux := http.NewServeMux()
			mux.HandleFunc("/api/v1/sync/backlog", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			cobraWorkspace(t, srv)
		}},
		// REQ-CROSS-434 (F-CLI024-R1-05): the same 401 on the create, after a
		// read that succeeded.
		{"the server rejects the credential on the create", "rejected the session token", "", func(t *testing.T) {
			srv, _ := feedbackServer(t, nil, http.StatusUnauthorized)
			cobraWorkspace(t, srv)
		}},
		{"the bound system is not reachable", "bound system is not reachable", "no workspace binding", func(t *testing.T) {
			srv, _ := feedbackServer(t, nil, 200)
			cobraWorkspace(t, srv) // bound to system 1
			stubSystems(t)         // this user reaches system 7 only
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.setup(t)

			var warnings bytes.Buffer
			savedErr := color.Error
			color.Error = &warnings
			out, err := runRoot(t, "feedback", "a gap the store cannot take")
			color.Error = savedErr

			if err != nil {
				t.Fatalf("the line must be kept, not lost with an exit: %v", err)
			}
			raw, readErr := os.ReadFile(filepath.Join("process", "tooling-gaps.md"))
			if readErr != nil {
				t.Fatalf("the interim file must be written: %v", readErr)
			}
			if !strings.Contains(string(raw), "a gap the store cannot take") {
				t.Errorf("the row must carry the line:\n%s", raw)
			}
			said := out + warnings.String()
			if !strings.Contains(said, c.want) {
				t.Errorf("the output must say why the store was not written (%q), got:\n%s", c.want, said)
			}
			if c.notWant != "" && strings.Contains(said, c.notWant) {
				t.Errorf("the output must not say %q, got:\n%s", c.notWant, said)
			}
		})
	}
}

// (c′) A refused create is reported verbatim and fails; no row is written.
func TestFeedbackReportsARefusedCreate(t *testing.T) {
	srv, _ := feedbackServer(t, nil, 422)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "feedback", "a line the server refuses")
	if err == nil {
		t.Fatal("a refused create must fail")
	}
	if !strings.Contains(err.Error(), "title: is too long") {
		t.Errorf("the refusal must be verbatim, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join("process", "tooling-gaps.md")); statErr == nil {
		t.Error("a refusal must not fall back to the interim file")
	}
}

// writeHistory appends entries to the workspace history file as the recorder
// would.
func writeHistory(t *testing.T, root string, entries ...historyEntry) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, ".modernpath", "cli-history.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, e := range entries {
		blob, _ := json.Marshal(e)
		f.Write(append(blob, '\n'))
	}
}

// A server that answers — a 500 on the backlog read here — is not
// unreachable: the error is returned and nothing lands in the interim file
// (F-CLI019-PR-05).
func TestFeedbackReportsARefusedBacklogRead(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/backlog", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": "backlog read exploded"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "feedback", "a line behind a broken read")
	if err == nil {
		t.Fatal("a refused read must fail")
	}
	if !strings.Contains(err.Error(), "backlog read exploded") {
		t.Errorf("the refusal must be verbatim, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join("process", "tooling-gaps.md")); statErr == nil {
		t.Error("a refusal must not fall back to the interim file")
	}
}

// REQ-CROSS-410 (EPIC-CLI-020): --last shows the entry it is about to attach
// before the record is written; --ref <n> picks the n-th most recent; a
// value beyond the recorded count is refused naming how many exist.
func seedThreeCommands(t *testing.T) {
	t.Helper()
	writeHistory(t, ".",
		historyEntry{Argv: []string{"factory", "status"}, Started: time.Now().Add(-3 * time.Minute), Exit: 0, Output: "workspace: /w\n"},
		historyEntry{Argv: []string{"factory", "release", "activate", "v1"}, Started: time.Now().Add(-2 * time.Minute), Exit: 1, Output: "author refused (server 403): pin_required\n"},
		historyEntry{Argv: []string{"install", "--check"}, Started: time.Now().Add(-time.Minute), Exit: 0, Output: "✓ installed process matches this CLI\n"},
	)
}

// S1 — the third entry is shown before the record id, and attached.
func TestFeedbackLastShowsTheEntryBeforeWriting(t *testing.T) {
	srv, got := feedbackServer(t, nil, 200)
	cobraWorkspace(t, srv)
	seedThreeCommands(t)

	out, err := runRoot(t, "feedback", "--last", "install check said nothing about the gate")
	if err != nil {
		t.Fatalf("feedback --last: %v", err)
	}
	shown := strings.Index(out, "modernpath install --check (exit 0)")
	filed := strings.Index(out, "BACKLOG-TOOL-1")
	if shown < 0 || filed < 0 || shown > filed {
		t.Fatalf("the attached entry must be shown before the record id:\n%s", out)
	}
	if !strings.Contains(out, "✓ installed process matches this CLI") {
		t.Fatalf("the first output line of the entry must be shown:\n%s", out)
	}
	record, _ := (*got)["record"].(map[string]any)
	observation, _ := record["observation"].(string)
	if !strings.Contains(observation, "install --check") {
		t.Fatalf("the record must carry the shown entry, got %q", observation)
	}
}

// S2 — --ref 2 attaches the second most recent; --ref 5 refuses naming three.
func TestFeedbackRefPicksAnEarlierCommand(t *testing.T) {
	srv, got := feedbackServer(t, nil, 200)
	cobraWorkspace(t, srv)
	seedThreeCommands(t)

	out, err := runRoot(t, "feedback", "--ref", "2", "activation refused without saying whose PIN")
	if err != nil {
		t.Fatalf("feedback --ref 2: %v", err)
	}
	if !strings.Contains(out, "modernpath factory release activate v1 (exit 1)") {
		t.Fatalf("--ref 2 must show the second most recent entry:\n%s", out)
	}
	record, _ := (*got)["record"].(map[string]any)
	observation, _ := record["observation"].(string)
	if !strings.Contains(observation, "factory release activate v1") || !strings.Contains(observation, "pin_required") {
		t.Fatalf("the record must carry the chosen entry, got %q", observation)
	}

	*got = nil
	_, err = runRoot(t, "feedback", "--ref", "5", "nothing")
	if err == nil || !strings.Contains(err.Error(), "3") {
		t.Fatalf("--ref 5 over three entries must refuse naming the count, got %v", err)
	}
	if *got != nil {
		t.Fatal("a refused --ref must write nothing")
	}
}

// S3 — without --last nothing is attached and no history line is printed.
func TestFeedbackWithoutLastPrintsNoHistory(t *testing.T) {
	srv, got := feedbackServer(t, nil, 200)
	cobraWorkspace(t, srv)
	seedThreeCommands(t)

	out, err := runRoot(t, "feedback", "a plain line")
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if strings.Contains(out, "install --check") || strings.Contains(out, "Previous command") {
		t.Fatalf("no history line without --last:\n%s", out)
	}
	record, _ := (*got)["record"].(map[string]any)
	if record["observation"] != "a plain line" {
		t.Fatalf("the observation must be the line alone, got %v", record["observation"])
	}
}
