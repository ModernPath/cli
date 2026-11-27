package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Epic 422 unit tests: prepare's handler, upgradeAdvice and the notice line,
// called directly.

// ucLookupFails makes the release lookup fail quickly.
func ucLookupFails(f *ucFixture) { f.releases.set(ucStatus(500)) }

// Criterion 5607ba0b: each release state has its own text on prepare's CLI:
// line, also when no connected repository is found; a newer release taken from
// the cache after a failed lookup says that the lookup failed; none says a
// bare unknown; a newer release is the first Next: line and exits non-zero.
func TestUpdateCheckPrepareCLILine(t *testing.T) {
	foundAt := time.Date(2026, 11, 11, 14, 0, 0, 0, time.UTC)
	connected := []struct {
		name    string
		setup   func(f *ucFixture)
		line    string
		first   string // the first Next: line, when it matters
		channel string // the release-channel step the drifted kit gets, or ""
	}{
		{name: "newer", setup: func(f *ucFixture) { f.releases.set(ucTag("v0.27.1")) },
			line: "CLI: 0.26.0 (latest release: 0.27.1; upgrade available)", first: ucPlatformUpgrade(runtime.GOOS)},
		{name: "newer from the cache after a failed lookup", setup: func(f *ucFixture) {
			ucLookupFails(f)
			f.setTime(foundAt.Add(2 * time.Hour))
			f.writeCache(ucLatest, foundAt)
		}, line: "CLI: 0.26.0 (latest release: 0.27.1, found 2026-11-11T14:00Z; upgrade available; this lookup failed)", first: ucPlatformUpgrade(runtime.GOOS)},
		{name: "current", setup: func(f *ucFixture) { f.releases.set(ucTag("v0.26.0")) },
			line: "CLI: 0.26.0 (latest release: 0.26.0; up to date)", first: "modernpath install"},
		{name: "current above the latest release", setup: func(f *ucFixture) { f.releases.set(ucTag("0.25.0")) },
			line: "CLI: 0.26.0 (latest release: 0.25.0; up to date)", first: "modernpath install"},
		{name: "local build", setup: func(f *ucFixture) { ucSetVersion(f.t, "0.27.0-dev+abc1234 (2026-11-10T15:09Z)") },
			line: "CLI: 0.27.0-dev+abc1234 (2026-11-10T15:09Z) (local build; update check not used)"},
		{name: "unknown", setup: ucLookupFails,
			line: "CLI: 0.26.0 (latest release unknown: the lookup failed)", channel: "Check your CLI release channel for a newer version before modernpath install (latest release could not be verified)"},
		{name: "unknown with a fresh cache of the same release", setup: func(f *ucFixture) {
			ucLookupFails(f)
			f.writeCache(ucRunning, f.time().Add(-time.Hour))
		}, line: "CLI: 0.26.0 (latest release unknown: the lookup failed)", channel: "Check your CLI release channel for a newer version before modernpath install (latest release could not be verified)"},
		{name: "off in CI", setup: func(f *ucFixture) { f.setEnv("CI", "true") },
			line: "CLI: 0.26.0 (update check off: CI is set)", channel: "Check your CLI release channel for a newer version before modernpath install (the update check is off)"},
		{name: "off with the opt-out", setup: func(f *ucFixture) { f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "1") },
			line: "CLI: 0.26.0 (update check off: MODERNPATH_NO_UPDATE_CHECK is set)", channel: "Check your CLI release channel for a newer version before modernpath install (the update check is off)"},
		{name: "off with CI and the opt-out", setup: func(f *ucFixture) {
			f.setEnv("CI", "1")
			f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "true")
		}, line: "CLI: 0.26.0 (update check off: MODERNPATH_NO_UPDATE_CHECK is set)", channel: "Check your CLI release channel for a newer version before modernpath install (the update check is off)"},
	}
	// Without an installed kit, so the kit has drifted: these cases check the
	// CLI: line and the Next: lines. The kit drift alone makes prepare exit
	// non-zero, so the exit code is checked below on an installed kit.
	for _, tc := range connected {
		t.Run(tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(false)
			tc.setup(f)
			out, _ := f.prepare(false)
			if got := ucCLILine(t, out); got != tc.line {
				t.Fatalf("CLI line = %q, want %q", got, tc.line)
			}
			if strings.Contains(out, "latest release: unknown") {
				t.Fatalf("prepare said a bare unknown:\n%s", out)
			}
			steps := ucNextSteps(out)
			if tc.first != "" && (len(steps) == 0 || steps[0] != tc.first) {
				t.Fatalf("first Next: = %q, want %q", steps, tc.first)
			}
			if tc.channel != "" && ucIndex(steps, tc.channel) < 0 {
				t.Fatalf("the drifted kit must get %q, got %q", tc.channel, steps)
			}
			if tc.channel == "" && strings.Contains(out, "Check your CLI release channel") {
				t.Fatalf("no release-channel step expected:\n%s", out)
			}
		})
	}

	// The exit code, from a workspace that would otherwise exit 0: an
	// installed kit, a reachable session and checked documents. A newer CLI
	// release makes prepare exit non-zero, also when it comes from the cache
	// after a failed lookup; a current release or a failed lookup alone
	// doesn't.
	for _, tc := range []struct {
		name  string
		setup func(f *ucFixture)
		line  string
		fails bool
	}{
		{"newer", func(f *ucFixture) { f.releases.set(ucTag("v0.27.1")) }, "CLI: 0.26.0 (latest release: 0.27.1; upgrade available)", true},
		{"newer from the cache after a failed lookup", func(f *ucFixture) {
			ucLookupFails(f)
			f.setTime(foundAt.Add(2 * time.Hour))
			f.writeCache(ucLatest, foundAt)
		}, "CLI: 0.26.0 (latest release: 0.27.1, found 2026-11-11T14:00Z; upgrade available; this lookup failed)", true},
		{"current", func(f *ucFixture) { f.releases.set(ucTag("v0.26.0")) }, "CLI: 0.26.0 (latest release: 0.26.0; up to date)", false},
		{"unknown", ucLookupFails, "CLI: 0.26.0 (latest release unknown: the lookup failed)", false},
	} {
		t.Run("exit code with an installed kit: "+tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			f.connectedRepo(true)
			tc.setup(f)
			out, err := f.prepare(false)
			if got := ucCLILine(t, out); got != tc.line {
				t.Fatalf("CLI line = %q, want %q", got, tc.line)
			}
			if (err != nil) != tc.fails {
				t.Fatalf("prepare returned %v; want a non-zero exit: %t\n%s", err, tc.fails, out)
			}
			if steps := ucNextSteps(out); tc.fails && (len(steps) == 0 || steps[0] != ucPlatformUpgrade(runtime.GOOS)) {
				t.Fatalf("a newer CLI release must be the first Next: line, got %q", steps)
			}
		})
	}

	// No connected repository, and a configuration that can't be read: prepare
	// stops early, still names the release state and lists the upgrade first.
	early := []struct {
		name, config, last string
	}{
		{"no connected repository", "", "modernpath init"},
		{"unreadable configuration", "{", "modernpath init --help"},
	}
	for _, tc := range early {
		t.Run(tc.name, func(t *testing.T) {
			for _, state := range []struct {
				name  string
				setup func(f *ucFixture)
				line  string
			}{
				{"newer", func(f *ucFixture) { f.releases.set(ucTag("v0.27.1")) }, "CLI: 0.26.0 (latest release: 0.27.1; upgrade available)"},
				{"local build", func(f *ucFixture) { ucSetVersion(f.t, "0.27.0-dev+abc1234 (2026-11-10T15:09Z)") }, "CLI: 0.27.0-dev+abc1234 (2026-11-10T15:09Z) (local build; update check not used)"},
				{"unknown", ucLookupFails, "CLI: 0.26.0 (latest release unknown: the lookup failed)"},
				{"off", func(f *ucFixture) { f.setEnv("CI", "yes") }, "CLI: 0.26.0 (update check off: CI is set)"},
			} {
				t.Run(state.name, func(t *testing.T) {
					f := newUCFixture(t)
					root := t.TempDir()
					t.Chdir(root)
					if tc.config != "" {
						if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(filepath.Join(root, ".modernpath", "config.json"), []byte(tc.config), 0o644); err != nil {
							t.Fatal(err)
						}
					}
					state.setup(f)
					out, err := f.prepare(false)
					if err == nil {
						t.Fatal("an early stop exits non-zero")
					}
					if got := ucCLILine(t, out); got != state.line {
						t.Fatalf("CLI line = %q, want %q", got, state.line)
					}
					steps := ucNextSteps(out)
					if len(steps) == 0 || steps[len(steps)-1] != tc.last {
						t.Fatalf("Next: lines = %q, want them to end with %q", steps, tc.last)
					}
					if state.name == "newer" && steps[0] != ucPlatformUpgrade(runtime.GOOS) {
						t.Fatalf("a newer CLI release must be the first Next: line, got %q", steps)
					}
					if state.name != "newer" && len(steps) != 1 {
						t.Fatalf("only %q is expected, got %q", tc.last, steps)
					}
				})
			}
		})
	}
}

