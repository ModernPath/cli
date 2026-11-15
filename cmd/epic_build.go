package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 427: epics build record records a delegated build — its code and
// test-file traces, its outcomes read from the test result files and its
// assumed defaults — in one write.

func init() { epicsCmd.AddCommand(newEpicBuildCommand(cliEnvLoad)) }

// maxBuildRecordCriteria is the most distinct criteria one build record sends,
// for results and for regression results each; maxBuildRecordExcluded the
// most excluded entries.
const (
	maxBuildRecordCriteria = 50
	maxBuildRecordExcluded = 50
)

func newEpicBuildCommand(load func() (*cliEnv, error)) *cobra.Command {
	group := &cobra.Command{Use: "build", Short: "Record a delegated build of an Epic", Long: epicBuildHelp}
	record := &cobra.Command{
		Use: "record <epic-id>", Short: "Record a build's traces, test outcomes and assumed defaults in one write",
		Long: epicBuildHelp, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error { return runEpicBuildRecord(cmd, load, args[0]) },
	}
	record.Flags().String("file", "", "Build handoff JSON file, or - for standard input (required)")
	record.Flags().Bool("dry-run", false, "Send the same request with dry_run true; the server writes nothing")
	record.Flags().Bool("json", false, "Emit the server's answer and the local lists as JSON")
	record.Flags().String("agent", "", "Slug of the agent that ran this command; the signed-in person is the human behind it")
	group.AddCommand(record)
	return group
}

const epicBuildHelp = `record <epic-id> --file <build handoff> [--dry-run] [--json] [--agent <slug>]
records a build of an approved Epic in one write: implements traces from the
plan's requirements to the changed source files, verifies traces from TestCases
to their test files, the plan's criterion outcomes read from the test result
files, and the build's assumed defaults. Run it with --dry-run first.

The build handoff is one JSON object; an unknown field is refused:

  {"epic_id": <this Epic>, "head": "<full checkout HEAD SHA>", "base": "<base SHA>",
   "repository_key"?: "<default repository key of every path>",
   "results": [{"format": "trx" | "junit" | "go-test-json", "path": "<file>",
                "root"?: "<directory the runner's paths are relative to>"}],
   "requirements": [{"requirement_id": "<plan requirement UUID>",
                     "repository_key"?: "...", "paths": ["<repository path>"],
                     "unchanged_paths"?: ["<repository path the build did not change>"]}],
   "excluded"?: [{"criterion_id": "<plan criterion UUID>", "reason": "<what is not built>"}],
   "regression"?: [{"criterion_id": "<criterion UUID of a published requirement outside the plan>",
                    "test_case_id": "<UUID of the TestCase the build reran>"}],
   "tests"?: [{"test_case_id": "<UUID>", "repository_key"?: "...",
               "file": "<repository path>", "names": ["<test name as the result file reports it>"]}],
   "assumed_defaults"?: [{"title": "...", "observation": "...", "notes_md"?: "..."}]}

Paths are relative to the repository root. Outcomes: TRX Passed and Warning
pass; Failed fails; Error, Timeout, Aborted, PassedButRunAborted, Disconnected
and InProgress are errors; NotExecuted, NotRunnable and Pending skip;
Inconclusive and Completed are inconclusive. A JUnit testcase with failure
fails, with error is an error, with skipped skips, otherwise passes. A go test
-json test takes its last pass, fail or skip; a test that ran without one is an
error. A go package that fails without test events of its own (a setup or
build failure) records nothing and is listed. Build failures are attributed
from Go 1.24 and later output (a package fail event with FailedBuild).

A result takes its TestCase from the first of: a test_case_id trait or
property; a full TestCase UUID in the test name; a full TestCase UUID on the
line above a test declaration at HEAD (Go func TestX(, a C# method after
[Fact], [Theory], [TestMethod] or [Test], ExUnit test "name", JavaScript
it("name" or test("name"), when the result names that test and comes from the
declaring file's own package, type or file; the handoff's tests list, for
other forms such as NUnit [TestCase] or names in single quotes. A TRX result
names a C# test by the TestMethod name of its test definition when it carries
one, so an xUnit DisplayName does not matter; without one, by its testName.
Each plan criterion takes its TestCase's outcome; one without a result is
listed as not recorded. A TestCase that is neither in the plan nor named by a
regression entry is not sent.

excluded: a plan criterion the build left partly built, with the reason. Its
outcome is not sent. The server writes a gap record routed to the Epic and
replaces the outcomes already stored for that criterion with inconclusive,
also a stored fail or error, so an earlier pass no longer counts and the Epic
cannot complete until the criterion is proven again. This is the one outcome
build record stores that no test run reported; the gap record and the audit
event tell it from an observed outcome.

regression: a TestCase the build reran for a criterion of a published
requirement that no Epic owns. Its outcome is matched like a plan TestCase's
and stored as that criterion's current outcome; no trace is sent, so the
TestCase needs its stored traces. The server refuses a criterion that is in
this plan, owned by active work, not published or OBSOLETE, and one refused
entry refuses the whole build record: drop the entry and run again.

unchanged_paths: files a requirement uses that the build did not change. Each
gets an implements trace and no warning.

Refused, with nothing sent: an epic_id that is not this Epic, a head that is
not the checkout HEAD, a traced file or a regression test file with
uncommitted changes, a path not tracked at HEAD, a missing or unreadable
result file, an ambiguous source match, a TestCase with no test file unless
every criterion it proves is excluded, results or regression results for more
than 50 criteria, more than 50 excluded entries, an excluded criterion that is
not in the plan's verification or is listed twice or has no reason, a
regression criterion of a plan requirement, and an unchanged path that the
build changed or that paths also lists. A path in paths that git diff
base...head does not list is sent with a warning. Re-running the same handoff
is safe: items already written are kept.

In a linked git worktree without its own .modernpath folder, the command uses
the main checkout's binding and compares head with the worktree's HEAD.`

