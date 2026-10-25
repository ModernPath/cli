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

// TestCase (Epic 323): "CLI items form for start-execution and evidence record,
// index in refusal, single form unchanged".

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

func runBatchedStartExecution(t *testing.T, server *httptest.Server, input string) (string, error) {
	t.Helper()
	command := newEpicGatesCommand(func() (*cliEnv, error) { return nativeCLIEnv(t, server, 105), nil })
	command.SetArgs([]string{"start-execution", "287", "--file", "-", "--json"})
	command.SetIn(strings.NewReader(input))
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&out)
	err := command.Execute()
	return out.String(), err
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

const (
	batchedRegisterInput = `{"items":[{"revision":"code","obligation_ids":["o-1"]},{"revision":"code","obligation_ids":["o-2","o-3"]}]}`
	batchedFinalizeInput = `{"items":[{"id":"11111111-1111-4111-8111-111111111111","status":"complete","native_verification":{"tests":[],"criteria":[]}},{"id":"22222222-2222-4222-8222-222222222222","status":"error"}]}`
)

func TestCLIItemsFormForStartExecutionAndEvidenceRecordIndexInRefusalSingleFormUnchanged(t *testing.T) {
	t.Run("CLI items form: start-execution posts items to /executions/batch and prints one result per item in input order", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 200, `{"data":{"items":[{"index":0,"run_id":"run-first","execution":{}},{"index":1,"run_id":"run-second","execution":{}}]}}`, &calls)
		output, err := runBatchedStartExecution(t, server, batchedRegisterInput)
		if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/api/work/epics/287/executions/batch" {
			t.Fatalf("items form must post once to the register batch route: %#v", calls)
		}
		if !reflect.DeepEqual(calls[0].body, decodeBatchedInput(t, batchedRegisterInput)) {
			t.Fatalf("items payload changed: %#v", calls[0].body)
		}
		if err != nil {
			t.Fatalf("items form failed: %v %s", err, output)
		}
		assertBatchedOrder(t, output, "run-first", "run-second")
	})

	t.Run("CLI items form: evidence record posts system_id and items to /api/v1/sync/evidence/batch and prints one result per item in input order", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 200, `{"data":{"items":[{"index":0,"result":"updated","run":{"id":"11111111-1111-4111-8111-111111111111"},"traces":[],"results":[]},{"index":1,"result":"replayed","run":{"id":"22222222-2222-4222-8222-222222222222"},"traces":[],"results":[]}]}}`, &calls)
		output, err := runBatchedEvidenceRecord(t, server, batchedFinalizeInput)
		if len(calls) != 1 || calls[0].method != "POST" || calls[0].path != "/api/v1/sync/evidence/batch" {
			t.Fatalf("items form must post once to the finalize batch route: %#v", calls)
		}
		if calls[0].body["system_id"] != json.Number("4") {
			t.Fatalf("batch must carry the bound System: %#v", calls[0].body)
		}
		if !reflect.DeepEqual(calls[0].body["items"], decodeBatchedInput(t, batchedFinalizeInput)["items"]) {
			t.Fatalf("finalize items changed: %#v", calls[0].body["items"])
		}
		if err != nil {
			t.Fatalf("items form failed: %v %s", err, output)
		}
		assertBatchedOrder(t, output, "11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	})

	t.Run("index in refusal: a register batch refusal prints code, item index, obligations and message", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 422, `{"error":{"code":"overlapping_obligations","message":"Two items share an obligation or a test case."},"index":1,"item":{"obligation_ids":["o-2","o-3"]}}`, &calls)
		_, err := runBatchedStartExecution(t, server, batchedRegisterInput)
		if len(calls) != 1 || calls[0].path != "/api/work/epics/287/executions/batch" {
			t.Errorf("items form must post to the register batch route: %#v", calls)
		}
		want := "overlapping_obligations — item 1 (obligations o-2,o-3) — Two items share an obligation or a test case."
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal must name the failing item:\nwant %q\ngot  %v", want, err)
		}
	})

	t.Run("index in refusal: a finalize batch refusal prints code, item index, run and message", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 422, `{"error":{"code":"invalid_verification_endpoint","message":"An executed TestCase is not in this System."},"index":1,"item":{"run_id":"22222222-2222-4222-8222-222222222222"}}`, &calls)
		_, err := runBatchedEvidenceRecord(t, server, batchedFinalizeInput)
		if len(calls) != 1 || calls[0].path != "/api/v1/sync/evidence/batch" {
			t.Errorf("items form must post to the finalize batch route: %#v", calls)
		}
		want := "invalid_verification_endpoint — item 1 (run 22222222-2222-4222-8222-222222222222) — An executed TestCase is not in this System."
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("refusal must name the failing item:\nwant %q\ngot  %v", want, err)
		}
	})

	t.Run("index in refusal: a refusal without a message leaves out the message part", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 403, `{"error":{"code":"not_change_owner"},"index":0,"item":{"run_id":"11111111-1111-4111-8111-111111111111"}}`, &calls)
		_, err := runBatchedEvidenceRecord(t, server, batchedFinalizeInput)
		want := "not_change_owner — item 0 (run 11111111-1111-4111-8111-111111111111)"
		if err == nil || !strings.Contains(err.Error(), want) || strings.Contains(err.Error(), want+" —") {
			t.Fatalf("refusal without a message:\nwant %q\ngot  %v", want, err)
		}
	})

	t.Run("single form unchanged: start-execution posts the single object to /executions", func(t *testing.T) {
		var calls []batchedCall
		single := `{"revision":"code","obligation_ids":["o-1"]}`
		server := batchedServer(t, 200, `{"data":{"run_id":"single-run","execution":{}}}`, &calls)
		output, err := runBatchedStartExecution(t, server, single)
		if err != nil || len(calls) != 1 || calls[0].path != "/api/work/epics/287/executions" || !reflect.DeepEqual(calls[0].body, decodeBatchedInput(t, single)) || !strings.Contains(output, "single-run") {
			t.Fatalf("single registration changed: err=%v calls=%#v output=%s", err, calls, output)
		}
	})

	t.Run("single form unchanged: evidence record posts the single object to /api/v1/sync/evidence", func(t *testing.T) {
		var calls []batchedCall
		single := `{"id":"11111111-1111-4111-8111-111111111111","status":"error"}`
		server := batchedServer(t, 200, `{"data":{"result":"updated","run":{"id":"11111111-1111-4111-8111-111111111111"},"traces":[],"results":[]}}`, &calls)
		_, err := runBatchedEvidenceRecord(t, server, single)
		want := decodeBatchedInput(t, single)
		want["system_id"] = json.Number("4")
		if err != nil || len(calls) != 1 || calls[0].path != "/api/v1/sync/evidence" || !reflect.DeepEqual(calls[0].body, want) {
			t.Fatalf("single finalization changed: err=%v calls=%#v", err, calls)
		}
	})

	t.Run("single form unchanged: a string error from the single register route prints as before", func(t *testing.T) {
		var calls []batchedCall
		server := batchedServer(t, 409, `{"error":"not change owner"}`, &calls)
		_, err := runBatchedStartExecution(t, server, `{"revision":"code","obligation_ids":["o-1"]}`)
		if err == nil || !strings.Contains(err.Error(), "not change owner") {
			t.Fatalf("single refusal changed: %v", err)
		}
	})
}
