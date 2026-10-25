// Command-tree smoke checks ensure every retained Cobra leaf is reachable,
// documented forms resolve, and each active leaf has an exercised or explicit
// local/precondition disposition.
package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestEveryAdvertisedLeafResolves(t *testing.T) {
	paths := [][]string{
		{"ask"}, {"auth", "status"}, {"context"},
		{"datamodel", "export"},
		{"dev", "list"}, {"dev", "setup"}, {"dev", "run"},
		{"docs", "sync"}, {"docs", "generate"}, {"docs", "refresh"}, {"docs", "preview"},
		{"docs", "cleanup"}, {"docs", "push"},
		{"analysis", "start"}, {"analysis", "reanalyze"}, {"analysis", "reset"}, {"analysis", "status"},
		{"epics", "search"}, {"epics", "plan", "show"}, {"epics", "plan", "save"}, {"epics", "plan", "history"},
		{"epics", "plan", "approve-implementation"}, {"epics", "plan", "submit-review"}, {"epics", "plan", "approve-completion"},
		{"epics", "plan", "return-to-implementation"},
		{"focus"},
		{"github"},
		{"hooks", "install"}, {"hooks", "uninstall"}, {"hooks", "status"}, {"hooks", "doctor"},
		{"import"}, {"init"}, {"init", "workspace"}, {"install"}, {"new"},
		{"source", "push"},
		{"read-doc"}, {"read-file"}, {"scan"}, {"search"}, {"status"}, {"prepare"},
		{"system-docs", "push"}, {"system-docs", "pull"}, {"system-docs", "list"}, {"system-docs", "get"},
		{"reverse-engineer", "record"},
		{"env", "list"}, {"env", "test"},
		{"dev", "review"}, {"epics", "list"}, {"epics", "get"}, {"epics", "create"},
		{"epics", "specs", "generate"}, {"epics", "specs", "status"}, {"epics", "specs", "list"}, {"epics", "specs", "pull"}, {"epics", "specs", "push"},
		{"requirements", "get"}, {"requirements", "create"}, {"requirements", "update"}, {"requirements", "obsolete"}, {"requirements", "set-context"}, {"traces", "list"}, {"test-cases", "create"}, {"test-cases", "list"}, {"test-cases", "get"}, {"test-cases", "update"},
		{"evidence", "record"}, {"evidence", "runs"}, {"evidence", "latest"},
	}

	for _, path := range paths {
		cmd, _, err := rootCmd.Find(path)
		if err != nil {
			t.Errorf("%v does not resolve: %v", path, err)
			continue
		}
		want := path[len(path)-1]
		if cmd.Name() != want {
			t.Errorf("%v resolved to %q, want %q", path, cmd.Name(), want)
		}
	}
}

func TestAuthRejectsPositionalArgs(t *testing.T) {
	// `modernpath auth logout` silently ignored the positional and STARTED A
	// LOGIN (D22). The real form is --logout; positionals must be errors.
	auth, _, err := rootCmd.Find([]string{"auth"})
	if err != nil {
		t.Fatalf("auth does not resolve: %v", err)
	}
	if auth.Args == nil {
		t.Fatal("auth has no Args validator — positionals are silently ignored")
	}
	if err := auth.Args(auth, []string{"logout"}); err == nil {
		t.Fatal("auth accepted a positional arg; 'auth logout' must be an error naming --logout")
	}
}

func TestDevAdvertisesTheRealForm(t *testing.T) {
	// The dev group's own help advertised `modernpath dev <tool> [task]`,
	// a form with no RunE that prints help and exits 0 (D5). The launchable
	// form is `dev run <tool> [task]`.
	dev, _, err := rootCmd.Find([]string{"dev"})
	if err != nil {
		t.Fatalf("dev does not resolve: %v", err)
	}
	for _, text := range []string{dev.Long, dev.Short} {
		if strings.Contains(text, "dev <tool>") || strings.Contains(text, "dev [tool]") {
			t.Fatalf("dev help still advertises the dead 'dev <tool>' form:\n%s", text)
		}
	}
}

