package cmd

// REQ-CROSS-536 (EPIC-CLI-OUTAGE) — no command or hook waits on a server known
// to be unavailable. Every case drives the command or hook through run(args)
// as the installed binary does, and asserts only the exit code, the printed
// text, the files written, the requests the fake saw and the time taken.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/zitadel"
)

// countingStore answers every request as a healthy store would and counts
// all of them — the probe and the contract read included.
func countingStore(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var all atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		all.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/systems":
			_, _ = w.Write([]byte(`[{"id":1,"name":"T","slug":"modernpath"}]`))
		case "/api/v1/sync/contract":
			w.WriteHeader(http.StatusNotFound)
		case "/api/mcp/tools/context_search":
			_, _ = w.Write([]byte(`{"success":true,"result":{"relevant":true,"context":"served context"}}`))
		default:
			w.Header().Set("x-modernpath-store-revision", "rev-1")
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &all
}

// freshUnavailable records an outage that began five minutes ago and was
// checked just now: inside the window in which nothing calls the server.
func freshUnavailable(t *testing.T, root string) {
	t.Helper()
	now := time.Now().UTC()
	st := map[string]any{
		"state":        "unavailable",
		"since":        now.Add(-5 * time.Minute).Format(time.RFC3339),
		"last_checked": now.Format(time.RFC3339),
		"reason":       "connection refused",
	}
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(availabilityPath(root), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// feedbackCredential stores a bearer that feedback accepts as a ModernPath
// production credential, valid for an hour, so no refresh is due.
func feedbackCredential(t *testing.T, root string) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"iss":                     zitadel.ProdProfile.Issuer,
		"exp":                     time.Now().Add(time.Hour).Unix(),
		"urn:modernpath:token:v2": map[string]any{"org_id": feedbackOrganizationID},
	})
	if err != nil {
		t.Fatal(err)
	}
	token := "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	auth := `{"token":"` + token + `","actor":"dev@example.com"}`
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "auth.json"), []byte(auth), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runCLIWithStdin is runCLI with the agent's hook payload on stdin.
func runCLIWithStdin(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(stdin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	saved := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = saved; _ = f.Close() }()
	return runCLI(t, args...)
}

// AC1 — with a fresh unavailable state, a store command, feedback, the
// SessionStart brief (through factoryEnvLoad) and the context hook make no
// request and finish within one second.
func TestKnownOutageMakesNoCall(t *testing.T) {
	cases := []struct {
		name     string
		prepare  func(t *testing.T, root string)
		stdin    string
		args     []string
		wantExit int // -1: any exit; the hooks always exit 0
	}{
		{name: "your-move", args: []string{"your-move"}, wantExit: -1},
		{name: "factory gates", args: []string{"factory", "gates"}, wantExit: -1},
		{name: "feedback", prepare: feedbackCredential, args: []string{"feedback", "a tooling defect"}, wantExit: -1},
		{
			name:     "SessionStart brief",
			stdin:    `{"session_id":"outage-1","hook_event_name":"SessionStart","source":"startup"}`,
			args:     []string{"your-move", "--hook", "SessionStart"},
			wantExit: 0,
		},
		{
			name:     "context hook",
			stdin:    `{"prompt":"how does sync work?"}`,
			args:     []string{"context", "--hook", "UserPromptSubmit"},
			wantExit: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, requests := countingStore(t)
			root := outageWorkspace(t, srv.URL)
			if tc.prepare != nil {
				tc.prepare(t, root)
			}
			freshUnavailable(t, root)

			started := time.Now()
			code, out := runCLIWithStdin(t, tc.stdin, tc.args...)
			took := time.Since(started)

			if n := requests.Load(); n != 0 {
				t.Errorf("%d request(s) reached the server while it is known to be unavailable; want 0. output:\n%s", n, out)
			}
			if took >= time.Second {
				t.Errorf("took %v, want under 1 s while the server is known to be unavailable", took)
			}
			if tc.wantExit >= 0 && code != tc.wantExit {
				t.Errorf("exit %d, want %d; output:\n%s", code, tc.wantExit, out)
			}
		})
	}
}

// AC2 — a server that accepts connections and never answers, with no fresh
// state: a store command exits 75 within the probe bound plus a few seconds,
// not after the call timeout.
func TestHangingServerFailsWithinProbeBound(t *testing.T) {
	scaleOutageTimeouts(t, 200*time.Millisecond, 10*time.Second)
	release := make(chan struct{})
	hang := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}
	srv, _ := storeFake(t, hang, hang)
	t.Cleanup(func() { close(release) }) // runs before srv.Close
	outageWorkspace(t, srv.URL)

	started := time.Now()
	code, out := runCLI(t, "factory", "gates")
	took := time.Since(started)

	if code != exitTempFail {
		t.Errorf("exit %d, want %d (EX_TEMPFAIL); output:\n%s", code, exitTempFail, out)
	}
	// The slack is wide for -race; the call timeout is far above it.
	if bound := 200*time.Millisecond + 3*time.Second; took > bound {
		t.Errorf("took %v, want within the probe bound plus a few seconds (%v), not the 10 s call timeout", took, bound)
	}
}
