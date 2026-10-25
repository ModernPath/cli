package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRDDEpicActionsSendOnlyDecisionFields(t *testing.T) {
	for _, action := range []string{"approve-implementation", "approve-completion", "return-to-implementation", "return-to-planning"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload) != 1 || payload["reason"] != "USER: explicit decision" {
					t.Errorf("action uploaded more than the decision: %v", payload)
				}
				json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"process_status": "IN_PROGRESS"}})
			}))
			defer server.Close()
			command := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetIn(bytes.NewBufferString(""))
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs([]string{action, "308", "--reason", "USER: explicit decision", "--json"})
			if err := command.Execute(); err != nil {
				t.Fatalf("current-state action required a bundle: %v", err)
			}
			if calls != 1 {
				t.Fatalf("action calls = %d, want 1", calls)
			}
		})
	}
}

func TestRDDReverseAcceptanceSendsOnlySelectionAndDecision(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if len(payload) != 3 || payload["review"] != nil {
			t.Errorf("acceptance uploaded a review: %v", payload)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"system_id": 4, "requirements": []any{map[string]any{
			"kind": "system_requirement", "id": id, "revision": 1, "work_status": "DONE", "result": "accepted",
			"version": map[string]any{"id": "22222222-2222-4222-8222-222222222222", "revision": 1, "acceptance_kind": "existing_behavior", "approved_by_id": 5, "approved_at": "2026-10-23T00:00:00Z", "reason": "verified"},
		}}}})
	}))
	defer server.Close()
	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetIn(bytes.NewBufferString(`{"requirements":[{"kind":"system_requirement","id":"` + id + `"}],"source":"USER: explicit acceptance","reason":"verified"}`))
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"accept", "--file", "-", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("acceptance required a saved review: %v", err)
	}
	if calls != 1 {
		t.Fatalf("acceptance calls = %d, want 1", calls)
	}
}
