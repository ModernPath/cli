package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type asBuiltClause struct {
	Clause            string `json:"clause"`
	Assertion         string `json:"assertion"`
	ProductionSubject string `json:"production_subject"`
	CodeSourceID      string `json:"code_source_id"`
	TestSourceID      string `json:"test_source_id"`
	RunID             string `json:"run_id"`
	ResultID          string `json:"result_id"`
	ReportDigest      string `json:"report_digest"`
}
type asBuiltRequirement struct {
	Kind               string          `json:"kind"`
	ExternalID         string          `json:"external_id"`
	ContentFingerprint string          `json:"content_fingerprint"`
	Clauses            []asBuiltClause `json:"clauses"`
}
type asBuiltDelivery struct {
	RepositoryKey string `json:"repository_key"`
	RunID         string `json:"run_id"`
	ReportDigest  string `json:"report_digest"`
}
type asBuiltProof struct {
	Version      int                  `json:"version"`
	Requirements []asBuiltRequirement `json:"requirements"`
	Delivery     []asBuiltDelivery    `json:"delivery"`
}
type asBuiltBrief struct {
	What           string `json:"what"`
	WhyNow         string `json:"why_now"`
	Changes        string `json:"changes_if_approved"`
	Risk           string `json:"risk_if_wrong"`
	Recommendation string `json:"recommendation"`
}
type asBuiltOpen struct {
	Key         string       `json:"key"`
	Proof       asBuiltProof `json:"proof"`
	ProofDigest string       `json:"proof_digest"`
	Brief       asBuiltBrief `json:"brief"`
}
type asBuiltApply struct {
	Key             string `json:"key"`
	ProofDigest     string `json:"proof_digest"`
	GateFingerprint string `json:"gate_fingerprint"`
}

func validateAsBuiltIntent(name string, input map[string]any) error {
	var target any
	switch name {
	case "proof-preview":
		target = &asBuiltProof{}
	case "acceptance-open":
		target = &asBuiltOpen{}
	case "acceptance-apply":
		target = &asBuiltApply{}
	default:
		return nil
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid exact as-built intent: %w", err)
	}
	return nil
}

func reverseAcceptancePath(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("gate")
	if id == "" {
		return "", fmt.Errorf("--gate is required")
	}
	return "/reverse-engineering/acceptances/" + url.PathEscape(id), nil
}

type asBuiltExecutionResult struct {
	Target      string `json:"target_external_id"`
	TargetType  string `json:"target_type"`
	Clause      string `json:"target_clause"`
	Test        string `json:"test_case_ref"`
	Role        string `json:"role"`
	Result      string `json:"result"`
	Fingerprint string `json:"content_fingerprint"`
	Revision    string `json:"revision"`
	Detail      struct {
		Assertion         string `json:"assertion"`
		ProductionSubject string `json:"production_subject"`
	} `json:"detail"`
}
type asBuiltExecution struct {
	Key         string                   `json:"key"`
	Kind        string                   `json:"kind"`
	SHA         string                   `json:"sha"`
	RanAt       string                   `json:"ran_at"`
	RawEvidence string                   `json:"raw_evidence"`
	Results     []asBuiltExecutionResult `json:"results"`
}

func asBuiltExecutionCommand(load func() (*factoryEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "execution-proof", Short: "Retain genuine named execution and per-clause assertion proof; does not execute tests", Args: cobra.NoArgs}
	c.Flags().String("file", "", "typed execution report JSON; - reads stdin (required)")
	_ = c.MarkFlagRequired("file")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		var input asBuiltExecution
		if err := readReverseJSON(cmd, file, &input); err != nil {
			return err
		}
		if input.Key == "" || input.SHA == "" || input.RanAt == "" || len(input.Results) == 0 || !json.Valid([]byte(input.RawEvidence)) {
			return fmt.Errorf("key, exact sha, ran_at, raw JSON execution report and per-clause results are required")
		}
		if input.Kind != "local_test" && input.Kind != "ci" && input.Kind != "browser_verification" && input.Kind != "compliance_test_run" {
			return fmt.Errorf("execution kind must be local_test, ci, browser_verification or compliance_test_run")
		}
		if _, err := time.Parse(time.RFC3339, input.RanAt); err != nil {
			return fmt.Errorf("invalid ran_at: %w", err)
		}
		if err := asBuiltCheckExecutionResults(&input); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		_, err = asBuiltRecordEvidence(cmd, env, map[string]any{"external_id": "ASBUILT-EXEC-" + reverseDigest([]byte(input.Key)), "kind": input.Kind, "sha": input.SHA, "ran_at": input.RanAt, "raw_evidence": input.RawEvidence, "results": input.Results}, nil)
		return err
	}
	return c
}

