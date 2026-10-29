package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeObjectValidatesShapeAndRetainsNumbers(t *testing.T) {
	for _, input := range []string{`[]`, `[{}]`, `null`, `false`, `42`, `"text"`} {
		t.Run(input, func(t *testing.T) {
			if _, err := decodeObject(strings.NewReader(input)); err == nil || err.Error() != "input must be a JSON object" {
				t.Fatalf("error = %v", err)
			}
		})
	}
	for _, input := range []string{`{"title":`, `{} {}`, `{} trailing`} {
		t.Run(input, func(t *testing.T) {
			if _, err := decodeObject(strings.NewReader(input)); err == nil {
				t.Fatal("accepted malformed or multiple JSON values")
			}
		})
	}
	object, err := decodeObject(strings.NewReader(`{"id":9007199254740993,"steps":[{"number":1}]} `))
	if err != nil {
		t.Fatal(err)
	}
	if object["id"] != json.Number("9007199254740993") || len(object["steps"].([]any)) != 1 {
		t.Fatalf("object = %#v", object)
	}
}

func TestTraceListReadsNativeCriterionTestResultInBoundSystem(t *testing.T) {
	const resultID = "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("entity_type") != "criterion_test_result" || r.URL.Query().Get("entity_id") != resultID || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("unscoped result trace request: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"source":{"type":"criterion_test_result","id":"11111111-1111-4111-8111-111111111111"},"relation":"relates","target":{"type":"test_case","id":"22222222-2222-4222-8222-222222222222"}}],"next_cursor":null}}`))
	}))
	defer server.Close()
	root := newTraceListCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"--entity-type", "criterion_test_result", "--entity-id", resultID, "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), resultID) {
		t.Fatalf("missing result trace: %s", out.String())
	}
}

func TestCLIHasAtomicReleaseScopedModernizationCapture(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"modernization", "record"})
	if err != nil || command == rootCmd {
		t.Fatalf("modernization record command missing: command=%v error=%v", command, err)
	}
	if strings.Contains(command.CommandPath(), "run start") || strings.Contains(command.CommandPath(), "finalize") {
		t.Fatalf("capture must be a single atomic operation, got %s", command.CommandPath())
	}
}

func TestModernizationHistoryReadsExplicitLegacySystemAndCommit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/compliance/modernization/history" ||
			r.URL.Query().Get("system_id") != "41" || r.URL.Query().Get("commit") != "failed123" {
			t.Errorf("history request = %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"commit":"failed123","results":{"recorded":1,"passed":0,"not_passed":[{"id":"tc-1","title":"Legacy check"}]}}}`))
	}))
	defer server.Close()
	command := newModernizationCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"history", "--system-id", "41", "--commit", "failed123", "--json"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"recorded": 1`) || !strings.Contains(output.String(), `"Legacy check"`) {
		t.Fatalf("history response = %s", output.String())
	}
}

func TestModernizationRecordPostsSingleAtomicReleaseCapture(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/compliance/releases/17/modernization/test-runs" || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("request = %s %s", r.Method, r.URL.String())
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(`{"data":{"run":{"id":"run-1","sha":"abc123","system_id":4},"results":[{"id":"result-1","test_case_id":"22222222-2222-4222-8222-222222222222","result":"fail","criterion_ids":["11111111-1111-4111-8111-111111111111"]}]}}`))
	}))
	defer server.Close()
	dir := t.TempDir()
	file := filepath.Join(dir, "capture.json")
	input := `{"sha":"abc123","tests":[{"test_case_id":"22222222-2222-4222-8222-222222222222","result":"fail","criterion_ids":["11111111-1111-4111-8111-111111111111"]}]}`
	if err := os.WriteFile(file, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	command := newModernizationCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	command.SetArgs([]string{"record", "--release-id", "17", "--file", file, "--json"})
	var output bytes.Buffer
	command.SetOut(&output)
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if received["system_id"] != float64(4) || received["sha"] != "abc123" || len(received) != 3 {
		t.Fatalf("capture payload = %#v", received)
	}
	if !strings.Contains(output.String(), `"run"`) || !strings.Contains(output.String(), `"results"`) {
		t.Fatalf("capture receipt = %s", output.String())
	}
}

func TestRequirementGetUsesNativeKindAndSystemScopedDetail(t *testing.T) {
	const requirementID = "11111111-1111-4111-8111-111111111111"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/compliance/system-requirements/"+requirementID || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("request = %s %s, want scoped native detail", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"entity":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"title":"Keep native identity"},"display_id":"SR-1-0","criteria":[],"traces_from":[],"traces_to":[]}}`))
	}))
	defer server.Close()
	root := newRequirementGetCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{requirementID, "--kind", "system_requirement", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id"`) || !strings.Contains(out.String(), "SR-1-0") || strings.Contains(out.String(), `"criteria"`) {
		t.Fatalf("default detail is not a concise identity summary: %s", out.String())
	}
}

