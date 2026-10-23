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
		{"approve-implementation", "Approve the exact reviewed plan from standard input and acquire requirement ownership", "POST", "/approve-implementation", true},
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
				if op.input {
					file, _ := cmd.Flags().GetString("file")
					proposal, err := commandObject(cmd, file)
					if err != nil {
						return err
					}
					payload["plan"] = proposal
				}
				if op.name == "approve-implementation" {
					file, _ := cmd.Flags().GetString("presentation-file")
					if file != "" {
						if file == "-" {
							return fmt.Errorf("--presentation-file requires a saved file path")
						}
						presentation, err := commandObject(cmd, file)
						if err != nil {
							return err
						}
						payload["presentation"] = presentation
					}
				}
				if op.name == "approve-completion" || op.name == "return-to-implementation" {
					file, _ := cmd.Flags().GetString("file")
					if file == "" && op.name == "approve-completion" {
						return fmt.Errorf("approve-completion requires --file with the exact reviewed presentation (use --file - to read standard input)")
					}
					if file == "" {
						return fmt.Errorf("return-to-implementation requires --file with the exact completion bundle the human answered (use --file - to read standard input)")
					}
					presentation, err := commandObject(cmd, file)
					if err != nil {
						return err
					}
					payload["presentation"] = presentation
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
					allowed := []string{"id", "system_id", "process_status", "plan", "implementation_approval", "completion_approval", "versions", "suggested_plan", "source_files"}
					selected, err := selectRecordFields(cmd, data, []string{"id", "process_status", "implementation_approval", "completion_approval"}, allowed, nil)
					if err != nil {
						return err
					}
					return writeRecord(cmd, selected)
				}
				operation := map[string]string{
					"save": "saved", "approve-implementation": "implementation-approved",
					"submit-review": "review-submitted", "return-to-implementation": "implementation-returned",
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
			command.Flags().String("file", "-", "Plan or completion presentation JSON file, or - for standard input")
		}
		if op.name == "approve-completion" {
			command.Flags().String("file", "", "Exact reviewed completion presentation JSON file; use - for standard input")
		}
		if op.name == "approve-implementation" {
			command.Flags().String("presentation-file", "", "Exact review-bundle JSON shown to the human; required for declared decisions")
		}
		if op.name == "return-to-implementation" {
			command.Flags().String("file", "", "Exact completion bundle (epics gates presentation) the human answered; use - for standard input")
		}
		if op.method != "GET" {
			reasonHelp := "Optional comment on the change or decision"
			if op.name == "return-to-implementation" {
				reasonHelp = "The human's answer (USER:<date>: ...); required, must not be blank"
			}
			command.Flags().String("reason", "", reasonHelp)
			command.Flags().String("agent", "", "Slug of the agent that ran this command; the signed-in person is the human behind it")
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
	history.Flags().Bool("full", false, "Print the stored details in full, plan bodies included")
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
	summary := map[string]any{
		"id": item["id"], "at": item["inserted_at"], "user_name": item["user_name"],
		"action": item["action"], "comment": details["comment"],
	}
	if item["action"] == "updated" {
		summary["fields"] = details["fields"]
		return summary
	}
	saved := planCounts(details["plan"])
	previous := planCounts(details["previous_plan"])
	delta := map[string]any{}
	for key, count := range saved {
		delta[key] = count - previous[key]
	}
	summary["saved_plan"] = saved
	summary["delta"] = delta
	return summary
}

func planCounts(raw any) map[string]int {
	counts := map[string]int{"requirements": 0, "criteria": 0, "traces": 0, "body_chars": 0}
	plan, ok := raw.(map[string]any)
	if !ok {
		return counts
	}
	body, _ := plan["body"].(string)
	counts["body_chars"] = len(body)
	requirements, _ := plan["requirements"].([]any)
	counts["requirements"] = len(requirements)
	for _, raw := range requirements {
		requirement, _ := raw.(map[string]any)
		criteria, _ := requirement["criteria"].([]any)
		counts["criteria"] += len(criteria)
	}
	traces, _ := plan["traces"].([]any)
	counts["traces"] = len(traces)
	return counts
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
               "link_kind": "implements" | "verifies" | "derives" | "relates"}
              | {"source_type": "user_requirement" | "system_requirement", "source_id",
                 "link_kind": "planned"}],
   "verification": {"author_context": "<authoring context>", "decisions": ["<issued decision UUID>"],
                    "criteria": [{"id": "<criterion UUID>", "changed": true,
                                  "test_case_id": "<test-case UUID>", "boundary": "<name>"}],
                    "checks": [{"id"?, "name": "<required project check>",
                                "test_case_id": "<test-case UUID>", "boundary": "<name>"}],
                    "boundaries": [{"name": "<name>", "applicable": true}]}}