type buildHandoffResult struct {
	Format string `json:"format"`
	Path   string `json:"path"`
	Root   string `json:"root,omitempty"`
}

type buildHandoffRequirement struct {
	RequirementID string   `json:"requirement_id"`
	RepositoryKey string   `json:"repository_key,omitempty"`
	Paths         []string `json:"paths"`
	// UnchangedPaths are files the requirement uses that the build did not
	// change (Epic 428).
	UnchangedPaths []string `json:"unchanged_paths"`
}

// buildHandoffExcluded is a plan criterion the build left partly built.
type buildHandoffExcluded struct {
	CriterionID string `json:"criterion_id"`
	Reason      string `json:"reason"`
}

// buildHandoffRegression is a TestCase the build reran for a criterion of a
// published requirement outside the plan.
type buildHandoffRegression struct {
	CriterionID string `json:"criterion_id"`
	TestCaseID  string `json:"test_case_id"`
}

type buildHandoffDefault struct {
	Title       string `json:"title"`
	Observation string `json:"observation"`
	NotesMD     string `json:"notes_md,omitempty"`
}

type buildHandoff struct {
	EpicID          int64                     `json:"epic_id"`
	Head            string                    `json:"head"`
	Base            string                    `json:"base"`
	RepositoryKey   string                    `json:"repository_key,omitempty"`
	Results         []buildHandoffResult      `json:"results"`
	Requirements    []buildHandoffRequirement `json:"requirements"`
	Tests           []handoffTestEntry        `json:"tests"`
	AssumedDefaults []buildHandoffDefault     `json:"assumed_defaults"`
	Excluded        []buildHandoffExcluded    `json:"excluded"`
	Regression      []buildHandoffRegression  `json:"regression"`
}

func readBuildHandoff(cmd *cobra.Command, file string) (*buildHandoff, error) {
	var input io.Reader
	if file == "-" {
		input = cmd.InOrStdin()
	} else {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read build handoff: %w", err)
		}
		input = bytes.NewReader(raw)
	}
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	var handoff buildHandoff
	if err := decoder.Decode(&handoff); err != nil {
		return nil, fmt.Errorf("build handoff %s: %v; nothing was sent", file, err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, fmt.Errorf("build handoff %s must hold exactly one JSON object; nothing was sent", file)
	}
	return &handoff, nil
}

// repoPath is p as a clean repository-relative path.
func repoPath(p string) string {
	return strings.TrimPrefix(path.Clean(filepath.ToSlash(strings.TrimSpace(p))), "./")
}

