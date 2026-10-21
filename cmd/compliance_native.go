package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	cmds := newNativeComplianceCommands(cliEnvLoad)
	for _, cmd := range cmds {
		rootCmd.AddCommand(cmd)
	}
}

func newNativeComplianceCommands(load func() (*cliEnv, error)) []*cobra.Command {
	requirementsCmd.AddCommand(newRequirementGetCommand(load))
	return []*cobra.Command{newTraceCommands(load), newTestCasesCommand(load), newEvidenceCommand(load)}
}

func newRequirementGetCommand(load func() (*cliEnv, error)) *cobra.Command {
	cmd := &cobra.Command{Use: "get <uuid|display-ref>", Short: "Read a concise native requirement record", Args: cobra.ExactArgs(1)}
	kind := ""
	cmd.Flags().StringVar(&kind, "kind", "", "requirement kind for UUIDs: user or system (native *_requirement names also accepted)")
	cmd.Flags().Bool("json", false, "Emit selected fields as JSON")
	addRecordReadFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		isUUID := requirementUUIDPattern.MatchString(args[0])
		effectiveKind := strings.TrimSuffix(kind, "_requirement")
		entityType := ""
		if isUUID {
			if effectiveKind != "user" && effectiveKind != "system" {
				return fmt.Errorf("UUID requirement reads require --kind user or system")
			}
			entityType = map[string]string{"user": "user-requirements", "system": "system-requirements"}[effectiveKind]
		} else {
			inferred, valid := requirementKindFromDisplayRef(args[0])
			if !valid {
				return fmt.Errorf("requirement must be a UUID or exact UR-<number>-<revision>/SR-<number>-<revision> reference")
			}
			if effectiveKind != "" && effectiveKind != inferred {
				return fmt.Errorf("--kind conflicts with requirement reference %q", args[0])
			}
			effectiveKind = inferred
		}
		allowed := requirementDetailFields(effectiveKind)
		if err := validateRecordSelection(cmd, allowed); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		var route string
		if isUUID {
			route = "/api/compliance/" + entityType + "/" + url.PathEscape(args[0]) + "?system_id=" + fmt.Sprint(env.SystemID)
		} else {
			params := url.Values{"system_id": {fmt.Sprint(env.SystemID)}}
			route = "/api/compliance/requirements/by-ref/" + url.PathEscape(args[0]) + "?" + params.Encode()
		}
		body, err := nativeGetData(env, route, "requirement detail", true)
		if err != nil {
			return err
		}
		data, _ := body["data"].(map[string]any)
		entity, _ := data["entity"].(map[string]any)
		record := make(map[string]any, len(entity)+8)
		for key, value := range entity {
			record[key] = value
		}
		record["requirement_kind"] = map[string]string{"user": "user_requirement", "system": "system_requirement"}[effectiveKind]
		record["display_ref"] = data["display_id"]
		for _, key := range []string{"created_by", "criteria", "traces_from", "traces_to", "audit_events"} {
			if value, exists := data[key]; exists {
				record[key] = value
			}
		}
		selected, err := selectRecordFields(cmd, record, []string{"title", "work_status"}, allowed,
			[]string{"id", "requirement_kind", "display_ref"})
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
	}
	return cmd
}

func requirementKindFromDisplayRef(ref string) (string, bool) {
	if requirementDisplayRefPattern.MatchString(ref) && strings.HasPrefix(strings.ToUpper(ref), "UR-") {
		return "user", true
	}
	if requirementDisplayRefPattern.MatchString(ref) && strings.HasPrefix(strings.ToUpper(ref), "SR-") {
		return "system", true
	}
	return "", false
}

var requirementDisplayRefPattern = regexp.MustCompile(`(?i)^(?:UR|SR)-[1-9][0-9]*-[0-9]+$`)

