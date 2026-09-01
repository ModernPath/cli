package rdd

import "testing"

// REQ-CROSS-137: a qualified acceptance-criteria label still yields criteria.
// 62 ledger rows wrote "- **Acceptance criteria (as-built):**" and every one of
// them synced with zero criteria (RUN:2026-08-14). The qualifier carries meaning
// — "as-built", "deferred-scope — done-bar when BYOK is picked up" — so the
// parser accepts it rather than the ledgers dropping it.
func TestParseCriteriaAcceptsQualifiedLabel(t *testing.T) {
	for _, label := range []string{
		"- **Acceptance criteria:**",
		"- **Acceptance criteria (as-built):**",
		"- **Acceptance criteria (as built + tested):**",
		"- **Acceptance criteria (deferred-scope — done-bar when BYOK is picked up):**",
		"- **Acceptance criteria (BDD):**",
	} {
		detail := label + "\n  - GIVEN a thing WHEN it happens THEN it is recorded.\n- **Tests:** x_test.exs\n"
		got := ParseCriteria(detail, "REQ-CROSS-137")
		if len(got) != 1 {
			t.Errorf("label %q: got %d criteria, want 1", label, len(got))
		}
	}
}

// A label that merely starts with the words must NOT swallow a different field.
func TestParseCriteriaRejectsUnrelatedLabel(t *testing.T) {
	detail := "- **Acceptance criteria review notes:**\n  - not a criterion\n"
	if got := ParseCriteria(detail, "REQ-CROSS-137"); len(got) != 0 {
		t.Errorf("got %d criteria, want 0", len(got))
	}
}
