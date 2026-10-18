package kit

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Each Claude Code agent definition ships a Codex rendering, kept equal to
// what codexAgentTOML makes of it; install writes both.
func TestCodexAgentsMatchTheirClaudeSource(t *testing.T) {
	for _, name := range []string{"rdd-cold-reviewer", "rdd-context-deriver"} {
		md, err := assets.ReadFile("assets/agents/" + name + ".md")
		if err != nil {
			t.Fatal(err)
		}
		want, err := codexAgentTOML(md)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		got, err := assets.ReadFile("assets/agents-codex/" + name + ".toml")
		if err != nil {
			t.Fatalf("the kit must embed the Codex rendering of %s: %v", name, err)
		}
		if !bytes.Equal(want, got) {
			if os.Getenv("UPDATE_CODEX_AGENTS") == "1" {
				_ = os.WriteFile(filepath.Join("assets/agents-codex", name+".toml"), want, 0o644)
				continue
			}
			t.Errorf("assets/agents-codex/%s.toml is stale; rerun with UPDATE_CODEX_AGENTS=1", name)
		}
	}

	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rdd-cold-reviewer", "rdd-context-deriver"} {
		if _, err := os.Stat(filepath.Join(root, ".codex/agents", name+".toml")); err != nil {
			t.Errorf("install must write .codex/agents/%s.toml: %v", name, err)
		}
	}
	reviewer, _ := os.ReadFile(filepath.Join(root, ".codex/agents/rdd-cold-reviewer.toml"))
	if !bytes.Contains(reviewer, []byte("shell_tool = false")) {
		t.Error("the Codex reviewer, like the Claude one, has no shell")
	}
}
