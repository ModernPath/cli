package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

func newModernizationCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "modernization", Short: "Capture and read historical modernization test results"}
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Atomically capture test outcomes for a Modernization Release",
		Long: `Capture a completed test execution as immutable modernization history.

The JSON file contains one commit and its actual test outcomes:
  {"sha":"<commit>","tests":[{"test_case_id":"<UUID>","result":"pass","criterion_ids":["<UUID>"]}]}

The Release authorizes the legacy System and scope. The server stores the run,
results and their criterion/TestCase Traces atomically; this does not write
current criterion proof. There is no run registration or finalization step.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			releaseID, _ := cmd.Flags().GetInt("release-id")
			if releaseID < 1 {
				return fmt.Errorf("--release-id must be a positive integer")
			}
			file, _ := cmd.Flags().GetString("file")
			payload, err := readObjectInput(cmd, file)
			if err != nil {
				return err
			}
			if err := validateModernizationCapture(payload); err != nil {
				return err
			}
			env, err := load()
			if err != nil {
				return err
			}
			payload["system_id"] = env.SystemID
			route := fmt.Sprintf("/api/compliance/releases/%d/modernization/test-runs?system_id=%s", releaseID, url.QueryEscape(fmt.Sprint(env.SystemID)))
			status, body, err := env.write("POST", route, payload)
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("modernization record refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("modernization response is missing its data object")
			}
			if _, ok := data["run"].(map[string]any); !ok {
				return fmt.Errorf("modernization response is missing its run")
			}
			if _, ok := data["results"].([]any); !ok {
				return fmt.Errorf("modernization response is missing its results")
			}
			return writeRecord(cmd, data)
		},
	}
	cmd.Flags().Int("release-id", 0, "Modernization Release ID")
	cmd.MarkFlagRequired("release-id")
	cmd.Flags().String("file", "", "modernization capture JSON file (required)")
	cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("json", false, "emit JSON")
	root.AddCommand(cmd)

	history := &cobra.Command{
		Use: "history", Short: "Read bounded history for a legacy System, with or without a Release",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			systemID, _ := cmd.Flags().GetInt64("system-id")
			if systemID < 1 {
				return fmt.Errorf("--system-id must be a positive legacy System ID")
			}
			commit, _ := cmd.Flags().GetString("commit")
			if cmd.Flags().Changed("commit") && strings.TrimSpace(commit) == "" {
				return fmt.Errorf("--commit must be nonempty when supplied")
			}
			env, err := load()
			if err != nil {
				return err
			}
			query := url.Values{"system_id": {fmt.Sprint(systemID)}}
			if commit != "" {
				query.Set("commit", commit)
			}
			status, body, err := env.get("/api/compliance/modernization/history?" + query.Encode())
			if err != nil {
				return err
			}
			if status < 200 || status >= 300 {
				return serverRefusal("modernization history refused", status, body)
			}
			data, ok := body["data"].(map[string]any)
			if !ok {
				return fmt.Errorf("modernization history response is missing its data object")
			}
			return writeRecord(cmd, data)
		},
	}
	history.Flags().Int64("system-id", 0, "Legacy System ID; required, including when it differs from the bound System")
	_ = history.MarkFlagRequired("system-id")
	history.Flags().String("commit", "", "Commit to inspect; omit for passing choices and recorded-results indicator")
	history.Flags().Bool("json", false, "Emit JSON")
	root.AddCommand(history)
	return root
}

func validateModernizationCapture(payload map[string]any) error {
	for key := range payload {
		if key != "sha" && key != "tests" {
			return fmt.Errorf("unsupported modernization capture field %q", key)
		}
	}
	if sha, ok := payload["sha"].(string); !ok || sha == "" {
		return fmt.Errorf("input must include a nonempty sha")
	}
	tests, ok := payload["tests"].([]any)
	if !ok || len(tests) == 0 {
		return fmt.Errorf("input must include a nonempty tests array")
	}
	if len(tests) > 500 {
		return fmt.Errorf("tests may contain at most 500 entries")
	}
	testCases := map[string]bool{}
	criteriaSeen := map[string]bool{}
	for i, raw := range tests {
		test, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("tests[%d] must be an object", i)
		}
		for key := range test {
			if key != "test_case_id" && key != "result" && key != "criterion_ids" {
				return fmt.Errorf("tests[%d] has unsupported field %q", i, key)
			}
		}
		id, ok := test["test_case_id"].(string)
		if !ok || !requirementUUIDPattern.MatchString(id) {
			return fmt.Errorf("tests[%d] must include test_case_id", i)
		}
		if testCases[id] {
			return fmt.Errorf("tests[%d] duplicates test_case_id", i)
		}
		testCases[id] = true
		if result, ok := test["result"].(string); !ok || !map[string]bool{"pass": true, "fail": true, "error": true, "skip": true, "inconclusive": true}[result] {
			return fmt.Errorf("tests[%d].result must be pass, fail, error, skip or inconclusive", i)
		}
		criteria, ok := test["criterion_ids"].([]any)
		if !ok || len(criteria) == 0 {
			return fmt.Errorf("tests[%d] must include nonempty criterion_ids", i)
		}
		pairCriteria := map[string]bool{}
		for j, criterion := range criteria {
			criterionID, ok := criterion.(string)
			if !ok || !requirementUUIDPattern.MatchString(criterionID) {
				return fmt.Errorf("tests[%d].criterion_ids[%d] must be a nonempty UUID", i, j)
			}
			if pairCriteria[criterionID] {
				return fmt.Errorf("tests[%d].criterion_ids duplicates %s", i, criterionID)
			}
			pairCriteria[criterionID] = true
			criteriaSeen[criterionID] = true
		}
	}
	if len(criteriaSeen) > 50 {
		return fmt.Errorf("capture may cover at most 50 distinct criteria")
	}
	return nil
}
