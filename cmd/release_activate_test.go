package cmd

// REQ-CROSS-339 (EPIC-CLI-010): `factory release activate <slug>` is the
// attributed, tenant-wide activation verb — distinct from the workspace-local
// `release use` stamp. It POSTs the release_activate authoring action carrying
// an explicit USER: source (never inheriting the agent actor), and refuses
// without one.
//
// RED first: the verb and its activateRelease helper do not exist yet.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReleaseActivatePostsTheAttributedCall(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"release_activation": map[string]any{"slug": "modernpath-v1-09", "status": "active"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	env := wsEnv(t, srv)

	if err := activateRelease(env, "modernpath-v1-09", "USER:2026-09-11:make v1-09 active", "2468"); err != nil {
		t.Fatalf("activateRelease failed: %v", err)
	}
	if got["action"] != "release_activate" {
		t.Fatalf("action = %v, want release_activate", got["action"])
	}
	if got["slug"] != "modernpath-v1-09" {
		t.Fatalf("slug = %v, want modernpath-v1-09", got["slug"])
	}
	if got["source"] != "USER:2026-09-11:make v1-09 active" {
		t.Fatalf("source = %v — the USER: source must be attached explicitly, not the agent actor", got["source"])
	}
	if got["pin"] != "2468" {
		t.Fatalf("pin = %v — the release PIN must be forwarded to the guarded write", got["pin"])
	}
}

func TestReleaseActivateRefusesWithoutUserSource(t *testing.T) {
	prev := releaseActivateSource
	releaseActivateSource = "not-a-user-ref"
	t.Cleanup(func() { releaseActivateSource = prev })

	err := factoryReleaseActivateCmd.RunE(factoryReleaseActivateCmd, []string{"modernpath-v1-09"})
	if err == nil || !strings.Contains(err.Error(), "USER:") {
		t.Fatalf("expected a USER: source refusal before any server call, got %v", err)
	}
}
