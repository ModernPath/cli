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
		Short: "Check current verification before running existing tests",
		Long: `Check current requirement status, required test identities and current execution
results before running tests. Selection: {"requirements":[{"kind":"system_requirement","id":"<UUID>"}]}.
Each unresolved requirement uses one current run; shared tests and results appear once.
Reuse applicable passing results and skip DONE requirements. Missing, changed or
nonpassing evidence blocks acceptance. Keep a reserved run's ID for submission retries;
never reserve a second run merely to retry finalization. This read writes no evidence
or approval and returns eligible/problems without content snapshots, historical versions
or old execution lists.
Read requirement content separately when needed. Human acceptance remains separate`,
		Args: cobra.NoArgs,
	}
	command.Flags().String("file", "", "JSON selection file; - reads stdin (required)")
	command.Flags().Bool("json", false, "emit current verification status and identities as JSON")
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
		Long: `Apply an explicit human acceptance decision to eligible current database state.
Input: {"requirements":[{"kind":"system_requirement","id":"<UUID>"}],
"source":"USER:<source>","reason":"<human decision>"}.
The server reads current content and native evidence under locks, then publishes
all selected requirements atomically. No review bundle is uploaded. A repeated
acceptance returns the existing accepted version and publishes nothing twice.
Already DONE requirements from other completion paths return skipped unchanged.
Read state and the approval audit after an uncertain response`,
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
	Requirements []reverseRequirementRef `json:"requirements"`
	Source       string                  `json:"source"`
	Reason       string                  `json:"reason"`
}

func readReverseRequirementSelection(cmd *cobra.Command, path string) ([]reverseRequirementRef, error) {
	fields, err := readReverseJSONFields(cmd, path, "requirements")
	if err != nil {
		return nil, err
	}
	return decodeReverseRequirementSelection(fields)
}

