// Package browser is what the CLI knows about the operator's browser: how to
// open a URL in it (Open), and whether one on this machine can reach a
// loopback listener in this process at all (LoopbackUnavailable). Nothing
// here is specific to any identity provider; the ZITADEL login and the
// GitHub-connect command are its callers.
package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Open opens url in the operator's browser: the programs named by the
// BROWSER environment variable when it is set, otherwise the platform's
// default opener. The ZITADEL login and the GitHub-connect command both use
// it.
func Open(url string) error {
	cmds := commands(runtime.GOOS, os.Getenv("BROWSER"), url)
	if len(cmds) == 0 {
		return fmt.Errorf("unsupported platform")
	}

	var firstErr error
	for _, argv := range cmds {
		err := launch(argv, launchGrace)
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// launchGrace is how long a launcher gets to fail after it has
// started. Openers such as open, xdg-open and wslview exit as soon as they
// have handed the URL over — with a non-zero status when nothing takes it, an
// https handler being absent, say — so a launcher still running when the
// grace is up is taken to have opened something, and one that has exited
// non-zero by then has not.
const launchGrace = 750 * time.Millisecond

// launch starts argv and reports whether it opened a browser. A launch
// that starts but exits with an error inside grace is a failure, so the next
// BROWSER alternative is tried and, when none works, the login hands over to
// the device flow instead of waiting out the full login timeout on a browser
// that never opened. The process is always waited on, so no launcher is left
// as a zombie for the length of the login.
func launch(argv []string, grace time.Duration) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		return err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case err := <-exited:
		if err != nil {
			return fmt.Errorf("%s: %w", argv[0], err)
		}
		return nil
	case <-timer.C:
		return nil
	}
}

// commands is the argv list Open tries, in order, for one GOOS
// and one BROWSER value. It is separated from the launch so both the BROWSER
// convention and the Windows command line can be tested from any host.
func commands(goos, browserEnv, url string) [][]string {
	if cmds := envCommands(goos, browserEnv, url); len(cmds) > 0 {
		return cmds
	}
	switch goos {
	case "darwin":
		return [][]string{{"open", url}}
	case "linux":
		return [][]string{{"xdg-open", url}}
	case "windows":
		// rundll32 hands the URL to the registered https handler directly.
		// Going through cmd.exe would mean escaping for its parser: it
		// splits on "&" and expands %NAME% for any defined variable, and an
		// authorization URL is full of both.
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		return nil
	}
}

// envCommands reads BROWSER the way xdg-open, python's webbrowser and
// gh read it: a list of commands separated by the platform's path-list
// separator, tried in order, where "%s" in a command stands for the URL and a
// command naming no "%s" takes the URL as its last argument. Each command is
// split into arguments shell-style — whitespace separates, single or double
// quotes group, and on POSIX a backslash escapes the next character — so a
// program path containing spaces can be quoted. The separator and the escape
// rule follow goos, not the host the binary was built on: Windows separates
// with ";" and its paths are full of backslashes, so a Windows path is split
// on ";" and read verbatim.
func envCommands(goos, browserEnv, url string) [][]string {
	sep, backslashEscapes := ":", true
	if goos == "windows" {
		sep, backslashEscapes = ";", false
	}
	var cmds [][]string
	for _, entry := range strings.Split(browserEnv, sep) {
		argv := splitCommand(entry, backslashEscapes)
		if len(argv) == 0 {
			continue
		}
		substituted := false
		for i, arg := range argv {
			if strings.Contains(arg, "%s") {
				argv[i] = strings.ReplaceAll(arg, "%s", url)
				substituted = true
			}
		}
		if !substituted {
			argv = append(argv, url)
		}
		cmds = append(cmds, argv)
	}
	return cmds
}

// splitCommand splits one BROWSER entry into arguments: whitespace separates,
// a single- or double-quoted span is kept together with its quotes removed,
// and, when backslashEscapes is set, a backslash outside single quotes makes
// the next character literal. An unterminated quote runs to the end of the
// entry.
func splitCommand(entry string, backslashEscapes bool) []string {
	var argv []string
	var cur strings.Builder
	inArg := false
	var quote rune
	escaped := false
	for _, r := range entry {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'' && r == '\'':
			quote = 0
		case quote == '\'':
			cur.WriteRune(r)
		case r == '\\' && backslashEscapes:
			escaped = true
			inArg = true
		case quote == '"' && r == '"':
			quote = 0
		case quote == '"':
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
			inArg = true
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			if inArg {
				argv = append(argv, cur.String())
				cur.Reset()
				inArg = false
			}
		default:
			cur.WriteRune(r)
			inArg = true
		}
	}
	if inArg {
		argv = append(argv, cur.String())
	}
	return argv
}
