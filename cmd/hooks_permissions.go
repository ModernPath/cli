package cmd

// Claude Code permission rules for the CLI verbs, shipped with the kit
// (tooling self-sufficiency plan H4, USER:2026-09-12). Every workspace has the
// same verbs, so every workspace gets the same answer to "which of these may an
// agent run without a prompt": the reads and the routine writes are allowed,
// the in-loop decision verbs are allowed because the decision is made in the
// conversation before the verb runs and the subagent guard in the PreToolUse
// adapter keeps them out of delegated passes, and the verbs that change
// system-wide state ask. The kit adds a rule only where the workspace holds
// it in no list; a rule the workspace placed — in any list — stays there.

import (
	"encoding/json"
	"fmt"
	"os"
)

// kitPermissionVerbs lists each verb the kit takes a position on, with the
// list it belongs in. Both invocation forms are ruled, so the installed-binary
// path cannot bypass a rule written for the bare name.
var kitPermissionVerbs = []struct {
	pattern string // the text after the binary name, in Claude Code's Bash() prefix form
	list    string // "allow" or "ask"
}{
	// reads
	{"--version", "allow"}, {"--help", "allow"}, {"help *", "allow"},
	{"process next*", "allow"}, {"process check *", "allow"}, {"process findings list*", "allow"},
	{"working-set pull *", "allow"}, {"working-set check*", "allow"}, {"your-move*", "allow"},
	{"factory status*", "allow"}, {"factory gates *", "allow"}, {"status*", "allow"},
	{"check *", "allow"}, {"coverage *", "allow"}, {"requirements-corpus *", "allow"},
	{"search *", "allow"}, {"read-doc *", "allow"}, {"read-file *", "allow"},
	{"env", "allow"}, {"env test*", "allow"}, {"install --check*", "allow"},
	{"auth *", "allow"},
	// routine agent writes, legality enforced by the server
	{"author requirement *", "allow"}, {"author epic *", "allow"}, {"author update *", "allow"},
	{"author member *", "allow"}, {"author relate *", "allow"}, {"author trace *", "allow"},
	{"author gate *", "allow"}, {"author backlog *", "allow"}, {"feedback *", "allow"},
	{"factory evidence *", "allow"}, {"factory sync*", "allow"},
	{"working-set select *", "allow"}, {"working-set push*", "allow"},
	{"process reconcile *", "allow"}, {"process findings add *", "allow"},
	// in-loop decision verbs: decided in the conversation, attributed by the server
	{"factory answer *", "allow"}, {"author advance *", "allow"},
	{"author gate-withdraw *", "allow"}, {"process findings disposition *", "allow"},
	{"process reapply-entry *", "allow"},
	// system-wide state: always a prompt
	{"factory release *", "ask"}, {"process cascade-mode *", "ask"}, {"process supersede *", "ask"},
	// BACKLOG-TOOL-44: reenter re-approves a reversed decision (material re-entry) —
	// stricter than reapply-entry (allow), so it prompts like supersede.
	{"process reenter *", "ask"},
	// REQ-CROSS-421 (EPIC-CLI-022): a demotion reopens delivered work on a
	// human decision — it prompts like supersede and reenter.
	{"author demote *", "ask"},
	{"migrate *", "ask"}, {"env --set*", "ask"},
}

var kitPermissionPrefixes = []string{"modernpath", ".modernpath/bin/modernpath"}

func kitPermissionRule(prefix, pattern string) string {
	return "Bash(" + prefix + " " + pattern + ")"
}

// kitPermissionRules returns every rule the kit owns, keyed by the list it
// belongs in.
func kitPermissionRules() map[string][]string {
	out := map[string][]string{"allow": nil, "ask": nil}
	for _, v := range kitPermissionVerbs {
		for _, prefix := range kitPermissionPrefixes {
			out[v.list] = append(out[v.list], kitPermissionRule(prefix, v.pattern))
		}
	}
	return out
}

func kitOwnedPermissionRule(rule string) bool {
	for _, v := range kitPermissionVerbs {
		for _, prefix := range kitPermissionPrefixes {
			if rule == kitPermissionRule(prefix, v.pattern) {
				return true
			}
		}
	}
	return false
}

// installPermissionsClaude adds each kit rule that is in none of the
// workspace's lists — allow, ask or deny — to the kit's list for it. A rule the
// workspace already holds stays exactly where it is (USER:2026-09-12: the
// customer's placement wins; the kit's own later repositioning of a verb
// reaches only workspaces that never placed it). Entries are carried as
// found: nothing is trimmed, re-typed or reordered. Reports whether the file
// changed.
func installPermissionsClaude(agent agentConfig) (bool, error) {
	settings, err := readSettingsForMerge(agent.configPath)
	if err != nil {
		return false, err
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	if perms == nil {
		perms = map[string]interface{}{}
	}
	placed := map[string]bool{}
	for _, list := range []string{"allow", "ask", "deny"} {
		for _, r := range stringList(perms[list]) {
			placed[r] = true
		}
	}
	changed := false
	for _, list := range []string{"allow", "ask"} {
		items, _ := perms[list].([]interface{})
		for _, r := range kitPermissionRules()[list] {
			if placed[r] {
				continue
			}
			items = append(items, r)
			placed[r] = true
			changed = true
		}
		if len(items) > 0 {
			perms[list] = items
		}
	}
	if !changed {
		return false, nil
	}
	settings["permissions"] = perms
	return true, writeSettingsFile(agent.configPath, settings)
}

// uninstallPermissionsClaude removes the kit's rules from allow and ask and
// nothing else; a kit rule the workspace placed in deny is a decision and
// stays.
func uninstallPermissionsClaude(agent agentConfig) (bool, error) {
	data, err := os.ReadFile(agent.configPath)
	if err != nil {
		return false, nil
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil || settings == nil {
		return false, nil
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	if perms == nil {
		return false, nil
	}
	removed := false
	for _, list := range []string{"allow", "ask"} {
		items, _ := perms[list].([]interface{})
		var kept []interface{}
		for _, it := range items {
			if s, ok := it.(string); ok && kitOwnedPermissionRule(s) {
				removed = true
				continue
			}
			kept = append(kept, it)
		}
		if len(kept) == 0 {
			delete(perms, list)
		} else {
			perms[list] = kept
		}
	}
	if !removed {
		return false, nil
	}
	if len(perms) == 0 {
		delete(settings, "permissions")
	} else {
		settings["permissions"] = perms
	}
	if err := writeSettingsFile(agent.configPath, settings); err != nil {
		return false, fmt.Errorf("cannot write %s: %w", agent.configPath, err)
	}
	return true, nil
}

// permissionsState reports how many kit rules the workspace has placed, in
// any list, for hooks status.
func permissionsState(agent agentConfig) (present, total int) {
	want := kitPermissionRules()
	total = len(want["allow"]) + len(want["ask"])
	data, err := os.ReadFile(agent.configPath)
	if err != nil {
		return 0, total
	}
	var settings map[string]interface{}
	if json.Unmarshal(data, &settings) != nil {
		return 0, total
	}
	perms, _ := settings["permissions"].(map[string]interface{})
	placed := map[string]bool{}
	for _, list := range []string{"allow", "ask", "deny"} {
		for _, r := range stringList(perms[list]) {
			placed[r] = true
		}
	}
	for _, rules := range want {
		for _, r := range rules {
			if placed[r] {
				present++
			}
		}
	}
	return present, total
}

// stringList reads the string entries of a list as they are; non-string
// entries are the project's business and are neither read nor rewritten.
func stringList(v interface{}) []string {
	items, _ := v.([]interface{})
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