func requirementDetailFields(kind string) []string {
	fields := []string{
		"id", "requirement_kind", "display_ref", "title", "description", "revision", "display_number",
		"embedding_model", "context", "context_name", "work_status", "release_note",
		"detail_md", "delivered_revision", "process_metadata", "completed_at",
		"release_exempt", "internal_only", "deleted_at", "tenant_id", "system_id", "created_by_id",
		"change_epic_id", "release_id", "inserted_at", "updated_at",
		"created_by", "criteria", "traces_from", "traces_to", "audit_events",
	}
	if kind == "system" {
		return append(fields, "boundary", "rationale", "verification_method", "carried_from_release_id")
	}
	return append(fields, "actor", "intended_use")
}

func newTraceListCommand(load func() (*cliEnv, error)) *cobra.Command {
	cmd := &cobra.Command{Use: "list", Short: "List traces touching a native entity", Args: cobra.NoArgs,
		Long: `List the active traces whose source or target is one native entity of the
bound System. Each row is source, relation and target; an endpoint is its type
and id (a source_file endpoint shows repository and path). --relation filters
by link kind: implements, verifies, derives or relates. A requirement's
criteria are entities of their own: list a criterion's traces with
--entity-type criterion. Traces are written by reverse-engineer record and by
an Epic plan at implementation and completion approval; there is no direct
trace write.`}
	entityType, entityID := "", ""
	cmd.Flags().StringVar(&entityType, "entity-type", "", "native entity kind (user_requirement, system_requirement, test_case, system_document, source_file, file_analysis, subsystem, epic, criterion)")
	cmd.Flags().StringVar(&entityID, "entity-id", "", "native entity id")
	cmd.Flags().Bool("json", false, "emit JSON")
	cmd.Flags().Int("limit", 20, "page size (1–200)")
	cmd.Flags().String("cursor", "", "continue from a previous page")
	cmd.Flags().String("relation", "", "filter by trace relation")
	cmd.MarkFlagRequired("entity-type")
	cmd.MarkFlagRequired("entity-id")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		allowed := map[string]bool{"user_requirement": true, "system_requirement": true, "test_case": true, "system_document": true, "source_file": true, "file_analysis": true, "subsystem": true, "epic": true, "criterion": true}
		if !allowed[entityType] {
			return fmt.Errorf("unsupported --entity-type %q", entityType)
		}
		env, err := load()
		if err != nil {
			return err
		}
		// A detail read rejects a UUID-backed compliance entity from another System;
		// the trace endpoint also filters every native type to the bound System.
		if kind, ok := map[string]string{"user_requirement": "user", "system_requirement": "system", "test_case": "test-case"}[entityType]; ok {
			detailType := map[string]string{"user": "user-requirements", "system": "system-requirements", "test-case": "test-cases"}[kind]
			if _, err := nativeGetData(env, "/api/compliance/"+detailType+"/"+url.PathEscape(entityID)+"?system_id="+fmt.Sprint(env.SystemID), "entity detail", true); err != nil {
				return err
			}
		}
		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		relation, _ := cmd.Flags().GetString("relation")
		if limit < 1 || limit > 200 {
			return fmt.Errorf("--limit must be between 1 and 200")
		}
		p := url.Values{"entity_type": {entityType}, "entity_id": {entityID}, "system_id": {fmt.Sprint(env.SystemID)}, "page": {"true"}, "limit": {fmt.Sprint(limit)}}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		if relation != "" {
			p.Set("relation", relation)
		}
		body, err := nativeGetData(env, "/api/compliance/traces?"+p.Encode(), "trace list", false)
		if err != nil {
			return err
		}
		data, _ := body["data"].(map[string]any)
		return writeNativePage(cmd, data, "items", func(item map[string]any) (map[string]any, error) {
			return map[string]any{"source": item["source"], "relation": item["relation"], "target": item["target"]}, nil
		})
	}
	return cmd
}

func newTraceCommands(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "traces", Short: "Read native trace relationships"}
	root.AddCommand(newTraceListCommand(load))
	return root
}

func newTestCasesCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "test-cases", Short: "Read and update retained test-case procedures"}
	create := &cobra.Command{Use: "create", Short: "Create a test-case procedure in the bound System", Args: cobra.NoArgs}
	createFile := ""
	create.Flags().StringVar(&createFile, "file", "", "JSON object with title and optional description, preconditions, and steps (required)")
	_ = create.MarkFlagRequired("file")
	create.Flags().Bool("json", false, "emit JSON")
	create.Long = `Create retained test-case procedure metadata. Example: {"title":"Sign in","description":"Check account access","preconditions":"An account exists","steps":[{"number":1,"description":"Enter valid credentials","expected_result":"The account is signed in"}]}. The CLI supplies the bound system_id.`
	create.RunE = func(cmd *cobra.Command, _ []string) error {
		payload, err := readObjectInput(cmd, createFile)
		if err != nil {
			return err
		}
		if err := validateTestCaseWrite(payload, true); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		payload["system_id"] = env.SystemID
		status, body, err := env.call("POST", "/api/compliance/test-cases", payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("test-case creation refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("test-case creation response is missing its data object")
		}
		if sid, ok := numberAsInt(data["system_id"]); !ok || sid != int64(env.SystemID) {
			return fmt.Errorf("created test case belongs to a different System or has no System identity")
		}
		return writeRecord(cmd, map[string]any{"id": data["id"], "display_ref": data["display_ref"], "title": data["title"]})
	}
	list := &cobra.Command{Use: "list", Short: "List test cases in the bound System", Args: cobra.NoArgs}
	list.Flags().Int("limit", 20, "page size (1–200)")
	list.Flags().String("cursor", "", "continue from a previous page")
	list.Flags().String("query", "", "literal case-insensitive title or description match")
	list.Flags().String("status", "", "filter by status: draft or approved")
	list.Flags().String("order", "id", "order by id or title")
	list.Flags().Bool("json", false, "emit JSON")
	list.RunE = func(cmd *cobra.Command, _ []string) error {
		limit, _ := cmd.Flags().GetInt("limit")
		cursor, _ := cmd.Flags().GetString("cursor")
		query, _ := cmd.Flags().GetString("query")
		status, _ := cmd.Flags().GetString("status")
		order, _ := cmd.Flags().GetString("order")
		if limit < 1 || limit > 200 {
			return fmt.Errorf("--limit must be between 1 and 200")
		}
		if status != "" && status != "draft" && status != "approved" {
			return fmt.Errorf("--status must be draft or approved")
		}
		if order != "id" && order != "title" {
			return fmt.Errorf("--order must be id or title")
		}
		env, err := load()
		if err != nil {
			return err
		}
		p := url.Values{"system_id": {fmt.Sprint(env.SystemID)}, "limit": {fmt.Sprint(limit)}, "page": {"true"}, "order": {order}}
		if query != "" {
			p.Set("query", query)
		}
		if status != "" {
			p.Set("status", status)
		}
		if cursor != "" {
			p.Set("cursor", cursor)
		}
		body, err := nativeGetData(env, "/api/compliance/test-cases?"+p.Encode(), "test-case list", false)
		if err != nil {
			return err
		}
		data, _ := body["data"].(map[string]any)
		return writeNativePage(cmd, data, "items", func(item map[string]any) (map[string]any, error) {
			return map[string]any{"id": item["id"], "display_ref": item["display_ref"], "title": item["title"]}, nil
		})
	}
	get := &cobra.Command{Use: "get <uuid|display-ref>", Short: "Read one test case", Args: cobra.ExactArgs(1)}
	addRecordReadFlags(get)
	get.Flags().Bool("json", false, "emit JSON")
	get.RunE = func(cmd *cobra.Command, args []string) error {
		allowed := []string{"id", "display_ref", "title", "description", "preconditions", "steps", "system_id", "inserted_at", "updated_at"}
		if err := validateRecordSelection(cmd, allowed); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		route := testCaseDetailRoute(env.SystemID, args[0])
		body, err := nativeGetData(env, route, "test-case detail", true)
		if err != nil {
			return err
		}
		data, _ := body["data"].(map[string]any)
		entity, _ := data["entity"].(map[string]any)
		record := make(map[string]any, len(entity)+1)
		for key, value := range entity {
			record[key] = value
		}
		record["display_ref"] = data["display_id"]
		selected, err := selectRecordFields(cmd, record, []string{"id", "display_ref", "title"}, allowed, []string{"id", "display_ref"})
		if err != nil {
			return err
		}
		return writeRecord(cmd, selected)
	}
	update := &cobra.Command{Use: "update <uuid|display-ref>", Short: "Update retained test-case descriptive metadata from JSON", Args: cobra.ExactArgs(1)}
	file := ""
	update.Flags().StringVar(&file, "file", "", "JSON object with title, description, and/or preconditions (required)")
	update.MarkFlagRequired("file")
	update.Flags().Bool("json", false, "emit JSON")
	update.Long = `Update retained procedure metadata. Input example: {"title":"Login","description":"Verify sign-in","preconditions":"Account exists","steps":[{"number":1,"description":"Enter valid credentials","expected_result":"The account is signed in"}]}.`
	update.RunE = func(cmd *cobra.Command, args []string) error {
		payload, err := readObjectInput(cmd, file)
		if err != nil {
			return err
		}
		if err := validateTestCaseWrite(payload, false); err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		resolved, err := nativeGetData(env, testCaseDetailRoute(env.SystemID, args[0]), "test-case detail", true)
		if err != nil {
			return err
		}
		detail := resolved["data"].(map[string]any)
		entity := detail["entity"].(map[string]any)
		id, ok := entity["id"].(string)
		if !ok || strings.TrimSpace(id) == "" {
			return fmt.Errorf("test-case detail response is missing its native ID")
		}
		id = url.PathEscape(id)
		status, body, err := env.call("PATCH", "/api/compliance/test-cases/"+id, payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("test-case update refused", status, body)
		}
		data, ok := body["data"].(map[string]any)
		if !ok {
			return fmt.Errorf("test-case update response is missing its data object")
		}
		if sid, ok := numberAsInt(data["system_id"]); !ok || sid != int64(env.SystemID) {
			return fmt.Errorf("updated test case belongs to a different System or has no System identity")
		}
		return writeRecord(cmd, map[string]any{"id": data["id"], "display_ref": data["display_ref"], "title": data["title"]})
	}
	root.AddCommand(create, list, get, update)
	return root
}

