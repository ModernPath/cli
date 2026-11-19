package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCase e43aa777-9ce0-42d9-8136-2be28704a8fb
func TestEpicPlanHistorySummaryHasChangedOfEachPlanSave(t *testing.T) {
	changed := map[string]any{
		"parts":           []any{"criteria", "traces"},
		"requirement_ids": []any{},
		"criterion_ids":   []any{"0b827c64-2c01-4d88-86f9-50408563ed06"},
	}
	items := []any{
		map[string]any{"id": "e2", "action": "plan_saved", "inserted_at": "2026-11-17T10:00:00Z", "user_name": "Editor",
			"details": map[string]any{"description": "Epic plan saved.", "changed": changed}},
		map[string]any{"id": "e1", "action": "plan_saved", "inserted_at": "2026-11-01T09:00:00Z", "user_name": "Editor",
			"details": map[string]any{"description": "Epic plan saved."}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items, "total_count": 2, "has_more": false, "limit": 20, "offset": 0}})
	}))
	defer server.Close()
	cobraWorkspace(t, server)

	out, err := runRoot(t, "epics", "plan", "history", "42", "--json")
	if err != nil {
		t.Fatalf("history: %s %v", out, err)
	}
	var page struct {
		Items []map[string]json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal([]byte(out), &page); err != nil || len(page.Items) != 2 {
		t.Fatalf("history output: %s %v", out, err)
	}
	var got any
	if raw, has := page.Items[0]["changed"]; !has {
		t.Fatalf("the summary of a plan_saved item has no changed: %s", out)
	} else if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(changed)
	if have, _ := json.Marshal(got); string(have) != string(want) {
		t.Fatalf("changed is not as the server returned it: got %s want %s", have, want)
	}
	if raw, has := page.Items[1]["changed"]; !has || string(raw) != "null" {
		t.Fatalf("changed is not null for a plan_saved item whose details have none: %s", out)
	}
}
