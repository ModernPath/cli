package rdd

import "testing"

// REQ-CROSS-145: a resolution marker counts at the edge of a title, not inside it.
// The heuristic matched RESOLVED or DONE anywhere in a heading, so a question
// whose SUBJECT is done-ness — "seven epics that are DONE without a recorded
// approval" — marked itself answered and never reached the queue (RUN:2026-08-14).
// Every genuine resolution in two real workspaces writes the marker as a prefix
// ("RESOLVED: …") or a suffix ("… — RESOLVED"); none mid-sentence.
func TestResolutionMarkerIsPositional(t *testing.T) {
	answered := []string{
		`Q-001 — What does "lines of code" mean on a system? — RESOLVED`,
		`Q-002 — RESOLVED: the 10-second timeout is an OAuth token exchange`,
		`Q-003 — the sync outage ✅`,
		`Q-004 — priced at $1.50? — DONE`,
	}
	open := []string{
		`Q-005 — What happens to seven epics that are DONE without a recorded approval?`,
		`Q-006 — Should a run be marked DONE before its evidence is recorded?`,
		`Q-007 — Is "resolved" the right status name for a closed question?`,
	}
	for _, title := range answered {
		if !oqResolvedTitleRe.MatchString(title) {
			t.Errorf("should be answered, was not: %q", title)
		}
	}
	for _, title := range open {
		if oqResolvedTitleRe.MatchString(title) {
			t.Errorf("should stay OPEN, was marked answered: %q", title)
		}
	}
}
