package cmd

import "testing"

// REQ-CROSS-136: `conflict` is the server refusing a change because a human
// edited that row on the platform. Counting it under a green "✓ ok:" invites the
// reader to skim past a refusal — the same failure as a green gate with
// baselined violations, or a passing suite with excluded tests.
func TestSyncSummaryFlagsConflicts(t *testing.T) {
	cases := []struct {
		name     string
		counts   map[string]int
		wantWarn bool
	}{
		{"ordinary sync", map[string]int{"unchanged": 1400, "updated": 6}, false},
		{"a refusal is not a success", map[string]int{"unchanged": 1400, "conflict": 3}, true},
		{"conflict alone", map[string]int{"conflict": 1}, true},
		{"nothing at all", map[string]int{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := syncHadConflicts(tc.counts); got != tc.wantWarn {
				t.Errorf("syncHadConflicts(%v) = %v, want %v", tc.counts, got, tc.wantWarn)
			}
		})
	}
}
