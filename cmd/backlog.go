package cmd

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// REQ-CROSS-423 (EPIC-CLI-023): `process backlog list` — the read the loop
// routes people to and no verb served. Backlog, gap and tooling records are
// store records like every other; this prints them by kind and disposition,
// newest first, and names `working-set pull <id>` as the read of one.
//
// The vocabulary is the store's (`Storage.Schema.BacklogRecord` exports it;
// the wire schema states it): a value outside it is refused here, before any
// request, naming the accepted values.

var (
	backlogKinds               = []string{"backlog", "gap", "tooling"}
	backlogGapKinds            = []string{"capability", "specification"}
	backlogDispositionWords    = []string{"OPEN", "DEFERRED"}
	backlogDispositionPrefixes = []string{"ROUTED to ", "REJECTED with ", "CLOSED by ", "ACCEPTED with "}
	backlogDispositionShape    = "OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>, CLOSED by <id>, ACCEPTED with <source>"
)

type backlogListOpts struct {
	kind        string
	disposition string
}

var backlogListFlags backlogListOpts

var processBacklogCmd = &cobra.Command{
	Use:   "backlog",
	Short: "Backlog, gap and tooling records",
}

var processBacklogListCmd = &cobra.Command{
	Use:   "list",
	Short: "List backlog, gap and tooling records, newest first",
	Long: `List the system's backlog, gap and tooling records — one line each: id,
kind, disposition, who raised it, when, title — newest first.

--kind backlog|gap|tooling narrows by kind; --disposition <word> narrows by
disposition prefix (OPEN, DEFERRED, ROUTED to <id>, REJECTED with <source>,
CLOSED by <id>, ACCEPTED with <source>). A value outside the vocabulary is
refused before any request. Read one record with working-set pull <id>;
change its disposition with author update --kind backlog … --source USER:…`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := authorEnv()
		if err != nil {
			return err
		}
		return processBacklogList(env, backlogListFlags, os.Stdout)
	},
}

func init() {
	processBacklogListCmd.Flags().StringVar(&backlogListFlags.kind, "kind", "", "backlog | gap | tooling")
	processBacklogListCmd.Flags().StringVar(&backlogListFlags.disposition, "disposition", "",
		"a disposition or its prefix: "+backlogDispositionShape)
	processBacklogCmd.AddCommand(processBacklogListCmd)
	processCmd.AddCommand(processBacklogCmd)
}

// validBacklogDispositionFilter accepts a whole word, a prefixed form with a
// target, or a prefix of either — `ROUTED` is the common way to filter.
func validBacklogDispositionFilter(value string) bool {
	if value == "" {
		return true
	}
	for _, w := range backlogDispositionWords {
		if strings.HasPrefix(w, value) || w == value {
			return true
		}
	}
	for _, p := range backlogDispositionPrefixes {
		if strings.HasPrefix(p, value) || strings.HasPrefix(value, p) {
			return true
		}
	}
	return false
}

func processBacklogList(env *factoryEnv, opts backlogListOpts, out io.Writer) error {
	if opts.kind != "" && !contains(backlogKinds, opts.kind) {
		return fmt.Errorf("--kind %q: expected %s", opts.kind, strings.Join(backlogKinds, "|"))
	}
	if !validBacklogDispositionFilter(opts.disposition) {
		return fmt.Errorf("--disposition %q: expected one of %s, or a prefix of one", opts.disposition, backlogDispositionShape)
	}
	path := fmt.Sprintf("/api/v1/sync/backlog?system_id=%d", env.SystemID)
	if opts.kind != "" {
		path += "&kind=" + url.QueryEscape(opts.kind)
	}
	if opts.disposition != "" {
		path += "&disposition=" + url.QueryEscape(opts.disposition)
	}
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return err
	}
	if status != 200 {
		return serverRefusal("backlog list", status, body)
	}
	raw, _ := dataOf(body)["backlog"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			rows = append(rows, m)
		}
	}
	// Newest first by raised_at (ISO-8601 sorts lexically); ties by id so the
	// order is stable across runs.
	sort.SliceStable(rows, func(i, j int) bool {
		ai, aj := str(rows[i], "raised_at"), str(rows[j], "raised_at")
		if ai != aj {
			return ai > aj
		}
		return str(rows[i], "external_id") < str(rows[j], "external_id")
	})
	for _, r := range rows {
		kind := str(r, "kind")
		if gk := str(r, "gap_kind"); kind == "gap" && gk != "" {
			kind = "gap/" + gk
		}
		fmt.Fprintf(out, "  %-22s %-18s %-26s %-28s %-10s %s\n",
			str(r, "external_id"), kind, fieldOr(r, "disposition", "—"),
			fieldOr(r, "raised_by", "—"), dateOf(str(r, "raised_at")), str(r, "title"))
	}
	fmt.Fprintf(out, "%d record(s) — read one with working-set pull <id>\n", len(rows))
	return nil
}

// dateOf keeps the date of an ISO-8601 timestamp; anything else verbatim.
func dateOf(ts string) string {
	if len(ts) >= 10 && ts[4] == '-' && ts[7] == '-' {
		return ts[:10]
	}
	if ts == "" {
		return "—"
	}
	return ts
}
