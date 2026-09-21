package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-379 (EPIC-CLI-018): when the caller holds several current pieces
// and names none, the server answers 409 with the held list; every scoped
// read and write names those pieces and the --piece remedy instead of
// printing "no current selection" (the opposite of the truth) or "nothing to
// do" against a scope it never resolved.

func ambiguityServer(t *testing.T, pieces []string) *httptest.Server {
	t.Helper()
	body := map[string]any{
		"error":  map[string]any{"reason": "you hold several current selections (" + strings.Join(pieces, ", ") + ") — name one with ?scope=<id>"},
		"pieces": pieces,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func assertNamesPieces(t *testing.T, err error, out string) {
	t.Helper()
	if err == nil {
		t.Fatalf("an ambiguous scope must be a refusal, not a success; output %q", out)
	}
	text := err.Error() + "\n" + out
	for _, want := range []string{"EPIC-A", "EPIC-B", "--piece"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the refusal must name the held pieces and the remedy (%q), got %v / %q", want, err, out)
		}
	}
	if strings.Contains(text, "no current selection") || strings.Contains(text, "nothing to do") {
		t.Fatalf("an ambiguous scope must never read as absent or idle: %v / %q", err, out)
	}
}

func TestREQCROSS379ProcessNextNamesTheHeldPieces(t *testing.T) {
	srv := ambiguityServer(t, []string{"EPIC-A", "EPIC-B"})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	var err error
	out := captureOut(t, func() { err = processNext(env) })
	assertNamesPieces(t, err, out)
}

func TestREQCROSS379ProcessCheckNamesTheHeldPieces(t *testing.T) {
	srv := ambiguityServer(t, []string{"EPIC-A", "EPIC-B"})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	var err error
	out := captureOut(t, func() { err = processCheck(env, "plan") })
	assertNamesPieces(t, err, out)
}

func TestREQCROSS379ProcessReconcileApplyRefusesByName(t *testing.T) {
	srv := ambiguityServer(t, []string{"EPIC-A", "EPIC-B"})
	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	var err error
	out := captureOut(t, func() { err = processReconcile(env, true) })
	assertNamesPieces(t, err, out)
}

// factory status stays a binding command (it must run on a malformed auth.json,
// see TestFactoryStatusDoesNotLoadAuthentication); with a loadable token it
// lists every held piece, best-effort, and says "not signed in" otherwise.
func TestREQCROSS379FactoryStatusListsEveryHeldPiece(t *testing.T) {
	srv := ambiguityServer(t, []string{"EPIC-A", "EPIC-B"})
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"`+srv.URL+`","system_id":7}`)
	writeFactoryTestFile(t, root, ".modernpath/auth.json", `{"token":"t"}`)

	var err error
	out := captureOut(t, func() { err = factoryStatusCmd.RunE(factoryStatusCmd, nil) })
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, want := range []string{"EPIC-A", "EPIC-B"} {
		if !strings.Contains(out, want) {
			t.Fatalf("status must list every held piece (%q):\n%s", want, out)
		}
	}
}

func TestREQCROSS379FactoryStatusWithoutATokenSaysSo(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://localhost:4000","system_id":7}`)
	writeFactoryTestFile(t, root, ".modernpath/auth.json", `{"token":"truncated`)

	var err error
	out := captureOut(t, func() { err = factoryStatusCmd.RunE(factoryStatusCmd, nil) })
	if err != nil {
		t.Fatalf("status must still run on a malformed auth.json: %v", err)
	}
	if !strings.Contains(out, "not signed in") {
		t.Fatalf("status must say the held pieces are unreadable without a token:\n%s", out)
	}
}

// A named --piece the caller does not hold answers 200 with a nil current
// selection; the pull must refuse instead of writing "No current selection."
// over a snapshot that was true a minute ago.
func TestREQCROSS379PullSelectionNeverOverwritesOnAnUnheldPiece(t *testing.T) {
	defer func() { wsPiece = "" }()
	wsPiece = "EPIC-NOPE"
	fx := &wsFixture{workSelection: map[string]any{
		"active_release": []any{map[string]any{"slug": "modernpath-v1-09", "status": "active"}},
		"current":        nil,
		"suspended":      []any{},
		"history":        []any{},
	}}
	env := wsEnv(t, wsServe(t, fx))
	dir := filepath.Join(env.Root, workingSetDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := "# WORK-SELECTION — working-set snapshot\n\nEPIC-B3 was here\n"
	if err := os.WriteFile(filepath.Join(dir, "WORK-SELECTION.md"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	err := workingSetPull(env, []string{"selection"}, wsNow)
	if err == nil || !strings.Contains(err.Error(), "EPIC-NOPE") {
		t.Fatalf("an unheld --piece must be refused by name, got %v", err)
	}
	raw, rerr := os.ReadFile(filepath.Join(dir, "WORK-SELECTION.md"))
	if rerr != nil || string(raw) != existing {
		t.Fatalf("the snapshot must be left untouched on a refusal:\n%s", raw)
	}
}
