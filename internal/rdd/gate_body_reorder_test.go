package rdd

// SR-MC-044 acceptance round 1, defect 3 (EPIC-MC-004): the live gate showed
// "recom…" — the stored body_md was cut at the extractor's cap BEFORE the
// recommendation completed, so no parser downstream could recover it. The
// heading-form OQ is the only gate flavor whose recommendation lives ONLY in
// the body (RQ and approval gates carry it as a first-class field), so its
// builder must extract the wire brief fields and put them BEFORE the long
// technical detail, capping the technical tail only.

import (
	"strings"
	"testing"
)

func TestOQGateBodyKeepsRecommendationWhole(t *testing.T) {
	detail := strings.Repeat(
		"the audit-risk-scorer.service.ts weighting pipeline reads DEFAULT_COMPONENT_WEIGHTS "+
			"and RISK_LEVEL_THRESHOLDS from code constants and recomputes the blended score per finding. ", 10)
	reco := "keep DEFAULT_COMPONENT_WEIGHTS in code and expose only RISK_LEVEL_THRESHOLDS as configuration."
	content := "## OQ-AUD-2 — Decide if audit weights become configuration\n\n" +
		"Should the scorer weights move to tenant configuration?\n" +
		"why_now: the audit risk scorer ships this release.\n" +
		"changes_if_approved: DEFAULT_COMPONENT_WEIGHTS and RISK_LEVEL_THRESHOLDS become tenant config.\n" +
		"risk_if_wrong: silently divergent scores between tenants.\n" +
		"**Technical detail**: " + detail + "\n" +
		"recommendation: " + reco + "\n"

	oqs := ParseOpenQuestions(content)
	if len(oqs) != 1 {
		t.Fatalf("expected 1 OQ, got %d", len(oqs))
	}
	op := BuildOQGateOp(oqs[0])
	body, _ := op.Payload["body_md"].(string)

	// the recommendation ALWAYS survives, whole
	if !strings.Contains(body, reco) {
		t.Fatalf("recommendation truncated out of body_md:\n%s", body)
	}
	// brief fields sit BEFORE the long technical detail
	recoAt := strings.Index(body, "recommendation:")
	techAt := strings.Index(body, "Technical detail")
	if recoAt == -1 || techAt == -1 || recoAt > techAt {
		t.Errorf("recommendation (at %d) must precede the technical tail (at %d):\n%s", recoAt, techAt, body)
	}
	// the cap still holds — applied to the technical tail, with an honest ellipsis
	if n := len([]rune(body)); n > 1201 { // 1200 + the ellipsis rune
		t.Errorf("body_md exceeds the cap: %d runes", n)
	}
	if !strings.HasSuffix(body, "…") {
		t.Errorf("expected the capped technical tail to end with an ellipsis:\n…%s", body[len(body)-80:])
	}
}

// A body without wire labels keeps the exact legacy composition — no hash
// churn for gates the defect never touched.
func TestOQGateBodyWithoutWireLabelsUnchanged(t *testing.T) {
	content := "## OQ-P1 — Where does the schema live?\n\nOwn repo or in-frontend? Nobody decided.\n"
	oqs := ParseOpenQuestions(content)
	if len(oqs) != 1 {
		t.Fatalf("expected 1 OQ, got %d", len(oqs))
	}
	op := BuildOQGateOp(oqs[0])
	if body := op.Payload["body_md"].(string); body != oqs[0].Full {
		t.Errorf("label-free body must stay the legacy Full text, got:\n%s", body)
	}
}
