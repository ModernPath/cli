package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pullResultRows(t *testing.T, stdout string) []map[string]any {
	t.Helper()
	result := decodeJSONOutput(t, stdout)
	values, ok := result["results"].([]any)
	if !ok {
		t.Fatalf("pull result has no result rows: %#v", result)
	}
	rows := make([]map[string]any, 0, len(values))
	for _, value := range values {
		row, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("pull result row is not an object: %#v", value)
		}
		rows = append(rows, row)
	}
	return rows
}

func TestSystemDocsPullUsesPathSegmentsForSameBasename(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{
		{"id": 1, "name": "README.md", "path_segments": []string{"source", "README.md"}, "has_encrypted_file": true},
		{"id": 3, "name": "README.md", "path_segments": []string{"tests", "README.md"}, "has_encrypted_file": true},
	})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, stdout)
	}
	rows := pullResultRows(t, stdout)
	got := map[float64]string{}
	for _, row := range rows {
		if id, ok := row["document_id"].(float64); ok {
			got[id] = row["path"].(string)
		}
	}
	want := map[float64]string{
		1: filepath.Join(outDir, "source", "README.md"),
		3: filepath.Join(outDir, "tests", "README.md"),
	}
	for id, path := range want {
		if got[id] != path {
			t.Errorf("document %v receipt path = %q, want %q", id, got[id], path)
		}
	}
	for file, content := range map[string]string{
		want[1]: "document one",
		want[3]: "document three",
	} {
		data, readErr := os.ReadFile(file)
		if readErr != nil || string(data) != content {
			t.Errorf("file %s = %q, err=%v; want %q", file, data, readErr, content)
		}
	}
}

func TestSystemDocsPullReceiptReportsActualContentFallbackPath(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 7, "name": "README.md", "path_segments": []string{"guides", "README.md"},
		"content": "the extracted text", "has_encrypted_file": false,
	}})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err != nil {
		t.Fatalf("pull failed: %v\n%s", err, stdout)
	}
	rows := pullResultRows(t, stdout)
	want := filepath.Join(outDir, "guides", "README.content.txt")
	if rows[0]["path"] != want || rows[0]["status"] != "succeeded" {
		t.Fatalf("fallback receipt = %#v, want successful actual path %q", rows[0], want)
	}
	data, err := os.ReadFile(want)
	if err != nil || !strings.Contains(string(data), "the extracted text") {
		t.Fatalf("fallback content = %q, err=%v", data, err)
	}
}

func TestSystemDocsPullDoesNotFallbackWhenEncryptedOriginalIsMissing(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 7, "name": "README.md", "path_segments": []string{"guides", "README.md"},
		"content": "the extracted text", "has_encrypted_file": true,
	}})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err == nil {
		t.Fatalf("missing encrypted original must fail: %s", stdout)
	}
	rows := pullResultRows(t, stdout)
	if rows[0]["status"] != "failed" || rows[0]["path"] != filepath.Join(outDir, "guides", "README.md") {
		t.Fatalf("missing original result = %#v", rows[0])
	}
	if _, statErr := os.Stat(filepath.Join(outDir, "guides", "README.content.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("unexpected content fallback was written: %v", statErr)
	}
}

func TestSystemDocsPullRejectsDuplicateResolvedPathsAndManifestCollision(t *testing.T) {
	t.Run("duplicate documents", func(t *testing.T) {
		srv := systemDocsServer(t, []map[string]any{
			{"id": 1, "name": "README.md", "path_segments": []string{"same", "README.md"}, "has_encrypted_file": true},
			{"id": 3, "name": "README.md", "path_segments": []string{"same", "README.md"}, "has_encrypted_file": true},
		})
		dir := bindSystemDocsWorkspace(t, srv)
		outDir := filepath.Join(dir, "out")
		stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
		if err == nil {
			t.Fatalf("duplicate resolved path must return failure: %s", stdout)
		}
		rows := pullResultRows(t, stdout)
		if rows[0]["status"] != "succeeded" || rows[1]["status"] != "failed" ||
			!strings.Contains(rows[1]["error"].(string), "output path conflict") {
			t.Fatalf("duplicate path results = %#v", rows)
		}
		data, readErr := os.ReadFile(filepath.Join(outDir, "same", "README.md"))
		if readErr != nil || string(data) != "document one" {
			t.Fatalf("first document was overwritten: content=%q err=%v", data, readErr)
		}
	})

	t.Run("manifest collision", func(t *testing.T) {
		srv := systemDocsServer(t, []map[string]any{{
			"id": 1, "name": "manifest.json", "path_segments": []string{"manifest.json"}, "has_encrypted_file": true,
		}})
		dir := bindSystemDocsWorkspace(t, srv)
		outDir := filepath.Join(dir, "out")
		stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
		if err == nil {
			t.Fatalf("manifest collision must return failure: %s", stdout)
		}
		rows := pullResultRows(t, stdout)
		if rows[0]["status"] != "failed" || !strings.Contains(rows[0]["error"].(string), "manifest.json") {
			t.Fatalf("manifest collision result = %#v", rows[0])
		}
		if data, readErr := os.ReadFile(filepath.Join(outDir, "manifest.json")); readErr != nil || !json.Valid(data) {
			t.Fatalf("manifest was not preserved: %q err=%v", data, readErr)
		}
	})
}