// The execution-proof vocabulary: the server's result words (lowercase) and
// the trace roles (uppercase). The CLI normalizes the case of both before the
// check, so a report written as PASS or lower is accepted and sent normalized.
var (
	asBuiltResultWords = []string{"pass", "fail", "error", "skip", "inconclusive"}
	asBuiltRoleWords   = []string{"LOWER", "UPPER"}
)

// asBuiltCheckExecutionResults normalizes and checks every per-clause result
// and the raw report's executed_tests, one refusal per finding, naming the
// position, the field, the value and what is expected (SR-RDD-ONBOARD-045).
func asBuiltCheckExecutionResults(input *asBuiltExecution) error {
	for i := range input.Results {
		r := &input.Results[i]
		// The refusal quotes the value as written; the normalized one is sent.
		result, role := r.Result, r.Role
		r.Result = strings.ToLower(result)
		r.Role = strings.ToUpper(role)
		for _, field := range []struct{ name, value string }{
			{"target_external_id", r.Target}, {"target_clause", r.Clause}, {"test_case_ref", r.Test},
			{"content_fingerprint", r.Fingerprint}, {"detail.assertion", r.Detail.Assertion}, {"detail.production_subject", r.Detail.ProductionSubject},
		} {
			if field.value == "" {
				return fmt.Errorf("results[%d].%s is required", i, field.name)
			}
		}
		if r.TargetType != "requirement" {
			return fmt.Errorf("results[%d].target_type %q: expected requirement", i, r.TargetType)
		}
		if !slices.Contains(asBuiltRoleWords, r.Role) {
			return fmt.Errorf("results[%d].role %q: expected %s", i, role, strings.Join(asBuiltRoleWords, "|"))
		}
		if !slices.Contains(asBuiltResultWords, r.Result) {
			return fmt.Errorf("results[%d].result %q: expected %s", i, result, strings.Join(asBuiltResultWords, "|"))
		}
		if r.Revision != input.SHA {
			return fmt.Errorf("results[%d].revision %q: expected the report's sha %s", i, r.Revision, input.SHA)
		}
	}
	// The raw report is sent as written; only its named results are checked,
	// because the server counts a named pass by the lowercase word.
	var report struct {
		ExecutedTests []struct {
			Result string `json:"result"`
		} `json:"executed_tests"`
	}
	if err := json.Unmarshal([]byte(input.RawEvidence), &report); err != nil {
		return nil
	}
	for i, executed := range report.ExecutedTests {
		if !slices.Contains(asBuiltResultWords, executed.Result) {
			return fmt.Errorf("raw_evidence.executed_tests[%d].result %q: expected %s", i, executed.Result, strings.Join(asBuiltResultWords, "|"))
		}
	}
	return nil
}

// asBuiltRecordEvidence posts the evidence and prints the server's receipt as
// one JSON value. With a report, the collected report is printed beside the
// receipt under "report", so the agent retains what the server retained
// without a second read (SR-RDD-ONBOARD-046); the receipt's data is returned.
func asBuiltRecordEvidence(cmd *cobra.Command, env *factoryEnv, payload, report map[string]any) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system first")
	}
	payload["system_id"] = env.SystemID
	payload["runner"] = map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"}
	status, body, err := env.call("POST", "/api/v1/sync/evidence", payload)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, serverRefusal("as-built evidence refused", status, body)
	}
	data := dataOf(body)
	run, ok := data["run"].(map[string]any)
	if !ok || str(run, "id") == "" {
		return nil, fmt.Errorf("evidence was recorded but response lacks its durable run id; read evidence before retrying")
	}
	if str(data, "report_digest") == "" {
		return nil, fmt.Errorf("server did not return the retained report digest; update the server before collecting as-built proof")
	}
	printed := body
	if report != nil {
		printed = map[string]any{"data": data, "report": report}
	}
	return data, json.NewEncoder(cmd.OutOrStdout()).Encode(printed)
}

