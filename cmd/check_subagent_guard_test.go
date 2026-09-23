package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func hookPayload(marker map[string]string, command string) []byte {
	p := map[string]interface{}{"tool_name": "Bash", "tool_input": map[string]string{"command": command}}
	for k, v := range marker {
		p[k] = v
	}
	out, _ := json.Marshal(p)
	return out
}

func TestSubagentStoreWriteIsDeniedThroughTheGateAdapter(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123", "agent_type": "rdd-cold-reviewer"}
	for _, cmd := range []string{
		"modernpath author trace CR-1 --purpose cold-review --verdict PASS",
		"modernpath author gate X --title t",
		"modernpath feedback \"the verb has no --scope\"",
		"modernpath feedback",
		"modernpath reverse-engineer authorize --file grant.json",
		"modernpath reverse-engineer capture-source --run r --repository catalog --root .",
		"modernpath reverse-engineer publish --run r --group g --file group.json",
		"modernpath reverse-engineer decide --file decision.json",
		"modernpath factory answer G --options approve",
		"modernpath factory evidence --pass REQ-1",
		"modernpath working-set select EPIC-1 --kind epic",
		"modernpath working-set push",
		"modernpath process reconcile --apply",
		"modernpath process findings add --id F1",
		"modernpath process findings disposition --id F1 --disposition RESOLVED",
		// REQ-CROSS-413: a USER-attested re-pin of an applied entry gate — a
		// subagent must never attest a human decision.
		"modernpath process reapply-entry EPIC-1 --decision USER:2026-09-14:x",
		// BACKLOG-TOOL-44: material re-entry re-approves a reversed decision —
		// denied to a subagent, open and apply alike.
		"modernpath process reenter EPIC-1",
		"modernpath process reenter EPIC-1 --apply",
		// REQ-CROSS-421 (EPIC-CLI-022): a demotion is a human decision about
		// delivered work — denied to a subagent, open and apply alike.
		"modernpath author demote REQ-1 --to IN_PROGRESS --basis defect --reason USER:2026-09-21:x",
		"modernpath author demote REQ-1 --apply",
		".modernpath/bin/modernpath author advance REQ-1 --to DONE",
		"  modernpath migrate run",
		// the shapes a reformulating subagent emits next
		"cd /repo && modernpath author trace T --verdict PASS",
		"modernpath process next && modernpath author trace T --verdict PASS",
		"modernpath process next; modernpath factory answer G --options approve",
		"FOO=1 modernpath author trace T",
		"command modernpath author trace T",
		"env FOO=1 modernpath author trace T",
		"modernpath --verbose author trace T",
		"modernpath -v author trace T",
		"modernpath --api-url https://x author trace T",
		"./.modernpath/bin/modernpath author trace T",
		"/usr/local/bin/modernpath author trace T",
		`sh -c "modernpath author trace T"`,
		"bash -lc 'modernpath working-set push'",
		"echo $(modernpath author trace T)",
		"(modernpath process reconcile --apply)",
		"modernpath process reconcile",
		// BACKLOG-TOOL-6/105: a --help word inside quotes is prose the write
		// carries, not a help request — the invocation still writes.
		`modernpath author backlog --title "x --help"`,
		`modernpath author backlog --title 'see --help'`,
		"modernpath author gate X --title t",
		// PR #624 cold review, BLOCKER: the help word only ever excuses the
		// invocation that carries it. A real write in one simple command and a
		// help call in another is still a write, whatever joins them — and a
		// help call inside a command substitution excuses nothing at all.
		"modernpath author trace T --verdict PASS && modernpath author --help",
		"modernpath author gate X --title t; modernpath author --help",
		"modernpath author gate X --title t\nmodernpath author --help",
		"modernpath author gate X --title t | tee log; modernpath author -h",
		`modernpath author trace T --body "$(modernpath author --help)"`,
		"modernpath author --help && modernpath author trace T --verdict PASS",
		// PR #624 cold review round 2: pflag takes the next argv as a flag's
		// value even when it starts with a dash, so these set a field to "-h"
		// or "--help" and write. They are not help requests.
		"modernpath author gate X --title -h",
		"modernpath author gate X --title --help",
		"modernpath author update REQ-1 --detail -h --expected-fingerprint abc --source USER:x",
		// A separator inside a quoted value is data: the value cannot split the
		// command into a harmless-looking second half.
		`modernpath author gate --title "a; b" X`,
	} {
		out := processGateHookPayload(hookPayload(sub, cmd))
		if !strings.Contains(out, `"permissionDecision":"deny"`) || !strings.Contains(out, "abc123") {
			t.Errorf("subagent %q was not denied: %s", cmd, out)
		}
	}
}

