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

func TestTestCaseWritesAllowRetainedStepsAndRejectProtectedFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "update.json")
	if err := os.WriteFile(file, []byte(`{"title":"Sign in","steps":[{"number":1,"description":"Enter credentials"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	payload, err := readObject(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateTestCaseWrite(payload, true); err != nil {
		t.Fatalf("native TestCase steps should be retained writable content: %v", err)
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

func TestTestCaseCreatePostsBoundSystemAndNativeSteps(t *testing.T) {
	input := `{"title":"Sign in","steps":[{"number":1,"description":"Enter credentials","expected_result":"Signed in"}]}`
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
		if _, ok := body["steps"].([]any); !ok {
			t.Errorf("steps missing: %v", body)
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
	if err := os.WriteFile(file, []byte(`{"kind":"local_test","ran_at":"2026-10-18T10:00:00Z","runner":{"kind":"agent","agent_slug":"codex"},"results":[{"target_type":"requirement","requirement_kind":"system_requirement","target_id":"abc","result":"pass"}]}`), 0600); err != nil {
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

func TestTestCaseUpdateAcceptsListedDisplayReference(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	patched := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/compliance/test-cases/by-ref/TC-5-0":
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
	root.SetArgs([]string{"update", "TC-5-0", "--file", "-", "--json"})
	root.SetIn(strings.NewReader(`{"title":"After"}`))
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !patched || !strings.Contains(out.String(), "TC-5-0") || !strings.Contains(out.String(), "After") {
		t.Fatalf("missing update receipt: %s", out.String())
	}
}
