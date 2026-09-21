package cmd

// REQ-CROSS-373 (EPIC-CLI-017): `process enter <scope>` opens the entry gate
// from store facts — the epic plus every member still in the FROM state (or
// the single SR), the passing cold-review trace at the current aggregate as
// prerequisite, the brief from the packet's entry_brief section — and refuses
// with the specific unmet fact before any write.

import (
	"fmt"
	"strings"
	"testing"
)

const entryBrief = `**Brief:**
- What: Approve entering EPIC-A.
- Why now: The packet is reviewed and
  the members are waiting.
- Changes if approved: Three verbs land.
- Risk if wrong: Low; a gate is withdrawable.
- Recommendation: approve — the change is bounded.
`

func enterFacts(members []map[string]any, scopeStatus, verdict string, independent bool, traceID string, missing []string) map[string]any {
	return map[string]any{
		"aggregate":   pinAggregate,
		"scope":       map[string]any{"external_id": "EPIC-A", "kind": "epic", "status": scopeStatus},
		"members":     members,
		"cold_review": map[string]any{"verdict": verdict, "independent": independent, "trace_external_id": traceID},
		"sections":    map[string]any{"complete": len(missing) == 0, "missing": missing},
	}
}

func member(id, status string) map[string]any {
	return map[string]any{"external_id": id, "kind": "sr", "status": status, "content_fingerprint": strings.Repeat("1", 64),
		"evidence_state": "claimed", "red_recorded": false, "lower_trace_pass": false}
}

func enterServer(t *testing.T, facts map[string]any, serveFacts bool) *ceremonyServer {
	t.Helper()
	cs := newCeremonyServer(t, facts, serveFacts)
	cs.packetSections = []map[string]any{{"section_key": "entry_brief", "content": entryBrief}}
	return cs
}

// Review nit 4: an indented sub-bullet inside a continuation is part of its
// bullet, and a bold label (`- **What:**`) is the same bullet.
func TestParseBriefSectionKeepsSubBulletsAndBoldLabels(t *testing.T) {
	brief, err := parseBriefSection("- **What:** x\n- Why now: because\n  - first reason\n  - second reason\n- Changes if approved: c\n- Risk if wrong: r\n- Recommendation: approve\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if brief["what"] != "x" {
		t.Fatalf("a bold label is the same bullet, got %v", brief)
	}
	if !strings.Contains(fmt.Sprint(brief["why_now"]), "first reason") || !strings.Contains(fmt.Sprint(brief["why_now"]), "second reason") {
		t.Fatalf("indented sub-bullets join their bullet, got %q", brief["why_now"])
	}
}

func TestParseBriefSection(t *testing.T) {
	brief, err := parseBriefSection(entryBrief)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if brief["what"] != "Approve entering EPIC-A." || brief["recommendation"] != "approve — the change is bounded." {
		t.Fatalf("bullets must map to the brief keys, got %v", brief)
	}
	if brief["why_now"] != "The packet is reviewed and the members are waiting." {
		t.Fatalf("a continuation line joins its bullet, got %q", brief["why_now"])
	}
	if _, err := parseBriefSection("- What: x\n- Why now: y\n"); err == nil || !strings.Contains(err.Error(), "Risk if wrong") {
		t.Fatalf("a missing bullet is refused by name, got %v", err)
	}
}

