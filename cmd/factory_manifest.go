package cmd

// factory manifest — the onboarding mapper (REQ-CROSS-014, EPIC-SYNC-004
// TASK-SY-406). `init` detects the known formats deterministically and writes
// .modernpath/manifest.json; anything it cannot map becomes a REPORTED
// mapping-proposal artifact for LLM-assisted review — never a silent guess,
// and the LLM edge never enters the sync loop (D3).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modernpath/cli/internal/manifest"
	"github.com/modernpath/cli/internal/rdd"
	"github.com/spf13/cobra"
)

var factoryManifestCmd = &cobra.Command{
	Use:   "manifest",
	Short: "The document manifest: which files feed sync, and how they parse",
}

var factoryManifestShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the active manifest (file or defaults) and what it resolves to",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workspaceRootForManifest()
		if err != nil {
			return err
		}
		m, fromFile, err := manifest.Load(root)
		if err != nil {
			return err
		}
		if fromFile {
			printInfo("manifest: %s", manifest.Path(root))
		} else {
			printInfo("manifest: defaults (no %s)", manifest.Path(root))
		}
		for docType, spec := range m.Documents {
			files := m.Resolve(root, docType)
			fmt.Printf("  %-15s %-22s %v -> %d file(s)\n", docType, spec.Format, spec.Globs(), len(files))
		}
		for _, missing := range m.MissingMandated(root) {
			printWarning("MANDATED document type %q matches no files", missing)
		}
		return nil
	},
}

var manifestInitForce bool

// Candidate globs the deterministic detector probes, most-conventional first.
var detectorCandidates = map[string][]string{
	manifest.DocRequirements:  {"tasks/*-REQUIREMENTS.md", "*-REQUIREMENTS.md", "requirements/*-REQUIREMENTS.md", "docs/*-REQUIREMENTS.md", "specs/*-REQUIREMENTS.md"},
	manifest.DocWorklist:      {"WORKLIST.md", "docs/WORKLIST.md", "process/WORKLIST.md"},
	manifest.DocEpics:         {"epics/*", "docs/epics/*", "work/epics/*"},
	manifest.DocReviewQueue:   {"docs/85-loop-review-queue.md"},
	manifest.DocOpenQuestions: {"process/08-open-questions.md", "docs/08-open-questions.md"},
}

var detectorFormats = map[string]string{
	manifest.DocRequirements:  "rdd-ledger-v1",
	manifest.DocWorklist:      "rdd-worklist-v1",
	manifest.DocEpics:         "rdd-epic-v1",
	manifest.DocReviewQueue:   "rdd-review-queue-v1",
	manifest.DocOpenQuestions: "rdd-open-questions-v1",
}

var factoryManifestInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Detect this repo's process documents and write .modernpath/manifest.json",
	RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workspaceRootForManifest()
		if err != nil {
			return err
		}
		if _, statErr := os.Stat(manifest.Path(root)); statErr == nil && !manifestInitForce {
			return fmt.Errorf("%s already exists — review it, or re-run with --force to overwrite", manifest.Path(root))
		}

		m := &manifest.Manifest{SchemaVersion: 1, Documents: map[string]manifest.DocSpec{}}
		var unmapped []string
		for docType, candidates := range detectorCandidates {
			glob, files := firstMatchingGlob(root, candidates)
			if glob == "" {
				if isMandated(docType) {
					unmapped = append(unmapped, docType)
				}
				continue
			}
			if docType == manifest.DocRequirements && !sniffLedger(files) {
				unmapped = append(unmapped, docType)
				printWarning("%s: %q matched files but none parse as rdd-ledger-v1 — needs mapping", docType, glob)
				continue
			}
			spec := manifest.DocSpec{Format: detectorFormats[docType]}
			if len(candidates) == 1 || !strings.Contains(glob, "*") {
				spec.Path = glob
			} else {
				spec.Paths = []string{glob}
			}
			m.Documents[docType] = spec
			printInfo("detected %-15s %s (%d file(s))", docType, glob, len(files))
		}

		// the proposal is written FIRST — an undetectable layout must still
		// leave the reviewable artifact, never error out silently (SCN-SY-036)
		if len(unmapped) > 0 {
			proposal := writeMappingProposal(root, unmapped)
			printWarning("%d mandated document type(s) could not be detected: %s", len(unmapped), strings.Join(unmapped, ", "))
			printWarning("  a mapping-proposal worksheet was written to %s — fill it in (LLM-assisted is fine), then edit the manifest; sync will report these as gaps until mapped", proposal)
		}
		if len(m.Documents) == 0 {
			printWarning("nothing detected — no manifest written; start from the proposal worksheet")
			return nil
		}
		if err := m.Write(root); err != nil {
			return err
		}
		printSuccess("wrote %s — review and commit it (the manifest is a reviewable artifact)", manifest.Path(root))
		return nil
	},
}

