package rdd

// REQ-CROSS-245 (EPIC-CLI-003 T9): the parsers extract the declared value,
// not a formatting artifact. One subtest per approved clause, each with the
// negative fixture its widening must still reject.

import (
	"strings"
	"testing"
)

// §245.1 — an epic H1 joined with an ASCII hyphen still yields the title.
func TestEpicTitleAcceptsHyphenH1(t *testing.T) {
	cases := []struct {
		name, record, want string
	}{
		{"ascii hyphen", "# EPIC-T-001 - API key management\n\n## User outcome\n\nBody.\n", "API key management"},
		{"em dash", "# EPIC-T-001 — API key management\n\n## User outcome\n\nBody.\n", "API key management"},
		{"en dash", "# EPIC-T-001 – API key management\n\n## User outcome\n\nBody.\n", "API key management"},
		// negative: no spaced separator at all — the id stays the title.
		{"no separator", "# EPIC-T-001 API key management\n\n## User outcome\n\nBody.\n", "EPIC-T-001"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op := BuildEpicOp(Epic{ID: "EPIC-T-001"}, c.record)
			if got := op.Payload["title"]; got != c.want {
				t.Fatalf("title = %v, want %q", got, c.want)
			}
		})
	}
}

// §245.2 — a Stage cell holding a dash placeholder is absence, not a value.
func TestStageDashIsAbsence(t *testing.T) {
	for _, dash := range []string{"—", "-", "–"} {
		op := BuildRequirementOp(Req{ID: "REQ-T-001", Title: "t", Ctx: "T", Stage: dash})
		if got := op.Payload["stage"]; got != nil {
			t.Fatalf("stage for %q = %v, want nil", dash, got)
		}
	}
	// negative: a real stage survives.
	op := BuildRequirementOp(Req{ID: "REQ-T-001", Title: "t", Ctx: "T", Stage: "MVP"})
	if got := op.Payload["stage"]; got != "MVP" {
		t.Fatalf("stage = %v, want MVP", got)
	}
}

// §245.3 — an answered question/decision gate whose closure carries a
// USER:<date> tag dates its answer from the tag, never the placeholder.
func TestAnsweredAtFromUserTag(t *testing.T) {
	t.Run("OQ resolved with tag", func(t *testing.T) {
		op := BuildOQGateOp(OQ{ID: "OQ-T-1", Title: "q", Full: "Decided — USER:2026-08-17 by the owner.", Resolved: true})
		if got := op.Payload["answered_at"]; got != "2026-08-17T00:00:00.000000Z" {
			t.Fatalf("answered_at = %v, want the tag date", got)
		}
	})
	t.Run("OQ resolved without tag keeps the opened placeholder", func(t *testing.T) {
		op := BuildOQGateOp(OQ{ID: "OQ-T-2", Title: "q", Full: "Decided in review.", Resolved: true})
		if got := op.Payload["answered_at"]; got != "2026-07-01T00:00:00.000000Z" {
			t.Fatalf("answered_at = %v, want the opened placeholder", got)
		}
	})
	t.Run("RQ closed with tag", func(t *testing.T) {
		op := BuildRQGateOp(RQ{ID: "RQ-9001", State: "closed", Heading: "RQ-9001 — APPROVED", Body: "Fixed. USER:2026-08-18 sign-off.", Date: "2026-08-10"})
		if got := op.Payload["answered_at"]; got != "2026-08-18T00:00:00.000000Z" {
			t.Fatalf("answered_at = %v, want the tag date", got)
		}
	})
}

// §245.4 — a wrapped acceptance-criterion bullet lands whole.
func TestCriterionBulletWrapsAcrossLines(t *testing.T) {
	detail := strings.Join([]string{
		"- **Acceptance criteria:**",
		"  - GIVEN a valid, unique email WHEN RegisterUser THEN the user is created",
		"    and a confirmation email is sent.",
		"  - Second criterion statement.",
		"- **Tests:** `x_test.go`",
	}, "\n")
	criteria := ParseCriteria(detail, "REQ-T-001")
	if len(criteria) != 2 {
		t.Fatalf("criteria = %d, want 2", len(criteria))
	}
	first := criteria[0].(map[string]any)
	then, _ := first["then"].(string)
	if !strings.Contains(then, "confirmation email is sent") {
		t.Fatalf("wrapped THEN clause cut: %q", then)
	}
	// negative: the field head after the list is never absorbed.
	second := criteria[1].(map[string]any)
	if s, _ := second["statement"].(string); strings.Contains(s, "Tests:") {
		t.Fatalf("field head absorbed into criterion: %q", s)
	}
}

// §245.5 — a review-queue item under an ## heading builds like an ### item.
func TestReviewQueueAcceptsH2Headings(t *testing.T) {
	doc := strings.Join([]string{
		"## RQ-285 — Ranked first in the digest",
		"",
		"The decision text. DECISION NEEDED.",
		"",
		"## Not an item heading",
		"",
		"### RQ-286 — APPROVED",
		"",
		"Closed body. USER:2026-08-18",
	}, "\n")
	items := ParseReviewQueue(doc)
	byID := map[string]RQ{}
	for _, it := range items {
		byID[it.ID] = it
	}
	if _, ok := byID["RQ-285"]; !ok {
		t.Fatalf("H2 RQ-285 built no item; got %v", ids(items))
	}
	// negative: a non-RQ H2 still terminates the preceding block.
	if body := byID["RQ-285"].Body; strings.Contains(body, "Not an item heading") {
		t.Fatalf("H2 terminator lost: RQ-285 body absorbed the next section")
	}
	if _, ok := byID["RQ-286"]; !ok {
		t.Fatalf("### form regressed")
	}
}

