package authoring

import (
	"reflect"
	"strings"
	"testing"
)

// REQ-CROSS-313 (SR-CLI-0084), EPIC-CLI-008: the CLI-owned authoring grammar —
// render + parse over an item's editable body, with a closed vocabulary pinned
// by a render→parse→render round-trip. Mutable fields render as editable
// blocks; relations as typed entries (target, authority, direction); the
// timestamped header is excluded from the round-trip domain and projection
// blocks are carried as opaque read-only values, so parse(render(x)) == x and
// render(parse(render(x))) == render(x) are well-defined.
//
// RED first: the `authoring` package does not exist.

func sampleRecords() []Record {
	return []Record{
		// an epic with every mutable scalar + members + a read-only projection
		{
			Kind:        "epic",
			ExternalID:  "EPIC-RT-1",
			Fingerprint: "fp-epic-1",
			Scalars: []Scalar{
				{"title", "The round-trip epic"},
				{"description", "a multi-line\ndescription with two lines"},
				{"outcome_source", "USER:2026-09-02"},
				{"scope", "the in-scope surface"},
				{"generated_from", "manual"},
				{"owner", "core"},
			},
			Members: []string{"REQ-CROSS-1", "REQ-CROSS-2"},
			// impact_assessment and shared_context are map fields rendered read-only (#19).
			Projections: []Projection{
				{"status", "IN_PROGRESS"},
				{"impact_assessment", "{\n  \"risk\": \"high\"\n}"},
				{"shared_context", "shared decisions"},
				{"gates", "- ENTRY-1 (closed)\n- COMPLETION-1 (open)"},
			},
		},
		// a system requirement with the SR triple, source citations, typed relations
		{
			Kind:        "system",
			ExternalID:  "SR-RT-1",
			Fingerprint: "fp-sr-1",
			Scalars: []Scalar{
				{"title", "A system requirement"},
				{"description", "does one thing"},
				{"context", "CX"},
				{"context_name", "Context"},
				{"stage", "MVP"},
				{"priority", "must"},
				{"owner", "core"},
				{"detail_md", "the detail\nwith markdown *emphasis*"},
				{"release_note", ""},
				{"boundary", "the change boundary"},
				{"rationale", "why"},
				{"verification_method", "unit"},
			},
			SourceCitations: []string{"USER:2026-09-02", "CODE:core/author.ex:1"},
			Relations: []Relation{
				{"derives-from", "UR-RT-1", "confirmed"},
				{"derives-from", "UR-RT-2", "candidate"},
			},
			Scenarios: []string{"GIVEN x WHEN y THEN z"},
			Projections: []Projection{
				{"status", "TODO"},
				{"evidence", "no current lower evidence"},
			},
		},
		// a user requirement with scenarios and no relations
		{
			Kind:        "user",
			ExternalID:  "UR-RT-1",
			Fingerprint: "fp-ur-1",
			Scalars: []Scalar{
				{"title", "A user requirement"},
				{"description", "an outcome"},
				{"context", "CX"},
				{"context_name", "Context"},
				{"stage", "MVP"},
				{"priority", "should"},
				{"owner", "core"},
				{"detail_md", ""},
				{"release_note", "shipped in v1"},
			},
			SourceCitations: []string{"USER:2026-09-02"},
			Scenarios:       []string{"GIVEN a session WHEN it starts THEN it knows the phase", "GIVEN an edit WHEN pushed THEN it lands"},
			Projections:     []Projection{{"status", "IN_REVIEW"}},
		},
	}
}

func TestREQCROSS313RoundTripBodyIsLosslessForEveryKind(t *testing.T) {
	for _, rec := range sampleRecords() {
		body := Render(rec)

		parsed, err := Parse(rec.Kind, body)
		if err != nil {
			t.Fatalf("%s %s: parse(render(x)) errored: %v", rec.Kind, rec.ExternalID, err)
		}
		// The round-trip domain is the BODY: ExternalID and Fingerprint live in
		// the file header (the cmd layer sets them from the filename + served
		// fingerprint), so Parse legitimately leaves them empty. Every body field
		// must survive exactly.
		want := rec
		want.ExternalID = ""
		want.Fingerprint = ""
		if !reflect.DeepEqual(parsed, want) {
			t.Fatalf("%s %s: parse(render(x)) != x (body)\n got: %#v\nwant: %#v", rec.Kind, rec.ExternalID, parsed, want)
		}

		if got := Render(parsed); got != body {
			t.Fatalf("%s %s: render(parse(render(x))) != render(x)\n--- got ---\n%s\n--- want ---\n%s", rec.Kind, rec.ExternalID, got, body)
		}
	}
}

