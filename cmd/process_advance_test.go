package cmd

// REQ-CROSS-374 (EPIC-CLI-017): `process advance <SR>` reads the recorded RED
// and passing evidence, records the lower trace at the SR's content hash, runs
// the reconcile passes the proofs allow, and reports the resulting status; it
// refuses with the specific unmet fact instead of doing nothing, and never
// writes on a refusal.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const advHash = "a6c3bc9efbb86c397a57d0f2d75fc60aa6c4541456b39545a5886c3ec6b9fcd8"

// ceremonyServer is the fake store the ceremony verbs talk to: a held epic
// selection, a delivery context (facts nil when serveFacts is false), a gate
// read that knows only the ids in existingGates, an author capture, and a
// reconcile that answers the scripted responses in order (then "nothing").
type ceremonyServer struct {
	srv           *httptest.Server
	authored      []map[string]any
	reconciles    []map[string]any
	reconcileResp []map[string]any
	evidence      []map[string]any
	selections    []map[string]any
	existingGates map[string]map[string]any
	// packetSections is what GET /sync/packet-sections serves (REQ-CROSS-373:
	// the entry_brief section the enter verb reads).
	packetSections []map[string]any
	// order records every write endpoint hit, in sequence (REQ-CROSS-375: the
	// completion ceremony is evidence -> trace -> gate -> phase).
	order []string
	// factsState, when set, is served beside a null facts block — the live
	// server's "unavailable" (no selection, or the gather failed).
	factsState string
	// factsOnlyFor, when set, serves the facts only for that ?scope= — any
	// other scope reads "unavailable", as the live server answers for an id
	// that names no held selection (a member, for one).
	factsOnlyFor string
}

func newCeremonyServer(t *testing.T, facts map[string]any, serveFacts bool) *ceremonyServer {
	t.Helper()
	cs := &ceremonyServer{existingGates: map[string]map[string]any{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			cs.selections = append(cs.selections, body)
			cs.order = append(cs.order, "work-selection")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"selection": body}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"active_release": []any{map[string]any{"slug": "r", "status": "active"}},
			"current": map[string]any{
				"scope_external_id": "EPIC-A", "scope_kind": "epic",
				"members": []string{"REQ-A-1", "REQ-A-2"}, "phase": "build",
			},
			"suspended": []any{}, "history": []any{},
		}})
	})
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"packet_fingerprint": pinAggregate, "process_revision": strings.Repeat("c", 40),
			"checks": map[string]any{}, "derived_phase": "build", "declared_phase": "build",
		}
		if serveFacts && (cs.factsOnlyFor == "" || r.URL.Query().Get("scope") == cs.factsOnlyFor) {
			data["facts"] = facts
			data["facts_state"] = "served"
		} else if serveFacts {
			data["facts"] = nil
			data["facts_state"] = "unavailable"
		} else if cs.factsState != "" {
			data["facts"] = nil
			data["facts_state"] = cs.factsState
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("/api/v1/sync/packet-sections", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"packet_sections": cs.packetSections}})
	})
	mux.HandleFunc("/api/v1/sync/gates/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/")
		if g, ok := cs.existingGates[id]; ok {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": g}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no such gate " + id}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		cs.authored = append(cs.authored, body)
		cs.order = append(cs.order, "author:"+str(body, "action"))
		record, _ := body["record"].(map[string]any)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": map[string]any{
			"external_id": record["external_id"], "state": "pass", "fingerprint": record["fingerprint"],
			"exact_scope": record["exact_scope"],
		}}})
	})
	mux.HandleFunc("/api/v1/sync/reconcile", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		cs.reconciles = append(cs.reconciles, body)
		cs.order = append(cs.order, "reconcile")
		resp := map[string]any{"applied": true, "transitions": []any{}, "fails": []any{}}
		if len(cs.reconcileResp) > 0 {
			resp = cs.reconcileResp[0]
			cs.reconcileResp = cs.reconcileResp[1:]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": resp})
	})
	mux.HandleFunc("/api/v1/sync/evidence", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		cs.evidence = append(cs.evidence, body)
		cs.order = append(cs.order, "evidence")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"result": "created", "run": map[string]any{"external_id": "RUN-C"}, "warnings": []string{},
		}})
	})
	cs.srv = httptest.NewServer(mux)
	t.Cleanup(cs.srv.Close)
	return cs
}

