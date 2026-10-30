package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

func init() {
	epicsCmd.AddCommand(newEpicPlanCommand(cliEnvLoad))
}

func newEpicPlanCommand(load func() (*cliEnv, error)) *cobra.Command {
	plan := &cobra.Command{Use: "plan", Short: "Read and approve an Epic's saved requirement changes", Long: epicPlanHelp}
	for _, operation := range []struct {
		name, help, method, suffix string
		input                      bool
	}{
		{"show", "Read the saved plan, human approvals and accepted versions", "GET", "/plan", false},
		{"save", "Save proposed content from JSON on standard input", "PUT", "/plan", true},
		{"return-to-planning", "Withdraw active approval and return the Epic to planning", "POST", "/return-to-planning", false},
		{"approve-implementation", "Approve the current saved plan and acquire requirement ownership", "POST", "/approve-implementation", false},
		{"submit-review", "Mark implementation complete for human completion review", "POST", "/submit-review", false},
		{"return-to-implementation", "Record the human's not-yet at completion review and resume implementation", "POST", "/return-to-implementation", false},
		{"approve-completion", "Approve completion and publish the approved requirement content", "POST", "/approve-completion", false},
	} {
		op := operation
		command := &cobra.Command{
			Use: op.name + " <epic-id>", Short: op.help, Long: epicPlanOperationHelp[op.name], Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				id, err := strconv.ParseInt(args[0], 10, 64)
				if err != nil || id <= 0 {
					return fmt.Errorf("epic ID must be a positive integer")
				}
				var payload map[string]any
				if op.method != "GET" {
					payload = map[string]any{}
					reason, _ := cmd.Flags().GetString("reason")
					payload["reason"] = reason
					// SR-1641: --agent names the agent that ran the command;
					// without it the server attributes the write to the signed-in
					// person, so the payload carries no actor at all.
					if agent, _ := cmd.Flags().GetString("agent"); agent != "" {
						payload["actor"] = map[string]any{"kind": "agent", "agent_slug": agent}
					}
				}
				if op.name == "return-to-planning" {
					// Epic 344: decisions of the current plan that keep their
					// question and answer in the next planning cycle.
					carried, _ := cmd.Flags().GetStringArray("carry-decision")
					for _, id := range carried {
						if !reviewUUIDPattern.MatchString(id) {
							return fmt.Errorf("--carry-decision must be a decision UUID, got %q", id)
						}
					}
					if len(carried) > 0 {
						payload["carry_decisions"] = carried
					}
				}
				if op.input {
					file, _ := cmd.Flags().GetString("file")
					proposal, err := commandObject(cmd, file)
					if err != nil {
						return err
					}
					payload["plan"] = proposal
				}
				env, err := load()
				if err != nil {
					return err
				}
				path := fmt.Sprintf("/api/work/epics/%d%s", id, op.suffix)
				var status int
				var response map[string]any
				if op.method == "GET" {
					status, response, err = env.get(path)
				} else {
					status, response, err = env.write(op.method, path, payload)
				}
				if err != nil {
					return err
				}
				if status < 200 || status >= 300 {
					if problem, ok := response["error"].(map[string]any); ok && problem["gate"] != nil {
						_ = writeCommandJSON(cmd, problem["gate"])
					}
					if message, ok := response["message"].(string); ok && message != "" {
						return fmt.Errorf("%w: %s", serverRefusal("epic plan", status, response), message)
					}
					return serverRefusal("epic plan", status, response)
				}
				data, ok := response["data"].(map[string]any)
				if !ok {
					return fmt.Errorf("Epic plan response is missing its data object")
				}
				if op.name == "show" {
					allowed := []string{"id", "system_id", "process_status", "plan", "implementation_approval", "completion_approval", "versions", "suggested_plan", "source_files", "modernization"}
					selected, err := selectRecordFields(cmd, data, []string{"id", "process_status", "implementation_approval", "completion_approval"}, allowed, nil)
					if err != nil {
						return err
					}
					return writeRecord(cmd, selected)
				}
				operation := map[string]string{
					"save": "saved", "approve-implementation": "implementation-approved",
					"return-to-planning": "returned-to-planning",
					"submit-review":      "review-submitted", "return-to-implementation": "implementation-returned",
					"approve-completion": "completion-approved",
				}[op.name]
				return writeRecord(cmd, map[string]any{"operation": operation, "id": id, "process_status": data["process_status"]})
			},
		}
		command.Flags().Bool("json", false, "Emit JSON")
		if op.name == "show" {
			addRecordReadFlags(command)
		}
		if op.input {
			command.Flags().String("file", "-", "Plan JSON file, or - for standard input")
		}
		if op.method != "GET" {
			reasonHelp := "Optional comment on the change or decision"
			if op.name == "return-to-implementation" {
				reasonHelp = "The human's answer (USER:<date>: ...); required, must not be blank"
			}
			command.Flags().String("reason", "", reasonHelp)
			command.Flags().String("agent", "", "Slug of the agent that ran this command; the signed-in person is the human behind it")
		}
		if op.name == "return-to-planning" {
			command.Flags().Lookup("reason").Usage = "Required human return decision and reason beginning USER:"
			_ = command.MarkFlagRequired("reason")
			command.Flags().StringArray("carry-decision", nil, "Decision ID of the current plan to keep, with its answer, in the next planning cycle; repeat for each decision")
		}
		plan.AddCommand(command)
	}
	plan.AddCommand(epicPlanHistoryCommand())
	return plan
}

