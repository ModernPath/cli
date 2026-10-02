package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
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
		for _, r := range input.Results {
			if r.Target == "" || r.TargetType != "requirement" || r.Clause == "" || r.Test == "" || (r.Role != "LOWER" && r.Role != "UPPER") || r.Revision != input.SHA || r.Fingerprint == "" || r.Detail.Assertion == "" || r.Detail.ProductionSubject == "" || (r.Result != "pass" && r.Result != "fail" && r.Result != "error" && r.Result != "skip") {
				return fmt.Errorf("every clause needs exact requirement, LOWER/UPPER role, execution identity, revision, result and semantic assertion/production subject")
			}
		}
		env, err := load()
		if err != nil {
			return err
		}
		return asBuiltRecordEvidence(cmd, env, map[string]any{"external_id": "ASBUILT-EXEC-" + reverseDigest([]byte(input.Key)), "kind": input.Kind, "sha": input.SHA, "ran_at": input.RanAt, "raw_evidence": input.RawEvidence, "results": input.Results})
	}
	return c
}

func asBuiltRecordEvidence(cmd *cobra.Command, env *factoryEnv, payload map[string]any) error {
	if env.SystemID <= 0 {
		return fmt.Errorf("connect this workspace to a system first")
	}
	payload["system_id"] = env.SystemID
	payload["runner"] = map[string]any{"kind": "agent", "agent_slug": "modernpath-cli"}
	status, body, err := env.call("POST", "/api/v1/sync/evidence", payload)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("as-built evidence refused", status, body)
	}
	data := dataOf(body)
	run, ok := data["run"].(map[string]any)
	if !ok || str(run, "id") == "" {
		return fmt.Errorf("evidence was recorded but response lacks its durable run id; read evidence before retrying")
	}
	if str(data, "report_digest") == "" {
		return fmt.Errorf("server did not return the retained report digest; update the server before collecting as-built proof")
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(body)
}

type asBuiltDeliveryInput struct {
	Key            string `json:"key"`
	RepositoryKey  string `json:"repository_key"`
	Root           string `json:"root"`
	TestedRevision string `json:"tested_revision"`
	SnapshotDigest string `json:"snapshot_digest"`
}

// asBuiltDeliveryScope is the file list a run's authorization recorded for one
// repository: what a delivery proof of a path-scoped run measures.
type asBuiltDeliveryScope struct {
	RunID          string
	SnapshotDigest string
	Files          []reverseFile
}

func asBuiltDeliveryCommand(load func() (*factoryEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "delivery-proof", Short: "Fetch and retain a clean tested snapshot at the remote default-branch tip", Args: cobra.NoArgs,
		Long: `Observe that the clean tested revision is the tip of the remote default branch
and retain the observation as evidence.

Without --run the snapshot is the whole repository. With --run the snapshot is
the files that run's authorization recorded for the repository: use it for a
run that was inventoried with --path, so the proof measures the same files
that were captured. The input's snapshot_digest must be the one that run captured.
The repository must still be clean as a whole.`}
	c.Flags().String("file", "", "key, repository_key, local root, tested_revision and snapshot_digest JSON (required)")
	c.Flags().String("run", "", "run whose authorized files are the snapshot, for a run inventoried with --path (optional; the whole repository when omitted)")
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
		return asBuiltRecordEvidence(cmd, env, map[string]any{"external_id": "ASBUILT-DELIVERY-" + reverseDigest([]byte(input.Key)), "kind": "manual", "sha": input.TestedRevision, "ran_at": report["observed_at"], "raw_evidence": string(raw)})
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
		return &asBuiltDeliveryScope{RunID: runID, SnapshotDigest: repository.SnapshotDigest, Files: repository.Files}, nil
	}
	return nil, fmt.Errorf("run %s does not authorize repository %q", runID, key)
}

// asBuiltObserve reads the repository's revision, clean state and snapshot
// digest: of the whole repository, or of the files a run authorized. The clean
// state is the whole repository's in both forms.
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
	files := make([]reverseFile, len(scope.Files))
	for i, file := range scope.Files {
		content, err := reverseRead(root, file.Path)
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
	}
	return report, nil
}

func passwordPresent(user *url.Userinfo) bool {
	_, hasPassword := user.Password()
	return hasPassword
}
