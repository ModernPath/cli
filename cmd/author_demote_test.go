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
		if ds.gate == nil || str(ds.gate, "external_id") != strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/") {
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

// REQ-CROSS-462 (EPIC-CLI-DELTA): `author demote --ids A,B,C` (or --file)
// opens one demotion gate over every item of one starting state, at the next
// free id of the DEMOTE-<first id> series, and `--apply` advances every item
// of the answered gate — user requirements first, an item with its own applied
// entry skipped, stopping at a refusal with what was applied and what remains,
// passing over an item another gate's follow already moved.

type bdItem struct {
	status  string
	ur      bool
	parents []string
}

// batchDemoteServer is a small store: the requirement list, one epic, the gate
// reads (by id, 404 when absent, and the list), and author create/advance. An
// advance checks the expected state, appends the item's own entry to its gate
// and closes the gate once every exact_scope id has an entry, as the server does.
type batchDemoteServer struct {
	srv    *httptest.Server
	items  map[string]*bdItem
	order  []string
	gates  map[string]map[string]any
	posts  []map[string]any
	refuse map[string]string
}

func newBatchDemoteServer(t *testing.T) *batchDemoteServer {
	t.Helper()
	bd := &batchDemoteServer{items: map[string]*bdItem{}, gates: map[string]map[string]any{}, refuse: map[string]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		srs, urs := []map[string]any{}, []map[string]any{}
		for _, id := range bd.order {
			it := bd.items[id]
			row := map[string]any{"external_id": id, "work_status": it.status, "fingerprint": "fp-" + id, "parent_external_ids": it.parents}
			if it.ur {
				urs = append(urs, row)
			} else {
				srs = append(srs, row)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"requirements": srs, "user_requirements": urs}})
	})
	mux.HandleFunc("/api/v1/sync/epics", func(w http.ResponseWriter, r *http.Request) {
		var srs, urs []string
		for _, id := range bd.order {
			if bd.items[id].ur {
				urs = append(urs, id)
			} else {
				srs = append(srs, id)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"epics": []map[string]any{{
			"external_id": "EPIC-BD", "code": "EPIC-BD", "process_status": "DONE",
			"requirement_external_ids": srs, "user_requirement_external_ids": urs,
		}}}})
	})
	mux.HandleFunc("/api/v1/sync/gates", func(w http.ResponseWriter, r *http.Request) {
		all := []map[string]any{}
		for _, g := range bd.gates {
			all = append(all, g)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gates": all}})
	})
	mux.HandleFunc("/api/v1/sync/gates/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/")
		g, ok := bd.gates[id]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no such gate: " + id}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": g}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bd.posts = append(bd.posts, body)
		record, _ := body["record"].(map[string]any)
		id := str(record, "external_id")
		switch body["action"] {
		case "create":
			g := map[string]any{}
			for k, v := range record {
				g[k] = v
			}
			g["state"], g["fingerprint"], g["applied_transitions"] = "open", "gfp-"+id, []any{}
			bd.gates[id] = g
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": g}})
		case "advance":
			if msg := bd.refuse[id]; msg != "" {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg}})
				return
			}
			it := bd.items[id]
			if it == nil || it.status != body["expected"] {
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": id + " is not in the expected state"}})
				return
			}
			from := it.status
			it.status = str(body, "to")
			g := bd.gates[str(body, "gate_ref")]
			entries, _ := g["applied_transitions"].([]any)
			entries = append(entries, map[string]any{"external_id": id, "from": from, "to": it.status})
			g["applied_transitions"] = entries
			covered := map[string]bool{}
			for _, e := range entries {
				covered[str(e.(map[string]any), "external_id")] = true
			}
			all := true
			for _, s := range stringSlice(g["exact_scope"]) {
				all = all && covered[s]
			}
			if all {
				g["state"], g["applied_state"] = "closed", "applied"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"requirement":      map[string]any{"external_id": id, "work_status": it.status},
				"transition_basis": "gate_transition_binding",
			}})
		}
	})
	bd.srv = httptest.NewServer(mux)
	t.Cleanup(bd.srv.Close)
	return bd
}

