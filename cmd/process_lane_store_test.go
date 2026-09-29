package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"testing"
)

// REQ-CROSS-458 (EPIC-RDD-LANE): a stateful fake of the lane's server half,
// shaped after the server contract (core/lib/core/rdd/lane.ex and the lane
// tests under core/test/core/rdd): the lane authorization gate, lane_class on
// a requirement, the narrow review at the SR's single_sr aggregate, entry by
// lane_ref, re-application after an edit, the server's eligibility verdict and
// the lane-batch completion. The single_sr aggregate is served only for a
// piece the caller holds, as the delivery-context read serves it.

type laneStore struct {
	*fakeAuthorStore
	t        *testing.T
	held     []string                  // held single_sr pieces, oldest first
	sel      map[string]map[string]any // piece -> selection
	red      map[string]bool
	passAt   map[string][]string // SR -> revisions with a passing result
	entered  map[string]string   // SR -> the aggregate its lane entry pinned
	selects  []map[string]any
	evidence []map[string]any
	unknown  []string
	clock    int
	rules    map[string]any
}

func newLaneStore(t *testing.T) *laneStore {
	l := &laneStore{fakeAuthorStore: newFakeAuthorStore(), t: t, sel: map[string]map[string]any{},
		red: map[string]bool{}, passAt: map[string][]string{}, entered: map[string]string{},
		rules: map[string]any{"max_non_test_files": 5, "default_excluded_globs": []any{"oidc-bff/**", ".claude/**"},
			"test_paths": "test/ directories and *_test.go"}}
	l.contextFn = l.context
	l.authorHook = l.authorWrite
	l.routes = l.extraRoutes
	l.gatesFn = l.gateList
	return l
}

func (l *laneStore) tick() string {
	l.clock++
	return fmt.Sprintf("2026-09-28T10:%02d:00Z", l.clock)
}

// seedSR stores a PROPOSED lane SR carrying a source, boundary and method.
func (l *laneStore) seedSR(id, class string) {
	fields := map[string]any{"work_status": "PROPOSED", "title": "Small change " + id, "context": "LN",
		"boundary": "modernpath-react/src/components/EmptyState.tsx", "verification_method": "Vitest",
		"source_citations": []any{map[string]any{"kind": "process_source", "source_tag": "USER:2026-09-28:lane"}}}
	if class != "" {
		fields["lane_class"] = class
	}
	l.seed(id, "system", fields)
}

// seedAuthorization stores an answered lane authorization of this System.
func (l *laneStore) seedAuthorization(id string) {
	l.gates[id] = map[string]any{"external_id": id, "gate_class": "human", "kind": "approval_request",
		"purpose": "lane_authorization", "exact_scope": []any{"system:1"}, "state": "answered",
		"answer": "approve", "chosen_option_keys": []any{"approve"}, "answered_at": l.tick(),
		"fingerprint": "fp-" + id}
}

func (l *laneStore) status(id string) string { return str(l.records[id], "work_status") }

func (l *laneStore) setStatus(id, status string) { l.records[id]["work_status"] = status }

// aggregate is the SR's single_sr packet aggregate: a function of its content.
func (l *laneStore) aggregate(id string) string {
	rec := l.records[id]
	if rec == nil {
		return ""
	}
	sum := sha256.Sum256([]byte("single_sr:" + id + ":" + str(rec, "fingerprint")))
	return hex.EncodeToString(sum[:])
}

func (l *laneStore) holds(piece string) bool { return slices.Contains(l.held, piece) }

// narrowReview is the passing cold-review trace with a LANE:narrow source at
// the SR's current aggregate, or "".
func (l *laneStore) narrowReview(id string) string {
	agg := l.aggregate(id)
	for gid, g := range l.gates {
		if g["purpose"] != "cold-review" || g["state"] != "pass" || str(g, "evaluated_scope_fingerprint") != agg {
			continue
		}
		if !slices.Contains(stringSlice(g["exact_scope"]), id) {
			continue
		}
		for _, raw := range anySlice(g["sources"]) {
			if src, ok := raw.(map[string]any); ok && strings.EqualFold(str(src, "ref"), "LANE:narrow") {
				return gid
			}
		}
	}
	return ""
}