type buildLocal struct {
	NotRecorded        []map[string]any `json:"not_recorded"`
	IgnoredResults     []map[string]any `json:"ignored_results"`
	PackagesNotRecord  []packageFailure `json:"packages_not_recorded"`
	PackagesFailedPass []packageFailure `json:"packages_failed_after_passing"`
	PathsNotInDiff     []string         `json:"paths_not_in_diff"`
	// Excluded lists the obligations the build handoff excludes;
	// ExcludedUntraced the TestCases whose obligations are all excluded and
	// for which no single test file was found; RegressionUnchecked the
	// regression TestCases with a result and no file to check (Epic 428).
	Excluded            []map[string]any `json:"excluded"`
	ExcludedUntraced    []string         `json:"excluded_untraced"`
	RegressionUnchecked []string         `json:"regression_unchecked"`
}

// dirtyFiles are the given repository paths that git status reports changed.
func dirtyFiles(root string, files []string) ([]string, error) {
	if len(files) == 0 {
		return nil, nil
	}
	wanted := map[string]bool{}
	for _, f := range files {
		wanted[f] = true
	}
	args := append([]string{"-C", root, "status", "--porcelain", "-z", "--"}, files...)
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git status failed: %v", err)
	}
	var dirty []string
	records := strings.Split(string(out), "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if len(record) > 3 && wanted[record[3:]] {
			dirty = append(dirty, record[3:])
		}
		// A rename or copy is followed by its original path, which has no
		// status prefix: skip it rather than cut three bytes from it.
		if len(record) > 1 && (record[0] == 'R' || record[0] == 'C' || record[1] == 'R' || record[1] == 'C') {
			i++
		}
	}
	return dirty, nil
}

