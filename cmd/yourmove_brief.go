package cmd

// REQ-CROSS-276 (EPIC-NEXT-003) — the personal brief.
//
// `your-move` keeps regenerating its GATES.md projection (REQ-CROSS-215) and
// then prints the caller's ranked feed: a header naming the person, the release
// and the system; the top items with their ledger titles beside their ids and
// their chips; and three CTA lines that never dead-end. The served order IS the
// order — no flag re-scores anything.

import (
	"fmt"
	"io"
	"strings"
	"time"
)

type briefOpts struct {
	more    bool
	queue   bool
	domain  string
	release string
	now     time.Time
}

func briefRelease(opts briefOpts) string {
	if opts.release == "" {
		return "active"
	}
	return opts.release
}

// yourMoveWithBrief writes the projection first (unchanged), then the brief. A
// projection failure is a hard error (REQ-CROSS-215); a feed failure leaves the
// projection written, names the server's reason and exits non-zero (§276.7).
func yourMoveWithBrief(env *factoryEnv, opts briefOpts, out io.Writer) error {
	if err := yourMoveRun(env, opts.now); err != nil {
		return err
	}
	feed, err := fetchFeed(env, briefRelease(opts))
	if err != nil {
		return fmt.Errorf("brief unavailable: %v", err)
	}
	renderBrief(out, feed, env.SystemID, opts)
	return nil
}

// fetchFeed reads the caller's personal feed. A 200 without data.items is a
// changed envelope, refused — never read as "empty" (the fetchList rule).
func fetchFeed(env *factoryEnv, release string) (map[string]any, error) {
	path := fmt.Sprintf("/api/v1/feed?system_id=%d&view=personal&release=%s", env.SystemID, release)
	status, body, err := env.call("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("server %d: %v", status, body["error"])
	}
	data := dataOf(body)
	if _, ok := data["items"]; !ok {
		return nil, fmt.Errorf("server response has no %q — refusing to read a changed envelope as empty", "items")
	}
	return data, nil
}

func renderBrief(out io.Writer, data map[string]any, systemID int, opts briefOpts) {
	release := releaseLabel(data, opts)
	fmt.Fprintf(out, "Your move — %s · %s · system %d\n", str(feedMap(data, "person"), "name"), release, systemID)

	items := feedList(data, "items")
	shown := selectBriefItems(items, opts)
	noFilter := !opts.more && !opts.queue && opts.domain == ""

	if len(shown) == 0 && noFilter {
		fmt.Fprintf(out, "Nothing ranked for you in %s.\n", release)
	}
	for i, raw := range shown {
		renderBriefItem(out, i+1, feedMapOf(raw))
	}
	renderBriefCtas(out, data, items)
}

// The served list is already ranked; the flags slice or filter it, never rescore.
func selectBriefItems(items []any, opts briefOpts) []any {
	switch {
	case opts.queue:
		return items
	case opts.domain != "":
		var out []any
		for _, raw := range items {
			for _, d := range feedList(feedMapOf(raw), "domains") {
				if str(feedMapOf(d), "name") == opts.domain {
					out = append(out, raw)
					break
				}
			}
		}
		return out
	case opts.more:
		return briefSlice(items, 5, 10)
	default:
		return briefSlice(items, 0, 5)
	}
}

func briefSlice(items []any, from, to int) []any {
	if from > len(items) {
		from = len(items)
	}
	if to > len(items) {
		to = len(items)
	}
	return items[from:to]
}

func renderBriefItem(out io.Writer, n int, it map[string]any) {
	line := fmt.Sprintf("%d. [%s] %s", n, titleKind(str(it, "kind")), str(it, "external_id"))
	if title := str(it, "title"); title != "" {
		line += " — " + title
	}
	// Every applied term is a chip, in served order — except the kind (already in
	// the bracket) and ownership (the owned-domain chip below carries the owner).
	for _, raw := range feedList(it, "score_terms") {
		t := feedMapOf(raw)
		if k := str(t, "term"); k == "preset" || k == "ownership" {
			continue
		}
		if chip := str(t, "chip"); chip != "" {
			line += " · " + chip
		}
	}
	// Touched-domain chips: "D · owner" when owned, "D" alone otherwise
	// (REQ-UI-056's rule, in text); a placeholder is never rendered as an owner.
	for _, raw := range feedList(it, "domains") {
		d := feedMapOf(raw)
		name := str(d, "name")
		if name == "" {
			continue
		}
		if owner := feedMap(d, "owner"); owner != nil {
			if on := str(owner, "name"); on != "" {
				line += " · " + name + " · " + on
				continue
			}
		}
		line += " · " + name
	}
	fmt.Fprintln(out, line)

	if holds := feedList(it, "holds"); len(holds) > 0 {
		var parts []string
		for _, raw := range holds {
			h := feedMapOf(raw)
			id := str(h, "held_external_id")
			if title := str(h, "title"); title != "" {
				parts = append(parts, fmt.Sprintf("%s (%s)", id, title))
			} else {
				parts = append(parts, id)
			}
		}
		fmt.Fprintf(out, "   holds %s\n", strings.Join(parts, " · "))
	}
}

func renderBriefCtas(out io.Writer, data map[string]any, items []any) {
	total := feedNum(feedMap(data, "totals"), "in_scope")
	if total == 0 {
		total = len(items)
	}
	next := len(items) - 5
	if next < 0 {
		next = 0
	}
	if next > 5 {
		next = 5
	}
	fmt.Fprintf(out, "Next: modernpath your-move --more (%d more)\n", next)
	fmt.Fprintf(out, "Everything: modernpath your-move --queue (%d in scope)\n", total)

	domains := feedList(data, "domains")
	if len(domains) == 0 {
		fmt.Fprintln(out, "By domain: no domains resolved yet (EPIC-NEXT-002) (modernpath your-move --domain <name>)")
		return
	}
	var parts []string
	for _, raw := range domains {
		d := feedMapOf(raw)
		parts = append(parts, fmt.Sprintf("%s %d", str(d, "name"), feedNum(d, "count")))
	}
	fmt.Fprintf(out, "By domain: %s (modernpath your-move --domain <name>)\n", strings.Join(parts, " · "))
}

// --- small readers over the decoded JSON envelope ---

func feedMapOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func feedMap(m map[string]any, key string) map[string]any {
	return feedMapOf(m[key])
}

func feedList(m map[string]any, key string) []any {
	l, _ := m[key].([]any)
	return l
}

func feedNum(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func titleKind(k string) string {
	if k == "" {
		return ""
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

func releaseLabel(data map[string]any, opts briefOpts) string {
	rel := feedMap(data, "release")
	if active := feedList(rel, "active"); len(active) > 0 {
		if slug := str(feedMapOf(active[0]), "slug"); slug != "" {
			return slug
		}
	}
	if opts.release != "" {
		return opts.release
	}
	if scope := str(rel, "scope"); scope != "" {
		return scope
	}
	return "active"
}
