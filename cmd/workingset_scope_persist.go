package cmd

// Local scope persistence owns draft protection and the ordering of content,
// baseline and packet CAS checkpoints. HTTP reads and rendering happen earlier.

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// atomicWrite writes via a same-directory temp file and rename, so a failure
// at any point leaves the previous content untouched.
func atomicWrite(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func newContextID(mode string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return mode + "-" + hex.EncodeToString(b)
}

const scopedDraftBaselineFile = ".local-draft-baseline.json"

type scopedDraftBaselineEntry struct {
	SHA256 string `json:"sha256"`
	Origin string `json:"origin"`
}

type scopedDraftBaseline struct {
	Version int                                 `json:"version"`
	Files   map[string]scopedDraftBaselineEntry `json:"files"`
}

func manualScopedRecovery(rel, reason string) error {
	return fmt.Errorf("%s %s; rename the whole scope directory under .modernpath/working-set/ to preserve every local byte, pull a fresh scope, then compare and reapply the draft manually", rel, reason)
}

func deletedScopedFileRecovery(rel string) error {
	return manualScopedRecovery(rel, "was deleted locally; nothing was written. Restore this file exactly as last pulled and retry. If you have no saved copy, use scope recovery")
}

func readScopedDraftBaselineEntry(dir, name string) (scopedDraftBaselineEntry, bool, error) {
	raw, err := os.ReadFile(filepath.Join(dir, scopedDraftBaselineFile))
	if err != nil {
		return scopedDraftBaselineEntry{}, false, err
	}
	var baseline scopedDraftBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil || baseline.Version != 1 || baseline.Files == nil {
		return scopedDraftBaselineEntry{}, false, fmt.Errorf("local draft baseline is unreadable")
	}
	entry, ok := baseline.Files[filepath.ToSlash(name)]
	return entry, ok, nil
}

func validateScopedDraftBaselinePath(scopeExt, name, origin string) error {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(name)))
	if name == "" || filepath.IsAbs(name) || strings.Contains(name, "\\") || clean != name {
		return fmt.Errorf("local draft baseline path %q is not canonical — refusing scoped refresh", name)
	}
	parts := strings.Split(name, "/")
	validFile := func(filename string) bool {
		return filename != "" && filename != "." && filename != ".." && !unsafeSnapshotName(filename) && strings.HasSuffix(filename, ".md")
	}
	valid := false
	switch origin {
	case "item":
		valid = (len(parts) == 1 && name == scopeExt+".md") ||
			(len(parts) == 2 && parts[0] == "members" && validFile(parts[1]))
	case "served-packet", "stub":
		valid = len(parts) == 2 && parts[0] == "packet" && validFile(parts[1])
	default:
		return fmt.Errorf("local draft baseline for %q has unknown origin %q — refusing scoped refresh", name, origin)
	}
	if !valid {
		return fmt.Errorf("local draft baseline path %q does not match origin %q — refusing scoped refresh", name, origin)
	}
	return nil
}

func validateScopedDraftBaseline(scopeExt, name string, entry scopedDraftBaselineEntry) error {
	if err := validateScopedDraftBaselinePath(scopeExt, name, entry.Origin); err != nil {
		return err
	}
	digest, err := hex.DecodeString(entry.SHA256)
	if err != nil || len(digest) != sha256.Size || entry.SHA256 != strings.ToLower(entry.SHA256) {
		return fmt.Errorf("local draft baseline for %q has an invalid SHA-256 — refusing scoped refresh", name)
	}
	return nil
}

// applyScopedPullPlan checks every destructive target against the last content
// written by this CLI before creating directories or replacing any file. This
// is a per-path draft guard; packet CAS fingerprints remain a separate server
// concurrency mechanism.
func applyScopedPullPlan(dir string, plan scopedPullPlan) error {
	return applyScopedPullPlanWithWriter(dir, plan, atomicWrite)
}

