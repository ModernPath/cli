package cmd

// REQ-CROSS-416 (EPIC-CLI-021) — RED first. The SessionStart brief ends with at
// most one freshness line — contract lag, kit drift, source lag, release lag,
// most blocking first — each naming its remedy, and carries none when
// everything is current. Never blocks or slows the hook beyond its deadline.
// BACKLOG-TOOL-71: today nothing at session start flags a stale binary or kit.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain pins the freshness seams for the whole package: no brief test runs
// real git against the developer's checkout, walks the real kit, or calls
// GitHub (F-CLI021-PR-03). A freshness test that needs a signal overrides a
// seam and restores it. Version stays "dev" — an unstamped build — so release
// lag is unreachable unless a test stamps one through pinFreshnessFresh.
func TestMain(m *testing.M) {
	freshnessGit = func(args ...string) (string, error) { return "", errors.New("no git in tests") }
	freshnessKitCheck = func(root string) ([]string, error) { return nil, nil }
	releaseLookupURL = "http://127.0.0.1:1/releases/latest"
	releaseHTTPClient = &http.Client{Timeout: 200 * time.Millisecond}
	os.Exit(m.Run())
}

// pinFreshnessFresh points every freshness seam at "nothing to report": no git
// (not applicable), no kit drift, a release lookup that answers this build's
// own version, and a fixed clock. Every brief test starts from here, so the
// existing exact-{} assertions keep holding; a test injects one signal at a
// time on top.
func pinFreshnessFresh(t *testing.T) {
	t.Helper()
	savedGit, savedKit, savedURL, savedClient, savedNow, savedVersion :=
		freshnessGit, freshnessKitCheck, releaseLookupURL, releaseHTTPClient, freshnessNow, Version
	t.Cleanup(func() {
		freshnessGit, freshnessKitCheck, releaseLookupURL, releaseHTTPClient, freshnessNow, Version =
			savedGit, savedKit, savedURL, savedClient, savedNow, savedVersion
	})
	freshnessGit = func(args ...string) (string, error) { return "", errors.New("no git") }
	freshnessKitCheck = func(root string) ([]string, error) { return nil, nil }
	// The latest release is the one before this build's own: a -dev build of
	// the next release is current, not behind.
	releaseLookupURL = releaseServer(t, 200, "v0.6.9", 0)
	releaseHTTPClient = &http.Client{Timeout: time.Second}
	freshnessNow = func() time.Time { return time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC) }
	Version = "0.7.0-dev+abc1234 (2026-09-19T10:00Z)"
}

