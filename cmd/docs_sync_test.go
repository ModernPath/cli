package cmd

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

// docsSyncWorkspace binds a workspace to system 7 under the given export slug.
func docsSyncWorkspace(t *testing.T, serverURL, slug string) string {
	t.Helper()
	root := prepareInputsWorkspace(t, serverURL)
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.SystemSlug = slug
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return root
}

// SR-RDD-ONBOARD-048 AC2: when the server's export folder no longer matches
// the bound slug — the system was renamed after the import — docs sync names
// both slugs and the remedy before any download, and leaves the binding alone.
func TestDocsSyncNamesSlugDriftBeforeDownloading(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"}), http.StatusOK)
	docsSyncWorkspace(t, srv.URL, "demo-old")

	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil {
		t.Fatalf("docs sync extracted under a drifted slug:\n%s", out)
	}
	for _, want := range []string{".modernpath/demo", ".modernpath/demo-old", "modernpath factory connect"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal must name %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "outside the bound root") {
		t.Errorf("the drift must be named before the archive is validated:\n%s", out)
	}
	if srv.downloads != 0 {
		t.Errorf("documentation export downloads = %d, want none", srv.downloads)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemSlug != "demo-old" {
		t.Errorf("docs sync rewrote the binding to %q", cfg.SystemSlug)
	}
}

// SR-RDD-ONBOARD-048 AC3: equal slugs extract as today, and an archive whose
// root is another system is still refused by the extraction.
func TestDocsSyncExtractsUnderAnEqualSlugAndStillRefusesAForeignRoot(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"}), http.StatusOK)
	root := docsSyncWorkspace(t, srv.URL, "demo")
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync under the bound slug failed: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".modernpath", "demo", "a.md")); err != nil || string(got) != "doc" {
		t.Fatalf("export not extracted under the bound slug: %q, %v", got, err)
	}

	srv = newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/other/x.md": "wrong"}), http.StatusOK)
	docsSyncWorkspace(t, srv.URL, "demo")
	out, err = runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil || !strings.Contains(out, ".modernpath/other/x.md") {
		t.Fatalf("a foreign root must still be refused by name: %v\n%s", err, out)
	}
}