func anySlice(v any) []any {
	switch s := v.(type) {
	case []any:
		return s
	case []map[string]any:
		out := make([]any, len(s))
		for i, m := range s {
			out[i] = m
		}
		return out
	}
	return nil
}

// currentAuthorization is the newest answered lane authorization.
func (l *laneStore) currentAuthorization() string {
	best, at := "", ""
	for id, g := range l.gates {
		if g["purpose"] == "lane_authorization" && g["state"] == "answered" && str(g, "answered_at") > at {
			best, at = id, str(g, "answered_at")
		}
	}
	return best
}

func (l *laneStore) eligiblePass(id string) bool {
	rev := str(l.records[id], "delivered_revision")
	for _, g := range l.gates {
		if g["purpose"] == "lane-eligibility" && g["state"] == "pass" && str(g, "fingerprint") == rev &&
			slices.Contains(stringSlice(g["exact_scope"]), id) {
			return true
		}
	}
	return false
}

func (l *laneStore) context(piece string) map[string]any {
	if piece == "" && len(l.held) == 1 {
		piece = l.held[0]
	}
	if !l.holds(piece) {
		return map[string]any{"derived_reason": "no_current_selection", "facts_state": "unavailable"}
	}
	rec := l.records[piece]
	agg := l.aggregate(piece)
	evidence := "claimed"
	if len(l.passAt[piece]) > 0 {
		evidence = "passing"
	}
	lower := l.gates["TRACE-LOWER-"+piece]
	cr := map[string]any{"verdict": "", "independent": false, "trace_external_id": "", "open_finding_ids": []any{}}
	if id := l.narrowReview(piece); id != "" {
		cr["verdict"], cr["independent"], cr["trace_external_id"] = "pass", true, id
	}
	return map[string]any{
		"packet_fingerprint": agg, "derived_phase": str(l.sel[piece], "phase"), "declared_phase": str(l.sel[piece], "phase"),
		"facts_state": "served",
		"facts": map[string]any{
			"aggregate": agg,
			"scope":     map[string]any{"external_id": piece, "kind": "single_sr", "status": l.status(piece)},
			"members": []any{map[string]any{"external_id": piece, "kind": "sr", "status": l.status(piece),
				"content_fingerprint": str(rec, "fingerprint"), "evidence_state": evidence, "red_recorded": l.red[piece],
				"lower_trace_pass": lower != nil && str(lower, "fingerprint") == str(rec, "fingerprint")}},
			"cold_review": cr,
			"entry_gate":  map[string]any{"applied": l.entered[piece] != "", "pinned_aggregate": l.entered[piece]},
		},
	}
}

func (l *laneStore) gateList(state string) []any {
	ids := make([]string, 0, len(l.gates))
	for id := range l.gates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []any{}
	for _, id := range ids {
		g := l.gates[id]
		if state == "all" || str(g, "state") == state || (state == "" && str(g, "state") == "open") {
			row := map[string]any{"external_id": id}
			for k, v := range g {
				row[k] = v
			}
			out = append(out, row)
		}
	}
	return out
}

func legality(field, msg string) map[string]any {
	return map[string]any{"error": map[string]any{"details": map[string]any{field: []any{msg}}, "reason": "legality"}}
}

