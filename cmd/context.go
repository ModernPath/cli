package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	contextMaxQueries int
	contextMaxTokens  int
	contextHookEvent  string

	// Below the agents' configured hook timeout of 60s on purpose: past this the
	// context is no longer worth the wait, and holding the turn is the failure
	// mode users uninstall a hook over (EPIC-CTX-001, D-CTX-6).
	contextDeadlineSeconds int
)

var contextCmd = &cobra.Command{
	Use:   "context <prompt>",
	Short: "Get codebase context for a prompt (optimized for hooks)",
	Long: `Evaluate a prompt for codebase relevance and return brief context.

This command is optimized for IDE hooks (Cursor, Claude Code, Codex).
It uses a fast LLM to:
1. Evaluate if the prompt asks about THIS specific codebase
2. Decompose complex questions into targeted search queries
3. Run searches and return combined, brief context

Returns empty output if the prompt is not codebase-relevant.

Examples:
  modernpath context "How does authentication work?"
  modernpath context "What is the database schema?" --max-queries=2
  
For hooks, simply:
  context=$(modernpath context "$prompt")
  # Returns markdown context or empty string`,
	Args: cobra.ArbitraryArgs,
	RunE: runContext,
}

func init() {
	contextCmd.Flags().IntVar(&contextMaxQueries, "max-queries", 3, "Maximum decomposed queries (1-5)")
	contextCmd.Flags().IntVar(&contextMaxTokens, "max-tokens", 3000, "Maximum output tokens")
	contextCmd.Flags().IntVar(&contextDeadlineSeconds, "deadline", 8, "Hook mode: seconds to wait before returning empty rather than holding the turn")
	contextCmd.Flags().StringVar(&contextHookEvent, "hook", "", "Hook mode: read the agent payload on stdin and emit its context envelope (value = hook event name)")
	rootCmd.AddCommand(contextCmd)
}

type contextResult struct {
	Success bool `json:"success"`
	Result  struct {
		Relevant    bool     `json:"relevant"`
		Reason      string   `json:"reason,omitempty"`
		Queries     []string `json:"queries,omitempty"`
		Context     string   `json:"context"`
		SourceCount int      `json:"source_count,omitempty"`
	} `json:"result"`
	Error string `json:"error,omitempty"`
}

func runContext(cmd *cobra.Command, args []string) error {
	// Hook mode owns the whole exchange — read the agent's payload, print its
	// envelope, and never fail. A hook that exits non-zero costs the user their
	// prompt, so every path below returns nil (RUN:2026-08-10, the sync hook's
	// lesson applied to its sibling).
	if contextHookEvent != "" {
		payload, _ := io.ReadAll(os.Stdin)
		fmt.Print(contextHookOutputWithin(
			time.Duration(contextDeadlineSeconds)*time.Second,
			contextHookEvent,
			promptFromHookPayload(payload),
			fetchContextOutcome,
		))
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("a prompt is required")
	}
	fmt.Print(fetchContextOutcome(strings.Join(args, " ")).text)
	return nil
}

