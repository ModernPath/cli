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
// Flag-first: with no identity in the payload nothing changes. For a
// delegated agent the guard is default-deny (PR #694 review, #10): it reads
// the command the way the shell will run it — quotes removed, so `process
// lane "approve"` is `process lane approve` — resolves every call of the
// binary against the command tree, and allows only the verbs listed in
// subagentReadVerbs. Any other subcommand is denied, so a write verb added
// later is denied until someone lists it as a read. A subcommand the guard
// cannot read literally ($VAR, $(…), xargs input, an indirect binary) is
// denied too.

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

// subagentReadVerbs are the command paths (after "modernpath") a delegated
// agent may run: reads of the store and the local analysis, and downloads.
// "" is the bare binary (help, --version).
var subagentReadVerbs = map[string]bool{
	"": true, "help": true, "completion": true,
	"ask": true, "auth status": true, "context": true, "coverage": true,
	"datamodel export": true, "dev list": true, "dev ralph status": true,
	"docs preview": true, "docs sync": true,
	"env list": true, "env test": true, "epics search": true,
	"factory drift": true, "factory gates": true, "factory manifest show": true,
	"factory next-id": true, "factory reconcile": true, "factory release show": true,
	"factory status": true,
	"hooks doctor":   true, "hooks status": true,
	"process backlog list": true, "process check": true, "process findings list": true,
	"process lane": true, "process next": true, "process prepare-inputs": true,
	"read-doc": true, "read-file": true, "requirements list": true, "requirements search": true,
	"requirements-corpus":         true,
	"reverse-engineer candidates": true, "reverse-engineer coverage": true,
	"reverse-engineer inventory": true, "reverse-engineer preflight": true,
	"reverse-engineer preview": true, "reverse-engineer proof-preview": true,
	"reverse-engineer acceptance-status": true, "reverse-engineer read-document": true,
	"reverse-engineer read-source": true, "reverse-engineer source-status": true,
	"reverse-engineer status": true,
	"search":                  true, "status": true,
	"system-docs list": true, "system-docs pull": true,
	"tasks fetch": true, "tasks list": true,
	"work list": true, "work review": true, "work specs sync": true, "work status": true,
	"work subtasks":     true,
	"working-set check": true, "working-set pull": true,
	"your-move": true,
}

// isHelpWord is the exact spelling of a help request. `--help=…` and
// `-hsomething` are not one.
func isHelpWord(tok string) bool { return tok == "--help" || tok == "-h" }

// helpRequested reports whether an invocation's arguments ask for help rather
// than running the verb: `author gate --help` prints help and writes nothing,
// so denying it left a delegated pass unable to read the very surface it is
// told to use (BACKLOG-TOOL-6, BACKLOG-TOOL-105).
//
// The word must be an argument in its own right. pflag takes the NEXT argv as
// a flag's value even when that value starts with a dash, so
// `author gate X --title -h` sets the title to "-h" and writes the gate — it
// is not a help request (PR #624 cold review, round 2). A help word directly
// after any other dash token is therefore read as that flag's value — unless
// that token already carries its value inline (`--title=t`). After a boolean
// flag (`--apply --help`) this denies a real help call: denying one costs a
// reordering; allowing a write costs the store. The words are the shell's
// words, so `--title "x --help"` is one word and not a help request.
func helpRequested(args []string) bool {
	for i, tok := range args {
		if !isHelpWord(tok) {
			continue
		}
		if i > 0 && valueTakingFlag(args[i-1]) {
			continue
		}
		return true
	}
	return false
}

// inlineValueFlag is a flag that already holds its value: `--title=t`, `-t=x`.
var inlineValueFlag = regexp.MustCompile(`^--?[^=]+=.+`)

// valueTakingFlag reports a token that would swallow the word after it.
func valueTakingFlag(tok string) bool {
	return strings.HasPrefix(tok, "-") && !isHelpWord(tok) && !inlineValueFlag.MatchString(tok)
}

