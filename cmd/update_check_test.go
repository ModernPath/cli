package cmd

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Epic 422 criterion tests on the user-flow boundary: each one drives run(),
// the function main calls.

var ucStatusLines = []string{"is available", "This is the latest release.", "No newer release is available.", "Couldn't check for a newer release."}

// ucNoStatusLine fails when stderr carries the notice or a --version status line.
func ucNoStatusLine(t *testing.T, what, stderr string) {
	t.Helper()
	for _, line := range ucStatusLines {
		if strings.Contains(stderr, line) {
			t.Fatalf("%s printed %q:\n%s", what, line, stderr)
		}
	}
}

// ucRepoHasNoReleaseFiles fails when a release file landed in the
// repository's .modernpath folder.
func ucRepoHasNoReleaseFiles(t *testing.T, repo string) {
	t.Helper()
	for _, name := range []string{"release-latest.json", "release-notice.json", "release-attempt.json"} {
		if _, err := os.Stat(filepath.Join(repo, ".modernpath", name)); err == nil {
			t.Fatalf("%s was written into the repository's .modernpath folder", name)
		}
	}
}

// Criterion 562b1281: prepare and --version look up on every run and write
// the cache; the notice reads it for 4 hours, also when its tag is older than
// the running release.
func TestUpdateCheckReleaseCache(t *testing.T) {
	t.Run("prepare and --version look up on every run and write the cache", func(t *testing.T) {
		f := newUCFixture(t)
		repo := f.connectedRepo(false)
		f.terminals(true, true)
		for i, args := range [][]string{{"prepare"}, {"prepare"}, {"--version"}, {"--version"}} {
			f.advance(10 * time.Minute)
			f.run(args...)
			if got := f.releases.requests(); got != i+1 {
				t.Fatalf("run %d (%v): want %d requests, got %d; a fresh cache must not stop the lookup", i+1, args, i+1, got)
			}
			tag, at, ok := f.cacheEntry()
			if !ok || tag != ucLatest || !at.Equal(f.time()) {
				t.Fatalf("run %d (%v): release-latest.json must hold %s fetched at %s, got %q at %s (found %t)", i+1, args, ucLatest, f.time(), tag, at, ok)
			}
		}
		if f.lookupsStarted() != 0 {
			t.Fatalf("prepare and --version start no background lookup, got %d", f.lookupsStarted())
		}
		ucRepoHasNoReleaseFiles(t, repo)
	})

	t.Run("the notice answers from a fresh cache without a lookup", func(t *testing.T) {
		for _, age := range []time.Duration{time.Minute, 3*time.Hour + 59*time.Minute, 4 * time.Hour} {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.terminals(true, true)
			f.writeCache(ucLatest, f.time().Add(-age))
			res := f.run("status")
			if !strings.Contains(res.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
				t.Fatalf("a cache %s old must give the notice, stderr:\n%s", age, res.stderr)
			}
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
				t.Fatalf("a cache %s old is fresh: want no lookup, got %d requests", age, f.releases.requests())
			}
		}
	})

	t.Run("an expired cache starts one background lookup that refreshes it", func(t *testing.T) {
		for _, tc := range []struct{ name, content string }{
			{"missing", ""},
			{"more than 4 hours old", `{"tag":"0.27.1","fetched_at":"2026-11-12T07:59:00Z"}`},
			{"fetched in the future", `{"tag":"0.27.1","fetched_at":"2026-11-12T12:01:00Z"}`},
			{"a tag that is not X.Y.Z", `{"tag":"latest","fetched_at":"2026-11-12T11:00:00Z"}`},
			{"a tag with a v", `{"tag":"v0.27.1","fetched_at":"2026-11-12T11:00:00Z"}`},
			{"unreadable", `{"tag":`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newUCFixture(t)
				f.connectedRepo(false)
				f.terminals(true, true)
				if tc.content != "" {
					f.writeFile("release-latest.json", tc.content)
				}
				f.setAPIWait(ucWaitFinished)
				res := f.run("status")
				if got := f.releases.requests(); got != 1 {
					t.Fatalf("an expired cache (%s) must send one background lookup, got %d", tc.name, got)
				}
				if tag, at, ok := f.cacheEntry(); !ok || tag != ucLatest || !at.Equal(f.time()) {
					t.Fatalf("the lookup must refresh release-latest.json, got %q at %s (found %t)", tag, at, ok)
				}
				if !strings.Contains(res.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
					t.Fatalf("a lookup that finished before the command ended feeds the notice, stderr:\n%s", res.stderr)
				}
			})
		}
	})

	t.Run("a fresh cache older than the running release reads as current", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.writeCache("0.25.0", f.time().Add(-time.Hour))
		res := f.run("status")
		if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
			t.Fatalf("the cache stays fresh: want no lookup, got %d requests", f.releases.requests())
		}
		ucNoStatusLine(t, "status", res.stderr)
	})

	t.Run("after a failed lookup prepare and --version read the cache only for a newer release", func(t *testing.T) {
		for _, tc := range []struct {
			name, tag string
			age       time.Duration
			newer     bool
		}{
			{"the same release", ucRunning, time.Hour, false},
			{"an older release", "0.25.0", time.Hour, false},
			{"a newer release", ucLatest, time.Hour, true},
			{"an expired newer release", ucLatest, 5 * time.Hour, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newUCFixture(t)
				f.connectedRepo(false)
				f.terminals(true, true)
				ucLookupFails(f)
				f.writeCache(tc.tag, f.time().Add(-tc.age))
				prep := f.run("prepare", "--json")
				release := ucRelease(ucReport(t, prep.stdout))
				version := f.run("--version")
				if tc.newer {
					if release == nil || release["state"] != "newer" || release["reason"] != "lookup_failed" {
						t.Fatalf("a fresh newer cache must give newer with reason lookup_failed, got %v", release)
					}
					if strings.TrimSpace(version.stderr) != ucNotice(ucLatest, ucRunning, ucPointer) {
						t.Fatalf("--version must report the newer release from the cache, stderr:\n%s", version.stderr)
					}
					return
				}
				if release == nil || release["state"] != "unknown" || release["reason"] != "lookup_failed" {
					t.Fatalf("the cache must not answer for a failed lookup here, got %v", release)
				}
				if strings.TrimSpace(version.stderr) != "Couldn't check for a newer release." {
					t.Fatalf("--version must say it couldn't check, stderr:\n%s", version.stderr)
				}
			})
		}
	})

	t.Run("the CLI writes its files only under the user cache folder", func(t *testing.T) {
		f := newUCFixture(t)
		repo := f.connectedRepo(false)
		f.run("prepare") // the cache
		if _, ok := f.readFile("release-latest.json"); !ok {
			t.Fatal("prepare's successful lookup must write release-latest.json in the CLI's folder under the user cache folder")
		}
		f.terminals(true, true)
		f.run("status") // the notice record
		if err := os.Remove(filepath.Join(f.folder(), "release-latest.json")); err != nil {
			t.Fatal(err)
		}
		ucLookupFails(f)
		f.setAPIWait(ucWaitFinished)
		f.run("status") // the back-off record
		for _, name := range []string{"release-notice.json", "release-attempt.json"} {
			if _, ok := f.readFile(name); !ok {
				t.Fatalf("%s must be in the CLI's folder under the user cache folder", name)
			}
		}
		ucRepoHasNoReleaseFiles(t, repo)
	})

	t.Run("without a user cache folder nothing is cached and no notice shows", func(t *testing.T) {
		f := newUCFixture(t)
		repo := f.connectedRepo(false)
		ucSwap(t, &userCacheFolder, func() (string, error) { return "", os.ErrNotExist })
		f.terminals(true, true)
		prep := f.run("prepare", "--json")
		if release := ucRelease(ucReport(t, prep.stdout)); release == nil || release["state"] != "newer" {
			t.Fatalf("prepare still looks up without a cache folder, got %v", release)
		}
		status := f.run("status")
		ucNoStatusLine(t, "status", status.stderr)
		if f.releases.requests() != 1 || f.lookupsStarted() != 0 {
			t.Fatalf("want prepare's one request and no background lookup, got %d requests and %d lookups", f.releases.requests(), f.lookupsStarted())
		}
		ucRepoHasNoReleaseFiles(t, repo)
		if _, err := os.Stat(f.folder()); err == nil {
			t.Fatal("nothing may be written when the user has no cache folder")
		}
	})
}

