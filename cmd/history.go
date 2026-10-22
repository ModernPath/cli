package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
)

// REQ-CROSS-387 (EPIC-CLI-019): a local, redacted history of user-run
// invocations for local troubleshooting of a previous command and its
// output. Hook-mode invocations are never recorded (they precede every user
// command in an agent session and would drain the log); `auth` is never
// recorded (its --token argument is a bearer); commands that hand the
// terminal to an interactive child (dev) record argv and exit only, so the
// child keeps the real fds. Nothing from the log reaches the store.
//
// Epic 288 (B6): the log holds a whole delivery loop. It is capped by size,
// not by a count of entries — the previous 32-entry cap held less than one
// loop, so the command that went wrong was gone by the time anyone looked.

const (
	historyFileName  = "cli-history.log"
	historyTailBytes = 8 * 1024
	// historyMaxBytes is the size at which cli-history.log is rotated aside.
	historyMaxBytes = 1 << 20
	// historyKeepRotated is how long a rotated file is kept.
	historyKeepRotated = 30 * 24 * time.Hour
)

// historyClock stamps a rotated history file; tests pin it.
var historyClock = time.Now

// historyEntry is one recorded invocation.
type historyEntry struct {
	Argv    []string  `json:"argv"`
	Started time.Time `json:"started"`
	Exit    int       `json:"exit"`
	Output  string    `json:"output,omitempty"`
}

func historyPath(root string) string {
	return filepath.Join(root, config.ConfigDir, historyFileName)
}

// activeHistory is the recorder of the running invocation, so exit(code)
// can drain the tee before the process ends.
var activeHistory *historyRun

type historyRun struct {
	root  string
	entry historyEntry
	tee   *outputTee
	done  bool
}

// runWithHistory runs one invocation under the recorder and returns its
// exit code.
func runWithHistory(argv []string, run func() int) int {
	h := beginHistory(argv)
	code := run()
	h.finish(code)
	return code
}

// exit ends the process through the recorder, so a tee never loses the tail
// it was about to write. Every process exit in cmd goes through here.
func exit(code int) {
	if activeHistory != nil {
		activeHistory.finish(code)
	}
	os.Exit(code)
}

func beginHistory(argv []string) *historyRun {
	if len(argv) == 0 || hookMode(argv) || leafWord(argv) == "auth" {
		return nil
	}
	cfgDir, err := config.FindConfigDir()
	if err != nil || cfgDir == "" {
		return nil
	}
	h := &historyRun{root: filepath.Dir(cfgDir), entry: historyEntry{Argv: redactArgv(argv), Started: time.Now().UTC()}}
	if !interactiveLeaf(argv) {
		h.tee = startTee()
	}
	activeHistory = h
	return h
}

func (h *historyRun) finish(code int) {
	if h == nil || h.done {
		return
	}
	h.done = true
	activeHistory = nil
	h.entry.Exit = code
	if h.tee != nil {
		h.entry.Output = h.tee.stop()
	}
	appendHistory(h.root, h.entry)
}

// hookMode reports an invocation made by an installed hook.
func hookMode(argv []string) bool {
	for _, a := range argv {
		switch {
		case a == "--hook" || strings.HasPrefix(a, "--hook="):
			return true
		}
	}
	return false
}

