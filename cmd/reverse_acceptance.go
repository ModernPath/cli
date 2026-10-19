package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const reverseAcceptancePrefix = "/api/v1/systems/%d/reverse-engineering/acceptance"

type reverseRequirementRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

func addReverseAcceptanceCommands(root *cobra.Command, load func() (*cliEnv, error)) {
	root.AddCommand(newReverseEngineerReviewCommand(load), newReverseEngineerAcceptCommand(load))
}

func newReverseEngineerReviewCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "review",
		Short: "Review current requirement content and evidence",
		Long: `Read a complete review bundle for selected native requirements. This command
uses POST for a read-only preview. Use --json to capture the exact bundle for
reverse-engineer accept --file.`,
		Args: cobra.NoArgs,
	}
	command.Flags().String("file", "", "JSON selection file; - reads stdin (required)")
	command.Flags().Bool("json", false, "emit the complete review bundle as JSON")
	_ = command.MarkFlagRequired("file")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		path, _ := cmd.Flags().GetString("file")
		selection, err := readReverseRequirementSelection(cmd, path)
		if err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		data, err := reverseAcceptanceReviewCall(env, selection)
		if err != nil {
			return err
		}
		return writeReverseReview(cmd, data)
	}
	return command
}

func newReverseEngineerAcceptCommand(load func() (*cliEnv, error)) *cobra.Command {
	command := &cobra.Command{
		Use:   "accept",
		Short: "Accept reviewed existing behavior for requirements",
		Long: `Accept an exact review bundle after a human has decided its content and
evidence are sufficient. The server rechecks the bundle and atomically publishes
the requirements as DONE. Repeat submissions return the existing versions.`,
		Args: cobra.NoArgs,
	}
	command.Flags().String("file", "", "JSON acceptance file; - reads stdin (required)")
	command.Flags().Bool("json", false, "emit the compact acceptance receipt as JSON")
	_ = command.MarkFlagRequired("file")
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		path, _ := cmd.Flags().GetString("file")
		input, err := readReverseAcceptanceInput(cmd, path)
		if err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		if err := validateReverseReviewBundle(input.Reviewed, env.SystemID, nil); err != nil {
			return fmt.Errorf("review bundle does not belong to this workspace binding: %w", err)
		}
		data, err := reverseAcceptanceAcceptCall(env, input)
		if err != nil {
			return err
		}
		receipt, err := reverseAcceptanceReceipt(env.SystemID, data)
		if err != nil {
			return err
		}
		return writeRecord(cmd, receipt)
	}
	return command
}

type reverseAcceptanceInput struct {
	Reviewed  map[string]any
	RawReview json.RawMessage
	Source    string
	Reason    string
}

