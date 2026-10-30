package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementUpdateCommand(cliEnvLoad))
}

func newRequirementUpdateCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use: "update <uuid|display-ref>", Short: "Update requirement content, grouping or verified completion", Args: cobra.ExactArgs(1),
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
Unchanged values write nothing. Published content changes need a follow-up Epic.
For verified completion, send work_status DONE. New completion requires a
USER: source and --reason; a refused update reports its blockers. The requirement must be confirmed, unowned, never-published
PROPOSED with at least one active criterion, active scoped implementation/test source
traces and complete current passing proof for every active criterion. Independently recorded passing pairs combine;
revision numbers and historical outcomes do not determine readiness. Changed content
must be saved before completion.
Completion publishes an immutable version and increments revision once. Retrying an
unowned DONE UUID returns unchanged without new approval or fabricated history.
UUIDs require --kind user or system; an exact display reference infers its kind.`,
	}
	command.Flags().String("kind", "", "requirement kind for UUIDs: user or system")
	command.Flags().String("file", "", "requirement content JSON file, or - for stdin (required)")
	command.Flags().String("reason", "", "explanation recorded in the Audit Trail (required for new completion)")
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
		completion := payload["work_status"] == "DONE"
		if err := validateRequirementUpdate(payload, kind, completion); err != nil {
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
		if completion {
			if err := validateCompletionReceipt(data, kind, args[0], env.SystemID, payload); err != nil {
				return err
			}
			return writeRecord(cmd, data)
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

// Completion metadata belongs only to updates; creation retains its content contract.
func validateRequirementUpdate(payload map[string]any, kind string, completion bool) error {
	if !completion {
		return validateRequirementContent(payload, kind, false)
	}
	content := make(map[string]any)
	for key, value := range payload {
		switch key {
		case "work_status":
		case "source":
			source, ok := value.(string)
			if !ok || !strings.HasPrefix(source, "USER:") || strings.TrimSpace(strings.TrimPrefix(source, "USER:")) == "" {
				return fmt.Errorf("source must name a USER decision")
			}
		default:
			content[key] = value
		}
	}
	if len(content) > 0 {
		return validateRequirementContent(content, kind, false)
	}
	return nil
}

func validateCompletionReceipt(data map[string]any, kind, requested string, systemID int, submitted map[string]any) error {
	invalid := fmt.Errorf("requirement update response does not confirm the requested completion identity and state")
	sid, _ := numberAsInt(data["system_id"])
	revision, validRevision := numberAsInt(data["revision"])
	ref := str(data, "display_ref")
	inferred, validRef := requirementKindFromDisplayRef(ref)
	parts := strings.Split(ref, "-")
	if !validRef || inferred != kind || len(parts) != 3 {
		return invalid
	}
	refRevision, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || refRevision != revision || !validRevision || revision < 0 || sid != int64(systemID) || data["requirement_kind"] != kind+"_requirement" || !requirementUUIDPattern.MatchString(str(data, "id")) || strings.TrimSpace(str(data, "title")) == "" {
		return invalid
	}
	result := str(data, "result")
	if requirementUUIDPattern.MatchString(requested) {
		if !strings.EqualFold(str(data, "id"), requested) {
			return invalid
		}
	} else {
		requestedParts := strings.Split(requested, "-")
		oldRevision, _ := strconv.ParseInt(requestedParts[2], 10, 64)
		expectedRevision := oldRevision
		if result == "completed" {
			expectedRevision++
		}
		if !strings.EqualFold(parts[0], requestedParts[0]) || parts[1] != requestedParts[1] || revision != expectedRevision {
			return invalid
		}
	}
	if str(data, "work_status") != "DONE" || (result != "completed" && result != "unchanged") {
		return invalid
	}
	if result == "completed" {
		source, decision := str(submitted, "source"), str(submitted, "reason")
		if !strings.HasPrefix(source, "USER:") || strings.TrimSpace(strings.TrimPrefix(source, "USER:")) == "" || strings.TrimSpace(decision) == "" || str(data, "reason") != strings.TrimSpace(decision) {
			return invalid
		}
		actor, _ := numberAsInt(data["approved_by_id"])
		_, dateErr := time.Parse(time.RFC3339Nano, str(data, "approved_at"))
		if revision != 1 || !requirementUUIDPattern.MatchString(str(data, "version_id")) || actor <= 0 || dateErr != nil || strings.TrimSpace(str(data, "reason")) == "" {
			return invalid
		}
	} else if data["version_id"] != nil && !requirementUUIDPattern.MatchString(str(data, "version_id")) {
		return invalid
	}
	return nil
}
