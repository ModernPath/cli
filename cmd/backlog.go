package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var backlogFields = strings.Fields("id system_id kind title notes_md raised_by raised_at observation why_unrouted route candidate_route disposition disposition_ref source_refs links gap_kind consequence metadata")

func init() { rootCmd.AddCommand(newBacklogCommand(cliEnvLoad)) }

func newBacklogCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "backlog", Short: "Create, read and update Backlog discoveries in the bound System"}
	update := &cobra.Command{Use: "update <uuid>", Short: "Update Backlog notes and disposition", Args: cobra.ExactArgs(1),
		Long: `Update a Backlog record from a JSON object using --file or --file - for stdin.
Allowed fields: notes_md, disposition, disposition_ref. Omitted fields are preserved.
Disposition uses OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>,
CLOSED by <id>, or ACCEPTED with <source>. Use an empty disposition_ref to clear it.
The server records the authenticated update actor and time; original attribution is preserved.
Example: {"disposition":"CLOSED by PR:972","disposition_ref":"PR:972","notes_md":"Verified fix"}.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !requirementUUIDPattern.MatchString(args[0]) {
				return fmt.Errorf("Backlog ID must be a UUID")
			}
			path, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, path)
			if err != nil {
				return err
			}
			if err = validateBacklogUpdate(payload); err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID <= 0 {
				return fmt.Errorf("Backlog update requires a bound System")
			}
			payload["system_id"] = env.SystemID
			data, err := backlogWrite(env, "PATCH", "/api/v1/backlog/"+url.PathEscape(args[0]), payload)
			if err != nil {
				return err
			}
			row, ok := data.(map[string]any)
			if !ok {
				return fmt.Errorf("Backlog response is missing its record")
			}
			if err = validateBacklogRecord(row, env.SystemID); err != nil {
				return err
			}
			if !strings.EqualFold(row["id"].(string), args[0]) {
				return fmt.Errorf("Backlog response has a different record identity")
			}
			return writeRecord(cmd, row)
		}}
	update.Flags().String("file", "", "Backlog update JSON file, or - for stdin (required)")
	_ = update.MarkFlagRequired("file")
	update.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(update)
	create := &cobra.Command{Use: "create", Short: "Create an attributed OPEN Backlog record", Args: cobra.NoArgs,
		Long: `Create a discovery from a JSON object using --file or --file - for stdin.
Title is required; kind defaults to backlog (backlog, gap or tooling).
Optional fields: notes_md, observation, why_unrouted, candidate_route,
gap_kind, consequence, metadata. A gap requires gap_kind capability or
specification. Identity, scope, attribution and OPEN disposition are assigned
by the server. Example: {"title":"Keep cart after payment retry","metadata":{"synthetic":true}}.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, path)
			if err != nil {
				return err
			}
			if err = validateBacklogCreate(payload); err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID <= 0 {
				return fmt.Errorf("Backlog creation requires a bound System")
			}
			payload["system_id"] = env.SystemID
			data, err := backlogWrite(env, "POST", "/api/v1/backlog", payload)
			if err != nil {
				return err
			}
			row, ok := data.(map[string]any)
			if !ok {
				return fmt.Errorf("Backlog response is missing its record")
			}
			if err = validateBacklogRecord(row, env.SystemID); err != nil {
				return err
			}
			if row["disposition"] != "OPEN" {
				return fmt.Errorf("created Backlog record has no initial OPEN disposition")
			}
			selected, err := selectRecordFields(cmd, row, []string{"id", "system_id", "kind", "title", "disposition"}, backlogFields, nil)
			if err != nil {
				return err
			}
			return writeRecord(cmd, selected)
		}}
	create.Flags().String("file", "", "Backlog JSON file, or - for stdin (required)")
	_ = create.MarkFlagRequired("file")
	create.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(create)
	get := &cobra.Command{Use: "get <uuid>", Short: "Read a native Backlog record", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if !requirementUUIDPattern.MatchString(args[0]) {
			return fmt.Errorf("Backlog ID must be a UUID")
		}
		if err := validateRecordSelection(cmd, backlogFields); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		data, err := backlogGet(env, "/api/v1/backlog/"+url.PathEscape(args[0])+"?system_id="+fmt.Sprint(env.SystemID))
		if err != nil {
			return err
		}
		row, ok := data.(map[string]any)
		if !ok {
			return fmt.Errorf("Backlog response is missing its record")
		}
		if err = validateBacklogRecord(row, env.SystemID); err != nil {
			return err
		}
		if !strings.EqualFold(row["id"].(string), args[0]) {
			return fmt.Errorf("Backlog response has a different record identity")
		}
		selected, err := selectRecordFields(cmd, row, []string{"id", "system_id", "kind", "title", "disposition"}, backlogFields, nil)
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
	}}
	addRecordReadFlags(get)
	get.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(get)
	page := listPage{}
	list := &cobra.Command{Use: "list", Short: "List Backlog records in the bound System", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := page.validate(); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		data, err := backlogGet(env, "/api/v1/backlog?system_id="+fmt.Sprint(env.SystemID))
		if err != nil {
			return err
		}
		rows, ok := data.([]any)
		if !ok {
			return fmt.Errorf("Backlog response is missing its list")
		}
		records := make([]map[string]any, 0, len(rows))
		for _, value := range rows {
			row, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid Backlog list record")
			}
			if err = validateBacklogRecord(row, env.SystemID); err != nil {
				return err
			}
			records = append(records, row)
		}
		machine, _ := cmd.Flags().GetBool("json")
		start, end := page.window(len(records), machine)
		if machine {
			return emitPagedJSON(cmd.OutOrStdout(), "backlog", records[start:end], end, len(records))
		}
		for _, row := range records[start:end] {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s\n", row["id"], row["kind"], row["disposition"], row["title"])
		}
		_, err = fmt.Fprint(cmd.OutOrStdout(), pageFooter(start, end, len(records)))
		return err
	}}
	addPageFlags(list, &page)
	list.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(list)
	return parent
}

