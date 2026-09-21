package cmd

// REQ-CROSS-421 (EPIC-CLI-022): `author demote <id> --to … --basis … --reason
// USER:…` opens the demotion gate DEMOTE-<id> — purpose demotion, the
// transition from the served status, the basis and the reason as sources, no
// prerequisite trace — and `--apply` after the human's answer advances the
// item on that gate with the triple read from the store, printing the
// server's transition basis and what followed. Every local refusal sends no
// request. Pattern A: a capture server per test.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type demoteServer struct {
	srv      *httptest.Server
	authored []map[string]any
	status   string
	gate     map[string]any
	calls    int
}

func newDemoteServer(t *testing.T, status string, gate map[string]any) *demoteServer {
	t.Helper()
	ds := &demoteServer{status: status, gate: gate}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		ds.calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirements":      []map[string]any{{"external_id": "REQ-D-1", "work_status": ds.status, "fingerprint": "fp-d1"}},
			"user_requirements": []map[string]any{},
		}})
	})
	mux.HandleFunc("/api/v1/sync/epics", func(w http.ResponseWriter, r *http.Request) {
		ds.calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"epics": []map[string]any{{"external_id": "EPIC-D", "code": "EPIC-D", "process_status": ds.status}},
		}})
	})
	mux.HandleFunc("/api/v1/sync/gates/", func(w http.ResponseWriter, r *http.Request) {
		ds.calls++
		if ds.gate == nil {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no such gate " + strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/")}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": ds.gate}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		ds.calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		ds.authored = append(ds.authored, body)
		record, _ := body["record"].(map[string]any)
		if body["action"] == "advance" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"requirement":      map[string]any{"external_id": record["external_id"], "work_status": body["to"]},
				"transition_basis": "gate_transition_binding",
				"followed_transitions": []map[string]any{
					{"type": "user_requirement", "external_id": "UR-D", "from": "DONE", "to": "IN_PROGRESS"},
					{"type": "epic", "external_id": "EPIC-D", "from": "DONE", "to": "IN_PROGRESS"},
				},
			}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": record["external_id"], "state": "open", "fingerprint": "gfp", "exact_scope": record["exact_scope"],
		}}})
	})
	ds.srv = httptest.NewServer(mux)
	t.Cleanup(ds.srv.Close)
	return ds
}

func answeredDemotionGate(transition string, option string) map[string]any {
	return map[string]any{
		"external_id": "DEMOTE-REQ-D-1", "state": "answered", "purpose": "demotion",
		"transition": transition, "fingerprint": "gfp-current", "content_fingerprint": "gfp-row", "chosen_option_keys": []string{option},
		"exact_scope": []string{"REQ-D-1"},
	}
}

