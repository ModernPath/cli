package cmd

// EPIC-CLI-002 (REQ-CROSS-215..218) — RED first. Each test names the clause it
// pins from epics/EPIC-CLI-002-working-set-sync/specs/requirements.md.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var wsNow = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

// wsServer serves the four read endpoints the commands consume, from mutable
// fixture state so tests can change the server side between calls.
type wsFixture struct {
	gates            []any // the open set: served on the default listing
	answeredGates    []any // served only under state=all|answered (REQ-CROSS-219)
	epics            []any
	requirements     []any
	userRequirements []any // served under data.user_requirements (REQ-CROSS-048/308)
	failGates        bool
	emptyEnvelope    bool   // serve 200 {"data":{}} — the envelope-skew case
	lastReqQuery     string // captured ?… of the last /api/v1/sync/requirements request

	// REQ-CROSS-109 — gate history reads and the by-id show route. Before this SR
	// the fixture served only state=""/open/answered/all and had no show route.
	dismissedGates  []any          // served under state=dismissed|all
	supersededGates []any          // served under state=superseded|all
	closedGates     []any          // applied answers: in `all`, never in `answered`
	gate            map[string]any // served by GET /sync/gates/{id} when its external_id matches
	lastGatesQuery  string         // captured ?… of the last list or show request
	gatesStatus     int            // force this status on the list AND show routes (0 = normal)
	gatesBody       map[string]any // the exact body to serve with gatesStatus

	// REQ-CROSS-220
	workSelection  map[string]any // served on GET /work-selection
	lastSelectPost map[string]any // captured POST body
	selectStatus   int            // POST response status (default 200)
	selectError    string         // POST error body when selectStatus >= 400

	// REQ-CROSS-345: the caller-scoped read. lastSelectGet captures the GET
	// query (so ?scope= is observable); selectReadStatus/Body serve the
	// "you hold several current pieces" 422 the client must surface.
	lastSelectGet    string         // captured ?… of the last GET work-selection
	selectReadStatus int            // GET response status (0 = normal 200)
	selectReadBody   map[string]any // the exact body to serve with selectReadStatus

	// REQ-CROSS-312/313: packet sections served on GET /sync/packet-sections
	packetSections []any
	// A non-200 status to serve from /sync/packet-sections (0 = normal 200). 404
	// is an absent endpoint (honest absence); any other non-200 is a real error.
	packetSectionsStatus int

	// REQ-CROSS-314 (push): capture author writes and drive per-record 409s.
	// authorConflict is keyed by a requirement/epic external_id or, for a packet
	// section, "<scope_kind>:<scope_external_id>:<section_key>".
	authorPosts    []map[string]any
	authorConflict map[string]bool

	// REQ-CROSS-274 feed, consumed by the your-move brief (REQ-CROSS-276/277)
	feed          map[string]any // served as {"data": feed}
	failFeed      bool           // serve 500 {"error":"boom"}
	feedNoItems   bool           // serve 200 {"data":{...}} without "items" (envelope skew)
	feedDelay     time.Duration  // sleep before answering /feed (the hook deadline test)
	lastFeedQuery string         // captured ?…  of the last /api/v1/feed request

	// REQ-CROSS-317: the delivery-context read the SessionStart hook appends its
	// scoped derived/declared-phase line from (served as {"data": deliveryContext}).
	deliveryContext map[string]any

	// REQ-CROSS-348: served as the x-modernpath-store-revision response header
	// on every route when non-empty.
	storeRevision string

	// REQ-CROSS-393: backlog, gap and tooling records served on GET /sync/backlog.
	backlog []any

	// REQ-CROSS-415 (EPIC-CLI-021): the caller-scoped held-work read the
	// SessionStart brief renders its continuing mode from, served as
	// {"data": {"pieces": held}} on GET /sync/work-selection/held.
	held []any

	// REQ-CROSS-417 (EPIC-CLI-021): the contract advertisement. Zero keeps the
	// route unserved (404, the pre-advertisement server); contractDelay holds
	// the answer so a status verb's own timeout can be observed.
	contractVersion int
	contractDelay   time.Duration

	// SCN-BRIEF-005: every request the fixture answered, "METHOD path" — the
	// reads-only assertion is that none of them is a write.
	requests []string
}

