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
	"strings"
)

// storeWriteCommandRE matches a store-write verb wherever a shell would run
// it: after a separator, an env-assignment prefix, `command`/`env`, a path,
// and with the root flags in between. Everything else — every read, `auth
// status`, the interactive sign-in — passes. The dry-run forms of write verbs
// (`process reconcile` without --apply, `factory pull` without --apply) count
// as writes: a delegated pass has no business on that surface.
const storeWritePattern = `(^|[\s;&|()])\\?(\S*/)?modernpath\s+(-v\s+|--verbose\s+|--api-url(=\S+|\s+\S+)\s+)*(` +
	`author\s|` +
	`feedback(\s|$)|` +
	`reverse-engineer\s+(authorize|capture-source|publish|decide)(\s|$)|` +
	`factory\s+(answer|evidence|sync|release|connect|pin|pull)|` +
	`working-set\s+(select|push)|` +
	`process\s+(reconcile|findings\s+(add|disposition)|supersede|cascade-mode|reapply-entry|reenter)|` +
	`migrate|env\s+--set|docs\s+push|import\s|new\s)`

var storeWriteCommandRE = regexp.MustCompile(storeWritePattern)

// isHelpWord is the exact spelling of a help request. `--help=…` and
// `-hsomething` are not one.
func isHelpWord(tok string) bool { return tok == "--help" || tok == "-h" }

// helpRequested reports whether one simple command asks a write verb for its
// help rather than running it: `author gate --help` prints help and writes
// nothing, so denying it left a delegated pass unable to read the very surface
// it is told to use (BACKLOG-TOOL-6, BACKLOG-TOOL-105).
//
// The word must be an argument in its own right. pflag takes the NEXT argv as
// a flag's value even when that value starts with a dash, so
// `author gate X --title -h` sets the title to "-h" and writes the gate — it
// is not a help request, and reading it as one let a real write through (PR
// #624 cold review, round 2). A help word directly after any other dash token
// is therefore read as that flag's value — unless that token already carries
// its value inline (`--title=t`), which consumes nothing further. That is
// deliberately conservative in one direction: after a boolean flag
// (`--apply --help`) the help word IS a help word, and this denies it.
// Denying a help call costs a reordering; allowing a write costs the store.
//
// The quote-aware scanner has already replaced quoted spans, so
// `author backlog --title "x --help"` is prose and stays denied.
func helpRequested(simple string) bool {
	tokens := strings.Fields(simple)
	for i, tok := range tokens {
		if i == 0 || !isHelpWord(tok) {
			continue
		}
		if prev := tokens[i-1]; valueTakingFlag(prev) {
			continue
		}
		return true
	}
	return false
}

// inlineValueFlag is a flag that already holds its value: `--title=t`, `-t=x`.
// The next word is a new argument, not this flag's value.
var inlineValueFlag = regexp.MustCompile(`^--?[^=]+=.+`)

// valueTakingFlag reports a token that would swallow the word after it.
func valueTakingFlag(tok string) bool {
	return strings.HasPrefix(tok, "-") && !isHelpWord(tok) && !inlineValueFlag.MatchString(tok)
}

// commandSeparators splits a span a shell would execute into its simple
// commands. `&&`, `;`, a pipe, a subshell boundary and a newline all end one,
// so each piece is judged on its own.
var commandSeparators = regexp.MustCompile(`[;&|()\n]`)

// storeWriteMatcher is the guard's verdict for one executable span: a store
// write that is NOT a help request. It judges each simple command separately,
// because a whole-span "is there a write?" and a whole-span "is there a help
// flag?" can be satisfied by two DIFFERENT commands — `author trace T
// --verdict PASS && modernpath author --help` would then read as help and the
// real write would pass. The guard exists because a delegated agent
// reformulated past refusals until one went through (see the file header), so
// `&&`, `;`, a newline and a command substitution are all reformulations to
// close, not gaps to accept.
type storeWriteMatcher struct{}

func (storeWriteMatcher) MatchString(span string) bool {
	for _, simple := range commandSeparators.Split(span, -1) {
		if storeWriteCommandRE.MatchString(simple) && !helpRequested(simple) {
			return true
		}
	}
	return false
}

// storeWriteCommand reads the command the way the commit gate does: chains,
// wrappers and shell -c bodies are commands; quoted prose is not. A help
// request for a write verb is a read, not a write — but only for the
// invocation that asks for it. The scanner recurses into a `$(…)` body and a
// shell's -c body, and each of those is segmented the same way.
func storeWriteCommand(command string) bool {
	return executableMatches(command, 0, storeWriteMatcher{})
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
