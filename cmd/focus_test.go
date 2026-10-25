package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-PLN-134 §134.3 (EPIC-NEXT-005): `modernpath focus <id>` POSTs
// {system_id, ref_id, requirement_kind (for UUIDs), source: "cli"} with the workspace
// bearer, prints the ref and its title; `--clear` DELETEs; the no-arg form GETs
// and lists; a non-200 is a refusal, never printed under a green success line.

func TestFocusDeclarePostsUUIDAsCli(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/focus" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_id":"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001","requirement_kind":"user_requirement","ref_kind":"requirement","display_ref":"UR-9-4","title":"Focus states recorded per person","resolved":true,"set_by":"declared","source":"cli"},"previous":null}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}

	out, err := focusDeclare(env, "8F3D6CA1-4DC2-4BC3-9F4A-1BC50E830001", "user_requirement")
	if err != nil {
		t.Fatal(err)
	}
	if out.status != http.StatusOK {
		t.Fatalf("status = %d", out.status)
	}
	if gotBody["ref_id"] != "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001" {
		t.Fatalf("focus ref = %v", gotBody["ref_id"])
	}
	if gotBody["requirement_kind"] != "user_requirement" {
		t.Fatalf("requirement kind = %v", gotBody["requirement_kind"])
	}
	if gotBody["source"] != "cli" {
		t.Fatalf("source = %v, want cli", gotBody["source"])
	}
	if id, _ := gotBody["system_id"].(float64); int(id) != 42 {
		t.Fatalf("system_id = %v, want 42", gotBody["system_id"])
	}

	line := formatFocusLine(out.focus)
	if !strings.Contains(line, "UR-9-4") || !strings.Contains(line, "Focus states recorded") || strings.Contains(line, "8f3d6ca1") {
		t.Fatalf("declare line %q missing ref/title", line)
	}
}

func TestFocusDeclarePostsNativeEpicInteger(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_id":42,"ref_kind":"epic","title":"Epic","resolved":true,"set_by":"declared","source":"cli"}}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, "42", "")
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["ref_id"] != float64(42) {
		t.Fatalf("ref_id = %v, want native Epic ID 42", gotBody["ref_id"])
	}
	if _, supplied := gotBody["requirement_kind"]; supplied {
		t.Fatal("Epic request must not carry requirement_kind")
	}
	if got := focusDisplayRef(out.focus); got != "42" {
		t.Fatalf("Epic display ref = %q, want native integer display", got)
	}
}

func TestFocusDeclareRefusesDisplayCodeBeforePost(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	_, err := focusDeclare(env, "UR-PLN-133", "user_requirement")
	if err == nil || !strings.Contains(err.Error(), "typed requirement UUID") {
		t.Fatalf("error = %v, want native-reference refusal", err)
	}
	if called {
		t.Fatal("retired requirement reference was sent to the server")
	}
}

func TestFocusDeclareRequiresKindForRequirementUUID(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	_, err := focusDeclare(env, "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001", "")
	if err == nil || !strings.Contains(err.Error(), "requirement-kind") {
		t.Fatalf("error = %v, want typed UUID requirement", err)
	}
	if called {
		t.Fatal("untyped requirement UUID was sent to the server")
	}
}

func TestFocusDeclareUnresolvedFlagsUnresolvedReference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_id":"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830002","requirement_kind":"system_requirement","ref_kind":"requirement","title":null,"resolved":false,"set_by":"declared","source":"cli"},"previous":null}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830002", "system_requirement")
	if err != nil {
		t.Fatal(err)
	}

	line := formatFocusLine(out.focus)
	if !strings.Contains(line, "Requirement") || !strings.Contains(line, "not resolved in this system") || strings.Contains(line, "8f3d6ca1") {
		t.Fatalf("unresolved line %q must flag the missing ref", line)
	}
}

func TestFocusDeclareRefusalCarriesServerMessageNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":{"message":"focus is unavailable for this UUID"}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001", "user_requirement")
	if err != nil {
		t.Fatal(err)
	}
	if out.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", out.status)
	}
	if out.focus != nil {
		t.Fatalf("a refusal must carry no focus to print under ok: got %v", out.focus)
	}
	if !strings.Contains(out.message, "focus is unavailable for this UUID") {
		t.Fatalf("refusal message %q did not preserve the server text", out.message)
	}
}

func TestFocusClearSendsDelete(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"ended":[{"ref_id":"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001","requirement_kind":"user_requirement"}]}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusClear(env)
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if !strings.Contains(gotQuery, "system_id=42") {
		t.Fatalf("query = %q, want system_id=42", gotQuery)
	}
	if len(out.ended) != 1 || out.ended[0]["ref_id"] != "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001" {
		t.Fatalf("ended = %v", out.ended)
	}
}

