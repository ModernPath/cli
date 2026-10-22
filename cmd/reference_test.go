package cmd

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func flagNames(c *cobra.Command) []string {
	var names []string
	collect := func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			names = append(names, f.Name)
		}
	}
	c.LocalNonPersistentFlags().VisitAll(collect)
	localPersistentFlags(c).VisitAll(collect)
	return names
}

func findCommand(t *testing.T, root *cobra.Command, path []string) *cobra.Command {
	t.Helper()
	c, _, err := root.Find(path)
	if err != nil {
		t.Fatalf("command %v: %v", path, err)
	}
	return c
}

// The installed CLI reference is rendered from the binary's own command tree
// so it cannot drift from the build that wrote it (tooling self-sufficiency
// plan C4, USER:2026-09-12). These checks pin what makes it usable as a
// lookup: every visible command is there with its flags, the loop commands carry the
// prerequisites their long help was given (plan B1–B7), and the rendering is
// deterministic so `install --check` compares bytes, not luck.
// availableCommands walks the tree by cobra's own notion of availability,
// independently of the renderer's visibleCommands, so a command the renderer
// drops fails here instead of vanishing from both sides.
func availableCommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, s := range c.Commands() {
		if !s.IsAvailableCommand() {
			continue
		}
		out = append(out, s)
		out = append(out, availableCommands(s)...)
	}
	return out
}

func TestCLIReferenceRendersEveryVisibleCommandAndFlag(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	available := availableCommands(rootCmd)
	if len(available) < 40 {
		t.Fatalf("only %d available commands walked — the walk is broken", len(available))
	}
	for _, c := range available {
		if !strings.Contains(ref, "## `"+c.CommandPath()+"`") {
			t.Errorf("reference has no section for %q", c.CommandPath())
		}
		for _, name := range flagNames(c) {
			if !strings.Contains(ref, "`--"+name+"`") && !strings.Contains(ref, ", --"+name+"`") {
				t.Errorf("reference never lists flag --%s of %q", name, c.CommandPath())
			}
		}
	}
	for _, want := range []string{
		"## `modernpath requirements create`",
		"## `modernpath epics plan show`",
		"## `modernpath epics plan save`",
		"## `modernpath epics plan approve-implementation`",
		"## `modernpath epics plan approve-completion`",
		"## `modernpath reverse-engineer record`",
		"## `modernpath reverse-engineer review`",
		"## `modernpath reverse-engineer accept`",
		"## Global flags",
		"`--api-url`",
	} {
		if !strings.Contains(ref, want) {
			t.Errorf("reference is missing %q", want)
		}
	}
	if strings.Contains(ref, "modernpath help") || strings.Contains(ref, "modernpath completion") {
		t.Error("reference lists cobra's own help or completion commands")
	}
	if strings.Contains(ref, "| `-h, --help`") {
		t.Error("reference lists cobra's -h flag, which appears only under the executing command")
	}
}

// referenceSection returns just the section the reference devotes to one
// command path, so a flag can be asserted where a reader would look for it
// rather than anywhere in the document.
func referenceSection(t *testing.T, ref, path string) string {
	t.Helper()
	head := "## `" + path + "`\n"
	i := strings.Index(ref, head)
	if i < 0 {
		t.Fatalf("reference has no section for %q", path)
	}
	rest := ref[i+len(head):]
	if j := strings.Index(rest, "\n## "); j >= 0 {
		return rest[:j]
	}
	return rest
}

// Exercise inherited non-root flags independently of the retired process groups.
func TestCLIReferenceListsInheritedGroupFlagsUnderEachSubcommand(t *testing.T) {
	root := &cobra.Command{Use: "example"}
	root.PersistentFlags().String("api-url", "", "Server URL")
	group := &cobra.Command{Use: "documents"}
	group.PersistentFlags().String("format", "text", "Output format")
	leaf := &cobra.Command{Use: "read", Run: func(*cobra.Command, []string) {}}
	group.AddCommand(leaf)
	root.AddCommand(group)
	section := referenceSection(t, renderCLIReference(root), "example documents read")
	if !strings.Contains(section, "`--format`") || !strings.Contains(section, "Inherited from `example documents`") {
		t.Fatal("child section omits the inherited group flag or its owner")
	}
	if strings.Contains(section, "`--api-url`") {
		t.Fatal("root global flags are repeated under a child command")
	}
}

