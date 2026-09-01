// REQ-CROSS-212 (EPIC-CLI-001 T9, RUN:2026-08-18): no test drove the cobra
// tree, so an unregistered command (top-level sync/ralph), an unregistered
// flag (factory drift --report), and a silently ignored positional
// (auth logout) all passed `go test ./...` green. This smoke pins the wiring:
// every advertised leaf resolves, dead surfaces stay dead, and the
// Q-ARCH-016 decision (USER:2026-08-18) is encoded — sync/ralph deleted,
// drift --report registered.
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
		{"ask"}, {"auth"}, {"check"}, {"context"}, {"coverage"},
		{"datamodel", "export"},
		{"dev", "list"}, {"dev", "setup"}, {"dev", "run"}, {"dev", "task"}, {"dev", "ralph"},
		{"docs", "sync"}, {"docs", "generate"}, {"docs", "refresh"}, {"docs", "preview"},
		{"docs", "repair"}, {"docs", "cleanup"}, {"docs", "push"},
		{"env"},
		{"factory", "connect"}, {"factory", "status"}, {"factory", "sync"},
		{"factory", "gates"}, {"factory", "answer"}, {"factory", "pull"},
		{"focus"},
		{"factory", "evidence"}, {"factory", "drift"}, {"factory", "watch"},
		{"factory", "image"}, {"factory", "manifest"}, {"factory", "next-id"},
		{"factory", "reconcile"}, {"factory", "release"},
		{"github"},
		{"hooks", "install"}, {"hooks", "uninstall"}, {"hooks", "status"}, {"hooks", "doctor"},
		{"import"}, {"init"}, {"init", "workspace"}, {"install"}, {"new"},
		{"read-doc"}, {"read-file"}, {"scan"}, {"search"}, {"status"},
		{"system-docs", "push"}, {"system-docs", "pull"}, {"system-docs", "list"},
		{"tasks", "list"}, {"tasks", "fetch"},
		{"work", "list"}, {"work", "new"}, {"work", "status"}, {"work", "select"},
		{"work", "subtasks"}, {"work", "derive"}, {"work", "review"},
		// external review RUN:2026-08-19: the first cut listed parent groups
		// for these and omitted the leaves themselves.
		{"env", "list"}, {"env", "test"},
		{"dev", "ralph", "status"},
		{"factory", "manifest", "show"}, {"factory", "manifest", "init"},
		{"factory", "release", "use"}, {"factory", "release", "show"}, {"factory", "release", "clear"},
		{"work", "specs", "generate"}, {"work", "specs", "sync"}, {"work", "specs", "push"},
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

func TestDeadSurfacesStayDead(t *testing.T) {
	// Q-ARCH-016 (USER:2026-08-18): top-level sync and ralph are deleted —
	// their living replacements are docs sync/factory sync and dev ralph.
	// review and implement were retired earlier for the same reason.
	for _, name := range []string{"sync", "ralph", "review", "implement"} {
		cmd, _, _ := rootCmd.Find([]string{name})
		if cmd != rootCmd {
			t.Errorf("%q resolves to %q — a deleted command has returned", name, cmd.Name())
		}
	}
}

