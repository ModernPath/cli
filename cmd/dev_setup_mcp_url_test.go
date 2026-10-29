// SR-MCP-001-009 (EPIC-MCP-001): `modernpath dev setup` writes ModernPath's
// one MCP address. On a platform API host core is reached through its
// `/api/ex` prefix, so the address is https://<api-host>/api/ex/mcp. On the
// local stack it is the local gateway's http://127.0.0.1:4123/api/ex/mcp:
// core's metadata names that address as the resource, so an assistant signs
// in only there. Anywhere else it is <api_url>/api/mcp. The address names no
// workspace: the assistant signs in itself and the token names the
// workspace.
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modernpath/cli/internal/config"
	"github.com/modernpath/cli/internal/zitadel"
)

func readServerURL(t *testing.T, path string, keys ...string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s must exist: %v", path, err)
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		t.Fatalf("%s must be valid JSON: %v", path, err)
	}
	for _, key := range keys {
		m, ok := node.(map[string]any)
		if !ok {
			t.Fatalf("%s: no object at %q in %s", path, key, raw)
		}
		node = m[key]
	}
	url, _ := node.(string)
	return url
}

func TestDevSetupWritesMCPAddress(t *testing.T) {
	cases := []struct {
		apiURL string
		want   string
	}{
		{zitadel.ProdProfile.APIURL, strings.TrimRight(zitadel.ProdProfile.APIURL, "/") + "/api/ex/mcp"},
		{zitadel.TestProfile.APIURL, strings.TrimRight(zitadel.TestProfile.APIURL, "/") + "/api/ex/mcp"},
		{"http://localhost:4000", "http://127.0.0.1:4123/api/ex/mcp"},
		{"http://127.0.0.1:4000/", "http://127.0.0.1:4123/api/ex/mcp"},
		{"https://core.example.test", "https://core.example.test/api/mcp"},
	}

	for _, tc := range cases {
		t.Run(tc.apiURL, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			// OpenCode writes a project opencode.json when one exists, else the
			// user's global config; keep it in the test directory.
			if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}

			cfg := &config.Config{APIURL: tc.apiURL}
			for name, setup := range map[string]func(*config.Config) error{
				"claude": setupClaudeMCP, "cursor": setupCursorMCP, "opencode": setupOpenCodeMCP,
			} {
				if err := setup(cfg); err != nil {
					t.Fatalf("%s: %v", name, err)
				}
			}

			got := map[string]string{
				"claude":   readServerURL(t, filepath.Join(dir, ".mcp.json"), "mcpServers", "modernpath", "url"),
				"cursor":   readServerURL(t, filepath.Join(dir, ".cursor", "mcp.json"), "mcpServers", "modernpath", "url"),
				"opencode": readServerURL(t, filepath.Join(dir, "opencode.json"), "mcp", "modernpath", "url"),
			}
			for name, url := range got {
				if url != tc.want {
					t.Errorf("%s names %q, want %q", name, url, tc.want)
				}
			}
		})
	}
}

// The tools an MCP client may run, mirrored from core's
// `Core.MCP.External` @external_surface (SR-MCP-001-005).
var mcpSurfaceTools = []string{
	"start_conversation", "list_conversations", "chat", "list_epics", "get_epic",
	"query_requirements", "trace_requirement", "chat_result", "search_epics",
	"search_requirements", "context_search", "list_systems",
}

func TestDevPromptNamesOnlyMCPTools(t *testing.T) {
	prompt := buildDevPrompt("do the task", &config.Config{})

	named := 0
	for _, field := range strings.Split(prompt, "`") {
		if strings.ContainsAny(field, " \n") || field == "" {
			continue
		}
		named++
		found := false
		for _, tool := range mcpSurfaceTools {
			if field == tool {
				found = true
			}
		}
		if !found {
			t.Errorf("the prompt names %q, which is not on the MCP list", field)
		}
	}
	if named == 0 {
		t.Fatalf("the prompt names no tool: %q", prompt)
	}
}

// Claude Code signs in only through a pre-registered app: neither ZITADEL nor
// the local mock offers dynamic client registration. `dev setup claude`
// writes the app's client id where one exists, and locally the callback port
// the mock's client registers.
func TestSetupClaudeWritesTheAssistantClient(t *testing.T) {
	cases := []struct {
		apiURL string
		oauth  map[string]any
	}{
		{zitadel.TestProfile.APIURL, map[string]any{"clientId": "397099556791124142"}},
		{"http://localhost:4000", map[string]any{"clientId": "modernpath-local-claude", "callbackPort": float64(4153)}},
		// No assistant app is registered on production yet.
		{zitadel.ProdProfile.APIURL, nil},
	}

	for _, tc := range cases {
		t.Run(tc.apiURL, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)

			if err := setupClaudeMCP(&config.Config{APIURL: tc.apiURL}); err != nil {
				t.Fatal(err)
			}

			server := readMCPFile(t, dir)["mcpServers"].(map[string]any)["modernpath"].(map[string]any)
			oauth, _ := server["oauth"].(map[string]any)
			if fmt.Sprint(oauth) != fmt.Sprint(tc.oauth) {
				t.Fatalf("oauth = %#v, want %#v", server["oauth"], tc.oauth)
			}
		})
	}
}