// shWord is one word as the shell will pass it: quotes removed. literal is
// false when the word holds an expansion the guard cannot resolve ($VAR,
// $(…), `…`).
type shWord struct {
	text    string
	literal bool
}

// heredocDelimiter reads a here-document operator's delimiter word starting
// at i (after `<<` and an optional `-`): blanks skipped, quotes removed.
// Returns the delimiter and the index of its last byte.
func heredocDelimiter(cmd string, i int) (string, int) {
	for i < len(cmd) && (cmd[i] == ' ' || cmd[i] == '\t') {
		i++
	}
	var b strings.Builder
	j := i
	for ; j < len(cmd); j++ {
		c := cmd[j]
		if c == ' ' || c == '\t' || c == '\n' || strings.IndexByte(";&|()<>", c) >= 0 {
			break
		}
		if c == '\'' || c == '"' {
			if k := strings.IndexByte(cmd[j+1:], c); k >= 0 {
				b.WriteString(cmd[j+1 : j+1+k])
				j += k + 1
				continue
			}
		}
		if c == '\\' && j+1 < len(cmd) {
			j++
			c = cmd[j]
		}
		b.WriteByte(c)
	}
	return b.String(), j - 1
}

// pendingHeredoc is a here-document whose body starts at the next newline.
type pendingHeredoc struct {
	owner int // the index of the simple command it feeds
	delim string
	strip bool // <<- strips leading tabs
}