func (l *laneStore) authorWrite(body map[string]any) (bool, int, map[string]any, string) {
	record, _ := body["record"].(map[string]any)
	id, kind, action := str(record, "external_id"), str(record, "kind"), str(body, "action")
	switch {
	case action == "evaluate_trace":
		if _, exists := l.gates[id]; exists {
			return true, 409, refusalBody(id + " already exists"), ""
		}
		if str(record, "purpose") == "lane-eligibility" {
			return true, 422, legality("purpose", "a lane-eligibility verdict is recorded only by the server from the delivered diff"), ""
		}
		g := map[string]any{"gate_class": "trace"}
		for k, v := range record {
			g[k] = v
		}
		g["state"] = strings.ToLower(str(record, "verdict"))
		g["evaluated_scope_fingerprint"] = record["fingerprint"]
		l.gates[id] = g
		return true, 200, map[string]any{"data": map[string]any{"gate": map[string]any{"external_id": id, "state": g["state"], "fingerprint": record["fingerprint"]}}}, ""
	case action == "create" && kind == "gate":
		return l.createGate(id, record)
	case (action == "create" || action == "patch") && kind == "requirement":
		if c := str(record, "lane_class"); c != "" && !slices.Contains([]string{"defect_with_failing_test", "wording", "presentation", "dependency_patch"}, c) {
			return true, 422, legality("lane_class", fmt.Sprintf("%q is not a lane class", c)), ""
		}
		return false, 0, nil, ""
	case action == "advance":
		return l.advance(body, record)
	case action == "lane_reapply":
		sr := id
		if l.entered[sr] == "" {
			return true, 409, refusalBody(sr + " did not enter through the lane — there is no lane application to renew"), ""
		}
		if l.entered[sr] == l.aggregate(sr) {
			return true, 409, refusalBody(sr + "'s lane entry is already at its current aggregate — it is not stranded"), ""
		}
		if refusal := l.laneChecks(sr, str(body, "lane_ref")); refusal != nil {
			return true, 422, refusal, ""
		}
		l.entered[sr] = l.aggregate(sr)
		return true, 200, map[string]any{"data": map[string]any{"gate": map[string]any{"external_id": str(body, "lane_ref"), "state": "answered"}}}, ""
	case action == "lane_eligibility":
		return l.eligibility(record)
	case action == "lane_approve":
		// DL-12: the server answers the lane authorization with approve as the
		// signed-in caller; the CLI channel is accepted only on this action.
		g := l.gates[id]
		switch {
		case g == nil:
			return true, 409, refusalBody("no such gate " + id), ""
		case str(g, "purpose") != "lane_authorization":
			return true, 422, legality("purpose", id+" is not a lane_authorization gate — lane_approve answers only a lane authorization"), ""
		case str(g, "state") != "open":
			return true, 409, refusalBody(id + " is already answered"), ""
		}
		l.answer(id, "approve")
		g["answer"], g["answer_channel"] = firstNonEmpty(str(body, "text"), "approve"), "cli"
		return true, 200, map[string]any{"data": map[string]any{"lane_approval": map[string]any{"external_id": id, "state": "answered",
			"answer": g["answer"], "answer_channel": "cli", "chosen_option_keys": []any{"approve"}, "fingerprint": g["fingerprint"]}}}, ""
	}
	return false, 0, nil, ""
}

