package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// protectedModernPathRoots are top-level CLI state or exported companion
// roots. A system may use one of these names, but its documents live below
// .modernpath/docs/<slug> so sync cannot replace the protected directory.
var protectedModernPathRoots = map[string]bool{
	"artifacts": true, "bin": true, "datamodel": true, "dev-core-token": true,
	"docs": true, "evidence": true, "focus-signals": true, "hooks": true,
	"last-sync-ok": true, "local-sources": true, "memories": true,
	"mp_api_key": true, "organization": true, "patterns": true, "rdd": true,
	"runtime": true, "scripts": true, "skills": true, "source": true,
	"source_docs": true, "specs": true, "system-docs": true, "tasks": true,
	"tmp": true, "verification": true, "viewer": true, "workflows": true,
	"working-set": true, "worktrees": true, "your-move": true,
}

func modernpathReservedTopDir(name string) bool {
	return protectedModernPathRoots[strings.ToLower(name)]
}

func hasSystemRootMarkers(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "docs_push_manifest.json")); err == nil {
		return true
	}
	if _, err := os.Stat(filepath.Join(dir, "blueprint.json")); err == nil {
		return true
	}
	return false
}

func validateSystemExportSlug(slug string) error {
	if slug == "" || filepath.Base(slug) != slug || slug == "." || slug == ".." {
		return fmt.Errorf("invalid bound system slug %q", slug)
	}
	for _, char := range slug {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return fmt.Errorf("invalid bound system slug %q", slug)
		}
	}
	return nil
}

// systemExportRootDir chooses an existing meaningful export when safe. A
// reserved flat root is never selected for replacement because it may contain
// CLI state mixed with an old export.
func systemExportRootDir(modernpathRoot, slug string) string {
	flat := filepath.Join(modernpathRoot, slug)
	nested := filepath.Join(modernpathRoot, "docs", slug)
	if modernpathReservedTopDir(slug) {
		return nested
	}
	if hasSystemRootMarkers(flat) {
		return flat
	}
	if hasSystemRootMarkers(nested) {
		return nested
	}
	return flat
}

func remapSystemExportPath(name, slug, targetRoot string) string {
	from := filepath.ToSlash(filepath.Join(".modernpath", slug))
	to := filepath.ToSlash(filepath.Join(".modernpath", targetRoot))
	name = filepath.ToSlash(name)
	if name == from {
		return to
	}
	if strings.HasPrefix(name, from+"/") {
		return to + strings.TrimPrefix(name, from)
	}
	return name
}

func exportGeneratedAt(zipData []byte, slug string) (string, error) {
	if err := validateSystemExportSlug(slug); err != nil {
		return "", err
	}
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", fmt.Errorf("open export zip: %w", err)
	}
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		if name != ".modernpath/"+slug+"/docs_push_manifest.json" &&
			name != ".modernpath/docs/"+slug+"/docs_push_manifest.json" {
			continue
		}
		r, err := entry.Open()
		if err != nil {
			return "", fmt.Errorf("open export manifest: %w", err)
		}
		data, decodeErr := io.ReadAll(r)
		closeErr := r.Close()
		if decodeErr != nil {
			return "", fmt.Errorf("read export manifest: %w", decodeErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("close export manifest: %w", closeErr)
		}
		return decodeExportTimestamp(data)
	}
	return "", nil
}

func readExportGeneratedAt(root, slug string) (*string, error) {
	manifestPath := filepath.Join(systemExportRootDir(root, slug), "docs_push_manifest.json")
	data, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stamp, err := decodeExportTimestamp(data)
	if err != nil || stamp == "" {
		return nil, err
	}
	return &stamp, nil
}

func decodeExportTimestamp(data []byte) (string, error) {
	var manifest struct {
		GeneratedAt string `json:"generated_at"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	if manifest.GeneratedAt == "" {
		return "", nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, manifest.GeneratedAt)
	if err != nil {
		return "", fmt.Errorf("export manifest has invalid generated_at timestamp %q", manifest.GeneratedAt)
	}
	return parsed.UTC().Format(time.RFC3339Nano), nil
}
