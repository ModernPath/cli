package kit

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Epic 436, SR-4919: the authoring skill rdd-constraint turns a rule in a
// person's words into a Proposed Engineering Constraint. TestCase
// 74c108c7-d0cc-4d13-a21a-e4290443d18c covers its install targets, its naming
// in the process documents and the phrases its text must carry.
const rddConstraintAsset = "assets/rdd/skills/rdd-constraint/SKILL.md"

func rddConstraintSkillText(t *testing.T) string {
	t.Helper()
	body, err := assets.ReadFile(rddConstraintAsset)
	if err != nil {
		t.Fatalf("the rdd-constraint skill asset is missing: %v", err)
	}
	return string(body)
}

// Criterion 835d5d7d: the skill is installed as .modernpath/rdd/skills/rdd-constraint/SKILL.md
// and the byte-identical .claude/skills/rdd-constraint/SKILL.md, and install --check
// reports no drift. The skill has no references folder, so the skill-references
// manifest lists nothing for it.
// TestCase 74c108c7-d0cc-4d13-a21a-e4290443d18c
func TestRddConstraintSkillInstallsToBothSkillTrees(t *testing.T) {
	body := rddConstraintSkillText(t)
	if !strings.HasPrefix(body, "---\nname: rdd-constraint\ndescription: ") {
		t.Errorf("the skill frontmatter must open with name: rdd-constraint and a description; got %q", firstLines(body, 3))
	}

	target, ok := TargetForAsset(rddConstraintAsset)
	if !ok || target != ".modernpath/rdd/skills/rdd-constraint/SKILL.md" {
		t.Errorf("TargetForAsset = %q, %v; want .modernpath/rdd/skills/rdd-constraint/SKILL.md", target, ok)
	}
	claude, ok := ClaudeSkillTargetForAsset(rddConstraintAsset)
	if !ok || claude != ".claude/skills/rdd-constraint/SKILL.md" {
		t.Errorf("ClaudeSkillTargetForAsset = %q, %v; want .claude/skills/rdd-constraint/SKILL.md", claude, ok)
	}

	var assetPaths []string
	if err := fs.WalkDir(assets, "assets", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			assetPaths = append(assetPaths, p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, managed := range managedSkillReferenceTargets(assetPaths) {
		if strings.Contains(managed, "rdd-constraint") {
			t.Errorf("the skill-references manifest lists %s; rdd-constraint ships no references", managed)
		}
	}

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-constraint", "SKILL.md")
	discovered := installedKitFile(t, root, ".claude", "skills", "rdd-constraint", "SKILL.md")
	if !bytes.Equal(canonical, []byte(body)) {
		t.Error("the installed .modernpath copy differs from the embedded asset")
	}
	if !bytes.Equal(canonical, discovered) {
		t.Error("the .claude/skills copy differs from the .modernpath/rdd/skills copy")
	}
	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 0 {
		t.Errorf("install --check reports drift after a fresh install: %v", drift)
	}
	manifest, err := os.ReadFile(filepath.Join(root, managedSkillReferencesFile))
	if err == nil && strings.Contains(string(manifest), "rdd-constraint") {
		t.Error("the installed skill-references manifest names rdd-constraint, which ships no references")
	}
}

// Criterion 1948fe0d: the PROCESS.md item table names the Engineering
// Constraint record with its EC-<number> ref and its three states, and the rdd
// AGENTS.md required reading and the managed agents block name rdd-constraint.
// TestCase 74c108c7-d0cc-4d13-a21a-e4290443d18c
func TestRddConstraintSkillIsNamedInTheProcessDocuments(t *testing.T) {
	read := func(parts ...string) string {
		body, err := os.ReadFile(filepath.Join(parts...))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	agents := read("assets", "rdd", "AGENTS.md")
	if !strings.Contains(agents, "skills/rdd-constraint/SKILL.md") {
		t.Error("assets/rdd/AGENTS.md required reading does not name skills/rdd-constraint/SKILL.md")
	}
	block := read("assets", "agents-block.md")
	if !strings.Contains(block, "`rdd-constraint`") {
		t.Error("assets/agents-block.md skill list does not name `rdd-constraint`")
	}
	process := read("assets", "rdd", "PROCESS.md")
	if !strings.Contains(process, "`rdd-constraint`") {
		t.Error("assets/rdd/PROCESS.md does not name `rdd-constraint` where it lists skills")
	}
	var row string
	for _, line := range strings.Split(process, "\n") {
		if strings.HasPrefix(line, "| `EC") {
			row = line
		}
	}
	if row == "" {
		t.Fatal("assets/rdd/PROCESS.md item table has no `EC` row for the Engineering Constraint record")
	}
	for _, phrase := range []string{"Engineering Constraint", "EC-<number>", "Proposed", "Active", "Deactivated", "never a requirement kind"} {
		if !strings.Contains(row, phrase) {
			t.Errorf("the PROCESS.md EC row lacks %q: %s", phrase, row)
		}
	}
}

// Criteria 6d08e9be (the eight steps in order), a14a50cf (the four shapes with
// a good and a bad example each), 480ed6f4 (the three refusals) and 20254f82
// (constraints create is the only store write; accept, deactivate, editing an
// Active rule's effective text and any write from a delegated agent are
// forbidden).
// TestCase 74c108c7-d0cc-4d13-a21a-e4290443d18c
func TestRddConstraintSkillStepsShapesRefusalsAndWrites(t *testing.T) {
	raw := rddConstraintSkillText(t)
	text := strings.Join(strings.Fields(raw), " ")

	steps := []string{
		"Restate the rule in one sentence and confirm it with the person",
		"Pick the shape with the person",
		"Write the statement as a sentence a reviewer can test on a change",
		"Decide whether the statement already says how a reviewer decides",
		"leave the verification method empty",
		"Write one only for what the statement leaves open",
		"Write the reason in the person's words",
		"Check the list for a duplicate or a conflict",
		"on the person's word, run `modernpath constraints create --file <file>`",
		"Read back the ref and the Proposed state with `modernpath constraints get <ref>`",
		"Draft the acceptance reason",
		"acceptance is the person's",
	}
	last := -1
	for _, step := range steps {
		at := strings.Index(text, step)
		if at < 0 {
			t.Errorf("the skill lacks the step %q", step)
			continue
		}
		if at < last {
			t.Errorf("the step %q is out of order", step)
		}
		last = at
	}
	for _, shape := range []string{"target platform", "sanctioned mechanism", "design rule", "change obligation"} {
		if !strings.Contains(text, shape) {
			t.Errorf("the shape step does not offer %q", shape)
		}
	}
	for _, phrase := range []string{"naming the document when there is one", "report what it found, with refs"} {
		if !strings.Contains(text, phrase) {
			t.Errorf("the skill lacks %q", phrase)
		}
	}

	if !strings.Contains(raw, "| Shape | Write it as | Good | Bad, and why |") {
		t.Error("the writing guidance table lacks the Shape, Write it as, Good and Bad columns")
	}
	for _, shape := range []string{"Target platform", "Sanctioned mechanism", "Design rule", "Change obligation"} {
		var row string
		for _, line := range strings.Split(raw, "\n") {
			if strings.HasPrefix(line, "| "+shape+" |") {
				row = line
			}
		}
		if row == "" {
			t.Errorf("the writing guidance has no row for the shape %q", shape)
			continue
		}
		cells := strings.Split(strings.Trim(row, "|"), "|")
		if len(cells) != 4 {
			t.Errorf("the %s row has %d cells, want shape, how to write it, a good and a bad example: %s", shape, len(cells), row)
			continue
		}
		for i, name := range []string{"how to write it", "good example", "bad example"} {
			if strings.TrimSpace(cells[i+1]) == "" {
				t.Errorf("the %s row has an empty %s cell", shape, name)
			}
		}
		if strings.Count(cells[2], `"`) < 2 || strings.Count(cells[3], `"`) < 2 {
			t.Errorf("the %s row's good and bad cells must each quote an example: %s", shape, row)
		}
	}

	for _, phrase := range []string{
		"a rule it cannot make judgeable",
		"a rule that restates an Active one",
		"a rule that contradicts",
		"without the person deciding which stands",
		"In each case say why and ask",
	} {
		if !strings.Contains(text, phrase) {
			t.Errorf("the refusals lack %q", phrase)
		}
	}

	for _, phrase := range []string{
		"`modernpath constraints create --file <file>` is the skill's only store write",
		"never accepts, never deactivates, never edits an Active rule's effective text, and never writes from a delegated agent",
		"Accept and deactivate are the person's acts",
		"`modernpath constraints accept <ref> --reason \"USER:<date>: ...\"`",
		"`modernpath constraints deactivate <ref> --reason \"USER:<date>: ...\"`",
		"forbidden for the skill, as are editing an Active rule's effective text and any store write from a delegated agent",
		`{"title": "...", "statement": "...", "rationale": "...", "verification_method": "..."}`,
	} {
		if !strings.Contains(text, phrase) {
			t.Errorf("the interfaces lack %q", phrase)
		}
	}
}

func firstLines(body string, n int) string {
	lines := strings.SplitN(body, "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
