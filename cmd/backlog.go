package cmd

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

var backlogFields = strings.Fields("id display_number display_ref system_id kind title notes_md raised_by raised_at observation why_unrouted route candidate_route disposition disposition_ref source_refs links gap_kind consequence metadata")

func init() { rootCmd.AddCommand(newBacklogCommand(cliEnvLoad)) }

func newBacklogCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "backlog", Short: "Create, read and update Backlog discoveries in the bound System"}
	update := &cobra.Command{Use: "update <uuid>", Short: "Update Backlog notes and disposition", Args: cobra.ExactArgs(1),
		Long: `Update a Backlog record from a JSON object using --file or --file - for stdin.
Allowed fields: notes_md, disposition, disposition_ref. Omitted fields are preserved.
Disposition uses OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>,
CLOSED by <id>, or ACCEPTED with <source>. Use an empty disposition_ref to clear it,
except on an assumed default after its first routing (below).
ROUTED to must name an Epic id or a requirement UUID of the bound System and writes the
routing link with the record; CLOSED by links an Epic, requirement or test case id and
keeps any other reference, such as a pull-request number, as text.
An assumed default is a record of kind assumption. Its first routing, from OPEN to
ROUTED to <epic id> while it has no disposition_ref, needs no source. Every change of
an assumed default's disposition but its first routing needs a USER: source: after
with for ACCEPTED with and REJECTED with, as in ACCEPTED with USER:<the person's words>,
and in the same file's disposition_ref for CLOSED by, DEFERRED, OPEN and ROUTED to.
A source stored by an earlier change does not count. After its first routing an
assumed default's disposition_ref changes only to a USER: source. An agent writes a
USER: source only from the person's own words, never from a template. Without one
the server refuses the whole update, notes included, and this command prints the
server's refusal as it is.
The server records the authenticated update actor and time; original attribution is preserved.
Text output names a routing or closure target by its title (ROUTED to 288 — <title>);
--json serves the disposition as stored, with the title on the link.
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
			return writeRecord(cmd, withTextDisposition(cmd, row, row))
		}}
	update.Flags().String("file", "", "Backlog update JSON file, or - for stdin (required)")
	_ = update.MarkFlagRequired("file")
	update.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(update)
	create := &cobra.Command{Use: "create", Short: "Create an attributed OPEN Backlog record", Args: cobra.NoArgs,
		Long: `Create a discovery from a JSON object using --file or --file - for stdin.
Title is required; kind defaults to backlog (backlog, gap, tooling or assumption).
Optional fields: notes_md, observation, why_unrouted, candidate_route,
gap_kind, consequence, metadata. A gap requires gap_kind capability or
specification. An assumption is an assumed default: an answer the agent chose
during a build for a question nobody had answered. Its title is the default and
its observation, which it requires, is the question the default answers, as in
{"kind":"assumption","title":"Show the points balance on the receipt","observation":"Should the receipt show the points balance?"}.
Identity, scope, attribution and OPEN disposition are assigned
by the server. --focus atomically adds the record to the signed-in reporter’s
focus, preserving other focus lanes. Example: {"title":"Keep cart after payment retry","metadata":{"synthetic":true}}.`,
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
			if focus, _ := cmd.Flags().GetBool("focus"); focus {
				payload["take_focus"] = true
			}
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
			selected, err := selectRecordFields(cmd, row, []string{"id", "display_ref", "system_id", "kind", "title", "disposition"}, backlogFields, nil)
			if err != nil {
				return err
			}
			return writeRecord(cmd, selected)
		}}
	create.Flags().String("file", "", "Backlog JSON file, or - for stdin (required)")
	_ = create.MarkFlagRequired("file")
	create.Flags().Bool("json", false, "Emit JSON")
	create.Flags().Bool("focus", false, "Add the new record to the signed-in reporter's focus")
	parent.AddCommand(create)
	get := &cobra.Command{Use: "get <uuid>", Short: "Read a Backlog record", Args: cobra.ExactArgs(1),
		Long: `Read one Backlog record of the bound System. Text output names a routing or closure
target by its title (ROUTED to 288 — <title>); --json serves the disposition as stored and the
target's title, display_ref and available on each link.`,
		RunE: func(cmd *cobra.Command, args []string) error {
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
			selected, err := selectRecordFields(cmd, row, []string{"id", "display_ref", "system_id", "kind", "title", "disposition"}, backlogFields, nil)
			if err != nil {
				return err
			}
			return writeRecord(cmd, withTextDisposition(cmd, row, selected))
		}}
	addRecordReadFlags(get)
	get.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(get)
	page := listPage{}
	list := &cobra.Command{Use: "list", Short: "List Backlog records in the bound System", Args: cobra.NoArgs,
		Long: `List the Backlog records of the bound System, one row per record: <display_ref>  <kind>  <disposition>  <title>.
A routing or closure target with a served title prints as ROUTED to 288 — <title>; --json serves
the records as stored.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			// Epic 288 (B1): a row the list cannot show is skipped and named, so
			// one bad record no longer hides every good one. create and get keep
			// their abort: a single record that fails its own check is no result.
			records := make([]map[string]any, 0, len(rows))
			skipped := make([]map[string]any, 0)
			for _, value := range rows {
				if problem := backlogRowProblem(value, env.SystemID); problem != "" {
					id := "?"
					if row, ok := value.(map[string]any); ok && str(row, "id") != "" {
						id = str(row, "id")
					}
					printWarning("backlog list: skipped record %s: %s\n", id, problem)
					skipped = append(skipped, map[string]any{"id": id, "reason": problem})
					continue
				}
				records = append(records, value.(map[string]any))
			}
			machine, _ := cmd.Flags().GetBool("json")
			start, end := page.window(len(records), machine)
			if machine {
				blob, err := json.MarshalIndent(map[string]any{"backlog": records[start:end], "total": len(records), "has_more": end < len(records), "skipped": skipped}, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(blob))
				return err
			}
			for _, row := range records[start:end] {
				// Epic 288 (B2): every column through orDash — a null disposition
				// printed `%!s(<nil>)` — and the title on one line. Epic 298
				// (SR-3813): the disposition names its target by title.
				title := strings.Join(strings.Fields(str(row, "title")), " ")
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s\n", orDash(backlogDisplayRef(row)), orDash(str(row, "kind")), orDash(backlogDispositionText(row)), orDash(title))
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

// backlogRowProblem says why a listed row cannot be shown, or "" when it can.
// The list skips such a row where create and get abort on it.
func backlogRowProblem(value any, system int) string {
	row, ok := value.(map[string]any)
	if !ok {
		return "not an object"
	}
	id, _ := row["id"].(string)
	if !requirementUUIDPattern.MatchString(id) {
		return "invalid identity"
	}
	sid, ok := numberAsInt(row["system_id"])
	if !ok {
		return "invalid identity"
	}
	if sid != int64(system) {
		return "different System"
	}
	return ""
}

func validateBacklogRecord(row map[string]any, system int) error {
	id, _ := row["id"].(string)
	sid, ok := numberAsInt(row["system_id"])
	if !requirementUUIDPattern.MatchString(id) || !ok || sid != int64(system) {
		return fmt.Errorf("Backlog response has an invalid identity or different System")
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
	if kind != "backlog" && kind != "gap" && kind != "tooling" && kind != "assumption" {
		return fmt.Errorf("kind must be backlog, gap, tooling or assumption")
	}
	if observation, _ := payload["observation"].(string); kind == "assumption" && strings.TrimSpace(observation) == "" {
		return fmt.Errorf("an assumed default needs its question: give the question the default answers in observation")
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

// backlogTargetTitle returns the title the server serves on the record's first
// routed_to or closed_by link, or "" when no such link carries one (SR-3813).
func backlogTargetTitle(row map[string]any) string {
	links, _ := row["links"].([]any)
	for _, raw := range links {
		link, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := link["role"].(string); role != "routed_to" && role != "closed_by" {
			continue
		}
		if title, _ := link["title"].(string); title != "" {
			return title
		}
	}
	return ""
}

// backlogDispositionText renders a disposition for text output: the stored
// text, then the routing or closure target's served title, as
// `ROUTED to 288 — <title>`. A disposition without a titled link is unchanged.
func backlogDispositionText(row map[string]any) string {
	disposition, _ := row["disposition"].(string)
	if title := backlogTargetTitle(row); title != "" {
		return disposition + " — " + title
	}
	return disposition
}

// withTextDisposition gives a text-mode projection the titled disposition; a
// --json projection keeps the disposition as stored and the title on the link.
func withTextDisposition(cmd *cobra.Command, row, selected map[string]any) map[string]any {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return selected
	}
	if _, present := selected["disposition"]; !present {
		return selected
	}
	out := maps.Clone(selected)
	out["disposition"] = backlogDispositionText(row)
	return out
}

func backlogDisplayRef(row map[string]any) string {
	if ref := str(row, "display_ref"); ref != "" {
		return ref
	}
	return str(row, "id")
}