func wsServe(t *testing.T, fx *wsFixture) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	write := func(w http.ResponseWriter, key string, items []any) {
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{key: items}})
	}
	mux.HandleFunc("/api/v1/sync/backlog", func(w http.ResponseWriter, r *http.Request) {
		write(w, "backlog", fx.backlog)
	})
	mux.HandleFunc("/api/v1/sync/gates", func(w http.ResponseWriter, r *http.Request) {
		fx.lastGatesQuery = r.URL.RawQuery
		if fx.gatesStatus != 0 && fx.gatesStatus != 200 {
			w.WriteHeader(fx.gatesStatus)
			json.NewEncoder(w).Encode(fx.gatesBody)
			return
		}
		if fx.emptyEnvelope {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
			return
		}
		if fx.failGates {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
			return
		}
		switch r.URL.Query().Get("state") {
		case "", "open":
			write(w, "gates", fx.gates)
		case "answered":
			write(w, "gates", fx.answeredGates)
		case "dismissed":
			write(w, "gates", fx.dismissedGates)
		case "superseded":
			write(w, "gates", fx.supersededGates)
		case "all":
			all := append([]any{}, fx.gates...)
			all = append(all, fx.answeredGates...)
			all = append(all, fx.dismissedGates...)
			all = append(all, fx.supersededGates...)
			all = append(all, fx.closedGates...)
			write(w, "gates", all)
		default:
			w.WriteHeader(422)
			json.NewEncoder(w).Encode(map[string]any{"error": "unknown state"})
		}
	})
	// REQ-CROSS-109: the by-id show route. Mirrors the server: 400 without
	// system_id, the served gate, or a nested {"error":{"message":"no such gate: …"}}
	// 404 when absent. The gatesStatus/gatesBody knob forces an arbitrary failure —
	// the routeless case (Phoenix's {"errors":{"detail":"Not Found"}}) needs the body,
	// not the status alone.
	mux.HandleFunc("/api/v1/sync/gates/", func(w http.ResponseWriter, r *http.Request) {
		fx.lastGatesQuery = r.URL.RawQuery
		if fx.gatesStatus != 0 && fx.gatesStatus != 200 {
			w.WriteHeader(fx.gatesStatus)
			json.NewEncoder(w).Encode(fx.gatesBody)
			return
		}
		if r.URL.Query().Get("system_id") == "" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"errors": map[string]any{"detail": "Bad Request"}})
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/sync/gates/")
		if fx.gate != nil && str(fx.gate, "external_id") == id {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gate": fx.gate}})
			return
		}
		w.WriteHeader(404)
		json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "no such gate: " + id}})
	})
	mux.HandleFunc("/api/v1/sync/epics", func(w http.ResponseWriter, r *http.Request) { write(w, "epics", fx.epics) })
	mux.HandleFunc("/api/v1/sync/delivery-context", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"data": fx.deliveryContext})
	})
	mux.HandleFunc("/api/v1/sync/packet-sections", func(w http.ResponseWriter, r *http.Request) {
		if fx.packetSectionsStatus != 0 && fx.packetSectionsStatus != 200 {
			w.WriteHeader(fx.packetSectionsStatus)
			json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"packet_sections":   fx.packetSections,
			"missing_canonical": []any{"red_strategy", "decisions"},
		}})
	})
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		fx.authorPosts = append(fx.authorPosts, body)
		rec, _ := body["record"].(map[string]any)
		conflictKey := str(rec, "external_id")
		respKey := "requirement"
		switch str(rec, "kind") {
		case "epic":
			respKey = "epic"
		case "packet_section":
			respKey = "packet_section"
			conflictKey = str(rec, "scope_kind") + ":" + str(rec, "scope_external_id") + ":" + str(rec, "section_key")
		}
		if fx.authorConflict[conflictKey] {
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": "the content has moved since that fingerprint"}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			respKey: map[string]any{"external_id": str(rec, "external_id"), "fingerprint": "served-after-" + conflictKey},
		}})
	})
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		fx.lastReqQuery = r.URL.RawQuery
		// The real endpoint serves system requirements AND user requirements in
		// one envelope, under separate keys (sync_api_controller requirements/2).
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"requirements":      fx.requirements,
			"user_requirements": fx.userRequirements,
		}})
	})
	mux.HandleFunc("/api/v1/sync/evidence/latest", func(w http.ResponseWriter, r *http.Request) {
		write(w, "evidence", nil)
	})
	mux.HandleFunc("/api/v1/feed", func(w http.ResponseWriter, r *http.Request) {
		fx.lastFeedQuery = r.URL.RawQuery
		if fx.feedDelay > 0 {
			time.Sleep(fx.feedDelay)
		}
		if fx.failFeed {
			w.WriteHeader(500)
			json.NewEncoder(w).Encode(map[string]any{"error": "boom"})
			return
		}
		if fx.feedNoItems {
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"person": map[string]any{"name": "Jussi Rajala"}}})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": fx.feed})
	})
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			body := map[string]any{}
			json.NewDecoder(r.Body).Decode(&body)
			fx.lastSelectPost = body
			if fx.selectStatus >= 400 {
				w.WriteHeader(fx.selectStatus)
				json.NewEncoder(w).Encode(map[string]any{"error": fx.selectError})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"selection": body}})
			return
		}
		fx.lastSelectGet = r.URL.RawQuery
		if fx.selectReadStatus >= 400 {
			w.WriteHeader(fx.selectReadStatus)
			json.NewEncoder(w).Encode(fx.selectReadBody)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": fx.workSelection})
	})
	mux.HandleFunc("/api/v1/sync/work-selection/held", func(w http.ResponseWriter, r *http.Request) {
		pieces := fx.held
		if pieces == nil {
			pieces = []any{}
		}
		write(w, "pieces", pieces)
	})
	mux.HandleFunc("/api/v1/sync/contract", func(w http.ResponseWriter, r *http.Request) {
		if fx.contractDelay > 0 {
			time.Sleep(fx.contractDelay)
		}
		if fx.contractVersion == 0 {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": fx.contractVersion, "capabilities": map[string]any{}}})
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.requests = append(fx.requests, r.Method+" "+r.URL.Path)
		if fx.storeRevision != "" {
			w.Header().Set("x-modernpath-store-revision", fx.storeRevision)
		}
		if fx.contractVersion > 0 {
			w.Header().Set("x-modernpath-contract", fmt.Sprint(fx.contractVersion))
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func wsEnv(t *testing.T, srv *httptest.Server) *factoryEnv {
	t.Helper()
	return &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
}

func wsGate(id, kind string) map[string]any {
	return map[string]any{"external_id": id, "kind": kind, "title": "Approve: " + id, "state": "open"}
}

func wsEpic(id, title string) map[string]any {
	return map[string]any{"external_id": id, "title": title, "status": "todo",
		"upper_loop_status": "-", "lower_loop_status": "-", "release": "modernpath-v1-09"}
}

func wsReq(id, title string) map[string]any {
	return map[string]any{"external_id": id, "title": title, "context": "CROSS",
		"work_status": "in_progress", "stage": "MVP", "release": "modernpath-v1-09"}
}

// wsUserReq builds a user-requirement payload shaped like the requirements
// read's data.user_requirements element: it carries its derived SR ids under
// system_requirement_external_ids and, unlike an SR, has no stage/priority/owner.
func wsUserReq(id, title string) map[string]any {
	return map[string]any{"external_id": id, "title": title, "context": "CROSS",
		"description": "a user outcome", "work_status": "PROPOSED",
		"system_requirement_external_ids": []any{"SR-1"}}
}

// --- REQ-CROSS-215 — the your-move projection ---

// §215.1+2: the projection lists what the server serves (the server's own
// open-only filter) under a snapshot header naming server, system, pulled-at,
// and CLI version.
func TestYourMoveMaterializesTheServedGatesUnderASnapshotHeader(t *testing.T) {
	fx := &wsFixture{gates: []any{wsGate("APPROVE-EPIC-X", "approval_request"), wsGate("Q-1", "question")}}
	env := wsEnv(t, wsServe(t, fx))

	if err := yourMoveRun(env, wsNow); err != nil {
		t.Fatalf("your-move failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, yourMoveDir, "GATES.md"))
	if err != nil {
		t.Fatalf("projection file not written: %v", err)
	}
	got := string(raw)
	for _, want := range []string{"APPROVE-EPIC-X", "Q-1", "Snapshot at", "system 4", env.APIURL} {
		if !strings.Contains(got, want) {
			t.Errorf("projection must contain %q:\n%s", want, got)
		}
	}
}

// REQ-CROSS-308 (EPIC-CLI-007): `working-set … --include-candidates` asks the
// requirements read for candidates so a just-authored DERIVED candidate is
// materializable; the default read is unchanged. RED first: wsIndex takes no
// include-candidates argument yet.
func TestWorkingSetIncludeCandidatesRidesTheReadQuery(t *testing.T) {
	fx := &wsFixture{requirements: []any{wsReq("SR-1", "x")}}
	env := wsEnv(t, wsServe(t, fx))

	if _, _, err := wsIndex(env, true, nil); err != nil {
		t.Fatalf("wsIndex(include): %v", err)
	}
	if !strings.Contains(fx.lastReqQuery, "include=candidates") {
		t.Fatalf("--include-candidates must add include=candidates, got %q", fx.lastReqQuery)
	}

	if _, _, err := wsIndex(env, false, nil); err != nil {
		t.Fatalf("wsIndex(default): %v", err)
	}
	if strings.Contains(fx.lastReqQuery, "include=candidates") {
		t.Fatalf("the default read must not request candidates, got %q", fx.lastReqQuery)
	}
}

// REQ-CROSS-048/308 (EPIC-CLI-007): the requirements read serves user
// requirements under a SEPARATE envelope key (data.user_requirements). wsIndex
// indexed only data.requirements, so every UR — including a just-authored
// DERIVED UR candidate the include=candidates read is meant to make
// materializable — came back unknown to `working-set pull`. RED first: wsIndex
// does not index user requirements at all.
func TestWorkingSetIndexesUserRequirements(t *testing.T) {
	fx := &wsFixture{
		requirements:     []any{wsReq("SR-1", "a system requirement")},
		userRequirements: []any{wsUserReq("UR-1", "a user requirement")},
	}
	env := wsEnv(t, wsServe(t, fx))

	index, _, err := wsIndex(env, true, nil)
	if err != nil {
		t.Fatalf("wsIndex: %v", err)
	}
	ur, ok := index["UR-1"]
	if !ok {
		keys := make([]string, 0, len(index))
		for k := range index {
			keys = append(keys, k)
		}
		t.Fatalf("wsIndex must index the user requirement UR-1 (so pull can resolve it); got keys %v", keys)
	}
	if ur.kind != "requirement" {
		t.Fatalf("a UR indexes as a requirement so pull materializes it, got kind %q", ur.kind)
	}
	if _, ok := index["SR-1"]; !ok {
		t.Fatalf("wsIndex must still index the system requirement SR-1 alongside the UR")
	}
}

// A pulled UR must render an HONEST body: its actual relation content is the
// derived system requirements it carries (system_requirement_external_ids), not
// the SR-side parent_external_ids it lacks. RED first: the requirement renderer
// only reads parent_external_ids, so a UR's derived SRs never appear.
func TestUserRequirementBodyShowsItsDerivedSRs(t *testing.T) {
	ur := wsItem{id: "UR-1", kind: "requirement", payload: wsUserReq("UR-1", "a user requirement")}
	body := renderItemBody(ur, nil)
	if !strings.Contains(body, "SR-1") {
		t.Fatalf("a user requirement's body must show its derived system requirements; got:\n%s", body)
	}
}

// §215.3: regenerated wholesale — a previous projection is fully replaced,
// never merged.
func TestYourMoveReplacesThePreviousProjectionWholesale(t *testing.T) {
	fx := &wsFixture{gates: []any{wsGate("Q-NEW", "question")}}
	env := wsEnv(t, wsServe(t, fx))
	dir := filepath.Join(env.Root, yourMoveDir)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "GATES.md"), []byte("STALE-GATE-FROM-LAST-WEEK\n"), 0o644)

	if err := yourMoveRun(env, wsNow); err != nil {
		t.Fatalf("your-move failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "GATES.md"))
	if strings.Contains(string(raw), "STALE-GATE-FROM-LAST-WEEK") {
		t.Fatal("a previous projection must be replaced wholesale, not merged")
	}
}

// §215.4: server failure exits non-zero with the server's reason and leaves
// the previous projection untouched.
func TestYourMoveFailureLeavesThePreviousProjectionUntouched(t *testing.T) {
	fx := &wsFixture{failGates: true}
	env := wsEnv(t, wsServe(t, fx))
	dir := filepath.Join(env.Root, yourMoveDir)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "GATES.md"), []byte("previous projection\n"), 0o644)

	err := yourMoveRun(env, wsNow)
	if err == nil {
		t.Fatal("a failed fetch must return an error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("the error must carry the server's reason, got %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "GATES.md"))
	if string(raw) != "previous projection\n" {
		t.Fatal("a failed fetch must leave the previous projection untouched")
	}
}

// --- REQ-CROSS-216 — working-set pull ---

// §216.1+2: shape files with the two Identity-design hashes in the header,
// and absent fields naming their actual cause with the right marker.
func TestPullMaterializesShapeFilesWithIdentityAndHonestAbsences(t *testing.T) {
	fx := &wsFixture{
		epics:        []any{wsEpic("EPIC-X", "The X epic")},
		requirements: []any{wsReq("REQ-Y-001", "The Y requirement")},
		gates:        []any{map[string]any{"external_id": "APPROVE-EPIC-X", "kind": "approval_request", "title": "Approve: EPIC-X", "state": "open"}},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-X", "REQ-Y-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	for _, name := range []string{"EPIC-X.md", "REQ-Y-001.md"} {
		raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, name))
		if err != nil {
			t.Fatalf("%s not written: %v", name, err)
		}
		got := string(raw)
		for _, want := range []string{"Snapshot at", "Source identity:** sha256:", "Written body:** sha256:"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s must carry %q in its header:\n%s", name, want, got)
			}
		}
	}
	epicFile, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-X.md"))
	got := string(epicFile)
	if !strings.Contains(got, "APPROVE-EPIC-X") {
		t.Error("a pulled epic must materialize its open gates")
	}
	// REQ-CROSS-262: prerequisites, predecessor and successor all have store
	// columns now, so «not recorded by store» is a false statement about them —
	// they are recorded and simply not served. The two markers must not be
	// interchanged, and applied_state is the same class.
	for _, want := range []string{
		"Prerequisites:** " + notServed,
		"Predecessor / successor:** " + notServed,
		"Application:** " + notServed,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("gate render must carry %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, notRecorded) {
		t.Errorf("no gate slot has a missing store column any more — %s is a false claim:\n%s", notRecorded, got)
	}
}

// REQ-CROSS-262 — the fingerprint IS served (the gate's content-shadow hash),
// and it is the value the flip and the advance both reference. Rendering it as
// «not recorded by store» was wrong twice over: the column exists and the
// surface serves it, and an operator reading the working set was told to go
// find a value they were already holding.
func TestPullRendersTheServedGateFingerprint(t *testing.T) {
	fx := &wsFixture{
		epics: []any{wsEpic("EPIC-F", "The F epic")},
		gates: []any{map[string]any{
			"external_id": "APPROVE-EPIC-F", "kind": "approval_request",
			"title": "Approve: EPIC-F", "state": "open", "fingerprint": "sha-of-the-gate",
		}},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-F"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-F.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Fingerprint:** sha-of-the-gate") {
		t.Errorf("the served fingerprint must render from the store:\n%s", raw)
	}
}

// And the honest absence: a gate with no shadow row yet serves a null
// fingerprint. That is an empty slot the store answered for — "—" — not an
// unserved field and not a missing column.
func TestPullRendersANullFingerprintAsAnEmptySlot(t *testing.T) {
	fx := &wsFixture{
		epics: []any{wsEpic("EPIC-G", "The G epic")},
		gates: []any{map[string]any{
			"external_id": "APPROVE-EPIC-G", "kind": "approval_request",
			"title": "Approve: EPIC-G", "state": "open", "fingerprint": nil,
		}},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-G"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-G.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "Fingerprint:** —") {
		t.Errorf("a served-but-null fingerprint is an empty slot, not an absence marker:\n%s", raw)
	}
}

// The other half: when the surface DOES serve prerequisites and the
// predecessor/successor pair, they render rather than staying behind a marker.
func TestPullRendersServedGateLineageWhenTheSurfaceCarriesIt(t *testing.T) {
	fx := &wsFixture{
		epics: []any{wsEpic("EPIC-H", "The H epic")},
		gates: []any{map[string]any{
			"external_id": "APPROVE-EPIC-H", "kind": "approval_request",
			"title": "Approve: EPIC-H", "state": "open",
			"prerequisite_gate_external_ids": []any{"TRACE-ENTRY-H", "TRACE-COLD-H"},
			"predecessor_external_id":        "APPROVE-EPIC-H-V1",
			"successor_external_id":          nil,
		}},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-H"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	got := string(mustReadFile(t, filepath.Join(env.Root, workingSetDir, "EPIC-H.md")))
	if !strings.Contains(got, "Prerequisites:** TRACE-ENTRY-H · TRACE-COLD-H") {
		t.Errorf("served prerequisites must render:\n%s", got)
	}
	if !strings.Contains(got, "Predecessor / successor:** APPROVE-EPIC-H-V1 / —") {
		t.Errorf("a served predecessor with a null successor must render both halves honestly:\n%s", got)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// §216.3: unresolvable ids are reported per id; resolvable ids still write;
// exit is non-nil when any id failed.
func TestPullReportsUnknownIdsAndStillWritesResolvableOnes(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "The X epic")}}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetPull(env, []string{"EPIC-X", "EPIC-NOPE"}, wsNow)
	if err == nil {
		t.Fatal("an unresolvable id must make the pull exit non-nil")
	}
	if !strings.Contains(err.Error(), "EPIC-NOPE") {
		t.Errorf("the error must name the id that failed, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, workingSetDir, "EPIC-X.md")); statErr != nil {
		t.Fatal("resolvable ids must still be written when another id fails")
	}
}

// --- REQ-CROSS-217 — staleness ---

// §217.1+2: a server-side change makes the file stale; an unchanged file is
// current. §217.3: --refresh re-pulls the stale file.
func TestCheckNamesStaleFilesAndRefreshRepullsThem(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "old title"), wsEpic("EPIC-Z", "steady")}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-X", "EPIC-Z"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	fx.epics = []any{wsEpic("EPIC-X", "new title"), wsEpic("EPIC-Z", "steady")}

	err := workingSetCheck(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "EPIC-X") {
		t.Fatalf("a stale file must be named, got %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "EPIC-Z") {
		t.Errorf("an unchanged file must not be reported stale: %v", err)
	}

	if err := workingSetCheck(env, true, wsNow); err != nil {
		t.Fatalf("--refresh must re-pull stale files cleanly: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-X.md"))
	if !strings.Contains(string(raw), "new title") {
		t.Fatal("--refresh must materialize the current server state")
	}
}

// §217.4: an item the server no longer serves is named and its file left in
// place — deletion is the human's call.
func TestCheckNamesAVanishedItemAndLeavesItsFile(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "here today")}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	fx.epics = nil

	err := workingSetCheck(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "EPIC-X") {
		t.Fatalf("a vanished item must be named, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, workingSetDir, "EPIC-X.md")); statErr != nil {
		t.Fatal("a guard flags, it does not delete: the file must remain")
	}
}

// --- REQ-CROSS-218 — a local edit is never overwritten silently ---

// §218.1: a divergent local file is preserved, the fresh pull lands beside it
// as <id>.md.pulled, and the pull reports the conflict non-nil.
func TestPullPreservesALocalEditAndLandsTheFreshPullBeside(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "server truth")}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	path := filepath.Join(env.Root, workingSetDir, "EPIC-X.md")
	raw, _ := os.ReadFile(path)
	edited := strings.Replace(string(raw), "server truth", "my local note", 1)
	os.WriteFile(path, []byte(edited), 0o644)

	err := workingSetPull(env, []string{"EPIC-X"}, wsNow)
	if err == nil || !strings.Contains(err.Error(), "EPIC-X") {
		t.Fatalf("a conflicting pull must report the conflict, got %v", err)
	}
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "my local note") {
		t.Fatal("the local edit must be preserved unchanged")
	}
	if _, statErr := os.Stat(path + ".pulled"); statErr != nil {
		t.Fatal("the fresh pull must land beside the local file as .pulled")
	}
}

// §218.3: an unedited file re-pulls in place with no conflict noise.
func TestPullRepullsAnUneditedFileInPlaceSilently(t *testing.T) {
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "server truth")}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("first pull failed: %v", err)
	}
	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("re-pulling an unedited file must be clean, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, workingSetDir, "EPIC-X.md.pulled")); statErr == nil {
		t.Fatal("an unedited re-pull must not produce a .pulled conflict file")
	}
}

