package cmd

import (
	"bytes"
	"os"
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
		"epics plan submit-review", "epics plan approve-completion", "epics plan history",
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

// renderedLoop is the loop section the reference renders before the
// command tree.
func renderedLoop() string {
	ref := renderCLIReference(rootCmd)
	return ref[:strings.Index(ref, "## Global flags")]
}

// lifecycleEntry returns the lifecycle block's line that starts with prefix and
// the indented lines that continue it.
func lifecycleEntry(loop, prefix string) string {
	var entry []string
	for _, line := range strings.Split(loop, "\n") {
		if len(entry) == 0 && !strings.HasPrefix(line, prefix) {
			continue
		}
		if len(entry) > 0 && !strings.HasPrefix(line, " ") {
			break
		}
		entry = append(entry, line)
	}
	return strings.Join(entry, "\n")
}

// glossaryRow returns the refusal glossary row keyed on the given text.
func glossaryRow(t *testing.T, loop, key string) string {
	t.Helper()
	for _, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, "| `"+key+"`") {
			return line
		}
	}
	t.Fatalf("the refusal glossary has no row keyed %q", key)
	return ""
}

// UR-270.3: the glossary names both trace refusals, each with its cause and
// what clears it.
func TestUR270_3GlossaryNamesTheTraceRefusals(t *testing.T) {
	loop := renderedLoop()
	for _, key := range []string{"trace requirement outside plan", "trace criterion has other epic"} {
		cells := strings.Split(strings.Trim(glossaryRow(t, loop, key), "| "), " | ")
		if len(cells) != 4 || cells[1] != "server 422" || cells[2] == "" || cells[3] == "" {
			t.Errorf("the %q row must give origin server 422, the cause and what clears it: %q", key, cells)
		}
	}
}

// UR-271.5: the lifecycle block and the rdd-completion-review row name the
// return as the recorded not-yet step out of IN_REVIEW.
func TestUR271_5LifecycleAndCompletionRowNameTheReturn(t *testing.T) {
	loop := renderedLoop()
	var lifecycle string
	for _, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, "plan return-to-implementation ") {
			lifecycle = line
		}
	}
	if !strings.Contains(lifecycle, "IN_PROGRESS") || !strings.Contains(lifecycle, "not yet") {
		t.Errorf("the lifecycle block has no plan return-to-implementation line for the not-yet: %q", lifecycle)
	}
	row := phaseRow(t, loop, "| Review, publish and complete |")
	if !strings.Contains(row, "`epics plan return-to-implementation") || !strings.Contains(row, "not yet") {
		t.Errorf("the rdd-completion-review row does not name the return as the not-yet step: %s", row)
	}
}

// SR-1636.2: epics plan --help states the trace rule and that a reactivated
// stale trace counts as a change.
func TestSR1636_2PlanHelpStatesTheTraceRule(t *testing.T) {
	out, err := runRoot(t, "epics", "plan", "--help")
	help := strings.Join(strings.Fields(out), " ")
	if err != nil || !strings.Contains(help, "An Epic's plan is one JSON object") {
		t.Fatalf("epics plan --help printed no plan help (err %v): %q", err, out)
	}
	for _, want := range []string{
		"Adding, changing or removing a trace needs every touched requirement in the plan",
		"A reactivated stale trace counts as a change",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("epics plan --help does not state %q", want)
		}
	}
}

// The messages the CLI prints for the structured plan refusals (the server's
// error.message), which their glossary rows are keyed on.
const (
	reasonRequiredMessage      = "Give the human's answer as a non-empty reason, such as a USER: source."
	invalidActorMessage        = "Name an agent with a non-empty agent slug, or send no actor so the signed-in person is the actor."
	presentationRequiredPrefix = "Present the exact completion bundle the human answered"
	presentationChangedPrefix  = "The completion bundle changed after the human saw it"
)

// SR-1636.1: the glossary names the two trace refusals by their bare text and
// the reason, actor and return-bundle refusals by the message the CLI prints,
// each with its origin, error code, cause and what clears it.
func TestSR1636_1GlossaryKeysTheRefusalsOnTheirPrintedText(t *testing.T) {
	loop := renderedLoop()
	for _, want := range []struct{ key, origin, code string }{
		{"trace requirement outside plan", "server 422", ""},
		{"trace criterion has other epic", "server 422", ""},
		{reasonRequiredMessage, "server 422", "`reason_required`"},
		{invalidActorMessage, "server 422", "`invalid_actor`"},
		{presentationRequiredPrefix, "server 422", "`return_presentation_required`"},
		{presentationChangedPrefix, "server 409", "`return_presentation_changed`"},
	} {
		cells := strings.Split(strings.Trim(glossaryRow(t, loop, want.key), "| "), " | ")
		if len(cells) != 4 || !strings.HasPrefix(cells[1], want.origin) || !strings.Contains(cells[1], want.code) || cells[2] == "" || cells[3] == "" {
			t.Errorf("the %q row must give origin %s, error code %s, the cause and what clears it: %q", want.key, want.origin, want.code, cells)
		}
	}
}

