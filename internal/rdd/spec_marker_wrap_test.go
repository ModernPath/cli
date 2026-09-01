package rdd

import "testing"

// REQ-CROSS-154: a SPEC-APPROVED marker whose USER: tag wraps onto the next line
// must still read as approved.
//
// Found on RUN:2026-08-14 by `factory reconcile`, not by any test:
// SPEC-APPROVE-EPIC-DIG-004 was built as an OPEN gate for an epic whose specs
// the user had approved on 2026-08-10 and whose implementation had already
// shipped. The marker is prose, prose wraps, and the tag landed on line two —
// so the gate queue would have asked for an approval that was already given.
//
// The failure direction is what makes it worth a test: degrading `approved` to
// `ready` produces a *plausible* gate rather than an error, and a queue of
// plausible gates is indistinguishable from a correct one.
func TestSpecApprovalMarkerUserTagMayWrap(t *testing.T) {
	for _, tc := range []struct {
		name    string
		section string
		want    string
	}{
		{
			// The real EPIC-DIG-004 wording that exposed this.
			name: "tag on the second line",
			section: "## Specification status\n" +
				"SPEC-APPROVED — the two open product decisions were put to the user and answered\n" +
				"before any code (`USER:2026-08-10`); the rest is grounded in the field audit below.\n",
			want: "approved",
		},
		{
			name: "tag on the marker line",
			section: "## Specification status\n" +
				"SPEC-APPROVED USER:2026-08-10 approved in chat\n",
			want: "approved",
		},
		{
			// Without a tag anywhere, approval has no provenance and must NOT
			// be taken on the marker's word — that is the guard this rule keeps.
			name: "no tag anywhere in the section",
			section: "## Specification status\n" +
				"SPEC-APPROVED — approved, honest, and sourced to nobody at all.\n",
			want: "ready",
		},
		{
			// SPEC-READY never consults the tag: a USER: citation in the
			// section is not an approval of the spec.
			name: "ready with an unrelated USER citation",
			section: "## Specification status\n" +
				"SPEC-READY — scope was set by the user (`USER:2026-08-01`), specs await review.\n",
			want: "ready",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, line := SpecApprovalLineOf(tc.section)
			if got != tc.want {
				t.Fatalf("marker = %q, want %q (line %q)", got, tc.want, line)
			}
		})
	}
}

// The classifier test above passed while BOTH builders still panicked, because
// each read the date back off the marker line with `tag[5:]` on an empty string.
// `MP_PARITY_ROOT=… go test -run Parity` is what caught it — a whole-workspace
// check finding what a unit test scoped to one function could not. Hence this:
// classify AND build, so the panic cannot return without a live workspace.
func TestSpecApprovalGateBuildsAnsweredFromWrappedTag(t *testing.T) {
	record := "# EPIC-DIG-004 — what just shipped\n\n" +
		"## Specification status\n" +
		"SPEC-APPROVED — the two open product decisions were put to the user and answered\n" +
		"before any code (`USER:2026-08-10`); the rest is grounded in the field audit below.\n"
	epic := Epic{
		ID:     "EPIC-DIG-004",
		Record: "epics/EPIC-DIG-004-what-just-shipped/EPIC.md",
		Specs:  []EpicSpec{{Rel: "epics/EPIC-DIG-004-what-just-shipped/specs/requirements.md", Name: "requirements", Content: "grounded"}},
	}

	op, ok := BuildSpecApprovalGateOp(epic, record)
	if !ok {
		t.Fatal("no gate op built for a folder-form epic with specs")
	}
	if state := op.Payload["state"]; state != "answered" {
		t.Fatalf("state = %v, want answered — an approved spec must not re-open its gate", state)
	}
	if got := op.Payload["answered_at"]; got != "2026-08-10T00:00:00.000000Z" {
		t.Fatalf("answered_at = %v, want the date from the wrapped USER: tag", got)
	}
	if got := op.Payload["source_tag"]; got != "USER:2026-08-10" {
		t.Fatalf("source_tag = %v — the approval must carry its provenance", got)
	}
}
