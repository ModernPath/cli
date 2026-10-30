package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// Epic 345: modernpath initiative — the bound System's Initiatives (roadmap
// lines) over the existing /api/roadmap/lines routes, and Initiative rounds
// under /api/work/initiatives/<initiative-id>/: one review, implementation
// approval, submission or completion approval for the explicit list of an
// Initiative's Epics.

const initiativeMaxRound = 10

var initiativeCreateFields = []string{"text", "horizon", "release_id", "goal_label", "line_type", "source", "notes"}

const initiativeRoundRules = `A round is the explicit list of the Initiative's Epics that one command names
with --epics (at most 10 distinct Epic IDs); nothing stores it. A round command
writes each Epic's own records, all or nothing: the same records as the
single-Epic commands, in one transaction, so every Epic passes or nothing is
written, and a refusal names the Epic by index and ID ("item 1 (Epic 7)").
A round submission needs the caller to be each Epic's implementation approver
or its assignee; any one person may approve a round's completion. Each Epic keeps its own
approvals, reviews, gate checks and completion, and can still be returned to
planning or to implementation alone with the single-Epic commands.
A round runs in one transaction bounded at two minutes. After an uncertain
answer (a timeout or a lost connection), read an Epic of the round back before
retrying; split a large round if it times out.`

func init() { rootCmd.AddCommand(newInitiativeCommand(cliEnvLoad)) }

func newInitiativeCommand(load func() (*cliEnv, error)) *cobra.Command {
	parent := &cobra.Command{Use: "initiative", Short: "Initiatives of the bound System and Initiative rounds of their Epics",
		Long: `Initiatives (roadmap lines) of the bound System: list, get, create, link and
unlink, over the existing roadmap line routes. API fields keep the name line.

Initiative rounds: review-bundle, review, check, approve-implementation,
submit-review, presentation and approve-completion take one Initiative and the
explicit list of its Epics.

` + initiativeRoundRules}
	parent.AddCommand(
		newInitiativeListCommand(load), newInitiativeGetCommand(load), newInitiativeCreateCommand(load),
		newInitiativeLinkCommand(load, "link"), newInitiativeLinkCommand(load, "unlink"),
	)
	for _, c := range newInitiativeRoundCommands(load) {
		parent.AddCommand(c)
	}
	return parent
}

// -- Initiatives -------------------------------------------------------------

func newInitiativeListCommand(load func() (*cliEnv, error)) *cobra.Command {
	list := &cobra.Command{Use: "list", Short: "List the bound System's Initiatives", Args: cobra.NoArgs,
		Long: `List the bound System's Initiatives in one read: <uuid>  <text>  release <id>
horizon <horizon>  <progress>. --json prints the served list.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			lines, _, err := initiativeLines(load)
			if err != nil {
				return err
			}
			if machine, _ := cmd.Flags().GetBool("json"); machine {
				return writeCommandJSON(cmd, lines)
			}
			if len(lines) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No Initiatives in the bound System.")
			}
			for _, line := range lines {
				fmt.Fprintln(cmd.OutOrStdout(), initiativeSummary(line))
			}
			return nil
		}}
	list.Flags().Bool("json", false, "Emit JSON")
	return list
}

func newInitiativeGetCommand(load func() (*cliEnv, error)) *cobra.Command {
	get := &cobra.Command{Use: "get <initiative-uuid>", Short: "Read one Initiative with its work", Args: cobra.ExactArgs(1),
		Long: `Read one Initiative of the bound System with its work (its Epics and
requirements). It is read from the System's list of Initiatives; an ID that
is not in the list fails as not found.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "Initiative"); err != nil {
				return err
			}
			lines, env, err := initiativeLines(load)
			if err != nil {
				return err
			}
			for _, line := range lines {
				if strings.EqualFold(str(line, "id"), args[0]) {
					if machine, _ := cmd.Flags().GetBool("json"); machine {
						return writeCommandJSON(cmd, line)
					}
					fmt.Fprintln(cmd.OutOrStdout(), initiativeSummary(line))
					work, _ := line["work"].([]any)
					for _, raw := range work {
						item, ok := raw.(map[string]any)
						if !ok {
							continue
						}
						own := ""
						if item["own"] == true {
							own = " (own link)"
						}
						fmt.Fprintf(cmd.OutOrStdout(), "- %s %s  %s  %s%s  %s\n", orDash(str(item, "kind")), acceptanceText(item["id"]),
							orDash(str(item, "status")), orDash(oneLine(str(item, "title"))), own, orDash(str(item, "progress")))
					}
					sources, _ := line["sources"].([]any)
					for _, raw := range sources {
						if source, ok := raw.(map[string]any); ok {
							fmt.Fprintf(cmd.OutOrStdout(), "source: %s  %s\n", orDash(oneLine(str(source, "text"))), orDash(str(source, "origin")))
						}
					}
					return nil
				}
			}
			return fmt.Errorf("Initiative %s not found in System %d", args[0], env.SystemID)
		}}
	get.Flags().Bool("json", false, "Emit JSON")
	return get
}

