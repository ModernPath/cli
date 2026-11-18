package cmd

import (
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 436 (SR-4917, SR-4918): the `constraints` group records, accepts and
// deactivates the Engineering Constraints of the bound System through the
// Core engineering-constraints routes. A constraint is a human-owned rule with
// the ref EC-<number> and the states proposed, active and deactivated; its
// accepted text lives in revisions the server appends on accept. The CLI
// checks the file and the flags before it reads the workspace, sends the
// request as given, and prints the server's refusal as one line.

// constraintRefPattern is the display ref the server resolves
// case-insensitively; a UUID is accepted as well (requirementUUIDPattern).
var constraintRefPattern = regexp.MustCompile(`^(?i)EC-[1-9][0-9]*$`)

var (
	constraintRequiredFields = []string{"title", "statement", "verification_method"}
	constraintStates         = []string{"proposed", "active", "deactivated"}
)

const constraintReasonRefusal = "--reason must be the human's decision beginning USER:"

// constraintFileHelp is the one file shape of create and update (SR-4918).
const constraintFileHelp = `The file is a JSON object with the keys title, statement and verification_method,
all required non-empty strings, and the optional string rationale. Any other key is
refused locally as 'unsupported constraint field "<key>"'; a missing required key is
refused naming it. Use ` + "`--file -`" + ` to read the object from standard input.
Example: {"title":"...","statement":"...","rationale":"...","verification_method":"..."}.
Acceptance is the person's: accept and deactivate take --reason "USER:<date>: ...".`

const constraintAgentHelp = "Slug of the agent that ran this command; sent as the actor beside the signed-in person"

func init() { rootCmd.AddCommand(newConstraintsCommand(cliEnvLoad)) }

func newConstraintsCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "constraints", Short: "Record, accept and deactivate the Engineering Constraints of the bound System",
		Long: `An Engineering Constraint is a rule of the System in a person's words, with the ref
EC-<number> and the states proposed, active and deactivated. Any workspace member
records one with create and corrects it with update; the person accepts it with a
USER: reason, which writes the revision every Epic is held to, and deactivates it
with a USER: reason when it no longer applies. Every write takes --agent <slug> to
name the agent that ran it; the signed-in person stays the actor of record.`}

	create := &cobra.Command{Use: "create", Short: "Propose an Engineering Constraint from a JSON file", Args: cobra.NoArgs,
		Long: `Create a proposed Engineering Constraint of the bound System from a JSON object
using --file or --file - for stdin. The server assigns the next EC-<number> and the
state proposed; nothing is effective until a person accepts it.
` + constraintFileHelp + `
Prints "EC-<n>  proposed", or the created record with --json.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := constraintFilePayload(cmd)
			if err != nil {
				return err
			}
			env, base, err := loadConstraintsBase(load)
			if err != nil {
				return err
			}
			payload["system_id"] = env.SystemID
			constraintActor(cmd, payload)
			record, err := constraintRequest(env, "constraints create", "POST", base, payload)
			if err != nil {
				return err
			}
			if str(record, "state") != "proposed" {
				return fmt.Errorf("created constraint is not proposed: state %q", str(record, "state"))
			}
			return writeConstraintReceipt(cmd, record)
		}}
	addConstraintFileFlags(create)
	parent.AddCommand(create)

	update := &cobra.Command{Use: "update <ref>", Short: "Edit the text of an Engineering Constraint", Args: cobra.ExactArgs(1),
		Long: `Replace the text of the constraint <ref> (EC-<number> or its UUID) from a JSON object
using --file or --file - for stdin. On a proposed constraint the new text is the
constraint. On an active constraint the accepted revision stays effective and the
new text is a pending change until a person accepts it with 'constraints accept' or
drops it with 'constraints discard'.
` + constraintFileHelp + `
Prints "EC-<n>  <state>", or the record with --json.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConstraintRef(args[0]); err != nil {
				return err
			}
			payload, err := constraintFilePayload(cmd)
			if err != nil {
				return err
			}
			env, base, err := loadConstraintsBase(load)
			if err != nil {
				return err
			}
			payload["system_id"] = env.SystemID
			constraintActor(cmd, payload)
			record, err := constraintRequest(env, "constraints update", "PATCH", base+"/"+url.PathEscape(args[0]), payload)
			if err != nil {
				return err
			}
			return writeConstraintReceipt(cmd, record)
		}}
	addConstraintFileFlags(update)
	parent.AddCommand(update)

	discard := &cobra.Command{Use: "discard <ref>", Short: "Drop the pending change of an Engineering Constraint", Args: cobra.ExactArgs(1),
		Long: `Put the text of the current revision back on the constraint <ref> (EC-<number> or its
UUID), dropping an edit nobody accepted. Takes the ref only; the server refuses
nothing_to_discard on a proposed constraint and when the text equals the current
revision. Prints "EC-<n>  <state>", or the record with --json.`,
		RunE: constraintActionRun(load, "discard", false),
	}
	addConstraintWriteFlags(discard)
	parent.AddCommand(discard)

	deleteCmd := &cobra.Command{Use: "delete <ref>", Short: "Delete a proposed or deactivated Engineering Constraint", Args: cobra.ExactArgs(1),
		Long: `Remove the constraint <ref> (EC-<number> or its UUID) with its revisions. Takes the
ref only. An active constraint is refused with active_must_be_deactivated: run
'constraints deactivate' with the person's USER: reason first. Prints
"EC-<n>  <state>" of the removed record, or the record with --json.`,
		RunE: constraintActionRun(load, "delete", false),
	}
	addConstraintWriteFlags(deleteCmd)
	parent.AddCommand(deleteCmd)

	accept := &cobra.Command{Use: "accept <ref>", Short: "Accept an Engineering Constraint with the person's USER: reason", Args: cobra.ExactArgs(1),
		Long: `Accept the constraint <ref> (EC-<number> or its UUID) so that every Epic is held to its
text: a proposed constraint becomes active with revision 1, a pending change becomes
the next revision, a deactivated constraint becomes active again. Acceptance is the
person's decision: pass it as --reason "USER:<date>: ..."; the command refuses
locally with '` + constraintReasonRefusal + `' when the flag is
missing, blank or does not start with USER:, and the server refuses the same as
human_decision_required. An active constraint without a pending change is refused
with nothing_to_accept. Prints "EC-<n>  active", or the record with --json.`,
		RunE: constraintActionRun(load, "accept", true),
	}
	addConstraintWriteFlags(accept)
	accept.Flags().String("reason", "", `the person's decision, beginning "USER:" (required)`)
	parent.AddCommand(accept)

	deactivate := &cobra.Command{Use: "deactivate <ref>", Short: "Deactivate an Engineering Constraint with the person's USER: reason", Args: cobra.ExactArgs(1),
		Long: `Take the active constraint <ref> (EC-<number> or its UUID) out of every active read while
keeping its text, revisions and history. The decision is the person's: pass it as
--reason "USER:<date>: ..."; the command refuses locally with
'` + constraintReasonRefusal + `' when the flag is missing, blank
or does not start with USER:, and the server refuses the same as
human_decision_required. A proposed or deactivated constraint is refused with
not_active. Prints "EC-<n>  deactivated", or the record with --json.`,
		RunE: constraintActionRun(load, "deactivate", true),
	}
	addConstraintWriteFlags(deactivate)
	deactivate.Flags().String("reason", "", `the person's decision, beginning "USER:" (required)`)
	parent.AddCommand(deactivate)

	list := &cobra.Command{Use: "list", Short: "List the Engineering Constraints of the bound System", Args: cobra.NoArgs,
		Long: `List the constraints of the bound System, one row per constraint:
EC-<n>  <state>  rev <current revision number, or - when none>  <title>.
--state proposed|active|deactivated keeps one state; --state active is what every
Epic is held to. An unknown state is refused locally. --json prints the list as served.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			state, _ := cmd.Flags().GetString("state")
			if cmd.Flags().Changed("state") && !contains(constraintStates, state) {
				return fmt.Errorf("--state must be %s", strings.Join(constraintStates, ", "))
			}
			env, base, err := loadConstraintsBase(load)
			if err != nil {
				return err
			}
			path := base
			if state != "" {
				path += "?state=" + url.QueryEscape(state)
			}
			status, body, err := env.get(path)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("constraints list", status, body)
			}
			data, _ := body["data"].(map[string]any)
			rows, ok := data["constraints"].([]any)
			if !ok {
				return fmt.Errorf("constraint response is missing its list")
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, rows)
			}
			for _, value := range rows {
				row, ok := value.(map[string]any)
				if !ok {
					return fmt.Errorf("constraint response holds a row that is not an object")
				}
				title := strings.Join(strings.Fields(str(row, "title")), " ")
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  rev %s  %s\n", orDash(str(row, "ref")), orDash(str(row, "state")), constraintRevisionNumber(row), orDash(title))
			}
			return nil
		}}
	list.Flags().String("state", "", "keep one state: proposed, active or deactivated")
	list.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(list)

	get := &cobra.Command{Use: "get <ref>", Short: "Read one Engineering Constraint with its revisions and history", Args: cobra.ExactArgs(1),
		Long: `Read the constraint <ref> (EC-<number> or its UUID): its ref and state, the row text
(title, statement, rationale, verification_method), whether a change is pending, the
current revision with its number and content, then the revisions and the audit events
oldest first, each with its actor, agent and reason. --json prints the record as served.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateConstraintRef(args[0]); err != nil {
				return err
			}
			env, base, err := loadConstraintsBase(load)
			if err != nil {
				return err
			}
			record, err := constraintRequest(env, "constraints get", "GET", base+"/"+url.PathEscape(args[0]), nil)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, record)
			}
			renderConstraint(cmd.OutOrStdout(), record)
			return nil
		}}
	get.Flags().Bool("json", false, "Emit JSON")
	parent.AddCommand(get)
	return parent
}