func runEpicBuildRecord(cmd *cobra.Command, load func() (*cliEnv, error), address string) error {
	id, err := strconv.ParseInt(address, 10, 64)
	if err != nil || id <= 0 {
		return fmt.Errorf("Epic ID must be a positive integer")
	}
	file, _ := cmd.Flags().GetString("file")
	if file == "" {
		return fmt.Errorf("--file is required: the build handoff JSON file, or - for standard input")
	}
	handoff, err := readBuildHandoff(cmd, file)
	if err != nil {
		return err
	}
	if handoff.EpicID != id {
		return fmt.Errorf("build handoff epic_id %d is not the command's Epic %d; nothing was sent", handoff.EpicID, id)
	}
	root := gitOut(".", "rev-parse", "--show-toplevel")
	if root == "" {
		return fmt.Errorf("epics build record runs inside the git checkout the build changed")
	}
	head := strings.ToLower(gitOut(root, "rev-parse", "HEAD"))
	if !fullGitSHA.MatchString(handoff.Head) {
		return fmt.Errorf("build handoff head %q must be a full commit SHA; nothing was sent", handoff.Head)
	}
	if strings.ToLower(handoff.Head) != head {
		return fmt.Errorf("build handoff head %s is not the checkout HEAD %s; commit the build and update head; nothing was sent", handoff.Head, head)
	}
	if strings.TrimSpace(handoff.Base) == "" {
		return fmt.Errorf("build handoff base is required: the commit the build started from; nothing was sent")
	}
	if gitOut(root, "rev-parse", "--verify", "--quiet", handoff.Base+"^{commit}") == "" {
		return fmt.Errorf("build handoff base %s is not a commit of this checkout; nothing was sent", handoff.Base)
	}

	local := buildLocal{NotRecorded: []map[string]any{}, IgnoredResults: []map[string]any{}, PackagesNotRecord: []packageFailure{}, PackagesFailedPass: []packageFailure{}, PathsNotInDiff: []string{},
		Excluded: []map[string]any{}, ExcludedUntraced: []string{}, RegressionUnchecked: []string{}}
	var results []testResult
	for _, entry := range handoff.Results {
		switch entry.Format {
		case "trx", "junit", "go-test-json":
		default:
			return fmt.Errorf("build handoff result %s has format %q; use trx, junit or go-test-json; nothing was sent", entry.Path, entry.Format)
		}
		location := entry.Path
		if !filepath.IsAbs(location) {
			location = filepath.Join(root, filepath.FromSlash(location))
		}
		if info, err := os.Stat(location); err != nil || info.IsDir() {
			return fmt.Errorf("result file %s does not exist; nothing was sent", entry.Path)
		}
		entryRoot := ""
		if strings.TrimSpace(entry.Root) != "" {
			entryRoot = repoPath(entry.Root)
		}
		parsed, err := parseTestResults(entry.Format, location, entryRoot)
		if err != nil {
			return fmt.Errorf("result file %s cannot be read as %s: %v; nothing was sent", entry.Path, entry.Format, err)
		}
		for i := range parsed.Results {
			parsed.Results[i].File = entry.Path
		}
		results = append(results, parsed.Results...)
		local.PackagesNotRecord = append(local.PackagesNotRecord, parsed.NotRecorded...)
		local.PackagesFailedPass = append(local.PackagesFailedPass, parsed.FailedAfterPassing...)
	}

	env, err := load()
	if err != nil {
		return err
	}
	status, response, err := env.get(epicScopedPath(env, id, "/plan"))
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return serverRefusal("epic plan", status, response)
	}
	data, _ := response["data"].(map[string]any)
	plan, _ := data["plan"].(map[string]any)
	if plan == nil {
		return fmt.Errorf("Epic %d has no saved plan; nothing was sent", id)
	}
	requirementKinds := map[string]string{}
	planCriteria := map[string]bool{}
	for _, raw := range anySlice(plan["requirements"]) {
		if r, ok := raw.(map[string]any); ok {
			requirementKinds[strings.ToLower(str(r, "id"))] = str(r, "kind")
			for _, rawCriterion := range anySlice(r["criteria"]) {
				if c, ok := rawCriterion.(map[string]any); ok && str(c, "id") != "" {
					planCriteria[strings.ToLower(str(c, "id"))] = true
				}
			}
		}
	}
	type obligation struct{ criterion, testCase string }
	var obligations []obligation
	planTestCases := map[string]bool{}
	var testCaseOrder []string
	verification, _ := plan["verification"].(map[string]any)
	for _, raw := range anySlice(verification["criteria"]) {
		c, ok := raw.(map[string]any)
		if !ok || str(c, "test_case_id") == "" {
			continue
		}
		tc := strings.ToLower(str(c, "test_case_id"))
		obligations = append(obligations, obligation{criterion: str(c, "id"), testCase: tc})
		if !planTestCases[tc] {
			planTestCases[tc] = true
			testCaseOrder = append(testCaseOrder, tc)
		}
		planCriteria[strings.ToLower(str(c, "id"))] = true
	}

	// Excluded entries: plan criteria the build left partly built. Each
	// names a verification obligation's criterion; its obligations are not
	// sent as results.
	if len(handoff.Excluded) > maxBuildRecordExcluded {
		return fmt.Errorf("the build handoff has %d excluded entries; one build record takes %d; nothing was sent", len(handoff.Excluded), maxBuildRecordExcluded)
	}
	excludedReason := map[string]string{}
	excludedItems := []map[string]any{}
	for i, entry := range handoff.Excluded {
		criterion := strings.ToLower(strings.TrimSpace(entry.CriterionID))
		testCase := ""
		for _, o := range obligations {
			if strings.ToLower(o.criterion) == criterion {
				testCase = o.testCase
				break
			}
		}
		switch {
		case testCase == "":
			return fmt.Errorf("excluded entry %d: criterion %s is not a verification obligation of the Epic's plan; nothing was sent", i+1, entry.CriterionID)
		case strings.TrimSpace(entry.Reason) == "":
			return fmt.Errorf("excluded entry %d: criterion %s needs a reason; nothing was sent", i+1, entry.CriterionID)
		}
		if _, twice := excludedReason[criterion]; twice {
			return fmt.Errorf("excluded entry %d: criterion %s is listed twice; nothing was sent", i+1, entry.CriterionID)
		}
		excludedReason[criterion] = strings.TrimSpace(entry.Reason)
		excludedItems = append(excludedItems, map[string]any{"criterion_id": criterion, "test_case_id": testCase, "reason": strings.TrimSpace(entry.Reason)})
	}
	// allExcluded: every obligation of the TestCase is excluded.
	allExcluded := map[string]bool{}
	for _, tc := range testCaseOrder {
		allExcluded[tc] = true
	}
	for _, o := range obligations {
		if _, excluded := excludedReason[strings.ToLower(o.criterion)]; !excluded {
			allExcluded[o.testCase] = false
		}
	}

	// Regression entries: TestCases the build reran for criteria of published
	// requirements outside the plan. Their results are matched like plan
	// TestCases and sent apart; no trace is sent for them.
	type regressionEntry struct{ criterion, testCase string }
	var regression []regressionEntry
	regressionTestCases := map[string]bool{}
	var regressionOrder []string
	regressionCriteria := map[string]bool{}
	seenRegression := map[string]bool{}
	for i, entry := range handoff.Regression {
		criterion := strings.ToLower(strings.TrimSpace(entry.CriterionID))
		testCase := strings.ToLower(strings.TrimSpace(entry.TestCaseID))
		switch {
		case len(fullUUIDs(criterion)) != 1 || fullUUIDs(criterion)[0] != criterion:
			return fmt.Errorf("regression entry %d: criterion_id %q must be a criterion UUID; nothing was sent", i+1, entry.CriterionID)
		case len(fullUUIDs(testCase)) != 1 || fullUUIDs(testCase)[0] != testCase:
			return fmt.Errorf("regression entry %d: test_case_id %q must be a TestCase UUID; nothing was sent", i+1, entry.TestCaseID)
		case planCriteria[criterion]:
			return fmt.Errorf("regression entry %d: criterion %s is listed under a requirement of the plan; its result belongs to the plan's results; nothing was sent", i+1, entry.CriterionID)
		case seenRegression[criterion+" "+testCase]:
			return fmt.Errorf("regression entry %d: criterion %s with TestCase %s is listed twice; nothing was sent", i+1, entry.CriterionID, entry.TestCaseID)
		}
		seenRegression[criterion+" "+testCase] = true
		regressionCriteria[criterion] = true
		regression = append(regression, regressionEntry{criterion: criterion, testCase: testCase})
		if !regressionTestCases[testCase] {
			regressionTestCases[testCase] = true
			regressionOrder = append(regressionOrder, testCase)
		}
	}
	if len(regressionCriteria) > maxBuildRecordCriteria {
		return fmt.Errorf("the build handoff has %d regression criteria; one build record takes %d; nothing was sent", len(regressionCriteria), maxBuildRecordCriteria)
	}

	repo, err := newHeadRepo(root, head)
	if err != nil {
		return err
	}
	scanned := append([]string{}, testCaseOrder...)
	for _, tc := range regressionOrder {
		if !planTestCases[tc] {
			scanned = append(scanned, tc)
		}
	}
	carriers, declarations, err := repo.scanTestCaseLines(scanned)
	if err != nil {
		return err
	}
	match, err := matchResults(repo, results, declarations, handoff.Tests)
	if err != nil {
		return err
	}
	outcomes := map[string]string{}
	for i, result := range results {
		ids := match.testCases[i]
		if len(ids) == 0 {
			local.IgnoredResults = append(local.IgnoredResults, map[string]any{"file": result.File, "name": result.Name, "reason": "no TestCase"})
			continue
		}
		for _, tc := range ids {
			if !planTestCases[tc] && !regressionTestCases[tc] {
				local.IgnoredResults = append(local.IgnoredResults, map[string]any{"file": result.File, "name": result.Name, "test_case_id": tc, "reason": "TestCase not in the plan"})
				continue
			}
			outcomes[tc] = worseOutcome(outcomes[tc], result.Outcome)
		}
	}

	endpoint := func(entryKey, p string) map[string]any {
		trace := map[string]any{"target_type": "source_file", "path": p, "revision": head}
		switch {
		case entryKey != "":
			trace["repository_key"] = entryKey
		case handoff.RepositoryKey != "":
			trace["repository_key"] = handoff.RepositoryKey
		case env.Config != nil && env.Config.RepositoryID != 0:
			trace["repository_id"] = env.Config.RepositoryID
		}
		return trace
	}
	traces := []map[string]any{}
	traced := map[string]bool{}
	var tracedOrder []string
	addTraced := func(p string) {
		if !traced[p] {
			traced[p] = true
			tracedOrder = append(tracedOrder, p)
		}
	}
	diff := map[string]bool{}
	for _, name := range strings.Split(gitOut(root, "diff", "--name-only", handoff.Base+"..."+head), "\n") {
		if name != "" {
			diff[name] = true
		}
	}
	for _, entry := range handoff.Requirements {
		kind, ok := requirementKinds[strings.ToLower(entry.RequirementID)]
		if !ok {
			return fmt.Errorf("build handoff requirement %s is not in the Epic's plan; nothing was sent", entry.RequirementID)
		}
		for _, raw := range entry.Paths {
			p := repoPath(raw)
			if !repo.tracked[p] {
				return fmt.Errorf("path %s of requirement %s is not a tracked file at HEAD %s; nothing was sent", raw, entry.RequirementID, head)
			}
			trace := endpoint(entry.RepositoryKey, p)
			trace["source_type"], trace["source_id"], trace["link_kind"] = kind, entry.RequirementID, "implements"
			traces = append(traces, trace)
			if !diff[p] && !contains(local.PathsNotInDiff, p) {
				local.PathsNotInDiff = append(local.PathsNotInDiff, p)
			}
			addTraced(p)
		}
		for _, raw := range entry.UnchangedPaths {
			p := repoPath(raw)
			switch {
			case !repo.tracked[p]:
				return fmt.Errorf("unchanged path %s of requirement %s is not a tracked file at HEAD %s; nothing was sent", raw, entry.RequirementID, head)
			case diff[p]:
				return fmt.Errorf("unchanged path %s of requirement %s is in git diff base...head: the build changed it; list it under paths; nothing was sent", raw, entry.RequirementID)
			}
			for _, listed := range entry.Paths {
				if repoPath(listed) == p {
					return fmt.Errorf("unchanged path %s of requirement %s is also listed under paths; list it once; nothing was sent", raw, entry.RequirementID)
				}
			}
			trace := endpoint(entry.RepositoryKey, p)
			trace["source_type"], trace["source_id"], trace["link_kind"] = kind, entry.RequirementID, "implements"
			traces = append(traces, trace)
			addTraced(p)
		}
	}
	for _, tc := range testCaseOrder {
		if outcomes[tc] == "" {
			continue
		}
		type testFile struct{ key, path string }
		var files []testFile
		if declared := match.declaredFiles[tc]; len(declared) > 0 {
			for _, f := range declared {
				files = append(files, testFile{path: f})
			}
		} else {
			for _, entry := range handoff.Tests {
				if strings.ToLower(entry.TestCaseID) == tc && entry.File != "" {
					p := repoPath(entry.File)
					if !repo.tracked[p] {
						return fmt.Errorf("test file %s of TestCase %s is not a tracked file at HEAD %s; nothing was sent", entry.File, tc, head)
					}
					files = append(files, testFile{key: entry.RepositoryKey, path: p})
				}
			}
		}
		if len(files) == 0 && allExcluded[tc] && len(carriers[tc]) == 0 {
			// Nothing of this TestCase is sent as a result, so the server
			// needs no test file for it: no trace, and it is named. A UUID
			// in several files is still refused below, as for any TestCase.
			local.ExcludedUntraced = append(local.ExcludedUntraced, tc)
			continue
		}
		if len(files) == 0 {
			switch found := carriers[tc]; len(found) {
			case 0:
				return fmt.Errorf("TestCase %s has a result but no test file: no matched declaration, tests entry or file at HEAD carries its UUID; the server would refuse its outcome; nothing was sent", tc)
			case 1:
				files = append(files, testFile{path: found[0]})
			default:
				return fmt.Errorf("TestCase %s is in more than one file at HEAD (%s) and no matched declaration or tests entry chooses between them; nothing was sent", tc, strings.Join(found, ", "))
			}
		}
		for _, f := range files {
			trace := endpoint(f.key, f.path)
			trace["source_type"], trace["source_id"], trace["link_kind"] = "test_case", tc, "verifies"
			traces = append(traces, trace)
			addTraced(f.path)
		}
	}
	dirty, err := dirtyFiles(root, tracedOrder)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return fmt.Errorf("traced files have uncommitted changes: %s; commit them and update head; nothing was sent", strings.Join(dirty, ", "))
	}

	// A regression TestCase outside the plan gets no trace, but the file its
	// result came from is checked: a result from locally edited test code
	// must not become the current proof of a published criterion.
	var regressionFiles []string
	for _, tc := range regressionOrder {
		if planTestCases[tc] || outcomes[tc] == "" {
			continue
		}
		var files []string
		if declared := match.declaredFiles[tc]; len(declared) > 0 {
			files = declared
		} else {
			for _, entry := range handoff.Tests {
				if strings.ToLower(entry.TestCaseID) == tc && entry.File != "" {
					files = append(files, repoPath(entry.File))
				}
			}
			if len(files) == 0 {
				files = carriers[tc]
			}
		}
		if len(files) == 0 {
			local.RegressionUnchecked = append(local.RegressionUnchecked, tc)
		}
		for _, f := range files {
			if !contains(regressionFiles, f) {
				regressionFiles = append(regressionFiles, f)
			}
		}
	}
	dirty, err = dirtyFiles(root, regressionFiles)
	if err != nil {
		return err
	}
	if len(dirty) > 0 {
		return fmt.Errorf("regression test files have uncommitted changes: %s; commit them and update head, or drop the regression entry; nothing was sent", strings.Join(dirty, ", "))
	}

	sentRegression := []map[string]any{}
	regressionByCriterion := map[string]map[string]any{}
	for _, entry := range regression {
		outcome := outcomes[entry.testCase]
		if outcome == "" {
			local.NotRecorded = append(local.NotRecorded, map[string]any{"criterion_id": entry.criterion, "test_case_id": entry.testCase, "regression": true, "reason": "no test result matched its regression TestCase"})
			continue
		}
		group, ok := regressionByCriterion[entry.criterion]
		if !ok {
			group = map[string]any{"criterion_id": entry.criterion, "tests": []map[string]any{}}
			regressionByCriterion[entry.criterion] = group
			sentRegression = append(sentRegression, group)
		}
		group["tests"] = append(group["tests"].([]map[string]any), map[string]any{"test_case_id": entry.testCase, "result": outcome})
	}

	sentResults := []map[string]any{}
	byCriterion := map[string]map[string]any{}
	for _, o := range obligations {
		if reason, excluded := excludedReason[strings.ToLower(o.criterion)]; excluded {
			local.Excluded = append(local.Excluded, map[string]any{"criterion_id": o.criterion, "test_case_id": o.testCase, "reason": reason})
			continue
		}
		outcome := outcomes[o.testCase]
		if outcome == "" {
			local.NotRecorded = append(local.NotRecorded, map[string]any{"criterion_id": o.criterion, "test_case_id": o.testCase, "reason": "no test result matched its TestCase"})
			continue
		}
		group, ok := byCriterion[o.criterion]
		if !ok {
			group = map[string]any{"criterion_id": o.criterion, "tests": []map[string]any{}}
			byCriterion[o.criterion] = group
			sentResults = append(sentResults, group)
		}
		group["tests"] = append(group["tests"].([]map[string]any), map[string]any{"test_case_id": o.testCase, "result": outcome})
	}
	if len(sentResults) > maxBuildRecordCriteria {
		return fmt.Errorf("results for %d distinct criteria exceed the %d one build record takes; nothing was sent", len(sentResults), maxBuildRecordCriteria)
	}
	defaults := []buildHandoffDefault{}
	defaults = append(defaults, handoff.AssumedDefaults...)

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	body := map[string]any{"dry_run": dryRun, "traces": traces, "results": sentResults, "assumed_defaults": defaults}
	// Empty sections are left out, so a handoff without them still works
	// against a server that does not know them.
	if len(excludedItems) > 0 {
		body["excluded"] = excludedItems
	}
	if len(sentRegression) > 0 {
		body["regression_results"] = sentRegression
	}
	if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
		body["actor"] = map[string]any{"kind": "agent", "agent_slug": agent}
	}
	status, response, err = env.write("POST", epicScopedPath(env, id, "/build-record"), body)
	if err != nil {
		if callTimedOut(err) {
			return fmt.Errorf("epic build record: the call timed out (%v); the write may have committed. Re-running the same handoff is safe: items already written are kept", err)
		}
		return err
	}
	if status < 200 || status >= 300 {
		return buildRecordRefusal(status, response)
	}
	answer, ok := response["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("epic build record response is missing its data object")
	}
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		printed := map[string]any{}
		for k, v := range answer {
			printed[k] = v
		}
		printed["local"] = local
		return writeCommandJSON(cmd, printed)
	}
	writeBuildRecordText(cmd.OutOrStdout(), id, answer, local)
	return nil
}

