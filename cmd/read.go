package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

// CLI commands for reading files and documents via API
// Used during modernization workflow to fetch additional context beyond locally synced files

var (
	readStartLine int
	readEndLine   int
	readDocID     string
	readDocTier   string
	readDocAngle  string
	readDocList   bool
	readFileJSON  bool
	readDocJSON   bool
	readDocQuery  string
	readDocLimit  int
	readDocCursor string
	readDocOrder  string
)

var readFileCmd = &cobra.Command{
	Use:   "read-file <file-path>",
	Short: "Read source code file content via API",
	Long: `Read a source code file from the analyzed repository via the ModernPath API.

This command fetches file content from the server-side cloned repository,
which may not be present locally. Useful for accessing source files during
transformation workflows.

Examples:
  modernpath read-file src/main.py
  modernpath read-file lib/parser.ex --start 10 --end 50
  modernpath read-file config/database.yml`,
	Args: cobra.ExactArgs(1),
	RunE: runReadFile,
}

var readDocCmd = &cobra.Command{
	Use:   "read-doc [title-search]",
	Short: "Read system documentation via API",
	Long: `Read system documentation from the ModernPath API.

This command fetches documentation that may not be synced locally,
such as detailed tier-specific docs or newly generated content.

Examples:
  modernpath read-doc --list                    # List all docs
  modernpath read-doc --id=doc_abc123           # Get specific doc by ID
  modernpath read-doc "Authentication"          # Search by title
  modernpath read-doc --tier=module             # Filter by tier
  modernpath read-doc --list --tier=subsystem    # List only subsystem docs
  modernpath read-doc --tier=subsystem --angle=architecture`,
	RunE: runReadDoc,
}

func init() {
	readFileCmd.Flags().BoolVar(&readFileJSON, "json", false, "Emit the complete file response as JSON")
	readDocCmd.Flags().BoolVar(&readDocJSON, "json", false, "Emit complete documents as JSON")
	// read-file flags
	readFileCmd.Flags().IntVar(&readStartLine, "start", 0, "Start line number (1-based)")
	readFileCmd.Flags().IntVar(&readEndLine, "end", 0, "End line number (inclusive)")

	// read-doc flags
	readDocCmd.Flags().StringVar(&readDocID, "id", "", "Document ID to retrieve")
	readDocCmd.Flags().StringVar(&readDocTier, "tier", "", "Filter by tier (module, subsystem, architecture)")
	readDocCmd.Flags().StringVar(&readDocAngle, "angle", "", "Filter by angle (e.g., architecture, api, data)")
	readDocCmd.Flags().BoolVar(&readDocList, "list", false, "List available documents, optionally filtered by tier or angle")
	readDocCmd.Flags().StringVar(&readDocQuery, "query", "", "Search document titles")
	readDocCmd.Flags().IntVar(&readDocLimit, "limit", 20, "Documents per page (1–200)")
	readDocCmd.Flags().StringVar(&readDocCursor, "cursor", "", "Continue from a previous page")
	readDocCmd.Flags().StringVar(&readDocOrder, "order", "", "Order: tier, title, tier_desc")
	addRecordReadFlags(readDocCmd)

	// Add commands to root
	rootCmd.AddCommand(readFileCmd)
	rootCmd.AddCommand(readDocCmd)
}

// FileReadResponse represents the API response for file reading
type FileReadResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		FilePath   string `json:"file_path"`
		Content    string `json:"content"`
		Language   string `json:"language"`
		TotalLines int    `json:"total_lines"`
		StartLine  int    `json:"start_line,omitempty"`
		EndLine    int    `json:"end_line,omitempty"`
	} `json:"data"`
}