// epicPlanHistoryCommand reads the plan-scoped history (Epic 290): every
// saved plan revision, both human approvals and every content edit, newest
// first. The default view summarizes each item; --full prints the served
// items unchanged.
func epicPlanHistoryCommand() *cobra.Command {
	history := &cobra.Command{
		Use: "history <epic-id>", Short: "List the saved plan revisions, approvals and content edits",
		Long: epicPlanOperationHelp["history"], Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil || id <= 0 {
				return fmt.Errorf("epic ID must be a positive integer")
			}
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			full, _ := cmd.Flags().GetBool("full")
			env, err := cliEnvLoad()
			if err != nil {
				return err
			}
			path := fmt.Sprintf("/api/work/epics/%d/plan/history?limit=%d&offset=%d", id, limit, offset)
			status, response, err := env.get(path)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("epic plan history", status, response)
			}
			data, ok := response["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("Epic plan history response is missing its data object")
			}
			items, _ := data["items"].([]any)
			page := map[string]any{
				"items": items, "total_count": data["total_count"], "has_more": data["has_more"],
				"limit": data["limit"], "offset": data["offset"],
			}
			if !full {
				summaries := make([]any, 0, len(items))
				for _, item := range items {
					summaries = append(summaries, planHistorySummary(item))
				}
				page["items"] = summaries
			}
			return writeRecord(cmd, page)
		},
	}
	history.Flags().Int("limit", 20, "Items per page, 1..50")
	history.Flags().Int("offset", 0, "Items to skip, newest first")
	history.Flags().Bool("full", false, "Print surviving audit details in full")
	history.Flags().Bool("json", false, "Emit JSON")
	return history
}

// planHistorySummary is one history item without its plan bodies: the saved
// plan's counts and the delta against the previous plan (zeros when absent).
// An `updated` event names its changed fields instead. The key is saved_plan,
// not revision: a revision is a published requirement version.
func planHistorySummary(raw any) map[string]any {
	item, _ := raw.(map[string]any)
	details, _ := item["details"].(map[string]any)
	summary := map[string]any{"id": item["id"], "at": item["inserted_at"], "user_name": item["user_name"], "action": item["action"], "comment": details["comment"]}
	if item["action"] == "updated" {
		summary["fields"] = details["fields"]
	}
	return summary
}

