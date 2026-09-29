package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

// REQ-PLN-192 (EPIC-REQ-SEARCH): find requirements by name or meaning, on the
// same server engine as MCP's search_requirements and chat.

const syncSearchMaxLimit = 25

var requirementsSearchLimit int
var requirementsSearchJSON bool

var requirementsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Find requirements and test cases by name or meaning",
	Args:  cobra.MinimumNArgs(1),
	Long: `Find user requirements, system requirements and test cases of the bound
system by id, display reference (UR-5, SR-12, TC-8), title, description or
meaning, in one ranked list.

Each hit shows its external id (or its display reference when it has none),
kind, work status, name and how it matched: exact, semantic, or both. Hits
found both ways come first. DERIVED requirements and unconfirmed test cases
are found by exact match only and are marked candidate.

For exact status filters use requirements list; for one requirement in full
use working-set pull.`,
	RunE: runRequirementsSearch,
}

func init() {
	requirementsSearchCmd.Flags().IntVar(&requirementsSearchLimit, "limit", 10, "hits to return (1–25)")
	requirementsSearchCmd.Flags().BoolVar(&requirementsSearchJSON, "json", false, "emit the query and items as JSON")
	requirementsCmd.AddCommand(requirementsSearchCmd)
}

func runRequirementsSearch(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	data, err := syncSearch(args, requirementsSearchLimit, "/api/v1/sync/requirements/search", "requirement search", "requirements search")
	if err != nil {
		return err
	}
	if requirementsSearchJSON {
		return writeSearchJSON(cmd.OutOrStdout(), data)
	}
	return writeRequirementSearchHuman(cmd.OutOrStdout(), searchItems(data))
}

// syncSearch validates the query and limit, calls one search route for the
// bound system and returns the response's data object. A 404 whose body says
// "system not found" is an unknown system; any other 404 is a server without
// the route.
func syncSearch(args []string, limit int, route, what, command string) (map[string]any, error) {
	query := strings.TrimSpace(strings.Join(args, " "))
	if query == "" {
		return nil, fmt.Errorf("the search text must not be empty")
	}
	if limit < 1 || limit > syncSearchMaxLimit {
		return nil, fmt.Errorf("--limit must be between 1 and %d", syncSearchMaxLimit)
	}

	env, err := factoryEnvLoad()
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"system_id": {fmt.Sprint(env.SystemID)},
		"q":         {query},
		"limit":     {fmt.Sprint(limit)},
	}
	status, body, err := env.call("GET", route+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		if searchErrorMessage(body) == "system not found" {
			return nil, fmt.Errorf("system %d is not known to the server (HTTP 404); check the workspace binding with modernpath status", env.SystemID)
		}
		return nil, fmt.Errorf("server does not support %s (HTTP 404); update the server before using %s", what, command)
	}
	if status != 200 {
		return nil, serverRefusal("", status, body)
	}
	return dataOf(body), nil
}

func searchErrorMessage(body map[string]any) string {
	if errVal, ok := body["error"].(map[string]any); ok {
		return str(errVal, "message")
	}
	return ""
}

func searchItems(data map[string]any) []map[string]any {
	raw, _ := data["items"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}

func writeSearchJSON(out io.Writer, data map[string]any) error {
	if _, ok := data["items"]; !ok {
		data["items"] = []any{}
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(data)
}

func searchMatched(item map[string]any) string {
	raw, _ := item["matched"].([]any)
	parts := make([]string, 0, len(raw))
	for _, m := range raw {
		if s, ok := m.(string); ok {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func writeRequirementSearchHuman(out io.Writer, items []map[string]any) error {
	if len(items) == 0 {
		fmt.Fprintln(out, "No matching requirements.")
		return nil
	}
	for _, item := range items {
		id := str(item, "external_id")
		if id == "" {
			id = str(item, "display_id")
		}
		fmt.Fprintf(out, "%s [%s / %s] %s (%s)", id, orDash(str(item, "requirement_kind")),
			orDash(str(item, "work_status")), str(item, "name"), searchMatched(item))
		if candidate, _ := item["candidate"].(bool); candidate {
			fmt.Fprint(out, " candidate")
		}
		fmt.Fprintln(out)
		if description := str(item, "description"); description != "" {
			fmt.Fprintf(out, "  %s\n", description)
		}
	}
	return nil
}