func TestRequirementGetDisplayRefInfersKindAndKeepsSelectedIdentityFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/compliance/requirements/by-ref/UR-3-2" || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("request = %s %s, want scoped display-ref detail", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"data":{"entity":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"title":"Sign in","description":"Accounts can sign in","work_status":"PROPOSED"},"display_id":"UR-3-2","criteria":[{"id":"criterion"}],"traces_from":[],"traces_to":[]}}`))
	}))
	defer server.Close()
	root := newRequirementGetCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"UR-3-2", "--fields", "description", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("selected output is not JSON: %v %s", err, out.String())
	}
	assertJSONKeys(t, result, []string{"description", "display_ref", "id", "requirement_kind"})
	var kind, ref, id string
	_ = json.Unmarshal(result["requirement_kind"], &kind)
	_ = json.Unmarshal(result["display_ref"], &ref)
	_ = json.Unmarshal(result["id"], &id)
	if kind != "user_requirement" || ref != "UR-3-2" || id != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("selected identity = %s/%s/%s", id, kind, ref)
	}
}

func TestRequirementGetFullDisplayRefIncludesCriteriaAndTraces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/compliance/requirements/by-ref/SR-9-4" {
			t.Errorf("request path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":{"entity":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"title":"Keys rotate","work_status":"PROPOSED","boundary":"api"},"display_id":"SR-9-4","criteria":[{"id":"criterion"}],"traces_from":[{"id":"trace-from"}],"traces_to":[{"id":"trace-to"}],"audit_events":{"items":[]}}}`))
	}))
	defer server.Close()
	root := newRequirementGetCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"SR-9-4", "--full", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"criteria"`, `"traces_from"`, `"traces_to"`, `"requirement_kind":"system_requirement"`} {
		if !strings.Contains(strings.ReplaceAll(out.String(), " ", ""), want) {
			t.Errorf("full detail omitted %s: %s", want, out.String())
		}
	}
}

func TestRequirementGetRejectsConflictingKindBeforeRequest(t *testing.T) {
	root := newRequirementGetCommand(func() (*cliEnv, error) {
		t.Fatal("conflicting display-ref kind must fail before loading the environment")
		return nil, nil
	})
	root.SetArgs([]string{"UR-3-2", "--kind", "system"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "conflicts") {
		t.Fatalf("conflicting kind error = %v", err)
	}
}

func TestRequirementGetUUIDRequiresKindBeforeRequest(t *testing.T) {
	root := newRequirementGetCommand(func() (*cliEnv, error) {
		t.Fatal("missing UUID kind must fail before loading the environment")
		return nil, nil
	})
	root.SetArgs([]string{"11111111-1111-4111-8111-111111111111"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "require --kind") {
		t.Fatalf("missing UUID kind error = %v", err)
	}
}

func TestRequirementGetRejectsCandidatePacketField(t *testing.T) {
	root := newRequirementGetCommand(func() (*cliEnv, error) {
		t.Fatal("retired candidate_packet field must fail before loading the environment")
		return nil, nil
	})
	root.SetArgs([]string{"UR-3-2", "--fields", "candidate_packet"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("candidate_packet field error = %v", err)
	}
}

func TestRequirementGetRejectsPriorityField(t *testing.T) {
	root := newRequirementGetCommand(func() (*cliEnv, error) {
		t.Fatal("retired priority field must fail before loading the environment")
		return nil, nil
	})
	root.SetArgs([]string{"UR-3-2", "--fields", "priority"})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("priority field error = %v", err)
	}
}

func TestTraceListScopesNativeRequirementBeforeReadingTraces(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path == "/api/compliance/user-requirements/abc" {
			if r.URL.Query().Get("system_id") != "4" {
				t.Errorf("detail was not scoped: %s", r.URL)
			}
			_, _ = w.Write([]byte(`{"data":{"entity":{"id":"abc","system_id":4}}}`))
			return
		}
		if r.URL.Path != "/api/compliance/traces" || r.URL.Query().Get("entity_type") != "user_requirement" || r.URL.Query().Get("entity_id") != "abc" || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("trace request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"source":{"type":"user_requirement","id":"abc"},"relation":"implements","target":{"type":"source_file","id":"file"}}],"next_cursor":"next"}}`))
	}))
	defer server.Close()
	root := newTraceListCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"--entity-type", "user_requirement", "--entity-id", "abc"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("requests=%v, want scope read then trace read", calls)
	}
}