func readReverseRequirementSelection(cmd *cobra.Command, path string) ([]reverseRequirementRef, error) {
	fields, err := readReverseJSONFields(cmd, path, "requirements")
	if err != nil {
		return nil, err
	}
	raw, ok := fields["requirements"]
	if !ok {
		return nil, fmt.Errorf("input must include %q", "requirements")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return nil, fmt.Errorf("input field %q must be an array", "requirements")
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("input field %q must contain at least one requirement", "requirements")
	}
	selection := make([]reverseRequirementRef, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	for i, rawRow := range rows {
		var row map[string]json.RawMessage
		if err := json.Unmarshal(rawRow, &row); err != nil || row == nil {
			return nil, fmt.Errorf("requirement %d must be a JSON object", i)
		}
		for key := range row {
			if key != "kind" && key != "id" {
				return nil, fmt.Errorf("requirement %d has unknown field %q", i, key)
			}
		}
		var ref reverseRequirementRef
		if raw, ok := row["kind"]; !ok || json.Unmarshal(raw, &ref.Kind) != nil {
			return nil, fmt.Errorf("requirement %d must include a string kind", i)
		}
		if ref.Kind != "user_requirement" && ref.Kind != "system_requirement" {
			return nil, fmt.Errorf("requirement %d kind must be user_requirement or system_requirement", i)
		}
		if raw, ok := row["id"]; !ok || json.Unmarshal(raw, &ref.ID) != nil || !requirementUUIDPattern.MatchString(ref.ID) {
			return nil, fmt.Errorf("requirement %d must include a native UUID id", i)
		}
		key := ref.Kind + ":" + strings.ToLower(ref.ID)
		if seen[key] {
			return nil, fmt.Errorf("requirement %d duplicates %s", i, ref.ID)
		}
		seen[key] = true
		selection = append(selection, ref)
	}
	return selection, nil
}

func readReverseAcceptanceInput(cmd *cobra.Command, path string) (reverseAcceptanceInput, error) {
	fields, err := readReverseJSONFields(cmd, path, "review", "source", "reason")
	if err != nil {
		return reverseAcceptanceInput{}, err
	}
	for _, required := range []string{"review", "source", "reason"} {
		if _, ok := fields[required]; !ok {
			return reverseAcceptanceInput{}, fmt.Errorf("input must include %q", required)
		}
	}
	var source, reason string
	if json.Unmarshal(fields["source"], &source) != nil || strings.TrimSpace(source) == "" {
		return reverseAcceptanceInput{}, fmt.Errorf("input field %q must be a non-empty string", "source")
	}
	if json.Unmarshal(fields["reason"], &reason) != nil || strings.TrimSpace(reason) == "" {
		return reverseAcceptanceInput{}, fmt.Errorf("input field %q must be a non-empty string", "reason")
	}
	reviewed, err := decodeReverseReviewBundle(fields["review"])
	if err != nil {
		return reverseAcceptanceInput{}, fmt.Errorf("input field %q: %w", "review", err)
	}
	return reverseAcceptanceInput{Reviewed: reviewed, RawReview: fields["review"], Source: source, Reason: reason}, nil
}

func readReverseJSONFields(cmd *cobra.Command, path string, allowed ...string) (map[string]json.RawMessage, error) {
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	var reader io.Reader = cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		reader = file
	}
	const maxBytes = 48_000_000
	contents, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxBytes {
		return nil, fmt.Errorf("JSON input exceeds 48 MB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	var fields map[string]json.RawMessage
	if err := decoder.Decode(&fields); err != nil {
		return nil, fmt.Errorf("invalid JSON input: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("input must contain exactly one JSON object")
	}
	if fields == nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	accepted := make(map[string]bool, len(allowed))
	for _, field := range allowed {
		accepted[field] = true
	}
	for field := range fields {
		if !accepted[field] {
			return nil, fmt.Errorf("unknown input field %q", field)
		}
	}
	return fields, nil
}

func reverseAcceptanceReviewCall(env *cliEnv, selection []reverseRequirementRef) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system before reverse-engineering")
	}
	payload := map[string]any{"requirements": selection}
	status, body, err := env.call(http.MethodPost, fmt.Sprintf(reverseAcceptancePrefix+"/preview", env.SystemID), payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("reverse-engineering review refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reverse-engineering review response is missing its data object; no review can be confirmed")
	}
	if err := validateReverseReviewBundle(data, env.SystemID, selection); err != nil {
		return nil, fmt.Errorf("invalid reverse-engineering review response: %w", err)
	}
	return data, nil
}

func reverseAcceptanceAcceptCall(env *cliEnv, input reverseAcceptanceInput) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system before reverse-engineering")
	}
	payload := struct {
		Review json.RawMessage `json:"review"`
		Source string          `json:"source"`
		Reason string          `json:"reason"`
	}{Review: input.RawReview, Source: input.Source, Reason: input.Reason}
	status, body, err := env.call(http.MethodPost, fmt.Sprintf(reverseAcceptancePrefix, env.SystemID), payload)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal("reverse-engineering acceptance refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("reverse-engineering acceptance response is missing its data object; no acceptance can be confirmed")
	}
	if err := validateReverseAcceptanceResponse(data, env.SystemID, input.Reviewed); err != nil {
		return nil, fmt.Errorf("invalid reverse-engineering acceptance response: %w", err)
	}
	return data, nil
}

func decodeReverseReviewBundle(raw json.RawMessage) (map[string]any, error) {
	var bundle map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&bundle); err != nil || bundle == nil {
		return nil, fmt.Errorf("must be an object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return nil, fmt.Errorf("must contain exactly one JSON object")
	}
	if err := validateReverseReviewBundle(bundle, 0, nil); err != nil {
		return nil, err
	}
	return bundle, nil
}