// constraintActionRun runs discard, delete, accept and deactivate: the ref is
// checked, then the reason when the action needs one, before the workspace is
// read; the body carries the reason and, with --agent, the actor.
func constraintActionRun(load func() (*cliEnv, error), action string, needsReason bool) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := validateConstraintRef(args[0]); err != nil {
			return err
		}
		payload := map[string]any{}
		if needsReason {
			reason, _ := cmd.Flags().GetString("reason")
			if !strings.HasPrefix(reason, "USER:") || strings.TrimSpace(strings.TrimPrefix(reason, "USER:")) == "" {
				return fmt.Errorf("%s", constraintReasonRefusal)
			}
			payload["reason"] = reason
		}
		constraintActor(cmd, payload)
		env, base, err := loadConstraintsBase(load)
		if err != nil {
			return err
		}
		method, path := "POST", base+"/"+url.PathEscape(args[0])+"/"+action
		var body any = payload
		if action == "delete" {
			method, path = "DELETE", base+"/"+url.PathEscape(args[0])
			if len(payload) == 0 {
				body = nil
			}
		}
		record, err := constraintRequest(env, "constraints "+action, method, path, body)
		if err != nil {
			return err
		}
		return writeConstraintReceipt(cmd, record)
	}
}

func addConstraintFileFlags(c *cobra.Command) {
	c.Flags().String("file", "", "constraint JSON file, or - for stdin (required)")
	_ = c.MarkFlagRequired("file")
	addConstraintWriteFlags(c)
}