// promptFromHookPayload pulls the user's text out of the agent's stdin JSON.
// Unparseable input yields "" — which the hook renders as silence.
func promptFromHookPayload(raw []byte) string {
	var body struct {
		Prompt     string `json:"prompt"`
		UserPrompt string `json:"user_prompt"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return ""
	}
	if body.Prompt != "" {
		return body.Prompt
	}
	return body.UserPrompt
}

// contextHookOutputWithin runs the fetch under a deadline and records what
// happened. Past the deadline the in-flight request is abandoned: a context that
// arrives after the agent should have started is worse than no context, because
// the user paid for it in wall-clock either way.
func contextHookOutputWithin(deadline time.Duration, event, prompt string, fetch func(string) contextOutcome) string {
	if strings.TrimSpace(prompt) == "" {
		logContextOutcome("empty", 0, "empty prompt")
		return "{}"
	}

	started := time.Now()
	done := make(chan contextOutcome, 1)
	go func() { done <- fetch(prompt) }()

	select {
	case found := <-done:
		out := contextHookOutput(event, prompt, func(string) string { return found.text })
		if out == "{}" {
			logContextOutcome(outcomeClass(found.reason), time.Since(started), found.reason)
		} else {
			logContextOutcome("delivered", time.Since(started),
				fmt.Sprintf("%d bytes", len(found.text)))
		}
		return out

	case <-time.After(deadline):
		logContextOutcome("failed", time.Since(started), "deadline exceeded")
		return "{}"
	}
}

// contextOutcome carries WHY there is no context, not merely that there is none.
// Collapsing every cause into an empty string made a broken hook and a quiet one
// look identical in the log (`RUN:2026-08-11`).
type contextOutcome struct {
	text   string
	reason string
}

// Only a genuine "nothing matched" is `empty`; everything else is a failure and
// is filed as one, so a degraded hook is visible in a glance down the log.
func outcomeClass(reason string) string {
	switch reason {
	case "not relevant", "empty prompt":
		return "empty"
	default:
		return "failed"
	}
}

// logContextOutcome appends one line per run, mirroring the sync family's
// `.modernpath/sync-hooks.log`. Without it, a hook that has silently stopped
// working looks exactly like a hook with nothing to say.
func logContextOutcome(outcome string, took time.Duration, detail string) {
	line := fmt.Sprintf("%s · %s · %.1fs · %s\n",
		time.Now().UTC().Format(time.RFC3339), outcome, took.Seconds(), detail)

	f, err := os.OpenFile(filepath.Join(".modernpath", "context-hook.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}

// contextHookOutput renders the agent-facing response. The envelope shape is the
// agents' documented contract (`hookSpecificOutput.additionalContext`); anything
// else is parsed, matched against no known field, and silently discarded — the
// context fetched and thrown away (measured RUN:2026-08-08, 0/2 delivered).
func contextHookOutput(event, prompt string, fetch func(string) string) string {
	if strings.TrimSpace(prompt) == "" {
		return "{}"
	}

	found := strings.TrimSpace(fetch(prompt))
	if found == "" {
		return "{}"
	}

	out, err := json.Marshal(map[string]interface{}{
		"hookSpecificOutput": map[string]interface{}{
			"hookEventName":     event,
			"additionalContext": "## ModernPath Architectural Context\n\n" + found + "\n\n---",
		},
	})
	if err != nil {
		return "{}"
	}
	return string(out)
}

// fetchContextOutcome returns the server's relevance-checked context together
// with the reason there is none, so the log can distinguish a workspace with
// nothing to say from one that cannot reach its server.
func fetchContextOutcome(prompt string) contextOutcome {
	if !config.IsInitialized() {
		return contextOutcome{reason: "no workspace config"}
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		return contextOutcome{reason: "unreadable workspace config"}
	}

	if contextMaxQueries < 1 {
		contextMaxQueries = 1
	} else if contextMaxQueries > 5 {
		contextMaxQueries = 5
	}

	apiURL := fmt.Sprintf("%s/api/mcp/tools/context_search", cfg.APIURL)

	payload := map[string]interface{}{
		"arguments": map[string]interface{}{
			"system_id":         cfg.SystemID,
			"prompt":            prompt,
			"max_queries":       contextMaxQueries,
			"max_output_tokens": contextMaxTokens,
		},
	}

	jsonPayload, _ := json.Marshal(payload)

	resp, err := api.DoAuthenticatedPostRaw(apiURL, bytes.NewBuffer(jsonPayload), 60*time.Second)
	if err != nil {
		return contextOutcome{reason: fmt.Sprintf("request failed: %v", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The server's own message, trimmed. A bare status code sends the reader
		// to the server logs for something the response already said.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return contextOutcome{
			reason: strings.TrimSpace(fmt.Sprintf("server error %d %s", resp.StatusCode, detail)),
		}
	}

	var result contextResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return contextOutcome{reason: "unreadable response"}
	}

	if !result.Success {
		return contextOutcome{reason: serverReason(result.Error)}
	}

	if !result.Result.Relevant {
		return contextOutcome{reason: "not relevant"}
	}

	return contextOutcome{text: result.Result.Context, reason: "delivered"}
}

func serverReason(err string) string {
	if strings.TrimSpace(err) == "" {
		return "server declined without a reason"
	}
	return "server: " + err
}
