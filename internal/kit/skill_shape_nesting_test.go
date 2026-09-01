package kit

import "strings"

import "testing"

// REQ-CROSS-160: the navigability rule must apply at every heading level.
//
// As first written (REQ-CROSS-151) it measured only `## ` sections and exempted
// any that carried at least one `### `. That exemption is permanent: once a
// section has one sub-heading it can grow without limit, and the sub-sections
// themselves are never measured. On RUN:2026-08-14 the skill this test guards
// had two `### ` sections at 188 and 177 lines with no internal structure —
// both grown a rule at a time by the same passes that wrote the rule. The test
// was green throughout.
func TestOversizedSectionsMeasuresEveryLevel(t *testing.T) {
	long := strings.Repeat("a rule that someone appended.\n", 60)

	t.Run("a long ### with no #### is flagged", func(t *testing.T) {
		body := "## Parent\n\n### Child\n\n" + long
		got := oversizedSections(body, 50)
		if len(got) != 1 || got[0].title != "Child" {
			t.Fatalf("got %v, want the oversized ### section 'Child'", got)
		}
	})

	t.Run("a long ### with #### sub-headings passes", func(t *testing.T) {
		half := strings.Repeat("a rule.\n", 30)
		body := "## Parent\n\n### Child\n\n#### One\n" + half + "\n#### Two\n" + half
		if got := oversizedSections(body, 50); len(got) != 0 {
			t.Fatalf("got %v, want none — the section is structured", got)
		}
	})

	t.Run("a long ## still counts its own prose, not its children's", func(t *testing.T) {
		// A parent whose own preamble is short but which has structured
		// children must not be flagged for the total.
		half := strings.Repeat("a rule.\n", 30)
		body := "## Parent\n\nshort preamble\n\n### A\n" + half + "\n### B\n" + half
		for _, s := range oversizedSections(body, 50) {
			if s.title == "Parent" {
				t.Fatalf("Parent flagged at %d lines — a parent is measured on its own prose", s.lines)
			}
		}
	})

	t.Run("headings inside fences are still content", func(t *testing.T) {
		body := "## Parent\n\n### Child\n\n```\n### not a heading\n```\n" + long
		got := oversizedSections(body, 50)
		if len(got) != 1 || got[0].title != "Child" {
			t.Fatalf("got %v — a fenced ### must not exempt the section", got)
		}
	})
}
