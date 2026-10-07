package cmd

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSelectionOverviewAliasesRenderAmbiguityAndParkedBlockers(t *testing.T) {
	for _, alias := range []string{"selection", "WORK-SELECTION", "WORK-SELECTION.md"} {
		t.Run(alias, func(t *testing.T) {
			payload := wsSelectionPayload()
			payload["current"] = nil
			payload["ambiguous_current"] = []any{"EPIC-A", "EPIC-B"}
			payload["suspended"] = []any{
				map[string]any{"scope_external_id": "EPIC-P1", "suspended_status": "blocked",
					"suspended_reason": "review", "waiting_on": "human review", "blocker_gate_external_id": "GATE-P1",
					"holder": map[string]any{"name": "Other", "email": "other@example.com"}},
				map[string]any{"scope_external_id": "EPIC-P2", "suspended_status": "deferred",
					"suspended_reason": "postponed", "waiting_on": "vendor", "holder": nil},
			}
			fx := &wsFixture{workSelection: payload}
			env := wsEnv(t, wsServe(t, fx))
			if err := workingSetPull(env, []string{alias}, wsNow); err != nil {
				t.Fatal(err)
			}
			query, err := url.ParseQuery(fx.lastSelectGet)
			if err != nil || query.Get("overview") != "true" {
				t.Fatalf("selection-only pull must request overview: %q (%v)", fx.lastSelectGet, err)
			}
			raw, err := os.ReadFile(filepath.Join(env.Root, workingSetDir, selectionFile))
			if err != nil {
				t.Fatal(err)
			}
			body := string(raw)
			for _, want := range []string{"AMBIGUOUS", "EPIC-A", "EPIC-B", "--piece", "EPIC-P1", "EPIC-P2", "GATE-P1", "vendor", "Other (other@example.com)", "claimable"} {
				if !strings.Contains(body, want) {
					t.Errorf("overview must show %q:\n%s", want, body)
				}
			}
			if strings.Contains(body, "No current selection.") {
				t.Error("ambiguous work must not render as absent")
			}
			for _, request := range fx.requests {
				if !strings.HasPrefix(request, "GET ") {
					t.Errorf("overview wrote to the store: %s", request)
				}
			}
		})
	}
}

func TestSelectionOverviewCurrencyAndRefreshPreserveLocalEdits(t *testing.T) {
	payload := wsSelectionPayload()
	payload["current"] = nil
	payload["ambiguous_current"] = []any{"EPIC-A", "EPIC-B"}
	fx := &wsFixture{workSelection: payload}
	env := wsEnv(t, wsServe(t, fx))
	if err := workingSetPull(env, []string{"selection"}, wsNow); err != nil {
		t.Fatal(err)
	}
	if err := workingSetCheck(env, false, wsNow); err != nil {
		t.Fatalf("unchanged overview must be current: %v", err)
	}
	query, _ := url.ParseQuery(fx.lastSelectGet)
	if query.Get("overview") != "true" {
		t.Fatalf("currency must use the same overview as pull: %q", fx.lastSelectGet)
	}
	payload["ambiguous_current"] = []any{"EPIC-A", "EPIC-B", "EPIC-C"}
	if err := workingSetCheck(env, false, wsNow); err == nil {
		t.Fatal("changed held pieces must stale the overview")
	}
	if err := workingSetCheck(env, true, wsNow); err != nil {
		t.Fatalf("unedited stale overview must refresh: %v", err)
	}
	path := filepath.Join(env.Root, workingSetDir, selectionFile)
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "EPIC-C") {
		t.Fatalf("refreshed overview must show the added piece: %v", err)
	}
	local := string(raw) + "\nLocal note to preserve.\n"
	if err := os.WriteFile(path, []byte(local), 0o644); err != nil {
		t.Fatal(err)
	}
	payload["suspended"] = []any{map[string]any{"scope_external_id": "EPIC-P"}}
	if err := workingSetCheck(env, true, wsNow); err == nil {
		t.Fatal("refresh must report a local-edit conflict")
	}
	raw, err = os.ReadFile(path)
	if err != nil || string(raw) != local {
		t.Fatalf("refresh discarded a local edit: %v", err)
	}
}

func TestSelectionOverviewDoesNotWeakenScopeDependentReads(t *testing.T) {
	fx := &wsFixture{
		selectReadStatus: 422,
		selectReadBody:   map[string]any{"error": "name one current piece"},
	}
	env := wsEnv(t, wsServe(t, fx))
	if _, err := fetchWorkSelectionFor(env, ""); err == nil {
		t.Fatal("ordinary scope resolution must preserve ambiguity refusal")
	}
	query, _ := url.ParseQuery(fx.lastSelectGet)
	if query.Has("overview") {
		t.Fatal("scope-dependent reads must not request overview")
	}
	if err := workingSetPull(env, []string{"selection"}, wsNow); err == nil {
		t.Fatal("an older server's overview refusal must not become empty work")
	}
	if _, err := os.Stat(filepath.Join(env.Root, workingSetDir, selectionFile)); !os.IsNotExist(err) {
		t.Fatalf("refused overview must not write a snapshot: %v", err)
	}
}
