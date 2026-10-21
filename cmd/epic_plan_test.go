package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEpicReviewedPlanPreservesNativeBigintEndpoints(t *testing.T) {
	const input = `{"body":"Reviewed plan","requirements":[],"traces":[{"target_type":"epic","target_id":9223372036854775807}]}`
	plan, err := readEpicPlan(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"target_id":9223372036854775807`) {
		t.Fatalf("reviewed native endpoint changed in transport: %s", encoded)
	}
}

func TestEpicPlanSaveForwardsEmptyDraftAndKeepsSuccessfulJSON(t *testing.T) {
	input := `{"body":"Draft before requirements", "requirements":[]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/api/work/epics/42/plan" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		plan, ok := request["plan"].(map[string]any)
		if !ok || plan["body"] != "Draft before requirements" || len(plan["requirements"].([]any)) != 0 {
			t.Errorf("forwarded plan = %#v", request["plan"])
		}
		_, _ = w.Write([]byte(`{"data":{"process_status":"PROPOSED"}}`))
	}))
	defer server.Close()
	cmd := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	cmd.SetArgs([]string{"save", "42", "--json"})
	cmd.SetIn(strings.NewReader(input))
	var output bytes.Buffer
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
		t.Fatalf("successful response is not JSON: %s; %v", output.String(), err)
	}
	if receipt["operation"] != "saved" || receipt["process_status"] != "PROPOSED" {
		t.Fatalf("successful response JSON = %s", output.String())
	}
}

func TestEpicPlanCoverageRefusalIsReadableAndNonzero(t *testing.T) {
	const message = "A selected requirement is already covered by another initiative."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"already_on_roadmap","ids":["00e93572-d3ec-454e-b07e-2be7f29232f7"],"message":"` + message + `"}`))
	}))
	defer server.Close()
	cmd := newEpicPlanCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 7), nil })
	cmd.SetArgs([]string{"save", "42"})
	cmd.SetIn(strings.NewReader(`{"body":"Draft","requirements":[]}`))
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), message) {
		t.Fatalf("coverage refusal = %v", err)
	}
}

func TestEpicApprovalInputIsOneCompletePlanObject(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{"body":"One"} {"body":"Another"}`, `{"body":"Incomplete"`, `{"body":"One"} trailing`} {
		t.Run(input, func(t *testing.T) {
			if _, err := readEpicPlan(strings.NewReader(input)); err == nil {
				t.Fatal("accepted an incomplete or ambiguous reviewed plan")
			}
		})
	}
}
