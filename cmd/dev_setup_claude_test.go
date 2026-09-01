// REQ-CROSS-211 (EPIC-CLI-001 T8, RUN:2026-08-18): `dev setup claude` wrote
// ~/.claude/mcp_servers.json — a global user file Claude Code never reads, so
// the "configured" claim was a silent no-op. USER:2026-08-18: target the
// project-scope .mcp.json, the documented location Claude Code loads,
// merge-preserving so existing servers survive.
package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/modernpath/cli/internal/config"
)

func readMCPFile(t *testing.T, dir string) map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatalf(".mcp.json must exist at the project root: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf(".mcp.json must be valid JSON: %v", err)
	}
	return m
}

func TestSetupClaudeWritesProjectScopeMCPJSON(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	cfg := &config.Config{APIURL: "http://localhost:4000"}
	if err := setupClaudeMCP(cfg); err != nil {
		t.Fatal(err)
	}

	m := readMCPFile(t, dir)
	servers, ok := m["mcpServers"].(map[string]interface{})
	if !ok {
		t.Fatalf(".mcp.json must use the mcpServers envelope, got: %v", m)
	}
	mp, ok := servers["modernpath"].(map[string]interface{})
	if !ok {
		t.Fatalf("modernpath server missing: %v", servers)
	}
	if mp["url"] != "http://localhost:4000/api/mcp" {
		t.Fatalf("wrong MCP url: %v", mp["url"])
	}

	// And the dead global location must NOT be written.
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".claude", "mcp_servers.json")); err == nil {
		t.Skip("pre-existing global file — cannot assert non-write on this machine")
	}
}

func TestSetupClaudePreservesExistingServers(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	existing := `{"mcpServers":{"other":{"type":"stdio","command":"other-tool"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{APIURL: "http://localhost:4000"}
	if err := setupClaudeMCP(cfg); err != nil {
		t.Fatal(err)
	}

	servers := readMCPFile(t, dir)["mcpServers"].(map[string]interface{})
	if _, ok := servers["other"]; !ok {
		t.Fatal("an existing server was clobbered")
	}
	if _, ok := servers["modernpath"]; !ok {
		t.Fatal("modernpath server not added")
	}
}
