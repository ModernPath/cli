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

// Epic 370: direct current proof batch requests retain atomic refusal handling
// and single-criterion input compatibility.

type batchedCall struct {
	method, path string
	body         map[string]any
}

// batchedServer records every request and answers each with the given status and body.
func batchedServer(t *testing.T, status int, reply string, calls *[]batchedCall) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		_ = decoder.Decode(&body)
		*calls = append(*calls, batchedCall{r.Method, r.URL.Path, body})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(server.Close)
	return server
}

func runBatchedEvidenceRecord(t *testing.T, server *httptest.Server, input string) (string, error) {
	t.Helper()
	root := newEvidenceCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 4), nil })
	root.SetArgs([]string{"record", "--file", "-", "--json"})
	root.SetIn(strings.NewReader(input))
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	err := root.Execute()
	return out.String(), err
}

func decodeBatchedInput(t *testing.T, input string) map[string]any {
	t.Helper()
	var value map[string]any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertBatchedOrder(t *testing.T, output string, values ...string) {
	t.Helper()
	last := -1
	for _, value := range values {
		at := strings.Index(output, value)
		if at < 0 || at < last {
			t.Fatalf("output must list %v in input order: %s", values, output)
		}
		last = at
	}
}

const batchedInput = `{"items":[{"criterion_id":"first","tests":[{"test_case_id":"test","result":"pass"}]},{"criterion_id":"second","tests":[{"test_case_id":"test","result":"fail"}]}]}`

func TestEvidenceRecordBatchAndSingleContract(t *testing.T) {
	t.Run("batch preserves items and ordered receipts", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 200, `{"data":{"items":[{"criterion_id":"first"},{"criterion_id":"second"}]}}`, &calls)
		output, err := runBatchedEvidenceRecord(t, server, batchedInput)
		if err != nil || len(calls) != 1 || calls[0].path != "/api/v1/sync/evidence/batch" || calls[0].body["system_id"] != json.Number("4") || !reflect.DeepEqual(calls[0].body["items"], decodeBatchedInput(t, batchedInput)["items"]) {
			t.Fatalf("batch: %v %#v %s", err, calls, output)
		}
		assertBatchedOrder(t, output, "first", "second")
	})
	t.Run("refusal identifies criterion and index", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 422, `{"error":{"code":"invalid_verification_endpoint","message":"TestCase is not in this System."},"index":1,"item":{"criterion_id":"second"}}`, &calls)
		_, err := runBatchedEvidenceRecord(t, server, batchedInput)
		if err == nil || !strings.Contains(err.Error(), "invalid_verification_endpoint — item 1 (criterion second) — TestCase is not in this System.") {
			t.Fatalf("refusal: %v", err)
		}
	})
	t.Run("single criterion uses single route", func(t *testing.T) {
		var calls []batchedCall
		input := `{"criterion_id":"first","tests":[{"test_case_id":"test","result":"pass"}]}`
		server := batchedServer(t, 200, `{"data":{"results":[{"id":"result"}],"traces":[]}}`, &calls)
		_, err := runBatchedEvidenceRecord(t, server, input)
		want := decodeBatchedInput(t, input)
		want["system_id"] = json.Number("4")
		if err != nil || len(calls) != 1 || calls[0].path != "/api/v1/sync/evidence" || !reflect.DeepEqual(calls[0].body, want) {
			t.Fatalf("single: %v %#v", err, calls)
		}
	})
}
