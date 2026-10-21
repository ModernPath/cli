package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var epicPublicFields = []string{
	"id", "title", "description", "process_status", "outcome_source", "scope", "non_goals",
	"start_date", "target_date", "generated_from", "severity", "category", "evidence",
	"knowledge_config", "impact_assessment", "shared_context", "approved_at", "approval_basis",
	"overview_updated_at", "target_architecture_id", "target_profile_id", "assignee_id", "approved_by_id", "release_id",
	"metadata", "inserted_at", "updated_at", "system", "target_profile", "assignee", "approved_by", "release",
	"summary", "scenarios", "audit_events", "user_requirements", "system_requirements", "traces", "relationships",
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
				return fmt.Errorf("Epic page item is missing its positive native ID")
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
		Long: `Create an Epic directly in the bound System. Input example: {"title":"Account access","description":"Allow users to sign in","scope":"Sign-in flow","non_goals":"Account recovery"}. Pass --file=- to read standard input. The command returns an identity receipt and does not select an active Epic.`}
	create.Flags().String("file", "", "Epic JSON file, or - for standard input (required)")
	_ = create.MarkFlagRequired("file")
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		file, _ := cmd.Flags().GetString("file")
		payload, err := commandObject(cmd, file)
		if err != nil {
			return err
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
			return fmt.Errorf("Epic creation response is missing its native ID")
		}
		if err := validateBoundEpic(row, env.SystemID, id); err != nil {
			return err
		}
		return writeRecord(cmd, map[string]any{"operation": "created", "id": id, "title": row["title"]})
	}
	update := &cobra.Command{Use: "update <epic-id>", Short: "Update an Epic's authored planning fields", Args: cobra.ExactArgs(1),
		Long: `Update authored fields on an explicit Epic in the bound System. Only supplied fields change.
String fields: title, description, scope, non_goals, outcome_source, start_date,
target_date, severity, category and target_profile_id. Dates use YYYY-MM-DD.
Native record IDs: assignee_id and release_id. Object fields: metadata,
shared_context, impact_assessment and knowledge_config. evidence is an array
of objects. Null clears an optional field. Existing server validation and
release/lifecycle rules apply. Approval, lifecycle, generated summary and
other server-managed fields cannot be set here. The server rejects content edits while the Epic is IN_PROGRESS or IN_REVIEW. Pass --file=- to read standard input. Input example: {"description":"Updated scope and review findings"}.`}
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
						return fmt.Errorf("Epic field %q must be a positive native record ID or null", field)
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
	for _, cmd := range []*cobra.Command{create, update} {
		cmd.Flags().Bool("json", false, "Emit JSON")
		epicsCmd.AddCommand(cmd)
	}
	epicsCmd.AddCommand(list, get)
	specs := &cobra.Command{Use: "specs", Short: "Read, generate and exchange Epic specifications"}
	for _, op := range []struct{ name, help, method, suffix string }{
		{"list", "Summarize specifications; use --fields specs for content", "GET", "/specs"},
		{"status", "Read specification generation status", "GET", "/spec-status"},
		{"generate", "Start specification generation and return its server response", "POST", "/run-pipeline"},
		{"pull", "Download the specification bundle to a local JSON file", "GET", "/export-specs"},
		{"push", "Import specification content from a JSON file", "POST", "/import-specs"},
	} {
		op := op
		cmd := &cobra.Command{Use: op.name + " <epic-id>", Short: op.help, Args: cobra.ExactArgs(1)}
		cmd.Flags().Bool("json", false, "Emit JSON")
		if op.name == "list" || op.name == "status" {
			addRecordReadFlags(cmd)
		}
		if op.name == "pull" || op.name == "push" {
			cmd.Flags().String("file", "", "Local JSON file (required); push accepts - for standard input")
			_ = cmd.MarkFlagRequired("file")
		}
		if op.name == "push" {
			cmd.Long = `Import specification content into the explicit Epic. Input example: {"specs":[{"category":"architecture","filename":"overview.md","content":"# Overview"}]}. A bundle from epics specs pull is also accepted. Partial import failures return a nonzero exit status.`
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			env, id, err := explicitEpic(args[0])
			if err != nil {
				return err
			}
			if _, err := readBoundEpic(env, id); err != nil {
				return err
			}
			var payload map[string]any
			if op.method == "POST" {
				payload = map[string]any{}
			}
			if op.name == "push" {
				file, _ := cmd.Flags().GetString("file")
				input, err := commandObject(cmd, file)
				if err != nil {
					return err
				}
				payload, err = epicSpecsImport(input, id)
				if err != nil {
					return err
				}
			}
			path := fmt.Sprintf("/api/work/epics/%d%s", id, op.suffix)
			var data any
			if op.method == "GET" {
				data, err = epicGet(env, path)
			} else {
				data, err = epicWrite(env, op.method, path, payload)
			}
			if err != nil {
				return err
			}
			result, ok := data.(map[string]any)
			if !ok {
				return fmt.Errorf("specification response must be an object")
			}
			switch op.name {
			case "list":
				if _, ok := result["specs"].([]any); !ok {
					return fmt.Errorf("specification response is missing specs")
				}
			case "status", "generate", "pull":
				responseID, ok := numberAsInt(result["epic_id"])
				if !ok || responseID != id {
					return fmt.Errorf("specification response has no matching Epic ID")
				}
				if op.name == "status" {
					if _, ok := result["artifacts"].([]any); !ok {
						return fmt.Errorf("specification status is missing artifacts")
					}
				}
				if op.name == "generate" && str(result, "status") != "started" {
					return fmt.Errorf("specification generation was not reported as started")
				}
				if op.name == "pull" {
					if _, ok := result["specs"].(map[string]any); !ok {
						return fmt.Errorf("specification export is missing specs")
					}
				}
			}
			if op.name == "pull" {
				file, _ := cmd.Flags().GetString("file")
				if file == "-" {
					return fmt.Errorf("pull needs a local --file path; use epics specs list for stdout")
				}
				b, err := json.MarshalIndent(data, "", "  ")
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
					return err
				}
				if err := os.WriteFile(file, append(b, '\n'), 0644); err != nil {
					return err
				}
				return writeRecord(cmd, map[string]any{"operation": "exported", "epic_id": id, "file": file})
			}
			if op.name == "push" {
				result, ok := data.(map[string]any)
				if !ok {
					return fmt.Errorf("specification import response must be an object")
				}
				imported, ok := numberAsInt(result["imported"])
				errors, errorsOK := numberAsInt(result["errors"])
				if !ok || !errorsOK {
					return fmt.Errorf("specification import response is missing result counts")
				}
				if errors != 0 || imported != int64(len(payload["specs"].([]any))) {
					return fmt.Errorf("specification import incomplete: %d imported, %d failed", imported, errors)
				}
				return writeRecord(cmd, map[string]any{"operation": "imported", "epic_id": id, "imported": imported})
			}
			if op.name == "generate" {
				return writeRecord(cmd, map[string]any{"operation": "generation-started", "epic_id": id, "status": result["status"]})
			}
			// A specification's content is opt-in, like the Epic's detail fields.
			public := make([]string, 0, len(result)+1)
			for field := range result {
				public = append(public, field)
			}
			result["epic_id"] = id
			public = append(public, "epic_id")
			defaults := []string{"epic_id"}
			if op.name == "list" {
				rows := result["specs"].([]any)
				result["count"] = len(rows)
				public = append(public, "count")
				defaults = append(defaults, "count")
			} else if op.name == "status" {
				artifacts := result["artifacts"].([]any)
				counts := map[string]int{}
				for _, raw := range artifacts {
					if artifact, ok := raw.(map[string]any); ok {
						if status, ok := artifact["status"].(string); ok && status != "" {
							counts[status]++
						}
					}
				}
				result["artifact_count"] = len(artifacts)
				result["status_counts"] = counts
				public = append(public, "artifact_count", "status_counts")
				defaults = append(defaults, "artifact_count", "status_counts")
			} else if _, exists := result["status"]; exists {
				defaults = append(defaults, "status")
			}
			selected, err := selectRecordFields(cmd, result, defaults, public, nil)
			if err != nil {
				return err
			}
			return writeRecord(cmd, selected)
		}
		specs.AddCommand(cmd)
	}
	epicsCmd.AddCommand(specs)
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
		return fmt.Errorf("Epic detail has no matching native ID")
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

