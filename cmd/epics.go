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
	Short: "Find epics",
}

var epicsSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Find epics by name or meaning",
	Args:  cobra.MinimumNArgs(1),
	Long: `Find epics of the bound system by code, title, description or meaning, in
one ranked list.

Each hit shows its code (or #id when it has none), process status, name, app link and
how it matched: keyword, semantic, or both. Hits found both ways come first.
Pull an epic in full with working-set pull.`,
	RunE: runEpicsSearch,
}

func init() {
	epicsSearchCmd.Flags().IntVar(&epicsSearchLimit, "limit", 10, "hits to return (1–25)")
	epicsSearchCmd.Flags().BoolVar(&epicsSearchJSON, "json", false, "emit the query and items as JSON")
	epicsCmd.AddCommand(epicsSearchCmd)
	rootCmd.AddCommand(epicsCmd)
}

func runEpicsSearch(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	env, err := factoryEnvLoad()
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
	if epicsSearchJSON {
		return writeSearchJSON(cmd.OutOrStdout(), data)
	}
	return writeEpicSearchHuman(cmd.OutOrStdout(), searchItems(data))
}

func writeEpicSearchHuman(out io.Writer, items []map[string]any) error {
	if len(items) == 0 {
		fmt.Fprintln(out, "No matching epics.")
		return nil
	}
	for _, item := range items {
		id := str(item, "code")
		if id == "" {
			id = fmt.Sprintf("#%v", item["id"])
		}
		fmt.Fprintf(out, "%s [%s] %s (%s)\n", id, orDash(str(item, "process_status")), str(item, "name"), searchMatched(item))
		if link := str(item, "url"); link != "" {
			fmt.Fprintf(out, "  %s\n", link)
		}
		if description := str(item, "description"); description != "" {
			fmt.Fprintf(out, "  %s\n", description)
		}
	}
	return nil
}
