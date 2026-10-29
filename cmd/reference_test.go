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
		"requirements create", "requirements update", "requirements get", "requirements obsolete", "requirements set-context",
		"reverse-engineer record", "reverse-engineer check", "reverse-engineer review", "reverse-engineer accept",
		"evidence record", "evidence latest", "traces list",
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
			`"requirements"`, "current",
		},
		"modernpath reverse-engineer accept":           {`"requirements"`, `"source"`, `"reason"`, "current"},
		"modernpath epics plan":                        {`"body"`, `"requirements"`, `"traces"`, "current", "ownership"},
		"modernpath epics plan approve-implementation": {"USER:", "IN_PROGRESS"},
		"modernpath epics plan approve-completion":     {"USER:", "DONE"},
		"modernpath evidence record": {
			"test_case_id", "criterion", "result",
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

func TestCLIReferenceOmitsRepositoryInternals(t *testing.T) {
	reference := renderCLIReference(rootCmd)
	for _, internal := range []string{
		"ModernPath/modernpath-v1", "req-driven-dev", "modernpath-core/",
		"tools/modernpath/", "internal/kit/",
	} {
		if strings.Contains(reference, internal) {
			t.Errorf("customer command reference exposes repository internals: %q", internal)
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

// SR-1636.2: epics plan --help states the trace rule and stale trace behavior.
func TestSR1636_2PlanHelpStatesTheTraceRule(t *testing.T) {
	out, err := runRoot(t, "epics", "plan", "--help")
	help := strings.Join(strings.Fields(out), " ")
	if err != nil || !strings.Contains(help, "An Epic's plan is one JSON object") {
		t.Fatalf("epics plan --help printed no plan help (err %v): %q", err, out)
	}
	for _, want := range []string{
		"Adding, changing or removing a trace needs every touched requirement in the plan",
		"Listing a stale trace does not implicitly revive it",
		"remove_traces names undeleted active or stale traces",
		"Deleted, unknown, foreign-System and out-of-plan IDs are refused",
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
func TestReturnUsesCurrentStateWithoutAFile(t *testing.T) {
	loop := renderedLoop()
	row := phaseRow(t, loop, "| Review, publish and complete |")
	for _, want := range []string{"epics plan return-to-implementation <id> --reason", "current", "readiness", "ownership"} {
		if !strings.Contains(row, want) {
			t.Errorf("completion row omits %s", want)
		}
	}
	kit, err := os.ReadFile("../internal/kit/assets/cli-guide/cli.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kit), "return-to-implementation") || strings.Contains(string(kit), "--file <presentation.json>") {
		t.Fatal("guide has obsolete bundle upload")
	}
	help, err := runRoot(t, "epics", "plan", "return-to-implementation", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(help, "--reason string") || strings.Contains(help, "--file string") {
		t.Fatal(help)
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

// Epic 329 (A6), test case bd44c890, criterion a7fba45b: the reference's
// completion row closes only backlog, gap and tooling records by the Epic and
// never an assumed default; its triage row gives the routing template only for
// a backlog, gap or tooling record and for an assumed default's first routing,
// and says moving one that is already routed needs the person's own words; the
// help of `backlog update` says where the USER: source goes for each
// disposition and that after its first routing an assumed default's
// disposition_ref changes only to a USER: source. The phrases are read with
// a6Plain, so they may wrap or carry Markdown code marks.

// a6Plain folds whitespace, drops Markdown code and emphasis marks, makes
// typographic quotes plain and lowers letters, as the kit's phrase tests do.
func a6Plain(text string) string {
	plain := strings.NewReplacer("`", "", "*", "", "’", "'").Replace(text)
	return strings.ToLower(strings.Join(strings.Fields(plain), " "))
}

func TestA6BD44C890ReferenceCompletionRowClosesNoAssumedDefault(t *testing.T) {
	row := a6Plain(phaseRow(t, renderedLoop(), "| Review, publish and complete |"))
	for _, want := range []string{
		"for each backlog, gap or tooling record routed to the epic",
		"an assumed default is never closed by the epic",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the completion row does not say %q: %s", want, row)
		}
	}
}

func TestA6BD44C890ReferenceTriageRowLimitsTheRoutingTemplate(t *testing.T) {
	row := a6Plain(phaseRow(t, renderedLoop(), "| Discoveries and blockers |"))
	for _, want := range []string{
		"only for a backlog, gap or tooling record and for an assumed default's first routing",
		"moving an assumed default that is already routed needs the person's own words",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("the triage row does not say %q: %s", want, row)
		}
	}
}

func TestA6BD44C890BacklogUpdateHelpPlacesTheUserSource(t *testing.T) {
	cmd := newBacklogCommand(func() (*cliEnv, error) { t.Fatal("help loaded the workspace"); return nil, nil })
	cmd.SetArgs([]string{"update", "--help"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	help := a6Plain(out.String())
	if !strings.Contains(help, "update a backlog record from a json object") {
		t.Fatalf("backlog update --help printed no update help: %q", help)
	}
	for _, want := range []string{
		"every change of an assumed default's disposition but its first routing needs a user: source",
		"after with for accepted with and rejected with",
		"in the same file's disposition_ref for closed by, deferred, open and routed to",
		"after its first routing an assumed default's disposition_ref changes only to a user: source",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("the help of backlog update does not say %q: %s", want, help)
		}
	}
}

// g1ContextRefusals are the refusals of a context that disagrees with the
// System's list of contexts (Epic 340, G1).
var g1ContextRefusals = []string{"context_required", "context_name_mismatch", "context_code_conflict", "context_name_conflict", "invalid_context"}

// g1GlossaryCells returns the four cells of the glossary row keyed on key, or
// nil, without stopping the test, so every missing row is reported.
func g1GlossaryCells(loop, key string) []string {
	for _, line := range strings.Split(loop, "\n") {
		if strings.HasPrefix(line, "| `"+key+"`") {
			return strings.Split(strings.Trim(line, "| "), " | ")
		}
	}
	return nil
}

func g1Folded(text string) string { return strings.Join(strings.Fields(text), " ") }

// Epic 340 (G1), test case 94c50400: the Record existing behavior row of the
// pass-to-command map names requirements contexts. The check runs this test by
// its name.
func TestG1ListRecordRowNamesRequirementsContexts(t *testing.T) {
	row := phaseRow(t, renderedLoop(), "| Record existing behavior |")
	if !strings.Contains(row, "`requirements contexts") {
		t.Errorf("the Record existing behavior row does not name `requirements contexts`: %s", row)
	}
}

// Epic 340 (G1), test case e62a7b48: the refusal glossary has a row keyed on
// each context refusal, with the origin server 422, a cause that names
// reverse-engineer record and what clears it; and the section on
// reverse-engineer record says that every new requirement needs a context
// from the System's list and names requirements contexts. The check runs this
// test by its name.
func TestG1RecordingReferenceNeedsAContextFromTheList(t *testing.T) {
	loop := renderedLoop()
	for _, key := range g1ContextRefusals {
		cells := g1GlossaryCells(loop, key)
		if cells == nil {
			t.Errorf("the refusal glossary has no row keyed %q", key)
			continue
		}
		if len(cells) != 4 || cells[1] != "server 422" || !strings.Contains(cells[2], "reverse-engineer record") || cells[3] == "" {
			t.Errorf("the %q row must give origin server 422, a cause naming reverse-engineer record and what clears it: %q", key, cells)
		}
	}
	section := g1Folded(referenceSection(t, renderCLIReference(rootCmd), "modernpath reverse-engineer record"))
	if !strings.Contains(strings.ToLower(section), "every new requirement needs a context from the system's list") {
		t.Error("the reverse-engineer record section does not say that every new requirement needs a context from the System's list")
	}
	if !strings.Contains(section, "requirements contexts") {
		t.Error("the reverse-engineer record section does not name requirements contexts")
	}
}

// Epic 340 (G1), test case b71f8e62: the rendered sections of requirements
// set-context and requirements update say that a requirement takes the name
// the System's list gives a listed context, no longer that a missing name
// defaults to the code; and the glossary rows of the refusals regrouping shares
// with recording name both commands. The check runs this test by its name.
func TestG1RegroupReferenceGivesTheListsNameAndRefusals(t *testing.T) {
	ref := renderCLIReference(rootCmd)
	for path, stale := range map[string]string{
		"modernpath requirements set-context": "defaults to --context",
		"modernpath requirements update":      "context_name defaults to context",
	} {
		section := g1Folded(referenceSection(t, ref, path))
		if !strings.Contains(section, "takes the name the System's list gives that context") {
			t.Errorf("the %s section does not say that a requirement takes the name the System's list gives that context", path)
		}
		if strings.Contains(section, stale) {
			t.Errorf("the %s section still says %q", path, stale)
		}
	}
	loop := renderedLoop()
	for _, key := range []string{"context_code_conflict", "context_name_conflict", "invalid_context"} {
		cells := g1GlossaryCells(loop, key)
		if len(cells) != 4 || !strings.Contains(cells[2], "requirements set-context") || !strings.Contains(cells[2], "requirements update") {
			t.Errorf("the %q row's cause must name requirements set-context and requirements update: %q", key, cells)
		}
	}
}
