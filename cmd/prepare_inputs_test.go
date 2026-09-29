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
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

type prepareInputsServer struct {
	*httptest.Server
	archive        []byte
	archiveCode    int
	downloads      int
	requests       []string
	systems        []map[string]any
	beforeFile     func()
	prepareStatus  int
	prepareContext map[string]any
}

func newPrepareInputsServer(t *testing.T, archive []byte, archiveCode int) *prepareInputsServer {
	t.Helper()
	f := &prepareInputsServer{
		archive: archive, archiveCode: archiveCode,
		systems:       []map[string]any{{"id": 7, "name": "Demo", "slug": "demo"}},
		prepareStatus: http.StatusOK,
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := r.Method + " " + r.URL.Path
		if r.URL.RawQuery != "" {
			request += "?" + r.URL.RawQuery
		}
		f.requests = append(f.requests, request)
		switch {
		case r.URL.Path == "/_health":
			writePrepareJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		case r.URL.Path == "/api/systems":
			writePrepareJSON(w, http.StatusOK, f.systems)
		case r.URL.Path == "/api/systems/7/export/jobs" && r.Method == http.MethodPost:
			f.downloads++
			// The export API of REQ-OBAN-014/018: the zip is fetched through the link.
			writePrepareJSON(w, http.StatusAccepted, map[string]any{"export_id": "job-1", "status": "queued", "poll_path": "/api/systems/7/export/jobs/job-1", "link_path": "/api/systems/7/export/jobs/job-1/link"})
		case r.URL.Path == "/api/systems/7/export/jobs/job-1":
			writePrepareJSON(w, http.StatusOK, map[string]any{"export_id": "job-1", "status": "ready", "error": nil})
		case r.URL.Path == "/api/systems/7/export/jobs/job-1/link":
			writePrepareJSON(w, http.StatusOK, map[string]any{"url": "/api/systems/7/export/jobs/job-1/file", "filename": "demo.modernpath.zip", "expires_at": nil})
		case r.URL.Path == "/api/systems/7/export/jobs/job-1/file":
			if f.beforeFile != nil {
				f.beforeFile()
			}
			w.WriteHeader(f.archiveCode)
			_, _ = w.Write(f.archive)
		case strings.HasPrefix(r.URL.Path, "/api/v1/"):
			// Read surfaces used by input preparation. Unspecified list reads are
			// empty by default; writes are recorded and rejected below.
			if r.Method != http.MethodGet {
				http.Error(w, "unexpected state write", http.StatusMethodNotAllowed)
				return
			}
			switch {
			case strings.HasSuffix(r.URL.Path, "/sync/prepare-inputs"):
				if f.prepareStatus != http.StatusOK {
					writePrepareJSON(w, f.prepareStatus, map[string]any{"error": f.prepareContext["error"]})
				} else {
					writePrepareJSON(w, f.prepareStatus, map[string]any{"data": f.prepareContext})
				}
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	setPrepareInputsContext(f)
	t.Cleanup(f.Close)
	return f
}

func setPrepareInputsContext(srv *prepareInputsServer) {
	srv.prepareContext = map[string]any{
		"system":           map[string]any{"id": 7, "name": "Demo", "slug": "demo"},
		"contract_version": 1,
		"active_releases": []any{map[string]any{
			"slug": "release-26", "status": "active", "source_tag": "USER:2026-09-25:select release-26",
		}},
		"held_piece_count":     2,
		"documents_updated_at": "2026-09-25T10:30:00Z",
		"pending_decisions": []any{
			map[string]any{"id": "RQ-268", "title": "Board column semantics"},
			map[string]any{"id": "OQ-NX-04", "title": "Focus inference beyond explicit references?"},
		},
	}
}

func TestPrepareInputsShowsDocumentTimestampsWithoutRefreshingExport(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/stale.md": "stale"}), http.StatusOK)
	setPrepareInputsContext(srv)
	root := prepareInputsWorkspace(t, srv.URL)
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LastSyncAt = "2026-09-20T12:00:00Z"
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	localDoc := filepath.Join(root, ".modernpath", "demo", "architecture", "local.md")
	if err := os.MkdirAll(filepath.Dir(localDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localDoc, []byte("local export"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLocalExportTimestamp(t, root, "2026-09-20T12:00:00Z")
	configBefore, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs: %v\n%s", err, out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	for field, want := range map[string]string{
		"local_documents_last_synced_at":   "2026-09-20T12:00:00Z",
		"server_documents_last_updated_at": "2026-09-25T10:30:00Z",
	} {
		if report[field] != want {
			t.Errorf("%s = %#v, want %q", field, report[field], want)
		}
	}
	if report["local_documents_status"] != "available" {
		t.Errorf("local_documents_status = %#v, want available", report["local_documents_status"])
	}
	if srv.downloads != 0 {
		t.Errorf("prepare-inputs started %d export downloads, want none", srv.downloads)
	}
	if len(srv.requests) != 1 || srv.requests[0] != "GET /api/v1/sync/prepare-inputs?system_id=7" {
		t.Errorf("prepare-inputs requests = %v, want one combined context read", srv.requests)
	}
	configAfter, err := os.ReadFile(filepath.Join(root, ".modernpath", "config.json"))
	if err != nil || !bytes.Equal(configBefore, configAfter) {
		t.Errorf("prepare-inputs changed local config: read error %v", err)
	}
	if got, err := os.ReadFile(localDoc); err != nil || string(got) != "local export" {
		t.Errorf("prepare-inputs changed local documents: %q, %v", got, err)
	}
}

func TestPrepareInputsDoesNotClaimLocalDocumentsWhenBoundTreeIsMissing(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	setPrepareInputsContext(srv)
	root := prepareInputsWorkspace(t, srv.URL)
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LastSyncAt = "2026-09-20T12:00:00Z"
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}

	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("missing local export should not block context readiness: %v\n%s", err, out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	if report["local_documents_status"] != "unavailable" || report["local_documents_last_synced_at"] != nil {
		t.Errorf("missing local tree claimed a local timestamp: status=%#v synced=%#v", report["local_documents_status"], report["local_documents_last_synced_at"])
	}
	remedy, _ := report["local_documents_remedy"].(string)
	if !strings.Contains(strings.ToLower(remedy), "modernpath docs sync") {
		t.Errorf("missing local tree omitted docs sync guidance: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath", "demo")); !os.IsNotExist(err) {
		t.Errorf("prepare-inputs created the local document tree: %v", err)
	}
}

func serverDocumentTimestampCase(t *testing.T, field bool, value any) (*prepareInputsServer, string, error) {
	t.Helper()
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	setPrepareInputsContext(srv)
	if !field {
		delete(srv.prepareContext, "documents_updated_at")
	} else {
		srv.prepareContext["documents_updated_at"] = value
	}
	prepareInputsWorkspace(t, srv.URL)
	out, err := runPrepareInputs(t)
	return srv, out, err
}

func TestPrepareInputsAllowsNoServerDocuments(t *testing.T) {
	srv, out, err := serverDocumentTimestampCase(t, true, nil)
	if err != nil {
		t.Fatalf("no server documents should remain ready: %v\n%s", err, out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	if report["server_documents_last_updated_at"] != nil {
		t.Errorf("no server documents timestamp = %#v, want explicit null", report["server_documents_last_updated_at"])
	}
	if srv.downloads != 0 {
		t.Errorf("prepare-inputs started %d export downloads, want none", srv.downloads)
	}
}

func TestPrepareInputsRejectsMissingServerDocumentTimestamp(t *testing.T) {
	srv, out, err := serverDocumentTimestampCase(t, false, nil)
	if err == nil {
		t.Fatalf("missing document timestamp reported ready:\n%s", out)
	}
	if srv.downloads != 0 {
		t.Errorf("prepare-inputs started %d export downloads, want none", srv.downloads)
	}
}

func TestPrepareInputsRejectsInvalidServerDocumentTimestamp(t *testing.T) {
	srv, out, err := serverDocumentTimestampCase(t, true, "yesterday")
	if err == nil {
		t.Fatalf("invalid document timestamp reported ready:\n%s", out)
	}
	if srv.downloads != 0 {
		t.Errorf("prepare-inputs started %d export downloads, want none", srv.downloads)
	}
}

func TestPrepareInputsRequiresPreparationEndpoint(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	srv.prepareStatus = http.StatusNotFound
	srv.prepareContext["error"] = map[string]any{"message": "route not found"}
	prepareInputsWorkspace(t, srv.URL)
	out, err := runPrepareInputs(t)
	if err == nil || !strings.Contains(strings.ToLower(out), "server upgrade required") {
		t.Fatalf("missing preparation endpoint did not require a server upgrade: err=%v\n%s", err, out)
	}
	if len(srv.requests) != 1 || srv.downloads != 0 {
		t.Errorf("missing endpoint fell back to other requests: %v, downloads=%d", srv.requests, srv.downloads)
	}
}

func TestPrepareInputsPreservesBoundSystemNotFoundRefusal(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	srv.prepareStatus = http.StatusNotFound
	srv.prepareContext["error"] = map[string]any{"message": "system not found"}
	prepareInputsWorkspace(t, srv.URL)
	out, err := runPrepareInputs(t)
	if err == nil || !strings.Contains(strings.ToLower(out), "system not found") {
		t.Fatalf("bound-system 404 was not preserved: err=%v\n%s", err, out)
	}
	if strings.Contains(strings.ToLower(out), "server upgrade required") {
		t.Errorf("bound-system 404 was misreported as an old server: %s", out)
	}
}

func TestPrepareInputsRejectsMalformedHeldPieceCount(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	srv.prepareContext["held_piece_count"] = "two"
	prepareInputsWorkspace(t, srv.URL)
	out, err := runPrepareInputs(t)
	if err == nil || !strings.Contains(strings.ToLower(out), "held-piece count") {
		t.Fatalf("malformed held-piece count was accepted: err=%v\n%s", err, out)
	}
}

func TestPrepareInputsDoesNotReportMalformedLocalSyncTimestamp(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	root := prepareInputsWorkspace(t, srv.URL)
	docRoot := filepath.Join(root, ".modernpath", "demo")
	if err := os.MkdirAll(docRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LastSyncAt = "2026-09-20T12:00:00Z"
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	writeLocalExportTimestamp(t, root, "yesterday")
	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("invalid local timestamp should not block context readiness: %v\n%s", err, out)
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report["local_documents_status"] != "available" || report["local_documents_last_synced_at"] != nil {
		t.Errorf("malformed local timestamp was asserted: status=%#v timestamp=%#v", report["local_documents_status"], report["local_documents_last_synced_at"])
	}
	remedy, _ := report["local_documents_remedy"].(string)
	if !strings.Contains(strings.ToLower(remedy), "timestamp cannot be read") {
		t.Errorf("malformed local timestamp omitted guidance: %s", out)
	}
	var typedReport prepareInputsReport
	if err := json.Unmarshal([]byte(out), &typedReport); err != nil {
		t.Fatal(err)
	}
	var human bytes.Buffer
	renderPrepareInputsHuman(&human, typedReport)
	if !strings.Contains(human.String(), "modernpath docs sync") {
		t.Errorf("human output omitted local sync action: %s", human.String())
	}
}

func writePrepareJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func zipWithFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		content := files[name]
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func prepareInputsWorkspace(t *testing.T, serverURL string) string {
	t.Helper()
	root := chdirTemp(t)
	if err := config.WriteConfig(&config.Config{APIURL: serverURL, SystemID: 7, SystemName: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAuth(&config.Auth{Token: initGoodToken, Actor: "jane@example.com"}); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeLocalExportTimestamp(t *testing.T, root, syncedAt string) {
	t.Helper()
	path := filepath.Join(root, ".modernpath", "demo", "docs_push_manifest.json")
	data := `{"version":1,"system_doc_files":{},"generated_at":"` + syncedAt + `"}`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPrepareInputs(t *testing.T) (string, error) {
	t.Helper()
	rootCmd.SetArgs([]string{"process", "prepare-inputs", "--json"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	return runCapturing(t, rootCmd.Execute)
}

func prepareStatusJSON(t *testing.T, out string, ready bool) string {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("preparation must return machine-readable status: %v (output %q)", err, out)
	}
	encoded, _ := json.Marshal(result)
	lower := strings.ToLower(string(encoded))
	if ready {
		if !strings.Contains(lower, `"ready":true`) && !strings.Contains(lower, `"status":"ready"`) {
			t.Fatalf("preparation JSON does not report ready: %s", encoded)
		}
	} else {
		if !strings.Contains(lower, `"ready":false`) && !strings.Contains(lower, `"status":"not_ready"`) && !strings.Contains(lower, `"status":"not ready"`) {
			t.Fatalf("preparation JSON does not report non-ready: %s", encoded)
		}
		if !strings.Contains(lower, `"remedy"`) {
			t.Fatalf("non-ready preparation JSON omits its remedy: %s", encoded)
		}
	}
	return lower
}

func TestFormatPrepareTimestampNormalizesToUTCWithSeconds(t *testing.T) {
	stamp := "2026-09-20T15:04:05+03:00"
	if got := formatPrepareTimestamp(&stamp, "unknown"); got != "20 Sep 2026, 12:04:05 UTC" {
		t.Fatalf("formatted timestamp = %q, want UTC with seconds", got)
	}
}

func TestPrepareInputsRefreshesAndReportsCurrentContext(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	root := prepareInputsWorkspace(t, srv.URL)
	localDoc := filepath.Join(root, ".modernpath", "demo", "architecture", "current.md")
	if err := os.MkdirAll(filepath.Dir(localDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(localDoc, []byte("current local document"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeLocalExportTimestamp(t, root, "2026-09-20T12:00:00Z")
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.LastSyncAt = "2026-09-20T12:00:00Z"
	if err := config.WriteConfig(cfg); err != nil {
		t.Fatal(err)
	}
	authBefore, err := os.ReadFile(filepath.Join(root, ".modernpath", "auth.json"))
	if err != nil {
		t.Fatal(err)
	}

	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs: %v\n%s", err, out)
	}
	encoded := prepareStatusJSON(t, out, true)
	for _, want := range []string{"jane@example.com", "release-26", "USER:2026-09-25:select release-26", `"held_piece_count":2`, `"local_documents_last_synced_at":"2026-09-20T12:00:00Z"`, `"server_documents_last_updated_at":"2026-09-25T10:30:00Z"`, "RQ-268", "--piece"} {
		if !strings.Contains(encoded, strings.ToLower(want)) {
			t.Errorf("preparation output lacks %q:\n%s", want, out)
		}
	}
	for _, heldID := range []string{"EPIC-A", "EPIC-B"} {
		if strings.Contains(encoded, strings.ToLower(heldID)) {
			t.Errorf("preparation output described held piece %q instead of returning only the count:\n%s", heldID, out)
		}
	}
	if srv.downloads != 0 {
		t.Errorf("documentation export downloads = %d, want none", srv.downloads)
	}
	cfg, err = config.ReadConfig()
	if err != nil || cfg.SystemSlug != "" || cfg.LastSyncAt != "2026-09-20T12:00:00Z" {
		t.Errorf("preparation changed the binding or local sync stamp: config=%+v, err=%v", cfg, err)
	}
	authAfter, err := os.ReadFile(filepath.Join(root, ".modernpath", "auth.json"))
	if err != nil || !bytes.Equal(authBefore, authAfter) {
		t.Errorf("preparation changed auth.json: read err %v", err)
	}
	for _, path := range []string{filepath.Join("working-set", "GATES.md"), "release-latest.json"} {
		if _, err := os.Stat(filepath.Join(root, ".modernpath", path)); err == nil {
			t.Errorf("preparation wrote forbidden local state %s", path)
		}
	}
	for _, request := range srv.requests {
		if strings.HasPrefix(request, "POST /api/v1/") || strings.HasPrefix(request, "PUT /api/v1/") || strings.HasPrefix(request, "DELETE /api/v1/") {
			t.Errorf("preparation sent a process-state write: %s", request)
		}
	}
	wantRequests := []string{"GET /api/v1/sync/prepare-inputs?system_id=7"}
	if strings.Join(srv.requests, "\n") != strings.Join(wantRequests, "\n") {
		t.Errorf("ready preparation requests = %v, want exactly %v", srv.requests, wantRequests)
	}
}

func TestPrepareInputsIncludesApprovalsAddressedToCurrentUser(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/current.md": "current"}), http.StatusOK)
	setPrepareInputsContext(srv)
	srv.prepareContext["pending_decisions"] = []any{
		map[string]any{"id": "GATE-APPROVAL-42", "title": "Approve workspace access"},
		map[string]any{"id": "GATE-DECISION-43", "title": "Choose a retention period"},
	}
	prepareInputsWorkspace(t, srv.URL)

	out, err := runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs: %v\n%s", err, out)
	}
	var report prepareInputsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	want := []prepareInputsDecision{
		{ID: "GATE-APPROVAL-42", Title: "Approve workspace access"},
		{ID: "GATE-DECISION-43", Title: "Choose a retention period"},
	}
	if len(report.PendingDecisions) != len(want) {
		t.Fatalf("pending decisions = %+v, want %+v", report.PendingDecisions, want)
	}
	for i := range want {
		if report.PendingDecisions[i] != want[i] {
			t.Errorf("pending decision %d = %+v, want %+v", i, report.PendingDecisions[i], want[i])
		}
	}
}

func TestDocsSyncIncludesKnownExportCompanions(t *testing.T) {
	archive := zipWithFiles(t, map[string]string{
		".modernpath/demo/architecture/current.md": "system document",
		".modernpath/AGENTS.md":                    "export instructions",
		".modernpath/workflows/guide.md":           "workflow document",
		".modernpath/memories/context.md":          "project memory",
	})
	srv := newPrepareInputsServer(t, archive, http.StatusOK)
	root := prepareInputsWorkspace(t, srv.URL)
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync refused valid system export with companion files: %v\n%s", err, out)
	}
	for path, want := range map[string]string{
		"demo/architecture/current.md": "system document",
		"AGENTS.md":                    "export instructions",
		"workflows/guide.md":           "workflow document",
		"memories/context.md":          "project memory",
	} {
		got, readErr := os.ReadFile(filepath.Join(root, ".modernpath", path))
		if readErr != nil || string(got) != want {
			t.Errorf("exported %s = %q, err %v; want %q", path, got, readErr, want)
		}
	}
}

func TestDocsSyncServerTimestampBelongsToInstalledExportAndPrepareInputsReportsIt(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{
		".modernpath/demo/architecture/current.md": "synced document",
		".modernpath/demo/docs_push_manifest.json": `{"version":1,"system_doc_files":{},"generated_at":"2026-09-20T12:00:00Z"}`,
	}), http.StatusOK)
	setPrepareInputsContext(srv)
	root := prepareInputsWorkspace(t, srv.URL)
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync failed: %v\n%s", err, out)
	}

	manifest, err := os.ReadFile(filepath.Join(root, ".modernpath", "demo", "docs_push_manifest.json"))
	if err != nil {
		t.Fatalf("read installed export manifest: %v", err)
	}
	syncedAt := "2026-09-20T12:00:00Z"
	if !strings.Contains(string(manifest), `"generated_at":"`+syncedAt+`"`) {
		t.Fatalf("server timestamp missing from installed export manifest: %s", manifest)
	}
	cfg, err := config.ReadConfig()
	if err != nil {
		t.Fatalf("read shared config sync time: %v", err)
	}
	if cfg.LastSyncAt != syncedAt {
		t.Fatalf("shared config sync time = %q; want server timestamp %q", cfg.LastSyncAt, syncedAt)
	}

	out, err = runPrepareInputs(t)
	if err != nil {
		t.Fatalf("process prepare-inputs after sync: %v\n%s", err, out)
	}
	var report prepareInputsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode preparation report: %v\n%s", err, out)
	}
	if report.LocalDocumentsLastSyncedAt == nil || *report.LocalDocumentsLastSyncedAt != syncedAt {
		t.Errorf("prepare-inputs local timestamp = %v, want server timestamp %q", report.LocalDocumentsLastSyncedAt, syncedAt)
	}
}

func TestDocsSyncPreservesDistinctModuleAngleDocuments(t *testing.T) {
	apiPath := ".modernpath/demo/architecture/configuration/modules/tokens/api/tokens.md"
	dataPath := ".modernpath/demo/architecture/configuration/modules/tokens/data/tokens.md"
	archive := zipWithFiles(t, map[string]string{
		apiPath:  "# API tokens\n\nSee [data tokens](../data/tokens.md).\n",
		dataPath: "# Data tokens\n",
	})
	srv := newPrepareInputsServer(t, archive, http.StatusOK)
	root := prepareInputsWorkspace(t, srv.URL)
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync refused valid export with distinct module angle documents: %v\n%s", err, out)
	}

	apiFile := filepath.Join(root, filepath.FromSlash(apiPath))
	dataFile := filepath.Join(root, filepath.FromSlash(dataPath))
	apiContent, err := os.ReadFile(apiFile)
	if err != nil {
		t.Fatalf("read API tokens document: %v", err)
	}
	if string(apiContent) != "# API tokens\n\nSee [data tokens](../data/tokens.md).\n" {
		t.Errorf("API tokens document content = %q", apiContent)
	}
	dataContent, err := os.ReadFile(dataFile)
	if err != nil {
		t.Fatalf("relative Markdown link target %s was not preserved: %v", dataPath, err)
	}
	if string(dataContent) != "# Data tokens\n" {
		t.Errorf("data tokens document content = %q", dataContent)
	}
}

func TestDocsSyncRejectsInvalidArchivesWithoutReplacingCurrentExport(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
	}{
		{name: "empty archive", archive: zipWithFiles(t, map[string]string{})},
		{name: "wrong system root", archive: zipWithFiles(t, map[string]string{".modernpath/another-system/architecture/a.md": "wrong"})},
		{name: "CLI-owned root", archive: zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc", ".modernpath/rdd/AGENTS.md": "overwrite"})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPrepareInputsServer(t, tt.archive, http.StatusOK)
			root := prepareInputsWorkspace(t, srv.URL)
			oldDoc := filepath.Join(root, ".modernpath", "demo", "architecture", "old.md")
			if err := os.MkdirAll(filepath.Dir(oldDoc), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldDoc, []byte("known current export"), 0o644); err != nil {
				t.Fatal(err)
			}

			out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
			if err == nil {
				t.Fatalf("docs sync accepted invalid archive:\n%s", out)
			}
			got, readErr := os.ReadFile(oldDoc)
			if readErr != nil || string(got) != "known current export" {
				t.Errorf("failed refresh replaced the prior export: %q, %v", got, readErr)
			}
			if srv.downloads != 1 {
				t.Errorf("documentation export downloads = %d, want exactly one", srv.downloads)
			}
		})
	}
}

func TestDocsSyncReservedSlugUsesNestedRootAndPreservesTopLevel(t *testing.T) {
	for _, slug := range []string{"rdd", "working-set", "workflows", "tasks", "specs"} {
		t.Run(slug, func(t *testing.T) {
			srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/" + slug + "/new.md": "exported"}), http.StatusOK)
			srv.systems = []map[string]any{{"id": 7, "name": "Reserved", "slug": slug}}
			root := prepareInputsWorkspace(t, srv.URL)
			protected := filepath.Join(root, ".modernpath", slug, "prior.md")
			if err := os.MkdirAll(filepath.Dir(protected), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(protected, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
			if err != nil {
				t.Fatalf("reserved slug sync failed: %v\n%s", err, out)
			}
			if got, err := os.ReadFile(filepath.Join(root, ".modernpath", "docs", slug, "new.md")); err != nil || string(got) != "exported" {
				t.Errorf("nested reserved export = %q, %v", got, err)
			}
			if got, err := os.ReadFile(protected); err != nil || string(got) != "keep" {
				t.Errorf("reserved root changed: %q, %v", got, err)
			}
		})
	}
}

func TestDocsSyncRefreshesExistingLegacyRootInPlace(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{
		".modernpath/demo/new.md":                  "refreshed",
		".modernpath/demo/docs_push_manifest.json": `{"version":1,"system_doc_files":{},"generated_at":"2026-09-20T12:00:00.123456Z"}`,
	}), http.StatusOK)
	setPrepareInputsContext(srv)
	root := prepareInputsWorkspace(t, srv.URL)
	legacy := filepath.Join(root, ".modernpath", "docs", "demo")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "blueprint.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "stale.md"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err != nil {
		t.Fatalf("docs sync legacy root: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(legacy, "stale.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy root retained stale document: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(legacy, "new.md")); err != nil || string(got) != "refreshed" {
		t.Fatalf("refreshed legacy document = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath", "demo")); !os.IsNotExist(err) {
		t.Fatalf("sync created a duplicate flat export: %v", err)
	}
	out, err = runPrepareInputs(t)
	if err != nil {
		t.Fatalf("prepare-inputs after legacy sync: %v\n%s", err, out)
	}
	var report prepareInputsReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatal(err)
	}
	if report.LocalDocumentsLastSyncedAt == nil || *report.LocalDocumentsLastSyncedAt != "2026-09-20T12:00:00.123456Z" {
		t.Fatalf("prepare-inputs did not read the refreshed legacy manifest time: %v", report.LocalDocumentsLastSyncedAt)
	}
}

func TestDocsSyncReportsDownloadFailure(t *testing.T) {
	srv := newPrepareInputsServer(t, nil, http.StatusInternalServerError)
	root := prepareInputsWorkspace(t, srv.URL)
	oldDoc := filepath.Join(root, ".modernpath", "demo", "architecture", "old.md")
	if err := os.MkdirAll(filepath.Dir(oldDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldDoc, []byte("known current export"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil {
		t.Fatalf("docs sync reported success after download failure:\n%s", out)
	}
	if _, err := os.Stat(oldDoc); err != nil {
		t.Errorf("download failure removed prior export: %v", err)
	}
	if srv.downloads != 1 {
		t.Errorf("documentation export downloads = %d, want exactly one", srv.downloads)
	}
}

// Docs sync replaces only the validated bound-system export and its supported
// companion files, while preserving the previous state on failure.
func TestDocsSyncReplacesOnlyWithANonemptyBoundSystemExport(t *testing.T) {
	tests := []struct {
		name       string
		archive    []byte
		wantErr    bool
		wantRemove bool
	}{
		{
			name:       "valid export removes upstream-deleted files",
			archive:    zipWithFiles(t, map[string]string{".modernpath/demo/architecture/current.md": "current"}),
			wantRemove: true,
		},
		{name: "empty export refused", archive: zipWithFiles(t, map[string]string{}), wantErr: true},
		{name: "wrong system root refused", archive: zipWithFiles(t, map[string]string{".modernpath/other/architecture/a.md": "wrong"}), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPrepareInputsServer(t, tt.archive, http.StatusOK)
			root := prepareInputsWorkspace(t, srv.URL)
			oldDoc := filepath.Join(root, ".modernpath", "demo", "architecture", "removed-upstream.md")
			if err := os.MkdirAll(filepath.Dir(oldDoc), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(oldDoc, []byte("stale"), 0o644); err != nil {
				t.Fatal(err)
			}

			out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
			if (err != nil) != tt.wantErr {
				t.Fatalf("docs sync error = %v, wantErr %v\n%s", err, tt.wantErr, out)
			}
			_, staleErr := os.Stat(oldDoc)
			if tt.wantRemove && !os.IsNotExist(staleErr) {
				t.Errorf("successful replacement retained upstream-deleted file: stat err %v", staleErr)
			}
			if tt.wantErr && staleErr != nil {
				t.Errorf("failed replacement changed prior export: stat err %v", staleErr)
			}
			if srv.downloads != 1 {
				t.Errorf("documentation export downloads = %d, want exactly one", srv.downloads)
			}
		})
	}
}

func TestPrepareInputsPreflightFailuresAreDistinctAndDoNotDownload(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T, string, *prepareInputsServer)
		want      string
	}{
		{
			name: "missing credential",
			configure: func(t *testing.T, root string, _ *prepareInputsServer) {
				if err := config.WriteAuth(&config.Auth{}); err != nil {
					t.Fatal(err)
				}
			},
			want: "credential",
		},
		{
			name: "mismatched system binding",
			configure: func(_ *testing.T, _ string, srv *prepareInputsServer) {
				srv.prepareContext["system"] = map[string]any{"id": 8, "name": "Other", "slug": "other"}
			},
			want: "system id 8",
		},
		{
			name: "unsupported server contract",
			configure: func(_ *testing.T, _ string, srv *prepareInputsServer) {
				srv.prepareContext["contract_version"] = 2
			},
			want: "contract",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/a.md": "doc"}), http.StatusOK)
			root := prepareInputsWorkspace(t, srv.URL)
			tt.configure(t, root, srv)
			out, err := runPrepareInputs(t)
			if err == nil {
				t.Fatalf("preflight failure reported ready:\n%s", out)
			}
			encoded := prepareStatusJSON(t, out, false)
			if !strings.Contains(encoded, tt.want) {
				t.Errorf("preflight result does not identify %q:\n%s", tt.want, out)
			}
			wantRequests := 1
			if tt.name == "missing credential" {
				wantRequests = 0
			}
			if srv.downloads != 0 || len(srv.requests) != wantRequests {
				t.Errorf("preflight made extra requests: downloads=%d requests=%v", srv.downloads, srv.requests)
			}
		})
	}
}

func TestDocsSyncStampFailureRollsBackExport(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{
		".modernpath/demo/new.md":         "new",
		".modernpath/AGENTS.md":           "new companion",
		".modernpath/workflows/guide.md":  "new workflow",
		".modernpath/memories/context.md": "new memory",
	}), http.StatusOK)
	root := prepareInputsWorkspace(t, srv.URL)
	oldDoc := filepath.Join(root, ".modernpath", "demo", "old.md")
	if err := os.MkdirAll(filepath.Dir(oldDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldDoc, []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	const oldSync = "2026-09-15T08:00:00Z"
	writeLocalExportTimestamp(t, root, oldSync)
	oldCompanion := filepath.Join(root, ".modernpath", "AGENTS.md")
	if err := os.WriteFile(oldCompanion, []byte("previous companion"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldWorkflow := filepath.Join(root, ".modernpath", "workflows", "guide.md")
	if err := os.MkdirAll(filepath.Dir(oldWorkflow), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldWorkflow, []byte("previous workflow"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv.beforeFile = func() {
		configPath := filepath.Join(root, ".modernpath", "config.json")
		if err := os.Remove(configPath); err != nil {
			t.Error(err)
			return
		}
		if err := os.Mkdir(configPath, 0o755); err != nil {
			t.Error(err)
		}
	}

	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil {
		t.Fatalf("docs sync stamp failure reported success:\n%s", out)
	}
	if got, readErr := os.ReadFile(oldDoc); readErr != nil || string(got) != "previous" {
		t.Errorf("stamp failure did not restore previous export: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(filepath.Join(root, ".modernpath", "demo", "new.md")); !os.IsNotExist(statErr) {
		t.Errorf("stamp failure left the candidate export installed: %v", statErr)
	}
	if got, readErr := os.ReadFile(filepath.Join(root, ".modernpath", "demo", "docs_push_manifest.json")); readErr != nil || !strings.Contains(string(got), `"generated_at":"`+oldSync+`"`) {
		t.Errorf("stamp failure did not restore prior export manifest: %q, %v", got, readErr)
	}
	for path, want := range map[string]string{oldCompanion: "previous companion", oldWorkflow: "previous workflow"} {
		if got, readErr := os.ReadFile(path); readErr != nil || string(got) != want {
			t.Errorf("stamp failure did not restore %s: %q, %v", path, got, readErr)
		}
	}
	if _, statErr := os.Stat(filepath.Join(root, ".modernpath", "memories", "context.md")); !os.IsNotExist(statErr) {
		t.Errorf("stamp failure left a new companion installed: %v", statErr)
	}
}

func TestDocsSyncCompanionFailureRollsBackEarlierReplacement(t *testing.T) {
	srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{
		".modernpath/AGENTS.md":          "new companion",
		".modernpath/demo/new.md":        "new document",
		".modernpath/workflows/guide.md": "cannot replace directory",
	}), http.StatusOK)
	root := prepareInputsWorkspace(t, srv.URL)
	oldDoc := filepath.Join(root, ".modernpath", "demo", "old.md")
	if err := os.MkdirAll(filepath.Dir(oldDoc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldDoc, []byte("old document"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldCompanion := filepath.Join(root, ".modernpath", "AGENTS.md")
	if err := os.WriteFile(oldCompanion, []byte("old companion"), 0o644); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, ".modernpath", "workflows", "guide.md")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runCapturing(t, func() error { return docsSyncCmd.RunE(docsSyncCmd, nil) })
	if err == nil {
		t.Fatalf("docs sync accepted failed companion replacement: %s", out)
	}
	for path, want := range map[string]string{oldDoc: "old document", oldCompanion: "old companion"} {
		if got, readErr := os.ReadFile(path); readErr != nil || string(got) != want {
			t.Errorf("failed replacement did not restore %s: %q, %v", path, got, readErr)
		}
	}
	if info, statErr := os.Stat(blocked); statErr != nil || !info.IsDir() {
		t.Errorf("failed replacement changed the blocked companion directory: %v, %v", info, statErr)
	}
}

func TestPrepareInputsRequiresOneUserSourcedActiveRelease(t *testing.T) {
	release := map[string]any{"slug": "release-26", "status": "active", "source_tag": "USER:2026-09-25:select release-26"}
	tests := []struct {
		name      string
		configure func(*prepareInputsServer)
		want      string
	}{
		{"no active release", func(s *prepareInputsServer) { s.prepareContext["active_releases"] = []any{} }, "active release"},
		{"multiple active releases", func(s *prepareInputsServer) {
			s.prepareContext["active_releases"] = []any{release, map[string]any{"slug": "release-27", "status": "active"}}
		}, "active release"},
		{"no USER source", func(s *prepareInputsServer) {
			s.prepareContext["active_releases"] = []any{map[string]any{"slug": "release-26", "status": "active", "source_tag": "DOC:release"}}
		}, "USER"},
		{"missing release slug", func(s *prepareInputsServer) {
			s.prepareContext["active_releases"] = []any{map[string]any{"status": "active", "source_tag": "USER:2026-09-25"}}
		}, "active-release"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newPrepareInputsServer(t, zipWithFiles(t, map[string]string{".modernpath/demo/doc.md": "doc"}), http.StatusOK)
			tt.configure(srv)
			prepareInputsWorkspace(t, srv.URL)
			out, err := runPrepareInputs(t)
			if err == nil {
				t.Fatalf("invalid release context reported ready: %s", out)
			}
			if got := prepareStatusJSON(t, out, false); !strings.Contains(got, strings.ToLower(tt.want)) {
				t.Errorf("invalid release context was not explained: %s", out)
			}
		})
	}
}

// SR-CLI-026-001-C2: the existing status read exposes the current gap in the
// multi-piece branch that preparation must compose without selecting a piece.
func TestHeldPiecesRetainActiveReleaseSourceWhenSeveralAreHeld(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := config.WriteAuth(&config.Auth{Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/sync/work-selection":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"pieces": []string{"EPIC-A", "EPIC-B"}, "active_release": []map[string]any{{"slug": "release-26"}}})
		case "/api/v1/sync/gates":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"gates": []map[string]any{{
				"external_id": "GATE-RELEASE-release-26", "purpose": "release_selection", "state": "answered",
				"source_tag": "USER:2026-09-25:select release-26", "chosen_option_keys": []string{"approve"},
			}}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	env := &factoryEnv{Root: root, APIURL: srv.URL, SystemID: 7}
	text, _ := runCapturing(t, func() error { printHeldPieces(env); return nil })
	for _, want := range []string{"EPIC-A", "EPIC-B", "release-26", "USER:2026-09-25:select release-26"} {
		if !strings.Contains(text, want) {
			t.Errorf("multi-piece preparation output lacks %q:\n%s", want, text)
		}
	}
}