// shLex splits a command into simple commands of words, the way the shell
// would: quotes join and are removed, a backslash escapes, `;` `&` `|` `(`
// `)` and a newline end a simple command. Command substitutions are returned
// as scripts of their own, since they run. A here-document body is not
// lexed as commands: it is data on the standard input of the command that
// owns it, returned in heredocs by that command's index (REQ-CROSS-451,
// BACKLOG-TOOL-275). ok is false for an unterminated quote or substitution.
func shLex(cmd string) (commands [][]shWord, subs []string, heredocs map[int][]string, ok bool) {
	var words []shWord
	var cur strings.Builder
	var pending []pendingHeredoc
	heredocs = map[int][]string{}
	inWord, literal := false, true
	endWord := func() {
		if inWord {
			words = append(words, shWord{cur.String(), literal})
		}
		cur.Reset()
		inWord, literal = false, true
	}
	endCommand := func() {
		endWord()
		if len(words) > 0 {
			commands = append(commands, words)
		}
		words = nil
	}
	// substitution reads a $( … ) body starting after "$(" at i; returns the
	// index of the closing paren.
	substitution := func(i int) (string, int, bool) {
		depth := 1
		for j := i; j < len(cmd); j++ {
			switch cmd[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					return cmd[i:j], j, true
				}
			}
		}
		return "", 0, false
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case c == '\\' && i+1 < len(cmd):
			if cmd[i+1] != '\n' {
				cur.WriteByte(cmd[i+1])
				inWord = true
			}
			i++
		case c == '\'':
			j := strings.IndexByte(cmd[i+1:], '\'')
			if j < 0 {
				return nil, nil, nil, false
			}
			cur.WriteString(cmd[i+1 : i+1+j])
			inWord = true
			i += j + 1
		case c == '"':
			j := i + 1
			for ; j < len(cmd) && cmd[j] != '"'; j++ {
				switch {
				case cmd[j] == '\\' && j+1 < len(cmd):
					cur.WriteByte(cmd[j+1])
					j++
				case cmd[j] == '$' && j+1 < len(cmd) && cmd[j+1] == '(':
					body, end, ok := substitution(j + 2)
					if !ok {
						return nil, nil, nil, false
					}
					subs = append(subs, body)
					cur.WriteString("$(" + body + ")")
					literal = false
					j = end
				case cmd[j] == '`':
					k := strings.IndexByte(cmd[j+1:], '`')
					if k < 0 {
						return nil, nil, nil, false
					}
					subs = append(subs, cmd[j+1:j+1+k])
					cur.WriteString(cmd[j : j+2+k])
					literal = false
					j += k + 1
				case cmd[j] == '$':
					cur.WriteByte('$')
					literal = false
				default:
					cur.WriteByte(cmd[j])
				}
			}
			if j >= len(cmd) {
				return nil, nil, nil, false
			}
			inWord = true
			i = j
		case c == '`':
			k := strings.IndexByte(cmd[i+1:], '`')
			if k < 0 {
				return nil, nil, nil, false
			}
			subs = append(subs, cmd[i+1:i+1+k])
			cur.WriteString(cmd[i : i+2+k])
			inWord, literal = true, false
			i += k + 1
		case c == '$' && i+1 < len(cmd) && cmd[i+1] == '(':
			body, end, ok := substitution(i + 2)
			if !ok {
				return nil, nil, nil, false
			}
			subs = append(subs, body)
			cur.WriteString("$(" + body + ")")
			inWord, literal = true, false
			i = end
		case c == '$':
			cur.WriteByte('$')
			inWord, literal = true, false
		case c == '#' && !inWord:
			for i < len(cmd) && cmd[i] != '\n' {
				i++
			}
			endCommand()
		case c == '<' && i+1 < len(cmd) && cmd[i+1] == '<' && (i+2 >= len(cmd) || cmd[i+2] != '<'):
			// A here-document operator: <<DELIM or <<-DELIM.
			endWord()
			j := i + 2
			strip := j < len(cmd) && cmd[j] == '-'
			if strip {
				j++
			}
			delim, end := heredocDelimiter(cmd, j)
			if delim != "" {
				owner := len(commands)
				pending = append(pending, pendingHeredoc{owner: owner, delim: delim, strip: strip})
			}
			i = end
		case c == '\n' && len(pending) > 0:
			endCommand()
			// Each pending body runs from the next line to its delimiter line.
			pos := i + 1
			for _, h := range pending {
				var body []string
				for pos <= len(cmd) {
					nl := strings.IndexByte(cmd[pos:], '\n')
					line := cmd[pos:]
					next := len(cmd) + 1
					if nl >= 0 {
						line = cmd[pos : pos+nl]
						next = pos + nl + 1
					}
					pos = next
					cmp := line
					if h.strip {
						cmp = strings.TrimLeft(line, "\t")
					}
					if cmp == h.delim {
						break
					}
					body = append(body, line)
				}
				heredocs[h.owner] = append(heredocs[h.owner], strings.Join(body, "\n"))
			}
			pending = nil
			i = pos - 1
		case c == ';' || c == '&' || c == '|' || c == '(' || c == ')' || c == '\n':
			endCommand()
		case c == ' ' || c == '\t':
			endWord()
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	endCommand()
	return commands, subs, heredocs, true
}

var (
	mentionsBinary = regexp.MustCompile("(^|[^A-Za-z0-9_.-])modernpath($|[^A-Za-z0-9_.-])")
	assignmentRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	shellRe        = regexp.MustCompile(`^(sh|bash|zsh|dash|ksh)$`)
	cFlagRe        = regexp.MustCompile(`^-[A-Za-z]*c$`)
)

// wrapperCommands run the words after them as a command.
var wrapperCommands = map[string]bool{
	"command": true, "builtin": true, "exec": true, "env": true, "sudo": true, "doas": true,
	"nohup": true, "time": true, "nice": true, "timeout": true, "stdbuf": true, "caffeinate": true,
	"xargs": true, "parallel": true, "watch": true, "!": true, "{": true, "then": true, "do": true,
	"else": true, "if": true, "while": true, "until": true,
}

// isModernpath reports a word naming the binary, by any path.
func isModernpath(word string) bool {
	return word == "modernpath" || strings.HasSuffix(word, "/modernpath")
}

// subagentDenied is the guard's verdict on one script for a delegated agent.
func subagentDenied(cmd string, depth int) bool {
	return scriptDenied(cmd, depth, map[string]shWord{})
}

