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
// {system_id, ref_external_id (upper-cased), source: "cli"} with the workspace
// bearer, prints the ref and its title; `--clear` DELETEs; the no-arg form GETs
// and lists; a non-200 is a refusal, never printed under a green success line.

func TestFocusDeclarePostsUppercasedRefAsCli(t *testing.T) {
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/focus" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_external_id":"REQ-PLN-133","title":"Focus states recorded per person","resolved":true,"set_by":"declared","source":"cli"},"previous":null}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}

	out, err := focusDeclare(env, "req-pln-133")
	if err != nil {
		t.Fatal(err)
	}
	if out.status != http.StatusOK {
		t.Fatalf("status = %d", out.status)
	}
	if gotBody["ref_external_id"] != "REQ-PLN-133" {
		t.Fatalf("ref not upper-cased: %v", gotBody["ref_external_id"])
	}
	if gotBody["source"] != "cli" {
		t.Fatalf("source = %v, want cli", gotBody["source"])
	}
	if id, _ := gotBody["system_id"].(float64); int(id) != 42 {
		t.Fatalf("system_id = %v, want 42", gotBody["system_id"])
	}

	line := formatFocusLine(out.focus)
	if !strings.Contains(line, "REQ-PLN-133") || !strings.Contains(line, "Focus states recorded") {
		t.Fatalf("declare line %q missing ref/title", line)
	}
}

func TestFocusDeclareUnresolvedFlagsNotInLedger(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus":{"ref_external_id":"REQ-PLN-999","title":null,"resolved":false,"set_by":"declared","source":"cli"},"previous":null}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, "REQ-PLN-999")
	if err != nil {
		t.Fatal(err)
	}

	line := formatFocusLine(out.focus)
	if !strings.Contains(line, "REQ-PLN-999") || !strings.Contains(line, "not in this system's ledger") {
		t.Fatalf("unresolved line %q must flag the missing ref", line)
	}
}

func TestFocusDeclareRefusalCarriesServerMessageNotSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"error":{"message":"\"nope\" is not a work reference — use REQ-<CTX>-NNN or EPIC-<AREA>-NNN"}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusDeclare(env, "nope")
	if err != nil {
		t.Fatal(err)
	}
	if out.status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", out.status)
	}
	if out.focus != nil {
		t.Fatalf("a refusal must carry no focus to print under ok: got %v", out.focus)
	}
	if !strings.Contains(out.message, "REQ") {
		t.Fatalf("refusal message %q should name the grammar", out.message)
	}
}

func TestFocusClearSendsDelete(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"ended":[{"ref_external_id":"REQ-PLN-133"}]}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
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
	if len(out.ended) != 1 || out.ended[0]["ref_external_id"] != "REQ-PLN-133" {
		t.Fatalf("ended = %v", out.ended)
	}
}

func TestFocusDropSendsDeleteWithRef(t *testing.T) {
	var gotMethod, gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"ended":[{"ref_external_id":"REQ-PLN-133"}]}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	// a lowercase ref is upcased into the query (REQ-PLN-142 §142.5)
	out, err := focusDrop(env, "req-pln-133")
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method = %s, want DELETE", gotMethod)
	}
	if !strings.Contains(gotQuery, "system_id=42") || !strings.Contains(gotQuery, "ref=REQ-PLN-133") {
		t.Fatalf("query = %q, want system_id=42 and ref=REQ-PLN-133", gotQuery)
	}
	if len(out.ended) != 1 || out.ended[0]["ref_external_id"] != "REQ-PLN-133" {
		t.Fatalf("ended = %v", out.ended)
	}
}

func TestFocusListGetsAndFormatsPerPerson(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"data":{"focus_states":[{"user":{"id":1,"name":"Pasi"},"ref_external_id":"REQ-PLN-133","title":"Focus states","set_by":"declared","source":"cli","started_at":"2026-08-29T09:00:00.000000Z"}]}}`)
	}))
	defer srv.Close()

	env := &factoryEnv{APIURL: srv.URL, token: "t", SystemID: 42, Root: "."}
	out, err := focusList(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.list) != 1 {
		t.Fatalf("list len = %d", len(out.list))
	}

	line := formatFocusListLine(out.list[0])
	for _, want := range []string{"Pasi", "REQ-PLN-133", "declared"} {
		if !strings.Contains(line, want) {
			t.Fatalf("list line %q missing %q", line, want)
		}
	}
}
