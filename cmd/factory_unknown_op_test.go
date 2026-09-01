package cmd

import "testing"

// REQ-CROSS-089: the server halts a batch on the first op type it does not
// recognise and 422s the whole thing (`sync_api_controller.ex:901`). So shipping
// a new op kind in the CLI before the server understands it does not degrade the
// sync — it stops it completely, for every workspace, silently, because the
// hooks are fire-and-forget.
//
// That is exactly what happened when `upsert_document` shipped ahead of its
// server half (`RUN:2026-08-13`): modernpath-v1's own sync had been failing at
// op_index 1387 with nothing landing.
func TestUnknownOpTypeIsRecognisedFromTheServerError(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
		want string
	}{
		{
			name: "the shape the server actually returns",
			body: map[string]any{"error": map[string]any{
				"op_index":    float64(1387),
				"external_id": "docs/03-architecture.md",
				"details":     map[string]any{"type": []any{"unknown op type upsert_document"}},
			}},
			want: "upsert_document",
		},
		{
			name: "a different unknown kind",
			body: map[string]any{"error": map[string]any{
				"details": map[string]any{"type": []any{"unknown op type upsert_widget"}},
			}},
			want: "upsert_widget",
		},
		{
			name: "an ordinary validation failure is NOT an unknown op",
			body: map[string]any{"error": map[string]any{
				"op_index": float64(3),
				"details":  map[string]any{"title": []any{"can't be blank"}},
			}},
			want: "",
		},
		{
			name: "no error at all",
			body: map[string]any{},
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := unknownOpType(tc.body); got != tc.want {
				t.Errorf("unknownOpType = %q, want %q", got, tc.want)
			}
		})
	}
}

// Dropping the ops the server cannot take must leave everything else intact and
// in order — a sync that quietly reordered or lost requirements would be a far
// worse failure than the one being worked around.
func TestDropOpsOfTypeKeepsEverythingElseInOrder(t *testing.T) {
	ops := []map[string]any{
		{"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-A-001"}},
		{"type": "upsert_document", "payload": map[string]any{"external_id": "docs/03-architecture.md"}},
		{"type": "upsert_epic", "payload": map[string]any{"external_id": "EPIC-A-001"}},
		{"type": "upsert_document", "payload": map[string]any{"external_id": "ARCHITECTURE.md"}},
		{"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-A-002"}},
	}
	kept, dropped := dropOpsOfType(ops, "upsert_document")
	if dropped != 2 {
		t.Errorf("dropped = %d, want 2", dropped)
	}
	want := []string{"REQ-A-001", "EPIC-A-001", "REQ-A-002"}
	if len(kept) != len(want) {
		t.Fatalf("kept %d ops, want %d", len(kept), len(want))
	}
	for i, id := range want {
		payload, _ := kept[i]["payload"].(map[string]any)
		if got := str(payload, "external_id"); got != id {
			t.Errorf("kept[%d] = %q, want %q — order must be preserved", i, got, id)
		}
	}
}