// DocReadResponse represents the API response for document reading
type DocReadResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Data    struct {
		// Single doc fields
		ID      any    `json:"id,omitempty"`
		Title   string `json:"title,omitempty"`
		Content string `json:"content,omitempty"`
		Summary string `json:"summary,omitempty"`
		Tier    string `json:"tier,omitempty"`
		Angle   string `json:"angle,omitempty"`

		// List/search results
		Documents []struct {
			ID      any    `json:"id"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
			Tier    string `json:"tier"`
			Angle   string `json:"angle"`
		} `json:"documents,omitempty"`
	} `json:"data"`
}

func runReadFile(cmd *cobra.Command, args []string) error {
	// REQ-CROSS-405: binding and credential statements before any request.
	env, err := apiClientCredentialLoad()
	if err != nil {
		printError("%v\n", err)
		return err
	}

	cfg, err := commandConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	filePath := args[0]

	// Build request URL
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	params := url.Values{}
	params.Set("system_id", strconv.Itoa(cfg.SystemID))
	params.Set("file_path", filePath)

	if readStartLine > 0 {
		params.Set("start_line", strconv.Itoa(readStartLine))
	}
	if readEndLine > 0 {
		params.Set("end_line", strconv.Itoa(readEndLine))
	}

	requestURL := fmt.Sprintf("%s/api/files/read?%s", baseURL, params.Encode())

	route := strings.TrimPrefix(requestURL, baseURL)
	status, response, err := env.call("GET", route, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return serverRefusal("read", status, response)
	}
	if success, exists := response["success"]; exists && success == false {
		return serverRefusal("read failed", status, response)
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("read response is missing data")
	}
	if readFileJSON {
		return writeSearchJSON(cmd.OutOrStdout(), data)
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	var fileResp FileReadResponse
	if err := json.Unmarshal(body, &fileResp); err != nil {
		return err
	}

	// Display file content
	displayFileContent(&fileResp)
	return nil
}

func displayFileContent(resp *FileReadResponse) {
	d := resp.Data

	// Header
	bold := color.New(color.Bold)
	gray := color.New(color.FgHiBlack)

	fmt.Println()
	bold.Printf("📄 %s\n", d.FilePath)
	gray.Printf("   Language: %s | Total lines: %d", d.Language, d.TotalLines)
	if d.StartLine > 0 && d.EndLine > 0 {
		gray.Printf(" | Showing lines %d-%d", d.StartLine, d.EndLine)
	}
	fmt.Println()
	fmt.Println("─────────────────────────────────────────────────────────────────")

	// Content with line numbers (server-side numbering stripped first)
	lines := strings.Split(stripServerLineNumbers(d.Content), "\n")
	startNum := 1
	if d.StartLine > 0 {
		startNum = d.StartLine
	}

	for i, line := range lines {
		lineNum := startNum + i
		gray.Printf("%4d │ ", lineNum)
		fmt.Println(line)
	}

	fmt.Println("─────────────────────────────────────────────────────────────────")
	fmt.Println()
}

func runReadDoc(cmd *cobra.Command, args []string) error {
	allowedFields := []string{"id", "title", "summary", "tier", "angle", "target_path"}
	if readDocID != "" {
		allowedFields = append(allowedFields, "content")
	}
	if err := validateRecordSelection(cmd, allowedFields); err != nil {
		return err
	}
	// REQ-CROSS-405: binding and credential statements before any request.
	env, err := apiClientCredentialLoad()
	if err != nil {
		printError("%v\n", err)
		return err
	}

	cfg, err := commandConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	// Build request URL
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	params := url.Values{}
	params.Set("system_id", strconv.Itoa(cfg.SystemID))

	// Determine request type
	if readDocID != "" {
		params.Set("doc_id", readDocID)
	} else {
		params.Set("paged", "true")
		query := readDocQuery
		if query == "" && len(args) > 0 && !readDocList {
			query = strings.Join(args, " ")
		}
		if query != "" {
			params.Set("q", query)
		}
		if readDocLimit < 1 || readDocLimit > 200 {
			return fmt.Errorf("--limit must be between 1 and 200")
		}
		params.Set("limit", strconv.Itoa(readDocLimit))
		if readDocCursor != "" {
			params.Set("cursor", readDocCursor)
		}
		if readDocOrder != "" {
			params.Set("order", readDocOrder)
		}
		if readDocTier != "" {
			params.Set("tier", readDocTier)
		}
		if readDocAngle != "" {
			params.Set("angle", readDocAngle)
		}
	}

	requestURL := fmt.Sprintf("%s/api/docs/read?%s", baseURL, params.Encode())

	route := strings.TrimPrefix(requestURL, baseURL)
	status, response, err := env.call("GET", route, nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return serverRefusal("read", status, response)
	}
	if success, exists := response["success"]; exists && success == false {
		return serverRefusal("read failed", status, response)
	}
	data, ok := response["data"].(map[string]any)
	if !ok {
		return fmt.Errorf("read response is missing data")
	}
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	var docResp DocReadResponse
	if err := json.Unmarshal(body, &docResp); err != nil {
		return err
	}
	if len(docResp.Data.Documents) > 0 || data["documents"] != nil {
		selected, err := selectSearchItems(cmd, anyRows(data["documents"]), []string{"id", "title", "tier", "angle"}, allowedFields, []string{"id", "title"})
		if err != nil {
			return err
		}
		if readDocJSON {
			return writeSearchJSON(cmd.OutOrStdout(), map[string]any{"items": selected, "next_cursor": data["next_cursor"]})
		}
		for _, row := range selected {
			if cmd.Flags().Changed("fields") || flagIsTrue(cmd, "full") {
				if err := writeRecord(cmd, row); err != nil {
					return err
				}
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%v  %s\n", row["id"], strings.Join(strings.Fields(fmt.Sprint(row["title"])), " "))
			}
		}
		if cursor, _ := data["next_cursor"].(string); cursor != "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "Next cursor: %s (repeat the same filters with --cursor)\n", cursor)
		}
		return nil
	} else {
		selected, err := selectRecordFields(cmd, data, []string{"id", "title", "tier", "angle", "summary", "content"}, allowedFields, []string{"id", "title"})
		if err != nil {
			return err
		}
		if readDocJSON {
			return writeCommandJSON(cmd, selected)
		}
		body, err = json.Marshal(map[string]any{"data": selected})
		if err != nil {
			return err
		}
		if err := json.Unmarshal(body, &docResp); err != nil {
			return err
		}
	}

	if cmd.Flags().Changed("fields") || flagIsTrue(cmd, "full") {
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return err
		}
		return writeRecord(cmd, envelope.Data)
	}
	displayDocContent(&docResp)
	return nil
}

func displayDocContent(resp *DocReadResponse) {
	d := resp.Data

	fmt.Println()
	bold := color.New(color.Bold)
	gray := color.New(color.FgHiBlack)
	tierColor := color.New(color.FgCyan)

	tierColor.Printf("[%s/%s] ", d.Tier, d.Angle)
	bold.Printf("%s\n", d.Title)
	gray.Printf("ID: %v\n", d.ID)
	fmt.Println("═════════════════════════════════════════════════════════════════")

	if d.Summary != "" {
		fmt.Println()
		gray.Printf("Summary: %s\n", d.Summary)
	}

	fmt.Println()
	fmt.Println(d.Content)
	fmt.Println()
}

// newReadAPIClient creates an HTTP client for read operations carrying the
// bearer the credential pre-check established.
func newReadAPIClient(cfg *config.Config, token string) *authenticatedClient {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	return &authenticatedClient{
		client:  &http.Client{Timeout: 30 * time.Second},
		token:   token,
		baseURL: baseURL,
	}
}

// stripServerLineNumbers removes the server's own "   N | " line numbering
// when EVERY non-empty line carries it, so the CLI's gutter is the only one
// (REQ-CROSS-210 cosmetics: read-file rendered "1 │     1 | defmodule…").
// Mixed content passes through untouched — a file whose real text happens to
// contain that shape on some lines must not be mangled.
func stripServerLineNumbers(content string) string {
	lines := strings.Split(content, "\n")

	numbered := false
	for _, l := range lines {
		if l == "" {
			continue
		}
		if !serverNumberedLineRe.MatchString(l) {
			return content
		}
		numbered = true
	}
	if !numbered {
		return content
	}

	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = serverNumberedLineRe.ReplaceAllString(l, "")
	}
	return strings.Join(out, "\n")
}

var serverNumberedLineRe = regexp.MustCompile(`^\s*\d+ \| `)
