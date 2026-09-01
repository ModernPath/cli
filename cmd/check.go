package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/modernpath/cli/internal/gate"
	"github.com/spf13/cobra"
)

var (
	checkWriteBaseline bool
	checkHookEvent     string
)

var checkCmd = &cobra.Command{
	Use:   "check",
	Short: "Check the process rules that must hold (status hygiene, approval before DONE)",
	Long: `Check the process rules that cannot be left to good intentions.

  status-hygiene         a ledger's dashboard rows must agree with its Totals line
  approval-before-done   an epic the work-list calls DONE must have a recorded
                         approval carrying a USER: source

Both were written rules long before they were checks. Written rules are context,
not configuration: they are usually followed, and the times they are not look
exactly like the times they are. This exits non-zero so a hook or CI can act.

Adopting the gate on an existing repository: run with --baseline once to accept
the violations already there. New ones still block; the accepted list lives in
.modernpath/rdd/gate-baseline and shrinks as you fix them.`,
	RunE: runCheck,
}

func init() {
	checkCmd.Flags().BoolVar(&checkWriteBaseline, "baseline", false,
		"record the current violations as accepted, so only new ones block")
	checkCmd.Flags().StringVar(&checkHookEvent, "hook", "",
		"read an agent hook payload from stdin (PreToolUse)")
	rootCmd.AddCommand(checkCmd)
}

func runCheck(cmd *cobra.Command, args []string) error {
	cmd.SilenceUsage = true
	if checkHookEvent != "" {
		return runCheckHook(checkHookEvent)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}

	// REQ-CROSS-098 slice 1: name the root, and refuse to pass over nothing.
	// Six measured vacuous passes came from committing in a subdirectory —
	// the gate ran, found no records, and said "pass" (RUN:2026-08-13/18).
	printInfo("checking %s\n", root)

	if !hasProcessRecords(root) {
		// REQ-CROSS-225 §225.4: a store-backed workspace has no ledgers BY
		// DECLARATION — that is the configuration, not a vacuous pass.
		if storeBackedWorkspace(root) {
			printSuccess("store-backed workspace (process/store-backed.md): file-based process gates do not apply — state lives in the server store")
			return nil
		}
		return fmt.Errorf("no process records under %s — nothing was checked (tasks/*-REQUIREMENTS.md and WORKLIST.md absent; wrong directory?)", root)
	}

	all, err := gate.CheckAll(root)
	if err != nil {
		return err
	}

	if checkWriteBaseline {
		if err := gate.WriteBaseline(root, all); err != nil {
			return err
		}
		fmt.Printf("✓ baselined %d existing violation(s) → .modernpath/rdd/gate-baseline\n", len(all))
		fmt.Println("  New violations will now block. Remove a line to start enforcing it.")
		return nil
	}

	baseline, err := gate.LoadBaseline(root)
	if err != nil {
		return err
	}
	fresh := gate.Unbaselined(all, baseline)

	if len(fresh) == 0 {
		if accepted := len(all); accepted > 0 {
			fmt.Printf("✓ no new violations (%d baselined and still outstanding)\n", accepted)
		} else {
			fmt.Println("✓ process checks pass")
		}
		return nil
	}

	fmt.Printf("✗ %d process violation(s):\n", len(fresh))
	for _, v := range fresh {
		// Name the rule: the reader needs it to look the rule up (rdd-ledger has a
		// table of what each enforces and what it deliberately does not flag), and
		// to baseline it deliberately rather than by guessing the line format.
		fmt.Printf("    %s  [%s]\n      %s\n", v.File, v.Rule, v.Detail)
	}
	fmt.Println("\nStatus lives in three places — the dashboard row, the detail block, and the")
	fmt.Println("Totals line. An epic is not DONE until its approval is recorded with a source.")
	return fmt.Errorf("%d process violation(s)", len(fresh))
}

func runCheckHook(event string) error {
	payload, err := io.ReadAll(os.Stdin)
	if err != nil || event != "PreToolUse" {
		fmt.Print("{}")
		return nil
	}
	fmt.Print(processGateHookPayload(payload))
	return nil
}

