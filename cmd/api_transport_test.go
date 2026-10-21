package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type noticeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

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
	env := &cliEnv{APIURL: srv.URL, token: "expired-bearer"}

	_, _, err := env.get("/api/v1/sync/gates")
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
		"https://legacy.example.test/api":              "modernpath auth --api-url=https://legacy.example.test/api",
	}

	for target, want := range tests {
		got := (&cliEnv{APIURL: target}).credentialRejected().Error()
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
		env := &cliEnv{APIURL: srv.URL, token: "t"}

		got, _, err := env.get("/api/v1/sync/gates")
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
	env := &cliEnv{APIURL: srv.URL, token: "t"}

	status, body, err := env.get("/api/v1/sync/gates")
	if err != nil || status != 200 {
		t.Fatalf("a good call must be untouched: status=%d err=%v", status, err)
	}
	if body == nil {
		t.Fatal("body was dropped")
	}
}

func TestSuccessfulResponseMustContainOneJSONObject(t *testing.T) {
	for name, response := range map[string]string{
		"malformed": `{"data":`,
		"empty":     "",
		"scalar":    `null`,
		"multiple":  `{} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, response)
			}))
			defer srv.Close()

			status, body, err := (&cliEnv{APIURL: srv.URL}).get("/api/test")
			if err == nil {
				t.Fatalf("successful HTTP response with %q body silently succeeded: status=%d body=%v", response, status, body)
			}
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200", status)
			}
		})
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

			env, err := cliEnvLoad()
			if err == nil || env != nil {
				t.Fatal("key-only state must not construct a CLI environment")
			}
			var ce credentialError
			if !errors.As(err, &ce) {
				t.Fatalf("want a credentialError naming the repair, got %v", err)
			}
			if !strings.Contains(err.Error(), "modernpath auth --local") {
				t.Fatalf("error must preserve the configured localhost target: %v", err)
			}
		})
	}
}

func stringPtr(value string) *string { return &value }

func enterTestWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// captureOutput redirects os.Stdout, os.Stderr, and fatih/color's writer — which
// printSuccess uses via color.Green — for the duration of fn, so an accidental
// print of the PIN is actually caught rather than slipping through color.Output.
func captureOutput(t *testing.T, fn func()) string {
	t.Helper()
	oldOut, oldErr, oldColor := os.Stdout, os.Stderr, color.Output
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr, color.Output = w, w, w
	defer func() { os.Stdout, os.Stderr, color.Output = oldOut, oldErr, oldColor }()
	fn()
	_ = w.Close()
	data, _ := io.ReadAll(r)
	return string(data)
}

// jwtWithAudience builds an unsigned token carrying the given audience. Only
// the payload is read locally; the edge and core validate for real.
func jwtWithAudience(t *testing.T, aud ...string) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	return enc(map[string]string{"alg": "RS256", "typ": "JWT"}) + "." +
		enc(map[string]any{"sub": "cli-user", "aud": aud}) + ".not-a-real-signature"
}

func (b *noticeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *noticeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// A slow API response produces a progress notice instead of appearing hung.
func TestSlowAPICallPrintsANotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(60 * time.Millisecond)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	prevAfter, prevTo := slowCallNoticeAfter, slowCallNoticeTo
	var buf noticeBuffer
	slowCallNoticeAfter, slowCallNoticeTo = 5*time.Millisecond, &buf
	defer func() { slowCallNoticeAfter, slowCallNoticeTo = prevAfter, prevTo }()

	env := &cliEnv{APIURL: srv.URL, token: "t"}
	if _, _, err := env.write("POST", "/api/v1/sync/batch", map[string]any{}); err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !strings.Contains(out, "still waiting") || !strings.Contains(out, "/api/v1/sync/batch") {
		t.Fatalf("no notice for a slow call — a slow server and a hung CLI look identical (got %q)", out)
	}
}

func TestFastAPICallPrintsNothing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	prevAfter, prevTo := slowCallNoticeAfter, slowCallNoticeTo
	var buf noticeBuffer
	slowCallNoticeAfter, slowCallNoticeTo = 200*time.Millisecond, &buf
	defer func() { slowCallNoticeAfter, slowCallNoticeTo = prevAfter, prevTo }()

	env := &cliEnv{APIURL: srv.URL, token: "t"}
	if _, _, err := env.get("/api/v1/sync/gates"); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "" {
		t.Fatalf("fast call produced noise: %q — the notice must mark the exception, not the rule", buf.String())
	}
}

// testSystemsServer answers GET /api/systems and counts requests of
// any kind, so a test can assert "no further API request happened."
func testSystemsServer(t *testing.T, status int, systemIDs ...int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/systems" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		systems := make([]map[string]any, len(systemIDs))
		for i, id := range systemIDs {
			systems[i] = map[string]any{"id": id, "name": fmt.Sprintf("system-%d", id), "slug": fmt.Sprintf("sys-%d", id)}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(systems)
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

// captureStderr runs fn with os.Stderr and color.Error redirected to a pipe.
func captureStderr(t *testing.T, fn func(), sink *[]byte) {
	t.Helper()
	r, w, _ := os.Pipe()
	savedErr, savedColor := os.Stderr, color.Error
	os.Stderr, color.Error = w, w
	fn()
	_ = w.Close()
	os.Stderr, color.Error = savedErr, savedColor
	out, _ := io.ReadAll(r)
	*sink = out
}

// expiryWorkspace binds a workspace to apiURL/system 7 with a credential
// carrying the given expiry and refresh token, issued by the test plane so a
// refresh profile resolves. The reachability probe is stubbed to system 7.
func expiryWorkspace(t *testing.T, apiURL string, expiry time.Time, refreshToken string) string {
	t.Helper()
	root := enterTestWorkspace(t)
	writeTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":7}`, apiURL))
	auth := config.Auth{
		Token:        jwtWithClaims(t, map[string]any{"sub": "u1", "exp": expiry.Unix()}),
		ExpiresAt:    expiry.UTC().Format(time.RFC3339),
		RefreshToken: refreshToken,
		Issuer:       zitadel.TestProfile.Issuer,
		WorkspaceID:  "org-1",
		Actor:        "jane@example.com",
	}
	blob, _ := json.Marshal(auth)
	writeTestFile(t, root, ".modernpath/auth.json", string(blob))
	return root
}

