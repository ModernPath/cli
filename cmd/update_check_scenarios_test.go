package cmd

import (
	"net/http"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Epic 422 scenarios, driven through run(), the function main calls: the
// history capture, the command and the end-of-run update notice.

// Scenario 811d900d: a released CLI 0.26.0 installed with Homebrew, a release
// cache that says 0.27.1 was fetched an hour ago, stdout and stderr in a
// terminal. The first `modernpath status` ends with one notice line; the same
// command an hour later prints none.
func TestUpdateCheckScenarioHomebrewNoticeOnceInTerminal(t *testing.T) {
	f := newUCFixture(t)
	f.connectedRepo(false)
	f.binary("/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath")
	f.writeCache(ucLatest, f.time().Add(-time.Hour))
	f.terminals(true, true)

	first := f.run("status")
	want := ucNotice(ucLatest, ucRunning, ucBrew)
	lines := ucLines(first.stderr)
	if first.code != 0 || len(lines) == 0 || lines[len(lines)-1] != want {
		t.Fatalf("the first run must end with the notice %q; exit %d, stderr:\n%s", want, first.code, first.stderr)
	}
	if strings.Count(first.stderr, "is available") != 1 {
		t.Fatalf("want exactly one notice line, stderr:\n%s", first.stderr)
	}
	for _, part := range []string{"0.27.1", "0.26.0", ucBrew} {
		if !strings.Contains(lines[len(lines)-1], part) {
			t.Fatalf("the notice must name %q: %q", part, lines[len(lines)-1])
		}
	}

	f.advance(time.Hour)
	second := f.run("status")
	if second.code != 0 || strings.Contains(second.stderr, "is available") {
		t.Fatalf("an hour later the notice must not repeat; exit %d, stderr:\n%s", second.code, second.stderr)
	}
	if got := f.releases.requests(); got != 0 {
		t.Fatalf("a fresh cache answers the notice without a lookup, got %d requests", got)
	}
}

// Scenario d22a8e91: an agent runs `modernpath prepare --json` in a connected
// repository without a terminal while 0.27.1 is the latest CLI release.
func TestUpdateCheckScenarioAgentPrepareJSONReportsNewerRelease(t *testing.T) {
	f := newUCFixture(t)
	f.connectedRepo(false)
	f.releases.set(ucTag("v" + ucLatest))

	res := f.run("prepare", "--json")
	report := ucReport(t, res.stdout)
	release := ucRelease(report)
	if release == nil || release["state"] != "newer" || release["latest"] != ucLatest {
		t.Fatalf("cli_release must be newer with latest %s, got %v\n%s", ucLatest, report["cli_release"], res.stdout)
	}
	steps := ucSteps(report)
	upgrade, install := ucIndex(steps, ucPlatformUpgrade(runtime.GOOS)), ucIndex(steps, "modernpath install")
	if upgrade < 0 || install < 0 || upgrade > install {
		t.Fatalf("the upgrade command must come before modernpath install: %q", steps)
	}
	if strings.Contains(res.stderr, "is available") || strings.TrimSpace(res.stderr) != "" {
		t.Fatalf("stderr must carry no update notice, got:\n%s", res.stderr)
	}
	if got := f.releases.requests(); got != 1 {
		t.Fatalf("prepare looks up once, got %d requests", got)
	}

	// Control: in a terminal the same harness sees the notice, so the empty
	// stderr above is not a capture that misses it.
	f.terminals(true, true)
	control := f.run("status")
	if !strings.Contains(control.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
		t.Fatalf("control: a terminal run must show the notice from the cache prepare wrote, stderr:\n%s", control.stderr)
	}
}

// Scenario 2853b74f: a released CLI 0.26.0 in a connected repository with a
// valid sign-in and no release cache. `modernpath prepare` looks up while it
// runs its other checks and waits up to 3 seconds: a release lookup that
// answers 0.27.1 only after 1.5 seconds, or only after prepare has read the
// System list from the server, still gives `latest release: 0.27.1; upgrade
// available`.
func TestUpdateCheckScenarioPrepareWaitsForASlowLookup(t *testing.T) {
	const want = "CLI: 0.26.0 (latest release: 0.27.1; upgrade available)"
	for _, tc := range []struct {
		name   string
		answer func(f *ucFixture) http.HandlerFunc
	}{
		{"a lookup that answers after 1.5 seconds", func(f *ucFixture) http.HandlerFunc {
			return f.releases.after(1500*time.Millisecond, ucTag("v"+ucLatest))
		}},
		{"a lookup that answers only after prepare has read the System list", func(f *ucFixture) http.HandlerFunc {
			return f.answerWhen(f.listedSignal, ucTag("v"+ucLatest))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.releases.set(tc.answer(f))

			res := f.run("prepare")
			if got := ucCLILine(t, res.stdout); got != want {
				t.Fatalf("CLI line = %q, want %q", got, want)
			}
			if !strings.Contains(res.stdout, "System: 17 Example (reachable)") {
				t.Fatalf("prepare must read the System list as before:\n%s", res.stdout)
			}
			if got := f.releases.requests(); got != 1 {
				t.Fatalf("prepare looks up once, got %d requests", got)
			}
		})
	}

	// The opposite order: the fake API serves the System list only once the
	// release server has prepare's request. A prepare that looked up only after
	// its server reads would wait out the 5-second reachability probe here and
	// report the System's access as unknown.
	t.Run("the System list waits for prepare's lookup", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.setAPIWait(ucWaitPrepareLookup)

		res := f.run("prepare")
		if !strings.Contains(res.stdout, "System: 17 Example (reachable)") {
			t.Fatalf("prepare must look up while it reads the System list, so the System is reachable:\n%s", res.stdout)
		}
		if got := ucCLILine(t, res.stdout); got != want {
			t.Fatalf("CLI line = %q, want %q", got, want)
		}
	})
}