func (l *laneStore) createGate(id string, record map[string]any) (bool, int, map[string]any, string) {
	if _, taken := l.gates[id]; taken {
		return true, 409, refusalBody("a gate is opened once"), ""
	}
	g := map[string]any{"gate_class": "human", "kind": "approval_request"}
	for k, v := range record {
		g[k] = v
	}
	switch str(record, "purpose") {
	case "lane_authorization":
		if !slices.Equal(stringSlice(record["exact_scope"]), []string{"system:1"}) {
			return true, 422, legality("exact_scope", "a lane authorization names exactly this System, [\"system:1\"]"), ""
		}
		if str(record, "transition") != "" {
			return true, 422, legality("transition", "a lane authorization declares no transition — each application is checked on its own"), ""
		}
		payload, _ := record["raw_payload"].(map[string]any)
		if len(stringSlice(payload["classes"])) == 0 {
			return true, 422, legality("raw_payload", "classes must name at least one of defect_with_failing_test, wording, presentation, dependency_patch"), ""
		}
		if dc, _ := payload["daily_cap"].(float64); dc < 1 || dc > 10 {
			return true, 422, legality("raw_payload", "daily_cap must be an integer from 1 to 10"), ""
		}
	case "lane-batch":
		var causes []string
		prereqs := stringSlice(record["prerequisite_gate_external_ids"])
		for _, m := range stringSlice(record["exact_scope"]) {
			switch {
			case l.entered[m] == "":
				causes = append(causes, m+" did not enter through the lane at its current aggregate")
			case l.status(m) != "IN_REVIEW":
				causes = append(causes, m+" is "+l.status(m)+", not IN_REVIEW")
			case !slices.Contains(l.passAt[m], str(l.records[m], "delivered_revision")):
				causes = append(causes, m+" has no current passing evidence at its delivered revision")
			case !l.eligiblePass(m):
				causes = append(causes, m+" has no server lane-eligibility PASS at its delivered revision — run the lane eligibility check")
			}
			named := false
			for _, p := range prereqs {
				if t := l.gates[p]; t != nil && t["purpose"] == "completion" && t["state"] == "pass" &&
					str(t, "fingerprint") == l.aggregate(m) && slices.Contains(stringSlice(t["exact_scope"]), m) {
					named = true
				}
			}
			if !named {
				causes = append(causes, fmt.Sprintf("the gate names no passing completion trace for %s at its aggregate %s", m, l.aggregate(m)))
			}
			if !slices.ContainsFunc(anySlice(record["options"]), func(o any) bool { return str(o.(map[string]any), "key") == "reject:"+m }) {
				causes = append(causes, "no reject:"+m+" option")
			}
		}
		if len(causes) > 0 {
			return true, 409, refusalBody("a lane-batch gate may open only when every member can complete in the lane — " + strings.Join(causes, "; ")), ""
		}
	}
	g["state"] = "open"
	g["fingerprint"] = l.nextFP(id)
	l.gates[id] = g
	return true, 200, map[string]any{"data": map[string]any{"gate": map[string]any{"external_id": id, "state": "open",
		"fingerprint": g["fingerprint"], "exact_scope": record["exact_scope"]}}}, str(g, "fingerprint")
}

// laneChecks mirrors the lane arm's refusals after the shape check.
func (l *laneStore) laneChecks(sr, ref string) map[string]any {
	auth := l.gates[ref]
	switch {
	case auth == nil:
		return legality("lane_ref", "no lane authorization `"+ref+"` on this System")
	case auth["state"] != "answered":
		return legality("lane_ref", "`"+ref+"` is `"+str(auth, "state")+"`, not answered — a workspace admin answers it in the web app or with `process lane approve`")
	case ref != l.currentAuthorization():
		return legality("lane_ref", "`"+ref+"` is not the System's current lane authorization — `"+l.currentAuthorization()+"` is")
	case str(l.records[sr], "lane_class") == "":
		return legality("lane_class", "`"+sr+"`'s lane_class is not set — set it before the narrow review")
	case l.fail["applier:"+sr]:
		return legality("lane_ref", "the signed-in caller is not an applier of `"+ref+"` — only its named appliers apply it")
	case l.narrowReview(sr) == "":
		return legality("external_id", "`"+sr+"` has no passing narrow review at its current aggregate `"+l.aggregate(sr)+"` — record a cold-review trace with a LANE:narrow source from an independent review context")
	}
	return nil
}

func (l *laneStore) advance(body, record map[string]any) (bool, int, map[string]any, string) {
	id := str(record, "external_id")
	rec := l.records[id]
	if rec == nil {
		return true, 404, refusalBody("no record " + id), ""
	}
	from, to := str(body, "expected"), str(body, "to")
	if from != l.status(id) {
		return true, 409, refusalBody("stale expected"), ""
	}
	basis := "automatic"
	switch {
	case str(body, "lane_ref") != "":
		if str(body, "gate_ref") != "" {
			return true, 422, legality("lane_ref", "lane_ref and gate_ref are exclusive"), ""
		}
		if from != "PROPOSED" || to != "TODO" {
			return true, 422, legality("lane_ref", "lane_ref applies only on PROPOSED->TODO"), ""
		}
		if refusal := l.laneChecks(id, str(body, "lane_ref")); refusal != nil {
			return true, 422, refusal, ""
		}
		l.entered[id] = l.aggregate(id)
		basis = "lane_authorization"
	case str(body, "gate_ref") != "":
		g := l.gates[str(body, "gate_ref")]
		if g == nil || g["state"] != "answered" {
			return true, 422, refusalBody("the gate is not answered"), ""
		}
		if str(body, "gate_fingerprint") != str(g, "fingerprint") {
			return true, 409, refusalBody("the gate's content has moved since that fingerprint — re-read it before applying its answer"), ""
		}
		if slices.Contains(stringSlice(g["chosen_option_keys"]), "reject:"+id) {
			return true, 422, legality("gate_ref", "`"+id+"` was rejected in `"+str(body, "gate_ref")+"` — it stays IN_REVIEW; fix it and include it in a new lane batch"), ""
		}
		if !slices.Contains(stringSlice(g["exact_scope"]), id) || str(body, "gate_answer") != "approve" {
			return true, 422, refusalBody("the gate does not approve " + id), ""
		}
		basis = "gate"
	}
	l.setStatus(id, to)
	return true, 200, map[string]any{"data": map[string]any{"requirement": map[string]any{"external_id": id, "work_status": to},
		"transition_basis": basis}}, str(rec, "fingerprint")
}

