package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/modernpath/cli/internal/authoring"
)

func stringSlice(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// citationRefs renders each served source-citation for the authoring body.
// source_citations are {:array,:map} on the wire — commonly {kind, ref} — so
// stringSlice dropped them, hiding existing citations from author and reviewer
// and, on push, sending bare strings into a map column (#5). Each renders as
// "<kind>: <ref>" so citationMaps can reconstruct the map losslessly; a
// citation carrying only a source_tag or already a bare string is kept as-is.
func citationRefs(v any) []string {
	items, _ := v.([]any)
	out := []string{}
	for _, it := range items {
		switch c := it.(type) {
		case string:
			out = append(out, c)
		case map[string]any:
			ref := str(c, "ref")
			if ref == "" {
				ref = str(c, "source_tag")
			}
			if ref == "" {
				continue
			}
			if kind := str(c, "kind"); kind != "" {
				out = append(out, kind+": "+ref)
			} else {
				out = append(out, ref)
			}
		}
	}
	return out
}

// citationMaps turns each authored citation token back into the {kind, ref} map
// shape the source_citations column accepts. A "<kind>: <ref>" token (as
// citationRefs renders) splits on the first ": "; a bare token derives its kind
// from the tag prefix, as traceSources does for verdict sources — so a citation
// edit lands instead of being rejected as a bare string.
func citationMaps(refs []string) []any {
	out := make([]any, 0, len(refs))
	for _, r := range refs {
		kind, ref := "", r
		if i := strings.Index(r, ": "); i > 0 {
			kind, ref = r[:i], r[i+2:]
		} else {
			kind = strings.ToLower(strings.SplitN(r, ":", 2)[0])
		}
		out = append(out, map[string]any{"kind": kind, "ref": ref})
	}
	return out
}

func recordFromPayload(rec scopeRecord, members []string) authoring.Record {
	m := rec.payload
	out := authoring.Record{
		Kind:        rec.kind,
		ExternalID:  str(m, "external_id"),
		Fingerprint: str(m, "fingerprint"),
	}
	for _, f := range authoring.ScalarFields(rec.kind) {
		out.Scalars = append(out.Scalars, authoring.Scalar{Key: f, Value: str(m, f)})
	}
	if rec.kind == "epic" {
		// REQ-CROSS-382: the members block reads the store's declared membership
		// (SRs then URs) so a push computes its ops against what the store holds;
		// the selection's frozen copy is only the fallback for a read that serves
		// neither list.
		out.Members = servedMembers(m, members)
	} else {
		out.SourceCitations = citationRefs(m["source_citations"])
		out.Relations = relationsFromPayload(m["relations"])
		out.Scenarios = scenariosFromPayload(m)
	}
	statusKey := "work_status"
	if rec.kind == "epic" {
		statusKey = "process_status"
	}
	out.Projections = []authoring.Projection{{Name: "status", Content: str(m, statusKey)}}
	if rec.kind == "epic" {
		// impact_assessment and shared_context are structured (map) fields the flat
		// working-set grammar cannot round-trip as editable scalars; render them
		// read-only for review, like acceptance content (#19). Editing them through
		// the working set is a disclosed follow-up.
		for _, f := range []string{"impact_assessment", "shared_context"} {
			if c := mapFieldProjection(m[f]); c != "" {
				out.Projections = append(out.Projections, authoring.Projection{Name: f, Content: c})
			}
		}
		// REQ-CROSS-471: what the chat that created the epic found, as a
		// read-only list a planner or reviewer reads without the JSON.
		if found := foundInChat(m); len(found) > 0 {
			out.Projections = append(out.Projections, authoring.Projection{Name: "found_in_chat", Content: dashList(found)})
		}
	}
	return out
}

// reviewItemRecord is the read-only record of a review copy. It lists every
// stored citation, as the review bundle does; an editable record lists only
// the citations push can send back.
func reviewItemRecord(rec scopeRecord, members []string) authoring.Record {
	out := recordFromPayload(rec, members)
	if rec.kind != "epic" {
		out.SourceCitations = citationLabels(rec.payload["source_citations"])
	}
	return out
}

// mapFieldProjection renders a structured (map) mutable field's value for a
// read-only projection block: a string verbatim, anything else as indented JSON.
func mapFieldProjection(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		if b, err := json.MarshalIndent(t, "", "  "); err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", t)
	}
}

func relationsFromPayload(v any) []authoring.Relation {
	rels, _ := v.([]any)
	out := []authoring.Relation{}
	for _, r := range rels {
		rm, _ := r.(map[string]any)
		out = append(out, authoring.Relation{
			Direction: str(rm, "direction"),
			Target:    str(rm, "target"),
			Authority: str(rm, "authority"),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func scenariosFromPayload(m map[string]any) []string {
	// The UR read serves inline scenarios; the SR read serves criteria. Both are
	// served STRUCTURED ({given,when,then,statement}) — never a "text" key (#1) —
	// and are carried read-only for review (push refuses acceptance edits rather
	// than wiping the structure with a statement-only replace-set).
	for _, key := range []string{"scenarios", "criteria", "acceptance_scenarios"} {
		if items, ok := m[key].([]any); ok && len(items) > 0 {
			out := []string{}
			for _, it := range items {
				switch v := it.(type) {
				case string:
					out = append(out, v)
				case map[string]any:
					out = append(out, composeScenario(v))
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	return nil
}

// composeScenario renders a served acceptance criterion for display: its
// statement when it has one, else the assembled GIVEN/WHEN/THEN triple.
// REQ-CROSS-426: a line whose object carries an external_id is prefixed
// `<external_id> (<kind>) — ` so it can be named in a finding or replaced
// through `author update --criteria`. Every render — the flat pull, the
// review render and the authoring pull — comes through here, and push
// compares the authoring block against the store re-rendered through this
// same function, so the prefix round-trips without touching the grammar.
func composeScenario(v map[string]any) string {
	line := composeScenarioText(v)
	id := str(v, "external_id")
	if id == "" {
		return line
	}
	if kind := str(v, "kind"); kind != "" {
		return id + " (" + kind + ") — " + line
	}
	return id + " — " + line
}

func composeScenarioText(v map[string]any) string {
	if s := str(v, "statement"); s != "" {
		return s
	}
	parts := []string{}
	if g := str(v, "given"); g != "" {
		parts = append(parts, "GIVEN "+g)
	}
	if w := str(v, "when"); w != "" {
		parts = append(parts, "WHEN "+w)
	}
	if th := str(v, "then"); th != "" {
		parts = append(parts, "THEN "+th)
	}
	return strings.Join(parts, " ")
}