// asBuiltDeliveryLine states a recorded delivery proof in one line from the
// report the server retained, opening with the server's result word so it
// never reads as the as-built acceptance, which is a later gate.
func asBuiltDeliveryLine(data, report map[string]any) string {
	parts := []string{fmt.Sprintf("delivery proof %s at %s", str(data, "result"), str(report, "integrated_revision"))}
	if captured := str(report, "captured_revision"); captured != "" {
		ancestry, _ := report["ancestry"].(map[string]any)
		ancestor := str(ancestry, "result")
		if detail := str(ancestry, "error"); detail != "" {
			ancestor += " (" + detail + ")"
		}
		parts = append(parts, "captured "+captured, "ancestor: "+ancestor)
	}
	if measured, ok := report["measured_files"].(int); ok {
		noun := "files"
		if measured == 1 {
			noun = "file"
		}
		parts = append(parts, fmt.Sprintf("%d %s measured", measured, noun))
	}
	return strings.Join(parts, " · ")
}

type asBuiltDeliveryInput struct {
	Key            string `json:"key"`
	RepositoryKey  string `json:"repository_key"`
	Root           string `json:"root"`
	TestedRevision string `json:"tested_revision"`
	SnapshotDigest string `json:"snapshot_digest"`
}

// asBuiltDeliveryScope is the file list a run's authorization recorded for one
// repository: what a delivery proof of a path-scoped run measures, and the
// revision those files were captured at.
type asBuiltDeliveryScope struct {
	RunID            string
	SnapshotDigest   string
	Files            []reverseFile
	CapturedRevision string
}

func asBuiltDeliveryCommand(load func() (*factoryEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "delivery-proof", Short: "Fetch and retain a clean tested snapshot at the remote default-branch tip", Args: cobra.NoArgs,
		Long: `Observe that the clean tested revision is the tip of the remote default branch
and retain the observation as evidence.

Without --run the snapshot is the whole repository. With --run the snapshot is
the files that run's authorization recorded for the repository: use it for a
run that was inventoried with --path, or whose capture holds files under
.claude, which a new inventory leaves out, so the proof measures the same
files that were captured. The input's snapshot_digest must be the one that run captured.
The repository must still be clean as a whole.

The tested revision must be the tip of the remote default branch. With --run
the tip may be past the commit the run captured, when the authorized files at
the tip are the captured ones. The report then names the captured revision and
whether it is an ancestor of the tip: true, false, or unknown with Git's error
text, for example in a clone that lacks the captured commit. This is shown,
not required: a squash merge never keeps the captured commit.`}
	c.Flags().String("file", "", "key, repository_key, local root, tested_revision and snapshot_digest JSON (required)")
	c.Flags().String("run", "", "run whose authorized files are the snapshot, for a run inventoried with --path or whose capture holds files under .claude (optional; the whole repository when omitted)")
	_ = c.MarkFlagRequired("file")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		var input asBuiltDeliveryInput
		if err := readReverseJSON(cmd, file, &input); err != nil {
			return err
		}
		if input.Key == "" || input.RepositoryKey == "" || input.Root == "" || input.TestedRevision == "" || input.SnapshotDigest == "" {
			return fmt.Errorf("all delivery observation fields are required")
		}
		var env *factoryEnv
		var scope *asBuiltDeliveryScope
		if runID, _ := cmd.Flags().GetString("run"); runID != "" {
			// The scope is read before any observation: a run that cannot be
			// read, or that does not authorize the repository, records nothing.
			var err error
			if env, err = load(); err != nil {
				return err
			}
			if scope, err = asBuiltRunScope(env, runID, input.RepositoryKey); err != nil {
				return err
			}
		}
		report, err := collectScopedAsBuiltDelivery(input, scope)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(report)
		if err != nil {
			return err
		}
		if env == nil {
			if env, err = load(); err != nil {
				return err
			}
		}
		data, err := asBuiltRecordEvidence(cmd, env, map[string]any{"external_id": "ASBUILT-DELIVERY-" + reverseDigest([]byte(input.Key)), "kind": "manual", "sha": input.TestedRevision, "ran_at": report["observed_at"], "raw_evidence": string(raw)}, report)
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.ErrOrStderr(), asBuiltDeliveryLine(data, report))
		return nil
	}
	return c
}

// asBuiltRunScope reads the files a run's authorization recorded for one
// repository key.
func asBuiltRunScope(env *factoryEnv, runID, key string) (*asBuiltDeliveryScope, error) {
	body, err := reverseCall(env, "GET", "/reverse-engineering/runs/"+url.PathEscape(runID), nil)
	if err != nil {
		return nil, fmt.Errorf("could not read run %s: %w", runID, err)
	}
	repositories, err := reverseAuthorizedRepositories(body)
	if err != nil {
		return nil, fmt.Errorf("could not read run %s: %w", runID, err)
	}
	for _, repository := range repositories {
		if repository.Key != key {
			continue
		}
		if len(repository.Files) == 0 {
			return nil, fmt.Errorf("run %s authorizes no files for repository %q", runID, key)
		}
		return &asBuiltDeliveryScope{RunID: runID, SnapshotDigest: repository.SnapshotDigest, Files: repository.Files, CapturedRevision: repository.Revision}, nil
	}
	return nil, fmt.Errorf("run %s does not authorize repository %q", runID, key)
}

