package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequirementUpdateCommandIsAvailable(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"requirements", "update"})
	if err != nil || command.Name() != "update" {
		t.Fatalf("requirements update must resolve to the draft update command, got %v (%v)", command.Name(), err)
	}
}

func TestRequirementUpdateRejectsNonObjectWithPlainError(t *testing.T) {
	for _, payload := range []string{`[]`, `[{}]`, `null`, `true`, `42`, `"title"`} {
		t.Run(payload, func(t *testing.T) {
			command := newRequirementUpdateCommand(func() (*cliEnv, error) {
				t.Fatal("invalid input loaded workspace")
				return nil, nil
			})
			command.SetArgs([]string{"SR-3-0", "--file", "-"})
			command.SetIn(strings.NewReader(payload))
			if err := command.Execute(); err == nil || err.Error() != "input must be a JSON object" {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestRequirementUpdateSendsPartialContentInOneRequest(t *testing.T) {
	for _, target := range []string{"11111111-1111-4111-8111-111111111111", "SR-3-0", "sr-3-0", "UR-3-0"} {
		t.Run(target, func(t *testing.T) {
			kind, ref := "system", "SR-3-0"
			if strings.HasPrefix(target, "UR-") {
				kind, ref = "user", "UR-3-0"
			}
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "PATCH" || !strings.EqualFold(r.URL.Path, "/api/v1/systems/4/requirements/"+kind+"/"+target) {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if payload["description"] != "Clarified" || len(payload) != 1 {
					t.Errorf("payload = %#v", payload)
				}
				fmt.Fprintf(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","requirement_kind":%q,"display_ref":%q,"system_id":4,"title":"Existing title","work_status":"PROPOSED","revision":0,"result":"updated"}}`, kind+"_requirement", ref)
			}))
			defer server.Close()
			command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{target, "--kind", kind, "--file", "-", "--json"})
			command.SetIn(strings.NewReader(`{"description":"Clarified"}`))
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			if requests != 1 || !strings.Contains(out.String(), "Existing title") {
				t.Fatalf("requests=%d receipt=%s", requests, out.String())
			}
		})
	}
}

func TestRequirementUpdateOptionalReasonAndPublishedContext(t *testing.T) {
	for _, reason := range []string{"", "Human wording decision"} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var payload map[string]any
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["context"] != "Billing" {
				t.Errorf("payload = %#v", payload)
			}
			if reason != "" && payload["reason"] != reason {
				t.Errorf("reason = %#v", payload)
			}
			if reason == "" {
				if _, ok := payload["reason"]; ok {
					t.Error("omitted reason sent")
				}
			}
			fmt.Fprint(w, `{"data":{"id":"11111111-1111-4111-8111-111111111111","requirement_kind":"system_requirement","display_ref":"SR-3-1","system_id":4,"title":"Accepted","work_status":"DONE","revision":1,"context":"Billing","context_name":"Billing","result":"updated"}}`)
		}))
		command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		args := []string{"SR-3-1", "--file", "-", "--json"}
		if reason != "" {
			args = append(args, "--reason", reason)
		}
		command.SetArgs(args)
		command.SetIn(strings.NewReader(`{"context":"Billing"}`))
		if err := command.Execute(); err != nil {
			t.Error(err)
		}
		server.Close()
	}
}

