package cmd

import (
	"os"
	"strings"
	"testing"
)

// RUN:2026-09-12, two workspaces: `hooks install` rewrote .claude/settings.json
// with `>` escaped as \u003e in every hook command, moved autoMode to the top
// of the file, and dropped a project-owned second command that sat beside the
// gate command in the same PreToolUse matcher group. These pin the fix: the
// project's bytes survive an install.
const projectSettings = `{
  "hooks": {
    "PreToolUse": [
      {
        "hooks": [
          {
            "command": "command -v modernpath >/dev/null 2>&1 && modernpath check --hook PreToolUse 2>/dev/null || printf '{}'",
            "timeout": 30,
            "type": "command"
          },
          {
            "type": "command",
            "command": "\"${CLAUDE_PROJECT_DIR:-.}\"/.claude/hooks/project-audit.sh",
            "timeout": 10
          }
        ],
        "matcher": "Bash"
      }
    ]
  },
  "permissions": {
    "allow": ["Bash(modernpath process next*)"]
  },
  "autoMode": {
    "environment": ["$defaults", "a & b > c"]
  }
}
`

func TestGateInstallKeepsProjectOwnedHookBesideItsOwn(t *testing.T) {
	agent := gateTestAgent(t)
	if err := os.WriteFile(agent.configPath, []byte(projectSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(agent.configPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)

	if !strings.Contains(text, "project-audit.sh") {
		t.Fatalf("the project-owned guard command was dropped:\n%s", text)
	}
	if strings.Count(text, gateHookMarker) != 1 {
		t.Fatalf("expected exactly one gate command, got %d:\n%s", strings.Count(text, gateHookMarker), text)
	}
	if strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Fatalf("hook commands were HTML-escaped:\n%s", text)
	}
	if !strings.Contains(text, "a & b > c") {
		t.Fatalf("a project string outside hooks was altered:\n%s", text)
	}
	hooksAt := strings.Index(text, `"hooks"`)
	permsAt := strings.Index(text, `"permissions"`)
	autoAt := strings.Index(text, `"autoMode"`)
	if !(hooksAt < permsAt && permsAt < autoAt) {
		t.Fatalf("top-level key order changed (hooks=%d permissions=%d autoMode=%d):\n%s", hooksAt, permsAt, autoAt, text)
	}

	entries := preToolUseEntries(t, agent.configPath)
	if len(entries) != 1 {
		t.Fatalf("expected the guard salvaged into the single gate entry, got %d entries", len(entries))
	}

	// A second install is byte-stable.
	if err := installGateFamilyClaude(agent); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(agent.configPath)
	if string(again) != text {
		t.Fatalf("second install changed the file:\n--- first\n%s\n--- second\n%s", text, again)
	}
}

func TestWriteSettingsFileFallsBackToSortedKeysForANewFile(t *testing.T) {
	path := t.TempDir() + "/settings.json"
	if err := writeSettingsFile(path, map[string]interface{}{"zeta": 1, "alpha": "x > y"}); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(path)
	text := string(out)
	if strings.Index(text, `"alpha"`) > strings.Index(text, `"zeta"`) {
		t.Fatalf("new keys are not sorted:\n%s", text)
	}
	if !strings.Contains(text, "x > y") {
		t.Fatalf("value was escaped:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Fatalf("file does not end with a newline:\n%q", text)
	}
}