// A served external id becomes a file name under the working-set directory.
// filepath.Join collapses "../", so an id carrying path separators would
// escape the directory and overwrite arbitrary repository files — refuse it,
// write nothing, and name the refused id.
func TestPullRefusesAnExternalIdThatEscapesTheWorkingSetDirectory(t *testing.T) {
	evil := "../../escaped"
	fx := &wsFixture{epics: []any{wsEpic(evil, "hostile id"), wsEpic("EPIC-X", "good")}}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetPull(env, []string{evil, "EPIC-X"}, wsNow)
	if err == nil || !strings.Contains(err.Error(), evil) {
		t.Fatalf("a path-escaping id must fail the pull and be named, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, "escaped.md")); statErr == nil {
		t.Fatal("the escaping id must not write outside the working-set directory")
	}
	if _, statErr := os.Stat(filepath.Join(env.Root, workingSetDir, "EPIC-X.md")); statErr != nil {
		t.Fatal("well-formed ids must still be written when a hostile id is refused")
	}
}

// --- REQ-CROSS-219 — answered/closed gate history (specs §219) ---

// §219.3: pull requests state=all; a gate ASSOCIATED BY HOLDS (not by id
// embedding — the RQ-268/REQ-PLN-043 pattern) materializes with its item,
// rendering the store state verbatim, the answer with attribution, and
// Application from the served applied_state.
func TestPullMaterializesAnsweredGateHistoryWithAttribution(t *testing.T) {
	fx := &wsFixture{
		epics: []any{wsEpic("EPIC-X", "The X epic")},
		answeredGates: []any{map[string]any{
			"external_id": "RQ-9", "kind": "decision", "title": "Should X do Y?",
			"state": "answered", "answer": "Yes, create-only.", "source_tag": "USER:2026-08-20",
			"answerer_kind": "user", "applied_state": "pending",
			"holds": []any{map[string]any{"held_external_id": "EPIC-X"}},
		}},
	}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-X.md"))
	got := string(raw)
	for _, want := range []string{
		"RQ-9",
		"State:** ANSWERED",
		"Yes, create-only.",
		"USER:2026-08-20",
		"Application:** pending",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("materialized gate history must contain %q:\n%s", want, got)
		}
	}
}