func testCaseDetailRoute(systemID int, identity string) string {
	path := "/api/compliance/test-cases/"
	if strings.HasPrefix(strings.ToUpper(identity), "TC-") {
		path += "by-ref/"
	}
	return path + url.PathEscape(identity) + "?system_id=" + fmt.Sprint(systemID)
}

func validateTestCaseWrite(payload map[string]any, creating bool) error {
	if len(payload) == 0 {
		return fmt.Errorf("input must include at least one editable field")
	}
	for k := range payload {
		if k != "title" && k != "description" && k != "preconditions" && k != "steps" {
			return fmt.Errorf("unsupported test-case field %q", k)
		}
	}
	if creating {
		if _, ok := payload["title"]; !ok {
			return fmt.Errorf("input must include title")
		}
	}
	return nil
}

func newEvidenceCommand(load func() (*cliEnv, error)) *cobra.Command {
	root := &cobra.Command{Use: "evidence", Short: "Record and read evidence observations"}
	root.AddCommand(&cobra.Command{Use: "record", Short: "Record an evidence run and per-target observations", Args: cobra.NoArgs, Long: evidenceRecordHelp, RunE: func(cmd *cobra.Command, _ []string) error {
		f, _ := cmd.Flags().GetString("file")
		payload, err := readObjectInput(cmd, f)
		if err != nil {
			return err
		}
		env, err := load()
		if err != nil {
			return err
		}
		payload["system_id"] = env.SystemID
		status, body, err := env.call("POST", "/api/v1/sync/evidence", payload)
		if err != nil {
			return err
		}
		if status < 200 || status >= 300 {
			return serverRefusal("evidence record refused", status, body)
		}
		if _, ok := body["data"].(map[string]any); !ok {
			return fmt.Errorf("evidence response is missing its data object")
		}
		data, _ := body["data"].(map[string]any)
		run, _ := data["run"].(map[string]any)
		return writeRecord(cmd, map[string]any{"run_id": run["id"], "result": data["result"], "results": lenAny(data["results"])})
	}})
	root.Commands()[0].Flags().String("file", "", "evidence JSON file (required)")
	root.Commands()[0].MarkFlagRequired("file")
	root.Commands()[0].Flags().Bool("json", false, "emit JSON")
	for _, name := range []string{"latest", "runs"} {
		name := name
		c := &cobra.Command{Use: name, Short: "Read evidence " + name, Args: cobra.NoArgs}
		c.Flags().Bool("json", false, "emit JSON")
		if name == "runs" {
			c.Flags().String("kind", "", "filter run kind: ci, local_test, browser_verification, manual, migration")
			c.Flags().Bool("full", false, "include matching per-target result rows")
		}
		c.Flags().Int("limit", 20, "page size (1–200)")
		c.Flags().String("cursor", "", "continue from a previous page")
		c.Flags().String("target-type", "", "filter target type")
		c.Flags().String("target-id", "", "filter target ID")
		c.Flags().String("outcome", "", "filter outcome: pass, fail, error, inconclusive, skip")
		c.Flags().String("since", "", "include observations at or after RFC3339 time")
		c.Flags().String("until", "", "include observations before RFC3339 time")
		c.RunE = func(cmd *cobra.Command, _ []string) error {
			env, err := load()
			if err != nil {
				return err
			}
			p := url.Values{"system_id": {fmt.Sprint(env.SystemID)}}
			route := "/api/v1/sync/evidence/" + name
			limit, _ := cmd.Flags().GetInt("limit")
			if limit < 1 || limit > 200 {
				return fmt.Errorf("--limit must be between 1 and 200")
			}
			p.Set("page", "true")
			p.Set("limit", fmt.Sprint(limit))
			for flag, param := range map[string]string{"cursor": "cursor", "target-type": "target_type", "target-id": "target_id", "outcome": "outcome", "since": "since", "until": "until"} {
				value, _ := cmd.Flags().GetString(flag)
				if value != "" {
					p.Set(param, value)
				}
			}
			fullRuns := false
			if name == "runs" {
				k, _ := cmd.Flags().GetString("kind")
				if k != "" {
					p.Set("kind", k)
				}
				fullRuns, _ = cmd.Flags().GetBool("full")
				if fullRuns {
					p.Set("include_results", "true")
				}
			}
			body, err := nativeGetData(env, route+"?"+p.Encode(), "evidence "+name, false)
			if err != nil {
				return err
			}
			data, _ := body["data"].(map[string]any)
			if name == "latest" {
				return writeNativePage(cmd, data, "targets", func(item map[string]any) (map[string]any, error) {
					return selectKeys(item, []string{"target_type", "requirement_kind", "target_id", "result", "state", "sha", "ran_at"}), nil
				})
			}
			return writeNativePage(cmd, data, "runs", func(item map[string]any) (map[string]any, error) {
				selected := selectKeys(item, []string{"id", "kind", "sha", "branch", "ran_at", "status", "totals"})
				if fullRuns {
					mergeSelectedKeys(selected, item, []string{"source_revision", "log_ref", "environment", "raw_evidence", "runner_kind", "runner_agent_slug", "runner_user_id"})
				}
				if rawResults, exists := item["results"]; exists {
					results, _ := rawResults.([]any)
					compact := make([]any, 0, len(results))
					for _, raw := range results {
						row, ok := raw.(map[string]any)
						if !ok {
							return nil, fmt.Errorf("evidence run contains an invalid result")
						}
						result := selectKeys(row, []string{"target_type", "requirement_kind", "target_id", "target_clause", "test_case_ref", "role", "result", "validity", "revision", "detail"})
						if fullRuns {
							mergeSelectedKeys(result, row, []string{"id", "run_id", "environment", "raw_evidence", "metadata"})
						}
						compact = append(compact, result)
					}
					selected["results"] = compact
				}
				return selected, nil
			})
		}
		root.AddCommand(c)
	}
	return root
}

