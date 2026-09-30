package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
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

Uses the ModernPath platform's agentic search to find relevant documentation,
code, and the curated patterns and capabilities the system uses, then
synthesizes an answer. Each source names its kind and id.

A question that takes the server longer than 45 seconds keeps running there:
ask waits for the answer, showing "Still working (m:ss)…" on stderr in pretty
format, for up to 10 minutes.

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
	Success bool   `json:"success"`
	Error   string `json:"error"`
	Result  struct {
		// Status is "done", or "running" with an AskID to poll (REQ-CROSS-490).
		Status     string          `json:"status"`
		AskID      string          `json:"ask_id"`
		Answer     string          `json:"answer"`
		Sources    []askWireSource `json:"sources"`
		Iterations int             `json:"iterations"`
	} `json:"result"`
}

// REQ-CROSS-490: a slow ask keeps running on the server and is polled.
const (
	// askWaitLimitDefault is how long ask waits for an answer in all. It is
	// reached only when the server's ask queue is backlogged.
	askWaitLimitDefault = 10 * time.Minute
	// askPollFloor is the least time between two requests. Each poll blocks on
	// the server for up to 45 s, so this matters only if it answers at once.
	askPollFloor = time.Second
)

// askWaitLimit is askWaitLimitDefault unless MODERNPATH_ASK_WAIT_LIMIT names
// a Go duration, such as "30s" or "20m".
func askWaitLimit() time.Duration {
	if d, err := time.ParseDuration(os.Getenv("MODERNPATH_ASK_WAIT_LIMIT")); err == nil && d > 0 {
		return d
	}
	return askWaitLimitDefault
}

// askSource is a source as ask prints it, and as --format=json emits it.
type askSource struct {
	Title string `json:"title"`
	Type  string `json:"type"`
	Path  string `json:"path"`
}

// askWireSource adds the ids the server sends, used only to list each source
// once; they never reach the output.
type askWireSource struct {
	askSource
	FileID any `json:"file_id"`
	DocID  any `json:"doc_id"`
}

// label is what names a source: its title, or its path when it has none (a
// file source has no title).
func (s askSource) label() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Path
}

// uniqueAskSources lists each source once, by file, document or path, in the
// order the server first named it. The server lists a file once per tool
// call that read it.
func uniqueAskSources(wire []askWireSource) []askSource {
	seen := map[string]bool{}
	out := make([]askSource, 0, len(wire))
	for _, src := range wire {
		var key string
		switch {
		case src.FileID != nil:
			key = fmt.Sprintf("file:%v", src.FileID)
		case src.DocID != nil:
			key = fmt.Sprintf("doc:%v", src.DocID)
		case src.Path != "":
			key = "path:" + src.Path
		default:
			key = "title:" + src.Type + "\x00" + src.Title
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, src.askSource)
	}
	return out
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args, " ")

	// REQ-CROSS-405: binding and credential statements before any request.
	env, err := apiClientCredentialLoad()
	if err != nil {
		if askFormat == "json" {
			fmt.Printf(`{"error": %q}`+"\n", err.Error())
		} else {
			printError("%v\n", err)
		}
		return err
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

	started := time.Now()
	result, err := postAskTool(env, fmt.Sprintf("%s/api/mcp/tools/agentic_search", cfg.APIURL), map[string]interface{}{
		"system_id":      cfg.SystemID,
		"question":       question,
		"max_iterations": askIterations,
	})
	if err == nil && result.Result.Status == "running" {
		result, err = pollAsk(env, cfg.APIURL, result.Result.AskID, started)
	}
	if err != nil {
		if askFormat == "json" {
			fmt.Printf(`{"error": %q}`+"\n", err.Error())
		} else {
			printError("%v\n", err)
		}
		return err
	}

	sources := uniqueAskSources(result.Result.Sources)

	// Output based on format
	switch askFormat {
	case "json":
		outputAskJSON(result, sources, question)
	case "markdown":
		outputAskMarkdown(result, sources, question)
	default:
		outputAskPretty(result, sources)
	}

	return nil
}