// Criterion 11b4507f: the background lookup never runs for prepare or
// --version, never changes a command's stdout and exit code, is cancelled
// without a failure record when the command ends, and backs off an hour after
// a failure. TestUpdateCheckCutOffLookup covers the cut-off record and the
// wait after it.
func TestUpdateCheckBackgroundLookup(t *testing.T) {
	baseline := func(f *ucFixture) ucRun {
		f.t.Helper()
		f.terminals(false, false)
		res := f.run("status")
		f.terminals(true, true)
		return res
	}

	t.Run("answers at once", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		want := baseline(f)
		f.setAPIWait(ucWaitFinished)
		res := f.run("status")
		if res.code != want.code || res.stdout != want.stdout {
			t.Fatalf("the lookup changed the command: exit %d/%d, stdout:\n%s\nwant:\n%s", res.code, want.code, res.stdout, want.stdout)
		}
		if f.releases.requests() != 1 || f.lookupsStarted() != 1 {
			t.Fatalf("want one background lookup, got %d requests and %d lookups", f.releases.requests(), f.lookupsStarted())
		}
		if tag, at, ok := f.cacheEntry(); !ok || tag != ucLatest || !at.Equal(f.time()) {
			t.Fatalf("a lookup that succeeds before the command ends updates the cache, got %q at %s (found %t)", tag, at, ok)
		}
	})

	for _, tc := range []struct {
		name   string
		answer func(s *ucReleaseServer) func()
	}{
		{"answers late", func(s *ucReleaseServer) func() {
			s.set(s.late(ucTag("v0.27.1")))
			return s.free
		}},
		{"never answers", func(s *ucReleaseServer) func() {
			s.set(s.never())
			return func() {}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(false)
			want := baseline(f)
			const expired = `{"tag":"0.27.0","fetched_at":"2026-11-12T07:00:00Z"}`
			f.writeFile("release-latest.json", expired)
			after := tc.answer(f.releases)
			f.setAPIWait(ucWaitReceived)
			res := f.run("status") // returns while the release server still holds the request
			after()
			if res.code != want.code || res.stdout != want.stdout {
				t.Fatalf("the lookup changed the command: exit %d/%d, stdout:\n%s\nwant:\n%s", res.code, want.code, res.stdout, want.stdout)
			}
			if f.releases.requests() != 1 || f.lookupsStarted() != 1 {
				t.Fatalf("want one background lookup, got %d requests and %d lookups", f.releases.requests(), f.lookupsStarted())
			}
			f.waitLookups()
			if got, _ := f.readFile("release-latest.json"); got != expired {
				t.Fatalf("a cancelled lookup must leave the cache as it was, got %q", got)
			}
			// It may record a cut-off, which TestUpdateCheckCutOffLookup checks.
			if raw, ok := f.readFile("release-attempt.json"); ok {
				var attempt map[string]any
				if err := json.Unmarshal([]byte(raw), &attempt); err != nil {
					t.Fatalf("release-attempt.json is not a JSON object: %v\n%s", err, raw)
				}
				if _, failed := attempt["failed_at"]; failed {
					t.Fatalf("a cancelled lookup records no failed_at, got %s", raw)
				}
			}
			ucNoStatusLine(t, "status", res.stderr)
		})
	}

	t.Run("a failed lookup is recorded and holds the next one back for an hour", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		ucLookupFails(f)
		f.setAPIWait(ucWaitFinished)
		f.run("status")
		raw, ok := f.readFile("release-attempt.json")
		var attempt struct {
			FailedAt string `json:"failed_at"`
		}
		if !ok || json.Unmarshal([]byte(raw), &attempt) != nil || attempt.FailedAt != f.time().Format(time.RFC3339) {
			t.Fatalf("release-attempt.json must record the failure at %s, got %q (found %t)", f.time().Format(time.RFC3339), raw, ok)
		}
		f.advance(59 * time.Minute)
		f.run("status")
		if f.releases.requests() != 1 {
			t.Fatalf("within the hour no new background lookup may start, got %d requests", f.releases.requests())
		}
		f.advance(2 * time.Minute)
		f.run("status")
		if f.releases.requests() != 2 {
			t.Fatalf("after the hour the next command looks up again, got %d requests", f.releases.requests())
		}
	})

	t.Run("a back-off record dated in the future holds nothing back", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.writeFile("release-attempt.json", `{"failed_at":"`+f.time().Add(10*time.Minute).Format(time.RFC3339)+`"}`)
		f.setAPIWait(ucWaitFinished)
		f.run("status")
		if f.releases.requests() != 1 {
			t.Fatalf("a future-dated record must not hold the lookup back, got %d requests", f.releases.requests())
		}
	})

	t.Run("a cache folder the CLI cannot write starts no lookup", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		file := filepath.Join(t.TempDir(), "a-file")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		ucSwap(t, &userCacheFolder, func() (string, error) { return filepath.Join(file, "cache"), nil })
		res := f.run("status")
		if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
			t.Fatalf("an unwritable folder starts no background lookup, got %d requests", f.releases.requests())
		}
		if res.code != 0 || strings.TrimSpace(res.stderr) != "" {
			t.Fatalf("an unwritable cache folder is silent: exit %d, stderr:\n%s", res.code, res.stderr)
		}
	})

	t.Run("prepare and --version send one request and start no background lookup", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		for _, args := range [][]string{{"prepare"}, {"--api-url", f.api.URL, "prepare"}, {"prepare", "--json"}, {"--version"}, {"--version=true"}} {
			_ = os.Remove(filepath.Join(f.folder(), "release-latest.json")) // an expired cache each time
			before := f.releases.requests()
			f.run(args...)
			if got := f.releases.requests() - before; got != 1 {
				t.Fatalf("%v must send exactly one request, got %d", args, got)
			}
			if f.lookupsStarted() != 0 {
				t.Fatalf("%v started a background lookup", args)
			}
		}
	})

	// Mattias's review of #1221: --help --version prints help and asks for no
	// release, so it starts no background lookup either. With an expired cache
	// and an earlier cut-off, such a lookup would hold the help for 3 seconds.
	// --help alone keeps the background lookup any quick command starts.
	t.Run("--help with --version starts no background lookup, also after a cut-off", func(t *testing.T) {
		const expired = `{"tag":"0.27.0","fetched_at":"2026-11-12T07:00:00Z"}` // 5 hours before ucStart
		for _, args := range [][]string{{"--help", "--version"}, {"-h", "--version"}, {"--version", "--help"}} {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.terminals(true, true)
			f.writeFile("release-latest.json", expired)
			f.writeFile("release-attempt.json", `{"cut_off_at":"`+f.time().Add(-time.Minute).UTC().Format(time.RFC3339)+`"}`)
			f.releases.set(f.releases.never())
			started := time.Now()
			res := f.run(args...)
			if elapsed := time.Since(started); elapsed >= time.Second {
				t.Fatalf("%v took %s, want no wait for a release lookup", args, elapsed)
			}
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 || f.waitsStarted() != 0 {
				t.Fatalf("%v must send no request, got %d requests, %d lookups and %d waits", args, f.releases.requests(), f.lookupsStarted(), f.waitsStarted())
			}
			if !strings.Contains(res.stdout, "Usage:") {
				t.Fatalf("%v prints help, stdout:\n%s", args, res.stdout)
			}
			ucNoStatusLine(t, strings.Join(args, " "), res.stderr)
		}

		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.writeFile("release-latest.json", expired)
		f.run("--help")
		if f.lookupsStarted() != 1 {
			t.Fatalf("control: --help alone starts a background lookup on an expired cache, got %d", f.lookupsStarted())
		}
	})
}

