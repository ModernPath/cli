package kit

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// REQ-CROSS-173: a skill the shipped corpus points at is a skill the CLI ships.
//
// `TestEverySkillIsInInstallTargets` walks assets/ and asks whether each entry
// is installed. That direction cannot see the failure that actually happened: a
// skill never added to assets/ at all. It passes vacuously, which is how
// `rdd-audit` and `mp-knowledge-search` reached a consuming workspace as
// dangling pointers (RUN:2026-08-15, a consuming workspace).
//
// The cause is worth stating because it is not carelessness — it is the normal
// shape of an extraction. `rdd-audit` was split out of `rdd-reverse-engineer`,
// which then kept two "→ rdd-audit, §…" redirects where the content used to be.
// The pointer shipped; the target did not. An agent in the consuming repo
// follows the arrow to the citation sweep and finds nothing — and nothing errors,
// because a missing skill is silence, not a failure.
//
// So this check runs the other way: enumerate what the corpus REFERENCES and
// diff the shipped inventory against it, never the reverse.
func TestReferencedSkillsAreShipped(t *testing.T) {
	shipped := map[string]bool{}
	for _, dir := range []string{
		filepath.Join("assets", "skills"),
		filepath.Join("assets", "rdd", "skills"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() {
				shipped[e.Name()] = true
			}
		}
	}
	if len(shipped) == 0 {
		t.Fatal("no skills walked — the check would pass vacuously")
	}

	// Backticked skill names are how the corpus cites one. Prose mentions
	// without backticks are not directives and are deliberately out of scope.
	ref := regexp.MustCompile("`(rdd-[a-z0-9-]+|mp-[a-z0-9-]+)`")

	roots := []string{filepath.Join("assets", "skills"), filepath.Join("assets", "rdd")}
	checked := 0
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			checked++
			for _, m := range ref.FindAllStringSubmatch(string(body), -1) {
				name := m[1]
				if !shipped[name] {
					t.Errorf("%s cites `%s`, which the CLI does not ship — "+
						"an agent following that pointer finds nothing, silently", path, name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no documents read — the check would pass vacuously")
	}
}

// REQ-CROSS-175: no shipped skill cites a retired process file.
//
// The consolidated package retired the process/ manuals and templates/work/ —
// they are on retiredInstallTargets, so install DELETES them from consuming
// repositories. A skill citing one sends the reader to a file the installer
// itself removed. (`PROCESS.md` left this list when the consolidation made it
// the live canonical document.) Found live originally: rdd-verify cited
// "PROCESS.md §7.6" while that path was retired.
func TestNoSkillCitesARetiredProcessFile(t *testing.T) {
	retired := regexp.MustCompile(`V-model-loop\.md|state-tracking\.md|prompts\.md|templates/work/|\.claude/rdd/|interview-flows\.md`)
	checked := 0
	for _, dir := range []string{
		filepath.Join("assets", "skills"),
		filepath.Join("assets", "rdd", "skills"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name(), "SKILL.md")
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			checked++
			for i, line := range strings.Split(string(body), "\n") {
				if m := retired.FindString(line); m != "" {
					t.Errorf("%s:%d cites retired %q — install deletes that file from consuming repos", path, i+1, m)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no skills read — the check would pass vacuously")
	}
}

// REQ-CROSS-175: every skill has frontmatter, and the harness adapter block
// names every shipped skill.
//
// Two halves of the same discoverability contract. A skill without frontmatter
// never triggers in a harness that surfaces skills by description — it installs
// and then does not exist. And a skill absent from agents-block.md is invisible
// to every NON-Claude harness, because that block (merged into AGENTS.md and
// reached from copilot-instructions.md) is the only place they learn the skills
// exist. Found live: the block named 3 of 8 — the same inventory-rot direction
// as REQ-CROSS-173, one layer up.
func TestSkillsAreDiscoverableOnEveryHarness(t *testing.T) {
	block, err := os.ReadFile(filepath.Join("assets", "agents-block.md"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range []string{
		filepath.Join("assets", "skills"),
		filepath.Join("assets", "rdd", "skills"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			checked++
			if !strings.HasPrefix(string(body), "---\n") || !strings.Contains(string(body), "\ndescription:") {
				t.Errorf("%s/%s/SKILL.md has no frontmatter description — it never triggers in a description-driven harness", dir, e.Name())
			}
			if !strings.Contains(string(block), e.Name()) {
				t.Errorf("agents-block.md does not name %s — non-Claude harnesses never learn it exists", e.Name())
			}
		}
	}
	if checked == 0 {
		t.Fatal("no skills read — the check would pass vacuously")
	}
}

// The retired-vs-live collision pin: a path retired by one layout can be claimed again by
// a later one, and retirement runs after the write pass — so without the
// live-target subtraction in RetiredTargets, install deletes a file it just
// wrote and reports success (observed live: `.modernpath/rdd/PROCESS.md`,
// retired by the pre-consolidation layout and live in the consolidated one;
// `.claude/skills/rdd-verify/SKILL.md`, retired with the workspace skill set
// and re-claimed by the package skill of the same name).
func TestNoRetiredTargetIsALiveTarget(t *testing.T) {
	assetPaths, err := ListAssets()
	if err != nil {
		t.Fatal(err)
	}
	live := map[string]bool{}
	for _, asset := range assetPaths {
		if target, ok := TargetForAsset(asset); ok {
			live[target] = true
		}
		if target, ok := ClaudeSkillTargetForAsset(asset); ok {
			live[target] = true
		}
	}
	for _, target := range mergeTargets {
		live[target] = true
	}

	// The raw retired list must overlap live targets — that collision is the
	// scenario this pin exists for. If it ever stops overlapping, the pin is
	// vacuous and should be re-pointed at whatever collision replaced it.
	overlap := 0
	for _, target := range retiredInstallTargets {
		if live[target] {
			overlap++
		}
	}
	if overlap == 0 {
		t.Fatal("no retired path collides with a live target any more — the subtraction is untested; re-point this pin")
	}

	retired, err := RetiredTargets()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range retired {
		if live[target] {
			t.Errorf("RetiredTargets includes live target %s — install would delete the file it just wrote and report success", target)
		}
	}
}

// A mapping whose asset does not exist in the embed is silently never
// visited — the install walk is asset-driven, so the target simply fails to
// appear with no error (observed live: a restored skill once landed in a
// phantom nested path and every suite stayed green while it was missing
// from the embed).
func TestEveryInstallTargetAssetExists(t *testing.T) {
	for asset := range installTargets {
		if _, err := assets.ReadFile(asset); err != nil {
			t.Errorf("installTargets maps %s, which is not in the embedded tree: %v — the mapping is silently dead", asset, err)
		}
	}
	for asset := range legacyInstallTargets {
		if _, err := assets.ReadFile(asset); err != nil {
			t.Errorf("legacyInstallTargets maps %s, which is not in the embedded tree: %v", asset, err)
		}
	}
	for asset := range mergeTargets {
		if _, err := assets.ReadFile(asset); err != nil {
			t.Errorf("mergeTargets maps %s, which is not in the embedded tree: %v", asset, err)
		}
	}
}

// REQ-CROSS-174: the four hollowed skills carry their full content again.
//
// REQ-CROSS-029 had replaced them with ten-line "Canonical process pointer"
// stubs, and five of the ledger skill's eight rules survived nowhere in the docs
// those pointers named. Restoring the content fixed it once; nothing since
// asserts it stayed fixed, and a stub passes every shape and frontmatter check
// there is — it has valid frontmatter, a heading, and a body. The regression is
// invisible to everything except a reader who notices the rules are gone.
//
// This detects the known stub FORM, which is what the criterion names. It cannot
// judge thinness in general, and does not claim to.
// scanForStubs returns every SKILL.md under dirs that carries the stub marker,
// and how many it read. Separated so the detector can be exercised against a
// planted fixture: the embedded assets must never be edited, not even
// temporarily, so a mutation run cannot prove this test by breaking them.
func scanForStubs(t *testing.T, dirs []string) (found []string, checked int) {
	t.Helper()
	stub := regexp.MustCompile(`(?i)canonical process pointer`)
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name(), "SKILL.md")
			body, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			checked++
			if stub.Match(body) {
				found = append(found, path)
			}
		}
	}
	return found, checked
}

// REQ-CROSS-174: the four hollowed skills carry their full content again.
//
// REQ-CROSS-029 had replaced them with ten-line "Canonical process pointer"
// stubs, and five of the ledger skill's eight rules survived nowhere in the docs
// those pointers named. Restoring the content fixed it once; nothing since
// asserts it stayed fixed, and a stub passes every shape and frontmatter check
// there is — it has valid frontmatter, a heading, and a body. The regression is
// invisible to everything except a reader who notices the rules are gone.
//
// This detects the known stub FORM, which is what the criterion names. It cannot
// judge thinness in general, and does not claim to.
func TestNoShippedSkillIsAPointerStub(t *testing.T) {
	found, checked := scanForStubs(t, []string{
		filepath.Join("assets", "skills"),
		filepath.Join("assets", "rdd", "skills"),
	})
	for _, path := range found {
		t.Errorf("%s is a pointer stub — a pointer has valid frontmatter and a heading, so every "+
			"other check here passes while the rules it replaced survive nowhere (REQ-CROSS-174)", path)
	}
	if checked == 0 {
		t.Fatal("no skills read — the check would pass vacuously")
	}
	if checked < 12 {
		t.Fatalf("only %d skills read; the package ships more, so the sweep is missing a directory", checked)
	}
}

// The detector, proved against a planted stub rather than against the real
// assets. Without this the test above is a green light that has never been seen
// to go red.
func TestThePointerStubDetectorActuallyDetects(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("rdd-real", "---\nname: rdd-real\n---\n\n# A skill\n\nRules that exist.\n")
	write("rdd-hollow", "---\nname: rdd-hollow\n---\n\n# Canonical process pointer\n\nSee PROCESS.md.\n")

	found, checked := scanForStubs(t, []string{root})

	if checked != 2 {
		t.Fatalf("read %d skills, want 2", checked)
	}
	if len(found) != 1 || filepath.Base(filepath.Dir(found[0])) != "rdd-hollow" {
		t.Fatalf("detector found %v, want only rdd-hollow — it must flag the stub and leave the real skill alone", found)
	}
}
