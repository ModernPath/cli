package cmd

// REQ-CROSS-407 (EPIC-CLI-020) — the release-selection read resolves the gate
// by what it is (purpose release_selection, exact_scope naming the release,
// answered with approve chosen), newest first, not by the fixed id; the
// active-without-gate state has a name and a remedy on the preflight and on
// factory status; pin_required and pin_locked say whose PIN and what to do.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// releaseSelectionGateN is an approved release_selection gate under the
// successor id GATE-RELEASE-<slug>-<n>, answered at the given time.
func releaseSelectionGateN(slug string, n int, source, answeredAt string) map[string]any {
	g := releaseSelectionGate(slug, source)
	g["external_id"] = fmt.Sprintf("GATE-RELEASE-%s-%d", slug, n)
	g["answered_at"] = answeredAt
	return g
}

const missingReleaseGateRemedy = "no recorded selection gate on this system; run factory release activate modernpath-v1-09 --source USER:… here to record one"

// S5 — two approved gates for the release, -2 newer: the read returns -2.
func TestReleaseReadTakesTheNewestApprovedGate(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{
			releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04:first"),
			releaseSelectionGateN("modernpath-v1-09", 2, "USER:2026-09-14:second", "2026-09-14T10:00:00Z"),
		},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** USER:2026-09-14:second (gate GATE-RELEASE-modernpath-v1-09-2)") {
		t.Errorf("the newest approved gate must be the source:\n%s", got)
	}
}

// S4 — a superseded bare gate beside its approved successor: the successor
// resolves, although its id is not the fixed one.
func TestReleaseReadResolvesASuccessorUnderAnotherID(t *testing.T) {
	old := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04:first")
	old["state"] = "superseded"
	old["successor_external_id"] = "GATE-RELEASE-modernpath-v1-09-2"
	fx := &wsFixture{
		workSelection:   oneActiveSelection("modernpath-v1-09"),
		supersededGates: []any{old},
		answeredGates:   []any{releaseSelectionGateN("modernpath-v1-09", 2, "USER:2026-09-14:second", "2026-09-14T10:00:00Z")},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** USER:2026-09-14:second (gate GATE-RELEASE-modernpath-v1-09-2)") {
		t.Errorf("the approved successor must resolve:\n%s", got)
	}
}

// C2 — the binding is the release in exact_scope; a gate naming another
// release never binds. Review follow-through (PR #487, finding 4): a gate
// written before the scope token existed — the bare id and no release in its
// scope — still binds by its id; a successor id without the token does not.
func TestReleaseReadBindsByScopeOrByTheBareID(t *testing.T) {
	cases := []struct {
		name  string
		id    string
		scope []any
		bound bool
	}{
		{"another release in the scope", "GATE-RELEASE-modernpath-v1-09", []any{"release:other"}, false},
		{"bare id, no release in the scope", "GATE-RELEASE-modernpath-v1-09", []any{"system:4"}, true},
		{"successor id, no release in the scope", "GATE-RELEASE-modernpath-v1-09-2", []any{"system:4"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04")
			g["external_id"] = tc.id
			g["exact_scope"] = tc.scope
			fx := &wsFixture{
				workSelection: oneActiveSelection("modernpath-v1-09"),
				answeredGates: []any{g},
			}
			env := wsEnv(t, wsServe(t, fx))
			markStoreBacked(t, env.Root)
			if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
				t.Fatalf("pull selection failed: %v", err)
			}
			got := readWorkSelection(t, env)
			if tc.bound && !strings.Contains(got, "Active release source:** USER:2026-08-04 (gate "+tc.id+")") {
				t.Errorf("the gate must bind:\n%s", got)
			}
			if !tc.bound && !strings.Contains(got, "Active release source:** MISSING") {
				t.Errorf("the gate must not bind:\n%s", got)
			}
		})
	}
}

// C4 — the active-without-gate state is named on the preflight with its remedy.
func TestReleasePreflightNamesTheMissingGateAndTheRemedy(t *testing.T) {
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{releaseSelectionGate("other-release", "USER:2026-08-04")},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "Active release source:** MISSING") || !strings.Contains(got, missingReleaseGateRemedy) {
		t.Errorf("the preflight must name the missing gate and the remedy:\n%s", got)
	}
}

