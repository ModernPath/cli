package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newAutopilotCommand(cliEnvLoad)) }

func newAutopilotCommand(load func() (*cliEnv, error)) *cobra.Command {
	group := &cobra.Command{Use: "autopilot", Short: "Record an explicit autopilot grant", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	create := &cobra.Command{Use: "create", Short: "Record the human's explicit grant", Long: autopilotHelp, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			file, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, file)
			if err != nil {
				return err
			}
			if err = validateAutopilotTerms(payload); err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID <= 0 {
				return fmt.Errorf("Grant creation requires a bound System")
			}
			payload["system_id"] = env.SystemID
			status, body, err := env.write("POST", "/api/work/autopilot-grants", payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				refusal := serverRefusal("Autopilot grant refused", status, body)
				if problem, ok := body["error"].(map[string]any); ok {
					if code, ok := problem["code"].(string); ok {
						return fmt.Errorf("%s: %w", code, refusal)
					}
				}
				return refusal
			}
			grant, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("Autopilot response is missing its grant")
			}
			id, ok := grant["id"].(string)
			if !ok || !requirementUUIDPattern.MatchString(id) {
				return fmt.Errorf("Autopilot response has an invalid grant identity")
			}
			if sid, ok := numberAsInt(grant["system_id"]); !ok || sid != int64(env.SystemID) {
				return fmt.Errorf("Autopilot response belongs to a different System")
			}
			if actor, ok := numberAsInt(grant["granted_by_id"]); !ok || actor <= 0 {
				return fmt.Errorf("Autopilot response is missing its grantor")
			}
			if stamp, ok := grant["inserted_at"].(string); !ok || stamp == "" {
				return fmt.Errorf("Autopilot response is missing its recording time")
			}
			return writeCommandJSON(cmd, grant)
		},
	}
	create.Flags().String("file", "", "Grant JSON object, or - for stdin (required)")
	_ = create.MarkFlagRequired("file")
	create.Flags().Bool("json", false, "Print the saved grant receipt as JSON")
	group.AddCommand(create)
	return group
}

func validateAutopilotTerms(p map[string]any) error {
	invalid := func() error {
		return fmt.Errorf("Provide only epic_ids (1–200 distinct positive IDs), expires_at (RFC3339), explicit exclusions ([] if none) and the human's nonblank USER: source")
	}
	if len(p) != 4 {
		return invalid()
	}
	for k := range p {
		if k != "epic_ids" && k != "expires_at" && k != "exclusions" && k != "source" {
			return invalid()
		}
	}
	ids, ok := p["epic_ids"].([]any)
	if !ok || len(ids) < 1 || len(ids) > 200 {
		return invalid()
	}
	seen := map[int64]bool{}
	for _, v := range ids {
		n, ok := numberAsInt(v)
		if !ok || n <= 0 || seen[n] {
			return invalid()
		}
		seen[n] = true
	}
	ex, ok := p["exclusions"].([]any)
	if !ok || len(ex) > 200 {
		return invalid()
	}
	for _, v := range ex {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return invalid()
		}
	}
	s, ok := p["source"].(string)
	if !ok || !strings.HasPrefix(s, "USER:") || strings.TrimSpace(strings.TrimPrefix(s, "USER:")) == "" {
		return invalid()
	}
	stamp, ok := p["expires_at"].(string)
	if !ok {
		return invalid()
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		return invalid()
	}
	return nil
}

const autopilotHelp = `Record a human's explicit grant in the bound System:

modernpath autopilot create --file grant.json --json

Input requires epic_ids, expires_at (RFC3339 with timezone), exclusions ([] if
none), and source (the human's own words prefixed USER:). The server returns
the saved grant with its identity, signed-in grantor and recording time.
A grant does not approve implementation, completion, merge, deployment or release.`