func (bd *batchDemoteServer) add(id, status string, ur bool, parents ...string) {
	bd.items[id] = &bdItem{status: status, ur: ur, parents: parents}
	bd.order = append(bd.order, id)
}

func (bd *batchDemoteServer) answered(id, transition string, scope ...string) map[string]any {
	// The wire shape: a decoded JSON list is []any, which stringSlice reads.
	served := []any{}
	for _, s := range scope {
		served = append(served, s)
	}
	g := map[string]any{
		"external_id": id, "state": "answered", "purpose": "demotion", "transition": transition,
		"fingerprint": "gfp-" + id, "chosen_option_keys": []any{"approve"}, "exact_scope": served, "applied_transitions": []any{},
	}
	bd.gates[id] = g
	return g
}

func (bd *batchDemoteServer) created() []map[string]any {
	out := []map[string]any{}
	for _, p := range bd.posts {
		if p["action"] == "create" {
			out = append(out, p["record"].(map[string]any))
		}
	}
	return out
}

func (bd *batchDemoteServer) advanced() []string {
	out := []string{}
	for _, p := range bd.posts {
		if p["action"] == "advance" {
			out = append(out, str(p["record"].(map[string]any), "external_id")+"@"+str(p, "gate_ref"))
		}
	}
	return out
}

const bdReason = "USER:2026-09-28:the decision is reversed"

func runDemote(t *testing.T, args ...string) (string, error) {
	t.Helper()
	said := captureCLIOutput(t)
	out, err := runRoot(t, append([]string{"author", "demote"}, args...)...)
	return out + said(), err
}

func TestREQCROSS462BatchOpensOneGateOverEveryItem(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("UR-BD-1", "IN_REVIEW", true)
	bd.add("SR-BD-1", "IN_REVIEW", false, "UR-BD-1")
	bd.add("SR-BD-2", "IN_REVIEW", false, "UR-BD-9")
	bd.add("UR-BD-9", "DONE", true)
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--ids", "SR-BD-1,SR-BD-2,UR-BD-1", "--to", "PROPOSED", "--basis", "reversed-decision", "--reason", bdReason)
	if err != nil {
		t.Fatalf("batch demote: %v\n%s", err, out)
	}
	created := bd.created()
	if len(created) != 1 {
		t.Fatalf("one gate is opened for the batch, got %d: %v", len(created), created)
	}
	g := created[0]
	if g["external_id"] != "DEMOTE-SR-BD-1" || g["transition"] != "IN_REVIEW->PROPOSED" || g["purpose"] != "demotion" {
		t.Fatalf("the gate is DEMOTE-<first id> with the shared transition, got %v", g)
	}
	if scope := strings.Join(stringSlice(g["exact_scope"]), ","); scope != "SR-BD-1,SR-BD-2,UR-BD-1" {
		t.Fatalf("exact_scope lists every item, got %s", scope)
	}
	title := str(g, "title")
	for _, id := range []string{"SR-BD-1", "SR-BD-2", "UR-BD-1"} {
		if !strings.Contains(title, id) {
			t.Errorf("the title names %s: %q", id, title)
		}
	}
	brief, _ := g["brief"].(map[string]any)
	what := str(brief, "what")
	for _, want := range []string{"SR-BD-1", "SR-BD-2", "UR-BD-1", "UR-BD-9", "EPIC-BD"} {
		if !strings.Contains(what, want) {
			t.Errorf("the brief lists every item and what follows (%s missing): %q", want, what)
		}
	}
	if !strings.Contains(out, "--gate-id DEMOTE-SR-BD-1 --apply") {
		t.Errorf("a batch's apply hint always names --gate-id:\n%s", out)
	}
}