// scriptDenied judges a script with the shell variables assigned so far.
func scriptDenied(cmd string, depth int, vars map[string]shWord) bool {
	if depth > 8 {
		return mentionsBinary.MatchString(cmd)
	}
	commands, subs, heredocs, ok := shLex(cmd)
	if !ok {
		// Unreadable: deny only when it could be a call of the binary — the
		// word modernpath, not a path such as ~/modernpath-v1 or .modernpath/.
		return mentionsBinary.MatchString(cmd)
	}
	for _, sub := range subs {
		if scriptDenied(sub, depth+1, vars) {
			return true
		}
	}
	for k, words := range commands {
		if simpleCommandDenied(words, depth, vars) {
			return true
		}
		// A here-document is data on the command's standard input; it runs
		// only when that command is a shell (REQ-CROSS-451, BACKLOG-TOOL-275).
		if feedsShell(words) {
			for _, body := range heredocs[k] {
				if scriptDenied(body, depth+1, vars) {
					return true
				}
			}
		}
	}
	return false
}

// feedsShell reports a command that runs its standard input as a script.
func feedsShell(words []shWord) bool {
	for _, w := range words {
		base := path.Base(w.text)
		switch {
		case assignmentRe.MatchString(w.text), wrapperCommands[base], strings.HasPrefix(w.text, "-"):
			continue
		}
		return shellRe.MatchString(base) || base == "eval" || base == "source" || base == "."
	}
	return false
}

var varRefRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// expandHead resolves a command word's $NAME and ${NAME} from the literal
// assignments seen so far. ok is false when anything is left unresolved;
// mentions reports a referenced variable whose value names the binary.
func expandHead(text string, vars map[string]shWord) (expanded string, ok, mentions bool) {
	ok = true
	expanded = varRefRe.ReplaceAllStringFunc(text, func(ref string) string {
		m := varRefRe.FindStringSubmatch(ref)
		name := m[1] + m[2]
		v, known := vars[name]
		if known && mentionsBinary.MatchString(v.text) {
			mentions = true
		}
		if !known || !v.literal {
			ok = false
			return ref
		}
		return v.text
	})
	if strings.Contains(expanded, "$") || strings.Contains(expanded, "`") {
		ok = false
	}
	return expanded, ok, mentions
}

// simpleCommandDenied judges one simple command. An assignment is recorded,
// not judged: a variable naming the binary is a call only when it is later
// run as a command, so `T=tools/modernpath && git add $T/x` passes and
// `MP=modernpath && $MP author apply` is denied (REQ-CROSS-451).
func simpleCommandDenied(words []shWord, depth int, vars map[string]shWord) bool {
	fromStdin := false
	i := 0
	for i < len(words) {
		w := words[i].text
		if assignmentRe.MatchString(w) {
			name, value, _ := strings.Cut(w, "=")
			vars[name] = shWord{text: value, literal: words[i].literal}
			i++
			continue
		}
		base := path.Base(w)
		if base == "export" || base == "declare" || base == "local" || base == "readonly" || base == "typeset" {
			for _, a := range words[i+1:] {
				if assignmentRe.MatchString(a.text) {
					name, value, _ := strings.Cut(a.text, "=")
					vars[name] = shWord{text: value, literal: a.literal}
				}
			}
			return false
		}
		if base == "eval" {
			var rest []string
			for _, a := range words[i+1:] {
				rest = append(rest, a.text)
			}
			return scriptDenied(strings.Join(rest, " "), depth+1, vars)
		}
		if !wrapperCommands[base] {
			break
		}
		if base == "xargs" || base == "parallel" {
			fromStdin = true
		}
		i++
		// The wrapper's own options, and timeout's duration.
		for i < len(words) && (strings.HasPrefix(words[i].text, "-") || assignmentRe.MatchString(words[i].text)) {
			i++
		}
		if base == "timeout" && i < len(words) {
			i++
		}
		if base == "watch" {
			// watch runs its arguments as one shell command.
			var rest []string
			for _, a := range words[i:] {
				rest = append(rest, a.text)
			}
			return scriptDenied(strings.Join(rest, " "), depth+1, vars)
		}
	}
	if i >= len(words) {
		return false
	}
	head := words[i]
	if !head.literal {
		// $MP, ${BIN}, $(which modernpath): an indirect command. Resolved from
		// a literal assignment, it is judged as the command it names.
		expanded, ok, mentions := expandHead(head.text, vars)
		if !ok {
			return mentions || strings.Contains(head.text, "modernpath")
		}
		fields := strings.Fields(expanded)
		if len(fields) == 0 {
			return false
		}
		resolved := make([]shWord, 0, len(fields)+len(words)-i-1)
		for _, f := range fields {
			resolved = append(resolved, shWord{text: f, literal: true})
		}
		words = append(resolved, words[i+1:]...)
		i = 0
		head = words[0]
	}
	if isModernpath(head.text) {
		return invocationDenied(words[i+1:], fromStdin)
	}
	for j := i + 1; j < len(words); j++ {
		w := words[j].text
		switch {
		// A shell's -c body runs.
		case cFlagRe.MatchString(w) && shellRe.MatchString(path.Base(head.text)) && j+1 < len(words):
			if scriptDenied(words[j+1].text, depth+1, vars) {
				return true
			}
		// find -exec runs the words after it, with arguments it supplies.
		case (w == "-exec" || w == "-execdir" || w == "-ok" || w == "-okdir") && j+1 < len(words) && isModernpath(words[j+1].text):
			return invocationDenied(words[j+2:], true)
		}
	}
	return false
}

