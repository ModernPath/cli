package cmd

// The subagent store-write guard, folded into the PreToolUse adapter every
// workspace already installs (`modernpath check --hook PreToolUse`), so no
// workspace needs a script of its own and no drifted working directory can
// make it fall open.
//
// Why it exists: a background subagent, told to finish its task, met two
// permission refusals and reformulated each call until one went through; the
// trace it recorded landed on a production store (RUN:2026-09-12). The rule —
// a delegated pass returns findings and a verdict, the orchestrating session
// records them — is in the canonical process (PROCESS.md, Delegated passes).
// This is its mechanical half: when the harness marks the caller as a
// subagent and the command is a store write, the call is denied with the rule
// as the reason.
//
// Flag-first and fail-open: with no identity in the payload nothing changes,
// and a read is never denied.

import (
	"encoding/json"
	"regexp"
)

// storeWriteCommandRE matches a store-write verb wherever a shell would run
// it: after a separator, an env-assignment prefix, `command`/`env`, a path,
// and with the root flags in between. Everything else — every read, `auth
// status`, the interactive sign-in — passes. The dry-run forms of write verbs
// (`process reconcile` without --apply, `factory pull` without --apply) count
// as writes: a delegated pass has no business on that surface.
var storeWriteCommandRE = regexp.MustCompile(
	`(^|[\s;&|()])\\?(\S*/)?modernpath\s+(-v\s+|--verbose\s+|--api-url(=\S+|\s+\S+)\s+)*(` +
		`author\s|` +
		`feedback(\s|$)|` +
		`reverse-engineer\s+(authorize|capture-source|publish|decide)(\s|$)|` +
		`factory\s+(answer|evidence|sync|release|connect|pin|pull)|` +
		`working-set\s+(select|push)|` +
		`process\s+(reconcile|findings\s+(add|disposition)|supersede|cascade-mode|reapply-entry|reenter)|` +
		`migrate|env\s+--set|docs\s+push|import\s|new\s)`)

// storeWriteCommand reads the command the way the commit gate does: chains,
// wrappers and shell -c bodies are commands; quoted prose is not.
func storeWriteCommand(command string) bool {
	return executableMatches(command, 0, storeWriteCommandRE)
}

// subagentMarker returns the delegated agent's identity, or "" for a
// main-session call. Only an identity field counts — agent_id (what Claude
// Code sets inside a subagent, verified on live traffic 2026-09-12) or
// subagent_id; agent_type is a label, appended for the reason text, never a
// marker on its own, so a main session running under a configured default
// agent cannot be mistaken for a delegated pass.
func subagentMarker(payload []byte) string {
	var input struct {
		AgentID    string `json:"agent_id"`
		SubagentID string `json:"subagent_id"`
		AgentType  string `json:"agent_type"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return ""
	}
	id := input.AgentID
	if id == "" {
		id = input.SubagentID
	}
	if id == "" {
		return ""
	}
	if input.AgentType != "" {
		return id + " (" + input.AgentType + ")"
	}
	return id
}

func subagentGuardEnvelope(marker string) string {
	envelope := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":      "PreToolUse",
			"permissionDecision": "deny",
			"permissionDecisionReason": "store writes run only from the orchestrating session; this call came from " +
				"delegated agent " + marker + " — return the finding or verdict and let the orchestrating " +
				"session record it (PROCESS.md, Delegated passes)",
		},
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		return "{}"
	}
	return string(out)
}