func addConstraintWriteFlags(c *cobra.Command) {
	c.Flags().String("agent", "", constraintAgentHelp)
	c.Flags().Bool("json", false, "Emit JSON")
}

// constraintFilePayload reads and checks the file before any workspace read.
func constraintFilePayload(cmd *cobra.Command) (map[string]any, error) {
	path, _ := cmd.Flags().GetString("file")
	payload, err := readObjectInput(cmd, path)
	if err != nil {
		return nil, err
	}
	if err := validateConstraintFile(payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// validateConstraintFile is the one file shape of SR-4918: title, statement
// and verification_method as non-empty strings, rationale an optional string,
// nothing else.
func validateConstraintFile(payload map[string]any) error {
	for key, value := range payload {
		if key != "rationale" && !contains(constraintRequiredFields, key) {
			return fmt.Errorf("unsupported constraint field %q", key)
		}
		if _, ok := value.(string); !ok {
			return fmt.Errorf("constraint field %q must be a string", key)
		}
	}
	for _, key := range constraintRequiredFields {
		if text, _ := payload[key].(string); strings.TrimSpace(text) == "" {
			return fmt.Errorf("constraint file requires a non-empty %q", key)
		}
	}
	return nil
}

func validateConstraintRef(ref string) error {
	if constraintRefPattern.MatchString(ref) || requirementUUIDPattern.MatchString(ref) {
		return nil
	}
	return fmt.Errorf("constraint ref must be EC-<number> or a UUID, got %q", ref)
}

// constraintActor adds the agent attribution `--agent` names, in the shape
// `epics plan save` sends; without the flag the signed-in person is the actor
// and the payload carries no actor at all.
func constraintActor(cmd *cobra.Command, payload map[string]any) {
	if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
		payload["actor"] = map[string]any{"kind": "agent", "agent_slug": agent}
	}
}

func loadConstraintsBase(load func() (*cliEnv, error)) (*cliEnv, string, error) {
	env, err := load()
	if err != nil {
		return nil, "", err
	}
	if env.SystemID <= 0 {
		return nil, "", fmt.Errorf("constraints commands require a bound System")
	}
	return env, fmt.Sprintf("/api/v1/systems/%d/engineering-constraints", env.SystemID), nil
}

// constraintRequest sends one request and returns the constraint record of a
// 2xx answer; a refusal reads `<what> (server <status>): <reason>`.
func constraintRequest(env *cliEnv, what, method, path string, payload any) (map[string]any, error) {
	var (
		status int
		body   map[string]any
		err    error
	)
	if method == "GET" {
		status, body, err = env.get(path)
	} else {
		status, body, err = env.write(method, path, payload)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal(what, status, body)
	}
	data, _ := body["data"].(map[string]any)
	record, ok := data["constraint"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("constraint response is missing its record")
	}
	if err := validateConstraintRecord(record, env.SystemID); err != nil {
		return nil, err
	}
	return record, nil
}

// validateConstraintRecord accepts a record with a UUID id and an EC ref; a
// system_id the server serves must be the bound System's (the route is
// System-scoped by path, so a record without one is the bound System's).
func validateConstraintRecord(record map[string]any, system int) error {
	if !requirementUUIDPattern.MatchString(str(record, "id")) || !constraintRefPattern.MatchString(str(record, "ref")) {
		return fmt.Errorf("constraint response has an invalid identity")
	}
	if raw, present := record["system_id"]; present {
		if sid, ok := numberAsInt(raw); !ok || sid != int64(system) {
			return fmt.Errorf("constraint response belongs to a different System")
		}
	}
	return nil
}

func writeConstraintReceipt(cmd *cobra.Command, record map[string]any) error {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, record)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", str(record, "ref"), orDash(str(record, "state")))
	return err
}

