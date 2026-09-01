package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-029: install writes the tool-owned process snapshot and adapters
// wholesale and merges only its managed block into the client's AGENTS.md.
func TestInstallWritesTheKitAndLeavesClientContentAlone(t *testing.T) {
	root := t.TempDir()
	clientAgents := "# AGENTS.md — acme\n\nStack: Rails 7, Postgres.\nDeploy with `make ship`.\n"
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(clientAgents), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"CLAUDE.md",
		".modernpath/rdd/.source",
		".modernpath/rdd/AGENTS.md",
		".modernpath/rdd/PROCESS.md",
		".modernpath/rdd/file-state/GATES.md",
		".modernpath/rdd/skills/rdd-start/SKILL.md",
		".claude/skills/rdd-start/SKILL.md",
		".claude/skills/rdd-verify/SKILL.md",
		".claude/skills/rdd-audit/SKILL.md",
		".github/copilot-instructions.md",
	} {
		if _, err := os.Stat(filepath.Join(root, want)); err != nil {
			t.Errorf("missing after install: %s", want)
		}
	}

	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	got := string(agents)
	if !strings.Contains(got, "Stack: Rails 7, Postgres.") || !strings.Contains(got, "make ship") {
		t.Fatal("client content was lost from AGENTS.md")
	}
	if !HasManagedBlock(got) {
		t.Fatal("managed block not merged into AGENTS.md")
	}
	if !strings.Contains(got, ".modernpath/rdd/AGENTS.md") {
		t.Fatal("the managed block must point non-Claude agents at the installed process")
	}
	if res.AgentsCreated {
		t.Error("AGENTS.md existed; it should be reported as merged, not created")
	}

	// The root CLAUDE.md must import the installed package, or nothing loads at session start.
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(claude), "@.modernpath/rdd/PROCESS.md") {
		t.Error("CLAUDE.md must @-import the installed process")
	}

	process, _ := os.ReadFile(filepath.Join(root, ".modernpath/rdd/PROCESS.md"))
	if !strings.Contains(string(process), "# Requirement-driven delivery process") {
		t.Error("the full canonical process must be installed from the embedded snapshot")
	}

	// The package skills install twice on purpose: the tool-neutral canonical
	// path and the Claude Code discovery path must be byte-identical.
	canonical, _ := os.ReadFile(filepath.Join(root, ".modernpath/rdd/skills/rdd-start/SKILL.md"))
	claudeCopy, _ := os.ReadFile(filepath.Join(root, ".claude/skills/rdd-start/SKILL.md"))
	if len(canonical) == 0 || string(canonical) != string(claudeCopy) {
		t.Error("the .claude/skills copy of a package skill must be byte-identical to the canonical one")
	}
}

// Re-installing is how upgrades happen; it must not drift the client's file.
func TestInstallIsIdempotent(t *testing.T) {
	root := t.TempDir()
	client := "# acme\n\nStack: Rails.\n"
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte(client), 0o644)

	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if string(first) != string(second) {
		t.Fatal("second install changed AGENTS.md — upgrades would show spurious diffs")
	}
}

// A fresh repository with no AGENTS.md still gets one, so Codex is served.
func TestInstallCreatesAgentsWhenAbsent(t *testing.T) {
	root := t.TempDir()
	res, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.AgentsCreated {
		t.Error("expected AGENTS.md to be reported as created")
	}
	agents, _ := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if !HasManagedBlock(string(agents)) {
		t.Fatal("created AGENTS.md must carry the managed block")
	}
}

// Damaged markers must abort the whole install, not half-write it.
func TestInstallRefusesDamagedMarkersWithoutWritingAnything(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("# acme\n"+BeginMarker+"\nhalf\n"), 0o644)

	if _, err := Install(root); err == nil {
		t.Fatal("expected an error for damaged markers")
	}
	if _, err := os.Stat(filepath.Join(root, "CLAUDE.md")); err == nil {
		t.Error("nothing should have been written when the merge cannot be done safely")
	}
}