func releaseServer(t *testing.T, status int, tag string, delay time.Duration) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": tag})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func writeReleaseCache(t *testing.T, root, tag string, fetchedAt time.Time) {
	t.Helper()
	dir := filepath.Join(root, ".modernpath")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"tag": tag, "fetched_at": fetchedAt.UTC().Format(time.RFC3339)})
	if err := os.WriteFile(filepath.Join(dir, releaseCacheName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// The pure line: each signal alone, precedence between two, silence when fresh.
func TestFreshnessLineSignalsAndPrecedence(t *testing.T) {
	stale := freshnessResult{state: freshnessStale, detail: "this build is behind the checked-out CLI source"}
	current := freshnessResult{state: freshnessCurrent}
	cases := []struct {
		name       string
		in         freshnessInputs
		wantSignal string
		wantLine   []string
	}{
		{"fresh", freshnessInputs{version: "0.7.0-dev+abc (d)", servedContract: "1", source: current}, "", nil},
		{"contract lag", freshnessInputs{version: "0.7.0", servedContract: "2", source: current}, "contract",
			[]string{"sync contract 2", "speaks 1", "rebuild", "modernpath install"}},
		{"kit drift", freshnessInputs{version: "0.7.0", kitDrift: []string{".modernpath/rdd/PROCESS.md"}, source: current}, "kit",
			[]string{"kit", "drifted", "modernpath install"}},
		{"source lag", freshnessInputs{version: "0.7.0-dev+abc (d)", source: stale}, "source",
			[]string{"behind the checked-out CLI source", "install-local.sh"}},
		{"release lag", freshnessInputs{version: "0.6.0", latestRelease: "0.7.0", source: current}, "release",
			[]string{"behind the latest release", "0.7.0", "upgrade"}},
		{"contract before kit", freshnessInputs{version: "0.7.0", servedContract: "2", kitDrift: []string{"x"}, source: stale, latestRelease: "9.0.0"}, "contract", nil},
		{"kit before source", freshnessInputs{version: "0.7.0", kitDrift: []string{"x"}, source: stale, latestRelease: "9.0.0"}, "kit", nil},
		{"source before release", freshnessInputs{version: "0.7.0", source: stale, latestRelease: "9.0.0"}, "source", nil},
		{"unverifiable source is silent", freshnessInputs{version: "0.7.0-dev+abc.dirty (d)", source: freshnessResult{state: freshnessUnverifiable}}, "", nil},
		{"dev build is silent on release", freshnessInputs{version: "dev", latestRelease: "9.0.0", source: current}, "", nil},
		{"a -dev of the next release is not behind the last", freshnessInputs{version: "0.7.0-dev+abc (d)", latestRelease: "0.6.2", source: current}, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			signal, line := freshnessLine(c.in)
			if signal != c.wantSignal {
				t.Fatalf("signal %q, want %q (line %q)", signal, c.wantSignal, line)
			}
			if c.wantSignal == "" && line != "" {
				t.Fatalf("fresh must be silent, got %q", line)
			}
			for _, needle := range c.wantLine {
				if !strings.Contains(line, needle) {
					t.Errorf("line %q lacks %q", line, needle)
				}
			}
		})
	}
}

func TestBehindReleaseOrdersLocalBuildsBetweenReleases(t *testing.T) {
	cases := []struct {
		version, latest string
		want            bool
	}{
		{"0.6.0", "0.7.0", true},
		{"0.7.0", "0.7.0", false},
		{"0.7.0", "v0.7.0", false},
		{"0.7.1", "0.7.0", false},
		{"0.7.0-dev", "0.7.0", true},
		{"0.7.0-dev+abc1234 (2026-09-19T10:00Z)", "0.7.0", true},
		{"0.7.0-dev+abc1234.dirty (2026-09-19T10:00Z)", "0.6.9", false},
		{"0.7.0-dev", "0.6.9", false},
		{"dev", "9.9.9", false},
		{"0.7.0", "", false},
		{"0.7.0", "not-a-version", false},
	}
	for _, c := range cases {
		if got := behindRelease(c.version, c.latest); got != c.want {
			t.Errorf("behindRelease(%q, %q) = %v, want %v", c.version, c.latest, got, c.want)
		}
	}
}

// The release cache: written once with the fetch time inside the file, reused
// within 24 h, refreshed after; a failed refresh keeps the old tag; offline
// with no cache is silent.
func TestLatestReleaseCachesForADay(t *testing.T) {
	pinFreshnessFresh(t)
	root := t.TempDir()
	now := freshnessNow()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"tag_name": "v0.8.0"})
	}))
	t.Cleanup(srv.Close)
	releaseLookupURL = srv.URL

	if got := latestRelease(root, now); got != "0.8.0" {
		t.Fatalf("first lookup = %q", got)
	}
	if got := latestRelease(root, now.Add(23*time.Hour)); got != "0.8.0" || calls != 1 {
		t.Fatalf("within a day the cache answers: got %q after %d calls", got, calls)
	}
	raw, err := os.ReadFile(filepath.Join(root, ".modernpath", releaseCacheName))
	if err != nil || !strings.Contains(string(raw), "fetched_at") {
		t.Fatalf("the cache carries its fetch time inside the file: %s (%v)", raw, err)
	}
	if got := latestRelease(root, now.Add(25*time.Hour)); got != "0.8.0" || calls != 2 {
		t.Fatalf("after a day the lookup runs again: got %q after %d calls", got, calls)
	}

	releaseLookupURL = releaseServer(t, 403, "", 0)
	if got := latestRelease(root, now.Add(50*time.Hour)); got != "0.8.0" {
		t.Fatalf("a refused refresh keeps the old tag, got %q", got)
	}

	releaseLookupURL = "http://127.0.0.1:1/"
	if got := latestRelease(t.TempDir(), now); got != "" {
		t.Fatalf("offline with no cache is silent, got %q", got)
	}
}

