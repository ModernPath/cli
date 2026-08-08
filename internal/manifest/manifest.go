// Package manifest — the workspace document manifest (REQ-CROSS-013,
// EPIC-SYNC-004 D1/D2). `.modernpath/manifest.json` maps process-document
// types to paths + a named, versioned parser format so any repo layout can
// feed the sync op-builder. No manifest -> Default(), which matches the
// modernpath-v1 workspace layout — existing workspaces work with zero config.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Doc types. Mandated types (D2) are what the factory needs to function;
// the rest are optional enrichment — absent files are skipped quietly,
// absent MANDATED files are reported loudly by sync (never a silent skip).
const (
	DocRequirements  = "requirements"
	DocWorklist      = "worklist"
	DocEpics         = "epics"
	DocReviewQueue   = "review_queue"
	DocOpenQuestions = "open_questions"
)

// MandatedTypes per decision D2 (USER:2026-07-28).
var MandatedTypes = []string{DocRequirements, DocWorklist, DocEpics}

// KnownFormats maps each named parser format to the doc type it parses.
var KnownFormats = map[string]string{
	"rdd-ledger-v1":         DocRequirements,
	"rdd-worklist-v1":       DocWorklist,
	"rdd-epic-v1":           DocEpics,
	"rdd-review-queue-v1":   DocReviewQueue,
	"rdd-open-questions-v1": DocOpenQuestions,
}

// DocSpec is one document-type entry: where the files live and which named
// parser reads them. Path and Paths are interchangeable (Path = one glob).
type DocSpec struct {
	Path   string   `json:"path,omitempty"`
	Paths  []string `json:"paths,omitempty"`
	Format string   `json:"format"`
}

// Globs returns the spec's path globs, whichever field carried them.
func (s DocSpec) Globs() []string {
	if len(s.Paths) > 0 {
		return s.Paths
	}
	if s.Path != "" {
		return []string{s.Path}
	}
	return nil
}

type Manifest struct {
	SchemaVersion int                `json:"schema_version"`
	Documents     map[string]DocSpec `json:"documents"`
	// IDPrefix optionally namespaces external ids for repos whose naming
	// conventions collide across systems (D4). Empty = ids used as-is.
	IDPrefix string `json:"id_prefix,omitempty"`
}

// Default is the modernpath-v1 layout — the zero-config behavior.
func Default() *Manifest {
	return &Manifest{
		SchemaVersion: 1,
		Documents: map[string]DocSpec{
			DocRequirements:  {Paths: []string{"tasks/*-REQUIREMENTS.md"}, Format: "rdd-ledger-v1"},
			DocWorklist:      {Path: "WORKLIST.md", Format: "rdd-worklist-v1"},
			DocEpics:         {Paths: []string{"epics/*"}, Format: "rdd-epic-v1"},
			DocReviewQueue:   {Path: "docs/85-loop-review-queue.md", Format: "rdd-review-queue-v1"},
			DocOpenQuestions: {Path: "process/08-open-questions.md", Format: "rdd-open-questions-v1"},
		},
	}
}

// Path of the manifest file under a workspace root.
func Path(root string) string {
	return filepath.Join(root, ".modernpath", "manifest.json")
}

// Load reads .modernpath/manifest.json if present, else returns Default().
// The second return says whether a file was read (for status/reporting).
func Load(root string) (*Manifest, bool, error) {
	raw, err := os.ReadFile(Path(root))
	if os.IsNotExist(err) {
		return Default(), false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, true, fmt.Errorf("manifest.json is not valid JSON: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, true, err
	}
	return &m, true, nil
}

// Validate enforces the manifest contract: version 1, known formats, formats
// matched to their doc type, every doc entry carrying at least one path.
func (m *Manifest) Validate() error {
	if m.SchemaVersion != 1 {
		return fmt.Errorf("unsupported manifest schema_version %d (supported: 1)", m.SchemaVersion)
	}
	if len(m.Documents) == 0 {
		return fmt.Errorf("manifest declares no documents")
	}
	for docType, spec := range m.Documents {
		wantType, known := KnownFormats[spec.Format]
		if !known {
			return fmt.Errorf("documents.%s: unknown format %q (known: %v)", docType, spec.Format, knownFormatNames())
		}
		if wantType != docType {
			return fmt.Errorf("documents.%s: format %q parses %q documents, not %q", docType, spec.Format, wantType, docType)
		}
		if len(spec.Globs()) == 0 {
			return fmt.Errorf("documents.%s: no path/paths given", docType)
		}
	}
	return nil
}

// MissingMandated resolves each mandated doc type's globs under root and
// returns the types that match no files at all — the loud-report input.
// A doc type absent from the manifest entirely is also missing.
func (m *Manifest) MissingMandated(root string) []string {
	var missing []string
	for _, docType := range MandatedTypes {
		spec, declared := m.Documents[docType]
		if !declared || len(resolve(root, spec.Globs())) == 0 {
			missing = append(missing, docType)
		}
	}
	return missing
}

// Resolve returns the files a doc type's globs match under root, sorted.
func (m *Manifest) Resolve(root, docType string) []string {
	spec, ok := m.Documents[docType]
	if !ok {
		return nil
	}
	return resolve(root, spec.Globs())
}

func resolve(root string, globs []string) []string {
	seen := map[string]bool{}
	var files []string
	for _, g := range globs {
		matches, err := filepath.Glob(filepath.Join(root, g))
		if err != nil {
			continue
		}
		for _, f := range matches {
			if !seen[f] {
				seen[f] = true
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)
	return files
}

// Write saves the manifest (manifest init's output — a reviewable artifact).
func (m *Manifest) Write(root string) error {
	if err := m.Validate(); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(Path(root)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(Path(root), append(raw, '\n'), 0o644)
}

func knownFormatNames() []string {
	names := make([]string, 0, len(KnownFormats))
	for name := range KnownFormats {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