// §219.4 pin: the your-move projection stays open-only — an answered gate
// reachable under state=all must never appear in the pending-decision queue.
func TestYourMoveStaysOpenOnlyWhenHistoryIsReadable(t *testing.T) {
	fx := &wsFixture{
		gates: []any{wsGate("Q-OPEN", "question")},
		answeredGates: []any{map[string]any{
			"external_id": "Q-DONE", "kind": "question", "title": "answered", "state": "answered",
		}},
	}
	env := wsEnv(t, wsServe(t, fx))
	if err := yourMoveRun(env, wsNow); err != nil {
		t.Fatalf("your-move failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, yourMoveDir, "GATES.md"))
	if !strings.Contains(string(raw), "Q-OPEN") || strings.Contains(string(raw), "Q-DONE") {
		t.Fatalf("the projection must list open gates only:\n%s", string(raw))
	}
}

// §219.5 (Identity design revision): a gate flipping state changes the file's
// rightful content, so the working set must report it stale even though the
// item payload is unchanged.
func TestCheckReportsStaleWhenAnAssociatedGateFlips(t *testing.T) {
	openGate := map[string]any{
		"external_id": "APPROVE-EPIC-X", "kind": "approval_request",
		"title": "Approve: EPIC-X", "state": "open",
	}
	fx := &wsFixture{epics: []any{wsEpic("EPIC-X", "steady")}, gates: []any{openGate}}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"EPIC-X"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	// the gate flips; the item payload does not change
	fx.gates = nil
	fx.answeredGates = []any{map[string]any{
		"external_id": "APPROVE-EPIC-X", "kind": "approval_request",
		"title": "Approve: EPIC-X", "state": "answered", "answer": "Approved.",
	}}

	err := workingSetCheck(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "EPIC-X") {
		t.Fatalf("a gate flip must make the file stale, got %v", err)
	}
}

// --- REQ-CROSS-220 — work-selection held by the store (specs §220) ---

// §220.4: select posts the recorded selection; a workspace without git posts
// no invented fingerprint.
func TestSelectPostsTheRecordedSelection(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-B3", kind: "epic", phase: "build",
		members: []string{"REQ-1", "REQ-2"}, owner: "jussi",
	}, wsNow)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	got := fx.lastSelectPost
	if got == nil {
		t.Fatal("select must POST the selection")
	}
	if got["scope_external_id"] != "EPIC-B3" || got["phase"] != "build" {
		t.Fatalf("posted payload wrong: %v", got)
	}
	if _, present := got["fingerprint"]; present {
		t.Fatalf("a workspace without git must not invent a fingerprint: %v", got["fingerprint"])
	}
}

