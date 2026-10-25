package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementCreateCommand(cliEnvLoad))
}

func newRequirementCreateCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use: "create", Short: "Create a native requirement in the bound System", Args: cobra.NoArgs,
		Long: `Create a user or system requirement for new behavior. Supply a JSON object
with title and optional content fields using --file or --file - for stdin.
The server assigns its native UUID, display reference, revision 0 and PROPOSED
status. Use the returned kind and UUID in an Epic plan to propose acceptance
criteria and requirement changes. Example: {"title":"Rotate signing keys","description":"Keys can be rotated without interrupting requests"}.`,
	}
	command.Flags().String("kind", "", "requirement kind: user or system (required)")
	command.Flags().String("file", "", "requirement content JSON file, or - for stdin (required)")
	command.Flags().Bool("json", false, "emit the creation receipt as JSON")
	_ = command.MarkFlagRequired("kind")
	_ = command.MarkFlagRequired("file")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		kind, _ := cmd.Flags().GetString("kind")
		if kind != "user" && kind != "system" {
			return fmt.Errorf("--kind must be user or system")
		}
		path, _ := cmd.Flags().GetString("file")
		payload, err := readObjectInput(cmd, path)
		if err != nil {
			return err
		}
		if err := validateRequirementCreate(payload, kind); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("requirement creation requires a bound System")
		}
		payload["system_id"] = env.SystemID
		status, body, err := env.write("POST", "/api/compliance/"+kind+"-requirements", payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement creation refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement creation response is missing its data object")
		}
		if sid, ok := numberAsInt(data["system_id"]); !ok || sid != int64(env.SystemID) {
			return fmt.Errorf("created requirement belongs to a different System or has no System identity")
		}
		id, _ := data["id"].(string)
		ref, _ := data["display_ref"].(string)
		title, _ := data["title"].(string)
		revision, validRevision := numberAsInt(data["revision"])
		if !requirementUUIDPattern.MatchString(id) || ref == "" || strings.TrimSpace(title) == "" || data["requirement_kind"] != kind+"_requirement" ||
			data["work_status"] != "PROPOSED" || !validRevision || revision != 0 {
			return fmt.Errorf("requirement creation response is missing its native identity or initial state")
		}
		return writeRecord(cmd, map[string]any{
			"id": id, "display_ref": ref, "requirement_kind": data["requirement_kind"],
			"title": data["title"], "system_id": data["system_id"], "work_status": data["work_status"], "revision": data["revision"],
		})
	}
	return command
}

func validateRequirementCreate(payload map[string]any, kind string) error {
	return validateRequirementContent(payload, kind, true)
}

func validateRequirementContent(payload map[string]any, kind string, requireTitle bool) error {
	if len(payload) == 0 && !requireTitle {
		return fmt.Errorf("requirement content must contain at least one editable field")
	}
	allowed := map[string]bool{}
	for _, field := range strings.Fields("title description context context_name release_note detail_md") {
		allowed[field] = true
	}
	fields := "actor intended_use"
	if kind == "system" {
		fields = "boundary rationale verification_method"
	}
	for _, field := range strings.Fields(fields) {
		allowed[field] = true
	}
	for field, value := range payload {
		if !allowed[field] {
			return fmt.Errorf("unsupported %s requirement field %q", kind, field)
		}
		if value != nil {
			if _, ok := value.(string); !ok {
				return fmt.Errorf("requirement field %q must be a string or null", field)
			}
		}
	}
	title, hasTitle := payload["title"]
	titleText, _ := title.(string)
	if (requireTitle || hasTitle) && strings.TrimSpace(titleText) == "" {
		return fmt.Errorf("requirement title is required")
	}
	return nil
}

func validateRequirementGrouping(payload map[string]any) error {
	context, hasContext := payload["context"]
	name, hasName := payload["context_name"]
	if !hasContext && !hasName {
		return nil
	}
	contextText, ok := context.(string)
	if !ok || strings.TrimSpace(contextText) == "" || len(contextText) > 255 {
		return fmt.Errorf("context must contain 1 to 255 bytes of nonblank text")
	}
	if !hasName {
		name = context
	}
	nameText, ok := name.(string)
	if !ok || strings.TrimSpace(nameText) == "" || len(nameText) > 255 {
		return fmt.Errorf("context_name must contain 1 to 255 bytes of nonblank text")
	}
	return nil
}
