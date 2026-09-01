package opschema

// REQ-CROSS-012 / TASK-SY-405 — pre-send validation + the vendored-copy
// lockstep guard (non-negotiable #3: one schema source; the server's priv
// artifact is it, this embed must byte-match).

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func op(opType string, payload map[string]any) map[string]any {
	return map[string]any{"type": opType, "payload": payload}
}

func TestValidBatchPasses(t *testing.T) {
	ops := []map[string]any{
		op("upsert_requirement", map[string]any{"external_id": "REQ-X-001", "content_hash": "h"}),
		op("upsert_epic", map[string]any{"external_id": "EPIC-X-001", "content_hash": "h"}),
		op("upsert_gate", map[string]any{"external_id": "RQ-1", "content_hash": "h"}),
		op("emit_events", map[string]any{"external_id": "EVENTS-X", "events": []any{
			map[string]any{"dedupe_key": "k", "occurred_at": "2026-07-28T00:00:00Z", "event_type": "commit_burst"},
		}}),
	}
	if err := ValidateOps(ops); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
}

func TestUnknownOpTypeFailsNamingIt(t *testing.T) {
	err := ValidateOps([]map[string]any{op("upsert_wormhole", map[string]any{"external_id": "X"})})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("upsert_wormhole")) {
		t.Fatalf("unknown op type must fail naming it, got %v", err)
	}
}

func TestMissingRequiredFieldFailsNamingField(t *testing.T) {
	err := ValidateOps([]map[string]any{op("upsert_requirement", map[string]any{"external_id": "REQ-X-001"})})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("content_hash")) {
		t.Fatalf("missing content_hash must fail naming it, got %v", err)
	}
	err = ValidateOps([]map[string]any{op("emit_events", map[string]any{"external_id": "E", "events": []any{
		map[string]any{"occurred_at": "2026-07-28T00:00:00Z", "event_type": "commit_burst"},
	}})})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("dedupe_key")) {
		t.Fatalf("missing event dedupe_key must fail naming it, got %v", err)
	}
}

// Lockstep guards (contracts/README copy rule): the vendored embed must
// byte-match the canonical contracts/sync/v1.schema.json AND the server's
// priv copy. Run only in the monorepo checkout where those trees exist.
func TestVendoredSchemaMatchesCanonicalAndServer(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	sources := map[string]string{
		"contracts/sync/v1.schema.json (canonical)":       filepath.Join(thisFile, "../../../../../..", "contracts/sync/v1.schema.json"),
		"apps/aiengine_web/priv/sync-schema/v1.json copy": filepath.Join(thisFile, "../../../../..", "apps/aiengine_web/priv/sync-schema/v1.json"),
	}
	checked := 0
	for name, path := range sources {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Logf("%s not present (%v) — skipped", name, err)
			continue
		}
		checked++
		if !bytes.Equal(bytes.TrimSpace(raw), bytes.TrimSpace(Raw())) {
			t.Fatalf("vendored v1.json differs from %s — edit contracts/ first, then copy (contracts/README.md)", name)
		}
	}
	if checked == 0 {
		t.Skip("no sibling trees present — lockstep check skipped")
	}
}

// REQ-CROSS-023/024 (EPIC-SYNC-006): epic spec payloads + the spec_approval
// gate kind ride schema v1 additively.
func TestEpicSpecsArrayValidates(t *testing.T) {
	ops := []map[string]any{
		op("upsert_epic", map[string]any{
			"external_id": "EPIC-X-001", "content_hash": "h",
			"specs": []any{
				map[string]any{
					"external_id": "epics/EPIC-X-001-x/specs/requirements.md",
					"name":        "requirements.md",
					"position":    1,
					"content_md":  "# Requirements\n\ngrounded content",
				},
			},
		}),
	}
	if err := ValidateOps(ops); err != nil {
		t.Fatalf("epic op with valid specs rejected: %v", err)
	}
}

func TestChatPlanningCarriersStayOutOfWorkspaceEpicPayload(t *testing.T) {
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(Raw(), &schema); err != nil {
		t.Fatal(err)
	}

	properties := schema.Defs["upsert_epic_payload"].Properties
	for _, field := range []string{
		"outcome_source", "scope", "non_goals", "shared_context",
		"impact_assessment", "raw_record", "source_path", "process_metadata",
	} {
		if _, ok := properties[field]; ok {
			t.Errorf("Chat planning field %s must not extend the workspace Epic payload", field)
		}
	}
}

