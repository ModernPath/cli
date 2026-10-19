package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTestCaseListUsesBoundPagedRouteAndReturnsMinimalRows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/compliance/test-cases" || r.URL.Query().Get("page") != "true" || r.URL.Query().Get("system_id") != "4" || r.URL.Query().Get("limit") != "20" || r.URL.Query().Get("query") != "sign in" || r.URL.Query().Get("status") != "draft" || r.URL.Query().Get("order") != "title" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"id":"11111111-1111-4111-8111-111111111111","display_ref":"TC-3-2","title":"Sign in","system_id":4,"steps":[{"number":1}]}],"next_cursor":"opaque"}}`))
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"list", "--query", "sign in", "--status", "draft", "--order", "title", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, out.String())
	}
	assertJSONKeys(t, response, []string{"items", "next_cursor"})
	if !strings.Contains(out.String(), "11111111") || strings.Contains(out.String(), "steps") {
		t.Fatalf("list omitted native UUID or returned full content: %s", out.String())
	}
	if !strings.Contains(out.String(), "TC-3-2") || !strings.Contains(out.String(), "opaque") {
		t.Fatalf("missing compact ref or cursor: %s", out.String())
	}
}

func TestTestCaseGetDisplayRefUsesExactRevisionRouteAndSelectedSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/compliance/test-cases/by-ref/TC-3-2" || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"entity":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"title":"Sign in","description":"Long","steps":[1]},"display_id":"TC-3-2"}}`))
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"get", "TC-3-2", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Long") || strings.Contains(out.String(), "steps") || !strings.Contains(out.String(), "TC-3-2") {
		t.Fatalf("unexpected detail summary: %s", out.String())
	}
}

func TestEvidenceRunsPageCompactsTargetsAndForwardsFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/evidence/runs" || r.URL.Query().Get("page") != "true" || r.URL.Query().Get("target_id") != "abc" || r.URL.Query().Get("outcome") != "pass" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"runs":[{"id":"run","kind":"ci","ran_at":"2026-10-18T10:00:00Z","status":"complete","source_revision":"commit-run","log_ref":"report.log","runner_kind":"github_actions","runner_agent_slug":"ci-agent","runner_user_id":7,"environment":{"RUN_ENV_FULL":"linux"},"raw_evidence":"full run report","results":[{"id":"result-id","run_id":"run","target_type":"criterion","requirement_kind":"system_requirement","target_id":"abc","target_clause":"1","test_case_ref":"TC-2-1","role":"green","result":"pass","validity":"current","revision":"commit-result","detail":{"command":"mix test","report":"test-report.txt"},"environment":{"RESULT_ENV_FULL":"test"},"raw_evidence":"full result report","metadata":{"source":"ci"}}]}],"next_cursor":null}}`))
	}))
	defer server.Close()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"runs", "--target-id", "abc", "--outcome", "pass", "--full", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"abc", "next_cursor", "TC-2-1", "mix test", "commit-result", "commit-run", "report.log", "github_actions", "ci-agent", "RUN_ENV_FULL", "RESULT_ENV_FULL", "full run report", "full result report", "metadata", "result-id"} {
		if !strings.Contains(out.String(), expected) {
			t.Errorf("full evidence output is missing %q: %s", expected, out.String())
		}
	}
	if strings.Contains(out.String(), "abc123") {
		t.Fatalf("unexpected evidence output: %s", out.String())
	}
}

func TestEvidenceRunsDefaultKeepsCompactProjection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_results") != "" {
			t.Errorf("default runs requested full results: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"runs":[{"id":"run","kind":"ci","status":"complete","source_revision":"RUN_ENV_FULL","environment":{"RUN_ENV_FULL":"linux"},"raw_evidence":"full report","results":[{"id":"result-id","run_id":"run","target_type":"criterion","target_id":"abc","test_case_ref":"TC-2-1","result":"pass","validity":"current","revision":"commit-result","detail":{"command":"mix test"},"environment":{"RESULT_ENV_FULL":"test"},"raw_evidence":"full result report","metadata":{"source":"ci"}}]}],"next_cursor":null}}`))
	}))
	defer server.Close()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"runs", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, fullOnly := range []string{"RUN_ENV_FULL", "RESULT_ENV_FULL", "raw_evidence", "metadata", "result-id"} {
		if strings.Contains(out.String(), fullOnly) {
			t.Errorf("default evidence output included full-only field %q: %s", fullOnly, out.String())
		}
	}
	if !strings.Contains(out.String(), "mix test") {
		t.Fatalf("default compact result lost existing detail field: %s", out.String())
	}
}