func (l *laneStore) eligibility(record map[string]any) (bool, int, map[string]any, string) {
	id, commit := str(record, "external_id"), str(record, "commit")
	if l.entered[id] == "" {
		return true, 409, refusalBody("`" + id + "` did not enter through the small-change lane — its eligibility is not a lane check"), ""
	}
	files := stringSlice(record["files"])
	if len(files) == 0 {
		return true, 422, legality("files", "must be the delivered file list (git diff --name-only over the range)"), ""
	}
	var nonTest, tests []string
	offending := []any{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || strings.Contains(f, ".test.") {
			tests = append(tests, f)
			continue
		}
		nonTest = append(nonTest, f)
		if strings.HasPrefix(f, "oidc-bff/") {
			offending = append(offending, map[string]any{"file": f, "reason": "excluded area oidc-bff/**"})
		}
	}
	if len(nonTest) > 5 {
		offending = append(offending, map[string]any{"file": nonTest[5], "reason": "more than 5 non-test source files"})
	}
	verdict := "PASS"
	if len(offending) > 0 {
		verdict = "FAIL"
	}
	base := str(record, "base")
	rng := commit + "^1.." + commit
	if base != "" {
		rng = base + ".." + commit
	}
	l.records[id]["delivered_revision"] = commit
	trace := fmt.Sprintf("LANE-ELIG-%s-%d", id, len(l.gates))
	var body strings.Builder
	fmt.Fprintf(&body, "Lane eligibility of %s over %s: **%s** (server verdict).\n\nNon-test source files (%d of at most 5):\n", id, rng, verdict, len(nonTest))
	for _, f := range nonTest {
		fmt.Fprintf(&body, "- `%s`\n", f)
	}
	fmt.Fprintf(&body, "\nTest files (not counted): %d\n", len(tests))
	l.gates[trace] = map[string]any{"gate_class": "trace", "purpose": "lane-eligibility", "state": strings.ToLower(verdict),
		"verdict": verdict, "exact_scope": []any{id}, "fingerprint": commit, "body_md": body.String()}
	return true, 200, map[string]any{"data": map[string]any{"lane_eligibility": map[string]any{
		"verdict": verdict, "offending": offending, "non_test_files": nonTest, "test_files": tests, "trace": trace,
		"delivered_revision": commit, "range": rng, "lane_ref": l.currentAuthorization(), "external_id": id, "max_non_test_files": 5}}}, ""
}

// answer records a human answer the way the web app would.
func (l *laneStore) answer(id string, keys ...string) {
	g := l.gates[id]
	g["state"], g["answer"], g["answered_at"] = "answered", strings.Join(keys, ", "), l.tick()
	g["chosen_option_keys"] = toAnySlice(keys)
	g["fingerprint"] = l.nextFP(id)
}

