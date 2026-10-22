package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleasedBuildUpgradeAdviceUsesItsPlatform(t *testing.T) {
	for _, tc := range []struct{ goos, primary, alternative string }{
		{"darwin", "brew update && brew upgrade modernpath", "scripts/install.sh"},
		{"linux", "brew update && brew upgrade modernpath", "scripts/install.sh"},
		{"windows", "scoop update modernpath", "scripts/install.ps1"},
	} {
		t.Run(tc.goos, func(t *testing.T) {
			steps := upgradeAdvice("0.20.0", tc.goos, t.TempDir())
			if len(steps) != 2 || steps[0] != tc.primary || !strings.Contains(steps[1], tc.alternative) {
				t.Fatalf("upgrade advice = %v", steps)
			}
			if strings.Contains(strings.Join(steps, " "), "scripts/install-local.sh") {
				t.Fatalf("released advice names developer installer: %v", steps)
			}
		})
	}
	if got := installAdvice("0.20.0", "windows"); strings.Contains(got, "&&") || !strings.Contains(got, "scoop install modernpath") {
		t.Fatalf("Windows install advice is not suitable for PowerShell: %q", got)
	}
}

func TestPrepareHumanReportShowsObservedLatestRelease(t *testing.T) {
	var out bytes.Buffer
	prepareCmd.SetOut(&out)
	defer prepareCmd.SetOut(nil)
	if err := prepareCmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}
	if err := writePrepare(prepareCmd, prepareReport{CLIVersion: "0.20.0", LatestCLI: "0.21.0"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "latest release: 0.21.0") {
		t.Fatalf("human report hides the live release: %s", out.String())
	}
}

func TestBuildStampDeterminesDeveloperAdvice(t *testing.T) {
	for _, version := range []string{"dev", "0.21.0-dev+abc", "0.21.0+abc", "0.21.0-rc.1"} {
		if releasedBuild(version) {
			t.Fatalf("%q is not a released build", version)
		}
		if steps := upgradeAdvice(version, "windows", t.TempDir()); strings.Contains(strings.Join(steps, " "), "scoop") {
			t.Fatalf("developer build got customer channel: %v", steps)
		}
	}
	if !releasedBuild("v0.20.0") {
		t.Fatal("tagged release was not recognized")
	}
}

func TestLatestReleaseReadIsBoundedAndTruthful(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, want string
	}{
		{"newer", 200, `{"tag_name":"v0.21.0"}`, "0.21.0"},
		{"release with assets", 200, `{"tag_name":"v0.21.0","assets":"` + strings.Repeat("x", 8192) + `"}`, "0.21.0"},
		{"equal", 200, `{"tag_name":"v0.20.0"}`, "0.20.0"},
		{"rate limited", 403, `{}`, ""},
		{"malformed", 200, `{"tag_name":"latest"}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.Header.Get("Authorization") != "" {
					t.Errorf("lookup must be public GET")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			old := releaseLookupURL
			releaseLookupURL = srv.URL
			defer func() { releaseLookupURL = old }()
			if got := latestPublishedRelease(); got != tc.want {
				t.Fatalf("latest = %q, want %q", got, tc.want)
			}
		})
	}
	if !newerRelease("0.20.0", "0.21.0") || newerRelease("0.20.0", "0.20.0") || newerRelease("0.21.0-dev+abc", "0.22.0") {
		t.Fatal("version comparison asserted a false upgrade")
	}
}