func TestREQCROSS462TitleStaysWithinTheLimit(t *testing.T) {
	bd := newBatchDemoteServer(t)
	ids := []string{}
	for i := 0; i < 40; i++ {
		id := "REQ-CROSS-BATCH-LONG-" + strings.Repeat("X", 5) + string(rune('A'+i%26)) + string(rune('A'+i/26))
		bd.add(id, "DONE", false)
		ids = append(ids, id)
	}
	cobraWorkspace(t, bd.srv)
	out, err := runDemote(t, "--ids", strings.Join(ids, ","), "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err != nil {
		t.Fatalf("batch demote: %v\n%s", err, out)
	}
	title := str(bd.created()[0], "title")
	if n := len([]rune(title)); n > 255 || !strings.Contains(title, ids[0]) || !strings.Contains(title, "more") {
		t.Fatalf("the title names the first ids and \"and N more\" within 255 characters (%d): %q", n, title)
	}
}

func TestREQCROSS462FileListsOneIDPerLine(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.add("SR-BD-2", "DONE", false)
	cobraWorkspace(t, bd.srv)
	file := writePlanFile(t, "ids.txt", "SR-BD-1\n\nSR-BD-2\n")
	out, err := runDemote(t, "--file", file, "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err != nil {
		t.Fatalf("batch demote --file: %v\n%s", err, out)
	}
	if created := bd.created(); len(created) != 1 || strings.Join(stringSlice(created[0]["exact_scope"]), ",") != "SR-BD-1,SR-BD-2" {
		t.Fatalf("--file opens one gate over its ids, got %v", created)
	}
}

func TestREQCROSS462MixedBatchIsRefusedBeforeAnyWrite(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.add("UR-BD-1", "IN_REVIEW", true)
	bd.add("SR-BD-2", "IN_REVIEW", false, "UR-BD-1")
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--ids", "SR-BD-1,UR-BD-1,SR-BD-2", "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err == nil {
		t.Fatalf("a batch mixing IN_REVIEW and DONE is refused\n%s", out)
	}
	msg := err.Error()
	urGroup, doneGroup := strings.Index(msg, "UR-BD-1"), strings.Index(msg, "SR-BD-1")
	if urGroup < 0 || doneGroup < 0 || !strings.Contains(msg, "IN_REVIEW") || !strings.Contains(msg, "DONE") || !strings.Contains(msg, "SR-BD-2") {
		t.Fatalf("the refusal names both groups with their states: %v", err)
	}
	if urGroup > doneGroup {
		t.Fatalf("the group holding the user requirement is named first: %v", err)
	}
	if len(bd.posts) != 0 {
		t.Fatalf("nothing is written, posted %v", bd.posts)
	}
}

func TestREQCROSS462ClosedDefaultIDOpensTheNextInTheSeries(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.gates["DEMOTE-SR-BD-1"] = map[string]any{"external_id": "DEMOTE-SR-BD-1", "state": "closed", "applied_state": "applied", "purpose": "demotion"}
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "SR-BD-1", "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err != nil {
		t.Fatalf("demote with a closed DEMOTE-X: %v\n%s", err, out)
	}
	if created := bd.created(); len(created) != 1 || created[0]["external_id"] != "DEMOTE-SR-BD-1-R2" {
		t.Fatalf("the gate is the next free id DEMOTE-SR-BD-1-R2, got %v", created)
	}
	if !strings.Contains(out, "--gate-id DEMOTE-SR-BD-1-R2") {
		t.Fatalf("the apply hint names the non-default id:\n%s", out)
	}
}

