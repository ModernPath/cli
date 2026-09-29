package cmd

// REQ-CMP-013 / TOOL-234: distinct observations must not overwrite each other.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEvidenceRunIDsKeepRapidObservationsSeparate(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		id := newEvidenceRunID(now, "a1b2c3")
		if !strings.HasPrefix(id, "RUN-2026-09-27T00-00-00Z-a1b2c3") {
			t.Fatalf("run ID lost its fixed timestamp or revision: %q", id)
		}
		if seen[id] {
			t.Fatalf("distinct observations shared run ID %q", id)
		}
		seen[id] = true
	}
}

func TestEvidencePostsPreserveObservationMetadata(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)
	writeFile(t, filepath.Join(root, "a.txt"), "x\n")
	revision := gitCommitAll(t, root, "one")

	posted := make(chan map[string]any, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		posted <- payload
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"result": "created", "run": payload},
		})
	}))
	t.Cleanup(srv.Close)
	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 4, token: "t"}

	observations := []evidenceOpts{
		{kind: "local_test", fail: "REQ-X-1", role: "RED", log: "focused RED", totals: "passed=0,failed=1"},
		{kind: "local_test", pass: "REQ-X-1,REQ-X-2", log: "full GREEN", totals: "passed=2,failed=0"},
	}
	for i, opts := range observations {
		if err := factoryEvidenceRun(env, opts); err != nil {
			t.Fatalf("observation %d: %v", i, err)
		}
		payload := <-posted
		if id, _ := payload["external_id"].(string); id == "" {
			t.Fatalf("observation %d has no run ID", i)
		}
		if payload["log_ref"] != opts.log {
			t.Fatalf("observation %d lost its log: %v", i, payload)
		}
		results := payload["results"].([]any)
		for _, result := range results {
			if result.(map[string]any)["revision"] != revision {
				t.Fatalf("observation %d lost its tested revision: %v", i, result)
			}
		}
	}
}
