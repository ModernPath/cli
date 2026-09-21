package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/contract"
)

// REQ-CROSS-390 (EPIC-CLI-019) — before a store write the CLI compares its
// build against the server's contract: a write that needs a capability this
// build lacks is refused by name and never posted; a newer contract on a read
// warns once per version; a server without the read is older than the check.

func contractServer(t *testing.T, capabilities map[string][]string, contractStatus int, headerVersion string) (*httptest.Server, *int) {
	t.Helper()
	posts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/sync/contract", func(w http.ResponseWriter, r *http.Request) {
		if headerVersion != "" {
			w.Header().Set("x-modernpath-contract", headerVersion)
		}
		if contractStatus != 0 && contractStatus != 200 {
			w.WriteHeader(contractStatus)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"version": contract.Version, "capabilities": capabilities}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if headerVersion != "" {
			w.Header().Set("x-modernpath-contract", headerVersion)
		}
		if r.Method == http.MethodPost {
			posts++
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gates": []any{}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &posts
}

// (a) A write needing a capability this build lacks is refused by name,
// naming the build, before any post.
func TestWriteNeedingAMissingCapabilityIsRefusedByName(t *testing.T) {
	srv, posts := contractServer(t, map[string][]string{"author.evaluate_trace": {"review_context", "future_cap"}}, 200, "")
	env := wsEnv(t, srv)

	_, _, err := env.call("POST", "/api/v1/sync/author", map[string]any{"action": "evaluate_trace", "record": map[string]any{"kind": "gate"}})
	if err == nil {
		t.Fatal("a write this build cannot mean must be refused")
	}
	for _, want := range []string{"future_cap", Version, "rebuild"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal must name %q, got %v", want, err)
		}
	}
	if *posts != 0 {
		t.Errorf("nothing may be posted after a refusal, got %d posts", *posts)
	}
}

// A write whose capabilities this build implements proceeds.
func TestWriteWithImplementedCapabilitiesProceeds(t *testing.T) {
	srv, posts := contractServer(t, map[string][]string{"author.evaluate_trace": {"review_context"}}, 200, "")
	env := wsEnv(t, srv)

	if _, _, err := env.call("POST", "/api/v1/sync/author", map[string]any{"action": "evaluate_trace", "record": map[string]any{"kind": "gate"}}); err != nil {
		t.Fatalf("an implemented write must proceed: %v", err)
	}
	if *posts != 1 {
		t.Errorf("want one post, got %d", *posts)
	}
}

// (b) A read against a newer contract warns once per version.
func TestNewerContractOnAReadWarnsOncePerVersion(t *testing.T) {
	srv, _ := contractServer(t, nil, 200, "2")
	env := wsEnv(t, srv)

	var out []byte
	captureStderr(t, func() {
		for i := 0; i < 2; i++ {
			if _, _, err := env.call("GET", "/api/v1/sync/gates", nil); err != nil {
				t.Errorf("read %d: %v", i, err)
			}
		}
	}, &out)
	if n := strings.Count(string(out), "contract"); n != 1 {
		t.Fatalf("want exactly one contract warning across two reads, got %d:\n%s", n, out)
	}
	if !strings.Contains(string(out), "rebuild") {
		t.Errorf("the warning must name the remedy:\n%s", out)
	}
}

// (c) A server without the contract read is older than the check: the write
// proceeds and nothing is printed. Passes today; kept as the guard.
func TestServerWithoutTheContractReadIsNotChecked(t *testing.T) {
	srv, posts := contractServer(t, nil, 404, "")
	env := wsEnv(t, srv)

	var out []byte
	captureStderr(t, func() {
		if _, _, err := env.call("POST", "/api/v1/sync/author", map[string]any{"action": "create"}); err != nil {
			t.Errorf("the write must proceed: %v", err)
		}
	}, &out)
	if *posts != 1 || strings.Contains(string(out), "contract") {
		t.Errorf("an older server must be written to silently: posts=%d stderr=%q", *posts, out)
	}
}
