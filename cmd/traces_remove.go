package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"net/url"
	"strings"
)

func newTraceRemoveCommand(load func() (*cliEnv, error)) *cobra.Command {
	c := &cobra.Command{Use: "remove <trace-UUID>", Short: "Retire one named standalone implementation Trace", Args: cobra.ExactArgs(1)}
	c.Flags().String("requirement", "", "native requirement UUID (required)")
	_ = c.MarkFlagRequired("requirement")
	c.Flags().String("kind", "", "requirement kind: user or system (required)")
	_ = c.MarkFlagRequired("kind")
	c.Flags().String("reason", "", "attributed reason, 1 to 2000 bytes (required)")
	_ = c.MarkFlagRequired("reason")
	c.Flags().Bool("json", false, "emit removal receipt")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, _ := cmd.Flags().GetString("requirement")
		kind, _ := cmd.Flags().GetString("kind")
		kind = strings.TrimSuffix(kind, "_requirement")
		reason, _ := cmd.Flags().GetString("reason")
		if !requirementUUIDPattern.MatchString(args[0]) || !requirementUUIDPattern.MatchString(id) || (kind != "user" && kind != "system") || !validSmallChangeReason(reason) {
			return fmt.Errorf("select Trace and requirement UUIDs, --kind user or system, and a nonblank reason of at most 2000 bytes")
		}
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("Trace removal requires a bound System")
		}
		route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s/traces/%s/remove", env.SystemID, kind, url.PathEscape(id), url.PathEscape(args[0]))
		status, body, err := env.write("POST", route, map[string]any{"reason": reason})
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("Trace removal refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("Trace removal response is missing its data object")
		}
		sid, _ := numberAsInt(data["system_id"])
		if sid != int64(env.SystemID) || data["requirement_kind"] != kind+"_requirement" || !strings.EqualFold(str(data, "requirement_id"), id) || !strings.EqualFold(str(data, "trace_id"), args[0]) || (data["result"] != "removed" && data["result"] != "unchanged") {
			return fmt.Errorf("Trace removal response does not confirm the selected Trace and requirement")
		}
		return writeRecord(cmd, data)
	}
	return c
}