// REQ-CROSS-029: `.modernpath/rdd/PROCESS.md` is generated.
// Editing it is silently reverted by the next
// install, which is the same shape as every other failure this week: no error,
// no signal, the change simply stops existing. Check() makes that state
// detectable instead.
func TestCheckDetectsAnEditedGeneratedFile(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("a fresh install must be clean: drift=%v err=%v", drift, err)
	}

	edited := filepath.Join(root, ".modernpath/rdd/PROCESS.md")
	body, _ := os.ReadFile(edited)
	os.WriteFile(edited, append(body, []byte("\n\nlocal edit that install would revert\n")...), 0o644)

	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0] != ".modernpath/rdd/PROCESS.md" {
		t.Fatalf("expected the edited file to be reported, got %v", drift)
	}

	// A deleted tool-owned file is drift too — it means the repo is missing
	// part of the process it claims to follow.
	os.Remove(filepath.Join(root, ".claude/skills/rdd-audit/SKILL.md"))
	drift, _ = Check(root)
	if len(drift) != 2 {
		t.Fatalf("a missing tool-owned file must count as drift, got %v", drift)
	}
}

// REQ-CROSS-029: a real client repository already had a 403-line CLAUDE.md —
// its own process manual, with project-specific non-negotiables the kit does
// not contain. The installer would have deleted it. Any file a client may
// already own is merged, never overwritten; only kit-namespaced paths are
// replaced wholesale.
func TestInstallPreservesAnExistingClaudeMdAndCopilotInstructions(t *testing.T) {
	root := t.TempDir()
	theirs := "# ACME — Development Process\n\n## 1. The non-negotiables\n\n" +
		"1. **Agents are never the system of record.**\n" +
		"5. **Money is integer minor units.**\n"
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte(theirs), 0o644)
	os.MkdirAll(filepath.Join(root, ".github"), 0o755)
	os.WriteFile(filepath.Join(root, ".github/copilot-instructions.md"), []byte("# ours\n\nUse tabs.\n"), 0o644)

	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	got := string(claude)
	if !strings.Contains(got, "Agents are never the system of record") ||
		!strings.Contains(got, "Money is integer minor units") {
		t.Fatal("the client's own CLAUDE.md content was destroyed")
	}
	if !strings.Contains(got, "@.modernpath/rdd/PROCESS.md") {
		t.Fatal("the installed process import must still be added")
	}
	if !HasManagedBlock(got) {
		t.Fatal("CLAUDE.md must carry a managed block so upgrades can refresh it")
	}

	copilot, _ := os.ReadFile(filepath.Join(root, ".github/copilot-instructions.md"))
	if !strings.Contains(string(copilot), "Use tabs.") {
		t.Fatal("the client's copilot instructions were destroyed")
	}

	before, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if string(before) != string(after) {
		t.Fatal("second install changed a co-owned file")
	}
}

func TestInstallDoesNotRequireCanonicalSourceCheckout(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatalf("embedded process installation must not require req-driven-dev: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath/rdd/PROCESS.md")); err != nil {
		t.Fatalf("embedded canonical process was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "req-driven-dev")); !os.IsNotExist(err) {
		t.Fatal("install must not create or require a vendored req-driven-dev directory")
	}
}