func TestDriftReportFlagIsRegistered(t *testing.T) {
	// Q-ARCH-016: --report is advertised in drift's own output; it must exist.
	drift, _, err := rootCmd.Find([]string{"factory", "drift"})
	if err != nil {
		t.Fatalf("factory drift does not resolve: %v", err)
	}
	if drift.Flags().Lookup("report") == nil {
		t.Fatal("factory drift advertises --report but the flag is not registered")
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

	// The Q-ARCH-016 flag registration, through real dispatch.
	if err := run([]string{"factory", "drift", "--report", "--help"}); err != nil {
		t.Errorf("factory drift --report does not parse through Execute: %v", err)
	}
	// Leave no --help set behind: a later test executing `factory drift` for
	// real would otherwise get help output and no handler run (found by
	// TestEveryLeafIsClassifiedAndBehavesAsClassified, RUN:2026-08-19).
	resetTreeFlags(rootCmd)
}

// resetTreeFlags returns every flag in the tree to its default and clears
// Changed. cobra's tree is package-level and shared across tests, so a flag
// set by one test otherwise silently changes another test's behaviour.
func resetTreeFlags(c *cobra.Command) {
	reset := func(f *pflag.Flag) {
		_ = f.Value.Set(f.DefValue)
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

// Floors: exemption must not be usable to hollow the tier out. Both are
// deliberately just below the measured RUN:2026-08-19 figures (38 executed,
// 5 unrun) so ordinary growth passes and wholesale reclassification fails.
const (
	minExecutedLeaves = 35
	maxUnrunLeaves    = 6
)

// Dispositions measured RUN:2026-08-19 by executing every leaf against a
// stub server in a temp workspace: 38 executed, 19 local-only, 4 blocked on a
// precondition, 5 never run. Every entry here was observed, not assumed.
var leafDispositions = map[string]disposition{
	// --- executed: the handler runs and the server receives the request ---
	"ask":               {kind: dispExecute, args: []string{"ask", "smoke question"}},
	"context":           {kind: dispExecute, args: []string{"context", "smoke query"}},
	"datamodel export":  {kind: dispExecute, args: []string{"datamodel", "export", "--json"}},
	"docs cleanup":      {kind: dispExecute},
	"docs preview":      {kind: dispExecute},
	"docs push":         {kind: dispExecute},
	"docs refresh":      {kind: dispExecute},
	"docs repair":       {kind: dispExecute},
	"docs sync":         {kind: dispExecute},
	"env test":          {kind: dispExecute}, // the leaf the hard-coded ten missed
	"factory connect":   {kind: dispExecute},
	"factory drift":     {kind: dispExecute},
	"factory evidence":  {kind: dispExecute, args: []string{"factory", "evidence", "report", "--kind", "local_test", "--log", "x", "--totals", "passed=1,failed=0", "--pass", "REQ-X-001"}},
	"factory gates":     {kind: dispExecute},
	"factory image":     {kind: dispExecute, args: []string{"factory", "image", "smoke"}},
	"factory next-id":   {kind: dispExecute, args: []string{"factory", "next-id", "CROSS"}},
	"factory pull":      {kind: dispExecute},
	"factory reconcile": {kind: dispExecute},
	"factory sync":      {kind: dispExecute},
	// REQ-PLN-134 (EPIC-NEXT-005): `focus <id>` posts the declaration.
	"focus": {kind: dispExecute, args: []string{"focus", "REQ-SMK-001"}},
	// REQ-CROSS-221/224: server-backed but both refuse the smoke's ledgerless
	// workspace before any call — a report or import over nothing must not
	// read as clean.
	"migrate report": {kind: dispNoCall, reason: "needs-precondition"},
	"migrate run":    {kind: dispNoCall, reason: "needs-precondition"},
	// REQ-CROSS-225: refuses the ledgerless smoke workspace before any call.
	"migrate flip": {kind: dispNoCall, reason: "needs-precondition"},
	// REQ-CROSS-228: every authoring verb posts the attributed call.
	"author requirement": {kind: dispExecute, args: []string{"author", "requirement", "REQ-SMK-001", "--title", "t", "--context", "SMK"}},
	"author epic":        {kind: dispExecute, args: []string{"author", "epic", "EPIC-SMK-001", "--title", "t"}},
	"author gate":        {kind: dispExecute, args: []string{"author", "gate", "Q-SMK-001", "--title", "t"}},
	"author trace":       {kind: dispExecute, args: []string{"author", "trace", "TRACE-SMK-001", "--title", "t", "--purpose", "cold-review", "--transition", "plan->entry", "--scope", "EPIC-SMK-001", "--fingerprint", "packet-sha256", "--verdict", "FAIL", "--source", "RUN:2026-08-26"}},
	"author advance":     {kind: dispExecute, args: []string{"author", "advance", "REQ-SMK-001", "--to", "IN_PROGRESS", "--expected", "TODO"}},
	"import":             {kind: dispExecute},
	"new":                {kind: dispExecute},
	"read-doc":           {kind: dispExecute, args: []string{"read-doc", "--list"}},
	"read-file":          {kind: dispExecute, args: []string{"read-file", "lib/app.ex"}},
	"scan":               {kind: dispExecute},
	"search":             {kind: dispExecute, args: []string{"search", "smoke query"}},
	// REQ-CROSS-282: status now checks the bound system's reachability
	// whenever a bearer and a system_id are both present — exactly this
	// fixture's shape — so it genuinely reaches the server and is no longer
	// local-only. Not asserted dispNoCall/needs-precondition: the check
	// fails open on any error (including this stub's unparseable body), so
	// the command still succeeds; only the request count moved.
	"status":              {kind: dispExecute},
	"system-docs list":    {kind: dispExecute},
	"system-docs pull":    {kind: dispExecute},
	"tasks fetch":         {kind: dispExecute},
	"tasks list":          {kind: dispExecute},
	"work derive":         {kind: dispExecute},
	"work list":           {kind: dispExecute},
	"work new":            {kind: dispExecute, args: []string{"work", "new", "smoke epic"}},
	"work select":         {kind: dispExecute},
	"work specs generate": {kind: dispExecute},
	"work specs sync":     {kind: dispExecute},
	"work status":         {kind: dispExecute},
	"working-set pull":    {kind: dispExecute, args: []string{"working-set", "pull", "EPIC-SMOKE"}},
	"working-set select":  {kind: dispExecute, args: []string{"working-set", "select", "EPIC-SMOKE", "--phase", "build"}},
	"your-move":           {kind: dispExecute},
	"work subtasks":       {kind: dispExecute, args: []string{"work", "subtasks", "1"}},

	// --- exempt, ASSERTED: run, and required to reach no server ---
	"check":                 {kind: dispNoCall, reason: "local-only"},
	"coverage":              {kind: dispNoCall, reason: "local-only"},
	"dev list":              {kind: dispNoCall, reason: "local-only"},
	"dev ralph status":      {kind: dispNoCall, reason: "local-only"},
	"dev setup":             {kind: dispNoCall, args: []string{"dev", "setup", "claude"}, reason: "local-only"},
	"env list":              {kind: dispNoCall, reason: "local-only"},
	"factory manifest init": {kind: dispNoCall, reason: "local-only"},
	"factory manifest show": {kind: dispNoCall, reason: "local-only"},
	"factory release clear": {kind: dispNoCall, reason: "local-only"},
	"factory release show":  {kind: dispNoCall, reason: "local-only"},
	"factory release use":   {kind: dispNoCall, args: []string{"factory", "release", "use", "smoke"}, reason: "local-only"},
	"factory status":        {kind: dispNoCall, reason: "local-only"},
	"hooks doctor":          {kind: dispNoCall, reason: "local-only"},
	"hooks install":         {kind: dispNoCall, reason: "local-only"},
	"hooks status":          {kind: dispNoCall, reason: "local-only"},
	"hooks uninstall":       {kind: dispNoCall, reason: "local-only"},
	"init workspace":        {kind: dispNoCall, reason: "local-only"},
	"install":               {kind: dispNoCall, reason: "local-only"},
	"work review":           {kind: dispNoCall, reason: "local-only"},

	// Server-backed, but they refuse before the call without state the smoke
	// cannot cheaply build. Asserted as no-call so that fixing the
	// precondition forces a move to dispExecute rather than passing silently.
	"docs generate":    {kind: dispNoCall, reason: "needs-precondition"},
	"factory answer":   {kind: dispNoCall, args: []string{"factory", "answer", "1", "yes"}, reason: "needs-precondition"},
	"system-docs push": {kind: dispNoCall, reason: "needs-precondition"},
	// REQ-CROSS-282: working-set check's own sync-payload logic still bails
	// before its own call, but its RunE calls factoryEnvLoad() first (a
	// chokepoint, not this leaf's own precondition path), and that now
	// always makes one reachability call when a bearer+system_id are
	// present — exactly this fixture's shape.
	"working-set check": {kind: dispExecute},
	"work specs push":   {kind: dispNoCall, reason: "needs-precondition"},

	// --- exempt, NOT run: the only unchecked claims in this test ---
	"auth":          {kind: dispUnrun, reason: "interactive"},  // stdin prompt + openBrowser
	"github":        {kind: dispUnrun, reason: "interactive"},  // openBrowser
	"dev run":       {kind: dispUnrun, reason: "spawns-agent"}, // exec.Command(tool.Command)
	"dev task":      {kind: dispUnrun, reason: "spawns-agent"}, // exec.Command(tool.Command)
	"factory watch": {kind: dispUnrun, reason: "blocking"},     // daemon cadence, SIGINT to stop

}

func TestEveryLeafIsClassifiedAndBehavesAsClassified(t *testing.T) {
	requests := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
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
	cfg := `{"api_url":"` + server.URL + `","system_id":1,"system_name":"T","system_slug":"t","epic_id":1}`
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
	mk("epics/EPIC-X-001/specs/requirements.md", "# spec\n")
	mk("README.md", "# r\n")
	git := func(args ...string) {
		_ = exec.Command("git", append([]string{"-C", dir}, args...)...).Run()
	}
	git("init")
	git("add", "-A")
	git("-c", "user.email=t@t", "-c", "user.name=t", "commit", "-m", "init")
	mk("README.md", "# r\n\nchanged\n")
}
