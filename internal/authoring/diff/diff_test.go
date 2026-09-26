package diff

import (
	"testing"

	"github.com/modernpath/cli/internal/authoring"
)

func TestDiffAcceptanceOrderDoesNotChangeContent(t *testing.T) {
	for _, tc := range []struct {
		name         string
		base, edited []string
		refused      bool
	}{
		{"reordered", []string{"C-1 — first", "C-2 — second"}, []string{"C-2 — second", "C-1 — first"}, false},
		{"text changed", []string{"C-1 — first", "C-2 — second"}, []string{"C-2 — changed", "C-1 — first"}, true},
		{"duplicate count changed", []string{"first", "first", "second"}, []string{"first", "second", "second"}, true},
		{"duplicates reordered", []string{"first", "first", "second"}, []string{"second", "first", "first"}, false},
		{"removed", []string{"first", "second"}, []string{"first"}, true},
		{"added", []string{"first"}, []string{"first", "second"}, true},
	} {
		patch, refusal := Diff(authoring.Record{Kind: "system", Scenarios: tc.base}, authoring.Record{Kind: "system", Scenarios: tc.edited})
		if (refusal != nil) != tc.refused {
			t.Errorf("%s: refusal = %v, want refused %v", tc.name, refusal, tc.refused)
		}
		if refusal == nil && !patch.Empty() {
			t.Errorf("%s: unchanged acceptance emitted a patch: %v", tc.name, patch)
		}
	}
}

// External PR #299 review (#20): deleting a mutable scalar or a read-only
// projection block must be refused (like a deleted relation), not silently
// ignored while other edits in the same file apply.
func TestDiffRefusesDeletedScalarBlock(t *testing.T) {
	base := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"}, {Key: "description", Value: "d"},
	}}
	edited := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"},
	}}
	if _, ref := Diff(base, edited); ref == nil {
		t.Fatal("deleting a mutable scalar block must be refused")
	}
}

func TestDiffRefusesDeletedProjectionBlock(t *testing.T) {
	base := authoring.Record{Kind: "system", Projections: []authoring.Projection{
		{Name: "status", Content: "TODO"}, {Name: "evidence", Content: "none"},
	}}
	edited := authoring.Record{Kind: "system", Projections: []authoring.Projection{
		{Name: "status", Content: "TODO"},
	}}
	if _, ref := Diff(base, edited); ref == nil {
		t.Fatal("deleting a read-only projection block must be refused")
	}
}

// An unsupported relation edit on an existing edge — a direction change or an
// authority downgrade — must be refused, not silently dropped as a no-op that
// leaves the file inconsistent with the server. The only supported edits are
// promoting candidate->confirmed and withdrawing with an explicit marker.
func TestDiffRefusesRelationAuthorityDowngrade(t *testing.T) {
	base := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "incoming", Target: "UR-1", Authority: "confirmed"},
	}}
	edited := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "incoming", Target: "UR-1", Authority: "candidate"},
	}}
	if _, ref := Diff(base, edited); ref == nil {
		t.Fatal("downgrading a confirmed relation to candidate must be refused")
	}
}

func TestDiffRefusesRelationDirectionChange(t *testing.T) {
	base := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "incoming", Target: "UR-1", Authority: "confirmed"},
	}}
	edited := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "outgoing", Target: "UR-1", Authority: "confirmed"},
	}}
	if _, ref := Diff(base, edited); ref == nil {
		t.Fatal("changing a relation's direction must be refused")
	}
}

func TestDiffConfirmsCandidateRelation(t *testing.T) {
	base := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "incoming", Target: "UR-1", Authority: "candidate"},
	}}
	edited := authoring.Record{Kind: "system", Relations: []authoring.Relation{
		{Direction: "incoming", Target: "UR-1", Authority: "confirmed"},
	}}
	p, ref := Diff(base, edited)
	if ref != nil {
		t.Fatalf("promoting candidate to confirmed must be accepted: %v", ref)
	}
	if len(p.Relations) != 1 || p.Relations[0].Mode != "confirm" || p.Relations[0].Target != "UR-1" {
		t.Fatalf("expected a single confirm op for UR-1, got %v", p.Relations)
	}
}

func TestDiffUnchangedRelationIsNoOp(t *testing.T) {
	rels := []authoring.Relation{{Direction: "incoming", Target: "UR-1", Authority: "confirmed"}}
	base := authoring.Record{Kind: "system", Relations: rels}
	edited := authoring.Record{Kind: "system", Relations: rels}
	p, ref := Diff(base, edited)
	if ref != nil {
		t.Fatalf("an unchanged relation must not be refused: %v", ref)
	}
	if len(p.Relations) != 0 {
		t.Fatalf("an unchanged relation must emit no op, got %v", p.Relations)
	}
}

// A once-nonempty optional scalar can be cleared through push with an explicit
// clear marker (Record.Cleared, from an authoring:cleared block); a bare blank
// stays refused as ambiguous.
func TestDiffClearMarkerEmptiesAScalar(t *testing.T) {
	base := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"}, {Key: "owner", Value: "core"},
	}}
	edited := authoring.Record{
		Kind:    "system",
		Scalars: []authoring.Scalar{{Key: "title", Value: "t"}},
		Cleared: []string{"owner"},
	}
	p, ref := Diff(base, edited)
	if ref != nil {
		t.Fatalf("an explicit clear marker must be accepted: %v", ref)
	}
	if v, ok := p.Fields["owner"]; !ok || v != "" {
		t.Fatalf("clear must post owner as empty, got %q ok=%v", v, ok)
	}
}

func TestDiffBareBlankStillRefused(t *testing.T) {
	base := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"}, {Key: "owner", Value: "core"},
	}}
	edited := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"}, {Key: "owner", Value: ""},
	}}
	if _, ref := Diff(base, edited); ref == nil {
		t.Fatal("a bare blank value is still ambiguous and must be refused")
	}
}

// A normal edit that keeps every block still diffs cleanly.
func TestDiffAcceptsAKeptEdit(t *testing.T) {
	base := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t"}, {Key: "description", Value: "d"},
	}}
	edited := authoring.Record{Kind: "system", Scalars: []authoring.Scalar{
		{Key: "title", Value: "t2"}, {Key: "description", Value: "d"},
	}}
	p, ref := Diff(base, edited)
	if ref != nil {
		t.Fatalf("a kept edit must not be refused: %v", ref)
	}
	if p.Fields["title"] != "t2" {
		t.Fatalf("the title edit must be in the patch, got %v", p.Fields)
	}
}