func TestSystemDocsPullRejectsUnsafePathSegments(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 7, "name": "outside.txt", "path_segments": []string{"..", "outside.txt"}, "content": "unsafe",
	}})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err == nil {
		t.Fatalf("unsafe path segments must fail: %s", stdout)
	}
	rows := pullResultRows(t, stdout)
	if rows[0]["status"] != "failed" {
		t.Fatalf("unsafe path result = %#v", rows[0])
	}
	if _, statErr := os.Stat(filepath.Join(dir, "outside.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("pull wrote outside output directory: %v", statErr)
	}
}

func TestSystemDocsPullRejectsPathThroughSymlinkInsideOutputRoot(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 1, "name": "README.md", "path_segments": []string{"linked", "README.md"}, "has_encrypted_file": true,
	}})
	dir := bindSystemDocsWorkspace(t, srv)
	outDir := filepath.Join(dir, "out")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(outDir, "linked")); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}

	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", outDir)
	if err == nil {
		t.Fatalf("symlink traversal must fail: %s", stdout)
	}
	rows := pullResultRows(t, stdout)
	if rows[0]["status"] != "failed" || !strings.Contains(rows[0]["error"].(string), "symlink") {
		t.Fatalf("symlink result = %#v", rows[0])
	}
	if _, statErr := os.Stat(filepath.Join(outside, "README.md")); !os.IsNotExist(statErr) {
		t.Fatalf("pull wrote outside output root: %v", statErr)
	}
}

func TestSystemDocsPathCollisionKeyAccountsForCaseInsensitiveFilesystems(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "README.md")
	if err := os.WriteFile(first, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	claimed := map[string]string{}
	if err := claimSystemDocPathsForOS(claimed, []string{first}, "document 1", "darwin"); err != nil {
		t.Fatal(err)
	}
	caseVariant := filepath.Join(dir, "readme.md")
	if err := claimSystemDocPathsForOS(claimed, []string{caseVariant}, "document 2", "darwin"); err == nil || !strings.Contains(err.Error(), "output path conflict") {
		t.Fatalf("case-variant path should collide on Darwin: %v", err)
	}
	data, err := os.ReadFile(first)
	if err != nil || string(data) != "original" {
		t.Fatalf("existing file changed: content=%q err=%v", data, err)
	}
}

func TestSystemDocsPullAllowsSymlinkAsSelectedOutputRoot(t *testing.T) {
	srv := systemDocsServer(t, []map[string]any{{
		"id": 1, "name": "README.md", "path_segments": []string{"source", "README.md"}, "has_encrypted_file": true,
	}})
	dir := bindSystemDocsWorkspace(t, srv)
	realRoot := filepath.Join(dir, "real-output")
	chosenRoot := filepath.Join(dir, "chosen-output")
	if err := os.MkdirAll(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, chosenRoot); err != nil {
		t.Skipf("symlinks are unavailable: %v", err)
	}
	stdout, _, err := runAskStreams(t, "system-docs", "pull", "--json", "--output", chosenRoot)
	if err != nil {
		t.Fatalf("pull to selected symlink root failed: %v\n%s", err, stdout)
	}
	if data, readErr := os.ReadFile(filepath.Join(realRoot, "source", "README.md")); readErr != nil || string(data) != "document one" {
		t.Fatalf("symlink-root file = %q err=%v", data, readErr)
	}
}
