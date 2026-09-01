package kit

import (
	"fmt"
	"strings"
	"testing"
)

// REQ-CROSS-151: a shipped skill's long sections carry sub-headings.
//
// Three sections crossed into unnavigable during one session — D6 at 463 lines,
// C8 at 221, D1 at 240 — each reached the same way, by appending one good rule
// at a time to a section nobody re-reads whole. Each was found by measuring on a
// whim rather than by anything that would have said so.
//
// The threshold is a navigability heuristic, not a size limit: a long section is
// fine, a long section a reader cannot scan is not.
func TestSkillSectionsAreNavigable(t *testing.T) {
	const maxUnstructured = 150

	entries, err := assets.ReadDir("assets/skills")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		path := "assets/skills/" + e.Name() + "/SKILL.md"
		body, err := assets.ReadFile(path)
		if err != nil {
			continue // a skill without a SKILL.md is another test's problem
		}
		for _, s := range oversizedSections(string(body), maxUnstructured) {
			t.Errorf("%s: section %q is %d lines with no sub-headings — group its rules under ### headings",
				path, s.title, s.lines)
		}
	}
}

type section struct {
	title string
	lines int
}

// oversizedSections returns sections longer than max that carry no sub-heading
// one level deeper. Fenced blocks are skipped: a record template inside ```
// fences contains headings that are content, not structure.
//
// REQ-CROSS-160: this measures EVERY level, not just "## ". The original checked
// `##` sections and exempted any carrying one `###` — a permanent exemption, so
// the sub-sections themselves could grow without limit and never be measured.
// That is exactly what happened: two `###` sections reached 188 and 177 lines
// while this test stayed green.
//
// A section's length is its OWN prose — lines until the next heading of the same
// or higher level — so a short parent with well-structured children is not
// flagged for their total.
func oversizedSections(body string, max int) []section {
	type open struct {
		title string
		level int
		lines int
		subs  int
	}
	var out []section
	var stack []open
	inFence := false

	closeTo := func(level int) {
		for len(stack) > 0 && stack[len(stack)-1].level >= level {
			s := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if s.lines > max && s.subs == 0 {
				out = append(out, section{s.title, s.lines})
			}
		}
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			continue
		}
		level := 0
		if !inFence {
			for _, p := range []int{2, 3, 4, 5} {
				if strings.HasPrefix(line, strings.Repeat("#", p)+" ") {
					level = p
					break
				}
			}
		}
		if level > 0 {
			closeTo(level)
			if len(stack) > 0 {
				stack[len(stack)-1].subs++
			}
			stack = append(stack, open{strings.TrimPrefix(line, strings.Repeat("#", level)+" "), level, 0, 0})
			continue
		}
		// Prose counts towards the innermost open section only — a parent is
		// measured on its own preamble, not on its children's contents.
		if len(stack) > 0 {
			stack[len(stack)-1].lines++
		}
	}
	closeTo(2)
	return out
}

var _ = fmt.Sprintf
