package cmd

// REQ-CROSS-316 (SR-CLI-0087): `process check --phase` renders the server-computed
// decision-table checks and exits non-zero on a served FAIL, naming the unmet
// fact. A pure read: it only GETs /sync/delivery-context.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func dcServer(t *testing.T, checks map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/delivery-context" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"packet_fingerprint": "agg-abcdef012345",
				"process_revision":   strings.Repeat("a", 40),
				"checks":             checks,
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func captureOut(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	fn()
	_ = w.Close()
	os.Stdout = orig
	out, _ := io.ReadAll(r)
	return string(out)
}

func TestREQCROSS316ProcessCheckFailsAndNamesTheFact(t *testing.T) {
	srv := dcServer(t, map[string]any{
		"plan": []map[string]any{{"name": "canonical_sections", "state": "FAIL"}},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processCheck(env, "plan") })

	if err == nil {
		t.Fatalf("a served FAIL must make `process check --phase plan` exit non-zero")
	}
	if !strings.Contains(out, "canonical_sections") || !strings.Contains(out, "FAIL") {
		t.Fatalf("the output did not name the unmet fact: %q", out)
	}
}

func TestREQCROSS316ProcessCheckPassesWithNoFail(t *testing.T) {
	srv := dcServer(t, map[string]any{
		"cold_review": []map[string]any{
			{"name": "independent_verdict", "state": "PASS"},
			{"name": "findings", "state": "UNAVAILABLE"},
		},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	// UNAVAILABLE is never a FAIL — the check passes.
	if err := processCheck(env, "cold_review"); err != nil {
		t.Fatalf("UNAVAILABLE must not fail the phase, got %v", err)
	}
}

func TestREQCROSS316ProcessCheckRejectsUnknownPhase(t *testing.T) {
	env := &factoryEnv{Root: t.TempDir(), APIURL: "http://127.0.0.1:0", SystemID: 4, token: "t"}
	if err := processCheck(env, "not-a-phase"); err == nil {
		t.Fatalf("an unknown phase must be refused before any server call")
	}
}

// REQ-CROSS-317 (SR-CLI-0088): `process next` is a PURE READ — it only GETs
// /sync/delivery-context and prints the derived phase, whether it diverges from
// the declared one, and the owning skill. It never issues a POST.
func TestREQCROSS317ProcessNextIssuesNoPostAndPrintsTheSkill(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"packet_fingerprint": "agg-abcdef012345",
				"process_revision":   strings.Repeat("a", 40),
				"derived_phase":      "cold_review",
				"declared_phase":     "plan",
				"divergence":         true,
				"skill":              "rdd-cold-review",
				"derived_reason":     "verdict_absent",
				"checks":             map[string]any{},
			},
		})
	}))
	t.Cleanup(srv.Close)
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	for _, m := range methods {
		if m != "GET" {
			t.Fatalf("process next is a pure read; it issued a %s", m)
		}
	}
	if len(methods) == 0 {
		t.Fatalf("process next made no server call")
	}
	if !strings.Contains(out, "rdd-cold-review") {
		t.Fatalf("process next did not print the owning skill: %q", out)
	}
	if !strings.Contains(out, "cold_review") || !strings.Contains(out, "plan") {
		t.Fatalf("process next did not print the derived and declared phase: %q", out)
	}
}