func workspaceRootForManifest() (string, error) {
	// same root resolution the factory env uses, minus the server binding —
	// manifest init must work before `factory connect`
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for probe := dir; ; probe = filepath.Dir(probe) {
		if _, err := os.Stat(filepath.Join(probe, ".modernpath")); err == nil {
			return probe, nil
		}
		if _, err := os.Stat(filepath.Join(probe, ".git")); err == nil {
			return probe, nil
		}
		if filepath.Dir(probe) == probe {
			return dir, nil // fall back to cwd
		}
	}
}

func firstMatchingGlob(root string, candidates []string) (string, []string) {
	for _, g := range candidates {
		matches, err := filepath.Glob(filepath.Join(root, g))
		if err == nil && len(matches) > 0 {
			return g, matches
		}
	}
	return "", nil
}

// sniffLedger: at least one candidate file actually parses as a ledger
// (naming convention + ≥1 REQ row) — detection is by content, not just path.
func sniffLedger(files []string) bool {
	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if reqs := rdd.ParseLedger(f, string(content)); len(reqs) > 0 {
			return true
		}
	}
	return false
}

func isMandated(docType string) bool {
	for _, m := range manifest.MandatedTypes {
		if m == docType {
			return true
		}
	}
	return false
}

func writeMappingProposal(root string, unmapped []string) string {
	path := filepath.Join(root, ".modernpath", "manifest-proposal.md")
	var b strings.Builder
	b.WriteString("# Manifest mapping proposal — needs a human (or LLM-assisted) decision\n\n")
	b.WriteString("`factory manifest init` could not detect these MANDATED document types.\n")
	b.WriteString("For each: find where this repo keeps that information, then either\n")
	b.WriteString("(a) add a `documents` entry to `.modernpath/manifest.json` if the files\n")
	b.WriteString("match a supported format, or (b) normalize the docs to a supported format\n")
	b.WriteString("once (preferred over a bespoke parser). This file is the reviewable\n")
	b.WriteString("artifact for that mapping — an LLM may draft it, a human commits it.\n\n")
	for _, docType := range unmapped {
		fmt.Fprintf(&b, "## %s (format: %s)\n- Candidate paths probed: %v\n- Where does this repo keep it? _fill in_\n- Mapping or normalization plan: _fill in_\n\n",
			docType, detectorFormats[docType], detectorCandidates[docType])
	}
	b.WriteString("Supported formats: rdd-ledger-v1, rdd-worklist-v1, rdd-epic-v1, rdd-review-queue-v1, rdd-open-questions-v1.\n")
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	_ = os.WriteFile(path, []byte(b.String()), 0o644)
	return path
}

func init() {
	factoryManifestInitCmd.Flags().BoolVar(&manifestInitForce, "force", false, "overwrite an existing manifest")
	factoryManifestCmd.AddCommand(factoryManifestShowCmd, factoryManifestInitCmd)
}