// Whitespace counts as a boundary before `git`, not only shell separators:
// agents emit env-assignment prefixes (GIT_AUTHOR_DATE=… git commit) and
// wrappers (command git commit, env FOO=1 git commit), and a shape the gate
// misses is a commit that lands unchecked. The regex is quote-blind in both
// directions — quote-interior whitespace is a boundary (prose mentioning a
// commit matches) and the quote character is not one (`sh -c "git commit"`
// does not) — so it serves only as the first pass; commitGated reads the
// quotes.
var gitCommitCommand = regexp.MustCompile(`(^|[\s;&|()])\\?git[[:space:]]+commit([[:space:]]|$)`)

// commitGated decides whether a command faces the process gate. The quote
// scan corrects the regex in both of its blind directions: a quoted span the
// shell treats as data no longer gates by its prose (quote-interior
// whitespace read as a boundary), and a span the shell will EXECUTE — a
// shell's -c body, a command substitution — gates even though the quote
// character hid it from the regex. Anything the scan cannot classify (an
// unterminated quote or substitution, runaway nesting) falls back to the
// regex's own verdict, so uncertainty never widens what slips the gate.
func commitGated(cmd string) bool {
	return commitGatedDepth(cmd, 0)
}

func commitGatedDepth(cmd string, depth int) bool {
	if depth > 8 {
		return gitCommitCommand.MatchString(cmd)
	}
	gated, classified := scanForExecutableCommit(cmd, depth)
	if !classified {
		return gitCommitCommand.MatchString(cmd)
	}
	return gated
}

var (
	// A shell whose -c body is a command again, with or without a path prefix.
	gateShellRe = regexp.MustCompile(`^(.*/)?(sh|bash|zsh|dash|ksh)$`)
	// -c possibly folded into a flag cluster, as in `bash -lc`.
	gateCFlagRe = regexp.MustCompile(`^-[A-Za-z]*c$`)
)

// scanForExecutableCommit walks the command once, replacing each quoted span
// with a placeholder and re-scanning the spans a shell would execute. It
// returns (gated, classified); classified=false means the walk met something
// it cannot be sure about — an unterminated quote or substitution — and the
// caller keeps the regex's deny.
func scanForExecutableCommit(cmd string, depth int) (bool, bool) {
	var stripped, cur strings.Builder
	var tokens []string // the current simple command's completed words
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	hasShell := func() bool {
		for _, tok := range tokens {
			if gateShellRe.MatchString(tok) {
				return true
			}
		}
		return false
	}
	// A quoted span is data unless the word before it is a -c whose simple
	// command names a shell; data in double quotes still executes its
	// substitutions.
	span := func(content string, double bool) (bool, bool) {
		prev := cur.String()
		if prev == "" && len(tokens) > 0 {
			prev = tokens[len(tokens)-1]
		}
		if gateCFlagRe.MatchString(prev) && hasShell() {
			if commitGatedDepth(content, depth+1) {
				return true, true
			}
		} else if double {
			if g, ok := scanSubstitutions(content, depth); !ok || g {
				return g, ok
			}
		}
		stripped.WriteString("_q_")
		cur.WriteString("_q_")
		return false, true
	}

	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && i+1 < len(cmd):
			stripped.WriteByte(c)
			stripped.WriteByte(cmd[i+1])
			cur.WriteByte(c)
			cur.WriteByte(cmd[i+1])
			i++
		case c == '\'':
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				return false, false
			}
			if g, ok := span(cmd[i+1:i+1+j], false); !ok || g {
				return g, ok
			}
			i += j + 1
		case c == '"':
			var content strings.Builder
			j := i + 1
			closed := false
			for ; j < len(cmd); j++ {
				if cmd[j] == '\\' && j+1 < len(cmd) {
					content.WriteByte(cmd[j])
					content.WriteByte(cmd[j+1])
					j++
					continue
				}
				if cmd[j] == '"' {
					closed = true
					break
				}
				content.WriteByte(cmd[j])
			}
			if !closed {
				return false, false
			}
			if g, ok := span(content.String(), true); !ok || g {
				return g, ok
			}
			i = j
		case c == '`':
			j := strings.IndexByte(cmd[i+1:], '`')
			if j < 0 {
				return false, false
			}
			if commitGatedDepth(cmd[i+1:i+1+j], depth+1) {
				return true, true
			}
			stripped.WriteString("_q_")
			cur.WriteString("_q_")
			i += j + 1
		case c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
			open := 1
			j := i + 2
			for ; j < len(cmd) && open > 0; j++ {
				switch cmd[j] {
				case '(':
					open++
				case ')':
					open--
				}
			}
			if open != 0 {
				return false, false
			}
			if commitGatedDepth(cmd[i+2:j-1], depth+1) {
				return true, true
			}
			stripped.WriteString("_q_")
			cur.WriteString("_q_")
			i = j - 1
		case c == ';' || c == '|' || c == '&' || c == '(' || c == ')' || c == '\n':
			stripped.WriteByte(c)
			flush()
			tokens = nil
		case c == ' ' || c == '\t':
			stripped.WriteByte(c)
			flush()
		default:
			stripped.WriteByte(c)
			cur.WriteByte(c)
		}
	}
	return gitCommitCommand.MatchString(stripped.String()), true
}