// dcDataServer serves an arbitrary delivery-context `data` map for the pure-read
// commands (`process next`).
func dcDataServer(t *testing.T, data map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A nil route is not always "no selection". The decision table also returns a
// nil route with derived_reason "complete" (the loop is finished) or
// "entry_origin_unavailable" (a live selection whose routing evidence is
// missing). Rendering every nil route as "no current selection" hides both a
// finished loop and a stuck one.
func TestREQCROSS317ProcessNextDistinguishesCompleteFromNoSelection(t *testing.T) {
	srv := dcDataServer(t, map[string]any{
		"derived_phase":      "",
		"derived_reason":     "complete",
		"packet_fingerprint": "agg-abcdef012345",
		"process_revision":   strings.Repeat("a", 40),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	if strings.Contains(out, "no current selection") {
		t.Fatalf("a completed loop must not render as 'no current selection': %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "complete") {
		t.Fatalf("a completed loop should say so: %q", out)
	}
}

func TestREQCROSS317ProcessNextSurfacesEntryOriginUnavailable(t *testing.T) {
	srv := dcDataServer(t, map[string]any{
		"derived_phase":      "",
		"derived_reason":     "entry_origin_unavailable",
		"packet_fingerprint": "agg-abcdef012345",
		"process_revision":   strings.Repeat("a", 40),
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	if strings.Contains(out, "no current selection") {
		t.Fatalf("a live selection missing routing evidence must not render as 'no current selection': %q", out)
	}
	if !strings.Contains(out, "entry_origin_unavailable") {
		t.Fatalf("the unresolved routing reason should be surfaced: %q", out)
	}
}

func TestREQCROSS317ProcessNextStillReportsGenuineAbsence(t *testing.T) {
	srv := dcDataServer(t, map[string]any{
		"derived_phase":  "",
		"derived_reason": "no_current_selection",
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	if !strings.Contains(out, "no current selection") {
		t.Fatalf("a genuine absence of a selection should still say so: %q", out)
	}
}

// REQ-CROSS-383 (EPIC-CLI-018): a FAIL on canonical_sections names the missing
// keys the server already computes — one word was the whole answer before.
func TestREQCROSS383ProcessCheckNamesTheMissingSections(t *testing.T) {
	srv := dcDataServer(t, map[string]any{
		"packet_fingerprint": strings.Repeat("c", 64),
		"process_revision":   strings.Repeat("a", 40),
		"checks": map[string]any{
			"plan": []any{map[string]any{"name": "canonical_sections", "state": "FAIL"}},
		},
		"facts_state": "served",
		"facts": map[string]any{
			"sections": map[string]any{
				"complete": false,
				"missing":  []any{"enrichment:REQ-X", "decisions"},
				"required": []any{"reconnaissance", "red_strategy", "decisions", "enrichment:REQ-X"},
			},
		},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processCheck(env, "plan") })
	if err == nil {
		t.Fatal("a FAIL check must still exit non-zero")
	}
	for _, want := range []string{"enrichment:REQ-X", "decisions"} {
		if !strings.Contains(out, want) {
			t.Fatalf("the check must name the missing section %q:\n%s", want, out)
		}
	}
}

// reconcileServer serves a fixed /sync/reconcile response.
func reconcileServer(t *testing.T, data map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/sync/reconcile" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A reconcile that reports FAIL entries (a green-without-RED proof, or a
// rolled-back apply) must exit non-zero, or shell automation reads the failure
// as success — and it must never also print "nothing to do".
func TestREQCROSS317ProcessReconcileFailsWhenServerReportsFails(t *testing.T) {
	srv := reconcileServer(t, map[string]any{
		"applied":     true,
		"transitions": []any{},
		"fails": []map[string]any{
			{"external_id": "SR-CLI-0081", "reason": "passing evidence without a recorded RED"},
		},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processReconcile(env, true) })
	if err == nil {
		t.Fatalf("a reconcile that reports FAIL entries must exit non-zero")
	}
	if !strings.Contains(out, "SR-CLI-0081") || !strings.Contains(out, "recorded RED") {
		t.Fatalf("the reconcile output did not surface the fail: %q", out)
	}
	if strings.Contains(out, "nothing to do") {
		t.Fatalf("a reconcile with fails must not also claim 'nothing to do': %q", out)
	}
}

func TestREQCROSS317ProcessReconcileSucceedsWithNoFails(t *testing.T) {
	srv := reconcileServer(t, map[string]any{
		"applied": true,
		"transitions": []map[string]any{
			{"external_id": "SR-CLI-0081", "kind": "requirement", "from": "TODO", "to": "IN_PROGRESS", "basis": "entry_applied+lower_red"},
		},
		"fails": []any{},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processReconcile(env, true) })
	if err != nil {
		t.Fatalf("a clean reconcile must return nil, got %v", err)
	}
	if !strings.Contains(out, "SR-CLI-0081") {
		t.Fatalf("the applied transition was not printed: %q", out)
	}
}

// REQ-CROSS-345 / cold-review Finding 1: the navigation reads are caller-scoped
// server-side; --piece names which of the caller's own pieces to resolve. It
// must ride to the delivery-context read as ?scope= and to reconcile as `scope`.
//
// RED: the process commands send neither, so a caller holding several pieces
// cannot point `process next`/`reconcile` at the one they mean.
func TestProcessNextCarriesPieceAsScope(t *testing.T) {
	defer func() { processPiece = "" }()
	processPiece = "EPIC-P"

	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"derived_reason": "no_current_selection"}})
	}))
	t.Cleanup(srv.Close)
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	_ = captureOut(t, func() { _ = processNext(env) })

	if !strings.Contains(gotQuery, "scope=EPIC-P") {
		t.Fatalf("--piece must carry to the delivery-context read as ?scope=, got query %q", gotQuery)
	}
}

func TestProcessReconcileCarriesPieceAsScope(t *testing.T) {
	defer func() { processPiece = "" }()
	processPiece = "EPIC-P"

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"applied": false, "transitions": []any{}, "fails": []any{}}})
	}))
	t.Cleanup(srv.Close)
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	_ = captureOut(t, func() { _ = processReconcile(env, false) })

	if gotBody["scope"] != "EPIC-P" {
		t.Fatalf("--piece must carry to reconcile as the scope field, got body %v", gotBody)
	}
}