func stubSystems(t *testing.T) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	saved := listSystemsFn
	listSystemsFn = func(apiURL, token string) ([]api.System, error) {
		calls.Add(1)
		return []api.System{{ID: 7}}, nil
	}
	t.Cleanup(func() { listSystemsFn = saved })
	return &calls
}

func stubRefresh(t *testing.T, delay time.Duration, newExpiry time.Time) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	saved := refreshCredentialFn
	refreshCredentialFn = func(ctx context.Context, profile zitadel.Profile, refreshToken string) (*zitadel.Token, error) {
		calls.Add(1)
		time.Sleep(delay)
		return &zitadel.Token{AccessToken: "refreshed-bearer", RefreshToken: "rt-2", Expiry: newExpiry, Flow: zitadel.FlowRefresh}, nil
	}
	t.Cleanup(func() { refreshCredentialFn = saved })
	return &calls
}

// (a) Five minutes from expiry with a refresh token: the credential is
// refreshed at the issuer, the store call carries the new bearer, and the
// file holds the new pair.
func TestNearExpiryCredentialIsRefreshedBeforeTheCall(t *testing.T) {
	var bearer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(srv.Close)
	root := expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "rt-1")
	stubSystems(t)
	refreshes := stubRefresh(t, 0, time.Now().Add(12*time.Hour))

	env, err := cliEnvLoad()
	if err != nil {
		t.Fatalf("cliEnvLoad: %v", err)
	}
	if _, _, err := env.get("/api/v1/sync/gates"); err != nil {
		t.Fatalf("call: %v", err)
	}

	if refreshes.Load() != 1 {
		t.Fatalf("want exactly one refresh at the issuer, got %d", refreshes.Load())
	}
	if bearer != "Bearer refreshed-bearer" {
		t.Errorf("the store call must carry the refreshed bearer, got %q", bearer)
	}
	stored := readAuthFile(t, root)
	if stored["token"] != "refreshed-bearer" || stored["refresh_token"] != "rt-2" {
		t.Errorf("auth.json must hold the new pair, got token=%v refresh=%v", stored["token"], stored["refresh_token"])
	}
	if stored["workspace_id"] != "org-1" || stored["actor"] != "jane@example.com" {
		t.Errorf("a refresh must keep the workspace and the actor, got %v", stored)
	}
}

// (b) Five minutes from expiry with no refresh token: exactly one warning
// across two loads, naming the time left and the remedy.
func TestNearExpiryWithoutRefreshWarnsOnce(t *testing.T) {
	srv, _ := testSystemsServer(t, http.StatusOK, 7)
	expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "")
	stubSystems(t)

	var out []byte
	captureStderr(t, func() {
		for i := 0; i < 2; i++ {
			if _, err := cliEnvLoad(); err != nil {
				t.Errorf("load %d: a still-valid credential must load: %v", i, err)
			}
		}
	}, &out)

	text := string(out)
	if n := strings.Count(text, "expires in"); n != 1 {
		t.Fatalf("want exactly one expiry warning across two loads, got %d:\n%s", n, text)
	}
	if !strings.Contains(text, authRepairCommand(srv.URL)) {
		t.Errorf("the warning must name the remedy:\n%s", text)
	}
}

