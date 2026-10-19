package cmd

// REQ-CROSS-096: `modernpath status` names which sync it reports, and finds
// the export that is on disk.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func TestStatusJSONOutsideWorkspaceReturnsStructuredError(t *testing.T) {
	t.Chdir(t.TempDir())
	var stdout bytes.Buffer
	statusCmd.SetOut(&stdout)
	defer statusCmd.SetOut(nil)
	resetTreeFlags(statusCmd)
	if err := statusCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	err := runStatus(statusCmd, nil)
	if err == nil {
		t.Fatal("status --json outside a workspace must fail")
	}
	if stdout.Len() != 0 {
		t.Fatalf("error must leave stdout empty for the root JSON error handler, got %q", stdout.String())
	}
}

// seedStatusWorkspace returns a workspace root with an empty .modernpath.
func seedStatusWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// seedExport writes an export tree with the markers Core.Export produces.
func seedExport(t *testing.T, dir string, files ...string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	all := append([]string{"blueprint.json"}, files...)
	for _, rel := range all {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func configDirOf(root string) string { return filepath.Join(root, ".modernpath") }

// A workspace bound by `init` has no system_slug — the export on
// disk is still the answer to "are the docs synced?".
func TestStatusFindsTheExportWhenTheConfigHasNoSlug(t *testing.T) {
	root := seedStatusWorkspace(t)
	seedExport(t, filepath.Join(root, ".modernpath", "acme"),
		"BLUEPRINT.md", "architecture/overview.md")

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243})

	if len(rep.Exports) != 1 {
		t.Fatalf("a slugless config with an export on disk must resolve to it, got %d roots", len(rep.Exports))
	}
	if got, want := rep.Exports[0].Rel, filepath.Join(".modernpath", "acme"); got != want {
		t.Errorf("export root = %q, want %q", got, want)
	}
	if got := rep.Exports[0].Files; got != 3 {
		t.Errorf("file count = %d, want 3", got)
	}
}

// The zip's own top-level folder is the export root; the legacy docs/<slug>
// layout still exists in older workspaces and must also be found.
func TestStatusFindsTheLegacyDocsExportLayout(t *testing.T) {
	root := seedStatusWorkspace(t)
	seedExport(t, filepath.Join(root, ".modernpath", "docs", "acme"), "BLUEPRINT.md")

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243})

	if len(rep.Exports) != 1 {
		t.Fatalf("legacy docs/<slug> export must be found, got %d roots", len(rep.Exports))
	}
	if got, want := rep.Exports[0].Rel, filepath.Join(".modernpath", "docs", "acme"); got != want {
		t.Errorf("export root = %q, want %q", got, want)
	}
}

// An export produced before Core.Export wrote marker files carries neither
// docs_push_manifest.json nor blueprint.json. The config's own slug vouches
// for it: with docs on disk at the slug's path, "Not synced" is the status
// contradicting the disk — the misreport this command exists to remove.
func TestStatusFindsAMarkerlessExportTheConfigSlugNames(t *testing.T) {
	root := seedStatusWorkspace(t)
	dir := filepath.Join(root, ".modernpath", "docs", "acme")
	if err := os.MkdirAll(filepath.Join(dir, "architecture"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "architecture", "overview.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243, SystemSlug: "acme"})

	if len(rep.Exports) != 1 {
		t.Fatalf("a marker-less legacy export named by the config slug must be reported, got %d roots", len(rep.Exports))
	}
	if got, want := rep.Exports[0].Rel, filepath.Join(".modernpath", "docs", "acme"); got != want {
		t.Errorf("export root = %q, want %q", got, want)
	}
}

// Guard: without the config slug vouching for it, a marker-less directory
// stays invisible — that is what keeps CLI-owned directories out of the list.
func TestStatusStillIgnoresMarkerlessDirectoriesTheConfigDoesNotName(t *testing.T) {
	root := seedStatusWorkspace(t)
	if err := os.MkdirAll(filepath.Join(root, ".modernpath", "docs", "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "docs", "other", "a.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243, SystemSlug: "acme"})

	if len(rep.Exports) != 0 {
		t.Fatalf("a marker-less directory the config does not name is not an export, got %v", rep.Exports)
	}
}

// The guard that stops the fix reporting an export that is not there.
func TestStatusReportsNoExportWhenNoneIsOnDisk(t *testing.T) {
	root := seedStatusWorkspace(t)

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243, SystemSlug: "acme"})

	if len(rep.Exports) != 0 {
		t.Fatalf("an empty workspace must report no export, got %d roots", len(rep.Exports))
	}
}

