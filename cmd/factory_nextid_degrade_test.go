package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

// REQ-CROSS-123, third criterion: "REQ-CROSS-140 `next-id` treats that error as
// best-effort server unavailability and returns the labelled ledger-only result
// without HTTP."
//
// The pure allocator is tested (TestNextIDDegradesWhenServerUnknown feeds it an
// empty server list), and the two credentialError branches in factoryEnvLoad are
// tested. Nothing joined them: that `next-id` REACHES its ledger-only answer
// when the credential is the thing that failed, and reaches it without a
// request. A refusal that is correct one call up still costs the user their id
// if the command surfaces it as a fatal error.
func writeWorkspace(t *testing.T, apiURL string, auth string) string {
	t.Helper()
	root := chdirTemp(t)
	cfgDir := filepath.Join(root, config.ConfigDir)
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"api_url": apiURL, "system_id": 1})
	if err := os.WriteFile(filepath.Join(cfgDir, config.ConfigFile), cfg, 0o644); err != nil {
		t.Fatal(err)
	}
	if auth != "" {
		if err := os.WriteFile(filepath.Join(cfgDir, config.AuthFile), []byte(auth), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "tasks"), 0o755); err != nil {
		t.Fatal(err)
	}
	ledger := "## Dashboard — SBX\n\n| ID | Title | Stage | Status |\n|---|---|---|---|\n" +
		"| REQ-SBX-001 | one | MVP | DONE |\n| REQ-SBX-007 | seven | MVP | DONE |\n"
	if err := os.WriteFile(filepath.Join(root, "tasks", "SBX-REQUIREMENTS.md"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func runNextID(t *testing.T, area string) (stdout, stderr string, err error) {
	t.Helper()
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	runErr := factoryNextIDCmd.RunE(factoryNextIDCmd, []string{area})

	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()
	// Both streams are a few hundred bytes, well inside the pipe buffer, so
	// reading after the writers close cannot deadlock.
	o, _ := io.ReadAll(outR)
	e, _ := io.ReadAll(errR)
	return string(o), string(e), runErr
}

func TestNextIDStillAnswersWhenTheCredentialIsTheProblem(t *testing.T) {
	var requests int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requests, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"requirements":[]}}`))
	}))
	defer server.Close()

	for _, tc := range []struct {
		name string
		auth string
	}{
		{"auth.json is absent", ""},
		{"auth.json is unreadable", "{ this is not json"},
		{"auth.json carries no bearer", `{"token":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			atomic.StoreInt64(&requests, 0)
			writeWorkspace(t, server.URL, tc.auth)

			stdout, stderr, err := runNextID(t, "SBX")

			if err != nil {
				t.Fatalf("next-id failed instead of degrading: %v", err)
			}
			if got := strings.TrimSpace(stdout); got != "REQ-SBX-008" {
				t.Errorf("stdout = %q, want REQ-SBX-008 from the ledger alone", got)
			}
			if !strings.Contains(stderr, "server not consulted") {
				t.Errorf("a degraded answer must say it is one; stderr = %q", stderr)
			}
			if n := atomic.LoadInt64(&requests); n != 0 {
				t.Errorf("%d authenticated request(s) were made — the refusal is supposed to happen "+
					"before any HTTP, not be discovered by it", n)
			}
		})
	}
}
