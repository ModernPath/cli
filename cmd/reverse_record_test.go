package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReverseEngineerRecordPostsNativeRequirementsWithoutRunOrRevisionProof(t *testing.T) {
	input := `{"requirements":[{"id":"8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c","kind":"system_requirement","title":"Catalog lookup","description":"Look up a catalog item"}],"traces":[{"source_type":"system_requirement","source_id":"8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c","target_type":"source_file","repository_key":"catalog","path":"src/catalog.go","link_kind":"implements"},{"source_type":"system_document","source_integer_id":9223372036854775807,"target_type":"system_requirement","target_id":"8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c","link_kind":"relates"}]}`
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/reverse-engineering/requirements" {
			t.Errorf("request = %s %s, want POST direct requirements endpoint", r.Method, r.URL.Path)
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":{"requirements":[],"traces":[]}}`))
	}))
	defer server.Close()

	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetIn(strings.NewReader(input))
	command.SetArgs([]string{"record", "--file", "-", "--json"})
	err := command.Execute()
	if err != nil {
		t.Fatalf("record failed without a run, repository checkout, or revision: %v; output %s", err, out.String())
	}
	if got == nil || !strings.Contains(out.String(), `"requirements": []`) || !strings.Contains(out.String(), `"trace_count": 0`) {
		t.Fatalf("record did not send the native payload and print its compact receipt: payload=%v output=%s", got, out.String())
	}
	for _, field := range []string{"requirements"} {
		entries, ok := got[field].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("record did not preserve one %s entry: %v", field, got[field])
		}
	}
	traces, ok := got["traces"].([]any)
	if !ok || len(traces) != 2 {
		t.Fatalf("record did not preserve both trace entries: %v", got["traces"])
	}
	if _, exists := got["files"]; exists {
		t.Fatal("record sent a separate files array")
	}
	fileTrace := traces[0].(map[string]any)
	if fileTrace["repository_key"] != "catalog" || fileTrace["path"] != "src/catalog.go" {
		t.Fatalf("file trace lost repository/path: %v", fileTrace)
	}
	integerTrace, ok := traces[1].(map[string]any)
	if !ok || integerTrace["source_integer_id"] != json.Number("9223372036854775807") {
		t.Fatalf("record rounded the native integer trace endpoint: %#v", traces[1])
	}
	if _, ok := got["run_id"]; ok {
		t.Fatalf("record added a run dependency: %v", got)
	}
	if _, ok := got["snapshot_digest"]; ok {
		t.Fatalf("record added a snapshot hash: %v", got)
	}
}

// The server projects display_number, not display_ref; the receipt used to
// select a key that was never there and so printed no display reference.
func TestReverseEngineerRecordReceiptNamesKindAndDisplayNumber(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"requirements":[{"id":"8f5ec2c0-57c1-4a9f-aeb0-a8dfc4d12a9c","kind":"system_requirement","display_number":12,"title":"Catalog lookup","work_status":"PROPOSED","description":"long text the receipt leaves out"}],"traces":[{"id":"t1"}]}}`))
	}))
	defer server.Close()

	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetIn(strings.NewReader(`{"requirements":[],"traces":[]}`))
	command.SetArgs([]string{"record", "--file", "-", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("record failed: %v; output %s", err, out.String())
	}
	for _, want := range []string{`"kind": "system_requirement"`, `"display_number": 12`, `"work_status": "PROPOSED"`, `"trace_count": 1`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("receipt lacks %s: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "long text") {
		t.Errorf("receipt is not compact: %s", out.String())
	}
}

func TestReverseEngineerRecordRejectsSeparateFilesArray(t *testing.T) {
	command := newReverseEngineerCommand(func() (*cliEnv, error) {
		t.Fatal("invalid input must not load the connection")
		return nil, nil
	})
	command.SetIn(strings.NewReader(`{"requirements":[],"files":[],"traces":[]}`))
	command.SetArgs([]string{"record", "--file", "-"})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), `unknown input field "files"`) {
		t.Fatalf("expected obsolete files field rejection, got %v", err)
	}
}
