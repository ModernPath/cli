package cmd

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func systemDocsServer(t *testing.T, docs []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/systems/42/documents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("metadata") == "true" {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"documents": docs}})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{"data": docs})
			}
		case http.MethodPost:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("parse upload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, header, err := r.FormFile("file")
			if err != nil {
				t.Errorf("read upload: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if strings.HasPrefix(header.Filename, "bad") {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = io.WriteString(w, `{"error":"ingestion_failed","message":"bad document"}`)
				return
			}
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"id": 31, "name": header.Filename, "document_type": r.FormValue("document_type"),
			}})
		}
	})
	mux.HandleFunc("/api/systems/42/documents/1/download", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "document one")
	})
	mux.HandleFunc("/api/systems/42/documents/2/download", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"error":"download_failed"}`)
	})
	mux.HandleFunc("/api/systems/42/documents/3/download", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "document three")
	})
	mux.HandleFunc("/api/systems/42/documents/24", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 24, "name": "spec.md", "document_type": "requirements", "content": "full document body", "summary": "short summary"}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func bindSystemDocsWorkspace(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeJSON(t, dir, ".modernpath/config.json", map[string]any{
		"api_url": srv.URL, "system_id": 42, "system_name": "Docs System",
	})
	writeTestFile(t, dir, ".modernpath/auth.json", `{"token":"t"}`)
	t.Chdir(dir)
	return dir
}

func decodeJSONOutput(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(stdout), &value); err != nil {
		t.Fatalf("stdout must contain one JSON value: %v\n%s", err, stdout)
	}
	return value
}

func TestSystemDocsListJSONIncludesIDsAndSelectedMetadataWithoutBodies(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 24, "name": "spec.md", "description": "API spec", "document_type": "requirements",
		"file_type": "text/markdown", "file_size": 19, "content": "contents", "summary": "summary",
		"provenance": "upload", "document_role": "guide", "source_revision": "r1",
		"processing_state": "ready", "metadata": map[string]any{"owner": "platform"},
	}})
	bindSystemDocsWorkspace(t, srv)

	stdout, stderr, err := runAskStreams(t, "system-docs", "list", "--json", "--full")
	if err != nil {
		t.Fatalf("list --json: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	result := decodeJSONOutput(t, stdout)
	if cursor, exists := result["next_cursor"]; !exists || cursor != nil {
		t.Fatalf("exhausted list must expose next_cursor null: %#v", result)
	}
	documents := result["items"].([]any)
	doc := documents[0].(map[string]any)
	if doc["id"] != float64(24) || doc["name"] != "spec.md" || doc["source_revision"] != "r1" {
		t.Fatalf("JSON must retain ID and server metadata: %v", doc)
	}
	if _, exists := doc["content"]; exists {
		t.Fatalf("list must not fetch document bodies: %v", doc)
	}
	if strings.TrimSpace(stderr) != "" || strings.Contains(stdout, "System Documents") {
		t.Fatalf("JSON mode leaked human output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSystemDocsHumanListIncludesDocumentIDs(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{"id": 24, "name": "spec.md", "document_type": "requirements"}})
	bindSystemDocsWorkspace(t, srv)
	stdout, _, err := runAskStreams(t, "system-docs", "list")
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "24  spec.md\n" {
		t.Fatalf("human list must show document ID for later pull/reference: %s", stdout)
	}
}

func TestSystemDocsGetReadsOneDocumentAndProjectsFields(t *testing.T) {
	srv := systemDocsServer(t, nil)
	bindSystemDocsWorkspace(t, srv)
	stdout, _, err := runAskStreams(t, "system-docs", "get", "24", "--json", "--fields", "id,name,content")
	if err != nil {
		t.Fatal(err)
	}
	result := decodeJSONOutput(t, stdout)
	if _, exists := result["items"]; exists {
		t.Fatalf("detail must not add a search envelope: %#v", result)
	}
	if result["id"] != float64(24) || result["name"] != "spec.md" || result["content"] != "full document body" {
		t.Fatalf("unexpected document projection: %v", result)
	}
}

func TestSystemDocsPushJSONReportsEveryOutcomeAndFailsOnPartialFailure(t *testing.T) {
	srv := systemDocsServer(t, nil)
	dir := bindSystemDocsWorkspace(t, srv)
	for _, name := range []string{"good.txt", "bad.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	stdout, stderr, err := runAskStreams(t, "system-docs", "push", "good.txt", "bad.txt", "--json")
	if err == nil {
		t.Fatalf("partial upload failure must return non-zero\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	result := decodeJSONOutput(t, stdout)
	if result["succeeded"] != float64(1) || result["failed"] != float64(1) || len(result["results"].([]any)) != 2 {
		t.Fatalf("JSON must report both item outcomes: %v", result)
	}
	if strings.Contains(stdout, "Uploading") || !json.Valid([]byte(stderr)) || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("partial failure must emit one JSON error and no human progress: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSystemDocsPushWithoutSupportedInputsReturnsError(t *testing.T) {
	srv := systemDocsServer(t, nil)
	dir := bindSystemDocsWorkspace(t, srv)
	if err := os.WriteFile(filepath.Join(dir, "unsupported.csv"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err := runAskStreams(t, "system-docs", "push", "unsupported.csv", "--json")
	if err == nil {
		t.Fatal("push with no supported inputs must fail")
	}
}

func TestSystemDocsListWithoutBoundSystemReturnsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	writeJSON(t, dir, ".modernpath/config.json", map[string]any{"api_url": "http://127.0.0.1:1", "system_id": 0})
	t.Chdir(dir)
	_, _, err := runAskStreams(t, "system-docs", "list", "--json")
	if err == nil {
		t.Fatal("list without a bound system must fail")
	}
}

func TestSystemDocsPullJSONReportsPerDocumentFailures(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{
		{"id": 1, "name": "one.txt", "document_type": "other", "file_type": "text/plain", "file_size": 12, "has_encrypted_file": true},
		{"id": 2, "name": "two.txt", "document_type": "other", "file_type": "text/plain", "file_size": 12, "has_encrypted_file": true},
	})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	stdout, stderr, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err == nil {
		t.Fatalf("partial download failure must return non-zero\nstdout=%s\nstderr=%s", stdout, stderr)
	}
	result := decodeJSONOutput(t, stdout)
	if result["succeeded"] != float64(2) || result["failed"] != float64(1) {
		t.Fatalf("JSON must report each document outcome: %v", result)
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "one.txt")); statErr != nil {
		t.Fatalf("successful download should be present: %v", statErr)
	}
	if strings.Contains(stdout, "Downloading") || !json.Valid([]byte(stderr)) || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("partial failure must emit one JSON error and no human progress: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSystemDocsPullRejectsNamesThatEscapeOutputDirectory(t *testing.T) {
	for _, name := range []string{"../outside.txt", "/tmp/outside.txt", `..\\outside.txt`, `C:\\outside.txt`} {
		t.Run(name, func(t *testing.T) {
			srv := systemDocsServer(t, []map[string]any{{"id": 8, "name": name, "content": "text"}})
			dir := bindSystemDocsWorkspace(t, srv)
			outDir := filepath.Join(dir, "out")
			stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
			if err == nil {
				t.Fatalf("unsafe server document name must fail\n%s", stdout)
			}
			result := decodeJSONOutput(t, stdout)
			items := result["results"].([]any)
			if items[0].(map[string]any)["status"] != "failed" {
				t.Fatalf("unsafe path must be reported as an item failure: %v", result)
			}
			if _, statErr := os.Stat(filepath.Join(dir, "outside.txt")); !os.IsNotExist(statErr) {
				t.Fatalf("pull wrote outside output directory: %v", statErr)
			}
		})
	}
}
