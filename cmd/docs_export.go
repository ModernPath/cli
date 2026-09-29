package cmd

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/config"
)

type docsExportCompanion struct {
	target    string
	backup    string
	backedUp  bool
	installed bool
}

type docsExportInstall struct {
	root           string
	targetRel      string
	stage          string
	boundBackedUp  bool
	boundInstalled bool
	companions     []docsExportCompanion
}

func (install *docsExportInstall) commit() { _ = os.RemoveAll(install.stage) }

func (install *docsExportInstall) rollback() error {
	var failures []error
	for i := len(install.companions) - 1; i >= 0; i-- {
		companion := install.companions[i]
		if companion.installed {
			if err := os.Remove(companion.target); err != nil && !os.IsNotExist(err) {
				failures = append(failures, err)
			}
		}
		if companion.backedUp {
			if err := os.Rename(companion.backup, companion.target); err != nil {
				failures = append(failures, err)
			}
		}
	}
	target := filepath.Join(install.root, config.ConfigDir, filepath.FromSlash(install.targetRel))
	if install.boundInstalled {
		if err := os.RemoveAll(target); err != nil {
			failures = append(failures, err)
		}
	}
	if install.boundBackedUp {
		if err := os.Rename(filepath.Join(install.stage, ".backup-system"), target); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) != 0 {
		return fmt.Errorf("%w (backup at %s)", errors.Join(failures...), install.stage)
	}
	_ = os.RemoveAll(install.stage)
	return nil
}

// replaceSystemDocsExport validates into a sibling staging directory before
// replacing the bound system tree and exported companion files.
func replaceSystemDocsExport(root, slug string, zipData []byte) (string, *docsExportInstall, error) {
	configRoot := filepath.Join(root, config.ConfigDir)
	if err := validateSystemExportSlug(slug); err != nil {
		return "", nil, err
	}
	target := systemExportRootDir(configRoot, slug)
	relTarget, err := filepath.Rel(configRoot, target)
	if err != nil || relTarget == ".." || strings.HasPrefix(relTarget, ".."+string(filepath.Separator)) {
		return "", nil, fmt.Errorf("invalid system export destination for %q", slug)
	}
	reader, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", nil, fmt.Errorf("open export zip: %w", err)
	}
	stage, err := os.MkdirTemp(configRoot, ".docs-sync-stage-")
	if err != nil {
		return "", nil, fmt.Errorf("create staging directory: %w", err)
	}
	keepStage := false
	defer func() {
		if !keepStage {
			_ = os.RemoveAll(stage)
		}
	}()
	originalBoundPrefix := filepath.ToSlash(filepath.Join(config.ConfigDir, slug)) + "/"
	boundPrefix := filepath.ToSlash(filepath.Join(config.ConfigDir, relTarget)) + "/"
	documentCount := 0
	var companions []string
	for _, entry := range reader.File {
		name := filepath.ToSlash(entry.Name)
		if strings.HasPrefix(name, "__MACOSX/") || strings.Contains(name, ".DS_Store") || entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, config.ConfigDir+"/") {
			continue // SQLite and other non-document export members are not installed.
		}
		bound := strings.HasPrefix(name, originalBoundPrefix)
		if !bound && !isSystemExportCompanion(name, slug) {
			return "", nil, fmt.Errorf("archive contains %s outside the bound root %s", name, strings.TrimSuffix(boundPrefix, "/"))
		}
		if bound && strings.HasSuffix(strings.ToLower(name), ".md") {
			documentCount++
		}
		remapped := filepath.Clean(filepath.FromSlash(remapSystemExportPath(name, slug, relTarget)))
		if filepath.IsAbs(remapped) || remapped == ".." || strings.HasPrefix(remapped, ".."+string(filepath.Separator)) {
			return "", nil, fmt.Errorf("archive path escapes the bound export: %s", name)
		}
		remappedSlash := filepath.ToSlash(remapped)
		if bound && !strings.HasPrefix(remappedSlash, boundPrefix) ||
			!bound && !isSystemExportCompanion(remappedSlash, slug) {
			return "", nil, fmt.Errorf("archive path remaps outside the validated export: %s", name)
		}
		dest := filepath.Join(stage, remapped)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", nil, err
		}
		src, err := entry.Open()
		if err != nil {
			return "", nil, fmt.Errorf("open archive member %s: %w", name, err)
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			src.Close()
			return "", nil, fmt.Errorf("create staged document %s: %w", name, err)
		}
		_, copyErr := io.Copy(out, src)
		closeErr := out.Close()
		srcErr := src.Close()
		if copyErr != nil {
			return "", nil, copyErr
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
		if srcErr != nil {
			return "", nil, srcErr
		}
		if !bound {
			companions = append(companions, remapped)
		}
	}
	if documentCount == 0 {
		return "", nil, errors.New("archive contains no Markdown documents for the bound system")
	}

	stagedExport := filepath.Join(stage, config.ConfigDir, relTarget)
	install := &docsExportInstall{root: root, targetRel: relTarget, stage: stage}
	fail := func(err error) (string, *docsExportInstall, error) {
		if rollbackErr := install.rollback(); rollbackErr != nil {
			keepStage = true // leave the backup for manual recovery
			return "", nil, fmt.Errorf("%v; restore export: %w", err, rollbackErr)
		}
		return "", nil, err
	}
	if _, err := os.Lstat(target); err == nil {
		if err := os.Rename(target, filepath.Join(stage, ".backup-system")); err != nil {
			return fail(fmt.Errorf("stage prior export: %w", err))
		}
		install.boundBackedUp = true
	} else if !os.IsNotExist(err) {
		return fail(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fail(fmt.Errorf("create export parent: %w", err))
	}
	if err := os.Rename(stagedExport, target); err != nil {
		return fail(fmt.Errorf("install staged export: %w", err))
	}
	install.boundInstalled = true
	for _, rel := range companions {
		from := filepath.Join(stage, rel)
		to := filepath.Join(root, rel)
		change := docsExportCompanion{target: to, backup: filepath.Join(stage, ".backup-companions", rel)}
		install.companions = append(install.companions, change)
		current := &install.companions[len(install.companions)-1]
		if info, err := os.Lstat(to); err == nil {
			if !info.Mode().IsRegular() {
				return fail(fmt.Errorf("export companion is not a regular file: %s", rel))
			}
			if err := os.MkdirAll(filepath.Dir(current.backup), 0o755); err != nil {
				return fail(err)
			}
			if err := os.Rename(to, current.backup); err != nil {
				return fail(err)
			}
			current.backedUp = true
		} else if !os.IsNotExist(err) {
			return fail(err)
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fail(err)
		}
		if err := os.Rename(from, to); err != nil {
			return fail(err)
		}
		current.installed = true
	}
	keepStage = true
	return filepath.Join(config.ConfigDir, filepath.FromSlash(relTarget)), install, nil
}

func isSystemExportCompanion(name, slug string) bool {
	switch name {
	case ".modernpath/AGENTS.md", ".modernpath/CLAUDE.md", ".modernpath/workspace.json", ".modernpath/" + slug + ".sqlite":
		return true
	}
	for _, dir := range []string{"memories", "viewer", "patterns", "workflows", "skills"} {
		if strings.HasPrefix(name, ".modernpath/"+dir+"/") {
			return true
		}
	}
	return false
}
