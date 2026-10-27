package cmd

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// Epic 340 (G1): `modernpath requirements contexts` reads the bound System's
// list of contexts, the list reverse engineering and regrouping apply: each
// context code with its name, its number of requirements and the other names
// its requirements carry, then the requirements without a context on a line
// labelled Unclassified.

func init() {
	requirementsCmd.AddCommand(newRequirementContextsCommand(cliEnvLoad))
}

func newRequirementContextsCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "contexts",
		Short: "List the bound System's contexts: each code with its name and number of requirements",
		Args:  cobra.NoArgs,
		Long: `List the bound System's list of contexts: every context code its requirements
carry, with the name the list gives it, how many requirements carry it and the
other names its requirements carry. The name is the one most of the code's
requirements carry, ties broken alphabetically; a requirement without a name
carries its code. A last line labelled Unclassified gives how many requirements
have no context. The numbers count every requirement that is not deleted,
OBSOLETE ones included.

Read the list before recording with reverse-engineer record: give every new
requirement a listed code and leave the name out, or a new code and name that
no listed context has. requirements set-context and requirements update move a
requirement to another context.

--json prints the route's data: {"contexts": [{"code", "name", "other_names",
"requirement_count"}], "without_context": n}.`,
	}
	command.Flags().Bool("json", false, "emit the route's data object as JSON")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if cfg, err := commandConfig(); err == nil && cfg.SystemID <= 0 {
			return fmt.Errorf("requirements contexts requires a bound System; run 'modernpath init' to bind one")
		}
		env, err := load()
		if err != nil {
			return err
		}
		if env.SystemID <= 0 {
			return fmt.Errorf("requirements contexts requires a bound System; run 'modernpath init' to bind one")
		}
		status, body, err := env.get(fmt.Sprintf("/api/v1/systems/%d/requirements/contexts", env.SystemID))
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("requirement contexts read refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("requirement contexts response is missing its data object")
		}
		if machine, _ := cmd.Flags().GetBool("json"); machine {
			return writeCommandJSON(cmd, data)
		}
		return writeRequirementContexts(cmd, data)
	}
	return command
}

func writeRequirementContexts(cmd *cobra.Command, data map[string]any) error {
	contexts, _ := data["contexts"].([]any)
	without, _ := numberAsInt(data["without_context"])
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	for _, raw := range contexts {
		item, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("requirement contexts response contains an invalid context")
		}
		count, _ := numberAsInt(item["requirement_count"])
		line := fmt.Sprintf("%s\t%s\t%s", str(item, "code"), str(item, "name"), requirementCount(count))
		var others []string
		if names, ok := item["other_names"].([]any); ok {
			for _, name := range names {
				others = append(others, fmt.Sprint(name))
			}
		}
		if len(others) > 0 {
			line += "\tother names: " + strings.Join(others, ", ")
		}
		fmt.Fprintln(w, line)
	}
	fmt.Fprintf(w, "Unclassified\t\t%s without a context\n", requirementCount(without))
	return w.Flush()
}

func requirementCount(n int64) string {
	if n == 1 {
		return "1 requirement"
	}
	return fmt.Sprintf("%d requirements", n)
}
