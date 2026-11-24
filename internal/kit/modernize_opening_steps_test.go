package kit

import (
	"strings"
	"testing"
)

// Epic 460 (H3), TestCase TC-H3-S11, SR-6 criterion 0: rdd-modernize holds,
// right before its capture steps, the section Start the proof, whose three
// numbered steps say what the agent does, in this order: it commits the
// capture scripts to the legacy repository, because the platform accepts a
// capture, a recording of the legacy system's behaviour, only when it was made
// at the reference revision, the commit of the legacy system that every proof
// compares against, or, onto a new system, at a later commit of the legacy
// repository when that commit and the reference revision are both in its
// analysed history, so the scripts must be in the commit that becomes the
// reference revision; at that commit, it runs the
// legacy system's own tests and records their results under the Release with
// the capture command, from a folder bound to the new system, onto a new system
// also giving the command the path of the legacy checkout so that the platform
// keeps the commit's message, author and date; and it asks a person on the team
// to choose that commit as the reference revision on the Modernization tab of
// the Release page, which no command can do. The section names no command or
// flag, and every sentence the kit's existing phrase tests assert stays, as
// those tests check (TestModernizeProcedure and the others).
//
// The section is the product owner's approved text word for word (DESIGN.md §8
// of Epic 460's plan: revision 6's rewrite and revision 7's clause "and the
// reference revision is in it too"): the folded section equals his folded
// text, and each of its ten sentences is also a phrase test of its own.
//
// The skill is read as TestModernizeProcedure reads it: whitespace folded and
// code marks stripped. The section runs from its heading "## Start the proof"
// to the next heading of its level, which is "## The capture steps".
//
// With revision 6's text, sentence 5 and the comparison with the approved text
// fail; with the wording built before it, every sentence fails.

// h3ApprovedSection is the section as the product owner approved it.
const h3ApprovedSection = `## Start the proof

Before the capture steps, do these three things in this order.

1. The agent commits the capture scripts to the legacy repository. A capture is a recording of the legacy system's behaviour, and the platform accepts a capture only when it was made at the reference revision: the commit of the legacy system that every proof compares against. So the scripts must be in the commit that becomes the reference revision. When the modernization goes onto a new system, the platform also accepts a capture made at a later commit of the legacy repository, if that commit is in its analysed history and the reference revision is in it too.
2. At that commit, the agent runs the legacy system's own tests and records their results under the Release with the capture command. The agent runs the command from a folder that is bound to the new system. When the modernization goes onto a new system, the agent also gives the command the path of the legacy checkout, so that the platform keeps the commit's message, author and date.
3. The agent asks a person on the team to choose that commit as the reference revision, on the Modernization tab of the Release page. No command can choose it.
`

// h3OpeningSteps are the section's sentences, in their order.
var h3OpeningSteps = []struct{ name, text string }{
	{"three things before the capture steps, in this order",
		"Before the capture steps, do these three things in this order."},
	{"the agent commits the capture scripts to the legacy repository",
		"The agent commits the capture scripts to the legacy repository."},
	{"a capture is accepted only at the reference revision",
		"A capture is a recording of the legacy system's behaviour, and the platform accepts a capture only when it was made at the reference revision: the commit of the legacy system that every proof compares against."},
	{"so the scripts are in the commit that becomes the reference revision",
		"So the scripts must be in the commit that becomes the reference revision."},
	{"onto a new system, a later commit in the analysed history, with the reference revision",
		"When the modernization goes onto a new system, the platform also accepts a capture made at a later commit of the legacy repository, if that commit is in its analysed history and the reference revision is in it too."},
	{"the legacy system's own test results are recorded at that commit",
		"At that commit, the agent runs the legacy system's own tests and records their results under the Release with the capture command."},
	{"from a folder bound to the new system",
		"The agent runs the command from a folder that is bound to the new system."},
	{"onto a new system, the path of the legacy checkout",
		"When the modernization goes onto a new system, the agent also gives the command the path of the legacy checkout, so that the platform keeps the commit's message, author and date."},
	{"a person on the team chooses that commit",
		"The agent asks a person on the team to choose that commit as the reference revision, on the Modernization tab of the Release page."},
	{"no command can choose it",
		"No command can choose it."},
}

// h3Section is the text of rdd-modernize from the line `heading` to the next
// line that starts a heading of the same level, and that next heading.
func h3Section(t *testing.T, body, heading string) (section, next string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		return "", ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end, next = i, strings.TrimSpace(lines[i])
			break
		}
	}
	return strings.Join(lines[start:end], "\n"), next
}

func TestH3_ModernizeProcedureStartsTheProofBeforeTheCaptureSteps(t *testing.T) {
	raw, err := assets.ReadFile("assets/rdd/skills/rdd-modernize/SKILL.md")
	if err != nil {
		t.Fatalf("the kit must embed rdd-modernize/SKILL.md: %v", err)
	}
	fold := func(text string) string {
		return strings.Join(strings.Fields(strings.ReplaceAll(text, "`", "")), " ")
	}
	section, next := h3Section(t, string(raw), "## Start the proof")

	t.Run("the section Start the proof comes right before The capture steps", func(t *testing.T) {
		if section == "" {
			t.Fatalf("rdd-modernize/SKILL.md has no section ## Start the proof")
		}
		if next != "## The capture steps" {
			t.Errorf("the heading after ## Start the proof is %q; want ## The capture steps", next)
		}
		if strings.Index(string(raw), "## Start the proof") > strings.Index(string(raw), "## The capture steps") {
			t.Errorf("## Start the proof comes after ## The capture steps")
		}
	})

	plain := fold(section)
	at := -1
	for _, sentence := range h3OpeningSteps {
		t.Run("H3 SR-6 "+sentence.name, func(t *testing.T) {
			next := strings.Index(plain, sentence.text)
			if next < 0 {
				t.Errorf("the section ## Start the proof is missing the sentence %q:\n%s", sentence.text, plain)
				return
			}
			if next < at {
				t.Errorf("the sentence %q is out of order:\n%s", sentence.text, plain)
			}
			at = next
		})
	}

	t.Run("the section is the owner's approved text word for word", func(t *testing.T) {
		if want := fold(h3ApprovedSection); plain != want {
			t.Errorf("the section ## Start the proof is not the approved text.\n got: %s\nwant: %s", plain, want)
		}
	})

	t.Run("the section names no command or flag", func(t *testing.T) {
		if section == "" {
			t.Fatalf("rdd-modernize/SKILL.md has no section ## Start the proof")
		}
		if strings.Contains(plain, "modernpath") {
			t.Errorf("the section ## Start the proof names a modernpath command:\n%s", plain)
		}
		if strings.Contains(plain, "--") {
			t.Errorf("the section ## Start the proof names a flag:\n%s", plain)
		}
		if strings.Contains(plain, "roadmap recording") {
			t.Errorf("the section ## Start the proof names the command roadmap recording:\n%s", plain)
		}
	})

	t.Run("the procedure still says it names no command (guard)", func(t *testing.T) {
		if want := "This procedure names no command: the commands are in the CLI reference."; !strings.Contains(fold(string(raw)), want) {
			t.Errorf("rdd-modernize/SKILL.md is missing the sentence %q", want)
		}
	})
}