// SR-1636.3: the wrong-state row is keyed on the stable part of every printed
// lifecycle message and lists each plan write with the state it needs.
func TestSR1636_3WrongStateRowIsKeyedOnThePrintedLifecycleMessage(t *testing.T) {
	row := glossaryRow(t, renderedLoop(), "; the Epic is")
	for _, want := range []string{
		"`invalid_lifecycle_state`", "server 409",
		"`save` and `approve-implementation` need PROPOSED", "`submit-review` IN_PROGRESS",
		"`return-to-implementation` and `approve-completion` IN_REVIEW",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the wrong-state row does not say %q: %s", want, row)
		}
	}
}

// SR-1638.3: the reference, both CLI guides and the return help show the return
// with its completion bundle, after epics gates presentation, and name its
// refusals by their printed messages.
func TestSR1638_3TheReturnShowsItsBundleWhereverItIsDocumented(t *testing.T) {
	loop := renderedLoop()
	presentation, ret := -1, -1
	for i, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, "epics gates presentation ") {
			presentation = i
		}
		if strings.HasPrefix(line, "plan return-to-implementation --file <presentation.json> ") {
			ret = i
		}
	}
	if presentation < 0 || ret < 0 || presentation > ret {
		t.Errorf("the lifecycle block must show epics gates presentation (line %d) before plan return-to-implementation --file <presentation.json> (line %d)", presentation, ret)
	}
	row := phaseRow(t, loop, "| Review, publish and complete |")
	at, retAt := strings.Index(row, "`epics gates presentation <id> --json`"), strings.Index(row, "`epics plan return-to-implementation <id> --file <presentation.json>")
	if at < 0 || retAt < 0 || at > retAt {
		t.Errorf("the rdd-completion-review row must show the return with --file <presentation.json> after epics gates presentation: %s", row)
	}
	if !strings.Contains(glossaryRow(t, loop, "; the Epic is"), "`return-to-implementation`") {
		t.Error("the wrong-state row does not name return-to-implementation")
	}
	kit, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	// The image's Go test stage copies only tools/modernpath, so docs/cli.md is
	// compared wherever the workspace has it.
	if guide, err := os.ReadFile("../../../docs/cli.md"); err == nil {
		if !bytes.Equal(guide, kit) {
			t.Error("the kit's CLI guide differs from docs/cli.md")
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if !strings.Contains(string(kit), "modernpath epics plan return-to-implementation <epic-id> --file ") {
		t.Error("the CLI guide does not show the return with --file")
	}
	guide := strings.Join(strings.Fields(string(kit)), " ")
	for _, place := range []struct{ name, text string }{
		{"the lifecycle block's return entry", lifecycleEntry(loop, "plan return-to-implementation --file <presentation.json> ")},
		{"the rdd-completion-review row", row},
		{"the CLI guide", guide},
	} {
		for _, message := range []string{reasonRequiredMessage, presentationRequiredPrefix, presentationChangedPrefix} {
			if !strings.Contains(place.text, message) {
				t.Errorf("%s does not name the refusal %q", place.name, message)
			}
		}
	}
	for name, text := range map[string]string{"the CLI guide": guide, "the rdd-completion-review row": row} {
		if !strings.Contains(text, invalidActorMessage) {
			t.Errorf("%s does not name the refusal of a blank --agent slug %q", name, invalidActorMessage)
		}
	}
	help, err := runRoot(t, "epics", "plan", "return-to-implementation", "--help")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(help), " ")
	for _, want := range []string{"--file string", "--reason string", reasonRequiredMessage, presentationRequiredPrefix, presentationChangedPrefix} {
		if !strings.Contains(text, want) {
			t.Errorf("epics plan return-to-implementation --help does not show %q", want)
		}
	}
	if c, _, err := rootCmd.Find([]string{"epics", "plan", "return-to-implementation"}); err != nil || c.Name() != "return-to-implementation" {
		t.Error("the binary has no epics plan return-to-implementation")
	}
}

// SR-1638.4: the CHANGELOG section that lists the return with --file names its
// refusals by their printed messages and the server change. A release cut
// renames Unreleased to the release, so the section is found by the entry.
func TestSR1638_4ChangelogListsTheReturnWithItsBundle(t *testing.T) {
	data, err := os.ReadFile("../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	entry := "`epics plan return-to-implementation <id> --file <presentation.json>"
	var text string
	for _, section := range strings.Split(string(data), "\n## ") {
		if section = strings.Join(strings.Fields(section), " "); strings.Contains(section, entry) {
			text = section
			break
		}
	}
	if text == "" {
		t.Fatalf("no CHANGELOG section says %q", entry)
	}
	for _, want := range []string{
		reasonRequiredMessage, invalidActorMessage, presentationRequiredPrefix, presentationChangedPrefix,
		"Requires the matching server changes",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the CHANGELOG section listing the return does not say %q", want)
		}
	}
}
