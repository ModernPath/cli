package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	contextMaxQueries int
	contextMaxTokens  int
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
	Args: cobra.MinimumNArgs(1),
	RunE: runContext,
}

func init() {
	contextCmd.Flags().IntVar(&contextMaxQueries, "max-queries", 3, "Maximum decomposed queries (1-5)")
	contextCmd.Flags().IntVar(&contextMaxTokens, "max-tokens", 3000, "Maximum output tokens")
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
	prompt := strings.Join(args, " ")

	if !config.IsInitialized() {
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		return nil
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
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	var result contextResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	if !result.Success {
		return nil
	}

	if !result.Result.Relevant {
		return nil
	}

	if result.Result.Context == "" {
		return nil
	}

	fmt.Print(result.Result.Context)
	return nil
}
