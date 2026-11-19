package kit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const planReviewFreshnessParagraph = "For a plan review the rule is narrower. A plan review goes out of date when the plan's content changes, when something the plan relies on changes, such as which test proves a criterion, when the plan's list of traces to remove (`remove_traces`) changes, when the plan's modernizes traces change, or when a stored trace changes that belongs to the plan's requirements or criteria, to a requirement it retires, or that links a test of the plan and code. The gate names what changed in `causes`: `plan_content`, `requirement_edit`, `verification`, `decision`, `planning_context`, `release`, `initiative`, `stored_trace`, `trace_removal` or `modernizes_trace`, and lists the changed stored traces in `trace_ids`. These leave a plan review current: a trace added, changed or left out of the plan's `traces` that is neither a test's verifies link nor a modernizes trace, a renamed test, a requirement's grouping, the same answer recorded again and a selection change."

// TestCase 35e4c90d-9476-43e5-aab3-f842beab5ccb
func TestProcessAndLoopReferenceSayWhichChangesPutAPlanReviewOutOfDate(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	body := strings.ReplaceAll(string(installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md")), "\r\n", "\n")

	// Paragraphs are separated by blank lines; a paragraph may wrap.
	var paragraphs []string
	for _, paragraph := range regexp.MustCompile(`\n[ \t]*\n`).Split(body, -1) {
		if folded := strings.Join(strings.Fields(paragraph), " "); folded != "" {
			paragraphs = append(paragraphs, folded)
		}
	}
	const before = "Add no fingerprints, ETags, tokens, counters or reviewed-state fields."
	next := ""
	for i, paragraph := range paragraphs {
		if strings.HasSuffix(paragraph, before) && i+1 < len(paragraphs) {
			next = paragraphs[i+1]
		}
	}
	if next != planReviewFreshnessParagraph {
		t.Errorf("the installed PROCESS.md does not have the plan review paragraph directly after the paragraph that ends %q; the next paragraph is %q", before, next)
	}

	process := strings.Join(paragraphs, " ")
	loopBody, err := os.ReadFile(filepath.Join("..", "..", "cmd", "reference-loop.md"))
	if err != nil {
		t.Fatal(err)
	}
	loop := kitWhitespaceNormalised(loopBody)
	for _, text := range []struct {
		name, body string
		says, gone []string
	}{
		{"installed PROCESS.md", process,
			[]string{
				"If the gate reports a review out of date, disclose the stale review, with the causes the gate names for a plan review, and obtain a human decision before renewing it.",
				"A later answer that differs from the recorded one changes the reviewed content",
			},
			[]string{
				"If reviewed inputs changed, including decision answers, membership or test definitions, disclose the stale review",
				"A later answer changes the reviewed content",
			}},
		{"cmd/reference-loop.md", loop,
			[]string{"A new or changed answer puts the plan review out of date; the same answer recorded again does not."},
			[]string{"A new answer invalidates the previous independent review."}},
	} {
		for _, phrase := range text.says {
			if !strings.Contains(text.body, phrase) {
				t.Errorf("the %s does not say %q", text.name, phrase)
			}
		}
		for _, phrase := range text.gone {
			if strings.Contains(text.body, phrase) {
				t.Errorf("the %s still says %q", text.name, phrase)
			}
		}
	}
}
