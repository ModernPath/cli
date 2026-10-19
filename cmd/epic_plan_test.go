package cmd

import (
	"encoding/json"
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

func TestEpicApprovalInputIsOneCompletePlanObject(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{"body":"One"} {"body":"Another"}`, `{"body":"Incomplete"`, `{"body":"One"} trailing`} {
		t.Run(input, func(t *testing.T) {
			if _, err := readEpicPlan(strings.NewReader(input)); err == nil {
				t.Fatal("accepted an incomplete or ambiguous reviewed plan")
			}
		})
	}
}
