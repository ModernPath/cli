package cmd

// REQ-CROSS-416 (EPIC-CLI-021) — the SessionStart brief's freshness line.
//
// One line at most, silent when everything is current, reporting the first
// of these that holds — most blocking first (D4) — with the command that
// fixes it: contract lag (the server speaks a newer sync contract: writes
// would be refused), kit drift (the installed process misleads the loop),
// source lag (the developer's own build is behind the checked-out CLI
// source), release lag (advisory). Every input is a local read except a
// release lookup on a cache miss, once a day, bounded at one second and
// silent on any failure; the only file written is the release cache under
// the ignored .modernpath/ directory (the reads-only carve-out,
// USER:2026-09-19). BACKLOG-TOOL-71: until now the first sign of any of
// these was a verb that misbehaved.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/contract"
	"github.com/modernpath/cli/internal/kit"
)

const (
	releaseCacheName = "release-latest.json"
	releaseCacheTTL  = 24 * time.Hour
	// cliSourcePattern locates the CLI module in the checked-out repository;
	// its directory is what "the CLI source" means for source lag.
	cliSourcePattern = "*tools/modernpath/go.mod"
)

// The seams (F-CLI021-R1-04): the brief tests pin them fresh, so a test
// workspace never runs real git, compares a real kit, or calls GitHub.
var (
	freshnessGit      gitRunner = execGit
	freshnessKitCheck           = func(root string) ([]string, error) {
		return kit.Check(root, kit.Generated{Target: cliReferenceTarget, Body: []byte(renderCLIReference(rootCmd))})
	}
	releaseLookupURL  = "https://api.github.com/repos/ModernPath/cli/releases/latest"
	releaseHTTPClient = &http.Client{Timeout: time.Second}
	freshnessNow      = time.Now
)

type freshnessInputs struct {
	version        string          // the running build's stamp
	servedContract string          // x-modernpath-contract from this run's responses; "" unknown
	kitDrift       []string        // nil = not applicable or unknown; empty = current
	source         freshnessResult // sourceFreshness in brief mode
	latestRelease  string          // the latest published release, "" unknown
}

// freshnessLine returns the signal that fired ("contract", "kit", "source",
// "release", or "" when nothing did) and the line with its remedy.
func freshnessLine(in freshnessInputs) (signal, line string) {
	if served, err := strconv.Atoi(strings.TrimSpace(in.servedContract)); err == nil && served > contract.Version {
		return "contract", fmt.Sprintf("the server speaks sync contract %d and this build (%s) speaks %d — writes will be refused: rebuild and `modernpath install` (modernpath-core/AGENTS.md §CLI release procedure)",
			served, in.version, contract.Version)
	}
	if n := len(in.kitDrift); n > 0 {
		return "kit", fmt.Sprintf("the installed kit has %d tool-owned file(s) drifted from this build (first: %s) — run `modernpath install`", n, in.kitDrift[0])
	}
	if in.source.state == freshnessStale {
		return "source", in.source.detail + " — rebuild and install with modernpath-core/tools/modernpath/scripts/install-local.sh"
	}
	if behindRelease(in.version, in.latestRelease) {
		return "release", fmt.Sprintf("modernpath %s is behind the latest release %s — upgrade (`brew upgrade modernpath`, or scripts/install.sh from ModernPath/cli)",
			in.version, strings.TrimPrefix(in.latestRelease, "v"))
	}
	return "", ""
}

// localFreshness is everything the line needs that does not come from the
// hook's own server reads: kit drift, source provenance and the latest
// release (cache or one bounded lookup). Gathered off the hook's main path
// so none of it — the kit walk, the git subprocesses, the GET — can extend
// the hook past its deadline (F-CLI021-PR-02).
type localFreshness struct {
	kitDrift []string
	source   freshnessResult
	latest   string
}

func gatherLocalFreshness(root string) localFreshness {
	return localFreshness{
		kitDrift: kitDrift(root),
		source:   sourceFreshness(cliSourcePattern, Version, freshnessGit, freshnessModeBrief),
		latest:   latestRelease(root, freshnessNow()),
	}
}

// briefFreshness composes the line from the local inputs and the contract
// version the hook's fetch carried back.
func briefFreshness(local localFreshness, servedContract string) (signal, line string) {
	return freshnessLine(freshnessInputs{
		version:        Version,
		servedContract: servedContract,
		kitDrift:       local.kitDrift,
		source:         local.source,
		latestRelease:  local.latest,
	})
}

// kitDrift is install --check as a fact: nil when no kit is installed here
// (nothing to drift from — F-CLI021-R1-04) or the check itself failed, else
// the drifted targets.
func kitDrift(root string) []string {
	if st, err := os.Stat(filepath.Join(root, ".modernpath", "rdd")); err != nil || !st.IsDir() {
		return nil
	}
	drift, err := freshnessKitCheck(root)
	if err != nil {
		return nil
	}
	if drift == nil {
		return []string{}
	}
	return drift
}

// behindRelease orders the running build against the latest release by
// MAJOR.MINOR.PATCH after stripping the ` (date)` and `+sha[.dirty]` stamp;
// a `-dev` pre-release sits above the previous release and below its own.
// An unstamped test binary ("dev") and an unreadable latest are never behind.
func behindRelease(version, latest string) bool {
	v, vdev, ok := parseSemver(version)
	if !ok {
		return false
	}
	l, _, ok := parseSemver(latest)
	if !ok {
		return false
	}
	for i := 0; i < 3; i++ {
		if v[i] != l[i] {
			return v[i] < l[i]
		}
	}
	return vdev
}

func parseSemver(s string) (parts [3]int, dev bool, ok bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, " ")
	s, _, _ = strings.Cut(s, "+")
	s, pre, _ := strings.Cut(s, "-")
	dev = pre != ""
	fields := strings.Split(s, ".")
	if len(fields) != 3 {
		return parts, false, false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return parts, false, false
		}
		parts[i] = n
	}
	return parts, dev, true
}

type releaseCache struct {
	Tag       string `json:"tag"`
	FetchedAt string `json:"fetched_at"`
}

// latestRelease answers from the one-day cache under .modernpath/, else
// looks the release up once with the bounded client and caches it. A failed
// or refused lookup (offline, the anonymous rate limit) keeps the old tag if
// any and is otherwise silent; the fetch time lives inside the file, so a
// copied workspace cannot present a stale cache as fresh.
func latestRelease(root string, now time.Time) string {
	path := filepath.Join(root, ".modernpath", releaseCacheName)
	var cached releaseCache
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &cached) == nil {
		if at, err := time.Parse(time.RFC3339, cached.FetchedAt); err == nil && now.Sub(at) < releaseCacheTTL && now.Sub(at) >= 0 {
			return strings.TrimPrefix(cached.Tag, "v")
		}
	}
	tag, ok := fetchLatestRelease()
	if !ok {
		return strings.TrimPrefix(cached.Tag, "v")
	}
	if b, err := json.Marshal(releaseCache{Tag: tag, FetchedAt: now.UTC().Format(time.RFC3339)}); err == nil {
		if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
			_ = os.WriteFile(path, b, 0o644)
		}
	}
	return strings.TrimPrefix(tag, "v")
}

// fetchLatestRelease is the one call that leaves the platform: GitHub, not
// the API host, so it takes no platform.Prepare and no credential.
func fetchLatestRelease() (string, bool) {
	resp, err := releaseHTTPClient.Get(releaseLookupURL)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", false
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || strings.TrimSpace(body.TagName) == "" {
		return "", false
	}
	return strings.TrimSpace(body.TagName), true
}