// epicPlanHelp is the saved-plan contract as Core.Planning.EpicChanges
// enforces it: one shape for the draft and the approval, with the approval
// requiring the draft complete. The rdd skills name no command, so this is where
// an agent reads what a plan file holds.
const epicPlanHelp = `An Epic's plan is one JSON object the server stores with the Epic:

  {"body": "<the plan as markdown: outcome, scope, exclusions, sources,
            reconnaissance, change boundary, acceptance scenarios, expected
            RED, regression checks, risks, decisions>",
   "requirements": [{"kind": "user_requirement" | "system_requirement",
                     "id": "<uuid of a requirement in this System>",
                     "content": {<the fields completion will publish>},
                     "criteria": [{"id"?, "kind": "criterion" | "scenario",
                                   "position", "statement" | "given", "when", "then",
                                   "status": "active"}]}],
   "traces": [{"id"?, "source_type", "source_id", "target_type", "target_id",
               "link_kind": "implements" | "verifies" | "derives" | "relates" | "modernizes"}
              | {"source_type": "user_requirement" | "system_requirement", "source_id",
                 "link_kind": "planned"}],
   "remove_traces"?: ["<id of an existing trace this plan removes>"],
   "retire"?: [{"kind": "user_requirement" | "system_requirement",
                "id": "<uuid of a published requirement this Epic retires>",
                "reason": "<why the behavior ends, 1 to 2000 characters>"}],
   "verification": {"author_context": "<authoring context>", "decisions": ["<issued decision UUID>"],
                    "criteria": [{"id": "<criterion UUID>", "changed": true,
                                  "test_case_id": "<test-case UUID>", "boundary": "<name>"}],
                    "checks": [{"id"?, "name": "<required project check>",
                                "test_case_id": "<test-case UUID>", "boundary": "<name>"}],
                    "boundaries": [{"name": "<name>", "applicable": true}]}}

verification.mode is optional: omitted or "implementation" retains ordinary
implementation rules. To verify behavior already present, use
"mode":"existing_behavior" and "existing_behavior_reason":"<explanation>".
Verify current criterion results against the requirements, tests and code links. Every required test
must pass. Keep changed:true when criterion text differs from its baseline.
Existing-behavior entry requires current stored implements Traces for every
requirement. Reviews and separate human completion remain required. Product or
test changes use ordinary implementation. Change an active declaration through
an explicit return to planning, fresh review and approval.

content keys are title, description, context, context_name, release_note
and detail_md, plus actor and intended_use for a user
requirement or boundary, rationale and verification_method for a system
requirement. A requirement must already exist in the System (requirements
create, or reverse-engineer record); the plan proposes what its published
content, criteria and traces become at completion. A trace must touch one of
the plan's requirements or criteria; its other endpoint must exist in the
System, except the target of a modernizes trace. When the source file does not
exist yet, declare a plan-only trace:
  {"source_type":"system_requirement","source_id":"<requirement UUID>","link_kind":"planned"}
It has no target, path or trace ID and creates no file record. Real implements
traces and planned declarations may coexist for different requirements in the
same plan. Use real traces for code that already exists. Completion requires a
real implements trace to source_file or file_analysis for every requirement;
planned declarations are never stored as Traces. After implementing new code,
record its real links with reverse-engineer record without changing the approved
plan. Read reverse-engineer schema before constructing that input. New links
are covered by the implementation review and retained at completion.
A document trace is relates. Adding, changing or removing a trace
needs every touched requirement in the plan, or save refuses the edit as
trace requirement outside plan (trace criterion has other epic for a
criterion another Epic owns). Listing a stale trace does not implicitly revive it.
Criteria left out of the plan are retired at completion. A trace left out of
traces is kept: it is removed only when the plan lists its ID in remove_traces.
A kept trace does not by itself satisfy the planning gate's implements-or-planned
trace rule, which reads only traces. remove_traces names undeleted active or
stale traces of this System on a plan requirement or criterion. Deleted, unknown,
foreign-System and out-of-plan IDs are refused as trace removal not found; an
ID may not be in both lists. Approval records the list, and
completion removes exactly those traces. Code and test links added during
implementation survive.

retire ends published behavior: a requirement that is DONE, past revision 0 or
has an accepted version. requirements obsolete refuses those. List 1 to 200
distinct entries. A plan may hold only retire entries, with "requirements": []
and empty verification criteria. An entry may not also be in requirements, and
an OBSOLETE or unpublished requirement is refused. List in remove_traces every
active implements trace of the requirement and its criteria; save refuses a
new trace to it. Each entry needs its domain owner's confirmation, recorded with
epics gates confirm-removal, before implementation approval. A changed reason
needs a new confirmation. Completion publishes each retired requirement as a
new OBSOLETE version; its criteria and verifies traces are kept.

A modernizes trace links a new requirement of the plan, its source, to the
legacy requirement it modernizes in another System, its target:
  {"source_type":"user_requirement","source_id":"<plan requirement UUID>",
   "target_type":"user_requirement","target_id":"<legacy requirement UUID>",
   "link_kind":"modernizes"}
Either end may be a user or system requirement. The Epic's Initiative must
replace part of the legacy system of the Epic's Release, and save admits the
trace only for a legacy requirement that a keep or change decision of the
Epic's own Initiative names. Otherwise save refuses it as modernizes not
selected, naming the legacy requirement, and a target in the Epic's own System
as modernizes inside system. Reverse engineering never records it. A later
Epic that holds the source carries a written modernizes trace unchanged, or
retires it by leaving it out; its kind cannot change. In place, where the
Epic's Release modernizes its own System, the plan takes the requirements it
changes instead of linking new ones. In place, a verifies trace from a test
case to a kept requirement outside the plan is admitted under that
requirement's keep decision. An Epic of an Initiative that replaces part of
the legacy system is approved only while the business owner's latest
agreement holds the Initiative unchanged: the implementation check names the
blocker, and only the business owner, or a workspace admin while none is
named, can agree to the Now plan again.

save accepts proposed requirement content while the Epic is PROPOSED. The server
completes omitted content from current records, assigns missing criterion/Trace
identities, and reads the saved plan back through show. A check keeps its ID by
name: a check sent without an ID takes the ID of the saved check with the same
trimmed name, and a new name gets a new ID. Two checks with the same name, or a
check ID another check already has, are refused. Verification declares every
active criterion, project check, required test and boundary. Use decisions: [] when
there are no declared human decisions.

approve-implementation reads the current saved plan and current separate review
under server locks. It takes --reason and optional --agent; it uploads no plan or
presentation. It moves the Epic and requirements to IN_PROGRESS and acquires
requirement ownership atomically. The active approved plan and owned content stay
read-only. return-to-planning takes the explicit USER: reason, withdraws approval,
releases ownership and starts a new planning cycle. Each --carry-decision keeps
a decision of the current plan and its answer. Return clears current proof for
each published criterion whose approved meaning differs from its published meaning;
unchanged criterion proof remains current. Obtain a new plan review and implementation
approval, then retest each changed published criterion.

submit-review checks current completion-review readiness and moves work to
IN_REVIEW. approve-completion rechecks current requirements, tests, results and Traces under locks, publishes
one accepted requirement version and releases ownership. Each retired requirement becomes a
new OBSOLETE version. Repeating completion on
DONE publishes nothing twice. return-to-implementation needs a non-empty human
reason and the current IN_REVIEW lifecycle; it also works when completion readiness
fails. It keeps the plan and implementation approval. Both use small decision
requests, with no bundle upload. Human approval remains explicit and separate for
implementation and completion. These commands do not merge or deploy.

show reads current state; --fields plan,versions,suggested_plan,source_files selects
additional surviving records. --fields modernization reads the plan's
modernization block: the Initiative, the legacy system and the Release, whether
the Release is in place, each modernizes trace with the decision that admits
it (carried for a trace an earlier Epic wrote), and in place the taken
requirements, the tests linked to kept requirements and the kept requirements
at risk. It is null for an Epic that no replacing Initiative covers. history reads attributable saves, approvals, returns
and edits. Full historical presentations, outputs and copied plans are removed.
--full shows surviving audit details; it cannot reconstruct deleted payloads.
A preview is advisory. An action authorizes eligible current database state; it
does not certify that a previously rendered screen stayed unchanged. Read state
and audit after an uncertain nonterminal response.
`

