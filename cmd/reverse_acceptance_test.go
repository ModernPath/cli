package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const acceptanceRequirementID = "8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c"
const acceptanceVersionID = "f291ce3d-8b3b-4bd2-b80f-983d42b25da4"

func TestReverseEngineerReviewReturnsFullBundleAndAllowsIneligibleReview(t *testing.T) {
	selection := `{"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `"}]}`
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/reverse-engineering/acceptance/preview" {
			t.Errorf("request = %s %s, want read-only review preview POST", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"system_id":4,"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `","revision":2,"work_status":"IN_REVIEW","content":{"title":"Catalog lookup"},"criteria":[{"statement":"Returns an entry"}],"traces":[{"kind":"implements"}],"evidence":[{"result":"fail","role":"green"}],"versions":[]}],"eligible":false,"problems":[{"code":"evidence_not_passing"}]}}`))
	}))
	defer server.Close()

	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetIn(strings.NewReader(selection))
	command.SetArgs([]string{"review", "--file", "-", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("review failed: %v", err)
	}
	requested, ok := request["requirements"].([]any)
	if !ok || len(requested) != 1 || requested[0].(map[string]any)["id"] != acceptanceRequirementID {
		t.Fatalf("review request = %#v", request)
	}
	var result map[string]any
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("review output is not JSON: %v", err)
	}
	if result["eligible"] != false || len(result["problems"].([]any)) != 1 || result["requirements"].([]any)[0].(map[string]any)["evidence"] == nil {
		t.Fatalf("review omitted full ineligible bundle: %s", out.String())
	}
}

func TestReverseEngineerReviewRejectsMalformedSelectionBeforeLoading(t *testing.T) {
	for _, input := range []string{
		`{"requirements":[{"kind":"user_requirement","id":"not-a-uuid"}]}`,
		`{"requirements":[{"kind":"user_requirement","id":"` + acceptanceRequirementID + `","extra":true}]}`,
		`{"requirements":[]} {}`,
	} {
		command := newReverseEngineerCommand(func() (*cliEnv, error) {
			t.Fatal("invalid selection must fail before loading the connection")
			return nil, nil
		})
		command.SetIn(strings.NewReader(input))
		command.SetArgs([]string{"review", "--file", "-"})
		if err := command.Execute(); err == nil {
			t.Errorf("accepted invalid selection %s", input)
		}
	}
}

func TestReverseEngineerAcceptPostsExactReviewAndReturnsVersionReceipt(t *testing.T) {
	review := `{"system_id":4,"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `","revision":2,"work_status":"PROPOSED","content":{"title":"Catalog lookup"},"criteria":[],"traces":[],"evidence":[],"versions":[]}],"eligible":true,"problems":[]}`
	input := `{"review":` + review + `,"source":"USER:2026-10-19","reason":"Observed behavior and evidence are accepted"}`
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/reverse-engineering/acceptance" {
			t.Errorf("request = %s %s, want acceptance POST", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"system_id":4,"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `","revision":3,"work_status":"DONE","result":"accepted","version":{"id":"` + acceptanceVersionID + `","revision":3,"acceptance_kind":"existing_behavior","approved_by_id":8,"approved_at":"2026-10-19T10:00:00Z","reason":"Observed behavior and evidence are accepted"}}]}}`))
	}))
	defer server.Close()

	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetIn(strings.NewReader(input))
	command.SetArgs([]string{"accept", "--file", "-", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("accept failed: %v", err)
	}
	if request["source"] != "USER:2026-10-19" || request["reason"] != "Observed behavior and evidence are accepted" || request["review"].(map[string]any)["system_id"] != float64(4) {
		t.Fatalf("accept request lost explicit decision or review bundle: %#v", request)
	}
	var receipt map[string]any
	if err := json.Unmarshal(out.Bytes(), &receipt); err != nil {
		t.Fatalf("accept receipt is not JSON: %v", err)
	}
	row := receipt["requirements"].([]any)[0].(map[string]any)
	if row["result"] != "accepted" || row["work_status"] != "DONE" || row["version"].(map[string]any)["id"] != acceptanceVersionID {
		t.Fatalf("acceptance receipt omitted published version: %s", out.String())
	}
}

