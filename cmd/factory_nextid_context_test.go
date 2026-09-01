// EPIC-CLI-001 T7 cosmetics (REQ-CROSS-210, RUN:2026-08-18): `factory
// next-id REQ-SBX` answered "REQ-REQ-SBX-001" — the area argument must
// tolerate the full ledger-prefixed form a user naturally pastes.
package cmd

import "testing"

func TestNextIDContextAcceptsLedgerPrefixedForm(t *testing.T) {
	cases := map[string]string{
		"SBX":       "SBX",
		"sbx":       "SBX",
		"REQ-SBX":   "SBX",
		"req-cross": "CROSS",
	}
	for in, want := range cases {
		if got := normalizeNextIDContext(in); got != want {
			t.Errorf("normalizeNextIDContext(%q) = %q, want %q", in, got, want)
		}
	}
}
