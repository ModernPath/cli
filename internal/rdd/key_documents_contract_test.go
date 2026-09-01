package rdd

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/manifest"
)

// REQ-CROSS-184: the key document set — phase D's five system documents, the
// ADRs, and the NFR ledger — is the flagship client deliverable, and its
// definition lives in two places that must agree:
//
//	the skill      — rdd-reverse-engineer §D1/D2/D3 tells an agent what to write
//	the collector  — documents.go decides what actually reaches the platform
//
// Neither one checks the other. Edit the skill alone and a pass dutifully writes
// a document that never syncs; edit the collector alone and it hunts for a file
// nobody is told to write. Both failures are silent: the deliverable simply
// arrives short, and no test, build or sync goes red. That drift is what made
// this set nearly invisible in a fresh documentation audit (`USER:2026-08-17`).
//
// The guard reads the KIT-EMBEDDED skill, not the workspace copy: the embedded
// asset is what ships to a client repository, and `TestInstalledFilesMatchTheirSource`
// (REQ-CROSS-146, internal/kit) already pins the workspace copy to it. So one
// read covers both copies, and there is no second skill-content test to keep.
//
// It PARSES the expected set out of the skill rather than restating it. A guard
// that hardcodes the answer drifts exactly like the thing it guards — it would
// have to be edited in the same change as the collector, which is precisely the
// edit nobody remembers to make.

// keyDocumentSet is what the skill promises, read from the skill itself.
type keyDocumentSet struct {
	d1        []string // workspace-relative paths from the §D1 table, in table order
	adrDir    string   // from the §D2 heading
	nfrLedger string   // from the §D3 heading
	guidesDir string   // the input lens the skill tells a pass to READ
	body      string   // the whole skill, for "does the skill name this at all"
}

// skillPath locates the embedded reverse-engineering skill relative to this
// source file, the way internal/opschema pins its vendored schema copies.
func skillPath() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(thisFile, "..", "..", "kit", "assets", "rdd", "skills", "rdd-reverse-engineer", "SKILL.md")
}

func readKeyDocumentSet(t *testing.T) keyDocumentSet {
	t.Helper()
	raw, err := os.ReadFile(skillPath())
	if err != nil {
		t.Fatalf("cannot read the reverse-engineering skill at %s: %v — "+
			"the key document set has no second definition to check against, so this guard is blind (REQ-CROSS-184)",
			skillPath(), err)
	}
	body := string(raw)

	set := keyDocumentSet{
		d1:        d1TablePaths(t, body),
		adrDir:    dirOfHeadingPath(t, body, "## D2."),
		nfrLedger: headingPath(t, body, "## D3."),
		guidesDir: "docs/guides",
		body:      body,
	}

	// A parser that quietly finds nothing turns every containment check below
	// into a tautology, which is worse than no guard at all. Pin the shape.
	if len(set.d1) != 5 {
		t.Fatalf("parsed %d document paths from the skill's §D1 table (%v), want 5 — "+
			"either the table's shape changed or the set did. This guard can no longer tell the collector "+
			"whether it covers the deliverable; fix the parse or the table before trusting any green here (REQ-CROSS-184)",
			len(set.d1), set.d1)
	}
	if set.adrDir != "docs/adr" {
		t.Fatalf("§D2 names decision records under %q, want docs/adr — "+
			"the collector reads docs/adr, so every ADR a pass writes would land somewhere the sync never looks (REQ-CROSS-084)", set.adrDir)
	}
	if set.nfrLedger != "tasks/NFR-REQUIREMENTS.md" {
		t.Fatalf("§D3 names the NFR ledger %q, want tasks/NFR-REQUIREMENTS.md — "+
			"the quality-attribute rows reach the platform through the requirement-ledger glob, not the document collector (REQ-CROSS-084)", set.nfrLedger)
	}
	if !strings.Contains(body, set.guidesDir+"/") {
		t.Fatalf("the skill no longer names %s/ — the collector still carries those files as provenance %q, "+
			"so the platform would show a lens nobody is told to write or read (REQ-CROSS-084)", set.guidesDir, "guide")
	}
	return set
}

// d1TablePaths reads the first cell of every markdown table row in §D1, taking
// the backticked paths. The section ends at the next heading, which keeps the
// later "Document said / Reality" table out.
func d1TablePaths(t *testing.T, body string) []string {
	t.Helper()
	var out []string
	inSection := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "## D1.") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(line, "#") {
			break
		}
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 2 {
			continue
		}
		first := strings.TrimSpace(cells[1])
		if !strings.HasPrefix(first, "`") || !strings.HasSuffix(first, "`") {
			continue // header, separator, or a prose cell
		}
		if p := strings.Trim(first, "`"); strings.HasSuffix(p, ".md") {
			out = append(out, p)
		}
	}
	return out
}

// headingPath takes the backticked path out of a "## Dn. … — <path>" heading.
func headingPath(t *testing.T, body, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		open := strings.Index(line, "`")
		shut := strings.LastIndex(line, "`")
		if open < 0 || shut <= open {
			t.Fatalf("the %q heading no longer states its path in backticks: %q — "+
				"this guard reads the deliverable's definition from the skill and can no longer find it (REQ-CROSS-184)", prefix, line)
		}
		return line[open+1 : shut]
	}
	t.Fatalf("the skill has no %q section — a phase of the key document set has been dropped from the instructions "+
		"while the collector still expects it (REQ-CROSS-184)", prefix)
	return ""
}

func dirOfHeadingPath(t *testing.T, body, prefix string) string {
	t.Helper()
	return filepath.ToSlash(filepath.Dir(headingPath(t, body, prefix)))
}

