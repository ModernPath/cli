package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementObsoleteCommand(cliEnvLoad))
}

func newRequirementObsoleteCommand(load func() (*cliEnv, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "obsolete <uuid>",
		Short: "Obsolete an unpublished, unowned native requirement",
		Args:  cobra.ExactArgs(1),
		Long:  "Mark a PROPOSED, BLOCKED, or DEFERRED requirement obsolete. The server records the authenticated actor and reason, and rejects active Epic ownership or published requirements.",
	}
	kind, reason := "", ""
	cmd.Flags().StringVar(&kind, "kind", "", "requirement kind: user or system")
	cmd.Flags().StringVar(&reason, "reason", "", "why the requirement is obsolete (required)")
	cmd.Flags().Bool("json", false, "emit JSON")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("reason")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if kind != "user" && kind != "system" {
			return fmt.Errorf("--kind must be user or system")
		}
		if !requirementUUIDPattern.MatchString(args[0]) {
			return fmt.Errorf("requirement id must be a native UUID")
		}
		id := strings.ToLower(args[0])
		reason = strings.TrimSpace(reason)
		if reason == "" || len(reason) > 2000 {
			return fmt.Errorf("--reason must contain 1 to 2000 characters")
		}
		env, err := load()
		if err != nil {
			return err
		}
		route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s/obsolete", env.SystemID, kind, url.PathEscape(id))
		status, body, err := env.write("POST", route, map[string]string{"reason": reason})
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement obsoletion refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement obsoletion response is missing its data object")
		}
		if data["work_status"] != "OBSOLETE" || data["id"] != id {
			return fmt.Errorf("requirement obsoletion response does not confirm the requested requirement")
		}
		return writeRecord(cmd, map[string]any{"id": data["id"], "work_status": data["work_status"], "result": data["result"]})
	}
	return cmd
}
