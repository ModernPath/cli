package rdd

import "testing"

// REQ-CROSS-141: sync is one-directional and deliberately not a replace-set, so
// the server accumulates entities no workspace builds. Two fully-sourced
// requirements sat there for a day in no branch or ref of the repository, while
// the next id the workspace would have issued was one of theirs (RUN:2026-08-14).
//
// The subtlety is telling STRANDED from FILTERED. The requirements endpoint
// serves REQ-* and not UR-*; the gates endpoint serves open gates and not
// answered ones. Comparing raw sets reports 118 UR rows and 67 answered gates as
// missing, which is noise that trains a reader to ignore the whole report.
func TestReconcileSeparatesStrandedFromFiltered(t *testing.T) {
	built := []string{"REQ-A-001", "REQ-A-002", "UR-A-900"}
	served := []string{"REQ-A-001", "REQ-A-003"}

	got := Reconcile(built, served, func(id string) bool { return len(id) > 4 && id[:4] == "REQ-" })

	if len(got.Stranded) != 1 || got.Stranded[0] != "REQ-A-003" {
		t.Errorf("stranded = %v, want [REQ-A-003]", got.Stranded)
	}
	if len(got.NotServed) != 1 || got.NotServed[0] != "REQ-A-002" {
		t.Errorf("built-but-unserved = %v, want [REQ-A-002]", got.NotServed)
	}
	if got.Filtered != 1 {
		t.Errorf("filtered = %d, want 1 (UR-A-900 is not served by this endpoint)", got.Filtered)
	}
}

func TestReconcileCleanIsClean(t *testing.T) {
	got := Reconcile([]string{"REQ-A-001"}, []string{"REQ-A-001"}, func(string) bool { return true })
	if len(got.Stranded) != 0 || len(got.NotServed) != 0 {
		t.Errorf("clean corpus reported differences: %+v", got)
	}
}

// The two lists are read by a person and compared against the previous run, so
// their order has to come from the ids and not from whatever order the server
// happened to answer in. Both inputs here are shuffled relative to each other
// and to their own sort order; without the sort the report reproduces the
// arrival order and two identical reconciliations diff against each other.
func TestTheReportIsOrderedByIdNotByArrival(t *testing.T) {
	built := []string{"REQ-A-030", "REQ-A-004", "REQ-A-021", "UR-A-900"}
	served := []string{"REQ-A-077", "REQ-A-009", "REQ-A-050"}
	isReq := func(id string) bool { return len(id) > 4 && id[:4] == "REQ-" }

	got := Reconcile(built, served, isReq)

	wantStranded := []string{"REQ-A-009", "REQ-A-050", "REQ-A-077"}
	wantNotServed := []string{"REQ-A-004", "REQ-A-021", "REQ-A-030"}
	for _, tc := range []struct {
		name string
		got  []string
		want []string
	}{
		{"stranded", got.Stranded, wantStranded},
		{"built-but-unserved", got.NotServed, wantNotServed},
	} {
		if len(tc.got) != len(tc.want) {
			t.Fatalf("%s = %v, want %v", tc.name, tc.got, tc.want)
		}
		for i := range tc.want {
			if tc.got[i] != tc.want[i] {
				t.Fatalf("%s = %v, want %v — the list follows arrival order, so an unchanged "+
					"estate reports differently on every run", tc.name, tc.got, tc.want)
			}
		}
	}
}