// (c) An expired credential is refused before any request — not the
// reachability probe, not the call — naming the expiry and the remedy.
func TestExpiredCredentialIsRefusedBeforeAnyRequest(t *testing.T) {
	srv, hits := testSystemsServer(t, http.StatusOK, 7)
	expired := time.Now().Add(-time.Minute)
	expiryWorkspace(t, srv.URL, expired, "")
	probes := stubSystems(t)

	env, err := cliEnvLoad()
	if err == nil || env != nil {
		t.Fatal("an expired credential must not load")
	}
	if !strings.Contains(err.Error(), "expired at "+expired.UTC().Format(time.RFC3339)) {
		t.Errorf("the refusal must name the expiry time, got %v", err)
	}
	if !strings.Contains(err.Error(), authRepairCommand(srv.URL)) {
		t.Errorf("the refusal must name the remedy, got %v", err)
	}
	if probes.Load() != 0 || hits.Load() != 0 {
		t.Errorf("no request may leave before the refusal: probes=%d hits=%d", probes.Load(), hits.Load())
	}
}

// (e) Two concurrent loads inside the window refresh exactly once; the loser
// waits for the lock, re-reads, and carries the winner's bearer.
func TestConcurrentLoadsRefreshOnce(t *testing.T) {
	srv, _ := testSystemsServer(t, http.StatusOK, 7)
	expiryWorkspace(t, srv.URL, time.Now().Add(5*time.Minute), "rt-1")
	stubSystems(t)
	refreshes := stubRefresh(t, 200*time.Millisecond, time.Now().Add(12*time.Hour))

	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			env, err := cliEnvLoad()
			errs[i] = err
			if env != nil {
				tokens[i] = env.token
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("load %d: %v", i, err)
		}
	}
	if refreshes.Load() != 1 {
		t.Fatalf("want exactly one refresh under the lock, got %d", refreshes.Load())
	}
	if tokens[0] != "refreshed-bearer" || tokens[1] != "refreshed-bearer" {
		t.Errorf("both loads must carry the refreshed bearer, got %q and %q", tokens[0], tokens[1])
	}
	if _, err := os.Stat(filepath.Join(".modernpath", "auth.lock")); err == nil {
		t.Error("the credential lock must be released after the refresh")
	}
}

// (f) A 401 on a credential whose local expiry has passed says so, instead of
// leaving expiry to be inferred.
func TestCredentialRejectedNamesAPastExpiry(t *testing.T) {
	past := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	env := &cliEnv{APIURL: config.DefaultAPIURL, tokenExpiry: past}

	got := env.credentialRejected().Error()
	if !strings.Contains(got, "expired at "+past.Format(time.RFC3339)) {
		t.Errorf("the 401 message must name the past expiry, got %q", got)
	}
	if !strings.Contains(got, "modernpath auth --sso") {
		t.Errorf("the 401 message must keep the remedy, got %q", got)
	}
}

func chdirTemp(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	old, _ := os.Getwd()
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
	return root
}

func TestGETRejectsPayloadBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()
	var absent map[string]any
	for _, payload := range []any{absent, map[string]any{}} {
		_, _, err := (&cliEnv{APIURL: server.URL}).sendOnce(http.MethodGet, "/api/test", payload)
		if err == nil {
			t.Error("GET with a payload must be rejected")
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("invalid GET reached server %d times", requests.Load())
	}
}

func TestReadAndWriteRequestContracts(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method == http.MethodGet || r.Method == http.MethodDelete {
			if len(body) != 0 {
				t.Errorf("%s body = %q; want empty", r.Method, body)
			}
		} else if string(body) != `{"title":"Keep this write"}` {
			t.Errorf("%s payload changed: %s", r.Method, body)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer server.Close()
	env := &cliEnv{APIURL: server.URL}
	if _, _, err := env.get("/api/test"); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		var payload any
		if method != http.MethodDelete {
			payload = map[string]any{"title": "Keep this write"}
		}
		if _, _, err := env.write(method, "/api/test", payload); err != nil {
			t.Fatal(err)
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, "get", ""} {
		if _, _, err := env.write(method, "/api/test", map[string]any{}); err == nil {
			t.Errorf("write accepted method %q", method)
		}
	}
	if requests.Load() != 5 {
		t.Fatalf("expected only the read and four writes to reach server; got %d", requests.Load())
	}
}