content keys are title, description, context, context_name, release_note
and detail_md, plus actor and intended_use for a user
requirement or boundary, rationale and verification_method for a system
requirement. A requirement must already exist in the System (requirements
create, or reverse-engineer record); the plan proposes what its published
content, criteria and traces become at completion. A trace must touch one of
the plan's requirements or criteria; its other endpoint must exist in the
System. When the source file does not exist yet, declare a plan-only trace:
  {"source_type":"system_requirement","source_id":"<requirement UUID>","link_kind":"planned"}
It has no target, path or trace ID and creates no file record. Real implements
traces and planned declarations may coexist for different requirements in the
same plan. Use real traces for code that already exists. Completion requires a
real implements trace to source_file or file_analysis for every requirement;
planned declarations never become native traces. After implementing new code,
record its real links with reverse-engineer record without changing the approved
plan. Read reverse-engineer schema before constructing that input. New links
are covered by the implementation review and retained at completion.
A document trace is relates. Adding, changing or removing a trace
needs every touched requirement in the plan, or save refuses the edit as
trace requirement outside plan (trace criterion has other epic for a
criterion another Epic owns). A reactivated stale trace counts as a change.
Criteria left out of the plan are retired at completion; traces left out
that existed at approval are removed at completion, while code and test
links added during implementation survive.

save (the Epic PROPOSED) accepts a draft: content may name only the fields
that change, criteria default to the current active ones, traces default to
the current ones, and missing criterion and real trace ids are assigned. The saved
plan is read back complete by show. Before approval, verification must declare
every active criterion and required project check, with retained test-case IDs.
Use decisions: [] when no human questions are declared. Omit a new project-check
id on save to have it assigned; reuse the returned id on later saves. A boundary
with applicable: false must include its reason. See epics gates --help for
registration, RED/GREEN evidence and review details. approve-implementation (the Epic
PROPOSED) takes that complete plan - every content field of the kind, every
criterion field, and a traces key - and refuses a partial one as
incomplete reviewed content; send the plan object show returned. Both take
--reason; the approval's reason carries the human's answer as a USER: source.

Approval moves the Epic and the named requirements to IN_PROGRESS and locks
each requirement to this Epic and the approver (change_epic_id); a requirement
another Epic owns refuses the whole approval. From then on the plan, the Epic
content and the owned requirement content are read-only: a changed scope is
a human decision and new work. submit-review (the approver, Epic
IN_PROGRESS) moves the Epic and its requirements to IN_REVIEW.
return-to-implementation (Epic IN_REVIEW) records the human's "not yet"
with the completion bundle they declined (--file, from epics gates
presentation): the Epic and its requirements move back to IN_PROGRESS with
the plan, the implementation approval and the ownership kept; --reason is
required.
approve-completion (Epic IN_REVIEW) publishes the approved content: each
requirement's revision rises by one, an immutable version with criteria and
traces is retained, the audit names actor, time, reason and Epic, ownership
is released and the Epic is DONE. A refusal leaves everything unchanged; a
repeated completion publishes nothing twice.

Every write takes --agent <slug>: the slug names the agent that ran the
command and the signed-in person is the human behind it; both are written on
the transition audit. Without it the person is the actor.

show prints id, process_status, implementation_approval and
completion_approval; --fields plan, versions, suggested_plan or source_files, or --full,
add the plan, the accepted versions and, for an Epic with members but no
saved plan, the draft the server suggests. source_files maps the plan's file IDs
to repository_key and path for review; it stays outside the canonical plan.
Read back after every write.

