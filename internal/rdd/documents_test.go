package rdd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-084: the documents that explain a codebase are first-class. The
// collector had no test of its own, so every clause below is unpinned
// behaviour until proven otherwise.

func write(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func byPath(docs []Document) map[string]Document {
	m := map[string]Document{}
	for _, d := range docs {
		m[d.Path] = d
	}
	return m
}

// The criterion the UI rests on: a reader must be able to tell what the system
// IS from what someone TOLD the pass to look for. That is one field, and if it
// were constant in either direction the Documents facet would group everything
// into a single bucket while still rendering.
func TestGuidesAreInputsAndTheRestIsOutput(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/03-architecture.md", "# Arch\n")
	write(t, root, "docs/23-data-flow.md", "# Flow\n")
	write(t, root, "docs/adr/0001-pick-a-db.md", "# ADR 1\n")
	write(t, root, "docs/guides/domain-lens.md", "# Lens\n")

	got := byPath(CollectDocuments(root))
	if len(got) != 4 {
		t.Fatalf("collected %d documents, want 4: %v", len(got), got)
	}
	for path, want := range map[string]string{
		"docs/03-architecture.md":    "derived",
		"docs/23-data-flow.md":       "derived",
		"docs/adr/0001-pick-a-db.md": "derived",
		"docs/guides/domain-lens.md": "guide",
	} {
		if got[path].Provenance != want {
			t.Errorf("%s provenance = %q, want %q", path, got[path].Provenance, want)
		}
	}
	if got["docs/adr/0001-pick-a-db.md"].Type != "design" {
		t.Errorf("an ADR is a decision record: type = %q", got["docs/adr/0001-pick-a-db.md"].Type)
	}
	if got["docs/guides/domain-lens.md"].Type != "process" {
		t.Errorf("guide type = %q", got["docs/guides/domain-lens.md"].Type)
	}
}

// "GIVEN a workspace with none of these THEN the absent key is a no-op" — the
// sync reads an empty collection as "nothing to say", so a collector that
// invented an entry here would delete nothing but publish a phantom.
func TestAWorkspaceWithNoneYieldsNone(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/10-analytics.md", "# Per-context, not system-wide\n")
	write(t, root, "docs/20-surfaces-web.md", "# Prefix-matched by mistake?\n")
	write(t, root, "README.md", "# Not a document of this family\n")

	if docs := CollectDocuments(root); len(docs) != 0 {
		t.Fatalf("a workspace with none of the explanatory documents collected %d: %v", len(docs), docs)
	}
}

// The adopt rule (REQ-CROSS-088): exactly one architecture document reaches the
// reader, and which one is ordered, not arbitrary.
func TestExactlyOneArchitectureDocumentWins(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/03-architecture.md", "# Canonical\n")
	write(t, root, "ARCHITECTURE.md", "# The root one\n")

	docs := CollectDocuments(root)
	if len(docs) != 1 {
		t.Fatalf("carrying both reproduces the two-answers problem the adopt rule prevents: %v", docs)
	}
	if docs[0].Path != "docs/03-architecture.md" {
		t.Fatalf("first candidate must win, got %q", docs[0].Path)
	}
}

func TestARootArchitectureDocumentIsAdopted(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ARCHITECTURE.md", "# Adopted\n")

	docs := CollectDocuments(root)
	if len(docs) != 1 || docs[0].Path != "ARCHITECTURE.md" {
		t.Fatalf("REQ-CROSS-088's real-repository case dropped again: %v", docs)
	}
}

// The ADR README indexes the decisions; it is not one.
func TestTheADRIndexIsNotADecisionRecord(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/adr/README.md", "# Index\n")
	write(t, root, "docs/adr/0002-use-rls.md", "# ADR 2\n")

	docs := CollectDocuments(root)
	if len(docs) != 1 || docs[0].Path != "docs/adr/0002-use-rls.md" {
		t.Fatalf("want only the ADR, got %v", docs)
	}
}

// explanatoryDocs is a map, and Go randomizes map iteration per run. The sort
// is what makes the batch reproducible; without it the same workspace produces
// a differently ordered batch on every invocation.
func TestTheOrderIsStableAcrossRuns(t *testing.T) {
	root := t.TempDir()
	for _, rel := range []string{
		"docs/20-deployment-topology.md", "docs/21-integrations.md",
		"docs/22-cross-cutting.md", "docs/23-data-flow.md",
		"docs/adr/0001-a.md", "docs/guides/g.md",
	} {
		write(t, root, rel, "# "+rel+"\n")
	}

	first := CollectDocuments(root)
	if len(first) != 6 {
		t.Fatalf("collected %d, want 6", len(first))
	}
	for i := 0; i < 24; i++ {
		again := CollectDocuments(root)
		for j := range first {
			if again[j].Path != first[j].Path {
				t.Fatalf("run %d differs at %d: %q vs %q — map iteration is reaching the output",
					i, j, again[j].Path, first[j].Path)
			}
		}
	}
	for i := 1; i < len(first); i++ {
		if first[i-1].Path >= first[i].Path {
			t.Fatalf("not sorted by path: %q before %q", first[i-1].Path, first[i].Path)
		}
	}
}

func TestTheNameComesFromTheDocumentsOwnH1(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/23-data-flow.md", "---\ntitle: front matter\n---\n\n# How data moves\n\nbody\n")
	write(t, root, "docs/22-cross-cutting.md", "no heading at all\n")

	got := byPath(CollectDocuments(root))
	if got["docs/23-data-flow.md"].Name != "How data moves" {
		t.Errorf("name = %q, want the H1", got["docs/23-data-flow.md"].Name)
	}
	if got["docs/22-cross-cutting.md"].Name != "22-cross-cutting.md" {
		t.Errorf("without an H1 the filename is the fallback, got %q", got["docs/22-cross-cutting.md"].Name)
	}
}

func TestContentIsCappedNotDropped(t *testing.T) {
	root := t.TempDir()
	write(t, root, "docs/21-integrations.md", "# Big\n"+strings.Repeat("é", documentContentCap+500))

	docs := CollectDocuments(root)
	if len(docs) != 1 {
		t.Fatalf("want 1, got %v", docs)
	}
	if n := len([]rune(docs[0].Content)); n != documentContentCap {
		t.Fatalf("content capped to %d runes, want %d", n, documentContentCap)
	}
	if !strings.HasPrefix(docs[0].Content, "# Big\n") {
		t.Fatalf("the cap truncated from the wrong end")
	}
}

// The Path IS the sync identity. existsExact was written because macOS and
// Windows resolve a case-mismatched candidate, so the collector would record
// the CANDIDATE's spelling and the same repository would produce a different
// document id on a Mac than in Linux CI — a silent duplicate rather than an
// update. That reasoning is not specific to the architecture candidates.
func TestACollectedPathIsTheOnDiskSpelling(t *testing.T) {
	for _, tc := range []struct{ name, onDisk string }{
		{"architecture candidate", "docs/Architecture.md"},
		{"explanatory document", "docs/23-Data-Flow.md"},
		{"adr", "docs/adr/0001-Pick-A-DB.md"},
		{"guide", "docs/guides/Domain-Lens.md"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, tc.onDisk, "# Doc\n")

			for _, d := range CollectDocuments(root) {
				entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(d.Path)))
				if err != nil {
					t.Fatalf("collected %q, whose directory does not exist", d.Path)
				}
				found := false
				for _, e := range entries {
					if e.Name() == filepath.Base(d.Path) {
						found = true
					}
				}
				if !found {
					t.Fatalf("collected %q but the file on disk is %q — the sync identity is a spelling "+
						"that only resolves on a case-insensitive filesystem", d.Path, tc.onDisk)
				}
			}
		})
	}
}