// asBuiltObserve reads the repository's revision, clean state and snapshot
// digest: of the whole repository, or of the files a run authorized. The clean
// state is the whole repository's in both forms, and a clean checkout is
// hashed as committed at HEAD: an authorized file that HEAD does not hold
// refuses, even when an ignored copy on disk holds the captured bytes.
func asBuiltObserve(input asBuiltDeliveryInput, scope *asBuiltDeliveryScope) (reverseRepository, error) {
	if scope == nil {
		inventory, err := buildReverseInventory([]string{input.RepositoryKey + "=" + input.Root})
		if err != nil {
			return reverseRepository{}, err
		}
		return inventory.Repositories[0], nil
	}
	root, err := reverseRoot(input.Root)
	if err != nil {
		return reverseRepository{}, err
	}
	revision, dirty, err := reverseGitIdentity(root)
	if err != nil {
		return reverseRepository{}, err
	}
	// A clean checkout is measured as committed at HEAD, as its inventory is.
	committed := ""
	if !dirty {
		committed = revision
	}
	paths := make([]string, len(scope.Files))
	for i, file := range scope.Files {
		paths[i] = file.Path
	}
	reader, err := openCommittedReverseContent(root, committed, paths)
	if err != nil {
		return reverseRepository{}, err
	}
	defer reader.close()
	files := make([]reverseFile, len(scope.Files))
	for i, file := range scope.Files {
		content, err := reader.read(file.Path)
		if err != nil {
			return reverseRepository{}, fmt.Errorf("authorized file unavailable: %w", err)
		}
		files[i] = reverseFile{file.Path, reverseDigest(content), int64(len(content))}
	}
	return reverseRepository{Key: input.RepositoryKey, Revision: revision, Dirty: dirty, SnapshotDigest: reverseSnapshot(files)}, nil
}

func collectAsBuiltDelivery(input asBuiltDeliveryInput) (map[string]any, error) {
	return collectScopedAsBuiltDelivery(input, nil)
}

