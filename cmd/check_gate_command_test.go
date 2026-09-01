package cmd

import "testing"

// The gate exists so a violating commit cannot land; the command shapes agents
// actually emit include env-assignment prefixes and shell wrappers, and a
// shape the matcher misses is a gate that fails open silently. The replaced
// script hook matched "git commit" anywhere, so every miss here is a
// regression against the wiring this branch removes.
func TestGateMatchesTheCommitShapesAgentsEmit(t *testing.T) {
	gated := []string{
		"git commit -m x",
		"cd /repo && git commit -m x",
		"git add -A; git commit -m x",
		"(git commit -m x)",
		"GIT_AUTHOR_DATE=2026-08-01T00:00:00 git commit -m x",
		"FOO=1 BAR=2 git commit --amend",
		"command git commit -m x",
		"env GIT_TRACE=1 git commit -m x",
		`\git commit -m x`,
		"git add -A | git commit -m x",
		"git commit",
	}
	for _, cmd := range gated {
		if !commitGated(cmd) {
			t.Errorf("gate misses %q — the commit would land unchecked", cmd)
		}
	}

	// Not commits: the gate must not fire on commands that only mention one.
	ungated := []string{
		"npm test",
		"grep 'git commit' docs/runbook.md",
		"echo \"git commit\"",
		"git log --oneline",
		"legit commit",
		"git commitish",
	}
	for _, cmd := range ungated {
		if commitGated(cmd) {
			t.Errorf("gate over-fires on %q", cmd)
		}
	}
}

// The regex alone is quote-blind in both directions: quote-interior whitespace
// is a boundary, so prose mentioning a commit was denied whenever fresh
// violations existed; and the quote character is not one, so a commit wrapped
// in a shell string landed ungated. A quoted string is the argument it is —
// unless a shell will execute it.
func TestQuotedProseDoesNotTriggerTheGate(t *testing.T) {
	for _, cmd := range []string{
		`echo "please git commit later"`,
		`echo 'remember to git commit tonight'`,
		`printf "%s" "docs about git commit --amend"`,
		`grep -c "git commit" README.md`,
		`git commit-tree -m "not a commit command" abc123`,
	} {
		if commitGated(cmd) {
			t.Errorf("gate fires on non-commit %q", cmd)
		}
	}
}

func TestAShellStringBodyIsACommand(t *testing.T) {
	for _, cmd := range []string{
		`sh -c "git commit -m x"`,
		`bash -c 'git commit -m x'`,
		`bash -lc "git commit --amend"`,
		`env sh -c "cd /repo && git commit -m x"`,
		"echo \"$(git commit -m x)\"",
		"echo `git commit -m x`",
	} {
		if !commitGated(cmd) {
			t.Errorf("a commit the shell will execute slips the gate: %q", cmd)
		}
	}
}

// A commit whose own argument quotes the words is still a commit, and a
// command the scanner cannot classify keeps the regex's verdict — the quote
// reading may only narrow the gate, never widen what slips it.
func TestTheScannerOnlyNarrows(t *testing.T) {
	for _, cmd := range []string{
		`git commit -m "please git commit later"`,
		`echo "please git commit later`, // unbalanced: unclassifiable
	} {
		if !commitGated(cmd) {
			t.Errorf("gate must hold on %q", cmd)
		}
	}
}
