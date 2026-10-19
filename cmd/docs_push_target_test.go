package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func TestDocsPushHonoursGlobalAPIURLOverride(t *testing.T) {
	target, imported := docsImportTestServer(t)
	var configuredHits atomic.Int32
	configured := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		configuredHits.Add(1)
		http.Error(w, "wrong server", http.StatusInternalServerError)
	}))
	defer configured.Close()

	cobraWorkspace(t, configured)
	originalAPIURL := apiURL
	t.Cleanup(func() { apiURL = originalAPIURL })
	docDir := filepath.Join(".modernpath", "t")
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(docDir, "doc.md"), []byte("# Doc\n\nBody.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := runRoot(t, "--api-url", target.URL, "docs", "push"); err != nil {
		t.Fatalf("docs push: %v", err)
	}
	if _, ok := (*imported)["architecture_id"]; !ok {
		t.Fatalf("target override did not receive the docs import payload: %v", *imported)
	}
	if got := configuredHits.Load(); got != 0 {
		t.Fatalf("configured target received %d requests despite --api-url override", got)
	}
}

func TestAuthenticatedDocsClientHonoursPassedConfig(t *testing.T) {
	originalAPIURL := apiURL
	apiURL = "https://flag.example"
	t.Cleanup(func() { apiURL = originalAPIURL })

	client := newAuthenticatedClient(&config.Config{APIURL: "https://configured.example"})
	if client.baseURL != "https://configured.example" {
		t.Fatalf("base URL = %q, want passed config URL", client.baseURL)
	}
}
