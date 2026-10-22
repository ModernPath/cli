package cmd

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

func TestPrepareReportsReadOnlySessionAndNextSteps(t *testing.T) {
	oldVersion := Version
	Version = "0.20.0"
	defer func() { Version = oldVersion }()
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/releases/latest":
			_, _ = w.Write([]byte(`{"tag_name":"v0.21.0"}`))
		case "/api/systems":
			_, _ = w.Write([]byte(`[{"id":17,"name":"Example"}]`))
		case "/api/systems/17/documents":
			_, _ = w.Write([]byte(`{"data":{"documents":[{"id":2,"updated_at":"2026-10-20T12:00:00.123456"}],"next_cursor":null}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	oldLookupURL := releaseLookupURL
	releaseLookupURL = srv.URL + "/releases/latest"
	defer func() { releaseLookupURL = oldLookupURL }()
	root := t.TempDir()
	t.Chdir(root)
	if err := os.MkdirAll(filepath.Join(root, ".modernpath", "example"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "example", "docs_push_manifest.json"), []byte(`{"generated_at":"2026-10-19T12:00:00Z"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(&config.Config{APIURL: srv.URL, SystemID: 17, SystemName: "Example", SystemSlug: "example"}); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteAuth(&config.Auth{Token: "test-token", Actor: "alice@example.test"}); err != nil {
		t.Fatal(err)
	}
	command, _, err := rootCmd.Find([]string{"prepare"})
	if err != nil || command.Name() != "prepare" {
		t.Fatalf("prepare command missing: %v, %s", err, command.Name())
	}
	var out bytes.Buffer
	command.SetOut(&out)
	defer command.SetOut(nil)
	if err := command.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer command.Flags().Set("json", "false")
	// The temp workspace has no installed kit, so prepare must return a
	// non-zero decision while still emitting a clean JSON report.
	if err := command.RunE(command, nil); err == nil {
		t.Fatal("missing kit must keep the command non-zero")
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, out.String())
	}
	if report["docs_state"] != "newer_edits" {
		t.Fatalf("docs_state = %v; report=%s; requests=%v", report["docs_state"], out.String(), methods)
	}
	if report["actor"] != "alice@example.test" || report["system_id"] != float64(17) {
		t.Fatalf("identity missing: %v", report)
	}
	if report["latest_cli"] != "0.21.0" {
		t.Fatalf("released build must read latest release: %v", report["latest_cli"])
	}
	if report["session_detail"] != "credential accepted by the server" {
		t.Fatalf("online verification must replace the local-only detail: %v", report["session_detail"])
	}
	if !strings.Contains(out.String(), "modernpath docs sync") {
		t.Fatalf("missing next step: %s", out.String())
	}
	steps := report["next_steps"].([]any)
	if steps[0] != "brew update && brew upgrade modernpath" || steps[2] != "modernpath install" {
		t.Fatalf("released build must upgrade before install: %v", steps)
	}
	for _, method := range methods {
		if !strings.HasPrefix(method, "GET ") {
			t.Fatalf("prepare made a write request: %v", methods)
		}
	}
}

func TestPrepareDoesNotTreatAnotherSystemsExportAsUsable(t *testing.T) {
	root := seedStatusWorkspace(t)
	seedExport(t, filepath.Join(root, ".modernpath", "other"))
	report := prepareReport{DocsState: "unknown"}
	localDocsState(root, &config.Config{SystemID: 17, SystemSlug: "bound"}, &report)
	if report.DocsState != "missing" {
		t.Fatalf("wrong-System export must be missing, got %+v", report)
	}
}

func TestPrepareNeverCallsOlderDocumentsCurrentAndHandlesRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		body string
	}{
		{"older document", http.StatusOK, `{"data":{"documents":[{"updated_at":"2026-10-18T00:00:00"}],"next_cursor":null}}`},
		{"unauthorized", http.StatusUnauthorized, `{"error":"unauthorized"}`},
		{"malformed success", http.StatusOK, `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			report := prepareReport{DocsState: "unknown", ExportGeneratedAt: "2026-10-19T00:00:00Z"}
			checkLiveDocuments(&authenticatedClient{client: srv.Client(), baseURL: srv.URL, token: "test"}, 17, &report)
			if report.DocsState != "unknown" {
				t.Fatalf("must not assert current: %+v", report)
			}
			if tc.name == "older document" && report.DocsMetadata != "checked" {
				t.Fatalf("real terminal page with null cursor must count as checked: %+v", report)
			}
			if tc.name == "malformed success" && report.DocsMetadata != "unavailable" {
				t.Fatalf("malformed success must be unavailable: %+v", report)
			}
			if report.DocsDetail == "" {
				t.Fatal("missing explanation")
			}
		})
	}
}

func TestPrepareTreatsUnreadableCredentialAsUnknown(t *testing.T) {
	root := seedStatusWorkspace(t)
	t.Chdir(root)
	if err := config.WriteConfig(&config.Config{APIURL: "http://127.0.0.1:1", SystemID: 17}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".modernpath", "auth.json"), []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	prepareCmd.SetOut(&out)
	defer prepareCmd.SetOut(nil)
	if err := prepareCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	defer prepareCmd.Flags().Set("json", "false")
	if err := runPrepare(prepareCmd, nil); err == nil {
		t.Fatal("unreadable credential must fail")
	}
	var report prepareReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Session != "unknown" || strings.Contains(strings.Join(report.NextSteps, " "), "modernpath auth --") {
		t.Fatalf("unreadable state is not a missing credential: %+v", report)
	}
}

func TestPrepareDistinguishesRejectedCredentialFromServerFailure(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       int
		session    string
		authRemedy bool
	}{
		{"rejected", http.StatusUnauthorized, "unavailable", true},
		{"server failure", http.StatusServiceUnavailable, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.code)
			}))
			defer srv.Close()
			t.Chdir(seedStatusWorkspace(t))
			if err := config.WriteConfig(&config.Config{APIURL: srv.URL, SystemID: 17, SystemSlug: "example"}); err != nil {
				t.Fatal(err)
			}
			if err := config.WriteAuth(&config.Auth{Token: "test-token", Actor: "alice@example.test"}); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			prepareCmd.SetOut(&out)
			defer prepareCmd.SetOut(nil)
			if err := prepareCmd.Flags().Set("json", "true"); err != nil {
				t.Fatal(err)
			}
			defer prepareCmd.Flags().Set("json", "false")
			if err := runPrepare(prepareCmd, nil); err == nil {
				t.Fatal("failed server verification must exit non-zero")
			}
			var report prepareReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("JSON was contaminated: %v: %q", err, out.String())
			}
			if report.Session != tc.session || report.Access == "reachable" {
				t.Fatalf("misleading status: %+v", report)
			}
			if got := strings.Contains(strings.Join(report.NextSteps, " "), "modernpath auth --"); got != tc.authRemedy {
				t.Fatalf("auth remedy = %t, want %t: %+v", got, tc.authRemedy, report)
			}
			if calls != 1 {
				t.Fatalf("want one server probe, got %d", calls)
			}
		})
	}
}
