package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 318: modernpath acceptance — release acceptance specs and reports of an
// initiative (roadmap line) in the bound System. Release acceptance records
// what a human tester checked in a release; it is not RDD evidence and never
// accepts a requirement.

var (
	acceptanceSpecFields   = []string{"title", "steps_md", "expected_md"}
	acceptanceOpenFields   = []string{"release_id", "environment", "build_ref"}
	acceptanceResultFields = []string{"outcome", "notes_md"}
	// Fields the server sets; naming one in --file is refused before loading.
	acceptanceServerFields = strings.Fields("id system_id tenant_id line_id report_id spec_id position revision spec_revision tester_id status signed_off_by_id signed_off_at sign_off_note created_by_id retired_at inserted_at updated_at")
	acceptanceOutcomes     = []string{"pass", "fail", "blocked", "not_run"}
	acceptanceUserSource   = regexp.MustCompile(`^USER:\S+$`)
)

func init() { rootCmd.AddCommand(newAcceptanceCommand(cliEnvLoad)) }

func newAcceptanceCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "acceptance", Short: "Release acceptance specs and reports of an initiative (not RDD evidence)",
		Long: `Release acceptance of an initiative (roadmap line) in the bound System: acceptance
specs (test steps for a human tester), one acceptance report per initiative and
release with a result per spec, and its sign-off or rejection. Release acceptance
is not RDD evidence: it records no test evidence and accepts no requirement.`}
	parent.AddCommand(newAcceptanceSpecsCommand(load), newAcceptanceReportCommand(load), newAcceptanceReleaseCommand(load))
	return parent
}

func newAcceptanceSpecsCommand(load func() (*cliEnv, error)) *cobra.Command {
	specs := &cobra.Command{Use: "specs", Short: "List, create, update and retire an initiative's acceptance specs"}

	list := &cobra.Command{Use: "list", Short: "List an initiative's live acceptance specs in order", Args: cobra.NoArgs,
		Long: `List the live acceptance specs of the initiative --line, in their order:
<uuid>  r<revision>  <title>. --json prints the served list.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			line, err := acceptanceUUIDFlag(cmd, "line", "initiative")
			if err != nil {
				return err
			}
			data, _, err := acceptanceRead(load, "/api/roadmap/lines/"+url.PathEscape(line)+"/acceptance-specs")
			if err != nil {
				return err
			}
			return writeAcceptanceList(cmd, data, func(row map[string]any) string {
				return fmt.Sprintf("%s  r%s  %s", orDash(str(row, "id")), acceptanceText(row["revision"]), orDash(oneLine(str(row, "title"))))
			})
		}}
	acceptanceLineFlag(list)
	list.Flags().Bool("json", false, "Emit JSON")

	create := &cobra.Command{Use: "create", Short: "Add an acceptance spec to an initiative", Args: cobra.NoArgs,
		Long: `Add an acceptance spec to the initiative --line from a JSON object using --file or
--file - for stdin. Allowed fields: title (required), steps_md, expected_md.
The spec starts at revision 1 after the initiative's other specs; position,
revision, author and scope are set by the server.
Example: {"title":"Pay by card","steps_md":"1. Add an item\n2. Pay by card","expected_md":"A receipt is shown"}.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			line, err := acceptanceUUIDFlag(cmd, "line", "initiative")
			if err != nil {
				return err
			}
			payload, err := acceptanceInput(cmd, acceptanceSpecFields, true)
			if err != nil {
				return err
			}
			if title, _ := payload["title"].(string); strings.TrimSpace(title) == "" {
				return fmt.Errorf("acceptance spec title is required")
			}
			return acceptanceWriteRecord(cmd, load, "POST", "/api/roadmap/lines/"+url.PathEscape(line)+"/acceptance-specs", payload, acceptanceSpecSummary)
		}}
	acceptanceLineFlag(create)
	acceptanceFileFlag(create, "Acceptance spec JSON file, or - for stdin (required)", true)
	create.Flags().Bool("json", false, "Emit JSON")

	update := &cobra.Command{Use: "update <spec-uuid>", Short: "Change a live acceptance spec", Args: cobra.ExactArgs(1),
		Long: `Change a live acceptance spec from a JSON object using --file or --file - for stdin.
Allowed fields: title, steps_md, expected_md; omitted fields are kept. A change
to any of them bumps the spec's revision, so results recorded for the earlier
revision no longer count for sign-off.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "acceptance spec"); err != nil {
				return err
			}
			payload, err := acceptanceInput(cmd, acceptanceSpecFields, true)
			if err != nil {
				return err
			}
			if len(payload) == 0 {
				return fmt.Errorf("acceptance spec update requires title, steps_md or expected_md")
			}
			return acceptanceWriteRecord(cmd, load, "PATCH", "/api/roadmap/acceptance-specs/"+url.PathEscape(args[0]), payload, acceptanceSpecSummary)
		}}
	acceptanceFileFlag(update, "Acceptance spec update JSON file, or - for stdin (required)", true)
	update.Flags().Bool("json", false, "Emit JSON")

	retire := &cobra.Command{Use: "retire <spec-uuid>", Short: "Retire an acceptance spec (kept, no longer required)", Args: cobra.ExactArgs(1),
		Long: `Retire an acceptance spec. It keeps its row and results and is no longer required
for sign-off. A spec is never deleted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "acceptance spec"); err != nil {
				return err
			}
			return acceptanceWriteRecord(cmd, load, "POST", "/api/roadmap/acceptance-specs/"+url.PathEscape(args[0])+"/retire", map[string]any{}, acceptanceSpecSummary)
		}}
	retire.Flags().Bool("json", false, "Emit JSON")

	specs.AddCommand(list, create, update, retire)
	return specs
}

