package cmd

// REQ-CROSS-339 (EPIC-CLI-010): `factory release activate <slug>` is the
// attributed, system-wide activation verb — distinct from the workspace-local
// `release use` stamp. It POSTs the release_activate authoring action carrying
// an explicit USER: source (never inheriting the agent actor), and refuses
// without one.
//
// RED first: the verb and its activateRelease helper do not exist yet.

import (
	"encoding/json"
	"fmt"
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

	if err := activateRelease(env, "modernpath-v1-09", "USER:2026-09-11:make v1-09 active", "2468", true, "cutover"); err != nil {
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
	if got["close_current"] != true || got["reason"] != "cutover" {
		t.Fatalf("activation parity fields = %#v", got)
	}
}

func TestReleaseActivateRefusesWithoutUserSource(t *testing.T) {
	var got map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/author", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"release_activation": map[string]any{"slug": "modernpath-v1-09", "status": "active", "source": "USER:2026-09-22:agent activated modernpath-v1-09"},
		}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	if err := activateRelease(wsEnv(t, srv), "modernpath-v1-09", "", "2468", false, ""); err != nil {
		t.Fatalf("activation without an explicit source must use the server default: %v", err)
	}
	if _, sent := got["source"]; sent {
		t.Fatalf("source = %v; an omitted --source must let the server generate the attributable source", got["source"])
	}
}

func TestReleaseActivateOffersParityFlags(t *testing.T) {
	for _, flag := range []string{"close-current", "reason", "pin-stdin"} {
		if factoryReleaseActivateCmd.Flags().Lookup(flag) == nil {
			t.Errorf("factory release activate is missing --%s", flag)
		}
	}
}

func TestActivationPinUsesTheSelectedWorkspaceStatusWithoutResettingASuppliedPin(t *testing.T) {
	var posts int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/compliance/pin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": true}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	got, err := activationPin(wsEnv(t, srv), "2468", false)
	if err != nil || got != "2468" {
		t.Fatalf("activationPin = %q, %v", got, err)
	}
	if posts != 0 {
		t.Fatalf("a supplied PIN verifies only; set endpoint was called %d times", posts)
	}
}

func TestActivationPinReadsExistingPinFromStdin(t *testing.T) {
	saved := activationPinFromStdin
	activationPinFromStdin = func() (string, error) { return "2468", nil }
	t.Cleanup(func() { activationPinFromStdin = saved })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/compliance/pin", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": true}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	got, err := activationPin(wsEnv(t, srv), "", true)
	if err != nil || got != "2468" {
		t.Fatalf("--pin-stdin activation PIN = %q, %v", got, err)
	}
}

func TestActivationPinSetsUpOnlyAfterAnUnsetStatusProbe(t *testing.T) {
	savedSetup, savedTerminal := activationPinSetup, stdinIsTerminal
	activationPinSetup = func() (string, error) { return "2468", nil }
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { activationPinSetup, stdinIsTerminal = savedSetup, savedTerminal })
	var gotSet string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/compliance/pin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": false}})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotSet, _ = body["pin"].(string)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": true}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	got, err := activationPin(wsEnv(t, srv), "", false)
	if err != nil || got != "2468" {
		t.Fatalf("interactive setup PIN = %q, %v", got, err)
	}
	if gotSet != "2468" {
		t.Fatalf("setup posted PIN %q, want 2468", gotSet)
	}
}

func TestActivationPinRefusesNoninteractiveUnsetAndProbeFailureWithoutSetup(t *testing.T) {
	t.Run("noninteractive unset", func(t *testing.T) {
		savedTerminal := stdinIsTerminal
		stdinIsTerminal = func() bool { return false }
		t.Cleanup(func() { stdinIsTerminal = savedTerminal })
		var posts int
		mux := http.NewServeMux()
		mux.HandleFunc("/api/compliance/pin", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"has_pin": false}})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		_, err := activationPin(wsEnv(t, srv), "", true)
		if err == nil || !strings.Contains(err.Error(), "factory pin set --pin-stdin") {
			t.Fatalf("noninteractive unset error = %v", err)
		}
		if posts != 0 {
			t.Fatalf("unset noninteractive activation set PIN %d times", posts)
		}
	})
	t.Run("probe failure", func(t *testing.T) {
		var posts int
		mux := http.NewServeMux()
		mux.HandleFunc("/api/compliance/pin", func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				posts++
				t.Fatal("probe failure must never begin PIN setup")
			}
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprint(w, `{}`)
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		_, err := activationPin(wsEnv(t, srv), "", false)
		if err == nil || !strings.Contains(err.Error(), "PIN status failed") {
			t.Fatalf("probe error = %v", err)
		}
		if posts != 0 {
			t.Fatalf("probe failure posted PIN %d times", posts)
		}
	})
}

// A malformed successful response must not be interpreted as permission to set a PIN.
func TestActivationPinRejectsMalformedStatusWithoutSetup(t *testing.T) {
	savedSetup, savedTerminal := activationPinSetup, stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	var setups int
	activationPinSetup = func() (string, error) { setups++; return "2468", nil }
	t.Cleanup(func() { activationPinSetup, stdinIsTerminal = savedSetup, savedTerminal })
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	_, err := activationPin(wsEnv(t, srv), "", false)
	if err == nil || setups != 0 || posts != 0 {
		t.Fatalf("malformed probe: error=%v, setup prompts=%d, PIN writes=%d; want refusal before setup", err, setups, posts)
	}
}