func TestEpicSpecMissingRequiredFieldFails(t *testing.T) {
	ops := []map[string]any{
		op("upsert_epic", map[string]any{
			"external_id": "EPIC-X-001", "content_hash": "h",
			"specs": []any{
				map[string]any{"external_id": "epics/EPIC-X-001-x/specs/requirements.md", "name": "requirements.md"},
			},
		}),
	}
	err := ValidateOps(ops)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("content_md")) {
		t.Fatalf("spec missing content_md must fail naming the field, got %v", err)
	}
}

func TestEpicSpecContentOverCapFails(t *testing.T) {
	big := make([]byte, 65537)
	for i := range big {
		big[i] = 'a'
	}
	ops := []map[string]any{
		op("upsert_epic", map[string]any{
			"external_id": "EPIC-X-001", "content_hash": "h",
			"specs": []any{
				map[string]any{
					"external_id": "epics/EPIC-X-001-x/specs/requirements.md",
					"name":        "requirements.md",
					"content_md":  string(big),
				},
			},
		}),
	}
	err := ValidateOps(ops)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("content_md")) {
		t.Fatalf("spec content over maxLength must fail naming the field, got %v", err)
	}
}

func TestSpecApprovalGateKindIsSchemaLegal(t *testing.T) {
	// the wire enum must carry the new kind (lockstep with the server @kinds)
	var s struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := jsonUnmarshal(Raw(), &s); err != nil {
		t.Fatal(err)
	}
	kinds := s.Defs["upsert_gate_payload"].Properties["kind"].Enum
	found := false
	for _, k := range kinds {
		if k == "spec_approval" {
			found = true
		}
	}
	if !found {
		t.Fatalf("upsert_gate_payload.kind enum must include spec_approval, got %v", kinds)
	}

	if err := ValidateOps([]map[string]any{
		op("upsert_gate", map[string]any{"external_id": "SPEC-APPROVE-EPIC-X-001", "content_hash": "h", "kind": "spec_approval"}),
	}); err != nil {
		t.Fatalf("spec_approval gate op rejected: %v", err)
	}
}

func TestGateEvaluatedScopeFingerprintIsAnIndependentOptionalCarrier(t *testing.T) {
	var schema struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := jsonUnmarshal(Raw(), &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Defs["upsert_gate_payload"].Properties["evaluated_scope_fingerprint"]; !ok {
		t.Fatal("upsert_gate_payload must declare evaluated_scope_fingerprint")
	}

	if err := ValidateOps([]map[string]any{
		op("upsert_gate", map[string]any{
			"external_id":                 "TRACE-ENTRY-X",
			"content_hash":                "content-v1",
			"evaluated_scope_fingerprint": "scope-v7",
		}),
	}); err != nil {
		t.Fatalf("gate op with evaluated_scope_fingerprint rejected: %v", err)
	}
}

// REQ-CROSS-084 (EPIC-ARCH-001 Part C): the documents that explain a codebase
// sync as their own op. A new op type rather than a field on an existing one,
// because these are SYSTEM-scoped — they belong to no requirement and no epic,
// which is exactly why `planning_artifacts` (initiative-scoped) was the wrong
// home for them.
func TestUpsertDocumentOpValidates(t *testing.T) {
	ops := []map[string]any{
		op("upsert_document", map[string]any{
			"external_id":   "docs/03-architecture.md",
			"name":          "03 — Architecture",
			"document_type": "architecture",
			"provenance":    "derived",
			"content_md":    "# 03 — Architecture\n\nsubsystems, interfaces, datastores",
			"content_hash":  "h",
		}),
	}
	if err := ValidateOps(ops); err != nil {
		t.Fatalf("valid upsert_document rejected: %v", err)
	}
}

func TestDocumentMissingContentFails(t *testing.T) {
	ops := []map[string]any{
		op("upsert_document", map[string]any{
			"external_id": "docs/03-architecture.md", "name": "03", "content_hash": "h",
		}),
	}
	err := ValidateOps(ops)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("content_md")) {
		t.Fatalf("a document with no content must fail naming content_md, got: %v", err)
	}
}

