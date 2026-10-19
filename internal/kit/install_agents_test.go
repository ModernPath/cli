package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// REQ-CROSS-409 (EPIC-CLI-020): the kit ships the reviewer agent definition —
// the delegated cold review's read-only context — and installs it in both
// modes; an edited or missing copy is drift, like a skill.

const reviewerAgentAsset = "assets/agents/rdd-cold-reviewer.md"
const reviewerAgentTarget = ".claude/agents/rdd-cold-reviewer.md"

// S1 — install writes the file equal to the asset.
func TestInstallWritesTheReviewerAgentDefinition(t *testing.T) {
	want, err := assets.ReadFile(reviewerAgentAsset)
	if err != nil {
		t.Fatalf("the kit must embed %s: %v", reviewerAgentAsset, err)
	}
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, reviewerAgentTarget))
	if err != nil {
		t.Fatalf("install must write %s: %v", reviewerAgentTarget, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s must equal the asset", reviewerAgentTarget)
	}
	if !bytes.Contains(got, []byte("tools: Read, Grep, Glob")) || bytes.Contains(got, []byte("Bash")) {
		t.Fatalf("the reviewer carries Read, Grep, Glob and no shell:\n%s", got)
	}

	// Store-backed too: the reviewer is not the ledger skill.
	storeBacked := t.TempDir()
	if err := os.MkdirAll(filepath.Join(storeBacked, "process"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeBacked, "process", "store-backed.md"), []byte("# Store-backed declaration\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install(storeBacked); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(storeBacked, reviewerAgentTarget)); err != nil {
		t.Fatalf("a store-backed workspace gets the reviewer too: %v", err)
	}
}

// S2 — an edited copy is drift; a deleted one is drift.
func TestCheckReportsAnEditedOrMissingReviewerAgent(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	if drift, err := Check(root); err != nil || len(drift) != 0 {
		t.Fatalf("a fresh install must be clean: drift=%v err=%v", drift, err)
	}
	path := filepath.Join(root, reviewerAgentTarget)
	body, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(body, []byte("\ntools: Bash\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	drift, err := Check(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(drift) != 1 || drift[0] != reviewerAgentTarget {
		t.Fatalf("an edited reviewer must be reported as drift, got %v", drift)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	drift, _ = Check(root)
	if len(drift) != 1 || drift[0] != reviewerAgentTarget {
		t.Fatalf("a missing reviewer must be reported as drift, got %v", drift)
	}
}
