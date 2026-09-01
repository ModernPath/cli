package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// EPIC-CTX-001 (`USER:2026-08-11`): the hooks invoke `modernpath` by bare name,
// so PATH decides which build runs. On the machine this was written, two copies
// existed and the earlier one shadowed the newer — the deadline and the outcome
// log were committed, tested and simply never executed. Nothing reported that,
// because a stale binary exits cleanly (`RUN:2026-08-11`).
var hooksDoctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Report which modernpath the hooks will actually run",
	Long: `Reports every modernpath on PATH, which one wins, and whether the hooks
are wired to it.

The hooks call the CLI by name. A second, older copy earlier in PATH silently
downgrades every hook run while still exiting 0 — the failure this command
exists to make visible.`,
	RunE: runHooksDoctor,
}

func init() {
	hooksCmd.AddCommand(hooksDoctorCmd)
}

// pathCopies returns every executable named `name` on the given PATH, in the
// order the shell would consider them. The first is the one that runs; any
// others are shadowed.
func pathCopies(name, pathEnv string, exists func(string) bool) []string {
	var found []string
	seen := map[string]bool{}

	for _, dir := range strings.Split(pathEnv, string(os.PathListSeparator)) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, name)
		if seen[candidate] || !exists(candidate) {
			continue
		}
		seen[candidate] = true
		found = append(found, candidate)
	}

	return found
}

// classifyCopies splits the shadowed copies into those that would run different
// code from the winner and those that cannot be read to find out.
//
// Being earlier on PATH and being out of date are different facts. Warning
// about the second on evidence of only the first is how a real staleness
// warning comes to be ignored.
func classifyCopies(winner string, others []string) (differing, unverifiable []string) {
	winnerSum, err := fileDigest(winner)
	if err != nil {
		// nothing can be compared against a winner we cannot read; saying so
		// beats reporting every other copy as fine or as broken
		return nil, append([]string(nil), others...)
	}

	for _, other := range others {
		sum, err := fileDigest(other)
		switch {
		case err != nil:
			unverifiable = append(unverifiable, other)
		case sum != winnerSum:
			differing = append(differing, other)
		}
	}
	return differing, unverifiable
}

// execGit is the production gitRunner. Tests drive the same function against a
// throwaway repository, not a stub: a stub cannot express an exit code, a
// working directory, or the difference between a tracked edit and a stray file.
func execGit(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	return string(out), err
}

func reportExtractorFreshness() {
	result := extractorFreshness(Version, execGit)

	switch result.state {
	case freshnessCurrent:
		printSuccess("Extractor: this build carries the repository's current extractor\n")
	case freshnessStale:
		printWarning("Extractor: %s\n", result.detail)
	case freshnessUnverifiable:
		printWarning("Extractor: %s\n", result.detail)
	}
	// freshnessNotApplicable prints nothing: there is no fact here to report.
}

// A repository that carries the CLI's own source can tell whether the build
// running the extractor actually contains that source. One that does not carries
// no such fact, and this check stays silent there rather than inventing a verdict.
type buildFreshness int

const (
	freshnessNotApplicable buildFreshness = iota // no CLI source here, or no git
	freshnessCurrent
	freshnessStale
	freshnessUnverifiable
)

type freshnessResult struct {
	state  buildFreshness
	detail string
}

// gitRunner is the seam: the tests supply repository facts, so the check's
// reasoning is verified without staging commits.
type gitRunner func(args ...string) (string, error)

