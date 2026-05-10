package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question about the codebase using AI",
	Long: `Ask a natural language question about your codebase.

Uses the ModernPath platform's agentic search to find relevant documentation
and code, then synthesizes an answer.

Examples:
  modernpath ask "How does authentication work?"
  modernpath ask "What are the main data models?"
  modernpath ask "Where is error handling implemented?"`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

func init() {
	rootCmd.AddCommand(askCmd)
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	bold := color.New(color.Bold)

	fmt.Println()
	bold.Printf("❓ Question: %s\n", question)
	fmt.Println("═══════════════════════════════════════════════════════════")

	printInfo("Asking ModernPath platform...\n")

	// Call the remote API
	url := fmt.Sprintf("%s/api/mcp/tools/agentic_search", cfg.APIURL)

	payload := map[string]interface{}{
		"arguments": map[string]interface{}{
			"system_id":      cfg.SystemID,
			"question":       question,
			"max_iterations": 5,
		},
	}

	jsonPayload, _ := json.Marshal(payload)

	resp, err := api.DoAuthenticatedPostRaw(url, bytes.NewBuffer(jsonPayload), 120*time.Second)
	if err != nil {
		printError("API error: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		printError("API error: %s\n", string(body))
		return fmt.Errorf("API error: %s", resp.Status)
	}

	var result struct {
		Success bool `json:"success"`
		Result  struct {
			Answer  string `json:"answer"`
			Sources []struct {
				Title string `json:"title"`
				Type  string `json:"type"`
				Path  string `json:"path"`
			} `json:"sources"`
			Iterations int `json:"iterations"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	if !result.Success {
		printError("Search failed\n")
		return nil
	}

	// Show sources
	if len(result.Result.Sources) > 0 {
		fmt.Println()
		bold.Println("📚 Sources")
		fmt.Println("───────────────────────────────────────────────────────────")
		for _, src := range result.Result.Sources {
			typeColor := color.New(color.FgCyan)
			typeColor.Printf("[%s] ", src.Type)
			fmt.Printf("%s\n", src.Title)
		}
	}

	// Show answer
	fmt.Println()
	bold.Println("💡 Answer")
	fmt.Println("───────────────────────────────────────────────────────────")
	fmt.Println(result.Result.Answer)

	fmt.Println()
	gray := color.New(color.FgHiBlack)
	gray.Printf("(Completed in %d iterations)\n", result.Result.Iterations)

	return nil
}
