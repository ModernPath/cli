package cmd

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func readPermissionLists(t *testing.T, path string) (allow, ask []string, raw string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	return stringList(perms["allow"]), stringList(perms["ask"]), string(data)
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// USER:2026-09-12: the customer's placement wins. The kit adds a rule only
// where the workspace holds it in no list; a kit rule the workspace moved —
// here `author advance` deliberately in ask, `factory release` deliberately
// in deny — stays there through every install.
func TestPermissionsMergeAddsOnlyUnplacedRulesAndKeepsTheProjectsPlacement(t *testing.T) {
	agent := gateTestAgent(t)
	seed := `{
  "permissions": {
    "allow": ["Bash(git status*)", " Bash(modernpath factory answer *)", 42],
    "ask": ["Bash(rm -rf *)", "Bash(modernpath author advance *)"],
    "deny": ["Bash(curl *)", "Bash(modernpath factory release *)"]
  },
  "hooks": {}
}
`
	if err := os.WriteFile(agent.configPath, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := installPermissionsClaude(agent)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first merge reported no change")
	}
	allow, ask, raw := readPermissionLists(t, agent.configPath)

	for _, want := range []string{"Bash(git status*)", "Bash(modernpath process next*)", "Bash(.modernpath/bin/modernpath author trace *)"} {
		if !has(allow, want) {
			t.Errorf("allow is missing %q", want)
		}
	}
	for _, want := range []string{"Bash(rm -rf *)", "Bash(modernpath migrate *)", "Bash(.modernpath/bin/modernpath env --set*)"} {
		if !has(ask, want) {
			t.Errorf("ask is missing %q", want)
		}
	}
	// The project's placements survive: ask keeps the moved rule, allow does
	// not gain a duplicate, deny keeps the kit verb and the kit adds no copy.
	if !has(ask, "Bash(modernpath author advance *)") || has(allow, "Bash(modernpath author advance *)") {
		t.Errorf("the project's ask placement of author advance was overridden: allow=%v ask=%v", allow, ask)
	}
	if has(allow, "Bash(modernpath factory release *)") || has(ask, "Bash(modernpath factory release *)") {
		t.Error("a kit verb the project placed in deny was re-added to allow or ask")
	}
	// Entries are carried as found: the odd string and the non-string entry.
	if !strings.Contains(raw, `" Bash(modernpath factory answer *)"`) || !strings.Contains(raw, "42") {
		t.Errorf("project entries were rewritten:\n%s", raw)
	}
	if allow[0] != "Bash(git status*)" || ask[0] != "Bash(rm -rf *)" {
		t.Errorf("project rules were reordered: allow[0]=%q ask[0]=%q", allow[0], ask[0])
	}
	if !strings.Contains(raw, "Bash(curl *)") {
		t.Error("the project's deny list was lost")
	}
	if strings.Index(raw, `"permissions"`) > strings.Index(raw, `"hooks"`) {
		t.Error("top-level key order was not preserved")
	}

	// Idempotent.
	if changed, err = installPermissionsClaude(agent); err != nil || changed {
		t.Fatalf("second merge: changed=%v err=%v", changed, err)
	}
	// Placed anywhere counts as in place — the untrimmed factory answer rule
	// is a different string and so is not "placed"; it was added once.
	present, total := permissionsState(agent)
	if present != total {
		t.Fatalf("status reports %d/%d rules after install", present, total)
	}

	// Uninstall removes the kit's rules from allow and ask, never from deny.
	removed, err := uninstallPermissionsClaude(agent)
	if err != nil || !removed {
		t.Fatalf("uninstall: removed=%v err=%v", removed, err)
	}
	allow, ask, raw = readPermissionLists(t, agent.configPath)
	if !has(allow, "Bash(git status*)") || has(allow, "Bash(modernpath process next*)") {
		t.Errorf("uninstall left allow=%v", allow)
	}
	if !has(ask, "Bash(rm -rf *)") || has(ask, "Bash(modernpath migrate *)") || has(ask, "Bash(modernpath author advance *)") {
		t.Errorf("uninstall left ask=%v", ask)
	}
	if !strings.Contains(raw, "Bash(modernpath factory release *)") {
		t.Error("uninstall removed a kit verb the project had placed in deny")
	}
}

func TestPermissionsMergeOnAFreshWorkspace(t *testing.T) {
	// No settings file at all — the path every new workspace takes first.
	agent := gateTestAgent(t)
	changed, err := installPermissionsClaude(agent)
	if err != nil || !changed {
		t.Fatalf("fresh install: changed=%v err=%v", changed, err)
	}
	present, total := permissionsState(agent)
	if present != total || total == 0 {
		t.Fatalf("fresh install placed %d/%d rules", present, total)
	}
	// A settings file with no permissions key.
	agent2 := gateTestAgent(t)
	if err := os.WriteFile(agent2.configPath, []byte("{\n  \"hooks\": {}\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if changed, err := installPermissionsClaude(agent2); err != nil || !changed {
		t.Fatalf("no-permissions install: changed=%v err=%v", changed, err)
	}
	if _, _, raw := readPermissionLists(t, agent2.configPath); strings.Index(raw, `"hooks"`) > strings.Index(raw, `"permissions"`) {
		t.Errorf("a new key was not appended after the existing ones:\n%s", raw)
	}
	// Uninstall on a workspace that never had them reports nothing removed.
	agent3 := gateTestAgent(t)
	if removed, err := uninstallPermissionsClaude(agent3); err != nil || removed {
		t.Fatalf("uninstall on a fresh workspace: removed=%v err=%v", removed, err)
	}
}

func TestKitPermissionRulesCoverEveryStoreWriteVerbTheGuardKnows(t *testing.T) {
	// Every verb the subagent guard denies is one the kit has taken a position
	// on for the main session, so no store write is left to the classifier.
	rules := kitPermissionRules()
	all := append(append([]string{}, rules["allow"]...), rules["ask"]...)
	for _, verb := range []string{
		"author requirement", "author trace", "author gate", "author advance", "author gate-withdraw",
		"factory answer", "factory evidence", "factory sync", "factory release",
		"working-set select", "working-set push",
		"process reconcile", "process findings add", "process findings disposition",
		"process supersede", "process reenter", "process cascade-mode", "migrate", "env --set",
		// REQ-CROSS-421 (EPIC-CLI-022): a demotion reopens delivered work.
		"author demote",
	} {
		found := false
		for _, r := range all {
			if strings.Contains(r, "Bash(modernpath "+verb) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no kit rule for the store-write verb %q", verb)
		}
		if !storeWriteCommand("modernpath " + verb + " x") {
			t.Errorf("the guard does not recognise %q as a store write", verb)
		}
	}
}
