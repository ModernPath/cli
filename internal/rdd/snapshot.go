// Snapshot — manifest-driven workspace extraction (REQ-CROSS-013,
// TASK-SY-405): resolve each declared doc type, run its bundled parser, and
// report mandated gaps loudly (D2: honest gap, never silent skip).
package rdd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/modernpath/cli/internal/manifest"
)

// Snapshot parses every document the manifest declares under root.
// warnings carries the loud-report lines: missing mandated types, files a
// parser rejected. Optional types with no files are silent (enrichment).
func Snapshot(root string, m *manifest.Manifest) (Data, []string) {
	var data Data
	var warnings []string

	for _, docType := range m.MissingMandated(root) {
		spec := m.Documents[docType]
		warnings = append(warnings, fmt.Sprintf(
			"MANDATED document type %q matches no files (globs: %v) — the factory needs it; sync continues with what exists",
			docType, spec.Globs()))
	}

	for _, file := range m.Resolve(root, manifest.DocRequirements) {
		content, err := os.ReadFile(file)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("requirements: cannot read %s: %v", rel(root, file), err))
			continue
		}
		reqs := ParseLedger(file, string(content))
		if reqs == nil {
			warnings = append(warnings, fmt.Sprintf(
				"requirements: %s does not match the rdd-ledger-v1 naming convention (<CTX>-REQUIREMENTS.md) — skipped", rel(root, file)))
			continue
		}
		data.Reqs = append(data.Reqs, reqs...)
	}

	if files := m.Resolve(root, manifest.DocWorklist); len(files) > 0 {
		content, err := os.ReadFile(files[0])
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("worklist: cannot read %s: %v", rel(root, files[0]), err))
		} else {
			names, isDir := epicDirListing(root, m)
			data.Epics = ParseWorklist(string(content), names, isDir)
			// Worklist record links ("[record](epics/…)") are relative to the
			// worklist file, not the sync root. When the manifest points one
			// level up (../WORKLIST.md), give each epic a root-resolvable
			// RecordFS for reading — but leave Record untouched: it is payload
			// identity (origin_ref), and rebasing it would change gate hashes
			// between roots (SCN-SY-046).
			if base := filepath.Dir(rel(root, files[0])); base != "." {
				for i := range data.Epics {
					if data.Epics[i].Record != "" {
						data.Epics[i].RecordFS = filepath.Join(base, data.Epics[i].Record)
					}
				}
			}
			warnings = append(warnings, collectEpicSpecs(root, data.Epics)...)
		}
	}

	if files := m.Resolve(root, manifest.DocReviewQueue); len(files) > 0 {
		if content, err := os.ReadFile(files[0]); err == nil {
			data.RQs = ParseReviewQueue(string(content))
		}
	}
	if files := m.Resolve(root, manifest.DocOpenQuestions); len(files) > 0 {
		if content, err := os.ReadFile(files[0]); err == nil {
			data.OQs = ParseOpenQuestions(string(content))
		}
	}

	data.Commits = ParseGitLog(root)
	return data, warnings
}

// collectEpicSpecs loads each folder epic's specs/*.md (REQ-CROSS-023,
// EPIC-SYNC-006), sorted by name. Rel (payload identity) derives from the
// canonical Record path; bytes resolve via RecordFS when set (SCN-SY-046).
// Warnings: content over the wire cap (truncated at op build) and a
// SPEC-READY marker over an empty specs/ (no gate will be emitted — honest
// gap, never a silent skip).
func collectEpicSpecs(root string, epics []Epic) []string {
	var warnings []string
	for i := range epics {
		e := &epics[i]
		if !strings.HasSuffix(e.Record, "/EPIC.md") {
			continue
		}
		readRecord := e.Record
		if e.RecordFS != "" {
			readRecord = e.RecordFS
		}
		relDir := strings.TrimSuffix(e.Record, "/EPIC.md")
		fsSpecsDir := filepath.Join(root, filepath.Dir(readRecord), "specs")

		entries, err := os.ReadDir(fsSpecsDir)
		if err == nil {
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
					names = append(names, entry.Name())
				}
			}
			sort.Strings(names)
			for _, name := range names {
				raw, err := os.ReadFile(filepath.Join(fsSpecsDir, name))
				if err != nil {
					warnings = append(warnings, fmt.Sprintf("epic %s: cannot read spec %s: %v", e.ID, name, err))
					continue
				}
				content := string(raw)
				if capRunes(content, specContentCap) != content {
					warnings = append(warnings, fmt.Sprintf(
						"epic %s: spec %s exceeds the %d-char wire cap and will be TRUNCATED in the sync payload", e.ID, name, specContentCap))
				}
				e.Specs = append(e.Specs, EpicSpec{Rel: relDir + "/specs/" + name, Name: name, Content: content})
			}
		}

		marker := ""
		if raw, err := os.ReadFile(filepath.Join(root, readRecord)); err == nil {
			marker, _ = SpecApprovalLineOf(string(raw))
		}

		if len(e.Specs) == 0 && (marker == "ready" || marker == "approved") {
			warnings = append(warnings, fmt.Sprintf(
				"epic %s is %s but specs/ is empty or missing — no spec-approval gate will be emitted (an approval over nothing is dishonest)", e.ID, strings.ToUpper("spec-"+marker)))
		}

		// D5 enforcement signal (REQ-CROSS-024): implementation before the
		// spec approval is a process violation — warn loudly, never silently.
		if e.State == "in-progress" && marker != "approved" && (len(e.Specs) > 0 || marker != "") {
			warnings = append(warnings, fmt.Sprintf(
				"epic %s is IN_PROGRESS without an approved specification — the spec-approval gate (SPEC-APPROVE-%s) must pass before implementation (CLAUDE.md §6B)", e.ID, e.ID))
		}
	}
	return warnings
}

// epicDirListing reproduces the extractor's epics-directory view (name list +
// which entries are directories) from the manifest's epics globs.
func epicDirListing(root string, m *manifest.Manifest) ([]string, map[string]bool) {
	seen := map[string]bool{}
	isDir := map[string]bool{}
	var names []string
	for _, f := range m.Resolve(root, manifest.DocEpics) {
		name := filepath.Base(f)
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
		if info, err := os.Stat(f); err == nil && info.IsDir() {
			isDir[name] = true
		}
	}
	sort.Strings(names) // readdir order
	return names, isDir
}

func rel(root, file string) string {
	if r, err := filepath.Rel(root, file); err == nil {
		return r
	}
	return file
}
