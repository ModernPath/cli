package rdd

// SR-SY-1403 (EPIC-SYNC-014) — user requirements are extracted. The eval
// (`RUN:2026-08-16`): one estate's requirements/ carries 102 URs and 186 SCNs, all
// outside the manifest globs — zero kind:"user" ops, so
// compliance_user_requirements stays empty and every SR is an orphan.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// The field corpus shape, structure verbatim (requirements/DEAL-USER-REQUIREMENTS.md).
const urFileFixture = `# DEAL — User requirements (M&A deal execution, app-web)

Derived 2026-08-16 (UR/SCN derivation pass).

## Actors

- **Seller** — the company owner selling (` + "`CODE:app-web/prisma/schema.prisma`" + ` DealParticipant)

## UR-DEAL-001 — A seller or buyer starts a deal and assembles its participants

- Actor: Seller / Buyer (creator), Invitee
- Statement: An authenticated seller or buyer creates a deal against a company
  through a step-by-step wizard that fixes contract type and pricing tier.
- Intended use: turning a matched interest into a working deal room.
- Source: REQ-DEAL-001 · REQ-DEAL-029 (flow rows).
  ` + "`CODE:app-web/src/components/deals/deal-start-wizard.tsx`" + ` ·
  ` + "`CODE:app-web/src/trpc/routers/deal.ts`" + `
- Validation: SCN-DEAL-001 · SCN-DEAL-002
- Status: PROPOSED

### SCN-DEAL-001 — The wizard creates a deal

` + "```gherkin" + `
Scenario: Creating a deal through the start wizard
  Given an authenticated user and a target company
  When they complete the deal start wizard
  Then the deal exists in its initial DRAFT state
` + "```" + `

- Implements: UR-DEAL-001
- Implemented by: REQ-DEAL-029 (wizard) · REQ-DEAL-001 (createDeal)

### SCN-DEAL-002 — An invitation resolves and the draft activates

` + "```gherkin" + `
Scenario: Invitation acceptance activates the draft
  Given a DRAFT deal whose creator has invited the counterpart role
  When the invitee accepts in-app
  Then they join the deal as a typed participant
  And a declined invitation closes without joining
` + "```" + `

- Implements: UR-DEAL-001

## UR-DEAL-002 — A participant tracks their deals

- Actor: Deal participant
- Statement: A participant sees their deals listed with state badges.
- Source: REQ-DEAL-003 · REQ-DEAL-028.
- Validation: SCN-DEAL-004
- Status: PROPOSED
`

func TestParseUserRequirementsReadsTheCorpusShape(t *testing.T) {
	urs, _ := ParseUserRequirements("requirements/DEAL-USER-REQUIREMENTS.md", urFileFixture)
	if len(urs) != 2 {
		t.Fatalf("want 2 URs, got %d", len(urs))
	}
	ur := urs[0]
	if ur.ID != "UR-DEAL-001" ||
		ur.Title != "A seller or buyer starts a deal and assembles its participants" {
		t.Fatalf("heading parse wrong: %+v", ur)
	}
	if !strings.HasPrefix(ur.Statement, "An authenticated seller or buyer creates a deal") ||
		strings.Contains(ur.Statement, "\n") {
		t.Fatalf("wrapped statement not re-flowed: %q", ur.Statement)
	}
	if ur.Status != "PROPOSED" {
		t.Fatalf("status wrong: %q", ur.Status)
	}
	if len(ur.Scenarios) != 2 || ur.Scenarios[0].ID != "SCN-DEAL-001" {
		t.Fatalf("validation SCNs not resolved: %+v", ur.Scenarios)
	}
	// SCN-DEAL-004 has no block in the file — nothing invented.
	if len(urs[1].Scenarios) != 0 {
		t.Fatalf("an unresolved SCN id grew a scenario: %+v", urs[1].Scenarios)
	}
}