func TestREQCROSS462AnOpenGateInTheSeriesRefusesTheBatch(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.add("SR-BD-2", "DONE", false)
	bd.gates["DEMOTE-SR-BD-1"] = map[string]any{"external_id": "DEMOTE-SR-BD-1", "state": "open", "purpose": "demotion"}
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--ids", "SR-BD-1,SR-BD-2", "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err == nil || !strings.Contains(err.Error(), "DEMOTE-SR-BD-1") || !strings.Contains(err.Error(), "open") {
		t.Fatalf("an open gate in the series refuses the batch naming it, got %v\n%s", err, out)
	}
	if len(bd.created()) != 0 {
		t.Fatalf("nothing is opened, got %v", bd.created())
	}
}

func TestREQCROSS462ApplyWithoutGateIDResolvesTheNewestInTheSeries(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.gates["DEMOTE-SR-BD-1"] = map[string]any{"external_id": "DEMOTE-SR-BD-1", "state": "closed", "applied_state": "applied", "purpose": "demotion"}
	bd.answered("DEMOTE-SR-BD-1-R2", "DONE->IN_PROGRESS", "SR-BD-1")
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "SR-BD-1", "--apply")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "SR-BD-1@DEMOTE-SR-BD-1-R2" {
		t.Fatalf("the id form applies the newest gate in the series, got %s", got)
	}
}

func TestREQCROSS462IDFormAppliesEveryItemOfTheGate(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.add("SR-BD-2", "DONE", false)
	bd.answered("DEMOTE-SR-BD-1", "DONE->IN_PROGRESS", "SR-BD-1", "SR-BD-2")
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "SR-BD-1", "--apply")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "SR-BD-1@DEMOTE-SR-BD-1 SR-BD-2@DEMOTE-SR-BD-1" {
		t.Fatalf("the id form applies every item of a multi-item gate, got %s", got)
	}
	if bd.gates["DEMOTE-SR-BD-1"]["state"] != "closed" {
		t.Fatalf("the gate closes once every item is applied")
	}
}

func TestREQCROSS462ApplyAdvancesTheURFirstAndSkipsAnAppliedItem(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "IN_REVIEW", false, "UR-BD-1")
	bd.add("SR-BD-2", "PROPOSED", false, "UR-BD-1")
	bd.add("UR-BD-1", "IN_REVIEW", true)
	g := bd.answered("DEMOTE-SR-BD-1", "IN_REVIEW->PROPOSED", "SR-BD-1", "SR-BD-2", "UR-BD-1")
	g["applied_transitions"] = []any{map[string]any{"external_id": "SR-BD-2", "from": "IN_REVIEW", "to": "PROPOSED"}}
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--gate-id", "DEMOTE-SR-BD-1", "--apply")
	if err != nil {
		t.Fatalf("apply: %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "UR-BD-1@DEMOTE-SR-BD-1 SR-BD-1@DEMOTE-SR-BD-1" {
		t.Fatalf("the user requirement goes first and the applied SR-BD-2 is skipped, got %s", got)
	}
	if g["state"] != "closed" {
		t.Fatalf("the gate closes, state %v", g["state"])
	}
}

