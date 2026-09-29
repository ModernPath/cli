package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// REQ-CROSS-442 (EPIC-CLI-TURNS): `author apply --file` records an epic, its
// requirements, relations and membership from one file. The fake store below
// answers /sync/items and /sync/author, rotates a record's fingerprint on
// every write and refuses a stale expected_fingerprint, so a write that does
// not carry the fingerprint the previous write returned is refused here.

type fakeStorePost struct {
	body     map[string]any
	status   int
	returned string // the fingerprint the write returned, "" on a refusal
}

type fakeAuthorStore struct {
	mu      sync.Mutex
	records map[string]map[string]any // id -> payload; payload["_kind"] is the items kind
	gates   map[string]map[string]any
	posts   []fakeStorePost
	calls   int
	fail    map[string]bool // ids whose writes the store refuses
	seq     int
	// REQ-CROSS-443/446/449/450: the delivery-context read per named piece
	// ("" is the bare read); held names the pieces a bare read refuses with.
	contexts map[string]map[string]any
	held     []string
	// REQ-CROSS-446: the held-work read and the open gates.
	heldRows  []any
	openGates []any
	// REQ-CROSS-448: the call-budget test's stateful store. contextFn computes
	// the delivery-context read from the store's state (it wins over contexts);
	// authorHook takes an author post before the default handling (handled
	// false falls through); routes adds the endpoints this fake does not serve.
	contextFn  func(piece string) map[string]any
	authorHook func(body map[string]any) (handled bool, status int, resp map[string]any, fp string)
	routes     func(mux *http.ServeMux)
	// REQ-CROSS-458: the gate list by state, computed from the store's gates
	// (it wins over openGates).
	gatesFn func(state string) []any
	// SR-CLI-027-001: the sync contract's capabilities, served on
	// /api/v1/sync/contract when set (a nil map serves no contract route).
	capabilities map[string]any
}

func newFakeAuthorStore() *fakeAuthorStore {
	return &fakeAuthorStore{records: map[string]map[string]any{}, gates: map[string]map[string]any{}, fail: map[string]bool{},
		contexts: map[string]map[string]any{}}
}

// seedFinding stores a finding on scope (kind:ext) with the given disposition.
func (s *fakeAuthorStore) seedFinding(id, scope, disposition, category, severity string) {
	kind, ext := splitScope(scope)
	s.seed(id, "finding", map[string]any{"scope_kind": kind, "scope_external_id": ext, "disposition": disposition,
		"category": category, "severity": severity})
}

// withFindingResolution makes the fake server advertise finding_resolution,
// so a RESOLVED disposition may carry its kind (SR-CLI-027-001).
func (s *fakeAuthorStore) withFindingResolution() *fakeAuthorStore {
	s.capabilities = map[string]any{"author.finding": []string{"finding_resolution"}}
	return s
}

func (s *fakeAuthorStore) seed(id, kind string, fields map[string]any) {
	payload := map[string]any{"external_id": id, "_kind": kind, "fingerprint": "fp-seed-" + id}
	for k, v := range fields {
		payload[k] = v
	}
	s.records[id] = payload
}

func (s *fakeAuthorStore) nextFP(id string) string {
	s.seq++
	return fmt.Sprintf("fp-%s-%d", id, s.seq)
}