// Developer-tooling fix (no requirement record): `author trace --fingerprint`
// needs the whole 64-char packet_fingerprint, but `process check`/`next` print
// only the 12-char summary by default. Pasting the truncated value yields an
// inert trace whose evaluated_scope_fingerprint silently never matches. -v adds
// the full values; the default summary must stay truncated so existing parsers
// keep working.
const fullPacketFP = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789" // 64 chars
const fullProcessRev = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c"                       // 40 chars

func TestProcessNextVerboseSurfacesFullFingerprints(t *testing.T) {
	defer func() { verbose = false }()
	srv := dcDataServer(t, map[string]any{
		"derived_phase":      "build",
		"declared_phase":     "build",
		"skill":              "rdd-build",
		"packet_fingerprint": fullPacketFP,
		"process_revision":   fullProcessRev,
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	verbose = true
	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	if !strings.Contains(out, fullPacketFP) {
		t.Fatalf("verbose output must contain the full 64-char packet aggregate, got %q", out)
	}
	if !strings.Contains(out, fullProcessRev) {
		t.Fatalf("verbose output must contain the full process revision, got %q", out)
	}
	// The compact summary line must remain in verbose mode ("packet " + the
	// 12-char prefix is unique to it — the full line reads "full packet_...").
	if !strings.Contains(out, "packet "+shortFP(fullPacketFP)) {
		t.Fatalf("the compact summary line must remain in verbose output, got %q", out)
	}
}

func TestProcessNextDefaultStaysTruncated(t *testing.T) {
	verbose = false // explicit: guard against a prior test leaking the global
	srv := dcDataServer(t, map[string]any{
		"derived_phase":      "build",
		"declared_phase":     "build",
		"skill":              "rdd-build",
		"packet_fingerprint": fullPacketFP,
		"process_revision":   fullProcessRev,
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	var err error
	out := captureOut(t, func() { err = processNext(env) })
	if err != nil {
		t.Fatalf("process next: %v", err)
	}
	if strings.Contains(out, fullPacketFP) {
		t.Fatalf("default output must stay truncated — it leaked the full packet aggregate: %q", out)
	}
	if !strings.Contains(out, shortFP(fullPacketFP)) {
		t.Fatalf("default output should still show the 12-char packet prefix, got %q", out)
	}
}

func TestProcessCheckVerboseSurfacesFullFingerprints(t *testing.T) {
	defer func() { verbose = false }()
	srv := dcDataServer(t, map[string]any{
		"packet_fingerprint": fullPacketFP,
		"process_revision":   fullProcessRev,
		"checks": map[string]any{
			"build": []map[string]any{{"name": "red_first", "state": "PASS"}},
		},
	})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}

	verbose = true
	var err error
	out := captureOut(t, func() { err = processCheck(env, "build") })
	if err != nil {
		t.Fatalf("process check: %v", err)
	}
	if !strings.Contains(out, fullPacketFP) {
		t.Fatalf("verbose `process check` must contain the full 64-char packet aggregate, got %q", out)
	}
	if !strings.Contains(out, fullProcessRev) {
		t.Fatalf("verbose `process check` must contain the full process revision, got %q", out)
	}
}

// REQ-CROSS-408 (EPIC-CLI-020): a failing check says why, from the facts the
// server serves — the CLI computes nothing. PASS lines print no reason.
func TestREQCROSS408ProcessCheckNamesTheReasons(t *testing.T) {
	cases := []struct {
		name   string
		phase  string
		checks []any
		facts  map[string]any
		want   []string
		absent []string
	}{
		{
			name:   "not independent: authored a section",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "independent_verdict", "state": "FAIL"}, map[string]any{"name": "findings", "state": "PASS"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "pass", "independent": false, "trace_external_id": "CR-G",
				"independence_reason": "authored_section", "open_finding_ids": []any{}, "stale_traces": []any{},
			}},
			want: []string{"CR-G", "not independent: the review context authored a section of this scope"},
		},
		{
			name:   "not independent: no review context",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "independent_verdict", "state": "FAIL"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "pass", "independent": false, "trace_external_id": "CR-G", "independence_reason": "no_review_context",
			}},
			want: []string{"CR-G", "not independent: no review context on the trace"},
		},
		{
			name:   "verdict FAIL",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "independent_verdict", "state": "FAIL"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "fail", "independent": true, "trace_external_id": "CR-G",
			}},
			want: []string{"CR-G", "verdict FAIL"},
		},
		{
			name:   "unavailable with a stale trace",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "independent_verdict", "state": "UNAVAILABLE"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "absent", "independent": false,
				"stale_traces": []any{map[string]any{"external_id": "CR-OLD", "aggregate": strings.Repeat("0", 64)}},
			}},
			want: []string{"no cold-review trace at the current packet aggregate", "CR-OLD", strings.Repeat("0", 64)},
		},
		{
			name:   "open findings",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "findings", "state": "FAIL"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "pass", "independent": true, "open_finding_ids": []any{"F-1", "F-2"},
			}},
			want: []string{"F-1, F-2"},
		},
		{
			name:   "no settled completion gate",
			phase:  "completion",
			checks: []any{map[string]any{"name": "completion_gate", "state": "FAIL"}},
			facts: map[string]any{
				"scope":      map[string]any{"external_id": "EPIC-X", "kind": "epic", "status": "IN_REVIEW"},
				"completion": map[string]any{"delivered_current_reconciled": true, "gate_answered": false, "settled_gate_external_id": nil, "members_not_reviewed": []any{}},
			},
			want: []string{"no settled completion gate names EPIC-X"},
		},
		{
			name:   "members not reviewed",
			phase:  "completion",
			checks: []any{map[string]any{"name": "completion_gate", "state": "NOT_APPLICABLE"}},
			facts: map[string]any{
				"scope":      map[string]any{"external_id": "EPIC-X", "kind": "epic", "status": "IN_PROGRESS"},
				"completion": map[string]any{"delivered_current_reconciled": false, "gate_answered": false, "members_not_reviewed": []any{"REQ-X-2"}},
			},
			want: []string{"REQ-X-2"},
		},
		{
			name:   "pass prints no reason",
			phase:  "cold_review",
			checks: []any{map[string]any{"name": "independent_verdict", "state": "PASS"}, map[string]any{"name": "findings", "state": "PASS"}},
			facts: map[string]any{"cold_review": map[string]any{
				"verdict": "pass", "independent": true, "trace_external_id": "CR-G", "open_finding_ids": []any{}, "stale_traces": []any{},
			}},
			absent: []string{"not independent", "CR-G"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := dcDataServer(t, map[string]any{
				"packet_fingerprint": strings.Repeat("c", 64),
				"process_revision":   strings.Repeat("a", 40),
				"checks":             map[string]any{tc.phase: tc.checks},
				"facts_state":        "served",
				"facts":              tc.facts,
			})
			env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
			out := captureOut(t, func() { _ = processCheck(env, tc.phase) })
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("want %q under the check line:\n%s", w, out)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(out, a) {
					t.Errorf("a PASS line must print no reason (%q):\n%s", a, out)
				}
			}
		})
	}
}