func TestAuthorDemoteOpensTheDemotionGateFromTheServedStatus(t *testing.T) {
	ds := newDemoteServer(t, "DONE", nil)
	env := wsEnv(t, ds.srv)
	said := captureCLIOutput(t)
	var err error
	out := captureOut(t, func() {
		err = authorDemoteOpen(env, demoteOpts{id: "REQ-D-1", kind: "requirement", to: "IN_PROGRESS", basis: "defect", reason: "USER:2026-09-21:the shipped behaviour is wrong"})
	})
	out += said()
	if err != nil {
		t.Fatalf("demote open: %v\n%s", err, out)
	}
	if len(ds.authored) != 1 || ds.authored[0]["action"] != "create" {
		t.Fatalf("exactly one gate is created, got %v", ds.authored)
	}
	record, _ := ds.authored[0]["record"].(map[string]any)
	if record["kind"] != "gate" || record["external_id"] != "DEMOTE-REQ-D-1" || record["purpose"] != "demotion" ||
		record["gate_kind"] != "approval_request" || record["transition"] != "DONE->IN_PROGRESS" || record["basis"] != "defect" {
		t.Fatalf("the demotion gate shape is wrong: %v", record)
	}
	if scope := stringSlice(record["exact_scope"]); len(scope) != 1 || scope[0] != "REQ-D-1" {
		t.Fatalf("the gate names the item alone, got %v", scope)
	}
	sources, _ := record["sources"].([]any)
	if len(sources) != 1 || sources[0].(map[string]any)["kind"] != "user" || sources[0].(map[string]any)["ref"] != "USER:2026-09-21:the shipped behaviour is wrong" {
		t.Fatalf("the human's reason rides as the USER: source, got %v", record["sources"])
	}
	if _, present := record["prerequisite_gate_external_ids"]; present {
		t.Fatalf("a demotion gate never names a prerequisite trace, got %v", record["prerequisite_gate_external_ids"])
	}
	if opts, _ := record["options"].([]any); len(opts) < 2 || opts[0].(map[string]any)["key"] != "approve" {
		t.Fatalf("approve and decline are the options, got %v", record["options"])
	}
	if brief, _ := record["brief"].(map[string]any); brief["what"] == nil || brief["risk_if_wrong"] == nil {
		t.Fatalf("the brief is composed in plain words, got %v", record["brief"])
	}
	if !strings.Contains(out, "factory answer DEMOTE-REQ-D-1") || !strings.Contains(out, "author demote REQ-D-1 --apply") {
		t.Fatalf("the answer and apply hints are printed: %q", out)
	}
}

func TestAuthorDemoteSupersededNamesTheReplacement(t *testing.T) {
	ds := newDemoteServer(t, "DONE", nil)
	env := wsEnv(t, ds.srv)
	if err := authorDemoteOpen(env, demoteOpts{id: "REQ-D-1", kind: "requirement", to: "OBSOLETE", basis: "superseded", reason: "USER:2026-09-21:replaced", supersededBy: "REQ-D-2"}); err != nil {
		t.Fatalf("demote open: %v", err)
	}
	record, _ := ds.authored[0]["record"].(map[string]any)
	if record["superseded_by"] != "REQ-D-2" || record["transition"] != "DONE->OBSOLETE" {
		t.Fatalf("a supersession names its replacement, got %v", record)
	}
}

func TestAuthorDemoteRefusesLocallyBeforeAnyRequest(t *testing.T) {
	cases := []struct {
		name string
		o    demoteOpts
		want string
	}{
		{"no USER: reason", demoteOpts{id: "REQ-D-1", kind: "requirement", to: "IN_PROGRESS", basis: "defect", reason: "because"}, "USER:"},
		{"basis mismatch", demoteOpts{id: "REQ-D-1", kind: "requirement", to: "PROPOSED", basis: "defect", reason: "USER:2026-09-21:x"}, "IN_PROGRESS"},
		{"unknown basis", demoteOpts{id: "REQ-D-1", kind: "requirement", to: "PROPOSED", basis: "whim", reason: "USER:2026-09-21:x"}, "reversed-decision"},
		{"superseded without replacement", demoteOpts{id: "REQ-D-1", kind: "requirement", to: "OBSOLETE", basis: "superseded", reason: "USER:2026-09-21:x"}, "--superseded-by"},
	}
	for _, tc := range cases {
		ds := newDemoteServer(t, "DONE", nil)
		env := wsEnv(t, ds.srv)
		err := authorDemoteOpen(env, tc.o)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: refused naming the rule (%q), got %v", tc.name, tc.want, err)
		}
		if ds.calls != 0 {
			t.Fatalf("%s: a local refusal sends no request, got %d calls", tc.name, ds.calls)
		}
	}
}

func TestAuthorDemoteRefusesAnItemNotDelivered(t *testing.T) {
	ds := newDemoteServer(t, "TODO", nil)
	env := wsEnv(t, ds.srv)
	err := authorDemoteOpen(env, demoteOpts{id: "REQ-D-1", kind: "requirement", to: "IN_PROGRESS", basis: "defect", reason: "USER:2026-09-21:x"})
	if err == nil || !strings.Contains(err.Error(), "TODO") || !strings.Contains(err.Error(), "IN_REVIEW or DONE") {
		t.Fatalf("only delivered work is demoted; the served status is named, got %v", err)
	}
	if len(ds.authored) != 0 {
		t.Fatalf("nothing is authored, got %v", ds.authored)
	}
}

