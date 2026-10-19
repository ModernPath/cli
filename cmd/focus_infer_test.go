package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// REQ-PLN-135 §135.4 (EPIC-NEXT-005): `modernpath focus --infer <ref> --source
// <s>` posts one conclusion — {system_id, ref_id, requirement_kind, set_by: inferred,
// source}. The authenticated caller supplies human attribution.

func TestFocusInferPostsInferredConclusion(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/focus" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","requirement_kind":"system_requirement","ref_kind":"requirement","set_by":"inferred","source":"branch"},"previous":null,"applied":true}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: t.TempDir()}

	out, err := focusInfer(env, "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", "branch", "system_requirement")
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
	if gotBody["ref_id"] != "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa" {
		t.Fatalf("ref = %v (should be normalized UUID)", gotBody["ref_id"])
	}
	if gotBody["requirement_kind"] != "system_requirement" {
		t.Fatalf("requirement_kind = %v", gotBody["requirement_kind"])
	}

}
