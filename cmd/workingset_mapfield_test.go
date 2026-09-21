package cmd

import (
	"strings"
	"testing"
)

// External PR #299 review (#19): impact_assessment and shared_context are map
// fields in the epic schema; rendered through str() they became EMPTY editable
// scalars (their content lost on read, and a text edit Ecto-rejected on push).
// They must render as read-only projections, like acceptance content.
func TestRecordFromPayloadRendersEpicMapFieldsReadOnly(t *testing.T) {
	rec := scopeRecord{kind: "epic", payload: map[string]any{
		"external_id":       "EPIC-M",
		"impact_assessment": map[string]any{"risk": "high", "area": "auth"},
		"process_status":    "IN_PROGRESS",
	}}
	out := recordFromPayload(rec, nil)

	for _, s := range out.Scalars {
		if s.Key == "impact_assessment" {
			t.Fatalf("impact_assessment must not be an editable scalar, got %q", s.Value)
		}
	}

	var proj string
	for _, p := range out.Projections {
		if p.Name == "impact_assessment" {
			proj = p.Content
		}
	}
	if proj == "" {
		t.Fatal("impact_assessment must render as a read-only projection")
	}
	if !strings.Contains(proj, "high") {
		t.Fatalf("impact_assessment projection must carry the map content, got %q", proj)
	}
}
