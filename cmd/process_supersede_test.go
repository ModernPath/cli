package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// REQ-CROSS-318 (SR-CLI-0089): `process supersede <item> --reason` posts the
// `supersede` author action (report mode by default). RED: processSupersede
// does not exist, so the package fails to build.
func TestREQCROSS318ProcessSupersedePostsTheAction(t *testing.T) {
	var gotAction, gotID, gotReason, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/author" {
			gotMethod = r.Method
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			gotAction, _ = m["action"].(string)
			gotReason, _ = m["reason"].(string)
			if rec, ok := m["record"].(map[string]any); ok {
				gotID, _ = rec["external_id"].(string)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"supersede":{"mode":"report","would_demote":["SR-X"],"would_supersede":[],"would_stale_results":0,"applied":false}}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	if err := processSupersede(env, "SR-X", "no longer valid", "", "", false); err != nil {
		t.Fatalf("processSupersede: %v", err)
	}
	if gotMethod != "POST" {
		t.Fatalf("method = %q, want POST", gotMethod)
	}
	if gotAction != "supersede" {
		t.Fatalf("action = %q, want supersede", gotAction)
	}
	if gotID != "SR-X" {
		t.Fatalf("external_id = %q, want SR-X", gotID)
	}
	if gotReason != "no longer valid" {
		t.Fatalf("reason = %q, want 'no longer valid'", gotReason)
	}
}

// BACKLOG-TOOL-39: `process supersede <item> --apply --source USER:… --supersedes USER:…`
// posts the scoped-enforce request that reopens ONE shipped item — scoped_enforce
// true, and the attributed source + prior decision ride the body.
func TestBACKLOGTOOL39ScopedSupersedePostsEnforceAndSources(t *testing.T) {
	var gotScoped bool
	var gotSource, gotSupersedes string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/sync/author" {
			body, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			gotScoped, _ = m["scoped_enforce"].(bool)
			gotSource, _ = m["source"].(string)
			gotSupersedes, _ = m["supersedes"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"supersede":{"mode":"enforce","would_demote":["REQ-UI-061"],"would_supersede":[],"would_stale_results":1,"applied":true,"source":"USER:2026-09-16:x","supersedes":"USER:2026-08-29","attribution_recorded":true}}}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	err := processSupersede(env, "REQ-UI-061", "reverse §061.6", "USER:2026-09-16:x", "USER:2026-08-29", true)
	if err != nil {
		t.Fatalf("processSupersede: %v", err)
	}
	if !gotScoped {
		t.Fatalf("scoped_enforce = %v, want true when --apply is set", gotScoped)
	}
	if gotSource != "USER:2026-09-16:x" {
		t.Fatalf("source = %q, want the attributed USER: source", gotSource)
	}
	if gotSupersedes != "USER:2026-08-29" {
		t.Fatalf("supersedes = %q, want the prior decision", gotSupersedes)
	}
}

// BACKLOG-TOOL-39: --apply without a USER: source is refused locally, before any
// call — a scoped demotion must be attributed.
func TestBACKLOGTOOL39ScopedSupersedeRequiresSourceLocally(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(200)
	}))
	defer srv.Close()

	env := &factoryEnv{Root: t.TempDir(), APIURL: srv.URL, SystemID: 4, token: "t"}
	err := processSupersede(env, "REQ-UI-061", "reverse §061.6", "", "", true)
	if err == nil || !strings.Contains(err.Error(), "requires --source USER:") {
		t.Fatalf("--apply without --source must be refused locally; got: %v", err)
	}
	if called {
		t.Fatalf("no request should be sent when --apply lacks a USER: source")
	}
}
