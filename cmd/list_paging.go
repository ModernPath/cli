package cmd

// REQ-CROSS-447 (EPIC-CLI-TURNS): the lists that can return a whole system's
// records — process backlog list, factory gates, process findings list — print
// a bounded page in text. The server returns the whole array; the page is cut
// here. Under --json every record is printed unless --limit is given, so the
// documented script use keeps its shape.

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

const defaultListLimit = 50

type listPage struct {
	limit, offset int
}

func addPageFlags(c *cobra.Command, p *listPage) {
	c.Flags().IntVar(&p.limit, "limit", 0, fmt.Sprintf("print at most N records (default %d in text output; every record under --json)", defaultListLimit))
	c.Flags().IntVar(&p.offset, "offset", 0, "skip the first N records")
}

func (p listPage) validate() error {
	if p.limit < 0 || p.offset < 0 {
		return fmt.Errorf("--limit and --offset cannot be negative")
	}
	return nil
}

// window is the [start, end) slice of total records to print.
func (p listPage) window(total int, jsonOut bool) (int, int) {
	limit := p.limit
	if limit == 0 {
		limit = defaultListLimit
		if jsonOut {
			limit = total
		}
	}
	start := min(p.offset, total)
	return start, min(start+limit, total)
}

// emitPagedJSON prints a --json page: the rows under key, the total and
// whether records remain after this page (has_more), so a script can tell a
// short list from a cut one.
func emitPagedJSON[T any](out io.Writer, key string, rows []T, end, total int) error {
	if rows == nil {
		rows = []T{}
	}
	blob, err := json.MarshalIndent(map[string]any{key: rows, "total": total, "has_more": end < total}, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(blob))
	return nil
}

// pageFooter says how much of the list was shown and how to see more; empty
// when the page is the whole list.
func pageFooter(start, end, total int) string {
	if end-start == total {
		return ""
	}
	footer := fmt.Sprintf("showing %d of %d", end-start, total)
	if end < total {
		footer += fmt.Sprintf(" · --offset %d for more", end)
	}
	return footer + "\n"
}