// The forward direction: everything the skill tells an agent to write must have
// somewhere to land.
func TestCollectorCoversEveryDocumentTheSkillPromises(t *testing.T) {
	set := readKeyDocumentSet(t)

	candidate := map[string]bool{}
	for _, c := range architectureCandidates {
		candidate[filepath.ToSlash(c)] = true
	}

	for _, rel := range set.d1 {
		base := filepath.Base(rel)
		if _, ok := explanatoryDocs[base]; ok {
			continue
		}
		if candidate[rel] {
			continue
		}
		t.Errorf("the skill tells a phase-D pass to write %s, and the collector will never carry it: "+
			"it is in neither explanatoryDocs nor architectureCandidates. The pass writes the file, the sync ignores it, "+
			"and the client's document set arrives one document short with nothing failing (REQ-CROSS-084/088)", rel)
	}
}

// The reverse direction: the collector must not expect a document the skill
// never tells anyone to write.
func TestCollectorExpectsNoDocumentTheSkillDoesNotName(t *testing.T) {
	set := readKeyDocumentSet(t)

	named := map[string]bool{}
	for _, rel := range set.d1 {
		named[filepath.Base(rel)] = true
	}

	for base := range explanatoryDocs {
		if named[base] {
			continue
		}
		t.Errorf("the collector expects docs/%s but the skill's §D1 table never names it — "+
			"no pass will ever write that file, so the collector hunts for a document that cannot exist and the "+
			"platform shows a gap it can never close (REQ-CROSS-084)", base)
	}

	// The non-primary architecture candidates exist because §D1 says to ADOPT a
	// maintained architecture document rather than write a competing one
	// (REQ-CROSS-088). A candidate spelling the skill never mentions is a
	// filename only the collector believes in.
	for _, c := range architectureCandidates {
		if strings.Contains(set.body, filepath.Base(c)) {
			continue
		}
		t.Errorf("the collector would adopt %s as the architecture document, but the skill never mentions that filename — "+
			"a reviewer reading §D1 cannot tell which file plays the architecture role, which is the two-answers-to-one-question "+
			"failure the adopt rule exists to prevent (REQ-CROSS-088)", c)
	}

	if got := filepath.ToSlash(architectureCandidates[0]); got != set.d1[0] {
		t.Errorf("the collector's first architecture candidate is %s but §D1 leads with %s — "+
			"first match wins, so phase D's own document would lose to an adopted one and the reader gets the stale answer (REQ-CROSS-088)",
			got, set.d1[0])
	}
}

// End to end over a workspace built FROM the skill's own list: the collector
// carries exactly the promised set, no more, with the provenance split intact.
func TestCollectorCarriesExactlyTheKeyDocumentSet(t *testing.T) {
	set := readKeyDocumentSet(t)

	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	want := map[string]string{} // path -> provenance
	for _, rel := range set.d1 {
		write(rel, "# "+rel+"\n\nbody")
		want[rel] = "derived"
	}
	adr := set.adrDir + "/0001-a-job-queue.md"
	write(adr, "# 0001 — a job queue\n\nobserved")
	want[adr] = "derived"
	guide := set.guidesDir + "/configuration.md"
	write(guide, "# Where behaviour is configured\n\nsettings tables")
	want[guide] = "guide"

	// Files the collector must leave alone: the ADR index, a per-context
	// document, the repository README, and the NFR ledger — which reaches the
	// platform as a requirement ledger, not as a document.
	write(set.adrDir+"/README.md", "# ADRs")
	write("docs/10-analytics.md", "# 10 — Analytics")
	write("README.md", "# readme")
	write(set.nfrLedger, "# REQUIREMENTS — NFR\n")

	got := map[string]string{}
	for _, d := range CollectDocuments(root) {
		got[filepath.ToSlash(d.Path)] = d.Provenance
	}

	for rel, prov := range want {
		switch p, ok := got[rel]; {
		case !ok:
			t.Errorf("%s was written exactly as the skill instructs and the collector did not carry it — "+
				"the client's key document set is silently short one document (REQ-CROSS-084)", rel)
		case p != prov:
			t.Errorf("%s carried provenance %q, want %q — the split is what lets a reader tell what the system IS "+
				"(derived) from what someone TOLD the pass to look for (guide); collapse it and a hand-written lens "+
				"reads as a finding about the code (REQ-CROSS-084)", rel, p, prov)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("the collector carried %s, which the skill's key document set does not name — "+
				"an orphan expectation puts a file in front of the client that no phase-D rule governs (REQ-CROSS-084)", rel)
		}
	}
	if _, carried := got[set.nfrLedger]; carried {
		t.Errorf("%s was carried as a document — §D3 puts the quality attributes in an ordinary context ledger "+
			"so they sync and reconcile through machinery that already exists; as a document they would lose their "+
			"requirement status, sources and evidence (REQ-CROSS-084)", set.nfrLedger)
	}

	// …and the ledger machinery §D3 relies on really does pick it up.
	resolved := manifest.Default().Resolve(root, manifest.DocRequirements)
	found := false
	for _, p := range resolved {
		if rel, err := filepath.Rel(root, p); err == nil && filepath.ToSlash(rel) == set.nfrLedger {
			found = true
		}
	}
	if !found {
		t.Errorf("%s is not matched by the requirement-ledger glob %v — §D3 promises the NFR rows sync through "+
			"existing machinery, and they would reach the platform through nothing at all (REQ-CROSS-084)",
			set.nfrLedger, manifest.Default().Documents[manifest.DocRequirements].Globs())
	}

	sorted := make([]string, 0, len(got))
	for rel := range got {
		sorted = append(sorted, rel)
	}
	sort.Strings(sorted)
	t.Logf("key document set carried: %v", sorted)
}