func TestFocusDropSendsDeleteWithRef(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"ended":[{"ref_id":"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001","requirement_kind":"user_requirement"}]}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	// A UUID is normalized before being sent as the scoped focus reference.
	out, err := focusDrop(env, "8F3D6CA1-4DC2-4BC3-9F4A-1BC50E830001", "user_requirement")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if !strings.Contains(gotQuery, "system_id=42") || !strings.Contains(gotQuery, "ref_id=8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001") || !strings.Contains(gotQuery, "requirement_kind=user_requirement") {
		t.Fatalf("query = %q, want native requirement ref and kind", gotQuery)
	}
	if len(out.ended) != 1 || out.ended[0]["ref_id"] != "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001" {
		t.Fatalf("ended = %v", out.ended)
	}
}

func TestFocusListGetsAndFormatsPerPerson(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus_states":[{"user":{"id":1,"name":"Pasi"},"ref_id":"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001","ref_kind":"requirement","requirement_kind":"user_requirement","display_ref":"UR-9-4","title":"Focus states","set_by":"declared","source":"cli","started_at":"2026-08-29T09:00:00.000000Z"}]}}`)
	}))
	defer srv.Close()

	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusList(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.list) != 1 {
		t.Fatalf("list len = %d", len(out.list))
	}

	line := formatFocusListLine(out.list[0])
	for _, want := range []string{"Pasi", "UR-9-4", "declared"} {
		if !strings.Contains(line, want) {
			t.Fatalf("list line %q missing %q", line, want)
		}
	}
}

func TestFocusLabelsUseDisplayRefWhileKeepingUUIDTarget(t *testing.T) {
	entry := map[string]any{
		"ref_id":           "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001",
		"requirement_kind": "user_requirement",
		"requirement_id":   "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001",
		"display_ref":      "UR-9-4",
		"title":            "Pinned requirement",
		"resolved":         true,
		"set_by":           "declared",
	}

	if got := formatFocusLine(entry); !strings.Contains(got, "UR-9-4") || strings.Contains(got, "8f3d6ca1") {
		t.Fatalf("formatFocusLine = %q; want display reference without UUID", got)
	}
	if got := formatFocusListLine(entry); !strings.Contains(got, "UR-9-4") || strings.Contains(got, "8f3d6ca1") {
		t.Fatalf("formatFocusListLine = %q; want display reference without UUID", got)
	}
}

func TestFocusBacklogDeclareAndDropUseExplicitNativeKind(t *testing.T) {
	id := "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001"
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["ref_id"] != id || body["ref_kind"] != "backlog_record" || body["requirement_kind"] != nil {
				t.Errorf("invalid Backlog declaration: %v", body)
			}
			_, _ = io.WriteString(w, `{"data":{"focus":{"ref_id":"`+id+`","ref_kind":"backlog_record","title":"Prepare pagination","resolved":true}}}`)
		} else {
			if r.Method != http.MethodDelete || r.URL.Query().Get("ref_id") != id || r.URL.Query().Get("ref_kind") != "backlog_record" || r.URL.Query().Has("requirement_kind") {
				t.Errorf("invalid scoped drop: %s %s", r.Method, r.URL)
			}
			_, _ = io.WriteString(w, `{"data":{"ended":[]}}`)
		}
	}))
	defer srv.Close()
	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, strings.ToUpper(id), "backlog_record")
	if err != nil || out.status != 200 {
		t.Fatalf("Backlog declaration refused: %+v %v", out, err)
	}
	if focusDisplayRef(out.focus) != id {
		t.Fatalf("Backlog label=%q, want native UUID", focusDisplayRef(out.focus))
	}
	if _, err := focusDrop(env, id, "backlog_record"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(methods, ",") != "POST,DELETE" {
		t.Fatalf("requests=%v", methods)
	}
}

func TestFocusBacklogRefusesInferenceBeforePosting(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	env := &cliEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	if _, err := focusInfer(env, "8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001", "cli", "backlog_record"); err == nil {
		t.Fatal("inferred Backlog focus accepted")
	}
	if called {
		t.Fatal("refused inference posted to server")
	}
}

func TestFocusBacklogMixedSelectorsAreRefusedBeforeLoadingSession(t *testing.T) {
	oldRef, oldRequirement := focusRefKind, focusRequirementKind
	defer func() { focusRefKind, focusRequirementKind = oldRef, oldRequirement }()
	focusRefKind, focusRequirementKind = "backlog_record", "system_requirement"
	if err := focusCmd.RunE(focusCmd, []string{"8f3d6ca1-4dc2-4bc3-9f4a-1bc50e830001"}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed selection error=%v", err)
	}
}
