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
		if payload {
			command.Flags().String("file", "", "exact JSON intent file; - reads stdin (required)")
			_ = command.MarkFlagRequired("file")
		}
		command.RunE = func(cmd *cobra.Command, _ []string) error {
			var input map[string]any
			if payload {
				file, _ := cmd.Flags().GetString("file")
				if err := readReverseJSON(cmd, file, &input); err != nil {
					return err
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
	for _, command := range root.Commands() {
		switch command.Name() {
		case "status", "publish", "read-document", "coverage":
			command.Flags().String("run", "", "authorized run id (required)")
			_ = command.MarkFlagRequired("run")
		case "source-status":
			command.Flags().String("capture", "", "source capture id (required)")
			_ = command.MarkFlagRequired("capture")
		case "read-source":
			command.Flags().String("source", "", "immutable source_file id (required)")
			_ = command.MarkFlagRequired("source")
		}
		if command.Name() == "publish" {
			command.Flags().String("group", "", "stable coherent-group retry key (required)")
			_ = command.MarkFlagRequired("group")
		}
		if command.Name() == "read-document" {
			command.Flags().String("document", "", "authorized SystemDoc id (required)")
			_ = command.MarkFlagRequired("document")
		}
	}
	root.AddCommand(reverseInventoryCommand(), reverseCaptureCommand(load))
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
		authorization, ok := dataOf(body)["authorization"].(map[string]any)
		if !ok {
			return fmt.Errorf("run has no source authorization")
		}
		raw, err := json.Marshal(authorization["repositories"])
		if err != nil {
			return err
		}
		var repositories []reverseRepository
		if err := json.Unmarshal(raw, &repositories); err != nil {
			return fmt.Errorf("invalid authorized inventory: %w", err)
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

func reverseInventoryCommand() *cobra.Command {
	command := &cobra.Command{Use: "inventory", Short: "Inventory explicitly declared repositories, including non-Git roots and legacy source formats", Args: cobra.NoArgs,
		Long: `Read-only local inventory. Repeat --repository key=directory for every repository;
the parent workspace need not be Git. Git worktrees use their tracked/unignored
files. Non-Git roots are walked with explicit generated/secret exclusions.
All safe regular files are included, including XML, JSP, XSL and XSLT. Review
the returned exclusions and byte/file denominators before authorizing upload.
No file bytes, local absolute paths or credentials are sent to the server.`}
	command.Flags().StringArray("repository", nil, "explicit repository identity and directory, key=directory (repeatable)")
	_ = command.MarkFlagRequired("repository")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		roots, _ := cmd.Flags().GetStringArray("repository")
		inventory, err := buildReverseInventory(roots)
		if err != nil {
			return err
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(inventory)
	}
	return command
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
	if parts[2] == "reverse-engineering" && parts[3] == "runs" {
		if len(parts) == 4 {
			return "reverse_engineering.authorize"
		}
		if len(parts) == 6 && parts[5] == "sources" {
			return "reverse_engineering.capture"
		}
		if len(parts) == 7 && parts[5] == "groups" {
			return "reverse_engineering.publish"
		}
	}
	return ""
}