func newInitiativeCreateCommand(load func() (*cliEnv, error)) *cobra.Command {
	create := &cobra.Command{Use: "create", Short: "Create an Initiative in the bound System", Args: cobra.NoArgs,
		Long: `Create an Initiative in the bound System from a JSON object using --file or
--file - for stdin. Allowed fields: text (required), horizon (required: now,
next or later), release_id, goal_label, line_type, source and notes; any other
field is refused before the
workspace loads. The System is the bound System.
Example: {"text":"Pay in one step","horizon":"now","release_id":7}.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			payload, err := initiativeCreateInput(cmd)
			if err != nil {
				return err
			}
			data, err := initiativeWrite(load, "POST", "/api/roadmap/lines", payload)
			if err != nil {
				return err
			}
			return initiativePrintLine(cmd, data)
		}}
	acceptanceFileFlag(create, "Initiative JSON file, or - for stdin (required)", true)
	create.Flags().Bool("json", false, "Emit JSON")
	return create
}

func newInitiativeLinkCommand(load func() (*cliEnv, error), verb string) *cobra.Command {
	short, long := "Link an Epic or a user requirement to an Initiative", `Link one Epic (--epic <id>) or one requirement (--requirement <uuid>, with
--requirement-kind user_requirement, the default, or system_requirement) to
an Initiative of the bound System. The first link is the Initiative's own link;
later links are extra work. Work another Initiative covers is refused as
already_on_roadmap with its IDs.`
	if verb == "unlink" {
		short, long = "Unlink an Epic or a user requirement from an Initiative", `Remove one extra Epic (--epic <id>) or user requirement (--requirement <uuid>)
from an Initiative of the bound System. The Initiative's own link is not
removed this way (own_link).`
	}
	c := &cobra.Command{Use: verb + " <initiative-uuid>", Short: short, Long: long, Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := acceptanceUUID(args[0], "Initiative"); err != nil {
				return err
			}
			epic, _ := cmd.Flags().GetString("epic")
			requirement, _ := cmd.Flags().GetString("requirement")
			if (epic == "") == (requirement == "") {
				return fmt.Errorf("give exactly one of --epic <id> or --requirement <uuid>")
			}
			payload := map[string]any{}
			if epic != "" {
				id, err := strconv.ParseInt(epic, 10, 64)
				if err != nil || id <= 0 {
					return fmt.Errorf("--epic must be a positive Epic ID")
				}
				if verb == "link" {
					payload["epic_id"] = id
				} else {
					payload["kind"], payload["id"] = "epic", strconv.FormatInt(id, 10)
				}
			} else {
				if err := acceptanceUUID(requirement, "requirement"); err != nil {
					return err
				}
				if verb == "link" {
					kind, _ := cmd.Flags().GetString("requirement-kind")
					if kind != "user_requirement" && kind != "system_requirement" {
						return fmt.Errorf("--requirement-kind must be user_requirement or system_requirement")
					}
					payload["requirement_id"], payload["requirement_kind"] = requirement, kind
				} else {
					// The server unlinks only an Epic or a user requirement.
					payload["kind"], payload["id"] = "user_requirement", requirement
				}
			}
			method, path := "POST", "/api/roadmap/lines/"+url.PathEscape(args[0])+"/link"
			if verb == "unlink" {
				method, path = "DELETE", "/api/roadmap/lines/"+url.PathEscape(args[0])+"/links"
			}
			data, err := initiativeWrite(load, method, path, payload)
			if err != nil {
				return err
			}
			return initiativePrintLine(cmd, data)
		}}
	c.Flags().String("epic", "", "Epic ID")
	c.Flags().String("requirement", "", "Requirement UUID")
	if verb == "link" {
		c.Flags().String("requirement-kind", "user_requirement", "user_requirement | system_requirement")
	}
	c.Flags().Bool("json", false, "Emit JSON")
	return c
}

// initiativeCreateInput reads the --file object before the workspace loads:
// text is required and only the controller's content fields are allowed.
func initiativeCreateInput(cmd *cobra.Command) (map[string]any, error) {
	path, _ := cmd.Flags().GetString("file")
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	payload, err := readObjectInput(cmd, path)
	if err != nil {
		return nil, err
	}
	for key := range payload {
		if !contains(initiativeCreateFields, key) {
			return nil, fmt.Errorf("--file field %s is not allowed", key)
		}
	}
	if text, _ := payload["text"].(string); strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("--file field text is required")
	}
	return payload, nil
}

func initiativeLines(load func() (*cliEnv, error)) ([]map[string]any, *cliEnv, error) {
	env, err := load()
	if err != nil {
		return nil, nil, err
	}
	if env.SystemID <= 0 {
		return nil, nil, fmt.Errorf("initiative requires a bound System")
	}
	status, body, err := env.get("/api/roadmap/lines?system_id=" + strconv.Itoa(env.SystemID))
	if err != nil {
		return nil, nil, err
	}
	if status < 200 || status >= 300 {
		return nil, nil, serverRefusal("Initiative request refused", status, body)
	}
	raw, _ := body["data"].([]any)
	lines := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		if line, ok := value.(map[string]any); ok {
			lines = append(lines, line)
		}
	}
	return lines, env, nil
}

func initiativeWrite(load func() (*cliEnv, error), method, path string, payload map[string]any) (any, error) {
	env, err := load()
	if err != nil {
		return nil, err
	}
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("initiative requires a bound System")
	}
	payload["system_id"] = env.SystemID
	status, body, err := env.write(method, path, payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("Initiative request refused", status, body)
	}
	data, ok := body["data"]
	if !ok {
		return nil, fmt.Errorf("Initiative response is missing its data")
	}
	return data, nil
}

func initiativePrintLine(cmd *cobra.Command, data any) error {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, data)
	}
	line, _ := data.(map[string]any)
	fmt.Fprintln(cmd.OutOrStdout(), initiativeSummary(line))
	return nil
}

func initiativeSummary(line map[string]any) string {
	release := "-"
	if line["release_id"] != nil {
		release = acceptanceText(line["release_id"])
	}
	return fmt.Sprintf("%s  %s  release %s  horizon %s  %s", orDash(str(line, "id")), orDash(oneLine(str(line, "text"))),
		release, orDash(str(line, "horizon")), orDash(str(line, "progress")))
}

// -- Initiative rounds -------------------------------------------------------

func newInitiativeRoundCommands(load func() (*cliEnv, error)) []*cobra.Command {
	type round struct {
		use, short, long, method, suffix string
		phase, reason, file, sha, read   bool
	}
	rounds := []round{
		{use: "review-bundle", short: "Read the round review bundle of an Initiative's Epics", method: "GET", suffix: "/review-bundle", phase: true, read: true,
			long: `Read each Epic's review bundle of --phase, ordered by Epic ID. The reviewer
reviews the round once from this bundle and records one review with initiative
review.`},
		{use: "review", short: "Record one review of a round", method: "POST", suffix: "/phase-reviews", file: true,
			long: `Record one plan or implementation review of a round from a JSON object using
--file or --file - for stdin, sent unchanged: phase (plan or implementation),
reviewer_context, reason, and members listing exactly the round: each Epic's
epic_id and that Epic's criteria, obligations (every one of its criteria and
obligations), findings and scope_implemented. It writes one review per Epic,
each recording the Initiative and the Epic list it covered; any refusal writes
no review.`},
		{use: "check", short: "Record each Epic's own gate check of a round", method: "POST", suffix: "/gates/check", phase: true, sha: true,
			long: `Record each Epic's own gate check of --phase and print every Epic's readiness.
Exits nonzero when any Epic of the round is not ready.`},
		{use: "approve-implementation", short: "Apply the human's one implementation approval to a round", method: "POST", suffix: "/approve-implementation", reason: true,
			long: `Approve implementation of every Epic of the round with the human's one answer
(--reason "USER:..."). Every Epic's implementation gate is rechecked in one
transaction.`},
		{use: "submit-review", short: "Submit every Epic of a round for completion review", method: "POST", suffix: "/submit-review", reason: true,
			long: `Submit every Epic of the round for completion review. The caller must be each
Epic's implementation approver or its assignee; every Epic's completion-review gate is
rechecked in one transaction.`},
		{use: "presentation", short: "Read each Epic's completion presentation of a round", method: "GET", suffix: "/completion-presentation", read: true,
			long: `Read each Epic's completion presentation, ordered by Epic ID. review.current is
true only when every Epic's latest implementation review was recorded on this
Initiative for the same Epic list and is current by recorded changes.`},
		{use: "approve-completion", short: "Apply the human's one completion approval to a round", method: "POST", suffix: "/approve-completion", reason: true,
			long: `Approve completion of every Epic of the round with the human's one answer
(--reason "USER:..."). Any one person may approve a round's completion; every
Epic's completion gate is rechecked in one transaction.`},
	}
	commands := make([]*cobra.Command, 0, len(rounds))
	for _, r := range rounds {
		r := r
		use := r.use + " <initiative-uuid>"
		if r.file {
			use += " --file <json>"
		} else {
			use += " --epics <id,id>"
		}
		c := &cobra.Command{Use: use, Short: r.short, Long: r.long + "\n\n" + initiativeRoundRules, Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if err := acceptanceUUID(args[0], "Initiative"); err != nil {
					return err
				}
				var payload map[string]any
				var ids []int64
				if r.file {
					file, _ := cmd.Flags().GetString("file")
					input, err := readObjectInput(cmd, file)
					if err != nil {
						return err
					}
					payload = input
				} else {
					raw, _ := cmd.Flags().GetString("epics")
					parsed, err := initiativeEpicIDs(raw)
					if err != nil {
						return err
					}
					ids = parsed
				}
				suffix := r.suffix
				query := url.Values{}
				if ids != nil && r.read {
					query.Set("epic_ids", joinIDs(ids))
				}
				var phase string
				if r.phase {
					phase, _ = cmd.Flags().GetString("phase")
					switch phase {
					case "plan-ready", "implementation", "completion-review", "completion":
					default:
						return fmt.Errorf("invalid phase %q", phase)
					}
					if r.read {
						query.Set("phase", phase)
					}
				}
				if r.sha {
					revision, _ := cmd.Flags().GetString("git-sha")
					provenance, err := epicCheckGitProvenance(revision, cmd.Flags().Changed("git-sha"))
					if err != nil {
						return err
					}
					payload = provenance
					payload["phase"] = phase
					payload["epic_ids"] = ids
				}
				if r.reason {
					reason, _ := cmd.Flags().GetString("reason")
					if strings.TrimSpace(reason) == "" {
						return fmt.Errorf("--reason is required: the human's answer, such as USER:<date>")
					}
					payload = map[string]any{"epic_ids": ids, "reason": reason}
					// As the single commands: --agent names the agent that ran the command.
					if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
						payload["actor"] = map[string]any{"kind": "agent", "agent_slug": agent}
					}
				}
				if len(query) > 0 {
					suffix += "?" + query.Encode()
				}
				data, err := initiativeRoundCall(cmd, load, args[0], r.method, suffix, payload)
				if err != nil {
					return err
				}
				if r.use == "check" {
					return initiativePrintCheck(cmd, args[0], phase, data)
				}
				return writeCommandJSON(cmd, data)
			}}
		if r.file {
			c.Flags().String("file", "", "Round review JSON file, or - for stdin (required)")
			_ = c.MarkFlagRequired("file")
		} else {
			c.Flags().String("epics", "", "Comma-separated Epic IDs of the round (at most 10, distinct)")
			_ = c.MarkFlagRequired("epics")
		}
		if r.phase {
			c.Flags().String("phase", "plan-ready", "plan-ready | implementation | completion-review | completion")
		}
		if r.sha {
			c.Flags().String("git-sha", "", "Full Git SHA for the audit records (defaults to checkout HEAD)")
		}
		if r.reason {
			c.Flags().String("reason", "", "The human's answer, such as USER:<date> (required)")
			c.Flags().String("agent", "", "Slug of the agent that ran this command; the signed-in person is the human behind it")
		}
		c.Flags().Bool("json", false, "Emit JSON (the default output for round reads)")
		commands = append(commands, c)
	}
	return commands
}

