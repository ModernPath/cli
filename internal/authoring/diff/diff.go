// Package diff turns two parsed authoring records (the pulled snapshot vs the
// edited file) into one `author patch` op plan, or refuses an edit it cannot
// type (REQ-CROSS-314 / SR-CLI-0085). It reads only SR-CLI-0084's own grammar;
// it never parses free-form markdown, and every refusal names why.
package diff

import (
	"fmt"

	"github.com/modernpath/cli/internal/authoring"
)

// RelOp / MemberOp are one typed edge or membership change. Mode ∈
// declare | withdraw | confirm.
type RelOp struct{ Mode, Target string }
type MemberOp struct{ Mode, Target string }

// Patch is the op plan for one record. Only the non-empty parts ride the wire.
// Acceptance content is not here: it is read-only in the working-set file (a
// scenario edit is refused, not diffed into a wiping replace-set — #1).
type Patch struct {
	Fields       map[string]string // changed mutable scalars
	Citations    []string          // source_citations replace-set
	CitationsSet bool
	Relations    []RelOp
	Members      []MemberOp
}

// Empty reports whether the plan would change nothing (a no-op file).
func (p *Patch) Empty() bool {
	return len(p.Fields) == 0 && !p.CitationsSet &&
		len(p.Relations) == 0 && len(p.Members) == 0
}

// Refusal is a typed reason a file cannot be pushed.
type Refusal struct{ Detail string }

func (r *Refusal) Error() string { return r.Detail }

// Diff computes the op plan from base→edited, or the first refusal.
func Diff(base, edited authoring.Record) (*Patch, *Refusal) {
	p := &Patch{Fields: map[string]string{}}

	// A projection block is read-only: lifecycle status moves through
	// `author advance` and the gates, never a push.
	baseProj := map[string]string{}
	for _, pr := range base.Projections {
		baseProj[pr.Name] = pr.Content
	}
	editedProj := map[string]bool{}
	for _, pr := range edited.Projections {
		editedProj[pr.Name] = true
		if bv, ok := baseProj[pr.Name]; ok && bv != pr.Content {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"the read-only projection block `## %s` was edited (%q → %q); lifecycle status moves through `author advance`, not push",
				pr.Name, bv, pr.Content)}
		}
	}

	// Mutable scalars. Clearing a field to empty is ambiguous (PD-CLI008-5):
	// an explicit clear marker is required, never a bare blank.
	baseScalar := map[string]string{}
	for _, s := range base.Scalars {
		baseScalar[s.Key] = s.Value
	}
	present := map[string]bool{}
	for _, s := range edited.Scalars {
		present[s.Key] = true
		bv, existed := baseScalar[s.Key]
		if existed && bv != "" && s.Value == "" {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"field `%s` was cleared to empty — an empty value is ambiguous; to clear a field replace its `authoring:editable` block with an `authoring:cleared` block, never a bare blank", s.Key)}
		}
		if bv != s.Value {
			p.Fields[s.Key] = s.Value
		}
	}
	// The explicit clear marker: post the field as empty when it currently holds a
	// value. A cleared field counts as present, so the block-deletion guard below
	// does not fire for it.
	for _, key := range edited.Cleared {
		present[key] = true
		if bv, existed := baseScalar[key]; existed && bv != "" {
			p.Fields[key] = ""
		}
	}

	if !equalStrings(base.SourceCitations, edited.SourceCitations) {
		p.Citations = edited.SourceCitations
		p.CitationsSet = true
	}
	// Acceptance content is served STRUCTURED and applied as a replace-set keyed by
	// external_id; the flat scenario strings cannot round-trip that structure, so
	// pushing an edit would supersede the real criteria with statement-only rows
	// under synthetic ids (#1). It is visible for review but authored elsewhere —
	// an edit is refused, never silently wiped.
	if !equalStrings(base.Scenarios, edited.Scenarios) {
		return nil, &Refusal{Detail: "acceptance content was edited — scenarios/criteria are read-only in the working-set file (visible for review, authored through the acceptance path, not push)"}
	}

	if base.Kind == "epic" {
		mops, ref := memberOps(base.Members, edited.Members)
		if ref != nil {
			return nil, ref
		}
		p.Members = mops
	} else {
		rops, ref := relationOps(base.Relations, edited.Relations)
		if ref != nil {
			return nil, ref
		}
		p.Relations = rops
	}

	// Deleted generated blocks are an explicit refusal (#20), checked AFTER the
	// relation/member ops so that when a file drops several blocks at once the
	// relation/member refusal — which names the dropped edge and points at the
	// withdraw marker — wins.
	for name := range baseProj {
		if !editedProj[name] {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"the read-only projection block `## %s` was deleted — projections are carried verbatim, not removed by push",
				name)}
		}
	}
	for key := range baseScalar {
		if !present[key] {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"field `%s` block was deleted — to clear a field use an `authoring:cleared` block, do not remove its block",
				key)}
		}
	}
	return p, nil
}

