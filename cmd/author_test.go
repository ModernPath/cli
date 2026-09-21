package cmd

// REQ-CROSS-228 — focused lower evidence for the CLI half: the authoring
// verb posts the attributed call and surfaces refusals verbatim. RED first.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
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

// REQ-CROSS-367 (EPIC-CROSS-002) — a birth joins its release. `author
// requirement|epic` sends the workspace current_release so the server can
// resolve the target release (rung 2 of the precedence: parent → current_release
// → open delivery target → base). When current_release is unset the field is
// omitted and the server falls through to the open target. RED first: authorCreate
// sends no current_release.
func TestAuthorCreateSendsCurrentRelease(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-367", "work_status": "PROPOSED"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	env := wsEnv(t, srv)
	env.CurrentRelease = "modernpath-v1-09"
	if err := authorCreate(env, "requirement", "REQ-AU-367", map[string]any{
		"title": "Born into a release", "context": "AU",
	}); err != nil {
		t.Fatalf("author create failed: %v", err)
	}
	if got["current_release"] != "modernpath-v1-09" {
		t.Fatalf("author create must send current_release; body current_release = %v, want modernpath-v1-09", got["current_release"])
	}

	// Unset current_release → the field is omitted, so the server falls through to
	// the tenant's open delivery target rather than being handed an empty slug.
	got = nil
	env.CurrentRelease = ""
	if err := authorCreate(env, "epic", "EPIC-AU-367", map[string]any{"title": "Epic"}); err != nil {
		t.Fatalf("author create (epic) failed: %v", err)
	}
	if _, present := got["current_release"]; present {
		t.Fatalf("an unset current_release must be omitted, got %v", got["current_release"])
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
		"title":       "Cold review EPIC-AU-001",
		"body_md":     "P1 finding",
		"purpose":     "cold-review",
		"transition":  "plan->entry",
		"exact_scope": []string{"EPIC-AU-001", "REQ-AU-001"},
		// REQ-CROSS-364: a cold-review verdict carries the review context it was
		// recorded under; without one, creation is refused. The reviewer's
		// --for-review pull stamps it — supplied directly here.
		"review_context_id":              "review-au-001",
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

// REQ-CROSS-357: the server now accepts the gate's stable option key (or that
// key's label) as the echoed answer, not only its free-text answer. So
// `advance --gate-answer <key>` must forward the value the operator gave — the
// key — verbatim; the CLI must not rewrite, map, or drop it. The widening is
// server-side; this locks the CLI's end of the contract.
func TestAuthorAdvanceForwardsGateAnswerVerbatim(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-357", "work_status": "TODO"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	said := captureCLIOutput(t)
	_ = said
	if err := authorAdvance(env, "requirement", "REQ-AU-357", "TODO", "PROPOSED",
		"ENTRY-REQ-AU-357", "gb357", "approve", ""); err != nil {
		t.Fatalf("advance failed: %v", err)
	}
	if got, _ := gotBody["gate_answer"].(string); got != "approve" {
		t.Fatalf("--gate-answer must forward the operator's value verbatim; body gate_answer = %q, want %q", got, "approve")
	}
}

// REQ-CROSS-356 (EPIC-CLI-013): `author gate-withdraw <id> --reason ...` posts
// action:"withdraw" naming the gate and carrying the reason (the audit); the
// actor rides via authorPost.
func TestAuthorGateWithdrawPostsWithdrawAction(t *testing.T) {
	var gotBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{"external_id": "G-WD-9", "state": "dismissed"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	said := captureCLIOutput(t)
	_ = said
	if err := authorGateWithdraw(env, "G-WD-9", "opened by mistake"); err != nil {
		t.Fatalf("gate-withdraw failed: %v", err)
	}

	if gotBody["action"] != "withdraw" {
		t.Fatalf("action = %v, want withdraw", gotBody["action"])
	}
	if gotBody["reason"] != "opened by mistake" {
		t.Fatalf("reason = %v, want the withdrawal reason", gotBody["reason"])
	}
	rec, _ := gotBody["record"].(map[string]any)
	if rec["external_id"] != "G-WD-9" || rec["kind"] != "gate" {
		t.Fatalf("record = %v, want kind gate external_id G-WD-9", rec)
	}
	if gotBody["actor"] == nil {
		t.Fatalf("authorPost must attach the actor; got none")
	}
}

// REQ-CROSS-307 (EPIC-CLI-007) — `author requirement --kind ur` sends
// requirement_kind:"user", so the sanctioned post-flip path can create a user
// requirement and not only a system one (BACKLOG.md:13). Absent the selector the
// SR default is preserved: no field sent, the server defaults to system
// (author.ex:302). A distinct flag from `author advance --kind requirement|epic`.
// RED first: no --kind flag or requirement_kind field exists on `author
// requirement` today.
func TestAuthorRequirementKindDiscriminator(t *testing.T) {
	t.Cleanup(func() { authorRequirementKind, authorTitle, authorContext = "", "", "" })
	authorTitle, authorContext = "A user requirement", "USR"

	authorRequirementKind = "ur"
	fields, err := authorRequirementFields()
	if err != nil {
		t.Fatalf("--kind ur must be accepted: %v", err)
	}
	if fields["requirement_kind"] != "user" {
		t.Fatalf("--kind ur → requirement_kind = %v, want \"user\"", fields["requirement_kind"])
	}

	authorRequirementKind = "sr"
	if fields, _ = authorRequirementFields(); fields["requirement_kind"] != "system" {
		t.Fatalf("--kind sr → requirement_kind = %v, want \"system\"", fields["requirement_kind"])
	}

	// Back-compat: no selector → requirement_kind omitted, server defaults to system.
	authorRequirementKind = ""
	fields, _ = authorRequirementFields()
	if _, present := fields["requirement_kind"]; present {
		t.Fatalf("no --kind must omit requirement_kind, got %v", fields["requirement_kind"])
	}

	// Only ur|sr name a requirement kind — an unknown value refuses rather than
	// silently creating a system requirement.
	authorRequirementKind = "epic"
	if _, err := authorRequirementFields(); err == nil {
		t.Fatal("--kind epic must refuse: only ur|sr name a requirement kind")
	}
}

// REQ-CROSS-304/305/309 (EPIC-CLI-007) — `author update` edits an existing
// requirement's content. The expected_fingerprint rides so a stale edit
// conflicts instead of overwriting, and the content fields the caller set are
// carried through to the record. RED first: authorUpdate does not exist yet.
func TestAuthorUpdatePostsTheContentEdit(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-300", "work_status": "TODO"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorUpdate(env, "requirement", "REQ-AU-300", map[string]any{
		"expected_fingerprint": "sha-current",
		"description":          "Refined behavior",
	})
	if err != nil {
		t.Fatalf("author update failed: %v", err)
	}
	if got["action"] != "update" {
		t.Fatalf("payload action = %v, want update", got["action"])
	}
	record, _ := got["record"].(map[string]any)
	if record["kind"] != "requirement" || record["external_id"] != "REQ-AU-300" {
		t.Fatalf("record incomplete: %v", record)
	}
	if record["expected_fingerprint"] != "sha-current" {
		t.Fatalf("expected_fingerprint must ride the edit, got: %v", record["expected_fingerprint"])
	}
	if record["description"] != "Refined behavior" {
		t.Fatalf("content field not carried: %v", record["description"])
	}
	if got["actor"] == nil {
		t.Fatal("the call must be actor-attributed")
	}
}

// Criteria supplied inline as a JSON array parse and land in record["criteria"]
// — the server replaces the requirement's scenarios/criteria from this array.
func TestAuthorUpdateCarriesInlineCriteria(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-301", "work_status": "TODO"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	criteria, err := parseCriteria(`[{"external_id":"SCN-1","kind":"scenario","given":"g","when":"w","then":"t","position":1}]`)
	if err != nil {
		t.Fatalf("inline criteria JSON must parse: %v", err)
	}
	if err := authorUpdate(env, "requirement", "REQ-AU-301", map[string]any{
		"expected_fingerprint": "sha-current",
		"criteria":             criteria,
	}); err != nil {
		t.Fatalf("author update with criteria failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	list, _ := record["criteria"].([]any)
	if len(list) != 1 {
		t.Fatalf("criteria = %v, want one parsed scenario", record["criteria"])
	}
	first, _ := list[0].(map[string]any)
	if first["external_id"] != "SCN-1" || first["kind"] != "scenario" {
		t.Fatalf("criteria element not carried intact: %v", list[0])
	}
}

// REQ-CROSS-306 (EPIC-CLI-007) — `author relate` declares a UR<->SR relation
// from the SR side: the parent user-requirement ids ride as parent_external_ids
// and the expected_fingerprint guards a stale target. RED first: authorRelate
// does not exist yet.
func TestAuthorRelatePostsTheParentDeclaration(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{"external_id": "REQ-AU-310", "work_status": "TODO"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorRelate(env, "REQ-AU-310", map[string]any{
		"parent_external_ids":  []string{"REQ-AU-200", "REQ-AU-201"},
		"expected_fingerprint": "sha-current",
	})
	if err != nil {
		t.Fatalf("author relate failed: %v", err)
	}
	if got["action"] != "relate" {
		t.Fatalf("payload action = %v, want relate", got["action"])
	}
	record, _ := got["record"].(map[string]any)
	if record["kind"] != "requirement" || record["external_id"] != "REQ-AU-310" {
		t.Fatalf("record incomplete: %v", record)
	}
	parents, _ := record["parent_external_ids"].([]any)
	if len(parents) != 2 || parents[0] != "REQ-AU-200" || parents[1] != "REQ-AU-201" {
		t.Fatalf("parent_external_ids = %v, want [REQ-AU-200 REQ-AU-201]", record["parent_external_ids"])
	}
	if record["expected_fingerprint"] != "sha-current" {
		t.Fatalf("expected_fingerprint must ride the relation, got: %v", record["expected_fingerprint"])
	}
	if got["actor"] == nil {
		t.Fatal("the call must be actor-attributed")
	}
}

// REQ-CROSS-306/304 (EPIC-CLI-007) — a relate is a fingerprint-bumping edit, so
// the server serves the record's new content fingerprint in the response
// exactly as `author update` does. The CLI must print it, so a subsequent
// fingerprint-guarded edit chains without a side-channel read. RED first:
// authorRelate did not print the served fingerprint.
func TestAuthorRelateServesTheChainedFingerprint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirement": map[string]any{
				"external_id": "REQ-AU-310",
				"work_status": "TODO",
				"fingerprint": "sha-after-relate",
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	var buf bytes.Buffer
	oldOut, oldNoColor := color.Output, color.NoColor
	color.Output, color.NoColor = &buf, true
	t.Cleanup(func() { color.Output, color.NoColor = oldOut, oldNoColor })

	if err := authorRelate(env, "REQ-AU-310", map[string]any{
		"parent_external_ids":  []string{"REQ-AU-200"},
		"expected_fingerprint": "sha-current",
	}); err != nil {
		t.Fatalf("author relate failed: %v", err)
	}

	if !strings.Contains(buf.String(), "sha-after-relate") {
		t.Fatalf("author relate must print the served fingerprint for chaining; got: %q", buf.String())
	}
}

// REQ-CROSS-304 (EPIC-CLI-007) — `author update --kind epic` edits an epic's
// content: the record's kind rides as "epic" so the server routes it to the
// epic edit clause (not the requirement tables). RED first: authorUpdate had no
// kind parameter and always sent "requirement".
func TestAuthorUpdateKindEpicPostsTheEpicEdit(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"epic": map[string]any{"external_id": "EPIC-DEMO-1", "process_status": "PROPOSED", "fingerprint": "sha-next"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorUpdate(env, "epic", "EPIC-DEMO-1", map[string]any{
		"expected_fingerprint": "sha-current",
		"description":          "Description added",
	})
	if err != nil {
		t.Fatalf("author update --kind epic failed: %v", err)
	}
	record, _ := got["record"].(map[string]any)
	if record["kind"] != "epic" || record["external_id"] != "EPIC-DEMO-1" {
		t.Fatalf("epic edit record incomplete: %v", record)
	}
	if record["description"] != "Description added" {
		t.Fatalf("description not carried: %v", record["description"])
	}
	if record["expected_fingerprint"] != "sha-current" {
		t.Fatalf("expected_fingerprint must ride the epic edit, got: %v", record["expected_fingerprint"])
	}
}

// SR-CLI-0071 (EPIC-CLI-007) — `author update` must reach the SR content fields
// the server persists: boundary, rationale, verification_method. Without flags
// for them the sanctioned CLI path cannot perform the full content edit. An
// omitted flag sends no key (server preserves the stored value). RED first:
// the flags are not registered, so Flags().Set errors.
func TestAuthorUpdateCarriesBoundaryRationaleAndVerificationMethod(t *testing.T) {
	t.Cleanup(func() {
		authorBoundary, authorRationale, authorVerificationMethod = "", "", ""
	})

	// omitted → the record carries no such key (nothing overwrites the stored value)
	base, err := authorUpdateRecord(authorUpdateCmd)
	if err != nil {
		t.Fatalf("authorUpdateRecord (omitted): %v", err)
	}
	for _, k := range []string{"boundary", "rationale", "verification_method"} {
		if _, present := base[k]; present {
			t.Fatalf("an omitted flag must send no %q key, got %v", k, base[k])
		}
	}

	for flag, val := range map[string]string{
		"boundary":            "the change boundary",
		"rationale":           "why the requirement exists",
		"verification-method": "unit test",
	} {
		if err := authorUpdateCmd.Flags().Set(flag, val); err != nil {
			t.Fatalf("--%s must be a registered author-update flag: %v", flag, err)
		}
	}

	rec, err := authorUpdateRecord(authorUpdateCmd)
	if err != nil {
		t.Fatalf("authorUpdateRecord (set): %v", err)
	}
	if rec["boundary"] != "the change boundary" {
		t.Fatalf("--boundary must ride as boundary, got %v", rec["boundary"])
	}
	if rec["rationale"] != "why the requirement exists" {
		t.Fatalf("--rationale must ride as rationale, got %v", rec["rationale"])
	}
	if rec["verification_method"] != "unit test" {
		t.Fatalf("--verification-method must ride as verification_method, got %v", rec["verification_method"])
	}
}

// REQ-CROSS-306 (EPIC-CLI-007) — `author relate --mode` reaches the server's
// declare/withdraw/confirm modes (declare was the only one wired). RED first:
// authorRelateFields and authorRelateMode do not exist yet.
func TestAuthorRelateModeRidesInTheRecord(t *testing.T) {
	t.Cleanup(func() { authorRelateMode, authorParents, authorExpectedFingerprint = "declare", nil, "" })
	authorParents = []string{"UR-1"}
	authorExpectedFingerprint = "sha-current"

	authorRelateMode = "withdraw"
	if authorRelateFields()["mode"] != "withdraw" {
		t.Fatalf("--mode withdraw must ride as mode, got %v", authorRelateFields()["mode"])
	}
	authorRelateMode = "confirm"
	if authorRelateFields()["mode"] != "confirm" {
		t.Fatalf("--mode confirm must ride as mode, got %v", authorRelateFields()["mode"])
	}

	// the default mode still rides, and parent/fingerprint ride alongside it
	authorRelateMode = "declare"
	fields := authorRelateFields()
	if fields["mode"] != "declare" {
		t.Fatalf("default mode must be declare, got %v", fields["mode"])
	}
	if fields["expected_fingerprint"] != "sha-current" {
		t.Fatalf("expected_fingerprint must still ride, got %v", fields["expected_fingerprint"])
	}
}

// REQ-CROSS-306 (EPIC-CLI-007) — `author member` authors EPIC MEMBERSHIP: the
// call posts action:"relate" with a kind:"epic" record naming the epic and its
// member requirements, so the CLI reaches the server's epic-membership clause.
// RED first: authorMember does not exist yet.
func TestAuthorMemberPostsEpicMembership(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"epic": map[string]any{"external_id": "EPIC-CLI-007", "process_status": "IN_PROGRESS"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	err := authorMember(env, "EPIC-CLI-007", map[string]any{
		"member_external_ids": []string{"SR-1", "UR-2"},
		"mode":                "declare",
	})
	if err != nil {
		t.Fatalf("author member failed: %v", err)
	}
	if got["action"] != "relate" {
		t.Fatalf("payload action = %v, want relate", got["action"])
	}
	record, _ := got["record"].(map[string]any)
	if record["kind"] != "epic" || record["external_id"] != "EPIC-CLI-007" {
		t.Fatalf("record incomplete: %v", record)
	}
	members, _ := record["member_external_ids"].([]any)
	if len(members) != 2 || members[0] != "SR-1" || members[1] != "UR-2" {
		t.Fatalf("member_external_ids = %v, want [SR-1 UR-2]", record["member_external_ids"])
	}
	if record["mode"] != "declare" {
		t.Fatalf("mode = %v, want declare", record["mode"])
	}
	if got["actor"] == nil {
		t.Fatal("the call must be actor-attributed")
	}
}

// REQ-CROSS-310 (SR-CLI-0081): `author update` gains --context and repeatable
// --source (the server already accepts both); RED first — the flags are not
// registered and authorUpdateRecord does not build them.
func TestAuthorUpdateCarriesContextAndSources(t *testing.T) {
	if authorUpdateCmd.Flags().Lookup("context") == nil {
		t.Fatal("author update must register --context")
	}
	if authorUpdateCmd.Flags().Lookup("source") == nil {
		t.Fatal("author update must register --source")
	}

	cmd := &cobra.Command{Use: "update"}
	cmd.Flags().StringVar(&authorContext, "context", "", "")
	cmd.Flags().StringArrayVar(&authorSources, "source", nil, "")
	cmd.Flags().StringVar(&authorExpectedFingerprint, "expected-fingerprint", "", "")
	_ = cmd.Flags().Set("context", "CROSS")
	_ = cmd.Flags().Set("source", "USER:2026-09-02:x")
	t.Cleanup(func() { authorContext = ""; authorSources = nil; authorExpectedFingerprint = "" })

	record, err := authorUpdateRecord(cmd)
	if err != nil {
		t.Fatalf("authorUpdateRecord: %v", err)
	}
	if record["context"] != "CROSS" {
		t.Fatalf("record context = %v, want CROSS", record["context"])
	}
	if record["source_citations"] == nil {
		t.Fatalf("record must carry source_citations, got %v", record)
	}
}

// REQ-CROSS-310: `author member` prints the epic's new content fingerprint so a
// membership edit chains without a side-channel read; RED first — it prints none.
func TestAuthorMemberPrintsFingerprintHint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"epic": map[string]any{"external_id": "EPIC-M-1", "process_status": "TODO", "fingerprint": "abc123"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	var buf bytes.Buffer
	prev := color.Output
	color.Output = &buf
	t.Cleanup(func() { color.Output = prev })

	if err := authorMember(env, "EPIC-M-1", map[string]any{
		"member_external_ids": []string{"SR-1"}, "mode": "declare",
	}); err != nil {
		t.Fatalf("authorMember: %v", err)
	}
	if !strings.Contains(buf.String(), "abc123") {
		t.Fatalf("member output missing the fingerprint hint: %q", buf.String())
	}
}

// REQ-CROSS-310: `author gate` gains the brief block, recommendation, repeatable
// --source and --prerequisite — the fields PROCESS requires on a human gate;
// RED first — authorGateFields builds none of them and the vars do not exist.
func TestAuthorGateCarriesBriefAndRecommendation(t *testing.T) {
	authorTitle = "Approve entry"
	authorGateBriefWhat = "decide the entry"
	authorGateBriefWhyNow = "blocked work"
	authorGateRecommendation = "approve"
	authorGateSources = []string{"USER:2026-09-02:x"}
	authorGatePrereqs = []string{"TRACE-ENTRY-1"}
	t.Cleanup(func() {
		authorTitle, authorGateBriefWhat, authorGateBriefWhyNow = "", "", ""
		authorGateRecommendation = ""
		authorGateSources, authorGatePrereqs = nil, nil
	})

	fields := authorGateFields()
	brief, ok := fields["brief"].(map[string]any)
	if !ok || brief["what"] != "decide the entry" || brief["why_now"] != "blocked work" {
		t.Fatalf("gate brief not assembled: %v", fields["brief"])
	}
	if fields["recommendation"] != "approve" {
		t.Fatalf("recommendation missing: %v", fields["recommendation"])
	}
	if fields["sources"] == nil {
		t.Fatal("sources missing")
	}
	if fields["prerequisite_gate_external_ids"] == nil {
		t.Fatal("prerequisite_gate_external_ids missing")
	}
}

// REQ-CROSS-371 (EPIC-CLI-016) — an entry gate names what it moves, and the
// person who opened it should see exactly that. The server now serves the gate's
// exact_scope on the create response; the CLI prints it on success so an epic
// approval's member list is visible without a second read. RED first: the
// success line carries only kind, id and state.
func TestAuthorGateSuccessPrintsTheServedExactScope(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"gate": map[string]any{
				"external_id": "ENTRY-EPIC-X",
				"state":       "open",
				"fingerprint": "abc123",
				"exact_scope": []string{"EPIC-X", "REQ-X-1", "REQ-X-2"},
			},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	done := captureCLIOutput(t)
	if err := authorCreate(env, "gate", "ENTRY-EPIC-X", map[string]any{"title": "Entry"}); err != nil {
		t.Fatalf("author gate failed: %v", err)
	}
	out := done()
	for _, want := range []string{"EPIC-X", "REQ-X-1", "REQ-X-2"} {
		if !strings.Contains(out, want) {
			t.Fatalf("success output must print the served exact_scope (%s); got:\n%s", want, out)
		}
	}
}

// REQ-CROSS-353 (EPIC-CLI-016), D-EPIC-CLI-016-SUPERSEDE-CARRIER: `author gate
// <new> --supersedes <old>` names the predecessor on the wire so the server
// retires it in the same write that births the successor. RED first: the flag
// does not exist, so parsing it fails and no predecessor rides the record.
func TestAuthorGateSupersedesNamesThePredecessor(t *testing.T) {
	prev := authorGateFields()
	t.Cleanup(func() { _ = prev })
	if err := authorGateCmd.ParseFlags([]string{"--title", "Corrected", "--supersedes", "D-OLD-1"}); err != nil {
		t.Fatalf("--supersedes must be a gate flag: %v", err)
	}
	fields := authorGateFields()
	if fields["predecessor_external_id"] != "D-OLD-1" {
		t.Fatalf("the predecessor must ride the record as predecessor_external_id; got %v", fields)
	}
}
