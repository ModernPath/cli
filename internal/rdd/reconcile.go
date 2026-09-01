package rdd

import "sort"

// Reconciliation is the two-way difference between what a workspace builds and
// what a server serves, with the third category that makes the report readable.
type Reconciliation struct {
	// Stranded is on the server and in no workspace. These are the dangerous
	// ones: invisible to every local query, and the next id allocated from the
	// ledger's maximum can land on one (REQ-CROSS-140).
	Stranded []string
	// NotServed is built here and absent from the server's response. Usually a
	// state the endpoint does not return rather than a failure to land — say so
	// rather than implying loss.
	NotServed []string
	// Filtered counts built entities the endpoint does not serve at all, so the
	// reader knows the comparison excluded them deliberately.
	Filtered int
}

// Reconcile compares built against served, counting anything the endpoint does
// not serve rather than reporting it as missing.
//
// The served predicate is what keeps this honest. Without it a real corpus
// reports 118 UR rows and 67 answered gates as absent (RUN:2026-08-14) — all
// correct behaviour, and enough noise to make the whole report ignorable.
func Reconcile(built, served []string, serves func(id string) bool) Reconciliation {
	inServed := make(map[string]bool, len(served))
	for _, id := range served {
		inServed[id] = true
	}
	inBuilt := make(map[string]bool, len(built))

	var out Reconciliation
	for _, id := range built {
		inBuilt[id] = true
		if !serves(id) {
			out.Filtered++
			continue
		}
		if !inServed[id] {
			out.NotServed = append(out.NotServed, id)
		}
	}
	for _, id := range served {
		if !inBuilt[id] {
			out.Stranded = append(out.Stranded, id)
		}
	}
	sort.Strings(out.Stranded)
	sort.Strings(out.NotServed)
	return out
}
