package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var (
	docsOnly   bool
	filesOnly  bool
	limit      int
	searchJSON bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search documentation and code files",
	Long: `Search through documentation and analyzed code files via the ModernPath API.

Examples:
  modernpath search "authentication"
  modernpath search "user login" --docs-only
  modernpath search "error handling" --files-only
  modernpath search "API" --limit=20`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&searchJSON, "json", false, "Emit selected results as JSON")
	addRecordReadFlags(searchCmd)
	searchCmd.Flags().BoolVar(&docsOnly, "docs-only", false, "Search only documentation")
	searchCmd.Flags().BoolVar(&filesOnly, "files-only", false, "Search only file analyses")
	searchCmd.Flags().IntVar(&limit, "limit", 10, "Maximum results to return")
}

func runSearch(cmd *cobra.Command, args []string) error {
	// REQ-CROSS-405: binding and credential statements before any request.
	env, err := apiClientCredentialLoad()
	if err != nil {
		printError("%v\n", err)
		return err
	}

	if docsOnly && filesOnly {
		return fmt.Errorf("--docs-only and --files-only cannot be combined")
	}
	if limit < 1 {
		return fmt.Errorf("--limit must be positive")
	}
	allowedFields := []string{"id", "title", "summary", "tier", "angle", "relative_path", "language", "purpose"}
	if err := validateRecordSelection(cmd, allowedFields); err != nil {
		return err
	}
	query := strings.Join(args, " ")
	params := url.Values{"system_id": {strconv.Itoa(env.SystemID)}, "q": {query}, "limit": {strconv.Itoa(limit)}, "type": {"all"}}
	if docsOnly {
		params.Set("type", "docs")
	} else if filesOnly {
		params.Set("type", "files")
	}
	status, response, err := env.call("GET", "/api/search?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("search", status, response)
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("search response is missing data")
	}
	selectedDocs, err := projectSearchRows(cmd, anyRows(data["docs"]), []string{"id", "title", "tier", "angle"}, allowedFields, []string{"id", "title"})
	if err != nil {
		return err
	}
	selectedFiles, err := projectSearchRows(cmd, anyRows(data["files"]), []string{"id", "relative_path", "language"}, allowedFields, []string{"id", "relative_path"})
	if err != nil {
		return err
	}
	if searchJSON {
		return writeSearchJSON(cmd.OutOrStdout(), map[string]any{"docs": selectedDocs, "files": selectedFiles})
	}

	totalResults := 0

	// Display doc results
	if len(selectedDocs) > 0 {
		bold := color.New(color.Bold)
		bold.Printf("📚 Documentation (%d results)\n", len(selectedDocs))

		for _, row := range selectedDocs {
			fmt.Fprintf(cmd.OutOrStdout(), "%s [%s/%s] %s\n", str(row, "id"), str(row, "tier"), str(row, "angle"), str(row, "title"))
			printSearchDetails(cmd, row, "summary")
		}
		totalResults += len(selectedDocs)
	}

	// Display file results
	if len(selectedFiles) > 0 {
		bold := color.New(color.Bold)
		bold.Printf("📁 Files (%d results)\n", len(selectedFiles))

		for _, row := range selectedFiles {
			fmt.Fprintf(cmd.OutOrStdout(), "%s [%s]\n", str(row, "relative_path"), str(row, "language"))
			printSearchDetails(cmd, row, "purpose")
		}
		totalResults += len(selectedFiles)
	}

	if totalResults == 0 {
		printWarning("No results found for '%s'\n", query)
	} else {
		printSuccess("Found %d results\n", totalResults)
	}

	return nil
}

func anyRows(value any) []map[string]any {
	raw, _ := value.([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, value := range raw {
		if row, ok := value.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows
}

func projectSearchRows(cmd *cobra.Command, rows []map[string]any, defaults, allowed, required []string) ([]map[string]any, error) {
	selected, err := selectSearchItems(cmd, rows, defaults, allowed, required)
	if err != nil {
		return nil, err
	}
	for i := range selected {
		for key := range selected[i] {
			if _, exists := rows[i][key]; !exists && !containsField(required, key) {
				delete(selected[i], key)
			}
		}
	}
	return selected, nil
}

func containsField(fields []string, target string) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}

func printSearchDetails(cmd *cobra.Command, row map[string]any, field string) {
	if value, ok := row[field].(string); ok && value != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", value)
	}
}
