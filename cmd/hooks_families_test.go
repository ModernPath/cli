package cmd

// Two failure shapes these tests pin:
//
//	1 — `hooks install --claude` in a repo that had only run `modernpath
//	     install` printed "not initialized" and exited 0 with no settings
//	     entry, so the REQ-CROSS-030 gate never armed for the process-only
//	     audience. The gate runs `modernpath check`, which needs no server.
//	2 — `hooks uninstall` detected sync (and context) by a script file this
//	     version never writes, so it removed neither, and it removed the gate
//	     without saying so. The report read "No ModernPath hooks found to
//	     remove" over a settings file still full of them.

import (
	"bytes"
	"os"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

func hooksTestCmd() *cobra.Command { return &cobra.Command{} }

// selectClaudeOnly points the install/uninstall commands at Claude Code and
// restores the package-level flags afterwards.
func selectClaudeOnly(t *testing.T) agentConfig {
	t.Helper()
	hooksClaude = true
	t.Cleanup(func() { hooksClaude = false })

	agent := hookAgents["claude"]
	if err := os.MkdirAll(agent.hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return agent
}

// captureCLIOutput redirects the print helpers (which write through
// fatih/color's package-level writer) and returns a reader for what was said.
func captureCLIOutput(t *testing.T) func() string {
	t.Helper()
	var buf bytes.Buffer
	prev := color.Output
	prevErr := color.Error
	color.Output = &buf
	// Warnings and errors ride stderr since REQ-CROSS-210 (D13: --json stdout
	// purity); the CLI's "what was said" is both streams together.
	color.Error = &buf
	t.Cleanup(func() {
		color.Output = prev
		color.Error = prevErr
	})
	return buf.String
}

func settingsText(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no settings written at %s: %v", path, err)
	}
	return string(raw)
}

// ── F2: the gate arms without a server binding ────────────────────────────

// Exiting 0 when nothing could be installed is deliberate; the defect this
// pins against was *silence*, not the exit code.
// Nothing is broken when a user asks Cursor for a gate that is Claude-only, so
// the command explains and exits 0 — a non-zero exit would fail every CI step
// and setup script that installs hooks best-effort.
// ── F3: uninstall removes every family and says which ─────────────────────

// ── a settings file the installer cannot parse is not an empty one ─────────

// Every family installer merges into .claude/settings.json by read → unmarshal
// → rewrite. Treating an unparseable file as empty turns one stray comma in a
// hand-edited file into the silent loss of the customer's entire configuration.
// ── uninstall must not claim a removal it could not persist ────────────────

// ── the Claude context entry must be shell form ────────────────────────────

// Claude Code selects execution form by the presence of an "args" key: with
// it, the command string is posix_spawned as a literal executable name; only
// without it does the string reach a shell. The context command is a shell
// pipeline (`command -v … && … || …`), so an entry carrying "args" fails with
// ENOENT on every prompt instead of injecting context.
// ── status asks the settings, not the filesystem ───────────────────────────

// The old probe asked the filesystem for a script the install deliberately
// never writes, so a correctly wired repository reported every family absent.