// advertisedSequences scans every command's own help text for "modernpath
// <tokens>" references and returns them with their source, so dead advice is
// caught wherever it is written — the hardcoded leaf list above is a floor,
// this is the sweep (five dead advertisements once survived the
// string-targeted test).
var advertisedRe = regexp.MustCompile(`(^|.)modernpath ((?:[a-z][a-z0-9-]*)(?: [a-z][a-z0-9-]*)*)`)

// Prose about "the modernpath binary" is not a command advertisement. A false
// positive here costs a ten-second stopword addition — the failure message
// names the source command.
var advertisedStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "on": true, "in": true, "to": true,
	"with": true, "and": true, "or": true, "is": true, "it": true,
	"directory": true, "binary": true, "command": true, "cli": true,
	"found": true, "check`": true,
}

func advertisedSequences() map[string][]string {
	found := map[string][]string{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, text := range []string{c.Short, c.Long, c.Example} {
			for _, m := range advertisedRe.FindAllStringSubmatch(text, -1) {
				// ".modernpath …" is the config directory, not the binary.
				if m[1] == "." || m[1] == "-" {
					continue
				}
				first := strings.Fields(m[2])[0]
				if advertisedStopwords[first] {
					continue
				}
				found[m[2]] = append(found[m[2]], c.CommandPath())
			}
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	return found
}

func TestEveryAdvertisedSequenceResolves(t *testing.T) {
	for seq, sources := range advertisedSequences() {
		tokens := strings.Fields(seq)

		// The first token must be a real root command.
		cmd, _, _ := rootCmd.Find(tokens[:1])
		if cmd == rootCmd {
			t.Errorf("help text of %v advertises 'modernpath %s' — %q is not a command", sources, seq, tokens[0])
			continue
		}

		// Walk deeper while the resolved command is a GROUP (no RunE): for a
		// group, the next advertised token must be one of its subcommands —
		// otherwise the advice prints help and exits 0 (the D5 shape). Once a
		// command has RunE, further tokens are positional args and are fine.
		for i := 1; i < len(tokens); i++ {
			if cmd.RunE != nil || cmd.Run != nil {
				break
			}
			next, _, _ := rootCmd.Find(tokens[:i+1])
			if next == cmd {
				t.Errorf("help text of %v advertises 'modernpath %s' — %q is a group and %q is not among its subcommands",
					sources, seq, cmd.Name(), tokens[i])
				break
			}
			cmd = next
		}
	}
}

func TestGroupsRejectUnknownSubcommands(t *testing.T) {
	// Pure groups = subcommands, no handler of their own; commands with a
	// real RunE treat extra tokens as positional args and are not the D5
	// shape. The guard mutates the shared tree, so a group already guarded
	// by an earlier test still counts — without that, this test passes or
	// fails on test ORDER rather than on the tree (found under -shuffle,
	// RUN:2026-08-19).
	var pure []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		guarded := c.Annotations[groupGuardAnnotation] == "1"
		if len(c.Commands()) > 0 && c != rootCmd && (guarded || (c.Run == nil && c.RunE == nil)) {
			pure = append(pure, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	if len(pure) == 0 {
		t.Fatal("no pure groups found — the walk is broken")
	}

	applyGroupUnknownArgGuard(rootCmd)

	for _, g := range pure {
		if g.RunE == nil {
			t.Errorf("group %q has no handler after the guard", g.CommandPath())
			continue
		}
		if err := g.RunE(g, []string{"definitely-not-a-subcommand"}); err == nil {
			t.Errorf("group %q accepts an unknown token silently (help + exit 0)", g.CommandPath())
		}
	}
}

// TestEveryLeafDispatchesThroughExecute drives rootCmd.Execute with SetArgs
// for every leaf in help mode (SR REQ-CROSS-212; tightened after the external
// review RUN:2026-08-19 showed Find-only checks skip cobra's real dispatch).
// Help mode still parses the full flag set, so an unregistered advertised
// flag fails here — `--report` is exercised explicitly.
func TestEveryLeafDispatchesThroughExecute(t *testing.T) {
	applyGroupUnknownArgGuard(rootCmd)

	var leaves [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if len(c.Commands()) == 0 {
			leaves = append(leaves, path)
			return
		}
		for _, sub := range c.Commands() {
			walk(sub, append(append([]string{}, path...), sub.Name()))
		}
	}
	walk(rootCmd, nil)

	if len(leaves) < 50 {
		t.Fatalf("leaf walk found only %d leaves — the walk is broken", len(leaves))
	}

	run := func(args []string) error {
		rootCmd.SetArgs(args)
		defer rootCmd.SetArgs(nil)
		return rootCmd.Execute()
	}

	for _, leaf := range leaves {
		// A leaf with no handler is the D5 shape at its root: cobra would
		// print help and exit 0. Removing a leaf's RunE must fail HERE, not
		// pass because --help returned early (external review RUN:2026-08-19).
		cmd, _, err := rootCmd.Find(leaf)
		if err != nil || (cmd.Run == nil && cmd.RunE == nil) {
			t.Errorf("leaf %v has no handler (Run/RunE nil)", leaf)
		}

		args := append(append([]string{}, leaf...), "--help")
		if err := run(args); err != nil {
			t.Errorf("Execute(%v) failed: %v", args, err)
		}
		// Cobra's help flag PERSISTS across Execute calls on the shared tree —
		// without this reset, a later real execution of the same leaf prints
		// help instead of running its handler.
		if f := cmd.Flags().Lookup("help"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
	}

	// Leave no --help set behind for later command executions.
	resetTreeFlags(rootCmd)
}

// resetTreeFlags returns every flag in the tree to its default and clears
// Changed. cobra's tree is package-level and shared across tests, so a flag
// set by one test otherwise silently changes another test's behaviour.
func resetTreeFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		// A slice flag's Set appends once it has been set, and its DefValue is
		// the rendered "[a,b]": Set would add the literal "[]" as a value.
		if sv, ok := f.Value.(pflag.SliceValue); ok {
			def := []string{}
			if inner := strings.Trim(f.DefValue, "[]"); inner != "" {
				def = strings.Split(inner, ",")
			}
			_ = sv.Replace(def)
		} else {
			_ = f.Value.Set(f.DefValue)
		}
		f.Changed = false
	}
	c.Flags().VisitAll(reset)
	c.PersistentFlags().VisitAll(reset)
	for _, sub := range c.Commands() {
		resetTreeFlags(sub)
	}
}

// TestSourceLiteralsAdvertiseLiveCommands sweeps the package sources (and the
// agents generator, whose strings land in generated client AGENTS.md) for
// "modernpath <tokens>" references in string literals and runtime messages —
// cobra metadata scanning alone let five dead advertisements through
// (external review RUN:2026-08-19).
func TestSourceLiteralsAdvertiseLiveCommands(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join("..", "internal", "agents", "generator.go"))

	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range advertisedRe.FindAllStringSubmatch(string(raw), -1) {
			if m[1] == "." || m[1] == "-" {
				continue
			}
			tokens := strings.Fields(m[2])
			if advertisedStopwords[tokens[0]] {
				continue
			}
			cmd, _, _ := rootCmd.Find(tokens[:1])
			if cmd == rootCmd {
				t.Errorf("%s advertises 'modernpath %s' — %q is not a command", f, m[2], tokens[0])
				continue
			}
			for i := 1; i < len(tokens); i++ {
				if cmd.RunE != nil || cmd.Run != nil {
					break
				}
				next, _, _ := rootCmd.Find(tokens[:i+1])
				if next == cmd {
					t.Errorf("%s advertises 'modernpath %s' — %q is a group and %q is not among its subcommands",
						f, m[2], cmd.Name(), tokens[i])
					break
				}
				cmd = next
			}
		}
	}
}

// Tier (b) of SR REQ-CROSS-212 (amended USER:2026-08-19). The first cut
// hard-coded ten commands and could not show they were the whole eligible
// population — `env test` is GET-only, interaction-free, and was simply
// missing (external review RUN:2026-08-19, P1). So the population is now
// DISCOVERED from the tree and every leaf must carry a disposition:
// executed against httptest, or exempt with a reason. A new leaf that
// nobody classifies fails this test.
//
// Two of the three exempt kinds are themselves ASSERTED rather than merely
// claimed: dispNoCall runs the leaf and requires that it reach no server, so
// a leaf that later grows a server call is forced back into dispExecute.
// Only dispUnrun is an unchecked claim, and it is capped.
type dispKind int

const (
	dispExecute dispKind = iota // runs; the server MUST receive a request
	dispNoCall                  // runs; the server must receive NOTHING (asserted exemption)
	dispUnrun                   // never run: interactive, spawns an agent, or blocks
)

type disposition struct {
	kind   dispKind
	args   []string // argv; nil means "the leaf path alone"
	reason string   // required for dispNoCall and dispUnrun
}

// Closed vocabulary — an exemption must name one of these, so "exempt because"
// cannot become a free-text escape hatch.
var exemptReasons = map[string]bool{
	"local-only":         true, // makes no server call by design
	"needs-precondition": true, // server-backed, but bails before the call without state the smoke cannot cheaply build
	"interactive":        true, // prompts on stdin or opens a browser
	"spawns-agent":       true, // launches an external agent process
	"blocking":           true, // daemon loop; ends only on a signal
}

// The executed floor keeps coverage meaningful as the command surface changes;
// unrun exemptions remain capped.
const (
	minExecutedLeaves = 25
	maxUnrunLeaves    = 6
)

// Each current leaf is classified against the retained command tree below.
var leafDispositions = map[string]disposition{
	"releases list":              {kind: dispExecute},
	"releases members":           {kind: dispExecute, args: []string{"releases", "members", "2", "--all", "--json"}},
	"releases move-requirements": {kind: dispExecute, args: []string{"releases", "move-requirements", "2", "--file", "-", "--json"}},
	"releases activate":          {kind: dispExecute, args: []string{"releases", "activate", "2", "--pin-stdin"}},
	"releases pin status":        {kind: dispExecute},
	"releases pin set":           {kind: dispExecute, args: []string{"releases", "pin", "set", "--pin-stdin"}},
	"factory pin status":         {kind: dispExecute},
	"factory pin set":            {kind: dispExecute, args: []string{"factory", "pin", "set", "--pin-stdin"}},
	"factory release activate":   {kind: dispExecute, args: []string{"factory", "release", "activate", "2", "--pin-stdin"}},

	"backlog create":                    {kind: dispExecute, args: []string{"backlog", "create", "--file", "-", "--json"}},
	"backlog update":                    {kind: dispExecute, args: []string{"backlog", "update", "10000000-0000-4000-8000-000000000001", "--file", "-", "--json"}},
	"backlog list":                      {kind: dispExecute, args: []string{"backlog", "list", "--json"}},
	"backlog get":                       {kind: dispExecute, args: []string{"backlog", "get", "10000000-0000-4000-8000-000000000001", "--json"}},
	"ask":                               {kind: dispExecute, args: []string{"ask", "smoke question"}},
	"auth status":                       {kind: dispExecute},
	"context":                           {kind: dispExecute, args: []string{"context", "smoke query"}},
	"datamodel export":                  {kind: dispExecute, args: []string{"datamodel", "export", "--json"}},
	"dev list":                          {kind: dispNoCall, reason: "local-only"},
	"dev setup":                         {kind: dispNoCall, args: []string{"dev", "setup", "claude"}, reason: "local-only"},
	"dev run":                           {kind: dispUnrun, reason: "spawns-agent"},
	"docs cleanup":                      {kind: dispExecute},
	"docs generate":                     {kind: dispExecute, args: []string{"docs", "generate", "--yes"}},
	"docs preview":                      {kind: dispExecute},
	"docs push":                         {kind: dispExecute},
	"docs refresh":                      {kind: dispExecute},
	"docs sync":                         {kind: dispExecute},
	"env list":                          {kind: dispNoCall, reason: "local-only"},
	"env test":                          {kind: dispExecute},
	"analysis start":                    {kind: dispExecute},
	"analysis reanalyze":                {kind: dispExecute, args: []string{"analysis", "reanalyze", "7"}},
	"analysis reset":                    {kind: dispExecute, args: []string{"analysis", "reset", "--yes"}},
	"analysis status":                   {kind: dispExecute},
	"epics search":                      {kind: dispExecute, args: []string{"epics", "search", "teams", "--json"}},
	"epics gates check":                 {kind: dispExecute, args: []string{"epics", "gates", "check", "42"}},
	"epics gates review-bundle":         {kind: dispExecute, args: []string{"epics", "gates", "review-bundle", "42"}},
	"epics gates presentation":          {kind: dispExecute, args: []string{"epics", "gates", "presentation", "42"}},
	"epics gates candidate":             {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "candidate", "42"}},
	"epics gates start-execution":       {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "start-execution", "42"}},
	"epics gates review":                {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "review", "42"}},
	"epics gates resolve":               {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "resolve", "42"}},
	"epics gates decision":              {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "decision", "42"}},
	"epics gates declare-decision":      {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "declare-decision", "42"}},
	"epics gates recover-legacy":        {kind: dispNoCall, reason: "needs-precondition", args: []string{"epics", "gates", "recover-legacy", "42"}},
	"epics plan show":                   {kind: dispExecute, args: []string{"epics", "plan", "show", "42"}},
	"epics plan history":                {kind: dispExecute, args: []string{"epics", "plan", "history", "42"}},
	"epics plan save":                   {kind: dispNoCall, args: []string{"epics", "plan", "save", "42", "--reason", "smoke"}, reason: "needs-precondition"},
	"epics plan return-to-planning":     {kind: dispExecute, args: []string{"epics", "plan", "return-to-planning", "42", "--reason", "USER: smoke"}},
	"epics plan approve-implementation": {kind: dispExecute, args: []string{"epics", "plan", "approve-implementation", "42", "--reason", "smoke"}},
	"epics plan submit-review":          {kind: dispExecute, args: []string{"epics", "plan", "submit-review", "42", "--reason", "smoke"}},
	"epics plan approve-completion":     {kind: dispExecute, args: []string{"epics", "plan", "approve-completion", "42", "--reason", "smoke"}},
	"focus":                             {kind: dispExecute, args: []string{"focus", "10000000-0000-4000-8000-000000000001"}},
	"github":                            {kind: dispUnrun, reason: "interactive"},
	"hooks doctor":                      {kind: dispNoCall, reason: "local-only"},
	"hooks install":                     {kind: dispNoCall, reason: "local-only"},
	"hooks status":                      {kind: dispNoCall, reason: "local-only"},
	"hooks uninstall":                   {kind: dispNoCall, reason: "local-only"},
	"import":                            {kind: dispExecute},
	"init workspace":                    {kind: dispNoCall, reason: "local-only"},
	"install":                           {kind: dispNoCall, reason: "local-only"},
	"new":                               {kind: dispNoCall, reason: "needs-precondition"},
	"read-doc":                          {kind: dispExecute, args: []string{"read-doc", "--list"}},
	"read-file":                         {kind: dispExecute, args: []string{"read-file", "lib/app.ex"}},
	"requirements list":                 {kind: dispExecute, args: []string{"requirements", "list", "--json"}},
	"requirements search":               {kind: dispExecute, args: []string{"requirements", "search", "smoke", "--json"}},
	"requirements obsolete":             {kind: dispExecute, args: []string{"requirements", "obsolete", "10000000-0000-4000-8000-000000000001", "--kind", "system", "--reason", "smoke"}},
	"requirements set-context":          {kind: dispExecute, args: []string{"requirements", "set-context", "10000000-0000-4000-8000-000000000001", "--kind", "system", "--context", "snake-game"}},
	"reverse-engineer schema":           {kind: dispExecute, args: []string{"reverse-engineer", "schema"}},
	"reverse-engineer record":           {kind: dispExecute, args: []string{"reverse-engineer", "record", "--file", "-"}},
	"reverse-engineer review":           {kind: dispExecute, args: []string{"reverse-engineer", "review", "--file", "-", "--json"}},
	"reverse-engineer accept":           {kind: dispExecute, args: []string{"reverse-engineer", "accept", "--file", "-", "--json"}},
	"scan":                              {kind: dispExecute},
	"search":                            {kind: dispExecute, args: []string{"search", "smoke query"}},
	"source push":                       {kind: dispExecute},
	"status":                            {kind: dispExecute},
	"prepare":                           {kind: dispExecute},
	"system-docs get":                   {kind: dispExecute, args: []string{"system-docs", "get", "1"}},
	"system-docs list":                  {kind: dispExecute},
	"system-docs pull":                  {kind: dispExecute},
	"system-docs push":                  {kind: dispNoCall, reason: "needs-precondition"},
	"dev review":                        {kind: dispNoCall, reason: "local-only"},
	"epics list":                        {kind: dispExecute},
	"epics get":                         {kind: dispExecute, args: []string{"epics", "get", "42"}},
	"epics create":                      {kind: dispNoCall, reason: "needs-precondition"},
	"epics update":                      {kind: dispNoCall, reason: "needs-precondition"},
	"epics specs list":                  {kind: dispExecute, args: []string{"epics", "specs", "list", "42"}},
	"epics specs status":                {kind: dispExecute, args: []string{"epics", "specs", "status", "42"}},
	"epics specs generate":              {kind: dispExecute, args: []string{"epics", "specs", "generate", "42"}},
	"epics specs pull":                  {kind: dispExecute, args: []string{"epics", "specs", "pull", "42", "--file", "specs.json"}},
	"epics specs push":                  {kind: dispNoCall, reason: "needs-precondition"},
	"requirements get":                  {kind: dispExecute, args: []string{"requirements", "get", "10000000-0000-4000-8000-000000000001", "--kind", "system"}},
	"traces list":                       {kind: dispExecute, args: []string{"traces", "list", "--entity-type", "epic", "--entity-id", "42"}},
	"test-cases list":                   {kind: dispExecute},
	"test-cases get":                    {kind: dispExecute, args: []string{"test-cases", "get", "10000000-0000-4000-8000-000000000001"}},
	"requirements create":               {kind: dispNoCall, reason: "needs-precondition"},
	"requirements update":               {kind: dispNoCall, reason: "needs-precondition"},
	"test-cases create":                 {kind: dispNoCall, reason: "needs-precondition"},
	"test-cases update":                 {kind: dispNoCall, reason: "needs-precondition"},
	"evidence record":                   {kind: dispNoCall, reason: "needs-precondition"},
	"evidence latest":                   {kind: dispExecute},
	"evidence runs":                     {kind: dispExecute},

	// Epic 292: the human's "not yet" at completion review. Like
	// approve-completion it stops before the call without the completion
	// bundle. Its own block keeps gofmt from re-aligning the whole map around
	// the longer key.
	"epics plan return-to-implementation": {kind: dispExecute, args: []string{"epics", "plan", "return-to-implementation", "42", "--reason", "smoke"}},
}