// statusWorkspace binds a temp workspace to the fixture server as system 4 and
// signs it in, so factory status can read the store.
func statusWorkspace(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	root := chdirTemp(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":4}`, srv.URL))
	writeInitAuth(t, root, "t", time.Now().Add(time.Hour))
	markStoreBacked(t, root)
	return root
}

// S2/S4 — factory status prints the active release with its source, or the
// missing-gate line with the remedy.
func TestFactoryStatusPrintsTheActiveReleaseSourceOrTheMissingGate(t *testing.T) {
	t.Run("with a gate", func(t *testing.T) {
		fx := &wsFixture{
			workSelection: oneActiveSelection("modernpath-v1-09"),
			answeredGates: []any{releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04:chosen")},
		}
		statusWorkspace(t, wsServe(t, fx))
		out, err := runCapturing(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) })
		if err != nil {
			t.Fatalf("factory status: %v\n%s", err, out)
		}
		if !strings.Contains(out, "active release modernpath-v1-09") || !strings.Contains(out, "USER:2026-08-04:chosen (gate GATE-RELEASE-modernpath-v1-09)") {
			t.Errorf("factory status must print the active release and its source:\n%s", out)
		}
	})
	t.Run("without a gate", func(t *testing.T) {
		fx := &wsFixture{
			workSelection: oneActiveSelection("modernpath-v1-09"),
			answeredGates: []any{},
		}
		statusWorkspace(t, wsServe(t, fx))
		out, err := runCapturing(t, func() error { return factoryStatusCmd.RunE(factoryStatusCmd, nil) })
		if err != nil {
			t.Fatalf("factory status: %v\n%s", err, out)
		}
		if !strings.Contains(out, "active release modernpath-v1-09 — "+missingReleaseGateRemedy) {
			t.Errorf("factory status must name the missing gate and the remedy:\n%s", out)
		}
	})
}

// S3 — pin_required names the holder, Mission Control and --pin; pin_locked
// names the lockout and no duration.
func TestReleaseActivateRendersThePinRefusals(t *testing.T) {
	serve := func(t *testing.T, status int, reason string) *factoryEnv {
		t.Helper()
		mux := http.NewServeMux()
		mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"reason": reason}})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		return wsEnv(t, srv)
	}

	t.Run("pin_required", func(t *testing.T) {
		env := serve(t, http.StatusForbidden, "pin_required")
		err := activateRelease(env, "modernpath-v1-09", "USER:2026-09-14:activate", "")
		if err == nil {
			t.Fatal("a 403 pin_required must refuse")
		}
		for _, want := range []string{"pin_required", "release PIN", "signed-in", "Mission Control", "--pin"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal must carry %q, got: %v", want, err)
			}
		}
	})
	t.Run("pin_locked", func(t *testing.T) {
		env := serve(t, http.StatusTooManyRequests, "pin_locked")
		err := activateRelease(env, "modernpath-v1-09", "USER:2026-09-14:activate", "0000")
		if err == nil {
			t.Fatal("a 429 pin_locked must refuse")
		}
		for _, want := range []string{"pin_locked", "locked", "later"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("the refusal must carry %q, got: %v", want, err)
			}
		}
		if regexp.MustCompile(`\d+\s*(minute|second|hour)`).MatchString(err.Error()) {
			t.Errorf("the refusal must state no duration — the server serves none: %v", err)
		}
	})
}

// C2 — a gate is pullable by its own id, with its Fingerprint line.
func TestWorkingSetPullServesAGateByID(t *testing.T) {
	g := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04:chosen")
	g["fingerprint"] = "abc123"
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{g},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"GATE-RELEASE-modernpath-v1-09"}, wsNow); err != nil {
		t.Fatalf("pull of a gate by id failed: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, "GATE-RELEASE-modernpath-v1-09.md"))
	if err != nil {
		t.Fatalf("the gate must be written as its own snapshot: %v", err)
	}
	for _, want := range []string{"## GATE GATE-RELEASE-modernpath-v1-09", "- **Fingerprint:** abc123", "USER:2026-08-04:chosen"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("want %q in the gate snapshot:\n%s", want, raw)
		}
	}
}

// Nit 12 — answered_at is compared as a time, not as text: the same second
// written with and without fractional digits orders by the id tie-break, and a
// later second always wins regardless of its spelling.
func TestReleaseReadOrdersAnsweredAtAsTime(t *testing.T) {
	older := releaseSelectionGate("modernpath-v1-09", "USER:2026-08-04:older")
	older["answered_at"] = "2026-09-14T10:00:01Z"
	newer := releaseSelectionGateN("modernpath-v1-09", 2, "USER:2026-09-14:newer", "2026-09-14T10:00:01.500000Z")
	fx := &wsFixture{
		workSelection: oneActiveSelection("modernpath-v1-09"),
		answeredGates: []any{older, newer},
	}
	env := wsEnv(t, wsServe(t, fx))
	markStoreBacked(t, env.Root)
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection failed: %v", err)
	}
	got := readWorkSelection(t, env)
	if !strings.Contains(got, "USER:2026-09-14:newer (gate GATE-RELEASE-modernpath-v1-09-2)") {
		t.Errorf("the later second must win over a longer-spelled earlier one:\n%s", got)
	}
}