func TestProcessEnterOpensTheGateFromStoreFacts(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{
		member("REQ-A-1", "PROPOSED"), member("REQ-A-2", "PROPOSED"), member("REQ-A-3", "TODO"),
	}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)

	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
	if err != nil {
		t.Fatalf("enter: %v\n%s", err, out)
	}
	if len(cs.authored) != 1 || cs.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["kind"] != "gate" || record["external_id"] != "ENTRY-EPIC-A" || record["purpose"] != "entry" ||
		record["gate_kind"] != "approval_request" || record["transition"] != "PROPOSED->TODO" {
		t.Fatalf("the entry gate shape is wrong: %v", record)
	}
	scope := stringSlice(record["exact_scope"])
	if strings.Join(scope, ",") != "EPIC-A,REQ-A-1,REQ-A-2" {
		t.Fatalf("the scope is the epic plus every member still in FROM (the TODO member is skipped), got %v", scope)
	}
	if prereq := stringSlice(record["prerequisite_gate_external_ids"]); len(prereq) != 1 || prereq[0] != "CR-A" {
		t.Fatalf("the cold-review trace is the prerequisite, got %v", record["prerequisite_gate_external_ids"])
	}
	brief, _ := record["brief"].(map[string]any)
	if brief["what"] != "Approve entering EPIC-A." {
		t.Fatalf("the brief is read from the entry_brief section, got %v", record["brief"])
	}
	if opts, _ := record["options"].([]any); len(opts) == 0 || opts[0].(map[string]any)["key"] != "approve" {
		t.Fatalf("an approving option is what the answer rides, got %v", record["options"])
	}
	if len(cs.selections) != 1 || cs.selections[0]["phase"] != "entry" || cs.selections[0]["scope_external_id"] != "EPIC-A" {
		t.Fatalf("the selection moves to phase entry, got %v", cs.selections)
	}
	if !strings.Contains(out, "REQ-A-3") || !strings.Contains(out, "ENTRY-EPIC-A") {
		t.Fatalf("the skipped member and the gate id are reported: %q", out)
	}
}

func TestProcessEnterSingleSR(t *testing.T) {
	facts := map[string]any{
		"aggregate":   pinAggregate,
		"scope":       map[string]any{"external_id": "REQ-S-1", "kind": "requirement", "status": "PROPOSED"},
		"members":     []map[string]any{member("REQ-S-1", "PROPOSED")},
		"cold_review": map[string]any{"verdict": "pass", "independent": true, "trace_external_id": "CR-S"},
		"sections":    map[string]any{"complete": true, "missing": []string{}},
	}
	cs := enterServer(t, facts, true)
	env := wsEnv(t, cs.srv)
	if err := processEnter(env, "REQ-S-1", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "REQ-S-1" {
		t.Fatalf("a single SR scope names the SR alone, got %v", scope)
	}
	if cs.selections[0]["scope_kind"] != "single_sr" {
		t.Fatalf("the phase move keeps the single_sr kind, got %v", cs.selections[0])
	}
}

func assertNoWrites(t *testing.T, cs *ceremonyServer) {
	t.Helper()
	if len(cs.authored)+len(cs.selections)+len(cs.reconciles)+len(cs.evidence) != 0 {
		t.Fatalf("a refusal writes nothing, got authored=%v selections=%v", cs.authored, cs.selections)
	}
}

func TestProcessEnterRefusesWithoutAColdReviewTrace(t *testing.T) {
	for _, tc := range []struct {
		name    string
		verdict string
		indep   bool
		trace   string
	}{{"absent", "absent", false, ""}, {"fail", "fail", true, "CR-A"}, {"not independent", "pass", false, "CR-A"}} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", tc.verdict, tc.indep, tc.trace, nil), true)
		env := wsEnv(t, cs.srv)
		err := processEnter(env, "EPIC-A", enterOpts{})
		if err == nil || !strings.Contains(err.Error(), "cold-review") {
			t.Fatalf("%s: the missing independent passing cold-review trace must be named, got %v", tc.name, err)
		}
		assertNoWrites(t, cs)
	}
}

func TestProcessEnterRefusesWithMissingSections(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", []string{"enrichment:REQ-A-1"}), true)
	env := wsEnv(t, cs.srv)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "enrichment:REQ-A-1") || !strings.Contains(err.Error(), "working-set push") {
		t.Fatalf("missing sections are named with the remedy, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessEnterRefusesWithoutFacts(t *testing.T) {
	cs := enterServer(t, nil, false)
	env := wsEnv(t, cs.srv)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("a server without facts is refused before any write, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessEnterRefusesAnUnparseableBrief(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.packetSections = []map[string]any{{"section_key": "entry_brief", "content": "- What: only this\n"}}
	env := wsEnv(t, cs.srv)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "Why now") {
		t.Fatalf("an incomplete brief is refused naming the missing bullet, got %v", err)
	}
	assertNoWrites(t, cs)

	cs.packetSections = nil
	err = processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "entry_brief") || !strings.Contains(err.Error(), "--brief-file") {
		t.Fatalf("no brief section and no --brief-file is refused naming both, got %v", err)
	}
	assertNoWrites(t, cs)
}

