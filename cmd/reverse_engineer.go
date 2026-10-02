package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newReverseEngineerCommand(factoryCredentialLoad)) }

// The agent runs the analysis and asks for the mode. This surface carries exact
// scoped intents, never selects a mode/release or upgrades a refused request.
func newReverseEngineerCommand(load func() (*factoryEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "reverse-engineer", Short: "Establish an authorized as-built baseline or review-only DERIVED proposals", Long: `Bind the workspace and sync documentation first. Inventory every declared repository,
read preflight, then ask the user to choose baseline or derived. Baseline publishes
to Base/PENDING_VERIFICATION, not compliance approval or DONE. DERIVED proposals
stay out of Ledger until an exact requirement-and-link decision is applied.

All remote calls use the authenticated workspace binding. JSON inputs preserve
nested citations, criteria and packets. Writes return durable receipts; retry the
same run/group key and identical input after interruption. A conflict requires a
fresh read and decision, not an automatic retry. No task-ledger import is used.`}
	add := func(name, description, method string, path func(*cobra.Command) (string, error), payload bool) {
		command := &cobra.Command{Use: name, Short: description, Args: cobra.NoArgs}
		if payload && name != "authorize" {
			command.Flags().String("file", "", "exact JSON intent file; - reads stdin (required)")
			_ = command.MarkFlagRequired("file")
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			var input map[string]any
			// assembled: the authorization was built from the CLI's own outputs,
			// so a server refusal can say what the operator does next.
			assembled := false
			if payload {
				if name == "authorize" {
					var err error
					if input, assembled, err = reverseAuthorizeInput(cmd); err != nil {
						return err
					}
				} else {
					file, _ := cmd.Flags().GetString("file")
					if err := readReverseJSON(cmd, file, &input); err != nil {
						return err
					}
				}
				if input == nil {
					return fmt.Errorf("intent must be a JSON object")
				}
				for _, key := range []string{"system_id", "tenant_id", "actor_id", "created_by_id", "run_id", "group_key", "release_id", "base_release_id", "process_revision"} {
					if _, exists := input[key]; exists {
						return fmt.Errorf("%s is owned by the workspace binding, command or server", key)
					}
				}
				if name == "authorize" && input["mode"] != "baseline" && input["mode"] != "derived" {
					return fmt.Errorf("explicit mode baseline or derived is required after the user's choice")
				}
				if err := validateAsBuiltIntent(name, input); err != nil {
					return err
				}
			}
			suffix, err := path(cmd)
			if err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			body, err := reverseCall(env, method, suffix, input)
			if err != nil {
				if assembled {
					return reverseAuthorizeGuidance(err)
				}
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(body)
		}
		root.AddCommand(command)
	}
	fixed := func(path string) func(*cobra.Command) (string, error) {
		return func(*cobra.Command) (string, error) { return path, nil }
	}
	add("preflight", "Read current system scope, corpus fingerprint and recommended mode (read-only)", "GET", fixed("/reverse-engineering/preflight"), false)
	add("authorize", "Record the explicit source-scoped baseline or derived authorization", "POST", fixed("/reverse-engineering/runs"), true)
	add("status", "Read a durable run and its group receipts", "GET", reverseRunPath, false)
	add("coverage", "Measure authorized inventory against current governed and candidate traces", "GET", func(cmd *cobra.Command) (string, error) {
		path, err := reverseRunPath(cmd)
		return path + "/coverage", err
	}, false)
	add("read-document", "Read the exact authorized SystemDoc snapshot, never the live document", "GET", func(cmd *cobra.Command) (string, error) {
		path, err := reverseRunPath(cmd)
		if err != nil {
			return "", err
		}
		id, _ := cmd.Flags().GetString("document")
		if id == "" {
			return "", fmt.Errorf("--document is required")
		}
		return path + "/documents/" + url.PathEscape(id), nil
	}, false)
	add("publish", "Atomically publish one coherent requirement group under its run", "PUT", func(cmd *cobra.Command) (string, error) {
		path, err := reverseRunPath(cmd)
		if err != nil {
			return "", err
		}
		group, _ := cmd.Flags().GetString("group")
		if group == "" {
			return "", fmt.Errorf("--group is required")
		}
		return path + "/groups/" + url.PathEscape(group), nil
	}, true)
	add("refresh-traces", "Refresh confirmed captured code/test links on existing pending baselines; preserve requirement content", "PUT", func(cmd *cobra.Command) (string, error) {
		path, err := reverseRunPath(cmd)
		if err != nil {
			return "", err
		}
		group, _ := cmd.Flags().GetString("group")
		if group == "" {
			return "", fmt.Errorf("--group is required")
		}
		return path + "/trace-refreshes/" + url.PathEscape(group), nil
	}, true)
	add("source-status", "Read source capture status and immutable file identities", "GET", func(cmd *cobra.Command) (string, error) {
		id, _ := cmd.Flags().GetString("capture")
		if id == "" {
			return "", fmt.Errorf("--capture is required")
		}
		return "/reverse-engineering/sources/" + url.PathEscape(id), nil
	}, false)
	add("read-source", "Read the exact captured source, base64 encoded; never falls back to latest", "GET", func(cmd *cobra.Command) (string, error) {
		id, _ := cmd.Flags().GetString("source")
		if id == "" {
			return "", fmt.Errorf("--source is required")
		}
		return "/reverse-engineering/source-files/" + url.PathEscape(id), nil
	}, false)
	add("candidates", "Read typed candidate requirements and proposed links", "GET", fixed("/candidate-sets"), false)
	add("preview", "Preview an exact typed candidate set without applying it", "POST", fixed("/candidate-sets/preview"), true)
	add("decide", "Apply the exact reviewed selection, fingerprint, USER source and retry key", "POST", fixed("/candidate-sets/decisions"), true)
	add("proof-preview", "Preview exact existing assertion, execution and integration proof without lifecycle writes", "POST", fixed("/reverse-engineering/proof-preview"), true)
	add("acceptance-open", "Open the single human decision for an eligible exact as-built proof packet", "POST", fixed("/reverse-engineering/acceptances"), true)
	add("acceptance-apply", "Atomically apply an approved exact as-built packet and retain its receipt", "POST", func(cmd *cobra.Command) (string, error) {
		path, err := reverseAcceptancePath(cmd)
		return path + "/apply", err
	}, true)
	add("acceptance-status", "Read the recorded versus applied answer, current proof and durable receipt", "GET", reverseAcceptancePath, false)
	for _, command := range root.Commands() {
		switch command.Name() {
		case "authorize":
			command.Long = reverseAuthorizeHelp
			command.Flags().String("inventory", "", "saved output of reverse-engineer inventory; - reads stdin")
			command.Flags().String("preflight", "", "saved output of reverse-engineer preflight; - reads stdin")
			command.Flags().String("mode", "", "baseline or derived, as the person chose")
			command.Flags().String("source", "", "who approved the run and what, starting with USER:")
			command.Flags().String("key", "", "a stable name for the run; the same key and content return the same run")
			command.Flags().String("documents", "", "all or none: attach the analysis documents the preflight lists, so requirements can cite them")
			command.Flags().String("file", "", "exact JSON intent file, instead of the other flags; - reads stdin")
		case "status", "publish", "refresh-traces", "read-document", "coverage":
			command.Flags().String("run", "", "authorized run id (required)")
			_ = command.MarkFlagRequired("run")
		case "acceptance-apply", "acceptance-status":
			command.Flags().String("gate", "", "dedicated acceptance gate id (required)")
			_ = command.MarkFlagRequired("gate")
		case "source-status":
			command.Flags().String("capture", "", "source capture id (required)")
			_ = command.MarkFlagRequired("capture")
		case "read-source":
			command.Flags().String("source", "", "immutable source_file id (required)")
			_ = command.MarkFlagRequired("source")
		}
		if command.Name() == "publish" || command.Name() == "refresh-traces" {
			command.Flags().String("group", "", "stable coherent-group retry key (required)")
			_ = command.MarkFlagRequired("group")
		}
		if command.Name() == "read-document" {
			command.Flags().String("document", "", "authorized SystemDoc id (required)")
			_ = command.MarkFlagRequired("document")
		}
	}
	root.AddCommand(reverseInventoryCommand(), reverseCaptureCommand(load), asBuiltExecutionCommand(load), asBuiltDeliveryCommand(load))
	applyGroupUnknownArgGuard(root)
	return root
}

func reverseRunPath(cmd *cobra.Command) (string, error) {
	id, _ := cmd.Flags().GetString("run")
	if id == "" {
		return "", fmt.Errorf("--run is required")
	}
	return "/reverse-engineering/runs/" + url.PathEscape(id), nil
}

func reverseCall(env *factoryEnv, method, suffix string, payload any) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system before reverse-engineering")
	}
	status, body, err := env.call(method, fmt.Sprintf("/api/v1/systems/%d%s", env.SystemID, suffix), payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("reverse-engineering refused", status, body)
	}
	if _, ok := body["data"].(map[string]any); !ok {
		return nil, fmt.Errorf("reverse-engineering response is missing its data object; no success can be confirmed")
	}
	return body, nil
}