// scanSubstitutions re-scans what a double-quoted span still executes:
// $( … ) and backticks run whether or not they are quoted.
func scanSubstitutions(content string, depth int) (bool, bool) {
	for i := 0; i < len(content); i++ {
		switch {
		case content[i] == '\\' && i+1 < len(content):
			i++
		case content[i] == '`':
			j := strings.IndexByte(content[i+1:], '`')
			if j < 0 {
				return false, false
			}
			if commitGatedDepth(content[i+1:i+1+j], depth+1) {
				return true, true
			}
			i += j + 1
		case content[i] == '$' && i+1 < len(content) && content[i+1] == '(':
			open := 1
			j := i + 2
			for ; j < len(content) && open > 0; j++ {
				switch content[j] {
				case '(':
					open++
				case ')':
					open--
				}
			}
			if open != 0 {
				return false, false
			}
			if commitGatedDepth(content[i+2:j-1], depth+1) {
				return true, true
			}
			i = j - 1
		}
	}
	return false, true
}

func processGateHookPayload(payload []byte) string {
	var input struct {
		CWD       string `json:"cwd"`
		ToolInput struct {
			Command string `json:"command"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(payload, &input); err != nil || !commitGated(input.ToolInput.Command) {
		return "{}"
	}

	root, err := hookRepositoryRoot(input.CWD)
	if err != nil {
		return "{}"
	}
	report, violates, err := processCheckReport(root)
	if err != nil || !violates {
		return "{}"
	}

	envelope := map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": report,
		},
	}
	out, err := json.Marshal(envelope)
	if err != nil {
		return "{}"
	}
	return string(out)
}

func hookRepositoryRoot(cwd string) (string, error) {
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	info, err := os.Stat(cwd)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("invalid hook cwd")
	}
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = cwd
	out, err := command.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", fmt.Errorf("git returned an empty repository root")
	}
	return root, nil
}

func processCheckReport(root string) (string, bool, error) {
	all, err := gate.CheckAll(root)
	if err != nil {
		return "", false, err
	}
	baseline, err := gate.LoadBaseline(root)
	if err != nil {
		return "", false, err
	}
	fresh := gate.Unbaselined(all, baseline)
	if len(fresh) == 0 {
		return "", false, nil
	}

	var report strings.Builder
	fmt.Fprintf(&report, "✗ %d process violation(s):\n", len(fresh))
	for _, violation := range fresh {
		fmt.Fprintf(&report, "    %s\n      %s\n", violation.File, violation.Detail)
	}
	return report.String(), true, nil
}

// hasProcessRecords reports whether the root holds anything the gate can
// evaluate: a requirement ledger or a work-list. "No records found" is a
// distinct outcome from "no violations" (REQ-CROSS-098 slice 1).
func hasProcessRecords(root string) bool {
	if ledgers, _ := filepath.Glob(filepath.Join(root, "tasks", "*-REQUIREMENTS.md")); len(ledgers) > 0 {
		return true
	}
	_, err := os.Stat(filepath.Join(root, "WORKLIST.md"))
	return err == nil
}
