package rdd

import "testing"

// REQ-CROSS-123: a recorded approval is found whatever shape the record writes it in.
//
// EPIC-MAP-001 records its approval completely — approver, USER: date, gate id,
// decision — in a table under `## Human approval`. The matcher wanted the
// heading `## Approval` exactly, and its fallback wanted APPROVED at the start
// of a line, so a table row beginning `|` matched neither. The epic read as DONE
// without approval, and the violation was **baselined** rather than
// investigated, which is how a false positive becomes permanent.
func TestApprovalLineOfAcceptsRealRecordShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{
			name: "a table row under '## Human approval'",
			text: "## Human approval\n\n| Approval id | Approver | Decision |\n|---|---|---|\n" +
				"| APP-MAP-001 | Pasi (`USER:2026-07-21`) | **APPROVED** — the reframed Map is signed off. |\n",
			want: true,
		},
		{
			name: "the canonical '## Approval' section still works",
			text: "## Approval\nAPPROVED USER:2026-08-01 — signed off in chat.\n",
			want: true,
		},
		{
			name: "APPROVED at the start of a line still works",
			text: "some prose\n**APPROVED** USER:2026-08-01 — signed off.\n",
			want: true,
		},
		{
			// The source is what makes an approval an approval; a bare claim is not one.
			name: "APPROVED with no USER: source is not an approval",
			text: "## Human approval\n\n| APP-X | Pasi | **APPROVED** — looks fine. |\n",
			want: false,
		},
		{
			name: "a pending approval section is not an approval",
			text: "## Approval\n_pending — awaiting sign-off (USER:2026-08-01 requested)_\n",
			want: false,
		},
		{
			// SPEC-APPROVED is the specification gate, deliberately a different
			// decision from the implementation approval. `\bAPPROVED\b` matches
			// inside it — the hyphen is a word boundary — so widening the matcher
			// silently conflated the two gates until this case was added.
			name: "SPEC-APPROVED is not an implementation approval",
			text: "## Specification status\nSPEC-APPROVED — specs reviewed and accepted · USER:2026-08-05\n",
			want: false,
		},
		{
			// The real EPIC-KNW-001 shape: two rows in one table, the SPEC gate
			// and the implementation gate, verdict lowercase. The implementation
			// row is the one that counts, and the SPEC row must not stand in for
			// it — same approver, same table, different decision.
			name: "lowercase verdict in a two-row table, SPEC row first",
			text: "## Human approval\n| Approval id | Approver | Source | Decision |\n|---|---|---|---|\n" +
				"| SPEC-APPROVE-EPIC-KNW-001 | Pasi | `USER:2026-08-05` | approved |\n" +
				"| APPROVE-EPIC-KNW-001 | Pasi | `USER:2026-08-06` (\"approve\") | approved — deferrals routed |\n",
			want: true,
		},
		{
			// Only the SPEC row present: the implementation gate is still open.
			name: "a SPEC row alone is not an implementation approval",
			text: "## Human approval\n| Approval id | Approver | Source | Decision |\n|---|---|---|---|\n" +
				"| SPEC-APPROVE-EPIC-X | Pasi | `USER:2026-08-05` | approved |\n",
			want: false,
		},
		{
			// REQ-CROSS-126: widening the heading to "contains approval" made
			// "## Pre-approval audit" and "## Task outline (sharpened at
			// approval)" capture the match ahead of the real section. Here the
			// wrong section carries a USER: tag and the word approved in prose,
			// which is exactly how a false pass would look.
			name: "a section merely mentioning approval does not stand in",
			text: "## Pre-approval audit (`RUN:2026-07-22`)\n" +
				"Two audits found no drift; the work was approved for audit on `USER:2026-07-22`.\n\n" +
				"## Human approval\n_pending — not yet signed off_\n",
			want: false,
		},
		{
			// main's approvalHeadingRe is a deliberate allow-list —
			// "## Approval", "## Human approval", "## Completion approval" —
			// and requires the heading to END at the word. That is stricter than
			// an open `[a-z]+ approval` qualifier and excludes
			// "## Pre-approval audit" by construction, so it is kept.
			name: "an enumerated qualified heading matches",
			text: "## Completion approval\n| Approval id | Approver | Source | Decision |\n|---|---|---|---|\n| APP-X | Pasi | `USER:2026-08-01` | approved |\n",
			want: true,
		},
		{
			// The line-start anchor existed to stop a QUOTED approval in evidence
			// prose from counting — "heading said \"(✅ APPROVED USER:… )\"" is a
			// report ABOUT an approval, not a record of one. Widening to allow a
			// table row must not also readmit prose.
			name: "a quoted approval inside evidence prose does not count",
			text: "## Evidence\nEvidence: heading said \"(✅ APPROVED USER:2026-07-17 — keep both)\".\n",
			want: false,
		},
		{
			name: "a record with no approval at all",
			text: "## Tasks\n- TASK-1\n",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ApprovalLineOf(tc.text) != ""
			if got != tc.want {
				t.Fatalf("ApprovalLineOf found=%v, want %v (got %q)", got, tc.want, ApprovalLineOf(tc.text))
			}
		})
	}
}