// evidenceRecordHelp is the run file as Core.Evidence and the evidence
// schemas accept it, with the fields the acceptance review later requires,
// so a verification pass records what the review will read.
const evidenceRecordHelp = `Record one test, CI, browser or manual run and its per-target results. The
CLI adds the bound system_id. Example:

  {"kind":"local_test","sha":"<commit>","branch":"<branch>","ran_at":"2026-10-18T10:00:00Z",
   "runner":{"kind":"agent","agent_slug":"claude-code"},"status":"complete",
   "totals":{"pass":1,"fail":0},"log_ref":"<command or CI run url>","environment":{"os":"darwin"},
   "results":[{"target_type":"requirement","requirement_kind":"system_requirement",
               "target_id":"<uuid>","result":"pass","role":"GREEN","revision":"<commit>",
               "test_case_ref":"test/catalog_test.exs:12","detail":{"command":"mix test test/catalog_test.exs"}}]}

The run:

  kind        ci | local_test | browser_verification | manual | migration (required)
  ran_at      RFC 3339 time (required)
  runner      optional: {"kind": "agent", "agent_slug": "<slug>"} names the
              agent that ran it, with the signed-in person as the human
              behind it; left out, the run is attributed to the signed-in
              person
  sha, branch, source_revision, status (running | complete | error; default
  complete), totals, log_ref, environment, raw_evidence
  id          an existing run to update: supplied results replace its result
              set, and omitted fields keep their values. Ordinary runs allow
              proof to be overwritten. Only runs stored with
              raw_payload.as_built_proof set to execution or delivery refuse
              changed proof under the same id

Each result:

  target_type       requirement | criterion | test_case | backlog_record | epic
  target_id         the target's UUID, or its integer ID for an epic
  requirement_kind  user_requirement | system_requirement, required with a
                    requirement target
  result            pass | fail | skip | error | inconclusive
  role              RED for the red-first failing run, GREEN (or none) for a
                    passing run; a RED never shadows a later pass
  revision, target_clause, test_case_ref, detail (a map; put the command
  under "command"), environment, raw_evidence

Record the RED run before the passing run, each at its tested commit. Omit
id to create a new run for each execution, including reruns. Requirement-level
results drive evidence latest and the Epic's build state. For existing-behavior acceptance
(reverse-engineer review), record one result per criterion with target_type
criterion and target_id the criterion's id (requirements get <uuid> --kind …
--full lists them), result pass, a test_case_ref, a revision on the result or
sha on the run, a report (raw_evidence, log_ref or detail.command) and an
environment on the result or the run, in a complete run of a kind other than
migration, after the requirement's content, criteria and traces last changed.

The receipt is run_id, result (created | updated | replayed) and the number
of results. Read back with evidence runs --full --target-id <uuid> and
evidence latest.`