// §220.2/4: the server owns the state machine — its refusal surfaces verbatim.
func TestSelectSurfacesTheServerRefusal(t *testing.T) {
	fx := &wsFixture{selectStatus: 422, selectError: "displacing EPIC-A requires previous_outcome"}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetSelect(env, wsSelectOpts{scope: "EPIC-B", kind: "epic", phase: "plan"}, wsNow)
	if err == nil || !strings.Contains(err.Error(), "previous_outcome") {
		t.Fatalf("the server's refusal must surface, got %v", err)
	}
}

// --- REQ-CROSS-345: one holder per piece of work (client half) ---

// A take names the one current piece it displaces, and the displaced piece's
// outcome rides along so the server can close it in the same write.
func TestSelectNamesWhatItReplaces(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-NEW", kind: "epic", phase: "plan",
		replaces: "EPIC-OLD", outcome: "returned=plan",
	}, wsNow); err != nil {
		t.Fatalf("select --replaces failed: %v", err)
	}
	got := fx.lastSelectPost
	if got["scope_external_id"] != "EPIC-NEW" {
		t.Fatalf("the taken scope must post: %v", got)
	}
	if got["replaces"] != "EPIC-OLD" {
		t.Fatalf("--replaces must name the displaced piece, got %v", got["replaces"])
	}
	if got["previous_outcome"] != "returned:plan" {
		t.Fatalf("the displaced piece's outcome must ride along, got %v", got["previous_outcome"])
	}
}

