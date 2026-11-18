package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() { requirementsCmd.AddCommand(newRequirementProposalCommand(cliEnvLoad)) }

func newRequirementProposalCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "proposal", Short: "Save, discover or cancel a standalone published requirement proposal"}
	for _, operation := range []string{"save", "get", "cancel"} {
		op := operation
		c := &cobra.Command{Use: op + " <requirement-UUID>", Short: map[string]string{"save": "Save exact proposed content without changing accepted content", "get": "Discover the current open proposal without mutation selectors", "cancel": "Cancel a selected proposal generation"}[op], Args: cobra.ExactArgs(1)}
		c.Flags().String("kind", "", "requirement kind: user or system")
		c.Flags().Bool("json", false, "emit the complete canonical receipt")
		if op != "get" {
			c.Flags().String("file", "", "proposal JSON file, or - for stdin (required)")
			_ = c.MarkFlagRequired("file")
			c.Flags().String("reason", "", "attributed reason, 1 to 2000 bytes (required)")
			_ = c.MarkFlagRequired("reason")
		}
		c.RunE = func(cmd *cobra.Command, args []string) error {
			kind, _ := cmd.Flags().GetString("kind")
			kind = strings.TrimSuffix(kind, "_requirement")
			if !requirementUUIDPattern.MatchString(args[0]) || (kind != "user" && kind != "system") {
				return fmt.Errorf("proposal operations require a native requirement UUID and --kind user or system")
			}
			payload := map[string]any{}
			if op != "get" {
				path, _ := cmd.Flags().GetString("file")
				var err error
				payload, err = readObjectInput(cmd, path)
				if err != nil {
					return err
				}
				reason, _ := cmd.Flags().GetString("reason")
				if !validSmallChangeReason(reason) {
					return fmt.Errorf("--reason must contain 1 to 2000 bytes of nonblank text")
				}
				if err = validateProposalInput(payload, kind, op); err != nil {
					return err
				}
				payload["reason"] = reason
			}
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID <= 0 {
				return fmt.Errorf("proposal operations require a bound System")
			}
			route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s/proposal", env.SystemID, kind, url.PathEscape(args[0]))
			var status int
			var body map[string]any
			if op == "get" {
				status, body, err = env.get(route)
			} else {
				method := "PUT"
				if op == "cancel" {
					method = "DELETE"
				}
				status, body, err = env.write(method, route, payload)
			}
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("proposal "+op+" refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("proposal response is missing its data object")
			}
			if err = validateProposalReceipt(data, kind, args[0], env.SystemID, payload, op); err != nil {
				return err
			}
			return writeRecord(cmd, data)
		}
		root.AddCommand(c)
	}
	return root
}

func validSmallChangeReason(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 2000
}

func validateProposalInput(payload map[string]any, kind, op string) error {
	allowed := "proposal_id expected_generation"
	if op == "save" {
		allowed += " expected_revision content criteria traces remove_traces no_user_requirement_reason user_requirement_id scenario_id supporting_system_requirement_ids"
	}
	for key := range payload {
		if !strings.Contains(" "+allowed+" ", " "+key+" ") {
			return fmt.Errorf("unsupported proposal field %q", key)
		}
	}
	generation, ok := numberAsInt(payload["expected_generation"])
	if !ok || generation < 0 {
		return fmt.Errorf("expected_generation must be a nonnegative integer")
	}
	if op == "cancel" || generation > 0 {
		if !requirementUUIDPattern.MatchString(str(payload, "proposal_id")) {
			return fmt.Errorf("proposal_id must name the selected proposal UUID")
		}
	}
	if id, exists := payload["proposal_id"]; exists && !requirementUUIDPattern.MatchString(fmt.Sprint(id)) {
		return fmt.Errorf("proposal_id must be a UUID")
	}
	if op == "cancel" {
		if generation == 0 {
			return fmt.Errorf("cancellation requires a positive expected_generation")
		}
		return nil
	}
	revision, ok := numberAsInt(payload["expected_revision"])
	if !ok || revision < 0 {
		return fmt.Errorf("expected_revision must be a nonnegative integer")
	}
	if raw, exists := payload["content"]; exists {
		content, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("content must be an object")
		}
		if err := validateRequirementContent(content, kind, false); err != nil {
			return err
		}
	}
	for _, field := range []string{"criteria", "traces"} {
		if raw, exists := payload[field]; exists {
			if err := validateUpdateArray(field, raw); err != nil {
				return err
			}
			items := raw.([]any)
			limit := 1000
			if field == "criteria" {
				limit = 200
				for _, v := range items {
					row := v.(map[string]any)
					if row["status"] == "superseded" {
						return fmt.Errorf("proposal criterion status must be active or retired")
					}
				}
			}
			if len(items) > limit {
				return fmt.Errorf("proposal exceeds %s limit %d", field, limit)
			}
		}
	}
	if raw, exists := payload["remove_traces"]; exists {
		if err := validateUUIDArray("remove_traces", raw); err != nil {
			return err
		}
		if len(raw.([]any))+lenAny(payload["traces"]) > 1000 {
			return fmt.Errorf("proposal exceeds 1000 structural declarations")
		}
	}
	return validateSmallChangeOutcome(payload, false)
}

func validateUUIDArray(field string, value any) error {
	items, ok := value.([]any)
	if !ok {
		return fmt.Errorf("%s must be an array of UUIDs", field)
	}
	seen := map[string]bool{}
	for _, v := range items {
		id, ok := v.(string)
		if !ok || !requirementUUIDPattern.MatchString(id) || seen[strings.ToLower(id)] {
			return fmt.Errorf("%s must contain distinct UUIDs", field)
		}
		seen[strings.ToLower(id)] = true
	}
	return nil
}

func validateProposalReceipt(data map[string]any, kind, id string, systemID int, input map[string]any, op string) error {
	invalid := fmt.Errorf("proposal response does not confirm the requested identity and generation")
	sid, _ := numberAsInt(data["system_id"])
	generation, ok := numberAsInt(data["proposal_generation"])
	if sid != int64(systemID) || data["requirement_kind"] != kind+"_requirement" || !strings.EqualFold(str(data, "requirement_id"), id) || !requirementUUIDPattern.MatchString(str(data, "proposal_id")) || !ok || generation <= 0 {
		return invalid
	}
	if selected, exists := input["proposal_id"]; exists && selected != data["proposal_id"] {
		return invalid
	}
	if op == "cancel" {
		expected, _ := numberAsInt(input["expected_generation"])
		if generation != expected || (data["result"] != "cancelled" && data["result"] != "unchanged") {
			return invalid
		}
		return nil
	}
	if (op == "get" && data["result"] != "current") || (op == "save" && data["result"] != "created" && data["result"] != "updated" && data["result"] != "unchanged") {
		return invalid
	}
	baseline, valid := numberAsInt(data["baseline_revision"])
	if !valid || baseline < 0 {
		return invalid
	}
	if expected, exists := input["expected_revision"]; exists {
		revision, _ := numberAsInt(expected)
		if revision != baseline {
			return invalid
		}
	}
	if expected, exists := input["expected_generation"]; exists {
		g, _ := numberAsInt(expected)
		if generation != g+1 {
			return invalid
		}
	}
	if _, ok := data["content"].(map[string]any); !ok {
		return invalid
	}
	for _, field := range []string{"criteria", "traces", "remove_traces"} {
		if _, ok := data[field].([]any); !ok {
			return invalid
		}
	}
	return nil
}
