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
		if r.URL.Path != "/api/compliance/test-cases" || r.URL.Query().Get("page") != "true" || r.URL.Query().Get("system_id") != "4" || r.URL.Query().Get("limit") != "20" || r.URL.Query().Get("query") != "sign in" || r.URL.Query().Get("order") != "title" {
			t.Errorf("request = %s %s", r.Method, r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"items":[{"id":"11111111-1111-4111-8111-111111111111","display_ref":"11111111-1111-4111-8111-111111111111","title":"Sign in","system_id":4,"steps":[{"number":1}]}],"next_cursor":"opaque"}}`))
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"list", "--query", "sign in", "--order", "title", "--json"})
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
	if !strings.Contains(out.String(), "11111111-1111-4111-8111-111111111111") || !strings.Contains(out.String(), "opaque") {
		t.Fatalf("missing compact ref or cursor: %s", out.String())
	}
}

func TestTestCaseGetUsesNativeIdentityAndSelectedSummary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/compliance/test-cases/11111111-1111-4111-8111-111111111111" || r.URL.Query().Get("system_id") != "4" {
			t.Errorf("request = %s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"entity":{"id":"11111111-1111-4111-8111-111111111111","system_id":4,"title":"Sign in","description":"Long","steps":[1]},"display_id":"11111111-1111-4111-8111-111111111111"}}`))
	}))
	defer server.Close()
	root := newTestCasesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"get", "11111111-1111-4111-8111-111111111111", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "Long") || strings.Contains(out.String(), "steps") || !strings.Contains(out.String(), "11111111-1111-4111-8111-111111111111") {
		t.Fatalf("unexpected detail summary: %s", out.String())
	}
}

func TestEvidenceLatestForwardsFiltersAndPreservesCurrentPair(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/evidence/latest" || r.URL.Query().Get("system_id") != "4" || r.URL.Query().Get("target_id") != "abc" || r.URL.Query().Get("outcome") != "pass" {
			t.Errorf("request=%s", r.URL)
		}
		_, _ = w.Write([]byte(`{"data":{"targets":[{"id":"result-id","criterion_id":"criterion","target_id":"abc","result":"pass"}],"next_cursor":null}}`))
	}))
	defer server.Close()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"latest", "--target-id", "abc", "--outcome", "pass", "--json"})
	var out bytes.Buffer
	root.SetOut(&out)
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"abc", "next_cursor", "result-id", "criterion", "pass"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %s: %s", want, out.String())
		}
	}
}