// leafWord is the first non-flag argument — the command word.
func leafWord(argv []string) string {
	for _, a := range argv {
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// interactiveLeaf names the commands that hand the terminal to a child.
func interactiveLeaf(argv []string) bool {
	switch leafWord(argv) {
	case "dev":
		return true
	}
	return false
}

// redactArgv replaces the value of any flag whose name ends in token, secret
// or password, in both `--flag value` and `--flag=value` forms.
func redactArgv(argv []string) []string {
	out := make([]string, len(argv))
	redactNext := false
	for i, a := range argv {
		switch {
		case redactNext:
			out[i] = "<redacted>"
			redactNext = false
		case strings.HasPrefix(a, "-") && sensitiveFlag(a):
			if idx := strings.Index(a, "="); idx >= 0 {
				out[i] = a[:idx+1] + "<redacted>"
			} else {
				out[i] = a
				redactNext = true
			}
		default:
			out[i] = a
		}
	}
	return out
}

func sensitiveFlag(flag string) bool {
	name := strings.ToLower(strings.TrimLeft(flag, "-"))
	if idx := strings.Index(name, "="); idx >= 0 {
		name = name[:idx]
	}
	for _, suffix := range []string{"token", "secret", "password", "pin", "otp", "passphrase", "key"} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// appendHistory writes one entry with O_APPEND and keeps the file ignored;
// a full file is rotated aside first, best-effort (F-CLI019-R1-11).
func appendHistory(root string, entry historyEntry) {
	cfgDir := filepath.Join(root, config.ConfigDir)
	_ = config.EnsureLocalIgnores(cfgDir)
	blob, err := json.Marshal(entry)
	if err != nil {
		return
	}
	rotateHistory(cfgDir)
	f, err := os.OpenFile(historyPath(root), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(append(blob, '\n'))
	_ = f.Close()
}

// rotateHistory moves a log of historyMaxBytes or more aside as
// cli-history.<UTC second stamp>.log and prunes rotated files older than
// historyKeepRotated. The target name is never one that exists (a numeric
// suffix is added), so no rename ever lands on a file: on POSIX it would
// replace it, on Windows it would fail. The rotated name matches `*.log` in
// the directory's own ignore file and `/.modernpath/*` at the repository root.
func rotateHistory(cfgDir string) {
	path := filepath.Join(cfgDir, historyFileName)
	info, err := os.Stat(path)
	if err != nil || info.Size() < historyMaxBytes {
		return
	}
	now := historyClock().UTC()
	stamp := now.Format("20060102T150405Z")
	target := filepath.Join(cfgDir, "cli-history."+stamp+".log")
	for n := 1; n < 1000; n++ {
		if _, err := os.Lstat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(cfgDir, fmt.Sprintf("cli-history.%s-%d.log", stamp, n))
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		return
	}
	_ = os.Rename(path, target)
	pruneRotatedHistory(cfgDir, now)
}

// pruneRotatedHistory removes rotated files older than historyKeepRotated;
// cli-history.log itself is never removed.
func pruneRotatedHistory(cfgDir string, now time.Time) {
	matches, _ := filepath.Glob(filepath.Join(cfgDir, "cli-history.*.log"))
	for _, match := range matches {
		if filepath.Base(match) == historyFileName {
			continue
		}
		info, err := os.Stat(match)
		if err != nil || now.Sub(info.ModTime()) <= historyKeepRotated {
			continue
		}
		_ = os.Remove(match)
	}
}

// historyEntries reads the recorded invocations, oldest first; a malformed
// line is skipped.
func historyEntries(root string) []historyEntry {
	f, err := os.Open(historyPath(root))
	if err != nil {
		return nil
	}
	defer f.Close()
	var entries []historyEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e historyEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err == nil && len(e.Argv) > 0 {
			entries = append(entries, e)
		}
	}
	return entries
}

// outputTee copies everything written to stdout and stderr — through the
// os files and through color.Output/color.Error, which bind their own
// writers — to the real destinations and to a bounded tail. Each stream's
// bytes reach their fd unchanged and in order; the relative order of a
// stdout write and a stderr write is not preserved across the two pipes.
type outputTee struct {
	origOut, origErr           *os.File
	origColorOut, origColorErr io.Writer
	wOut, wErr                 *os.File
	doneOut, doneErr           chan struct{}
	tail                       *tailBuffer
}

func startTee() *outputTee {
	t := &outputTee{
		origOut: os.Stdout, origErr: os.Stderr,
		origColorOut: color.Output, origColorErr: color.Error,
		doneOut: make(chan struct{}), doneErr: make(chan struct{}),
		tail: &tailBuffer{limit: historyTailBytes},
	}
	rOut, wOut, err := os.Pipe()
	if err != nil {
		return nil
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		_ = rOut.Close()
		_ = wOut.Close()
		return nil
	}
	t.wOut, t.wErr = wOut, wErr
	go func() { _, _ = io.Copy(io.MultiWriter(t.origOut, t.tail), rOut); _ = rOut.Close(); close(t.doneOut) }()
	go func() { _, _ = io.Copy(io.MultiWriter(t.origErr, t.tail), rErr); _ = rErr.Close(); close(t.doneErr) }()
	os.Stdout, os.Stderr = wOut, wErr
	color.Output, color.Error = wOut, wErr
	return t
}

// stop drains the pipes, restores the writers and returns the tail.
func (t *outputTee) stop() string {
	if t == nil {
		return ""
	}
	_ = t.wOut.Close()
	_ = t.wErr.Close()
	<-t.doneOut
	<-t.doneErr
	os.Stdout, os.Stderr = t.origOut, t.origErr
	color.Output, color.Error = t.origColorOut, t.origColorErr
	return t.tail.String()
}

// tailBuffer keeps the last limit bytes written to it; two copy goroutines
// write to it, so it locks.
type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.limit {
		b.buf = b.buf[len(b.buf)-b.limit:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
