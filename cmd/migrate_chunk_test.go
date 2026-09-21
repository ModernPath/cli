package cmd

// migrate run's import pass must chunk /api/v1/sync/batch the same way factory
// sync does: a full-corpus import is thousands of ops, and one POST carrying
// all of them 504s at a platform edge with a tight per-request timeout (test-plat
// answered 504 on the whole batch, nothing landed). MODERNPATH_SYNC_CHUNK_SIZE
// brings each POST back under the window, and the per-result counts must
// aggregate across the chunks so the two-pass idempotence proof still reads the
// whole corpus.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestMigrateSyncPassChunksByEnv(t *testing.T) {
	t.Setenv("MODERNPATH_SYNC_CHUNK_SIZE", "2")

	var mu sync.Mutex
	var batchSizes []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// REQ-CROSS-390: this fake predates the contract read (an older server).
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		ops, _ := body["ops"].([]any)
		mu.Lock()
		batchSizes = append(batchSizes, len(ops))
		mu.Unlock()
		var results []any
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			payload, _ := op["payload"].(map[string]any)
			results = append(results, map[string]any{
				"external_id": payload["external_id"], "result": "created",
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": results}})
	}))
	t.Cleanup(srv.Close)

	env := &factoryEnv{APIURL: srv.URL, SystemID: 1, token: "t"}
	ops := make([]map[string]any, 5)
	for i := range ops {
		ops[i] = map[string]any{
			"type":    "upsert_requirement",
			"payload": map[string]any{"external_id": fmt.Sprintf("REQ-%02d", i)},
		}
	}

	counts, changed, err := migrateSyncPass(env, ops)
	if err != nil {
		t.Fatalf("migrateSyncPass: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// 5 ops at chunk size 2 → three POSTs of 2, 2, 1 (order preserved).
	if want := []int{2, 2, 1}; len(batchSizes) != len(want) ||
		batchSizes[0] != want[0] || batchSizes[1] != want[1] || batchSizes[2] != want[2] {
		t.Fatalf("chunk sizes = %v, want %v — the import must split one POST per chunk", batchSizes, want)
	}
	// Counts and changed-ids must aggregate across the chunks, not just the last.
	if counts["created"] != 5 {
		t.Fatalf("counts[created] = %d, want 5 — per-chunk results must be summed", counts["created"])
	}
	if len(changed) != 5 {
		t.Fatalf("changed ids = %d, want 5 — the idempotence proof reads the aggregate", len(changed))
	}
}

// An unset MODERNPATH_SYNC_CHUNK_SIZE keeps the historical single-POST behavior
// for any corpus under the 200-op default: one batch, not many.
func TestMigrateSyncPassSinglePostUnderDefault(t *testing.T) {
	t.Setenv("MODERNPATH_SYNC_CHUNK_SIZE", "")

	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// REQ-CROSS-390: this fake predates the contract read (an older server).
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		ops, _ := body["ops"].([]any)
		mu.Lock()
		posts++
		mu.Unlock()
		var results []any
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			payload, _ := op["payload"].(map[string]any)
			results = append(results, map[string]any{
				"external_id": payload["external_id"], "result": "created",
			})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": results}})
	}))
	t.Cleanup(srv.Close)

	env := &factoryEnv{APIURL: srv.URL, SystemID: 1, token: "t"}
	ops := make([]map[string]any, 10)
	for i := range ops {
		ops[i] = map[string]any{
			"type":    "upsert_requirement",
			"payload": map[string]any{"external_id": fmt.Sprintf("REQ-%02d", i)},
		}
	}

	if _, _, err := migrateSyncPass(env, ops); err != nil {
		t.Fatalf("migrateSyncPass: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Fatalf("10 ops under the 200 default must be one POST, got %d", posts)
	}
}

// A conflict (the server refusing a row) must stop the pass at the chunk that
// hits it, not push the remaining chunks first — otherwise chunking would apply
// more of the corpus past a refusal than the old single-POST path did.
func TestMigrateSyncPassStopsAtFirstConflictingChunk(t *testing.T) {
	t.Setenv("MODERNPATH_SYNC_CHUNK_SIZE", "2")

	var mu sync.Mutex
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// REQ-CROSS-390: this fake predates the contract read (an older server).
		if r.URL.Path == "/api/v1/sync/contract" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		ops, _ := body["ops"].([]any)
		mu.Lock()
		posts++
		mu.Unlock()
		var results []any
		for _, raw := range ops {
			op, _ := raw.(map[string]any)
			payload, _ := op["payload"].(map[string]any)
			id, _ := payload["external_id"].(string)
			result := "created"
			if id == "REQ-00" { // a refused row, in the first chunk
				result = "conflict"
			}
			results = append(results, map[string]any{"external_id": id, "result": result})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": results}})
	}))
	t.Cleanup(srv.Close)

	env := &factoryEnv{APIURL: srv.URL, SystemID: 1, token: "t"}
	ops := make([]map[string]any, 6) // three chunks of two
	for i := range ops {
		ops[i] = map[string]any{
			"type":    "upsert_requirement",
			"payload": map[string]any{"external_id": fmt.Sprintf("REQ-%02d", i)},
		}
	}

	if _, _, err := migrateSyncPass(env, ops); err == nil {
		t.Fatal("a conflict must fail the pass")
	}
	mu.Lock()
	defer mu.Unlock()
	if posts != 1 {
		t.Fatalf("a conflict in the first chunk must stop before later chunks: got %d POSTs, want 1", posts)
	}
}
