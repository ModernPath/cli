package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestFactoryStatusDoesNotLoadAuthentication(t *testing.T) {
	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", `{"api_url":"http://localhost:4000","system_id":7}`)
	writeFactoryTestFile(t, root, ".modernpath/auth.json", `{"token":"truncated`)

	if err := factoryStatusCmd.RunE(factoryStatusCmd, nil); err != nil {
		t.Fatalf("status is a local binding command and must ignore malformed auth.json: %v", err)
	}
}

func TestNextIDWithOnlyRetiredKeyUsesLedgerWithoutHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"requirements":[]}}`))
	}))
	t.Cleanup(server.Close)

	root := enterFactoryTestWorkspace(t)
	writeFactoryTestFile(t, root, ".modernpath/config.json", fmt.Sprintf(`{"api_url":%q,"system_id":7}`, server.URL))
	writeFactoryTestFile(t, root, ".modernpath/mp_api_key", "retired-key")
	writeFactoryTestFile(t, root, "tasks/CROSS-REQUIREMENTS.md", "| REQ-CROSS-007 | local row | MVP | TODO |\n")

	if err := factoryNextIDCmd.RunE(factoryNextIDCmd, []string{"CROSS"}); err != nil {
		t.Fatalf("next-id must retain its ledger-only behavior without bearer auth: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("key-only next-id made %d HTTP request(s), want zero", got)
	}
}

func TestDeprecatedNodeTransportIsGoneButOperationBuildersRemain(t *testing.T) {
	cliDir := filepath.Join("..", "..", "..", "..", "mission-control", "cli")
	if _, err := os.Stat(cliDir); os.IsNotExist(err) {
		t.Skip("workspace operation builders are outside the standalone CLI build context")
	}
	if _, err := os.Stat(filepath.Join(cliDir, "mp.js")); !os.IsNotExist(err) {
		t.Fatalf("deprecated executable transport still exists: %v", err)
	}
	for _, name := range []string{"ops.js", "ops-dump.js"} {
		if _, err := os.Stat(filepath.Join(cliDir, name)); err != nil {
			t.Fatalf("required operation builder %s is missing: %v", name, err)
		}
	}
}

func enterFactoryTestWorkspace(t *testing.T) string {
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

func writeFactoryTestFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