// Without --replaces a take names nothing to displace: it adds a current
// holder beside any the caller already has, never silently overwriting one.
func TestSelectWithoutReplacesAdds(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-NEW", kind: "epic", phase: "plan",
	}, wsNow); err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if _, present := fx.lastSelectPost["replaces"]; present {
		t.Fatalf("an unnamed take must not post replaces — it adds a holder: %v", fx.lastSelectPost)
	}
}

// A put-down closes the caller's own named current piece, recording how it
// ended. It is a close, not a take — it posts `close`, not scope_external_id.
func TestPutDownClosesTheNamedPiece(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-B3", putDown: true, outcome: "done",
	}, wsNow); err != nil {
		t.Fatalf("select --put-down failed: %v", err)
	}
	got := fx.lastSelectPost
	if got["close"] != "EPIC-B3" {
		t.Fatalf("--put-down must close the named piece, got %v", got["close"])
	}
	if got["previous_outcome"] != "done" {
		t.Fatalf("the put-down outcome must ride along, got %v", got["previous_outcome"])
	}
	if _, present := got["scope_external_id"]; present {
		t.Fatalf("a put-down is a close, not a take — it must post no scope_external_id: %v", got)
	}
}

// A put-down with no named piece is refused client-side and posts nothing.
func TestPutDownNeedsAScope(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetSelect(env, wsSelectOpts{putDown: true, outcome: "done"}, wsNow)
	if err == nil || !strings.Contains(err.Error(), "put-down") {
		t.Fatalf("--put-down with no scope must be refused client-side, got %v", err)
	}
	if fx.lastSelectPost != nil {
		t.Fatalf("a refused put-down must post nothing, got %v", fx.lastSelectPost)
	}
}

