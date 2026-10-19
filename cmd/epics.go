package cmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// REQ-PLN-192 (EPIC-REQ-SEARCH): find epics by name or meaning, on the same
// server engine as MCP's search_epics.

var epicsSearchLimit int
var epicsSearchJSON bool

var epicsCmd = &cobra.Command{
	Use:   "epics",
	Short: "Read and write Epics, specifications and saved plans",
}

var epicsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Find epics by name or meaning",
	Args:  cobra.MinimumNArgs(1),
	Long: `Find epics of the bound system by title, description or meaning.
Each hit shows its native ID, process status, name, app link and how it matched.
Read a saved plan with epics plan show <epic-id>.`,
	RunE: runEpicsSearch,
}

func init() {
	epicsSearchCmd.Flags().IntVar(&epicsSearchLimit, "limit", 10, "hits to return (1–25)")
	epicsSearchCmd.Flags().BoolVar(&epicsSearchJSON, "json", false, "emit the query and items as JSON")
	addRecordReadFlags(epicsSearchCmd)
	epicsCmd.AddCommand(epicsSearchCmd)
	rootCmd.AddCommand(epicsCmd)
}

func runEpicsSearch(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	allowed := []string{"id", "name", "title", "process_status", "url", "description", "matched", "similarity", "system"}
	if err := validateRecordSelection(cmd, allowed); err != nil {
		return err
	}
	env, err := cliEnvLoad()
	if err != nil {
		return err
	}
	data, err := syncSearchWithEnv(env, args, epicsSearchLimit, "/api/v1/sync/epics/search", "epic search", "epics search")
	if err != nil {
		return err
	}
	items := searchItems(data)
	if len(items) > 0 {
		appURL := readAppURL(&authenticatedClient{client: &http.Client{Timeout: 10 * time.Second}, token: env.token, baseURL: env.APIURL})
		base := appURL
		if base == "" {
			base = env.APIURL
		}
		for _, item := range items {
			path := "/epics/" + url.PathEscape(fmt.Sprint(item["id"]))
			if env.Auth != nil && env.Auth.WorkspaceID != "" {
				path = fmt.Sprintf("/workspaces/%s/systems/%d%s", url.PathEscape(env.Auth.WorkspaceID), env.SystemID, path)
			}
			item["url"] = strings.TrimRight(base, "/") + path
		}
	}
	defaults := []string{"id", "name", "process_status", "url"}
	items, err = selectSearchItems(cmd, items, defaults, allowed, []string{"id"})
	if err != nil {
		return err
	}
	if epicsSearchJSON {
		return writeSearchJSON(cmd.OutOrStdout(), map[string]any{"query": data["query"], "items": items})
	}
	return writeEpicSearchHuman(cmd.OutOrStdout(), items)
}

func writeEpicSearchHuman(out io.Writer, items []map[string]any) error {
	if len(items) == 0 {
		fmt.Fprintln(out, "No matching epics.")
		return nil
	}
	for _, item := range items {
		id := fmt.Sprintf("#%v", item["id"])
		fmt.Fprintf(out, "%s [%s] %s", id, orDash(str(item, "process_status")), str(item, "name"))
		if _, exists := item["matched"]; exists {
			fmt.Fprintf(out, " (%s)", searchMatched(item))
		}
		fmt.Fprintln(out)
		if link := str(item, "url"); link != "" {
			fmt.Fprintf(out, "  %s\n", link)
		}
		if description := str(item, "description"); description != "" {
			fmt.Fprintf(out, "  %s\n", description)
		}
	}
	return nil
}