// postAskTool calls one of the ask tools and returns its decoded reply. A
// refused credential, an HTTP error and a failed tool call are errors.
func postAskTool(env *factoryEnv, url string, arguments map[string]interface{}) (askResult, error) {
	var result askResult
	jsonPayload, _ := json.Marshal(map[string]interface{}{"arguments": arguments})

	resp, err := api.DoPostWithToken(url, bytes.NewBuffer(jsonPayload), env.token, 120*time.Second)
	if err != nil {
		return result, fmt.Errorf("API error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return result, env.credentialRejected()
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &result); err != nil {
		if resp.StatusCode != http.StatusOK {
			return result, fmt.Errorf("API error: %s", string(body))
		}
		return result, fmt.Errorf("Failed to parse response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || !result.Success {
		reason := result.Error
		if unquoted, err := strconv.Unquote(reason); err == nil {
			reason = unquoted
		}
		if reason == "" {
			reason = "Search failed"
		}
		return result, fmt.Errorf("%s", reason)
	}
	return result, nil
}

// pollAsk waits for a running ask through agentic_search_result until it is
// done, has failed, or the wait limit counted from started has passed.
func pollAsk(env *factoryEnv, apiURL, askID string, started time.Time) (askResult, error) {
	limit := askWaitLimit()
	url := fmt.Sprintf("%s/api/mcp/tools/agentic_search_result", apiURL)
	progress := askFormat == "pretty"
	if progress {
		defer fmt.Fprintln(os.Stderr)
	}
	lastRequest := started

	for {
		if wait := askPollFloor - time.Since(lastRequest); wait > 0 {
			if left := limit - time.Since(started); left < wait {
				wait = left
			}
			if wait > 0 {
				time.Sleep(wait)
			}
		}
		if time.Since(started) >= limit {
			return askResult{}, fmt.Errorf("ask %s: the answer was not ready after %s", askID, limit)
		}
		if progress {
			fmt.Fprintf(os.Stderr, "\rStill working (%s)…", askClock(time.Since(started)))
		}

		lastRequest = time.Now()
		result, err := postAskTool(env, url, map[string]interface{}{"ask_id": askID})
		if err != nil {
			return result, fmt.Errorf("ask %s: %v", askID, err)
		}
		if result.Result.Status != "running" {
			return result, nil
		}
	}
}

// askClock renders an elapsed time as m:ss.
func askClock(d time.Duration) string {
	s := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func outputAskJSON(result askResult, sources []askSource, question string) {
	output := map[string]interface{}{
		"question":   question,
		"answer":     result.Result.Answer,
		"iterations": result.Result.Iterations,
	}
	if !askBrief && len(sources) > 0 {
		output["sources"] = sources
	}
	jsonBytes, _ := json.Marshal(output)
	fmt.Println(string(jsonBytes))
}

func outputAskMarkdown(result askResult, sources []askSource, question string) {
	var sb strings.Builder

	if !askBrief {
		sb.WriteString(fmt.Sprintf("## Question: %s\n\n", question))
	}

	sb.WriteString(result.Result.Answer)

	if !askBrief && len(sources) > 0 {
		sb.WriteString("\n\n### Sources\n\n")
		for _, src := range sources {
			if src.Title == "" {
				sb.WriteString(fmt.Sprintf("- [%s] `%s`", src.Type, src.Path))
			} else {
				sb.WriteString(fmt.Sprintf("- [%s] %s", src.Type, src.Title))
				if src.Path != "" {
					sb.WriteString(fmt.Sprintf(" (`%s`)", src.Path))
				}
			}
			sb.WriteString("\n")
		}
	}

	fmt.Print(sb.String())
}

func outputAskPretty(result askResult, sources []askSource) {
	bold := color.New(color.Bold)

	// Show sources
	if !askBrief && len(sources) > 0 {
		fmt.Println()
		bold.Println("📚 Sources")
		fmt.Println("───────────────────────────────────────────────────────────")
		for _, src := range sources {
			typeColor := color.New(color.FgCyan)
			typeColor.Printf("[%s] ", src.Type)
			fmt.Printf("%s\n", src.label())
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