func newAcceptanceReportCommand(load func() (*cliEnv, error)) *cobra.Command {
	report := &cobra.Command{Use: "report", Short: "Open, read and record an acceptance report; sign it off or reject it"}

	open := &cobra.Command{Use: "open", Short: "Open the initiative's acceptance report for a release", Args: cobra.NoArgs,
		Long: `Open the acceptance report of the initiative --line. With --file (or --file - for
stdin) a JSON object may name release_id, environment and build_ref; without
release_id the initiative's effective release is used. When a live (draft or
signed-off) report exists for the initiative and release, that report is
returned; a rejected one is never reopened. The tester is the signed-in person.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			line, err := acceptanceUUIDFlag(cmd, "line", "initiative")
			if err != nil {
				return err
			}
			payload, err := acceptanceInput(cmd, acceptanceOpenFields, false)
			if err != nil {
				return err
			}
			return acceptanceWriteRecord(cmd, load, "POST", "/api/roadmap/lines/"+url.PathEscape(line)+"/acceptance-reports", payload, acceptanceReportSummary)
		}}
	acceptanceLineFlag(open)
	acceptanceFileFlag(open, "Report JSON file, or - for stdin (optional)", false)
	open.Flags().Bool("json", false, "Emit JSON")

	get := &cobra.Command{Use: "get <report-uuid>", Short: "Read an acceptance report with its results", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "acceptance report"); err != nil {
				return err
			}
			data, _, err := acceptanceRead(load, "/api/roadmap/acceptance-reports/"+url.PathEscape(args[0]))
			if err != nil {
				return err
			}
			return writeAcceptanceRecord(cmd, data, acceptanceReportSummary)
		}}
	get.Flags().Bool("json", false, "Emit JSON")

	record := &cobra.Command{Use: "record <report-uuid>", Short: "Record the result of one spec in a draft report", Args: cobra.ExactArgs(1),
		Long: `Record the result of the spec --spec in a draft acceptance report from a JSON object
using --file or --file - for stdin. Allowed fields: outcome (pass, fail, blocked
or not_run; required) and notes_md. The result stores the spec's current
revision and replaces an earlier result for the same spec.
Example: {"outcome":"fail","notes_md":"The receipt shows the wrong total"}.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "acceptance report"); err != nil {
				return err
			}
			spec, err := acceptanceUUIDFlag(cmd, "spec", "acceptance spec")
			if err != nil {
				return err
			}
			payload, err := acceptanceInput(cmd, acceptanceResultFields, true)
			if err != nil {
				return err
			}
			outcome, _ := payload["outcome"].(string)
			if !contains(acceptanceOutcomes, outcome) {
				return fmt.Errorf("outcome must be pass, fail, blocked or not_run")
			}
			return acceptanceWriteRecord(cmd, load, "PUT", "/api/roadmap/acceptance-reports/"+url.PathEscape(args[0])+"/results/"+url.PathEscape(spec), payload, []string{"id", "spec_id", "spec_revision", "outcome"})
		}}
	record.Flags().String("spec", "", "Acceptance spec UUID (required)")
	acceptanceFileFlag(record, "Result JSON file, or - for stdin (required)", true)
	record.Flags().Bool("json", false, "Emit JSON")

	decide := func(use, short, action, long string, noteRequired bool) *cobra.Command {
		c := &cobra.Command{Use: use + " <report-uuid>", Short: short, Args: cobra.ExactArgs(1), Long: long,
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := acceptanceUUID(args[0], "acceptance report"); err != nil {
					return err
				}
				source, _ := cmd.Flags().GetString("source")
				if !acceptanceUserSource.MatchString(source) {
					return fmt.Errorf("%s needs the human's decision: pass --source USER:<date or reference> (a person signs off or rejects, not an agent)", use)
				}
				note, _ := cmd.Flags().GetString("note")
				if noteRequired && strings.TrimSpace(note) == "" {
					return fmt.Errorf("%s requires --note", use)
				}
				payload := map[string]any{"source": source}
				if note != "" {
					payload["sign_off_note"] = note
				}
				return acceptanceWriteRecord(cmd, load, "POST", "/api/roadmap/acceptance-reports/"+url.PathEscape(args[0])+"/"+action, payload, acceptanceReportSummary)
			}}
		c.Flags().String("source", "", "The human decision, USER:<date or reference> (required)")
		c.Flags().String("note", "", "Sign-off note")
		c.Flags().Bool("json", false, "Emit JSON")
		return c
	}
	signOff := decide("sign-off", "Sign off a draft report as the signed-in person", "sign-off",
		`Sign off a draft acceptance report as the signed-in person. Every live spec needs a
result for its current revision, and a fail or blocked result needs --note.
--source USER:<date or reference> names the human decision and is required: an
agent records results but never signs off on its own. The source is kept on the
audit event. A signed-off report is closed.`, false)
	reject := decide("reject", "Reject a draft report as the signed-in person", "reject",
		`Reject a draft acceptance report as the signed-in person; --note is required.
--source USER:<date or reference> names the human decision and is required. The
source is kept on the audit event. A rejected report is closed; opening the
report again creates a new draft.`, true)

	report.AddCommand(open, get, record, signOff, reject)
	return report
}

