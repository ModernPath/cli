package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Epic 298 (SR-3815, TC-1400): the installed kit says how a delivered backlog,
// gap or tooling record is routed at plan save and closed after completion
// approval, and PROCESS.md says the records are created, routed and closed on
// the server. The sentences are pinned by distinctive fragments of the process
// text, read after whitespace normalisation from the installed
// copy under .modernpath/rdd, whose .claude/skills copy must be byte-identical.
func TestInstalledKitRoutesAndClosesBacklogRecords(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	closing := "use the CLI to mark each backlog, gap or tooling record routed to the Epic as closed by the Epic, and read each record back"
	keepOpen := "If the human wants a record left open, leave it open and add a note saying why."
	for _, c := range []struct {
		skill   string
		markers []string
	}{
		{"rdd-plan", []string{
			"use the CLI to mark each backlog, gap or tooling record the plan delivers as routed to the Epic, with a reference to this plan save, and read the record back",
			"Do the same when a later plan save adds a record.",
		}},
		{"rdd-triage", []string{
			"use the CLI to mark the record as routed to that Epic, with a reference to the plan save, and read it back",
			"Listing the record in the plan body does not route it.",
		}},
		{"rdd-completion-review", []string{closing, keepOpen}},
		{"rdd-deliver", []string{closing, keepOpen}},
	} {
		canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", c.skill, "SKILL.md")
		claude := installedKitFile(t, root, ".claude", "skills", c.skill, "SKILL.md")
		if !bytes.Equal(canonical, claude) {
			t.Errorf("%s: the .claude/skills copy differs from the .modernpath/rdd copy", c.skill)
		}
		text := kitWhitespaceNormalised(canonical)
		for _, marker := range c.markers {
			if !strings.Contains(text, marker) {
				t.Errorf("installed %s/SKILL.md does not say %q", c.skill, marker)
			}
		}
	}
	process := kitWhitespaceNormalised(installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md"))
	for _, marker := range []string{
		"they are created, routed and closed there",
		"is read from the server, not from the plan text",
		// The small-change lane (added with PR 54's second commit): a record a
		// small change delivers is closed by the merged pull request.
		"use the CLI to mark the record as closed by that pull request and read it back",
	} {
		if !strings.Contains(process, marker) {
			t.Errorf("installed PROCESS.md does not say %q", marker)
		}
	}
}

func installedKitFile(t *testing.T, root string, parts ...string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(append([]string{root}, parts...)...))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func kitWhitespaceNormalised(body []byte) string {
	return strings.Join(strings.Fields(string(body)), " ")
}

// Planning applies to Epic work; it must not override PROCESS.md's separate
// small-change route when the installed skills are read together.
func TestInstalledPlanningSkillPreservesSmallChangeRoute(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	plan := kitWhitespaceNormalised(installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-plan", "SKILL.md"))
	if strings.Contains(plan, "small changes have no exemption") {
		t.Fatal("planning skill contradicts the small-change route in PROCESS.md")
	}
	if !strings.Contains(plan, "PROCESS.md` §Small changes") {
		t.Fatal("planning skill must route small changes to the canonical exception")
	}
}
