package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementUpdateCommand(cliEnvLoad))
}

func newRequirementUpdateCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use: "update <uuid|display-ref>", Short: "Edit requirement draft content or current grouping context", Args: cobra.ExactArgs(1),
		Long: `Update draft content from a JSON object using --file or --file - for stdin.
Accepts every content field supported by requirements create, including context;
omitted fields, criteria and traces are preserved. Content edits require unowned,
never-published PROPOSED, BLOCKED or DEFERRED requirements. Drafts in planning
Epics can be edited, but need a fresh plan review before implementation approval.
Context-only edits follow set-context rules: context is required, and without
context_name the requirement takes the name the System's list gives that context
(see requirements contexts), or the code for a new context; leave the name out
for a listed context. A code or name that conflicts with the list is refused.
Evidence and accepted versions stay unchanged, including on DONE requirements.
Active Epic-owned work must return to planning first.
On editable drafts, unchanged content fields alongside a grouping change also
preserve evidence; requests containing content fields still require a draft.
Any workspace member may edit. --reason optionally records why in the Audit Trail.
Unchanged values write nothing. This operation does not publish content or change
lifecycle or revision. Published content changes need a follow-up Epic.
UUIDs require --kind user or system; an exact display reference infers its kind.`,
	}
	command.Flags().String("kind", "", "requirement kind for UUIDs: user or system")
	command.Flags().String("file", "", "requirement content JSON file, or - for stdin (required)")
	command.Flags().String("reason", "", "optional explanation recorded in the Audit Trail")
	command.Flags().Bool("json", false, "emit the update receipt as JSON")
	_ = command.MarkFlagRequired("file")
	command.RunE = func(cmd *cobra.Command, args []string) error {
		kind, _ := cmd.Flags().GetString("kind")
		kind = strings.TrimSuffix(kind, "_requirement")
		id := strings.ToLower(args[0])
		isUUID := requirementUUIDPattern.MatchString(id)
		if isUUID {
			if kind != "user" && kind != "system" {
				return fmt.Errorf("UUID requirement updates require --kind user or system")
			}
		} else {
			inferred, valid := requirementKindFromDisplayRef(args[0])
			if !valid {
				return fmt.Errorf("requirement must be a UUID or exact UR-<number>-<revision>/SR-<number>-<revision> reference")
			}
			if kind != "" && kind != inferred {
				return fmt.Errorf("--kind conflicts with requirement reference %q", args[0])
			}
			kind = inferred
		}
		path, _ := cmd.Flags().GetString("file")
		payload, err := readObjectInput(cmd, path)
		if err != nil {
			return err
		}
		if err := validateRequirementContent(payload, kind, false); err != nil {
			return err
		}
		if err := validateRequirementGrouping(payload); err != nil {
			return err
		}
		groupingOnly := true
		for field := range payload {
			if field != "context" && field != "context_name" {
				groupingOnly = false
			}
		}
		reason, _ := cmd.Flags().GetString("reason")
		if cmd.Flags().Changed("reason") {
			if strings.TrimSpace(reason) == "" || len(reason) > 2000 {
				return fmt.Errorf("--reason must contain 1 to 2000 bytes of nonblank text")
			}
			payload["reason"] = strings.TrimSpace(reason)
		}
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("requirement update requires a bound System")
		}
		route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s", env.SystemID, kind, url.PathEscape(id))
		status, body, err := env.write("PATCH", route, payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement update refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement update response is missing its data object")
		}
		sid, _ := numberAsInt(data["system_id"])
		revision, validRevision := numberAsInt(data["revision"])
		state := str(data, "work_status")
		validIdentity := requirementUUIDPattern.MatchString(str(data, "id"))
		if isUUID {
			validIdentity = validIdentity && strings.EqualFold(str(data, "id"), id)
		} else {
			validIdentity = validIdentity && strings.EqualFold(str(data, "display_ref"), args[0])
		}
		validState := revision == 0 && (state == "PROPOSED" || state == "BLOCKED" || state == "DEFERRED")
		if groupingOnly {
			validState = revision >= 0 && (state == "PROPOSED" || state == "BLOCKED" || state == "DEFERRED" || state == "DONE" || state == "OBSOLETE")
			// Without context_name the server answers with the list's name; any
			// nonblank name confirms the request then.
			returned, _ := data["context_name"].(string)
			name, named := payload["context_name"]
			validIdentity = validIdentity && data["context"] == payload["context"] &&
				strings.TrimSpace(returned) != "" && (!named || returned == name)
		}
		if !validIdentity || sid != int64(env.SystemID) || data["requirement_kind"] != kind+"_requirement" ||
			str(data, "display_ref") == "" || strings.TrimSpace(str(data, "title")) == "" || !validRevision || !validState {
			return fmt.Errorf("requirement update response does not confirm the requested draft identity and state")
		}
		return writeRecord(cmd, data)
	}
	return command
}