// Criterion 82c37a8c: prepare --json keeps cli_version and latest_cli and adds
// cli_release with state, latest, checked_at and reason.
func TestUpdateCheckPrepareJSON(t *testing.T) {
	foundAt := time.Date(2026, 11, 11, 14, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		version string
		setup   func(f *ucFixture)
		latest  string
		release map[string]any
	}{
		{"newer", ucRunning, func(f *ucFixture) { f.releases.set(ucTag("v0.27.1")) }, "0.27.1",
			map[string]any{"state": "newer", "latest": "0.27.1", "checked_at": "2026-11-12T12:00:00Z"}},
		{"newer from the cache after a failed lookup", ucRunning, func(f *ucFixture) {
			ucLookupFails(f)
			f.setTime(foundAt.Add(time.Hour))
			f.writeCache(ucLatest, foundAt)
		}, "0.27.1", map[string]any{"state": "newer", "latest": "0.27.1", "checked_at": "2026-11-11T14:00:00Z", "reason": "lookup_failed"}},
		{"current", ucRunning, func(f *ucFixture) { f.releases.set(ucTag("v0.26.0")) }, "0.26.0",
			map[string]any{"state": "current", "latest": "0.26.0", "checked_at": "2026-11-12T12:00:00Z"}},
		{"local build", "0.27.0-dev+abc1234 (2026-11-10T15:09Z)", func(*ucFixture) {}, "unknown",
			map[string]any{"state": "local_build"}},
		{"unknown", ucRunning, ucLookupFails, "unknown",
			map[string]any{"state": "unknown", "reason": "lookup_failed"}},
		{"off in CI", ucRunning, func(f *ucFixture) { f.setEnv("CI", "true") }, "unknown",
			map[string]any{"state": "off", "reason": "ci"}},
		{"off with the opt-out", ucRunning, func(f *ucFixture) { f.setEnv("MODERNPATH_NO_UPDATE_CHECK", "1") }, "unknown",
			map[string]any{"state": "off", "reason": "opt_out"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			ucSetVersion(t, tc.version)
			f.connectedRepo(false)
			tc.setup(f)
			out, _ := f.prepare(true)
			report := ucReport(t, out)
			if report["cli_version"] != tc.version {
				t.Fatalf("cli_version = %v, want %q", report["cli_version"], tc.version)
			}
			if report["latest_cli"] != tc.latest {
				t.Fatalf("latest_cli = %v, want %q", report["latest_cli"], tc.latest)
			}
			if got := ucRelease(report); !reflect.DeepEqual(got, tc.release) {
				t.Fatalf("cli_release = %v, want %v", report["cli_release"], tc.release)
			}
		})
	}
}