func backlogGet(env *cliEnv, path string) (any, error) {
	return backlogResponse(env.get(path))
}

func backlogWrite(env *cliEnv, method, path string, payload map[string]any) (any, error) {
	return backlogResponse(env.write(method, path, payload))
}

func backlogResponse(status int, body map[string]any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("Backlog request refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Backlog response is missing its data object")
	}
	return data["backlog"], nil
}
func validateBacklogRecord(row map[string]any, system int) error {
	id, _ := row["id"].(string)
	sid, ok := numberAsInt(row["system_id"])
	if !requirementUUIDPattern.MatchString(id) || !ok || sid != int64(system) {
		return fmt.Errorf("Backlog response has an invalid native identity or different System")
	}
	return nil
}
func validateBacklogCreate(payload map[string]any) error {
	allowed := strings.Fields("kind title notes_md observation why_unrouted candidate_route gap_kind consequence metadata")
	for key, value := range payload {
		valid := false
		for _, field := range allowed {
			if key == field {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("unsupported Backlog field %q", key)
		}
		if key == "metadata" {
			metadata, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("metadata must be an object")
			}
			for _, field := range []string{"raised_by_user_id", "updated_by_user_id", "updated_at"} {
				if _, exists := metadata[field]; exists {
					return fmt.Errorf("metadata cannot set server attribution")
				}
			}
		} else if _, ok := value.(string); !ok {
			return fmt.Errorf("Backlog field %q must be a string", key)
		}
	}
	title, _ := payload["title"].(string)
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("Backlog title is required")
	}
	kind := "backlog"
	if value, exists := payload["kind"]; exists {
		kind = value.(string)
	}
	if kind != "backlog" && kind != "gap" && kind != "tooling" {
		return fmt.Errorf("kind must be backlog, gap or tooling")
	}
	gap, hasGap := payload["gap_kind"]
	if kind == "gap" && gap != "capability" && gap != "specification" {
		return fmt.Errorf("a gap requires gap_kind capability or specification")
	}
	if kind != "gap" && hasGap {
		return fmt.Errorf("gap_kind is only valid for a gap")
	}
	return nil
}

func validateBacklogUpdate(payload map[string]any) error {
	if len(payload) == 0 {
		return fmt.Errorf("Backlog update requires notes_md, disposition or disposition_ref")
	}
	for key, value := range payload {
		if key != "notes_md" && key != "disposition" && key != "disposition_ref" {
			return fmt.Errorf("unsupported Backlog update field %q", key)
		}
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("Backlog field %q must be a string", key)
		}
		if key == "disposition" {
			valid := text == "OPEN" || text == "DEFERRED"
			for _, prefix := range []string{"ROUTED to ", "REJECTED with ", "CLOSED by ", "ACCEPTED with "} {
				valid = valid || (strings.HasPrefix(text, prefix) && strings.TrimSpace(strings.TrimPrefix(text, prefix)) != "" && !strings.ContainsAny(text, "\r\n"))
			}
			if !valid {
				return fmt.Errorf("disposition must be OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>, CLOSED by <id>, or ACCEPTED with <source>")
			}
		}
	}
	return nil
}
