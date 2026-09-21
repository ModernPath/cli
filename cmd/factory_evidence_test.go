package cmd

import "testing"

// B8: the CLI must be able to stamp an evidence role. Core.RDD.Reconcile
// promotes an SR only on a result with role "RED" at current validity
// (reconcile.ex red_recorded?), and `factory evidence` is the agent-facing
// path that posts results — so without a role it can never drive a promotion.
func TestEvidenceRoleStampsEveryResultWhenSet(t *testing.T) {
	results := buildEvidenceResults("", "SR-CLI-0001,SR-CLI-0002", "", "RED")
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r["role"] != "RED" {
			t.Fatalf("every result must carry role RED, got %v", r)
		}
		if r["result"] != "fail" {
			t.Fatalf("--fail targets must record result fail, got %v", r)
		}
	}
}

// Role unset keeps the payload byte-identical to the pre-role behaviour: no
// "role" key at all (not an empty string), so callers that never pass --role
// send exactly what they sent before.
func TestEvidenceRoleAbsentWhenUnset(t *testing.T) {
	results := buildEvidenceResults("REQ-X-001", "", "", "")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if _, present := results[0]["role"]; present {
		t.Fatalf("role must be absent when unset, got %v", results[0])
	}
}

// The server matches the role exactly — reconcile.ex promotes on "RED" — and
// validates nothing, so `--role red` posted successfully, printed success, and
// never promoted an SR; nothing pointed at the spelling. The CLI owns the
// normalisation: trimmed and upper-cased before it leaves.
func TestEvidenceRoleIsNormalisedToTheServerVocabulary(t *testing.T) {
	results := buildEvidenceResults("", "SR-CLI-0001", "", " red ")
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0]["role"] != "RED" {
		t.Fatalf("role must reach the server as RED whatever the operator typed, got %q", results[0]["role"])
	}
}
