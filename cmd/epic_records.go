package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var epicPublicFields = []string{
	"id", "title", "description", "process_status", "outcome_source", "scope", "non_goals",
	"start_date", "target_date", "generated_from", "severity", "category", "evidence",
	"knowledge_config", "impact_assessment", "shared_context", "approved_at", "approval_basis",
	"target_architecture_id", "target_profile_id", "assignee_id", "approved_by_id", "release_id",
	"metadata", "inserted_at", "updated_at", "system", "target_profile", "assignee", "approved_by", "release",
	"summary", "scenarios", "audit_events", "user_requirements", "system_requirements", "traces", "relationships",
	// SR-3812: the backlog records routed to or closed by the Epic.
	"backlog",
	// Epic 325 (A2): the planning contexts of the Epic's Release and Initiative.
	"planning_contexts",
	// Epic 437 (SR-4921): the System's Active Engineering Constraints, each with
	// cited_by_review and accepted_since_approval for this Epic.
	"engineering_constraints",
	// Epic 327 (A4): the Initiative on the Roadmap that covers the Epic.
	"covered_by",
}

func init() {
	list := &cobra.Command{Use: "list", Short: "List Epics in the bound System", Args: cobra.NoArgs}
	list.Flags().String("status", "", "Filter by process status")
	list.Flags().String("query", "", "Literal case-insensitive match in title, description or scope")
	list.Flags().Int("limit", 20, "Page size (1–200)")
	list.Flags().String("order", "id", "Ascending order: id or title")
	list.Flags().String("cursor", "", "Continue from a previous page")
	list.Flags().Bool("json", false, "Emit JSON")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		if limit < 1 || limit > 200 {
			return fmt.Errorf("--limit must be between 1 and 200")
		}
		order, _ := cmd.Flags().GetString("order")
		if order != "id" && order != "title" {
			return fmt.Errorf("--order must be id or title")
		}
		env, err := cliEnvLoad()
		if err != nil {
			return err
		}
		p := url.Values{"system_id": {strconv.Itoa(env.SystemID)}}
		if status, _ := cmd.Flags().GetString("status"); status != "" {
			p.Set("status", status)
		}
		p.Set("limit", strconv.Itoa(limit))
		p.Set("order", order)
		if query, _ := cmd.Flags().GetString("query"); query != "" {
			p.Set("query", query)
		}
		if cursor, _ := cmd.Flags().GetString("cursor"); cursor != "" {
			p.Set("cursor", cursor)
		}
		data, err := epicGet(env, "/api/work/epics?"+p.Encode())
		if err != nil {
			return err
		}
		result, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("Epic page response must be an object")
		}
		items, ok := result["items"].([]any)
		if !ok {
			return fmt.Errorf("Epic page response must contain items")
		}
		selected := make([]any, 0, len(items))
		for _, raw := range items {
			row, ok := raw.(map[string]any)
			if !ok {
				return fmt.Errorf("Epic page item must be an object")
			}
			id, valid := numberAsInt(row["id"])
			if !valid || id <= 0 {
				return fmt.Errorf("Epic page item is missing its positive ID")
			}
			title, ok := row["title"].(string)
			if !ok {
				return fmt.Errorf("Epic page item is missing its title")
			}
			selected = append(selected, map[string]any{"id": id, "title": title})
		}
		cursorValue, exists := result["next_cursor"]
		if !exists {
			return fmt.Errorf("Epic page response is missing next_cursor")
		}
		cursor, ok := cursorValue.(string)
		if cursorValue == nil {
			cursor, ok = "", true
		}
		if !ok {
			return fmt.Errorf("Epic page next_cursor must be a string or null")
		}
		if machine, _ := cmd.Flags().GetBool("json"); machine {
			return writeCommandJSON(cmd, map[string]any{"items": selected, "next_cursor": nullableCursor(cursor)})
		}
		for _, raw := range selected {
			row := raw.(map[string]any)
			title := strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(row["title"].(string))
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%d  %s\n", row["id"], title); err != nil {
				return err
			}
		}
		if cursor != "" {
			_, err := fmt.Fprintf(cmd.ErrOrStderr(), "next_cursor: %s\n", cursor)
			return err
		}
		return nil
	}
	get := &cobra.Command{Use: "get <epic-id>", Short: "Read an Epic summary or selected detail", Args: cobra.ExactArgs(1)}
	addRecordReadFlags(get)
	get.Flags().Bool("json", false, "Emit JSON")
	get.RunE = func(cmd *cobra.Command, args []string) error {
		if err := validateRecordSelection(cmd, epicPublicFields); err != nil {
			return err
		}
		env, id, err := explicitEpic(args[0])
		if err != nil {
			return err
		}
		data, err := readBoundEpic(env, id)
		if err != nil {
			return err
		}
		selected, err := selectRecordFields(cmd, data, []string{"id", "title", "process_status"}, epicPublicFields, nil)
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
	}
	create := &cobra.Command{Use: "create", Short: "Create an Epic from a JSON object", Args: cobra.NoArgs,
		Long: `Create an Epic directly in the bound System. Input example: {"title":"Account access","description":"Allow users to sign in","scope":"Sign-in flow","non_goals":"Account recovery"}. Pass --file=- to read standard input. The command returns an identity receipt and does not select an active Epic.

The file may name an Initiative of the Roadmap by its id as line_id (a
string), beside release_id: the server links the Epic to that Initiative and
places it in the Initiative's Release unless release_id names another open
Release, and the receipt names the Release and the Initiative with its
context, which you write into the requirements you create for the Epic.
line_id needs a server with this change: when the answer names no Initiative,
the command stops with an error naming the Epic the server created.

The server accepts system_id (the CLI supplies the bound System), line_id and
the fields epics update may write; any other field is refused with 422
unsupported Epic fields, a System outside your workspace with 404, and a
planning-field value epics update would refuse on a new Epic with 422 and the
field named.`}
	create.Flags().String("file", "", "Epic JSON file, or - for standard input (required)")
	_ = create.MarkFlagRequired("file")
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		payload, err := commandObject(cmd, file)
		if err != nil {
			return err
		}
		// Epic 327 (A4): line_id names an Initiative by its id, a string.
		lineID, sentLine := payload["line_id"]
		if _, isString := lineID.(string); sentLine && !isString {
			return fmt.Errorf("line_id must be the Initiative's id as a string")
		}
		env, err := cliEnvLoad()
		if err != nil {
			return err
		}
		if _, exists := payload["target_architecture_id"]; exists {
			return fmt.Errorf("omit target_architecture_id; the CLI supplies the bound System")
		}
		if _, exists := payload["system_id"]; exists {
			return fmt.Errorf("omit system_id; the CLI supplies the bound System")
		}
		payload["system_id"] = env.SystemID
		data, err := epicWrite(env, "POST", "/api/work/epics", payload)
		if err != nil {
			return err
		}
		row, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("Epic creation response must be an object")
		}
		id, ok := numberAsInt(row["id"])
		if !ok || id <= 0 {
			return fmt.Errorf("Epic creation response is missing its ID")
		}
		if err := validateBoundEpic(row, env.SystemID, id); err != nil {
			return err
		}
		receipt := map[string]any{"operation": "created", "id": id, "title": row["title"], "release_id": row["release_id"]}
		if initiative, ok := row["initiative"].(map[string]any); ok {
			receipt["initiative"] = initiative
		} else if sentLine {
			// A server without this change ignores line_id and writes the Epic
			// without the link before the CLI reads the answer.
			return fmt.Errorf("the server created Epic %d but did not link it to the Initiative: line_id needs a server with this change; link Epic %d with Add on the Initiative's panel", id, id)
		}
		return writeRecord(cmd, receipt)
	}
	update := &cobra.Command{Use: "update <epic-id>", Short: "Update an Epic's authored and planning fields", Args: cobra.ExactArgs(1),
		Long: `Update authored fields on an explicit Epic in the bound System. The CLI checks
JSON types and forwards the fields: strings title, description, scope,
non_goals, outcome_source, start_date, target_date, severity, category and
target_profile_id (dates use YYYY-MM-DD); record IDs assignee_id and
release_id; objects metadata, shared_context, impact_assessment and
knowledge_config; evidence, an array of objects. Null clears an optional
field. The server decides what it writes: the client-writable fields are
title, description, scope, non_goals, outcome_source, start_date,
target_date, severity, category, target_profile_id, assignee_id, release_id,
impact_assessment (object or null) and shared_context (object; the server refuses null); a
server-assigned field (metadata, evidence, knowledge_config, tenant_id,
system_id, process_status, plan, ...) is refused with 422 unsupported Epic
fields and nothing is written. Only supplied fields change; every accepted
edit is audited with the previous and new values (epics plan history).
Existing server validation and release/lifecycle rules apply. The server
rejects content edits while the Epic is IN_PROGRESS or IN_REVIEW, and accepts
outcome_source, impact_assessment and shared_context only while the Epic is
PROPOSED. impact_assessment is written whole: findings receive ids when
missing and contain only target references, description and proposed_action.
An empty items list or null clears it; unsupported fields or invalid findings
make the server refuse the edit. shared_context is the whole version-1 object, the only accepted
shape (the stored default {} cannot be written back); its references and
accepted_context_revision must equal the stored values (an Epic created by
the CLI stores [] and 0), so decisions, assumptions and open_questions are
what an edit changes. Each decision and assumption is an object with a
non-empty statement, and each open question an object with a non-empty
question; other keys in an entry are kept as sent, and a plain string is
refused (each decision needs a non-empty statement). Pass --file=- to read
standard input. Input examples:
{"description":"Updated scope and review findings"}
{"shared_context":{"version":1,"accepted_context_revision":0,"references":[],"decisions":[{"statement":"<decision and its source>"}],"assumptions":[{"statement":"<assumption>"}],"open_questions":[{"question":"<question>"}]}}`}
	update.Flags().String("file", "", "Epic field changes as JSON, or - for standard input (required)")
	_ = update.MarkFlagRequired("file")
	update.RunE = func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		payload, err := commandObject(cmd, file)
		if err != nil {
			return err
		}
		if len(payload) == 0 {
			return fmt.Errorf("provide at least one Epic field to update")
		}
		for field, value := range payload {
			switch field {
			case "title", "description", "scope", "non_goals", "outcome_source", "start_date", "target_date", "severity", "category", "target_profile_id":
				if value != nil {
					if _, ok := value.(string); !ok {
						return fmt.Errorf("Epic field %q must be a string or null", field)
					}
				}
			case "assignee_id", "release_id":
				if value != nil {
					id, err := strconv.ParseInt(fmt.Sprint(value), 10, 64)
					if err != nil || id <= 0 {
						return fmt.Errorf("Epic field %q must be a positive record ID or null", field)
					}
				}
			case "metadata", "shared_context", "impact_assessment", "knowledge_config":
				if value != nil {
					if _, ok := value.(map[string]any); !ok {
						return fmt.Errorf("Epic field %q must be an object or null", field)
					}
				}
			case "evidence":
				if value != nil {
					items, ok := value.([]any)
					if !ok {
						return fmt.Errorf("Epic field %q must be an array of objects or null", field)
					}
					for _, item := range items {
						if _, ok := item.(map[string]any); !ok {
							return fmt.Errorf("Epic field %q must be an array of objects or null", field)
						}
					}
				}
			default:
				return fmt.Errorf("unsupported Epic update field %q", field)
			}
		}
		env, id, err := explicitEpic(args[0])
		if err != nil {
			return err
		}
		if _, err := readBoundEpic(env, id); err != nil {
			return err
		}
		data, err := epicWrite(env, "PUT", fmt.Sprintf("/api/work/epics/%d", id), map[string]any{"epic": payload})
		if err != nil {
			return err
		}
		row, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("Epic update response must be an object")
		}
		if err := validateBoundEpic(row, env.SystemID, id); err != nil {
			return err
		}
		return writeRecord(cmd, map[string]any{"operation": "updated", "id": id, "title": row["title"]})
	}
	retire := &cobra.Command{Use: "retire <epic-id>", Short: "Retire an Epic while preserving its history", Args: cobra.ExactArgs(1),
		Long: `Retire an explicit Epic in the bound System through the server's soft-delete operation. Requirements, saved plans, tests, evidence and audit history are preserved. The server refuses retirement while the Epic owns an approved requirement change. Retirement does not approve or complete the Epic.`}
	retire.RunE = func(cmd *cobra.Command, args []string) error {
		env, id, err := explicitEpic(args[0])
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("retirement requires a bound System")
		}
		if _, err := readBoundEpic(env, id); err != nil {
			return err
		}
		data, err := epicWrite(env, "DELETE", fmt.Sprintf("/api/work/epics/%d", id), nil)
		if err != nil {
			return err
		}
		row, ok := data.(map[string]any)
		if !ok || row["deleted"] != true {
			return fmt.Errorf("Epic response did not confirm retirement")
		}
		return writeRecord(cmd, map[string]any{"operation": "retired", "id": id})
	}
	for _, cmd := range []*cobra.Command{create, update, retire} {
		cmd.Flags().Bool("json", false, "Emit JSON")
		epicsCmd.AddCommand(cmd)
	}
	epicsCmd.AddCommand(list, get)

}