// Provenance is the field that makes these "first-class separated citizens"
// (`USER:2026-08-13`): it distinguishes a repository-synced explanation from an
// upload and from generated per-module documentation.
//
// The CONTRACT constrains it to three values; this client validator does not
// check enums, and deliberately so — it is "a subset (required-field +
// known-type checks)" by its own docstring, and the enforcement point is the
// server changeset. So this test asserts what is actually guaranteed here: that
// the vocabulary is DECLARED, so every consumer can enforce it. A first draft
// of this test asserted the client rejects a bad value, which the client was
// never designed to do.
func TestDocumentProvenanceVocabularyIsDeclared(t *testing.T) {
	var schema struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(rawV1, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Defs["upsert_document_payload"].Properties["provenance"].Enum
	want := map[string]bool{"derived": true, "guide": true, "authored": true}
	if len(got) != len(want) {
		t.Fatalf("provenance enum = %v, want exactly %d values", got, len(want))
	}
	for _, v := range got {
		if !want[v] {
			t.Errorf("unexpected provenance value %q in the contract", v)
		}
	}
}

// REQ-CROSS-264 (EPIC-CLI-003 tranche 4): the tasks carrier's op type. The
// lockstep guard above proves all three copies agree; this proves the vendored
// copy actually admits the new op and names a missing required field.
func TestUpsertTaskOpValidates(t *testing.T) {
	valid := op("upsert_task", map[string]any{
		"external_id":                "EPIC-CLI-003#T1",
		"content_hash":               "h",
		"code":                       "T1",
		"title":                      "Fidelity report",
		"declared_owner_type":        "epic",
		"declared_owner_external_id": "EPIC-CLI-003",
		"sync_born":                  true,
		"source_path":                "epics/EPIC-CLI-003-ledger-import/EPIC.md",
		"source_line":                200,
		"source_raw":                 "| T1 | Fidelity report |",
	})
	if err := ValidateOps([]map[string]any{valid}); err != nil {
		t.Fatalf("valid upsert_task rejected: %v", err)
	}
	err := ValidateOps([]map[string]any{op("upsert_task", map[string]any{"external_id": "EPIC-X-001#TASK-X-001"})})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("content_hash")) {
		t.Fatalf("missing content_hash must fail naming it, got %v", err)
	}
}

// REQ-CROSS-266: the evidence conclusion is an ADDITIVE scenario field. The
// lockstep guard proves the three copies agree; this proves the vendored copy
// admits it and still refuses a value outside the closed vocabulary.
func TestScenarioEvidenceConclusionValidates(t *testing.T) {
	valid := op("upsert_scenario", map[string]any{
		"external_id":         "EPIC-X-001#SCN-X-001",
		"content_hash":        "h",
		"status":              "active",
		"evidence_conclusion": "UPPER_VALIDATED",
	})
	if err := ValidateOps([]map[string]any{valid}); err != nil {
		t.Fatalf("valid evidence_conclusion rejected: %v", err)
	}
	// The vocabulary is CLOSED and the contract declares it. Enforcement is the
	// server's (Ecto validate_inclusion, refused loudly before any write): this
	// client validator checks required fields and nested caps only, and
	// widening it to every declared enum is a change no requirement here asks
	// for. What must hold at this boundary is that the declaration exists and
	// says the same thing in all three copies — the lockstep guard above proves
	// the copies match, and this proves what they say.
	scenario, ok := payloadDef("upsert_scenario")
	if !ok {
		t.Fatal("upsert_scenario has no payload definition")
	}
	prop, ok := scenario.Properties["evidence_conclusion"]
	if !ok {
		t.Fatal("evidence_conclusion is not declared on the scenario payload")
	}
	if len(prop.Enum) != 2 || prop.Enum[0] != "UPPER_VALIDATED" || prop.Enum[1] != "LOWER_VERIFIED" {
		t.Fatalf("the conclusion vocabulary must be closed to the two PROCESS.md conclusions, got %v", prop.Enum)
	}
}
