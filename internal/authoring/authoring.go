// Package authoring is the CLI-owned grammar for a pulled working-set item
// (REQ-CROSS-313 / SR-CLI-0084). It renders an item's editable body and parses
// only its own render back — a closed vocabulary pinned by a render→parse→render
// round-trip. Push (SR-CLI-0085) reads this grammar and refuses anything it
// cannot type; it never parses free-form markdown.
//
// Each block is a fenced region so a value's blank lines and markdown headings
// round-trip verbatim:
//
//	## <heading>
//	```authoring:<type>
//	<content>
//	```
//
// <type> ∈ editable (a mutable scalar) · citations · relations · scenarios ·
// members · readonly (a projection: lifecycle status, gates, evidence — never
// authored). The timestamped file header is NOT part of this grammar.
package authoring

import (
	"fmt"
	"strings"
)

const (
	editableFieldMarker = "```authoring:editable"
	readOnlyMarker      = "```authoring:readonly"
)

// Exported so the working-set command can assert a --for-review render carries
// no editable block and key its projection handling off the read-only marker.
const (
	EditableFieldMarker = editableFieldMarker
	ReadOnlyMarker      = readOnlyMarker
)

// Scalar is one mutable content field. Relation is a typed edge (target,
// authority candidate/confirmed/rejected, direction). Projection is an opaque
// read-only block carried verbatim.
type Scalar struct{ Key, Value string }
type Relation struct{ Direction, Target, Authority string }
type Projection struct{ Name, Content string }

type Record struct {
	Kind            string // "epic" | "user" | "system"
	ExternalID      string
	Fingerprint     string
	Scalars         []Scalar
	SourceCitations []string
	Relations       []Relation
	Scenarios       []string
	Members         []string
	Projections     []Projection
	// Cleared holds the keys of scalar fields an edit explicitly empties via an
	// `authoring:cleared` block — the typed clear marker, distinct from a bare
	// blank (which stays ambiguous and refused).
	Cleared []string
}

// mutableFields mirrors SR-CLI-0081's Elixir constants exactly (per kind); the
// parity test pins the two together so an editable field never drops silently.
var mutableFields = map[string][]string{
	"system": {"title", "description", "context", "context_name", "stage", "priority", "owner", "detail_md", "release_note", "boundary", "rationale", "verification_method", "source_citations", "parent_external_ids"},
	"user":   {"title", "description", "context", "context_name", "stage", "priority", "owner", "detail_md", "release_note", "source_citations"},
	"epic":   {"title", "description", "outcome_source", "scope", "impact_assessment", "shared_context", "generated_from", "owner"},
}

// MutableFields is the ordered mutable-field set for a kind.
func MutableFields(kind string) []string { return mutableFields[kind] }

// ScalarFields is the mutable set minus fields the render does NOT emit as
// editable scalars: source_citations and parent_external_ids (their own typed
// blocks), and the epic's impact_assessment and shared_context — structured
// (map) fields the flat grammar cannot round-trip, rendered read-only (#19). They
// stay in mutableFields (the wire parity set); only the editable render omits them.
func ScalarFields(kind string) []string {
	out := []string{}
	for _, f := range mutableFields[kind] {
		switch f {
		case "source_citations", "parent_external_ids", "impact_assessment", "shared_context":
			continue
		}
		out = append(out, f)
	}
	return out
}

// Render emits the editable authoring body. RenderReadOnly emits every block
// read-only (a --for-review view that is never pushed back).
func Render(rec Record) string         { return render(rec, false) }
func RenderReadOnly(rec Record) string { return render(rec, true) }

func render(rec Record, readOnly bool) string {
	var b strings.Builder
	styp := "editable"
	if readOnly {
		styp = "readonly"
	}
	for _, s := range rec.Scalars {
		writeBlock(&b, s.Key, styp, s.Value)
	}
	if len(rec.SourceCitations) > 0 {
		writeBlock(&b, "source_citations", listType("citations", readOnly), dashList(rec.SourceCitations))
	}
	if len(rec.Relations) > 0 {
		writeBlock(&b, "relations", listType("relations", readOnly), renderRelations(rec.Relations))
	}
	if len(rec.Scenarios) > 0 {
		writeBlock(&b, "scenarios", listType("scenarios", readOnly), dashList(rec.Scenarios))
	}
	if len(rec.Members) > 0 {
		writeBlock(&b, "members", listType("members", readOnly), dashList(rec.Members))
	}
	for _, p := range rec.Projections {
		writeBlock(&b, p.Name, "readonly", p.Content)
	}
	return b.String()
}

func listType(t string, readOnly bool) string {
	if readOnly {
		return "readonly"
	}
	return t
}

func writeBlock(b *strings.Builder, heading, typ, content string) {
	fence := fenceFor(content)
	fmt.Fprintf(b, "## %s\n%sauthoring:%s\n%s\n%s\n\n", heading, fence, typ, content, fence)
}