// A scalar value that itself contains a bare ``` code fence must survive the
// render→parse round-trip (#7). Before the fix, render emits a fixed 3-backtick
// fence and Parse ends the block at the first ``` line, truncating the value to
// the text before the inner fence; diff then sees a spurious change.
func TestREQCROSS313AScalarContainingACodeFenceRoundTripsExactly(t *testing.T) {
	fenced := "before the fence\n```\ncode inside a fence\n```\nafter the fence"
	rec := Record{
		Kind:        "system",
		ExternalID:  "SR-FENCE-1",
		Fingerprint: "fp",
		Scalars: []Scalar{
			{"title", "fenced"},
			{"detail_md", fenced},
		},
		Projections: []Projection{{"status", "TODO"}},
	}
	body := Render(rec)

	parsed, err := Parse(rec.Kind, body)
	if err != nil {
		t.Fatalf("parse(render(x)) errored on a fenced scalar: %v", err)
	}
	got := ""
	for _, s := range parsed.Scalars {
		if s.Key == "detail_md" {
			got = s.Value
		}
	}
	if got != fenced {
		t.Fatalf("a scalar containing a ``` fence did not round-trip:\n got: %q\nwant: %q", got, fenced)
	}
	if r2 := Render(parsed); r2 != body {
		t.Fatalf("render(parse(render(x))) != render(x) for a fenced scalar")
	}
}

func TestREQCROSS313ProjectionBlocksRoundTripVerbatimAndAreReadOnly(t *testing.T) {
	rec := sampleRecords()[1] // the SR with two projections
	body := Render(rec)

	// every projection's content survives verbatim
	for _, p := range rec.Projections {
		if !strings.Contains(body, p.Content) {
			t.Fatalf("projection %q content not rendered verbatim", p.Name)
		}
	}
	// projections are demarcated read-only (a marker the push engine keys on)
	if !strings.Contains(body, readOnlyMarker) {
		t.Fatalf("render carries no read-only projection marker %q", readOnlyMarker)
	}
	parsed, err := Parse(rec.Kind, body)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(parsed.Projections, rec.Projections) {
		t.Fatalf("projections did not round-trip: got %#v want %#v", parsed.Projections, rec.Projections)
	}
}

// The Go render's mutable-field set per kind must equal the wire's (SR-CLI-0081's
// Elixir constants). Drift between the two silently drops an editable field.
func TestREQCROSS313MutableFieldSetMatchesTheWire(t *testing.T) {
	want := map[string][]string{
		"system": {"title", "description", "context", "context_name", "stage", "priority", "owner", "detail_md", "release_note", "boundary", "rationale", "verification_method", "source_citations", "parent_external_ids"},
		"user":   {"title", "description", "context", "context_name", "stage", "priority", "owner", "detail_md", "release_note", "source_citations"},
		"epic":   {"title", "description", "outcome_source", "scope", "impact_assessment", "shared_context", "generated_from", "owner"},
	}
	for kind, exp := range want {
		if got := MutableFields(kind); !reflect.DeepEqual(got, exp) {
			t.Fatalf("MutableFields(%q) = %v, want %v", kind, got, exp)
		}
	}
}

// External PR #299 review (#14): the closed grammar must refuse what it cannot
// type — free text, an unknown editable heading, and a duplicate block — rather
// than silently skipping it while push reports success.
func TestREQCROSS313ParseRefusesFreeText(t *testing.T) {
	body := "## title\n```authoring:editable\nThe epic\n```\n\nsome stray free text\n"
	if _, err := Parse("epic", body); err == nil {
		t.Fatal("free text outside a block must be refused")
	}
}

func TestREQCROSS313ParseRefusesUnknownEditableHeading(t *testing.T) {
	body := "## titel\n```authoring:editable\ntypo heading\n```\n"
	if _, err := Parse("epic", body); err == nil {
		t.Fatal("an editable block whose heading is not a mutable field must be refused")
	}
}

func TestREQCROSS313ParseRefusesDuplicateBlock(t *testing.T) {
	body := "## title\n```authoring:editable\none\n```\n\n## title\n```authoring:editable\ntwo\n```\n"
	if _, err := Parse("epic", body); err == nil {
		t.Fatal("a duplicate block must be refused")
	}
}

// The typed clear marker: an `authoring:cleared` block on a scalar field parses
// into Record.Cleared (the explicit "empty this field" request), and never also
// becomes an editable scalar.
func TestParseClearedBlock(t *testing.T) {
	body := "## owner\n```authoring:cleared\n```\n"
	rec, err := Parse("system", body)
	if err != nil {
		t.Fatalf("a cleared block must parse: %v", err)
	}
	if !reflect.DeepEqual(rec.Cleared, []string{"owner"}) {
		t.Fatalf("expected Cleared=[owner], got %v", rec.Cleared)
	}
	if len(rec.Scalars) != 0 {
		t.Fatalf("a cleared field must not also be an editable scalar, got %v", rec.Scalars)
	}
}

func TestParseClearedBlockRejectsNonScalarField(t *testing.T) {
	// `relations` is not an editable scalar field, so clearing it is meaningless.
	body := "## relations\n```authoring:cleared\n```\n"
	if _, err := Parse("system", body); err == nil {
		t.Fatal("a cleared block on a non-scalar field must be refused")
	}
}

func TestParseClearedBlockRejectsContent(t *testing.T) {
	body := "## owner\n```authoring:cleared\nstill has a value\n```\n"
	if _, err := Parse("system", body); err == nil {
		t.Fatal("a cleared block that carries a value is contradictory and must be refused")
	}
}
