package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

type docsExportTestServer struct {
	*httptest.Server
	downloads int
}

func newDocsExportTestServer(t *testing.T, archive []byte) *docsExportTestServer {
	t.Helper()
	s := &docsExportTestServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_health":
			w.WriteHeader(http.StatusOK)
		case "/api/systems":
			_, _ = io.WriteString(w, `[{"id":7,"name":"Demo","slug":"demo"}]`)
		case "/api/systems/7/export/jobs":
			s.downloads++
			w.WriteHeader(http.StatusAccepted)
			_, _ = io.WriteString(w, `{"export_id":"test-job","status":"queued","poll_path":"/api/systems/7/export/jobs/test-job","link_path":"/api/systems/7/export/jobs/test-job/link"}`)
		case "/api/systems/7/export/jobs/test-job":
			_, _ = io.WriteString(w, `{"export_id":"test-job","status":"ready"}`)
		case "/api/systems/7/export/jobs/test-job/link":
			_, _ = io.WriteString(w, `{"url":"/api/systems/7/export/jobs/test-job/file","filename":"demo.zip"}`)
		case "/api/systems/7/export/jobs/test-job/file":
			_, _ = w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func zipWithFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	w := zip.NewWriter(&output)
	for name, contents := range files {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

// docsSyncWorkspace binds a workspace to system 7 under the given export slug.
func docsSyncWorkspace(t *testing.T, server *docsExportTestServer, slug string) string {
	t.Helper()
	cobraWorkspace(t, server.Server, 7)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
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
	archive := zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"})
	srv := newDocsExportTestServer(t, archive)
	docsSyncWorkspace(t, srv, "demo-old")

	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil {
		t.Fatalf("docs sync extracted under a drifted slug:\n%s", out)
	}
	for _, want := range []string{".modernpath/demo", ".modernpath/demo-old", "modernpath init"} {
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
	archive := zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"})
	srv := newDocsExportTestServer(t, archive)
	root := docsSyncWorkspace(t, srv, "demo")
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync under the bound slug failed: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(root, ".modernpath", "demo", "a.md")); err != nil || string(got) != "doc" {
		t.Fatalf("export not extracted under the bound slug: %q, %v", got, err)
	}

	srv = newDocsExportTestServer(t, zipWithFiles(t, map[string]string{".modernpath/other/x.md": "wrong"}))
	docsSyncWorkspace(t, srv, "demo")
	out, err = runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil || !strings.Contains(out, ".modernpath/other/x.md") {
		t.Fatalf("a foreign root must still be refused by name: %v\n%s", err, out)
	}
}

func TestDocsSyncJSONReturnsSyncReceipt(t *testing.T) {
	archive := zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"})
	srv := newDocsExportTestServer(t, archive)
	docsSyncWorkspace(t, srv, "demo")
	stdout, stderr, err := runAskStreams(t, "docs", "sync", "--json")
	if err != nil {
		t.Fatalf("docs sync --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(stdout), &receipt); err != nil {
		t.Fatalf("docs sync stdout must be JSON only, got %q: %v", stdout, err)
	}
	if receipt["synced"] != true || receipt["system_id"] != float64(7) || receipt["files"] == nil || receipt["path"] == nil {
		t.Fatalf("unexpected sync receipt: %#v", receipt)
	}
}