func decodeReverseRequirementSelection(fields map[string]json.RawMessage) ([]reverseRequirementRef, error) {
	raw, ok := fields["requirements"]
	if !ok {
		return nil, fmt.Errorf("input must include %q", "requirements")
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return nil, fmt.Errorf("input field %q must be an array", "requirements")
	}
	if len(rows) == 0 || len(rows) > 1000 {
		return nil, fmt.Errorf("input field %q must contain between 1 and 1000 requirements", "requirements")
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
	fields, err := readReverseJSONFields(cmd, path, "requirements", "source", "reason")
	if err != nil {
		return reverseAcceptanceInput{}, err
	}
	selection, err := decodeReverseRequirementSelection(fields)
	if err != nil {
		return reverseAcceptanceInput{}, err
	}
	var source, reason string
	if json.Unmarshal(fields["source"], &source) != nil || !strings.HasPrefix(source, "USER:") || strings.TrimSpace(strings.TrimPrefix(source, "USER:")) == "" {
		return reverseAcceptanceInput{}, fmt.Errorf("source must retain an explicit USER: decision")
	}
	if json.Unmarshal(fields["reason"], &reason) != nil || strings.TrimSpace(reason) == "" {
		return reverseAcceptanceInput{}, fmt.Errorf("reason must be a non-empty human decision")
	}
	return reverseAcceptanceInput{Requirements: selection, Source: source, Reason: reason}, nil
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
	status, body, err := env.write(http.MethodPost, fmt.Sprintf(reverseAcceptancePrefix+"/preview", env.SystemID), payload)
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
	if err := validateReverseReview(data, env.SystemID, selection); err != nil {
		return nil, fmt.Errorf("invalid reverse-engineering review response: %w", err)
	}
	return data, nil
}

func reverseAcceptanceAcceptCall(env *cliEnv, input reverseAcceptanceInput) (map[string]any, error) {
	if env.SystemID <= 0 {
		return nil, fmt.Errorf("connect this workspace to a system before reverse-engineering")
	}
	status, body, err := env.write(http.MethodPost, fmt.Sprintf(reverseAcceptancePrefix, env.SystemID), input)
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
	if err := validateReverseAcceptanceResponse(data, env.SystemID, reverseSelectionMap(input.Requirements)); err != nil {
		return nil, fmt.Errorf("invalid reverse-engineering acceptance response: %w", err)
	}
	return data, nil
}

func reverseSelectionMap(refs []reverseRequirementRef) map[string]any {
	rows := make([]any, 0, len(refs))
	for _, ref := range refs {
		rows = append(rows, map[string]any{"kind": ref.Kind, "id": ref.ID})
	}
	return map[string]any{"requirements": rows}
}

func validateReverseReview(data map[string]any, systemID int, expected []reverseRequirementRef) error {
	gotSystemID, ok := numberAsInt(data["system_id"])
	if !ok || gotSystemID != int64(systemID) {
		return fmt.Errorf("review is outside the bound System")
	}
	if _, ok := data["eligible"].(bool); !ok {
		return fmt.Errorf("eligible must be a boolean")
	}
	if _, ok := data["problems"].([]any); !ok {
		return fmt.Errorf("problems must be an array")
	}
	records := make(map[string]map[string]map[string]any)
	for _, name := range []string{"runs", "evidence", "test_cases"} {
		rows, ok := data[name].([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", name)
		}
		records[name] = make(map[string]map[string]any)
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			id := strings.ToLower(str(row, "id"))
			if !ok || !requirementUUIDPattern.MatchString(id) || records[name][id] != nil {
				return fmt.Errorf("%s contains an invalid or duplicate identity", name)
			}
			records[name][id] = row
		}
	}
	for _, row := range records["test_cases"] {
		if rev, ok := numberAsInt(row["revision"]); !ok || rev < 0 {
			return fmt.Errorf("test revision is missing")
		}
	}
	for _, row := range records["runs"] {
		if _, ok := row["sha"].(string); !ok {
			return fmt.Errorf("tested code revision is missing")
		}
		if row["status"] != "running" && row["status"] != "complete" && row["status"] != "error" {
			return fmt.Errorf("invalid run status")
		}
	}
	for _, row := range records["evidence"] {
		if records["runs"][strings.ToLower(str(row, "run_id"))] == nil || records["test_cases"][strings.ToLower(str(row, "target_id"))] == nil {
			return fmt.Errorf("result references an unknown run or test")
		}
		if _, ok := row["result"].(string); !ok {
			return fmt.Errorf("result outcome is missing")
		}
		if _, ok := row["validity"].(string); !ok {
			return fmt.Errorf("result validity is missing")
		}
	}
	rows, ok := data["requirements"].([]any)
	if !ok || len(rows) == 0 || (expected != nil && len(rows) != len(expected)) {
		return fmt.Errorf("requirements do not match the selection")
	}
	wanted := make(map[string]bool)
	for _, ref := range expected {
		wanted[ref.Kind+":"+strings.ToLower(ref.ID)] = true
	}
	seen := make(map[string]bool)
	for _, raw := range rows {
		row, ok := raw.(map[string]any)
		key := requirementKey(row)
		if !ok || (row["kind"] != "user_requirement" && row["kind"] != "system_requirement") ||
			!requirementUUIDPattern.MatchString(str(row, "id")) || seen[key] || (expected != nil && !wanted[key]) {
			return fmt.Errorf("requirement is invalid, duplicated or outside the selection")
		}
		seen[key] = true
		if rev, ok := numberAsInt(row["revision"]); !ok || rev < 0 {
			return fmt.Errorf("requirement revision is missing")
		}
		if _, ok := row["work_status"].(string); !ok {
			return fmt.Errorf("requirement status is missing")
		}
		for _, field := range []string{"content", "traces", "evidence", "versions"} {
			if _, exists := row[field]; exists {
				return fmt.Errorf("current review contains a %s snapshot", field)
			}
		}
		criteria, ok := row["criteria"].([]any)
		if !ok {
			return fmt.Errorf("criterion checks must be an array")
		}
		for _, raw := range criteria {
			criterion, ok := raw.(map[string]any)
			if !ok || !requirementUUIDPattern.MatchString(str(criterion, "criterion_id")) {
				return fmt.Errorf("invalid criterion identity")
			}
			if _, ok := criterion["state"].(string); !ok {
				return fmt.Errorf("criterion state is missing")
			}
			for field, collection := range map[string]string{"test_keys": "test_cases", "result_ids": "evidence"} {
				refs, ok := criterion[field].([]any)
				if !ok {
					return fmt.Errorf("%s must be an array", field)
				}
				for _, ref := range refs {
					id, ok := ref.(string)
					if !ok || records[collection][strings.ToLower(id)] == nil {
						return fmt.Errorf("criterion references an unknown %s identity", collection)
					}
				}
			}
			if id := str(criterion, "run_id"); id != "" && records["runs"][strings.ToLower(id)] == nil {
				return fmt.Errorf("criterion references an unknown run")
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
		if revision, ok := numberAsInt(row["revision"]); !ok || revision < 0 || (revision == 0 && row["result"] != "skipped") {
			return fmt.Errorf("requirement receipt %d is missing its resulting revision", i)
		}
		result, ok := row["result"].(string)
		if ok && result == "skipped" {
			continue
		}
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
		receipt := map[string]any{
			"kind": row["kind"], "id": row["id"], "revision": row["revision"],
			"work_status": row["work_status"], "result": row["result"],
		}
		if row["result"] != "skipped" {
			receipt["version"] = row["version"]
		}
		receipts = append(receipts, receipt)
	}
	return map[string]any{"system_id": systemID, "requirements": receipts}, nil
}

func writeReverseReview(cmd *cobra.Command, data map[string]any) error {
	machine, _ := cmd.Flags().GetBool("json")
	if machine {
		return writeCommandJSON(cmd, data)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Current verification for System %v:\n", data["system_id"]); err != nil {
		return err
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return err
}