// The reference is the document the installed rdd skills call "the current
// CLI reference" and the only place an agent can read how a phase maps onto
// this binary's commands and what each input file holds. The retired
// mp-process-cli skill carried that; PR #936 dropped it without moving it
// here, and a reverse-engineering session then found `record --help` naming
// no field at all. These checks pin the loop section and the file contracts
// so a future command change cannot leave the how behind again.
func TestCLIReferenceCarriesTheLoopAndTheFileContracts(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	loop := ref[:strings.Index(ref, "## Global flags")]
	for _, want := range []string{
		"## Operating the delivery loop",
		"### Pass to command",
		"### Refusals and what clears them",
		"`rdd-start`", "`rdd-plan`", "`rdd-entry-review`", "`rdd-build`", "`rdd-verify`",
		"`rdd-completion-review`", "`rdd-reverse-engineer`", "`rdd-reverse-engineer-verify`",
		"`rdd-reverse-engineer-accept`", "`rdd-triage`",
		"One store command per shell call",
		"tooling gap",
		// Epic 288 (B5): the nested-workspace refusal is in the glossary.
		"below an already bound workspace",
	} {
		if !strings.Contains(loop, want) {
			t.Errorf("the loop section before the command tree is missing %q", want)
		}
	}
	// Every command the phase table names must exist, or the table sends an
	// agent to a command the binary does not have.
	for _, path := range []string{
		"epics plan save", "epics plan show", "epics plan approve-implementation",
		"epics plan submit-review", "epics plan approve-completion",
		"requirements create", "requirements get", "requirements obsolete", "requirements set-context",
		"reverse-engineer record", "reverse-engineer review", "reverse-engineer accept",
		"evidence record", "evidence latest", "evidence runs", "traces list",
		"test-cases create", "backlog create", "backlog update", "epics create", "epics update",
	} {
		if !strings.Contains(loop, "`"+path) && !strings.Contains(loop, "`"+strings.TrimPrefix(path, "epics plan ")) {
			t.Errorf("the phase table never names %q", path)
		}
		if _, _, err := rootCmd.Find(strings.Fields(path)); err != nil {
			t.Errorf("the loop section names %q, which this binary lacks: %v", path, err)
		}
	}
	for section, keys := range map[string][]string{
		"modernpath reverse-engineer record": {
			"modernpath reverse-engineer schema", "server JSON Schema", "existing_requirement_conflict",
		},
		"modernpath reverse-engineer review": {
			`{"requirements": [{"kind"`, "missing_criterion_evidence", "test_identity_required",
			"evidence_predates_current_content", "requirement_owned",
		},
		"modernpath reverse-engineer accept":           {`{"review":`, `"source"`, `"reason"`, "review_changed", "already_accepted"},
		"modernpath epics plan":                        {`"body"`, `"requirements"`, `"traces"`, "incomplete reviewed content", "change_epic_id"},
		"modernpath epics plan approve-implementation": {"USER:", "IN_PROGRESS"},
		"modernpath epics plan approve-completion":     {"USER:", "DONE"},
		"modernpath evidence record": {
			"ci | local_test | browser_verification | manual | migration",
			"pass | fail | skip | error | inconclusive", "RED", "criterion", "test_case_ref", "requirement_kind",
		},
		"modernpath traces list": {"implements, verifies, derives or relates"},
	} {
		body := referenceSection(t, ref, section)
		for _, key := range keys {
			if !strings.Contains(body, key) {
				t.Errorf("section %q does not document %q", section, key)
			}
		}
	}
}

func TestCLIReferenceIsDeterministic(t *testing.T) {
	a := renderCLIReference(rootCmd)
	b := renderCLIReference(rootCmd)
	if a != b {
		t.Fatal("two renders of the same tree differ — install --check would report drift on every run")
	}
	for _, stamp := range []string{"Auto generated by", "20"} {
		if strings.Contains(a, "###### "+stamp) {
			t.Fatalf("reference carries a timestamp-like footer %q", stamp)
		}
	}
}

// phaseRow returns the one phase-map row that starts with the given cell, so a
// command can be asserted in the phase an agent reads it under.
func phaseRow(t *testing.T, loop, prefix string) string {
	t.Helper()
	for _, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("the phase map has no row starting %q", prefix)
	return ""
}

// Epic 298 (SR-3816, TC-1401): the phase map names the backlog disposition
// write where the skills ask for it — routing at plan save and in triage,
// closing after completion approval — so the kit's tool-neutral sentences
// resolve to a command here and nowhere else.
func TestCLIReferencePhaseMapNamesTheBacklogDispositionWrite(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	loop := ref[:strings.Index(ref, "## Global flags")]
	for _, want := range []struct{ row, text string }{
		{"| Plan |", "`backlog update"},
		{"| Plan |", "ROUTED to <epic id>"},
		{"| Discoveries and blockers |", "`backlog update"},
		{"| Review, publish and complete |", "`backlog update"},
		{"| Review, publish and complete |", "CLOSED by <epic id>"},
	} {
		if row := phaseRow(t, loop, want.row); !strings.Contains(row, want.text) {
			t.Errorf("the %s row does not carry %q: %s", want.row, want.text, row)
		}
	}
}
