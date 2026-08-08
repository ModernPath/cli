package kit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// REQ-CROSS-029: install writes the tool-owned files wholesale and merges only
// its managed block into the client's AGENTS.md.
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
		".claude/rdd/PROCESS.md",
		".claude/rdd/platform.md",
		".claude/skills/rdd-build-loop/SKILL.md",
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
	if !strings.Contains(got, ".claude/rdd/PROCESS.md") {
		t.Fatal("the managed block must point non-Claude agents at the process")
	}
	if res.AgentsCreated {
		t.Error("AGENTS.md existed; it should be reported as merged, not created")
	}

	// The root CLAUDE.md must import the process, or nothing loads at session start.
	claude, _ := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if !strings.Contains(string(claude), "@.claude/rdd/PROCESS.md") {
		t.Error("CLAUDE.md must @-import the process")
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

// REQ-CROSS-029: `.claude/rdd/PROCESS.md` is the file a person naturally opens
// — it is the one they read. Editing it is silently reverted by the next
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

	edited := filepath.Join(root, ".claude/rdd/PROCESS.md")
	body, _ := os.ReadFile(edited)
	os.WriteFile(edited, append(body, []byte("\n\nlocal edit that install would revert\n")...), 0o644)

	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0] != ".claude/rdd/PROCESS.md" {
		t.Fatalf("expected the edited file to be reported, got %v", drift)
	}

	// A deleted tool-owned file is drift too — it means the repo is missing
	// part of the process it claims to follow.
	os.Remove(filepath.Join(root, ".claude/skills/rdd-ledger/SKILL.md"))
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
	theirs := "# SAMPO — Development Process\n\n## 1. The non-negotiables\n\n" +
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
	if !strings.Contains(got, "@.claude/rdd/PROCESS.md") {
		t.Fatal("the process import must still be added")
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