// The guard that stops disk discovery mistaking CLI-owned directories for
// exports: these carry no export marker, and some are reserved names.
func TestStatusIgnoresDirectoriesThatAreNotExports(t *testing.T) {
	root := seedStatusWorkspace(t)
	for _, dir := range []string{"rdd", "skills", "bin", "patterns", "viewer", "workflows"} {
		if err := os.MkdirAll(filepath.Join(root, ".modernpath", dir, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, ".modernpath", dir, "nested", "a.md"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243})

	if len(rep.Exports) != 0 {
		t.Fatalf("marker-less CLI directories are not exports, got %v", rep.Exports)
	}
}

func statusSystemsServer(t *testing.T, status int, systemIDs ...int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			systems[i] = map[string]any{"id": id}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(systems)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStatusWarnsWhenTheBoundSystemIsUnreachable(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	if err := config.WriteAuth(&config.Auth{Token: "good-bearer"}); err != nil {
		t.Fatal(err)
	}
	srv := statusSystemsServer(t, http.StatusOK, 7, 8)

	warning := statusReachabilityWarning(srv.URL, 999)
	if warning == "" {
		t.Fatal("an unreachable bound system must produce a warning")
	}
	if !strings.Contains(warning, "999") {
		t.Errorf("warning must name the configured system 999, got %q", warning)
	}
}

func TestStatusStaysSilentWhenTheBoundSystemIsReachable(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	if err := config.WriteAuth(&config.Auth{Token: "good-bearer"}); err != nil {
		t.Fatal(err)
	}
	srv := statusSystemsServer(t, http.StatusOK, 7)

	if warning := statusReachabilityWarning(srv.URL, 7); warning != "" {
		t.Fatalf("a reachable bound system must produce no warning, got %q", warning)
	}
}

func TestStatusStaysSilentWithoutABearer(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	// No auth.json written at all.
	srv := statusSystemsServer(t, http.StatusOK, 7)

	if warning := statusReachabilityWarning(srv.URL, 999); warning != "" {
		t.Fatalf("an unauthenticated workspace must produce no warning, got %q", warning)
	}
}

// TestStatusStaysSilentWhenUnconfigured is the status-path analogue of
// TestFreshInitHasNothingToValidate — the fourth surface's own version of
// the SystemID != 0 gate the other three surfaces already had.
func TestStatusStaysSilentWhenUnconfigured(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	if err := config.WriteAuth(&config.Auth{Token: "good-bearer"}); err != nil {
		t.Fatal(err)
	}
	// A server that would otherwise report a mismatch for any real id.
	srv := statusSystemsServer(t, http.StatusOK, 7)

	if warning := statusReachabilityWarning(srv.URL, 0); warning != "" {
		t.Fatalf("an unconfigured (SystemID == 0) workspace must produce no warning, got %q", warning)
	}
}

func TestStatusStaysSilentWhenTheReachabilityCallFails(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	if err := config.WriteAuth(&config.Auth{Token: "good-bearer"}); err != nil {
		t.Fatal(err)
	}
	srv := statusSystemsServer(t, http.StatusInternalServerError)

	if warning := statusReachabilityWarning(srv.URL, 999); warning != "" {
		t.Fatalf("a check that itself errors must fail open — no warning, got %q", warning)
	}
}
