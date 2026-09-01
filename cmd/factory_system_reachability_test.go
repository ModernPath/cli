package cmd

// REQ-CROSS-282 — factoryEnvLoad is the single chokepoint every credentialed
// factory subcommand (sync, gates, answer, pull, evidence, drift, watch,
// plus author/context_pack/image/next-id/reconcile/quiescent/migrate/
// workingset) already calls, so the guard here protects all of them by
// construction — it satisfies AC2's "sync refuses before queuing any ops"
// for free, since sync cannot reach its env.call sites without going
// through this function first.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// factoryTestSystemsServer answers GET /api/systems and counts requests of
// any kind, so a test can assert "no further env.call happened."
func factoryTestSystemsServer(t *testing.T, status int, systemIDs ...int) (*httptest.Server, *atomic.Int32) {
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

func factoryTestWorkspace(t *testing.T, apiURL string, systemID int) string {
	t.Helper()
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json",
		fmt.Sprintf(`{"api_url":%q,"system_id":%d}`, apiURL, systemID))
	writeFactoryTestFile(t, root, ".modernpath/auth.json", `{"token":"good-bearer"}`)
	return root
}

func TestFactoryEnvLoadRefusesAnUnreachableSystem(t *testing.T) {
	srv, calls := factoryTestSystemsServer(t, http.StatusOK, 7, 8)
	factoryTestWorkspace(t, srv.URL, 999)

	env, err := factoryEnvLoad()
	if err == nil {
		t.Fatal("an unreachable configured system must refuse, got a usable env")
	}
	if env != nil {
		t.Fatal("a refused load must not return a usable env")
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error must name the configured system 999, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "factory connect --system") {
		t.Errorf("error must name the repair command, got %q", err.Error())
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("exactly one reachability call, want no further env.call after refusal — got %d total calls", got)
	}
}

func TestFactoryEnvLoadAllowsAReachableSystem(t *testing.T) {
	srv, calls := factoryTestSystemsServer(t, http.StatusOK, 7)
	factoryTestWorkspace(t, srv.URL, 7)

	env, err := factoryEnvLoad()
	if err != nil {
		t.Fatalf("a reachable configured system must load, got: %v", err)
	}
	if env == nil || env.SystemID != 7 {
		t.Fatalf("env = %+v, want a usable env bound to system 7", env)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("exactly one reachability call for a normal load, got %d", got)
	}
}

func TestFactoryEnvLoadFailsOpenWhenTheReachabilityCallErrors(t *testing.T) {
	srv, calls := factoryTestSystemsServer(t, http.StatusInternalServerError)
	factoryTestWorkspace(t, srv.URL, 999)

	env, err := factoryEnvLoad()
	if err != nil {
		t.Fatalf("a check that itself errors must fail open — proceed exactly as today, got: %v", err)
	}
	if env == nil || env.SystemID != 999 {
		t.Fatalf("env = %+v, want a usable env even though the check errored", env)
	}
	if calls.Load() == 0 {
		t.Fatal("the reachability call must actually have been attempted")
	}
}

// The existing REQ-CROSS-271 pin, TestFactoryStatusDoesNotLoadAuthentication
// (factory_bearer_only_test.go), is re-run unmodified as part of this
// package's full suite below — factory status must stay credential-free;
// the new guard lives in factoryEnvLoad, not factoryBindingLoad, and must
// never leak into it.
