package rdd

import "testing"

// REQ-CROSS-150: a DEFERRED row's **Reason:** and a BLOCKED row's **Observed:**
// ARE its one-line description. Requiring a separate Statement made 6 rows across
// two workspaces sync as bare titles while carrying a well-formed reason and a
// tracking link — satisfying the ledger's own rule for those statuses
// (RUN:2026-08-14).
//
// Same precedent as REQ-CROSS-137: when a convention and a parser disagree and
// the convention is the more informative one, change the parser.
func TestDescriptionFallsBackToReasonAndObserved(t *testing.T) {
	statement := "- **Status:** DONE\n- **Statement:** The canonical one-liner.\n- **Reason:** ignored when a statement exists.\n"
	if got := ParseDescription(statement); got != "The canonical one-liner." {
		t.Errorf("Statement must win: got %q", got)
	}

	deferred := "- **Status:** DEFERRED\n- **Reason:** Not v1 — only if the indexing profile needs independent scaling.\n- **Tracking:** RQ-77\n"
	if got := ParseDescription(deferred); got != "Not v1 — only if the indexing profile needs independent scaling." {
		t.Errorf("Reason must describe a deferred row: got %q", got)
	}

	blocked := "- **Status:** BLOCKED on `Q-1`\n- **Observed `RUN:2026-08-14`:** three unlinked constants govern retention.\n"
	if got := ParseDescription(blocked); got != "three unlinked constants govern retention." {
		t.Errorf("Observed must describe a blocked row: got %q", got)
	}

	// A criterion still beats a reason nobody wrote.
	none := "- **Status:** PROPOSED\n- **Tests:** none\n"
	if got := ParseDescription(none); got != "" {
		t.Errorf("a blank beats an invented sentence: got %q", got)
	}
}

// REQ-CROSS-150: the same qualified-label defect REQ-CROSS-137 fixed for
// acceptance criteria. "**Statement (unconfirmed):**" is how a BLOCKED row marks
// a description it cannot yet assert — the qualifier carries meaning, and three
// rows synced bare because the parser demanded a bare label.
func TestDescriptionAcceptsQualifiedStatement(t *testing.T) {
	for _, label := range []string{
		"- **Statement:**",
		"- **Statement (unconfirmed):**",
		"- **Statement (as-built):**",
	} {
		detail := label + " The code clears left_at on the existing row.\n- **Tests:** x\n"
		if got := ParseDescription(detail); got != "The code clears left_at on the existing row." {
			t.Errorf("label %q: got %q", label, got)
		}
	}
	// Must not swallow a neighbouring field that merely starts with the word.
	if got := ParseDescription("- **Statement review notes:** not a statement\n"); got != "" {
		t.Errorf("unrelated label captured: %q", got)
	}
}
