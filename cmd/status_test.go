package cmd

// REQ-CROSS-096: `modernpath status` names which sync it reports, and finds
// the export that is on disk.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modernpath/cli/internal/config"
)

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

// A workspace bound by `factory connect` has no system_slug — the export on
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

// The two sync lanes are written by different commands to different places.
// A state sync minutes ago must not be hidden behind a day-old docs sync.
func TestStatusReportsBothSyncLanesSeparately(t *testing.T) {
	root := seedStatusWorkspace(t)
	const stateStamp = "2026-08-13T08:59:39Z"
	if err := os.WriteFile(lastSyncOkPath(root), []byte(stateStamp+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{SystemID: 243, LastSyncAt: "2026-08-12T11:51:18+03:00"}
	rep := buildStatusReport(configDirOf(root), cfg)

	if rep.StateSyncAt != stateStamp {
		t.Errorf("state sync = %q, want %q (from .modernpath/last-sync-ok)", rep.StateSyncAt, stateStamp)
	}
	if rep.DocsSyncAt != cfg.LastSyncAt {
		t.Errorf("docs sync = %q, want %q (from config last_sync_at)", rep.DocsSyncAt, cfg.LastSyncAt)
	}
	if rep.StateSyncAt == rep.DocsSyncAt {
		t.Error("the two lanes must stay distinct; neither may be presented as the other")
	}
}

// The guard: no stamp means the state sync has never succeeded here, and the
// docs timestamp must not be borrowed to fill the gap.
func TestStatusReportsNoStateSyncWithoutAStamp(t *testing.T) {
	root := seedStatusWorkspace(t)

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243, LastSyncAt: "2026-08-12T11:51:18+03:00"})

	if rep.StateSyncAt != "" {
		t.Errorf("state sync = %q, want empty when last-sync-ok is absent", rep.StateSyncAt)
	}
}

// The two lanes are written by commands that disagree on zone: `docs sync`
// records a local offset, `factory sync` records Z. Reading the pair must not
// require converting between them in your head.
func TestStatusRendersBothLanesInOneZone(t *testing.T) {
	helsinki := time.FixedZone("EEST", 3*60*60)

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "a Z stamp from factory sync",
			raw:  "2026-08-13T09:12:33Z",
			want: "2026-08-13T12:12:33+03:00",
		},
		{
			name: "a local-offset stamp from docs sync is already there",
			raw:  "2026-08-13T11:06:15+03:00",
			want: "2026-08-13T11:06:15+03:00",
		},
		{
			name: "a stamp from another offset is the same instant, re-expressed",
			raw:  "2026-08-13T11:06:15-04:00",
			want: "2026-08-13T18:06:15+03:00",
		},
		{
			name: "a winter stamp keeps the offset it was written with",
			raw:  "2026-01-30T17:53:00+02:00",
			want: "2026-01-30T18:53:00+03:00",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderSyncTime(tt.raw, helsinki); got != tt.want {
				t.Errorf("renderSyncTime(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// The guard: a hand-edited or older-format value is shown as recorded rather
// than dropped or guessed at. Display normalization may not lose a fact.
func TestStatusShowsAnUnparseableStampAsRecorded(t *testing.T) {
	helsinki := time.FixedZone("EEST", 3*60*60)

	for _, raw := range []string{"", "yesterday", "2026-08-13 09:12:33"} {
		if got := renderSyncTime(raw, helsinki); got != raw {
			t.Errorf("renderSyncTime(%q) = %q, want it shown as recorded", raw, got)
		}
	}
}

// A stamp written by an older CLI, or truncated, still proves a sync happened.
func TestStatusFallsBackToTheStampFileTime(t *testing.T) {
	root := seedStatusWorkspace(t)
	if err := os.WriteFile(lastSyncOkPath(root), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rep := buildStatusReport(configDirOf(root), &config.Config{SystemID: 243})

	if rep.StateSyncAt == "" {
		t.Error("an unparseable stamp still evidences a sync; fall back to the file time")
	}
}

// REQ-CROSS-117 — `State Sync: Never (run 'modernpath factory sync')` printed an
// instruction that could not change the line it sat under.
//
// .modernpath/last-sync-ok had exactly one writer, at the tail of
// factorySyncQuiescent, reachable only through `factory sync --if-quiescent`
// from a hook. A plain `factory sync` went to factorySyncRun and stamped
// nothing. So a workspace with no sync hooks — the state `hooks doctor` and
// `hooks status` exist to detect — reported "Never" for ever: the user follows
// the printed advice, the sync succeeds, and the line does not move.
func acceptingSyncServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"results":[]}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAManualSyncStampsTheStateSyncLane(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := acceptingSyncServer(t)
	env := &factoryEnv{APIURL: srv.URL, token: "t", Root: root, SystemID: 243}

	if err := factorySyncRun(env, false); err != nil {
		t.Fatalf("the stub accepts the batch: %v", err)
	}

	stamp := readStateSyncStamp(lastSyncOkPath(root))
	if stamp == "" {
		t.Fatal("a successful `factory sync` left no State Sync stamp, so status still reports Never and its advice cannot be followed")
	}
	if _, err := time.Parse(time.RFC3339, stamp); err != nil {
		t.Fatalf("the stamp must parse as RFC3339 for renderSyncTime: %v", err)
	}
}

// Guard: a dry run reports what WOULD be sent. Stamping it would claim a sync
// that never reached the server.
func TestADryRunDoesNotStampTheStateSyncLane(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := &factoryEnv{APIURL: "http://127.0.0.1:1", token: "t", Root: root, SystemID: 243}
	_ = factorySyncRun(env, true)

	if readStateSyncStamp(lastSyncOkPath(root)) != "" {
		t.Fatal("--dry-run stamped a sync that never happened")
	}
}

// Guard: the hook lane debounces on this file, so both lanes must write the
// identical shape — UTC RFC3339 with a trailing newline.
func TestBothSyncLanesWriteTheSameStampShape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := acceptingSyncServer(t)
	env := &factoryEnv{APIURL: srv.URL, token: "t", Root: root, SystemID: 243}
	if err := factorySyncRun(env, false); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(lastSyncOkPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(body), "\n") {
		t.Fatalf("the hook lane writes a trailing newline; this one must match: %q", body)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(string(body))); err != nil {
		t.Fatalf("not the UTC RFC3339 the hook lane writes: %q", body)
	}
}

// A sync that landed but could not stamp is a contradiction in the making:
// status keeps reporting the previous sync and the hook debounce misjudges.
// The write failure is returned so the sync can say so.
func TestRecordSyncSucceededReportsAFailedStampWrite(t *testing.T) {
	root := seedStatusWorkspace(t)
	// last-sync-ok occupied by a directory: the stamp cannot be written
	if err := os.MkdirAll(lastSyncOkPath(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := recordSyncSucceeded(root); err == nil {
		t.Fatal("a failed stamp write must be reported, not swallowed")
	}
	if err := os.RemoveAll(lastSyncOkPath(root)); err != nil {
		t.Fatal(err)
	}
	if err := recordSyncSucceeded(root); err != nil {
		t.Fatalf("a writable stamp must succeed: %v", err)
	}
}

// ---------------------------------------------------- REQ-CROSS-282 reachability

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