func newAcceptanceReleaseCommand(load func() (*cliEnv, error)) *cobra.Command {
	release := &cobra.Command{Use: "release <release-id>", Short: "List every initiative in a release with its acceptance report status", Args: cobra.ExactArgs(1),
		Long: `List every initiative whose effective release is <release-id>, with its acceptance
report and status (none, draft, signed_off or rejected): <line-uuid>  <status>  <text>.
The live report is shown, else the newest rejected one. --json prints the served list.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.Atoi(args[0])
			if err != nil || id <= 0 {
				return fmt.Errorf("release id must be a positive integer")
			}
			data, _, err := acceptanceRead(load, "/api/compliance/releases/"+strconv.Itoa(id)+"/acceptance")
			if err != nil {
				return err
			}
			return writeAcceptanceList(cmd, data, func(row map[string]any) string {
				return fmt.Sprintf("%s  %s  %s", orDash(str(row, "line_id")), orDash(str(row, "status")), orDash(oneLine(str(row, "text"))))
			})
		}}
	release.Flags().Bool("json", false, "Emit JSON")
	return release
}

var (
	acceptanceSpecSummary   = []string{"id", "line_id", "title", "revision", "retired_at"}
	acceptanceReportSummary = []string{"id", "line_id", "release_id", "status", "tester_name", "signed_off_by_name"}
)

func acceptanceLineFlag(cmd *cobra.Command) {
	cmd.Flags().String("line", "", "Initiative (roadmap line) UUID (required)")
}

func acceptanceFileFlag(cmd *cobra.Command, usage string, required bool) {
	cmd.Flags().String("file", "", usage)
	if required {
		_ = cmd.MarkFlagRequired("file")
	}
}

func acceptanceUUID(value, what string) error {
	if !requirementUUIDPattern.MatchString(value) {
		return fmt.Errorf("%s ID must be a UUID", what)
	}
	return nil
}

func acceptanceUUIDFlag(cmd *cobra.Command, flag, what string) (string, error) {
	value, _ := cmd.Flags().GetString(flag)
	if value == "" {
		return "", fmt.Errorf("--%s <uuid> is required", flag)
	}
	return value, acceptanceUUID(value, what)
}

// acceptanceInput reads the --file object and refuses a field outside
// allowed, naming a server-set field as such, before the workspace loads.
func acceptanceInput(cmd *cobra.Command, allowed []string, required bool) (map[string]any, error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		if required {
			return nil, fmt.Errorf("--file is required")
		}
		return map[string]any{}, nil
	}
	payload, err := readObjectInput(cmd, path)
	if err != nil {
		return nil, err
	}
	for key, value := range payload {
		if contains(acceptanceServerFields, key) {
			return nil, fmt.Errorf("%q is set by the server and cannot be sent", key)
		}
		if !contains(allowed, key) {
			return nil, fmt.Errorf("unsupported field %q (allowed: %s)", key, strings.Join(allowed, ", "))
		}
		if key == "release_id" {
			if _, ok := numberAsInt(value); !ok {
				return nil, fmt.Errorf("release_id must be an integer")
			}
			continue
		}
		if _, ok := value.(string); !ok {
			return nil, fmt.Errorf("field %q must be a string", key)
		}
	}
	return payload, nil
}

// acceptanceRead loads the workspace and GETs path with the bound System.
func acceptanceRead(load func() (*cliEnv, error), path string) (any, *cliEnv, error) {
	env, err := load()
	if err != nil {
		return nil, nil, err
	}
	if env.SystemID <= 0 {
		return nil, nil, fmt.Errorf("acceptance requires a bound System")
	}
	data, err := acceptanceResponse(env.get(path + "?system_id=" + strconv.Itoa(env.SystemID)))
	return data, env, err
}

func acceptanceWriteRecord(cmd *cobra.Command, load func() (*cliEnv, error), method, path string, payload map[string]any, summary []string) error {
	env, err := load()
	if err != nil {
		return err
	}
	if env.SystemID <= 0 {
		return fmt.Errorf("acceptance requires a bound System")
	}
	payload["system_id"] = env.SystemID
	data, err := acceptanceResponse(env.write(method, path, payload))
	if err != nil {
		return err
	}
	return writeAcceptanceRecord(cmd, data, summary)
}

// acceptanceResponse unwraps {data: ...}; a refusal names the server code
// (not_found, report_closed, results_missing, ...).
func acceptanceResponse(status int, body map[string]any, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("Acceptance request refused", status, body)
	}
	data, ok := body["data"]
	if !ok {
		return nil, fmt.Errorf("acceptance response is missing its data")
	}
	return data, nil
}

func writeAcceptanceRecord(cmd *cobra.Command, data any, summary []string) error {
	row, ok := data.(map[string]any)
	if !ok {
		return fmt.Errorf("acceptance response is missing its record")
	}
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, row)
	}
	selected := make(map[string]any, len(summary))
	for _, field := range summary {
		selected[field] = row[field]
	}
	if results, ok := row["results"].([]any); ok {
		selected["results"] = len(results)
	}
	return writeRecord(cmd, selected)
}

func writeAcceptanceList(cmd *cobra.Command, data any, line func(map[string]any) string) error {
	rows, ok := data.([]any)
	if !ok {
		return fmt.Errorf("acceptance response is missing its list")
	}
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		blob, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(blob))
		return err
	}
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if _, err := fmt.Fprintln(cmd.OutOrStdout(), line(row)); err != nil {
			return err
		}
	}
	return nil
}

func acceptanceText(value any) string {
	if value == nil {
		return "-"
	}
	return fmt.Sprint(value)
}

func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }
