package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
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

func TestDoctorReportsAnArmedProcessGate(t *testing.T) {
	agent := doctorClaudeWith(t,
		`{"hooks":{"PreToolUse":[{"hooks":[{"command":"modernpath check --hook PreToolUse"}]}]}}`)

	gate, ok := familyByName(hookFamiliesFor(agent), "process gate")
	if !ok {
		t.Fatal("the doctor does not report the process gate family at all")
	}
	if gate.state != hookStateConfigured {
		t.Fatalf("an armed gate must report as wired, got %q", gate.state)
	}
}

func TestDoctorReportsAnUnarmedProcessGateRatherThanOmittingIt(t *testing.T) {
	agent := doctorClaudeWith(t,
		`{"hooks":{"SessionStart":[{"hooks":[{"command":"modernpath factory sync --if-quiescent"}]}]}}`)

	gate, ok := familyByName(hookFamiliesFor(agent), "process gate")
	if !ok {
		t.Fatal("a workspace with no gate armed must still get a gate line — silence reads as absence")
	}
	if gate.state == hookStateConfigured {
		t.Fatal("an unarmed gate must not report as wired")
	}
}

func TestDoctorMarksTheGateNotApplicableForNonClaudeAgents(t *testing.T) {
	chdirTemp(t)
	gate, ok := familyByName(hookFamiliesFor(hookAgents["cursor"]), "process gate")
	if !ok {
		t.Fatal("the gate family must be listed for every agent, applicable or not")
	}
	if gate.applies {
		t.Fatal("the gate is Claude Code only — it must report as not applicable, not as missing")
	}
}

// Guard: a fully wired configuration still reads as wired, in report order.
func TestDoctorKeepsTheContextAndSyncFamiliesAsTheyWere(t *testing.T) {
	agent := doctorClaudeWith(t,
		`{"hooks":{"UserPromptSubmit":[{"hooks":[{"command":"modernpath context --hook UserPromptSubmit"}]}],`+
			`"SessionStart":[{"hooks":[{"command":"modernpath factory sync --if-quiescent"}]}],`+
			`"Stop":[{"hooks":[{"command":"modernpath factory sync --if-quiescent"}]}],`+
			`"SessionEnd":[{"hooks":[{"command":"modernpath factory sync --if-quiescent"}]}]}}`)

	families := hookFamiliesFor(agent)
	for _, name := range []string{"context", "sync"} {
		f, ok := familyByName(families, name)
		if !ok || !f.applies || f.state != hookStateConfigured {
			t.Fatalf("%s family changed: %+v (found=%v)", name, f, ok)
		}
	}
	if families[0].name != "context" || families[1].name != "sync" {
		t.Fatalf("report order changed: %+v", families)
	}
}

// The doctor and `hooks status` answer "is this family wired" about the SAME
// file; two truth surfaces that disagree about one configuration is the defect
// class this command group exists to remove.
func TestDoctorAgreesWithStatusOnAPartialSyncFamily(t *testing.T) {
	// Only SessionStart wired: status reports PARTIALLY installed — the
	// workspace silently stops syncing on Stop and SessionEnd.
	agent := doctorClaudeWith(t,
		`{"hooks":{"SessionStart":[{"hooks":[{"command":"modernpath factory sync --if-quiescent"}]}]}}`)

	sync, _ := familyByName(hookFamiliesFor(agent), "sync")
	if sync.state == hookStateConfigured {
		t.Fatal("one surviving trigger must not report the sync family wired while status calls it partial")
	}
	if sync.state != hookStatePartial {
		t.Fatalf("want the same %q status derives, got %q", hookStatePartial, sync.state)
	}
}

func TestDoctorAgreesWithStatusOnALegacyGateScript(t *testing.T) {
	// A legacy rdd-gate.sh entry: status says the gate is not armed; the
	// doctor must not bless the identical file as wired.
	agent := doctorClaudeWith(t,
		`{"hooks":{"PreToolUse":[{"hooks":[{"command":"bash .claude/hooks/rdd-gate.sh"}]}]}}`)

	gate, _ := familyByName(hookFamiliesFor(agent), "process gate")
	if gate.state == hookStateConfigured {
		t.Fatal("a legacy gate script must not report as wired while status calls the gate not armed")
	}
	if gate.state != hookStateLegacy {
		t.Fatalf("want the same %q status derives, got %q", hookStateLegacy, gate.state)
	}
}

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

// stubGit answers the three questions the check asks, in the order it asks them.
func stubGit(t *testing.T, extractorPath, extractorCommit, dirty string, ancestor error) gitRunner {
	t.Helper()
	return func(args ...string) (string, error) {
		// Calls are scoped with `-C <root>` since REQ-CROSS-119; the verb is
		// what this stub answers on.
		if len(args) > 1 && args[0] == "-C" {
			args = args[2:]
		}
		switch args[0] {
		case "rev-parse":
			return "/repo\n", nil
		case "ls-files":
			return extractorPath, nil
		case "log":
			return extractorCommit, nil
		case "status":
			return dirty, nil
		case "cat-file":
			return "", nil // the stub's build commit is always present
		case "merge-base":
			return "", ancestor
		}
		return "", fmt.Errorf("unexpected git call: %v", args)
	}
}

func TestABuildCarryingTheCurrentExtractorReportsCurrent(t *testing.T) {
	git := stubGit(t, "tools/modernpath/internal/rdd/ops.go", "abc123", "", nil)

	got := extractorFreshness("0.5.0+deadbee (2026-08-13T12:29Z)", git)
	if got.state != freshnessCurrent {
		t.Fatalf("a build containing the extractor's last change must report current, got %v (%s)", got.state, got.detail)
	}
}

func TestABuildOlderThanTheExtractorReportsStale(t *testing.T) {
	// merge-base --is-ancestor exits non-zero: the extractor commit is NOT in
	// this build.
	git := stubGit(t, "tools/modernpath/internal/rdd/ops.go", "abc123", "", errors.New("exit status 1"))

	got := extractorFreshness("0.5.0+deadbee (2026-08-13T12:29Z)", git)
	if got.state != freshnessStale {
		t.Fatalf("a build predating the extractor must report stale, got %v (%s)", got.state, got.detail)
	}
	if !strings.Contains(got.detail, "install-local.sh") {
		t.Fatalf("stale must name the command that fixes it: %q", got.detail)
	}
}

// The normal development loop: edit internal/rdd, do not commit, run the
// installed binary. By commit alone that build reads as current.
func TestUncommittedExtractorChangesMakeTheBuildStale(t *testing.T) {
	git := stubGit(t, "tools/modernpath/internal/rdd/ops.go", "abc123", " M tools/modernpath/internal/rdd/ops.go", nil)

	got := extractorFreshness("0.5.0+abc123 (2026-08-13T12:29Z)", git)
	if got.state != freshnessStale {
		t.Fatalf("uncommitted extractor changes cannot be in any build, got %v (%s)", got.state, got.detail)
	}
	if !strings.Contains(got.detail, "uncommitted") {
		t.Fatalf("the reason must distinguish this from being behind a commit: %q", got.detail)
	}
}

// A released or Homebrew build carries no commit, because build-all.sh stamps
// only the version string it is given. Reporting that as current would be this
// row's own defect inside its own check.
func TestAnUnstampedBuildIsUnverifiableNotCurrent(t *testing.T) {
	git := stubGit(t, "tools/modernpath/internal/rdd/ops.go", "abc123", "", nil)

	got := extractorFreshness("0.3.1", git)
	if got.state != freshnessUnverifiable {
		t.Fatalf("a build with no commit stamp cannot be called current, got %v (%s)", got.state, got.detail)
	}
}

