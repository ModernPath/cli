package rdd

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// REQ-CROSS-084 (EPIC-ARCH-001 Part C, `USER:2026-08-13`): the documents that
// explain a codebase are first-class on the platform — "main documents that
// explain the codebase for the audience, not our low-level docs we currently
// expose".
//
// Two families, both SYSTEM-scoped, which is why they are neither specs
// (Epic-scoped) nor ledger rows:
//
//	derived  — the phase-D output: 03, 20, 21, 22, 23 and the ADRs
//	guide    — docs/guides/*.md, the human-written lenses a pass reads as INPUT
//
// The distinction matters in the UI: a reader must be able to tell what the
// system IS from what someone TOLD the pass to look for.
type Document struct {
	Path       string // workspace-relative — this is the identity
	Name       string
	Type       string // maps onto system_documents.document_type
	Provenance string // derived | guide
	Content    string
}

// The explanatory documents, by exact basename. A prefix match would sweep in
// the per-context corpus (`10-analytics.md`, `20-surfaces-*.md`), which is
// deliberately NOT part of phase D's system-wide set.
//
// `03` is resolved separately — see architectureCandidates.
var explanatoryDocs = map[string]string{
	"20-deployment-topology.md": "architecture",
	"21-integrations.md":        "architecture",
	"22-cross-cutting.md":       "architecture",
	"23-data-flow.md":           "architecture",
}

// REQ-CROSS-088: phase D is told to ADOPT a maintained architecture document
// rather than write a competing one — "never write a second document answering a
// question the repository already answers". So the architecture document is
// often NOT `docs/03-architecture.md`; in the first repository this phase ran
// against for real it was a root `ARCHITECTURE.md` (`RUN:2026-08-13`), and the
// collector silently dropped it.
//
// Ordered, and the FIRST match wins: exactly one architecture document reaches
// the reader. Carrying both would reproduce on the platform the very thing the
// adopt rule exists to prevent — two answers to one question, with an agent
// picking between them arbitrarily.
var architectureCandidates = []string{
	filepath.Join("docs", "03-architecture.md"),
	filepath.Join("docs", "ARCHITECTURE.md"),
	filepath.Join("docs", "architecture.md"),
	"ARCHITECTURE.md",
}

// documentContentCap bounds one document. Larger than a spec's 64 KiB because
// an architecture document for a real estate is legitimately long; small enough
// that a batch stays sendable.
const documentContentCap = 262144

// CollectDocuments finds the explanatory documents and the discovery guides.
// A workspace with none yields none, and the sync treats that as a no-op rather
// than as "delete everything".
func CollectDocuments(root string) []Document {
	var out []Document

	add := func(rel, name, dtype, provenance string) {
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			return
		}
		out = append(out, Document{
			Path:       rel,
			Name:       name,
			Type:       dtype,
			Provenance: provenance,
			Content:    capRunes(string(raw), documentContentCap),
		})
	}

	for _, rel := range architectureCandidates {
		if existsExact(root, rel) {
			add(rel, titleOf(filepath.Join(root, rel), filepath.Base(rel)), "architecture", "derived")
			break
		}
	}

	for base, dtype := range explanatoryDocs {
		rel := filepath.Join("docs", base)
		// existsExact, not os.Stat: the same case-folding reasoning as
		// architectureCandidates below. Path is the sync identity, so a
		// candidate spelling that only resolves on a case-insensitive
		// filesystem makes one repository two documents.
		if existsExact(root, rel) {
			add(rel, titleOf(filepath.Join(root, rel), base), dtype, "derived")
		}
	}

	// Decision records — every markdown file under docs/adr/ except its README.
	for _, rel := range markdownIn(root, filepath.Join("docs", "adr")) {
		if strings.EqualFold(filepath.Base(rel), "README.md") {
			continue
		}
		add(rel, titleOf(filepath.Join(root, rel), filepath.Base(rel)), "design", "derived")
	}

	// Discovery guides — the input lens, marked so a reader can tell it apart.
	for _, rel := range markdownIn(root, filepath.Join("docs", "guides")) {
		add(rel, titleOf(filepath.Join(root, rel), filepath.Base(rel)), "process", "guide")
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func markdownIn(root, dir string) []string {
	entries, err := os.ReadDir(filepath.Join(root, dir))
	if err != nil {
		return nil
	}
	var rels []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		rels = append(rels, filepath.Join(dir, e.Name()))
	}
	sort.Strings(rels)
	return rels
}

// titleOf reads the document's own H1, falling back to the filename. A document
// named by its path alone reads as a filename in every list it appears in.
func titleOf(path, fallback string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return fallback
}

// existsExact reports whether rel names a file whose on-disk basename matches
// byte for byte.
//
// os.Stat is not enough: macOS and Windows are case-insensitive, so a candidate
// `docs/ARCHITECTURE.md` "finds" a file actually called `docs/architecture.md`
// and the collector records the candidate's spelling. That spelling is the sync
// identity, so the same repository would produce a different document id on a
// Mac than in Linux CI — a silent duplicate rather than an update.
func existsExact(root, rel string) bool {
	entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(rel)))
	if err != nil {
		return false
	}
	want := filepath.Base(rel)
	for _, e := range entries {
		if !e.IsDir() && e.Name() == want {
			return true
		}
	}
	return false
}
