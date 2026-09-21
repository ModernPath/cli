package browser

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Host is what the loopback pre-check knows about the process's
// surroundings. DetectHost reads the real ones; tests build their own.
type Host struct {
	GOOS       string
	Getenv     func(key string) string
	LookPath   func(file string) (string, error)
	FileExists func(path string) bool
}

// DetectHost describes the process the CLI is running in.
func DetectHost() Host {
	return Host{
		GOOS:     runtime.GOOS,
		Getenv:   os.Getenv,
		LookPath: exec.LookPath,
		FileExists: func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		},
	}
}

// LoopbackUnavailable says why the browser-plus-loopback flow cannot work on
// h, or returns "" when nothing rules it out. It is a pre-check on the
// surroundings, not a probe: the listener and the browser launch each get
// their own chance to fail afterwards, and either failure also falls back to
// the device flow.
//
// The rule is where the browser would run. The loopback flow needs the
// browser on the same machine as the listener, so an SSH session (including
// tmux inside one — the SSH variables are inherited), a container, a CI shell,
// or a Linux box with no display and no launcher all rule it out.
//
// BROWSER is checked before any of that. It is the operator saying which
// program opens URLs here, and it beats every inference from the
// surroundings: an editor's remote session or dev container exports it to a
// helper that opens the URL on the local desktop and forwards the loopback
// port, which is exactly where the flow works, and a platform the CLI has no
// launcher of its own for can still name one.
func LoopbackUnavailable(h Host) string {
	env := func(key string) string {
		if h.Getenv == nil {
			return ""
		}
		return h.Getenv(key)
	}
	exists := func(path string) bool {
		return h.FileExists != nil && h.FileExists(path)
	}
	goos := h.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}

	if env("BROWSER") != "" {
		return ""
	}
	if env("SSH_CONNECTION") != "" || env("SSH_TTY") != "" || env("SSH_CLIENT") != "" {
		return "this is an SSH session, so a browser on your machine cannot reach a listener here"
	}
	if envTrue(env("CI")) {
		return "this is a CI shell (CI is set)"
	}
	if env("container") != "" || env("KUBERNETES_SERVICE_HOST") != "" || exists("/.dockerenv") || exists("/run/.containerenv") {
		return "this process runs inside a container"
	}

	switch goos {
	case "darwin", "windows":
		return ""
	case "linux":
		if env("DISPLAY") == "" && env("WAYLAND_DISPLAY") == "" {
			return "no graphical display here (DISPLAY and WAYLAND_DISPLAY are unset)"
		}
		if h.LookPath == nil {
			return ""
		}
		if _, err := h.LookPath("xdg-open"); err != nil {
			return "no browser launcher here (xdg-open is not on PATH and BROWSER is unset)"
		}
		return ""
	default:
		return "the CLI has no browser launcher for " + goos
	}
}

// envTrue reads a flag-shaped variable the way ci-info and the tools built on
// it do: set to anything but an explicit negation means on. A shell profile
// that exports CI=false to switch other tools out of CI mode is not a CI
// shell.
func envTrue(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}