func (s *fakeAuthorStore) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/items", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		items := []any{}
		for _, id := range r.URL.Query()["ids[]"] {
			if rec, ok := s.records[id]; ok {
				payload := map[string]any{}
				for k, v := range rec {
					if k != "_kind" {
						payload[k] = v
					}
				}
				items = append(items, map[string]any{"kind": rec["_kind"], "item": payload, "gates": []any{}})
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"items": items}})
	})
	mux.HandleFunc("/api/v1/sync/gates/", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/")
		gate, ok := s.gates[id]
		if !ok {
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no such gate " + id}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": gate}})
	})
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		piece := r.URL.Query().Get("scope")
		data, ok := s.contexts[piece]
		if s.contextFn != nil {
			data, ok = s.contextFn(piece), true
		}
		if !ok && piece == "" && len(s.held) > 1 {
			w.WriteHeader(409)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": "several"}, "pieces": s.held})
			return
		}
		if !ok {
			data = map[string]any{"derived_reason": "no_current_selection"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	})
	mux.HandleFunc("/api/v1/sync/work-selection/held", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"pieces": s.heldRows}})
	})
	mux.HandleFunc("/api/v1/sync/gates", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		rows := s.openGates
		if s.gatesFn != nil {
			rows = s.gatesFn(r.URL.Query().Get("state"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gates": rows}})
	})
	mux.HandleFunc("/api/v1/sync/findings", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		kind, ext := splitScope(r.URL.Query().Get("scope"))
		rows := []any{}
		for id, rec := range s.records {
			if rec["_kind"] != "finding" || (ext != "" && (rec["scope_kind"] != kind || rec["scope_external_id"] != ext)) {
				continue
			}
			row := map[string]any{"external_id": id, "content_fingerprint": rec["fingerprint"]}
			for k, v := range rec {
				if k != "_kind" && k != "fingerprint" {
					row[k] = v
				}
			}
			rows = append(rows, row)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"findings": rows}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		handled, status, resp, fp := false, 0, map[string]any(nil), ""
		if s.authorHook != nil {
			handled, status, resp, fp = s.authorHook(body)
		}
		if !handled {
			status, resp, fp = s.author(body)
		}
		s.posts = append(s.posts, fakeStorePost{body: body, status: status, returned: fp})
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(resp)
	})
	if s.capabilities != nil {
		mux.HandleFunc("/api/v1/sync/contract", func(w http.ResponseWriter, r *http.Request) {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.calls++
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": 1, "capabilities": s.capabilities}})
		})
	}
	if s.routes != nil {
		s.routes(mux)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func refusalBody(msg string) map[string]any {
	return map[string]any{"error": map[string]any{"message": msg}}
}

func (s *fakeAuthorStore) author(body map[string]any) (int, map[string]any, string) {
	record, _ := body["record"].(map[string]any)
	id := str(record, "external_id")
	kind := str(record, "kind")
	if s.fail[id] {
		return 422, refusalBody("refused " + id), ""
	}
	respKey := kind
	switch str(body, "action") {
	case "evaluate_trace":
		// REQ-CROSS-450: a trace is recorded once and is immutable.
		if _, exists := s.gates[id]; exists {
			return 409, refusalBody(id + " already exists"), ""
		}
		s.gates[id] = record
		return 200, map[string]any{"data": map[string]any{"gate": map[string]any{"external_id": id, "state": record["verdict"],
			"fingerprint": record["fingerprint"]}}}, ""
	case "create":
		if _, exists := s.records[id]; exists {
			return 409, refusalBody(id + " already exists"), ""
		}
		itemKind := kind
		if kind == "requirement" {
			itemKind = "system"
			if record["requirement_kind"] == "user" {
				itemKind = "user"
			}
		}
		payload := map[string]any{"external_id": id, "_kind": itemKind, "work_status": "PROPOSED"}
		for k, v := range record {
			if k != "kind" && k != "external_id" && k != "requirement_kind" && k != "authoring_context_id" && k != "current_release" {
				payload[k] = v
			}
		}
		s.records[id] = payload
	case "patch":
		// The single-record atomic patch working-set push sends: fields,
		// criteria, relation ops and member ops under one fingerprint check.
		payload, ok := s.records[id]
		if !ok {
			return 422, refusalBody("no such record " + id + " — patch never creates"), ""
		}
		if str(record, "expected_fingerprint") != str(payload, "fingerprint") {
			return 409, refusalBody("stale fingerprint for " + id), ""
		}
		for k, v := range record {
			switch k {
			case "kind", "external_id", "expected_fingerprint", "authoring_context_id":
			case "relations":
				for _, raw := range anyList(v) {
					op, _ := raw.(map[string]any)
					if str(op, "mode") == "declare" {
						payload["parent_external_ids"] = appendIDs(payload["parent_external_ids"], op["parents"])
					}
				}
			case "members":
				for _, raw := range anyList(v) {
					op, _ := raw.(map[string]any)
					if str(op, "mode") != "declare" {
						continue
					}
					for _, m := range stringSlice(op["member_external_ids"]) {
						key := "requirement_external_ids"
						if rec, ok := s.records[m]; ok && rec["_kind"] == "user" {
							key = "user_requirement_external_ids"
						}
						payload[key] = appendIDs(payload[key], []any{m})
					}
				}
			default:
				payload[k] = v
			}
		}
	case "update", "relate", "advance":
		payload, ok := s.records[id]
		if !ok {
			return 404, refusalBody("no record " + id), ""
		}
		if str(body, "action") == "advance" {
			if body["expected"] != payload["work_status"] && body["expected"] != payload["process_status"] {
				return 409, refusalBody("stale expected"), ""
			}
			if payload["_kind"] == "epic" {
				payload["process_status"] = body["to"]
			} else {
				payload["work_status"] = body["to"]
			}
			break
		}
		if str(record, "expected_fingerprint") != str(payload, "fingerprint") {
			return 409, refusalBody("stale fingerprint for " + id), ""
		}
		for k, v := range record {
			switch k {
			case "kind", "external_id", "expected_fingerprint", "mode":
			case "parent_external_ids":
				payload[k] = appendIDs(payload[k], v)
			case "member_external_ids":
				for _, m := range stringSlice(v) {
					key := "requirement_external_ids"
					if rec, ok := s.records[m]; ok && rec["_kind"] == "user" {
						key = "user_requirement_external_ids"
					}
					payload[key] = appendIDs(payload[key], []any{m})
				}
			default:
				payload[k] = v
			}
		}
	default:
		return 400, refusalBody("unknown action"), ""
	}
	payload := s.records[id]
	fp := s.nextFP(id)
	payload["fingerprint"] = fp
	row := map[string]any{"external_id": id, "fingerprint": fp, "work_status": payload["work_status"], "process_status": payload["process_status"]}
	return 200, map[string]any{"data": map[string]any{respKey: row}}, fp
}

func appendIDs(existing, add any) []any {
	out := []any{}
	for _, v := range stringSlice(existing) {
		out = append(out, v)
	}
	for _, v := range stringSlice(add) {
		out = append(out, v)
	}
	return out
}

// postsFor returns the author posts naming id with the given action.
func (s *fakeAuthorStore) postsFor(id, action string) []fakeStorePost {
	var out []fakeStorePost
	for _, p := range s.posts {
		record, _ := p.body["record"].(map[string]any)
		if str(record, "external_id") == id && str(p.body, "action") == action {
			out = append(out, p)
		}
	}
	return out
}

func writePlanFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const applyPlanYAML = `epic:
  id: EPIC-T
  title: Epic T
  description: the new epic prose
requirements:
  - id: UR-T-1
    kind: ur
    context: CROSS
    title: A user requirement
    description: the user can do it
  - id: REQ-T-1
    kind: sr
    context: CROSS
    title: An existing SR
    description: the new statement
    parents: [UR-T-1]
  - id: REQ-T-2
    kind: sr
    context: CROSS
    title: A new SR
    description: a statement
    rationale: why
    boundary: CLI only
    verification_method: Go tests
    criteria:
      - external_id: AC-1
        statement: it works
    parents: [UR-T-1]
members: [UR-T-1, REQ-T-1, REQ-T-2]
`

func seedApplyStore(s *fakeAuthorStore) {
	s.seed("EPIC-T", "epic", map[string]any{"title": "Epic T", "description": "old prose", "process_status": "PLANNED",
		"requirement_external_ids": []any{}, "user_requirement_external_ids": []any{}})
	s.seed("REQ-T-1", "system", map[string]any{"title": "An existing SR", "context": "CROSS", "description": "old statement",
		"work_status": "PROPOSED", "parent_external_ids": []any{}})
}

// pinned is the plan with the fingerprints the seeded records were read at,
// as a plan written from a pull carries them.
func pinned(plan string) string {
	plan = strings.Replace(plan, "  description: the new epic prose\n", "  description: the new epic prose\n  expected_fingerprint: fp-seed-EPIC-T\n", 1)
	return strings.Replace(plan, "    description: the new statement\n", "    description: the new statement\n    expected_fingerprint: fp-seed-REQ-T-1\n", 1)
}

// patchRecord is the record of the only patch posted for id, or nil.
func patchRecord(t *testing.T, s *fakeAuthorStore, id string) map[string]any {
	t.Helper()
	patches := s.postsFor(id, "patch")
	if len(patches) != 1 {
		t.Errorf("%s wants exactly one patch, got %d: %v", id, len(patches), s.posts)
		return nil
	}
	rec, _ := patches[0].body["record"].(map[string]any)
	return rec
}

// relationTargets lists the targets a patch declares, from its relation or
// member ops.
func declaredTargets(rec map[string]any, key, idsKey string) []string {
	var out []string
	for _, raw := range anyList(rec[key]) {
		op, _ := raw.(map[string]any)
		if str(op, "mode") == "declare" {
			out = append(out, stringSlice(op[idsKey])...)
		}
	}
	sort.Strings(out)
	return out
}

// REQ-CROSS-442 (revised, PR #694 review): apply is an input format for
// working-set push's write engine. Each record is one atomic patch — fields,
// criteria, relations and membership together — carrying the authoring
// context; a record the store does not know is created first, then patched.
func TestREQCROSS442ApplyWritesOnePatchPerRecordThroughPush(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	plan := writePlanFile(t, "plan.yaml", pinned(applyPlanYAML))

	out, err := runRoot(t, "author", "apply", "--file", plan)
	if err != nil {
		t.Fatalf("author apply: %v\n%s", err, out)
	}
	for _, action := range []string{"update", "relate"} {
		for _, id := range []string{"EPIC-T", "UR-T-1", "REQ-T-1", "REQ-T-2"} {
			if n := len(store.postsFor(id, action)); n != 0 {
				t.Errorf("%s: %d %s post(s); every change rides the record's one patch", id, n, action)
			}
		}
	}
	for _, p := range store.posts {
		if p.status != 200 {
			t.Errorf("the store refused a write: %v -> %d", p.body, p.status)
		}
		rec, _ := p.body["record"].(map[string]any)
		if str(rec, "authoring_context_id") == "" {
			t.Errorf("every create and patch carries the authoring context, as push does; posted %v", p.body)
		}
	}
	// New records: created once, then patched once under the create's fingerprint.
	for _, id := range []string{"UR-T-1", "REQ-T-2"} {
		if n := len(store.postsFor(id, "create")); n != 1 {
			t.Fatalf("%s must be created once, got %d: %v", id, n, store.posts)
		}
	}
	if rec, _ := store.postsFor("UR-T-1", "create")[0].body["record"].(map[string]any); rec["requirement_kind"] != "user" {
		t.Errorf("a kind ur record is created as a user requirement, posted %v", rec)
	}
	if rec := patchRecord(t, store, "REQ-T-2"); rec != nil {
		if rec["expected_fingerprint"] != store.postsFor("REQ-T-2", "create")[0].returned {
			t.Errorf("the new record's patch guards on the fingerprint its create returned, posted %v", rec)
		}
		if rec["rationale"] != "why" || rec["verification_method"] != "Go tests" || len(anyList(rec["criteria"])) != 1 {
			t.Errorf("the new record's prose and criteria ride its patch, posted %v", rec)
		}
		if got := declaredTargets(rec, "relations", "parents"); strings.Join(got, ",") != "UR-T-1" {
			t.Errorf("the new SR's parent is declared in its patch, got %v", got)
		}
	}
	// Existing records: one patch each, under the pinned fingerprint.
	if len(store.postsFor("EPIC-T", "create")) != 0 || len(store.postsFor("REQ-T-1", "create")) != 0 {
		t.Errorf("an existing record is never created again")
	}
	if rec := patchRecord(t, store, "REQ-T-1"); rec != nil {
		if rec["expected_fingerprint"] != "fp-seed-REQ-T-1" || rec["description"] != "the new statement" {
			t.Errorf("the patch guards on the pinned fingerprint and sends the changed statement, posted %v", rec)
		}
		if _, sent := rec["title"]; sent {
			t.Errorf("an unchanged title is not sent, posted %v", rec)
		}
		if got := declaredTargets(rec, "relations", "parents"); strings.Join(got, ",") != "UR-T-1" {
			t.Errorf("the relation rides the same patch as the statement, got %v", got)
		}
	}
	if rec := patchRecord(t, store, "EPIC-T"); rec != nil {
		if rec["expected_fingerprint"] != "fp-seed-EPIC-T" || rec["description"] != "the new epic prose" {
			t.Errorf("the epic patch guards on the pinned fingerprint and sends its prose, posted %v", rec)
		}
		if got := declaredTargets(rec, "members", "member_external_ids"); strings.Join(got, ",") != "REQ-T-1,REQ-T-2,UR-T-1" {
			t.Errorf("the membership rides the epic's patch, got %v", got)
		}
	}
	// Every create comes before the first patch, so a relation or member
	// always names a record that exists.
	lastCreate, firstPatch := -1, len(store.posts)
	for i, p := range store.posts {
		switch str(p.body, "action") {
		case "create":
			lastCreate = i
		case "patch":
			firstPatch = min(firstPatch, i)
		}
	}
	if lastCreate > firstPatch {
		t.Errorf("a patch ran before every create: %v", store.posts)
	}
	if !strings.Contains(out, "created") || !strings.Contains(out, str(store.records["REQ-T-1"], "fingerprint")) {
		t.Errorf("each record's result and new fingerprint is printed:\n%s", out)
	}

	// A second identical run posts nothing and says so.
	before := len(store.posts)
	out, err = runRoot(t, "author", "apply", "--file", plan)
	if err != nil {
		t.Fatalf("re-run: %v\n%s", err, out)
	}
	if len(store.posts) != before {
		t.Errorf("a re-run of the same file must post nothing, posted %v", store.posts[before:])
	}
	if !strings.Contains(out, "unchanged") {
		t.Errorf("a re-run says each record is unchanged:\n%s", out)
	}
}

const applyPlanJSON = `{"epic":{"id":"EPIC-T","title":"Epic T","description":"the new epic prose","expected_fingerprint":"fp-seed-EPIC-T"},
"requirements":[
 {"id":"UR-T-1","kind":"ur","context":"CROSS","title":"A user requirement"},
 {"id":"REQ-T-2","kind":"sr","context":"CROSS","title":"A new SR","parents":["UR-T-1"]},
 {"id":"REQ-T-1","kind":"sr","context":"CROSS","title":"An existing SR","description":"the new statement","parents":["UR-T-1"],"expected_fingerprint":"fp-seed-REQ-T-1"}],
"members":["UR-T-1","REQ-T-1","REQ-T-2"]}`

// Push's plan pass: the whole plan validates before the first write, so an
// invalid record refuses the plan and nothing is written.
func TestREQCROSS442ApplyInvalidRecordRefusesTheWholePlanBeforeAnyWrite(t *testing.T) {
	for name, plan := range map[string]string{
		"a new SR with no context": strings.Replace(applyPlanJSON,
			`{"id":"REQ-T-2","kind":"sr","context":"CROSS",`, `{"id":"REQ-T-2","kind":"sr",`, 1),
		"a lane class on a UR": strings.Replace(applyPlanJSON,
			`"title":"A user requirement"`, `"title":"A user requirement","lane_class":"wording"`, 1),
		"a criterion with no external_id": strings.Replace(applyPlanJSON,
			`"title":"A new SR",`, `"title":"A new SR","criteria":[{"statement":"it works"}],`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			store := newFakeAuthorStore()
			seedApplyStore(store)
			cobraWorkspace(t, store.serve(t))
			out, err := runRoot(t, "author", "apply", "--file", writePlanFile(t, "plan.json", plan))
			if err == nil {
				t.Fatalf("an invalid record must refuse the plan\n%s", out)
			}
			if len(store.posts) != 0 {
				t.Errorf("the refusal comes before the first write, posted %v", store.posts)
			}
			if !strings.Contains(err.Error()+out, "REQ-T-2") && !strings.Contains(err.Error()+out, "UR-T-1") {
				t.Errorf("the refusal names the record: %v\n%s", err, out)
			}
		})
	}
}

func TestREQCROSS442AuthorApplyDryRunWritesNothing(t *testing.T) {
	store := newFakeAuthorStore()
	seedApplyStore(store)
	cobraWorkspace(t, store.serve(t))
	plan := writePlanFile(t, "plan.json", applyPlanJSON)

	out, err := runRoot(t, "author", "apply", "--file", plan, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	if len(store.posts) != 0 {
		t.Errorf("--dry-run must post nothing, posted %v", store.posts)
	}
	if !strings.Contains(out, "UR-T-1") || !strings.Contains(out, "create") {
		t.Errorf("--dry-run prints the plan:\n%s", out)
	}
}
