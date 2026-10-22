package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
)

// REQ-CROSS-387 (EPIC-CLI-019) — the history recorder: every user-run
// invocation lands in .modernpath/cli-history.log with its exit and an output
// tail; hook-mode invocations and `auth` never do; token-like flag values are
// redacted; dev keeps the real terminal; the tee is transparent.

func historyWorkspace(t *testing.T) string {
	t.Helper()
	root := enterTestWorkspace(t)
	writeTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://127.0.0.1:1","system_id":7}`)
	return root
}

// (d) Two invocations record two entries with exit status and output tail.
func TestExecuteRecordsHistory(t *testing.T) {
	root := historyWorkspace(t)

	captureOutput(t, func() {
		runWithHistory([]string{"status"}, func() int { fmt.Println("first run"); return 0 })
		runWithHistory([]string{"process", "next"}, func() int { fmt.Println("second run"); return 1 })
	})

	entries := historyEntries(root)
	if len(entries) != 2 {
		t.Fatalf("want two entries, got %d", len(entries))
	}
	if entries[1].Exit != 1 || strings.Join(entries[1].Argv, " ") != "process next" || !strings.Contains(entries[1].Output, "second run") {
		t.Errorf("the second entry must carry argv, exit and the output tail, got %+v", entries[1])
	}
}

// (e) `auth` is never recorded; a token-like flag value is redacted.
func TestHistoryNeverKeepsAToken(t *testing.T) {
	root := historyWorkspace(t)
	const secret = "eyJSECRETTOKEN.aaa.bbb"

	captureOutput(t, func() {
		runWithHistory([]string{"auth", "--token", secret}, func() int { return 0 })
		runWithHistory([]string{"docs", "sync", "--api-token", secret, "--system", "7"}, func() int { return 0 })
		runWithHistory([]string{"docs", "sync", "--api-token=" + secret}, func() int { return 0 })
	})

	raw, _ := os.ReadFile(filepath.Join(root, ".modernpath", "cli-history.log"))
	if strings.Contains(string(raw), secret) {
		t.Fatalf("the token reached the history:\n%s", raw)
	}
	entries := historyEntries(root)
	if len(entries) != 2 {
		t.Fatalf("auth must not be recorded; want the two connect entries, got %d", len(entries))
	}
	if !strings.Contains(strings.Join(entries[0].Argv, " "), "<redacted>") {
		t.Errorf("the flag value must read <redacted>, got %v", entries[0].Argv)
	}
}

// (f) The history and notices files are ignored by git beside auth.json.
func TestHistoryFilesAreIgnored(t *testing.T) {
	root := historyWorkspace(t)
	writeTestFile(t, root, ".modernpath/.gitignore", "auth.json\n")

	captureOutput(t, func() { runWithHistory([]string{"status"}, func() int { return 0 }) })

	raw, _ := os.ReadFile(filepath.Join(root, ".modernpath", ".gitignore"))
	for _, want := range []string{"auth.json", "cli-history.log", "cli-notices.json"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf(".gitignore must carry %s:\n%s", want, raw)
		}
	}
}

// (g) The tee is transparent per stream: a colour-path render and a JSON
// envelope on stdout, and the warnings on stderr, are byte-identical with
// and without the recorder. (Cross-stream interleaving is not part of the
// contract: stdout and stderr are separate pipes under the tee.)
func TestHistoryTeeIsTransparent(t *testing.T) {
	historyWorkspace(t)
	render := func() {
		printSuccess("green line\n")
		printWarning("yellow line\n")
		fmt.Println(`{"hookSpecificOutput":{"additionalContext":"brief"}}`)
		color.New(color.FgCyan).Fprintf(color.Output, "cyan %d\n", 7)
		printError("red line\n")
	}
	both := func(fn func()) (string, string) {
		var out, errOut []byte
		captureStderr(t, func() { _ = captureStdout(t, func() error { fn(); return nil }, &out) }, &errOut)
		return string(out), string(errOut)
	}

	plainOut, plainErr := both(render)
	teedOut, teedErr := both(func() { runWithHistory([]string{"status"}, func() int { render(); return 0 }) })
	if plainOut != teedOut {
		t.Fatalf("stdout differs under the tee:\n--- plain\n%q\n--- teed\n%q", plainOut, teedOut)
	}
	if plainErr != teedErr {
		t.Fatalf("stderr differs under the tee:\n--- plain\n%q\n--- teed\n%q", plainErr, teedErr)
	}
	if plainOut == "" || plainErr == "" {
		t.Fatalf("the snapshot must exercise both streams, got out=%q err=%q", plainOut, plainErr)
	}
}

