package rdd

import (
	"regexp"
	"sort"
	"strings"
)

// Collision is one external id that names a different thing locally than on the
// server.
type Collision struct {
	ID     string
	Local  string
	Served string
}

var collisionNoiseRe = regexp.MustCompile(`[^a-z0-9 ]+`)

// normaliseTitle absorbs the differences that are not a change of meaning:
// case, whitespace re-flow, markdown emphasis and trailing punctuation. Titles
// pass through several re-flows between ledger and server, and a collision
// detector that fired on those would be ignored within a day.
func normaliseTitle(s string) string {
	s = collisionNoiseRe.ReplaceAllString(strings.ToLower(s), " ")
	return strings.Join(strings.Fields(s), " ")
}

// Collisions reports ids present on both sides whose meanings differ.
//
// REQ-CROSS-171. Reconcile compares ids alone, so two branches that each minted
// REQ-CROSS-096 for a different requirement look like agreement: the id is
// built here and served there. On the server each such row is simply whoever
// synced last, with no conflict raised and no event marking the overwrite
// (process/86). This is the check that sees it.
//
// An id on only one side is NOT a collision — that is stranded or not-served,
// which Reconcile already reports, and duplicating it here would make one fault
// appear under two names.
func Collisions(local, served map[string]string) []Collision {
	var out []Collision
	for id, mine := range local {
		theirs, ok := served[id]
		if !ok {
			continue
		}
		if normaliseTitle(mine) == normaliseTitle(theirs) {
			continue
		}
		out = append(out, Collision{ID: id, Local: mine, Served: theirs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