func readReverseJSON(cmd *cobra.Command, path string, target any) error {
	var reader io.Reader = cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	contents, err := io.ReadAll(io.LimitReader(reader, 48_000_001))
	if err != nil {
		return err
	}
	if len(contents) > 48_000_000 {
		return fmt.Errorf("JSON intent exceeds 48 MB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON intent: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("intent must contain exactly one JSON value")
	}
	return nil
}

func reverseCaptureCommand(load func() (*factoryEnv, error)) *cobra.Command {
	command := &cobra.Command{Use: "capture-source", Short: "Capture exactly the authorized repository files; refuse changed bytes or symlinks", Args: cobra.NoArgs}
	command.Flags().String("run", "", "authorized run id (required)")
	command.Flags().String("repository", "", "repository key in the authorization (required)")
	command.Flags().String("root", "", "local repository directory (required; not sent to server)")
	for _, flag := range []string{"run", "repository", "root"} {
		_ = command.MarkFlagRequired(flag)
	}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		env, err := load()
		if err != nil {
			return err
		}
		path, err := reverseRunPath(cmd)
		if err != nil {
			return err
		}
		body, err := reverseCall(env, "GET", path, nil)
		if err != nil {
			return err
		}
		key, _ := cmd.Flags().GetString("repository")
		directory, _ := cmd.Flags().GetString("root")
		repositories, err := reverseAuthorizedRepositories(body)
		if err != nil {
			return err
		}
		for _, repository := range repositories {
			if repository.Key != key {
				continue
			}
			files, err := reverseSourceBundle(repository, directory)
			if err != nil {
				return err
			}
			result, err := reverseCall(env, "POST", path+"/sources", map[string]any{"repository_key": key, "files": files})
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return fmt.Errorf("repository %q is not in this run's authorization", key)
	}
	return command
}

// reverseAuthorizedRepositories reads the repositories a run's authorization
// recorded, from the run read.
func reverseAuthorizedRepositories(body map[string]any) ([]reverseRepository, error) {
	authorization, ok := dataOf(body)["authorization"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("run has no source authorization")
	}
	raw, err := json.Marshal(authorization["repositories"])
	if err != nil {
		return nil, err
	}
	var repositories []reverseRepository
	if err := json.Unmarshal(raw, &repositories); err != nil {
		return nil, fmt.Errorf("invalid authorized inventory: %w", err)
	}
	return repositories, nil
}

const reverseAuthorizeHelp = `Record what a person approved for one run: which source, which mode, on whose
word. The request holds file paths, hashes and sizes. No file content is uploaded
and nothing is published by this command.

Build the authorization from the CLI's own outputs, saved unchanged:

  modernpath reverse-engineer inventory --repository app=/abs/repo > inventory.json
  modernpath reverse-engineer preflight > preflight.json
  modernpath reverse-engineer authorize --inventory inventory.json --preflight preflight.json --mode baseline --source "USER:2026-10-02:Ann approved baseline, documents all" --key billing-baseline --documents all

--inventory is the source list the person reviewed. --preflight pins the state
of the requirements they saw. --mode and --documents are the person's choices,
--source says who approved them and --key names the run. None has a default;
--source and --key are at most 255 bytes. --documents all attaches every analysis
document the preflight lists, so the run's requirements can cite them;
--documents none attaches none, and requirements then cite code and tests only.

If the server refuses, nothing was recorded:
  stale_corpus                 the requirements changed; save preflight again
  document_not_authorized      a document changed; save preflight again
  document_snapshot_too_large  too much document text for one run; use --documents none

A file built another way can be given with --file instead of the flags above.
Its fields are key, mode, authorization_source, corpus_fingerprint, repositories, documents.
The two forms cannot be mixed.`

var reverseAuthorizeFlags = []string{"inventory", "preflight", "mode", "source", "key", "documents"}

// reverseAuthorizeInput returns the authorization to send and whether it was
// assembled from the saved inventory and preflight outputs. The file form is
// read as it always was.
func reverseAuthorizeInput(cmd *cobra.Command) (map[string]any, bool, error) {
	assembled := false
	for _, flag := range reverseAuthorizeFlags {
		assembled = assembled || cmd.Flags().Changed(flag)
	}
	if cmd.Flags().Changed("file") {
		if assembled {
			return nil, false, fmt.Errorf("give either --file or --inventory with its flags, not both")
		}
		var input map[string]any
		file, _ := cmd.Flags().GetString("file")
		err := readReverseJSON(cmd, file, &input)
		return input, false, err
	}
	if !assembled {
		return nil, false, fmt.Errorf("authorize needs --inventory with --preflight, --mode, --source, --key and --documents, or --file; see authorize --help")
	}
	values := map[string]string{}
	for _, flag := range reverseAuthorizeFlags {
		value, _ := cmd.Flags().GetString(flag)
		if strings.TrimSpace(value) == "" {
			return nil, false, fmt.Errorf("--%s is required when authorize is built from --inventory and --preflight", flag)
		}
		values[flag] = value
	}
	switch {
	case values["documents"] != "all" && values["documents"] != "none":
		return nil, false, fmt.Errorf("--documents must be all or none")
	case !strings.HasPrefix(values["source"], "USER:"):
		return nil, false, fmt.Errorf("--source must start with USER: and say who approved the run")
	case len(values["source"]) > 255:
		return nil, false, fmt.Errorf("--source must be at most 255 bytes")
	case len(values["key"]) > 255:
		return nil, false, fmt.Errorf("--key must be at most 255 bytes")
	case values["inventory"] == "-" && values["preflight"] == "-":
		return nil, false, fmt.Errorf("only one of --inventory and --preflight can read stdin")
	}
	var inventory reverseInventory
	if err := readReverseJSON(cmd, values["inventory"], &inventory); err != nil {
		return nil, false, fmt.Errorf("inventory file: %w; save the output of reverse-engineer inventory unchanged", err)
	}
	if len(inventory.Repositories) == 0 {
		return nil, false, fmt.Errorf("inventory file holds no repository; save the output of reverse-engineer inventory unchanged")
	}
	for _, repository := range inventory.Repositories {
		if len(repository.Files) == 0 || reverseSnapshot(repository.Files) != repository.SnapshotDigest {
			return nil, false, fmt.Errorf("inventory file: repository %s: snapshot digest does not match its file list; save the output of reverse-engineer inventory unchanged", repository.Key)
		}
	}
	var preflight map[string]any
	if err := readReverseJSON(cmd, values["preflight"], &preflight); err != nil {
		return nil, false, fmt.Errorf("preflight file: %w; save the output of reverse-engineer preflight unchanged", err)
	}
	data, _ := preflight["data"].(map[string]any)
	fingerprint, _ := data["corpus_fingerprint"].(string)
	if fingerprint == "" {
		return nil, false, fmt.Errorf("preflight file holds no corpus_fingerprint; save the output of reverse-engineer preflight unchanged")
	}
	documents := []any{}
	if values["documents"] == "all" {
		if truncated, _ := data["documents_truncated"].(bool); truncated {
			return nil, false, fmt.Errorf("the preflight's document list is truncated: the system has more analysis documents than one run can attach; authorize with --documents none, or give a chosen set of documents with --file")
		}
		if listed, ok := data["documents"].([]any); ok {
			documents = listed
		}
	}
	return map[string]any{
		"key":                  values["key"],
		"mode":                 values["mode"],
		"authorization_source": values["source"],
		"corpus_fingerprint":   fingerprint,
		"repositories":         inventory.Repositories,
		"documents":            documents,
	}, true, nil
}

// reverseAuthorizeGuidance adds the next step to the three refusals an
// assembled authorization can meet and its operator can fix. It is applied to
// authorize only: publish and refresh-traces return the same reasons in the
// middle of a run, where a new authorization would be the wrong advice.
func reverseAuthorizeGuidance(err error) error {
	next := ""
	switch text := err.Error(); {
	case strings.Contains(text, "document_snapshot_too_large"):
		next = "the analysis documents are too large to attach to one run; authorize again with --documents none, and the run's requirements will cite code and tests only"
	case strings.Contains(text, "document_not_authorized"):
		next = "an analysis document changed after preflight was saved; save preflight again and authorize again"
	case strings.Contains(text, "stale_corpus"):
		next = "the system's requirements changed after preflight was saved; save preflight again and authorize again"
	default:
		return err
	}
	return fmt.Errorf("%w — nothing was recorded; %s", err, next)
}

func reverseInventoryCommand() *cobra.Command {
	command := &cobra.Command{Use: "inventory", Short: "Inventory explicitly declared repositories, including non-Git roots and legacy source formats", Args: cobra.NoArgs,
		Long: `Read-only local inventory. Repeat --repository key=directory for every repository;
the parent workspace need not be Git. Git worktrees use their tracked/unignored
files. Non-Git roots are walked with explicit generated/secret exclusions.
All safe regular files are included, including XML, JSP, XSL and XSLT. Review
the returned exclusions and byte/file denominators before authorizing upload.
No file bytes, local absolute paths or credentials are sent to the server.

To baseline part of a large Git repository, repeat --path key=relative/path
for the directories or files to include. Name the tests with the code they
cover: one delivery proof covers one run. The inventory then holds only the
files under those paths. The repository keeps its commit and its clean or dirty
state, the size and file limits count the included files, and every file left
out is listed as an exclusion. A path is a literal name: no patterns, no
trailing slash. Non-Git roots cannot be scoped.

A tracked symbolic link in a Git repository is left out and listed as an
exclusion. It is never followed.`}
	command.Flags().StringArray("repository", nil, "explicit repository identity and directory, key=directory (repeatable)")
	command.Flags().StringArray("path", nil, "limit a Git repository to a path, key=relative/path (repeatable); the rest is listed as exclusions")
	_ = command.MarkFlagRequired("repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		roots, _ := cmd.Flags().GetStringArray("repository")
		named, _ := cmd.Flags().GetStringArray("path")
		scopes, err := parseReverseScopes(named)
		if err != nil {
			return err
		}
		inventory, err := buildScopedReverseInventory(roots, scopes)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(inventory)
	}
	return command
}

// parseReverseScopes reads --path key=relative/path values. A path is a
// literal name under the repository root.
func parseReverseScopes(values []string) (map[string][]string, error) {
	scopes := map[string][]string{}
	for _, value := range values {
		key, path, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" || path == "" {
			return nil, fmt.Errorf("--path must be key=relative/path: %q", value)
		}
		switch {
		case strings.HasPrefix(path, "/"):
			return nil, fmt.Errorf("path %q must be relative to the repository root", path)
		case strings.HasSuffix(path, "/"):
			return nil, fmt.Errorf("path %q ends with a slash; name the directory without it", path)
		case !reverseSafePath(path):
			return nil, fmt.Errorf("path %q is not a safe relative path", path)
		}
		scopes[key] = append(scopes[key], path)
	}
	return scopes, nil
}

