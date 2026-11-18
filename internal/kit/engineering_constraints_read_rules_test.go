package kit

import (
	"bytes"
	"strings"
	"testing"
)

// Epic 437 (SR-4922, criteria 2874c295, a940c395, cfd8ced4; UR-907 criterion
// 436299a5): the delivery process text the CLI installs reads the System's
// Active Engineering Constraints at the moments it reads the planning
// contexts. rdd-start reads them at session entry, rdd-plan again before
// planning and names in the reconnaissance each one the change touches by its
// EC ref, rdd-build again before the first change; each opens the ones the
// change may touch with `constraints get <ref>`, whose Active text is the
// current revision. PROCESS.md names the read moments, the bundle field, that
// a constraint change does not outdate a review and that a finding whose only
// ground is a constraint is a note. Each skill's .claude/skills copy equals
// its .modernpath/rdd copy. Phrases are compared on folded text, as the
// planning-context rules are (planningContextText).

const (
	constraintsListRead   = "constraints list --state active"
	constraintsGetRead    = "constraints get <ref>"
	constraintsActiveText = "the active text is the current revision, not the row text when a change is pending"
)

// TestCase c3622dce-8bb8-4f8e-8a9b-018a5b4de9c5
func TestInstalledProcessNamesTheEngineeringConstraintsRead(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	asset, err := assets.ReadFile("assets/rdd/PROCESS.md")
	if err != nil {
		t.Fatal(err)
	}
	installed := installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md")
	if !bytes.Equal(asset, installed) {
		t.Error("the installed PROCESS.md differs from the embedded asset")
	}
	process := planningContextText(installed)
	for _, phrase := range []string{
		// The read moments, the same as the planning contexts'.
		"active engineering constraints",
		"at session entry and before the agent plans and before it builds",
		constraintsListRead,
		constraintsGetRead,
		constraintsActiveText,
		// The bundle field the cold review reads.
		"the review bundle carries them as engineering_constraints",
		"the cold review reads them beside the planning contexts",
		// A constraint change leaves a review current; a citing finding is information.
		"a constraint accepted or changed after a plan review does not outdate that review",
		"a finding that cites a constraint names it as ec-<number> with the revision read and is information, not a verdict",
		"a finding whose only ground is a constraint is recorded with severity note and blocks no gate",
	} {
		if !strings.Contains(process, phrase) {
			t.Errorf("the installed PROCESS.md is missing %q", phrase)
		}
	}
}

// TestCase c3622dce-8bb8-4f8e-8a9b-018a5b4de9c5
func TestInstalledSkillsReadTheEngineeringConstraints(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, skill := range []struct {
		name    string
		phrases []string
	}{
		{"rdd-start", []string{
			"read the system's active engineering constraints at the same moment with " + constraintsListRead,
			"the epic is held to them as written, by ref and revision",
			"open each one the change may touch with " + constraintsGetRead,
			constraintsActiveText,
		}},
		{"rdd-plan", []string{
			"read the system's active engineering constraints at the same moment with " + constraintsListRead,
			"open each one the change may touch with " + constraintsGetRead,
			constraintsActiveText,
			"name in the reconnaissance each active constraint the change touches by its ec ref",
			"a change that contradicts one is a question for the human",
		}},
		{"rdd-build", []string{
			"read the system's active engineering constraints beside them with " + constraintsListRead,
			"open each one the change may touch with " + constraintsGetRead,
			constraintsActiveText,
			"build keeps to them as written",
		}},
	} {
		asset, err := assets.ReadFile("assets/rdd/skills/" + skill.name + "/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", skill.name, "SKILL.md")
		claude := installedKitFile(t, root, ".claude", "skills", skill.name, "SKILL.md")
		if !bytes.Equal(asset, canonical) {
			t.Errorf("%s: the installed .modernpath/rdd copy differs from the embedded asset", skill.name)
		}
		if !bytes.Equal(canonical, claude) {
			t.Errorf("%s: the .claude/skills copy differs from the .modernpath/rdd copy", skill.name)
		}
		text := planningContextText(canonical)
		for _, phrase := range skill.phrases {
			if !strings.Contains(text, phrase) {
				t.Errorf("the installed %s is missing %q", skill.name, phrase)
			}
		}
		// The read sits beside the planning-context read, not elsewhere in the skill.
		contexts := strings.Index(text, planningContextRead)
		constraints := strings.Index(text, constraintsListRead)
		if contexts < 0 || constraints < 0 || constraints-contexts > 600 || constraints < contexts {
			t.Errorf("the installed %s must name %q after and beside %q (positions %d, %d)", skill.name, constraintsListRead, planningContextRead, contexts, constraints)
		}
	}
}