func nullableCursor(cursor string) any {
	if cursor == "" {
		return nil
	}
	return cursor
}

func commandObject(cmd *cobra.Command, file string) (map[string]any, error) {
	if file == "-" {
		return readEpicPlan(cmd.InOrStdin())
	}
	return readObject(file)
}

func writeCommandJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func explicitEpic(raw string) (*cliEnv, int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return nil, 0, fmt.Errorf("epic ID must be a positive integer")
	}
	env, err := cliEnvLoad()
	return env, id, err
}

func readBoundEpic(env *cliEnv, id int64) (map[string]any, error) {
	raw, err := epicGet(env, fmt.Sprintf("/api/work/epics/%d", id))
	if err != nil {
		return nil, err
	}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Epic detail response must be an object")
	}
	if err := validateBoundEpic(data, env.SystemID, id); err != nil {
		return nil, err
	}
	return data, nil
}

func validateBoundEpic(data map[string]any, systemID int, id int64) error {
	responseID, ok := numberAsInt(data["id"])
	if !ok || responseID != id {
		return fmt.Errorf("Epic detail has no matching ID")
	}
	sid, ok := numberAsInt(data["system_id"])
	if !ok {
		sid, ok = numberAsInt(data["target_architecture_id"])
	}
	if !ok || sid != int64(systemID) {
		return fmt.Errorf("Epic %d does not belong to the bound System %d", id, systemID)
	}
	return nil
}

func epicGet(env *cliEnv, path string) (any, error) {
	return epicResponse(env.get(path))
}

func epicWrite(env *cliEnv, method, path string, payload map[string]any) (any, error) {
	return epicResponse(env.write(method, path, payload))
}

func epicResponse(status int, response map[string]any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("epic request", status, response)
	}
	data, ok := response["data"]
	if !ok || data == nil {
		return nil, fmt.Errorf("Epic response is missing data")
	}
	return data, nil
}
