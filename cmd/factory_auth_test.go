package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// REQ-CROSS-271 — factory transport is bearer-only and every credential repair
// must preserve the currently configured server target.

func rejectingServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestA401AgainstABearerReportsAnExpiredSession(t *testing.T) {
	srv := rejectingServer(t, http.StatusUnauthorized)
	env := &factoryEnv{APIURL: srv.URL, token: "expired-bearer"}

	_, _, err := env.call("GET", "/api/v1/sync/gates", nil)
	if err == nil {
		t.Fatal("a 401 came back as a bare status, so the caller prints `server 401` and the expired session is left to be inferred")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("the message must name the cause: %v", err)
	}
	if !strings.Contains(err.Error(), "modernpath auth") {
		t.Fatalf("the message must name the command that fixes it: %v", err)
	}
}

func TestAuthRepairCommandSelectsTheExactConfiguredTarget(t *testing.T) {
	tests := map[string]string{
		"https://api.modernpath.ai":                    "modernpath auth --sso",
		"https://api.workload.test-plat.modernpath.ai": "modernpath auth --sso --test",
		"http://localhost:4000":                        "modernpath auth --local",
		"https://beta.modernpath.ai":                   "modernpath auth --api-url=https://beta.modernpath.ai",
		"https://legacy.example.test/api":              "modernpath auth --api-url=https://legacy.example.test/api",
	}

	for target, want := range tests {
		got := (&factoryEnv{APIURL: target}).credentialRejected().Error()
		if !strings.Contains(got, want) {
			t.Errorf("credential rejection for %q = %q, want exact repair %q", target, got, want)
		}
	}
}

// Guards, expected green from the start: only 401 changes shape. Every other
// non-200 keeps the existing `server <status>` handling in the callers, which
// depends on the status being returned with no error.
func TestOtherFailuresKeepReturningTheirStatus(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		srv := rejectingServer(t, status)
		env := &factoryEnv{APIURL: srv.URL, token: "t"}

		got, _, err := env.call("GET", "/api/v1/sync/gates", nil)
		if err != nil {
			t.Fatalf("%d must stay a status for the caller to format, got error: %v", status, err)
		}
		if got != status {
			t.Fatalf("want status %d, got %d", status, got)
		}
	}
}

func TestASuccessfulCallIsUnaffected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("bearer header lost: %q", r.Header.Get("Authorization"))
		}
		if got := r.Header.Get("X-API-Key"); got != "" {
			t.Errorf("tenant key header must never be sent, got %q", got)
		}
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()
	env := &factoryEnv{APIURL: srv.URL, token: "t"}

	status, body, err := env.call("GET", "/api/v1/sync/gates", nil)
	if err != nil || status != 200 {
		t.Fatalf("a good call must be untouched: status=%d err=%v", status, err)
	}
	if body == nil {
		t.Fatal("body was dropped")
	}
}

func TestMissingOrUnreadableBearerNeverReadsTheRetiredKey(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(".modernpath", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".modernpath/config.json", []byte(`{"api_url":"http://localhost:4000","system_id":7}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(".modernpath/mp_api_key", []byte("workspace-key"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, authBody := range map[string]*string{
		"missing":    nil,
		"unreadable": stringPtr(`{"token": "trunc`),
	} {
		t.Run(name, func(t *testing.T) {
			authPath := ".modernpath/auth.json"
			_ = os.Remove(authPath)
			if authBody != nil {
				if err := os.WriteFile(authPath, []byte(*authBody), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			env, err := factoryEnvLoad()
			if err == nil || env != nil {
				t.Fatal("key-only state must not construct a factory environment")
			}
			var ce credentialError
			if !errors.As(err, &ce) {
				t.Fatalf("want a credentialError naming the repair, got %v", err)
			}
			if !strings.Contains(err.Error(), "modernpath auth --local") {
				t.Fatalf("error must preserve the configured localhost target: %v", err)
			}
			if strings.Contains(err.Error(), "mp_api_key") || strings.Contains(err.Error(), "API key") {
				t.Fatalf("retired key fallback must not be read or suggested: %v", err)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }
