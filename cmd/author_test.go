package cmd

// REQ-CROSS-228 — focused lower evidence for the CLI half: the authoring
// verb posts the attributed call and surfaces refusals verbatim. RED first.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorCreatePostsTheAttributedCall(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-100", "work_status": "PROPOSED"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorCreate(env, "requirement", "REQ-AU-100", map[string]any{
		"title": "Authored", "context": "AU", "work_status": "PROPOSED",
	})
	if err != nil {
		t.Fatalf("author create failed: %v", err)
	}
	if got["action"] != "create" {
		t.Fatalf("payload action = %v, want create", got["action"])
	}
	record, _ := got["record"].(map[string]any)
	if record["kind"] != "requirement" || record["external_id"] != "REQ-AU-100" {
		t.Fatalf("record incomplete: %v", record)
	}
	if got["actor"] == nil {
		t.Fatal("the call must be actor-attributed")
	}
}

// REQ-CROSS-259 — the store-backed declaration gate has to be constructible
// through this verb, in both directions: the server's predicates read the
// purpose, the exact scope naming the system, and the structured options the
// approving answer rides. Without these flags an active declaration would be
// unclearable, because the bulk channel that carried the first gate is refused.
func TestAuthorGateCarriesTheDeclarationFields(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "UNFLIP-STORE-BACKED", "state": "open"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	authorTitle = "Clear the store-backed declaration"
	authorGateKind = "approval_request"
	authorGatePurpose = "store_backed_clearing"
	authorGateScope = []string{"system:243"}
	authorGateOptions = []string{
		"approve=Approve — authority returns to the files",
		"refuse=Refuse — the store stays authoritative",
	}
	authorGateTransition = "active->cleared"
	t.Cleanup(func() {
		authorTitle, authorGateKind, authorGatePurpose = "", "question", ""
		authorGateScope, authorGateOptions, authorGateTransition = nil, nil, ""
	})

	if err := authorCreate(env, "gate", "UNFLIP-STORE-BACKED", authorGateFields()); err != nil {
		t.Fatalf("author gate failed: %v", err)
	}

	record, _ := got["record"].(map[string]any)
	if record["purpose"] != "store_backed_clearing" {
		t.Fatalf("purpose = %v, want store_backed_clearing", record["purpose"])
	}
	if record["transition"] != "active->cleared" {
		t.Fatalf("transition = %v, want active->cleared", record["transition"])
	}
	scope, _ := record["exact_scope"].([]any)
	if len(scope) != 1 || scope[0] != "system:243" {
		t.Fatalf("exact_scope = %v, want [system:243]", record["exact_scope"])
	}
	options, _ := record["options"].([]any)
	if len(options) != 2 {
		t.Fatalf("options = %v, want two structured options", record["options"])
	}
	first, _ := options[0].(map[string]any)
	if first["key"] != "approve" || first["label"] != "Approve — authority returns to the files" {
		t.Fatalf("option 0 = %v, want key/label split on the first '='", options[0])
	}
}

func TestAuthorTracePostsTheCompletedMachineVerdict(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{
				"external_id": "TRACE-COLD-EPIC-AU-001",
				"state":       "fail",
				"fingerprint": "packet-sha256",
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorTrace(env, "TRACE-COLD-EPIC-AU-001", map[string]any{
		"title":                          "Cold review EPIC-AU-001",
		"body_md":                        "P1 finding",
		"purpose":                        "cold-review",
		"transition":                     "plan->entry",
		"exact_scope":                    []string{"EPIC-AU-001", "REQ-AU-001"},
		"prerequisite_gate_external_ids": []string{"TRACE-PLAN-EPIC-AU-001"},
		"fingerprint":                    "packet-sha256",
		"verdict":                        "FAIL",
		"sources":                        []map[string]any{{"ref": "RUN:2026-08-26:review"}},
		"application_revision":           "deadbeef",
	})
	if err != nil {
		t.Fatalf("author trace failed: %v", err)
	}
	if got["action"] != "evaluate_trace" {
		t.Fatalf("payload action = %v, want evaluate_trace", got["action"])
	}
	record, _ := got["record"].(map[string]any)
	if record["external_id"] != "TRACE-COLD-EPIC-AU-001" || record["verdict"] != "FAIL" {
		t.Fatalf("trace record incomplete: %v", record)
	}
	if got["actor"] == nil {
		t.Fatal("the trace evaluation must be actor-attributed")
	}
}

// An option flag without a key is a gate nobody can answer approvingly — the
// server refuses such a gate at creation, so the CLI must not build one.
func TestAuthorGateOptionsRefuseAMalformedFlag(t *testing.T) {
	authorGateOptions = []string{"approve"}
	t.Cleanup(func() { authorGateOptions = nil })

	if _, err := parseGateOptions(authorGateOptions); err == nil {
		t.Fatal("a --option without key=label must refuse, not build an unanswerable gate")
	}
}

// REQ-CROSS-258's Go side of the same honesty: an advance response now says on
// what basis it was legal, and the weakest basis — the naming-and-echo check an
// imported gate falls back to — is exactly the one worth seeing. A client that
// reads only `data[kind]` swallows the distinction the server took care to
// make, and the operator cannot tell a transition-bound gate from an imported
// one.
func TestAuthorAdvanceSurfacesTheTransitionBasis(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement":      map[string]any{"external_id": "REQ-AU-221", "work_status": "TODO"},
			"transition_basis": "imported gate: naming and echo only",
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	said := captureCLIOutput(t)
	err := authorAdvance(env, "requirement", "REQ-AU-221", "TODO", "PROPOSED",
		"APPROVE-REQ-AU-221", "gb1", "approved", "")
	if err != nil {
		t.Fatalf("an advance the server accepted must not fail on the extra field: %v", err)
	}
	if !strings.Contains(said(), "naming and echo only") {
		t.Fatalf("the basis the server named must reach the operator:\n%s", said())
	}
}

// And the older shape, which the server still uses for an advance that needed
// no basis: no basis key, no invented one.
func TestAuthorAdvanceSaysNothingWhenNoBasisIsServed(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-222", "work_status": "IN_PROGRESS"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	said := captureCLIOutput(t)
	if err := authorAdvance(env, "requirement", "REQ-AU-222", "IN_PROGRESS", "TODO", "", "", "", ""); err != nil {
		t.Fatalf("advance failed: %v", err)
	}
	if strings.Contains(said(), "basis") {
		t.Fatalf("no basis was served — the CLI must not invent one:\n%s", said())
	}
}

func TestAuthorAdvanceSurfacesRefusalVerbatim(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
			"details": map[string]any{"to": []string{"a human-gated transition needs an ANSWERED gate reference"}},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorAdvance(env, "requirement", "REQ-AU-100", "DONE", "IN_REVIEW", "", "", "", "")
	if err == nil || !strings.Contains(err.Error(), "ANSWERED gate reference") {
		t.Fatalf("the refusal must surface verbatim, got: %v", err)
	}
}