func TestReverseEngineerAcceptRequiresExplicitDecisionBeforeLoading(t *testing.T) {
	for _, input := range []string{
		`{"review":{},"source":" ","reason":"accept"}`,
		`{"review":{},"source":"USER:test","reason":" "}`,
		`{"review":{},"source":"USER:test","reason":"accept","extra":true}`,
		`{"review":{},"source":"USER:test","reason":"accept"} {}`,
		`{"review":[],"source":"USER:test","reason":"accept"}`,
	} {
		command := newReverseEngineerCommand(func() (*cliEnv, error) {
			t.Fatal("invalid acceptance input must fail before loading the connection")
			return nil, nil
		})
		command.SetIn(strings.NewReader(input))
		command.SetArgs([]string{"accept", "--file", "-"})
		if err := command.Execute(); err == nil {
			t.Errorf("accepted invalid acceptance input %s", input)
		}
	}
}

func TestReverseAcceptanceRejectsWrongSystemAndRequirementSet(t *testing.T) {
	selection := []reverseRequirementRef{{Kind: "system_requirement", ID: acceptanceRequirementID}}
	bundle := map[string]any{
		"system_id": json.Number("4"), "eligible": false, "problems": []any{},
		"requirements": []any{map[string]any{
			"kind": "system_requirement", "id": acceptanceRequirementID,
			"revision": json.Number("2"), "work_status": "IN_REVIEW", "content": map[string]any{},
			"criteria": []any{}, "traces": []any{}, "evidence": []any{}, "versions": []any{},
		}},
	}
	if err := validateReverseReviewBundle(bundle, 4, selection); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	if err := validateReverseReviewBundle(bundle, 5, selection); err == nil {
		t.Fatal("review for a different bound System was accepted")
	}
	wrongSelection := []reverseRequirementRef{{Kind: "user_requirement", ID: acceptanceRequirementID}}
	if err := validateReverseReviewBundle(bundle, 4, wrongSelection); err == nil {
		t.Fatal("review for a different native requirement was accepted")
	}
}

func TestReverseAcceptanceValidatesCurrentFullVersionReadback(t *testing.T) {
	bundle := map[string]any{
		"system_id": json.Number("4"), "eligible": false, "problems": []any{},
		"requirements": []any{map[string]any{
			"kind": "system_requirement", "id": acceptanceRequirementID,
			"revision": json.Number("3"), "work_status": "DONE", "content": map[string]any{},
			"criteria": []any{}, "traces": []any{}, "evidence": []any{},
			"versions": []any{map[string]any{
				"id": acceptanceVersionID, "requirement_kind": "system_requirement", "requirement_id": acceptanceRequirementID,
				"revision": json.Number("3"), "content": map[string]any{}, "criteria": []any{}, "traces": []any{},
				"evidence": []any{}, "acceptance_kind": "existing_behavior", "epic_id": nil,
				"approved_by_id": json.Number("8"), "approved_at": "2026-10-19T10:00:00Z", "reason": "Accepted",
			}},
		}},
	}
	if err := validateReverseReviewBundle(bundle, 4, nil); err != nil {
		t.Fatalf("complete current version readback rejected: %v", err)
	}
	row := bundle["requirements"].([]any)[0].(map[string]any)
	version := row["versions"].([]any)[0].(map[string]any)
	version["revision"] = json.Number("2")
	if err := validateReverseReviewBundle(bundle, 4, nil); err == nil {
		t.Fatal("DONE requirement with stale latest version readback was accepted")
	}
	delete(row, "versions")
	if err := validateReverseReviewBundle(bundle, 4, nil); err == nil {
		t.Fatal("review without a versions array was accepted")
	}
	row["versions"] = []any{}
	if err := validateReverseReviewBundle(bundle, 4, nil); err != nil {
		t.Fatalf("legacy DONE requirement without an immutable version was rejected: %v", err)
	}
}

func TestReverseAcceptanceRejectsIncompleteSuccessResponse(t *testing.T) {
	reviewed := map[string]any{"requirements": []any{map[string]any{
		"kind": "system_requirement", "id": acceptanceRequirementID,
	}}}
	response := map[string]any{"system_id": json.Number("4"), "requirements": []any{map[string]any{
		"kind": "system_requirement", "id": acceptanceRequirementID, "revision": json.Number("3"),
		"work_status": "DONE", "result": "accepted",
	}}}
	if err := validateReverseAcceptanceResponse(response, 4, reviewed); err == nil {
		t.Fatal("success response without a version record was accepted")
	}
}
