package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newReverseEngineerCommand(cliEnvLoad)) }

// Reverse engineering records proposed native requirements and their source
// file traces. Analysis and content decisions belong to the caller.
func newReverseEngineerCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{
		Use:   "reverse-engineer",
		Short: "Record and accept requirements found in existing behavior",
		Long:  "Record sourced requirements, review their current content and evidence, and explicitly accept existing behavior.",
	}
	command := &cobra.Command{
		Use:   "record",
		Short: "Record requirements and traces from a JSON file",
		Long:  reverseRecordHelp,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("file")
			input, err := readReverseRecord(cmd, path)
			if err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			body, err := reverseRecordCall(env, input)
			if err != nil {
				return err
			}
			data, _ := body["data"].(map[string]any)
			requirements, _ := data["requirements"].([]any)
			traces, _ := data["traces"].([]any)
			items := make([]any, 0, len(requirements))
			for _, raw := range requirements {
				row, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("reverse-engineering response contains an invalid requirement")
				}
				items = append(items, selectKeys(row, []string{"id", "kind", "display_number", "title", "work_status"}))
			}
			return writeRecord(cmd, map[string]any{"requirements": items, "trace_count": len(traces)})
		},
	}
	command.Flags().String("file", "", "JSON input file; - reads stdin (required)")
	command.Flags().Bool("json", false, "emit the compact receipt as JSON")
	_ = command.MarkFlagRequired("file")
	root.AddCommand(command)
	addReverseAcceptanceCommands(root, load)
	return root
}

// reverseRecordHelp is the record file's contract as the server enforces it
// (Core.RDD.ReverseEngineering.Recording). It is the only place an agent can
// read the field names from, so it names every accepted key.
const reverseRecordHelp = `Record requirements and their typed traces in the bound System in one atomic
request. The file is one JSON object with exactly two arrays:

  {"requirements": [...], "traces": [...]}

A requirement object:

  kind         user_requirement | system_requirement (required)
  id           a UUID you generate; optional. Give every new requirement one,
               because a trace in the same file names it by this id.
  title, description, context, context_name, priority, release_note,
  detail_md, delivered_revision
  actor, intended_use                           (user_requirement only)
  boundary, rationale, verification_method      (system_requirement only)
  criteria     a list of {id?, kind: criterion | scenario, position,
               statement (criterion) | given, when, then (scenario),
               status?: active, introduced_in_revision?}; a scenario needs
               given and then, a criterion needs statement

A trace object:

  source_type, target_type   user_requirement, system_requirement, test_case,
                             source_file, criterion (UUID endpoints, in
                             source_id / target_id) or system_document,
                             file_analysis, subsystem, epic (integer
                             endpoints, in source_integer_id / target_integer_id)
  link_kind                  implements | verifies | derives | relates
                             (a requirement-to-document trace must be relates)
  id                         optional UUID
  A source_file endpoint is named by repository_key and path (revision is
  optional, descriptive context) instead of an id; the server resolves or
  creates the file record. Every other endpoint must already exist in the
  System, so an existing requirement is named by its UUID in the trace alone.

Example:

  {"requirements":[{"kind":"system_requirement","id":"<uuid>","title":"Catalog lookup",
    "description":"...","boundary":"...","verification_method":"integration test",
    "criteria":[{"kind":"criterion","position":1,"statement":"..."}]}],
   "traces":[{"source_type":"system_requirement","source_id":"<uuid>",
    "target_type":"source_file","repository_key":"catalog","path":"src/catalog.go","link_kind":"implements"},
    {"source_type":"system_requirement","source_id":"<uuid>","target_type":"user_requirement",
    "target_id":"<existing UR uuid>","link_kind":"derives"}]}

A requirement without an id receives a fresh server UUID on every submission.
With a supplied UUID the System does not hold, it is created with that UUID.
Both receive a display reference, revision 0 and PROPOSED. A requirement whose
id the System holds is reused when the supplied fields and criteria equal
the stored ones; different content refuses the whole request
(existing_requirement_conflict), so a content change is Epic work, not a
record. A trace with the same endpoints and link kind as an active one is
reused. Retain and reuse every requirement UUID for retry safety. Repeating a
file that omits requirement ids creates duplicate requirements; if the first
response was lost, inspect the stored requirements before retrying. Reusing
the same requirement UUIDs and unchanged content avoids creating new requirements.
Any refusal leaves nothing written. Recording is neither verification nor
acceptance; nothing here hashes files or needs a clean checkout.

The receipt lists each requirement's id, kind, display_number, title and
work_status, and the number of traces. Read back with requirements get
<uuid> --kind user|system --full and traces list --entity-type <kind>
--entity-id <uuid>.`

type reverseRecordPayload struct {
	Requirements []map[string]any `json:"requirements"`
	Traces       []map[string]any `json:"traces"`
}

func readReverseRecord(cmd *cobra.Command, path string) (reverseRecordPayload, error) {
	var reader io.Reader = cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return reverseRecordPayload{}, err
		}
		defer file.Close()
		reader = file
	}
	const maxBytes = 48_000_000
	contents, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return reverseRecordPayload{}, err
	}
	if len(contents) > maxBytes {
		return reverseRecordPayload{}, fmt.Errorf("JSON input exceeds 48 MB limit")
	}
	var fields map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(contents))
	if err := decoder.Decode(&fields); err != nil {
		return reverseRecordPayload{}, fmt.Errorf("invalid JSON input: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return reverseRecordPayload{}, fmt.Errorf("input must contain exactly one JSON value")
	}
	if fields == nil {
		return reverseRecordPayload{}, fmt.Errorf("input must be a JSON object")
	}
	for key := range fields {
		if key != "requirements" && key != "traces" {
			return reverseRecordPayload{}, fmt.Errorf("unknown input field %q", key)
		}
	}
	for _, key := range []string{"requirements", "traces"} {
		raw, ok := fields[key]
		if !ok {
			return reverseRecordPayload{}, fmt.Errorf("input must include %q", key)
		}
		if value := bytes.TrimSpace(raw); len(value) == 0 || value[0] != '[' {
			return reverseRecordPayload{}, fmt.Errorf("input field %q must be an array", key)
		}
	}
	var payload reverseRecordPayload
	decoder = json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return reverseRecordPayload{}, fmt.Errorf("invalid native input: %w", err)
	}
	return payload, nil
}

func reverseRecordCall(env *cliEnv, payload reverseRecordPayload) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system before reverse-engineering")
	}
	status, body, err := env.call(http.MethodPost, fmt.Sprintf("/api/v1/systems/%d/reverse-engineering/requirements", env.SystemID), payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("reverse-engineering refused", status, body)
	}
	if _, ok := body["data"].(map[string]any); !ok {
		return nil, fmt.Errorf("reverse-engineering response is missing its data object; no success can be confirmed")
	}
	return body, nil
}