// relationOps: a new target declares; a `- withdraw <target>` marker withdraws;
// a candidate promoted to confirmed confirms; a base edge neither kept nor
// withdrawn was removed by deleting its line — refused (the omission failure the
// EPIC-CLI-007 cold review caught). Order follows the edited file.
func relationOps(base, edited []authoring.Relation) ([]RelOp, *Refusal) {
	baseByTarget := map[string]authoring.Relation{}
	for _, b := range base {
		baseByTarget[b.Target] = b
	}
	ops := []RelOp{}
	kept := map[string]bool{}
	for _, e := range edited {
		if e.Direction == "withdraw" {
			kept[e.Target] = true
			ops = append(ops, RelOp{Mode: "withdraw", Target: e.Target})
			continue
		}
		kept[e.Target] = true
		b, existed := baseByTarget[e.Target]
		switch {
		case !existed:
			ops = append(ops, RelOp{Mode: "declare", Target: e.Target})
		case b.Authority == "candidate" && e.Authority == "confirmed":
			ops = append(ops, RelOp{Mode: "confirm", Target: e.Target})
		case b.Authority == e.Authority && b.Direction == e.Direction:
			// unchanged — no op
		default:
			// A direction change or an authority downgrade on an existing edge is
			// not a supported push op; leaving it a silent no-op would report a
			// no-op while the file diverges from the server. Refuse it explicitly.
			return nil, &Refusal{Detail: fmt.Sprintf(
				"relation to %s changed in an unsupported way (%s [%s] -> %s [%s]) — the only supported relation edits are promoting a candidate to confirmed and withdrawing with `- withdraw %s`; to change anything else, withdraw and re-declare",
				e.Target, b.Direction, b.Authority, e.Direction, e.Authority, e.Target)}
		}
	}
	for _, b := range base {
		if !kept[b.Target] {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"relation to %s was removed by deleting its line — withdraw it explicitly with `- withdraw %s`, never a bare deletion", b.Target, b.Target)}
		}
	}
	return ops, nil
}

func memberOps(base, edited []string) ([]MemberOp, *Refusal) {
	inEdited := map[string]bool{}
	inBase := map[string]bool{}
	for _, m := range base {
		inBase[m] = true
	}
	ops := []MemberOp{}
	for _, m := range edited {
		if trimmed := withdrawTarget(m); trimmed != "" {
			ops = append(ops, MemberOp{Mode: "withdraw", Target: trimmed})
			inEdited[trimmed] = true
			continue
		}
		inEdited[m] = true
		if !inBase[m] {
			ops = append(ops, MemberOp{Mode: "declare", Target: m})
		}
	}
	for _, m := range base {
		if !inEdited[m] {
			return nil, &Refusal{Detail: fmt.Sprintf(
				"member %s was removed by deleting its line — withdraw it explicitly with `- withdraw %s`, never a bare deletion", m, m)}
		}
	}
	return ops, nil
}

// A member withdraw marker is a `withdraw <id>` line (members render as bare ids).
func withdrawTarget(line string) string {
	const p = "withdraw "
	if len(line) > len(p) && line[:len(p)] == p {
		return line[len(p):]
	}
	return ""
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