func TestRequirementUpdateValidatesContentBeforeLoadingWorkspace(t *testing.T) {
	for _, payload := range []string{`{}`, `null`, `[]`, `{"title":" "}`, `{"title":null}`, `{"description":42}`, `{"work_status":"DONE"}`, `{"revision":1}`, `{"criteria":[]}`, `{"tenant_id":4}`, `{"system_id":4}`, `{"change_epic_id":3}`, `{"release_id":3}`, `{"actor":"wrong kind"}`, `{"expected_ref":"SR-3-0"}`, `{"context":null}`, `{"context_name":"Orphan"}`, `{"context":" "}`, `{"context":"Billing","context_name":" "}`} {
		t.Run(payload, func(t *testing.T) {
			command := newRequirementUpdateCommand(func() (*cliEnv, error) { t.Fatal("invalid input loaded workspace"); return nil, nil })
			command.SetArgs([]string{"SR-3-0", "--file", "-"})
			command.SetIn(strings.NewReader(payload))
			if err := command.Execute(); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	for _, args := range [][]string{{"bad-id"}, {"11111111-1111-4111-8111-111111111111"}, {"SR-3-0", "--kind", "user"}} {
		command := newRequirementUpdateCommand(func() (*cliEnv, error) { t.Fatal("invalid identity loaded workspace"); return nil, nil })
		command.SetArgs(append(args, "--file", "-"))
		command.SetIn(strings.NewReader(`{"title":"Valid"}`))
		if err := command.Execute(); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}

func TestRequirementUpdatePreservesActionableServerRefusalsAndChecksReceipt(t *testing.T) {
	for _, tc := range []struct {
		status         int
		response, want string
	}{
		{422, `{"error":{"reason":"validation_failed","details":{"context":["should be at most 255 character(s)"]}}}`, "context: should be at most"},
		{404, `{"error":{"reason":"not_found"}}`, "not_found"},
		{409, `{"error":{"reason":"requirement_in_progress","message":"Requirement is approved; return to planning before editing."}}`, "return to planning"},
		{409, `{"error":{"reason":"requirement_already_published","message":"DONE requirements need a follow-up Epic."}}`, "follow-up Epic"},
		{200, `{"data":{"id":"11111111-1111-4111-8111-111111111111","system_id":99}}`, "draft identity"},
		{200, `{"data":{"id":"22222222-2222-4222-8222-222222222222","system_id":4}}`, "draft identity"},
		{200, `{"data":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"requirement_kind":"system_requirement","display_ref":"SR-3-1","title":"Published","work_status":"DONE","revision":1}}`, "draft identity"},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.response) }))
		command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{"11111111-1111-4111-8111-111111111111", "--kind", "system", "--file", "-"})
		command.SetIn(strings.NewReader(`{"title":"Corrected"}`))
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("error = %v, want %q", err, tc.want)
		}
		server.Close()
	}
}

// requirementGroupingReceipt is the update route's receipt for a grouping-only
// change of SR-3-0 in System 4.
func requirementGroupingReceipt(context, contextName, result string) string {
	return fmt.Sprintf(`{"data":{"id":"11111111-1111-4111-8111-111111111111","requirement_kind":"system_requirement","display_ref":"SR-3-0","system_id":4,"title":"Existing title","work_status":"PROPOSED","revision":0,"context":%q,"context_name":%q,"result":%q}}`,
		context, contextName, result)
}

// Epic 340 (G1), test case 9022296b: requirements update with a grouping-only
// file without context_name sends the file as it is and accepts the non-blank
// name the server returns, the list's name, updated or unchanged.
func TestRequirementUpdateGroupingOnlyAcceptsTheNameTheServerReturns(t *testing.T) {
	for _, tc := range []struct{ file, context, contextName, result string }{
		{`{"context":"BILLING"}`, "BILLING", "Billing", "updated"},
		{`{"context":"SHIPPING"}`, "SHIPPING", "Shipping", "unchanged"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				_ = json.NewDecoder(r.Body).Decode(&payload)
				if len(payload) != 1 || payload["context"] != tc.context {
					t.Errorf("payload = %#v; want only the context %s", payload, tc.context)
				}
				fmt.Fprint(w, requirementGroupingReceipt(tc.context, tc.contextName, tc.result))
			}))
			defer server.Close()
			command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
			command.SetArgs([]string{"SR-3-0", "--file", "-", "--json"})
			command.SetIn(strings.NewReader(tc.file))
			var out bytes.Buffer
			command.SetOut(&out)
			if err := command.Execute(); err != nil {
				t.Fatalf("requirements update refused the server's receipt: %v", err)
			}
			for _, want := range []string{`"context_name": "` + tc.contextName + `"`, `"result": "` + tc.result + `"`} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("receipt lacks %s:\n%s", want, out.String())
				}
			}
		})
	}
}

// Epic 340 (G1), test case 9022296b, guard: a grouping-only receipt with
// another context, or with a blank name, is still refused.
func TestRequirementUpdateGroupingOnlyStillRefusesAnotherContextOrABlankName(t *testing.T) {
	for _, receipt := range []string{
		requirementGroupingReceipt("OTHER", "Billing", "updated"),
		requirementGroupingReceipt("BILLING", "", "updated"),
		requirementGroupingReceipt("BILLING", "  ", "updated"),
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, receipt) }))
		command := newRequirementUpdateCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
		command.SetArgs([]string{"SR-3-0", "--file", "-"})
		command.SetIn(strings.NewReader(`{"context":"BILLING"}`))
		if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "draft identity") {
			t.Errorf("receipt %s: error = %v; want the receipt refused", receipt, err)
		}
		server.Close()
	}
}
