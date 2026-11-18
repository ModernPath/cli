package cmd

// Epic 437 (SR-4921 criterion 3108b71c): the Epic detail route serves the
// System's Active Engineering Constraints as `engineering_constraints`, each
// entry with the two Epic facts `cited_by_review` and `accepted_since_approval`.
// `epics get` accepts the field in --fields and includes it with --full. The
// tests drive the root command and use no new symbol, so they build on a binary
// without the field and fail on `unknown field "engineering_constraints"`.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const epicEngineeringConstraintsReply = `{"data":{"id":42,"system_id":1,"title":"Wallet checkout","process_status":"IN_PROGRESS",
"engineering_constraints":[
 {"id":"aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa","ref":"EC-12","number":12,"title":"No raw tasks",
  "statement":"Background work runs through Oban, never Task.start.","rationale":"Supervision and retries",
  "verification_method":"Search for Task.start and Task.async outside the jobs app.","revision":2,
  "accepted_at":"2026-11-18T09:00:00Z","accepted_by":"user-1","cited_by_review":true,"accepted_since_approval":false}
]}}`

func epicEngineeringConstraintsServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/systems" {
			healthyProbe(w, r)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/work/epics/42" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(epicEngineeringConstraintsReply))
	}))
	t.Cleanup(server.Close)
	return server
}

// TestCase 04ca4372-b162-48f7-aaa4-456d315b2124
func TestEpicGetAcceptsFieldsEngineeringConstraints(t *testing.T) {
	cobraWorkspace(t, epicEngineeringConstraintsServer(t))

	out, err := runRoot(t, "epics", "get", "42", "--fields", "engineering_constraints", "--json")
	if err != nil {
		t.Fatalf("epics get --fields engineering_constraints: %v\n%s", err, out)
	}
	var selected map[string]any
	if err := json.Unmarshal([]byte(out), &selected); err != nil {
		t.Fatalf("epics get --json is not JSON: %v\n%s", err, out)
	}
	constraints, ok := selected["engineering_constraints"].([]any)
	if len(selected) != 1 || !ok || len(constraints) != 1 {
		t.Fatalf("--fields engineering_constraints must return exactly that field as served: %#v", selected)
	}
	entry, _ := constraints[0].(map[string]any)
	for key, want := range map[string]any{
		"ref": "EC-12", "revision": float64(2), "title": "No raw tasks",
		"statement":           "Background work runs through Oban, never Task.start.",
		"rationale":           "Supervision and retries",
		"verification_method": "Search for Task.start and Task.async outside the jobs app.",
		"cited_by_review":     true, "accepted_since_approval": false,
	} {
		if entry[key] != want {
			t.Errorf("entry %q = %#v, want %#v (the field is printed as served)", key, entry[key], want)
		}
	}

	out, err = runRoot(t, "epics", "get", "42", "--fields", "engineering_constraints")
	if err != nil || !strings.Contains(out, "engineering_constraints: [") || !strings.Contains(out, "EC-12") {
		t.Fatalf("text output must print engineering_constraints as a JSON array field: %q err=%v", out, err)
	}
}

// TestCase 04ca4372-b162-48f7-aaa4-456d315b2124
func TestEpicGetFullIncludesTheEngineeringConstraints(t *testing.T) {
	cobraWorkspace(t, epicEngineeringConstraintsServer(t))

	out, err := runRoot(t, "epics", "get", "42", "--full", "--json")
	if err != nil {
		t.Fatalf("epics get --full: %v\n%s", err, out)
	}
	var full map[string]any
	if err := json.Unmarshal([]byte(out), &full); err != nil {
		t.Fatalf("epics get --full --json is not JSON: %v\n%s", err, out)
	}
	constraints, ok := full["engineering_constraints"].([]any)
	if !ok || len(constraints) != 1 {
		t.Fatalf("--full must include engineering_constraints: %#v", full)
	}
}