func validateReverseReviewBundle(bundle map[string]any, systemID int, expected []reverseRequirementRef) error {
	gotSystemID, ok := numberAsInt(bundle["system_id"])
	if !ok || gotSystemID <= 0 || (systemID > 0 && gotSystemID != int64(systemID)) {
		return fmt.Errorf("system_id is missing, invalid, or outside the bound System")
	}
	if _, ok := bundle["eligible"].(bool); !ok {
		return fmt.Errorf("eligible must be a boolean")
	}
	if _, ok := bundle["problems"].([]any); !ok {
		return fmt.Errorf("problems must be an array")
	}
	rows, ok := bundle["requirements"].([]any)
	if !ok || len(rows) == 0 {
		return fmt.Errorf("requirements must be a non-empty array")
	}
	if expected != nil && len(rows) != len(expected) {
		return fmt.Errorf("requirements count does not match the requested selection")
	}
	wanted := make(map[string]bool, len(expected))
	for _, ref := range expected {
		wanted[ref.Kind+":"+strings.ToLower(ref.ID)] = true
	}
	seen := make(map[string]bool, len(rows))
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("requirement %d must be an object", i)
		}
		kind, kindOK := row["kind"].(string)
		id, idOK := row["id"].(string)
		if !kindOK || (kind != "user_requirement" && kind != "system_requirement") || !idOK || !requirementUUIDPattern.MatchString(id) {
			return fmt.Errorf("requirement %d has an invalid kind or native UUID id", i)
		}
		key := kind + ":" + strings.ToLower(id)
		if seen[key] || (expected != nil && !wanted[key]) {
			return fmt.Errorf("requirement %d is duplicated or was not requested", i)
		}
		seen[key] = true
		revision, revisionOK := numberAsInt(row["revision"])
		if !revisionOK || revision < 0 {
			return fmt.Errorf("requirement %d is missing its current revision", i)
		}
		if _, ok := row["work_status"].(string); !ok {
			return fmt.Errorf("requirement %d is missing its work status", i)
		}
		if _, ok := row["content"].(map[string]any); !ok {
			return fmt.Errorf("requirement %d is missing its content snapshot", i)
		}
		for _, key := range []string{"criteria", "traces", "evidence"} {
			if _, ok := row[key].([]any); !ok {
				return fmt.Errorf("requirement %d is missing its %s snapshot", i, key)
			}
		}
		versionRows, ok := row["versions"].([]any)
		if !ok {
			return fmt.Errorf("requirement %d is missing its versions readback", i)
		}
		latestVersionRevision := int64(0)
		for j, rawVersion := range versionRows {
			version, ok := rawVersion.(map[string]any)
			if !ok {
				return fmt.Errorf("requirement %d version %d must be an object", i, j)
			}
			if !requirementUUIDPattern.MatchString(str(version, "id")) ||
				version["requirement_kind"] != kind ||
				!strings.EqualFold(str(version, "requirement_id"), id) {
				return fmt.Errorf("requirement %d version %d does not identify this native requirement", i, j)
			}
			versionRevision, ok := numberAsInt(version["revision"])
			if !ok || versionRevision < 1 {
				return fmt.Errorf("requirement %d version %d has an invalid revision", i, j)
			}
			if versionRevision > latestVersionRevision {
				latestVersionRevision = versionRevision
			}
			if _, ok := version["content"].(map[string]any); !ok {
				return fmt.Errorf("requirement %d version %d is missing its content snapshot", i, j)
			}
			for _, field := range []string{"criteria", "traces", "evidence"} {
				if _, ok := version[field].([]any); !ok {
					return fmt.Errorf("requirement %d version %d is missing its %s snapshot", i, j, field)
				}
			}
			if acceptanceKind, ok := version["acceptance_kind"].(string); !ok ||
				(acceptanceKind != "epic_completion" && acceptanceKind != "existing_behavior") {
				return fmt.Errorf("requirement %d version %d has an invalid acceptance kind", i, j)
			}
			if _, exists := version["epic_id"]; !exists {
				return fmt.Errorf("requirement %d version %d is missing Epic readback", i, j)
			}
			if _, exists := version["approved_by_id"]; !exists {
				return fmt.Errorf("requirement %d version %d is missing approver readback", i, j)
			}
			if approvedAt, ok := version["approved_at"].(string); !ok || strings.TrimSpace(approvedAt) == "" {
				return fmt.Errorf("requirement %d version %d is missing its approval time", i, j)
			}
			if _, exists := version["reason"]; !exists {
				return fmt.Errorf("requirement %d version %d is missing reason readback", i, j)
			}
		}
		status, _ := row["work_status"].(string)
		if status == "DONE" && len(versionRows) > 0 {
			if latestVersionRevision != revision {
				return fmt.Errorf("requirement %d current revision does not match its latest accepted version", i)
			}
		}
	}
	return nil
}