// §245.6 — the inline `· **UR:** <id>` decoration feeds the parent fallback.
func TestInlineURDecorationFeedsFallback(t *testing.T) {
	ledger := strings.Join([]string{
		"# T ledger — Test Context (T)",
		"",
		"| ID | Title | Stage | Status | UR | Source | Tests | Code |",
		"|---|---|---|---|---|---|---|---|",
		"| REQ-T-001 | Something behaves | MVP | DONE |  | doc | — | — |",
		"",
		"### REQ-T-001 — Something behaves",
		"- **Status:** DONE · **Stage:** MVP · **UR:** UR-T-901 · **Source:** doc",
		"- **Statement:** It behaves.",
	}, "\n")
	reqs := ParseLedger("tasks/T-REQUIREMENTS.md", ledger)
	if len(reqs) != 1 {
		t.Fatalf("rows = %d, want 1", len(reqs))
	}
	if reqs[0].UR != "UR-T-901" {
		t.Fatalf("UR fallback = %q, want UR-T-901", reqs[0].UR)
	}
	// negative: a **URL:** label is not a UR declaration.
	if got := urFromDetail("- **Status:** DONE · **URL:** https://x"); got != "" {
		t.Fatalf("URL matched as UR: %q", got)
	}
}

// §245.7 — DONE / FIXED / ANSWERED close a review-queue block.
func TestReviewQueueClosedStates(t *testing.T) {
	for _, heading := range []string{"RQ-294 — DONE", "RQ-295 — FIXED in the same pass", "RQ-426 — ANSWERED by the owner"} {
		items := ParseReviewQueue("### " + heading + "\n\nBody. USER:2026-08-18\n")
		if len(items) != 1 || items[0].State != "closed" {
			t.Fatalf("%q parsed as %v, want closed", heading, states(items))
		}
	}
	// negative: UNANSWERED is not ANSWERED.
	items := ParseReviewQueue("### RQ-300 — UNANSWERED question\n\nBody.\n")
	if len(items) != 1 || items[0].State == "closed" {
		t.Fatalf("UNANSWERED read as closed")
	}
}

// §245.8 — superseding_ref only from an explicit "superseded by".
func TestSupersedingRefRequiresPhrase(t *testing.T) {
	// negative: an id mention without the phrase asserts nothing.
	op := BuildRequirementOp(Req{ID: "REQ-T-001", Title: "t", Ctx: "T", Status: "OBSOLETE",
		Source: "no umbrella — residuals tracked individually in REQ-PLT-004"})
	if got := op.Payload["superseding_ref"]; got != nil {
		t.Fatalf("superseding_ref = %v, want absent without the phrase", got)
	}
	op = BuildRequirementOp(Req{ID: "REQ-T-002", Title: "t", Ctx: "T", Status: "OBSOLETE",
		Source: "Superseded by REQ-PLT-004 after the pivot"})
	if got := op.Payload["superseding_ref"]; got != "REQ-PLT-004" {
		t.Fatalf("superseding_ref = %v, want REQ-PLT-004", got)
	}
}

// §245.9 — a parent reference must be UR-shaped; prose mints nothing and the
// cell text is preserved.
func TestParentRefsMustBeURShaped(t *testing.T) {
	op := BuildRequirementOp(Req{ID: "REQ-T-001", Title: "t", Ctx: "T",
		UR: "finding `RUN:2026-08-12` · fixed `RUN:2026-08-20`"})
	if got := op.Payload["parent_external_id"]; got != nil {
		t.Fatalf("parent_external_id = %v, want none from prose", got)
	}
	if got := op.Payload["parent_external_ids"]; got != nil {
		t.Fatalf("parent_external_ids = %v, want none from prose", got)
	}
	if !hasExtra(op, "ur_raw", "finding") {
		t.Fatalf("prose UR cell not preserved as ur_raw extra: %v", op.Payload["extra_columns"])
	}
	// a decorated reference still resolves to its token
	op = BuildRequirementOp(Req{ID: "REQ-T-002", Title: "t", Ctx: "T", UR: "UR-UI-901 (as-built)"})
	if got := op.Payload["parent_external_id"]; got != "UR-UI-901" {
		t.Fatalf("decorated ref = %v, want UR-UI-901", got)
	}
	// negative: a REQ-shaped id is not a user requirement.
	op = BuildRequirementOp(Req{ID: "REQ-T-003", Title: "t", Ctx: "T", UR: "REQ-UI-010"})
	if got := op.Payload["parent_external_id"]; got != nil {
		t.Fatalf("REQ-shaped token minted a parent: %v", got)
	}
	// a clean single token adds no ur_raw noise
	op = BuildRequirementOp(Req{ID: "REQ-T-004", Title: "t", Ctx: "T", UR: "UR-T-001"})
	if hasExtra(op, "ur_raw", "") {
		t.Fatalf("clean UR cell duplicated as ur_raw")
	}
}

func ids(items []RQ) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func states(items []RQ) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.State
	}
	return out
}

func hasExtra(op Op, name, contains string) bool {
	extras, _ := op.Payload["extra_columns"].([]map[string]any)
	for _, e := range extras {
		if e["name"] == name {
			v, _ := e["value"].(string)
			return strings.Contains(v, contains)
		}
	}
	return false
}