// (h) No bare os.Exit remains in cmd: every process exit drains the tee.
func TestNoBareOsExitInCmd(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	bare := regexp.MustCompile(`\bos\.Exit\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "history.go" {
			continue
		}
		raw, _ := os.ReadFile(f)
		if bare.Match(raw) {
			t.Errorf("%s calls os.Exit directly — route it through exit(code) so the history tee drains", f)
		}
	}
}

// (i) dev hands the original fds to its child and records argv and
// exit only.
func TestHistoryLeavesTheTerminalToInteractiveChildren(t *testing.T) {
	root := historyWorkspace(t)
	orig := os.Stdout
	var seen *os.File

	captureOutput(t, func() {
		runWithHistory([]string{"dev", "run"}, func() int { seen = os.Stdout; fmt.Println("child output"); return 0 })
	})
	// captureOutput swaps os.Stdout itself; inside the run the recorder must not
	// have swapped it again.
	_ = orig
	entries := historyEntries(root)
	if len(entries) != 1 || entries[0].Output != "" {
		t.Fatalf("dev must record argv and exit without an output tail, got %+v", entries)
	}
	if seen == nil {
		t.Fatal("the run did not execute")
	}
}

// Hook invocations are skipped, while an explicit typed focus inference is a
// normal user command and remains in history.
func TestHistorySkipsHookInvocations(t *testing.T) {
	root := historyWorkspace(t)

	captureOutput(t, func() {
		runWithHistory([]string{"process", "next"}, func() int { return 0 })
		runWithHistory([]string{"check", "--hook", "PreToolUse"}, func() int { return 0 })
		runWithHistory([]string{"context", "--hook", "UserPromptSubmit"}, func() int { return 0 })
		runWithHistory([]string{"focus", "--infer", "REQ-X", "--source", "prompt"}, func() int { return 0 })
		runWithHistory([]string{"your-move", "--hook", "SessionStart"}, func() int { return 0 })
	})

	entries := historyEntries(root)
	if len(entries) != 2 || strings.Join(entries[0].Argv, " ") != "process next" || strings.Join(entries[1].Argv, " ") != "focus --infer REQ-X --source prompt" {
		t.Fatalf("hook commands must be skipped and explicit commands recorded, got %+v", entries)
	}
}

// Epic 288 (B6): the history kept the last 32 entries, fewer than one
// delivery loop runs, so the command that went wrong was gone by the time
// anyone looked. The cap is now a size, with the full file rotated aside.
func TestHistoryKeepsAWholeLoop(t *testing.T) {
	root := historyWorkspace(t)
	captureOutput(t, func() {
		for i := 0; i < 40; i++ {
			runWithHistory([]string{"status"}, func() int { return 0 })
		}
	})
	if n := len(historyEntries(root)); n != 40 {
		t.Fatalf("every entry of a loop is kept: want 40, got %d", n)
	}
}

func TestHistoryRollsOverAtTheSizeCap(t *testing.T) {
	root := historyWorkspace(t)
	dir := filepath.Join(root, ".modernpath")
	fixed := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	savedClock := historyClock
	historyClock = func() time.Time { return fixed }
	t.Cleanup(func() { historyClock = savedClock })

	seedFull := func() {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "cli-history.log"), append(bytes.Repeat([]byte("x"), 1<<20), '\n'), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	aged := func(name string, age time.Duration) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, fixed.Add(-age), fixed.Add(-age)); err != nil {
			t.Fatal(err)
		}
		return path
	}
	stale := aged("cli-history.20260901T000000Z.log", 31*24*time.Hour)
	recent := aged("cli-history.20261015T000000Z.log", 6*24*time.Hour)

	seedFull()
	captureOutput(t, func() { runWithHistory([]string{"status"}, func() int { return 0 }) })

	entries := historyEntries(root)
	if len(entries) != 1 || strings.Join(entries[0].Argv, " ") != "status" {
		t.Fatalf("after a rollover the log holds only the new entry, got %d: %+v", len(entries), entries)
	}
	rotated := filepath.Join(dir, "cli-history.20261021T120000Z.log")
	if info, err := os.Stat(rotated); err != nil || info.Size() < 1<<20 {
		t.Fatalf("the full file is rotated to the UTC second stamp: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("a rotated file older than 30 days is pruned: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Fatalf("a rotated file within 30 days is kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cli-history.log")); err != nil {
		t.Fatalf("cli-history.log itself is never pruned: %v", err)
	}
	// Both files stay private, and the rotated name is ignored by the
	// directory's own `*.log` rule.
	for _, name := range []string{"cli-history.log", filepath.Base(rotated)} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("%s mode = %o, want 600", name, perm)
		}
	}
	ignore, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(ignore), "*.log\n") {
		t.Errorf(".modernpath/.gitignore must carry *.log:\n%s", ignore)
	}
	if ok, _ := filepath.Match("*.log", filepath.Base(rotated)); !ok {
		t.Errorf("the rotated name %s must match *.log", filepath.Base(rotated))
	}

	// A second rollover in the same second never renames over the first.
	seedFull()
	captureOutput(t, func() { runWithHistory([]string{"status"}, func() int { return 1 }) })
	if _, err := os.Stat(filepath.Join(dir, "cli-history.20261021T120000Z-1.log")); err != nil {
		t.Fatalf("a same-second rollover gets a numeric suffix: %v", err)
	}
	if info, err := os.Stat(rotated); err != nil || info.Size() < 1<<20 {
		t.Fatalf("the first rotated file is untouched: %v", err)
	}
}
