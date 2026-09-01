package cmd

import (
	"reflect"
	"testing"
)

// REQ-PLN-135 §135.1 (EPIC-NEXT-005): every substring matching
// (REQ|EPIC)-[A-Z0-9]+-\d+ (case-insensitive) is returned upper-cased, in
// first-appearance order, deduplicated. No range expansion; the source text is
// discarded once its refs are out.

func TestExtractRefs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"feat/epic-next-005-focus-state", []string{"EPIC-NEXT-005"}},
		{"REQ-CROSS-270: CLI must validate", []string{"REQ-CROSS-270"}},
		{"fix: mix format", nil},
		{"REQ-CROSS-225..228", []string{"REQ-CROSS-225"}},
		{"touches REQ-PLN-133 and EPIC-NEXT-005 and REQ-PLN-133 again",
			[]string{"REQ-PLN-133", "EPIC-NEXT-005"}},
		{"lower epic-knw-001 done", []string{"EPIC-KNW-001"}},
		{"let's finish REQ-PLN-134 — the token is hunter2", []string{"REQ-PLN-134"}},
	}

	for _, c := range cases {
		got := ExtractRefs(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ExtractRefs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