// invocationDenied judges the arguments of one call of the binary. fromStdin
// marks arguments that xargs or find will add after the literal ones.
func invocationDenied(args []shWord, fromStdin bool) bool {
	argv := make([]string, len(args))
	for k, a := range args {
		argv[k] = a.text
	}
	if !fromStdin && helpRequested(argv) {
		return false
	}
	if len(argv) > 0 && (argv[0] == "help" || argv[0] == "completion") {
		return false
	}
	c, rest, err := rootCmd.Find(argv)
	if err != nil || c == nil {
		return true
	}
	positional := positionalArgs(c, rest)
	if c.HasSubCommands() && (len(positional) > 0 || fromStdin) {
		// An unknown or non-literal subcommand: `process lane $V`.
		return true
	}
	name := strings.TrimSpace(strings.TrimPrefix(c.CommandPath(), rootCmd.Name()))
	return !subagentReadVerbs[name]
}

// positionalArgs are the arguments left after flags, read the way cobra's
// command lookup reads them: a flag without an inline value takes the next
// word unless it is boolean.
func positionalArgs(c *cobra.Command, args []string) []string {
	var out []string
	lookup := func(name string, short bool) (bool, bool) {
		f := c.Flags().Lookup(name)
		if short {
			f = c.Flags().ShorthandLookup(name)
		}
		if f == nil {
			if short {
				f = c.InheritedFlags().ShorthandLookup(name)
			} else {
				f = c.InheritedFlags().Lookup(name)
			}
		}
		if f == nil {
			return false, false
		}
		return true, f.NoOptDefVal != ""
	}
	for k := 0; k < len(args); k++ {
		a := args[k]
		switch {
		case a == "--":
			return append(out, args[k+1:]...)
		case strings.HasPrefix(a, "--") && !strings.Contains(a, "="):
			if _, boolean := lookup(a[2:], false); !boolean {
				k++
			}
		case strings.HasPrefix(a, "-") && len(a) == 2:
			if _, boolean := lookup(a[1:], true); !boolean {
				k++
			}
		case strings.HasPrefix(a, "-"):
		default:
			out = append(out, a)
		}
	}
	return out
}

// storeWriteCommand is the guard's verdict for a delegated agent's command:
// true when it runs any verb of the binary that is not listed as a read.
func storeWriteCommand(command string) bool {
	return subagentDenied(command, 0)
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
