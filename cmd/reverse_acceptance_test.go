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

func TestReverseEngineerReviewReturnsCurrentChecksAndAllowsIneligibleReview(t *testing.T) {
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
		_, _ = w.Write([]byte(`{"data":{"system_id":4,"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `","revision":2,"work_status":"IN_REVIEW","criteria":[]}],"runs":[],"evidence":[],"test_cases":[],"eligible":false,"problems":[{"code":"evidence_not_passing"}]}}`))
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
	if result["eligible"] != false || len(result["problems"].([]any)) != 1 || result["evidence"] == nil {
		t.Fatalf("review omitted current checks: %s", out.String())
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

func TestReverseEngineerAcceptPostsDecisionAndReturnsVersionReceipt(t *testing.T) {
	input := `{"requirements":[{"kind":"system_requirement","id":"` + acceptanceRequirementID + `"}],"source":"USER:2026-10-19","reason":"Observed behavior and evidence are accepted"}`
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
	if request["source"] != "USER:2026-10-19" || request["reason"] != "Observed behavior and evidence are accepted" || len(request) != 3 || request["requirements"].([]any)[0].(map[string]any)["id"] != acceptanceRequirementID {
		t.Fatalf("accept request lost explicit decision or selection: %#v", request)
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
		"runs": []any{}, "evidence": []any{}, "test_cases": []any{},
		"requirements": []any{map[string]any{
			"kind": "system_requirement", "id": acceptanceRequirementID,
			"revision": json.Number("2"), "work_status": "IN_REVIEW", "criteria": []any{},
		}},
	}
	if err := validateReverseReview(bundle, 4, selection); err != nil {
		t.Fatalf("valid bundle rejected: %v", err)
	}
	if err := validateReverseReview(bundle, 5, selection); err == nil {
		t.Fatal("review for a different bound System was accepted")
	}
	wrongSelection := []reverseRequirementRef{{Kind: "user_requirement", ID: acceptanceRequirementID}}
	if err := validateReverseReview(bundle, 4, wrongSelection); err == nil {
		t.Fatal("review for a different native requirement was accepted")
	}
}

func TestReverseReviewSkipsDoneAndRejectsCopiedHistory(t *testing.T) {
	data := map[string]any{
		"system_id": json.Number("4"), "eligible": true, "problems": []any{},
		"runs": []any{}, "evidence": []any{}, "test_cases": []any{},
		"requirements": []any{map[string]any{"kind": "system_requirement", "id": acceptanceRequirementID,
			"revision": json.Number("3"), "work_status": "DONE", "criteria": []any{}}},
	}
	if err := validateReverseReview(data, 4, nil); err != nil {
		t.Fatal(err)
	}
	row := data["requirements"].([]any)[0].(map[string]any)
	for _, key := range []string{"content", "traces", "evidence", "versions"} {
		row[key] = []any{}
		if err := validateReverseReview(data, 4, nil); err == nil {
			t.Fatalf("accepted copied %s", key)
		}
		delete(row, key)
	}
}

func TestReverseReviewResolvesSharedEvidenceIdentities(t *testing.T) {
	const caseID = "11111111-1111-4111-8111-111111111111"
	const runID = "22222222-2222-4222-8222-222222222222"
	const resultID = "33333333-3333-4333-8333-333333333333"
	const criterionID = "44444444-4444-4444-8444-444444444444"
	fixture := func() map[string]any {
		return map[string]any{
			"system_id": json.Number("4"), "eligible": true, "problems": []any{},
			"test_cases": []any{map[string]any{"id": caseID, "revision": json.Number("0")}},
			"runs":       []any{map[string]any{"id": runID, "sha": "tested", "status": "complete"}},
			"evidence":   []any{map[string]any{"id": resultID, "run_id": runID, "target_id": caseID, "result": "pass", "validity": "current"}},
			"requirements": []any{map[string]any{
				"id": acceptanceRequirementID, "kind": "system_requirement", "revision": json.Number("0"), "work_status": "PROPOSED",
				"criteria": []any{map[string]any{"criterion_id": criterionID, "state": "pass", "problem": nil,
					"test_keys": []any{caseID}, "run_id": runID, "result_ids": []any{resultID}}},
			}},
		}
	}
	if err := validateReverseReview(fixture(), 4, nil); err != nil {
		t.Fatal(err)
	}
	for _, collection := range []string{"test_cases", "runs", "evidence"} {
		data := fixture()
		data[collection] = []any{}
		if err := validateReverseReview(data, 4, nil); err == nil {
			t.Fatalf("accepted missing %s reference", collection)
		}
		data = fixture()
		rows := data[collection].([]any)
		data[collection] = append(rows, rows[0])
		if err := validateReverseReview(data, 4, nil); err == nil {
			t.Fatalf("accepted duplicated %s identity", collection)
		}
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

func TestReverseAcceptanceSkipsDoneWithoutReadingItsHistoricalVersion(t *testing.T) {
	selection := map[string]any{"requirements": []any{map[string]any{
		"kind": "system_requirement", "id": acceptanceRequirementID,
	}}}
	row := map[string]any{
		"kind": "system_requirement", "id": acceptanceRequirementID, "revision": json.Number("0"),
		"work_status": "DONE", "result": "skipped",
	}
	response := map[string]any{"system_id": json.Number("4"), "requirements": []any{row}}
	if err := validateReverseAcceptanceResponse(response, 4, selection); err != nil {
		t.Fatal(err)
	}
	receipt, err := reverseAcceptanceReceipt(4, response)
	if err != nil {
		t.Fatal(err)
	}
	if _, copied := receipt["requirements"].([]any)[0].(map[string]any)["version"]; copied {
		t.Fatal("skipped receipt must not carry a historical version")
	}
	row["work_status"] = "PROPOSED"
	if err := validateReverseAcceptanceResponse(response, 4, selection); err == nil {
		t.Fatal("accepted skipped receipt for an unfinished requirement")
	}
}
