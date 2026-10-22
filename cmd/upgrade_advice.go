package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var releaseVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
var releaseLookupURL = "https://api.github.com/repos/ModernPath/cli/releases/latest"
var releaseLookupClient = &http.Client{Timeout: time.Second}

// A build stamp with -dev or +commit is source-built. A released binary
// should be upgraded through its platform's published distribution channel
// before it writes its embedded kit into a repository.
func releasedBuild(version string) bool { return releaseVersionPattern.MatchString(version) }

func newerRelease(current, latest string) bool {
	if !releasedBuild(current) || !releasedBuild(latest) {
		return false
	}
	left := strings.Split(strings.TrimPrefix(current, "v"), ".")
	right := strings.Split(strings.TrimPrefix(latest, "v"), ".")
	for i := range left {
		a, _ := strconv.Atoi(left[i])
		b, _ := strconv.Atoi(right[i])
		if a != b {
			return a < b
		}
	}
	return false
}

// A public, bounded read. An outage, rate limit or malformed tag leaves the
// latest version unknown; no credential or cache file is used.
func latestPublishedRelease() string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseLookupURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := releaseLookupClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || !releasedBuild(body.TagName) {
		return ""
	}
	return strings.TrimPrefix(body.TagName, "v")
}

func upgradeAdvice(version, goos, root string) []string {
	if releasedBuild(version) {
		if goos == "windows" {
			return []string{
				"scoop update modernpath",
				"Alternative to Scoop: re-run the official PowerShell installer (irm https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.ps1 | iex)",
			}
		}
		return []string{
			"brew update && brew upgrade modernpath",
			"Alternative to Homebrew: re-run the official install script (curl -fsSL https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.sh | bash)",
		}
	}
	if _, err := os.Stat(filepath.Join(root, "modernpath-core", "tools", "modernpath", "go.mod")); err == nil {
		return []string{"rebuild the local CLI with modernpath-core/tools/modernpath/scripts/install-local.sh"}
	}
	return []string{fmt.Sprintf("rebuild the local CLI (%s) from its source checkout before running modernpath install", version)}
}

func upgradeAdviceForCurrentBuild(root string) []string {
	return upgradeAdvice(Version, runtime.GOOS, root)
}

func installAdvice(version, goos string) string {
	if !releasedBuild(version) {
		return "scripts/install-local.sh"
	}
	if goos == "windows" {
		return "scoop bucket add modernpath " + "https://github.com/ModernPath/scoop-bucket; scoop install modernpath (or re-run scripts/install.ps1 from github.com/ModernPath/cli)"
	}
	return "brew install modernpath/tap/modernpath (or re-run scripts/install.sh from github.com/ModernPath/cli)"
}