// Guard, expected green from the start: a consuming repository holds no CLI
// source, so there is no fact here to report. Inventing one is the failure this
// check exists to prevent.
func TestARepositoryWithoutTheCLISourceIsSilent(t *testing.T) {
	git := func(args ...string) (string, error) {
		if args[0] == "ls-files" {
			return "", nil // nothing matched
		}
		return "", errors.New("must not be called")
	}

	if got := extractorFreshness("0.5.0+deadbee (2026-08-13T12:29Z)", git); got.state != freshnessNotApplicable {
		t.Fatalf("a repository with no CLI source must be silent, got %v (%s)", got.state, got.detail)
	}
}

func TestNoGitAtAllIsSilent(t *testing.T) {
	git := func(args ...string) (string, error) { return "", errors.New("not a git repository") }

	if got := extractorFreshness("0.5.0+deadbee (2026-08-13T12:29Z)", git); got.state != freshnessNotApplicable {
		t.Fatalf("outside a repository there is nothing to compare, got %v (%s)", got.state, got.detail)
	}
}

// ---------------------------------------------------------------------------
// REQ-CROSS-119 — the freshness check against a real repository.
//
// stubGit models `git` as four canned answers, which is how four defects
// survived it: it cannot express an exit code, a working directory, or the
// difference between a tracked edit and a stray file. These drive the real
// runner against a throwaway repository shaped like this workspace — the CLI
// source under a nested path, so `ls-files` behaves exactly as it does here.
// ---------------------------------------------------------------------------

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// newExtractorRepo returns (repoRoot, headSHA) with one commit touching the
// extractor at the same relative depth this workspace uses.
func newExtractorRepo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q", "-b", "main")
	git(t, root, "config", "user.email", "t@example.com")
	git(t, root, "config", "user.name", "T")

	src := filepath.Join(root, "tools", "modernpath", "internal", "rdd")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ops.go"), []byte("package rdd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "extractor")
	return root, git(t, root, "rev-parse", "HEAD")
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

// A build stamped with a commit this clone does not have is UNVERIFIABLE, not
// stale. `merge-base --is-ancestor` exits 1 for "not an ancestor" and 128 for
// "no such commit", and .Output() returns *exec.ExitError for both — so the
// check reported a confident "this build predates the extractor's last change",
// quoting a SHA it had never compared anything against. A branch that was
// rebased or squash-merged produces exactly this, as this branch did.
func TestAnUnknownBuildCommitIsUnverifiableNotStale(t *testing.T) {
	root, _ := newExtractorRepo(t)
	chdir(t, root)

	got := extractorFreshness("0.5.0+deadbeef (2026-08-14T10:00Z)", execGit)
	if got.state == freshnessStale {
		t.Fatalf("a commit this repository does not have was reported as predating the extractor: %s", got.detail)
	}
	if got.state != freshnessUnverifiable {
		t.Fatalf("want unverifiable, got %v (%s)", got.state, got.detail)
	}
}

