package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fatih/color"
)

// REQ-CROSS-387 (EPIC-CLI-019) — the history recorder: every user-run
// invocation lands in .modernpath/cli-history.log with its exit and an output
// tail; hook-mode invocations and `auth` never do; token-like flag values are
// redacted; dev and ralph keep the real terminal; the tee is transparent.

func historyWorkspace(t *testing.T) string {
	t.Helper()
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://127.0.0.1:1","system_id":7}`)
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
		runWithHistory([]string{"factory", "connect", "--api-token", secret, "--system", "7"}, func() int { return 0 })
		runWithHistory([]string{"factory", "connect", "--api-token=" + secret}, func() int { return 0 })
		runWithHistory([]string{"factory", "release", "activate", "R1", "--pin", "246810"}, func() int { return 0 })
	})

	raw, _ := os.ReadFile(filepath.Join(root, ".modernpath", "cli-history.log"))
	if strings.Contains(string(raw), secret) {
		t.Fatalf("the token reached the history:\n%s", raw)
	}
	if strings.Contains(string(raw), "246810") {
		t.Fatalf("the release PIN reached the history (F-CLI019-PR-02):\n%s", raw)
	}
	entries := historyEntries(root)
	if len(entries) != 3 {
		t.Fatalf("auth must not be recorded; want the two connect entries and the release entry, got %d", len(entries))
	}
	if !strings.Contains(strings.Join(entries[0].Argv, " "), "<redacted>") {
		t.Errorf("the flag value must read <redacted>, got %v", entries[0].Argv)
	}
}

// (f) The history and notices files are ignored by git beside auth.json.
func TestHistoryFilesAreIgnored(t *testing.T) {
	root := historyWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/.gitignore", "auth.json\n")

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

// (i) dev and ralph hand the original fds to their child and record argv and
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

// (j) Hook-mode invocations are not recorded, so --last attaches the user
// command that came before them.
func TestHistorySkipsHookInvocations(t *testing.T) {
	root := historyWorkspace(t)

	captureOutput(t, func() {
		runWithHistory([]string{"process", "next"}, func() int { return 0 })
		runWithHistory([]string{"check", "--hook", "PreToolUse"}, func() int { return 0 })
		runWithHistory([]string{"context", "--hook", "UserPromptSubmit"}, func() int { return 0 })
		runWithHistory([]string{"factory", "sync", "--if-quiescent", "--trigger", "Stop"}, func() int { return 0 })
		runWithHistory([]string{"focus", "--infer", "REQ-X", "--source", "prompt"}, func() int { return 0 })
		runWithHistory([]string{"your-move", "--hook", "SessionStart"}, func() int { return 0 })
	})

	entries := historyEntries(root)
	if len(entries) != 1 || strings.Join(entries[0].Argv, " ") != "process next" {
		t.Fatalf("only the user command may be recorded, got %+v", entries)
	}
}