func initiativeEpicIDs(raw string) ([]int64, error) {
	invalid := fmt.Errorf("--epics must list 1 to %d distinct Epic IDs", initiativeMaxRound)
	if strings.TrimSpace(raw) == "" {
		return nil, invalid
	}
	parts := strings.Split(raw, ",")
	if len(parts) > initiativeMaxRound {
		return nil, invalid
	}
	seen := map[int64]bool{}
	ids := make([]int64, 0, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			return nil, invalid
		}
		seen[id] = true
		ids = append(ids, id)
	}
	return ids, nil
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ",")
}

func initiativeRoundCall(cmd *cobra.Command, load func() (*cliEnv, error), line, method, suffix string, payload map[string]any) (map[string]any, error) {
	env, err := load()
	if err != nil {
		return nil, err
	}
	path := "/api/work/initiatives/" + url.PathEscape(line) + suffix
	var status int
	var body map[string]any
	if method == "GET" {
		status, body, err = env.get(path)
	} else {
		status, body, err = env.write(method, path, payload)
	}
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		// A blocked gate prints that Epic's report first, as epics plan does.
		if problem, ok := body["error"].(map[string]any); ok && problem["gate"] != nil {
			_ = writeCommandJSON(cmd, problem["gate"])
		}
		return nil, serverRefusal("Initiative round refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Initiative round response is missing data")
	}
	return data, nil
}

func initiativePrintCheck(cmd *cobra.Command, line, phase string, data map[string]any) error {
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		if err := writeCommandJSON(cmd, data); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Initiative %s · %s · ready: %v\n", line, phase, data["ready"])
		members, _ := data["members"].([]any)
		for _, raw := range members {
			report, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Epic %s · ready: %v\n", acceptanceText(report["epic_id"]), report["ready"])
			blockers, _ := report["blockers"].([]any)
			for _, b := range blockers {
				if blocker, ok := b.(map[string]any); ok {
					fmt.Fprintf(cmd.OutOrStdout(), "- %s: %s\n", blocker["code"], blocker["message"])
				}
			}
		}
	}
	if data["ready"] != true {
		return fmt.Errorf("Initiative %s round has unresolved %s conditions", line, phase)
	}
	return nil
}