// REQ-CROSS-029: upgrades move the shared package out of the Claude-specific
// directory and retire old flat paths without deleting client-added files.
func TestInstallMigratesKnownLegacyProcessFilesOnly(t *testing.T) {
	root := t.TempDir()
	legacy := filepath.Join(root, ".claude/rdd")
	oldLayout := filepath.Join(root, ".modernpath/rdd")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(oldLayout, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"state-tracking.md", "custom.md", ".source"} {
		if err := os.WriteFile(filepath.Join(legacy, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"state-tracking.md", "custom.md"} {
		if err := os.WriteFile(filepath.Join(oldLayout, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	compat := filepath.Join(oldLayout, "compat")
	if err := os.MkdirAll(compat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compat, "PROCESS.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	process := filepath.Join(oldLayout, "process")
	if err := os.MkdirAll(process, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(process, "interview-flows.md"), []byte("old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	optionalTemplates := filepath.Join(oldLayout, "templates/docs")
	if err := os.MkdirAll(optionalTemplates, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"DOMAIN_DOC.md", "custom.md"} {
		if err := os.WriteFile(filepath.Join(optionalTemplates, name), []byte("old\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	res, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".modernpath/rdd/PROCESS.md")); err != nil {
		t.Fatalf("replacement process file was not installed: %v", err)
	}
	for _, retired := range []string{"state-tracking.md", ".source"} {
		if _, err := os.Stat(filepath.Join(legacy, retired)); !os.IsNotExist(err) {
			t.Fatalf("known legacy process file was not removed: %s", retired)
		}
	}
	if _, err := os.Stat(filepath.Join(legacy, "custom.md")); err != nil {
		t.Fatalf("client-owned legacy file must be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldLayout, "state-tracking.md")); !os.IsNotExist(err) {
		t.Fatal("known file from the old flat package layout was not removed")
	}
	if _, err := os.Stat(filepath.Join(compat, "PROCESS.md")); !os.IsNotExist(err) {
		t.Fatal("known file from the old compatibility directory was not removed")
	}
	if _, err := os.Stat(compat); !os.IsNotExist(err) {
		t.Fatal("empty retired compatibility directory was not removed")
	}
	if _, err := os.Stat(filepath.Join(process, "interview-flows.md")); !os.IsNotExist(err) {
		t.Fatal("retired interview guide was not removed")
	}
	if _, err := os.Stat(filepath.Join(optionalTemplates, "DOMAIN_DOC.md")); !os.IsNotExist(err) {
		t.Fatal("retired optional documentation template was not removed")
	}
	if _, err := os.Stat(filepath.Join(optionalTemplates, "custom.md")); err != nil {
		t.Fatalf("client-owned file beside retired templates must be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oldLayout, "custom.md")); err != nil {
		t.Fatalf("client-owned file in the package namespace must be preserved: %v", err)
	}
	if len(res.Removed) != 6 {
		t.Fatalf("expected six retired files to be reported, got %v", res.Removed)
	}
}

func TestEmbeddedProcessSnapshotRecordsItsSourceRevision(t *testing.T) {
	body, err := assets.ReadFile("assets/rdd-source.txt")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "repository=https://github.com/ModernPath/req-driven-dev\n") {
		t.Fatal("embedded process snapshot must identify its canonical source")
	}
	const revisionPrefix = "revision="
	start := strings.Index(text, revisionPrefix)
	if start < 0 || len(strings.TrimSpace(text[start+len(revisionPrefix):])) != 40 {
		t.Fatal("embedded process snapshot must record a full git revision")
	}
}

// REQ-CROSS-039 (`USER:2026-08-11`): every agent that reads AGENTS.md must know
// the knowledge core exists and how to reach it. The two paths do different
// jobs — the API finds, the local export reads — and an agent told only about
// one of them either pays a round trip for a file it already has, or trusts a
// cache that can lag.
func TestAgentsBlockPointsAtTheKnowledgeCore(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	agents := string(raw)

	for _, want := range []string{
		"modernpath search",       // find
		".modernpath/modernpath/", // read
		"modernpath docs sync",    // repair, when the export was never run
	} {
		if !strings.Contains(agents, want) {
			t.Fatalf("AGENTS.md never mentions %q:\n%s", want, agents)
		}
	}

	// The precedence rule is the point: a stale export that answers confidently
	// is the failure this sentence exists to prevent (USER:2026-08-11).
	if !strings.Contains(agents, "cache") {
		t.Fatal("AGENTS.md must say which source wins when they disagree")
	}

	// It has to live inside the managed region, or an upgrade cannot refresh it.
	managed := agents[strings.Index(agents, BeginMarker):strings.Index(agents, EndMarker)]
	if !strings.Contains(managed, "modernpath search") {
		t.Fatal("the knowledge section must be inside the managed block")
	}
}

// Managed blocks are generated content — an edit
// inside one was invisible to Check and silently reverted by the next install,
// and a CLI release changing only the adapter blocks raised no drift at all.
func TestCheckDetectsAManagedBlockEdit(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("fresh install must be drift-free, got %v, %v", drift, err)
	}

	path := filepath.Join(root, "AGENTS.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := string(body)
	at := strings.Index(doc, BeginMarker) + len(BeginMarker)
	edited := doc[:at] + "\nlocal override: skip the upper loop" + doc[at:]
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	want := "AGENTS.md (managed block)"
	found := false
	for _, d := range drift {
		if d == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("an edit inside the managed block must be reported as %q, got %v", want, drift)
	}
}

// A fresh install reported "managed block merged;
// your content untouched" for three files it had just created.
func TestInstallReportsCreatedEntryFilesAsCreated(t *testing.T) {
	root := t.TempDir()
	res, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Merged) != 0 {
		t.Fatalf("nothing pre-existed, so nothing was merged: %v", res.Merged)
	}
	if len(res.Created) != 3 {
		t.Fatalf("expected the three entry files to be reported created, got %v", res.Created)
	}
}

// RUN:2026-08-12: a client repository deliberately symlinked CLAUDE.md ->
// AGENTS.md so one file serves every agent. `modernpath install` replaced the
// symlink with a regular copy, silently ending their convention — and because
// the copy still carried the managed block, nothing looked wrong.
func TestInstallDoesNotClobberASymlinkedTarget(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# Their guide\n\nProject rules.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("install replaced a symlink with a regular file — the project's convention is gone")
	}

	// And the process must still reach an agent through the link.
	raw, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), BeginMarker) {
		t.Fatal("the managed block did not reach the symlink's target")
	}
	if !strings.Contains(string(raw), "Project rules.") {
		t.Fatal("their content was lost")
	}
}

// REQ-CROSS-040 (EPIC-ONB-001, `USER:2026-08-11`): the brownfield path — a
// repository with code and no ledgers — existed only as a prompt someone retyped
// per repo. It ships as a skill so the agent finds the method by itself.
func TestInstallPlacesTheReverseEngineeringSkill(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	skill := string(raw)

	// The description is how an agent finds it without being handed the path, and
	// it must not invite a second pass over a workspace that already has a ledger.
	if !strings.Contains(skill, "name: rdd-reverse-engineer") {
		t.Fatalf("skill needs its frontmatter name:\n%s", skill[:min(400, len(skill))])
	}
	for _, want := range []string{"IN_REVIEW", "BLOCKED", "modernpath check"} {
		if !strings.Contains(skill, want) {
			t.Fatalf("the method never mentions %q — the decisions it encodes are missing", want)
		}
	}
}

func TestAgentsBlockNamesTheReverseEngineeringSkill(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	agents := string(raw)

	if !strings.Contains(agents, "rdd-reverse-engineer") {
		t.Fatalf("AGENTS.md never names the skill, so a harness without skills cannot follow it:\n%s", agents)
	}
	// Inside the managed region, or an upgrade cannot refresh it.
	managed := agents[strings.Index(agents, BeginMarker):strings.Index(agents, EndMarker)]
	if !strings.Contains(managed, "rdd-reverse-engineer") {
		t.Fatal("the pointer must live inside the managed block")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// REQ-CROSS-042 (EPIC-ONB-001, `USER:2026-08-11`): the first method derived from
// endpoints and produced 47 system requirements, zero user requirements, and
// never opened one of 42 view files. The skill now carries three phases, and
// these assert the decisions that distinguish them — an endpoint-first method
// would pass a test that only checked the file exists.
func TestReverseEngineerSkillCarriesTheThreePhases(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	for _, want := range []struct{ token, why string }{
		{"aggregate ownership", "D-ONB-8: contexts come from who writes which table, not route-file layout"},
		{"role gate", "D-ONB-10: a user requirement cites the gate that admits its actor"},
		{"join report", "D-ONB-11: views calling nothing and endpoints no view reaches are findings"},
		{"user requirement", "D-ONB-9: phase B produces epics carrying UR, not only ledger rows"},
		{"epics/", "the pass must write epics — their absence is what factory status kept reporting"},
	} {
		if !strings.Contains(skill, want.token) {
			t.Fatalf("skill is missing %q — %s", want.token, want.why)
		}
	}

	// Order matters: domain before surfaces before requirements. A skill that
	// mentions all three but derives requirements first is the method we replaced.
	domain := strings.Index(skill, "aggregate ownership")
	surfaces := strings.Index(skill, "role gate")
	if domain < 0 || surfaces < 0 || domain > surfaces {
		t.Fatalf("phases out of order: domain at %d, surfaces at %d", domain, surfaces)
	}
}

// REQ-CROSS-079/083 (EPIC-ARCH-001, spec approved `USER:2026-08-13`): the pass
// gains a fourth phase. Three passes produced 7,000+ requirements and not one
// line about the system's shape — no document names the subsystems, the external
// interfaces, the datastores, or the path a request takes.
//
// Asserted against the INSTALLED copy, never the source: the two have diverged
// before, and only what `modernpath install` writes is what another repository
// actually gets.
func TestReverseEngineerSkillCarriesPhaseDAndItsDocuments(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	for _, want := range []struct{ token, why string }{
		{"03-architecture", "D-ARCH-2: the document a reader opens first — subsystems, interfaces, datastores"},
		{"20-deployment-topology", "D-ARCH-2: what runs where, and what a request traverses"},
		{"21-integrations", "D-ARCH-2: every external system, and what happens when it is unreachable"},
		{"22-cross-cutting", "D-ARCH-2: auth, tenancy, observability, resilience — as implemented"},
		{"23-data-flow", "D-ARCH-4: adopted from the nine-document set; structure and events are not flow"},
		{"docs/adr/", "D-ARCH-2: decisions the code already made"},
		{"observed", "D-ARCH-2: an ADR may not claim a ratification the repository never performed"},
		{"unreachable", "SCN-ARCH-002: the column a dependency list cannot give you"},
		{"docs/guides/", "D-ARCH-5: human-written lenses, read before the pass walks anything"},
		{"lens", "REQ-CROSS-083: a guide directs attention and never supplies evidence"},
	} {
		if !strings.Contains(skill, want.token) {
			t.Errorf("installed rdd-reverse-engineer is missing %q — %s", want.token, want.why)
		}
	}
}

// REQ-CROSS-080/081: the NFR ledger, and the guard that makes it safe.
func TestReverseEngineerSkillCarriesTheNFRRules(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	for _, want := range []struct{ token, why string }{
		{"NFR-REQUIREMENTS.md", "D-ARCH-1: quality attributes get their own context, not a contract kind"},
		{"performance", "the eight-term taxonomy — an open list becomes forty labels in two passes"},
		{"operability", "same taxonomy; a term nobody would invent by accident"},
		{"BLOCKED", "D-ARCH-3: a latent threshold is a question, never an assertion"},
	} {
		if !strings.Contains(skill, want.token) {
			t.Errorf("installed rdd-reverse-engineer is missing %q — %s", want.token, want.why)
		}
	}
}

// REQ-CROSS-085 (`USER:2026-08-13`, "for client repo's, we need a process /
// loop to run all of them at a same time?"): phases A–C have an exit criterion —
// `process/coverage.md`, and C11's rule that the loop ends when every context
// has a ledger. Phase D had NEITHER. It was described and never runnable: no
// coverage row, no definition of done, and nothing telling a loop when to stop
// invoking it.
//
// A phase a loop cannot terminate is a phase that either never runs or never
// stops, and on a client repository nobody is watching which.
func TestReverseEngineerSkillMakesPhaseDRunnable(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	for _, want := range []struct{ token, why string }{
		{"D5", "phase D needs its own exit criterion, not only its content"},
		{"once per system", "A–C loop per context; D runs once — a loop must know the difference"},
		{"phase D", "the run order has to name it explicitly or a loop cannot sequence it"},
		{"citations resolve", "a document whose citations do not resolve is not done, however complete it reads"},
	} {
		if !strings.Contains(skill, want.token) {
			t.Errorf("installed rdd-reverse-engineer is missing %q — %s", want.token, want.why)
		}
	}
}

// REQ-CROSS-086 (`RUN:2026-08-13`): two rules the FIRST real phase-D run
// exposed, neither of which the skill had. Both are duplication hazards, and
// both would have produced a confidently wrong artefact:
//
//   - The target repository already had a 303-line ARCHITECTURE.md covering
//     services, schema, inter-service communication and design decisions. Writing
//     docs/03-architecture.md beside it would create exactly the two-answers
//     problem this epic exists to avoid.
//   - MAX_CONDITIONAL_FOLLOWUPS = 3 already has REQ-CON-042. An NFR row for it
//     would be a duplicate wearing a different id.
func TestReverseEngineerSkillRefusesToDuplicateWhatExists(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".claude", "skills", "rdd-reverse-engineer", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(raw)

	// REQ-CROSS-086 records three hazards from the first real phase-D run, all
	// three found in the first ten minutes against a repository nobody wrote the
	// skill for. Two were pinned here; the third was not, and it is the one that
	// fabricates rather than duplicates.
	for _, want := range []struct{ token, why string }{
		{"already covers", "an equivalent document may exist — adopt or extend it, never compete with it"},
		{"already has a requirement", "a threshold may already be a requirement; an NFR row for it is a duplicate"},
		{"read the whole expression", "a grep hit stopping at the line start turned " +
			"`MAX_FILE_SIZE_BYTES = 50 * 1024 * 1024  # 50MB` into \"50 bytes\" — a fabricated " +
			"absurdity in an architecture document costs more trust than the row was worth"},
	} {
		if !strings.Contains(skill, want.token) {
			t.Errorf("installed rdd-reverse-engineer is missing %q — %s", want.token, want.why)
		}
	}
}

// REQ-CROSS-129: `install --check` is what makes the build script able to say
// DRIFT, so every state it treats as drift has to be one it can actually detect.
//
// Damaged markers were covered for Install — which refuses to write — but not for
// Check. That gap matters more than an ordinary edit: with the markers gone the
// managed block can no longer be located, so the next install cannot update it
// and the workspace silently keeps whatever it has. That is the exact
// invisible-staleness this row exists to catch, and it reported clean.
func TestCheckReportsDamagedMarkersAsDrift(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("fresh install must be drift-free, got %v, %v", drift, err)
	}

	path := filepath.Join(root, "AGENTS.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Remove the closing marker: the block's content is untouched, so only the
	// damage detection can notice this.
	damaged := strings.Replace(string(body), EndMarker, "", 1)
	if damaged == string(body) {
		t.Fatalf("end marker not present in the installed file — fixture assumption broken")
	}
	if err := os.WriteFile(path, []byte(damaged), 0o644); err != nil {
		t.Fatal(err)
	}

	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range drift {
		if strings.HasPrefix(d, "AGENTS.md") && strings.Contains(d, "markers damaged") {
			return
		}
	}
	t.Fatalf("damaged markers reported as %v — a workspace whose managed block cannot be located "+
		"reads as up to date, and the next install cannot repair what it cannot find", drift)
}
