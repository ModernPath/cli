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

var (
	askFormat     string
	askBrief      bool
	askIterations int
)

var askCmd = &cobra.Command{
	Use:   "ask <question>",
	Short: "Ask a question about the codebase using AI",
	Long: `Ask a natural language question about your codebase.

Uses the ModernPath platform's agentic search to find relevant documentation
and code, then synthesizes an answer.

Output formats:
  --format=pretty   Colored terminal output (default)
  --format=markdown Plain markdown for hooks/injection
  --format=json     JSON for programmatic use

Examples:
  modernpath ask "How does authentication work?"
  modernpath ask "What are the main data models?" --format=markdown
  modernpath ask "Where is error handling?" --brief`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAsk,
}

func init() {
	askCmd.Flags().StringVar(&askFormat, "format", "pretty", "Output format: pretty, markdown, json")
	askCmd.Flags().BoolVar(&askBrief, "brief", false, "Brief output (answer only, no sources)")
	askCmd.Flags().IntVar(&askIterations, "iterations", 5, "Max search iterations (1-10)")
	rootCmd.AddCommand(askCmd)
}

type askResult struct {
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

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	if !config.IsInitialized() {
		if askFormat == "json" {
			fmt.Println(`{"error": "Not initialized. Run 'modernpath init' first."}`)
		} else {
			printError("Not initialized. Run 'modernpath init' first.\n")
		}
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		if askFormat == "json" {
			fmt.Printf(`{"error": "Failed to read config: %v"}`, err)
		} else {
			printError("Failed to read config: %v\n", err)
		}
		return err
	}

	// Clamp iterations
	if askIterations < 1 {
		askIterations = 1
	} else if askIterations > 10 {
		askIterations = 10
	}

	// Only show progress for pretty format
	if askFormat == "pretty" {
		bold := color.New(color.Bold)
		fmt.Println()
		bold.Printf("❓ Question: %s\n", question)
		fmt.Println("═══════════════════════════════════════════════════════════")
		printInfo("Asking ModernPath platform...\n")
	}

	// Call the remote API
	apiURL := fmt.Sprintf("%s/api/mcp/tools/agentic_search", cfg.APIURL)

	payload := map[string]interface{}{
		"arguments": map[string]interface{}{
			"system_id":      cfg.SystemID,
			"question":       question,
			"max_iterations": askIterations,
		},
	}

	jsonPayload, _ := json.Marshal(payload)

	resp, err := api.DoAuthenticatedPostRaw(apiURL, bytes.NewBuffer(jsonPayload), 120*time.Second)
	if err != nil {
		if askFormat == "json" {
			fmt.Printf(`{"error": "API error: %v"}`, err)
		} else {
			printError("API error: %v\n", err)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if askFormat == "json" {
			fmt.Printf(`{"error": "API error: %s"}`, string(body))
		} else {
			printError("API error: %s\n", string(body))
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	var result askResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		if askFormat == "json" {
			fmt.Printf(`{"error": "Failed to parse response: %v"}`, err)
		} else {
			printError("Failed to parse response: %v\n", err)
		}
		return err
	}

	if !result.Success {
		if askFormat == "json" {
			fmt.Println(`{"error": "Search failed"}`)
		} else {
			printError("Search failed\n")
		}
		return nil
	}

	// Output based on format
	switch askFormat {
	case "json":
		outputAskJSON(result, question)
	case "markdown":
		outputAskMarkdown(result, question)
	default:
		outputAskPretty(result)
	}

	return nil
}

func outputAskJSON(result askResult, question string) {
	output := map[string]interface{}{
		"question":   question,
		"answer":     result.Result.Answer,
		"iterations": result.Result.Iterations,
	}
	if !askBrief && len(result.Result.Sources) > 0 {
		output["sources"] = result.Result.Sources
	}
	jsonBytes, _ := json.Marshal(output)
	fmt.Println(string(jsonBytes))
}

func outputAskMarkdown(result askResult, question string) {
	var sb strings.Builder

	if !askBrief {
		sb.WriteString(fmt.Sprintf("## Question: %s\n\n", question))
	}

	sb.WriteString(result.Result.Answer)

	if !askBrief && len(result.Result.Sources) > 0 {
		sb.WriteString("\n\n### Sources\n\n")
		for _, src := range result.Result.Sources {
			sb.WriteString(fmt.Sprintf("- [%s] %s", src.Type, src.Title))
			if src.Path != "" {
				sb.WriteString(fmt.Sprintf(" (`%s`)", src.Path))
			}
			sb.WriteString("\n")
		}
	}

	fmt.Print(sb.String())
}

func outputAskPretty(result askResult) {
	bold := color.New(color.Bold)

	// Show sources
	if !askBrief && len(result.Result.Sources) > 0 {
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
}
