// Package opschema — the vendored sync op schema and pre-send validation
// (REQ-CROSS-012, EPIC-SYNC-004 TASK-SY-405/407). The canonical source is
// contracts/sync/v1.schema.json (JSON Schema 2020-12, the central contract
// home per OQ-A1x USER:2026-07-28); this embed and the server's priv copy are
// byte-identical derivatives, kept honest by lockstep tests. Invalid ops fail
// locally, naming the op and field — they never hit the wire.
//
// The validator is deliberately a subset (required-field + known-type checks
// derived from the schema's $defs) — full JSON Schema validation belongs to
// the A2 conformance gate (REQ-CROSS-006), not the CLI hot path.
package opschema

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed v1.json
var rawV1 []byte

// SchemaVersion is the op-schema version this CLI speaks (the batch envelope's
// schema_version field; the artifact's x-schema-version).
const SchemaVersion = 1

type propSpec struct {
	MaxLength int `json:"maxLength"`
}

type defSpec struct {
	Required   []string            `json:"required"`
	Properties map[string]propSpec `json:"properties"`
}

type schema struct {
	XSchemaVersion int                `json:"x-schema-version"`
	Defs           map[string]defSpec `json:"$defs"`
}

var v1 = func() schema {
	var s schema
	if err := json.Unmarshal(rawV1, &s); err != nil {
		panic("opschema: vendored v1.json is invalid: " + err.Error())
	}
	if s.XSchemaVersion != SchemaVersion {
		panic(fmt.Sprintf("opschema: vendored artifact is x-schema-version %d, CLI speaks %d", s.XSchemaVersion, SchemaVersion))
	}
	return s
}()

// Raw returns the vendored schema artifact bytes (for display / comparison).
func Raw() []byte { return rawV1 }

// payloadDef resolves an op type to its $defs payload entry
// (upsert_requirement -> $defs.upsert_requirement_payload).
func payloadDef(opType string) (defSpec, bool) {
	spec, ok := v1.Defs[opType+"_payload"]
	return spec, ok
}

// ValidateOps checks a built op batch against the vendored schema: known op
// types, required payload fields, required event fields. The first violation
// is returned as an error naming the op index, external id, and field.
func ValidateOps(ops []map[string]any) error {
	eventSpec := v1.Defs["work_event"]
	for i, op := range ops {
		opType, _ := op["type"].(string)
		payload, _ := op["payload"].(map[string]any)
		externalID := ""
		if payload != nil {
			externalID, _ = payload["external_id"].(string)
		}

		spec, known := payloadDef(opType)
		if !known {
			return fmt.Errorf("op %d (%s): unknown op type %q (schema v%d)", i, externalID, opType, SchemaVersion)
		}
		if payload == nil {
			return fmt.Errorf("op %d: missing payload", i)
		}
		for _, field := range spec.Required {
			if isMissing(payload[field]) {
				return fmt.Errorf("op %d (%s, %s): missing required field %q", i, opType, externalID, field)
			}
		}
		// REQ-CROSS-023: epic spec items get the same nested treatment as
		// events — required fields + the content_md maxLength cap. Builders
		// cap-and-warn; a violation here is a builder bug caught pre-wire.
		if opType == "upsert_epic" {
			specSpec := v1.Defs["spec"]
			specs, _ := payload["specs"].([]any)
			for j, s := range specs {
				spec, _ := s.(map[string]any)
				if spec == nil {
					return fmt.Errorf("op %d (%s): spec %d is not an object", i, externalID, j)
				}
				for _, field := range specSpec.Required {
					if isMissing(spec[field]) {
						return fmt.Errorf("op %d (%s): spec %d missing required field %q", i, externalID, j, field)
					}
				}
				for field, prop := range specSpec.Properties {
					if prop.MaxLength > 0 {
						if str, ok := spec[field].(string); ok && len([]rune(str)) > prop.MaxLength {
							return fmt.Errorf("op %d (%s): spec %d field %q exceeds maxLength %d", i, externalID, j, field, prop.MaxLength)
						}
					}
				}
			}
		}
		if opType == "emit_events" {
			events, _ := payload["events"].([]any)
			for j, e := range events {
				event, _ := e.(map[string]any)
				if event == nil {
					return fmt.Errorf("op %d (%s): event %d is not an object", i, externalID, j)
				}
				for _, field := range eventSpec.Required {
					if isMissing(event[field]) {
						return fmt.Errorf("op %d (%s): event %d missing required field %q", i, externalID, j, field)
					}
				}
			}
		}
	}
	return nil
}

func isMissing(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return false
}
