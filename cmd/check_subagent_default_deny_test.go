package cmd

import (
	"strings"
	"testing"
)

// REQ-CROSS-451 (PR #694 review, #10): the subagent guard blanked quoted text
// before matching its list of write verbs, so `modernpath process lane
// "approve" G` went through, and a write verb missing from the list went
// through too. The guard now reads the command as the shell will run it
// (quotes removed), allows a delegated agent only the verbs listed as reads,
// and denies any other modernpath subcommand, including one it cannot read
// literally ($VAR, eval, xargs, an indirect binary).

func TestREQCROSS451GuardDeniesQuotedAndIndirectWrites(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		`modernpath process lane "approve" LANE-AUTH`,
		`modernpath process lane 'approve' LANE-AUTH`,
		`modernpath "process" "lane" approve LANE-AUTH`,
		`modernpath process "lane approve" LANE-AUTH`,
		`mod"ern"path author trace T --verdict PASS`,
		`"modernpath" author trace T --verdict PASS`,
		`modernpath author "trace" T`,
		`V=approve; modernpath process lane $V LANE-AUTH`,
		`modernpath process lane $V LANE-AUTH`,
		`modernpath process lane ${VERB} LANE-AUTH`,
		`modernpath $VERB`,
		`modernpath process lane $(echo approve) LANE-AUTH`,
		`eval "modernpath process lane approve LANE-AUTH"`,
		`eval modernpath author trace T`,
		`echo approve LANE-AUTH | xargs modernpath process lane`,
		`xargs -n1 modernpath author trace < ids.txt`,
		`MP=modernpath; $MP author trace T`,
		`$(which modernpath) author trace T`,
		`find . -name '*.json' -exec modernpath author apply --file {} \;`,
		`timeout 60 modernpath process lane approve LANE-AUTH`,
		`modernpath author trace T --title "unterminated`,
	} {
		out := processGateHookPayload(hookPayload(sub, cmd))
		if !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("subagent %q was not denied: %s", cmd, out)
		}
	}
}

// A write verb nobody listed is denied until it is listed as a read.
func TestREQCROSS451GuardDeniesEveryVerbNotListedAsARead(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath focus REQ-1",
		"modernpath docs generate",
		"modernpath docs refresh",
		"modernpath source push",
		"modernpath system-docs push",
		"modernpath work new 'an epic'",
		"modernpath work specs push",
		"modernpath factory image --prompt x",
		"modernpath factory watch",
		"modernpath process cascade-mode",
		"modernpath process frobnicate REQ-1",
		"modernpath frobnicate",
		"modernpath env",
	} {
		out := processGateHookPayload(hookPayload(sub, cmd))
		if !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("subagent %q was not denied: %s", cmd, out)
		}
	}
}

// REQ-CROSS-451 (reopened, BACKLOG-TOOL-275): a heredoc body is data unless
// the heredoc feeds a shell. The guard read a body written to a file as
// commands, and its stray apostrophe made the whole call unreadable, so a
// test-file edit was denied as a store write.
func TestREQCROSS451GuardReadsAHeredocBodyOnlyWhenItFeedsAShell(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	body := "modernpath author apply --file plan.yaml\nit's data here\n"
	for _, cmd := range []string{
		"cat > f <<'EOF'\n" + body + "EOF",
		"cat > f <<EOF\n" + body + "EOF\necho done",
		"cat <<-\"EOF\" > f\n\t" + strings.ReplaceAll(body, "\n", "\n\t") + "EOF",
		"python3 - <<'PY'\nprint('modernpath author apply')\nPY",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); out != "{}" {
			t.Errorf("a heredoc body written as data was read as a command %q: %s", cmd, out)
		}
	}
	for _, cmd := range []string{
		"bash <<'EOF'\n" + body + "EOF",
		"sh <<EOF\n" + body + "EOF",
		"zsh -s <<'EOF'\n" + body + "EOF",
		"cat > f <<'EOF'\nharmless\nEOF\nmodernpath author apply --file f",
		"eval \"$(cat <<'EOF'\nmodernpath author apply --file plan.yaml\nEOF\n)\"",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("a heredoc that feeds a shell, or a call after the body, was not denied %q: %s", cmd, out)
		}
	}
}

// REQ-CROSS-451 (reopened): an assignment is not a call. A variable whose
// value ends in /modernpath is a directory as often as the binary; it is
// denied only when it is run as a command.
func TestREQCROSS451GuardDeniesAVariableOnlyWhenItRunsTheBinary(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"T=a/modernpath && git add $T/x",
		"T=modernpath-core/tools/modernpath; git add $T/cmd/x.go $T/internal/y.go",
		"export T=modernpath-core/tools/modernpath && go test $T/...",
		"MP=modernpath && $MP working-set pull REQ-1",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); out != "{}" {
			t.Errorf("an assignment that is never run as the binary was denied %q: %s", cmd, out)
		}
	}
	for _, cmd := range []string{
		"MP=modernpath && $MP author apply --file plan.yaml",
		"MP=/usr/local/bin/modernpath; ${MP} author trace T",
		`MP="modernpath author"; $MP apply --file p.yaml`,
		"export MP=modernpath; $MP process lane approve X",
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); !strings.Contains(out, `"permissionDecision":"deny"`) {
			t.Errorf("a variable run as the binary's write verb was not denied %q: %s", cmd, out)
		}
	}
}

func TestREQCROSS451GuardKeepsTheReadPaths(t *testing.T) {
	sub := map[string]string{"agent_id": "abc123"}
	for _, cmd := range []string{
		"modernpath process next",
		"modernpath process next -v",
		"modernpath your-move --more",
		"modernpath status",
		`modernpath search "modernpath author trace"`,
		"modernpath read-doc --id=12",
		"modernpath working-set pull $ID",
		`modernpath working-set pull "REQ-CROSS-442"`,
		"echo REQ-1 REQ-2 | xargs modernpath working-set pull",
		"modernpath process backlog list --kind tooling",
		"modernpath requirements list --limit 20",
		`modernpath requirements search "signing keys" --limit 5`,
		"modernpath epics search teams --json",
		"modernpath factory gates --state answered --json",
		"modernpath process lane",
		"modernpath process lane approve --help",
		"modernpath hooks doctor",
		"rg -n modernpath docs",
		`grep -rn "modernpath process lane approve" .`,
		"ls ~/.local/bin/modernpath",
		"cd modernpath-core/tools/modernpath && go test ./...",
		`git commit -m "fix(cli): modernpath process lane approve"`,
		"cat .modernpath/working-set/REQ-CROSS-442.md",
		// An unterminated quote is unreadable; a path that merely contains the
		// name is not a call of the binary.
		`cd /Users/x/modernpath-v1 && echo "unterminated`,
	} {
		if out := processGateHookPayload(hookPayload(sub, cmd)); out != "{}" {
			t.Errorf("subagent read %q was not passed through: %s", cmd, out)
		}
	}
}