func reverseWriteName(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	if len(parts) < 4 || parts[0] != "systems" {
		return ""
	}
	if parts[2] == "candidate-sets" {
		if len(parts) == 4 && parts[3] == "decisions" {
			return "candidate_set.decide"
		}
		if len(parts) == 4 && parts[3] == "preview" {
			return "candidate_set.preview"
		}
	}
	if parts[2] == "reverse-engineering" {
		if len(parts) == 4 && parts[3] == "proof-preview" {
			return "reverse_engineering.proof_preview"
		}
		if len(parts) == 4 && parts[3] == "acceptances" {
			return "reverse_engineering.acceptance_open"
		}
		if len(parts) == 6 && parts[3] == "acceptances" && parts[5] == "apply" {
			return "reverse_engineering.acceptance_apply"
		}
	}
	if parts[2] == "reverse-engineering" && parts[3] == "runs" {
		if len(parts) == 4 {
			return "reverse_engineering.authorize"
		}
		if len(parts) == 6 && parts[5] == "sources" {
			return "reverse_engineering.capture"
		}
		if len(parts) == 7 && parts[5] == "trace-refreshes" {
			return "reverse_engineering.refresh_traces"
		}
		if len(parts) == 7 && parts[5] == "groups" {
			return "reverse_engineering.publish"
		}
	}
	return ""
}