// fenceFor returns the backtick fence for a block: at least three, and always
// longer than the longest backtick run in the content, so a value that itself
// contains a ``` code fence cannot close the block early (#7). Parse counts the
// opening run and closes only on a line of exactly that many backticks.
func fenceFor(content string) string {
	longest, run := 0, 0
	for i := 0; i < len(content); i++ {
		if content[i] == '`' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	n := 3
	if longest+1 > n {
		n = longest + 1
	}
	return strings.Repeat("`", n)
}

func countLeadingBackticks(s string) int {
	n := 0
	for n < len(s) && s[n] == '`' {
		n++
	}
	return n
}

func renderRelations(rs []Relation) string {
	lines := make([]string, len(rs))
	for i, r := range rs {
		lines[i] = fmt.Sprintf("- %s %s [%s]", r.Direction, r.Target, r.Authority)
	}
	return strings.Join(lines, "\n")
}

func dashList(items []string) string {
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = "- " + it
	}
	return strings.Join(lines, "\n")
}

// Parse reads an authoring-mode body back into a Record. It errors on any block
// it cannot type — the "refuse what you cannot parse" contract.
func Parse(kind, body string) (Record, error) {
	rec := Record{Kind: kind}
	scalarOK := fieldSet(ScalarFields(kind))
	seen := map[string]bool{}
	lines := strings.Split(body, "\n")
	i := 0
	for i < len(lines) {
		if !strings.HasPrefix(lines[i], "## ") {
			// A blank separator (render emits one between blocks) is fine; any other
			// non-heading line is free text the closed grammar refuses (#14).
			if strings.TrimSpace(lines[i]) == "" {
				i++
				continue
			}
			return rec, fmt.Errorf(
				"line %d is outside any block (%q) — the authoring grammar is closed; free text is refused",
				i+1, lines[i])
		}
		heading := strings.TrimPrefix(lines[i], "## ")
		if seen[heading] {
			return rec, fmt.Errorf("block %q appears more than once", heading)
		}
		seen[heading] = true
		i++
		bticks := 0
		if i < len(lines) {
			bticks = countLeadingBackticks(lines[i])
		}
		if bticks < 3 || !strings.HasPrefix(lines[i][bticks:], "authoring:") {
			return rec, fmt.Errorf("block %q is missing its authoring fence", heading)
		}
		typ := strings.TrimPrefix(lines[i][bticks:], "authoring:")
		fence := strings.Repeat("`", bticks)
		i++
		var content []string
		for i < len(lines) && lines[i] != fence {
			content = append(content, lines[i])
			i++
		}
		if i >= len(lines) {
			return rec, fmt.Errorf("block %q is not closed", heading)
		}
		i++ // closing ```
		if i < len(lines) && lines[i] == "" {
			i++ // the single blank separator render emits
		}
		value := strings.Join(content, "\n")
		switch typ {
		case "editable":
			if !scalarOK[heading] {
				return rec, fmt.Errorf("block %q is not an editable field for a %s record", heading, kind)
			}
			rec.Scalars = append(rec.Scalars, Scalar{Key: heading, Value: value})
		case "citations":
			if heading != "source_citations" {
				return rec, fmt.Errorf("a citations block must be headed source_citations, not %q", heading)
			}
			rec.SourceCitations = parseDashList(content)
		case "relations":
			if heading != "relations" {
				return rec, fmt.Errorf("a relations block must be headed relations, not %q", heading)
			}
			rec.Relations = parseRelations(content)
		case "scenarios":
			if heading != "scenarios" {
				return rec, fmt.Errorf("a scenarios block must be headed scenarios, not %q", heading)
			}
			rec.Scenarios = parseDashList(content)
		case "members":
			if heading != "members" {
				return rec, fmt.Errorf("a members block must be headed members, not %q", heading)
			}
			rec.Members = parseDashList(content)
		case "cleared":
			// The typed clear marker: an empty block on an editable scalar field is
			// the explicit "empty this field" request, distinct from a bare blank
			// (which stays ambiguous and refused).
			if !scalarOK[heading] {
				return rec, fmt.Errorf("a cleared block %q is not an editable field for a %s record", heading, kind)
			}
			if strings.TrimSpace(value) != "" {
				return rec, fmt.Errorf(
					"a cleared block %q carries a value (%q) — a cleared block is the empty marker; edit its editable block to change the value instead",
					heading, value)
			}
			rec.Cleared = append(rec.Cleared, heading)
		case "readonly":
			// A read-only projection block carries an opaque server projection under
			// an arbitrary heading (status, gates, evidence, …); it is never editable.
			rec.Projections = append(rec.Projections, Projection{Name: heading, Content: value})
		default:
			return rec, fmt.Errorf("block %q has an unknown authoring type %q", heading, typ)
		}
	}
	return rec, nil
}

func fieldSet(fields []string) map[string]bool {
	s := make(map[string]bool, len(fields))
	for _, f := range fields {
		s[f] = true
	}
	return s
}

func parseDashList(lines []string) []string {
	out := []string{}
	for _, l := range lines {
		out = append(out, strings.TrimPrefix(l, "- "))
	}
	return out
}

func parseRelations(lines []string) []Relation {
	out := []Relation{}
	for _, l := range lines {
		l = strings.TrimPrefix(l, "- ")
		auth := ""
		if k := strings.LastIndex(l, "["); k >= 0 {
			auth = strings.TrimSuffix(strings.TrimSpace(l[k+1:]), "]")
			l = strings.TrimSpace(l[:k])
		}
		parts := strings.Fields(l)
		r := Relation{Authority: auth}
		if len(parts) >= 1 {
			r.Direction = parts[0]
		}
		if len(parts) >= 2 {
			r.Target = parts[1]
		}
		out = append(out, r)
	}
	return out
}
