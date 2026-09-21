package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-381 (EPIC-CLI-018): working-set pull renders every stored field a
// review reads — statement, sources, acceptance criteria, rationale, boundary,
// verification method, owner, stage, priority, detail — so a reviewer reading
// the snapshot sees what the store holds. A served-but-empty field is "—"; the
// «not served» marker is only ever a statement about an absent key.
func TestREQCROSS381PullRendersEveryServedReviewField(t *testing.T) {
	req := wsReq("REQ-RF-001", "Rendered fields")
	req["description"] = "the statement"
	req["source_citations"] = []any{map[string]any{"kind": "USER", "ref": "USER:2026-09-12:x"}}
	req["rationale"] = "because reviewers read it"
	req["boundary"] = "the renderer only"
	req["verification_method"] = "golden render"
	req["priority"] = "P1"
	req["owner"] = "cli"
	req["criteria"] = []any{map[string]any{"statement": "AC1 renders"}}
	req["detail_md"] = "## Problem\n\nprose here"
	req["parent_external_ids"] = []any{"UR-RF-001"}
	req["fingerprint"] = strings.Repeat("f", 64)
	epic := wsEpic("EPIC-RF-001", "Rendered epic")
	epic["owner"] = "jussi"
	epic["scope"] = "the CLI read"
	epic["outcome_source"] = "USER:2026-09-12:y"
	epic["process_status"] = "TODO"
	epic["requirement_external_ids"] = []any{"REQ-RF-001"}
	fx := &wsFixture{epics: []any{epic}, requirements: []any{req}}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-RF-001", "REQ-RF-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-RF-001.md"))
	got := string(raw)
	for _, want := range []string{
		"Statement / source:** the statement / USER: USER:2026-09-12:x",
		"Rationale:** because reviewers read it",
		"Boundary:** the renderer only",
		"Verification method:** golden render",
		"Priority:** P1",
		"Owner / release:** cli / modernpath-v1-09",
		"AC1 renders",
		"prose here",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a served field must render (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, notServed) {
		t.Errorf("every reviewer field is served here — nothing may read as not served:\n%s", got)
	}

	raw, _ = os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-RF-001.md"))
	got = string(raw)
	for _, want := range []string{
		"Owner / release:** jussi / modernpath-v1-09",
		"Scope:** the CLI read",
		"Outcome source:** USER:2026-09-12:y",
		"Process status:** TODO",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("a served epic field must render (%q):\n%s", want, got)
		}
	}
	if strings.Contains(got, "Kind / status:** Epic / "+notServed) {
		t.Errorf("the epic has no `status` column; the slot must not claim one is withheld:\n%s", got)
	}
}

func TestREQCROSS381AServedEmptyFieldReadsAsEmptyNotWithheld(t *testing.T) {
	req := wsReq("REQ-RF-002", "Empty fields")
	req["description"] = "the statement"
	req["source_citations"] = []any{}
	req["rationale"] = ""
	fx := &wsFixture{requirements: []any{req}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"REQ-RF-002"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-RF-002.md"))
	got := string(raw)
	for _, want := range []string{"Statement / source:** the statement / —", "Rationale:** —"} {
		if !strings.Contains(got, want) {
			t.Errorf("a served-but-empty field reads as empty (%q), never as withheld:\n%s", want, got)
		}
	}
}

// REQ-CROSS-382 (EPIC-CLI-018): an epic pull renders every declared member,
// user requirements and system requirements alike, in declared order.
func TestREQCROSS382EpicPullRendersUserRequirementMembers(t *testing.T) {
	epic := wsEpic("EPIC-UR-001", "With a UR")
	epic["requirement_external_ids"] = []any{"REQ-1", "REQ-2"}
	epic["user_requirement_external_ids"] = []any{"UR-1"}
	fx := &wsFixture{epics: []any{epic}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-UR-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-UR-001.md"))
	if !strings.Contains(string(raw), "Members:** REQ-1 · REQ-2 · UR-1") {
		t.Fatalf("the Members line must carry the UR beside the SRs:\n%s", raw)
	}
}

// The authoring render's members block reads the store's declared membership
// too, not the selection's frozen copy — so a push computes its membership ops
// against what the store holds.
func TestREQCROSS382ScopePullMembersBlockCarriesTheServedUnion(t *testing.T) {
	fx := scaffoldFixture()
	fx.workSelection["current"].(map[string]any)["members"] = []any{"REQ-CROSS-310"}
	fx.epics[0].(map[string]any)["requirement_external_ids"] = []any{"REQ-CROSS-310"}
	fx.epics[0].(map[string]any)["user_requirement_external_ids"] = []any{"UR-CLI-008"}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPullScope(env, false, wsNow); err != nil {
		t.Fatalf("pull: %v", err)
	}
	content := readScopeFile(t, filepath.Join(scopeDir(env), "EPIC-CLI-008.md"))
	if !strings.Contains(content, "UR-CLI-008") {
		t.Fatalf("the members block must list the served UR member:\n%s", content)
	}
}