// extractorFreshness compares the running build against the extractor sources
// this repository would have it run.
func extractorFreshness(version string, git gitRunner) freshnessResult {
	// Resolve the repository root and scope every call to it. `git ls-files
	// <pattern>` matches only at or below the working directory, so without
	// this the whole check went silent from any subdirectory — and silence is
	// indistinguishable from a clean result, in the command written on the
	// premise that silence cannot be noticed (the same cwd-relative bypass as
	// REQ-CROSS-098, one command over).
	top, err := git("rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(top) == "" {
		return freshnessResult{state: freshnessNotApplicable} // no git here
	}
	at := strings.TrimSpace(top)

	sourcePath, err := git("-C", at, "ls-files", "*internal/rdd/ops.go")
	if err != nil || strings.TrimSpace(sourcePath) == "" {
		return freshnessResult{state: freshnessNotApplicable}
	}
	dir := filepath.Dir(strings.Split(strings.TrimSpace(sourcePath), "\n")[0])

	// Uncommitted changes are in no build by definition, so they settle it
	// before any commit comparison. This is the normal development loop — edit
	// the extractor, run the installed binary — where commits alone say
	// "current" and are wrong.
	//
	// Untracked files are classified by whether they are build inputs, not
	// ignored wholesale: this directory carries no go:embed, so an untracked
	// .go file is in the next build and in no installed one, while a coverage
	// file, an editor backup or a .DS_Store is an input to nothing — ignoring
	// those is what keeps a clean tree reading current, and ignoring the .go
	// file with them is how a brand-new parser goes silently unbuilt.
	if status, err := git("-C", at, "status", "--porcelain", "--", dir); err == nil {
		tracked, untrackedGo := porcelainDirt(status)
		switch {
		case tracked:
			return freshnessResult{
				state:  freshnessStale,
				detail: "the extractor sources have uncommitted changes, so no installed build contains them — reinstall with scripts/install-local.sh after committing, or expect this build to ignore them",
			}
		case untrackedGo != "":
			return freshnessResult{
				state:  freshnessStale,
				detail: fmt.Sprintf("the extractor sources include an untracked Go file (%s), so no installed build contains it — commit it and reinstall with scripts/install-local.sh, or expect this build to ignore it", untrackedGo),
			}
		}
	}

	build := buildCommitOf(version)
	if build == "" {
		return freshnessResult{
			state:  freshnessUnverifiable,
			detail: "this build carries no commit stamp (released builds stamp only a version), so it cannot be checked against the extractor sources in this repository — scripts/install-local.sh stamps one",
		}
	}

	// Does this clone even have the build's commit? `merge-base --is-ancestor`
	// exits 1 for "not an ancestor" and 128 for "no such commit", and both
	// reach us as the same *exec.ExitError — so asking first is the only way to
	// keep the two apart. A branch that was rebased or squash-merged produces
	// exactly this, and calling it stale quotes a SHA nothing was compared to.
	if _, err := git("-C", at, "cat-file", "-e", build+"^{commit}"); err != nil {
		return freshnessResult{
			state: freshnessUnverifiable,
			detail: fmt.Sprintf("this build is stamped %s, which is not a commit in this repository — it may have been rebased, squash-merged, or built from another clone, so it cannot be compared with the extractor sources here",
				shortSHA(build)),
		}
	}

	extractor, err := git("-C", at, "log", "-1", "--format=%H", "--", dir)
	if err != nil || strings.TrimSpace(extractor) == "" {
		return freshnessResult{state: freshnessNotApplicable}
	}

	// With the build's commit known to exist, a non-zero exit here means what
	// it says: the extractor's last change is not contained in this build.
	if _, err := git("-C", at, "merge-base", "--is-ancestor", strings.TrimSpace(extractor), build); err != nil {
		return freshnessResult{
			state:  freshnessStale,
			detail: fmt.Sprintf("this build predates the extractor's last change (%s) — reinstall with scripts/install-local.sh, or a sync will report success while omitting what the newer code writes", shortSHA(extractor)),
		}
	}
	return freshnessResult{state: freshnessCurrent}
}

// porcelainDirt separates the two claims a `git status --porcelain` listing
// supports about the extractor sources: a tracked modification, and an
// untracked file that is a build input (.go — the extractor embeds nothing
// else). Every other untracked entry is an input to nothing and stays
// invisible.
func porcelainDirt(status string) (tracked bool, untrackedGo string) {
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "?? ") {
			// porcelain quotes paths containing specials; the suffix test
			// needs the bare name either way
			path := strings.Trim(strings.TrimSpace(line[3:]), `"`)
			if strings.HasSuffix(path, ".go") && untrackedGo == "" {
				untrackedGo = filepath.Base(path)
			}
			continue
		}
		tracked = true
	}
	return tracked, untrackedGo
}

// buildCommitOf reads the commit out of a stamp like "0.5.0+7b7b427 (…)".
// Anything without one — a released build, or "dev" — yields "".
func buildCommitOf(version string) string {
	_, after, found := strings.Cut(version, "+")
	if !found {
		return ""
	}
	commit := strings.TrimSpace(strings.Split(after, " ")[0])
	if commit == "" {
		return ""
	}
	return commit
}

