package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// B9 (REQ-CROSS-259, UNFLIP-259): `migrate clear` posts the store-backed
// clearing transition — state "cleared" on an ANSWERED gate — reopening the
// bulk channel. Unlike flip it sends no corpus_revision and runs no import.
func TestMigrateClearPostsClearedState(t *testing.T) {
	var got map[string]any
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"state":"cleared","source_tag":"USER:2026-09-05:clear"}}`))
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 5, token: "t"}
	if err := migrateClear(env, "UNFLIP-1", "fp123", "approved"); err != nil {
		t.Fatalf("migrateClear: %v", err)
	}
	if gotMethod != "POST" || gotPath != "/api/v1/sync/store-backed" {
		t.Fatalf("posted %s %s; want POST /api/v1/sync/store-backed", gotMethod, gotPath)
	}
	if got["state"] != "cleared" || got["gate_ref"] != "UNFLIP-1" || got["gate_fingerprint"] != "fp123" || got["gate_answer"] != "approved" {
		t.Fatalf("payload = %v", got)
	}
	if _, present := got["corpus_revision"]; present {
		t.Fatal("clear must not send corpus_revision — that is activation's audit trail")
	}
}

func TestMigrateClearRefusesWithoutGateTriple(t *testing.T) {
	env := &factoryEnv{Root: t.TempDir(), APIURL: "http://127.0.0.1:1", SystemID: 5, token: "t"}
	if err := migrateClear(env, "", "fp", "a"); err == nil {
		t.Fatal("missing --gate must error")
	}
	if err := migrateClear(env, "g", "", "a"); err == nil {
		t.Fatal("missing --gate-fingerprint must error")
	}
	if err := migrateClear(env, "g", "fp", ""); err == nil {
		t.Fatal("missing --gate-answer must error")
	}
}
