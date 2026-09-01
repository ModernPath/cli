package rdd

import "testing"

// A ledger row's id prefix carries the UR/SR distinction PROCESS.md draws.
//
// Before this, BuildRequirementOp hardcoded kind "system": a reverse-engineered
// corpus of 163 user and 694 system requirements reached the store as 857
// system requirements, so a fifth of it was filed under the wrong evidence
// class. The server already routes by kind (Core.Sync.requirement_schema/1);
// only the ledger path could not say which.
func TestRequirementKindFromIDPrefix(t *testing.T) {
	for _, c := range []struct{ id, want string }{
		{"UR-AUTH-001", "user"},
		{"SR-AUTH-002", "system"},
		// The historical form keeps its meaning, which is what makes this
		// change safe to ship: no existing ledger changes kind or content hash.
		{"REQ-SYS-007", "system"},
	} {
		if got := RequirementKind(c.id); got != c.want {
			t.Errorf("RequirementKind(%q) = %q, want %q", c.id, got, c.want)
		}
	}
}

func TestBuildRequirementOpCarriesTheDeclaredKind(t *testing.T) {
	for _, c := range []struct{ id, want string }{
		{"UR-AUTH-001", "user"},
		{"SR-AUTH-002", "system"},
		{"REQ-SYS-007", "system"},
	} {
		op := BuildRequirementOp(Req{ID: c.id, Title: "t", Ctx: "AUTH", Status: "DERIVED"})
		if got := op.Payload["kind"]; got != c.want {
			t.Errorf("payload kind for %q = %v, want %q", c.id, got, c.want)
		}
	}
}

// The row and detail-block matchers must admit the new prefixes, or the rows
// never become Reqs and the kind above is never reached.
func TestLedgerMatchersAdmitURAndSR(t *testing.T) {
	rows := map[string]bool{
		"| UR-AUTH-001 | A builder signs in | AS-BUILT | DERIVED |": true,
		"| SR-AUTH-002 | Dev login refuses  | AS-BUILT | DERIVED |": true,
		"| REQ-SYS-007 | Knowledge Core     | MVP      | DONE    |": true,
		"| NFR-PERF-001 | not a requirement id                    |": false,
	}
	for line, want := range rows {
		if got := ledgerRowRe.MatchString(line); got != want {
			t.Errorf("ledgerRowRe(%q) = %v, want %v", line[:20], got, want)
		}
	}
	for _, h := range []string{"### UR-AUTH-001 — title", "### SR-AUTH-002 — title", "### REQ-SYS-007 — title"} {
		if !ledgerDetailRe.MatchString(h) {
			t.Errorf("ledgerDetailRe missed %q", h)
		}
	}
}
