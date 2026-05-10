package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/modernpath/cli/internal/api"
	"github.com/modernpath/cli/internal/config"
	"github.com/spf13/cobra"
)

var (
	docsOnly  bool
	filesOnly bool
	limit     int
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search documentation and code files",
	Long: `Search through documentation and analyzed code files via the ModernPath API.

Examples:
  modernpath search "authentication"
  modernpath search "user login" --docs-only
  modernpath search "error handling" --files-only
  modernpath search "API" --limit=20`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSearch,
}

func init() {
	searchCmd.Flags().BoolVar(&docsOnly, "docs-only", false, "Search only documentation")
	searchCmd.Flags().BoolVar(&filesOnly, "files-only", false, "Search only file analyses")
	searchCmd.Flags().IntVar(&limit, "limit", 10, "Maximum results to return")
}

type SearchResponse struct {
	Data struct {
		Docs []struct {
			ID      interface{} `json:"id"`
			Title   string      `json:"title"`
			Summary string      `json:"summary"`
			Tier    string      `json:"tier"`
			Angle   string      `json:"angle"`
		} `json:"docs"`
		Files []struct {
			ID           interface{} `json:"id"`
			RelativePath string      `json:"relative_path"`
			Language     string      `json:"language"`
			Purpose      string      `json:"purpose"`
			Summary      string      `json:"summary"`
		} `json:"files"`
	} `json:"data"`
	Error string `json:"error,omitempty"`
}

func runSearch(cmd *cobra.Command, args []string) error {
	if !config.IsInitialized() {
		printError("Not initialized. Run 'modernpath init' first.\n")
		return nil
	}

	cfg, err := config.ReadConfig()
	if err != nil {
		printError("Failed to read config: %v\n", err)
		return err
	}

	query := strings.Join(args, " ")

	// Build request URL
	baseURL := cfg.APIURL
	if baseURL == "" {
		baseURL = config.DefaultAPIURL
	}

	params := url.Values{}
	params.Set("system_id", strconv.Itoa(cfg.SystemID))
	params.Set("q", query)
	params.Set("limit", strconv.Itoa(limit))

	searchType := "all"
	if docsOnly {
		searchType = "docs"
	} else if filesOnly {
		searchType = "files"
	}
	params.Set("type", searchType)

	requestURL := fmt.Sprintf("%s/api/search?%s", baseURL, params.Encode())

	printInfo("Searching for: %s\n\n", query)

	// Make authenticated request
	resp, err := api.DoAuthenticatedGet(requestURL, 30*time.Second)
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
		} else {
			printError("API error: HTTP %d - %s\n", resp.StatusCode, string(body))
		}
		return nil
	}

	var searchResp SearchResponse
	if err := json.Unmarshal(body, &searchResp); err != nil {
		printError("Failed to parse response: %v\n", err)
		return err
	}

	totalResults := 0

	// Display doc results
	if len(searchResp.Data.Docs) > 0 {
		bold := color.New(color.Bold)
		bold.Printf("📚 Documentation (%d results)\n", len(searchResp.Data.Docs))
		fmt.Println("─────────────────────────────────────────")

		for _, doc := range searchResp.Data.Docs {
			tierColor := color.New(color.FgCyan)
			tierColor.Printf("[%s/%s] ", doc.Tier, doc.Angle)
			fmt.Printf("%s\n", doc.Title)
			if doc.Summary != "" {
				gray := color.New(color.FgHiBlack)
				summary := doc.Summary
				if len(summary) > 100 {
					summary = summary[:97] + "..."
				}
				gray.Printf("  %s\n", summary)
			}
			fmt.Println()
		}
		totalResults += len(searchResp.Data.Docs)
	}

	// Display file results
	if len(searchResp.Data.Files) > 0 {
		bold := color.New(color.Bold)
		bold.Printf("📁 Files (%d results)\n", len(searchResp.Data.Files))
		fmt.Println("─────────────────────────────────────────")

		for _, file := range searchResp.Data.Files {
			langColor := color.New(color.FgYellow)
			lang := file.Language
			if lang == "" {
				lang = "unknown"
			}
			langColor.Printf("[%s] ", lang)
			fmt.Printf("%s\n", file.RelativePath)
			if file.Purpose != "" {
				gray := color.New(color.FgHiBlack)
				purpose := file.Purpose
				if len(purpose) > 100 {
					purpose = purpose[:97] + "..."
				}
				gray.Printf("  %s\n", purpose)
			}
			fmt.Println()
		}
		totalResults += len(searchResp.Data.Files)
	}

	if totalResults == 0 {
		printWarning("No results found for '%s'\n", query)
	} else {
		printSuccess("Found %d results\n", totalResults)
	}

	return nil
}