func TestEveryLeafIsClassifiedAndBehavesAsClassified(t *testing.T) {
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if strings.HasSuffix(r.URL.Path, "/reverse-engineering/schema") {
			_, _ = w.Write([]byte(`{"data":{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	// A temp HOME as well as a temp cwd: a leaf that writes agent config must
	// not be able to reach the developer's real ~/.claude during a test run.
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(dir, ".modernpath"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"api_url":"` + server.URL + `","system_id":1,"system_name":"T","system_slug":"t","epic_id":42,"epic_name":"Smoke"}`
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".modernpath", "auth.json"), []byte(`{"token":"t"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	seedSmokeWorkspace(t, dir)
	t.Chdir(dir)
	applyGroupUnknownArgGuard(rootCmd)

	leaves := walkLeafPaths(rootCmd)
	if len(leaves) < 50 {
		t.Fatalf("leaf walk found only %d leaves — the walk is broken", len(leaves))
	}

	// Both directions: no unclassified leaf, and no disposition for a leaf
	// that no longer exists.
	seen := map[string]bool{}
	for _, leaf := range leaves {
		name := strings.Join(leaf, " ")
		seen[name] = true
		if _, ok := leafDispositions[name]; !ok {
			t.Errorf("leaf %q has no disposition — classify it in leafDispositions as "+
				"dispExecute, or exempt it with a reason (SR REQ-CROSS-212 tier b)", name)
		}
	}
	for name := range leafDispositions {
		if !seen[name] {
			t.Errorf("leafDispositions has a stale entry %q — no such leaf in the tree", name)
		}
	}

	executed, unrun := 0, 0
	for _, leaf := range leaves {
		name := strings.Join(leaf, " ")
		d, ok := leafDispositions[name]
		if !ok {
			continue // already reported above
		}
		if d.kind != dispExecute {
			if d.reason == "" {
				t.Errorf("leaf %q is exempt with no reason", name)
				continue
			}
			if !exemptReasons[d.reason] {
				t.Errorf("leaf %q claims unknown exempt reason %q", name, d.reason)
				continue
			}
		}
		if d.kind == dispUnrun {
			unrun++
			continue
		}
		args := d.args
		if args == nil {
			args = leaf
		}
		before := requests
		resetTreeFlags(rootCmd) // do not inherit another test's flag state
		rootCmd.SetArgs(args)
		if strings.HasSuffix(name, "pin set") {
			rootCmd.SetIn(strings.NewReader("2468\n"))
		}
		if name == "backlog create" {
			rootCmd.SetIn(strings.NewReader(`{"title":"Smoke discovery"}`))
		}
		if name == "backlog update" {
			rootCmd.SetIn(strings.NewReader(`{"notes_md":"Smoke update"}`))
		}
		if name == "reverse-engineer record" {
			rootCmd.SetIn(strings.NewReader(`{"requirements":[],"traces":[]}`))
		}
		if name == "reverse-engineer review" {
			rootCmd.SetIn(strings.NewReader(`{"requirements":[{"kind":"system_requirement","id":"10000000-0000-4000-8000-000000000001"}]}`))
		}
		if name == "reverse-engineer accept" {
			rootCmd.SetIn(strings.NewReader(`{"requirements":[{"kind":"system_requirement","id":"10000000-0000-4000-8000-000000000001"}],"source":"USER:smoke","reason":"smoke"}`))
		}
		_ = rootCmd.Execute() // errors are fine — a stub server answers junk
		rootCmd.SetArgs(nil)
		switch d.kind {
		case dispExecute:
			executed++
			if requests == before {
				t.Errorf("Execute(%v) never reached the server — its handler did not run", args)
			}
		case dispNoCall:
			if requests != before {
				t.Errorf("leaf %q is exempt as %q but DID call the server — reclassify it as dispExecute",
					name, d.reason)
			}
		}
	}

	if executed < minExecutedLeaves {
		t.Errorf("only %d leaves executed against httptest, floor is %d — "+
			"exemption is being used to hollow out tier (b)", executed, minExecutedLeaves)
	}
	if unrun > maxUnrunLeaves {
		t.Errorf("%d leaves are exempt without being run, cap is %d — "+
			"an unrun exemption is the only unchecked claim here", unrun, maxUnrunLeaves)
	}
}

func walkLeafPaths(root *cobra.Command) [][]string {
	var leaves [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if len(c.Commands()) == 0 {
			if len(path) > 0 {
				leaves = append(leaves, path)
			}
			return
		}
		for _, sub := range c.Commands() {
			// cobra synthesizes `help` and `completion`, and materializes
			// `help` lazily the first time help is used — so including them
			// would make the population depend on test order. They are not
			// this project's surface; everything else must be classified.
			if len(path) == 0 && (sub.Name() == "help" || sub.Name() == "completion") {
				continue
			}
			walk(sub, append(append([]string{}, path...), sub.Name()))
		}
	}
	walk(root, nil)
	return leaves
}

// seedSmokeWorkspace gives the temp workspace the minimum state that
// server-backed leaves check before they call: an exported doc, an epic spec,
// and a git repo with one commit and one uncommitted change (for diff modes).
func seedSmokeWorkspace(t *testing.T, dir string) {
	t.Helper()
	mk := func(p, body string) {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk(".modernpath/modernpath/t/architecture/overview.md", "# Doc\n\nbody\n")
	mk(".modernpath/tasks/42/specs/requirements.md", "# spec\n")
	mk("README.md", "# r\n")
	git := func(args ...string) {
		_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	}
	git("init")
	git("add", "-A")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	mk("README.md", "# r\n\nchanged\n")
}