func (l *laneStore) reconcile() []any {
	var out []any
	for _, piece := range l.held {
		switch l.status(piece) {
		case "TODO":
			if l.entered[piece] == l.aggregate(piece) && l.red[piece] {
				out = append(out, transition(piece, "TODO", "IN_PROGRESS"))
				l.setStatus(piece, "IN_PROGRESS")
			}
		case "IN_PROGRESS":
			lower := l.gates["TRACE-LOWER-"+piece]
			if lower != nil && str(lower, "fingerprint") == str(l.records[piece], "fingerprint") && len(l.passAt[piece]) > 0 {
				out = append(out, transition(piece, "IN_PROGRESS", "IN_REVIEW"))
				l.setStatus(piece, "IN_REVIEW")
			}
		}
	}
	return out
}

func (l *laneStore) extraRoutes(mux *http.ServeMux) {
	encode := func(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }
	mux.HandleFunc("/api/v1/sync/work-selection", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		if r.Method == http.MethodPost {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			l.selects = append(l.selects, body)
			piece := str(body, "scope_external_id")
			next := map[string]any{"scope_kind": str(body, "scope_kind"), "scope_external_id": piece, "members": []any{}, "phase": str(body, "phase")}
			if !l.holds(piece) {
				l.held = append(l.held, piece)
			}
			l.sel[piece] = next
			encode(w, map[string]any{"data": map[string]any{"selection": next}})
			return
		}
		piece := r.URL.Query().Get("scope")
		if piece == "" && len(l.held) == 1 {
			piece = l.held[0]
		}
		if piece == "" && len(l.held) > 1 {
			w.WriteHeader(409)
			encode(w, map[string]any{"error": "you hold several current selections", "pieces": toAnySlice(l.held)})
			return
		}
		current := any(nil)
		if l.holds(piece) {
			current = l.sel[piece]
		}
		encode(w, map[string]any{"data": map[string]any{"current": current, "suspended": []any{}, "history": []any{},
			"active_release": []any{map[string]any{"slug": "r", "status": "active"}}}})
	})
	mux.HandleFunc("/api/v1/sync/packet-sections", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		encode(w, map[string]any{"data": map[string]any{"packet_sections": []any{}, "missing_canonical": []any{}}})
	})
	mux.HandleFunc("/api/v1/sync/requirements", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		srs := []any{}
		for id, rec := range l.records {
			if rec["_kind"] != "system" {
				continue
			}
			row := map[string]any{"external_id": id}
			for k, v := range rec {
				if k != "_kind" {
					row[k] = v
				}
			}
			srs = append(srs, row)
		}
		encode(w, map[string]any{"data": map[string]any{"requirements": srs, "user_requirements": []any{}}})
	})
	mux.HandleFunc("/api/v1/sync/lane/eligibility-rules", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		encode(w, map[string]any{"data": l.rules})
	})
	mux.HandleFunc("/api/v1/sync/evidence", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		l.evidence = append(l.evidence, body)
		for _, raw := range anySlice(body["results"]) {
			res, _ := raw.(map[string]any)
			id := str(res, "target_external_id")
			switch {
			case str(res, "result") == "fail" && str(res, "role") == "RED":
				l.red[id] = true
			case str(res, "result") == "pass":
				l.passAt[id] = append(l.passAt[id], str(res, "revision"))
			}
		}
		encode(w, map[string]any{"data": map[string]any{"result": "created", "run": map[string]any{"external_id": body["external_id"]}, "warnings": []any{}}})
	})
	mux.HandleFunc("/api/v1/sync/reconcile", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		defer l.mu.Unlock()
		l.calls++
		encode(w, map[string]any{"data": map[string]any{"applied": true, "transitions": l.reconcile(), "fails": []any{}}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.unknown = append(l.unknown, r.Method+" "+r.URL.Path)
		l.mu.Unlock()
		http.NotFound(w, r)
	})
}

// lanePosts returns the author posts with the given action.
func (l *laneStore) lanePosts(action string) []map[string]any {
	var out []map[string]any
	for _, p := range l.posts {
		if str(p.body, "action") == action {
			out = append(out, p.body)
		}
	}
	return out
}

// writes counts the author posts, evidence posts and selections — every
// write the CLI sent.
func (l *laneStore) writes() int { return len(l.posts) + len(l.evidence) + len(l.selects) }
