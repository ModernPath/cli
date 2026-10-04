package rdd

import (
	"sort"
)

// BuildCommitBurstOp folds the recent git log into one commit_burst event per
// CLOSED day (today's burst is still growing — it lands once the day closes).
func BuildCommitBurstOp(commits []Commit, today string) (Op, bool) {
	byDay := map[string][]Commit{}
	var days []string
	for _, c := range commits {
		if len(c.Date) < 10 {
			continue
		}
		day := c.Date[:10]
		if day >= today {
			continue
		}
		if _, ok := byDay[day]; !ok {
			days = append(days, day)
		}
		byDay[day] = append(byDay[day], c)
	}
	if len(byDay) == 0 {
		return Op{}, false
	}
	sort.Sort(sort.Reverse(sort.StringSlice(days)))

	events := make([]any, 0, len(days))
	for _, day := range days {
		dayCommits := byDay[day]
		ids := []string{}
		seen := map[string]bool{}
		for _, c := range dayCommits {
			for _, id := range c.IDs {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
		related := make([]any, len(ids))
		for i, id := range ids {
			related[i] = map[string]any{"external_id": id, "type": "requirement"}
		}
		subjects := []any{}
		for i, c := range dayCommits {
			if i == 5 {
				break
			}
			subjects = append(subjects, c.Subject)
		}
		events = append(events, map[string]any{
			"occurred_at":         day + "T23:59:59.000000Z",
			"event_type":          "commit_burst",
			"subject_external_id": day,
			"subject_type":        "commit_burst",
			"source_tag":          "GIT:" + day,
			"related":             related,
			"payload": map[string]any{
				"count":    len(dayCommits),
				"head":     dayCommits[0].Hash, // newest first
				"subjects": subjects,
			},
			"dedupe_key": "commit_burst:" + day,
		})
	}

	return Op{
		Type: "emit_events",
		Payload: map[string]any{
			"external_id": "EVENTS-COMMITS-" + today,
			"actor":       actor,
			"events":      events,
		},
	}, true
}
