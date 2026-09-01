package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"net/url"
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
  modernpath read-doc --tier=subsystem --angle=architecture`,
	RunE: runReadDoc,
}

func init() {
	// read-file flags
	readFileCmd.Flags().IntVar(&readStartLine, "start", 0, "Start line number (1-based)")
	readFileCmd.Flags().IntVar(&readEndLine, "end", 0, "End line number (inclusive)")

	// read-doc flags
	readDocCmd.Flags().StringVar(&readDocID, "id", "", "Document ID to retrieve")
	readDocCmd.Flags().StringVar(&readDocTier, "tier", "", "Filter by tier (module, subsystem, architecture)")
	readDocCmd.Flags().StringVar(&readDocAngle, "angle", "", "Filter by angle (e.g., architecture, api, data)")
	readDocCmd.Flags().BoolVar(&readDocList, "list", false, "List all available documents")

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
		ID      string `json:"id,omitempty"`
		Title   string `json:"title,omitempty"`
		Content string `json:"content,omitempty"`
		Summary string `json:"summary,omitempty"`
		Tier    string `json:"tier,omitempty"`
		Angle   string `json:"angle,omitempty"`
		
		// List/search results
		Documents []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Summary string `json:"summary"`
			Tier    string `json:"tier"`
			Angle   string `json:"angle"`
		} `json:"documents,omitempty"`
	} `json:"data"`
}

func runReadFile(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
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

	// Make authenticated request
	client := newReadAPIClient(cfg)
	resp, err := client.Get(requestURL)
	if err != nil {
		printError("Failed to connect to API: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		printError("Failed to read response: %v\n", err)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			printError("API error: %s\n", errResp.Error)
			return fmt.Errorf("API error: %s", errResp.Error)
		}
		printError("API error: HTTP %d - %s\n", resp.StatusCode, string(body))
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	var fileResp FileReadResponse
	if err := json.Unmarshal(body, &fileResp); err != nil {
		printError("Failed to parse response: %v\n", err)
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
	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	if cfg.SystemID == 0 {
		printError("No system configured. Run 'modernpath init' first.\n")
		return nil
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
	} else if readDocList {
		params.Set("list", "true")
	} else if len(args) > 0 {
		params.Set("title", strings.Join(args, " "))
	} else if readDocTier != "" || readDocAngle != "" {
		// Filter mode
		if readDocTier != "" {
			params.Set("tier", readDocTier)
		}
		if readDocAngle != "" {
			params.Set("angle", readDocAngle)
		}
	} else {
		// Default to list
		params.Set("list", "true")
	}

	requestURL := fmt.Sprintf("%s/api/docs/read?%s", baseURL, params.Encode())

	// Make authenticated request
	client := newReadAPIClient(cfg)
	resp, err := client.Get(requestURL)
	if err != nil {
		printError("Failed to connect to API: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		printError("Failed to read response: %v\n", err)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			printError("API error: %s\n", errResp.Error)
			return fmt.Errorf("API error: %s", errResp.Error)
		}
		printError("API error: HTTP %d - %s\n", resp.StatusCode, string(body))
		return fmt.Errorf("API error: HTTP %d", resp.StatusCode)
	}

	var docResp DocReadResponse
	if err := json.Unmarshal(body, &docResp); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	// Display based on response type
	if len(docResp.Data.Documents) > 0 {
		displayDocList(&docResp)
	} else if docResp.Data.Content != "" {
		displayDocContent(&docResp)
	} else {
		printWarning("No documents found.\n")
	}

	return nil
}

func displayDocList(resp *DocReadResponse) {
	docs := resp.Data.Documents

	fmt.Println()
	bold := color.New(color.Bold)
	bold.Printf("📚 Documentation (%d documents)\n", len(docs))
	fmt.Println("═════════════════════════════════════════════════════════════════")
	fmt.Println()

	for _, doc := range docs {
		tierColor := color.New(color.FgCyan)
		tierColor.Printf("[%s/%s] ", doc.Tier, doc.Angle)
		
		bold.Printf("%s\n", doc.Title)
		
		if doc.Summary != "" {
			gray := color.New(color.FgHiBlack)
			summary := doc.Summary
			if len(summary) > 100 {
				summary = summary[:97] + "..."
			}
			gray.Printf("  %s\n", summary)
		}
		
		gray := color.New(color.FgHiBlack)
		gray.Printf("  ID: %s\n", doc.ID)
		fmt.Println()
	}
}

func displayDocContent(resp *DocReadResponse) {
	d := resp.Data

	fmt.Println()
	bold := color.New(color.Bold)
	gray := color.New(color.FgHiBlack)
	tierColor := color.New(color.FgCyan)

	tierColor.Printf("[%s/%s] ", d.Tier, d.Angle)
	bold.Printf("%s\n", d.Title)
	gray.Printf("ID: %s\n", d.ID)
	fmt.Println("═════════════════════════════════════════════════════════════════")
	
	if d.Summary != "" {
		fmt.Println()
		gray.Printf("Summary: %s\n", d.Summary)
	}
	
	fmt.Println()
	fmt.Println(d.Content)
	fmt.Println()
}

// newReadAPIClient creates an authenticated HTTP client for read operations
func newReadAPIClient(cfg *config.Config) *authenticatedClient {
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	token := ""
	auth, _ := config.ReadAuth()
	if auth != nil {
		token = auth.Token
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