func TestAuthorDemoteApplyAdvancesOnTheAnsweredGate(t *testing.T) {
	ds := newDemoteServer(t, "DONE", answeredDemotionGate("DONE->IN_PROGRESS", "approve"))
	env := wsEnv(t, ds.srv)
	said := captureCLIOutput(t)
	var err error
	out := captureOut(t, func() { err = authorDemoteApply(env, "REQ-D-1", "requirement", "") })
	out += said()
	if err != nil {
		t.Fatalf("demote apply: %v\n%s", err, out)
	}
	if len(ds.authored) != 1 || ds.authored[0]["action"] != "advance" {
		t.Fatalf("exactly one advance is posted, got %v", ds.authored)
	}
	body := ds.authored[0]
	if body["to"] != "IN_PROGRESS" || body["expected"] != "DONE" || body["gate_ref"] != "DEMOTE-REQ-D-1" ||
		body["gate_fingerprint"] != "gfp-current" || body["gate_answer"] != "approve" {
		t.Fatalf("the advance carries the gate triple read from the store (the served guard value, not the row's content_fingerprint), got %v", body)
	}
	for _, want := range []string{"gate_transition_binding", "EPIC-D", "UR-D", "IN_PROGRESS", "rdd-build"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the basis, the followers and the next step are printed (%q missing): %q", want, out)
		}
	}
}

func TestAuthorDemoteApplyRefusesAGateNotAnsweredApprove(t *testing.T) {
	for _, tc := range []struct {
		name string
		gate map[string]any
		want string
	}{
		{"open", map[string]any{"external_id": "DEMOTE-REQ-D-1", "state": "open", "purpose": "demotion", "transition": "DONE->IN_PROGRESS"}, "answer"},
		{"declined", answeredDemotionGate("DONE->IN_PROGRESS", "decline"), "decline"},
		{"closed", map[string]any{"external_id": "DEMOTE-REQ-D-1", "state": "closed", "applied_state": "applied", "purpose": "demotion", "transition": "DONE->IN_PROGRESS"}, "already applied"},
		{"absent", nil, "author demote REQ-D-1"},
	} {
		ds := newDemoteServer(t, "DONE", tc.gate)
		env := wsEnv(t, ds.srv)
		err := authorDemoteApply(env, "REQ-D-1", "requirement", "")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: refused naming the state (%q), got %v", tc.name, tc.want, err)
		}
		if len(ds.authored) != 0 {
			t.Fatalf("%s: nothing is advanced, got %v", tc.name, ds.authored)
		}
	}
}

// Driven through cobra, the way an operator runs it: the demote flags are a
// dedicated set and never rebind author advance's --to (author_advance_flags_test).
func TestAuthorDemoteBindsItsOwnFlagsThroughCobra(t *testing.T) {
	ds := newDemoteServer(t, "DONE", nil)
	cobraWorkspace(t, ds.srv)
	resetTreeFlags(rootCmd)
	rootCmd.SetArgs([]string{"author", "demote", "REQ-D-1", "--to", "PROPOSED", "--basis", "reversed-decision", "--reason", "USER:2026-09-21:the decision is reversed"})
	defer rootCmd.SetArgs(nil)
	captureOut(t, func() { _ = rootCmd.Execute() })
	if len(ds.authored) != 1 {
		t.Fatalf("one gate is posted through cobra, got %v", ds.authored)
	}
	record, _ := ds.authored[0]["record"].(map[string]any)
	if record["transition"] != "DONE->PROPOSED" || record["basis"] != "reversed-decision" {
		t.Fatalf("the flags reach the record, got %v", record)
	}
	if authorTo != "" {
		t.Fatalf("author advance's --to must stay unbound by a demote run, got %q", authorTo)
	}
}
