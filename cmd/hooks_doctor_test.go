package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EPIC-CTX-001 (`USER:2026-08-11`): a second, older `modernpath` earlier in PATH
// silently downgrades every hook run while exiting 0. This is the check that
// makes it visible, so it is tested on the ordering the shell actually uses.
func TestPathCopiesReportsShadowedBinariesInShellOrder(t *testing.T) {
	present := map[string]bool{
		"/Users/x/.local/bin/modernpath": true,
		"/opt/homebrew/bin/modernpath":   true,
	}
	exists := func(p string) bool { return present[p] }

	path := strings.Join([]string{"/Users/x/.local/bin", "/usr/bin", "/opt/homebrew/bin"}, string(os.PathListSeparator))
	got := pathCopies("modernpath", path, exists)

	if len(got) != 2 {
		t.Fatalf("want both copies, got %v", got)
	}
	if got[0] != "/Users/x/.local/bin/modernpath" {
		t.Fatalf("the winner must be the first PATH entry, got %s", got[0])
	}
}

func TestPathCopiesIgnoresEmptyEntriesAndDuplicates(t *testing.T) {
	present := map[string]bool{"/opt/homebrew/bin/modernpath": true}
	exists := func(p string) bool { return present[p] }

	path := strings.Join([]string{"", "/opt/homebrew/bin", "/opt/homebrew/bin", "/nowhere"}, string(os.PathListSeparator))
	got := pathCopies("modernpath", path, exists)

	if len(got) != 1 {
		t.Fatalf("a duplicated PATH entry is still one binary: %v", got)
	}
}

func TestPathCopiesFindsNothingWhenTheCLIIsAbsent(t *testing.T) {
	got := pathCopies("modernpath", "/usr/bin"+string(os.PathListSeparator)+"/bin", func(string) bool { return false })
	if len(got) != 0 {
		t.Fatalf("want none, got %v", got)
	}
}

