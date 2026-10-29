package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newSourceFilesCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "source-files", Short: "Manage traced SourceFile identities"}
	relocate := &cobra.Command{
		Use:   "relocate <source-file-id>",
		Short: "Move a traced SourceFile path within its repository",
		Long: `Move a SourceFile to a new path in the same repository without changing its ID or Traces.

Pass a JSON file with expected_path, path and reason. expected_path protects
against moving a record that changed since it was read. If a SourceFile already
exists at the destination, include its destination_id to confirm that exact
identity. Both records and their Traces remain; this operation creates no alias
or graph rewrite. Path resolution for duplicate records is deterministic:
the oldest SourceFile at that path is selected. The server audits real moves;
an identical retry returns already_relocated.

The command uses the workspace's bound System. It cannot select another System
or tenant. Read the SourceFile ID from an Epic plan or a Trace read.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !requirementUUIDPattern.MatchString(args[0]) {
				return fmt.Errorf("source-file-id must be a UUID")
			}
			sourceID := strings.ToLower(args[0])
			file, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, file)
			if err != nil {
				return err
			}
			if err := validateSourceFileRelocation(payload); err != nil {
				return err
			}
			if destinationID, ok := payload["destination_id"].(string); ok {
				payload["destination_id"] = strings.ToLower(destinationID)
			}
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID < 1 {
				return fmt.Errorf("this workspace has no bound System")
			}
			payload["system_id"] = env.SystemID
			status, body, err := env.write("POST", "/api/compliance/source-files/"+url.PathEscape(sourceID)+"/relocate", payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("source file relocation refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok || data["id"] != sourceID || str(data, "repository_key") == "" || str(data, "path") != payload["path"] ||
				(str(data, "result") != "relocated" && str(data, "result") != "already_relocated") {
				return fmt.Errorf("source file relocation response is missing its identity or result")
			}
			receipt := map[string]any{
				"id": data["id"], "repository_key": data["repository_key"], "path": data["path"], "result": data["result"],
			}
			if destinationID, ok := payload["destination_id"]; ok {
				receipt["destination_id"] = destinationID
				receipt["warning"] = "Both SourceFile identities and their Traces remain at this path; path resolution selects the oldest record."
			}
			return writeRecord(cmd, receipt)
		},
	}
	relocate.Flags().String("file", "", "relocation JSON file (required); use - for stdin")
	_ = relocate.MarkFlagRequired("file")
	relocate.Flags().Bool("json", false, "Emit JSON receipt")
	root.AddCommand(relocate)
	return root
}

func validateSourceFileRelocation(payload map[string]any) error {
	for key := range payload {
		switch key {
		case "expected_path", "path", "reason", "destination_id":
		default:
			return fmt.Errorf("unsupported SourceFile relocation field %q", key)
		}
	}
	for _, key := range []string{"expected_path", "path", "reason"} {
		value, ok := payload[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s must be a nonempty string", key)
		}
	}
	if raw, exists := payload["destination_id"]; exists {
		id, ok := raw.(string)
		if !ok || !requirementUUIDPattern.MatchString(id) {
			return fmt.Errorf("destination_id must be a UUID")
		}
	}
	return nil
}