func advanceFacts(status, evidenceState string, red, lowerPass bool) map[string]any {
	return map[string]any{
		"aggregate":  pinAggregate,
		"entry_gate": map[string]any{"applied": true, "pinned_aggregate": pinAggregate},
		"scope":      map[string]any{"external_id": "EPIC-A", "kind": "epic", "status": "IN_PROGRESS"},
		"members": []map[string]any{
			{"external_id": "REQ-A-1", "kind": "sr", "status": status, "content_fingerprint": advHash,
				"evidence_state": evidenceState, "red_recorded": red, "lower_trace_pass": lowerPass},
			{"external_id": "REQ-A-2", "kind": "sr", "status": "TODO", "content_fingerprint": strings.Repeat("d", 64),
				"evidence_state": "claimed", "red_recorded": false, "lower_trace_pass": false},
		},
		"cold_review": map[string]any{"verdict": "pass", "independent": true, "trace_external_id": "CR-A"},
		"sections":    map[string]any{"complete": true, "missing": []string{}},
	}
}

func transition(id, from, to string) map[string]any {
	return map[string]any{"external_id": id, "kind": "requirement", "from": from, "to": to, "basis": "test"}
}

// The fake serves static facts, so the status the verb reports is what
// reconcile applied for the SR — the transitions are the fact of record.
func TestProcessAdvanceRecordsTheLowerTraceAndReconciles(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("TODO", "passing", true, false), true)
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{transition("REQ-A-1", "TODO", "IN_PROGRESS")}, "fails": []any{}},
		{"applied": true, "transitions": []any{transition("REQ-A-1", "IN_PROGRESS", "IN_REVIEW"), transition("EPIC-A", "TODO", "IN_PROGRESS")}, "fails": []any{}},
		{"applied": true, "transitions": []any{}, "fails": []any{}},
	}
	env := wsEnv(t, cs.srv)

	var err error
	out := captureOut(t, func() { err = processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"}) })
	if err != nil {
		t.Fatalf("advance: %v\n%s", err, out)
	}
	if len(cs.authored) != 1 {
		t.Fatalf("exactly one trace is recorded, got %d: %v", len(cs.authored), cs.authored)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if cs.authored[0]["action"] != "evaluate_trace" || record["purpose"] != "lower" ||
		record["fingerprint"] != advHash || record["transition"] != "build->verify" ||
		record["external_id"] != "TRACE-LOWER-REQ-A-1" || record["verdict"] != "PASS" {
		t.Fatalf("the lower trace must pin to the SR's content hash with the lower purpose: %v", record)
	}
	if scope, _ := record["exact_scope"].([]any); len(scope) != 1 || scope[0] != "REQ-A-1" {
		t.Fatalf("the lower trace names the SR alone, got %v", record["exact_scope"])
	}
	if len(cs.reconciles) != 3 {
		t.Fatalf("reconcile runs until nothing remains (3 passes here), got %d", len(cs.reconciles))
	}
	for _, rc := range cs.reconciles {
		if rc["apply"] != true || rc["scope"] != "EPIC-A" {
			t.Fatalf("every reconcile applies against the piece that holds the SR, got %v", rc)
		}
	}
	if !strings.Contains(out, "IN_REVIEW") {
		t.Fatalf("the resulting status is reported: %q", out)
	}
}

func TestProcessAdvanceRefusesWithoutRED(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("TODO", "passing", false, false), true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || !strings.Contains(err.Error(), "RED") || !strings.Contains(err.Error(), "--role RED") {
		t.Fatalf("no RED recorded must be refused naming the fact and the remedy, got %v", err)
	}
	if len(cs.authored)+len(cs.reconciles) != 0 {
		t.Fatalf("a refusal writes nothing, got %v %v", cs.authored, cs.reconciles)
	}
}

func TestProcessAdvanceRefusesWhenEvidenceIsNotPassing(t *testing.T) {
	for _, state := range []string{"failing", "claimed", "stale"} {
		cs := newCeremonyServer(t, advanceFacts("IN_PROGRESS", state, true, false), true)
		env := wsEnv(t, cs.srv)
		err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
		if err == nil || !strings.Contains(err.Error(), state) {
			t.Fatalf("evidence state %s must be refused by name, got %v", state, err)
		}
		if len(cs.authored)+len(cs.reconciles) != 0 {
			t.Fatalf("a refusal writes nothing (%s), got %v %v", state, cs.authored, cs.reconciles)
		}
	}
}

func TestProcessAdvanceRefusesWithoutFacts(t *testing.T) {
	cs := newCeremonyServer(t, nil, false)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Fatalf("a server without facts must be refused before any write, naming the deploy, got %v", err)
	}
	if len(cs.authored)+len(cs.reconciles) != 0 {
		t.Fatalf("nothing may be written without facts, got %v %v", cs.authored, cs.reconciles)
	}
}

func TestProcessAdvanceIgnoresSiblingFails(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("IN_PROGRESS", "passing", true, false), true)
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{transition("REQ-A-1", "IN_PROGRESS", "IN_REVIEW")},
			"fails": []any{map[string]any{"external_id": "REQ-A-2", "reason": "passing evidence without a recorded RED"}}},
		{"applied": true, "transitions": []any{}, "fails": []any{map[string]any{"external_id": "REQ-A-2", "reason": "passing evidence without a recorded RED"}}},
	}
	env := wsEnv(t, cs.srv)
	var err error
	out := captureOut(t, func() { err = processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"}) })
	if err != nil {
		t.Fatalf("a sibling's FAIL must not fail the advance of the named SR: %v", err)
	}
	if !strings.Contains(out, "REQ-A-2") {
		t.Fatalf("the sibling's FAIL is printed as information: %q", out)
	}
}

func TestProcessAdvanceIsIdempotentWhenAlreadyReviewed(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("IN_REVIEW", "passing", true, true), true)
	env := wsEnv(t, cs.srv)
	var err error
	out := captureOut(t, func() { err = processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"}) })
	if err != nil {
		t.Fatalf("a second call is not an error: %v", err)
	}
	if !strings.Contains(out, "nothing to do") || !strings.Contains(out, "IN_REVIEW") {
		t.Fatalf("a second call reports nothing to do with the reason, got %q", out)
	}
	if len(cs.authored)+len(cs.reconciles) != 0 {
		t.Fatalf("nothing is written when the SR is already reviewed at this hash, got %v %v", cs.authored, cs.reconciles)
	}
}

func TestProcessAdvanceDerivesAFreshTraceIdWhenTaken(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("IN_PROGRESS", "passing", true, false), true)
	// An earlier round's trace at an older hash holds the base id.
	cs.existingGates["TRACE-LOWER-REQ-A-1"] = map[string]any{"external_id": "TRACE-LOWER-REQ-A-1", "state": "stale", "fingerprint": strings.Repeat("9", 64)}
	cs.reconcileResp = []map[string]any{
		{"applied": true, "transitions": []any{transition("REQ-A-1", "IN_PROGRESS", "IN_REVIEW")}, "fails": []any{}},
	}
	env := wsEnv(t, cs.srv)
	if err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"}); err != nil {
		t.Fatalf("advance: %v", err)
	}
	record, _ := cs.authored[0]["record"].(map[string]any)
	if record["external_id"] != "TRACE-LOWER-REQ-A-1-R2" {
		t.Fatalf("a taken trace id yields the next free -R<n>, got %v", record["external_id"])
	}
}