func shortSHA(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 9 {
		return sha[:9]
	}
	return sha
}

func copyCount(n int) string {
	if n == 1 {
		return "1 other copy"
	}
	return fmt.Sprintf("%d other copies", n)
}

// Contents, not size or mtime: two builds minutes apart are routinely the same
// length, and that is exactly when a shadow is hardest to spot by eye.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// runningIsPathWinner reports whether the process answering this command is the
// same file PATH resolves `modernpath` to.
//
// Version and the extractor verdict both describe the RUNNING binary, and they
// print directly under a line naming copies[0] as "the one the hooks run". When
// those are different files — `./modernpath hooks doctor` from a build
// directory, which is the first thing anyone does — the report answers about a
// binary the hooks will never invoke.
//
// An unresolvable path returns true: "cannot tell" is not "they differ", and
// manufacturing a warning here is the defect this command exists to remove.
func runningIsPathWinner(winner string) bool {
	exe, err := os.Executable()
	if err != nil {
		return true
	}
	a, errA := filepath.EvalSymlinks(exe)
	b, errB := filepath.EvalSymlinks(winner)
	if errA != nil || errB != nil {
		return exe == winner
	}
	return a == b
}

func executableExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// versionBelongsTo reports whether the running build's Version describes the
// file at path. Version is a fact about the running process; printing it
// beside another path asserts they are the same build, and that claim is
// earned two ways only: the path IS the running file, or its bytes are
// identical — the digest comparison the shadow report already pays for.
// Unreadable earns no claim; "cannot tell" is not "same".
func versionBelongsTo(path string) bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if a, errA := filepath.EvalSymlinks(exe); errA == nil {
		if b, errB := filepath.EvalSymlinks(path); errB == nil && a == b {
			return true
		}
	}
	a, errA := fileDigest(exe)
	b, errB := fileDigest(path)
	return errA == nil && errB == nil && a == b
}

// versionSuffix is the version claim a path's report line may carry: the
// running Version when it belongs to that path, nothing otherwise — a line
// with no version makes no false one.
func versionSuffix(path string) string {
	if versionBelongsTo(path) {
		return fmt.Sprintf(" (version %s)", Version)
	}
	return ""
}

func runHooksDoctor(cmd *cobra.Command, args []string) error {
	copies := pathCopies("modernpath", os.Getenv("PATH"), executableExists)

	switch len(copies) {
	case 0:
		printWarning("No modernpath found on PATH — the hooks are silent no-ops.\n")
		printInfo("Install with scripts/install-local.sh, then re-run 'modernpath hooks install'.\n")
		return nil
	case 1:
		printSuccess("modernpath: %s%s\n", copies[0], versionSuffix(copies[0]))
	default:
		printSuccess("modernpath: %s%s — this is the one the hooks run\n", copies[0], versionSuffix(copies[0]))

		differing, unverifiable := classifyCopies(copies[0], copies[1:])
		if len(differing) == 0 && len(unverifiable) == 0 {
			printSuccess("%s on PATH, all identical to it\n", copyCount(len(copies)-1))
			break
		}
		if len(differing) > 0 {
			printWarning("%s would run DIFFERENT code:\n", copyCount(len(differing)))
			for _, other := range differing {
				fmt.Printf("    %s\n", other)
			}
			printInfo("Shadowed copies go stale silently. scripts/install-local.sh writes all of them.\n")
		}
		if len(unverifiable) > 0 {
			printWarning("%s could not be read, so staleness cannot be ruled out:\n", copyCount(len(unverifiable)))
			for _, other := range unverifiable {
				fmt.Printf("    %s\n", other)
			}
		}
	}

	// Everything below describes the running process. Say so when that is not
	// the binary named above as the one the hooks run.
	if len(copies) > 0 && !runningIsPathWinner(copies[0]) {
		if exe, err := os.Executable(); err == nil {
			printWarning("You are running %s (version %s), which is NOT the copy above — the extractor report below describes this binary\n", exe, Version)
		}
	}

	reportExtractorFreshness()

	agents := detectInstalledAgents()
	if len(agents) == 0 {
		printWarning("No agent config directories found (.claude/.cursor/.codex).\n")
		return nil
	}

	for _, key := range agents {
		agent := hookAgents[key]
		raw, err := os.ReadFile(agent.configPath)
		if err != nil {
			printWarning("%s: no hooks configured (%s)\n", agent.name, agent.configPath)
			continue
		}

		text := string(raw)
		if agent.name == "Codex" {
			reportCodexDoctorFamily("context", contextFamilyState(agent))
			reportCodexDoctorFamily("sync", syncFamilyState(agent))
			reportCodexDoctorFamily("process gate", gateFamilyState(agent))
			printInfo("Codex: review trusted/enabled state in /hooks; configuration alone does not prove execution.\n")
		} else {
			for _, f := range hookFamiliesFor(agent) {
				reportHookFamily(agent.name, f)
			}
		}

		if strings.Contains(text, agent.scriptName) {
			printWarning("%s: a hook still points at %s — a file this version does not write. Re-run 'modernpath hooks install'.\n",
				agent.name, agent.scriptName)
		}
	}

	reportRecentOutcomes()
	return nil
}

