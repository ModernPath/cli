package kit

import (
	"bytes"
	"strings"
	"testing"
)

// Epic 292 (SR-3835.2, TC-1432): the process installed into an empty folder
// says what the criterion promises. Both PROCESS.md lifecycles list
// IN_REVIEW -> IN_PROGRESS; PROCESS.md names a human's not-yet at completion
// review as a return that keeps the plan, the implementation approval and the
// ownership; rdd-completion-review records that answer with the unchanged
// retained presentation, reads the return back, continues the build and renews
// whatever changed before submitting again; its .claude/skills copy is
// byte-identical. modernpath install --check matches a workspace's installed
// copies to these files.
func TestSR3835_2InstalledProcessNamesTheReturn(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	process := installedKitFile(t, root, ".modernpath", "rdd", "PROCESS.md")
	for label, move := range map[string]string{
		"EPIC:":  "IN_REVIEW -> IN_PROGRESS (a human's not-yet at completion review)",
		"UR/SR:": "IN_REVIEW -> IN_PROGRESS (returned with their Epic)",
	} {
		if !strings.Contains(lifecycleLines(string(process), label), move) {
			t.Errorf("PROCESS.md's %s lifecycle does not list %q", label, move)
		}
	}
	canonical := installedKitFile(t, root, ".modernpath", "rdd", "skills", "rdd-completion-review", "SKILL.md")
	claude := installedKitFile(t, root, ".claude", "skills", "rdd-completion-review", "SKILL.md")
	if !bytes.Equal(canonical, claude) {
		t.Error("rdd-completion-review: the .claude/skills copy differs from the .modernpath/rdd copy")
	}
	for _, file := range []struct {
		name    string
		body    []byte
		clauses []string
	}{
		{"PROCESS.md", process, []string{
			"A human's not-yet at completion review returns the Epic and its owned requirements to `IN_PROGRESS`",
			"with the plan, the implementation approval and the ownership kept",
			"record it with the presentation the human declined",
			"continue the build and submit for review again",
		}},
		{"rdd-completion-review", canonical, []string{
			"If the human answers not yet, record that answer with the unchanged retained presentation as a return to implementation through the CLI.",
			"Read back the Epic and its owned requirements IN_PROGRESS with the plan, the implementation approval and the ownership unchanged",
			"continue the build, and renew whatever changed before submitting for review again",
		}},
	} {
		text := kitWhitespaceNormalised(file.body)
		for _, clause := range file.clauses {
			if !strings.Contains(text, clause) {
				t.Errorf("the installed %s does not say %q", file.name, clause)
			}
		}
	}
}

// lifecycleLines returns one lifecycle of the canonical model: the line that
// starts with the label and the indented lines that continue it.
func lifecycleLines(process, label string) string {
	var lines []string
	for _, line := range strings.Split(process, "\n") {
		if len(lines) == 0 && !strings.HasPrefix(line, label) {
			continue
		}
		if len(lines) > 0 && !strings.HasPrefix(line, " ") {
			break
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
