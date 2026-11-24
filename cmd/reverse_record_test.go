package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

// Epic 341 (G3), test case f17f2164 (TC-G3-11), criterion 5c8a73dd: the
// receipt record --file prints lists each requirement's what_it_has as the
// server returns it, a list or null, beside its id, kind, display_number,
// title and work_status. The five keys hold on the first slice (guard); the
// receipt leaves what_it_has out there, so that assertion fails.

const (
	g3ReceiptListID = "c3000000-0000-4000-8000-000000000001"
	g3ReceiptNullID = "c3000000-0000-4000-8000-000000000002"
)

// g3ReceiptResponse is the recording's answer for two requirements: one
// recorded as having an interface and a screen, one with nothing recorded.
const g3ReceiptResponse = `{"data":{"requirements":[` +
	`{"id":"` + g3ReceiptListID + `","kind":"system_requirement","display_number":31,"title":"Show the station map","work_status":"PROPOSED","context":"CATALOG","description":"long text the receipt leaves out","what_it_has":["interface","screen"]},` +
	`{"id":"` + g3ReceiptNullID + `","kind":"system_requirement","display_number":32,"title":"Price a charging session","work_status":"PROPOSED","context":"CATALOG","description":"long text the receipt leaves out","what_it_has":null}` +
	`],"traces":[]}}`

// g3ReceiptInput is the staged record input whose answer is g3ReceiptResponse.
const g3ReceiptInput = `{"requirements":[` +
	`{"id":"` + g3ReceiptListID + `","kind":"system_requirement","title":"Show the station map","description":"The map API and screen show the stations near a position.","context":"CATALOG","what_it_has":["screen","interface"]},` +
	`{"id":"` + g3ReceiptNullID + `","kind":"system_requirement","title":"Price a charging session","description":"The pricing job prices a closed charging session.","context":"CATALOG"}` +
	`],"traces":[]}`

// g3RequireReceiptWhatItHas checks that text is a receipt whose requirements
// keep the five keys and what_it_has exactly as g3ReceiptResponse serves them.
func g3RequireReceiptWhatItHas(t *testing.T, what, text string) {
	t.Helper()
	var receipt map[string]any
	if err := json.Unmarshal([]byte(text), &receipt); err != nil {
		t.Errorf("%s is not one JSON object: %v\n%s", what, err, text)
		return
	}
	var served map[string]any
	if err := json.Unmarshal([]byte(g3ReceiptResponse), &served); err != nil {
		t.Fatal(err)
	}
	want := map[string]map[string]any{}
	for _, raw := range served["data"].(map[string]any)["requirements"].([]any) {
		row := raw.(map[string]any)
		want[row["id"].(string)] = row
	}
	rows, _ := receipt["requirements"].([]any)
	if len(rows) != len(want) {
		t.Errorf("%s lists %d requirements; want %d:\n%s", what, len(rows), len(want), text)
	}
	for _, raw := range rows {
		row, _ := raw.(map[string]any)
		id, _ := row["id"].(string)
		served, ok := want[id]
		if !ok {
			t.Errorf("%s lists a requirement the server did not return: %v", what, row)
			continue
		}
		// Guard: the five keys the receipt has always kept.
		for _, key := range []string{"id", "kind", "display_number", "title", "work_status"} {
			if value, present := row[key]; !present || !reflect.DeepEqual(value, served[key]) {
				t.Errorf("%s requirement %s %s = %v (present %v); want %v", what, id, key, value, present, served[key])
			}
		}
		if _, present := row["description"]; present {
			t.Errorf("%s requirement %s carries its description; the receipt is compact", what, id)
		}
		value, present := row["what_it_has"]
		if !present || !reflect.DeepEqual(value, served["what_it_has"]) {
			gotJSON, _ := json.Marshal(value)
			wantJSON, _ := json.Marshal(served["what_it_has"])
			t.Errorf("%s requirement %s what_it_has = %s (present %v); want %s as the server returned it", what, id, gotJSON, present, wantJSON)
		}
	}
}

func TestG3_f17f2164_RecordFileReceiptListsWhatItHas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/systems/4/reverse-engineering/requirements" {
			t.Errorf("request = %s %s, want POST to the recording route", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(g3ReceiptResponse))
	}))
	defer server.Close()

	command := newReverseEngineerCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	command.SetIn(strings.NewReader(g3ReceiptInput))
	command.SetArgs([]string{"record", "--file", "-", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatalf("record --file failed: %v; output %s", err, out.String())
	}
	g3RequireReceiptWhatItHas(t, "the receipt record --file prints", out.String())
}
