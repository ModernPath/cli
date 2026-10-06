package cmd

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type selectedReviewSnapshot struct {
	Directory string
	Manifest  reviewSnapshotManifest
}

func revalidateReviewSnapshot(env *factoryEnv, selected *selectedReviewSnapshot) error {
	manifest := selected.Manifest
	current, err := resolveReviewSnapshot(env, manifest.ContextID,
		[]string{manifest.ScopeKind + ":" + manifest.ScopeExternalID}, manifest.AggregateFingerprint)
	if err != nil {
		return err
	}
	if current.Manifest.SnapshotDigest != manifest.SnapshotDigest {
		return fmt.Errorf("review snapshot changed since it was selected — refusing further writes")
	}
	return nil
}

func resolveReviewSnapshot(env *factoryEnv, contextID string, scopes []string, pin string) (*selectedReviewSnapshot, error) {
	if contextID == "" || unsafeSnapshotName(contextID) {
		return nil, fmt.Errorf("a safe explicit review context selector is required")
	}
	ids := make([]string, 0, len(scopes))
	allowedKinds := map[string]string{}
	for _, token := range scopes {
		kind, id := splitScope(token)
		if id == "" {
			id = normalizeScopeToken(token)
		}
		if id == "" || unsafeSnapshotName(id) {
			return nil, fmt.Errorf("review snapshot scope %q is unsafe", token)
		}
		if !contains(ids, id) {
			ids = append(ids, id)
		}
		if kind != "" {
			allowedKinds[id] = kind
		}
	}
	var found string
	for _, id := range ids {
		candidate := filepath.Join(env.Root, reviewSnapshotDir, id, contextID)
		if _, err := os.Lstat(candidate); err == nil {
			if found != "" {
				return nil, fmt.Errorf("review context %s is ambiguous across the selected scope", contextID)
			}
			found = candidate
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	if found == "" {
		return nil, fmt.Errorf("review context %s was not found for the selected scope — pull with `working-set pull --scope --for-review` and use its printed context ID", contextID)
	}
	if err := validateReviewSnapshotDirectories(env.Root, filepath.Base(filepath.Dir(found)), contextID); err != nil {
		return nil, err
	}
	return readAndValidateReviewSnapshot(env, found, contextID, ids, allowedKinds, pin)
}

func validateReviewSnapshotDirectories(root, scopeID, contextID string) error {
	current := root
	parts := strings.Split(filepath.ToSlash(reviewSnapshotDir), "/")
	for _, part := range append(parts, scopeID, contextID) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("review snapshot path is unavailable: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("review snapshot path contains an unsafe directory component: %s", current)
		}
	}
	return nil
}

func readAndValidateReviewSnapshot(env *factoryEnv, dir, contextID string, scopeIDs []string, scopeKinds map[string]string, pin string) (*selectedReviewSnapshot, error) {
	manifestPath := filepath.Join(dir, "MANIFEST.json")
	manifestInfo, err := os.Lstat(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("review snapshot manifest is unavailable: %w", err)
	}
	if !manifestInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("review snapshot manifest is not a regular file")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest reviewSnapshotManifest
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("review snapshot manifest is malformed: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("review snapshot manifest has trailing data")
	}
	if manifest.Version != 1 {
		return nil, fmt.Errorf("review snapshot manifest version %d is unsupported", manifest.Version)
	}
	if manifest.StoreURL != env.APIURL {
		return nil, fmt.Errorf("review snapshot store identity mismatch")
	}
	if manifest.SystemID != env.SystemID {
		return nil, fmt.Errorf("review snapshot system identity mismatch")
	}
	if manifest.ContextID != contextID || filepath.Base(dir) != contextID {
		return nil, fmt.Errorf("review snapshot context mismatch")
	}
	if !contains(scopeIDs, manifest.ScopeExternalID) {
		return nil, fmt.Errorf("review snapshot scope mismatch")
	}
	if kind := scopeKinds[manifest.ScopeExternalID]; kind != "" && kind != manifest.ScopeKind {
		return nil, fmt.Errorf("review snapshot scope kind mismatch")
	}
	if manifest.ScopeKind != "epic" && manifest.ScopeKind != "single_sr" {
		return nil, fmt.Errorf("review snapshot scope kind %q is unsupported", manifest.ScopeKind)
	}
	if filepath.Base(filepath.Dir(dir)) != manifest.ScopeExternalID {
		return nil, fmt.Errorf("review snapshot scope path mismatch")
	}
	if len(manifest.AggregateFingerprint) != 64 || manifest.AggregateFingerprint != strings.ToLower(manifest.AggregateFingerprint) {
		return nil, fmt.Errorf("review snapshot aggregate fingerprint is malformed")
	}
	if decoded, err := hex.DecodeString(manifest.AggregateFingerprint); err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("review snapshot aggregate fingerprint is malformed")
	}
	if pin != "" && pin != manifest.AggregateFingerprint {
		return nil, fmt.Errorf("explicit aggregate pin does not match the selected review snapshot")
	}
	if len(manifest.Files) == 0 {
		return nil, fmt.Errorf("review snapshot manifest has no rendered file inventory")
	}
	digest, err := reviewSnapshotDigest(manifest)
	if err != nil || digest != manifest.SnapshotDigest {
		return nil, fmt.Errorf("review snapshot manifest digest mismatch")
	}
	for name := range manifest.Files {
		if !safeReviewSnapshotPath(name) {
			return nil, fmt.Errorf("review snapshot manifest path %q is unsafe", name)
		}
	}
	seen := map[string]bool{"MANIFEST.json": true}
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("review snapshot contains a symlink at %s", rel)
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("review snapshot contains a non-regular file at %s", rel)
		}
		if _, ok := manifest.Files[rel]; !ok && rel != "MANIFEST.json" {
			return fmt.Errorf("review snapshot file inventory has an unlisted file %s", rel)
		}
		seen[rel] = true
		if rel == "MANIFEST.json" {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if sha256Hex(content) != manifest.Files[rel] {
			return fmt.Errorf("review snapshot file integrity mismatch at %s", rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(seen) != len(manifest.Files)+1 {
		return nil, fmt.Errorf("review snapshot file inventory is incomplete")
	}
	return &selectedReviewSnapshot{Directory: dir, Manifest: manifest}, nil
}

func safeReviewSnapshotPath(name string) bool {
	if name == "" || name == "MANIFEST.json" || filepath.IsAbs(name) || strings.Contains(name, "\\") {
		return false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if clean != name {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func appendReviewSnapshotProvenance(body string, selected *selectedReviewSnapshot) string {
	manifest := selected.Manifest
	marker := fmt.Sprintf("Review snapshot context: %s\nAggregate fingerprint: %s\nSnapshot digest: %s",
		manifest.ContextID, manifest.AggregateFingerprint, manifest.SnapshotDigest)
	if strings.Contains(body, marker) {
		return body
	}
	if strings.TrimSpace(body) == "" {
		return marker
	}
	return strings.TrimRight(body, "\n") + "\n\n" + marker
}