// Scenario 397cf2ba: prepare on a local build names it and sends no lookup.
func TestUpdateCheckScenarioLocalBuildPrepareSkipsLookup(t *testing.T) {
	f := newUCFixture(t)
	ucSetVersion(t, "0.27.0-dev+abc1234 (2026-11-10T15:09Z)")
	f.connectedRepo(false)

	res := f.run("prepare")
	if got, want := ucCLILine(t, res.stdout), "CLI: 0.27.0-dev+abc1234 (2026-11-10T15:09Z) (local build; update check not used)"; got != want {
		t.Fatalf("CLI line = %q, want %q", got, want)
	}
	if got := f.releases.requests(); got != 0 {
		t.Fatalf("a local build must not look anything up, got %d requests", got)
	}
	if strings.Contains(res.stdout+res.stderr, "latest release: unknown") {
		t.Fatalf("the output must not say latest release: unknown:\n%s%s", res.stdout, res.stderr)
	}
}

// Scenario 98dd883b: with CI=true a pipeline runs `modernpath prepare --json`
// and `modernpath status`; no lookup, no notice, and prepare reports off.
func TestUpdateCheckScenarioCIOffForPrepareAndStatus(t *testing.T) {
	f := newUCFixture(t)
	f.connectedRepo(false)
	f.writeCache(ucLatest, f.time().Add(-time.Hour))
	f.terminals(true, true)
	f.setEnv("CI", "true")

	prep := f.run("prepare", "--json")
	report := ucReport(t, prep.stdout)
	if release := ucRelease(report); release == nil || release["state"] != "off" || release["reason"] != "ci" {
		t.Fatalf("cli_release must be off with reason ci, got %v", report["cli_release"])
	}
	status := f.run("status")
	if got := f.releases.requests(); got != 0 {
		t.Fatalf("CI must send no release lookup, got %d requests", got)
	}
	for name, stderr := range map[string]string{"prepare --json": prep.stderr, "status": status.stderr} {
		if strings.Contains(stderr, "is available") {
			t.Fatalf("%s printed an update notice in CI:\n%s", name, stderr)
		}
	}

	f.setEnv("CI", "")
	control := f.run("status")
	if !strings.Contains(control.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
		t.Fatalf("control: without CI the same cache shows the notice, stderr:\n%s", control.stderr)
	}
}

// Scenario 076c17e9: with MODERNPATH_NO_UPDATE_CHECK=1 the engineer runs
// `modernpath status` and `modernpath prepare`; no lookup, no notice, and
// prepare says the update check is off.
func TestUpdateCheckScenarioOptOutTurnsCheckOff(t *testing.T) {
	f := newUCFixture(t)
	f.connectedRepo(false)
	f.writeCache(ucLatest, f.time().Add(-time.Hour))
	f.terminals(true, true)
	f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "1")

	status := f.run("status")
	prep := f.run("prepare")
	if got, want := ucCLILine(t, prep.stdout), "CLI: 0.26.0 (update check off: MODERNPATH_NO_UPDATE_CHECK is set)"; got != want {
		t.Fatalf("CLI line = %q, want %q", got, want)
	}
	if got := f.releases.requests(); got != 0 {
		t.Fatalf("the opt-out must send no release lookup, got %d requests", got)
	}
	for name, stderr := range map[string]string{"status": status.stderr, "prepare": prep.stderr} {
		if strings.Contains(stderr, "is available") {
			t.Fatalf("%s printed an update notice with the opt-out:\n%s", name, stderr)
		}
	}

	f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "")
	control := f.run("status")
	if !strings.Contains(control.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
		t.Fatalf("control: without the opt-out the same cache shows the notice, stderr:\n%s", control.stderr)
	}
}