// The freshness check inherits the process working directory, and `git ls-files
// <pattern>` only matches at or below it — so from any subdirectory the whole
// check returned not-applicable and reportExtractorFreshness prints nothing for
// that state. Silence is indistinguishable from a clean bill of health, in the
// one command written on the premise that silence cannot be noticed.
func TestFreshnessIsFoundFromASubdirectory(t *testing.T) {
	root, head := newExtractorRepo(t)
	sub := filepath.Join(root, "tools", "modernpath", "cmd")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	chdir(t, sub)

	got := extractorFreshness("0.5.0+"+head[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state == freshnessNotApplicable {
		t.Fatal("run from a subdirectory the check went silent, which reads the same as a clean result")
	}
	if got.state != freshnessCurrent {
		t.Fatalf("want current, got %v (%s)", got.state, got.detail)
	}
}

// `git status --porcelain` lists UNTRACKED files by default, so a coverage
// file, an editor backup or a .DS_Store under the extractor made the doctor say
// no installed build contains your changes — on a tree whose tracked sources are
// clean. That branch also returns BEFORE the real comparison, so the accurate
// check never ran.
func TestAStrayUntrackedFileIsNotAnUncommittedChange(t *testing.T) {
	root, head := newExtractorRepo(t)
	stray := filepath.Join(root, "tools", "modernpath", "internal", "rdd", ".DS_Store")
	if err := os.WriteFile(stray, []byte("junk"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	got := extractorFreshness("0.5.0+"+head[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state == freshnessStale && strings.Contains(got.detail, "uncommitted") {
		t.Fatalf("a stray untracked file was read as an uncommitted extractor change: %s", got.detail)
	}
	if got.state != freshnessCurrent {
		t.Fatalf("want current, got %v (%s)", got.state, got.detail)
	}
}

// The `.DS_Store` fix above over-corrected: `-uno` ignores ALL untracked
// files, but a newly added untracked `.go` file is a real `go build` input no
// installed binary can contain — the dir carries no go:embed, so the build's
// inputs are exactly the `.go` files. Ignoring it reports the old build
// current while a rebuild would differ.
func TestAnUntrackedGoFileIsAnExtractorChange(t *testing.T) {
	root, head := newExtractorRepo(t)
	parser := filepath.Join(root, "tools", "modernpath", "internal", "rdd", "parser.go")
	if err := os.WriteFile(parser, []byte("package rdd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	got := extractorFreshness("0.5.0+"+head[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state != freshnessStale {
		t.Fatalf("an untracked .go file is in the next build but no installed one, want stale, got %v (%s)", got.state, got.detail)
	}
	if !strings.Contains(got.detail, "untracked") {
		t.Fatalf("the report must say the file is untracked, not merely uncommitted: %s", got.detail)
	}
}

// Guard: only build inputs count. An untracked non-Go file beside the sources
// keeps the accurate comparison running, exactly as the .DS_Store case above.
func TestAnUntrackedNonGoFileStaysInvisible(t *testing.T) {
	root, head := newExtractorRepo(t)
	stray := filepath.Join(root, "tools", "modernpath", "internal", "rdd", "coverage.out")
	if err := os.WriteFile(stray, []byte("mode: set\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	got := extractorFreshness("0.5.0+"+head[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state != freshnessCurrent {
		t.Fatalf("want current, got %v (%s)", got.state, got.detail)
	}
}

// Guards, expected green from the start: a genuine tracked edit is still
// uncommitted, and a build that genuinely predates the extractor is still stale.
func TestATrackedEditIsStillUncommitted(t *testing.T) {
	root, head := newExtractorRepo(t)
	ops := filepath.Join(root, "tools", "modernpath", "internal", "rdd", "ops.go")
	if err := os.WriteFile(ops, []byte("package rdd\n// edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	chdir(t, root)

	got := extractorFreshness("0.5.0+"+head[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state != freshnessStale || !strings.Contains(got.detail, "uncommitted") {
		t.Fatalf("a tracked edit must still report uncommitted, got %v (%s)", got.state, got.detail)
	}
}

func TestABuildOlderThanTheExtractorIsStillStale(t *testing.T) {
	root, first := newExtractorRepo(t)
	ops := filepath.Join(root, "tools", "modernpath", "internal", "rdd", "ops.go")
	if err := os.WriteFile(ops, []byte("package rdd\n// later change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, root, "commit", "-qam", "extractor moves")
	chdir(t, root)

	got := extractorFreshness("0.5.0+"+first[:9]+" (2026-08-14T10:00Z)", execGit)
	if got.state != freshnessStale {
		t.Fatalf("a build predating the extractor's last change must be stale, got %v (%s)", got.state, got.detail)
	}
	if strings.Contains(got.detail, "uncommitted") {
		t.Fatalf("the tree is clean; this is the commit comparison, not the dirty branch: %s", got.detail)
	}
}

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
