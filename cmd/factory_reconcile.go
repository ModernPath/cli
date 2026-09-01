package cmd

import (
	"fmt"
	"strings"

	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

// REQ-CROSS-141: sync is one-directional and deliberately not a replace-set, so
// the server accumulates entities no workspace builds and nothing surfaces them.
// Two fully-sourced requirements sat on a server for a day in no branch or ref
// of the repository, while the next id the workspace would have issued was one
// of theirs (RUN:2026-08-14). They were found by hand; this is that by command.
var factoryReconcileCmd = &cobra.Command{
	Use:   "reconcile",
	Short: "Compare what this workspace builds against what the server serves",
	Long: `Lists entities the server holds that no workspace builds ("stranded"),
and entities built here that the server's response does not include.

Stranded rows are the dangerous ones: invisible to every local query, and an id
allocated from the ledger's maximum can land on one. Entities the endpoint does
not serve — UR rows on the requirements endpoint, answered gates on the
DEFAULT gates listing (readable since REQ-CROSS-219 via state=answered, which
this reconcile deliberately does not consume: its subject is the open queue) —
are counted rather than listed, because reporting them as missing is noise
that trains a reader to ignore the report.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		env, err := factoryEnvLoad()
		if err != nil {
			return err
		}
		ops, _, err := env.workspaceOps()
		if err != nil {
			return err
		}

		localTitles := map[string]string{}
		var builtReqs, builtGates []string
		for _, op := range ops {
			payload, _ := op["payload"].(map[string]any)
			id, _ := payload["external_id"].(string)
			if id == "" {
				continue
			}
			switch op["type"] {
			case "upsert_requirement":
				builtReqs = append(builtReqs, id)
				if t, _ := payload["title"].(string); t != "" {
					localTitles[id] = t
				}
			case "upsert_gate":
				// the gates endpoint returns open gates only
				if state, _ := payload["state"].(string); state == "open" {
					builtGates = append(builtGates, id)
				}
			}
		}

		reqs, err := env.fetchIDs("/api/v1/sync/requirements?system_id=%d&limit=3000", "requirements")
		if err != nil {
			return err
		}
		gates, err := env.fetchIDs("/api/v1/sync/gates?system_id=%d&limit=3000", "gates")
		if err != nil {
			return err
		}

		// The requirements endpoint serves REQ-* and not UR-*; every gate we
		// compare is already scoped to open.
		clean := true
		clean = report("requirements", rdd.Reconcile(builtReqs, reqs,
			func(id string) bool { return strings.HasPrefix(id, "REQ-") })) && clean
		clean = report("open gates", rdd.Reconcile(builtGates, gates,
			func(string) bool { return true })) && clean

		// REQ-CROSS-171: ids agreeing is not meanings agreeing. Several branches
		// sync to one system with no id coordination, and the server keeps
		// whoever synced last — silently, with no event. Reconcile compares ids,
		// so only this check can see that two branches mean different
		// requirements by the same id.
		servedTitles, err := env.fetchTitles("/api/v1/sync/requirements?system_id=%d&limit=3000", "requirements")
		if err != nil {
			return err
		}
		if cols := rdd.Collisions(localTitles, servedTitles); len(cols) > 0 {
			clean = false
			fmt.Printf("  id collisions  %d — this id means something else on the server\n", len(cols))
			for _, c := range cols {
				fmt.Printf("      %s\n        here:   %s\n        server: %s\n", c.ID, trimTo(c.Local, 68), trimTo(c.Served, 68))
			}
			fmt.Println("  Renumber locally before syncing: a sync overwrites the server's meaning")
			fmt.Println("  without raising a conflict. `modernpath factory next-id` allocates above both.")
		}

		if clean {
			fmt.Println("✓ nothing stranded — the server holds nothing this workspace does not build")
		}
		return nil
	},
}

func report(kind string, r rdd.Reconciliation) bool {
	if len(r.Stranded) == 0 && len(r.NotServed) == 0 {
		fmt.Printf("  %-14s in step%s\n", kind, filteredNote(r))
		return true
	}
	fmt.Printf("  %-14s %d stranded · %d built but not returned%s\n",
		kind, len(r.Stranded), len(r.NotServed), filteredNote(r))
	for _, id := range r.Stranded {
		fmt.Printf("      STRANDED  %s — on the server, in no workspace\n", id)
	}
	for _, id := range r.NotServed {
		fmt.Printf("      absent    %s — built here, not in the server's response\n", id)
	}
	return len(r.Stranded) == 0
}

func filteredNote(r rdd.Reconciliation) string {
	if r.Filtered == 0 {
		return ""
	}
	return fmt.Sprintf("  (%d not served by this endpoint, excluded)", r.Filtered)
}

func init() {
	factoryCmd.AddCommand(factoryReconcileCmd)
}

// fetchIDs pulls external_ids from a sync read endpoint. The response shape is
// {"data": {"<key>": [ {...}, ... ]}} — the same envelope every sync GET uses.
func (e *factoryEnv) fetchIDs(pathFmt, key string) ([]string, error) {
	status, body, err := e.call("GET", fmt.Sprintf(pathFmt, e.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", key, status)
	}
	data, _ := body["data"].(map[string]any)
	rows, _ := data[key].([]any)
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			if id, _ := m["external_id"].(string); id != "" {
				out = append(out, id)
			}
		}
	}
	return out, nil
}

// fetchTitles is fetchIDs plus the title, for the collision check. The server
// is the only place that can answer "does this id already mean something else",
// so the read has to happen here rather than in a workspace-local gate.
func (e *factoryEnv) fetchTitles(pathFmt, key string) (map[string]string, error) {
	status, body, err := e.call("GET", fmt.Sprintf(pathFmt, e.SystemID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", key, status)
	}
	data, _ := body["data"].(map[string]any)
	rows, _ := data[key].([]any)
	out := make(map[string]string, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			id, _ := m["external_id"].(string)
			title, _ := m["title"].(string)
			if id != "" {
				out[id] = title
			}
		}
	}
	return out, nil
}

func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
