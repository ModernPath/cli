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
	for _, want := range []string{".modernpath/working-set-reviews/<scope>/<context-id>/", "MANIFEST.json", "--review-context <context-id>", "stop the review"} {
		if !strings.Contains(text, want) {
			t.Errorf("the reviewer must use the explicit immutable snapshot and refuse missing inputs; missing %q", want)
		}
	}
	if strings.Contains(text, ".modernpath/working-set/<scope>/REVIEW.md") {
		t.Error("the reviewer must not be directed to the legacy mutable bundle")
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

func TestReviewerAgentSupportsNarrowLaneBundle(t *testing.T) {
	raw, err := assets.ReadFile(reviewerAgentAsset)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"working-set pull <SR> --for-review",
		".modernpath/working-set/<SR>/REVIEW.md",
		".context", "single_sr:<SR>", "content fingerprint",
		"does not have a snapshot manifest",
		"process lane review <SR> --file review.json",
		"never a fallback for a scoped snapshot",
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("reviewer must describe the explicit narrow-lane input and recording contract; missing %q", want)
		}
	}
}
