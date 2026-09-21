package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
)

// A live credential on a platform host: the audience pre-check attaches the
// bearer, the edge answers /api/ex/systems 200, and the online check confirms
// it — carrying the stored workspace identity into the report.
func TestAuthStatusReportsAuthenticatedAgainstThePlatformEdge(t *testing.T) {
	const projectID = "999888777666555444"
	server, records := platformEdge(t, projectID)

	st := evaluateAuthStatus(server.URL, &config.Auth{
		Token:         jwtWithAudience(t, projectID),
		WorkspaceID:   "org-1",
		WorkspaceName: "Acme",
	}, false)

	if !st.Authenticated || !st.CheckedOnline {
		t.Fatalf("want authenticated online, got %+v", st)
	}
	if st.WorkspaceID != "org-1" || st.WorkspaceName != "Acme" {
		t.Errorf("workspace identity not carried into the report: %+v", st)
	}
	probes := records()
	if len(probes) != 1 || probes[0].path != "/api/ex/systems" || !probes[0].authorized {
		t.Errorf("want one authorized probe to /api/ex/systems, got %+v", probes)
	}
}

// No stored credential is not authenticated, and short-circuits before any
// network call.
func TestAuthStatusNoCredential(t *testing.T) {
	st := evaluateAuthStatus(config.DefaultAPIURL, &config.Auth{}, false)

	if st.Authenticated || st.TokenPresent {
		t.Fatalf("empty credential must be unauthenticated, got %+v", st)
	}
	if !strings.Contains(st.Detail, "no stored credential") {
		t.Errorf("detail must name the missing credential, got %q", st.Detail)
	}
}

// A token the server rejects (401) is expired-or-invalid, reported after the
// online check actually ran.
func TestAuthStatusReportsExpiredCredential(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	st := evaluateAuthStatus(srv.URL, &config.Auth{Token: "stale-opaque-token"}, false)

	if st.Authenticated {
		t.Fatalf("a 401 must not be authenticated, got %+v", st)
	}
	if !st.CheckedOnline {
		t.Error("the online check must have run for a present token")
	}
	if !strings.Contains(st.Detail, "expired or invalid") {
		t.Errorf("detail must explain the 401, got %q", st.Detail)
	}
}

// --offline reports a present credential without contacting the server.
func TestAuthStatusOfflineMakesNoNetworkCall(t *testing.T) {
	srv, hits := systemsServer(t, 7)

	st := evaluateAuthStatus(srv.URL, &config.Auth{Token: "opaque-but-present"}, true)

	if !st.Authenticated || st.CheckedOnline {
		t.Fatalf("offline must report present-but-unverified, got %+v", st)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("offline must make no network call, got %d hits", got)
	}
	if !strings.Contains(st.Detail, "not verified") {
		t.Errorf("offline detail must disclaim verification, got %q", st.Detail)
	}
}

// Even offline, a JWT that lacks the host's project audience is known-bad
// locally (REQ-CROSS-291) — caught without a round trip.
func TestAuthStatusOfflineCatchesStaleAudience(t *testing.T) {
	server, records := platformEdge(t, "right-project")

	st := evaluateAuthStatus(server.URL, &config.Auth{Token: jwtWithAudience(t, "wrong-project")}, true)

	if st.Authenticated {
		t.Fatalf("a token missing the project audience must be unauthenticated, got %+v", st)
	}
	if len(records()) != 0 {
		t.Errorf("the local audience refusal must make no network call, got %+v", records())
	}
}