// REQ-CROSS-422 (EPIC-CLI-022, SCN-DEMOTE-006): a taken primary id that is
// neither open nor answered — closed after an earlier entry, or withdrawn —
// is not an error: the verb derives ENTRY-<scope>-R2 (the next free -R<n>)
// and names the predecessor, so a scope demoted to PROPOSED and re-planned
// re-enters with no by-hand id juggling.
func TestProcessEnterDerivesTheSuccessorIdWhenTheEntryGateIsClosed(t *testing.T) {
	for _, state := range []string{"closed", "dismissed"} {
		cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
		cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": state, "applied_state": "applied"}
		env := wsEnv(t, cs.srv)
		var err error
		out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{}) })
		if err != nil {
			t.Fatalf("%s: a closed primary id derives its successor, got %v\n%s", state, err, out)
		}
		record, _ := cs.authored[0]["record"].(map[string]any)
		if record["external_id"] != "ENTRY-EPIC-A-R2" {
			t.Fatalf("%s: the successor id is ENTRY-EPIC-A-R2, got %v", state, record["external_id"])
		}
		if !strings.Contains(out, "ENTRY-EPIC-A") || !strings.Contains(out, "predecessor") {
			t.Fatalf("%s: the plan names the closed predecessor: %q", state, out)
		}
		if !strings.Contains(str(record, "body_md"), "ENTRY-EPIC-A") {
			t.Fatalf("%s: the gate body names its predecessor, got %v", state, record["body_md"])
		}
	}
}

// A successor already in the series is skipped too: -R2 closed derives -R3.
func TestProcessEnterWalksTheSuccessorSeries(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": "closed"}
	cs.existingGates["ENTRY-EPIC-A-R2"] = map[string]any{"external_id": "ENTRY-EPIC-A-R2", "state": "closed"}
	env := wsEnv(t, cs.srv)
	if err := processEnter(env, "EPIC-A", enterOpts{}); err != nil {
		t.Fatalf("enter: %v", err)
	}
	if record, _ := cs.authored[0]["record"].(map[string]any); record["external_id"] != "ENTRY-EPIC-A-R3" {
		t.Fatalf("the next free id in the series is ENTRY-EPIC-A-R3, got %v", record["external_id"])
	}
}

func TestProcessEnterDryRunPostsNothing(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "ENTRY-EPIC-A") || !strings.Contains(out, "CR-A") {
		t.Fatalf("the dry run prints the plan: %q", out)
	}
	assertNoWrites(t, cs)
}

// Review round 2, finding 4: an id that is already open or answered is not a
// taken id to rotate past — the operator should answer or apply it.
func TestProcessEnterSaysAnswerItWhenTheGateIsOpen(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PROPOSED")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	cs.existingGates["ENTRY-EPIC-A"] = map[string]any{"external_id": "ENTRY-EPIC-A", "state": "open"}
	env := wsEnv(t, cs.srv)
	err := processEnter(env, "EPIC-A", enterOpts{})
	if err == nil || !strings.Contains(err.Error(), "already open") || strings.Contains(err.Error(), "-R2") {
		t.Fatalf("an open gate is named as open, never rotated, got %v", err)
	}
	assertNoWrites(t, cs)
}

// Review round 2, nit 13: members entering from PENDING_VERIFICATION while
// the epic is still PROPOSED yield a members-only gate; the plan says why the
// epic is not named.
func TestProcessEnterSaysWhyTheEpicIsNotNamed(t *testing.T) {
	cs := enterServer(t, enterFacts([]map[string]any{member("REQ-A-1", "PENDING_VERIFICATION")}, "PROPOSED", "pass", true, "CR-A", nil), true)
	env := wsEnv(t, cs.srv)
	var err error
	out := captureOut(t, func() { err = processEnter(env, "EPIC-A", enterOpts{dryRun: true}) })
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !strings.Contains(out, "EPIC-A is PROPOSED") || !strings.Contains(out, "not named") {
		t.Fatalf("the plan says the epic is not named and why: %q", out)
	}
}