var epicPlanOperationHelp = map[string]string{
	"show":                     "Read current saved plan, approvals and surviving accepted versions. Select additional fields, such as plan or modernization, with --fields or --full.",
	"save":                     "Save proposed content from --file while the Epic is PROPOSED. Read it back before review and human approval. A removal Epic lists the published requirements it retires as \"retire\": [{\"kind\", \"id\", \"reason\"}] and removes their traces through remove_traces (see epics plan --help).",
	"approve-implementation":   "Apply the human's explicit implementation decision to the current saved plan after its separate review. The server rechecks current state and acquires ownership atomically. Use --reason with the explicit USER: decision and optional --agent. Moves to IN_PROGRESS.",
	"return-to-planning":       "Withdraw implementation approval and release ownership under the current approver's account. --reason must begin USER:. Repeat --carry-decision <id> to keep a decision and its answer. Return clears current proof for each published criterion whose approved meaning differs from its published meaning; unchanged criterion proof remains current. Obtain a new plan review and implementation approval, then retest each changed published criterion. DONE stays closed.",
	"submit-review":            "Check completion-review readiness and submit current implementation for the separate human completion decision. Only the implementation approver or the Epic's assignee can submit.",
	"return-to-implementation": "Apply the human's not-yet decision to an IN_REVIEW Epic. --reason is required. Current completion readiness may fail. The plan, approval and ownership remain in effect.",
	"approve-completion":       "Apply explicit human completion approval to eligible current server state. Publish requirements, accepted versions, audit and released ownership atomically. Use --reason for the explicit USER: decision. A repeat on DONE publishes once. Each requirement the plan retires becomes a new OBSOLETE version, accepted as a retirement with the entry's reason.",
	"history":                  "Read attributable saves, approvals, returns and content edits. --full includes surviving audit details. Removed presentations and copied plans cannot be reconstructed.",
}

func readEpicPlan(input io.Reader) (map[string]any, error) {
	decoder := json.NewDecoder(input)
	decoder.UseNumber()
	var plan map[string]any
	if err := decoder.Decode(&plan); err != nil {
		return nil, fmt.Errorf("read proposed plan from standard input: %w", err)
	}
	if plan == nil {
		return nil, fmt.Errorf("proposed plan must be a JSON object")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("standard input must contain exactly one proposed plan")
	}
	return plan, nil
}