func applyScopedPullPlanWithWriter(dir string, plan scopedPullPlan, write func(string, []byte) error) error {
	baselinePath := filepath.Join(dir, scopedDraftBaselineFile)
	baseline := scopedDraftBaseline{Version: 1, Files: map[string]scopedDraftBaselineEntry{}}
	if raw, err := os.ReadFile(baselinePath); err == nil {
		if err := json.Unmarshal(raw, &baseline); err != nil || baseline.Version != 1 || baseline.Files == nil {
			return fmt.Errorf("local draft baseline is unreadable — refusing scoped refresh")
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	scopeExt := filepath.Base(dir)
	for name, entry := range baseline.Files {
		if err := validateScopedDraftBaseline(scopeExt, name, entry); err != nil {
			return err
		}
	}

	names := make([]string, 0, len(plan.files))
	managedNames := make([]string, 0, len(plan.files))
	for name, file := range plan.files {
		if file.origin == "server-cas" {
			return fmt.Errorf("packet CAS must be checkpointed with its section content — refusing scoped refresh")
		}
		name = filepath.ToSlash(name)
		names = append(names, name)
		if plan.files[name].managed {
			managedNames = append(managedNames, name)
		}
	}
	sort.Strings(names)
	sort.Strings(managedNames)
	for _, name := range managedNames {
		path := filepath.Join(dir, filepath.FromSlash(name))
		current, err := os.ReadFile(path)
		if err == nil {
			entry, known := baseline.Files[name]
			if !known {
				return manualScopedRecovery(name, "has no local draft baseline")
			}
			if sha256Hex(current) != entry.SHA256 {
				return fmt.Errorf("%s has local edits — refusing to replace it", name)
			}
		} else if os.IsNotExist(err) {
			if _, known := baseline.Files[name]; known {
				return deletedScopedFileRecovery(name)
			}
		} else {
			return err
		}
	}

	var deletePaths []string
	if plan.packetAbsent {
		for name, entry := range baseline.Files {
			name = filepath.ToSlash(name)
			if !strings.HasPrefix(name, "packet/") || !strings.HasSuffix(name, ".md") ||
				(entry.Origin != "served-packet" && entry.Origin != "stub") {
				continue
			}
			path := filepath.Join(dir, filepath.FromSlash(name))
			current, err := os.ReadFile(path)
			if os.IsNotExist(err) {
				return deletedScopedFileRecovery(name)
			}
			if err != nil {
				return err
			}
			if sha256Hex(current) != entry.SHA256 {
				if entry.Origin == "stub" {
					continue // an edited, never-served scaffold is a local draft
				}
				return fmt.Errorf("%s has local edits — refusing packet cleanup", name)
			}
			deletePaths = append(deletePaths, name)
		}
	}
	saveBaseline := func() error {
		raw, err := json.MarshalIndent(baseline, "", "  ")
		if err != nil {
			return err
		}
		return write(baselinePath, append(raw, '\n'))
	}
	packetPins := readPacketFingerprints(dir)
	savePacketPins := func(pins map[string]string) error {
		raw, err := json.Marshal(pins)
		if err != nil {
			return err
		}
		return write(packetFingerprintManifest(dir), raw)
	}
	// Persist each completed managed change before attempting the next path.
	// A later write failure must not make our own changes look like local edits.
	sort.Strings(deletePaths)
	for _, name := range deletePaths {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		if _, known := baseline.Files[name]; known {
			delete(baseline.Files, name)
			if err := saveBaseline(); err != nil {
				return fmt.Errorf("%s: %w", manualScopedRecovery(name, "was removed, but its local draft baseline could not be saved"), err)
			}
		}
	}
	if plan.packetAbsent {
		// Keep the original pins until every deletion and baseline checkpoint
		// succeeds. Surviving packet files still need their pull-time CAS.
		if err := os.Remove(packetFingerprintManifest(dir)); err != nil && !os.IsNotExist(err) {
			return err
		}
		packetDir := filepath.Join(dir, "packet")
		entries, err := os.ReadDir(packetDir)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil && len(entries) == 0 {
			if err := os.Remove(packetDir); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	for _, name := range names {
		file := plan.files[name]
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := write(path, file.content); err != nil {
			return err
		}
		if file.managed {
			baseline.Files[name] = scopedDraftBaselineEntry{SHA256: sha256Hex(file.content), Origin: file.origin}
			if err := saveBaseline(); err != nil {
				return fmt.Errorf("%s: %w", manualScopedRecovery(name, "was refreshed, but its local draft baseline could not be saved"), err)
			}
		}
		if file.packetKey != "" {
			// A failed body or baseline write above leaves its original pin intact.
			// Other sections retain their pins until their own refresh completes.
			packetPins[file.packetKey] = plan.packetFingerprints[file.packetKey]
			if err := savePacketPins(packetPins); err != nil {
				return fmt.Errorf("%s was refreshed, but its packet CAS could not be saved; retry the pull before editing or pushing: %w", name, err)
			}
		}
	}
	if plan.packetFingerprints != nil {
		// Retire pins for sections no longer served only after every write succeeds.
		if err := savePacketPins(plan.packetFingerprints); err != nil {
			return err
		}
	}
	return saveBaseline()
}

// refreshScopedDraftBaseline advances an existing managed path. A newly
// created packet path is adopted only after its accepted create is confirmed
// by a canonical read and the file still matches the staged bytes.
func refreshScopedDraftBaseline(dir, name string, content []byte, allowCreatedPacket bool) error {
	name = filepath.ToSlash(name)
	path := filepath.Join(dir, scopedDraftBaselineFile)
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var baseline scopedDraftBaseline
	if err := json.Unmarshal(raw, &baseline); err != nil || baseline.Version != 1 || baseline.Files == nil {
		return fmt.Errorf("local draft baseline is unreadable — refusing to refresh its entry")
	}
	entry, known := baseline.Files[name]
	if !known {
		if !allowCreatedPacket || !strings.HasPrefix(name, "packet/") {
			return nil
		}
		entry = scopedDraftBaselineEntry{Origin: "served-packet"}
		if err := validateScopedDraftBaselinePath(filepath.Base(dir), name, entry.Origin); err != nil {
			return err
		}
	}
	current, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	if !bytes.Equal(current, content) {
		return fmt.Errorf("%s changed while updating its local draft baseline", name)
	}
	if strings.HasPrefix(name, "packet/") && (entry.Origin == "stub" || entry.Origin == "served-packet") {
		entry.Origin = "served-packet"
	}
	entry.SHA256 = sha256Hex(content)
	baseline.Files[name] = entry
	raw, err = json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(raw, '\n'))
}

func packetFingerprintManifest(dir string) string {
	return filepath.Join(dir, "packet", ".served-fingerprints.json")
}

func readPacketFingerprints(dir string) map[string]string {
	out := map[string]string{}
	if raw, err := os.ReadFile(packetFingerprintManifest(dir)); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

// readContextAggregate reads the packet aggregate a review pull stamped in the
// scope directory's `.context` (REQ-CROSS-449); empty when there is none.
func readContextAggregate(dir string) string {
	for _, line := range strings.Split(readContextFile(dir), "\n") {
		if strings.HasPrefix(line, "aggregate: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "aggregate: "))
		}
	}
	return ""
}

// readContextFile is the scope directory's `.context` stamp; empty when absent.
func readContextFile(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		return ""
	}
	return string(raw)
}

func readContextStamp(dir string) (mode, ctxID string) {
	raw, err := os.ReadFile(filepath.Join(dir, contextFile))
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "mode: ") {
			mode = strings.TrimSpace(strings.TrimPrefix(line, "mode: "))
		}
		if strings.HasPrefix(line, "context_id: ") {
			ctxID = strings.TrimSpace(strings.TrimPrefix(line, "context_id: "))
		}
	}
	return mode, ctxID
}

func writePacketFingerprints(dir string, m map[string]string) error {
	blob, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return atomicWrite(packetFingerprintManifest(dir), blob)
}
