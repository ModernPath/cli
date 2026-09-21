package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// PR #458 review, finding 1 (REQ-CROSS-386): on a chunked run the unknown-op
// retry answered for the failing chunk alone, and its rows were merged only
// when that chunk was the first — a 207 retry on a later chunk reported
// nothing, the run read as success and the lane was stamped. The retry now
// happens inside the loop: the kind is dropped from this and every later
// chunk, the chunk is re-posted, and every chunk's rows reach the report.
func TestREQCROSS386ChunkedRetryKeepsReportingFailures(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// REQ-CROSS-390: this fake predates the contract read (an older server).
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		calls++
		var batch map[string]any
		_ = json.NewDecoder(r.Body).Decode(&batch)
		ops, _ := batch["ops"].([]any)
		w.Header().Set("Content-Type", "application/json")
		switch calls {
		case 1: // chunk 1 lands
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"results": []any{map[string]any{"external_id": "REQ-A", "type": "upsert_requirement", "result": "created"}},
			}})
		case 2: // chunk 2 carries an op kind this server does not know
			w.WriteHeader(422)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
				"op_index": 0, "external_id": "W-1", "details": map[string]any{"type": []any{"unknown op type upsert_widget"}},
			}})
		default: // the retry without that kind: one op failed
			for _, op := range ops {
				if m, _ := op.(map[string]any); str(m, "type") == "upsert_widget" {
					t.Errorf("the retry must not carry the dropped kind: %v", ops)
				}
			}
			w.WriteHeader(207)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"results": []any{map[string]any{"external_id": "REQ-B", "type": "upsert_requirement", "result": "failed", "op_index": float64(0), "reason": "title: can't be blank"}},
				"failed":  float64(1),
			}})
		}
	}))
	t.Cleanup(srv.Close)
	stubSyncSleep(t)
	env := &factoryEnv{APIURL: srv.URL, SystemID: 4, token: "t"}

	chunks := [][]map[string]any{
		{{"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-A"}}},
		{{"type": "upsert_widget", "payload": map[string]any{"external_id": "W-1"}}, {"type": "upsert_requirement", "payload": map[string]any{"external_id": "REQ-B"}}},
	}
	newBatch := func(chunk []map[string]any) map[string]any {
		return map[string]any{"system_id": 4, "ops": chunk}
	}

	var outcome syncChunksOutcome
	var err error
	out := captureOut(t, func() { outcome, err = postSyncChunks(env, chunks, newBatch) })
	if err != nil {
		t.Fatalf("a retried chunk that lands with a report is not a transport failure: %v", err)
	}
	if !outcome.partial {
		t.Fatalf("a 207 on the retried chunk must mark the run partial: %+v", outcome)
	}
	ids := []string{}
	for _, r := range outcome.results {
		m, _ := r.(map[string]any)
		ids = append(ids, str(m, "external_id")+":"+str(m, "result"))
	}
	joined := strings.Join(ids, " ")
	if !strings.Contains(joined, "REQ-A:created") || !strings.Contains(joined, "REQ-B:failed") {
		t.Fatalf("every chunk's rows, the retried one included, must reach the report; got %s", joined)
	}
	if !strings.Contains(out, "upsert_widget") {
		t.Fatalf("the dropped kind must be named:\n%s", out)
	}
}
