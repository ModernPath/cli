package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-132: every skill the kit installs is named in the process documents
// a reader is told to read.
//
// PROCESS.md §9 once carried one table listing four of the eight skills then
// installed. Nobody noticed, because "the inventory rotted in the direction
// nothing contradicts" — no reader misses a row that is absent. The test that
// caught it was retired in the origin/main merge, so the direction it enforced
// has been unguarded since.
//
// The consolidation split that single table in two, and the check follows: the
// phase table in PROCESS.md lists the skills that ARE phases, and AGENTS.md
// names the ones that are not (rdd-audit is "a shared utility other passes
// invoke, not a phase"). A skill named in neither is one the kit installs and
// no entry document mentions — invisible to the reader it was written for.
//
// Build-time rather than a workspace gate, because both halves ship in the kit:
// a workspace cannot make them disagree, only a release can.
// unnamedSkills returns the installed skills that no document mentions, and how
// many were read. Separated so the detector can be exercised against a planted
// fixture: assets/rdd/** must never be edited, not even temporarily, so a
// mutation run cannot show this test going red — and a green light never seen to
// go red is not evidence.
func unnamedSkills(t *testing.T, skillsDir string, docs map[string]string) (unnamed []string, checked int) {
	t.Helper()
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); err != nil {
			continue
		}
		checked++
		named := false
		for _, body := range docs {
			if strings.Contains(body, e.Name()) {
				named = true
			}
		}
		if !named {
			unnamed = append(unnamed, e.Name())
		}
	}
	return unnamed, checked
}

func TestEveryInstalledSkillIsNamedInTheProcessDocuments(t *testing.T) {
	docs := map[string]string{}
	for _, name := range []string{"AGENTS.md", "PROCESS.md"} {
		b, err := os.ReadFile(filepath.Join("assets", "rdd", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		docs[name] = string(b)
	}

	unnamed, checked := unnamedSkills(t, filepath.Join("assets", "rdd", "skills"), docs)
	for _, name := range unnamed {
		t.Errorf("%s is installed but named in neither AGENTS.md nor PROCESS.md — "+
			"a reader following the entry documents never learns it exists", name)
	}

	// The row's third criterion, and the reason the first attempt at this check
	// was a false pass: a sweep that finds nothing must fail, not pass.
	if checked == 0 {
		t.Fatal("no skills read — the check would pass vacuously")
	}
	if checked < 10 {
		t.Fatalf("only %d skills read; the package ships more, so the sweep is missing them", checked)
	}
	t.Logf("checked %d installed skills against AGENTS.md + PROCESS.md", checked)
}

// The detector, proved against a planted fixture rather than the real assets.
func TestTheSkillInventoryDetectorActuallyDetects(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"rdd-named", "rdd-orphan"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("# "+name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	unnamed, checked := unnamedSkills(t, root, map[string]string{
		"AGENTS.md":  "read skills/rdd-named/SKILL.md first",
		"PROCESS.md": "no skill names here",
	})

	if checked != 2 {
		t.Fatalf("read %d skills, want 2", checked)
	}
	if len(unnamed) != 1 || unnamed[0] != "rdd-orphan" {
		t.Fatalf("detector found %v, want only rdd-orphan — it must flag the unnamed skill and leave the named one alone", unnamed)
	}
}
