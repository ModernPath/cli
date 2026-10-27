package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	requirementsCmd.AddCommand(newRequirementSetContextCommand(cliEnvLoad))
}

func newRequirementSetContextCommand(load func() (*cliEnv, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-context <uuid>",
		Short: "Set the current grouping context for a requirement",
		Long: `Compatibility shortcut for requirements update with context fields. Uses the
same update service without changing requirement status, revision, accepted
versions, criteria, traces or evidence freshness.

Without --context-name the request sends only the context, and the requirement
takes the name the System's list gives that context (see requirements
contexts), or the code for a new context. For the requirement's own code that
is the name the whole list shows, so sending only the code gives a requirement
with a minority name the list's name and leaves one that already carries it
unchanged. Leave the name out for a listed context; --context-name gives a
listed context another name on this requirement, which renames it one
requirement at a time. Explicitly blank names are refused. A code that differs
from a listed code only in letter case, a name another context has, the code
"—", Unclassified, and a value with spaces before or after it are refused.`,
		Args: cobra.ExactArgs(1),
	}
	kind, context, contextName := "", "", ""
	cmd.Flags().StringVar(&kind, "kind", "", "requirement kind: user or system")
	cmd.Flags().StringVar(&context, "context", "", "current grouping context code")
	cmd.Flags().StringVar(&contextName, "context-name", "", "display name; when left out, the requirement takes the name the System's list gives that context")
	cmd.Flags().Bool("json", false, "emit JSON")
	_ = cmd.MarkFlagRequired("kind")
	_ = cmd.MarkFlagRequired("context")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if kind != "user" && kind != "system" {
			return fmt.Errorf("--kind must be user or system")
		}
		if !requirementUUIDPattern.MatchString(args[0]) {
			return fmt.Errorf("requirement id must be a UUID")
		}
		id := strings.ToLower(args[0])
		named := cmd.Flags().Changed("context-name")
		payload := map[string]any{"context": context}
		if named {
			payload["context_name"] = contextName
		}
		if err := validateRequirementGrouping(payload); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		route := fmt.Sprintf("/api/v1/systems/%d/requirements/%s/%s/context", env.SystemID, kind, url.PathEscape(id))
		status, body, err := env.write("PUT", route, payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement context update refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement context response is missing its data object")
		}
		// Without a name the server answers with the list's name; any nonblank
		// name confirms the request then.
		name, _ := data["context_name"].(string)
		if data["id"] != id || data["context"] != context || strings.TrimSpace(name) == "" || (named && name != contextName) {
			return fmt.Errorf("requirement context response does not confirm the requested values")
		}
		return writeRecord(cmd, map[string]any{
			"id": data["id"], "context": data["context"], "context_name": data["context_name"],
			"work_status": data["work_status"], "result": data["result"],
		})
	}
	return cmd
}