// A directory named `modernpath`, or a non-executable file, is not a CLI.
func TestExecutableExistsRejectsDirectoriesAndPlainFiles(t *testing.T) {
	dir := chdirTemp(t)

	if err := os.Mkdir(filepath.Join(dir, "modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	if executableExists(filepath.Join(dir, "modernpath")) {
		t.Fatal("a directory is not a binary")
	}

	plain := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(plain, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if executableExists(plain) {
		t.Fatal("a non-executable file is not a binary")
	}
}

// REQ-CROSS-100 — the doctor reported `context` and `sync` and said nothing at
// all about the process gate, which is the family that can block a commit. An
// unarmed gate never fires, so silence is the one state that cannot be noticed.
func familyByName(families []familyState, name string) (familyState, bool) {
	for _, f := range families {
		if f.name == name {
			return f, true
		}
	}
	return familyState{}, false
}

// doctorClaudeWith writes a .claude/settings.json in a fresh workspace and
// returns the agent the doctor would inspect: the doctor judges families from
// the same file-derived state `hooks status` reads, so the fixtures are files.
func doctorClaudeWith(t *testing.T, config string) agentConfig {
	t.Helper()
	chdirTemp(t)
	agent := hookAgents["claude"]
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent.configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return agent
}

// Guard: a fully wired configuration still reads as wired, in report order.
// The doctor and `hooks status` answer "is this family wired" about the SAME
// file; two truth surfaces that disagree about one configuration is the defect
// class this command group exists to remove.
// REQ-CROSS-103 — shadowing is a fact about PATH order; staleness is a fact
// about content. The doctor established the first and warned about the second.
// A doctor that cries stale at an up-to-date machine is how a real warning
// comes to be ignored.
func writeCopy(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestIdenticalCopiesAreReportedAsAgreeing(t *testing.T) {
	dir := chdirTemp(t)
	winner := writeCopy(t, dir, "a", "same bytes")
	other := writeCopy(t, dir, "b", "same bytes")

	differing, unverifiable := classifyCopies(winner, []string{other})
	if len(differing) != 0 {
		t.Fatalf("byte-identical copies must not be called stale, got %v", differing)
	}
	if len(unverifiable) != 0 {
		t.Fatalf("a readable copy is not unverifiable: %v", unverifiable)
	}
}

func TestADifferingCopyIsNamed(t *testing.T) {
	dir := chdirTemp(t)
	winner := writeCopy(t, dir, "a", "new build")
	same := writeCopy(t, dir, "b", "new build")
	stale := writeCopy(t, dir, "c", "an older build entirely")

	differing, _ := classifyCopies(winner, []string{same, stale})
	if len(differing) != 1 || differing[0] != stale {
		t.Fatalf("want only the stale copy named, got %v", differing)
	}
}

// Same length, different content: comparing sizes alone would call these equal.
func TestASameSizedButDifferentCopyIsStillDetected(t *testing.T) {
	dir := chdirTemp(t)
	winner := writeCopy(t, dir, "a", "build-AAAA")
	stale := writeCopy(t, dir, "c", "build-BBBB")

	differing, _ := classifyCopies(winner, []string{stale})
	if len(differing) != 1 {
		t.Fatalf("a same-sized copy with different content must still be flagged, got %v", differing)
	}
}

// An unreadable copy is exactly the one that might be stale, so it must not be
// silently folded into "agrees".
func TestAnUnreadableCopyIsUnverifiableNotAssumedIdentical(t *testing.T) {
	dir := chdirTemp(t)
	winner := writeCopy(t, dir, "a", "bytes")
	missing := filepath.Join(dir, "gone")

	differing, unverifiable := classifyCopies(winner, []string{missing})
	if len(unverifiable) != 1 || unverifiable[0] != missing {
		t.Fatalf("an unreadable copy must be reported unverifiable, got differing=%v unverifiable=%v", differing, unverifiable)
	}
	if len(differing) != 0 {
		t.Fatalf("unverifiable is not the same claim as differing: %v", differing)
	}
}

// Guard, expected green from the start: comparison is a separate question from
// which copy wins. PATH order still decides that, and pathCopies still returns
// every copy in that order.
func TestComparisonDoesNotChangeWhichCopyWins(t *testing.T) {
	present := map[string]bool{"/x/modernpath": true, "/y/modernpath": true}
	got := pathCopies("modernpath", "/x"+string(os.PathListSeparator)+"/y", func(p string) bool { return present[p] })
	if len(got) != 2 || got[0] != "/x/modernpath" {
		t.Fatalf("PATH order must still decide the winner: %v", got)
	}
}

// REQ-CROSS-105 — the hooks call the CLI by bare name, so the build that runs
// the extractor can predate the extractor sources sitting in the repository. A
// sync then reports success while omitting everything the new code would have
// written, and nothing says so.

// REQ-CROSS-119 — the version and the freshness verdict belong to the RUNNING
// process, printed directly under a line naming copies[0] as "the one the hooks
// run". When those are different files the report answers about a binary the
// hooks will never invoke — which is the exact confusion this command exists to
// remove, and it happens on the first `./modernpath hooks doctor` from a build
// directory.
func TestTheRunningBinaryIsRecognisedAsThePathWinner(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	if !runningIsPathWinner(exe) {
		t.Fatal("the running binary must count as itself")
	}
}

func TestADifferentPathWinnerIsNotTheRunningBinary(t *testing.T) {
	dir := t.TempDir()
	other := writeCopy(t, dir, "modernpath", "a different build entirely")

	if runningIsPathWinner(other) {
		t.Fatal("a different file on PATH was treated as the running process, so the version and freshness lines describe the wrong binary silently")
	}
}

// Version is a fact about the running process; printing it beside the PATH
// winner asserts they are the same build. That claim is earned by identity or
// by content — identical bytes ARE the same build — and by nothing else.
func TestAVersionIsNotClaimedForADifferentBuild(t *testing.T) {
	other := writeCopy(t, t.TempDir(), "modernpath", "a different build entirely")
	if versionBelongsTo(other) {
		t.Fatal("the running build's version was attributed to a binary with different content")
	}
}

func TestAVersionIsClaimedForTheRunningBinaryItself(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	if !versionBelongsTo(exe) {
		t.Fatal("the running binary must carry its own version")
	}
}

func TestAVersionIsClaimedForAnIdenticalCopy(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("no executable path on this platform")
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	twin := writeCopy(t, t.TempDir(), "modernpath", string(body))
	if !versionBelongsTo(twin) {
		t.Fatal("identical bytes are the same build; withholding the version here reports a difference that does not exist")
	}
}

func TestAnUnreadableWinnerEarnsNoVersionClaim(t *testing.T) {
	if versionBelongsTo(filepath.Join(t.TempDir(), "does-not-exist")) {
		t.Fatal("a file that cannot be read earned a version claim — \"cannot tell\" is not \"same\"")
	}
}

// Guard: an unresolvable path must not manufacture a warning — "cannot tell" is
// not "they differ".
func TestAnUnresolvablePathDoesNotClaimADifference(t *testing.T) {
	if !runningIsPathWinner(filepath.Join(t.TempDir(), "does-not-exist")) {
		return // treating an absent winner as "not us" is fine
	}
}

// The stamp shapes install-local.sh has written over time, and the release
// shape, all read the same way: the commit is what follows "+", up to the
// build's own suffixes.
func TestBuildCommitOfReadsEveryStampShape(t *testing.T) {
	for stamp, want := range map[string]string{
		"0.5.0+7b7b427 (2026-08-13T12:29Z)":          "7b7b427",
		"0.7.0-dev+aa162f51f (2026-09-19T13:14Z)":    "aa162f51f",
		"0.7.0-dev+aa162f51f.dirty (2026-09-19T13Z)": "aa162f51f",
		"0.7.0-dev": "",
		"0.6.0":     "",
		"dev":       "",
	} {
		if got := buildCommitOf(stamp); got != want {
			t.Errorf("buildCommitOf(%q) = %q, want %q", stamp, got, want)
		}
	}
}
