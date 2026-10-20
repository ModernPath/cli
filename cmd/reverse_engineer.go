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
	root.AddCommand(&cobra.Command{
		Use: "schema", Short: "Read the server JSON Schema for record input", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			env, err := load()
			if err != nil {
				return err
			}
			if env.SystemID <= 0 {
				return fmt.Errorf("connect this workspace to a system before reading its recording schema")
			}
			status, body, err := env.call(http.MethodGet, fmt.Sprintf("/api/v1/systems/%d/reverse-engineering/schema", env.SystemID), nil)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("schema read refused", status, body)
			}
			schema, ok := body["data"].(map[string]any)
			if !ok || schema["$schema"] == nil {
				return fmt.Errorf("schema response is missing its JSON Schema object")
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(schema)
		},
	})
	addReverseAcceptanceCommands(root, load)
	return root
}

// reverseRecordHelp describes recording behavior; the server owns the input model.
const reverseRecordHelp = `Record requirements and their typed traces in the bound System in one atomic request.

Before constructing input, read the current server JSON Schema:

  modernpath reverse-engineer schema

Use that schema for accepted fields, criteria and endpoint types. Do not copy
field lists from older instructions. The schema command requires a bound System
and a server that supports schema discovery; a failed read has no fallback.

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
