package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// REQ-CROSS-392 (EPIC-CLI-019) — a work selection carries a lane (planned
// work or a customer-blocking defect) that process next, your-move and the
// session brief render, so a session learns from the store that the clock
// is different.

// (a) --lane defect rides the take; the default sends no key.
func TestWorkingSetSelectPostsTheLane(t *testing.T) {
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetSelect(env, wsSelectOpts{scope: "EPIC-L", kind: "epic", phase: "plan", lane: "defect"}, wsNow); err != nil {
		t.Fatalf("select: %v", err)
	}
	if fx.lastSelectPost["lane"] != "defect" {
		t.Fatalf("the take must post the lane, got %v", fx.lastSelectPost)
	}
	if err := workingSetSelect(env, wsSelectOpts{scope: "EPIC-M", kind: "epic", phase: "plan"}, wsNow); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, present := fx.lastSelectPost["lane"]; present {
		t.Fatalf("an unset lane must post no key (an older server sees nothing new), got %v", fx.lastSelectPost)
	}
}

// An unknown lane is refused naming both values, before any post.
func TestWorkingSetSelectRefusesAnUnknownLane(t *testing.T) {
	fx := &wsFixture{workSelection: wsSelectionPayload()}
	srv := wsServe(t, fx)
	cobraWorkspace(t, srv)

	_, err := runRoot(t, "working-set", "select", "EPIC-L", "--kind", "epic", "--phase", "plan", "--lane", "urgent")
	if err == nil {
		t.Fatal("an unknown lane must be refused")
	}
	for _, want := range []string{"planned", "defect"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got %v", want, err)
		}
	}
	if fx.lastSelectPost != nil {
		t.Errorf("nothing may be posted for a refused lane, got %v", fx.lastSelectPost)
	}
}

// The selection snapshot renders the lane.
func TestPullSelectionRendersTheLane(t *testing.T) {
	payload := wsSelectionPayload()
	payload["current"].(map[string]any)["lane"] = "defect"
	fx := &wsFixture{workSelection: payload}
	env := wsEnv(t, wsServe(t, fx))

	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatalf("pull selection: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(env.Root, workingSetDir, selectionFile))
	if !strings.Contains(string(raw), "- **Lane:** customer-blocking defect") {
		t.Errorf("WORK-SELECTION.md must render the lane:\n%s", raw)
	}
}

func laneDCServer(t *testing.T, lane string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"packet_fingerprint": "agg-abcdef012345", "process_revision": strings.Repeat("a", 40),
			"derived_phase": "build", "declared_phase": "build", "derived_reason": "member_evidence_not_current_changed", "skill": "rdd-build",
		}
		if lane != "" {
			data["lane"] = lane
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// (b) process next prints the lane line for a defect and nothing for planned.
func TestProcessNextPrintsTheDefectLane(t *testing.T) {
	env := wsEnv(t, laneDCServer(t, "defect"))
	out := captureOut(t, func() { _ = processNext(env) })
	if !strings.Contains(out, "lane:           customer-blocking defect") {
		t.Errorf("a defect lane must be printed after the declared phase:\n%s", out)
	}

	env = wsEnv(t, laneDCServer(t, "planned"))
	out = captureOut(t, func() { _ = processNext(env) })
	if strings.Contains(out, "lane:") {
		t.Errorf("planned work prints no lane line:\n%s", out)
	}
}

// (c) The brief marks a defect-lane item and its held-piece line carries the
// lane (rendered from the held-work read since REQ-CROSS-415, D11).
func TestBriefMarksDefectLaneItems(t *testing.T) {
	feed := feedFixture()
	items := feed["items"].([]any)
	items[1].(map[string]any)["lane"] = "defect"
	fx := &wsFixture{feed: feed, held: []any{
		heldPieceFixture("EPIC-D-001", "build", map[string]any{"lane": "defect"}),
	}}
	env := wsEnv(t, wsServe(t, fx))

	var out bytes.Buffer
	runBriefHook("SessionStart", strings.NewReader(`{"session_id":"lane-1","hook_event_name":"SessionStart","source":"startup"}`), &out, time.Second, hookLoader(env))
	text := out.String()
	if !strings.Contains(text, "REQ-CROSS-274") || !strings.Contains(text, "⚑ defect") {
		t.Errorf("the defect-lane item must carry the chip:\n%s", text)
	}
	if !strings.Contains(text, "lane: customer-blocking defect") {
		t.Errorf("the Loop line must carry the lane:\n%s", text)
	}
}

// (b') The lane is a fact of the selection, not of the route: a defect-lane
// selection whose route cannot be derived (entry_origin_unavailable, or a
// finished loop) still prints the lane line (F-CLI019-CR-01, found by the
// completion live check on a single SR that needs an epic at entry).
func TestProcessNextPrintsTheDefectLaneWithoutARoute(t *testing.T) {
	for _, reason := range []string{"entry_origin_unavailable", "complete"} {
		srv := dcDataServer(t, map[string]any{
			"derived_phase":      "",
			"derived_reason":     reason,
			"lane":               "defect",
			"packet_fingerprint": "agg-abcdef012345",
			"process_revision":   strings.Repeat("a", 40),
		})
		env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
		out := captureOut(t, func() { _ = processNext(env) })
		if !strings.Contains(out, "lane:           customer-blocking defect") {
			t.Errorf("reason %s: a defect lane must be printed even when no route derives:\n%s", reason, out)
		}
	}
}