// collectScopedAsBuiltDelivery observes delivery over a run's authorized files
// when a scope is given, and over the whole repository otherwise.
func collectScopedAsBuiltDelivery(input asBuiltDeliveryInput, scope *asBuiltDeliveryScope) (map[string]any, error) {
	if strings.Contains(input.RepositoryKey, "=") {
		return nil, fmt.Errorf("repository key must not contain '='")
	}
	// The proof is of what the run captured: a digest of other bytes, even of
	// the current ones, would be retained under the run's name and never match.
	if scope != nil && scope.SnapshotDigest != input.SnapshotDigest {
		return nil, fmt.Errorf("snapshot digest does not match the snapshot run %s captured for repository %q", scope.RunID, input.RepositoryKey)
	}
	repo, err := asBuiltObserve(input, scope)
	if err != nil {
		return nil, err
	}
	if repo.Dirty {
		return nil, fmt.Errorf("repository is dirty; delivery proof requires a clean tested snapshot")
	}
	if repo.Revision != input.TestedRevision {
		return nil, fmt.Errorf("tested revision does not match the current repository revision")
	}
	if repo.SnapshotDigest != input.SnapshotDigest {
		// A run authorized before the inventory left .claude out may hold such
		// files; only the form that names the run measures exactly them.
		if scope == nil && reverseHasAgentExclusion(repo.Exclusions) {
			return nil, fmt.Errorf("snapshot digest does not match the current repository content; a new inventory leaves out files under .claude, so if the run's capture holds such files, run delivery-proof with --run <run id> to measure the files that run authorized")
		}
		return nil, fmt.Errorf("snapshot digest does not match the current repository content")
	}
	origin := gitOut(input.Root, "remote", "get-url", "origin")
	if origin == "" {
		return nil, fmt.Errorf("origin is required to observe delivery")
	}
	var remoteArgs [][]string
	if strings.Contains(origin, "://") {
		u, err := url.Parse(origin)
		if err != nil || u.RawQuery != "" || u.Fragment != "" || (u.User != nil && (u.Scheme != "ssh" || u.User.Username() == "" || passwordPresent(u.User))) {
			return nil, fmt.Errorf("origin must not contain HTTP credentials, passwords, query parameters or fragments")
		}
	}
	remoteArgs = append(remoteArgs, []string{"git", "ls-remote", "--symref", "origin", "HEAD"})
	remote := exec.Command(remoteArgs[0][0], remoteArgs[0][1:]...)
	remote.Dir = input.Root
	observed, err := remote.Output()
	if err != nil {
		return nil, fmt.Errorf("could not read the remote default branch: %w", err)
	}
	branch := ""
	for _, line := range strings.Split(string(observed), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "ref:" && fields[2] == "HEAD" && strings.HasPrefix(fields[1], "refs/heads/") {
			branch = strings.TrimPrefix(fields[1], "refs/heads/")
		}
	}
	if branch == "" {
		return nil, fmt.Errorf("remote did not advertise a default branch")
	}
	fetchArgs := []string{"git", "fetch", "--quiet", "origin", "+refs/heads/" + branch + ":refs/remotes/origin/" + branch}
	remoteArgs = append(remoteArgs, fetchArgs)
	fetch := exec.Command(fetchArgs[0], fetchArgs[1:]...)
	fetch.Dir = input.Root
	if err := fetch.Run(); err != nil {
		return nil, fmt.Errorf("could not fetch the remote default branch: %w", err)
	}
	revArgs := []string{"git", "rev-parse", "refs/remotes/origin/" + branch}
	remoteArgs = append(remoteArgs, revArgs)
	rev := exec.Command(revArgs[0], revArgs[1:]...)
	rev.Dir = input.Root
	tipBytes, err := rev.Output()
	if err != nil {
		return nil, fmt.Errorf("could not read fetched default-branch revision: %w", err)
	}
	tip := strings.TrimSpace(string(tipBytes))
	if tip != input.TestedRevision {
		return nil, fmt.Errorf("passing tested revision is not the fetched default-branch tip; repository integration proof is missing")
	}
	after, err := asBuiltObserve(input, scope)
	if err != nil {
		return nil, err
	}
	if after.Dirty || after.Revision != repo.Revision || after.SnapshotDigest != repo.SnapshotDigest {
		return nil, fmt.Errorf("repository changed during delivery observation")
	}
	commands := make([]string, len(remoteArgs))
	for i, args := range remoteArgs {
		commands[i] = strings.Join(args, " ")
	}
	report := map[string]any{"repository_key": input.RepositoryKey, "origin": origin, "default_branch": branch, "integrated_revision": tip, "tested_revision": repo.Revision, "snapshot_digest": repo.SnapshotDigest, "dirty": false, "commands": remoteArgs, "command": strings.Join(commands, "; "), "observed_at": time.Now().UTC().Format(time.RFC3339Nano)}
	// Only a scoped proof adds keys: the server replays a retained report by
	// comparing every key but observed_at, so the unscoped set must not grow.
	if scope != nil {
		report["authorization_run_id"] = scope.RunID
		report["measured_files"] = len(scope.Files)
		// At a tip past the captured commit the authorized files were proven
		// identical above; the report names the captured revision and whether
		// it is an ancestor of the tip. That is disclosed, never required: a
		// squash merge never keeps the captured commit.
		if scope.CapturedRevision != "" && scope.CapturedRevision != tip {
			report["captured_revision"] = scope.CapturedRevision
			report["ancestry"] = asBuiltAncestry(input.Root, scope.CapturedRevision, tip)
		}
	}
	return report, nil
}

var asBuiltCommitID = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// asBuiltAncestry states whether captured is an ancestor of tip, with its
// command: "true", "false", or "unknown" with the error text when Git cannot
// answer, as in a clone that lacks the captured commit.
func asBuiltAncestry(root, captured, tip string) map[string]any {
	args := []string{"git", "merge-base", "--is-ancestor", captured, tip}
	ancestry := map[string]any{"command": strings.Join(args, " ")}
	if !asBuiltCommitID.MatchString(captured) {
		ancestry["result"] = "unknown"
		ancestry["error"] = "the captured revision is not a commit id"
		return ancestry
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case err == nil:
		ancestry["result"] = "true"
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		ancestry["result"] = "false"
	default:
		ancestry["result"] = "unknown"
		ancestry["error"] = firstNonEmpty(strings.TrimSpace(stderr.String()), err.Error())
	}
	return ancestry
}

func reverseHasAgentExclusion(exclusions []reverseExclusion) bool {
	for _, exclusion := range exclusions {
		if exclusion.Reason == reverseAgentReason {
			return true
		}
	}
	return false
}

func passwordPresent(user *url.Userinfo) bool {
	_, hasPassword := user.Password()
	return hasPassword
}