// Criterion 1e31804c: a background lookup that its command cancels after its
// request went out records cut_off_at and leaves the cache as it was. While
// that cut-off is newer than the cache's last successful lookup and isn't
// dated in the future, the next command that starts a background lookup gives
// it 3 seconds and waits for it after its own output; a lookup that runs out
// records the failure in place of the cut-off.
func TestUpdateCheckCutOffLookup(t *testing.T) {
	const expired = `{"tag":"0.27.0","fetched_at":"2026-11-12T07:00:00Z"}` // 5 hours before ucStart
	cutOffAt := func(at time.Time) string { return `{"cut_off_at":"` + at.UTC().Format(time.RFC3339) + `"}` }
	newTerminalRepo := func(t *testing.T) *ucFixture {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		return f
	}
	// baseline is the same command without the update check.
	baseline := func(f *ucFixture) ucRun {
		f.t.Helper()
		f.terminals(false, false)
		res := f.run("status")
		f.terminals(true, true)
		return res
	}

	t.Run("a lookup cut off after its request went out is recorded", func(t *testing.T) {
		for _, tc := range []struct{ name, attempt, failedAt string }{
			{"with no earlier record", "", ""},
			{"keeping an earlier failure", `{"failed_at":"2026-11-12T10:00:00Z"}`, "2026-11-12T10:00:00Z"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newTerminalRepo(t)
				f.writeFile("release-latest.json", expired)
				if tc.attempt != "" {
					f.writeFile("release-attempt.json", tc.attempt)
				}
				f.releases.set(f.releases.late(ucTag("v" + ucLatest)))
				f.setAPIWait(ucWaitReceived)
				f.run("status") // ends while the release server holds the request
				f.releases.free()
				if f.releases.requests() != 1 || f.lookupsStarted() != 1 {
					t.Fatalf("want one background lookup, got %d requests and %d lookups", f.releases.requests(), f.lookupsStarted())
				}
				attempt := ucAttempt(t, f, "a lookup cut off after its request went out must record cut_off_at")
				if attempt["cut_off_at"] != f.time().Format(time.RFC3339) {
					t.Fatalf("release-attempt.json must record cut_off_at %s, got %v", f.time().Format(time.RFC3339), attempt)
				}
				failedAt, failed := attempt["failed_at"]
				if tc.failedAt == "" && failed {
					t.Fatalf("a cut-off is not a failure: want no failed_at, got %v", attempt)
				}
				if tc.failedAt != "" && failedAt != tc.failedAt {
					t.Fatalf("the cut-off keeps the failed_at already there (%s), got %v", tc.failedAt, attempt)
				}
				if got, _ := f.readFile("release-latest.json"); got != expired {
					t.Fatalf("a cut-off lookup must leave the cache as it was, got %q", got)
				}
				if f.waitsStarted() != 0 {
					t.Fatalf("without an earlier cut-off the command must not wait, got %d waits", f.waitsStarted())
				}
			})
		}
	})

	t.Run("the next command waits for its lookup, which refreshes the cache", func(t *testing.T) {
		for _, tc := range []struct{ name, cache string }{
			{"after a cut-off newer than an expired cache", expired},
			{"after a cut-off with no release cache", ""},
			{"after a cut-off, with a cache fetch time dated in the future", `{"tag":"0.27.0","fetched_at":"2026-11-12T13:00:00Z"}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newTerminalRepo(t)
				want := baseline(f)
				if tc.cache != "" {
					f.writeFile("release-latest.json", tc.cache)
				}
				f.writeFile("release-attempt.json", cutOffAt(f.time().Add(-time.Minute)))
				// The release server answers only once the end-of-run wait has started.
				f.releases.set(f.answerWhen(f.waitSignal, ucTag("v"+ucLatest)))
				res := f.run("status")
				if res.code != want.code || res.stdout != want.stdout {
					t.Fatalf("the wait changed the command: exit %d/%d, stdout:\n%s\nwant:\n%s", res.code, want.code, res.stdout, want.stdout)
				}
				lines := ucLines(res.stderr)
				if len(lines) == 0 || lines[len(lines)-1] != ucNotice(ucLatest, ucRunning, ucPointer) {
					t.Errorf("after a cut-off, status must wait for its lookup and end with the notice, stderr:\n%s", res.stderr)
				}
				if tag, at, ok := f.cacheEntry(); !ok || tag != ucLatest || !at.Equal(f.time()) {
					t.Errorf("the lookup it waits for must refresh release-latest.json, got %q at %s (found %t)", tag, at, ok)
				}
				if f.releases.requests() != 1 || f.waitsStarted() != 1 {
					t.Errorf("want one request and one end-of-run wait, got %d requests and %d waits", f.releases.requests(), f.waitsStarted())
				}
			})
		}
	})

	t.Run("a lookup that never answers is recorded as a failure in place of the cut-off", func(t *testing.T) {
		f := newTerminalRepo(t)
		want := baseline(f)
		f.writeFile("release-latest.json", expired)
		f.writeFile("release-attempt.json", cutOffAt(f.time().Add(-time.Minute)))
		f.releases.set(f.releases.never())
		started := time.Now()
		res := f.run("status")
		if took := time.Since(started); took < 2900*time.Millisecond || took > 4500*time.Millisecond {
			t.Errorf("status took %s, want 2.9s to 4.5s: after a cut-off it waits up to 3 seconds for its lookup", took)
		}
		if res.code != want.code || res.stdout != want.stdout || strings.TrimSpace(res.stderr) != "" {
			t.Errorf("a wait that runs out must not change the command or print an error: exit %d/%d, stderr %q, stdout:\n%s\nwant:\n%s", res.code, want.code, res.stderr, res.stdout, want.stdout)
		}
		attempt := ucAttempt(t, f, "a lookup that runs out must record failed_at")
		if attempt["failed_at"] != f.time().Format(time.RFC3339) {
			t.Fatalf("release-attempt.json must record failed_at %s, got %v", f.time().Format(time.RFC3339), attempt)
		}
		if _, cut := attempt["cut_off_at"]; cut {
			t.Fatalf("the failure takes the cut-off's place: want no cut_off_at, got %v", attempt)
		}
		if got, _ := f.readFile("release-latest.json"); got != expired {
			t.Fatalf("a failed lookup leaves the cache as it was, got %q", got)
		}
		ucNoStatusLine(t, "status", res.stderr)
		f.advance(30 * time.Minute)
		f.run("status")
		if f.releases.requests() != 1 || f.waitsStarted() != 1 {
			t.Fatalf("within the hour after the failure the next status sends no request and doesn't wait, got %d requests and %d waits", f.releases.requests(), f.waitsStarted())
		}
	})

	t.Run("a waited-for lookup that fails fast is recorded as a failure in place of the cut-off", func(t *testing.T) {
		f := newTerminalRepo(t)
		f.writeFile("release-latest.json", expired)
		f.writeFile("release-attempt.json", cutOffAt(f.time().Add(-time.Minute)))
		ucLookupFails(f)
		f.run("status")
		attempt := ucAttempt(t, f, "a waited-for lookup that fails must record failed_at")
		if attempt["failed_at"] != f.time().Format(time.RFC3339) {
			t.Fatalf("release-attempt.json must record failed_at %s, got %v", f.time().Format(time.RFC3339), attempt)
		}
		if _, cut := attempt["cut_off_at"]; cut {
			t.Fatalf("the failure takes the cut-off's place: want no cut_off_at, got %v", attempt)
		}
		if got, _ := f.readFile("release-latest.json"); got != expired {
			t.Fatalf("a failed lookup leaves the cache as it was, got %q", got)
		}
	})

	t.Run("no wait", func(t *testing.T) {
		for _, tc := range []struct {
			name, cache, attempt string
			lookup               bool // a background lookup starts: a cut-off holds none back
		}{
			{"a cut-off older than the cache's last fetch", expired, cutOffAt(ucStart.Add(-6 * time.Hour)), true},
			{"a cut-off dated in the future", expired, cutOffAt(ucStart.Add(10 * time.Minute)), true},
			{"an empty cut-off", expired, `{"cut_off_at":""}`, true},
			{"no release-attempt.json", expired, "", true},
			{"a cut-off with a fresh cache", `{"tag":"0.27.1","fetched_at":"2026-11-12T11:58:00Z"}`, cutOffAt(ucStart.Add(-time.Minute)), false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newTerminalRepo(t)
				f.writeFile("release-latest.json", tc.cache)
				if tc.attempt != "" {
					f.writeFile("release-attempt.json", tc.attempt)
				}
				f.releases.set(f.releases.never())
				f.setAPIWait(ucWaitReceived)
				f.run("status")
				if f.waitsStarted() != 0 {
					t.Fatalf("the end of the run must not wait for the lookup, got %d waits", f.waitsStarted())
				}
				want := 0
				if tc.lookup {
					want = 1
				}
				if f.releases.requests() != want || f.lookupsStarted() != want {
					t.Fatalf("want %d background lookups, got %d requests and %d lookups", want, f.releases.requests(), f.lookupsStarted())
				}
			})
		}
	})

	t.Run("a lookup cancelled before its request went out records nothing", func(t *testing.T) {
		f := newTerminalRepo(t)
		f.writeFile("release-latest.json", expired)
		transport := &ucBlockingTransport{entered: make(chan struct{})}
		ucSwap(t, &releaseLookupClient, &http.Client{Transport: transport})
		f.holdAPIUntil(transport.entered) // the command ends while the lookup is inside the client
		f.run("status")
		if f.lookupsStarted() != 1 || f.releases.requests() != 0 {
			t.Fatalf("want one background lookup that sends nothing, got %d lookups and %d requests", f.lookupsStarted(), f.releases.requests())
		}
		if raw, ok := f.readFile("release-attempt.json"); ok {
			t.Fatalf("a lookup cancelled before its request went out records nothing, got %s", raw)
		}
		if got, _ := f.readFile("release-latest.json"); got != expired {
			t.Fatalf("a cancelled lookup leaves the cache as it was, got %q", got)
		}
	})
}

// Criterion 386007d8: one stderr line about once a day per user and CLI
// release, with the pointer to prepare for unknown installs; --version looks
// up on every run within 1 second and states newer, latest, no newer or could
// not check.
func TestUpdateCheckNoticeAndVersionLine(t *testing.T) {
	notice := func(f *ucFixture) string {
		f.t.Helper()
		f.writeCache(ucLatest, f.time().Add(-time.Minute)) // fresh, so no lookup
		return f.run("status").stderr
	}

	t.Run("one line for a recognized install and for an unknown one", func(t *testing.T) {
		for _, tc := range []struct{ binary, upgrade string }{
			{"/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath", ucBrew},
			{"/opt/elsewhere/bin/modernpath", ucPointer},
		} {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.terminals(true, true)
			f.binary(tc.binary)
			if got, want := notice(f), ucNotice(ucLatest, ucRunning, tc.upgrade)+"\n"; got != want {
				t.Fatalf("stderr = %q, want exactly %q", got, want)
			}
			raw, ok := f.readFile("release-notice.json")
			var record struct {
				Tag     string `json:"tag"`
				ShownAt string `json:"shown_at"`
			}
			if !ok || json.Unmarshal([]byte(raw), &record) != nil || record.Tag != ucLatest || record.ShownAt != f.time().Format(time.RFC3339) {
				t.Fatalf("release-notice.json must record %s shown at %s, got %q", ucLatest, f.time().Format(time.RFC3339), raw)
			}
		}
	})

	t.Run("about once in 24 hours for each CLI release", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		if !strings.Contains(notice(f), "is available") {
			t.Fatal("the first command must show the notice")
		}
		f.advance(23*time.Hour + 59*time.Minute)
		if got := notice(f); strings.Contains(got, "is available") {
			t.Fatalf("within 24 hours the notice must not repeat:\n%s", got)
		}
		f.advance(time.Minute)
		if !strings.Contains(notice(f), "is available") {
			t.Fatal("after 24 hours the notice shows again")
		}
		f.advance(time.Hour)
		f.writeCache("0.27.2", f.time().Add(-time.Minute))
		if got := f.run("status").stderr; !strings.Contains(got, ucNotice("0.27.2", ucRunning, ucPointer)) {
			t.Fatalf("a newer CLI release gets its own notice within the 24 hours:\n%s", got)
		}
	})

	t.Run("no line when the record can't be written", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		if err := os.MkdirAll(filepath.Join(f.folder(), "release-notice.json"), 0o755); err != nil {
			t.Fatal(err)
		}
		if got := notice(f); strings.TrimSpace(got) != "" {
			t.Fatalf("an unwritable record must suppress the notice, stderr:\n%s", got)
		}
	})

	t.Run("a record dated in the future holds nothing back", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.writeFile("release-notice.json", `{"tag":"0.27.1","shown_at":"`+f.time().Add(2*time.Hour).Format(time.RFC3339)+`"}`)
		if !strings.Contains(notice(f), "is available") {
			t.Fatal("a notice record dated in the future must not hold the notice back")
		}
		if raw, _ := f.readFile("release-notice.json"); !strings.Contains(raw, f.time().Format(time.RFC3339)) {
			t.Fatalf("the record must be rewritten when the line shows, got %q", raw)
		}
	})

	t.Run("--version states the release status after its first line", func(t *testing.T) {
		for _, tc := range []struct {
			name, tag, line string
		}{
			{"newer", "v0.27.1", ucNotice(ucLatest, ucRunning, ucBrew)},
			{"the latest release", "v0.26.0", "This is the latest release."},
			{"above the latest release", "0.25.0", "No newer release is available."},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newUCFixture(t)
				f.connectedRepo(false)
				f.terminals(true, true)
				f.binary("/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath")
				f.releases.set(ucTag(tc.tag))
				for run := 1; run <= 2; run++ {
					started := time.Now()
					res := f.run("--version")
					if elapsed := time.Since(started); elapsed > 3*time.Second {
						t.Fatalf("--version took %s", elapsed)
					}
					if res.stdout != "modernpath version 0.26.0\n" {
						t.Fatalf("run %d: the first line must stay as it is, stdout = %q", run, res.stdout)
					}
					if res.stderr != tc.line+"\n" {
						t.Fatalf("run %d: stderr = %q, want %q", run, res.stderr, tc.line+"\n")
					}
					if f.releases.requests() != run {
						t.Fatalf("--version looks up on every run, got %d requests after %d runs", f.releases.requests(), run)
					}
				}
				if _, ok := f.readFile("release-notice.json"); ok {
					t.Fatal("a --version showing doesn't count toward the 24 hours")
				}
			})
		}
	})

	t.Run("--version after a failed lookup", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			age  time.Duration // of a cache that says 0.27.1; 0 means no cache
			line string
		}{
			{"no cache", 0, "Couldn't check for a newer release."},
			{"a fresh cache with a newer release", time.Hour, ucNotice(ucLatest, ucRunning, ucPointer)},
			{"an expired cache", 5 * time.Hour, "Couldn't check for a newer release."},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newUCFixture(t)
				f.connectedRepo(false)
				f.terminals(true, true)
				ucLookupFails(f)
				if tc.age > 0 {
					f.writeCache(ucLatest, f.time().Add(-tc.age))
				}
				res := f.run("--version")
				if res.code != 0 || res.stdout != "modernpath version 0.26.0\n" || res.stderr != tc.line+"\n" {
					t.Fatalf("exit %d, stdout %q, stderr %q; want stderr %q", res.code, res.stdout, res.stderr, tc.line+"\n")
				}
			})
		}
	})

	t.Run("--version shows the notice and it still counts for nothing", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.run("--version")
		if !strings.Contains(notice(f), "is available") {
			t.Fatal("after --version showed the newer release, the next command still shows the notice")
		}
	})

	t.Run("nothing extra for a local build or with the check off", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			version string
			env     map[string]string
		}{
			{"local build", "0.27.0-dev+abc1234 (2026-11-10T15:09Z)", nil},
			{"CI", ucRunning, map[string]string{"CI": "true"}},
			{"opt-out", ucRunning, map[string]string{"MODERNPATH_NO_UPDATE_CHECK": "1"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				f := newUCFixture(t)
				f.connectedRepo(false)
				f.terminals(true, true)
				ucSetVersion(t, tc.version)
				for key, value := range tc.env {
					f.setEnv(key, value)
				}
				res := f.run("--version")
				if res.stdout != "modernpath version "+tc.version+"\n" || res.stderr != "" || f.releases.requests() != 0 {
					t.Fatalf("stdout %q, stderr %q, %d requests; want only the first line", res.stdout, res.stderr, f.releases.requests())
				}
			})
		}
	})

	t.Run("--help --version, -- --version and --version --bogus print no status line", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.writeCache(ucRunning, f.time().Add(-time.Minute)) // current: the end-of-run notice can't print either
		for _, args := range [][]string{{"--help", "--version"}, {"--", "--version"}, {"--version", "--bogus"}} {
			res := f.run(args...)
			ucNoStatusLine(t, strings.Join(args, " "), res.stderr)
			if f.releases.requests() != 0 {
				t.Fatalf("%v sent %d requests", args, f.releases.requests())
			}
		}
	})
}

// Criterion 47c073a2: for commands other than prepare, no notice, no
// --version status line and no request when stdout or stderr is not a
// terminal, with JSON output, in hooks and in CI; no background lookup with
// --offline or in reverse-engineer check.
func TestUpdateCheckSuppression(t *testing.T) {
	// fresh gives the notice something to show; expired gives the
	// background lookup something to do.
	fresh := func(f *ucFixture) { f.writeCache(ucLatest, f.time().Add(-time.Minute)) }
	expired := func(f *ucFixture) { _ = os.Remove(filepath.Join(f.folder(), "release-latest.json")) }
	newRepo := func(t *testing.T) *ucFixture {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.terminals(true, true)
		f.setAPIWait(ucWaitReceived)
		return f
	}

	t.Run("both terminals: the notice and the background lookup", func(t *testing.T) {
		f := newRepo(t)
		fresh(f)
		res := f.run("status")
		if !strings.Contains(res.stderr, ucNotice(ucLatest, ucRunning, ucPointer)) {
			t.Fatalf("control: both terminals show the notice, stderr:\n%s", res.stderr)
		}
		expired(f)
		f.run("status")
		if f.releases.requests() != 1 {
			t.Fatalf("control: an expired cache starts a background lookup, got %d requests", f.releases.requests())
		}
		// The check reads the process's own streams, not the files that
		// os.Stdout and os.Stderr point to in this test.
		for _, fd := range f.askedDescriptors() {
			if fd != int(syscall.Stdout) && fd != int(syscall.Stderr) {
				t.Fatalf("the terminal check was asked about descriptor %d, want only %d and %d", fd, int(syscall.Stdout), int(syscall.Stderr))
			}
		}
		asked := map[int]bool{}
		for _, fd := range f.askedDescriptors() {
			asked[fd] = true
		}
		if !asked[int(syscall.Stdout)] || !asked[int(syscall.Stderr)] {
			t.Fatalf("the terminal check must be asked about stdout and stderr, got %v", f.askedDescriptors())
		}
	})

	for _, tc := range []struct {
		name           string
		stdout, stderr bool
	}{
		{"stdout piped, stderr a terminal", false, true},
		{"stdout a terminal, stderr redirected", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepo(t)
			f.terminals(tc.stdout, tc.stderr)
			fresh(f)
			ucNoStatusLine(t, "status", f.run("status").stderr)
			version := f.run("--version")
			ucNoStatusLine(t, "--version", version.stderr)
			if version.stdout != "modernpath version 0.26.0\n" {
				t.Fatalf("--version stdout = %q", version.stdout)
			}
			expired(f)
			f.run("status")
			f.run("--version")
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
				t.Fatalf("no request without both terminals, got %d requests", f.releases.requests())
			}
		})
	}

	t.Run("JSON output", func(t *testing.T) {
		f := newRepo(t)
		fresh(f)
		res := f.run("status", "--json")
		ucNoStatusLine(t, "status --json", res.stderr)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(res.stdout), &parsed); err != nil {
			t.Fatalf("status --json must stay one JSON object: %v\n%s", err, res.stdout)
		}
		// A JSON failure writes exactly one stderr line, the error object.
		failure := f.run("ask", "--format", "json", "where is billing?")
		if lines := ucLines(failure.stderr); failure.code == 0 || len(lines) != 1 || !strings.HasPrefix(lines[0], `{"error":`) {
			t.Fatalf("a JSON failure must write one error line, exit %d, stderr:\n%s", failure.code, failure.stderr)
		}
		expired(f)
		f.run("status", "--json")
		f.run("status", "--json=true")
		f.run("ask", "--format=json", "where is billing?")
		if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
			t.Fatalf("JSON runs send no request, got %d requests", f.releases.requests())
		}
	})

	// Mattias's review of #1221: cobra reads --json=1 as --json, so the run
	// asks for JSON and gets neither the notice nor a lookup, also after a
	// cut-off, when a lookup would hold the run for 3 seconds.
	t.Run("JSON output with a boolean value such as --json=1", func(t *testing.T) {
		for _, value := range []string{"1", "TRUE", "t"} {
			args := []string{"status", "--json=" + value}
			f := newRepo(t)
			fresh(f)
			res := f.run(args...)
			ucNoStatusLine(t, strings.Join(args, " "), res.stderr)
			var parsed map[string]any
			if err := json.Unmarshal([]byte(res.stdout), &parsed); err != nil {
				t.Fatalf("%v must print one JSON object: %v\n%s", args, err, res.stdout)
			}
			expired(f)
			f.writeFile("release-attempt.json", `{"cut_off_at":"`+f.time().Add(-time.Minute).UTC().Format(time.RFC3339)+`"}`)
			f.releases.set(f.releases.never())
			started := time.Now()
			f.run(args...)
			if elapsed := time.Since(started); elapsed >= time.Second {
				t.Fatalf("%v took %s after a cut-off, want no wait for a release lookup", args, elapsed)
			}
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 || f.waitsStarted() != 0 {
				t.Fatalf("%v must send no request, got %d requests, %d lookups and %d waits", args, f.releases.requests(), f.lookupsStarted(), f.waitsStarted())
			}
		}
	})

	t.Run("hook runs", func(t *testing.T) {
		f := newRepo(t)
		fresh(f)
		res := f.run("context", "--hook", "UserPromptSubmit")
		ucNoStatusLine(t, "context --hook", res.stderr)
		expired(f)
		f.run("context", "--hook=UserPromptSubmit")
		if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
			t.Fatalf("hook runs send no request, got %d requests", f.releases.requests())
		}
	})

	t.Run("CI", func(t *testing.T) {
		for _, value := range []string{"true", "1", " Yes ", "github-actions"} {
			f := newRepo(t)
			f.setEnv("CI", value)
			fresh(f)
			ucNoStatusLine(t, "status with CI="+value, f.run("status").stderr)
			ucNoStatusLine(t, "--version with CI="+value, f.run("--version").stderr)
			expired(f)
			f.run("status")
			f.run("--version")
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
				t.Fatalf("CI=%q must send no request, got %d requests", value, f.releases.requests())
			}
		}
		for _, value := range []string{"", "0", "false", " FALSE ", "no", "off", "Off "} {
			f := newRepo(t)
			f.setEnv("CI", value)
			fresh(f)
			if !strings.Contains(f.run("status").stderr, "is available") {
				t.Fatalf("CI=%q is not CI: the notice must show", value)
			}
		}
	})

	t.Run("offline runs show the notice but start no background lookup", func(t *testing.T) {
		for _, args := range [][]string{{"auth", "status", "--offline"}, {"auth", "status", "--offline=true"}, {"reverse-engineer", "check"}} {
			f := newRepo(t)
			fresh(f)
			if !strings.Contains(f.run(args...).stderr, "is available") {
				t.Fatalf("%v can show the notice from a fresh cache", args)
			}
			expired(f)
			f.run(args...)
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
				t.Fatalf("%v must start no background lookup, got %d requests", args, f.releases.requests())
			}
		}
	})
}

// Criterion d7a14eb9: prepare looks up without a terminal and with --json, and
// is off with reason ci when CI is on and the opt-out is not.
func TestUpdateCheckPrepareWithoutTerminal(t *testing.T) {
	f := newUCFixture(t)
	f.connectedRepo(false)
	for i, tc := range []struct {
		stdout, stderr bool
		args           []string
	}{
		{false, false, []string{"prepare", "--json"}},
		{false, true, []string{"prepare"}},
		{true, false, []string{"prepare", "--json"}},
	} {
		f.terminals(tc.stdout, tc.stderr)
		res := f.run(tc.args...)
		if f.releases.requests() != i+1 {
			t.Fatalf("%v with stdout terminal=%t and stderr terminal=%t must look up, got %d requests", tc.args, tc.stdout, tc.stderr, f.releases.requests())
		}
		if tc.args[len(tc.args)-1] == "--json" {
			if release := ucRelease(ucReport(t, res.stdout)); release == nil || release["state"] != "newer" || release["latest"] != ucLatest {
				t.Fatalf("prepare --json must report the newer release, got %v", release)
			}
		} else if got := ucCLILine(t, res.stdout); got != "CLI: 0.26.0 (latest release: 0.27.1; upgrade available)" {
			t.Fatalf("CLI line = %q", got)
		}
	}
	f.terminals(false, false)
	f.releases.set(ucTag("v0.26.0"))
	if release := ucRelease(ucReport(t, f.run("prepare", "--json").stdout)); release == nil || release["state"] != "current" {
		t.Fatalf("prepare --json must report current, got %v", release)
	}

	for _, env := range []map[string]string{
		{"CI": "true"},
		{"CI": "1"},
		{"CI": " On "},
		{"CI": "true", "MODERNPATH_NO_UPDATE_CHECK": "0"},
		{"CI": "yes", "MODERNPATH_NO_UPDATE_CHECK": "false"},
	} {
		g := newUCFixture(t)
		g.connectedRepo(false)
		for key, value := range env {
			g.setEnv(key, value)
		}
		res := g.run("prepare", "--json")
		if release := ucRelease(ucReport(t, res.stdout)); release == nil || release["state"] != "off" || release["reason"] != "ci" {
			t.Fatalf("%v: prepare must report off with reason ci, got %v", env, release)
		}
		if g.releases.requests() != 0 {
			t.Fatalf("%v: prepare must send no request, got %d", env, g.releases.requests())
		}
	}
	g := newUCFixture(t)
	g.connectedRepo(false)
	g.setEnv("CI", "false")
	g.run("prepare", "--json")
	if g.releases.requests() != 1 {
		t.Fatalf("CI=false is not CI: prepare must look up, got %d requests", g.releases.requests())
	}
}

// Criterion d120d1b2: release lookup failures print no error, keep the cache
// and change no exit code by themselves; prepare waits at most 3 seconds and
// --version at most 1 second, and both say the check failed, or report a
// newer release from a fresh cache, which prepare marks as a failed lookup.
func TestUpdateCheckLookupFailures(t *testing.T) {
	refused := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := ln.Addr().String()
		_ = ln.Close()
		return "http://" + addr + "/repos/ModernPath/cli/releases/latest"
	}()
	foundAt := ucStart.Add(-2 * time.Hour)
	cached := `{"tag":"0.27.1","fetched_at":"` + foundAt.Format(time.RFC3339) + `"}`
	fromCache := "CLI: 0.26.0 (latest release: 0.27.1, found 2026-11-12T10:00Z; upgrade available; this lookup failed)"

	newWorkspace := func(t *testing.T, kind string) *ucFixture {
		f := newUCFixture(t)
		f.connectedRepo(true) // kit installed: prepare exits 0 unless the release says otherwise
		f.terminals(true, true)
		switch kind {
		case "connection refused":
			ucSwap(t, &releaseLookupURL, refused)
		case "timeout":
			f.releases.set(f.releases.never())
		case "403", "429", "500":
			code := map[string]int{"403": 403, "429": 429, "500": 500}[kind]
			f.releases.set(ucStatus(code))
		case "malformed tag":
			f.releases.set(ucTag("latest"))
		case "pre-release tag":
			f.releases.set(ucTag("v0.27.1-rc.1"))
		case "malformed JSON":
			f.releases.set(ucBody(`{"tag_name":`))
		}
		return f
	}

	t.Run("control: the workspace exits 0 when the CLI is up to date", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(true)
		f.releases.set(ucTag("v0.26.0"))
		if res := f.run("prepare"); res.code != 0 {
			t.Fatalf("the fixture must be a workspace prepare passes, exit %d:\n%s%s", res.code, res.stdout, res.stderr)
		}
	})

	for _, kind := range []string{"connection refused", "403", "429", "500", "malformed tag", "pre-release tag", "malformed JSON"} {
		t.Run(kind, func(t *testing.T) {
			f := newWorkspace(t, kind)
			prep := f.run("prepare")
			if prep.code != 0 || prep.stderr != "" || ucCLILine(t, prep.stdout) != "CLI: 0.26.0 (latest release unknown: the lookup failed)" {
				t.Fatalf("a failed lookup alone keeps prepare at exit 0 with no error: exit %d, CLI %q, stderr %q", prep.code, ucCLILine(t, prep.stdout), prep.stderr)
			}
			version := f.run("--version")
			if version.code != 0 || version.stdout != "modernpath version 0.26.0\n" || version.stderr != "Couldn't check for a newer release.\n" {
				t.Fatalf("--version: exit %d, stdout %q, stderr %q", version.code, version.stdout, version.stderr)
			}
			if _, ok := f.readFile("release-latest.json"); ok {
				t.Fatal("a failed lookup writes no cache")
			}

			f.writeFile("release-latest.json", cached)
			prep = f.run("prepare")
			if prep.code == 0 || prep.stderr != "" || ucCLILine(t, prep.stdout) != fromCache {
				t.Fatalf("a fresh newer cache: want a non-zero exit and %q, got exit %d, CLI %q, stderr %q", fromCache, prep.code, ucCLILine(t, prep.stdout), prep.stderr)
			}
			if steps := ucNextSteps(prep.stdout); len(steps) == 0 || steps[0] != ucPlatformUpgrade(runtime.GOOS) {
				t.Fatalf("the upgrade must come first, got %q", steps)
			}
			version = f.run("--version")
			if version.code != 0 || version.stderr != ucNotice(ucLatest, ucRunning, ucPointer)+"\n" {
				t.Fatalf("--version must report the newer release from the cache: exit %d, stderr %q", version.code, version.stderr)
			}
			if got, _ := f.readFile("release-latest.json"); got != cached {
				t.Fatalf("a failed lookup leaves the cache as it was, got %q", got)
			}

			// A background lookup that fails prints nothing either.
			const expired = `{"tag":"0.26.0","fetched_at":"2026-11-12T06:00:00Z"}`
			f.writeFile("release-latest.json", expired)
			f.setAPIWait(ucWaitFinished)
			status := f.run("status")
			if status.code != 0 || strings.TrimSpace(status.stderr) != "" {
				t.Fatalf("a failed background lookup is silent: exit %d, stderr %q", status.code, status.stderr)
			}
			if got, _ := f.readFile("release-latest.json"); got != expired {
				t.Fatalf("a failed background lookup leaves the cache as it was, got %q", got)
			}
		})
	}

	t.Run("a lookup that times out after 3 seconds in prepare and 1 second in --version", func(t *testing.T) {
		f := newWorkspace(t, "timeout")
		started := time.Now()
		prep := f.run("prepare")
		prepTook := time.Since(started)
		started = time.Now()
		version := f.run("--version")
		versionTook := time.Since(started)
		if prep.code != 0 || prep.stderr != "" || ucCLILine(t, prep.stdout) != "CLI: 0.26.0 (latest release unknown: the lookup failed)" {
			t.Fatalf("prepare: exit %d, CLI %q, stderr %q", prep.code, ucCLILine(t, prep.stdout), prep.stderr)
		}
		if version.stderr != "Couldn't check for a newer release.\n" {
			t.Fatalf("--version stderr = %q", version.stderr)
		}
		for name, bound := range map[string]struct{ took, min, max time.Duration }{
			"prepare":   {prepTook, 2900 * time.Millisecond, 4500 * time.Millisecond},
			"--version": {versionTook, 900 * time.Millisecond, 2500 * time.Millisecond},
		} {
			if bound.took < bound.min || bound.took > bound.max {
				t.Fatalf("%s waited %s, want %s to %s: the lookup gives up after 3 seconds in prepare and 1 second in --version", name, bound.took, bound.min, bound.max)
			}
		}

		// A background lookup that times out is a failed lookup, not a
		// cancelled one: it is recorded, which holds the next background
		// lookup back an hour, and it leaves the cache as it was. The
		// command's own API waits for it, so the timeout ends it.
		const expired = `{"tag":"0.26.0","fetched_at":"2026-11-12T06:00:00Z"}`
		f.writeFile("release-latest.json", expired)
		f.setAPIWait(ucWaitFinished)
		status := f.run("status")
		if status.code != 0 || strings.TrimSpace(status.stderr) != "" {
			t.Fatalf("a background lookup that times out is silent: exit %d, stderr %q", status.code, status.stderr)
		}
		if f.lookupsStarted() != 1 {
			t.Fatalf("status with an expired cache must start one background lookup, got %d", f.lookupsStarted())
		}
		raw, ok := f.readFile("release-attempt.json")
		var attempt struct {
			FailedAt string `json:"failed_at"`
		}
		if !ok || json.Unmarshal([]byte(raw), &attempt) != nil || attempt.FailedAt != f.time().Format(time.RFC3339) {
			t.Fatalf("a background lookup that times out must be recorded in release-attempt.json at %s, got %q (found %t)", f.time().Format(time.RFC3339), raw, ok)
		}
		if got, _ := f.readFile("release-latest.json"); got != expired {
			t.Fatalf("a background lookup that times out leaves the cache as it was, got %q", got)
		}
	})
}

// Criterion 1abd8f02: MODERNPATH_NO_UPDATE_CHECK turns every lookup, notice and
// --version status line off; prepare reports off with reason opt_out.
func TestUpdateCheckOptOut(t *testing.T) {
	for _, value := range []string{"1", "true", " YES ", "please", "On"} {
		t.Run("on: "+value, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.terminals(true, true)
			f.setEnv("MODERNPATH_NO_UPDATE_CHECK", value)
			f.writeCache(ucLatest, f.time().Add(-time.Minute))
			ucNoStatusLine(t, "status", f.run("status").stderr)
			version := f.run("--version")
			if version.stdout != "modernpath version 0.26.0\n" || version.stderr != "" {
				t.Fatalf("--version: stdout %q, stderr %q", version.stdout, version.stderr)
			}
			_ = os.Remove(filepath.Join(f.folder(), "release-latest.json"))
			f.run("status")
			prep := f.run("prepare")
			if got := ucCLILine(t, prep.stdout); got != "CLI: 0.26.0 (update check off: MODERNPATH_NO_UPDATE_CHECK is set)" {
				t.Fatalf("CLI line = %q", got)
			}
			report := ucReport(t, f.run("prepare", "--json").stdout)
			if release := ucRelease(report); release == nil || release["state"] != "off" || release["reason"] != "opt_out" {
				t.Fatalf("cli_release must be off with reason opt_out, got %v", report["cli_release"])
			}
			if f.releases.requests() != 0 || f.lookupsStarted() != 0 {
				t.Fatalf("the opt-out sends no request, got %d", f.releases.requests())
			}
		})
	}
	for _, value := range []string{"", "0", "false", " NO ", "off"} {
		t.Run("not on: "+value, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(false)
			f.terminals(true, true)
			f.setEnv("MODERNPATH_NO_UPDATE_CHECK", value)
			f.writeCache(ucLatest, f.time().Add(-time.Minute))
			if !strings.Contains(f.run("status").stderr, "is available") {
				t.Fatalf("MODERNPATH_NO_UPDATE_CHECK=%q leaves the check on: the notice must show", value)
			}
			f.run("prepare")
			if f.releases.requests() != 1 {
				t.Fatalf("MODERNPATH_NO_UPDATE_CHECK=%q leaves the check on: prepare must look up", value)
			}
		})
	}
	t.Run("with CI as well, the opt-out is the reason", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(false)
		f.setEnv("CI", "true")
		f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "1")
		report := ucReport(t, f.run("prepare", "--json").stdout)
		if release := ucRelease(report); release == nil || release["state"] != "off" || release["reason"] != "opt_out" {
			t.Fatalf("with CI and the opt-out, the reason is opt_out, got %v", report["cli_release"])
		}
	})
}
