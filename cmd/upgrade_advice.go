package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/envflag"
)

var releaseVersionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+$`)
var releaseLookupURL = "https://api.github.com/repos/ModernPath/cli/releases/latest"

// How long a release lookup may take: 1 second for --version and for a
// background lookup, and 3 seconds for prepare's lookup and for the
// background lookup a command waits for after a cut-off (decisions 23 and
// 24).
const (
	releaseLookupLimit     = time.Second
	releaseLookupWaitLimit = 3 * time.Second
)

// releaseLookupClient's timeout is only an outer bound: each lookup sets its
// own limit through its context.
var releaseLookupClient = &http.Client{Timeout: releaseLookupWaitLimit}

// The update check's surroundings, replaceable so that tests never reach
// GitHub, a developer's own cache folder or the CI setting of the shell.
var (
	// updateCheckClock is the time the update check compares its records with.
	updateCheckClock = time.Now
	// updateCheckEnv reads CI and MODERNPATH_NO_UPDATE_CHECK.
	updateCheckEnv = os.Getenv
	// userCacheFolder is the user's cache folder; the update check keeps its
	// files in a folder named after the CLI under it.
	userCacheFolder = os.UserCacheDir
	// runningBinaryPath is the resolved path of the running binary, which
	// tells the upgrade advice how the CLI was installed.
	runningBinaryPath = runningExecutable
	// resolvedHomeFolder and resolvedLocalAppData resolve the folders the
	// install script and the PowerShell installer use, the way
	// runningExecutable resolves the binary.
	resolvedHomeFolder   = func() (string, error) { return resolvedFolder(os.UserHomeDir()) }
	resolvedLocalAppData = func() (string, error) { return resolvedFolder(os.Getenv("LOCALAPPDATA"), nil) }
	// backgroundLookupStarted is a test hook. run() calls it with a channel
	// that closes when the background lookup it started has finished.
	backgroundLookupStarted func(done <-chan struct{})
	// backgroundLookupWaitStarted is a test hook, nil outside tests. run()
	// calls it when the end-of-run wait for a background lookup after a
	// cut-off starts, so a test can let its fake release server answer only
	// during the wait.
	backgroundLookupWaitStarted func()
)

// resolvedFolder resolves the symbolic links in dir, as runningExecutable
// does for the binary.
func resolvedFolder(dir string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("folder is not set")
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved, nil
	}
	return dir, nil
}

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

// latestPublishedRelease is a public, bounded read of the latest CLI release
// on GitHub: one GET with no credential, given up when ctx ends, which sets
// the caller's limit, and after 3 seconds at most. It returns the tag without
// its v. Every failure is an error: no connection, a timeout, a status other
// than 200 (403 and 429 included), malformed JSON or a tag that is neither
// X.Y.Z nor vX.Y.Z. A lookup cancelled through ctx returns an error that
// wraps context.Canceled.
func latestPublishedRelease(ctx context.Context) (string, error) {
	return lookupLatestRelease(ctx, releaseLookupClient, releaseLookupURL)
}

// lookupLatestRelease is latestPublishedRelease with client against url. The
// background lookup takes both when it starts and calls this, so a lookup
// still running never reads a package variable.
func lookupLatestRelease(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release lookup answered %d", resp.StatusCode)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("release lookup: %w", err)
	}
	if !releasedBuild(body.TagName) {
		return "", fmt.Errorf("release lookup returned the tag %q", body.TagName)
	}
	return strings.TrimPrefix(body.TagName, "v"), nil
}

// The release states and reasons that prepare, --version and the update
// notice share.
const (
	releaseNewer      = "newer"
	releaseCurrent    = "current"
	releaseLocalBuild = "local_build"
	releaseUnknown    = "unknown"
	releaseOff        = "off"

	reasonLookupFailed = "lookup_failed"
	reasonCI           = "ci"
	reasonOptOut       = "opt_out"
)

// releaseStatus is the release status of the running CLI: a state, the latest
// version and its check time when they are known, and a reason.
type releaseStatus struct {
	State     string    // newer, current, local_build, unknown or off
	Latest    string    // the latest CLI release, in the states newer and current
	CheckedAt time.Time // the time of the successful lookup the state comes from
	Reason    string    // lookup_failed, ci or opt_out
}

// updateCheckOff names the setting that turns the update check off, or "".
// The opt-out comes first when both are on: it is the setting the person chose.
func updateCheckOff() string {
	if envflag.On(updateCheckEnv("MODERNPATH_NO_UPDATE_CHECK")) {
		return reasonOptOut
	}
	if envflag.On(updateCheckEnv("CI")) {
		return reasonCI
	}
	return ""
}

// checkRelease is the update check prepare and --version make: unless the
// build is local or the check is off, one lookup on every run, within limit,
// whose success is written to the release cache. After a failed lookup a
// fresh cache answers only a newer CLI release; it never answers up to date.
func checkRelease(ctx context.Context, version string, limit time.Duration) releaseStatus {
	if !releasedBuild(version) {
		return releaseStatus{State: releaseLocalBuild}
	}
	if reason := updateCheckOff(); reason != "" {
		return releaseStatus{State: releaseOff, Reason: reason}
	}
	folder, haveFolder := releaseFolder()
	lookup, cancel := context.WithTimeout(ctx, limit)
	tag, err := latestPublishedRelease(lookup)
	cancel()
	now := updateCheckClock()
	if err == nil {
		if haveFolder {
			writeReleaseCache(folder, tag, now)
		}
		return releaseStatusOf(version, tag, now)
	}
	if haveFolder {
		if cached := cachedReleaseStatus(folder, version, now); cached.State == releaseNewer {
			cached.Reason = reasonLookupFailed
			return cached
		}
	}
	return releaseStatus{State: releaseUnknown, Reason: reasonLookupFailed}
}

func releaseStatusOf(version, latest string, checkedAt time.Time) releaseStatus {
	state := releaseCurrent
	if newerRelease(version, latest) {
		state = releaseNewer
	}
	return releaseStatus{State: state, Latest: latest, CheckedAt: checkedAt}
}

// cachedReleaseStatus is what the release cache says while it is fresh, and
// unknown once it has expired. A fresh cache whose tag is older than the
// running release reads as current, so it stays fresh for its 4 hours.
func cachedReleaseStatus(folder, version string, now time.Time) releaseStatus {
	tag, fetchedAt, fresh := readReleaseCache(folder, now)
	if !fresh {
		return releaseStatus{State: releaseUnknown}
	}
	return releaseStatusOf(version, tag, fetchedAt)
}

// The update check's files, kept in the CLI's folder under the user's cache
// folder and never in a repository.
const (
	releaseCacheName   = "release-latest.json"
	releaseNoticeName  = "release-notice.json"
	releaseAttemptName = "release-attempt.json"

	releaseCacheLifetime = 4 * time.Hour
	releaseNoticeEvery   = 24 * time.Hour
	releaseLookupBackOff = time.Hour
)

var cachedTagPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// releaseFolder is the CLI's folder under the user's cache folder. Without a
// user cache folder there is no cache and no notice record.
func releaseFolder() (string, bool) {
	base, err := userCacheFolder()
	if err != nil || base == "" {
		return "", false
	}
	return filepath.Join(base, "modernpath"), true
}

type releaseCacheFile struct {
	Tag       string `json:"tag"`
	FetchedAt string `json:"fetched_at"`
}

type releaseNoticeFile struct {
	Tag     string `json:"tag"`
	ShownAt string `json:"shown_at"`
}

// releaseAttemptFile records the last failed background lookup, which holds
// the next one back an hour, and the last one cut off after its request went
// out, which makes the next command wait for its own (design 5a).
type releaseAttemptFile struct {
	FailedAt string `json:"failed_at,omitempty"`
	CutOffAt string `json:"cut_off_at,omitempty"`
}

// readReleaseCache returns the cached tag and its fetch time while the cache
// is fresh: fetched at most 4 hours ago and not in the future, with an X.Y.Z
// tag. A missing or unreadable file is expired.
func readReleaseCache(folder string, now time.Time) (string, time.Time, bool) {
	var file releaseCacheFile
	if !readReleaseFile(folder, releaseCacheName, &file) || !cachedTagPattern.MatchString(file.Tag) {
		return "", time.Time{}, false
	}
	fetchedAt, err := time.Parse(time.RFC3339, file.FetchedAt)
	if err != nil || fetchedAt.After(now) || now.Sub(fetchedAt) > releaseCacheLifetime {
		return "", time.Time{}, false
	}
	return file.Tag, fetchedAt, true
}

func writeReleaseCache(folder, tag string, fetchedAt time.Time) bool {
	return writeReleaseFile(folder, releaseCacheName, releaseCacheFile{Tag: tag, FetchedAt: fetchedAt.UTC().Format(time.RFC3339)})
}

// releaseNoticeDue says whether the update notice for tag may show: it hasn't
// shown for that release in the last 24 hours. A record dated in the future
// holds nothing back.
func releaseNoticeDue(folder, tag string, now time.Time) bool {
	var file releaseNoticeFile
	if !readReleaseFile(folder, releaseNoticeName, &file) || file.Tag != tag {
		return true
	}
	shownAt, err := time.Parse(time.RFC3339, file.ShownAt)
	if err != nil || shownAt.After(now) {
		return true
	}
	return now.Sub(shownAt) >= releaseNoticeEvery
}

// releaseLookupBackingOff says whether a background lookup failed less than an
// hour ago. A record dated in the future, or a cut-off alone, holds nothing
// back.
func releaseLookupBackingOff(folder string, now time.Time) bool {
	var file releaseAttemptFile
	if !readReleaseFile(folder, releaseAttemptName, &file) {
		return false
	}
	failedAt, err := time.Parse(time.RFC3339, file.FailedAt)
	if err != nil || failedAt.After(now) {
		return false
	}
	return now.Sub(failedAt) < releaseLookupBackOff
}

// releaseLookupCutOff says whether the next background lookup is to be waited
// for: release-attempt.json has a cut_off_at that is set, isn't dated in the
// future and is later than the cache's last successful lookup.
func releaseLookupCutOff(folder string, now time.Time) bool {
	var file releaseAttemptFile
	if !readReleaseFile(folder, releaseAttemptName, &file) || file.CutOffAt == "" {
		return false
	}
	cutOffAt, err := time.Parse(time.RFC3339, file.CutOffAt)
	if err != nil || cutOffAt.After(now) {
		return false
	}
	return cutOffAt.After(releaseCacheFetchedAt(folder, now))
}

// releaseCacheFetchedAt is the cache's fetched_at at any age, which the
// freshness check doesn't give for an expired cache. A missing or unreadable
// file, a tag that isn't X.Y.Z, and a fetched_at that can't be read or is in
// the future give the zero time, which is older than any cut-off.
func releaseCacheFetchedAt(folder string, now time.Time) time.Time {
	var file releaseCacheFile
	if !readReleaseFile(folder, releaseCacheName, &file) || !cachedTagPattern.MatchString(file.Tag) {
		return time.Time{}
	}
	fetchedAt, err := time.Parse(time.RFC3339, file.FetchedAt)
	if err != nil || fetchedAt.After(now) {
		return time.Time{}
	}
	return fetchedAt
}

// recordReleaseFailure records a failed background lookup as failed_at alone,
// which ends a cut-off and holds the next background lookup back an hour.
func recordReleaseFailure(folder string, at time.Time) {
	writeReleaseFile(folder, releaseAttemptName, releaseAttemptFile{FailedAt: at.UTC().Format(time.RFC3339)})
}

// recordReleaseCutOff records a background lookup cancelled after its request
// went out, and keeps a failed_at that is already there.
func recordReleaseCutOff(folder string, at time.Time) {
	var file releaseAttemptFile
	if !readReleaseFile(folder, releaseAttemptName, &file) {
		file = releaseAttemptFile{}
	}
	file.CutOffAt = at.UTC().Format(time.RFC3339)
	writeReleaseFile(folder, releaseAttemptName, file)
}

func readReleaseFile(folder, name string, value any) bool {
	raw, err := os.ReadFile(filepath.Join(folder, name))
	return err == nil && json.Unmarshal(raw, value) == nil
}

// writeReleaseFile writes name in folder through a temporary file of its own
// and a rename, so an interrupted or concurrent write never leaves a partial
// file in place. It reports whether the file was written; a failure is
// otherwise silent.
func writeReleaseFile(folder, name string, value any) bool {
	blob, err := json.Marshal(value)
	if err != nil || os.MkdirAll(folder, 0o755) != nil {
		return false
	}
	tmp, err := os.CreateTemp(folder, "."+name+".*")
	if err != nil {
		return false
	}
	_, writeErr := tmp.Write(append(blob, '\n'))
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil || os.Rename(tmp.Name(), filepath.Join(folder, name)) != nil {
		_ = os.Remove(tmp.Name())
		return false
	}
	return true
}

// releaseFolderWritable says whether the CLI can write its files in folder. A
// background lookup that could keep neither its result nor its failure record
// doesn't start.
func releaseFolderWritable(folder string) bool {
	if os.MkdirAll(folder, 0o755) != nil {
		return false
	}
	probe, err := os.CreateTemp(folder, ".write-check-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name) == nil
}

// The upgrade commands for the installs the CLI recognizes.
const (
	homebrewUpgrade      = "brew update && brew upgrade modernpath"
	scoopUpgrade         = "scoop update modernpath"
	installScriptUpgrade = "curl -fsSL https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.sh | bash"
	installerUpgrade     = "irm https://raw.githubusercontent.com/ModernPath/cli/main/scripts/install.ps1 | iex"
)

// installedUpgradeCommand is the one upgrade command for an install the CLI
// recognizes from the resolved path of its own binary: Homebrew's Cellar,
// Scoop's apps folder, and the default folders of the install script and the
// PowerShell installer. The rules take goos as an argument and turn
// backslashes into slashes themselves, so every platform's rules can be
// tested anywhere; on Windows they ignore letter case.
func installedUpgradeCommand(goos string) (string, bool) {
	binary, err := runningBinaryPath()
	if err != nil || binary == "" {
		return "", false
	}
	foldCase := func(p string) string {
		if goos == "windows" {
			return strings.ToLower(p)
		}
		return p
	}
	bin := foldCase(slashPath(binary))
	switch {
	case strings.Contains(bin, foldCase("/Cellar/modernpath/")):
		return homebrewUpgrade, true
	case strings.Contains(strings.ToLower(bin), "/scoop/apps/modernpath/"):
		return scoopUpgrade, true
	}
	// The install folders are resolved like the binary, so a folder that is a
	// symbolic link, such as ~/.local/bin pointing to ~/bin, still matches
	// (Epic 422, criterion f71d4f5c).
	if home, err := resolvedHomeFolder(); err == nil && home != "" {
		if dir, _ := resolvedFolder(filepath.Join(home, ".local", "bin"), nil); bin == foldCase(path.Join(slashPath(dir), "modernpath")) {
			return installScriptUpgrade, true
		}
	}
	if local, err := resolvedLocalAppData(); err == nil && local != "" {
		if dir, _ := resolvedFolder(filepath.Join(local, "Programs", "modernpath"), nil); bin == foldCase(path.Join(slashPath(dir), "modernpath.exe")) {
			return installerUpgrade, true
		}
	}
	return "", false
}

func slashPath(p string) string { return path.Clean(strings.ReplaceAll(p, `\`, "/")) }

// releaseNoticeLine is the update notice, without the ⚠ that printWarning
// adds. When the CLI can't tell how it was installed, the line points to
// prepare, which lists the commands, instead of guessing one.
func releaseNoticeLine(latest, version, goos string) string {
	line := fmt.Sprintf("modernpath %s is available (you have %s).", latest, version)
	if command, ok := installedUpgradeCommand(goos); ok {
		return line + " Upgrade: " + command
	}
	return line + " Upgrade it the way you installed it; modernpath prepare lists the commands."
}

// versionStatusLine is the line --version prints on stderr after its first
// line; warn marks the notice line, which keeps printWarning's ⚠.
func versionStatusLine(status releaseStatus, version, goos string) (line string, warn bool) {
	switch status.State {
	case releaseNewer:
		return releaseNoticeLine(status.Latest, version, goos), true
	case releaseCurrent:
		if newerRelease(status.Latest, version) {
			return "No newer release is available.", false
		}
		return "This is the latest release.", false
	case releaseUnknown:
		return "Couldn't check for a newer release.", false
	}
	return "", false
}

// upgradeAdvice lists the upgrade for the running build. A released build
// that the CLI recognizes from its install path gets that one command; any
// other released build gets the two lines for its operating system.
func upgradeAdvice(version, goos, root string) []string {
	if releasedBuild(version) {
		if command, ok := installedUpgradeCommand(goos); ok {
			return []string{command}
		}
		if goos == "windows" {
			return []string{
				scoopUpgrade,
				"Alternative to Scoop: re-run the official PowerShell installer (" + installerUpgrade + ")",
			}
		}
		return []string{
			homebrewUpgrade,
			"Alternative to Homebrew: re-run the official install script (" + installScriptUpgrade + ")",
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