// Criterion f71d4f5c: the upgrade command comes from the resolved path of the
// running binary; other paths get the platform advice in prepare and the
// pointer to prepare in the notice.
func TestUpdateCheckUpgradeAdviceFromInstallPath(t *testing.T) {
	platform := map[string][]string{
		"darwin":  {ucBrew, "Alternative to Homebrew: re-run the official install script (" + ucInstallScript + ")"},
		"linux":   {ucBrew, "Alternative to Homebrew: re-run the official install script (" + ucInstallScript + ")"},
		"windows": {ucScoop, "Alternative to Scoop: re-run the official PowerShell installer (" + ucInstaller + ")"},
	}
	for _, tc := range []struct {
		name, goos, binary, home, localAppData string
		upgrade                                string // the matched command, or ucPointer
	}{
		{"Homebrew on macOS", "darwin", "/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath", "/Users/dev", "", ucBrew},
		{"Homebrew on Linux", "linux", "/home/linuxbrew/.linuxbrew/Cellar/modernpath/0.26.0/bin/modernpath", "/home/dev", "", ucBrew},
		{"Scoop", "windows", `C:\Users\dev\scoop\apps\modernpath\current\modernpath.exe`, `C:\Users\dev`, `C:\Users\dev\AppData\Local`, ucScoop},
		{"Scoop in another letter case with forward slashes", "windows", "C:/Users/dev/SCOOP/Apps/ModernPath/0.26.0/modernpath.exe", `C:\Users\dev`, "", ucScoop},
		{"install script on macOS", "darwin", "/Users/dev/.local/bin/modernpath", "/Users/dev", "", ucInstallScript},
		{"install script on Linux", "linux", "/home/dev/.local/bin/modernpath", "/home/dev", "", ucInstallScript},
		{"PowerShell installer", "windows", `C:\Users\dev\AppData\Local\Programs\modernpath\modernpath.exe`, `C:\Users\dev`, `C:\Users\dev\AppData\Local`, ucInstaller},
		{"PowerShell installer in another letter case", "windows", `c:\users\DEV\appdata\local\Programs\ModernPath\MODERNPATH.EXE`, `C:\Users\dev`, `C:\Users\dev\AppData\Local`, ucInstaller},
		{"another folder on macOS", "darwin", "/usr/local/bin/modernpath", "/Users/dev", "", ucPointer},
		{"another file in the install script's folder", "linux", "/home/dev/.local/bin/modernpath-0.26.0", "/home/dev", "", ucPointer},
		{"another folder on Windows", "windows", `D:\tools\modernpath.exe`, `C:\Users\dev`, `C:\Users\dev\AppData\Local`, ucPointer},
		{"a Cellar of another formula", "darwin", "/opt/homebrew/Cellar/modernpath-tools/1.0/bin/modernpath", "/Users/dev", "", ucPointer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newUCFixture(t)
			f.binary(tc.binary)
			ucSwap(t, &resolvedHomeFolder, func() (string, error) { return tc.home, nil })
			ucSwap(t, &resolvedLocalAppData, func() (string, error) {
				if tc.localAppData == "" {
					return "", fmt.Errorf("LOCALAPPDATA is not set")
				}
				return tc.localAppData, nil
			})
			advice := upgradeAdvice(ucRunning, tc.goos, t.TempDir())
			want := []string{tc.upgrade}
			if tc.upgrade == ucPointer {
				want = platform[tc.goos]
			}
			if !reflect.DeepEqual(advice, want) {
				t.Fatalf("upgradeAdvice = %q, want %q", advice, want)
			}
			if got, want := "⚠ "+releaseNoticeLine(ucLatest, ucRunning, tc.goos), ucNotice(ucLatest, ucRunning, tc.upgrade); got != want {
				t.Fatalf("notice line = %q, want %q", got, want)
			}
		})
	}

	// Mattias's review of #1221: when an install folder is a symbolic link, such
	// as ~/.local/bin pointing to ~/bin, the binary's resolved path is in the
	// link's target, so the install folder is resolved the same way before the
	// paths are compared.
	linkedInstall := func(t *testing.T, base, link, target, file string) string {
		t.Helper()
		link, target = filepath.Join(base, link), filepath.Join(base, target)
		for _, dir := range []string{target, filepath.Dir(link)} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symbolic links are not available: %v", err)
		}
		if err := os.WriteFile(filepath.Join(target, file), nil, 0o755); err != nil {
			t.Fatal(err)
		}
		binary, err := filepath.EvalSymlinks(filepath.Join(link, file)) // as runningExecutable resolves it
		if err != nil {
			t.Fatal(err)
		}
		return binary
	}
	noFolder := func() (string, error) { return "", fmt.Errorf("not set") }
	t.Run("the install script's folder is a symbolic link", func(t *testing.T) {
		f := newUCFixture(t)
		home := t.TempDir()
		f.binary(linkedInstall(t, home, filepath.Join(".local", "bin"), "bin", "modernpath"))
		ucSwap(t, &resolvedHomeFolder, func() (string, error) { return resolvedFolder(home, nil) })
		ucSwap(t, &resolvedLocalAppData, noFolder)
		if advice := upgradeAdvice(ucRunning, "linux", t.TempDir()); !reflect.DeepEqual(advice, []string{ucInstallScript}) {
			t.Fatalf("upgradeAdvice = %q, want %q", advice, ucInstallScript)
		}
	})
	t.Run("the PowerShell installer's folder is a symbolic link", func(t *testing.T) {
		f := newUCFixture(t)
		local := t.TempDir()
		f.binary(linkedInstall(t, local, filepath.Join("Programs", "modernpath"), filepath.Join("Apps", "modernpath"), "modernpath.exe"))
		ucSwap(t, &resolvedHomeFolder, noFolder)
		ucSwap(t, &resolvedLocalAppData, func() (string, error) { return resolvedFolder(local, nil) })
		if advice := upgradeAdvice(ucRunning, "windows", t.TempDir()); !reflect.DeepEqual(advice, []string{ucInstaller}) {
			t.Fatalf("upgradeAdvice = %q, want %q", advice, ucInstaller)
		}
	})

	t.Run("a path that can't be read", func(t *testing.T) {
		newUCFixture(t)
		ucSwap(t, &runningBinaryPath, func() (string, error) { return "", fmt.Errorf("no executable path") })
		if advice := upgradeAdvice(ucRunning, "darwin", t.TempDir()); !reflect.DeepEqual(advice, platform["darwin"]) {
			t.Fatalf("upgradeAdvice = %q, want the platform lines", advice)
		}
		if got, want := "⚠ "+releaseNoticeLine(ucLatest, ucRunning, "darwin"), ucNotice(ucLatest, ucRunning, ucPointer); got != want {
			t.Fatalf("notice line = %q, want %q", got, want)
		}
	})

	t.Run("a local build keeps the source build", func(t *testing.T) {
		f := newUCFixture(t)
		f.binary("/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath")
		advice := upgradeAdvice("0.27.0-dev+abc1234", "darwin", t.TempDir())
		if len(advice) != 1 || !strings.Contains(advice[0], "rebuild the local CLI") {
			t.Fatalf("a local build must rebuild from source, got %q", advice)
		}
	})

	t.Run("prepare lists a matched command alone", func(t *testing.T) {
		f := newUCFixture(t)
		f.connectedRepo(true) // installed kit: only the newer release can make prepare exit non-zero
		f.binary("/opt/homebrew/Cellar/modernpath/0.26.0/bin/modernpath")
		out, err := f.prepare(true)
		if err == nil {
			t.Fatal("a newer CLI release exits non-zero")
		}
		steps := ucSteps(ucReport(t, out))
		if len(steps) < 2 || steps[0] != ucBrew || steps[1] != "modernpath install" {
			t.Fatalf("the Homebrew command must stand alone before modernpath install, got %q", steps)
		}
	})
}
