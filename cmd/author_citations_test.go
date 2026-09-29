package cmd

// SR-CLI-TYPED-CITATION-EDIT-001: typed citation edits preserve provenance,
// use guarded replacement, and reject invalid input before author writes.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAuthorUpdateTypedCitationsPreservesIdentity(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	citations := []map[string]any{
		{"kind": "code", "ref": "src/config.ts", "source_file_id": "immutable-file", "revision": "abc", "sha256": "digest"},
		{"kind": "test", "ref": "src/config.test.ts"},
	}
	body, _ := json.Marshal(citations)
	path := filepath.Join(t.TempDir(), "citations.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	record := (*got)["record"].(map[string]any)
	expected := make([]any, len(citations))
	for i, c := range citations {
		expected[i] = c
	}
	if !reflect.DeepEqual(record["source_citations"], expected) {
		t.Fatalf("typed sources lost: %#v", record)
	}
	if record["expected_fingerprint"] != strings.Repeat("a", 64) {
		t.Fatalf("CAS lost: %#v", record)
	}
	if _, ok := record["work_status"]; ok {
		t.Fatal("citation edit must not advance status")
	}
}

func TestAuthorUpdateTypedCitationsRejectsInvalidInputBeforePosting(t *testing.T) {
	for _, body := range []string{`null`, `{}`, `[null]`, `["code"]`, `[{"kind":"code"}]`, `[{"ref":"src/a.ts"}]`, `[`, `[{"kind":" " ,"ref":"src/a.ts"}]`, `[{"kind":"code","source_tag":"USER:x"}]`, `[{"kind":"process_source","source_tag":" "}]`} {
		t.Run(body, func(t *testing.T) {
			srv, got := authorCapture(t)
			cobraWorkspace(t, srv)
			path := filepath.Join(t.TempDir(), "citations.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
			if err == nil || *got != nil {
				t.Fatalf("invalid input posted: err=%v record=%v", err, *got)
			}
		})
	}
}

func TestAuthorUpdateTypedCitationsRefusesNonStringRef(t *testing.T) {
	for _, kind := range []string{"code", "test"} {
		for _, tc := range []struct {
			name string
			ref  any
		}{
			{"object", map[string]any{}},
			{"array", []any{}},
			{"number", 12},
			{"boolean", true},
			{"null", nil},
		} {
			srv, got := authorCapture(t)
			cobraWorkspace(t, srv)
			body, err := json.Marshal([]map[string]any{{
				"kind": kind, "source_file_id": "captured-file", "ref": tc.ref,
			}})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "citations.json")
			if err := os.WriteFile(path, body, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err = runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
			if err == nil || !strings.Contains(err.Error(), "citation 1 ref must be a string") || *got != nil {
				t.Errorf("%s/%s: malformed ref must be refused before posting: err=%v record=%v", kind, tc.name, err, *got)
			}
		}
	}
}

func TestAuthorUpdateTypedCitationsRefusesCompetingSourceFlags(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", "sources.json", "--source", "USER:decision", "--expected-fingerprint", strings.Repeat("a", 64))
	if err == nil || *got != nil {
		t.Fatalf("conflicting source flags posted: err=%v record=%v", err, *got)
	}
}

// AC1: existing --source citations and typed metadata survive replacement together.
func TestAuthorUpdateTypedCitationsLegacyProcessSources(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	citations := sourceCitations([]string{"USER:2026-09-27:approved", "DOC:docs/design.md"})
	citations = append(citations,
		map[string]any{"kind": "process_source", "id": "process-source-id"},
		map[string]any{"kind": "document", "system_doc_id": "document-id", "version": "v3", "fingerprint": "document-hash"},
		map[string]any{"kind": "code", "source_file_id": "captured-file", "repository_key": "core", "revision": "abc", "path": "src/a.ts", "sha256": "file-hash", "locator": map[string]any{"start_line": float64(12)}},
	)
	body, err := json.Marshal(citations)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "citations.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = runRoot(t, "author", "update", "UR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	expected := make([]any, len(citations))
	for i, citation := range citations {
		expected[i] = citation
	}
	record := (*got)["record"].(map[string]any)
	if !reflect.DeepEqual(record["source_citations"], expected) {
		t.Fatalf("legacy or typed metadata changed: %#v", record)
	}
	if record["external_id"] != "UR-CANDIDATE" || record["kind"] != "requirement" {
		t.Fatalf("wrong target: %#v", record)
	}
}

// AC4: clearing sends an explicit empty array, rather than omitting the field.
func TestAuthorUpdateTypedCitationsClearsCitations(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	path := filepath.Join(t.TempDir(), "citations.json")
	if err := os.WriteFile(path, []byte("[]"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	record := (*got)["record"].(map[string]any)
	citations, ok := record["source_citations"].([]any)
	if !ok || len(citations) != 0 {
		t.Fatalf("clear must send [], got %#v", record)
	}
}

// AC2: a content-only update must leave the server's citation set alone.
func TestAuthorUpdateTypedCitationsOmittedPreservesCitations(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--title", "Updated title", "--expected-fingerprint", strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	record := (*got)["record"].(map[string]any)
	if _, present := record["source_citations"]; present {
		t.Fatalf("omission must preserve citations, got %#v", record)
	}
}

// AC4: failed reads retain their cause and never post an empty replacement.
func TestAuthorUpdateTypedCitationsMissingFileRefused(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	path := filepath.Join(t.TempDir(), "missing.json")
	_, err := runRoot(t, "author", "update", "SR-CANDIDATE", "--citations-file", path, "--expected-fingerprint", strings.Repeat("a", 64))
	if !errors.Is(err, fs.ErrNotExist) || *got != nil {
		t.Fatalf("expected read error before posting: err=%v record=%v", err, *got)
	}
}

// AC3: backlog writes must refuse requirement citation fields explicitly.
func TestAuthorUpdateTypedCitationsBacklogRefused(t *testing.T) {
	srv, got := authorCapture(t)
	cobraWorkspace(t, srv)
	_, err := runRoot(t, "author", "update", "BACKLOG-CROSS-0001", "--kind", "backlog", "--citations-file", "unused.json", "--expected-fingerprint", strings.Repeat("a", 64))
	if err == nil || !strings.Contains(err.Error(), "a backlog record has no such field") || *got != nil {
		t.Fatalf("expected backlog refusal before posting: err=%v record=%v", err, *got)
	}
}