func TestREQCROSS462ApplyStopsAtARefusalAndAReRunResumes(t *testing.T) {
	bd := newBatchDemoteServer(t)
	for _, id := range []string{"SR-BD-1", "SR-BD-2", "SR-BD-3"} {
		bd.add(id, "DONE", false)
	}
	g := bd.answered("DEMOTE-SR-BD-1", "DONE->IN_PROGRESS", "SR-BD-1", "SR-BD-2", "SR-BD-3")
	bd.refuse["SR-BD-2"] = "a transient refusal"
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--gate-id", "DEMOTE-SR-BD-1", "--apply")
	if err == nil {
		t.Fatalf("a refusal exits non-zero\n%s", out)
	}
	report := out + err.Error()
	if !strings.Contains(err.Error(), "SR-BD-1") || !strings.Contains(err.Error(), "SR-BD-2, SR-BD-3") || !strings.Contains(report, "a transient refusal") {
		t.Fatalf("the refusal lists what was applied (SR-BD-1) and what remains (SR-BD-2, SR-BD-3): %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "SR-BD-1@DEMOTE-SR-BD-1 SR-BD-2@DEMOTE-SR-BD-1" {
		t.Fatalf("the run stops at the refused item, got %s", got)
	}

	delete(bd.refuse, "SR-BD-2")
	bd.posts = nil
	out, err = runDemote(t, "--gate-id", "DEMOTE-SR-BD-1", "--apply")
	if err != nil {
		t.Fatalf("the re-run resumes: %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "SR-BD-2@DEMOTE-SR-BD-1 SR-BD-3@DEMOTE-SR-BD-1" {
		t.Fatalf("the re-run applies only what remains, got %s", got)
	}
	if g["state"] != "closed" {
		t.Fatalf("the gate closes on the re-run, state %v", g["state"])
	}
}

func TestREQCROSS462BatchAppliedThroughItsPrintedHintClosesTheGate(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("SR-BD-1", "DONE", false)
	bd.add("SR-BD-2", "DONE", false)
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--ids", "SR-BD-1,SR-BD-2", "--to", "IN_PROGRESS", "--basis", "defect", "--reason", bdReason)
	if err != nil {
		t.Fatalf("batch demote: %v\n%s", err, out)
	}
	i := strings.Index(out, "author demote --gate-id ")
	if i < 0 {
		t.Fatalf("the hint names the gate by --gate-id:\n%s", out)
	}
	hint := strings.Fields(strings.SplitN(out[i:], "`", 2)[0])
	g := bd.gates["DEMOTE-SR-BD-1"]
	g["state"], g["chosen_option_keys"] = "answered", []any{"approve"}

	out, err = runDemote(t, hint[2:]...)
	if err != nil {
		t.Fatalf("the printed hint %v applies: %v\n%s", hint, err, out)
	}
	if g["state"] != "closed" || len(bd.advanced()) != 2 {
		t.Fatalf("the hint applies every item and the gate closes: state %v, advanced %v", g["state"], bd.advanced())
	}
}

func TestREQCROSS462ApplyPassesOverAnItemAnotherGatesFollowMoved(t *testing.T) {
	bd := newBatchDemoteServer(t)
	bd.add("UR-BD-1", "IN_PROGRESS", true)
	bd.add("SR-BD-1", "DONE", false, "UR-BD-1")
	bd.add("SR-BD-2", "DONE", false, "UR-BD-1")
	bd.gates["DEMOTE-OTHER"] = map[string]any{"external_id": "DEMOTE-OTHER", "state": "closed", "purpose": "demotion",
		"applied_transitions": []any{map[string]any{"external_id": "UR-BD-1", "from": "DONE", "to": "IN_PROGRESS", "basis": "demotion_follow"}}}
	g := bd.answered("DEMOTE-UR-BD-1", "DONE->IN_PROGRESS", "UR-BD-1", "SR-BD-1", "SR-BD-2")
	cobraWorkspace(t, bd.srv)

	out, err := runDemote(t, "--gate-id", "DEMOTE-UR-BD-1", "--apply")
	if err != nil {
		t.Fatalf("an item moved by another gate's follow is passed over, not a failure: %v\n%s", err, out)
	}
	if got := strings.Join(bd.advanced(), " "); got != "SR-BD-1@DEMOTE-UR-BD-1 SR-BD-2@DEMOTE-UR-BD-1" {
		t.Fatalf("the rest apply, got %s", got)
	}
	if !strings.Contains(out, "UR-BD-1") || !strings.Contains(out, "DEMOTE-OTHER") {
		t.Fatalf("the passed-over item is reported with the gate that moved it:\n%s", out)
	}
	if g["state"] == "closed" || !strings.Contains(out, "author gate-withdraw DEMOTE-UR-BD-1") {
		t.Fatalf("a gate that cannot close names author gate-withdraw:\n%s", out)
	}
}