Named transition preconditions are checked by epics gates check. See epics gates
--help for verification obligations, execution registration and independent reviews.
Implementation approval requires the exact current saved plan and its independent
review. If verification.decisions is nonempty, also pass --presentation-file with
the exact epics gates review-bundle JSON shown to the human. Capture and present
it before requesting approval, and pass its plan via --file. Never refresh the
bundle after the human answers. Changed answers require renewed independent
review and human approval; answers are immutable after implementation approval.
Completion requires --file with the exact bundle returned by epics gates
presentation and reviewed by the human. Changed evidence or content refuses the
whole transition. A preview is advisory; no Epic gate check grants human approval.

history lists what the store retained about the plan, newest first: every
save (the saved plan and the one it replaced), both human approvals (the
plan they approved) and every content edit of the Epic (the changed fields
with their previous and new values). The default view summarizes each item
by counts; --full prints the stored bodies. Use it to answer "what changed
between two revisions" and "who edited the Epic" from the record, not from
memory.`

var epicPlanOperationHelp = map[string]string{
	"show": `Read the saved plan, the two human approvals and the accepted versions.
--fields plan --json prints the complete saved plan, which is the input
approve-implementation expects. --fields plan,source_files --json also resolves
the real source-file IDs to repository/path pairs for review. See epics plan --help
for the plan shape.`,
	"save": `Save the plan and proposed requirement content while the Epic is PROPOSED.
A draft may be partial; the server completes it from the current records and
show reads it back complete. Repeat to revise. See epics plan --help for the
plan shape and the refusals.`,
	"approve-implementation": `Record the human's implementation approval of the exact plan they reviewed.
The file is the complete plan object show returned; a partial plan is refused
as incomplete reviewed content. --reason carries their answer as a USER:
source. Moves the Epic and its requirements to IN_PROGRESS and locks them to
this Epic and approver. Run only after the human answered in the session.
For declared decisions, --presentation-file must contain the exact review-bundle
shown to the human before approval, and --file its plan. A changed decision
requires renewed independent review and human approval. Do not fetch a new bundle
to attach to an earlier approval. Decision answers are frozen at implementation
approval; changed intent becomes separately approved work.`,
	"submit-review": `Mark the implementation complete: the Epic and its owned requirements move to
IN_REVIEW with their published content unchanged. Only the approver may run
it. The human's completion decision follows separately. --agent <slug> names
the agent that ran it; the signed-in person is the human behind it.`,
	"return-to-implementation": `Record the human's "not yet" at completion review: the Epic and its owned
requirements move from IN_REVIEW back to IN_PROGRESS. The plan, the
implementation approval and the requirement ownership are kept, so the work
continues under the same approval and submit-review runs again when it is
done. --file takes the exact bundle returned by epics gates presentation that
the human answered; the Epic's audit keeps it. --reason carries the human's
answer as a USER: source. Any workspace member may run it. Run only after the
human answered in the session.

Refusals, as printed:
  Give the human's answer as a non-empty reason, such as a USER: source.
  Present the exact completion bundle the human answered.
  The completion bundle changed after the human saw it. Present the current
  bundle and record the human's answer to it.`,
	"approve-completion": `Record the human's completion approval and publish the approved content in one
transaction: revision +1 per requirement, an immutable accepted version,
audit, released ownership, Epic DONE. --reason carries their answer as a
USER: source. --file takes the exact bundle returned by epics gates presentation,
which the human must have reviewed before approving. Changed content, evidence
or ownership refuses the transaction. An identical retry on DONE publishes
nothing twice; a different presentation refuses.`,
	"history": `List the Epic's plan revisions, approvals and content edits, newest first.
Each item is one retained audit record: plan_saved (the saved plan and the
previous one), implementation_approved and completion_approved (the plan
the human approved) and updated (an epics update: the changed fields with
their previous and new values). The default view prints per item
{id, at, user_name, action, comment, saved_plan: {requirements, criteria,
traces, body_chars}, delta} where delta is saved_plan minus the same counts
of the previous plan (zeros when none); an updated item prints fields
instead. --full prints the served items unchanged, details.plan and
details.previous_plan included. --limit 1..50 (default 20) and --offset
page the list; a 404 means the Epic is not in your workspace or System.`,
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
