package cmd

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
)

// REQ-CROSS-291, SCN-CLI-006-002 at the command level: a token stored by a CLI
// build older than the project-audience scope is refused locally, before any
// request the edge would only answer with a 401.
//
// `factory gates` is the exemplar because it makes two independent calls
// through the shared bearer path — factoryEnvLoad's system-reachability check
// (listSystemsFn) and its own env.call — so a zero-request assertion has to
// cover both. `factory status` cannot be used: it never calls factoryEnvLoad
// at all (REQ-CROSS-271 pins it offline), so the assertion would hold no
// matter what Authorize did.
func TestFactoryGatesRefusesTokenWithoutProjectAudienceBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"gates":[]}}`))
	}))
	t.Cleanup(server.Close)

	// The test server is not a platform host — platformOrigins is a fixed set
	// of the baked-in profiles — so register it as one for this test.
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	restore := platform.SetPlatformOriginsForTest(map[string]zitadel.Profile{
		u.Scheme + "://" + u.Host: {
			APIURL:    server.URL,
			ProjectID: "999888777666555444",
		},
	})
	t.Cleanup(restore)

	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json",
		fmt.Sprintf(`{"api_url":%q,"system_id":7}`, server.URL))
	writeFactoryTestFile(t, root, ".modernpath/auth.json",
		fmt.Sprintf(`{"token":%q}`, jwtWithAudience(t, "some-cli-client-id")))

	err = factoryGatesCmd.RunE(factoryGatesCmd, nil)
	if err == nil {
		t.Fatal("factory gates succeeded, want a local refusal")
	}
	if !errors.Is(err, platform.ErrTokenMissingProjectAudience) {
		t.Errorf("error = %v, want ErrTokenMissingProjectAudience", err)
	}
	if !strings.Contains(err.Error(), "modernpath auth --sso") {
		t.Errorf("error = %q, want it to name the command that fixes it", err)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("server recorded %d requests, want 0 — nothing may be sent, including the env-load reachability check", got)
	}
}

// A token that does carry the project id reaches the server as before: the
// pre-check refuses only what it knows to be wrong.
func TestFactoryGatesSendsTokenCarryingProjectAudience(t *testing.T) {
	const projectID = "999888777666555444"

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/systems") {
			_, _ = w.Write([]byte(`{"data":[{"id":7,"name":"test"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"gates":[]}}`))
	}))
	t.Cleanup(server.Close)

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	restore := platform.SetPlatformOriginsForTest(map[string]zitadel.Profile{
		u.Scheme + "://" + u.Host: {APIURL: server.URL, ProjectID: projectID},
	})
	t.Cleanup(restore)

	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json",
		fmt.Sprintf(`{"api_url":%q,"system_id":7}`, server.URL))
	writeFactoryTestFile(t, root, ".modernpath/auth.json",
		fmt.Sprintf(`{"token":%q}`, jwtWithAudience(t, "some-cli-client-id", projectID)))

	if err := factoryGatesCmd.RunE(factoryGatesCmd, nil); err != nil {
		t.Fatalf("factory gates: %v", err)
	}
	if got := requests.Load(); got == 0 {
		t.Error("server recorded no requests, want the command to reach it")
	}
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
