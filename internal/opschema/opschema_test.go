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
		op("upsert_document", map[string]any{
			"name":          "03 — Architecture",
			"document_type": "architecture", "provenance": "derived",
			"content_md": "# Architecture",
		}),
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
	err := ValidateOps([]map[string]any{op("upsert_document", map[string]any{
		"content_md": "# Architecture",
	})})
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("name")) {
		t.Fatalf("missing required document name must fail naming it, got %v", err)
	}
}

// Lockstep guards (contracts/README copy rule): the vendored embed must
// byte-match the canonical contracts/sync/v1.schema.json AND the server's
// priv copy. Run only in the monorepo checkout where those trees exist.
func TestVendoredSchemaMatchesCanonicalAndServer(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	sources := map[string]string{
		"contracts/sync/v1.schema.json (canonical)":        filepath.Join(thisFile, "../../../../../..", "contracts/sync/v1.schema.json"),
		"apps/core_http_api/priv/sync-schema/v1.json copy": filepath.Join(thisFile, "../../../../..", "apps/core_http_api/priv/sync-schema/v1.json"),
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
