package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
)

func init() {
	plan := &cobra.Command{Use: "plan", Short: "Read and approve an Epic's saved requirement changes"}
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
			Use: op.name + " <epic-id>", Short: op.help, Args: cobra.ExactArgs(1),
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