func TestTestCaseWritesAllowTitleAndRejectProtectedFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "update.json")
	if err := os.WriteFile(file, []byte(`{"title":"Sign in"}`), 0600); err != nil {
		t.Fatal(err)
	}
	payload, err := readObject(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTestCaseWrite(payload, true); err != nil {
		t.Fatalf("native TestCase title should be writable: %v", err)
	}
	if err := validateTestCaseWrite(map[string]any{"system_id": 9}, true); err == nil || !strings.Contains(err.Error(), `unsupported test-case field "system_id"`) {
		t.Fatalf("expected bound System protection, got %v", err)
	}
	root := newTestCasesCommand(func() (*cliEnv, error) {
		t.Fatal("test-case write surface unexpectedly includes creation")
		return nil, nil
	})
	foundCreate := false
	for _, sub := range root.Commands() {
		if sub.Name() == "create" {
			foundCreate = true
		}
	}
	if !foundCreate {
		t.Fatal("missing dedicated TestCase creation command")
	}
	if _, _, err := root.Find([]string{"create"}); err != nil {
		t.Fatalf("create command not findable: %v", err)
	}
	if strings.Contains(root.Long, "manual") {
		t.Fatal("manual executions are not part of the TestCase writing surface")
	}
}

func TestTestCaseCreatePostsBoundSystemAndTitle(t *testing.T) {
	input := `{"title":"Sign in"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/compliance/test-cases" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		var body map[string]any
		dec := json.NewDecoder(r.Body)
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			t.Error(err)
		}
		if body["system_id"] != json.Number("4") {
			t.Errorf("bound system missing: %v", body)
		}
		if body["title"] != "Sign in" || len(body) != 2 {
			t.Errorf("expected title and bound system only: %v", body)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"abc","system_id":4,"title":"Sign in"}}`))
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetIn(strings.NewReader(input))
	root.SetArgs([]string{"create", "--file", "-"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestEvidenceRecordInjectsBoundSystemAndPreservesNativePayload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "evidence.json")
	if err := os.WriteFile(file, []byte(`{"sha":"code-revision","status":"running","results":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sync/evidence" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Error(err)
		}
		if payload["system_id"] != json.Number("4") {
			t.Errorf("system_id=%v", payload["system_id"])
		}
		if _, ok := payload["results"].([]any); !ok {
			t.Errorf("results lost: %v", payload)
		}
		_, _ = w.Write([]byte(`{"data":{"run":{"id":"run"},"results":[]}}`))
	}))
	defer server.Close()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"record", "--file", file})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestTestCaseUpdateAcceptsListedNativeIdentity(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	patched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/compliance/test-cases/"+id+"":
			if r.URL.Query().Get("system_id") != "4" {
				t.Error("display-reference resolution must be scoped to the bound System")
			}
			_, _ = w.Write([]byte(`{"data":{"entity":{"id":"` + id + `","system_id":4,"title":"Before"},"display_id":"TC-5-0"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/api/compliance/test-cases/"+id:
			patched = true
			_, _ = w.Write([]byte(`{"data":{"id":"` + id + `","system_id":4,"title":"After","display_ref":"TC-5-0"}}`))
		default:
			http.Error(w, `{"error":"not_found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"update", id, "--file", "-", "--json"})
	root.SetIn(strings.NewReader(`{"title":"After"}`))
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !patched || !strings.Contains(out.String(), id) || !strings.Contains(out.String(), "After") {
		t.Fatalf("missing update receipt: %s", out.String())
	}
}

func TestEvidenceRecordReturnsNativeReceipt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"results":[{"id":"22222222-2222-4222-8222-222222222222"}],"test_cases":[{"id":"33333333-3333-4333-8333-333333333333"}],"traces":[{"id":"44444444-4444-4444-8444-444444444444"}]}}`))
	}))
	defer server.Close()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetIn(strings.NewReader(`{"criterion_id":"criterion","tests":[{"test_case_id":"test","result":"pass"}]}`))
	var output bytes.Buffer
	root.SetOut(&output)
	root.SetArgs([]string{"record", "--file", "-", "--json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444"} {
		if !strings.Contains(output.String(), id) {
			t.Errorf("native identity %s missing from receipt: %s", id, output.String())
		}
	}
}