// The read is caller-scoped; --piece names which of the caller's own current
// pieces the read resolves, carried to the server as ?scope=.
func TestSelectionReadNamesThePiece(t *testing.T) {
	defer func() { wsPiece = "" }()
	wsPiece = "EPIC-B3"
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	if !strings.Contains(fx.lastSelectGet, "scope=EPIC-B3") {
		t.Fatalf("--piece must carry as ?scope=, got query %q", fx.lastSelectGet)
	}
}

// When the caller holds several current pieces and names none, the server 422s
// naming them; the client surfaces that actionable refusal, not a bare status.
func TestSelectionReadSurfacesAmbiguousHolder(t *testing.T) {
	fx := &wsFixture{
		selectReadStatus: 422,
		selectReadBody: map[string]any{
			"error":  "you hold several current selections (EPIC-A, EPIC-B) — name one with ?scope=<id>",
			"pieces": []any{"EPIC-A", "EPIC-B"},
		},
	}
	env := wsEnv(t, wsServe(t, fx))

	err := workingSetPull(env, []string{"selection"}, wsNow)
	if err == nil {
		t.Fatal("an ambiguous holder must surface as an error")
	}
	for _, want := range []string{"EPIC-A", "EPIC-B"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must name the held pieces, got %v", err)
		}
	}
	if strings.Contains(err.Error(), "server 422") {
		t.Fatalf("the actionable server message must surface, not a bare status: %v", err)
	}
}

func wsSelectionPayload() map[string]any {
	return map[string]any{
		"active_release": []any{map[string]any{"slug": "modernpath-v1-09", "status": "active"}},
		"current": map[string]any{
			"scope_external_id": "EPIC-B3", "scope_kind": "epic", "phase": "build",
			"status": "current", "owner": "jussi", "fingerprint": "abc123",
		},
		"suspended": []any{},
		"history": []any{
			map[string]any{"scope_external_id": "EPIC-OLD", "outcome": "returned:plan", "status": "superseded"},
			map[string]any{"scope_external_id": "EPIC-B3", "outcome": "resumed", "status": "superseded"},
		},
	}
}

// §220.5: pull selection materializes the installed shape — header hashes,
// Active release slot, current section, and a history that keeps the shape's
// own vocabulary (resumed bookkeeping rows excluded).
func TestPullSelectionMaterializesTheShape(t *testing.T) {
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "WORK-SELECTION.md"))
	if err != nil {
		t.Fatalf("WORK-SELECTION.md not written: %v", err)
	}
	got := string(raw)
	for _, want := range []string{
		"Source identity:** sha256:", "Written body:** sha256:",
		"Active release", "modernpath-v1-09", "active",
		"EPIC-B3", "build",
		"EPIC-OLD", "returned:plan",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("WORK-SELECTION.md must contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "resumed") {
		t.Errorf("resumed bookkeeping rows must not render in history:\n%s", got)
	}
}

// §220.5: the materialized selection is checkably stale like any other file.
func TestCheckReportsSelectionStale(t *testing.T) {
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}

	fx.workSelection["current"].(map[string]any)["phase"] = "completion"

	err := workingSetCheck(env, false, wsNow)
	if err == nil || !strings.Contains(err.Error(), "WORK-SELECTION") {
		t.Fatalf("a phase advance must make the selection stale, got %v", err)
	}
}

// --- external-review hardening ---

// A named scope rides the suspend payload so the server
// can refuse a mismatch instead of suspending whatever is current.
func TestSelectSuspendCarriesTheNamedScope(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-B", suspend: true, reason: "blocked on vendor",
	}, wsNow); err != nil {
		t.Fatalf("select --suspend failed: %v", err)
	}
	if fx.lastSelectPost["scope_external_id"] != "EPIC-B" {
		t.Fatalf("the named scope must ride the suspend payload, got %v", fx.lastSelectPost)
	}
}

// Gate↔item id matching must respect id boundaries — RQ-15 must
// not absorb RQ-150's gates (real corpus numbering: RQ-15 and RQ-105..174).
func TestGateAssociationRespectsIdBoundaries(t *testing.T) {
	gate := func(id string) map[string]any { return map[string]any{"external_id": id} }
	if gateAssociated(gate("APPROVE-RQ-150"), "RQ-15") {
		t.Fatal("RQ-15 must not absorb RQ-150's gate")
	}
	if !gateAssociated(gate("APPROVE-RQ-15"), "RQ-15") {
		t.Fatal("an id bounded by non-id characters must associate")
	}
	if !gateAssociated(gate("RQ-15"), "RQ-15") {
		t.Fatal("an exact id must associate")
	}
}

// An explicitly set empty --waiting-on posts the key,
// so the server can clear the field.
func TestSelectPostsAnExplicitlyEmptyWaitingOn(t *testing.T) {
	fx := &wsFixture{}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{
		scope: "EPIC-A", kind: "epic", waitingOn: "", waitingOnSet: true,
	}, wsNow); err != nil {
		t.Fatalf("select failed: %v", err)
	}
	if v, present := fx.lastSelectPost["waiting_on"]; !present || v != "" {
		t.Fatalf("an explicitly empty waiting_on must be posted to clear the field, got %v", fx.lastSelectPost)
	}
}

