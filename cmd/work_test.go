package cmd

import (
	"encoding/json"
	"testing"
)

func TestWorkInitiativeDisplayTitle(t *testing.T) {
	cases := []struct {
		name  string
		input workInitiative
		want  string
	}{
		{
			name:  "uses title from API",
			input: workInitiative{ID: 1, Title: "OAuth login"},
			want:  "OAuth login",
		},
		{
			name:  "falls back to legacy name field",
			input: workInitiative{ID: 2, Name: "Legacy epic"},
			want:  "Legacy epic",
		},
		{
			name:  "prefers title when both present",
			input: workInitiative{ID: 3, Title: "Current title", Name: "Old name"},
			want:  "Current title",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.input.displayTitle(); got != tc.want {
				t.Fatalf("displayTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWorkInitiativeListJSON(t *testing.T) {
	payload := `{
		"data": [
			{
				"id": 169,
				"title": "Improve auth flow",
				"status": "to_do",
				"workflow_phase": "discovery"
			}
		]
	}`

	var result struct {
		Data []workInitiative `json:"data"`
	}
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(result.Data) != 1 {
		t.Fatalf("expected 1 initiative, got %d", len(result.Data))
	}

	if got := result.Data[0].displayTitle(); got != "Improve auth flow" {
		t.Fatalf("displayTitle() = %q, want %q", got, "Improve auth flow")
	}
}