func anySlice(v any) []any {
	items, _ := v.([]any)
	return items
}

func callTimedOut(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout() || errors.Is(err, context.DeadlineExceeded)
}

// buildRecordRefusal prints the server's code and message.
func buildRecordRefusal(status int, response map[string]any) error {
	text := refusalText(response)
	if problem, ok := response["error"].(map[string]any); ok {
		if code := str(problem, "code"); code != "" && !strings.Contains(text, code) {
			text = code + " — " + text
		}
	}
	return serverRefusalError{status: status, msg: withBindingHint(status, fmt.Sprintf("epic build record (server %d): %s", status, text))}
}

func writeBuildRecordText(out io.Writer, id int64, answer map[string]any, local buildLocal) {
	if answer["dry_run"] == true {
		fmt.Fprintf(out, "Dry run of Epic %d build record: the server checked the request and wrote nothing.\n", id)
	} else {
		fmt.Fprintf(out, "Epic %d build record:\n", id)
	}
	if traces := anySlice(answer["traces"]); len(traces) > 0 {
		fmt.Fprintln(out, "Traces:")
		for _, raw := range traces {
			t, _ := raw.(map[string]any)
			fmt.Fprintf(out, "  %s  %s %s %s -> %s\n", str(t, "status"), str(t, "link_kind"), str(t, "source_type"), str(t, "source_id"), str(t, "path"))
		}
	}
	if results := anySlice(answer["results"]); len(results) > 0 {
		fmt.Fprintln(out, "Results:")
		for _, raw := range results {
			r, _ := raw.(map[string]any)
			fmt.Fprintf(out, "  %s  criterion %s, TestCase %s: %s\n", str(r, "status"), str(r, "criterion_id"), str(r, "test_case_id"), str(r, "result"))
		}
	}
	if defaults := anySlice(answer["assumed_defaults"]); len(defaults) > 0 {
		fmt.Fprintln(out, "Assumed defaults:")
		for _, raw := range defaults {
			d, _ := raw.(map[string]any)
			fmt.Fprintf(out, "  %s  %s %s\n", str(d, "status"), orDash(str(d, "display_ref")), str(d, "title"))
		}
	}
	if results := anySlice(answer["regression_results"]); len(results) > 0 {
		fmt.Fprintln(out, "Regression results (criteria of published requirements outside the plan):")
		for _, raw := range results {
			r, _ := raw.(map[string]any)
			fmt.Fprintf(out, "  %s  criterion %s, TestCase %s: %s\n", str(r, "status"), str(r, "criterion_id"), str(r, "test_case_id"), str(r, "result"))
		}
	}
	if excluded := anySlice(answer["excluded"]); len(excluded) > 0 {
		fmt.Fprintln(out, "Excluded criteria (gap record; stored outcomes replaced with inconclusive):")
		for _, raw := range excluded {
			e, _ := raw.(map[string]any)
			fmt.Fprintf(out, "  criterion %s: gap %s %s, outcome %s\n", str(e, "criterion_id"), orDash(str(e, "display_ref")), str(e, "gap_status"), str(e, "outcome_status"))
		}
	}
	if len(local.Excluded) > 0 {
		fmt.Fprintln(out, "Obligations excluded:")
		for _, e := range local.Excluded {
			fmt.Fprintf(out, "  criterion %v, TestCase %v: %v\n", e["criterion_id"], e["test_case_id"], e["reason"])
		}
	}
	for _, tc := range local.ExcludedUntraced {
		fmt.Fprintf(out, "TestCase %s: every obligation is excluded and it has no test file to trace; no trace was sent\n", tc)
	}
	for _, tc := range local.RegressionUnchecked {
		fmt.Fprintf(out, "Regression TestCase %s: no file was found for it, so it was not checked for uncommitted changes\n", tc)
	}
	if len(local.NotRecorded) > 0 {
		fmt.Fprintln(out, "Obligations not recorded:")
		for _, n := range local.NotRecorded {
			fmt.Fprintf(out, "  criterion %v, TestCase %v: %v\n", n["criterion_id"], n["test_case_id"], n["reason"])
		}
	}
	unmatched := 0
	for _, r := range local.IgnoredResults {
		if r["test_case_id"] == nil {
			unmatched++
			continue
		}
		fmt.Fprintf(out, "Result ignored: %v (%v): TestCase %v is not in the plan\n", r["name"], r["file"], r["test_case_id"])
	}
	if unmatched > 0 {
		fmt.Fprintf(out, "Results ignored: %d results matched no TestCase (--json lists them)\n", unmatched)
	}
	for _, p := range local.PackagesNotRecord {
		fmt.Fprintf(out, "Go package not recorded: %s: %s\n", p.Package, p.Failure)
	}
	for _, p := range local.PackagesFailedPass {
		fmt.Fprintf(out, "Go package failed after its tests passed: %s: %s\n", p.Package, p.Failure)
	}
	for _, p := range local.PathsNotInDiff {
		fmt.Fprintf(out, "Warning: %s is not in git diff base...head; it was sent\n", p)
	}
}
