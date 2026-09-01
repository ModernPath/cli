package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// REQ-PLN-135 §135.4 (EPIC-NEXT-005): `modernpath focus --infer <ref> --source
// <s>` posts one conclusion — {system_id, ref_external_id, set_by: inferred,
// source, workspace_ref, machine_fingerprint} — the heartbeat identity so the
// server can attribute it to the confirming session.

func TestFocusInferPostsInferredConclusion(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/focus" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_external_id":"REQ-PLN-135","set_by":"inferred","source":"branch"},"previous":null,"applied":true}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: t.TempDir()}

	out, err := focusInfer(env, "req-pln-135", "branch")
	if err != nil {
		t.Fatal(err)
	}
	if out.status != http.StatusOK {
		t.Fatalf("status = %d", out.status)
	}

	if gotBody["set_by"] != "inferred" {
		t.Fatalf("set_by = %v, want inferred", gotBody["set_by"])
	}
	if gotBody["source"] != "branch" {
		t.Fatalf("source = %v, want branch", gotBody["source"])
	}
	if gotBody["ref_external_id"] != "REQ-PLN-135" {
		t.Fatalf("ref = %v (should be upper-cased)", gotBody["ref_external_id"])
	}
	if ws, _ := gotBody["workspace_ref"].(string); ws == "" {
		t.Fatal("workspace_ref must carry the heartbeat identity")
	}
	if mac, _ := gotBody["machine_fingerprint"].(string); mac == "" {
		t.Fatal("machine_fingerprint must carry the heartbeat identity")
	}
}
