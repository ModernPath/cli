package cmd

// REQ-CROSS-277 §277.6 (EPIC-NEXT-003) — RED first. The `brief` hook family:
// one SessionStart entry (marker `your-move --hook`, timeout 15) installed
// beside the sync family, recognised by doctor and uninstall, skippable with
// --no-brief, Claude Code only.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readCfg(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestBriefFamilyInstallsBesideSyncIdempotently(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	if err := installBriefFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	text := readCfg(t, agent.configPath)
	for _, want := range []string{syncHookMarker, briefHookMarker, `"timeout": 15`, "SessionStart"} {
		if !strings.Contains(text, want) {
			t.Fatalf("settings missing %q:\n%s", want, text)
		}
	}
	// the brief entry coexists with the sync family's SessionStart entry
	if strings.Count(text, briefHookMarker) != 1 {
		t.Fatalf("expected exactly one brief entry:\n%s", text)
	}
	if briefFamilyState(agent) != hookStateConfigured {
		t.Fatalf("brief family not configured after install")
	}

	// idempotent: a second install adds nothing
	if err := installBriefFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(readCfg(t, agent.configPath), briefHookMarker); got != 1 {
		t.Fatalf("re-install duplicated brief entries: %d", got)
	}
}

func TestBriefFamilyUninstallRemovesOnlyItself(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installSyncFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	if err := installBriefFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}

	ok, err := uninstallBriefFamilyClaude(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("uninstall reported nothing removed")
	}

	text := readCfg(t, agent.configPath)
	if strings.Contains(text, briefHookMarker) {
		t.Fatalf("brief entry survived uninstall:\n%s", text)
	}
	if !strings.Contains(text, syncHookMarker) {
		t.Fatalf("uninstall removed the sync family too:\n%s", text)
	}
}

func TestBriefFamilyNoBriefSkips(t *testing.T) {
	chdirTemp(t)
	agent := claudeAgent()
	if err := os.MkdirAll(filepath.Dir(agent.configPath), 0o755); err != nil {
		t.Fatal(err)
	}

	hooksNoBrief = true
	defer func() { hooksNoBrief = false }()
	if err := installForAgent(agent); err != nil {
		t.Fatal(err)
	}

	if strings.Contains(readCfg(t, agent.configPath), briefHookMarker) {
		t.Fatal("--no-brief still installed the brief hook")
	}
}