func lenAny(value any) int { rows, _ := value.([]any); return len(rows) }

func selectKeys(source map[string]any, keys []string) map[string]any {
	selected := make(map[string]any, len(keys))
	for _, key := range keys {
		if value, ok := source[key]; ok {
			selected[key] = value
		}
	}
	return selected
}

func mergeSelectedKeys(destination, source map[string]any, keys []string) {
	for key, value := range selectKeys(source, keys) {
		destination[key] = value
	}
}

func writeNativePage(cmd *cobra.Command, data map[string]any, key string, selectItem func(map[string]any) (map[string]any, error)) error {
	if data == nil {
		return fmt.Errorf("page response is missing its data object")
	}
	rawItems, ok := data[key].([]any)
	if !ok {
		return fmt.Errorf("page response is missing %s", key)
	}
	cursorRaw, exists := data["next_cursor"]
	if !exists {
		return fmt.Errorf("page response is missing next_cursor")
	}
	var cursor string
	if cursorRaw != nil {
		var valid bool
		cursor, valid = cursorRaw.(string)
		if !valid || cursor == "" {
			return fmt.Errorf("page next_cursor must be a string or null")
		}
	}
	items := make([]any, 0, len(rawItems))
	for i, raw := range rawItems {
		item, valid := raw.(map[string]any)
		if !valid {
			return fmt.Errorf("page item %d is not an object", i)
		}
		selected, err := selectItem(item)
		if err != nil {
			return err
		}
		items = append(items, selected)
	}
	if machine, _ := cmd.Flags().GetBool("json"); machine {
		return writeCommandJSON(cmd, map[string]any{"items": items, "next_cursor": nullableCursor(cursor)})
	}
	for _, raw := range items {
		item := raw.(map[string]any)
		fields := make([]string, 0, len(item))
		for k := range item {
			fields = append(fields, k)
		}
		sort.Strings(fields)
		if item["display_ref"] != nil && item["title"] != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s  %s\n", item["display_ref"], strings.Join(strings.Fields(fmt.Sprint(item["title"])), " "))
			continue
		}
		parts := make([]string, 0, len(fields))
		for _, k := range fields {
			value := fmt.Sprint(item[k])
			if k == "source" || k == "target" {
				if endpoint, ok := item[k].(map[string]any); ok {
					value = fmt.Sprint(endpoint["type"]) + ":" + fmt.Sprint(endpoint["id"])
				}
			}
			parts = append(parts, k+"="+value)
		}
		fmt.Fprintln(cmd.OutOrStdout(), strings.Join(parts, "  "))
	}
	if cursor != "" {
		_, err := fmt.Fprintf(cmd.ErrOrStderr(), "next_cursor: %s\n", cursor)
		return err
	}
	return nil
}

