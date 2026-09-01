package rdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-237 (`RUN:2026-08-24`): a reverse-engineering pass wrote 23
// confirmation gates into file-state/GATES.md, `factory sync` emitted zero
// upsert_gate ops, and `your-move` showed no open gates. A DERIVED corpus whose
// confirmation gates never reach the store cannot be confirmed, and PROCESS.md
// gives a candidate no other exit.
func gateFixture(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "gates_fixture.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseGateFileReadsWhatTheReverseEngineeringPassWrote(t *testing.T) {
	records := ParseGateFile(gateFixture(t))
	// six GATE sections; the CRF finding record between them is not a gate
	if len(records) != 6 {
		t.Fatalf("parsed %d gates, want 6", len(records))
	}

	acc := records[1]
	if acc.ID != "GATE-ACC-001" || acc.Title != "Confirm the derived ACC corpus" {
		t.Errorf("id/title = %q/%q", acc.ID, acc.Title)
	}
	if acc.Kind != "human" || acc.Purpose != "confirmation" {
		t.Errorf("kind/purpose = %q/%q, want human/confirmation", acc.Kind, acc.Purpose)
	}
	// The state cell carries prose after the token; the token is the state.
	if acc.State != "DRAFT" {
		t.Errorf("state = %q, want DRAFT — the trailing prose is not part of it", acc.State)
	}
	if len(acc.Scope) != 3 || acc.Scope[0] != "UR-ACC-001" {
		t.Errorf("scope = %v, want the 3 named ids", acc.Scope)
	}
	if len(acc.Prerequisites) != 1 || acc.Prerequisites[0] != "GATE-ACC-000" {
		t.Errorf("prerequisites = %v", acc.Prerequisites)
	}
	if !strings.Contains(acc.Brief, "confirm whether each derived") {
		t.Errorf("brief not captured: %q", acc.Brief)
	}
}

func TestBuildGateFileOpsOpensOnlyWhatItsPrerequisitesAllow(t *testing.T) {
	ops, warnings := BuildGateFileOps(ParseGateFile(gateFixture(t)), "file-state/GATES.md")

	byID := map[string]map[string]any{}
	for _, op := range ops {
		if op.Type != "upsert_gate" {
			t.Fatalf("unexpected op type %q", op.Type)
		}
		byID[op.Payload["external_id"].(string)] = op.Payload
	}

	// PROCESS.md: a human gate may become OPEN only once every prerequisite
	// trace gate is PASS. ACC's is PASS, WEB's is FAIL.
	acc, ok := byID["GATE-ACC-001"]
	if !ok {
		t.Fatal("GATE-ACC-001 did not reach the store — this is the whole defect")
	}
	if acc["state"] != "open" {
		t.Errorf("ACC state = %v, want open", acc["state"])
	}
	if got := len(acc["exact_scope"].([]any)); got != 3 {
		t.Errorf("exact_scope carries %d ids, want 3 — a gate naming no scope is unanswerable", got)
	}
	if acc["kind"] != "approval_request" {
		t.Errorf("kind = %v", acc["kind"])
	}

	if _, present := byID["GATE-WEB-001"]; present {
		t.Error("GATE-WEB-001 opened while its prerequisite is FAIL")
	}
	if !gateWarningNames(warnings, "GATE-WEB-000") {
		t.Errorf("a gate held back must be named, got %v", warnings)
	}

	old := byID["GATE-OLD-001"]
	if old == nil || old["state"] != "answered" {
		t.Fatalf("an answered gate must sync as answered, got %v", old)
	}
	if !strings.Contains(old["answer"].(string), "confirmed as-built") {
		t.Errorf("answer not carried: %v", old["answer"])
	}

	// Trace gates are evidence, not decisions — they have no store kind.
	for id := range byID {
		if strings.HasSuffix(id, "-000") {
			t.Errorf("%s is a trace gate and must not be synced as a decision", id)
		}
	}

	if !gateWarningNames(warnings, "synced OPEN") {
		t.Errorf("the opened gates must be named, got %v", warnings)
	}
}

func gateWarningNames(list []string, want string) bool {
	for _, s := range list {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

// req-driven-dev#13 co-locates `## CRF-…` finding records in the same file.
// A gate section that ends at the next GATE heading absorbs them: reviewing
// that PR, GATE-A-001 parsed with the FINDING's scope. A confirmation gate
// would reach the store carrying another record's ids — silent and plausible.
func TestParseGateFileDoesNotAbsorbNeighbouringRecords(t *testing.T) {
	records := ParseGateFile(gateFixture(t))

	byID := map[string]GateRecord{}
	for _, r := range records {
		byID[r.ID] = r
	}

	if _, present := byID["CRF-ACC-007"]; present {
		t.Error("a finding record is not a gate and must not parse as one")
	}

	last, ok := byID["GATE-LAST-001"]
	if !ok {
		t.Fatal("a gate after a CRF record was lost")
	}
	if len(last.Scope) != 1 || last.Scope[0] != "SR-LAST-001" {
		t.Errorf("scope = %v, want [SR-LAST-001]", last.Scope)
	}

	acc := byID["GATE-OLD-001"]
	for _, id := range acc.Scope {
		if id == "SR-BLEED-999" {
			t.Error("GATE-OLD-001 absorbed the CRF record's scope — the section boundary is wrong")
		}
	}
}