func TestBuildLedgerUserRequirementOp(t *testing.T) {
	urs, _ := ParseUserRequirements("requirements/DEAL-USER-REQUIREMENTS.md", urFileFixture)
	op := BuildLedgerUserRequirementOp(urs[0])
	if op.Type != "upsert_requirement" {
		t.Fatalf("wrong op type %q", op.Type)
	}
	p := op.Payload
	if p["kind"] != "user" || p["external_id"] != "UR-DEAL-001" || p["context"] != "DEAL" {
		t.Fatalf("identity fields wrong: %v", p)
	}
	if p["stage"] != nil || p["work_status"] != "PROPOSED" {
		t.Fatalf("stage/status wrong: %v / %v", p["stage"], p["work_status"])
	}
	if desc, _ := p["description"].(string); !strings.HasPrefix(desc, "An authenticated seller or buyer") {
		t.Fatalf("description is not the Statement: %q", desc)
	}

	cites, _ := json.Marshal(p["source_citations"])
	for _, want := range []string{
		`{"kind":"doc","ref":"REQ-DEAL-001"}`,
		`{"kind":"code","ref":"app-web/src/components/deals/deal-start-wizard.tsx"}`,
		`{"kind":"code","ref":"app-web/src/trpc/routers/deal.ts"}`,
	} {
		if !strings.Contains(string(cites), want) {
			t.Errorf("citation %s missing: %s", want, cites)
		}
	}

	criteria, _ := p["criteria"].([]any)
	if len(criteria) != 2 {
		t.Fatalf("want 2 scenario criteria, got %d: %v", len(criteria), p["criteria"])
	}
	first, _ := criteria[0].(map[string]any)
	if first["external_id"] != "UR-DEAL-001#SCN-DEAL-001" || first["kind"] != "scenario" ||
		first["position"] != 1 {
		t.Fatalf("criterion identity wrong: %v", first)
	}
	if first["given"] != "an authenticated user and a target company" ||
		first["when"] != "they complete the deal start wizard" ||
		first["then"] != "the deal exists in its initial DRAFT state" {
		t.Fatalf("gherkin GWT parse wrong: %v", first)
	}
	second, _ := criteria[1].(map[string]any)
	if second["then"] != "they join the deal as a typed participant And a declined invitation closes without joining" {
		t.Fatalf("And-line not folded into its clause: %v", second["then"])
	}
}

func TestSnapshotExtractsUserRequirementFiles(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, root, "requirements/DEAL-USER-REQUIREMENTS.md", urFileFixture)
	mustWriteFile(t, root, "tasks/DEAL-REQUIREMENTS.md", derivedLedgerFixture)
	mustWriteFile(t, root, "WORKLIST.md", "")

	data, warnings := Snapshot(root, manifest.Default())
	if len(data.UserReqs) != 2 {
		t.Fatalf("Snapshot missed the requirements glob: %d URs (warnings: %v)",
			len(data.UserReqs), warnings)
	}

	// The unresolved SCN-DEAL-004 is a loud gap, never a silent skip.
	found := false
	for _, w := range warnings {
		if strings.Contains(w, "SCN-DEAL-004") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unresolved Validation SCN not reported: %v", warnings)
	}

	// User ops precede system ops, so the parent exists before its children.
	ops := BuildOps(data, nil, "2026-08-16")
	var reqIDs []string
	for _, op := range ops {
		if op.Type == "upsert_requirement" {
			reqIDs = append(reqIDs, op.Payload["external_id"].(string))
		}
	}
	want := []string{"UR-DEAL-001", "UR-DEAL-002", "REQ-DEAL-028", "REQ-DEAL-029"}
	if strings.Join(reqIDs, " ") != strings.Join(want, " ") {
		t.Fatalf("op order wrong: %v", reqIDs)
	}
}

// The manifest gains the glob as a NON-mandated type: a workspace without a
// requirements/ directory syncs exactly as before, with no loud gap.
func TestUserRequirementsGlobIsNotMandated(t *testing.T) {
	m := manifest.Default()
	spec, declared := m.Documents[manifest.DocUserRequirements]
	if !declared || len(spec.Globs()) == 0 ||
		spec.Globs()[0] != "requirements/*-USER-REQUIREMENTS.md" {
		t.Fatalf("default manifest lacks the requirements glob: %+v", m.Documents)
	}
	root := t.TempDir()
	mustWriteFile(t, root, "tasks/X-REQUIREMENTS.md", "# X — x\n")
	mustWriteFile(t, root, "WORKLIST.md", "")
	for _, missing := range m.MissingMandated(root) {
		if missing == manifest.DocUserRequirements {
			t.Fatalf("user_requirements must not be mandated: %v", m.MissingMandated(root))
		}
	}
}