func nativeGet(cmd *cobra.Command, load func() (*cliEnv, error), route, what string, requireEntity bool) error {
	env, err := load()
	if err != nil {
		return err
	}
	body, err := nativeGetData(env, route, what, requireEntity)
	if err != nil {
		return err
	}
	return json.NewEncoder(cmd.OutOrStdout()).Encode(body)
}
func nativeGetData(env *cliEnv, route, what string, requireEntity bool) (map[string]any, error) {
	status, body, err := env.call("GET", route, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, serverRefusal(what+" refused", status, body)
	}
	data, ok := body["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s response is missing its data object", what)
	}
	if requireEntity {
		entity, ok := data["entity"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s response is missing its entity", what)
		}
		if sid, ok := numberAsInt(entity["system_id"]); !ok || sid != int64(env.SystemID) {
			return nil, fmt.Errorf("%s belongs to a different System or has no System identity", what)
		}
	}
	if items, ok := data["items"].([]any); ok {
		for _, raw := range items {
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("%s response contains an invalid item", what)
			}
			if rawSystem, exists := item["system_id"]; exists {
				if sid, ok := numberAsInt(rawSystem); !ok || sid != int64(env.SystemID) {
					return nil, fmt.Errorf("%s contains an item from a different System or without System identity", what)
				}
			}
		}
	}
	return body, nil
}
func numberAsInt(v any) (int64, bool) {
	switch n := v.(type) {
	case json.Number:
		parsed, err := n.Int64()
		return parsed, err == nil
	case float64:
		return int64(n), float64(int64(n)) == n
	case int:
		return int64(n), true
	case int64:
		return n, true
	}
	return 0, false
}
func readObject(path string) (map[string]any, error) {
	if path == "" {
		return nil, fmt.Errorf("--file is required")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeObject(bytes.NewReader(b))
}

func readObjectInput(cmd *cobra.Command, path string) (map[string]any, error) {
	if path == "-" {
		return decodeObject(cmd.InOrStdin())
	}
	return readObject(path)
}

func decodeObject(input io.Reader) (map[string]any, error) {
	const maxBytes = 48_000_000
	b, err := io.ReadAll(io.LimitReader(input, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxBytes {
		return nil, fmt.Errorf("JSON input exceeds 48 MB limit")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v map[string]any
	if err := d.Decode(&v); err != nil {
		return nil, fmt.Errorf("invalid JSON object: %w", err)
	}
	var trailing any
	if err := d.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("input must contain exactly one JSON object")
	}
	if v == nil {
		return nil, fmt.Errorf("input must be a JSON object")
	}
	return v, nil
}
