package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/modernpath/cli/internal/platform"
	"github.com/modernpath/cli/internal/zitadel"
)

// healthProbeRecord is what a fake platform edge saw for one request.
type healthProbeRecord struct {
	path       string
	authorized bool
}

// platformEdge stands in for the Gateway in front of core on a shared platform
// API host: it answers only inside core's `/api/ex` prefix, and only for a
// request that carries a bearer. Everything else gets the two answers the real
// edge gives — `404 fault filter abort` off-prefix, `401 Unauthorized`
// unauthenticated — which are exactly the two the CLI used to report as a
// broken server.
func platformEdge(t *testing.T, projectID string) (*httptest.Server, func() []healthProbeRecord) {
	t.Helper()

	var mu sync.Mutex
	var seen []healthProbeRecord

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorized := r.Header.Get("Authorization") != ""

		mu.Lock()
		seen = append(seen, healthProbeRecord{path: r.URL.Path, authorized: authorized})
		mu.Unlock()

		if r.URL.Path == "/api/ex/_health" || r.URL.Path == "/api/ex/systems" {
			if !authorized {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("Unauthorized"))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/ex/systems" {
				_, _ = w.Write([]byte(`[{"id":7,"name":"test"}]`))
				return
			}
			_, _ = w.Write([]byte(`{"status":"ok","service":"modernpath","database":"connected"}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("fault filter abort"))
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

	return server, func() []healthProbeRecord {
		mu.Lock()
		defer mu.Unlock()
		out := make([]healthProbeRecord, len(seen))
		copy(out, seen)
		return out
	}
}

// bindWorkspace points a temporary workspace at apiURL with a token the
// project-audience pre-check accepts, so `platform.Authorize` attaches it
// rather than refusing locally.
func bindWorkspace(t *testing.T, apiURL, projectID string) {
	t.Helper()
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json",
		fmt.Sprintf(`{"api_url":%q,"system_id":7}`, apiURL))
	writeFactoryTestFile(t, root, ".modernpath/auth.json",
		fmt.Sprintf(`{"token":%q}`, jwtWithAudience(t, projectID)))
}

// healthProbes narrows a recording to the health requests in it.
func healthProbes(records []healthProbeRecord) []healthProbeRecord {
	var out []healthProbeRecord
	for _, r := range records {
		if r.path == "/_health" || r.path == "/api/_health" || r.path == "/api/ex/_health" {
			out = append(out, r)
		}
	}
	return out
}

// The edge authenticates every route it fronts, core's health check included,
// so a probe sent without the stored bearer is answered `401` no matter how
// healthy the server is. `env test` reported that 401 as "health check
// failed" — a broken server — for a deployment that was working.
func TestEnvTestHealthProbeCarriesTheStoredBearer(t *testing.T) {
	const projectID = "999888777666555444"

	server, records := platformEdge(t, projectID)
	bindWorkspace(t, server.URL, projectID)

	if err := runEnvTest(envTestCmd, nil); err != nil {
		t.Fatalf("env test: %v", err)
	}

	probes := healthProbes(records())
	if len(probes) != 1 {
		t.Fatalf("health probes = %d, want 1: %+v", len(probes), records())
	}
	if probes[0].path != "/api/ex/_health" {
		t.Errorf("health probe path = %q, want /api/ex/_health", probes[0].path)
	}
	if !probes[0].authorized {
		t.Error("health probe carried no Authorization header, so the edge answers 401 " +
			"and a healthy server is reported as failing")
	}
}

// The bare `env` command draws the same conclusion from its own probe, so it
// needs the same two properties. It hardcoded `/_health`, which on a platform
// host is off core's prefix entirely: the edge answers `404 fault filter
// abort` and the command prints "Degraded" unconditionally.
func TestEnvShowHealthProbeUsesThePrefixAndCarriesTheBearer(t *testing.T) {
	const projectID = "999888777666555444"

	server, records := platformEdge(t, projectID)
	bindWorkspace(t, server.URL, projectID)

	if err := runEnv(envCmd, nil); err != nil {
		t.Fatalf("env: %v", err)
	}

	probes := healthProbes(records())
	if len(probes) != 1 {
		t.Fatalf("health probes = %d, want 1: %+v", len(probes), records())
	}
	if probes[0].path != "/api/ex/_health" {
		t.Errorf("health probe path = %q, want /api/ex/_health — /_health is off core's prefix "+
			"on a platform host and the edge 404s it", probes[0].path)
	}
	if !probes[0].authorized {
		t.Error("health probe carried no Authorization header")
	}
}

// Regression sensitivity for the two above: on any other host — localhost, an
// operator's --api-url — the probe stays the unauthenticated root `/_health`
// it has always been. A dev server with no credential configured must keep
// answering it.
func TestHealthProbeOnANonPlatformHostIsUnchanged(t *testing.T) {
	var mu sync.Mutex
	var seen []healthProbeRecord

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, healthProbeRecord{path: r.URL.Path, authorized: r.Header.Get("Authorization") != ""})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(server.Close)

	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json",
		fmt.Sprintf(`{"api_url":%q}`, server.URL))

	if err := runEnv(envCmd, nil); err != nil {
		t.Fatalf("env: %v", err)
	}
	if err := runEnvTest(envTestCmd, nil); err != nil {
		t.Fatalf("env test: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("requests = %d, want 2 (one probe per command): %+v", len(seen), seen)
	}
	for _, r := range seen {
		if r.path != "/_health" {
			t.Errorf("probe path = %q, want /_health on a non-platform host", r.path)
		}
		if r.authorized {
			t.Errorf("probe to %q carried an Authorization header; there is no stored token to send", r.path)
		}
	}
}
