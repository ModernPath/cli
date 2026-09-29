package kit

import (
	"strings"
	"testing"
)

// REQ-CROSS-449 (EPIC-CLI-TURNS): the installed reviewer reads the review
// pull's REVIEW.md bundle first, before any per-file read.
func TestREQCROSS449ReviewerAgentReadsTheBundleFirst(t *testing.T) {
	raw, err := assets.ReadFile(reviewerAgentAsset)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "REVIEW.md") || !strings.Contains(text, "first") {
		t.Fatalf("the reviewer agent must name REVIEW.md as its first read:\n%s", text)
	}
}

// REQ-CROSS-450: the reviewer ends its report with the review.json that
// `process review record --file` records in one call.
func TestREQCROSS450ReviewerAgentEndsWithTheReviewJSON(t *testing.T) {
	raw, err := assets.ReadFile(reviewerAgentAsset)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"process review record", `"verdict"`, `"body"`, `"source"`, `"findings"`, `"dispositions"`, `"from"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the reviewer agent's report schema must name %s:\n%s", want, text)
		}
	}
}
