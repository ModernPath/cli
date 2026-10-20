package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

func init() {
	plan := &cobra.Command{Use: "plan", Short: "Read and approve an Epic's saved requirement changes", Long: epicPlanHelp}
	for _, operation := range []struct {
		name, help, method, suffix string
		input                      bool
	}{
		{"show", "Read the saved plan, human approvals and accepted versions", "GET", "/plan", false},
		{"save", "Save proposed content from JSON on standard input", "PUT", "/plan", true},
		{"approve-implementation", "Approve the exact reviewed plan from standard input and acquire requirement ownership", "POST", "/approve-implementation", true},
		{"submit-review", "Mark implementation complete for human completion review", "POST", "/submit-review", false},
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
				}
				if op.input {
					file, _ := cmd.Flags().GetString("file")
					proposal, err := commandObject(cmd, file)
					if err != nil {
						return err
					}
					payload["plan"] = proposal
				}
				env, err := cliEnvLoad()
				if err != nil {
					return err
				}
				path := fmt.Sprintf("/api/work/epics/%d%s", id, op.suffix)
				status, response, err := env.call(op.method, path, payload)
				if err != nil {
					return err
				}
				if status < 200 || status >= 300 {
					return serverRefusal("epic plan", status, response)
				}
				data, ok := response["data"].(map[string]any)
				if !ok {
					return fmt.Errorf("Epic plan response is missing its data object")
				}
				if op.name == "show" {
					allowed := []string{"id", "system_id", "process_status", "plan", "implementation_approval", "completion_approval", "versions", "suggested_plan"}
					selected, err := selectRecordFields(cmd, data, []string{"id", "process_status", "implementation_approval", "completion_approval"}, allowed, nil)
					if err != nil {
						return err
					}
					return writeRecord(cmd, selected)
				}
				operation := map[string]string{
					"save": "saved", "approve-implementation": "implementation-approved",
					"submit-review": "review-submitted", "approve-completion": "completion-approved",
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
			command.Flags().String("reason", "", "Optional comment on the change or decision")
		}
		plan.AddCommand(command)
	}
	epicsCmd.AddCommand(plan)
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
               "link_kind": "implements" | "verifies" | "derives" | "relates"}]}

content keys are title, description, context, context_name, priority,
release_note and detail_md, plus actor and intended_use for a user
requirement or boundary, rationale and verification_method for a system
requirement. A requirement must already exist in the System (requirements
create, or reverse-engineer record); the plan proposes what its published
content, criteria and traces become at completion. A trace must touch one of
the plan's requirements or criteria; its other endpoint must exist in the
System; a document trace is relates. Criteria left out of the plan are retired
at completion; traces left out that existed at approval are removed at
completion, while code and test links added during implementation survive.

save (the Epic PROPOSED) accepts a draft: content may name only the fields
that change, criteria default to the current active ones, traces default to
the current ones, and missing criterion and trace ids are assigned. The saved
plan is read back complete by show. approve-implementation (the Epic
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
approve-completion (Epic IN_REVIEW) publishes the approved content: each
requirement's revision rises by one, an immutable version with criteria and
traces is retained, the audit names actor, time, reason and Epic, ownership
is released and the Epic is DONE. A refusal leaves everything unchanged; a
repeated completion publishes nothing twice.

show prints id, process_status, implementation_approval and
completion_approval; --fields plan, versions or suggested_plan, or --full,
add the plan, the accepted versions and, for an Epic with members but no
saved plan, the draft the server suggests. Read back after every write.`

var epicPlanOperationHelp = map[string]string{
	"show": `Read the saved plan, the two human approvals and the accepted versions.
--fields plan --json prints the complete saved plan, which is the input
approve-implementation expects. See epics plan --help for the plan shape.`,
	"save": `Save the plan and proposed requirement content while the Epic is PROPOSED.
A draft may be partial; the server completes it from the current records and
show reads it back complete. Repeat to revise. See epics plan --help for the
plan shape and the refusals.`,
	"approve-implementation": `Record the human's implementation approval of the exact plan they reviewed.
The file is the complete plan object show returned; a partial plan is refused
as incomplete reviewed content. --reason carries their answer as a USER:
source. Moves the Epic and its requirements to IN_PROGRESS and locks them to
this Epic and approver. Run only after the human answered in the session.`,
	"submit-review": `Mark the implementation complete: the Epic and its owned requirements move to
IN_REVIEW with their published content unchanged. Only the approver may run
it. The human's completion decision follows separately.`,
	"approve-completion": `Record the human's completion approval and publish the approved content in one
transaction: revision +1 per requirement, an immutable accepted version,
audit, released ownership, Epic DONE. --reason carries their answer as a
USER: source. Run only after the human answered in the session; a repeat on
a DONE Epic publishes nothing.`,
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
