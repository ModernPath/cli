package cmd

import "testing"

func TestRDDTestCaseWriteRejectsProcedureMetadata(t *testing.T) {
	if err := validateTestCaseWrite(map[string]any{"title": "Cross-tenant response is refused"}, true); err != nil {
		t.Fatalf("a test needs only its identifying title: %v", err)
	}

	for _, field := range []string{"description", "preconditions", "steps", "runner", "selector", "authority", "embedding", "embedding_model"} {
		t.Run(field, func(t *testing.T) {
			payload := map[string]any{"title": "Cross-tenant response is refused", field: "excluded metadata"}
			if err := validateTestCaseWrite(payload, true); err == nil {
				t.Fatalf("RDD TestCase input must reject %s", field)
			}
		})
	}
}