// Brief mode differs from the doctor: dirt under the CLI directory, a .dirty
// stamp, a missing stamp or a commit the clone lacks are unverifiable — a
// session start never accuses the developer's own uncommitted build. Doctor
// mode keeps stale-on-dirt.
func TestSourceFreshnessBriefModeReadsDirtAsUnverifiable(t *testing.T) {
	head := strings.Repeat("a", 40)
	other := strings.Repeat("b", 40)
	stamp := "0.7.0-dev+" + head[:9] + " (2026-09-19T10:00Z)"
	dirty := " M modernpath-core/tools/modernpath/cmd/x.go\n"
	clean := ""

	doctor := sourceFreshness("*internal/rdd/ops.go", stamp, stubGit(t, "modernpath-core/tools/modernpath/internal/rdd/ops.go", other, dirty, nil), freshnessModeDoctor)
	if doctor.state != freshnessStale {
		t.Fatalf("doctor mode: dirt is stale, got %v", doctor.state)
	}
	brief := sourceFreshness("*tools/modernpath/go.mod", stamp, stubGit(t, "modernpath-core/tools/modernpath/go.mod", other, dirty, nil), freshnessModeBrief)
	if brief.state != freshnessUnverifiable {
		t.Fatalf("brief mode: dirt is unverifiable, got %v", brief.state)
	}
	if got := sourceFreshness("*tools/modernpath/go.mod", "0.7.0-dev+abc.dirty (d)", stubGit(t, "modernpath-core/tools/modernpath/go.mod", other, clean, nil), freshnessModeBrief); got.state != freshnessUnverifiable {
		t.Fatalf("brief mode: a .dirty stamp is unverifiable, got %v", got.state)
	}
	if got := sourceFreshness("*tools/modernpath/go.mod", "dev", stubGit(t, "modernpath-core/tools/modernpath/go.mod", other, clean, nil), freshnessModeBrief); got.state != freshnessUnverifiable {
		t.Fatalf("brief mode: an unstamped build is unverifiable, got %v", got.state)
	}
	behind := sourceFreshness("*tools/modernpath/go.mod", stamp, stubGit(t, "modernpath-core/tools/modernpath/go.mod", other, clean, errors.New("exit 1")), freshnessModeBrief)
	if behind.state != freshnessStale {
		t.Fatalf("brief mode: clean tree, last change not in the build = stale, got %v", behind.state)
	}
	current := sourceFreshness("*tools/modernpath/go.mod", stamp, stubGit(t, "modernpath-core/tools/modernpath/go.mod", head, clean, nil), freshnessModeBrief)
	if current.state != freshnessCurrent {
		t.Fatalf("brief mode: last change in the build = current, got %v", current.state)
	}
	// The doctor's own check is unchanged in shape.
	if got := extractorFreshness(stamp, stubGit(t, "x/internal/rdd/ops.go", head, clean, nil)); got.state != freshnessCurrent {
		t.Fatalf("extractorFreshness still answers current, got %v", got.state)
	}
}

// The line rides the failure envelopes too: a failed feed must not drop it.
func TestFreshnessLineSurvivesAFailedFeed(t *testing.T) {
	pinFreshnessFresh(t)
	env := wsEnv(t, wsServe(t, &wsFixture{failFeed: true, contractVersion: 2}))
	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"ff","source":"startup"}`), &out, 2*time.Second, hookLoader(env))
	want(t, out.String(), "sync contract 2")
	want(t, readBriefLog(t, env.Root), "freshness:contract")
}

// A kit that was never installed is not drift.
func TestKitDriftIsNotApplicableWithoutAnInstalledKit(t *testing.T) {
	pinFreshnessFresh(t)
	freshnessKitCheck = func(root string) ([]string, error) { return []string{"everything"}, nil }
	root := t.TempDir()
	if got := kitDrift(root); got != nil {
		t.Fatalf("no .modernpath/rdd means not applicable, got %v", got)
	}
	if err := os.MkdirAll(filepath.Join(root, ".modernpath", "rdd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := kitDrift(root); len(got) != 1 {
		t.Fatalf("an installed kit is checked, got %v", got)
	}
}