// familyState is one row of the doctor's per-agent report: the family, whether
// this agent can run it at all, and the same tri-state `hooks status` derives.
type familyState struct {
	name    string
	applies bool
	state   hookFamilyState
}

// hookFamiliesFor lists the families the doctor reports for an agent, in report
// order. A family the agent cannot run is listed as not applicable rather than
// omitted: the doctor answers "what will actually run", and an omitted family
// is indistinguishable from an absent one. That is how the process gate stayed
// invisible — an unarmed gate never fires, so nothing else ever reports it.
//
// Each family's state comes from the same functions `hooks status` reads, so
// the two commands cannot disagree about one file: a raw substring probe here
// called a single surviving trigger "wired" while status called the family
// PARTIALLY installed, and called a legacy gate script "wired" while status
// called the gate not armed.
func hookFamiliesFor(agent agentConfig) []familyState {
	claude := agent.name == "Claude Code"
	families := []familyState{
		{name: "context", applies: true, state: contextFamilyState(agent)},
		{name: "sync", applies: claude},
		{name: "process gate", applies: claude},
		{name: "brief", applies: claude},
	}
	if claude {
		families[1].state = syncFamilyState(agent)
		families[2].state = gateFamilyState(agent)
		families[3].state = briefFamilyState(agent)
	}
	return families
}

// Codex reports a tri-state: configuration alone does not prove execution.
func reportCodexDoctorFamily(family string, state hookFamilyState) {
	if state == hookStateConfigured {
		printSuccess("Codex: %s configured\n", family)
		return
	}
	printWarning("Codex: %s %s\n", family, state)
}

func reportHookFamily(agentName string, f familyState) {
	switch {
	case !f.applies:
		printInfo("%s: %s hook not applicable (Claude Code only)\n", agentName, f.name)
	case f.state == hookStateConfigured:
		printSuccess("%s: %s hook wired\n", agentName, f.name)
	case f.state == hookStatePartial:
		printWarning("%s: %s hook PARTIALLY wired — re-run 'modernpath hooks install'\n", agentName, f.name)
	case f.state == hookStateLegacy:
		printWarning("%s: %s hook in the LEGACY script form — re-run 'modernpath hooks install'\n", agentName, f.name)
	case f.state == hookStateInvalid:
		printWarning("%s: %s hook config could not be parsed\n", agentName, f.name)
	default:
		printWarning("%s: %s hook NOT wired\n", agentName, f.name)
	}
}

// The log is the only place a degraded hook shows up, so the doctor reads it
// rather than asking the user to.
func reportRecentOutcomes() {
	raw, err := os.ReadFile(filepath.Join(".modernpath", "context-hook.log"))
	if err != nil {
		printInfo("No context-hook log yet — run a prompt, then check again.\n")
		return
	}

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) > 10 {
		lines = lines[len(lines)-10:]
	}

	failures := 0
	for _, line := range lines {
		if strings.Contains(line, "· failed ·") {
			failures++
		}
	}

	if failures > 0 {
		printWarning("Context hook: %d of the last %d runs failed:\n", failures, len(lines))
		for _, line := range lines {
			if strings.Contains(line, "· failed ·") {
				fmt.Printf("    %s\n", line)
			}
		}
	} else {
		printSuccess("Context hook: last %d runs healthy\n", len(lines))
	}
}
