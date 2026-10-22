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
				if op.name == "approve-completion" {
					file, _ := cmd.Flags().GetString("file")
					if file == "" {
						return fmt.Errorf("approve-completion requires --file with the exact reviewed presentation (use --file - to read standard input)")
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
			command.Flags().String("file", "-", "Plan or completion presentation JSON file, or - for standard input")
		}
		if op.name == "approve-completion" {
			command.Flags().String("file", "", "Exact reviewed completion presentation JSON file; use - for standard input")
		}
		if op.name == "approve-implementation" {
			command.Flags().String("presentation-file", "", "Exact review-bundle JSON shown to the human; required for declared decisions")
		}
		if op.method != "GET" {
			command.Flags().String("reason", "", "Optional comment on the change or decision")
		}
		plan.AddCommand(command)
	}
	return plan
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
               "link_kind": "implements" | "verifies" | "derives" | "relates"}],
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
System; a document trace is relates. Criteria left out of the plan are retired
at completion; traces left out that existed at approval are removed at
completion, while code and test links added during implementation survive.

save (the Epic PROPOSED) accepts a draft: content may name only the fields
that change, criteria default to the current active ones, traces default to
the current ones, and missing criterion and trace ids are assigned. The saved
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
approve-completion (Epic IN_REVIEW) publishes the approved content: each
requirement's revision rises by one, an immutable version with criteria and
traces is retained, the audit names actor, time, reason and Epic, ownership
is released and the Epic is DONE. A refusal leaves everything unchanged; a
repeated completion publishes nothing twice.

show prints id, process_status, implementation_approval and
completion_approval; --fields plan, versions or suggested_plan, or --full,
add the plan, the accepted versions and, for an Epic with members but no
saved plan, the draft the server suggests. Read back after every write.

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
whole transition. A preview is advisory; no Epic gate check grants human approval.`

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
this Epic and approver. Run only after the human answered in the session.
For declared decisions, --presentation-file must contain the exact review-bundle
shown to the human before approval, and --file its plan. A changed decision
requires renewed independent review and human approval. Do not fetch a new bundle
to attach to an earlier approval. Decision answers are frozen at implementation
approval; changed intent becomes separately approved work.`,
	"submit-review": `Mark the implementation complete: the Epic and its owned requirements move to
IN_REVIEW with their published content unchanged. Only the approver may run
it. The human's completion decision follows separately.`,
	"approve-completion": `Record the human's completion approval and publish the approved content in one
transaction: revision +1 per requirement, an immutable accepted version,
audit, released ownership, Epic DONE. --reason carries their answer as a
USER: source. --file takes the exact bundle returned by epics gates presentation,
which the human must have reviewed before approving. Changed content, evidence
or ownership refuses the transaction. An identical retry on DONE publishes
nothing twice; a different presentation refuses.`,
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