func TestSubagentReadsAndMainSessionWritesPass(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath process next",
		"modernpath process check --phase build",
		"modernpath process findings list",
		"modernpath working-set pull REQ-1",
		"modernpath factory gates G",
		"modernpath auth status",
		"modernpath reverse-engineer preflight",
		"modernpath reverse-engineer preview --file selection.json",
		"modernpath reverse-engineer candidates",
		"modernpath reverse-engineer inventory --repository catalog=.",
		"modernpath --version",
		"grep 'modernpath author' docs/x.md",
		`echo "modernpath author trace"`,
		"modernpath process next && grep 'modernpath author' docs/x.md",
		"go test ./...",
		"git commit -m 'modernpath author trace'",
		// BACKLOG-TOOL-6/105: reading a write verb's own help writes nothing,
		// so the guard must not deny a delegated pass its documentation.
		"modernpath author gate --help",
		"modernpath author create -h",
		"modernpath author --help",
		"modernpath feedback --help",
		"modernpath working-set select --help",
		"modernpath author demote REQ-1 --help",
		"modernpath help author gate",
		// PR #624 cold review: Go's $ is end-of-text, so a trailing newline —
		// what a heredoc or an editor-composed command carries — must not
		// re-deny a genuine help call.
		"modernpath author gate --help\n",
		"modernpath author gate --help && echo done",
		"modernpath author --help | head -40",
		`sh -c "modernpath author gate --help"`,
		// PR #624 cold review round 2: the help word is still a help word when
		// what precedes it is the verb, a positional id, or the binary itself.
		"modernpath author gate X --help",
		// Verification pass: a flag that carries its value inline swallows
		// nothing, so the help word after it is a help word.
		"modernpath author gate X --title=t --help",
		"modernpath author update REQ-1 --detail=x -h",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); out != "{}" {
			t.Errorf("subagent read %q was not passed through: %s", cmd, out)
		}
	}
	// No marker: the main session. The commit gate is not involved for these
	// commands, so the adapter answers {} as before.
	for _, cmd := range []string{
		"modernpath author trace CR-1 --verdict PASS",
		"modernpath factory answer G --options approve",
	} {
		if out := processGateHookPayload(hookPayload(nil, cmd)); out != "{}" {
			t.Errorf("main-session write %q was denied: %s", cmd, out)
		}
	}
}

func TestSubagentMarkerIsAnIdentityNotALabel(t *testing.T) {
	for _, key := range []string{"agent_id", "subagent_id"} {
		if m := subagentMarker(hookPayload(map[string]string{key: "x1"}, "modernpath author trace T")); m != "x1" {
			t.Errorf("%s: marker = %q, want x1", key, m)
		}
	}
	// agent_type alone must never mark a call: a main session running under a
	// configured default agent would otherwise be denied its own writes.
	if m := subagentMarker(hookPayload(map[string]string{"agent_type": "rdd-cold-reviewer"}, "modernpath author trace T")); m != "" {
		t.Errorf("agent_type alone yields marker %q", m)
	}
	if m := subagentMarker(hookPayload(map[string]string{"agent_id": "x1", "agent_type": "rdd-cold-reviewer"}, "x")); m != "x1 (rdd-cold-reviewer)" {
		t.Errorf("type is not folded into the label: %q", m)
	}
	if m := subagentMarker([]byte("not json")); m != "" {
		t.Errorf("malformed payload yields marker %q; the guard must fail open", m)
	}
}
