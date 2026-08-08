package rdd

// EPIC-DEC-001 (REQ-PLN-048): options with consequences — an **Option (n) — Label:**
// block gives each option an outcome, pros, cons, reversibility (one_way|two_way
// door) and at most ONE recommended flag with a rationale. Rich options replace
// the prose-derived heuristic ones; enforced at gate build, mirrored in ops.js.

import "testing"

const richOptionsBody = `Decide the storage.

**Option (1) — Reuse planning_artifacts:**
- Outcome: Specs live in the existing versioned table.
- Pros: No new table; editor exists.
- Cons: Session coupling needs loosening.
- Reversibility: two_way
- Recommended: Least new surface; the editor already works.

**Option (2) — New spec_documents table:**
- Outcome: A dedicated table with its own lifecycle.
- Pros: Clean model.
- Cons: New migrations, new editor, more code.
- Reversibility: one_way

Tail prose.`

func TestParseRichOptions(t *testing.T) {
	opts := parseRichOptions(richOptionsBody)
	if len(opts) != 2 {
		t.Fatalf("want 2 options, got %d", len(opts))
	}
	first := opts[0]
	if first["key"] != "(1)" || first["label"] != "Reuse planning_artifacts" {
		t.Fatalf("key/label: %#v", first)
	}
	if first["outcome"] != "Specs live in the existing versioned table." {
		t.Fatalf("outcome: %v", first["outcome"])
	}
	if first["pros"] == "" || first["cons"] == "" {
		t.Fatalf("pros/cons: %#v", first)
	}
	if first["reversibility"] != "two_way" {
		t.Fatalf("reversibility: %v", first["reversibility"])
	}
	if first["recommended"] != true || first["recommendation_rationale"] == "" {
		t.Fatalf("recommended: %#v", first)
	}
	if _, present := opts[1]["recommended"]; present {
		t.Fatalf("option 2 must not carry recommended: %#v", opts[1])
	}
	if opts[1]["reversibility"] != "one_way" {
		t.Fatalf("opt2 reversibility: %v", opts[1]["reversibility"])
	}
}

func TestParseRichOptionsOnlyFirstRecommendedWins(t *testing.T) {
	body := `**Option (1) — A:**
- Outcome: One.
- Recommended: first rationale.

**Option (2) — B:**
- Outcome: Two.
- Recommended: second rationale.`
	opts := parseRichOptions(body)
	if len(opts) != 2 {
		t.Fatalf("want 2, got %d", len(opts))
	}
	if opts[0]["recommended"] != true {
		t.Fatal("first keeps recommended")
	}
	if _, present := opts[1]["recommended"]; present {
		t.Fatal("exactly one recommended — the second flag is dropped")
	}
}

func TestRQGateOpPrefersRichOptions(t *testing.T) {
	op := BuildRQGateOp(RQ{ID: "RQ-301", Title: "T", Heading: "RQ-301 T — DECISION NEEDED",
		State: "decision-needed", Line: 5, Body: richOptionsBody,
		// prose heuristic would also find "(1)"/"(2)" marks — rich must win
		Options: []Option{{Label: "(1)", Text: "heuristic junk"}, {Label: "(2)", Text: "more junk"}}})
	opts, ok := op.Payload["options"].([]any)
	if !ok || len(opts) != 2 {
		t.Fatalf("options: %#v", op.Payload["options"])
	}
	first := opts[0].(map[string]any)
	if first["outcome"] != "Specs live in the existing versioned table." {
		t.Fatalf("rich options must replace heuristic ones: %#v", first)
	}
	if op.Payload["recommended_option_key"] != "(1)" {
		t.Fatalf("recommended_option_key: %v", op.Payload["recommended_option_key"])
	}

	plain := BuildRQGateOp(RQ{ID: "RQ-302", Title: "T", Heading: "RQ-302 T — DECISION NEEDED",
		State: "decision-needed", Line: 6, Body: "no rich options",
		Options: []Option{{Label: "(1)", Text: "prose option one."}, {Label: "(2)", Text: "prose option two."}}})
	popts := plain.Payload["options"].([]any)
	if popts[0].(map[string]any)["body"] != "prose option one." {
		t.Fatalf("heuristic path must be unchanged: %#v", popts[0])
	}
}