// A 200 whose envelope lacks the expected key is a FAILURE, not an
// empty list — your-move must error and leave the previous projection
// untouched, exactly like a non-200 (§215.4's spirit).
func TestYourMoveRefusesAnEnvelopeWithoutTheGatesKey(t *testing.T) {
	fx := &wsFixture{emptyEnvelope: true}
	env := wsEnv(t, wsServe(t, fx))
	dir := filepath.Join(env.Root, yourMoveDir)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "GATES.md"), []byte("previous projection\n"), 0o644)

	err := yourMoveRun(env, wsNow)
	if err == nil {
		t.Fatal("a missing envelope key must be an error, never an empty queue")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "GATES.md"))
	if string(raw) != "previous projection\n" {
		t.Fatal("the previous projection must survive an unreadable response")
	}
}

// The rendered WORK-SELECTION.md must carry the installed shape's
// exact tables — 7-column Suspended (incl. Restored-to status, Owner),
// 5-column history (incl. Selected at) — and the Members bullet.
func TestPullSelectionMatchesTheInstalledShapeColumns(t *testing.T) {
	payload := wsSelectionPayload()
	payload["current"].(map[string]any)["members"] = []any{"REQ-1", "REQ-2"}
	payload["suspended"] = []any{map[string]any{
		"scope_external_id": "EPIC-P", "suspended_status": "blocked",
		"suspended_reason": "vendor", "owner": "jussi", "waiting_on": "GATE-7",
	}}
	fx := &wsFixture{workSelection: payload}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, selectionFile))
	got := string(raw)
	for _, want := range []string{
		"- **Members:** REQ-1, REQ-2",
		"| Scope | Suspended status | Restored-to status | Reason | Owner | Target | Blocker/gate |",
		"| Scope | Selected at | Left at | Outcome | Successor selection |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the rendered file must match the installed shape — missing %q:\n%s", want, got)
		}
	}
}

// REQ-CROSS-223 §223.1 — the served relations, owner, and member list render
// in the pull shapes; their unconditional «not served» markers disappear.
// RED first: the renderer consults no field for these slots today.
func TestPullRendersServedRelationsOwnerAndMembers(t *testing.T) {
	req := wsReq("REQ-HM-001", "The captured requirement")
	req["owner"] = "cli"
	req["parent_external_id"] = "UR-HM-001"
	req["parent_external_ids"] = []any{"UR-HM-001", "UR-HM-002"}
	epic := wsEpic("EPIC-HM-001", "The homes epic")
	epic["requirement_external_ids"] = []any{"REQ-HM-001", "REQ-HM-002"}
	epic["process_status"] = "IN_REVIEW"
	fx := &wsFixture{epics: []any{epic}, requirements: []any{req}}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"EPIC-HM-001", "REQ-HM-001"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}

	reqFile, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "REQ-HM-001.md"))
	got := string(reqFile)
	if !strings.Contains(got, "Owner / release:** cli /") {
		t.Errorf("served owner must render, not %s:\n%s", notServed, got)
	}
	if !strings.Contains(got, "Declared relations:** UR-HM-001 · UR-HM-002") {
		t.Errorf("served relations must render, not %s:\n%s", notServed, got)
	}

	epicFile, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, "EPIC-HM-001.md"))
	got = string(epicFile)
	if !strings.Contains(got, "Members:** REQ-HM-001 · REQ-HM-002") {
		t.Errorf("served member list must render, not %s:\n%s", notServed, got)
	}
	if !strings.Contains(got, "Process status:** IN_REVIEW") {
		t.Errorf("the served process-lifecycle status must render:\n%s", got)
	}

	// A store that serves none of them still reads honestly.
	fx2 := &wsFixture{epics: []any{wsEpic("EPIC-HM-009", "Bare epic")}, requirements: []any{wsReq("REQ-HM-009", "Bare req")}}
	env2 := wsEnv(t, wsServe(t, fx2))
	if err := workingSetPull(env2, []string{"REQ-HM-009"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	bare, _ := os.ReadFile(filepath.Join(env2.Root, workingSetDir, "REQ-HM-009.md"))
	if !strings.Contains(string(bare), "Declared relations:** "+notServed) {
		t.Errorf("an unserved relation slot must keep its honest marker:\n%s", string(bare))
	}

	// Served-but-empty is "—" (the store answered: none), never the marker —
	// the REQ-CROSS-219 honesty rule.
	nullReq := wsReq("REQ-HM-010", "Null-served row")
	nullReq["owner"] = nil
	nullReq["parent_external_ids"] = []any{}
	fx3 := &wsFixture{requirements: []any{nullReq}}
	env3 := wsEnv(t, wsServe(t, fx3))
	if err := workingSetPull(env3, []string{"REQ-HM-010"}, wsNow); err != nil {
		t.Fatalf("pull failed: %v", err)
	}
	nulled, _ := os.ReadFile(filepath.Join(env3.Root, workingSetDir, "REQ-HM-010.md"))
	if !strings.Contains(string(nulled), "Owner / release:** —") {
		t.Errorf("served-null owner must read —:\n%s", string(nulled))
	}
	if !strings.Contains(string(nulled), "Declared relations:** —") {
		t.Errorf("served-empty relations must read —:\n%s", string(nulled))
	}
}
