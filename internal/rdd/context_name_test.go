package rdd

import "testing"

// REQ-CROSS-043 (`USER:2026-08-12`): Mission Control showed eleven three-letter
// codes because the friendly name never left the workspace. It sits in the first
// line of every ledger, in two formats that both exist in the wild.
func TestLedgerContextNameParsesBothHeaderShapes(t *testing.T) {
	cases := []struct{ header, want, why string }{
		{"# REQUIREMENTS — AGT (Agent Platform)", "Agent Platform",
			"modernpath-v1's own ledgers put the name in parentheses"},
		{"# REQUIREMENTS — CROSS (cross-cutting platform work)", "cross-cutting platform work",
			"lower-case names are names too"},
		{"# ANL — Analytics · requirements ledger", "Analytics",
			"the reverse-engineering skill writes name-then-middot"},
		{"# CHT — Conversations & Assistants · requirements ledger", "Conversations & Assistants",
			"ampersands survive"},
		{"# SOL — Solutions catalog", "Solutions catalog",
			"no trailing middot is fine"},
		{"# REQUIREMENTS — PLT", "",
			"a header with no name yields none rather than a guess"},
		{"", "", "no header, no name"},
		{"## CON — Consortiums", "", "only the H1 counts"},
	}
	for _, c := range cases {
		if got := LedgerContextName(c.header); got != c.want {
			t.Errorf("%q → %q, want %q — %s", c.header, got, c.want, c.why)
		}
	}
}

// The name must reach the op, or Mission Control still shows a code.
func TestBuildRequirementOpCarriesTheContextName(t *testing.T) {
	reqs := ParseLedger("tasks/CON-REQUIREMENTS.md",
		"# CON — Consortiums · requirements ledger\n\n| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n| REQ-CON-001 | A thing happens | MVP | IN_REVIEW | code | — | — |\n")
	if len(reqs) != 1 {
		t.Fatalf("want 1 row, got %d", len(reqs))
	}
	if reqs[0].CtxName != "Consortiums" {
		t.Fatalf("extractor lost the name: %q", reqs[0].CtxName)
	}

	p := BuildRequirementOp(reqs[0]).Payload
	if p["context"] != "CON" {
		t.Fatalf("context code changed: %v", p["context"])
	}
	if p["context_name"] != "Consortiums" {
		t.Fatalf("op does not carry context_name: %v", p["context_name"])
	}

	// An unnamed ledger must omit the key, not send "", so content hashes of
	// existing rows do not move for workspaces that never named their contexts.
	bare := ParseLedger("tasks/PLT-REQUIREMENTS.md",
		"# REQUIREMENTS — PLT\n\n| ID | Title | Stage | Status | Source | Tests | Code |\n|---|---|---|---|---|---|---|\n| REQ-PLT-001 | A thing | MVP | DONE | code | — | — |\n")
	if _, present := BuildRequirementOp(bare[0]).Payload["context_name"]; present {
		t.Fatal("an unnamed context must omit the key, not send an empty string")
	}
}