// Review of PR #452, finding 1: the verb must never write the trace and then
// advance nothing. An SR not yet entered, or a TODO SR whose entry gate is not
// applied at the current aggregate, is refused before any write; and if
// reconcile still moves nothing, the verb fails naming the status.
func TestProcessAdvanceRefusesAnUnenteredSR(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("PROPOSED", "passing", true, false), true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || !strings.Contains(err.Error(), "PROPOSED") || !strings.Contains(err.Error(), "process enter") {
		t.Fatalf("an unentered SR is refused naming its status and the entry verb, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessAdvanceRefusesWhenTheEntryGateIsNotAppliedAtTheAggregate(t *testing.T) {
	facts := advanceFacts("TODO", "passing", true, false)
	facts["entry_gate"] = map[string]any{"applied": true, "pinned_aggregate": strings.Repeat("0", 64)}
	cs := newCeremonyServer(t, facts, true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || !strings.Contains(err.Error(), "entry gate") || !strings.Contains(err.Error(), "aggregate") {
		t.Fatalf("a TODO SR whose entry gate is pinned elsewhere is refused before any write, got %v", err)
	}
	// REQ-CROSS-413: the refusal names the recovery verb `process reapply-entry`,
	// where it previously named none (BACKLOG-TOOL-22).
	if !strings.Contains(err.Error(), "process reapply-entry") {
		t.Fatalf("the stranded-entry-gate refusal must name `process reapply-entry`, got %v", err)
	}
	assertNoWrites(t, cs)
}

func TestProcessAdvanceFailsWhenNothingAdvances(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("TODO", "passing", true, false), true)
	// reconcile answers "nothing" every time: the trace is recorded, the SR stays TODO.
	env := wsEnv(t, cs.srv)
	var err error
	captureOut(t, func() { err = processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"}) })
	if err == nil || !strings.Contains(err.Error(), "TODO") {
		t.Fatalf("a trace recorded with no transition applied is a failure naming the status, got %v", err)
	}
}

// Review finding 3 / D14: a live server whose facts gather failed (or that has
// no current selection) serves facts null with facts_state unavailable — not
// a deploy problem, and the refusal must not say so.
func TestProcessAdvanceNamesAnUnavailableGather(t *testing.T) {
	cs := newCeremonyServer(t, nil, false)
	cs.factsState = "unavailable"
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{log: "go test ./cmd"})
	if err == nil || strings.Contains(err.Error(), "deploy") || !strings.Contains(err.Error(), "process next") {
		t.Fatalf("an unavailable gather is named as such, never as a missing deploy, got %v", err)
	}
	assertNoWrites(t, cs)
}

// Review round 2, finding 3: a user requirement is not an SR — reconcile's UR
// path wants no RED and no lower evidence, and its trace is the upper trace.
// The verb refuses a UR by name with the upper-trace remedy instead of
// demanding a RED it will never get.
func TestProcessAdvanceRefusesAURNamingTheUpperPath(t *testing.T) {
	facts := advanceFacts("IN_PROGRESS", "claimed", false, false)
	facts["members"] = []map[string]any{{"external_id": "UR-A", "kind": "ur", "status": "IN_PROGRESS",
		"content_fingerprint": advHash, "evidence_state": "claimed", "red_recorded": false, "lower_trace_pass": false}}
	cs := newCeremonyServer(t, facts, true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "UR-A", advanceOpts{log: "x"})
	if err == nil || !strings.Contains(err.Error(), "user requirement") || !strings.Contains(err.Error(), "upper") {
		t.Fatalf("a UR is refused naming the upper-trace path, got %v", err)
	}
	assertNoWrites(t, cs)
}

// Review round 2, nit 7: the lower trace cites the passing run; without --log
// there is nothing honest to cite.
func TestProcessAdvanceRequiresLog(t *testing.T) {
	cs := newCeremonyServer(t, advanceFacts("IN_PROGRESS", "passing", true, false), true)
	env := wsEnv(t, cs.srv)
	err := processAdvance(env, "REQ-A-1", advanceOpts{})
	if err == nil || !strings.Contains(err.Error(), "--log") {
		t.Fatalf("--log is required, got %v", err)
	}
	assertNoWrites(t, cs)
}

// REQ-CROSS-431 (F-CLI024-R1-07): the refusal for a user requirement spells the
// upper trace without the pin and the transition, which the CLI now reads —
// typing them from memory is how a display prefix reached an immutable trace.
func TestProcessAdvanceRefusesAURWithTheUpperTraceItsPinRead(t *testing.T) {
	facts := advanceFacts("IN_PROGRESS", "passing", true, false)
	facts["members"] = append(facts["members"].([]map[string]any),
		map[string]any{"external_id": "UR-A", "kind": "ur", "status": "IN_PROGRESS", "content_fingerprint": strings.Repeat("e", 64)})
	cs := newCeremonyServer(t, facts, true)
	env := wsEnv(t, cs.srv)

	err := processAdvance(env, "UR-A", advanceOpts{log: "go test ./cmd", piece: "EPIC-A"})
	if err == nil || !strings.Contains(err.Error(), "--purpose upper --scope UR-A --verdict PASS") {
		t.Fatalf("a user requirement must be refused naming its upper trace, got %v", err)
	}
	for _, typed := range []string{"--fingerprint", "--from", "--to"} {
		if strings.Contains(err.Error(), typed) {
			t.Errorf("the refusal must not ask for %s, which the CLI reads, got %v", typed, err)
		}
	}
	if len(cs.authored)+len(cs.reconciles) != 0 {
		t.Fatalf("a refusal writes nothing, got %v %v", cs.authored, cs.reconciles)
	}
}