func validateReverseAcceptanceResponse(data map[string]any, systemID int, reviewed map[string]any) error {
	gotSystemID, ok := numberAsInt(data["system_id"])
	if !ok || gotSystemID != int64(systemID) {
		return fmt.Errorf("system_id is missing or outside the bound System")
	}
	reviewedRows, _ := reviewed["requirements"].([]any)
	rows, ok := data["requirements"].([]any)
	if !ok || len(rows) != len(reviewedRows) {
		return fmt.Errorf("requirements count does not match the reviewed bundle")
	}
	wanted := make(map[string]bool, len(reviewedRows))
	for _, raw := range reviewedRows {
		row, _ := raw.(map[string]any)
		wanted[requirementKey(row)] = true
	}
	seen := make(map[string]bool, len(rows))
	for i, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("requirement receipt %d must be an object", i)
		}
		key := requirementKey(row)
		if key == ":" || seen[key] || !wanted[key] {
			return fmt.Errorf("requirement receipt %d is missing, duplicated, or outside the reviewed bundle", i)
		}
		seen[key] = true
		if row["work_status"] != "DONE" {
			return fmt.Errorf("requirement receipt %d does not confirm DONE", i)
		}
		if revision, ok := numberAsInt(row["revision"]); !ok || revision < 1 {
			return fmt.Errorf("requirement receipt %d is missing its resulting revision", i)
		}
		result, ok := row["result"].(string)
		if !ok || (result != "accepted" && result != "already_accepted") {
			return fmt.Errorf("requirement receipt %d has an invalid result", i)
		}
		version, ok := row["version"].(map[string]any)
		if !ok || !requirementUUIDPattern.MatchString(str(version, "id")) {
			return fmt.Errorf("requirement receipt %d is missing its accepted version", i)
		}
		if revision, ok := numberAsInt(version["revision"]); !ok || revision < 1 {
			return fmt.Errorf("requirement receipt %d has an invalid accepted version revision", i)
		}
		if version["acceptance_kind"] != "existing_behavior" {
			return fmt.Errorf("requirement receipt %d has an invalid acceptance kind", i)
		}
		if approvedBy, ok := numberAsInt(version["approved_by_id"]); !ok || approvedBy <= 0 {
			return fmt.Errorf("requirement receipt %d is missing the accepting actor", i)
		}
		if approvedAt, ok := version["approved_at"].(string); !ok || strings.TrimSpace(approvedAt) == "" {
			return fmt.Errorf("requirement receipt %d is missing the acceptance time", i)
		}
		if reason, ok := version["reason"].(string); !ok || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("requirement receipt %d is missing the acceptance reason", i)
		}
	}
	return nil
}

func requirementKey(row map[string]any) string {
	kind, _ := row["kind"].(string)
	id, _ := row["id"].(string)
	return kind + ":" + strings.ToLower(id)
}

func reverseAcceptanceReceipt(systemID int, data map[string]any) (map[string]any, error) {
	rows, _ := data["requirements"].([]any)
	receipts := make([]any, 0, len(rows))
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("acceptance response contains an invalid requirement receipt")
		}
		receipts = append(receipts, map[string]any{
			"kind": row["kind"], "id": row["id"], "revision": row["revision"],
			"work_status": row["work_status"], "result": row["result"], "version": row["version"],
		})
	}
	return map[string]any{"system_id": systemID, "requirements": receipts}, nil
}

func writeReverseReview(cmd *cobra.Command, data map[string]any) error {
	machine, _ := cmd.Flags().GetBool("json")
	if machine {
		return writeCommandJSON(cmd, data)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Review bundle for System %v:\n", data["system_id"]); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return err
}