// constraintRevisionNumber is the current revision's number, or "-" when the
// constraint has none (proposed).
func constraintRevisionNumber(row map[string]any) string {
	revision, ok := row["current_revision"].(map[string]any)
	if !ok {
		return "-"
	}
	if n, ok := numberAsInt(revision["number"]); ok {
		return fmt.Sprint(n)
	}
	return "-"
}

// renderConstraint is the text form of `constraints get`: ref and state, the
// row text, the current revision with its number, then the revisions and the
// audit events oldest first. writeRecord's sorted key: value form would not
// give this order.
func renderConstraint(w io.Writer, record map[string]any) {
	fmt.Fprintf(w, "%s  %s\n", str(record, "ref"), orDash(str(record, "state")))
	writeConstraintText(w, "", record)
	pending := "no"
	if record["pending_change"] == true {
		pending = "yes"
	}
	fmt.Fprintf(w, "pending change: %s\n", pending)
	if revision, ok := record["current_revision"].(map[string]any); ok {
		fmt.Fprintf(w, "current revision: %s\n", constraintRevisionLine(revision))
		writeConstraintText(w, "  ", revision)
	} else {
		fmt.Fprintln(w, "current revision: —")
	}
	if at := str(record, "deactivated_at"); at != "" {
		fmt.Fprintf(w, "deactivated: %s\n", joinConstraintParts(at, "by "+personName(record["deactivated_by"]), str(record, "deactivate_reason")))
	}
	fmt.Fprintln(w, "revisions:")
	revisions, _ := record["revisions"].([]any)
	if len(revisions) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, value := range revisions {
		revision, ok := value.(map[string]any)
		if !ok {
			continue
		}
		fmt.Fprintf(w, "  %s\n", joinConstraintParts(constraintNumberText(revision["number"]), str(revision, "accepted_at"), personName(revision["accepted_by"]), str(revision, "accept_reason")))
	}
	fmt.Fprintln(w, "events:")
	events, _ := record["audit_events"].([]any)
	if len(events) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, value := range events {
		event, ok := value.(map[string]any)
		if !ok {
			continue
		}
		agent := ""
		if slug := str(event, "agent_slug"); slug != "" {
			agent = "agent " + slug
		}
		fmt.Fprintf(w, "  %s\n", joinConstraintParts(str(event, "at"), str(event, "action"), personName(event["actor"]), agent, str(event, "reason")))
	}
}

// writeConstraintText prints the four text fields of a row or a revision.
func writeConstraintText(w io.Writer, indent string, text map[string]any) {
	for _, key := range []string{"title", "statement", "rationale", "verification_method"} {
		fmt.Fprintf(w, "%s%s: %s\n", indent, key, orDash(str(text, key)))
	}
}

// constraintRevisionLine is "<number>  accepted <at> by <name>  <reason>".
func constraintRevisionLine(revision map[string]any) string {
	accepted := ""
	if at := str(revision, "accepted_at"); at != "" {
		accepted = "accepted " + at + " by " + personName(revision["accepted_by"])
	}
	return joinConstraintParts(constraintNumberText(revision["number"]), accepted, str(revision, "accept_reason"))
}

func constraintNumberText(value any) string {
	if n, ok := numberAsInt(value); ok {
		return fmt.Sprint(n)
	}
	return "—"
}

// personName is the name of a served {id, name} person, or "—".
func personName(value any) string {
	person, ok := value.(map[string]any)
	if !ok {
		return "—"
	}
	return firstNonEmpty(str(person, "name"), constraintNumberText(person["id"]))
}

func joinConstraintParts(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, "  ")
}
