package opschema

// REQ-CROSS-012 / TASK-SY-405 — pre-send validation + the vendored-copy
// lockstep guard (non-negotiable #3: one schema source; the server's priv
// artifact is it, this embed must byte-match).

import (
	"bytes"
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
