package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequirementUpdateRejectsRemovedPreviewBeforeLoadingEnvironment(t *testing.T) {
	loaded := false
	command := newRequirementUpdateCommand(func() (*cliEnv, error) { loaded = true; return nil, fmt.Errorf("unexpected load") })
	command.SetArgs([]string{"SR-3-0", "--file", "-", "--preview"})
	command.SetIn(strings.NewReader(`{"work_status":"DONE"}`))
	err := command.Execute()
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --preview") || loaded {
		t.Fatalf("expected unknown preview flag before environment load; err=%v loaded=%v", err, loaded)
	}
}

func TestRequirementUpdateCompletionBlockers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		fmt.Fprint(w, `{"error":{"reason":"completion_not_ready","blockers":[{"requirement_kind":"system_requirement","requirement_id":"req-1","criterion_id":"criterion-1","reason":"missing_criterion_evidence"},{"requirement_kind":"system_requirement","requirement_id":"req-1","criterion_id":null,"reason":"missing_criteria"}]},"request_id":"request-1"}`)
	}))
	defer server.Close()
	command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"SR-3-0", "--file", "-", "--reason", "Approved"})
	command.SetIn(strings.NewReader(`{"work_status":"DONE","source":"USER:test"}`))
	err := command.Execute()
	if err == nil {
		t.Fatal("expected refusal")
	}
	for _, want := range []string{"completion_not_ready", "req-1", "criterion-1", "missing_criterion_evidence", "missing_criteria", "request-1", "409"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q missing %q", err, want)
		}
	}
}

func TestRequirementUpdateCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/systems/4/requirements/system/sr-3-0" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if input["work_status"] != "DONE" || input["source"] != "USER:test" || input["reason"] != "Approved completion" {
			t.Errorf("input = %#v", input)
		}
		fmt.Fprint(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","requirement_kind":"system_requirement","display_ref":"SR-3-1","system_id":4,"title":"Verified","work_status":"DONE","revision":1,"result":"completed","version_id":"22222222-2222-4222-8222-222222222222","approved_by_id":5,"approved_at":"2026-10-26T12:00:00Z","reason":"Approved completion"}}`)
	}))
	defer server.Close()
	command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"SR-3-0", "--file", "-", "--json", "--reason", "Approved completion"})
	command.SetIn(strings.NewReader(`{"work_status":"DONE","source":"USER:test"}`))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestRequirementUpdateCompletionRejectsForgedReceipts(t *testing.T) {
	for _, mutation := range []map[string]any{
		{"system_id": 99}, {"id": "bad"}, {"requirement_kind": "user_requirement"},
		{"display_ref": "SR-4-1"}, {"display_ref": "SR-3-2"}, {"revision": 0},
		{"result": "updated"}, {"work_status": "PROPOSED"}, {"version_id": nil},
		{"approved_by_id": nil}, {"approved_at": "bad"}, {"reason": ""},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := map[string]any{"id": "11111111-1111-4111-8111-111111111111", "system_id": 4, "requirement_kind": "system_requirement", "display_ref": "SR-3-1", "revision": 1, "title": "Verified", "result": "completed", "work_status": "DONE", "version_id": "22222222-2222-4222-8222-222222222222", "approved_by_id": 5, "approved_at": "2026-10-26T12:00:00Z", "reason": "Approved"}
			for key, value := range mutation {
				data[key] = value
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
		}))
		command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{"SR-3-0", "--file", "-", "--reason", "Approved"})
		command.SetIn(strings.NewReader(`{"work_status":"DONE","source":"USER:test"}`))
		if err := command.Execute(); err == nil {
			t.Errorf("accepted forged receipt %#v", mutation)
		}
		server.Close()
	}
}

func TestRequirementUpdateDoneReplayNeedsNoApprovalOrClassification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]any
		_ = json.NewDecoder(r.Body).Decode(&input)
		if len(input) != 1 || input["work_status"] != "DONE" {
			t.Errorf("input=%#v", input)
		}
		fmt.Fprint(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"requirement_kind":"system_requirement","display_ref":"SR-3-2","revision":2,"title":"Legacy","result":"unchanged","work_status":"DONE","version_id":null}}`)
	}))
	defer server.Close()
	command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"11111111-1111-4111-8111-111111111111", "--kind", "system", "--file", "-"})
	command.SetIn(strings.NewReader(`{"work_status":"DONE"}`))
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestRequirementUpdateCompletionReceiptMatchesSubmittedDecision(t *testing.T) {
	for _, submitted := range []string{"", "Different decision"} {
		t.Run(submitted, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"requirement_kind":"system_requirement","display_ref":"SR-3-1","revision":1,"title":"Verified","result":"completed","work_status":"DONE","version_id":"22222222-2222-4222-8222-222222222222","approved_by_id":5,"approved_at":"2026-10-26T12:00:00Z","reason":"Approved"}}`)
			}))
			defer server.Close()
			command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			args := []string{"11111111-1111-4111-8111-111111111111", "--kind", "system", "--file", "-"}
			input := `{"work_status":"DONE"}`
			if submitted != "" {
				args = append(args, "--reason", submitted)
				input = `{"work_status":"DONE","source":"USER:test"}`
			}
			command.SetArgs(args)
			command.SetIn(strings.NewReader(input))
			if err := command.Execute(); err == nil {
				t.Fatal("new completion accepted without matching submitted approval")
			}
		})
	}
}
