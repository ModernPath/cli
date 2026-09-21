package browser

import (
	"reflect"
	"runtime"
	"testing"
	"time"
)

// REQ-CROSS-323 review round `RUN:2026-09-08`: the browser launcher is what
// the loopback login stands on, so the two things that decide whether a
// browser opens at all — the Windows command line and the BROWSER
// convention — are tested from any host through commands.
func TestCommandsBuildALaunchableCommandLine(t *testing.T) {
	const authURL = "https://issuer.test/oauth/v2/authorize?response_type=code&client_id=cli&state=abc&code_challenge=xyz&redirect_uri=http%3A%2F%2F127.0.0.1%3A8766%2Fapi"
	// BROWSER's list separator follows the target platform, not the test
	// host: ";" on Windows, ":" elsewhere. A Windows path carries a ":" of
	// its own, which is why it cannot be split the POSIX way.
	const sep = ":"

	tests := []struct {
		name    string
		goos    string
		browser string
		want    [][]string
	}{
		{"macOS default", "darwin", "", [][]string{{"open", authURL}}},
		{"Linux default", "linux", "", [][]string{{"xdg-open", authURL}}},
		{
			// The URL goes to the https handler as one argument, untouched:
			// no cmd.exe in the way to split it on "&" or expand a %NAME%
			// between two of its percent-encoded triplets.
			"Windows hands the URL to the protocol handler verbatim",
			"windows", "",
			[][]string{{"rundll32", "url.dll,FileProtocolHandler", authURL}},
		},
		{"unsupported platform has no command", "plan9", "", nil},
		{"BROWSER names a program", "linux", "wslview", [][]string{{"wslview", authURL}}},
		{"BROWSER carries a %s placeholder", "linux", "firefox %s", [][]string{{"firefox", authURL}}},
		{"BROWSER places %s among other arguments", "linux", "chromium --new-window %s --foo", [][]string{{"chromium", "--new-window", authURL, "--foo"}}},
		{
			"BROWSER lists alternatives in order",
			"linux", "/usr/bin/firefox" + sep + "chromium %s",
			[][]string{{"/usr/bin/firefox", authURL}, {"chromium", authURL}},
		},
		{"BROWSER overrides the platform opener", "darwin", "wslview", [][]string{{"wslview", authURL}}},
		{"empty BROWSER entries are skipped", "linux", sep + " " + sep + "wslview", [][]string{{"wslview", authURL}}},
		{
			"BROWSER quotes a Windows program path with spaces, backslashes and a drive colon",
			"windows", `"C:\Program Files\Mozilla Firefox\firefox.exe"`,
			[][]string{{`C:\Program Files\Mozilla Firefox\firefox.exe`, authURL}},
		},
		{
			"BROWSER on Windows separates alternatives with a semicolon",
			"windows", `"C:\Program Files\Mozilla Firefox\firefox.exe";C:\tools\chromium.exe %s`,
			[][]string{{`C:\Program Files\Mozilla Firefox\firefox.exe`, authURL}, {`C:\tools\chromium.exe`, authURL}},
		},
		{
			"BROWSER quotes a program path with spaces",
			"linux", `"/opt/my browser/run" --kiosk`,
			[][]string{{"/opt/my browser/run", "--kiosk", authURL}},
		},
		{
			"BROWSER single-quotes a program path with spaces",
			"darwin", `'/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' --new-window`,
			[][]string{{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "--new-window", authURL}},
		},
		{
			"BROWSER escapes a space with a backslash",
			"darwin", `/Applications/Google\ Chrome.app/Contents/MacOS/Google\ Chrome %s`,
			[][]string{{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", authURL}},
		},
		{
			"BROWSER quotes part of an argument",
			"linux", `chromium --profile-directory="Profile 2" %s`,
			[][]string{{"chromium", "--profile-directory=Profile 2", authURL}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := commands(tt.goos, tt.browser, authURL)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("commands(%q, %q) = %q, want %q", tt.goos, tt.browser, got, tt.want)
			}
		})
	}
}

// A launcher that starts and then exits with an error has not opened a
// browser — xdg-open with no https handler, a BROWSER shim that is gone —
// and must say so, or the login waits out its whole timeout for a callback
// that cannot come. One that exits cleanly, or is still running when the
// grace is up, has.
func TestLaunchObservesTheLauncherExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses POSIX sh")
	}
	const grace = 300 * time.Millisecond

	if err := launch([]string{"sh", "-c", "exit 0"}, grace); err != nil {
		t.Fatalf("a launcher that exits 0 has opened the browser, got %v", err)
	}
	if err := launch([]string{"sh", "-c", "exit 3"}, grace); err == nil {
		t.Fatal("a launcher that exits non-zero inside the grace has not opened a browser")
	}
	start := time.Now()
	if err := launch([]string{"sh", "-c", "sleep 2"}, grace); err != nil {
		t.Fatalf("a launcher still running when the grace is up has opened the browser, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("the launch must return when the grace is up, not when the launcher exits; took %s", elapsed)
	}
	if err := launch([]string{"/nonexistent/browser-launcher"}, grace); err == nil {
		t.Fatal("a launcher that cannot start has not opened a browser")
	}
}
