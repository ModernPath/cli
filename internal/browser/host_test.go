package browser

import (
	"errors"
	"strings"
	"testing"
)

func hostWith(goos string, env map[string]string, path []string, files []string) Host {
	return Host{
		GOOS:   goos,
		Getenv: func(k string) string { return env[k] },
		LookPath: func(file string) (string, error) {
			for _, p := range path {
				if p == file {
					return "/usr/bin/" + file, nil
				}
			}
			return "", errors.New("not found")
		},
		FileExists: func(p string) bool {
			for _, f := range files {
				if f == p {
					return true
				}
			}
			return false
		},
	}
}

// REQ-CROSS-323 / USER:2026-09-07: the device flow is for where the loopback flow cannot
// work — a headless server, an SSH session, a container, a CI shell, tmux on
// a remote box, or no browser on the machine. Everything else gets the
// browser — and BROWSER, being the operator's explicit choice of launcher,
// beats every inference (review round `RUN:2026-09-08`).
func TestLoopbackUnavailableNamesTheSurroundingsThatRuleTheBrowserOut(t *testing.T) {
	tests := []struct {
		name string
		host Host
		want string // substring of the reason; "" means the loopback flow may run
	}{
		{"macOS desktop", hostWith("darwin", nil, nil, nil), ""},
		{"Windows desktop", hostWith("windows", nil, nil, nil), ""},
		{"Linux desktop with xdg-open", hostWith("linux", map[string]string{"DISPLAY": ":0"}, []string{"xdg-open"}, nil), ""},
		{"Linux Wayland desktop", hostWith("linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"xdg-open"}, nil), ""},
		{"SSH session", hostWith("darwin", map[string]string{"SSH_CONNECTION": "a b c d"}, nil, nil), "SSH session"},
		{"SSH tty (tmux on a remote box)", hostWith("linux", map[string]string{"SSH_TTY": "/dev/pts/0", "DISPLAY": ":0"}, []string{"xdg-open"}, nil), "SSH session"},
		{"CI shell", hostWith("linux", map[string]string{"CI": "true", "DISPLAY": ":0"}, []string{"xdg-open"}, nil), "CI"},
		{"CI shell (CI=1)", hostWith("linux", map[string]string{"CI": "1", "DISPLAY": ":0"}, []string{"xdg-open"}, nil), "CI"},
		{"CI=false is not a CI shell", hostWith("linux", map[string]string{"CI": "false", "DISPLAY": ":0"}, []string{"xdg-open"}, nil), ""},
		{"CI=0 is not a CI shell", hostWith("darwin", map[string]string{"CI": "0"}, nil, nil), ""},
		{"docker container", hostWith("linux", map[string]string{"DISPLAY": ":0"}, []string{"xdg-open"}, []string{"/.dockerenv"}), "container"},
		{"podman container", hostWith("linux", nil, nil, []string{"/run/.containerenv"}), "container"},
		{"kubernetes pod", hostWith("linux", map[string]string{"KUBERNETES_SERVICE_HOST": "10.0.0.1"}, nil, nil), "container"},
		{"headless Linux", hostWith("linux", nil, []string{"xdg-open"}, nil), "display"},
		{"Linux display without a launcher", hostWith("linux", map[string]string{"DISPLAY": ":0"}, nil, nil), "launcher"},
		{"Linux with BROWSER set and no display", hostWith("linux", map[string]string{"BROWSER": "wslview"}, nil, nil), ""},
		{"SSH session with BROWSER set (editor remote session forwarding the port)", hostWith("linux", map[string]string{"SSH_CONNECTION": "a b c d", "BROWSER": "/usr/local/bin/code-open"}, nil, nil), ""},
		{"container with BROWSER set", hostWith("linux", map[string]string{"BROWSER": "/usr/local/bin/code-open"}, nil, []string{"/.dockerenv"}), ""},
		{"CI shell with BROWSER set", hostWith("linux", map[string]string{"CI": "true", "BROWSER": "firefox"}, nil, nil), ""},
		{"unknown platform with BROWSER set", hostWith("freebsd", map[string]string{"BROWSER": "firefox"}, nil, nil), ""},
		{"unknown platform", hostWith("plan9", nil, nil, nil), "plan9"},
		{"nil probes inspect nothing", Host{GOOS: "darwin"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LoopbackUnavailable(tt.host)
			if tt.want == "" && got != "" {
				t.Fatalf("the loopback flow must be allowed here, got reason %q", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("reason = %q, want it to mention %q", got, tt.want)
			}
		})
	}
}