func epicSpecsImport(input map[string]any, id int64) (map[string]any, error) {
	if raw, exists := input["epic_id"]; exists {
		source, ok := numberAsInt(raw)
		if !ok || source != id {
			return nil, fmt.Errorf("specification bundle belongs to a different Epic")
		}
	}
	specs, ok := input["specs"].([]any)
	if !ok {
		groups, valid := input["specs"].(map[string]any)
		if !valid {
			return nil, fmt.Errorf("input must contain specs as an array or exported bundle")
		}
		specs = []any{}
		for category, raw := range groups {
			rows, valid := raw.([]any)
			if !valid {
				return nil, fmt.Errorf("invalid specification category %q", category)
			}
			for _, raw := range rows {
				row, valid := raw.(map[string]any)
				if !valid {
					return nil, fmt.Errorf("invalid specification in %q", category)
				}
				content := row["content"]
				if object, ok := content.(map[string]any); ok {
					if text, ok := object["text"].(string); ok {
						content = text
					} else {
						encoded, err := json.MarshalIndent(object, "", "  ")
						if err != nil {
							return nil, err
						}
						content = string(encoded)
					}
				}
				specs = append(specs, map[string]any{"category": category, "filename": row["filename"], "content": content})
			}
		}
	}
	if len(specs) == 0 {
		return nil, fmt.Errorf("no specifications to import")
	}
	for _, raw := range specs {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("each specification must be an object")
		}
		if str(row, "category") == "" || str(row, "filename") == "" {
			return nil, fmt.Errorf("each specification requires category and filename")
		}
		if _, ok := row["content"].(string); !ok {
			return nil, fmt.Errorf("specification content must be a string")
		}
	}
	return map[string]any{"specs": specs}, nil
}