// runAuthStatus returns an error when not authenticated, so a script can gate
// on the exit code.
func TestAuthStatusExitsNonZeroWhenNotAuthenticated(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"https://api.modernpath.ai"}`)

	err := captureStdout(t, func() error { return runAuthStatus(authStatusCmd, nil) })
	if err == nil {
		t.Fatal("auth status must return an error (non-zero exit) when not authenticated")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("error must say why, got %v", err)
	}
}

// --json owns stdout: exactly one JSON object, nothing else, and it parses.
func TestAuthStatusJSONStdoutIsPureJSON(t *testing.T) {
	const projectID = "999888777666555444"
	server, _ := platformEdge(t, projectID)
	bindWorkspace(t, server.URL, projectID)

	saved := authStatusJSON
	authStatusJSON = true
	t.Cleanup(func() { authStatusJSON = saved })

	var out []byte
	err := captureStdout(t, func() error {
		e := runAuthStatus(authStatusCmd, nil)
		return e
	}, &out)
	if err != nil {
		t.Fatalf("a valid credential must exit zero: %v", err)
	}
	if !json.Valid(out) {
		t.Fatalf("--json stdout is not pure JSON (a banner leaked?):\n%s", out)
	}
	var st authStatus
	if err := json.Unmarshal(out, &st); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !st.Authenticated {
		t.Errorf("want authenticated:true in the JSON, got %+v", st)
	}
}

// captureStdout runs fn with os.Stdout and color.Output redirected to a pipe.
// The optional sink receives everything written to stdout during fn. It mirrors
// the swap TestSyncNoDocsDryRunJSONStdoutIsPureJSON uses so --json purity is
// asserted against the same two writers the renderers use.
func captureStdout(t *testing.T, fn func() error, sink ...*[]byte) error {
	t.Helper()
	r, w, _ := os.Pipe()
	savedStdout, savedColor := os.Stdout, color.Output
	os.Stdout, color.Output = w, w

	runErr := fn()

	_ = w.Close()
	os.Stdout, color.Output = savedStdout, savedColor
	out, _ := io.ReadAll(r)
	if len(sink) > 0 && sink[0] != nil {
		*sink[0] = out
	}
	return runErr
}

// A malformed api_url (from `env --set`, or a hand-edited config) must be a
// clean unauthenticated result, not a nil-request panic and not a false
// "present" offline. Regression for the discarded http.NewRequest error.
func TestAuthStatusMalformedURLIsCleanNotPanic(t *testing.T) {
	for _, offline := range []bool{false, true} {
		st := evaluateAuthStatus("https://foo:bar", &config.Auth{Token: "present"}, offline)
		if st.Authenticated {
			t.Fatalf("offline=%v: a malformed api_url must not be authenticated, got %+v", offline, st)
		}
		if !strings.Contains(st.Detail, "invalid API URL") {
			t.Errorf("offline=%v: detail must name the bad URL, got %q", offline, st.Detail)
		}
	}
}

// A server that cannot be reached is unauthenticated, reported after the online
// check was attempted. Port 1 is not listening — connection refused, no wait.
func TestAuthStatusServerUnreachable(t *testing.T) {
	st := evaluateAuthStatus("http://127.0.0.1:1", &config.Auth{Token: "present"}, false)

	if st.Authenticated || !st.CheckedOnline {
		t.Fatalf("an unreachable server must be checked-online and unauthenticated, got %+v", st)
	}
	if !strings.Contains(st.Detail, "unreachable") {
		t.Errorf("detail must say unreachable, got %q", st.Detail)
	}
}

// A non-401 error status is neither authenticated nor mistaken for expiry.
func TestAuthStatusServerErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	st := evaluateAuthStatus(srv.URL, &config.Auth{Token: "present"}, false)

	if st.Authenticated {
		t.Fatalf("a 500 must not be authenticated, got %+v", st)
	}
	if !strings.Contains(st.Detail, "server returned") {
		t.Errorf("detail must report the status, got %q", st.Detail)
	}
}

// The bearer must never reach the output — not the struct, not the JSON, not
// the human render — even on the online error path where it was sent.
func TestAuthStatusDoesNotLeakToken(t *testing.T) {
	const secret = "SECRET-TOKEN-DO-NOT-LEAK.aaa.bbb"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	st := evaluateAuthStatus(srv.URL, &config.Auth{Token: secret, WorkspaceID: "org-9", WorkspaceName: "Acme"}, false)

	blob, _ := json.Marshal(st)
	if strings.Contains(string(blob), secret) {
		t.Fatalf("token leaked into the status JSON: %s", blob)
	}

	var out []byte
	_ = captureStdout(t, func() error { renderAuthStatusHuman(st); return nil }, &out)
	human := string(out)
	if strings.Contains(human, secret) {
		t.Fatalf("token leaked into the human render:\n%s", human)
	}
	// The renderer still shows the useful identity and a status.
	if !strings.Contains(human, "Acme") {
		t.Errorf("human render dropped the workspace name:\n%s", human)
	}
}

// --json owns stdout on the not-authenticated path too, and still exits
// non-zero.
func TestAuthStatusJSONUnauthenticatedStdoutIsPureJSON(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"https://api.modernpath.ai"}`)

	saved := authStatusJSON
	authStatusJSON = true
	t.Cleanup(func() { authStatusJSON = saved })

	var out []byte
	err := captureStdout(t, func() error { return runAuthStatus(authStatusCmd, nil) }, &out)
	if err == nil {
		t.Fatal("no credential must exit non-zero")
	}
	if !json.Valid(out) {
		t.Fatalf("--json stdout is not pure JSON on the unauth path:\n%s", out)
	}
	var st authStatus
	if err := json.Unmarshal(out, &st); err != nil || st.Authenticated {
		t.Errorf("want a parseable authenticated:false object, got %+v (err %v)", st, err)
	}
}

// The exit-code contract holds when the command is reached the real way —
// through the Cobra tree — not only by calling runAuthStatus directly.
func TestAuthStatusExitCodeRoutesThroughCobra(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"https://api.modernpath.ai"}`)

	savedOff, savedJSON := authStatusOffline, authStatusJSON
	t.Cleanup(func() {
		authStatusOffline, authStatusJSON = savedOff, savedJSON
		rootCmd.SetArgs([]string{})
	})
	// --offline: no credential, no network, but must still route and error.
	rootCmd.SetArgs([]string{"auth", "status", "--offline"})

	err := captureStdout(t, func() error { return rootCmd.Execute() })
	if err == nil {
		t.Fatal("`auth status --offline` with no credential must return a non-nil error through Cobra")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("routed error must be the not-authenticated one, got %v", err)
	}
}
